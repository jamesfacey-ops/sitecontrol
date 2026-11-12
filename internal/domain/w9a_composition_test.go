// w9a_composition_test.go — W9a's composition-shaped criteria:
//
//	W9a-1  (T1)  a correction is a NEW EVENT — the raw `trip` row byte-identical
//	W9a-4  (T4)  per-fact fixed-order resolution, expected value pinned as a LITERAL
//	W9a-5  (T5)  per-member occupant facts, expected SET pinned as a LITERAL
//	W9a-6  (T6)  the driver ∪ occupants union under the hostile interleaving
//	W9a-11 (T10) order-independence: byte-identical DumpProjections
//	W9a-13 (T-A-ext) the T-A agreement oracle over the T4/T5/T6 fixtures
//	W9a-17 (T18) composition is the guarantee — `Clear` and occupants FILTERED
//
// See w9a_seams_test.go for the one design decision every file here rests
// on: no W9a test names an unbuilt Go symbol, so each of these reds at
// RUNTIME against the pre-chunk tree rather than burying the package in a
// compile error.
//
// ⚠⚠ TWO LOCKED FIXTURES CANNOT BE BUILT AS WRITTEN — REPORTED, NOT REPAIRED.
//
// W9a-4's and W9a-5's tables each carry a "delivery position" column, and in
// both tables that column INVERTS WITHIN-STREAM SEQUENCE ORDER:
//
//	W9a-4: P = (site-b, 1, 12) delivered 4th, R = (site-b, 1, 7) delivered 5th
//	       Q = (site-b, 2, 3)  delivered 1st, S = (site-b, 2, 1) delivered 3rd
//	W9a-5: P = (site-b, 1, 12) delivered 1st, R = (site-b, 1, 7) delivered 3rd
//	       Q = (site-b, 2, 3)  delivered 2nd, S = (site-b, 2, 1) delivered 4th
//
// The store's replication contract forbids both: "seq > watermark+1: gap —
// *GapError, nothing applied, watermark stalled loudly (§8.4: never a silent
// skip)" [MEASURED — store/log.go, Apply's doc block and applyBatch's
// `case env.Seq > wm+1`]. Delivering seq 12 of a stream before seq 7 is not
// a slow path or an unusual one; it is refused, and the fixture cannot be
// constructed. I have NOT invented a replacement fixture that claims the
// separation the locked table claims. What these tests do instead, and
// exactly which rival each realisable permutation separates, is written out
// at each test and in reports/window-build/W9a-tests.md §3.
package domain

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// =========================================================================
// W9a-1 (T1) — a correction is a NEW EVENT
// =========================================================================

// TestW9a1AmendmentLeavesTheRawTripRowByteIdentical — W9a-1's byte-identity
// half and the front half of its "renders effective" claim.
//
// KILLER MUTANTS: (m1b) the EDITING FOLD — the fold UPDATEs `trip` instead
// of inserting a side-table row, which every "the driver reads Y" assertion
// in the suite passes; (m1) ZERO-RESOLUTION — the type ships, folds, and no
// reader composes, caught here by the side table's existence and its row.
//
// The "renders effective on the incident read with the chain post-attributed"
// half is a RENDERED surface and lives in internal/web (w9a_surfaces_test.go).
func TestW9a1AmendmentLeavesTheRawTripRowByteIdentical(t *testing.T) {
	st := w9aNewStore(t)
	w9aSeedPersons(t, st, "site-a", 1, "drv-x", "drv-y", "occ-1")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-14", "drv-x", []string{"occ-1"}, 240)
	w9aArriveExpected(t, st, "site-b", 1, trip.TripID)
	if got := w9aTripState(t, st, trip.TripID); got != TripStateArrived {
		t.Fatalf("PRECONDITION: trip %s is %q, want %q — T1's fixture is an ARRIVED trip", trip.TripID, got, TripStateArrived)
	}

	tripBefore := w9aSection(t, st, "trip")
	occBefore := w9aSection(t, st, "trip_occupant")
	stateBefore := w9aSection(t, st, "trip_state")

	key := w9aApplyNext(t, st, "site-b", 1, w9aBase+w9aMinute, w9aTripAmend{
		TripID: trip.TripID, Driver: w9aStr("drv-y"),
	}.payload())

	// --- (m1b): the raw row is EVIDENCE and never moves (D-1) -----------
	if got := w9aSection(t, st, "trip"); got != tripBefore {
		t.Fatalf("W9a-1 (m1b — the editing fold): the raw `trip` projection CHANGED across a %s.\nbefore: %s\nafter:  %s\n"+
			"F4 §3.2: the fold is a bare INSERT into the side table and nothing else; D-1 keeps the original assertion in the chain. "+
			"An UPDATE here is byte-identical-green on every 'the driver reads drv-y' assertion in this suite", w9aEvtTripAmended, tripBefore, got)
	}
	if got := w9aSection(t, st, "trip_occupant"); got != occBefore {
		t.Fatalf("W9a-1 (m1b): the raw `trip_occupant` projection CHANGED across a %s.\nbefore: %s\nafter:  %s", w9aEvtTripAmended, occBefore, got)
	}
	// I-2: the amendment never enters computeStates, so derived state is
	// byte-identical too. (W9a-14 restates this on the banner fixture; here
	// it guards the arrived-trip direction.)
	if got := w9aSection(t, st, "trip_state"); got != stateBefore {
		t.Fatalf("W9a-1 / I-2: `trip_state` CHANGED across a %s.\nbefore: %s\nafter:  %s\n"+
			"F4 §3.2: the fold does NOT enter computeStates or the scoped recompute", w9aEvtTripAmended, stateBefore, got)
	}

	// --- (m1): the amendment must LAND somewhere a reader can compose ---
	rows := w9aAmendmentRows(t, st, w9aTblTripAmendment)
	if len(rows) != 1 {
		t.Fatalf("W9a-1 (m1 — zero resolution): %s holds %d rows after one %s, want 1. "+
			"An event type that ships and folds nothing is the cheapest mutant in the register", w9aTblTripAmendment, len(rows), w9aEvtTripAmended)
	}
	if got := rows[0].key; got != key {
		t.Fatalf("W9a-1: the %s row's PK is %v, want the envelope's OWN key %v (F4 §3.2: 'PK (site_id, node_epoch, seq) — the envelope's own key')",
			w9aTblTripAmendment, got, key)
	}
	if got := rows[0].text(w9aKeyDriver); got != "drv-y" {
		t.Fatalf("W9a-1: the %s row's %s column reads %q, want %q — the assertion must survive verbatim for the audit chain to render it (D-1)",
			w9aTblTripAmendment, w9aKeyDriver, got, "drv-y")
	}
}

// =========================================================================
// W9a-4 (T4) — per-fact fixed-order resolution, the expected value a LITERAL
// =========================================================================

// w9a4 names the five amendments' LOCKED keys and asserted drivers. The
// table is transcribed from the criterion, not derived.
type w9a4Amendment struct {
	label     string
	site      string
	epoch     int64
	seq       int64
	wallLabel string
	wallMS    int64
	driver    string
}

// w9a4Table is W9a-4's table, verbatim. Wall-instants are the criterion's
// clock times on one fixture day; only their ORDER is load-bearing and the
// order is preserved exactly (Q 08:10 < R 08:40 < P 09:05 < S 09:50 < U 10:30).
var w9a4Table = []w9a4Amendment{
	{"P", "site-b", 1, 12, "09:05", w9aBase + 9*60*w9aMinute + 5*w9aMinute, "drv-nunez"},
	{"Q", "site-b", 2, 3, "08:10", w9aBase + 8*60*w9aMinute + 10*w9aMinute, "drv-okafor"},
	{"R", "site-b", 1, 7, "08:40", w9aBase + 8*60*w9aMinute + 40*w9aMinute, "drv-petrov"},
	{"S", "site-b", 2, 1, "09:50", w9aBase + 9*60*w9aMinute + 50*w9aMinute, "drv-quist"},
	{"U", "site-a", 1, 40, "10:30", w9aBase + 10*60*w9aMinute + 30*w9aMinute, "drv-vance"},
}

// The four LITERALS, written as the criterion writes them. `want` is NEVER
// computed as max(keys, Less): a want computed with the comparator the
// mutant replaces makes an epoch-blind build produce an epoch-blind
// expectation and pass.
const (
	w9a4WantCertified  = "drv-okafor" // site-b > site-a; epoch 2 > 1; seq 3 > 1
	w9a4RivalClock     = "drv-vance"  // U, the latest wall-instant of the five
	w9a4RivalEpochBlnd = "drv-nunez"  // P, highest seq at the highest site
	w9a4RivalArrivalR  = "drv-petrov" // R, "delivered last" in the LOCKED table
)

func w9a4Find(label string) w9a4Amendment {
	for _, a := range w9a4Table {
		if a.label == label {
			return a
		}
	}
	panic("w9a4: no amendment labelled " + label)
}

// w9a4Build lays the fixture down in the given DELIVERY ORDER (labels) and
// returns the trip.
//
// ⚠ The LOCKED delivery-position column is NOT reproducible — see this
// file's header. `order` must respect within-stream ascending seq or
// store.Apply refuses with a GapError; each caller states which rival its
// order separates.
func w9a4Build(t *testing.T, st *store.Store, order []string) Trip {
	t.Helper()
	w9aSeedPersons(t, st, "site-a", 1,
		"drv-start", "drv-nunez", "drv-okafor", "drv-petrov", "drv-quist", "drv-vance")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-99", "drv-start", nil, 240)
	for _, label := range order {
		a := w9a4Find(label)
		w9aApplyAt(t, st, a.site, a.epoch, a.seq, a.wallMS, w9aTripAmend{
			TripID: trip.TripID, Driver: w9aStr(a.driver),
		}.payload())
	}
	// The fixture asserts its own precondition — five `trip_amended`
	// events at the five LOCKED keys, each asserting its own driver —
	// read from the replicated LOG rather than from a side table the
	// builder may not have written yet. A fixture whose keys drifted would
	// make every ordering claim below meaningless while still going green.
	for _, a := range w9a4Table {
		want := store.Key{SiteID: a.site, NodeEpoch: a.epoch, Seq: a.seq}
		m := w9aLogPayloadAt(t, st, want)
		if m[w9aKeyType] != w9aEvtTripAmended || m[w9aKeyTripID] != trip.TripID || m[w9aKeyDriver] != a.driver {
			t.Fatalf("PRECONDITION (W9a-4): amendment %s at its LOCKED key %v reads %v, want a %s for trip %s asserting driver %q — the keys and the drivers are LITERALS",
				a.label, want, m, w9aEvtTripAmended, trip.TripID, a.driver)
		}
	}
	return trip
}

// TestW9a4PerFactResolutionPinsTheEffectiveDriverAsALiteral — W9a-4.
//
// KILLER MUTANTS: (m4) clock-order or arrival-order resolution;
// (m4b) the epoch-blind comparator `(site_id, seq)` — the §5 CONTROL
// pairing, carried INSIDE this one fixture as Q `(site-b, 2, 3)` beating
// P `(site-b, 1, 12)`: epoch 2 / low seq beats epoch 1 / high seq.
//
// The observation point is `OpenExpectations` — F4 §9.1's named COMPOSED
// read projection and the shared input of §9.2's consumers 2, 4 and 5. Its
// `DriverName` is the roster display name of the effective driver, and the
// fixture gives each roster person a display name EQUAL TO ITS ID, so the
// value read back is the criterion's literal and not a value this test
// computed.
func TestW9a4PerFactResolutionPinsTheEffectiveDriverAsALiteral(t *testing.T) {
	// Two REALISABLE delivery permutations. Neither is the LOCKED table's
	// (see the header). Between them they still deny an arrival-order build
	// a stable answer, and NEITHER of its two answers is the certified one:
	//   forward: S, Q, R, U, P  → arrival-order picks P = drv-nunez
	//   reverse: R, P, S, Q, U  → arrival-order picks U = drv-vance
	// so an arrival-order build reds in at least one arm, and in both arms
	// it is wrong. What is LOST against the locked table is the separation
	// of arrival-order from the epoch-blind comparator (forward arm) and
	// from clock order (reverse arm) — stated, not hidden.
	for _, perm := range []struct {
		name          string
		order         []string
		arrivalRival  string
		rivalSeparate string
	}{
		{"forward", []string{"S", "Q", "R", "U", "P"}, w9a4RivalEpochBlnd, "arrival order and the epoch-blind comparator coincide in this arm"},
		{"reverse", []string{"R", "P", "S", "Q", "U"}, w9a4RivalClock, "arrival order and clock order coincide in this arm"},
	} {
		t.Run(perm.name, func(t *testing.T) {
			st := w9aNewStore(t)
			trip := w9a4Build(t, st, perm.order)

			e := w9aExpectationFor(t, st, "site-b", trip.TripID)
			if e.DriverName != w9a4WantCertified {
				t.Fatalf("W9a-4 (m4 / m4b): the effective driver of trip %s reads %q, want the LITERAL %q.\n"+
					"  certified rule (site_id, then node_epoch, then seq)  -> %s   [Q = (site-b, 2, 3)]\n"+
					"  clock order (latest wall-instant)                    -> %s   [U, 10:30]\n"+
					"  arrival order (delivered last, this permutation)     -> %s\n"+
					"  epoch-blind comparator (site_id, seq)                -> %s   [P, seq 12]\n"+
					"The §5 CONTROL pairing is inside this fixture: Q (site-b, epoch 2, seq 3) must beat "+
					"P (site-b, epoch 1, seq 12) — epoch 2 / low seq beats epoch 1 / high seq. "+
					"NOTE: %s",
					trip.TripID, e.DriverName, w9a4WantCertified,
					w9a4WantCertified, w9a4RivalClock, perm.arrivalRival, w9a4RivalEpochBlnd, perm.rivalSeparate)
			}
			// The named rivals are asserted NEGATIVELY too, by name, so a
			// build that lands on one of them is reported as that build and
			// not as "some other value".
			for _, rival := range []struct{ name, value string }{
				{"clock order", w9a4RivalClock},
				{"the epoch-blind comparator (m4b)", w9a4RivalEpochBlnd},
				{"arrival order (m4), as the LOCKED table states it", w9a4RivalArrivalR},
			} {
				if e.DriverName == rival.value {
					t.Fatalf("W9a-4: the effective driver is %q — that is %s's answer, not the certified rule's (%q)",
						e.DriverName, rival.name, w9a4WantCertified)
				}
			}
		})
	}
}

// =========================================================================
// W9a-5 (T5) — per-member occupant facts, the expected SET a LITERAL
// =========================================================================

type w9a5Amendment struct {
	label   string
	site    string
	epoch   int64
	seq     int64
	wallMS  int64
	amend   w9aTripAmend
	factSet map[string]string // fact name suffix -> asserted value
}

const (
	w9aOccupantAboard    = "aboard"
	w9aOccupantNotAboard = "not aboard"
)

// The LOCKED literals of W9a-5, transcribed.
var (
	w9a5Start          = []string{"p-alvarez", "p-brand", "p-chen"}
	w9a5WantCertified  = []string{"p-alvarez", "p-brand", "p-delgado", "p-everett"}
	w9a5RivalClock     = []string{"p-alvarez", "p-delgado", "p-everett"}
	w9a5RivalArrival   = []string{"p-alvarez", "p-brand", "p-chen", "p-delgado", "p-everett"}
	w9a5RivalEpochBlnd = []string{"p-alvarez", "p-chen", "p-delgado", "p-everett"}
	w9a5RivalWholeList = []string{"p-alvarez", "p-brand"} // (m5)
)

const w9a5CargoNoteLiteral = "pallets"

func w9a5Table() []w9a5Amendment {
	return []w9a5Amendment{
		{"P", "site-b", 1, 12, w9aBase + 9*60*w9aMinute + 35*w9aMinute, w9aTripAmend{
			OccupantsRemove: []string{"p-brand"}, CargoNote: w9aStr(w9a5CargoNoteLiteral),
		}, map[string]string{"p-brand": w9aOccupantNotAboard}},
		{"Q", "site-b", 2, 3, w9aBase + 9*60*w9aMinute + 30*w9aMinute, w9aTripAmend{
			OccupantsAdd: []string{"p-brand"}, OccupantsRemove: []string{"p-chen"},
		}, map[string]string{"p-brand": w9aOccupantAboard, "p-chen": w9aOccupantNotAboard}},
		{"R", "site-b", 1, 7, w9aBase + 8*60*w9aMinute + 40*w9aMinute, w9aTripAmend{
			OccupantsAdd: []string{"p-chen", "p-delgado"},
		}, map[string]string{"p-chen": w9aOccupantAboard, "p-delgado": w9aOccupantAboard}},
		{"S", "site-b", 2, 1, w9aBase + 9*60*w9aMinute + 50*w9aMinute, w9aTripAmend{
			OccupantsAdd: []string{"p-everett"},
		}, map[string]string{"p-everett": w9aOccupantAboard}},
	}
}

// w9a5Build lays W9a-5's fixture down in a REALISABLE delivery order (the
// locked one inverts within-stream seq — see the file header) and returns
// the trip plus each label's key.
func w9a5Build(t *testing.T, st *store.Store, order []string) (Trip, map[string]store.Key) {
	t.Helper()
	w9aSeedPersons(t, st, "site-a", 1,
		"p-alvarez", "p-brand", "p-chen", "p-delgado", "p-everett")
	// The driver is p-alvarez, who is already in the dispatched set, so the
	// §3.4 union adds nobody and cannot mask a composition defect.
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-55", "p-alvarez",
		[]string{"p-brand", "p-chen"}, 240)
	if got := w9aRawOccupantIDs(t, st, trip.TripID); !w9aEqualStrings(got, w9aSortedCopy(w9a5Start)) {
		t.Fatalf("PRECONDITION (W9a-5): the dispatched occupant set is %v, want the LOCKED %v", got, w9a5Start)
	}
	byLabel := map[string]w9a5Amendment{}
	for _, a := range w9a5Table() {
		byLabel[a.label] = a
	}
	keys := map[string]store.Key{}
	for _, label := range order {
		a := byLabel[label]
		p := a.amend
		p.TripID = trip.TripID
		keys[label] = w9aApplyAt(t, st, a.site, a.epoch, a.seq, a.wallMS, p.payload())
	}
	return trip, keys
}

// TestW9a5PerMemberOccupantFactsPinTheEffectiveSetAsALiteral — W9a-5.
//
// KILLER MUTANTS: (m5) WHOLE-LIST resolution — occupants treated as ONE
// fact and the highest-key toucher applied wholesale, which yields
// {p-alvarez, p-brand} and never lets p-delgado or p-everett enter;
// (m4) clock/arrival-order resolution; (m4b) the epoch-blind comparator;
// (m5b) unconditional supersession language, killed by the disjoint half.
func TestW9a5PerMemberOccupantFactsPinTheEffectiveSetAsALiteral(t *testing.T) {
	st := w9aNewStore(t)
	// Realisable delivery order (within-stream ascending): R, P, S, Q.
	trip, keys := w9a5Build(t, st, []string{"R", "P", "S", "Q"})

	// --- the composed set, pinned as the LITERAL -----------------------
	e := w9aExpectationFor(t, st, "site-b", trip.TripID)
	got := w9aOccupantNames(e)
	if !w9aEqualStrings(got, w9aSortedCopy(w9a5WantCertified)) {
		t.Fatalf("W9a-5: the effective occupant set of trip %s is %v, want the LITERAL %v.\n"+
			"  certified rule (per member, ascending key)  -> %v\n"+
			"  clock order                                 -> %v\n"+
			"  arrival order                               -> %v\n"+
			"  epoch-blind comparator                      -> %v\n"+
			"  whole-list resolution (m5)                  -> %v\n"+
			"Five candidate answers, five different literals: this fixture is non-degenerate by construction.",
			trip.TripID, got, w9aSortedCopy(w9a5WantCertified),
			w9aSortedCopy(w9a5WantCertified), w9aSortedCopy(w9a5RivalClock), w9aSortedCopy(w9a5RivalArrival),
			w9aSortedCopy(w9a5RivalEpochBlnd), w9aSortedCopy(w9a5RivalWholeList))
	}
	if e.Occupants != len(w9a5WantCertified) {
		t.Fatalf("W9a-5: the composed occupant COUNT is %d, want %d — the count and the list are read by different surfaces and must not drift",
			e.Occupants, len(w9a5WantCertified))
	}

	// --- the raw membership is untouched (F4 §3.2, D-1) ----------------
	if raw := w9aRawOccupantIDs(t, st, trip.TripID); !w9aEqualStrings(raw, w9aSortedCopy(w9a5Start)) {
		t.Fatalf("W9a-5 / W9a-1 (m1b): the RAW trip_occupant set moved to %v; it must stay the dispatched %v — composition is a read, never a write",
			raw, w9a5Start)
	}

	// --- the per-fact-independence half --------------------------------
	// P loses `occupant/p-brand` to Q, and P's cargo_note stays Effective
	// with the pinned literal "pallets".
	brandFact := w9aFactOccupantPrefix + "p-brand"
	outP := w9aFact(t, st, keys["P"], brandFact)
	if outP.Status != Superseded {
		t.Fatalf("W9a-5: P's %q is %v, want Superseded — Q (site-b, epoch 2, seq 3) asserts the same fact at a higher key", brandFact, outP.Status)
	}
	if outP.Winner != keys["Q"] {
		t.Fatalf("W9a-5: P's %q is Superseded with Winner %v, want Q's key %v", brandFact, outP.Winner, keys["Q"])
	}
	outCargo := w9aFact(t, st, keys["P"], w9aKeyCargoNote)
	if outCargo.Status != Effective {
		t.Fatalf("W9a-5 (VF §4.2, per-fact never per-event): P's %s is %v, want Effective — P lost ONE fact and must keep the other",
			w9aKeyCargoNote, outCargo.Status)
	}
	if outCargo.Asserted != w9a5CargoNoteLiteral {
		t.Fatalf("W9a-5: P's %s renders %q, want the LITERAL %q", w9aKeyCargoNote, outCargo.Asserted, w9a5CargoNoteLiteral)
	}

	// --- the DISJOINT half: no supersession language on either ---------
	// (m5b) — `occupant/p-delgado` (R only) and `occupant/p-everett`
	// (S only) are uncontested: BOTH Effective, each its own Winner.
	for _, d := range []struct{ label, person string }{{"R", "p-delgado"}, {"S", "p-everett"}} {
		fact := w9aFactOccupantPrefix + d.person
		out := w9aFact(t, st, keys[d.label], fact)
		if out.Status != Effective {
			t.Fatalf("W9a-5 (m5b — unconditional supersession language): %s's %q is %v, want Effective. "+
				"Removing one occupant does NOT contest with adding another: different raw ids are different fact names (F4 §6)",
				d.label, fact, out.Status)
		}
		if out.Winner != keys[d.label] {
			t.Fatalf("W9a-5: %s's %q is Effective but Winner is %v, not the queried key %v (VF §4.2's convention)",
				d.label, fact, out.Winner, keys[d.label])
		}
	}
}

// =========================================================================
// W9a-6 (T6) — the driver ∪ occupants union
// =========================================================================

// TestW9a6DriverOccupantUnionHoldsUnderTheHostileInterleaving — W9a-6's
// union half.
//
// KILLER MUTANT: (m6) the union omitted — a composition that resolves each
// fact correctly and never re-adds the effective driver. NO SURFACE SEES
// this interleaving: site-a sets driver = Y, site-b removes Y as an
// occupant, and each amendment is valid at its own authoring post.
//
// The (m6b) half — the losing removal rendering a bare, falsely-reassuring
// `Effective` instead of §3.4's invariant-override sentence — is a RENDERED
// surface and lives in internal/web. What this test pins here is the fact
// that makes that rendering necessary: site-b's removal WINS its per-fact
// contest and Y is aboard anyway.
func TestW9a6DriverOccupantUnionHoldsUnderTheHostileInterleaving(t *testing.T) {
	st := w9aNewStore(t)
	w9aSeedPersons(t, st, "site-a", 1, "drv-first", "p-yates", "p-zane")
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-77", "drv-first", []string{"p-yates", "p-zane"}, 240)

	// site-a: the driver becomes p-yates.
	w9aApplyAt(t, st, "site-a", 2, 1, w9aBase+w9aMinute, w9aTripAmend{
		TripID: trip.TripID, Driver: w9aStr("p-yates"),
	}.payload())
	// site-b: p-yates is removed as an occupant. Nobody sees both.
	removeKey := w9aApplyAt(t, st, "site-b", 1, 1, w9aBase+2*w9aMinute, w9aTripAmend{
		TripID: trip.TripID, OccupantsRemove: []string{"p-yates"},
	}.payload())

	e := w9aExpectationFor(t, st, "site-b", trip.TripID)
	if e.DriverName != "p-yates" {
		t.Fatalf("W9a-6 PRECONDITION: the effective driver is %q, want %q — the union has nothing to do unless the driver amendment took effect", e.DriverName, "p-yates")
	}
	names := w9aOccupantNames(e)
	if !w9aContains(names, "p-yates") {
		t.Fatalf("W9a-6 (m6 — the union omitted): the composed occupant set of trip %s is %v and does NOT contain the effective driver p-yates. "+
			"F4 §3.4: 'effective occupants ∪ {effective driver}' — applyTripDispatched already prepends the driver structurally, and composition must repeat it, "+
			"or a hostile interleaving no surface sees produces a driverless occupant set", trip.TripID, names)
	}

	// The removal WON its per-fact contest — that is exactly why the bare
	// `Effective` is falsely reassuring and §3.4 owes the loser a rendering.
	fact := w9aFactOccupantPrefix + "p-yates"
	out := w9aFact(t, st, removeKey, fact)
	if out.Status != Effective {
		t.Fatalf("W9a-6: site-b's %q is %v, want Effective — there is no competing membership assertion, so it wins its per-fact contest. "+
			"The union negating it afterwards is what the §3.4 invariant-override sentence exists to say", fact, out.Status)
	}
}

// =========================================================================
// W9a-11 (T10) — order-independence
// =========================================================================

// TestW9a11OppositeDeliveryOrdersProduceByteIdenticalDumps — W9a-11.
//
// KILLER MUTANT: (m10) order-dependent apply — apply-time validation that
// drops a DANGLING amendment (one delivered before its trip), so one
// permutation composes and the other does not.
//
// ⚠ Two limbs, and the FIRST is what gives this test a pre-chunk red. On a
// tree where nothing folds an amendment, "byte-identical dumps" is
// trivially true, so a bare dump comparison would pass for the wrong
// reason. Limb 1 therefore asserts what F4 §3.2 requires of the dump —
// "Projections `trip_amendment` / `arrival_amendment`, DUMP-PARTICIPATING" —
// which is also the precondition that makes limb 2 mean anything.
func TestW9a11OppositeDeliveryOrdersProduceByteIdenticalDumps(t *testing.T) {
	// Build the two streams ONCE, then replay them into two fresh stores in
	// opposite orders. The amendment stream is delivered FIRST into store B,
	// so its amendment is dangling when it folds.
	build := func(t *testing.T, tripFirst bool) (*store.Store, string) {
		t.Helper()
		st := w9aNewStore(t)
		w9aSeedPersons(t, st, "site-r", 1, "drv-one", "drv-two")
		const tripID = "trip-w9a11"
		tripPayload := map[string]any{
			"type": "trip_dispatched", "trip_id": tripID,
			"vehicle_entered": "TRK-11", "driver_person_id": "drv-one",
			"occupant_person_ids": []string{"drv-one"},
			"destination_site":    "site-b", "deadline_minutes": 240,
		}
		amendPayload := w9aTripAmend{TripID: tripID, Driver: w9aStr("drv-two")}.payload()
		if tripFirst {
			w9aApplyAt(t, st, "site-a", 1, 1, w9aBase, tripPayload)
			w9aApplyAt(t, st, "site-b", 1, 1, w9aBase+w9aMinute, amendPayload)
		} else {
			w9aApplyAt(t, st, "site-b", 1, 1, w9aBase+w9aMinute, amendPayload) // DANGLING
			w9aApplyAt(t, st, "site-a", 1, 1, w9aBase, tripPayload)
		}
		return st, tripID
	}

	stA, tripID := build(t, true)
	stB, _ := build(t, false)

	// --- limb 1: the side table is DUMP-PARTICIPATING (F4 §3.2) --------
	for _, s := range []struct {
		name string
		st   *store.Store
	}{{"trip-first", stA}, {"amendment-first", stB}} {
		if _, err := s.st.DumpProjection(w9aTblTripAmendment); err != nil {
			t.Fatalf("W9a-11 limb 1 (%s store): DumpProjection(%q) failed: %v.\n"+
				"F4 §3.2 requires the amendment side tables be REGISTERED, DUMP-PARTICIPATING projections. "+
				"Until they are, 'byte-identical DumpProjections' is trivially true and certifies nothing",
				s.name, w9aTblTripAmendment, err)
		}
	}

	// --- limb 2: byte-identical whole-database dumps -------------------
	dumpA, dumpB := w9aAllProjections(t, stA), w9aAllProjections(t, stB)
	if dumpA != dumpB {
		t.Fatalf("W9a-11 (m10 — order-dependent apply): the two delivery orders produced DIFFERENT DumpProjections.\n"+
			"trip-first:      %s\namendment-first: %s\n"+
			"F4 §3.2: the fold must NOT validate against current state — 'a dangling amendment for a trip not yet delivered folds inert and composes when the trip arrives'",
			dumpA, dumpB)
	}

	// --- limb 3: the composed read agrees too --------------------------
	eA := w9aExpectationFor(t, stA, "site-b", tripID)
	eB := w9aExpectationFor(t, stB, "site-b", tripID)
	if eA.DriverName != eB.DriverName {
		t.Fatalf("W9a-11: the composed effective driver differs by delivery order: trip-first %q, amendment-first %q", eA.DriverName, eB.DriverName)
	}
	if eA.DriverName != "drv-two" {
		t.Fatalf("W9a-11: the composed effective driver is %q in BOTH orders, want %q — identical-and-uncomposed is (m1), not order-independence",
			eA.DriverName, "drv-two")
	}
}

// =========================================================================
// W9a-13 (T-A-ext) — the agreement oracle
// =========================================================================

// w9aTAFixture is one asserted fact of one fixture event, in the shape
// w3_seams_test.go's oracle established.
type w9aTAFixture struct {
	Key      store.Key
	TripID   string
	Fact     string
	Asserted string
}

// w9aComposedFactValue reads a fact's COMPOSED value at the consumer.
// SEAM W9A-S-COMPOSED, bound here once: the composed reader is
// `OpenExpectations` — F4 §9.1's named read projection — and the two facts
// the oracle ranges over are the two it demonstrably composes (§9.1's own
// list: "driver, occupant count and list").
func w9aComposedFactValue(t *testing.T, st *store.Store, siteID, tripID, fact string) (string, bool) {
	t.Helper()
	e := w9aExpectationFor(t, st, siteID, tripID)
	if fact == w9aKeyDriver {
		return e.DriverName, e.DriverName != ""
	}
	if strings.HasPrefix(fact, w9aFactOccupantPrefix) {
		person := strings.TrimPrefix(fact, w9aFactOccupantPrefix)
		if w9aContains(w9aOccupantNames(e), person) {
			return w9aOccupantAboard, true
		}
		return w9aOccupantNotAboard, true
	}
	t.Fatalf("w9aComposedFactValue: no accessor bound for fact %q (extend SEAM W9A-S-COMPOSED)", fact)
	return "", false
}

// w9aRunTAOracle is VF §8's T-A agreement oracle: for every fixture
// asserted-fact, Status == Effective IFF the composed value equals that
// event's asserted value. Precondition 1 (pairwise-distinct values per
// fact) is asserted at RUNTIME; precondition 2 (an unmatched fact is a
// FAILURE, never a skip) is w9aFact's job.
func w9aRunTAOracle(t *testing.T, st *store.Store, siteID string, fixtures []w9aTAFixture) {
	t.Helper()
	seen := map[string]map[string]store.Key{}
	for _, f := range fixtures {
		if seen[f.Fact] == nil {
			seen[f.Fact] = map[string]store.Key{}
		}
		if prev, dup := seen[f.Fact][f.Asserted]; dup {
			t.Fatalf("T-A FIXTURE DEFECT (precondition 1): fact %q asserted value %q twice (%v and %v) — with duplicate values the iff is unsatisfiable on correct code",
				f.Fact, f.Asserted, prev, f.Key)
		}
		seen[f.Fact][f.Asserted] = f.Key
	}
	for _, f := range fixtures {
		out := w9aFact(t, st, f.Key, f.Fact)
		val, present := w9aComposedFactValue(t, st, siteID, f.TripID, f.Fact)
		matches := present && val == f.Asserted
		if (out.Status == Effective) != matches {
			t.Fatalf("W9a-13 T-A disagreement (m11 — comparator drift): event %v fact %q asserted %q; Outcome says %v but the composed read says %q (present=%v). "+
				"The composition SQL's `ORDER BY site_id, node_epoch, seq` and `store.Key.Less` have drifted — the EPOCH TERM included",
				f.Key, f.Fact, f.Asserted, out.Status, val, present)
		}
		if out.Status == Effective && out.Winner != f.Key {
			t.Fatalf("W9a-13: fact %q Effective but Winner %v != the queried key %v (VF §4.2's convention)", f.Fact, out.Winner, f.Key)
		}
		if out.Status == Superseded {
			if out.Winner == (store.Key{}) {
				t.Fatalf("W9a-13: fact %q Superseded with a ZERO Winner — Winner is always a real event's key (VF §4.2)", f.Fact)
			}
			if out.Winner == f.Key {
				t.Fatalf("W9a-13: fact %q Superseded yet Winner == the queried key %v", f.Fact, f.Key)
			}
		}
	}
}

// TestW9a13TAOracleOverTheT4AndT5Fixtures — W9a-13, over the T4 and T5
// fixtures, epoch term included (T4's Q-over-P pairing IS the epoch term).
//
// ⚠ T6 IS DELIBERATELY NOT RUN THROUGH THE IFF, and this is a criteria
// observation, not a shortcut. On T6's fixture the union-negated fact
// `occupant/p-yates` is `Effective` (it won its per-fact contest) while the
// POST-UNION composed record says `aboard` — so the T-A iff is FALSE on
// correct code there, by §3.4's own design ("derived at read time by
// comparing the per-fact resolution against the post-union composed record").
// Running T6 through the iff would red a correct build. W9a-6 asserts T6's
// two halves directly instead. Reported in W9a-tests.md §3.
func TestW9a13TAOracleOverTheT4AndT5Fixtures(t *testing.T) {
	t.Run("T4", func(t *testing.T) {
		st := w9aNewStore(t)
		trip := w9a4Build(t, st, []string{"S", "Q", "R", "U", "P"})
		var fx []w9aTAFixture
		for _, a := range w9a4Table {
			fx = append(fx, w9aTAFixture{
				Key:    store.Key{SiteID: a.site, NodeEpoch: a.epoch, Seq: a.seq},
				TripID: trip.TripID, Fact: w9aKeyDriver, Asserted: a.driver,
			})
		}
		w9aRunTAOracle(t, st, "site-b", fx)
	})
	t.Run("T5", func(t *testing.T) {
		st := w9aNewStore(t)
		trip, keys := w9a5Build(t, st, []string{"R", "P", "S", "Q"})
		var fx []w9aTAFixture
		for _, a := range w9a5Table() {
			for person, asserted := range a.factSet {
				fx = append(fx, w9aTAFixture{
					Key: keys[a.label], TripID: trip.TripID,
					Fact: w9aFactOccupantPrefix + person, Asserted: asserted,
				})
			}
		}
		w9aRunTAOracle(t, st, "site-b", fx)
	})
}

// =========================================================================
// W9a-17 (T18) — composition is the guarantee
// =========================================================================

// TestW9a17CompositionFiltersClearAndRosterAbsentOccupants — W9a-17.
//
// KILLER MUTANT: (m26) the TRUSTING COMPOSITION — a composition that honours
// whatever the side table holds. `clear:["destination"]` then composes the
// cleared destination [[D-4]] exists to make unauthorable, and an
// `occupants_add` naming a roster-absent id enters the composed set.
//
// The rows are written DIRECTLY into `trip_amendment` — the divergent-writer
// path. That is not a contrivance: F4 §3.2 states that the fold deliberately
// does NOT validate against current state (for order-independence), so the
// authoring refusals of §3.5 are the ONLY other thing standing here, and
// §3.3a's whole point is that they must not be.
//
// CONTROL HALF, in the same test: `cleared = "cargo_note"` IS honoured, so
// the filter is a WHITELIST and not a blanket refusal of `Clear`.
func TestW9a17CompositionFiltersClearAndRosterAbsentOccupants(t *testing.T) {
	st := w9aNewStore(t)
	w9aSeedPersons(t, st, "site-a", 1, "drv-only", "p-onroster")
	dest := "site-b"
	trip, err := Dispatch(st, "site-a", 1, DispatchInput{
		// ⟦LOCK — §51, 2026-09-03⟧ ⟦was: "VehicleEntered: \"TRK-18\", DriverPersonID: \"drv-only\","⟧
		// §47/§50's ONE re-seed form, through the ONE shared helper
		// (w7_reseed_helper_test.go). `DispatchInput` no longer carries a
		// free-text vehicle field (DR-W7-4, tripauthor.go:31), so no repair
		// keeps the string. ⚠ §51 condition 4 required this site to be
		// verified on its OWN evidence and NOT on w5c's: the four assertions
		// it feeds were each planted against after the transformation —
		// whitelist removed, roster filter removed, blanket Clear refusal —
		// and each red is produced in reports/window-build/S51-BLOCKER-CLEAR.md
		// §5. (The fourth, D-1's byte-identical raw rows, is unreachable by a
		// code-level plant: `trip_amendment`'s INSERT-only triggers refuse both
		// DELETE and UPDATE, produced verbatim in §5.)
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-18"), DriverPersonID: "drv-only",
		OccupantPersonIDs: []string{"p-onroster"},
		DestinationSite:   &dest, DeadlineMinutes: w9aI64(240),
		CargoNote: w9aStr("drill pipe"),
	}, 0, "default")
	if err != nil {
		t.Fatalf("w9a fixture: dispatch: %v", err)
	}

	db := w9aRawDB(t, st)
	// Three rows, written by a DIVERGENT WRITER straight into the table.
	w9aInsertAmendmentRow(t, db, "site-z", 1, 1, trip.TripID, map[string]any{"cleared": "destination"})
	w9aInsertAmendmentRow(t, db, "site-z", 1, 2, trip.TripID, map[string]any{"occupants_add": "p-ghost"})
	w9aInsertAmendmentRow(t, db, "site-z", 1, 3, trip.TripID, map[string]any{"cleared": w9aKeyCargoNote})

	rawBefore := w9aSection(t, st, w9aTblTripAmendment)

	// Arm 1's subject is read off the UNSCOPED composed trip -- the same
	// EffectiveTrip call OpenExpectations itself makes before it applies its
	// own "is it destined here?" filter (tripauthor.go:966-971). Reading the
	// destination off `e` (OpenExpectations' filtered result, below) would
	// let a mutation that clears the destination remove the trip from
	// site-b's expectations BEFORE arm 1 ever evaluated -- the fixture would
	// then be doing the assertion's job (S125-DISPOSITIONS.md §N).
	c, err := EffectiveTrip(st, trip.TripID)
	if err != nil {
		t.Fatalf("w9a fixture: EffectiveTrip(%s): %v", trip.TripID, err)
	}
	if c == nil {
		t.Fatalf("PRECONDITION: EffectiveTrip(%s) returned nil -- the trip row is missing", trip.TripID)
	}

	// --- (m26) arm 1: the out-of-whitelist Clear is IGNORED ------------
	// Evaluated against the UNSCOPED composed trip, and BEFORE
	// w9aExpectationFor below -- w9aExpectationFor's own site-b filter would
	// otherwise fatal first on a mutant that clears the destination, masking
	// this arm behind the fixture's "it holds nothing" message instead of
	// this assertion's own.
	if c.Trip.DestinationSite == nil || *c.Trip.DestinationSite != "site-b" {
		t.Fatalf("W9a-17 (m26 — the trusting composition): after a divergent `cleared = \"destination\"` row the composed destination is %v, want \"site-b\". "+
			"F4 §3.3a: composition IGNORES any Clear token outside the {%s} whitelist — a cleared destination composes the crashed-check-in shape "+
			"[[D-4]] exists to make unauthorable", c.Trip.DestinationSite, strings.Join(w9aClearWhitelist, ", "))
	}

	e := w9aExpectationFor(t, st, "site-b", trip.TripID)

	// --- (m26) arm 2: the roster-absent occupant is IGNORED ------------
	if names := w9aOccupantNames(e); w9aContains(names, "p-ghost") {
		t.Fatalf("W9a-17 (m26): the composed occupant set is %v and contains `p-ghost`, which has NO roster row. "+
			"F4 §3.3a: composition filters occupant ids against the roster", names)
	}

	// --- the CONTROL half: an IN-whitelist Clear IS honoured -----------
	if e.Trip.CargoNote != nil {
		t.Fatalf("W9a-17 CONTROL: `cleared = %q` was NOT honoured — the composed cargo note is still %q. "+
			"Without this arm the two assertions above are satisfied by a build that simply refuses every `Clear`, "+
			"and the filter would be a blanket refusal rather than the WHITELIST §3.3a specifies", w9aKeyCargoNote, *e.Trip.CargoNote)
	}

	// --- the raw rows stand byte-identical (D-1) -----------------------
	if rawAfter := w9aSection(t, st, w9aTblTripAmendment); rawAfter != rawBefore {
		t.Fatalf("W9a-17: the raw %s rows CHANGED across a composed read.\nbefore: %s\nafter:  %s\n"+
			"F4 §3.3a: 'the raw row is preserved verbatim (D-1 — nothing is deleted, and the audit chain still shows what was asserted); "+
			"the EFFECTIVE read is filtered'", w9aTblTripAmendment, rawBefore, rawAfter)
	}
}

// =========================================================================
// shared helpers used only by this file's assertions
// =========================================================================

func w9aContains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// w9aAmendmentRow is one side-table row read back through the store's own
// projection dump — the amendment's PK plus its columns as JSON.
type w9aAmendmentRow struct {
	key store.Key
	col map[string]any
}

func (r w9aAmendmentRow) text(name string) string {
	v, ok := r.col[name]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// w9aAmendmentRows reads one amendment side table through the store's own
// projection reader. A missing projection is the pre-chunk red for every
// caller and says so in words.
func w9aAmendmentRows(t *testing.T, st *store.Store, projection string) []w9aAmendmentRow {
	t.Helper()
	rows, err := dumpSection[map[string]any](st, projection)
	if err != nil {
		t.Fatalf("W9a: reading projection %q: %v.\n"+
			"F4 §3.2 requires STRICT, INSERT-only side tables `%s`/`%s` with dump-participating projections. "+
			"On the pre-chunk tree neither exists, and THIS is the produced red",
			projection, err, w9aTblTripAmendment, w9aTblArrivalAmendment)
	}
	out := make([]w9aAmendmentRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, w9aAmendmentRow{key: w9aKeyOf(t, r), col: r})
	}
	return out
}

func w9aKeyOf(t *testing.T, row map[string]any) store.Key {
	t.Helper()
	num := func(name string) int64 {
		switch v := row[name].(type) {
		case float64:
			return int64(v)
		case int64:
			return v
		}
		t.Fatalf("W9a: side-table row has no numeric %q column: %v", name, row)
		return 0
	}
	site, _ := row["site_id"].(string)
	return store.Key{SiteID: site, NodeEpoch: num("node_epoch"), Seq: num("seq")}
}

// w9aFact returns the FactOutcome for one fact of the event at k. A
// cannot-match is a FAILURE, never a skip (T-A precondition 2 — the shape
// w3_seams_test.go bound).
func w9aFact(t *testing.T, st *store.Store, k store.Key, fact string) FactOutcome {
	t.Helper()
	outs, err := Outcome(st, k)
	if err != nil {
		t.Fatalf("W9a: Outcome(%v): %v.\n"+
			"F4 §3.3 registers `%s` and `%s` in VF rev 2's carrier registry as the SIXTH and SEVENTH carriers. "+
			"Until they are registered this is an unregistered-carrier error, and THAT is the produced red",
			k, err, w9aEvtTripAmended, w9aEvtArrivalAmended)
	}
	for _, o := range outs {
		if o.Fact == fact {
			return o
		}
	}
	t.Fatalf("W9a: Outcome(%v) reported no %q fact (got %+v) — cannot-match is a FAILURE, never a skip (T-A precondition 2). "+
		"F4 §6: `occupant/<person_id>` is a fact-name PATTERN, so `carrierRegistration.Facts` cannot stay a static []string",
		k, fact, outs)
	return FactOutcome{}
}

// w9aInsertAmendmentRow is W9a-17's DIVERGENT WRITER: it writes one row
// straight into `trip_amendment`, bypassing every authoring refusal. The
// column set is deliberately minimal — the PK triple, the trip, and
// whichever payload columns the caller names — so the INSERT names only
// columns SEAM W9A-S-COLS (w9a_sidetable_structural_test.go) requires to
// exist.
func w9aInsertAmendmentRow(t *testing.T, db *sql.DB, siteID string, epoch, seq int64, tripID string, cols map[string]any) {
	t.Helper()
	names := []string{"site_id", "node_epoch", "seq", w9aKeyTripID}
	args := []any{siteID, epoch, seq, tripID}
	for k, v := range cols {
		names = append(names, k)
		args = append(args, v)
	}
	ph := make([]string, len(names))
	for i := range ph {
		ph[i] = "?"
	}
	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", w9aTblTripAmendment, strings.Join(names, ", "), strings.Join(ph, ", "))
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("W9a-17: the divergent writer could not insert into %s: %v.\n"+
			"Query: %s\n"+
			"On the pre-chunk tree the table does not exist and THIS is the produced red. After the build, a failure here means the "+
			"column set differs from SEAM W9A-S-COLS — take it to the gate", w9aTblTripAmendment, err, q)
	}
}
