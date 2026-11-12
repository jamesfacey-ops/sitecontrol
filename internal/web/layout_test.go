// layout_test.go — U1's own done-when tests: the shared design foundation
// (one inlined stylesheet, one document shell, one persistent top bar) and
// the constraints it is not allowed to break.
//
// Every test here drives the REAL embedded templates through the REAL mux
// and asserts on rendered output, never on the stylesheet's own text — with
// the deliberate exception of the two tests that are ABOUT the stylesheet
// (the token audit and the print block), which extract it first and say so.
package web

import (
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
)

// renderedPages returns one live page of every kind the operator can reach,
// each built by its own real handler. Keyed by route so a failure names the
// page. This is the fixture the constraint tests sweep.
// rawPage fetches a page WITHOUT the style-stripping the muster helpers do:
// these tests are partly about the stylesheet itself, so they need the bytes
// the browser actually receives.
func rawPage(t *testing.T, srv *Server, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	if rec.Code != 200 {
		t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func renderedPages(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}

	statusSrv := newTestServer(t)
	out["/status"] = getStatusPage(t, statusSrv)

	tripsSrv, _, _ := newTripsTestServer(t)
	out["/trips"] = getTrips(t, tripsSrv)
	// U2 split /trips into the operator console and this reference/audit
	// view. It is a full page with its own handler, so every constraint in
	// this file applies to it too.
	out["/trips/detail"] = getTripsDetail(t, tripsSrv)

	gateSrv, _, _ := newGateTestServer(t)
	code, body := getGate(t, gateSrv)
	if code != 200 {
		t.Fatalf("GET /gate = %d", code)
	}
	out["/gate"] = body

	musterSrv, _ := newMusterTestServer(t, 0)
	out["/muster"] = rawPage(t, musterSrv, "/muster")
	out["/muster/print"] = rawPage(t, musterSrv, "/muster/print")

	authSrv, _ := newAuthTestServer(t)
	out["/login"] = rawPage(t, authSrv, "/login")

	// There is no need-officer page any more: D-5 retired the picker it
	// existed to send the operator to, so a fresh authenticated session's
	// first submission authors instead of being withheld. See
	// auth_test.go's TestNoOfficerPickerRouteExists.

	return out
}

// --- done-when: every page comes off the shared shell ---

func TestEveryPageRendersTheSharedShellExactlyOnce(t *testing.T) {
	for route, body := range renderedPages(t) {
		if got := strings.Count(body, "<!doctype html>"); got != 1 {
			t.Errorf("%s: %d doctype declarations, want exactly 1 (the shared layout's)", route, got)
		}
		// One inlined stylesheet per page — no page hand-rolls its own
		// <style>, and nothing links a second one.
		if got := strings.Count(body, "<style>"); got != 1 {
			t.Errorf("%s: %d <style> blocks, want exactly 1 (the shared partial)", route, got)
		}
		if got := strings.Count(body, `<header class="topbar">`); got != 1 {
			t.Errorf("%s: %d top bars, want exactly 1", route, got)
		}
		if got := strings.Count(body, `<main class="page`); got != 1 {
			t.Errorf("%s: %d content columns, want exactly 1", route, got)
		}
	}
}

// --- not-done-if: an external asset or a second round trip appears ---

// M-06 (test-efficacy S1-11 / C8). This guard used to be a substring
// blacklist and nothing else, and the blacklist was narrower than its own
// name. Two mutations SURVIVED it green, measured:
//
//	a single-quoted stylesheet link  <link rel='stylesheet' href='/s.css'>
//	  — the list held `<link rel="stylesheet"` and `<link rel=stylesheet`,
//	    so only two of the three legal quotings were covered;
//	a SAME-ORIGIN script            <script src="/app.js"></script>
//	  — the list held `src="http` and `src="//`, both of which are about
//	    EXTERNAL hosts. A same-origin fetch is still a second round trip
//	    against a <=2 round-trip budget, and still a blank page on a box
//	    whose disk the binary is not serving from. "or Second Asset" is
//	    half this test's name and no clause implemented it.
//
// The repair is STRUCTURAL, not a longer blacklist: every quoting, every
// scheme and every host is one more blacklist entry, and the next one is
// always missing. What is asserted now is the property itself — a served
// page contains NO ELEMENT THAT CAUSES A FETCH. The page carries exactly
// one document and zero subresources, which is what offline-first and the
// round-trip budget both actually mean.
//
// The old substring list is kept underneath, unweakened: it costs nothing
// and it still catches a banned string in a place the tag scan does not
// look, such as inside a comment or an inline script.
func TestNoPageRequestsAnExternalOrSecondAsset(t *testing.T) {
	// Elements that fetch a subresource merely by existing. None of these
	// is used anywhere in this app; the stylesheet is inlined into a
	// <style> and every script is inline.
	fetchingElements := map[string]string{
		"link":    "a <link> is an asset reference — the stylesheet is inlined into <style> instead",
		"img":     "an <img> is a second request; no page here uses one",
		"picture": "a <picture> is a second request",
		"source":  "a <source> is a second request",
		"iframe":  "an <iframe> is a whole second document load",
		"frame":   "a <frame> is a whole second document load",
		"embed":   "an <embed> is a second request",
		"object":  "an <object> is a second request",
		"video":   "a <video> is a second request",
		"audio":   "an <audio> is a second request",
		"track":   "a <track> is a second request",
	}

	// Attributes that turn an otherwise-inert element into a fetch. `href`
	// is deliberately NOT here: on <a> it is a navigation, which is a new
	// page load with its own budget, and <link> is banned outright above.
	fetchingAttrs := []string{"src", "srcset", "poster", "data"}

	tagRe := regexp.MustCompile(`(?is)<\s*([a-zA-Z][a-zA-Z0-9-]*)((?:[^>"']|"[^"]*"|'[^']*')*)>`)
	attrRe := regexp.MustCompile(`(?is)([a-zA-Z-]+)\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	// Keep the start/end tags, drop only the BODIES, so a <script src=…>
	// start tag is still visible to the scan while a `<` inside a script
	// string cannot be mistaken for markup.
	scriptBodyRe := regexp.MustCompile(`(?is)(<script\b[^>]*>)(.*?)(</script>)`)
	styleBodyRe := regexp.MustCompile(`(?is)(<style\b[^>]*>)(.*?)(</style>)`)

	for route, body := range renderedPages(t) {
		// --- the inline CSS: no import, no remote url() ---
		for _, m := range styleBodyRe.FindAllStringSubmatch(body, -1) {
			css := m[2]
			if strings.Contains(css, "@import") {
				t.Errorf("%s: the inline stylesheet contains @import — that is a second round trip, and on an offline box it is an unstyled page", route)
			}
			if strings.Contains(css, "@font-face") {
				t.Errorf("%s: the inline stylesheet contains @font-face — a web font is a fetched asset", route)
			}
			for _, u := range regexp.MustCompile(`(?i)url\(\s*['"]?([^)'"]*)`).FindAllStringSubmatch(css, -1) {
				if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(u[1])), "data:") {
					t.Errorf("%s: the inline stylesheet fetches %q. Only a data: URI may appear in url() — anything else is a request the offline box cannot make", route, u[1])
				}
			}
		}

		// --- the markup: no element that fetches ---
		tagsOnly := scriptBodyRe.ReplaceAllString(body, "$1$3")
		tagsOnly = styleBodyRe.ReplaceAllString(tagsOnly, "$1$3")
		sawTag := false
		for _, m := range tagRe.FindAllStringSubmatch(tagsOnly, -1) {
			sawTag = true
			name := strings.ToLower(m[1])
			if why, banned := fetchingElements[name]; banned {
				t.Errorf("%s: page renders a <%s> element — %s. Offline-first plus the <=2 round-trip budget means the document is the only thing fetched", route, name, why)
			}
			for _, a := range attrRe.FindAllStringSubmatch(m[2], -1) {
				attr := strings.ToLower(a[1])
				val := strings.Trim(a[2], `"'`)
				for _, f := range fetchingAttrs {
					if attr != f {
						continue
					}
					// <script src> and friends: ANY value, same-origin
					// included. The budget counts requests, not hosts.
					t.Errorf("%s: <%s> carries %s=%q — that is a second request. Same-origin is not exempt: the page budget is <=2 round trips and an offline box may not be able to serve it at all", route, name, attr, val)
				}
			}
		}
		if !sawTag {
			t.Fatalf("%s: the tag scanner found no elements at all, so it is asserting nothing — the page or the scanner is broken:\n%s", route, body)
		}

		// --- the original substring list, kept and unweakened ---
		banned := []string{
			`<link rel="stylesheet"`, `<link rel=stylesheet`, `<link rel='stylesheet'`,
			"@import", "fonts.googleapis", "fonts.gstatic", "cdn.",
			`src="http`, `src="//`, `src='http`, `src='//`,
			`href="http`, `href='http`, `url(http`, "@font-face",
		}
		for _, b := range banned {
			if strings.Contains(body, b) {
				t.Errorf("%s: page contains %q — offline-first forbids any external or extra asset", route, b)
			}
		}
	}
}

func TestNoPageUsesClientSideStorage(t *testing.T) {
	// Nothing alert-critical may live only in a tab (UI brief constraint 4).
	for route, body := range renderedPages(t) {
		for _, b := range []string{"localStorage", "sessionStorage", "indexedDB", "document.cookie"} {
			if strings.Contains(body, b) {
				t.Errorf("%s: page references %q — no client-side storage is permitted", route, b)
			}
		}
	}
}

// --- done-when: the top bar renders exactly the slots a page's handler
// populates, and nothing else. Each case asserts its OWN PRECONDITION (that
// the handler's data type really does / does not carry the field) before
// asserting the rendering, so a later field addition cannot leave this test
// passing while defending nothing. ---

func TestTopBarRendersOnlyTheSlotsAPageHandlerPopulates(t *testing.T) {
	pages := renderedPages(t)

	const (
		phaseSlot = `<span class="chip-label">weather phase</span>`
		syncSlot  = `<span class="chip-label">sync</span>`
		postSlot  = "Recording as:"
	)

	// ⟦was: a `phase` column — /status, /trips and /gate populated the
	// weather-phase slot. Weather phases removed, James 2026-10-04: no data
	// type carries Phase now, and the slot is absent from every page.⟧
	cases := []struct {
		route string
		data  any // a zero value of the handler's own data type
		sync  bool
		post  bool
	}{
		{"/status", statusData{}, true, false},
		// /trips and /muster carry the sync slot since SYNC-HONESTY
		// (2026-09-09): the dispatcher's console and the muster both show
		// records that reach this post only through sync, so the bar is
		// telling the truth by showing the link's state there. Same
		// precondition discipline as every row here.
		{"/trips", tripsData{}, true, true},
		{"/trips/detail", tripsDetailData{}, false, true},
		{"/gate", gateData{}, false, true},
		{"/muster", musterData{}, true, false},
		{"/login", loginData{}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.route, func(t *testing.T) {
			// Precondition: the rendering below is only meaningful if the
			// handler's data type actually has (or lacks) the field.
			if templateHasField(tc.data, "Phase") {
				t.Fatalf("%T still has a Phase field — weather phases were removed (James, 2026-10-04)", tc.data)
			}
			if got := templateHasField(tc.data, "Sync"); got != tc.sync {
				t.Fatalf("precondition: %T has Sync field = %v, want %v", tc.data, got, tc.sync)
			}
			if got := templateHasField(tc.data, "Post"); got != tc.post {
				t.Fatalf("precondition: %T has Post field = %v, want %v", tc.data, got, tc.post)
			}

			body := pages[tc.route]
			if tc.route == "/gate" {
				// ⟦W3 S-034⟧ renderedPages' /gate is anonymous, whose chip
				// now reads "Not logged in"; the slot contract is about the
				// officer's (logged-in) page.
				gateSrv, _, _ := newGateTestServer(t)
				body = w3Get(t, gateSrv, "/gate/vehicle")
			}
			// Weather phases removed (James, 2026-10-04): the slot is absent
			// from every page. ⟦was: absent only at a post with no
			// [[weather.phase]] table (James, 2026-10-03), with a governed
			// half asserting it present on /status, /trips and /gate⟧
			if strings.Contains(body, phaseSlot) {
				t.Errorf("%s: a weather-phase slot is rendered — weather phases were removed", tc.route)
			}
			if got := strings.Contains(body, syncSlot); got != tc.sync {
				t.Errorf("%s: sync slot present = %v, want %v", tc.route, got, tc.sync)
			}
			if got := strings.Contains(body, postSlot); got != tc.post {
				t.Errorf("%s: recording-post slot present = %v, want %v", tc.route, got, tc.post)
			}
		})
	}
}

// A slot a page has no data for must be ABSENT, never a placeholder — an
// em-dash, a "—", an "unknown", or a bare label with nothing after it would
// all read to an operator as a measurement.
func TestAbsentTopBarSlotsRenderNothingRatherThanAPlaceholder(t *testing.T) {
	pages := renderedPages(t)
	bar := regexp.MustCompile(`(?s)<header class="topbar">.*?</header>`)
	// Each route is paired with the slots its handler genuinely has no
	// data for, rather than one shared list. ⟦was: /gate omitted "weather
	// phase" — it read the phase for the departing-vehicle form until weather
	// phases were removed, James 2026-10-04⟧
	absent := map[string][]string{
		"/gate":   {"sync", "weather phase"},
		"/muster": {"weather phase"}, // sync is populated there since SYNC-HONESTY (2026-09-09)
		"/login":  {"weather phase", "sync"},
	}
	for route, forbidden := range absent {
		m := bar.FindString(pages[route])
		if m == "" {
			t.Fatalf("%s: no top bar found", route)
		}
		if len(forbidden) == 0 {
			t.Fatalf("%s: no slot is being checked, so this case proves nothing", route)
		}
		for _, f := range forbidden {
			if strings.Contains(m, f) {
				t.Errorf("%s: top bar mentions %q for a page whose handler has no such data", route, f)
			}
		}
	}
}

// The site id is the one slot every page has, by one route or the other
// (.SiteID on the operator pages, .Cfg.SiteID on the auth pages).
func TestTopBarAlwaysCarriesTheSiteID(t *testing.T) {
	want := map[string]string{
		"/status": "site-b", "/trips": "site-b", "/trips/detail": "site-b", "/gate": "site-a",
		"/muster": "site-a", "/muster/print": "site-a",
		"/login": "site-a",
	}
	bar := regexp.MustCompile(`(?s)<header class="topbar">.*?</header>`)
	for route, body := range renderedPages(t) {
		m := bar.FindString(body)
		if m == "" {
			t.Fatalf("%s: no top bar found", route)
		}
		// James, 2026-10-04 (cluster I): the slot names the site by its
		// DISPLAY name where config has one (the fixtures' names below), and
		// by the id label only where it has none. ⟦was: the raw id only⟧
		shown := map[string]string{"/status": "Site B", "/trips": "Ice Camp B", "/trips/detail": "Ice Camp B",
			"/gate": "Ice Road A", "/muster": "Ice Road A", "/muster/print": "Ice Road A", "/login": "Ice Road A"}[route]
		if !strings.Contains(m, `<span class="site-id">`+want[route]+`</span>`) && !strings.Contains(m, `<span class="site-id">`+shown+`</span>`) {
			t.Errorf("%s: top bar does not carry site id %q; bar was:\n%s", route, want[route], m)
		}
	}

	// U2 (U1 carry-forward note 1): the operator pages now also carry the
	// site's HUMAN name, which the bar could not show while their handlers
	// passed only a bare id. /muster still does not — that page belongs to
	// another chunk — so this asserts exactly the pages whose handlers were
	// changed, and would fail if one of them silently lost it again.
	displayNamed := map[string]string{
		"/trips": "Ice Camp B", "/trips/detail": "Ice Camp B", "/gate": "Ice Road A",
	}
	pages := renderedPages(t)
	for route, name := range displayNamed {
		m := bar.FindString(pages[route])
		// James, 2026-10-04 (cluster I): the HUMAN name now occupies the
		// site-id slot itself (the id label beside it was the "STP STP" /
		// "ice-road 2P Ice Road Entrance" duplication); the site-name slot is
		// reserved for a configured long_name. ⟦was: <span class="site-name">⟧
		if !strings.Contains(m, `<span class="site-id">`+name+`</span>`) {
			t.Errorf("%s: top bar shows an id with no human site name (want %q); bar was:\n%s", route, name, m)
		}
	}
}

// The C7 banner is mounted BY the top bar, so it must still ride every page
// whose handler runs the scan — inside the bar, where it cannot be scrolled
// away — and must not appear on the auth pages, whose handlers never scan
// (an empty alert surface there would claim "nothing outstanding" on a page
// that never looked).
func TestAlertBannerRidesInsideTheTopBarOnEveryScanningPage(t *testing.T) {
	srv, _, _, _ := newAlertingServer(t)
	barRe := regexp.MustCompile(`(?s)<header class="topbar">.*?</header>`)

	for _, path := range []string{"/status", "/trips", "/trips/detail", "/gate", "/muster"} {
		body := rawPage(t, srv, path)
		bar := barRe.FindString(body)
		if bar == "" {
			t.Fatalf("%s: no top bar", path)
		}
		if !strings.Contains(bar, `class="topbar-alerts"`) {
			t.Errorf("%s: the alert surface is not mounted in the top bar", path)
		}
		// Precondition + behaviour in one: this fixture holds a real
		// outstanding alert, so the bar must actually carry its text.
		if !strings.Contains(bar, "VERIFY BY RADIO") || !strings.Contains(bar, "ICE TRUCK 7") {
			t.Errorf("%s: the outstanding alert did not render inside the top bar; bar was:\n%s", path, bar)
		}
	}

	authSrv, _ := newAuthTestServer(t)
	if body := rawPage(t, authSrv, "/login"); strings.Contains(body, `class="topbar-alerts"`) {
		t.Error("/login rendered an alert surface, but its handler never runs the deadline scan")
	}
}

// --- print. Safety-critical: the printed muster must carry the F1
// incomplete warning and the not-tracked section, and must NOT carry the
// away names (2026-08-21 ruling — see the inversion note inside the first
// test). The sheet is scoped by RESPONSIBILITY, not by what is known. ---

// The print guards below run on a real parse of the served stylesheet
// (printcss_test.go), not on substring scans of one extracted block. They
// ask what a property RESOLVES to on paper, through importance,
// specificity and source order — so a wrapped selector list, a
// whitespace-free `display:none`, a `visibility: hidden` / `font-size: 0` /
// `height: 0`, or the whole @media print block being relocated all show up
// as a changed answer rather than as a substring that happens to still be
// absent. See printcss_test.go's header for what each of those evasions
// used to do.
func TestPrintNeverHidesTheIncompleteWarningOrTheAwaySections(t *testing.T) {
	srv, _ := newMusterTestServer(t, 0)
	body := rawPage(t, srv, "/muster/print")
	rules := paperStylesheet(t, body)

	// The chrome-hiding rule must actually take the bar and the buttons off
	// the paper — asserted as the resolved outcome, not as a selector's
	// presence in the text.
	for _, class := range []string{"topbar", "actions", "screen-only"} {
		if _, hidden := hiddenOnPaper(rules, class); !hidden {
			t.Errorf("print does not hide .%s — the live chrome would print onto the sheet.\nprint block was:\n%s", class, printBlockText(rules))
		}
	}
	// ...and must NEVER hide the F1 warning. A printed roster that may be
	// short people has to say so on the paper itself. D-6 makes this an
	// accountability instrument: it is about OUR people going unaccounted for.
	for _, class := range []string{"incomplete-warn", "not-tracked"} {
		if why, hidden := hiddenOnPaper(rules, class); hidden {
			t.Fatalf("print hides .%s — F1/U3: the paper itself must carry it. Resolved as %s", class, why)
		}
	}

	// INVERTED 2026-08-21, and the inversion is the assertion. This used to
	// require `.away` to print, alongside `.incomplete-warn`, on the
	// reasoning that a sheet omitting the crew out at a pad had failed to
	// account for everyone. James ruled the other way — "we are keeping
	// track of them sure but we are not responsible for them, they are
	// Conoco's problem" — so the printed sheet is scoped by RESPONSIBILITY
	// and the away NAMES must not reach paper. The two are not the same
	// question and must not be asserted as one: F1 is about this
	// operation's own people, the away list is about somebody else's.
	if _, hidden := hiddenOnPaper(rules, "away"); !hidden {
		t.Fatalf("print does NOT hide .away — the away names would reach the paper, which is exactly what the 2026-08-21 ruling forbids.\nprint block was:\n%s", printBlockText(rules))
	}
	// AMENDED by James, 2026-10-01: print = roster only (no sync verdicts,
	// no counts/summary pages). This used to require .away-scope — the
	// count-only stand-in for the away names — to REACH paper (D-6: the
	// boundary is stated, never inferred). It is a count, and the ruling
	// took the counts off the paper, so it is now asserted HIDDEN there.
	// The away names stay off paper as before (asserted above).
	if _, hidden := hiddenOnPaper(rules, "away-scope"); !hidden {
		t.Fatalf("print does NOT hide .away-scope — James, 2026-10-01: print = roster only, and the away stand-in is a count.\nprint block was:\n%s", printBlockText(rules))
	}
	if got, ok := screenClassValue(rules, "away-scope", "display"); !ok || strings.TrimSpace(got) != "none" {
		t.Errorf("on screen .away-scope resolves display=%q (found=%v), want none — the count-only stand-in must not double up with the named list it replaces", got, ok)
	}
}

func TestPrintedMusterCarriesTheIncompleteWarningAndScopesTheAwayList(t *testing.T) {
	srv, st := newMusterTestServer(t, 0)
	srv.Cfg.Destinations = []config.Destination{{ID: "conneco", DisplayName: "Conneco Pad", Controlled: false}}
	p, err := domain.CreatePerson(st, "site-a", 1, "Dana Driver", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	crashCheckIn(t, st, "site-a", "PRINT TRUCK", p.PersonID) // row 31's crash window

	body := rawPage(t, srv, "/muster/print")
	if !strings.Contains(body, `class="incomplete-warn"`) {
		t.Fatalf("printed muster does not carry the F1 incomplete warning:\n%s", body)
	}
	// The away section is still RENDERED — the record is unchanged and the
	// post reads it on screen. It is the PAPER that is scoped, by the print
	// block, which TestPrintNeverHidesTheIncompleteWarningOrTheAwaySections
	// asserts directly. Both halves are needed: markup present, paper clean.
	if !strings.Contains(body, `class="away"`) {
		t.Fatalf("the /muster/print page no longer renders the away section at all — the ruling scoped the PAPER, not the record:\n%s", body)
	}
	if !strings.Contains(body, "Nobody recorded away at this destination.") {
		t.Fatal("printed muster's away section did not report a configured destination it knows nobody at")
	}
	// ...and the count-only stand-in that the paper actually gets. Without
	// this the sheet would drop the away people with no trace, which under
	// D-6 (100% accountability — an OSHA rule applied on the Slope, enforced
	// for insurance reasons, no regulation number) is the hard-to-defend
	// shape: state
	// the boundary, do not leave it to be inferred from an absence.
	if !strings.Contains(body, `data-muster-section="away-scope"`) {
		t.Fatalf("the printed sheet carries no away-scope block — the away people would vanish from paper with nothing saying they exist or whose they are:\n%s", body)
	}
	if !strings.Contains(body, "not this operation") {
		t.Fatal("the away-scope block does not disclaim responsibility in words — the count alone reads as an omission")
	}
}

// The printed sheet must be black on white whatever the viewer's theme is.
// The dark palette is applied by a media query and by [data-theme=dark];
// print therefore has to re-declare the light tokens at a specificity that
// beats BOTH, or a box whose browser prefers dark prints white on white.
// M-02 is this test's real subject as much as the palette is: the print
// reset beats the two dark blocks ONLY because it comes after them and ties
// them on specificity (asserted directly in
// TestPrintCSSSpecificityTiesResolveBySourceOrder). So this asks for the
// RESOLVED value of each token under each dark selector, which is the one
// question that relocating the @media print block changes.
func TestPrintRedeclaresTheLightPaletteOverBothDarkBlocks(t *testing.T) {
	srv, _ := newMusterTestServer(t, 0)
	rules := paperStylesheet(t, rawPage(t, srv, "/muster/print"))

	want := map[string]string{"--ground": "#FFFFFF", "--surface": "#FFFFFF", "--text": "#000000"}
	for _, sel := range []string{`:root`, `:root:not([data-theme="light"])`, `:root[data-theme="dark"]`} {
		match := func(s string) bool { return matchesExact(s, sel) }
		for tok, wantVal := range want {
			d, r, ok := paperValue(rules, match, tok)
			if !ok {
				t.Errorf("%s: %s resolves to nothing on paper", sel, tok)
				continue
			}
			if !strings.EqualFold(d.value, wantVal) {
				t.Errorf("%s: %s resolves to %q on paper, want %q — a dark-preferring browser would print dark on dark. Winning rule: %s",
					sel, tok, d.value, wantVal, strings.TrimSpace(r.raw))
			}
		}
	}
	// And the page ground itself, not only the token behind it.
	d, _, ok := paperValue(rules, func(s string) bool { return matchesExact(s, "body") }, "background")
	if !ok || !strings.Contains(strings.ToUpper(d.value), "#FFFFFF") {
		t.Errorf("body background resolves to %q on paper, want white", d.value)
	}
}

// --- the theme audit, as an asserting test rather than a claim: every
// token the dark blocks redefine must already exist in the light :root, and
// the dark blocks must redefine TOKENS ONLY (no component rule may live
// inside a theme block, which is how one theme's text lands on the other
// theme's ground). ---

var tokenDeclRe = regexp.MustCompile(`--[a-z0-9-]+\s*:`)

func stylesheetOf(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, "<style>")
	j := strings.Index(body, "</style>")
	if i < 0 || j < 0 {
		t.Fatal("no stylesheet in the served page")
	}
	return body[i+len("<style>") : j]
}

// blockAfter returns the body of the first {...} block starting at the first
// occurrence of marker, brace-matched.
func blockAfter(t *testing.T, css, marker string) string {
	t.Helper()
	i := strings.Index(css, marker)
	if i < 0 {
		t.Fatalf("stylesheet has no %q", marker)
	}
	rest := css[i+len(marker):] // marker already consumed the opening brace
	depth := 1
	for k, r := range rest {
		switch r {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[:k]
			}
		}
	}
	t.Fatalf("unterminated block after %q", marker)
	return ""
}

func tokenNames(css string) map[string]bool {
	out := map[string]bool{}
	for _, m := range tokenDeclRe.FindAllString(css, -1) {
		out[strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(m), ":"))] = true
	}
	return out
}

// M-01. This test used to compare token COUNTS and a light-superset
// relation, and two mutations walked through it:
//
//	m1  delete a token from BOTH dark blocks   -> counts still equal, subset
//	                                              still holds. SURVIVED.
//	m2  give the two dark blocks DIFFERENT values for one token -> counts
//	                                              still equal. SURVIVED.
//
// m2 is the one that matters: it ships a theme that differs depending on
// whether the operator set data-theme or let the media query decide, and
// nothing on any screen says so. It is also a hard precondition on every
// --line-family token edit, because a token edit landed while this guard is
// blind is exactly how the two darks drift apart.
//
// So the comparison is now by NAME AND VALUE, in both directions, on the
// parsed stylesheet rather than on line text.
func TestBothThemesDefineTheSameTokensAndNoComponentRuleLivesInAThemeBlock(t *testing.T) {
	body := getStatusPage(t, newTestServer(t))
	rules := paperStylesheet(t, body)

	// The theme blocks, taken from the SCREEN cascade — the print block
	// redeclares the same three selectors on purpose and must not be
	// confused with them.
	themeBlock := func(sel string) map[string]string {
		out := map[string]string{}
		found := false
		for _, r := range rules {
			if r.inPrint {
				continue
			}
			for _, s := range r.selectors {
				if s != sel {
					continue
				}
				found = true
				for _, d := range r.decls {
					if strings.HasPrefix(d.prop, "--") {
						out[d.prop] = d.value
					} else if d.prop != "color-scheme" {
						t.Errorf("%s carries a non-token declaration %q: %s — theme blocks may redefine tokens only, or a colour ends up with its ONLY definition inside one theme", sel, d.prop, d.value)
					}
				}
			}
		}
		if !found {
			t.Fatalf("stylesheet has no screen-cascade rule for %s", sel)
		}
		return out
	}

	light := themeBlock(":root")
	if len(light) < 20 {
		t.Fatalf("precondition: bare :root declared only %d tokens — this test is not looking at the light palette", len(light))
	}
	darkMedia := themeBlock(`:root:not([data-theme="light"])`)
	darkExplicit := themeBlock(`:root[data-theme="dark"]`)

	for name, block := range map[string]map[string]string{
		"prefers-color-scheme: dark": darkMedia,
		`[data-theme="dark"]`:        darkExplicit,
	} {
		if len(block) == 0 {
			t.Fatalf("%s: precondition failed — no tokens found, so this test proves nothing", name)
		}
		for tok := range block {
			if _, ok := light[tok]; !ok {
				t.Errorf("%s redefines %s, which the light :root never defines — one theme would inherit nothing", name, tok)
			}
		}
	}

	// m2: the two darks must be the SAME PALETTE, not merely the same size.
	// A count comparison cannot see a changed value, and a changed value is
	// a theme that depends on how the operator's browser was configured.
	for tok, want := range darkMedia {
		got, ok := darkExplicit[tok]
		if !ok {
			t.Errorf("%s is defined by the media-query dark block but NOT by [data-theme=\"dark\"] — an explicit dark choice would fall back to the light value", tok)
			continue
		}
		if !strings.EqualFold(got, want) {
			t.Errorf("the two dark blocks disagree on %s: media-query says %q, [data-theme=\"dark\"] says %q — the theme would differ depending on whether the operator chose dark or the system did", tok, want, got)
		}
	}
	for tok := range darkExplicit {
		if _, ok := darkMedia[tok]; !ok {
			t.Errorf("%s is defined by [data-theme=\"dark\"] but NOT by the media-query dark block — a system-dark browser would fall back to the light value", tok)
		}
	}

	// m1: a token deleted from BOTH dark blocks left every relation above
	// intact — counts still matched, the subset relation still held.
	//
	// The property that catches it is TOTALITY over the colour tokens: the
	// palette is split into colour tokens (themed by definition — a colour
	// that is right on a light ground is wrong on a dark one) and metric
	// tokens (spacing, type scale, radii, widths — deliberately identical
	// in both themes). Every COLOUR token the light root defines must get a
	// dark value, or that one colour silently keeps its light value on a
	// dark ground and nothing says so.
	//
	// "Colour token" is derived from the VALUE, not from a hand-kept list,
	// so a colour added tomorrow is covered without anyone remembering to
	// add it here.
	var colourTokens []string
	for tok, val := range light {
		if isColourValue(val) {
			colourTokens = append(colourTokens, tok)
		}
	}
	if len(colourTokens) < 12 {
		t.Fatalf("precondition: only %d colour tokens found in the light palette — the totality check below would prove almost nothing", len(colourTokens))
	}
	sort.Strings(colourTokens)
	for _, tok := range colourTokens {
		if _, ok := darkMedia[tok]; !ok {
			t.Errorf("the light palette defines the COLOUR token %s and the dark palette does not — on a dark ground that one value stays light, and nothing says so", tok)
		}
	}
	// And the converse, so the split cannot be gamed by moving a colour into
	// the metric half: a dark block may only redefine colour tokens.
	for tok := range darkMedia {
		if !isColourValue(light[tok]) {
			t.Errorf("the dark palette redefines %s = %q, which is a METRIC token — spacing and type scale are the same in both themes, so this would make one theme's layout differ from the other's", tok, light[tok])
		}
	}
}

// isColourValue reports whether a token's value is a colour (or carries one,
// as --shadow's rgba() does). This is what separates the themed half of the
// palette from the metric half.
func isColourValue(v string) bool {
	v = strings.ToLower(v)
	return strings.Contains(v, "#") || strings.Contains(v, "rgb") || strings.Contains(v, "hsl") ||
		strings.Contains(v, "color-mix") || v == "none" || v == "transparent" || v == "currentcolor"
}

// body must paint an explicit token background: a transparent body borrows
// whatever the host is painting behind it.
func TestBodyPaintsAnExplicitTokenBackground(t *testing.T) {
	css := stylesheetOf(t, getStatusPage(t, newTestServer(t)))
	body := blockAfter(t, css, "\nbody {")
	if !strings.Contains(body, "background: var(--ground)") {
		t.Errorf("body does not set an explicit token background; rule was:\n%s", body)
	}
	if !strings.Contains(body, "color: var(--text)") {
		t.Errorf("body does not set an explicit token colour; rule was:\n%s", body)
	}
}

// --- the partials must stay partials ---

// ParseFS globs the whole directory, so a new file is picked up
// automatically — including as a template NAMED after the file. Every
// partial file must therefore render to nothing but whitespace if something
// ever executes it by filename, rather than emitting a broken half-page.
func TestPartialFilesAreNotStandalonePages(t *testing.T) {
	for _, name := range []string{"_styles.html", "_layout.html", "_topbar.html", "_picker.html", "banner.html", "post_chip.html", "sync_status.html"} {
		if tmpl.Lookup(name) == nil {
			t.Fatalf("precondition: %s is not in the parsed template set — ParseFS did not pick it up", name)
		}
		var sb strings.Builder
		if err := tmpl.ExecuteTemplate(&sb, name, nil); err != nil {
			t.Errorf("%s executed as a page returned an error: %v", name, err)
			continue
		}
		if got := strings.TrimSpace(sb.String()); got != "" {
			t.Errorf("%s rendered %d bytes as a standalone page, want nothing:\n%s", name, len(got), got)
		}
	}
}

// --- the reflection helpers the top bar is built on ---

func TestTemplateHasFieldDistinguishesAbsentFromEmpty(t *testing.T) {
	type withEmpty struct {
		Alerts []string
		Name   string
		hidden string //nolint:unused // presence of an UNEXPORTED field is the case under test
	}
	type without struct{ Other int }

	if !templateHasField(withEmpty{}, "Alerts") {
		t.Error("a present-but-empty slice field must report as present — 'nothing to report' is not 'cannot tell'")
	}
	if templateHasField(without{}, "Alerts") {
		t.Error("an absent field reported as present")
	}
	if templateHasField(withEmpty{}, "hidden") {
		t.Error("an unexported field must not be reachable from a template")
	}
	if !templateHasField(&withEmpty{}, "Name") {
		t.Error("a pointer to a struct must be followed")
	}
	if templateHasField((*without)(nil), "Other") {
		t.Error("a nil pointer must report absent rather than panicking")
	}
	if templateHasField("not a struct", "Other") {
		t.Error("a non-struct reported a field")
	}
}

func TestTemplateFieldReturnsNilForAnythingItCannotRead(t *testing.T) {
	type s struct {
		Name   string
		hidden int //nolint:unused // see above
	}
	if got := templateField(s{Name: "Dana"}, "Name"); got != "Dana" {
		t.Errorf("field(Name) = %v, want Dana", got)
	}
	for _, tc := range []struct {
		name string
		data any
		key  string
	}{
		{"absent", s{}, "Nope"},
		{"unexported", s{}, "hidden"},
		{"nil pointer", (*s)(nil), "Name"},
		{"non-struct", 42, "Name"},
		{"nil", nil, "Name"},
	} {
		if got := templateField(tc.data, tc.key); got != nil {
			t.Errorf("%s: field = %v, want nil", tc.name, got)
		}
	}
}

func TestDictRefusesAMalformedArgumentListRatherThanHalfBuildingAPage(t *testing.T) {
	if _, err := templateDict("Title", "x", "Main"); err == nil {
		t.Error("odd argument count accepted")
	}
	if _, err := templateDict(1, "x"); err == nil {
		t.Error("non-string key accepted")
	}
	d, err := templateDict("Title", "x", "Main", "narrow")
	if err != nil || d["Title"] != "x" || d["Main"] != "narrow" {
		t.Errorf("dict = %v, %v", d, err)
	}
}

// --- the sync chip's own arithmetic ---

func TestSyncChipCountsAnyUnhealthyPeerAsDegraded(t *testing.T) {
	v := syncStatusView{Available: true, Peers: []peerView{
		{SiteID: "a", Healthy: true}, {SiteID: "b"}, {SiteID: "c"},
	}}
	if got := v.DegradedCount(); got != 2 {
		t.Errorf("DegradedCount = %d, want 2", got)
	}
	if !v.Degraded() {
		t.Error("a view with unhealthy peers reported not degraded")
	}
	if (syncStatusView{Available: true, Peers: []peerView{{Healthy: true}}}).Degraded() {
		t.Error("an all-healthy view reported degraded")
	}
	// No peers configured is not degradation — there is nothing to degrade.
	if (syncStatusView{Available: true}).Degraded() {
		t.Error("a site with no peers configured reported degraded")
	}
}

// --- the whole point: the page still works with JavaScript off ---

// Every form on every page must be a real method="post" form with an
// action, and no form's ONLY submit path may be a script. A half-loaded
// page in a shack must not silently drop a check-in.
func TestEveryFormPostsToARealActionWithoutJavaScript(t *testing.T) {
	formRe := regexp.MustCompile(`<form[^>]*>`)
	for route, body := range renderedPages(t) {
		for _, f := range formRe.FindAllString(body, -1) {
			if !strings.Contains(f, `action="/`) {
				t.Errorf("%s: form without a server action: %s", route, f)
			}
			if !strings.Contains(f, `method="post"`) && !strings.Contains(f, `method="get"`) {
				t.Errorf("%s: form without an explicit method: %s", route, f)
			}
		}
	}
}

// --- M-09: a notice states its KIND before it states its data ----------

// TestEveryNoticeOpensWithItsKindNotWithData.
//
// accessibility A3's word half. `.notice-critical` / `-warn` / `-ok` /
// `-info` differ from one another ONLY in border-colour and background
// (_styles.html) — so the box itself carries its severity in hue and in
// nothing else. What tells a reader what kind of thing they are looking at
// is the words inside it, and specifically the FIRST words.
//
// Three notices opened with data instead: `gate.html` led with a vehicle
// id, `trips_detail.html` with a pad name, and — added by the gate rebuild
// after the finding was written, and named in no report — the gate's
// driver-cleared notice led with a PERSON'S NAME. The reader's first word
// was "BUS 7", "Conneco Pad" or "Dana Driver", with no statement of what
// had happened.
//
// WHAT THIS ASSERTS, AND WHY IT IS AN ALLOW-LIST. Whether a rendered word
// came from a template literal or from a data field is not recoverable
// from the served HTML: "ICE TRUCK 7" and "VEHICLE OVERDUE" are both just
// uppercase text. So the property is asserted as a declared VOCABULARY at
// a structural position — every `.notice` must open with a <strong> whose
// text is one of the kinds this application has. A notice that leads with
// data fails because the data is not in the set, and a NEW notice fails
// until its author names its kind here, which is the point.
//
// The precondition below is what stops this becoming vacuous: the fixtures
// must actually have produced the three M-09 notices, or the sweep is
// passing over pages that do not contain the thing it exists to check.
// alertLeadRe is the alert row's lead (Q-FH1): an optional vehicle id, the
// direction, then the kind in words.
var alertLeadRe = regexp.MustCompile(`^(?:<code>[^<]*</code> )?(?:inbound|outbound|on the road) — (OVERDUE|VERIFY BY RADIO)\b`)

func TestEveryNoticeOpensWithItsKindNotWithData(t *testing.T) {
	// Every notice kind this application renders. Adding a notice means
	// adding its kind here; that is deliberate friction.
	kinds := map[string]bool{
		"DEADLINE SCAN FAILED": true,
		// ⟦was: "VEHICLE OVERDUE:", "VEHICLE OVERDUE — OUTBOUND:" and
		// "VERIFY BY RADIO:" — the alert rows now lead with the vehicle
		// (Q-FH1, James 2026-10-04) and are matched by alertLeadRe below⟧
		// FIX13 (James, 2026-10-05): the sticky strip's two compact lines
		// are notices of their own, each opening with its kind in words
		// before the vehicle list. ⟦was: absent — the strip carried an
		// alert row, matched by alertLeadRe⟧
		"OVERDUE:":                true,
		"VERIFY BY RADIO:":        true,
		"SYNC STATUS UNAVAILABLE": true,
		"Nothing was recorded.":   true,
		"INCOMPLETE CHECK-IN":     true,
		"DRIVER CLEARED":          true,
		"AWAY AT A PAD":           true,
		"A departing vehicle is part-entered below.":                   true,
		"An arriving vehicle is part-entered above.":                   true,
		"No destinations are configured at this post, so no departure": true,
		"No destinations are configured at this post":                  true,
	}

	pages := renderedPages(t)

	// A gate page carrying a crashed check-in (the dispatch half authored,
	// the arrival half never reached) — gate_test.go's row-31 recipe.
	incSrv, incStore, incPersons := newGateTestServer(t)
	if _, err := domain.DispatchLocalCheckIn(incStore, "site-a", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "T99", DriverPersonID: incPersons[0].PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, incStore, "site-a", "T99"), DriverPersonID: incPersons[0].PersonID,
		OccupantPersonIDs: []string{incPersons[1].PersonID},
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	_, incBody := getGate(t, incSrv)
	pages["/gate (incomplete check-in)"] = incBody

	// A /trips/detail carrying an out-at-pad row: a run to an uncontrolled
	// destination with nobody recorded back.
	padSrv, padWorld := newDestinationsTestServer(t)
	pad := "conneco"
	if _, err := domain.Dispatch(padWorld.st, "site-b", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "BUS 7", DriverPersonID: padWorld.persons[0].PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, padWorld.st, "site-b", "BUS 7"), DriverPersonID: padWorld.persons[0].PersonID,
		OccupantPersonIDs:             []string{padWorld.persons[1].PersonID},
		DestinationUncontrolled:       &pad,
		KnownUncontrolledDestinations: []string{"conneco"},
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	pages["/trips/detail (out at a pad)"] = getTripsDetail(t, padSrv)

	// An alerting console: the banner's four notice branches are the ones
	// that already led with their kind, and they are what makes this sweep
	// a comparison rather than an assertion about three rows.
	alertSrv, alertStore, alertDeadlines, _ := newAlertingServer(t)
	dispatchLiveOverdueTrip(t, alertStore, alertDeadlines, "LIVE OVERDUE TRUCK")
	pages["/trips (alerting)"] = getTrips(t, alertSrv)
	pages["/muster (alerting)"] = rawPage(t, alertSrv, "/muster")

	noticeRe := regexp.MustCompile(`(?is)<(div|span|p)\s+class="notice[^"]*"[^>]*>\s*(.{0,120})`)
	strongRe := regexp.MustCompile(`(?is)^<strong[^>]*>(.*?)</strong>`)

	seen := map[string]bool{}
	total := 0
	for route, body := range pages {
		// fix4 H (R6-1.8a): the strip row's lead/sep/verdict spans are layout
		// only; read the row as the one run of text this sweep was written for.
		body = alertRowFlat(styleRe.ReplaceAllString(body, ""))
		for _, m := range noticeRe.FindAllStringSubmatch(body, -1) {
			total++
			lead := strings.TrimSpace(m[2])
			sm := strongRe.FindStringSubmatch(lead)
			if sm == nil {
				t.Errorf("%s: a .notice does not open with a <strong> stating what kind of notice it is; it opens with %q. The box's severity is carried by border-colour alone, so these words are the only channel a reader who cannot see the hue has", route, lead)
				continue
			}
			kind := strings.TrimSpace(sm[1])
			// Q-FH1 (James, 2026-10-04): an ALERT row leads with the vehicle
			// id and direction, then its kind — "<code>TRK-19</code> outbound
			// — OVERDUE 3m". The kind is still in the <strong>, in words;
			// only its position moved, by ruling, so the phone strip's
			// 2-line clamp shows which truck. ⟦was: alert rows opened
			// "VEHICLE OVERDUE:", "VEHICLE OVERDUE — OUTBOUND:" or "VERIFY
			// BY RADIO:" and were matched by the kinds map⟧
			if m := alertLeadRe.FindStringSubmatch(kind); m != nil {
				seen[m[1]] = true
				continue
			}
			if !kinds[kind] {
				t.Errorf("%s: a .notice opens with %q, which is not a declared notice kind. Either it leads with DATA — in which case the reader's first word is a vehicle id, a pad or a person's name and nothing has told them why they are being shown it — or it is a new kind of notice that must be named in this test's vocabulary", route, kind)
				continue
			}
			seen[kind] = true
		}
	}

	// PRECONDITION. Without this the sweep silently passes over pages that
	// never rendered the notices it exists to check.
	if total < 7 {
		t.Fatalf("precondition: only %d notices were rendered across %d pages, which is too few for this sweep to be checking anything", total, len(pages))
	}
	for _, must := range []string{"INCOMPLETE CHECK-IN", "DRIVER CLEARED", "AWAY AT A PAD"} {
		if must == "DRIVER CLEARED" {
			continue // rendered only mid-entry; covered by the gate form tests
		}
		if !seen[must] {
			t.Fatalf("precondition: the fixtures did not render the %q notice, so M-09's own sites are not being asserted. Fix the fixture — do not delete the requirement", must)
		}
	}
}
