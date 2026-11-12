// h8b_entry_on_completion_report_test.go — ⟦H-8b / H-8e, L11 round 2,
// 2026-09-30⟧ the visit report (and BuildAnalytics, which reuses it) must
// include the visit ENTRY-ON-COMPLETION opens. MEASURED before the fix, on
// build 9a2fb30 (pre-A3) and on 6a3a0fa alike: a person held out at the
// gate for lack of the site's orientation and then entered by completing it
// stood on the muster and on /gate, while /analytics' persons tile, the
// site card and `sitectl -report visits` were each short by exactly that
// visit at every post — the report's replay mirrored trip_arrival's visits[]
// and never orientation_completed's carried visit (visit_report.go).
//
// The fixture is the real d98 world (BUS 7, A → B): every event goes through
// the production doors, so the completion that carries the entry is the
// one buildOrientationCompletedPayload authors, and the replay is exercised
// over the same log shape a post writes. PRECONDITIONS are asserted, not
// assumed (BUILDER_BRIEF_RULES rule 6): the completion must carry a visit,
// and B's visit table must show Diaz open — otherwise the report has
// nothing to miss and the test would defend nothing.
package domain

import (
	"encoding/json"
	"testing"
	"time"
)

// h8bEntryWorld drives the held-out arrival and the completing entry, and
// returns the completion's own payload and B's open visit for Diaz.
func h8bEntryWorld(t *testing.T) (w *d98World, trip Trip, diazID string, payload OrientationCompleted, diazVisit Visit) {
	t.Helper()
	w = newD98World(t)
	w.syncBFromA()
	trip = w.dispatchBus7(t)
	// Adams, Baker, Clark and the driver hold B's orientation before the bus
	// arrives; Diaz and Evans do not, so the arrival holds them out.
	for _, id := range append(w.ids(0, 1, 2), w.driver.PersonID) {
		if _, err := CompleteOrientation(w.b, "site-b", 1, id, nil, 0, "default"); err != nil {
			t.Fatalf("CompleteOrientation(%s): %v", id, err)
		}
	}
	if _, err := RecordExpectedArrival(w.b, "site-b", 1, trip.TripID, nil, 0, "default", true); err != nil {
		t.Fatalf("RecordExpectedArrival: %v", err)
	}
	diazID = w.ids(3)[0]
	env, err := CompleteOrientation(w.b, "site-b", 1, diazID, nil, 0, "default")
	if err != nil {
		t.Fatalf("CompleteOrientation(Diaz): %v", err)
	}
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("PRECONDITION: decoding Diaz's completion payload: %v", err)
	}
	if payload.Visit == nil || payload.TripRef == nil {
		t.Fatalf("PRECONDITION: Diaz's completion carries no entry (visit=%v trip_ref=%v) — the scenario is not posed", payload.Visit, payload.TripRef)
	}
	open, err := OpenVisitsAt(w.b, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, v := range open {
		if v.PersonID == diazID {
			diazVisit = v
			found++
		}
	}
	if found != 1 {
		t.Fatalf("PRECONDITION: want exactly one open visit for Diaz at site-b in the visit table (the muster's source), got %d", found)
	}
	if diazVisit.VisitID != payload.Visit.VisitID {
		t.Fatalf("PRECONDITION: the visit table's open visit %s is not the one the completion minted (%s)", diazVisit.VisitID, payload.Visit.VisitID)
	}
	return w, trip, diazID, payload, diazVisit
}

// TestVisitReportIncludesTheVisitEntryOnCompletionOpened — the H-8b witness:
// the report row for Diaz's entry exists, is open, is keyed by the
// completion envelope, and carries the held-out arrival's trip; and
// BuildAnalytics counts Diaz on site at B and the visit in B's season
// total. FAILS before visit_report.go's orientation_completed branch (no row
// for the visit id; site-b open 4, not 5), PASSES after.
func TestVisitReportIncludesTheVisitEntryOnCompletionOpened(t *testing.T) {
	w, trip, diazID, payload, diazVisit := h8bEntryWorld(t)

	recs, sum, err := VisitReport(w.b, nil)
	if err != nil {
		t.Fatal(err)
	}
	by := vrByVisitID(recs)
	rec, ok := by[diazVisit.VisitID]
	if !ok {
		t.Fatalf("the visit report has NO row for %s, the visit entry-on-completion opened for Diaz — the muster shows them on site and the report does not (H-8b); rows: %d", diazVisit.VisitID, len(recs))
	}
	if rec.SiteID != "site-b" || rec.PersonID != diazID || rec.VisitPersonID != diazID {
		t.Errorf("row site/person = %s/%s (raw %s), want site-b/%s", rec.SiteID, rec.PersonID, rec.VisitPersonID, diazID)
	}
	if rec.ClosedBy != VisitStillOpen {
		t.Errorf("Diaz's entry reads %q, want open", rec.ClosedBy)
	}
	if rec.TripRef != trip.TripID || *payload.TripRef != trip.TripID {
		t.Errorf("row trip_ref %q / payload trip_ref %q, want the dispatched trip %q", rec.TripRef, *payload.TripRef, trip.TripID)
	}
	if rec.ArrivalKey.SiteID != "site-b" || rec.ArrivalKey.NodeEpoch != diazVisit.NodeEpoch || rec.ArrivalKey.Seq != diazVisit.Seq {
		t.Errorf("row arrival key %v, want the completion's own key (site-b, %d, %d) — the same instant the roster shows", rec.ArrivalKey, diazVisit.NodeEpoch, diazVisit.Seq)
	}
	if rec.ArrivalLabel == timeUnresolvable || rec.ArrivedAtMS == 0 {
		t.Errorf("row arrival instant unresolved (%q, %d) — the completion envelope carries one", rec.ArrivalLabel, rec.ArrivedAtMS)
	}

	// The whole open set at B agrees with the visit table, row for row.
	open, err := OpenVisitsAt(w.b, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	openB := 0
	for _, r := range recs {
		if r.SiteID == "site-b" && r.ClosedBy == VisitStillOpen {
			openB++
			if _, inTable := vrOpenIDs(open)[r.VisitID]; !inTable {
				t.Errorf("report says %s is open at site-b; the visit table does not", r.VisitID)
			}
		}
	}
	if openB != len(open) {
		t.Errorf("open visits at site-b: report %d, visit table %d", openB, len(open))
	}
	if sum.Open < len(open) {
		t.Errorf("summary open %d < site-b's own %d", sum.Open, len(open))
	}

	// Analytics, which reuses the report: Diaz is on site at B and the
	// visit is in B's season count.
	fig, err := BuildAnalytics(w.b, time.Now(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	var b *SiteFigures
	for i := range fig.Sites {
		if fig.Sites[i].SiteID == "site-b" {
			b = &fig.Sites[i]
		}
	}
	if b == nil {
		t.Fatal("analytics has no site-b figures")
	}
	if b.OnSiteNow != len(open) {
		t.Errorf("analytics site-b OnSiteNow %d, want %d (the visit table's open set, Diaz included)", b.OnSiteNow, len(open))
	}
	if !fig.openBySite["site-b"][diazID] {
		t.Errorf("analytics does not count Diaz (%s) on site at site-b", diazID)
	}
	if b.VisitsSeason != vrSiteRows(recs, "site-b") || b.VisitsSeason < 5 {
		t.Errorf("analytics site-b VisitsSeason %d, want %d (>= 5: four entered on arrival + Diaz on completion)", b.VisitsSeason, vrSiteRows(recs, "site-b"))
	}
	if got := fig.DistinctOnSite(map[string]bool{"site-b": true}); got != len(open) {
		t.Errorf("DistinctOnSite(site-b) %d, want %d", got, len(open))
	}
}

// TestVisitReportEntryOnCompletionIsIdenticalAtAReplicatingPeer — "at all 5
// posts": the report derives from the log, so a peer C that replicated A's
// and B's streams and never saw either door reads the same row.
func TestVisitReportEntryOnCompletionIsIdenticalAtAReplicatingPeer(t *testing.T) {
	w, _, diazID, _, diazVisit := h8bEntryWorld(t)
	c := newDomainTestStore(t)
	replicateStream(t, c, w.a, "site-a")
	replicateStream(t, c, w.b, "site-b")

	bRecs, bSum, err := VisitReport(w.b, nil)
	if err != nil {
		t.Fatal(err)
	}
	cRecs, cSum, err := VisitReport(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bSum.Visits != cSum.Visits || bSum.Open != cSum.Open {
		t.Errorf("B (visits %d, open %d) and C (visits %d, open %d) disagree", bSum.Visits, bSum.Open, cSum.Visits, cSum.Open)
	}
	cRec, ok := vrByVisitID(cRecs)[diazVisit.VisitID]
	if !ok {
		t.Fatalf("peer C's report has no row for Diaz's entry %s", diazVisit.VisitID)
	}
	bRec := vrByVisitID(bRecs)[diazVisit.VisitID]
	if cRec.PersonID != diazID || cRec.ClosedBy != VisitStillOpen || cRec.ArrivalKey != bRec.ArrivalKey || cRec.TripRef != bRec.TripRef {
		t.Errorf("peer C's row %+v differs from B's %+v", cRec, bRec)
	}
}

// TestVisitReportCountsEachEntryOnce — the no-double-count control for the
// fix: a person whose orientation was completed BEFORE the arrival (a bare
// completion, no carried visit) gets exactly one row, from the arrival; the
// held-out person gets exactly one, from the completion; and an ordinary
// same-site gate check-in elsewhere gets exactly one. Would fail if the new
// branch minted a row for every completion, or the arrival branch and the
// completion branch both produced one for the same person.
func TestVisitReportCountsEachEntryOnce(t *testing.T) {
	w, _, diazID, _, _ := h8bEntryWorld(t)

	// An ordinary check-in at A for Evans (never entered at B): one row.
	evansID := w.ids(4)[0]
	vrCheckIn(t, w.a, "site-a", "TRUCK-9", evansID)

	// Replicate A's new events to B so B's report sees the whole world.
	w.syncBFromA()

	recs, sum, err := VisitReport(w.b, nil)
	if err != nil {
		t.Fatal(err)
	}
	perPersonSite := map[[2]string]int{}
	for _, r := range recs {
		perPersonSite[[2]string{r.PersonID, r.SiteID}]++
	}
	for _, id := range append(w.ids(0, 1, 2), w.driver.PersonID) {
		if n := perPersonSite[[2]string{id, "site-b"}]; n != 1 {
			t.Errorf("oriented-before-arrival %s has %d rows at site-b, want exactly 1 (the arrival's; the bare completion mints none)", id, n)
		}
	}
	if n := perPersonSite[[2]string{diazID, "site-b"}]; n != 1 {
		t.Errorf("entered-on-completion Diaz has %d rows at site-b, want exactly 1", n)
	}
	if n := perPersonSite[[2]string{evansID, "site-a"}]; n != 1 {
		t.Errorf("gate check-in Evans has %d rows at site-a, want exactly 1", n)
	}
	if n := perPersonSite[[2]string{evansID, "site-b"}]; n != 0 {
		t.Errorf("held-out, never-completed Evans has %d rows at site-b, want 0", n)
	}
	// Every row's visit id is unique — a double count would show here too.
	if len(vrByVisitID(recs)) != len(recs) || sum.Visits != len(recs) {
		t.Errorf("%d rows but %d distinct visit ids (summary %d)", len(recs), len(vrByVisitID(recs)), sum.Visits)
	}
}

func vrOpenIDs(vs []Visit) map[string]bool {
	m := map[string]bool{}
	for _, v := range vs {
		m[v.VisitID] = true
	}
	return m
}

func vrSiteRows(recs []VisitRecord, site string) int {
	n := 0
	for _, r := range recs {
		if r.SiteID == site {
			n++
		}
	}
	return n
}
