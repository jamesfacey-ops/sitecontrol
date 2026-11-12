// noticeseal.go — ⟦M-7 / G-049, G-126, 2026-10-03⟧ a post-commit notice
// renders only off a redirect THIS PROCESS wrote.
//
// THE DEFECT. Every warn-accept notice on the gate pages (time warnings,
// the duplicate advisory, NOT ENTERED, the stale-tick notice, the one-click
// arrival's "TRK-14 arrived: 10 checked in", the Edit-person refusal) was
// carried across its redirect as bare query values and rendered from
// whatever the query said. So a hand-typed URL rendered notices for events
// that never happened: `?not_entered=<anyone on the roster>` printed "NOT
// ENTERED — NO ORIENTATION" for a person who was on site, and an anonymous
// `/gate/vehicle?arrived_vehicle=FAKE-99&arrived_checked=42` printed
// "FAKE-99 arrived: 42 checked in" (L02 G-049, L03 G-126). Escaped, never
// XSS — but an officer reads these at the moment of acting on them.
//
// THE FIX: a COMMIT TOKEN. Every redirect that carries a notice is sealed
// here — an HMAC over exactly the notice keys and values it carries plus
// the time it was written, under a key generated when this process starts
// and never stored or sent anywhere. The readers take the notice keys ONLY
// from a request whose seal verifies (noticeQuery); a missing, altered or
// expired seal renders no notice at all — the page itself is untouched, and
// every list on it is still read fresh from the store, as before.
//
// WHAT IT DELIBERATELY DOES NOT DO.
//   - It is not a login check. A sealed URL is a true statement about a
//     commit this post just made; whoever holds it within the window sees
//     the same line the officer saw. That is the same exposure the page
//     already has, and nothing on it is a record field (PII-1b's rules are
//     unchanged).
//   - It survives no restart: the key is per process. A redirect issued
//     just before a restart arrives with a seal the new process cannot
//     verify, so the notice is lost — never invented. The standing lists
//     (/orientation's held-out section, /trips/detail) still say it.
//   - The window (noticeSealTTL) is generous for a page load and short for
//     a bookmark: a sealed URL refreshed an hour later no longer repeats an
//     hour-old "arrived" line as if it were news.
//
// One mechanism for every notice key, listed in noticeQueryKeys — a reader
// added later must take its key from noticeQuery, and its producer must
// route its redirect through sealNotices, or the key reads as forged.
package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// noticeSealQueryKey carries the seal itself: "<unix seconds>.<hex mac>".
const noticeSealQueryKey = "ns"

// noticeSealTTL is how long a sealed redirect keeps rendering its notices.
const noticeSealTTL = 15 * time.Minute

// noticeQueryKeys is every query key that renders a notice. Sorted at use.
var noticeQueryKeys = []string{
	timeWarnQueryKey,
	dupWarnQueryKey,
	aboardWarnQueryKey, // L17-15 (fix3, 2026-10-04): aboardwarn.go
	notEnteredQueryKey,
	staleOrientTickQueryKey,
	arrivedVehicleQueryKey,
	arrivedCheckedQueryKey,
	editSelectQueryKey,
	// ⟦D23 / D22, fix2-fj 2026-10-04⟧ the rest of the URL-driven notice
	// readers, sealed by the same mechanism (FINDINGS-R4 cluster J). Before
	// this, each of them rendered whatever a hand-typed query said:
	// /orientation printed "Recorded 42 orientation completion(s)", "Fake
	// Person no longer holds …" and "X is now on site" for events that never
	// happened; /roster?enrolled=<anyone> printed "ENROLLED — Enrolled X"
	// with no login; /vehicle/amend?created= printed "Recorded."; and every
	// ?recorded= page answered a garbled or invented key with "Recorded, but
	// the outcome could not be determined" — a sentence that says something
	// WAS recorded. A seal that does not verify now renders no notice at all;
	// rendering 4 is left for what it means: a redirect this process wrote
	// whose read-back failed.
	recordedQueryKey,
	retractedQueryKey,
	enrolledQueryKey,
	createdQueryKey,
	heldOutQueryKey,
	heldOutEnteredQueryKey,
}

// The post-commit confirmation keys sealed since D23. heldOutQueryKey and
// heldOutEnteredQueryKey live with their reader in orientation.go.
const (
	// recordedQueryKey: every amend / correction / resolve / unref / trade
	// confirmation (an event key or a trade id) and /orientation's count.
	recordedQueryKey = "recorded"
	// retractedQueryKey: /orientation's "Retracted: X no longer holds …".
	retractedQueryKey = "retracted"
	// enrolledQueryKey: /roster's "ENROLLED — Enrolled X".
	enrolledQueryKey = "enrolled"
	// createdQueryKey: /vehicle/amend's "Recorded. The vehicle record is …".
	createdQueryKey = "created"
)

// noticeSecret is generated once per process and never leaves it.
var noticeSecret = func() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("web: notice seal: no randomness: " + err.Error())
	}
	return b
}()

// noticeNow is the seal's clock — a variable only so a test can age a seal.
var noticeNow = time.Now

func noticeMAC(q url.Values, issued int64) string {
	keys := append([]string(nil), noticeQueryKeys...)
	sort.Strings(keys)
	m := hmac.New(sha256.New, noticeSecret)
	m.Write([]byte(strconv.FormatInt(issued, 10)))
	for _, k := range keys {
		for _, v := range q[k] {
			m.Write([]byte{0})
			m.Write([]byte(k))
			m.Write([]byte{0})
			m.Write([]byte(v))
		}
	}
	return hex.EncodeToString(m.Sum(nil))
}

func hasNoticeKey(q url.Values) bool {
	for _, k := range noticeQueryKeys {
		if len(q[k]) > 0 {
			return true
		}
	}
	return false
}

// sealNotices (re)seals a redirect target over every notice key it carries.
// Idempotent and chainable: each *Redirect helper calls it on its own output,
// so the seal on the final URL always covers the whole set. A target with
// no notice key comes back exactly as given.
func sealNotices(target string) string {
	u, err := url.Parse(target)
	if err != nil {
		return target
	}
	q := u.Query()
	q.Del(noticeSealQueryKey)
	if !hasNoticeKey(q) {
		u.RawQuery = q.Encode()
		return u.String()
	}
	issued := noticeNow().Unix()
	// ⟦D23, fix2-fj 2026-10-04⟧ the seal is APPENDED, the target's own
	// parameters keep their order: "/person/amend?recorded=…&ns=…", not a
	// re-sorted "?ns=…&recorded=…". The MAC is over parsed values, so order
	// never mattered to it; it matters to every reader of a Location header
	// that looks for "?recorded=" where the producer put it.
	var kept []string
	for _, part := range strings.Split(u.RawQuery, "&") {
		if part == "" || part == noticeSealQueryKey || strings.HasPrefix(part, noticeSealQueryKey+"=") {
			continue
		}
		kept = append(kept, part)
	}
	kept = append(kept, noticeSealQueryKey+"="+url.QueryEscape(strconv.FormatInt(issued, 10)+"."+noticeMAC(q, issued)))
	u.RawQuery = strings.Join(kept, "&")
	return u.String()
}

// noticeQuery returns the request's notice keys when — and only when — its
// seal verifies. A forged, altered or expired URL yields an empty set, so
// every reader renders nothing for it.
func noticeQuery(r *http.Request) url.Values {
	q := r.URL.Query()
	if !hasNoticeKey(q) {
		return url.Values{}
	}
	issuedText, mac, ok := strings.Cut(q.Get(noticeSealQueryKey), ".")
	if !ok {
		return url.Values{}
	}
	issued, err := strconv.ParseInt(issuedText, 10, 64)
	if err != nil {
		return url.Values{}
	}
	age := noticeNow().Sub(time.Unix(issued, 0))
	if age < -time.Minute || age > noticeSealTTL {
		return url.Values{}
	}
	if !hmac.Equal([]byte(mac), []byte(noticeMAC(q, issued))) {
		return url.Values{}
	}
	out := url.Values{}
	for _, k := range noticeQueryKeys {
		if vs, ok := q[k]; ok {
			out[k] = vs
		}
	}
	return out
}
