// muster_away_test.go — the "away at uncontrolled destinations" view on the
// muster (team-lead follow-up, 2026-08-20): during an evacuation the officer
// needs ON SITE and OUT AT A PAD, and a derivable-but-invisible answer is
// the F1/F3/F6 pattern this project keeps re-finding. The section rides
// BOTH the live muster and the print, at zero added interactions. Styles
// are stripped before matching; assertions are on visible text.
package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

func getPage(t *testing.T, srv *Server, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	// Status is uninformative here (catch-all 200) — callers assert on body.
	return styleRe.ReplaceAllString(rec.Body.String(), "")
}

// sendPatToThePad puts both people on site, runs BUS 7 to the pad and
// returns it with Pat marked as stayed.
func sendPatToThePad(t *testing.T, w *destWorld) {
	t.Helper()
	walkIn, err := domain.DispatchLocalCheckIn(w.st, "site-b", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "gate walk-in", DriverPersonID: w.persons[0].PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.st, "site-b", "gate walk-in"), DriverPersonID: w.persons[0].PersonID,
		OccupantPersonIDs: []string{w.persons[1].PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordExpectedArrival(w.st, "site-b", 1, walkIn.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	pad := "conneco"
	run, err := domain.Dispatch(w.st, "site-b", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "BUS 7", DriverPersonID: w.persons[0].PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, w.st, "site-b", "BUS 7"), DriverPersonID: w.persons[0].PersonID,
		OccupantPersonIDs:       []string{w.persons[1].PersonID},
		DestinationUncontrolled: &pad,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordExpectedArrivalExcluding(w.st, "site-b", 1, run.TripID,
		[]string{w.persons[1].PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
}

func TestMusterLiveShowsAwayAtPadSection(t *testing.T) {
	srv, w := newDestinationsTestServer(t)
	sendPatToThePad(t, w)

	page := getPage(t, srv, "/muster")
	for _, want := range []string{
		"Away at uncontrolled destinations",
		// M-16: was "NOT on site", the half of the old category tag that
		// stated a disjointness the domain refuses. Re-pointed at the
		// corrected sentence, same position, same strength.
		"counted separately from the total above",
		"Conneco Pad",
		"Pat Passenger",
		"BUS 7",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("live muster missing %q:\n%s", want, page)
		}
	}
	// Pat appears ONLY in the away section — never in the on-site roster.
	// Dana (who came back) is on the roster and not away.
	if got := strings.Count(page, "Pat Passenger"); got != 1 {
		t.Fatalf("Pat Passenger appears %d times on the live muster, want exactly 1 (the away section)", got)
	}
	if !strings.Contains(page, "Dana Driver") {
		t.Fatal("the returnee is missing from the on-site roster")
	}
	// The on-site total, read off the headcount block's own structural hook
	// rather than off a floating "Total on site: N" sentence. U3 turned that
	// sentence into a laid-out block, and a substring that spans markup is
	// exactly the assertion that breaks on a restyle without the behaviour
	// changing. The attribute pins the number to the ON-SITE tally
	// specifically, which the old string could not do.
	if !strings.Contains(page, `data-tally-count="on-site">1<`) {
		t.Fatalf("away people must not count toward the on-site total:\n%s", page)
	}
	// ...and they are counted, by name, in the AWAY tally instead — the
	// other half of "not in the total" that the old assertion never checked.
	if !strings.Contains(page, `data-tally-count="away">1<`) {
		t.Fatalf("the away headcount does not report the one person out at a pad:\n%s", page)
	}
}

// The /muster/print PAGE still renders the away list — it is read on screen
// before the sheet goes to the printer, and the record never changed. What
// changed on 2026-08-21 is what reaches PAPER: James ruled that people away
// at an uncontrolled destination are tracked by this post but are not this
// operation's responsibility, so the print CSS hides this section and a
// count-only scope line prints instead. That paper-side claim is asserted
// where it can actually fail — against the resolved print cascade, in
// muster_print_test.go and layout_test.go — not by a substring scan here,
// which cannot tell rendered-and-hidden from printed.
func TestMusterPrintPageStillRendersAwayAtPadSectionForTheScreen(t *testing.T) {
	srv, w := newDestinationsTestServer(t)
	sendPatToThePad(t, w)

	// The direct one-click print path — the page the officer reads before
	// the sheet is carried outside.
	page := getPage(t, srv, "/muster/print")
	for _, want := range []string{
		"Away at uncontrolled destinations",
		"Conneco Pad",
		"Pat Passenger",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the /muster/print page is missing %q — the ruling scoped the paper, not the record", want)
		}
	}
	if got := strings.Count(page, "Pat Passenger"); got != 1 {
		t.Fatalf("Pat Passenger appears %d times on the print page, want exactly 1", got)
	}
	// ...and the paper's stand-in for that list is on the page too, or the
	// sheet would drop the away people with nothing said about them.
	if !strings.Contains(page, `data-muster-section="away-scope"`) {
		t.Fatalf("no away-scope block on the print page — the paper would omit the pad crew silently:\n%s", page)
	}
}

// Every CONFIGURED uncontrolled destination reports even when empty — the
// sheet says what it knows, including that it knows of nobody (F1's lesson,
// carried over).
func TestMusterShowsConfiguredPadEvenWhenEmpty(t *testing.T) {
	srv, _ := newDestinationsTestServer(t)
	page := getPage(t, srv, "/muster")
	if !strings.Contains(page, "Conneco Pad") {
		t.Fatal("configured pad missing from the muster when empty")
	}
	if !strings.Contains(page, "Nobody recorded away at this destination.") {
		t.Fatal("empty pad row does not affirmatively say nobody is recorded away")
	}
}

// --- the pad list belongs to the post that dispatched the run -----------
//
// James, 2026-08-21: "on the pad muster roster it should not display the
// persons going to the other site — the only one that would have any
// concern about them is the ice road site control."
//
// Trips REPLICATE, so before this both posts rendered both posts' pad runs
// and the Pad's muster named people who had left Ice Road for Conoco's pad.
// Meaningless there: they never came to the Pad and never will on that run.
// The scoping is done in the domain (domain.OutAtUncontrolledFrom) so it is
// a property of the answer; what this file pins is that the MUSTER — both
// the on-screen section and the print scope line — actually renders the
// scoped answer at each post.

// peerServer builds a second post's Server over the SAME store. That is
// precisely the post-replication state: one event log, two sites reading it.
func peerServer(t *testing.T, srv *Server, siteID string) *Server {
	t.Helper()
	cfg := *srv.Cfg // copy: the peer differs only in identity
	cfg.SiteID = siteID
	cfg.DisplayName = "Peer " + siteID
	return New(&cfg, srv.Store)
}

func TestMusterAwaySectionIsScopedToTheDispatchingPost(t *testing.T) {
	srv, w := newDestinationsTestServer(t) // this post is site-b
	sendPatToThePad(t, w)                  // site-b dispatches Pat to Conneco Pad
	other := peerServer(t, srv, "site-a")  // the post that had nothing to do with it

	for _, route := range []string{"/muster", "/muster/print"} {
		// The dispatching post names them — the presence half, which is
		// what makes the absence half below mean "scoped" and not "empty".
		mine := getPage(t, srv, route)
		if !strings.Contains(mine, "Pat Passenger") {
			t.Fatalf("%s at the DISPATCHING post does not name the crew it sent to the pad:\n%s", route, mine)
		}
		if !strings.Contains(mine, `data-tally-count="away">1<`) {
			t.Fatalf("%s at the dispatching post does not count its own pad crew:\n%s", route, mine)
		}

		// The other post does not, on either surface.
		theirs := getPage(t, other, route)
		if strings.Contains(theirs, "Pat Passenger") {
			t.Errorf("%s at the OTHER post names a person who left the peer for a pad. They never came here and never will on that run — the reader of this sheet has no relationship to them:\n%s", route, theirs)
		}
		if !strings.Contains(theirs, `data-tally-count="away">0<`) {
			t.Errorf("%s at the other post still counts the peer's pad crew in its away headcount:\n%s", route, theirs)
		}
	}

	// Precondition, asserted rather than assumed: the ONE shared store
	// really does hold the run, so the absences above mean "scoped out",
	// not "never replicated". Read straight from the unscoped predicate,
	// which is the answer the filter narrows.
	all, err := domain.OutAtUncontrolled(srv.Store)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range all {
		for _, p := range o.Persons {
			if p.Name == "Pat Passenger" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("precondition: the shared replica holds no pad run for Pat at all, so nothing in this test is being scoped: %+v", all)
	}

	// The PRINT scope line — the count-only stand-in that reaches paper —
	// is scoped by the same answer, or the paper would disclaim
	// responsibility for a crew this post never dispatched.
	printed := getPage(t, other, "/muster/print")
	if strings.Contains(printed, ">1 departed") {
		t.Errorf("the other post's printed scope line claims a departure it never recorded:\n%s", printed)
	}
	if !strings.Contains(printed, "Nobody is currently recorded as away") {
		t.Errorf("the other post's printed scope line does not affirmatively state it has nobody out at a pad:\n%s", printed)
	}
	if mineP := getPage(t, srv, "/muster/print"); !strings.Contains(mineP, ">1 departed") {
		t.Errorf("the dispatching post's printed scope line lost its own count:\n%s", mineP)
	}
}

// The scoping removes other posts' PEOPLE, never this post's CATEGORIES. A
// post that configures an uncontrolled destination still says, on its own
// sheet, that it knows of nobody out there — the F1 lesson, which an
// over-broad filter (dropping the section when it is empty) would undo.
func TestScopingKeepsTheEmptyPadRowAtTheOtherPost(t *testing.T) {
	srv, w := newDestinationsTestServer(t)
	sendPatToThePad(t, w)
	other := peerServer(t, srv, "site-a")

	page := getPage(t, other, "/muster")
	if !strings.Contains(page, "Conneco Pad") {
		t.Fatalf("the other post lost its configured uncontrolled destination entirely — scoping must drop the peer's PEOPLE, not this post's categories:\n%s", page)
	}
	if !strings.Contains(page, "Nobody recorded away at this destination.") {
		t.Fatal("the other post's pad row does not affirmatively say it knows of nobody")
	}
}

// F1 and the on-site roster are unaffected at BOTH posts. The away scoping
// must not become a general "hide the peer's data" rule: the incomplete
// warning is about whoever is holding the sheet, and it is a D-6
// accountability instrument.
func TestAwayScopingLeavesF1AndTheRosterAloneAtBothPosts(t *testing.T) {
	srv, w := newDestinationsTestServer(t)
	sendPatToThePad(t, w)
	late, err := domain.CreatePerson(w.st, "site-b", 1, "Lee Late", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	crashCheckIn(t, w.st, "site-b", "TRUCK 9", late.PersonID) // row 31's crash window
	// Somebody on site at the OTHER post too, in the same shared store —
	// otherwise "the other post's roster is fine" is true of an empty one.
	ida, err := domain.CreatePerson(w.st, "site-a", 1, "Ida Iceroad", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn(t, w.st, "site-a", ida.PersonID, nil)
	other := peerServer(t, srv, "site-a")

	// F1 is ORIGIN-scoped already (domain.IncompleteCheckIns) and stays
	// that way: site-b's unfinished check-in shows at site-b, not site-a.
	// That is the precedent this change followed, asserted as still true.
	if page := getPage(t, srv, "/muster/print"); !strings.Contains(page, `class="incomplete-warn"`) {
		t.Fatalf("the F1 warning vanished from the post whose check-in is unfinished:\n%s", page)
	}
	if page := getPage(t, other, "/muster/print"); strings.Contains(page, `class="incomplete-warn"`) {
		t.Errorf("the peer's unfinished check-in raised an F1 warning at a post that did not author it")
	}
	// And each post's on-site roster is untouched. The roster was ALREADY
	// scoped by open visits at this site — that is not what changed — so
	// the assertion is that each post still shows its OWN people: an
	// over-broad away filter that reached the roster would empty one.
	for _, tc := range []struct {
		name, want string
		srv        *Server
	}{
		{"dispatching post", "Dana Driver", srv},
		{"other post", "Ida Iceroad", other},
	} {
		page := getPage(t, tc.srv, "/muster/print")
		if !strings.Contains(page, tc.want) {
			t.Errorf("%s: the on-site roster lost %q:\n%s", tc.name, tc.want, page)
		}
	}
}
