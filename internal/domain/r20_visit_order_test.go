// r20_visit_order_test.go — R-20 (James, 2026-09-13: "Make the presence
// folds order-independent"). The domain-level witnesses for
// visit_open.go, and the harness that could not exist before it.
//
// THE DEFECT THESE GUARD (reports/witness/D87-D92-COHERENCE.md, F-3, measured
// through the real repl.Engine; re-measured RED on the unchanged tree at
// 999aba0 before this file's fix landed): a site re-imaged mid-shift owns
// rows on TWO streams — (site-b, epoch 1), held only by its peer, and
// (site-b, epoch 2), which it authors from boot. Boot Rejoin fails (link
// down), the post authors on epoch 2, and the §8.6 self-history retry later
// lands epoch 1 AFTER epoch 2 was folded. The visit folds were predicate
// UPDATEs, so the epoch-1 dispatch closed the epoch-2 visit: the AUTHOR
// showed Pat OFF site while its peer and a from-zero replay showed Pat ON
// site. Identical raw logs, different muster, under-report direction.
//
// ⚠ WHY NO EXISTING HARNESS COULD POSE IT. Both permutation harnesses in the
// tree (store's C2 scramble, domain's D-92 soups) preserve each SITE's
// total order — they interleave streams of DIFFERENT sites. The failing
// shape is one site's EARLIER epoch delivered after its LATER one, which is
// unreachable by construction there: a test suite that cannot generate the
// defect reports green forever. The harness below permutes STREAMS, not
// sites, and asserts of itself that it posed the reversed pair — a harness
// that stopped posing it would fail its own self-check rather than pass
// vacuously.
//
// Every store here is a THIRD POST in the replication sense: it never
// authored anything, it replays raw envelopes through store.Apply exactly
// as repl's pullStream hands a page to the store, in whatever order the
// permutation says. Convergence across all of them IS the replication
// convergence proof for the visit projection after the SchemaVersion 15
// bump.
package domain

import (
	"bytes"
	"fmt"
	"testing"

	"sitecontrol/internal/store"
)

// r20Stream is one (site, epoch) stream, held in the store that authored it.
type r20Stream struct {
	site  string
	epoch int64
	src   *store.Store
}

func (s r20Stream) String() string { return fmt.Sprintf("%s/%d", s.site, s.epoch) }

// r20Replay applies one stream from src into dst in seq order — store.Apply
// is exactly what repl's pullStream hands a page to.
func r20Replay(t *testing.T, dst, src *store.Store, site string, epoch int64) int {
	t.Helper()
	evs, err := src.Events(site, epoch, 1, 0)
	if err != nil {
		t.Fatalf("r20Replay(%s,%d): %v", site, epoch, err)
	}
	for _, ev := range evs {
		if _, err := dst.Apply(ev); err != nil {
			t.Fatalf("r20Replay(%s,%d): apply %v: %v", site, epoch, ev.Key(), err)
		}
	}
	return len(evs)
}

func r20OpenFor(t *testing.T, st *store.Store, site, personID string) []Visit {
	t.Helper()
	open, err := OpenVisitsAt(st, site)
	if err != nil {
		t.Fatal(err)
	}
	var out []Visit
	for _, v := range open {
		if v.PersonID == personID {
			out = append(out, v)
		}
	}
	return out
}

func r20i64(v int64) *int64 { return &v }

// r20SixOrders is every permutation of three streams. Hardcoded rather than
// generated so a reader can see which orders reverse a site's own epochs.
var r20SixOrders = [][3]int{
	{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0},
}

// r20Reversed reports whether `order` delivers stream j before stream i —
// the shape the folds could not survive.
func r20Reversed(order [3]int, i, j int) bool {
	pos := map[int]int{}
	for k, idx := range order {
		pos[idx] = k
	}
	return pos[j] < pos[i]
}

// r20AssertInvariantUnderEveryOrder replays the three streams into a fresh
// store in each of the six orders and requires the `visit` projection dump
// — the WHOLE table, is_open included — to be byte-identical to the
// (0,1,2) canonical replay, plus `check` on each store. It fails its OWN
// premise if no order reversed streams 1 and 2 (the site's two epochs).
func r20AssertInvariantUnderEveryOrder(t *testing.T, streams [3]r20Stream, check func(t *testing.T, label string, st *store.Store)) {
	t.Helper()
	var canonical []byte
	posedReversed := 0
	for _, order := range r20SixOrders {
		st := newDomainTestStore(t)
		label := ""
		for _, idx := range order {
			s := streams[idx]
			r20Replay(t, st, s.src, s.site, s.epoch)
			label += s.String() + " "
		}
		if r20Reversed(order, 1, 2) {
			posedReversed++
			label += "(site's own epochs REVERSED)"
		}
		dump, err := st.DumpProjection("visit")
		if err != nil {
			t.Fatal(err)
		}
		if canonical == nil {
			canonical = dump
		} else if !bytes.Equal(dump, canonical) {
			t.Errorf("DIVERGENCE under delivery order %s: the visit projection differs from the canonical epoch-major replay.\n  got:  %s\n  want: %s", label, dump, canonical)
		}
		check(t, label, st)
	}
	if posedReversed == 0 {
		t.Fatalf("HARNESS SELF-CHECK FAILED: no order delivered %s before %s — this harness no longer poses the R-20 shape and its green would be vacuous", streams[2], streams[1])
	}
	t.Logf("six delivery orders replayed into six fresh stores; %d of them delivered %s BEFORE %s (the R-20 shape); all six agree", posedReversed, streams[2], streams[1])
}

// r20Reimage builds the measured scenario: site-a owns the roster; site-b
// epoch 1 checks Pat in and out (history that survives only at the peer);
// site-b epoch 2 — the re-imaged post, roster recovered, own history NOT —
// checks Pat in again. Returns the three streams, Pat, and the epoch-2
// visit that must be the ONE open visit at site-b whatever the order.
func r20Reimage(t *testing.T) ([3]r20Stream, Person, Visit) {
	t.Helper()
	a := newDomainTestStore(t)
	p, err := CreatePerson(a, "site-a", 1, "Pat Reorder", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	num := "T12"
	veh, err := CreateVehicle(a, "site-a", 1, &num, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	b1 := newDomainTestStore(t)
	r20Replay(t, b1, a, "site-a", 1)
	if _, _, err := GateCheckIn(b1, "site-b", 1, GateCheckInInput{
		VehicleID: veh.VehicleID, Driver: PersonSlot{ExistingID: p.PersonID},
	}, 0, "default"); err != nil {
		t.Fatalf("epoch-1 check-in: %v", err)
	}
	v7 := r20OpenFor(t, b1, "site-b", p.PersonID)
	if len(v7) != 1 {
		t.Fatalf("fixture: epoch-1 open visits for Pat = %d, want 1", len(v7))
	}
	if err := CheckOutVisit(b1, "site-b", 1, v7[0].VisitID, nil, 0, "default"); err != nil {
		t.Fatalf("epoch-1 check-out: %v", err)
	}
	b2 := newDomainTestStore(t)
	r20Replay(t, b2, a, "site-a", 1)
	if _, _, err := GateCheckIn(b2, "site-b", 2, GateCheckInInput{
		VehicleID: veh.VehicleID, Driver: PersonSlot{ExistingID: p.PersonID},
	}, 0, "default"); err != nil {
		t.Fatalf("epoch-2 check-in: %v", err)
	}
	v8 := r20OpenFor(t, b2, "site-b", p.PersonID)
	if len(v8) != 1 {
		t.Fatalf("fixture: epoch-2 open visits for Pat = %d, want 1", len(v8))
	}
	return [3]r20Stream{{"site-a", 1, a}, {"site-b", 1, b1}, {"site-b", 2, b2}}, p, v8[0]
}

// The measured case, at the AUTHOR: epoch 2 folded first, then the post's
// own epoch-1 tail. Pat — physically checked in on epoch 2 — must be ON
// site, and the author must agree with a canonical epoch-major replay.
// RED on the unchanged tree (author 0, canonical 1); GREEN with
// visit_open.go's derivation.
func TestR20LateOwnEpochAfterAuthoringLeavesTheAuthorAgreeingWithACanonicalReplay(t *testing.T) {
	streams, p, v8 := r20Reimage(t)
	a, b1, b2 := streams[0].src, streams[1].src, streams[2].src

	// The link to the peer appears; the self-history retry drains site-b's
	// OWN epoch-1 stream into the author AFTER epoch 2 was authored.
	n := r20Replay(t, b2, b1, "site-b", 1)
	t.Logf("late self-history ingest applied %d epoch-1 events after epoch 2 was authored and folded", n)

	ref := newDomainTestStore(t)
	r20Replay(t, ref, a, "site-a", 1)
	r20Replay(t, ref, b1, "site-b", 1)
	r20Replay(t, ref, b2, "site-b", 2)

	openB2 := r20OpenFor(t, b2, "site-b", p.PersonID)
	openRef := r20OpenFor(t, ref, "site-b", p.PersonID)
	t.Logf("author (late self-history order): Pat open visits at site-b = %d", len(openB2))
	t.Logf("canonical epoch-major order:      Pat open visits at site-b = %d", len(openRef))
	for _, k := range []struct {
		site  string
		epoch int64
	}{{"site-a", 1}, {"site-b", 1}, {"site-b", 2}} {
		w1, _ := b2.Watermark(k.site, k.epoch)
		w2, _ := ref.Watermark(k.site, k.epoch)
		if w1 != w2 || w1 == 0 {
			t.Fatalf("raw logs differ at (%s,%d): %d vs %d", k.site, k.epoch, w1, w2)
		}
	}
	if len(openB2) != 1 || openB2[0].VisitID != v8.VisitID {
		t.Errorf("R-20: the AUTHOR holds Pat OFF site after its own late epoch-1 history landed (open=%d) — Pat physically checked in on epoch 2 and must be on the author's own muster (the epoch-2 visit %s)", len(openB2), v8.VisitID)
	}
	if len(openRef) != 1 || openRef[0].VisitID != v8.VisitID {
		t.Errorf("canonical replay should leave exactly the epoch-2 visit open, got %+v", openRef)
	}
	if len(openB2) != len(openRef) {
		t.Errorf("DIVERGENCE: identical raw logs, different muster (author %d, canonical %d)", len(openB2), len(openRef))
	}
}

// THE HARNESS: the same scenario under EVERY stream order, including the
// three orders that deliver site-b's epoch 2 before its epoch 1. RED on the
// unchanged tree on exactly those three; GREEN with the derivation.
func TestR20VisitOpenSetIsInvariantUnderEveryStreamOrderIncludingASitesOwnEpochsReversed(t *testing.T) {
	streams, p, v8 := r20Reimage(t)
	r20AssertInvariantUnderEveryOrder(t, streams, func(t *testing.T, label string, st *store.Store) {
		open := r20OpenFor(t, st, "site-b", p.PersonID)
		if len(open) != 1 || open[0].VisitID != v8.VisitID {
			t.Errorf("order %s: Pat's open visits at site-b = %v, want exactly the epoch-2 visit %s", label, open, v8.VisitID)
		}
	})
}

// THE HARNESS over the WHOLE presence family — dispatch-time check-out, an
// explicit check-out, and supersession by a later arrival, each across the
// site's two epochs — so a regression in any one of the three closing
// rules is caught, not only the arrival/check-out pair the measured case
// happened to use.
//
//	site-a/1: roster (Pat, Quinn) + vehicle T12
//	site-b/1: Pat drives Quinn in (VP1, VQ1 open) · Pat checked OUT (VP1
//	          closed by check-out) · Quinn DISPATCHED to site-a (VQ1 closed
//	          by dispatch)
//	site-b/2: Pat drives Quinn in again (VP2, VQ2 open) · Quinn checked OUT
//
// Whatever the order: at site-b, Pat is ON site on VP2 and Quinn is OFF.
// Under the old predicate folds, delivering epoch 2 first left NOBODY on
// site — epoch 1's check-in dispatch closed VP2 on arrival.
func TestR20EveryClosingRuleIsOrderIndependentAcrossASitesOwnEpochs(t *testing.T) {
	a := newDomainTestStore(t)
	pat, err := CreatePerson(a, "site-a", 1, "Pat Reorder", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	quinn, err := CreatePerson(a, "site-a", 1, "Quinn Reorder", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	num := "T12"
	veh, err := CreateVehicle(a, "site-a", 1, &num, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn := func(st *store.Store, epoch int64) {
		t.Helper()
		if _, _, err := GateCheckIn(st, "site-b", epoch, GateCheckInInput{
			VehicleID: veh.VehicleID,
			Driver:    PersonSlot{ExistingID: pat.PersonID},
			Occupants: []PersonSlot{{ExistingID: quinn.PersonID}},
		}, 0, "default"); err != nil {
			t.Fatalf("epoch-%d check-in: %v", epoch, err)
		}
	}
	checkOut := func(st *store.Store, epoch int64, personID string) {
		t.Helper()
		open := r20OpenFor(t, st, "site-b", personID)
		if len(open) != 1 {
			t.Fatalf("fixture: epoch-%d open visits for %s = %d, want 1", epoch, personID, len(open))
		}
		if err := CheckOutVisit(st, "site-b", epoch, open[0].VisitID, nil, 0, "default"); err != nil {
			t.Fatalf("epoch-%d check-out: %v", epoch, err)
		}
	}

	b1 := newDomainTestStore(t)
	r20Replay(t, b1, a, "site-a", 1)
	checkIn(b1, 1)
	checkOut(b1, 1, pat.PersonID)
	dest := "site-a"
	if _, err := Dispatch(b1, "site-b", 1, DispatchInput{
		VehicleID: veh.VehicleID, DriverPersonID: quinn.PersonID,
		DestinationSite: &dest, DeadlineMinutes: r20i64(90),
	}, 0, "default"); err != nil {
		t.Fatalf("epoch-1 dispatch: %v", err)
	}
	if n := len(r20OpenFor(t, b1, "site-b", quinn.PersonID)); n != 0 {
		t.Fatalf("fixture: dispatch did not close Quinn's epoch-1 visit (open=%d)", n)
	}

	b2 := newDomainTestStore(t)
	r20Replay(t, b2, a, "site-a", 1)
	checkIn(b2, 2)
	checkOut(b2, 2, quinn.PersonID)
	vp2 := r20OpenFor(t, b2, "site-b", pat.PersonID)
	if len(vp2) != 1 {
		t.Fatalf("fixture: epoch-2 open visits for Pat = %d, want 1", len(vp2))
	}

	streams := [3]r20Stream{{"site-a", 1, a}, {"site-b", 1, b1}, {"site-b", 2, b2}}
	r20AssertInvariantUnderEveryOrder(t, streams, func(t *testing.T, label string, st *store.Store) {
		if open := r20OpenFor(t, st, "site-b", pat.PersonID); len(open) != 1 || open[0].VisitID != vp2[0].VisitID {
			t.Errorf("order %s: Pat's open visits at site-b = %v, want exactly the epoch-2 visit %s (supersession + check-out rules)", label, open, vp2[0].VisitID)
		}
		if open := r20OpenFor(t, st, "site-b", quinn.PersonID); len(open) != 0 {
			t.Errorf("order %s: Quinn's open visits at site-b = %v, want none (dispatch + check-out rules)", label, open)
		}
		all, err := OpenVisitsAt(st, "site-b")
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 1 {
			t.Errorf("order %s: site-b open set = %d visits, want exactly 1", label, len(all))
		}
	})
}
