// w6fix_presence_test.go — W6 (James, 2026-10-03): two read-time derivations
// over the replicated visit rows, pinned at the domain.
//
//   - AlsoOnSiteElsewhere (M-6 / T-L08-C): one person, two open visits at two
//     posts. MEASURED on L08/L13: both posts' dumps hold both rows and neither
//     sheet said so. The flag must be SYMMETRIC and DETERMINISTIC — each post,
//     from the same rows, names the other and the other's sign-in time.
//   - ClosedVisits (M-5 / M-19): for a visit a reprint link froze that has
//     since closed, HOW and WHEN — mirrored from visit_open.go's closers.
//
// One store holds both sites' events: that is the fully-replicated state, so
// whatever each "post" derives here is what it derives after sync.
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// w6fixCheckIn opens a visit for person at site through the gate's own
// degenerate-trip pattern, at the store's current clock reading.
func w6fixCheckIn(t *testing.T, st *store.Store, site, vehicle, person string) Visit {
	t.Helper()
	trip, err := DispatchLocalCheckIn(st, site, 1, DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, site, vehicle), DriverPersonID: person,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordExpectedArrival(st, site, 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	return w6fixOpenVisitOf(t, st, site, person)
}

// w6fixOpenVisitOf is the one open visit of person at site — the test's own
// precondition check (rule 6): exactly one, or the scenario is not posed.
func w6fixOpenVisitOf(t *testing.T, st *store.Store, site, person string) Visit {
	t.Helper()
	open, err := OpenVisitsAt(st, site)
	if err != nil {
		t.Fatal(err)
	}
	var found []Visit
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

func TestAlsoOnSiteElsewhereNamesTheOtherPostAndItsSignInTimeSymmetricallyAtBothPosts(t *testing.T) {
	st := newDomainTestStore(t)
	p, q := seedTwoPersons(t, st, "site-a") // p: the double-recorded person; q: the control, on site at site-a only

	st.Clock = w3Reading(w3Base) // 1,800,000,000,000 — the site-a sign-ins
	va := w6fixCheckIn(t, st, "site-a", "TRK-1", p.PersonID)
	vq := w6fixCheckIn(t, st, "site-a", "TRK-2", q.PersonID)

	// T-L08-C: p turns up at site-b unannounced while site-a still holds p
	// on site. D-87: site-b's fold closes nothing of site-a's.
	st.Clock = w3Reading(w3Base + 2*w3Hour)
	if _, err := RecordUnsolicitedArrival(st, "site-b", 1, w7SeedVehicleID(t, st, "site-b", "WALK-1"), "site-a",
		[]string{p.PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	vb := w6fixOpenVisitOf(t, st, "site-b", p.PersonID)
	// Rule 6: the scenario is posed only if site-a's row is STILL open —
	// otherwise this test proves nothing about a double record.
	if again := w6fixOpenVisitOf(t, st, "site-a", p.PersonID); again.VisitID != va.VisitID {
		t.Fatalf("precondition: site-a's open visit for p changed (%s -> %s)", va.VisitID, again.VisitID)
	}

	openA, _ := OpenVisitsAt(st, "site-a")
	openB, _ := OpenVisitsAt(st, "site-b")
	atA, err := AlsoOnSiteElsewhere(st, "site-a", openA)
	if err != nil {
		t.Fatal(err)
	}
	atB, err := AlsoOnSiteElsewhere(st, "site-b", openB)
	if err != nil {
		t.Fatal(err)
	}

	// Site-a: p's row points at site-b's visit and its sign-in time; q's row
	// carries nothing.
	got := atA[va.VisitID]
	if len(got) != 1 || got[0].SiteID != "site-b" || got[0].VisitID != vb.VisitID {
		t.Fatalf("site-a: p's twin = %+v, want exactly site-b/%s", got, vb.VisitID)
	}
	if !got[0].SinceOK || got[0].SinceMS != w3Base+2*w3Hour {
		t.Errorf("site-a: p's twin since = (%d, ok=%v), want site-b's arrival instant %d — the time shown is the PEER visit's sign-in", got[0].SinceMS, got[0].SinceOK, w3Base+2*w3Hour)
	}
	if _, flagged := atA[vq.VisitID]; flagged {
		t.Errorf("site-a: the control q (on site at site-a only) is flagged: %+v", atA[vq.VisitID])
	}
	// Site-b: the mirror image, from the same rows.
	got = atB[vb.VisitID]
	if len(got) != 1 || got[0].SiteID != "site-a" || got[0].VisitID != va.VisitID {
		t.Fatalf("site-b: p's twin = %+v, want exactly site-a/%s", got, va.VisitID)
	}
	if !got[0].SinceOK || got[0].SinceMS != w3Base {
		t.Errorf("site-b: p's twin since = (%d, ok=%v), want site-a's sign-in instant %d", got[0].SinceMS, got[0].SinceOK, w3Base)
	}
	if len(atB) != 1 {
		t.Errorf("site-b flags %d visit(s), want 1 (only p is double-recorded)", len(atB))
	}
	// Deterministic: a second read is identical (no clock, no map-order luck).
	again, _ := AlsoOnSiteElsewhere(st, "site-a", openA)
	if len(again) != len(atA) || again[va.VisitID][0] != atA[va.VisitID][0] {
		t.Errorf("a second read differs: %+v vs %+v", again, atA)
	}
}

// Nobody double-recorded: the map is empty, and the function does not invent
// a twin out of a CLOSED visit at the peer.
func TestAlsoOnSiteElsewhereIgnoresClosedVisitsAtThePeer(t *testing.T) {
	st := newDomainTestStore(t)
	p, _ := seedTwoPersons(t, st, "site-a")
	st.Clock = w3Reading(w3Base)
	vb := w6fixCheckIn(t, st, "site-b", "TRK-9", p.PersonID)
	if err := CheckOutVisit(st, "site-b", 1, vb.VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	st.Clock = w3Reading(w3Base + w3Hour)
	va := w6fixCheckIn(t, st, "site-a", "TRK-1", p.PersonID)
	openA, _ := OpenVisitsAt(st, "site-a")
	atA, err := AlsoOnSiteElsewhere(st, "site-a", openA)
	if err != nil {
		t.Fatal(err)
	}
	if len(atA) != 0 {
		t.Fatalf("p's closed site-b visit was read as a twin of %s: %+v — only OPEN visits elsewhere are a double record", va.VisitID, atA)
	}
}

func TestClosedVisitsNamesHowAndWhenEachFrozenVisitClosed(t *testing.T) {
	st := newDomainTestStore(t)
	co, err := CreateCompany(st, "site-a", 1, "Kakivik", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	mk := func(name string) Person {
		p, err := CreatePerson(st, "site-a", 1, name, &co.CompanyID, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	out, away, back, stays, twice := mk("Out, Olly"), mk("Away, Ava"), mk("Back, Bea"), mk("Stays, Sam"), mk("Twice, Tam")

	st.Clock = w3Reading(w3Base) // everyone signs in at the base instant
	vOut := w6fixCheckIn(t, st, "site-a", "TRK-1", out.PersonID)
	vAway := w6fixCheckIn(t, st, "site-a", "TRK-2", away.PersonID)
	vBack := w6fixCheckIn(t, st, "site-a", "TRK-3", back.PersonID)
	vStays := w6fixCheckIn(t, st, "site-a", "TRK-4", stays.PersonID)
	vTwice := w6fixCheckIn(t, st, "site-a", "TRK-5", twice.PersonID)
	frozen := []Visit{vOut, vAway, vBack, vStays, vTwice} // "the sheet"

	// 1. Olly checks out at +1h.
	st.Clock = w3Reading(w3Base + 1*w3Hour)
	if err := CheckOutVisit(st, "site-a", 1, vOut.VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// 2. Ava is dispatched to site-b on TRK-7 at +2h.
	st.Clock = w3Reading(w3Base + 2*w3Hour)
	dest, dl := "site-b", int64(60)
	if _, err := Dispatch(st, "site-a", 1, DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-7"), DriverPersonID: away.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &dl,
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// 3. Bea walks back through the gate at +3h with no sign-out recorded:
	//    the destination-less dispatch half closes the sheet's line.
	st.Clock = w3Reading(w3Base + 3*w3Hour)
	w6fixCheckIn(t, st, "site-a", "TRK-8", back.PersonID)
	// 4. Tam is recorded arriving at site-a again at +4h (an unsolicited
	//    arrival from site-b) with no sign-out: the next arrival closes it.
	st.Clock = w3Reading(w3Base + 4*w3Hour)
	if _, err := RecordUnsolicitedArrival(st, "site-a", 1, w7SeedVehicleID(t, st, "site-a", "WALK-2"), "site-b",
		[]string{twice.PersonID}, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}

	// Rule 6: the sheet's rows really did close (and Sam's did not).
	for _, v := range frozen {
		row, err := VisitByID(st, v.VisitID)
		if err != nil || row == nil {
			t.Fatalf("visit %s: %v", v.VisitID, err)
		}
		if (row.IsOpen == 1) != (v.VisitID == vStays.VisitID) {
			t.Fatalf("precondition: visit %s (%s) is_open=%d — the fixture does not pose its case", v.VisitID, v.PersonID, row.IsOpen)
		}
	}
	// ClosedVisits reads the rows AS THEY STAND (the reprint re-reads by id).
	var current []Visit
	for _, v := range frozen {
		row, _ := VisitByID(st, v.VisitID)
		current = append(current, *row)
	}
	closes, err := ClosedVisits(st, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(closes) != 4 {
		t.Fatalf("got %d closed visit(s), want 4 (Sam is still on site):\n%+v", len(closes), closes)
	}
	want := []struct {
		visit   string
		how     VisitClosure
		instant int64
		vehicle string
		dest    string
	}{
		{vOut.VisitID, ClosedByCheckout, w3Base + 1*w3Hour, "", ""},
		{vAway.VisitID, ClosedByDispatch, w3Base + 2*w3Hour, "TRK-7", "site-b"},
		{vBack.VisitID, ClosedByReCheckin, w3Base + 3*w3Hour, "TRK-8", ""},
		{vTwice.VisitID, ClosedByNextArrival, w3Base + 4*w3Hour, "", ""},
	}
	for i, w := range want {
		c := closes[i]
		if c.Visit.VisitID != w.visit {
			t.Errorf("[%d] visit %s, want %s (input order must be kept)", i, c.Visit.VisitID, w.visit)
			continue
		}
		if c.How != w.how {
			t.Errorf("[%d] %s closed by %q, want %q", i, w.visit, c.How, w.how)
		}
		if !c.InstantOK || c.InstantMS != w.instant {
			t.Errorf("[%d] %s instant = (%d, ok=%v), want %d — the closing event's own recorded instant", i, w.visit, c.InstantMS, c.InstantOK, w.instant)
		}
		if c.Vehicle != w.vehicle || c.Destination != w.dest {
			t.Errorf("[%d] %s vehicle/destination = %q/%q, want %q/%q", i, w.visit, c.Vehicle, c.Destination, w.vehicle, w.dest)
		}
	}
}

// A check-out's instant is found by walking the visit's own site's streams
// from the visit forward — so a check-out recorded on a LATER EPOCH of the
// same site (a re-imaged post) is still found, and a foreign site's events
// are never read for it.
func TestClosedVisitsFindsACheckOutRecordedOnALaterEpoch(t *testing.T) {
	st := newDomainTestStore(t)
	p, _ := seedTwoPersons(t, st, "site-a")
	st.Clock = w3Reading(w3Base)
	v := w6fixCheckIn(t, st, "site-a", "TRK-1", p.PersonID)
	st.Clock = w3Reading(w3Base + w3Hour)
	if err := CheckOutVisit(st, "site-a", 2, v.VisitID, nil, 0, "default"); err != nil { // epoch 2: the post was re-imaged in between
		t.Fatal(err)
	}
	row, _ := VisitByID(st, v.VisitID)
	if row.IsOpen != 0 {
		t.Fatal("precondition: the epoch-2 check-out did not close the epoch-1 visit")
	}
	closes, err := ClosedVisits(st, []Visit{*row})
	if err != nil {
		t.Fatal(err)
	}
	if len(closes) != 1 || closes[0].How != ClosedByCheckout || !closes[0].InstantOK || closes[0].InstantMS != w3Base+w3Hour {
		t.Fatalf("got %+v, want checked_out at %d found on epoch 2", closes, w3Base+w3Hour)
	}
}
