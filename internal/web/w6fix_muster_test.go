// w6fix_muster_test.go — W6 (James, 2026-10-03): muster, print and
// cross-post presence, at the surfaces.
//
//   - M-5 / M-19: a reprint names everyone on the original sheet who has
//     checked out since, with the time, instead of dropping them in silence.
//   - M-22: a hand-edited link naming one id twice lists the person once and
//     says the link was malformed.
//   - M-23: a hand-edited link naming ANOTHER post's visit is refused loudly,
//     never printed under this post's identity.
//   - M-6 / T-L08-C: one person with open visits at two posts is flagged
//     "also recorded on site at <peer> since HH:MM" on the muster (live and
//     paper) and the gate roster AT BOTH POSTS, and stays counted at both.
//   - RESTORED markers on /analytics and /analytics/site/{site} like the
//     muster (orchestrator default, FIX-PLAN JAMES RULINGS).
//   - M-46: the summary and not-tracked tables carry their category in a
//     header row that repeats on every printed page the table runs onto.
//
// Every test here was RED on fork/next a982de3 (the hooks it asserts on did
// not exist) and is GREEN with the change. Structural hooks are data-*
// attributes (F19); stylesheet facts go through printcss_test.go's
// cascade-aware parser.
package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// w6fixOpenVisit is person's one open visit at site — asserted to be exactly
// one (rule 6: the fixture must pose its case, not merely be described).
func w6fixOpenVisit(t *testing.T, st *store.Store, site, person string) domain.Visit {
	t.Helper()
	open, err := domain.OpenVisitsAt(st, site)
	if err != nil {
		t.Fatal(err)
	}
	var found []domain.Visit
	for _, v := range open {
		if v.PersonID == person {
			found = append(found, v)
		}
	}
	if len(found) != 1 {
		t.Fatalf("precondition: %s has %d open visit(s) at %s, want exactly 1", person, len(found), site)
	}
	return found[0]
}

func w6fixReprintHref(t *testing.T, page string) string {
	t.Helper()
	m := regexp.MustCompile(`href="(/muster/print\?visits=[^"]+)"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no reprint link on the page:\n%s", page)
	}
	return strings.ReplaceAll(m[1], "&amp;", "&")
}

// w6fixLI returns the <li> (or roster row) that names person on the page —
// the region a per-name tag must sit inside.
func w6fixNameRegion(t *testing.T, page, name string) string {
	t.Helper()
	bare := styleRe.ReplaceAllString(page, "")
	at := strings.Index(bare, name)
	if at < 0 {
		t.Fatalf("%q is not on the page", name)
	}
	end := strings.Index(bare[at:], "</li>")
	if e2 := strings.Index(bare[at:], "</label>"); e2 >= 0 && (end < 0 || e2 < end) {
		end = e2
	}
	if end < 0 {
		end = len(bare) - at
	}
	return bare[at : at+end]
}

func TestReprintNamesEveryoneOnTheOriginalSheetWhoHasCheckedOutSince(t *testing.T) {
	srv, st := newMusterTestServer(t, 0)
	co, err := domain.CreateCompany(st, "site-a", 1, "Kakivik", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, n := range []string{"Ashdahl, Shea", "Brown, Ann", "Carter, Mo"} {
		p, err := domain.CreatePerson(st, "site-a", 1, n, &co.CompanyID, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.PersonID)
	}
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	for _, id := range ids {
		checkIn(t, st, "site-a", id, nil)
	}
	href := w6fixReprintHref(t, rawPage(t, srv, "/muster"))
	before := rawPage(t, srv, href)
	if !strings.Contains(before, `data-tally-count="on-site">3<`) || strings.Contains(before, `data-muster-section="checked-out-since"`) {
		t.Fatalf("precondition: the fresh reprint should list 3 and carry no checked-out block:\n%s", tallySnippet(before))
	}

	// L10 M-19's repro: one listed person is removed at the gate.
	gone := w6fixOpenVisit(t, st, "site-a", ids[0])
	st.Clock = w2Reading(w2Base+90*60_000, store.ClockSynced) // 90 min later
	if err := domain.CheckOutVisit(st, "site-a", 1, gone.VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	after := rawPage(t, srv, href)
	flat := strings.Join(strings.Fields(after), " ")

	// The roster lists the two still on site; the departed one is NAMED,
	// with the time, under it — never silently dropped.
	if !strings.Contains(after, `data-tally-count="on-site">2<`) {
		t.Errorf("reprint on-site total: want 2 (the roster lists open visits) — %s", tallySnippet(after))
	}
	if !strings.Contains(after, `data-checked-out-count="1"`) {
		t.Errorf("the reprint does not say how many of the original sheet have checked out since (want 1) — M-19: the departed person vanished without a word")
	}
	if !strings.Contains(after, `data-checked-out-visit="`+gone.VisitID+`"`) {
		t.Errorf("the departed visit %s is not named on the reprint", gone.VisitID)
	}
	wantWhen := time.UnixMilli(w2Base+90*60_000).In(time.Local).Format(signInLayout)
	if !strings.Contains(flat, "Ashdahl, Shea — checked out "+wantWhen) {
		t.Errorf("the departed person is not named with the check-out time; want %q in:\n%s", "Ashdahl, Shea — checked out "+wantWhen, flat)
	}
	if !strings.Contains(flat, "1 on the original sheet has checked out since") {
		t.Errorf("the ruled sentence is missing: want \"1 on the original sheet has checked out since\"")
	}
	// The false promise is gone; the true sentence stands.
	if strings.Contains(flat, "reproduces the same roster no matter when it is printed again") {
		t.Errorf("the reprint still carries the sentence M-19 measured as false")
	}
	if !strings.Contains(flat, "anyone on the original sheet who has checked out since is named") {
		t.Errorf("the reprint's mode-detail does not say the departed are named")
	}
	// And it PRINTS: the block is not hidden by the roster-only print rules.
	rules := paperStylesheet(t, after)
	if why, hidden := hiddenOnPaper(rules, "checked-out-since"); hidden {
		t.Errorf(".checked-out-since is hidden on paper (%s) — the departed names must reach the paper", why)
	}
	// A reprint with nobody departed is unchanged: no block at all (the
	// stylesheet carries the class on every page; the HOOK is the fact).
	if strings.Contains(styleRe.ReplaceAllString(before, ""), `data-muster-section="checked-out-since"`) {
		t.Errorf("a reprint with nobody departed renders the block")
	}
}

func TestReprintReadsADuplicatedVisitIdOnceAndSaysSo(t *testing.T) {
	srv, st := newMusterTestServer(t, 0)
	p, err := domain.CreatePerson(st, "site-a", 1, "Stenham, Peyton", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn(t, st, "site-a", p.PersonID, nil)
	v := w6fixOpenVisit(t, st, "site-a", p.PersonID)
	page := rawPage(t, srv, "/muster/print?visits="+v.VisitID+","+v.VisitID)
	if !strings.Contains(page, `data-tally-count="on-site">1<`) {
		t.Errorf("one person named twice in the link counts %s — want 1 (M-22)", tallySnippet(page))
	}
	if n := strings.Count(styleRe.ReplaceAllString(page, ""), "Stenham, Peyton"); n != 1 {
		t.Errorf("the person is listed %d times, want once", n)
	}
	if !strings.Contains(page, `data-duplicate-ids="1"`) {
		t.Errorf("the sheet does not say the link named 1 id more than once")
	}
	// Control: a well-formed link says nothing about duplicates.
	if clean := rawPage(t, srv, "/muster/print?visits="+v.VisitID); strings.Contains(clean, "data-duplicate-ids") {
		t.Errorf("a well-formed link is reported as malformed")
	}
}

func TestReprintRefusesAnotherPostsVisitIdLoudly(t *testing.T) {
	st := w3OpenRegistered(t, "w6fix-foreign.db")
	iceRoad := New(w3ConfigSiteA(st.Path), st)
	w9aSeedPerson(t, st, "site-b", 1, "p-pad", "Norsen, Nico")
	checkIn(t, st, "site-b", "p-pad", nil)
	padVisit := w6fixOpenVisit(t, st, "site-b", "p-pad")

	rec := httptest.NewRecorder()
	iceRoad.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/muster/print?visits="+padVisit.VisitID, nil))
	if rec.Code != http.StatusGone {
		t.Fatalf("GET site-a /muster/print?visits=<site-b visit> = %d, want 410 — M-23: a pad person printed under the ice road's sheet identity:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{padVisit.VisitID, "Pad Site Control", "another post"} {
		if !strings.Contains(body, want) {
			t.Errorf("the refusal does not name %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "data-muster-mode") || strings.Contains(body, "Norsen, Nico") {
		t.Errorf("a partial sheet was rendered alongside the refusal")
	}
	// The same id at ITS OWN post prints normally.
	pad := New(w3ConfigSiteB(st.Path), st)
	if page := rawPage(t, pad, "/muster/print?visits="+padVisit.VisitID); !strings.Contains(page, "Norsen, Nico") {
		t.Errorf("site-b's own reprint of its own visit does not list the person")
	}
}

func TestAPersonRecordedOnSiteAtTwoPostsIsFlaggedOnBothMustersAndGateRostersAndStaysCounted(t *testing.T) {
	st := w3OpenRegistered(t, "w6fix-elsewhere.db")
	iceRoad := New(w3ConfigSiteA(st.Path), st) // "Ice Road"
	pad := New(w3ConfigSiteB(st.Path), st)     // "Pad Site Control"
	w9aSeedPerson(t, st, "site-a", 1, "p-ravton", "Ravton, Val")
	w9aSeedPerson(t, st, "site-a", 1, "p-control", "Control, Cal")
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	checkIn(t, st, "site-a", "p-ravton", []string{"p-control"})
	va := w6fixOpenVisit(t, st, "site-a", "p-ravton")

	// T-L08-C: Ravton turns up at the pad unannounced, from the ice road.
	st.Clock = w2Reading(w2Base+2*3_600_000, store.ClockSynced)
	if _, err := domain.RecordUnsolicitedArrival(st, "site-b", 1, w7SeedVehicleID(t, st, "site-b", "WALK-1"), "site-a",
		[]string{"p-ravton"}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	vb := w6fixOpenVisit(t, st, "site-b", "p-ravton")
	if again := w6fixOpenVisit(t, st, "site-a", "p-ravton"); again.VisitID != va.VisitID {
		t.Fatalf("precondition: site-a's visit changed — the double record is not posed")
	}
	sinceA := time.UnixMilli(w2Base).In(time.Local).Format(signInLayout)
	sinceB := time.UnixMilli(w2Base+2*3_600_000).In(time.Local).Format(signInLayout)

	type post struct {
		name      string
		srv       *Server
		peerID    string
		peerLabel string
		since     string // the PEER visit's sign-in time
		onSite    string // this post's own on-site total
	}
	posts := []post{
		{"ice road", iceRoad, "site-b", "Pad Site Control", sinceB, "2"},
		{"pad", pad, "site-a", "Ice Road", sinceA, "1"},
	}
	for _, p := range posts {
		for _, route := range []string{"/muster", "/muster/print"} {
			page := rawPage(t, p.srv, route)
			region := w6fixNameRegion(t, page, "Ravton, Val")
			want := `data-also-on-site-at="` + p.peerID + `"`
			if !strings.Contains(region, want) {
				t.Errorf("%s %s: Ravton's row carries no %s — one person, two evacuation sheets, neither says so (T-L08-C)", p.name, route, want)
			}
			if !strings.Contains(region, "ALSO ON SITE AT "+p.peerLabel+" since "+p.since) {
				t.Errorf("%s %s: want %q in Ravton's row, got %q", p.name, route, "ALSO ON SITE AT "+p.peerLabel+" since "+p.since, region)
			}
			if !strings.Contains(page, `data-elsewhere-count="1"`) {
				t.Errorf("%s %s: the sheet does not count the double-recorded names (want 1)", p.name, route)
			}
			// BOTH STAY COUNTED (the ruling): the on-site total is unchanged.
			if !strings.Contains(page, `data-tally-count="on-site">`+p.onSite+`<`) {
				t.Errorf("%s %s: on-site total changed — want %s; the flag is read-time only, nothing is closed: %s", p.name, route, p.onSite, tallySnippet(page))
			}
			// The tag PRINTS — it is about whether the names are true.
			rules := paperStylesheet(t, page)
			for _, class := range []string{"elsewhere-tag", "elsewhere-note"} {
				if why, hidden := hiddenOnPaper(rules, class); hidden {
					t.Errorf("%s %s: .%s is hidden on paper (%s)", p.name, route, class, why)
				}
			}
		}
		// The gate roster: the officer's working list says the same thing.
		gate := rawPage(t, p.srv, "/gate")
		if !strings.Contains(gate, `data-roster-elsewhere="1"`) {
			t.Errorf("%s /gate: the roster does not say how many rows are also on site elsewhere (want 1)", p.name)
		}
		region := w6fixNameRegion(t, gate, "Ravton")
		if !strings.Contains(region, `data-also-on-site-at="`+p.peerID+`"`) {
			t.Errorf("%s /gate: Ravton's roster row carries no flag for %s: %q", p.name, p.peerID, region)
		}
	}
	// The CONTROL, on site at the ice road only, carries nothing anywhere.
	for _, route := range []string{"/muster", "/muster/print", "/gate"} {
		if region := w6fixNameRegion(t, rawPage(t, iceRoad, route), "Control, Cal"); strings.Contains(region, "data-also-on-site-at") {
			t.Errorf("ice road %s: the control person is flagged: %q", route, region)
		}
	}
	// No cross-site write happened: both visits are still open.
	for _, v := range []domain.Visit{va, vb} {
		row, err := domain.VisitByID(st, v.VisitID)
		if err != nil || row == nil || row.IsOpen != 1 {
			t.Errorf("visit %s is no longer open (%v) — rendering the flag must write nothing", v.VisitID, err)
		}
	}
}

// RESTORED markers on the analytics pages like the muster — the R-23 fixture
// (r23_restored_marker_test.go) re-read on /analytics and the site page.
func TestAnalyticsPagesMarkRestoredVisitsLikeTheMuster(t *testing.T) {
	st := w3OpenRegistered(t, "w6fix-an-restored.db")
	cfg := w3ConfigSiteB(st.Path)
	cfg.NodeEpoch = 2 // the re-imaged post
	srv := New(cfg, st)
	const forgedVisit = "site-b-visit-restored00000001"
	forged := w9aEnvelope(t, "site-b", 1, 1, w2Base, map[string]any{
		"type": "orientation_completed", "person_id": "p-stranger",
		"visit": map[string]any{"visit_id": forgedVisit, "person_id": "p-stranger", "person_name": "Diaz, Restored"},
	})
	if applied, err := st.Apply(forged); err != nil || !applied {
		t.Fatalf("self-history page did not apply (applied=%v err=%v)", applied, err)
	}
	w9aSeedPerson(t, st, "site-b", 2, "p-local", "Local, Lee")
	num := "TRK-118"
	veh, err := domain.CreateVehicle(st, "site-b", 2, &num, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := domain.GateCheckIn(st, "site-b", 2, domain.GateCheckInInput{
		VehicleID: veh.VehicleID, Driver: domain.PersonSlot{ExistingID: "p-local"},
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	local := w6fixOpenVisit(t, st, "site-b", "p-local")

	code, page := anSiteGet(t, srv, "GET", "/analytics", true)
	if code != 200 {
		t.Fatalf("/analytics = %d:\n%s", code, page)
	}
	if !strings.Contains(page, `data-an-restored="1"`) {
		t.Errorf("/analytics: this post's card does not mark 1 RESTORED beside persons on site")
	}
	if !strings.Contains(page, `data-an-restored-own="1"`) {
		t.Errorf("/analytics: the overview does not mark 1 RESTORED at this post")
	}
	code, site := anSiteGet(t, srv, "GET", "/analytics/site/site-b", true)
	if code != 200 {
		t.Fatalf("/analytics/site/site-b = %d:\n%s", code, site)
	}
	if !strings.Contains(site, `data-restored-visit="`+forgedVisit+`"`) {
		t.Errorf("site page: the restored visit %s is listed without the RESTORED tag", forgedVisit)
	}
	if !strings.Contains(site, `data-restored-count="1"`) {
		t.Errorf("site page: no RESTORED count (want 1)")
	}
	if strings.Contains(site, `data-restored-visit="`+local.VisitID+`"`) {
		t.Errorf("site page: the LOCAL first-hand visit is tagged RESTORED")
	}
	if !strings.Contains(site, `data-an-count="on-site">2<`) {
		t.Errorf("site page: the restored person must still be counted (want 2)")
	}
}

// M-46: the category rides in each table's <thead> so a printed page the
// table continues onto still says what the names are.
func TestSummaryAndNotTrackedTablesCarryTheirCategoryInARepeatingHeaderRowOnPaper(t *testing.T) {
	srv := musterWithAllThreeCategories(t)
	srv.Cfg.MusterMinGroupSize = 99 // every crew folds into the summary table
	for _, route := range []string{"/muster", "/muster/print"} {
		page := rawPage(t, srv, route)
		bare := styleRe.ReplaceAllString(page, "")
		for _, sec := range []string{"folded", "not-tracked"} {
			if !strings.Contains(bare, `data-muster-section="`+sec+`"`) {
				t.Fatalf("%s: precondition: no %q section — the fixture must carry both tables", route, sec)
			}
		}
		summary := sectionBody(bare, "folded")
		if !regexp.MustCompile(`(?s)<thead><tr class="page-cat-row"><th colspan="3"><span class="page-cat-tag">ON SITE</span></th></tr>`).MatchString(summary) {
			t.Errorf("%s: the summary table's <thead> carries no ON SITE row — pages 4-6 of a long sheet print names with no category (M-46)", route)
		}
		nt := sectionBody(bare, "not-tracked")
		if !strings.Contains(nt, `<thead><tr class="page-cat-row"><th colspan="2"><span class="page-cat-tag">NOT TRACKED — NOT ON SITE</span></th></tr>`) {
			t.Errorf("%s: the not-tracked table's <thead> carries no category row", route)
		}
		rules := paperStylesheet(t, page)
		if got, ok := screenClassValue(rules, "page-cat-row", "display"); !ok || strings.TrimSpace(got) != "none" {
			t.Errorf("%s: .page-cat-row on SCREEN resolves display=%q, want none (the heading is already visible there)", route, got)
		}
		d, _, ok := paperValue(rules, func(sel string) bool { return targetsClass(sel, "page-cat-row") }, "display")
		if !ok || strings.TrimSpace(d.value) != "table-row" {
			t.Errorf("%s: .page-cat-row on PAPER resolves display=%q, want table-row (a header group repeats per printed page; a hidden row repeats nothing)", route, d.value)
		}
	}
}
