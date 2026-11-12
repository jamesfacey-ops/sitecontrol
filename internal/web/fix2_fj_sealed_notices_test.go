// fix2_fj_sealed_notices_test.go — fix round 2, stream FJ (cluster J,
// "sealed notices"), 2026-10-04. FINDINGS-R4 D22, D23, the L01 forged-key
// wording LOW, and L10 M-21.
//
// Every test here fails on fork/next 2ba9188 and passes after the fix:
//   - D22: /person, /company and /vehicle amend (and /trips/amend, the same
//     shape) confirmed another record kind's ?recorded= key as their own.
//   - D23: /orientation ?recorded/retracted/held/entered, /roster?enrolled=
//     (anonymously) and /vehicle/amend?created= rendered whatever a
//     hand-typed URL said; a forged ?recorded= on any confirmation page said
//     "Recorded, but the outcome could not be determined".
//   - M-21: /muster/print?visits=, said "1 visit id(s) not found:" with
//     nothing after the colon.
//   - The sweep: every notice key, forged onto every GET page, renders no
//     notice; and the source holds no unsealed reader or producer.
package web

import (
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// fjNoticeTexts are the visible sentences a URL-driven confirmation renders.
// Lower-case: compared against w3PageText.
var fjNoticeTexts = []string{
	"recorded.",
	"recorded and effective",
	"now stands",
	"orientation completion(s)",
	"now on site",
	"still not on site",
	"retracted:",
	"enrolled —",
	"the vehicle record is",
	"could not be determined",
	"nothing to confirm",
	"superseded",
}

var (
	fjTagRe   = regexp.MustCompile(`<[^>]*>`)
	fjSpaceRe = regexp.MustCompile(`\s+`)
)

// fjText is the page's visible text: lower-case, tags dropped, whitespace
// collapsed, entities unescaped — "<strong>ENROLLED</strong> — Enrolled X"
// reads "enrolled — enrolled x".
func fjText(page string) string {
	t := fjTagRe.ReplaceAllString(w3PageText(page), " ")
	return fjSpaceRe.ReplaceAllString(html.UnescapeString(t), " ")
}

// fjNoNotice: none of the sentences appears on page that the bare page
// (the same route with no query) does not already carry as standing copy.
func fjNoNotice(t *testing.T, page, bare, what string) {
	t.Helper()
	text, standing := fjText(page), fjText(bare)
	for _, n := range fjNoticeTexts {
		if strings.Contains(text, n) && !strings.Contains(standing, n) {
			t.Errorf("%s: a confirmation rendered off a URL no commit of this post sealed — %q found", what, n)
		}
	}
}

// fjWorld is the orientation world plus one company and one vehicle, and a
// real amendment of each kind authored through the real POST doors.
type fjWorld struct {
	*orientBlockWorld
	companyID, vehicleID string
	// sealed redirect targets the real POSTs wrote
	personLoc, companyLoc, vehicleLoc string
}

func newFJWorld(t *testing.T) *fjWorld {
	t.Helper()
	w := &fjWorld{orientBlockWorld: newOrientBlockWorld(t, true)}
	c, err := domain.CreateCompany(w.st, "site-b", 1, "Kestrel Survey", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.companyID = c.CompanyID
	num := "TRK-7"
	v, err := domain.CreateVehicle(w.st, "site-b", 1, &num, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w.vehicleID = v.VehicleID
	post := func(path string, form url.Values) string {
		t.Helper()
		rec := postForm(t, w.srv, path, form)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("POST %s = %d: %s", path, rec.Code, rec.Body.String())
		}
		return rec.Header().Get("Location")
	}
	w.personLoc = post("/person/amend", url.Values{"person_id": {w.uma.PersonID}, "phone": {"9075550100"}})
	w.companyLoc = post("/company/amend", url.Values{"company_id": {w.companyID}, "name": {"Kestrel Survey Group"}})
	w.vehicleLoc = post("/vehicle/amend", url.Values{"vehicle_id": {w.vehicleID}, "role": {"Bus"}})
	return w
}

// fjQueryOf returns the query part of a redirect target.
func fjQueryOf(t *testing.T, loc string) string {
	t.Helper()
	_, q, ok := strings.Cut(loc, "?")
	if !ok || !strings.Contains(q, noticeSealQueryKey+"=") {
		t.Fatalf("redirect %q carries no seal", loc)
	}
	return q
}

// TestFJD22AmendPagesConfirmOnlyTheirOwnKind: L06 R4-SEALED-XKIND — a
// company rename's key opened on /person/amend read "Recorded. Effective:
// name.". Each page now answers a real key of another kind with NOTHING TO
// CONFIRM, while its own key still confirms. The query moved across is the
// SEALED one, so this is the kind check, not the seal, doing the work.
func TestFJD22AmendPagesConfirmOnlyTheirOwnKind(t *testing.T) {
	w := newFJWorld(t)
	own := map[string]string{"/person/amend": w.personLoc, "/company/amend": w.companyLoc, "/vehicle/amend": w.vehicleLoc}
	for page, loc := range own {
		text := fjText(w3Get(t, w.srv, loc))
		if !strings.Contains(text, "recorded") || strings.Contains(text, "nothing to confirm") {
			t.Errorf("%s: its own sealed key no longer confirms:\n%s", page, text)
		}
		for other, oloc := range own {
			if other == page {
				continue
			}
			cross := page + "?" + fjQueryOf(t, oloc)
			got := fjText(w3Get(t, w.srv, cross))
			if !strings.Contains(got, "nothing to confirm") {
				t.Errorf("D22: %s (a %s key) does not say NOTHING TO CONFIRM", cross, other)
			}
			for _, bad := range []string{"recorded.", "now stands", "superseded", "could not be determined"} {
				if strings.Contains(got, bad) {
					t.Errorf("D22: %s (a %s key) still renders %q as if this page's record changed", cross, other, bad)
				}
			}
		}
	}
	// /trips/amend has the same shape: a person amendment is not a trip one.
	cross := "/trips/amend?" + fjQueryOf(t, w.personLoc)
	if got := fjText(w3Get(t, w.srv, cross)); !strings.Contains(got, "nothing to confirm") || strings.Contains(got, "recorded.") {
		t.Errorf("D22's class: %s does not refuse a person amendment's key", cross)
	}
}

// TestFJD23ForgedConfirmationsRenderNothing: the unsealed readers. Each of
// these printed a confirmation for an event that never happened; anonymous
// /roster?enrolled= needed no login at all.
func TestFJD23ForgedConfirmationsRenderNothing(t *testing.T) {
	w := newFJWorld(t)
	_, personKey, _ := strings.Cut(w.personLoc, "recorded=")
	personKey, _, _ = strings.Cut(personKey, "&")
	uma := url.QueryEscape(w.uma.PersonID)
	forged := []string{
		"/orientation?recorded=42",
		"/orientation?retracted=Fake+Person",
		"/orientation?recorded=1&entered=" + uma,
		"/orientation?recorded=1&held=" + uma,
		"/roster?enrolled=" + uma,
		"/vehicle/amend?created=" + url.QueryEscape(w.vehicleID),
		// the L01 forged-key wording: garbled, no event, and a REAL key
		// typed by hand — none of them was sealed by a commit here
		"/person/amend?recorded=zz.9.9",
		"/person/amend?recorded=junk",
		"/person/amend?recorded=" + personKey,
		"/company/amend?recorded=site-b.1.999999",
		"/vehicle/amend?recorded=zz.9.9",
		"/trips/amend?recorded=zz.9.9",
		"/time/correct?recorded=zz.9.9",
		"/trade?recorded=zz.9.9",
		resolvePath + "?recorded=zz.9.9",
		unrefPath + "?recorded=zz.9.9",
		// a seal that is not one
		"/orientation?recorded=42&ns=1.deadbeef",
	}
	for _, path := range forged {
		route, _, _ := strings.Cut(path, "?")
		fjNoNotice(t, w3Get(t, w.srv, path), w3Get(t, w.srv, route), "logged-in "+path)
		fjNoNotice(t, w3AnonGet(t, w.srv, path), w3AnonGet(t, w.srv, route), "anonymous "+path)
	}
}

// TestFJD23RealRedirectsStillConfirm: the producers seal, so the officer who
// just acted still sees every confirmation — and a sealed value swapped by
// hand renders nothing.
func TestFJD23RealRedirectsStillConfirm(t *testing.T) {
	w := newFJWorld(t)
	rec := postForm(t, w.srv, "/orientation/complete", url.Values{"person_id": {w.uma.PersonID}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("orientation complete = %d: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	fjQueryOf(t, loc)
	if got := fjText(w3Get(t, w.srv, loc)); !strings.Contains(got, "recorded 1 orientation completion(s)") {
		t.Errorf("the sealed orientation redirect %q no longer confirms:\n%s", loc, got)
	}
	tampered := strings.Replace(loc, "recorded=1", "recorded=42", 1)
	fjNoNotice(t, w3Get(t, w.srv, tampered), w3Get(t, w.srv, "/orientation"), "tampered "+tampered)

	rec = postForm(t, w.srv, "/orientation/retract", url.Values{"person_id": {w.uma.PersonID}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("orientation retract = %d: %s", rec.Code, rec.Body.String())
	}
	if got := fjText(w3Get(t, w.srv, rec.Header().Get("Location"))); !strings.Contains(got, "retracted: uma unoriented") {
		t.Errorf("the sealed retraction redirect no longer confirms:\n%s", got)
	}

	rec = postForm(t, w.srv, "/vehicle/create", url.Values{"number": {"BUS-3"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("vehicle create = %d: %s", rec.Code, rec.Body.String())
	}
	if got := fjText(w3Get(t, w.srv, rec.Header().Get("Location"))); !strings.Contains(got, "the vehicle record is") {
		t.Errorf("the sealed vehicle-create redirect no longer confirms:\n%s", got)
	}

	srv, _, _ := newGateTestServer(t)
	companyID := companyIDNamed(t, srv, "Kakivik Asset Management")
	rec = postForm(t, srv, "/roster/intake", intakeForm2("Ferguson", "Mary", companyID, "", "", ""))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("intake = %d: %s", rec.Code, rec.Body.String())
	}
	if got := fjText(w3Get(t, srv, rec.Header().Get("Location"))); !strings.Contains(got, "enrolled — enrolled ferguson") {
		t.Errorf("the sealed enrolment redirect no longer confirms:\n%s", got)
	}
}

// TestFJM21AnEmptyVisitIDIsRefusedInWords: L10 M-21.
func TestFJM21AnEmptyVisitIDIsRefusedInWords(t *testing.T) {
	w := newFJWorld(t)
	for _, path := range []string{"/muster/print?visits=,", "/muster/print?visits=x,,y"} {
		rec := w2Get(t, w.srv, path, loginCookie(t, w.srv, testLoginPassword))
		if rec.Code == http.StatusOK {
			t.Fatalf("%s printed a sheet", path)
		}
		body := rec.Body.String()
		if strings.Contains(body, "not found:") || !strings.Contains(body, "empty visit id") {
			t.Errorf("M-21: %s is not refused in plain words:\n%s", path, body)
		}
	}
}

// fjGetRoutes reads every "GET <path>" registration out of this package's
// own source, so a page added later is swept without anyone editing a list.
func fjGetRoutes(t *testing.T) []string {
	t.Helper()
	files, _ := filepath.Glob("*.go")
	consts := map[string]string{}
	constRe := regexp.MustCompile(`(?m)^const (\w+) = "(/[^"]*)"`)
	litRe := regexp.MustCompile(`HandleFunc\("GET (/[^"]*)"`)
	refRe := regexp.MustCompile(`HandleFunc\("GET "\+(\w+)`)
	var src []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src = append(src, string(b))
		for _, m := range constRe.FindAllStringSubmatch(string(b), -1) {
			consts[m[1]] = m[2]
		}
	}
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = strings.ReplaceAll(p, "{$}", "")
		p = strings.ReplaceAll(p, "{site}", "site-a")
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, s := range src {
		for _, m := range litRe.FindAllStringSubmatch(s, -1) {
			add(m[1])
		}
		for _, m := range refRe.FindAllStringSubmatch(s, -1) {
			p, ok := consts[m[1]]
			if !ok {
				t.Fatalf("GET route on constant %s: its value was not found", m[1])
			}
			add(p)
		}
	}
	if len(out) < 15 {
		t.Fatalf("route scan found only %d GET routes — the scan is broken: %v", len(out), out)
	}
	return out
}

// TestFJSweepEveryNoticeKeyOnEveryPageIsSealed: every key in
// noticeQueryKeys, forged with a plausible value onto every GET page, both
// logged in and anonymous, renders exactly as many notices as the bare page
// and none of the confirmation sentences. A reader that takes a notice key
// from anywhere but noticeQuery fails here.
func TestFJSweepEveryNoticeKeyOnEveryPageIsSealed(t *testing.T) {
	w := newFJWorld(t)
	_, personKey, _ := strings.Cut(w.personLoc, "recorded=")
	personKey, _, _ = strings.Cut(personKey, "&")
	values := map[string]string{
		"recorded":             personKey,
		"retracted":            "Fake Person",
		"enrolled":             w.uma.PersonID,
		"created":              w.vehicleID,
		heldOutQueryKey:        w.uma.PersonID,
		heldOutEnteredQueryKey: w.uma.PersonID,
		arrivedVehicleQueryKey: "FAKE-99",
		arrivedCheckedQueryKey: "42",
		editSelectQueryKey:     "none",
		timeWarnQueryKey:       "age",
	}
	// The keys are spelled here as well as read from noticeQueryKeys, so a
	// key dropped from the sealed set is still forged — and still caught.
	keys := append([]string{"recorded", "retracted", "enrolled", "created", "held", "entered"}, noticeQueryKeys...)
	q := url.Values{}
	for _, k := range keys {
		v, ok := values[k]
		if !ok {
			v = w.uma.PersonID // the person-id-shaped keys: not_entered, dup, stale tick
		}
		q.Set(k, v)
	}
	// a second recorded value: the numeric shape /orientation reads
	forgedWithCount := url.Values{}
	for k, v := range q {
		forgedWithCount[k] = v
	}
	forgedWithCount.Set("recorded", "42")
	countNotices := func(page string) int { return strings.Count(page, `class="notice`) }
	for _, route := range fjGetRoutes(t) {
		if route == "/theme" || route == "/muster/print" {
			continue // a redirect; a print sheet (its ?visits= is a selector, not a notice — M-21 above)
		}
		for _, forged := range []url.Values{q, forgedWithCount} {
			path := route + "?" + forged.Encode()
			for _, anon := range []bool{false, true} {
				get := w3Get
				who := "logged-in"
				if anon {
					get, who = w3AnonGet, "anonymous"
				}
				bare, page := get(t, w.srv, route), get(t, w.srv, path)
				if countNotices(page) != countNotices(bare) {
					t.Errorf("%s %s: %d notice(s), the bare page has %d — an unsealed notice reader", who, path, countNotices(page), countNotices(bare))
				}
				fjNoNotice(t, page, bare, who+" "+path)
				for _, n := range w3NoticeTexts {
					if strings.Contains(page, n) && !strings.Contains(bare, n) {
						t.Errorf("%s %s: gate notice %q rendered off a forged URL", who, path, n)
					}
				}
			}
		}
	}
}

// TestFJSweepTheSourceHasNoUnsealedReaderOrProducer: the static half of the
// sweep. (1) A handler may read the raw query only for the listed
// non-notice keys — anything else must come through noticeQuery. (2) Every
// redirect whose target names a notice key goes through sealNotices or one
// of the sealing *Redirect helpers.
func TestFJSweepTheSourceHasNoUnsealedReaderOrProducer(t *testing.T) {
	// James, 2026-10-04 (FINDINGS-R4 cluster J): every URL-driven notice is
	// sealed. These are the raw-query reads that are NOT notices, each with
	// why: q (orientation search box), live (pause), next (login return),
	// mode / js (theme toggle), vehicle_id / person_id / fieldTripID (plain
	// prefills, re-checked by the POST), visits (the reprint's selector —
	// a refused or unknown id is refused loudly, never confirmed), group
	// (FIX16: which result table /search.csv writes — an unknown one is
	// refused 400, never confirmed).
	allowedRaw := map[string]bool{`"q"`: true, `"live"`: true, `"next"`: true, `"mode"`: true, `"js"`: true,
		`"vehicle_id"`: true, `"person_id"`: true, `fieldTripID`: true, `"visits"`: true, `"group"`: true}
	rawRe := regexp.MustCompile(`r\.URL\.Query\(\)(?:\.Get\(([^)]*)\)|\[([^\]]*)\])`)
	redirectRe := regexp.MustCompile(`http\.Redirect\(w, r, ([^\n]*)`)
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "noticeseal.go" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		for _, m := range rawRe.FindAllStringSubmatch(src, -1) {
			key := m[1] + m[2]
			if !allowedRaw[key] {
				t.Errorf("%s: raw query read of %s — a notice key must be read through noticeQuery (noticeseal.go); a non-notice key goes on this test's allow-list with its reason", f, key)
			}
		}
		for _, m := range redirectRe.FindAllStringSubmatch(src, -1) {
			target := m[1]
			for _, k := range append([]string{"recorded", "retracted", "enrolled", "created", "held", "entered"}, noticeQueryKeys...) {
				if !strings.Contains(target, k+"=") {
					continue
				}
				if !strings.Contains(target, "sealNotices(") {
					t.Errorf("%s: redirect carries notice key %q without sealNotices: %s", f, k, target)
				}
			}
		}
	}
}
