// schema12_entry_replication_test.go — SchemaVersion 12 (R-16): proves entry-
// on-completion's new wire shape (OrientationCompleted's arrival_ref/
// trip_ref/visit) survives real cross-post replication, not just a local
// fold. Modelled on d98_not_entered_test.go's d98World (BUS 7, A -> B), then
// extended one hop further: a THIRD post, C, that never saw the arrival or
// the completion directly, replicates them from A's and B's own event logs
// the way a real peer would (store.Apply, one envelope at a time, in stream
// order) — the same mechanism internal/repl's engine drives over real HTTP,
// exercised here at the fold layer that actually has to make sense of the
// bytes.
//
// "Stand up scratch posts and prove it" (the bump's own instruction): these
// are three real *store.Store databases, each domain.Register-ed, never the
// live bench's or demo pair's files or ports — nothing here binds a socket
// at all, which is the point: replication is proven at the layer that would
// refuse a wire shape it could not parse, independent of transport.
package domain

import (
	"encoding/json"
	"testing"

	"sitecontrol/internal/store"
)

// replicateStream pulls every envelope siteID's own store has authored and
// applies it to dst, in order — a peer's own view of one stream, built the
// same way store.Events/store.Apply are the sanctioned primitives for it
// (d98World.syncBFromA uses the identical pair).
func replicateStream(t *testing.T, dst, src *store.Store, siteID string) {
	t.Helper()
	evs, err := src.Events(siteID, 1, 1, 0)
	if err != nil {
		t.Fatalf("replicateStream(%s): reading source events: %v", siteID, err)
	}
	if len(evs) == 0 {
		t.Fatalf("replicateStream(%s): source authored nothing — fixture defect", siteID)
	}
	for _, ev := range evs {
		if _, err := dst.Apply(ev); err != nil {
			t.Fatalf("replicateStream(%s): peer refused envelope %v: %v", siteID, ev.Key(), err)
		}
	}
}

func TestSchema12EntryOnCompletionReplicatesToAThirdPostThatNeverSawTheArrival(t *testing.T) {
	w := newD98World(t)
	w.syncBFromA() // the roster must reach B before B can look anyone up
	trip := w.dispatchBus7(t)

	// Adams, Baker, Clark and the driver hold B's orientation; Diaz and Evans
	// do not — the same shape d98_not_entered_test.go's end-to-end arm uses.
	held := append(w.ids(0, 1, 2), w.driver.PersonID)
	for _, id := range held {
		if _, err := CompleteOrientation(w.b, "site-b", 1, id, nil, 0, "default"); err != nil {
			t.Fatalf("CompleteOrientation(%s): %v", id, err)
		}
	}
	if _, err := RecordExpectedArrival(w.b, "site-b", 1, trip.TripID, nil, 0, "default", true); err != nil {
		t.Fatalf("RecordExpectedArrival: %v", err)
	}

	// Diaz's orientation completes AFTER the arrival — entry-on-completion
	// (d91 §3.4): this is the event whose payload now carries arrival_ref,
	// trip_ref and visit.
	diazID := w.ids(3)[0]
	completionEnv, err := CompleteOrientation(w.b, "site-b", 1, diazID, nil, 0, "default")
	if err != nil {
		t.Fatalf("CompleteOrientation(Diaz): %v", err)
	}

	// PRECONDITION: entry actually happened locally at B, and the payload is
	// the shape being proven — a completion carrying an entry, not a bare
	// three-key one. If this is false the replication proof below would be
	// replicating a payload with nothing new in it.
	var payload OrientationCompleted
	if err := json.Unmarshal(completionEnv.Payload, &payload); err != nil {
		t.Fatalf("PRECONDITION: decoding Diaz's own completion payload: %v", err)
	}
	if payload.ArrivalRef == nil || payload.TripRef == nil || payload.Visit == nil {
		t.Fatalf("PRECONDITION: Diaz's completion payload carries no entry (arrival_ref=%v trip_ref=%v visit=%v) — "+
			"nothing new would be replicated", payload.ArrivalRef, payload.TripRef, payload.Visit)
	}
	bVisits, err := OpenVisitsAt(w.b, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	var bVisit *Visit
	for i := range bVisits {
		if bVisits[i].PersonID == diazID {
			bVisit = &bVisits[i]
		}
	}
	if bVisit == nil {
		t.Fatal("PRECONDITION: B itself did not enter Diaz — nothing to replicate")
	}

	// --- the replication proof: a THIRD post, C, pulling both streams -----
	//
	// C never called CompleteOrientation, never called RecordExpectedArrival,
	// never saw a single HTTP request from either A or B. Everything it
	// knows comes from replaying A's and B's OWN authored envelopes, one at
	// a time, in stream order — exactly what a real peer's repl.Engine does
	// after an HTTP pull, minus the transport.
	c := newDomainTestStore(t)
	replicateStream(t, c, w.a, "site-a")
	replicateStream(t, c, w.b, "site-b")

	// C's own fold must reproduce B's local state exactly — the derived
	// orientation row AND the visit entry-on-completion minted.
	cVisits, err := OpenVisitsAt(c, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	var cVisit *Visit
	for i := range cVisits {
		if cVisits[i].PersonID == diazID {
			cVisit = &cVisits[i]
		}
	}
	if cVisit == nil {
		t.Fatalf("C replicated B's log but Diaz has no open visit at site-b — entry-on-completion's replicated "+
			"payload did not fold into a visit at the peer. B's own visits: %+v", bVisits)
	}
	if cVisit.VisitID != bVisit.VisitID {
		t.Errorf("C's visit id %q != B's own %q — the two posts disagree about WHICH visit entry-on-completion minted", cVisit.VisitID, bVisit.VisitID)
	}
	if cVisit.TripID == nil || bVisit.TripID == nil || *cVisit.TripID != *bVisit.TripID {
		t.Errorf("C's visit trip_id %v != B's own %v — the replicated trip_ref did not fold identically", cVisit.TripID, bVisit.TripID)
	}
	// FALSIFIABILITY-2.md M-C: the two checks above only ever compare C's
	// derived copy to B's OWN — both are folded by the identical
	// applyOrientationCompleted code from the identical replicated
	// envelope, so a fold that mints the visit with the wrong field (e.g.
	// p.ArrivalRef where p.TripRef belongs) would be wrong IDENTICALLY at
	// both posts and this cross-copy comparison would still pass. Assert
	// the VALUE against the real, independently-known trip id instead —
	// the one fact a cross-copy agreement cannot see.
	if bVisit.TripID == nil || *bVisit.TripID != trip.TripID {
		t.Fatalf("B's own visit.trip_id = %v, want the ACTUAL dispatched trip's id %q — a value check, not a cross-copy comparison", bVisit.TripID, trip.TripID)
	}
	if cVisit.TripID == nil || *cVisit.TripID != trip.TripID {
		t.Fatalf("C's own visit.trip_id = %v, want the ACTUAL dispatched trip's id %q", cVisit.TripID, trip.TripID)
	}

	held2, err := OrientationHeld(c, diazID, "site-b")
	if err != nil {
		t.Fatal(err)
	}
	if held2 == nil {
		t.Fatal("C replicated B's log but does not derive Diaz as holding site-b's orientation")
	}
	if held2.NodeEpoch != completionEnv.NodeEpoch || held2.Seq != completionEnv.Seq {
		t.Errorf("C's derived orientation row keys to (%d,%d), want the completion's own (%d,%d)",
			held2.NodeEpoch, held2.Seq, completionEnv.NodeEpoch, completionEnv.Seq)
	}

	stored, err := c.StoredSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	// SchemaVersion 13 (R-17): re-pinned from "12" to "13" deliberately — the
	// entry-on-completion proof this file exists for rode the 11→12 bump and
	// is unrelated to the unidentified-vehicle change that landed 13; the
	// literal just needs to track whatever the CURRENT version is, so this
	// proof stays "only meaningful post-bump" rather than silently checking
	// a version that predates the store these three scratch posts actually
	// open.
	// SchemaVersion 14 (R-18): re-pinned again, same reasoning — this proof
	// is unrelated to the unsolicited-arrival vehicle change that landed 14.
	// SchemaVersion 15 (R-20 + R-23): re-pinned again. This one IS related:
	// the visit C reconstructs now carries its minting key and a DERIVED
	// is_open, and the third-post replay below is exactly the property
	// R-20 restores — the literal still just tracks the CURRENT version.
	// SchemaVersion 16 (R3 alert_kind, 2026-10-04): re-pinned; ⟦was: "15"⟧
	if stored != "16" {
		t.Errorf("C's own schema_version = %q, want \"16\" — this whole proof is only meaningful post-bump", stored)
	}
}
