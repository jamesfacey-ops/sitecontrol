// w5d_web_seams_test.go — THE ONE place W5d's web tests bind every
// HTTP/rendering spelling the LOCKED criteria leave undetermined
// (design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, W5d LOCKED 2026-08-31; plan
// §5 step 2), plus the shared fixture worlds and the ELEMENT-SCOPED
// assertion helpers W5-X2 requires. No assertions of its own.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD (domain.AmendCompany, CompanyAmendment,
// LoadCompanyMergeMap, Company.SameAs, domain.Competitors) — that IS the
// web suite's clean-tree red, captured via go vet.
//
// SHAPE PRESUMPTIONS (SEAMS), each bound once; disputes go to the gate:
//
//   - SEAM W5D-W-ROUTE: the officer-facing COMPANY amendment surface is
//     GET /company/amend and POST /company/amend, both behind
//     s.requireLogin — the same gating as /person/amend
//     (internal/web/person_amend.go:36–37, the ONLY amendment routes in the
//     tree today [MEASURED]) — per [[D-51]] (2026-08-31,
//     reports/JAMES-DECISIONS.md:2654), which is the company surface's OWN
//     ruling and expressly does NOT cite [[D-46]]. Its PRG confirmation is
//     303 → the same surface with ?recorded=<site_id>.<epoch>.<seq> (VF §5
//     — the parameter name and right-anchored parse are DOC-fixed, the
//     route path is the seam). There is NO separate merge affordance:
//     company same_as is an amendment like any other, authored through this
//     one POST (S6's locked form, "the same POST path, never a parallel
//     author").
//   - SEAM W5D-W-FORM: form fields are named after the PAYLOAD KEYS, which
//     are B-2-frozen literals rather than seams: company_id, name, same_as,
//     and a repeatable `clear`. Tests look inputs up BY NAME, never by id,
//     precisely so the element-id spelling stays the builder's.
//   - SEAM W5D-W-ELEMENT: an outcome/refusal is carried by an element whose
//     class contains "notice" — the house's own rendered form
//     (person_amend.html:26, :30, :32, :34, :41). W5-X2's extended clause
//     forbids strings.Contains over the BODY for adverse renders, so every
//     such assertion here runs against the text of ONE notice element.
//   - SEAM W5D-W-RENDER2: VF §5 rendering 2's AMENDMENT variant — the
//     WINNING post's side, built per [[D-53]] (JAMES-DECISIONS.md:2745;
//     James chose the FIX arm). Its wording is PINNED as the literal stem
//     below and is drawn from OUTSIDE w3AdverseTokens — the disposition
//     warns that a test which "only greps for 'superseded' is m7′-blind",
//     and the adverse register belongs to the LOSING side. L-1 rides here:
//     the "this post" form when winner and loser share a site_id, the
//     peer's config-derived display name otherwise, BOTH produced.
//   - SEAM W5D-W-VALUE: a refused POST re-renders the company amendment
//     form with the officer's typing PRESERVED as value="…" on the inputs
//     (W5-X2, ruled at the lock: W5d DOES build refusal-preservation on the
//     company amendment form; W5d does NOT retrofit the person one, which
//     carries zero value= attributes today [MEASURED]).
//
// NOT seams (DOC-pinned, cited): the adverse register and the terse success
// register (VF §5, bound once at w3_web_seams_test.go and REUSED here — the
// rendering vocabulary is chunk-global, the fixtures are per-clone);
// "site-b" > "site-a" (VF §1/§10).
package web

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

const (
	w5dAmendPath      = "/company/amend" // SEAM W5D-W-ROUTE
	w5dFieldCompanyID = "company_id"     // SEAM W5D-W-FORM (B-2-frozen payload keys)
	w5dFieldName      = "name"
	w5dFieldSameAs    = "same_as"
	w5dFieldClear     = "clear"

	// SEAM W5D-W-RENDER2 — the pinned literal stem. Checked against
	// w3AdverseTokens by TestW5dRendering2WordingIsOutsideTheAdverseRegister
	// so the choice is verified, not asserted.
	w5dRendering2Stem     = "now stands: it takes the place of an earlier amendment made at"
	w5dRendering2SamePost = "this post" // L-1's same-post form
)

// --- element-scoped rendering assertions (W5-X2) ------------------------

var (
	// ⚠ Closes on </div> SPECIFICALLY. A generic `</[a-z]+>` is non-greedy
	// and stops at the first inner close tag — the house's notices carry
	// <strong>…</strong>, so a generic pattern silently truncates the
	// element text and an assertion on the second half of the sentence
	// fails against a CORRECT build. Measured while writing these tests.
	w5dNoticeRe = regexp.MustCompile(`(?is)<div[^>]*class="[^"]*notice[^"]*"[^>]*>(.*?)</div>`)
	w5dTagRe    = regexp.MustCompile(`(?s)<[^>]*>`)
	w5dInputRe  = regexp.MustCompile(`(?is)<input\b[^>]*>`)
	w5dAttrRe   = regexp.MustCompile(`(?is)\b([a-z_-]+)="([^"]*)"`)
	w5dSpaceRe  = regexp.MustCompile(`\s+`)
)

// w5dNoticeTexts returns the plain text of every rendered notice element on
// the page — THE units an adverse-render assertion may be made against
// (W5-X2's extended form: never strings.Contains over the whole body, which
// is satisfied by any sentence that happens to carry the token, including
// one the fixture itself supplied).
func w5dNoticeTexts(page string) []string {
	stripped := w2StripNonMarkup(page)
	var out []string
	for _, m := range w5dNoticeRe.FindAllStringSubmatch(stripped, -1) {
		txt := html.UnescapeString(w5dTagRe.ReplaceAllString(m[1], " "))
		out = append(out, strings.ToLower(strings.TrimSpace(w5dSpaceRe.ReplaceAllString(txt, " "))))
	}
	return out
}

// w5dNoticeCarrying returns the first notice element whose text contains
// phrase, and whether one exists.
func w5dNoticeCarrying(page, phrase string) (string, bool) {
	for _, n := range w5dNoticeTexts(page) {
		if strings.Contains(n, strings.ToLower(phrase)) {
			return n, true
		}
	}
	return "", false
}

// w5dInputValueByName returns the value= attribute of the input whose name=
// attribute is the given payload key, and whether such an input exists.
// Looked up BY NAME because the payload key is a B-2-frozen literal while
// the element id is the builder's.
func w5dInputValueByName(page, name string) (value string, present bool) {
	for _, tag := range w5dInputRe.FindAllString(w2StripNonMarkup(page), -1) {
		attrs := map[string]string{}
		for _, a := range w5dAttrRe.FindAllStringSubmatch(tag, -1) {
			attrs[strings.ToLower(a[1])] = a[2]
		}
		if attrs["name"] == name {
			return html.UnescapeString(attrs["value"]), true
		}
	}
	return "", false
}

// w5dInputNames lists every input name= on the page — the detector behind
// W5d-2's "no `retired` on any authoring surface".
func w5dInputNames(page string) []string {
	var out []string
	for _, tag := range w5dInputRe.FindAllString(w2StripNonMarkup(page), -1) {
		for _, a := range w5dAttrRe.FindAllStringSubmatch(tag, -1) {
			if strings.EqualFold(a[1], "name") {
				out = append(out, a[2])
			}
		}
	}
	return out
}

// --- fixture worlds -----------------------------------------------------

// w5dSiteAServer: the site-a post with two companies authored locally.
func w5dSiteAServer(t *testing.T) (*Server, *store.Store, domain.Company, domain.Company) {
	t.Helper()
	st := w3OpenRegistered(t, "w5d-site-a.db")
	x, err := domain.CreateCompany(st, "site-a", 1, "Amend Target Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	y, err := domain.CreateCompany(st, "site-a", 1, "Merge Peer Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return New(w3ConfigSiteA(st.Path), st), st, x, y
}

// w5dSiteBServer: the site-b post holding the SAME roster, replicated in.
func w5dSiteBServer(t *testing.T, roster []store.Envelope) (*Server, *store.Store) {
	t.Helper()
	st := w3OpenRegistered(t, "w5d-site-b.db")
	w3ApplyWire(t, st, roster)
	return New(w3ConfigSiteB(st.Path), st), st
}

// w5dRemoteAmend authors ONE company amendment as siteID on a scratch post
// primed with the given history, at the given clock, and returns its
// envelope — the wire-shaped output of a real authoring edge at the other
// post.
func w5dRemoteAmend(t *testing.T, primer []store.Envelope, siteID string, clockMS int64, in domain.CompanyAmendment) store.Envelope {
	t.Helper()
	scratch := w3OpenRegistered(t, "w5d-scratch-"+siteID+".db")
	w3ApplyWire(t, scratch, primer)
	scratch.Clock = w2Reading(clockMS, store.ClockSynced)
	env, err := domain.AmendCompany(scratch, siteID, 1, in, 0, "default")
	if err != nil {
		t.Fatalf("remote company fixture amendment refused: %v", err)
	}
	return env
}

// w5dPostAmend posts to the COMPANY amendment surface with a kept session.
func w5dPostAmend(t *testing.T, srv *Server, form url.Values) (*httptest.ResponseRecorder, *http.Cookie) {
	t.Helper()
	return w2PostForm(t, srv, w5dAmendPath, form)
}

// w5dAnonRequest issues an UNAUTHENTICATED request to handler — the shared
// judgement behind W5d-23(a)'s control and its real assertion.
func w5dAnonRequest(t *testing.T, h http.Handler, method, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if method == http.MethodPost {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// w5dReachedHandler is W5d-23(a)'s judgement function: did an
// unauthenticated request REACH the handler? Asserted on BODY CONTENT (the
// login form is served in the handler's place), never on the status code —
// the house's own carry-forward, because the catch-all answers 200 to
// everything.
func w5dReachedHandler(rec *httptest.ResponseRecorder) bool {
	return !strings.Contains(rec.Body.String(), `action="/login"`)
}

// w5dConfigSiteAAlt is a THIRD config for L-1's same-post arm: site-a with
// NO peer at all, so "this post" is the only derivable precedence-taker.
func w5dConfigSiteAAlt(dbPath string) *config.Config {
	return &config.Config{
		SiteID: "site-a", DisplayName: "Ice Road", ListenAddr: ":0",
		NodeEpoch: 1, DBPath: dbPath, SyncIntervalSeconds: 3600,
		LoginPassword:  testLoginPassword,
		EntranceSiteID: "site-a",
		Destinations: []config.Destination{
			{ID: "conneco", DisplayName: "Conneco Pad", Controlled: false},
		},
	}
}
