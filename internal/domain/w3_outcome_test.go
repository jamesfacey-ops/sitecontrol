// w3_outcome_test.go — W3-9 (per-fact outcome, VF T8), W3-10
// (merge-induced supersession, VF T9b / §6 case 5), W3-16 (the deferred
// case at rest, VF T7's shape on W3's carrier), W3-12 (carrier registry
// honest, with the planted unregistered type as its own control), and
// W3-11 (one ordering spec: store.Key.Less truth-table + the T-A
// agreement oracle over W3's fixtures).
// design/WINDOW-BUILD-PLAN-v1.md §3 W3, criteria LOCKED 2026-08-27.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD (Outcome, FactOutcome, Status,
// store.Key.Less, IsRegisteredCarrier, AmendPerson) — that IS the
// clean-tree red.
//
// KILLER MUTANTS: m15 (event-level outcome), m16 (same-target-only
// resolver), m22 (status stamped at write), m18 (unregistered carrier →
// empty success; the planted type doubles as the control), m17
// (comparator/ordering drift), m5 (epoch-blind comparator — via the
// truth-table AND the cross-epoch fixture under the oracle).
package domain

import (
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// TestW3PerFactOutcome — W3-9 (VF T8): site-a amends {phone, name};
// site-b later-KEY amends {phone} with an EARLIER clock (clock contradicts
// key, T1's discipline). Outcome(site-a's event) = phone Superseded /
// name Effective, and for the Effective fact Winner == the queried key.
func TestW3PerFactOutcome(t *testing.T) {
	const phoneA, phoneB, nameA = "555-0401", "555-0402", "Per Fact Amended"
	st := w3NewStore(t)
	x, _, roster := w3RosterPair(t, st, "Per Fact X", "Per Fact Y")
	ea := w3AmendLocal(t, st, "site-a", 1, w3Base+2*w3Hour,
		PersonAmendment{PersonID: x, Phone: w3Str(phoneA), Name: w3Str(nameA)})
	eb := w3AuthorRemote(t, roster, "site-b", 1, w3Base+1*w3Hour, func(rs *store.Store) {
		if _, err := AmendPerson(rs, "site-b", 1, PersonAmendment{PersonID: x, Phone: w3Str(phoneB)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3ApplyAll(t, st, eb)

	phone := w3Fact(t, st, ea.Key(), w3FactPhone)
	if phone.Status != Superseded {
		t.Fatalf("W3-9 (m15/m1's shape): site-a's phone assertion is %v, want Superseded — site-b's later-KEY assertion wins even though its clock is EARLIER", phone.Status)
	}
	if phone.Winner != eb[0].Key() {
		t.Fatalf("W3-9: phone Winner = %v, want site-b's assertion key %v", phone.Winner, eb[0].Key())
	}
	// The registry's canonical-Asserted good (SEAM W3-S-FACTS): the
	// rendering of what THIS event claimed must carry the claimed value.
	if !strings.Contains(phone.Asserted, phoneA) {
		t.Fatalf("W3-12's registration good via W3-9: the phone fact's Asserted rendering %q does not carry the claimed value %q", phone.Asserted, phoneA)
	}
	name := w3Fact(t, st, ea.Key(), w3FactName)
	if name.Status != Effective {
		t.Fatalf("W3-9 (m15, event-level outcome): the name fact of the SAME event is %v, want Effective — a whole-event verdict falsely tells the officer their name fix failed too", name.Status)
	}
	if name.Winner != ea.Key() {
		t.Fatalf("W3-9: Effective name Winner = %v, want the queried key %v (the VF §4.2 convention, asserted)", name.Winner, ea.Key())
	}
}

// w3MergeSupersessionFixture is W3-10's fixture, shared with the T-A
// oracle run (W3-11).
type w3MergeSupersessionFixture struct {
	st     *store.Store
	x, y   string
	xPhone store.Envelope // X's phone assertion (the loser)
	yPhone store.Envelope // Y's LATER-keyed phone assertion (the winner)
	merge  store.Envelope // the joining same_as merge
	phoneX string
	phoneY string
}

func w3BuildMergeSupersession(t *testing.T) w3MergeSupersessionFixture {
	t.Helper()
	fx := w3MergeSupersessionFixture{phoneX: "555-0501", phoneY: "555-0502"}
	fx.st = w3NewStore(t)
	fx.x, fx.y, _ = w3RosterPair(t, fx.st, "Merge Sup X", "Merge Sup Y")
	fx.xPhone = w3AmendLocal(t, fx.st, "site-a", 1, w3Base+1*w3Hour,
		PersonAmendment{PersonID: fx.x, Phone: w3Str(fx.phoneX)})
	fx.yPhone = w3AmendLocal(t, fx.st, "site-a", 1, w3Base+2*w3Hour,
		PersonAmendment{PersonID: fx.y, Phone: w3Str(fx.phoneY)})
	fx.merge = w3AmendLocal(t, fx.st, "site-a", 1, w3Base+3*w3Hour,
		PersonAmendment{PersonID: fx.x, SameAs: w3Str(fx.y)})
	if !fx.xPhone.Key().Less(fx.yPhone.Key()) {
		t.Fatalf("fixture defect: Y's phone assertion %v is not later-keyed than X's %v", fx.yPhone.Key(), fx.xPhone.Key())
	}
	return fx
}

// TestW3MergeInducedSupersession — W3-10 (VF T9b / §6 case 5): X's phone
// assertion becomes Superseded by an event that never targeted X, with
// Winner = Y's assertion key, and the case-5 vocabulary is renderable
// from the result (the winner's key here; the joining merge identified by
// its own Effective same_as outcome).
func TestW3MergeInducedSupersession(t *testing.T) {
	fx := w3BuildMergeSupersession(t)
	out := w3Fact(t, fx.st, fx.xPhone.Key(), w3FactPhone)
	if out.Status != Superseded {
		t.Fatalf("W3-10 (m16, same-target-only resolver): X's phone assertion is %v, want Superseded — a resolver that only compares assertions targeting the same original record reports X's dead value Effective forever", out.Status)
	}
	if out.Winner != fx.yPhone.Key() {
		t.Fatalf("W3-10: Winner = %v, want Y's assertion key %v (the event that never targeted X)", out.Winner, fx.yPhone.Key())
	}
	// Case-5 renderability: the joining merge is identifiable from the
	// composed state — its own outcome reports same_as Effective at its key.
	mo := w3Fact(t, fx.st, fx.merge.Key(), w3FactSameAs)
	if mo.Status != Effective || mo.Winner != fx.merge.Key() {
		t.Fatalf("W3-10: the joining merge's same_as outcome is (%v, winner %v), want (Effective, %v) — without it the §6 case-5 vocabulary (\"superseded via the merge made at ⟨post⟩\") is not renderable", mo.Status, mo.Winner, fx.merge.Key())
	}
}

// TestW3DeferredCaseAtRest — W3-16 (VF T7's shape on W3's carrier): append
// the local merge and un-merge, THEN apply the peer's winning assertion
// via the wire/fold path with NO POST at either end; Outcome(local
// un-merge) = Superseded at the next read. Behavioural tooth of §4.3
// rule 2 (status computed at read); W3-13 is its structural tooth — each
// mutant survives the other's test.
func TestW3DeferredCaseAtRest(t *testing.T) {
	st := w3NewStore(t)
	x, y, roster := w3RosterPair(t, st, "Deferred X", "Deferred Y")
	z, err := CreatePerson(st, "site-a", 1, "Deferred Z", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	roster = w3Events(t, st, "site-a", 1)
	w3AmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour,
		PersonAmendment{PersonID: x, SameAs: w3Str(y)})
	unmerge := w3AmendLocal(t, st, "site-a", 1, w3Base+2*w3Hour,
		PersonAmendment{PersonID: x, Clear: []string{w3FactSameAs}})
	// At this instant the local un-merge IS effective — a status stamped
	// now (m22) is what this test exists to refuse.
	if got := w3Fact(t, st, unmerge.Key(), w3FactSameAs); got.Status != Effective {
		t.Fatalf("W3-16 fixture: the local un-merge should be Effective before the peer's winner arrives, got %v", got.Status)
	}
	// The peer's winning merge arrives by wire/fold — no POST anywhere.
	peer := w3AuthorRemote(t, roster, "site-b", 1, w3Base+90*60_000, func(rs *store.Store) {
		if _, err := AmendPerson(rs, "site-b", 1, PersonAmendment{PersonID: x, SameAs: w3Str(z.PersonID)}, 0, "default"); err != nil {
			t.Fatal(err)
		}
	})
	w3ApplyAll(t, st, peer)

	got := w3Fact(t, st, unmerge.Key(), w3FactSameAs)
	if got.Status != Superseded {
		t.Fatalf("W3-16 (m22, status stamped at write): after the peer's winning merge folded in, Outcome(local un-merge) = %v, want Superseded — supersession is computed at READ, never stored", got.Status)
	}
	if got.Winner != peer[0].Key() {
		t.Fatalf("W3-16: Winner = %v, want the peer's merge key %v", got.Winner, peer[0].Key())
	}
	if p := w3Person(t, st, x); p.SameAs == nil || *p.SameAs != z.PersonID {
		t.Fatalf("W3-16: composed same_as = %v, want the peer's winning target %s", p.SameAs, z.PersonID)
	}
}

// --- W3-12: the carrier registry, honest -------------------------------

const w3PlantedCarrierType = "gadget_amended" // carrier-CLASS name, never registered

// TestW3OutcomeUnregisteredCarrierErrors — W3-12's API half: Outcome on an
// unregistered event type returns a non-nil error, NEVER ([]FactOutcome{},
// nil) (VF §4.2: "an empty successful result does not exist in this API").
func TestW3OutcomeUnregisteredCarrierErrors(t *testing.T) {
	st := w3NewStore(t)
	w3RosterPair(t, st, "Registry X", "Registry Y")
	planted := w3WireAmendment(t, "site-b", 1, 1, w3Base, "person-nobody", nil)
	planted.Payload = []byte(`{"type":"` + w3PlantedCarrierType + `","person_id":"person-nobody"}`)
	w3Apply(t, st, planted)

	outs, err := Outcome(st, planted.Key())
	if err == nil {
		t.Fatalf("W3-12 (m18): Outcome on unregistered carrier %q returned (%v, nil) — an empty success is the silent-OK shipped inside the mechanism built to kill it; it must be a non-nil error", w3PlantedCarrierType, outs)
	}
}

// w3AmendmentFamilyTypes enumerates the distinct payload types present in
// the given streams of the live log, filtered to the amendment/correction
// carrier classes — the `_amended`/`_corrected` families (VF §4.4's own
// scoping; this classification is stated ONCE, at W3-12; W4-13(a) cites
// it). The inventory source is the LOG, never a hand-kept list.
func w3AmendmentFamilyTypes(t *testing.T, st *store.Store, streams [][2]interface{}) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, s := range streams {
		site := s[0].(string)
		epoch := s[1].(int64)
		for _, e := range w3Events(t, st, site, epoch) {
			typ := w2TypeOf(t, e)
			if strings.HasSuffix(typ, "_amended") || strings.HasSuffix(typ, "_corrected") {
				seen[typ] = true
			}
		}
	}
	var out []string
	for typ := range seen {
		out = append(out, typ)
	}
	sort.Strings(out)
	return out
}

// w2TypeOf extracts the payload type (domain-package twin of the web
// harness's w2PayloadType).
func w2TypeOf(t *testing.T, e store.Envelope) string {
	t.Helper()
	s := string(e.Payload)
	const marker = `"type":"`
	i := strings.Index(s, marker)
	if i < 0 {
		t.Fatalf("stored payload carries no type: %s", s)
	}
	rest := s[i+len(marker):]
	j := strings.Index(rest, `"`)
	return rest[:j]
}

// TestW3CarrierCoverage — W3-12's coverage half: the filtered inventory is
// checked against the registry with an EMPTY exemption list (the named
// exemption rows arrive with W9's retraction types — VF §7.7), and the
// check first proves it detects a PLANTED unregistered type (m18's
// control) before its clean pass counts.
func TestW3CarrierCoverage(t *testing.T) {
	w3ExemptNonCarriers := []string{} // EMPTY at W3 — ref_alias_created is a NON-carrier the filter never enumerates (it dies at W8 per P-3 §5)

	uncovered := func(st *store.Store, streams [][2]interface{}) []string {
		var out []string
		for _, typ := range w3AmendmentFamilyTypes(t, st, streams) {
			exempt := false
			for _, e := range w3ExemptNonCarriers {
				if e == typ {
					exempt = true
				}
			}
			if !exempt && !IsRegisteredCarrier(typ) {
				out = append(out, typ)
			}
		}
		return out
	}

	// CONTROL FIRST (§5's CONTROL row): the planted unregistered type must
	// be detected before any clean pass counts.
	planted := w3NewStore(t)
	w3RosterPair(t, planted, "Coverage X", "Coverage Y")
	pl := w3WireAmendment(t, "site-b", 1, 1, w3Base, "person-nobody", nil)
	pl.Payload = []byte(`{"type":"` + w3PlantedCarrierType + `","person_id":"person-nobody"}`)
	w3Apply(t, planted, pl)
	ctl := uncovered(planted, [][2]interface{}{{"site-a", int64(1)}, {"site-b", int64(1)}})
	if len(ctl) != 1 || ctl[0] != w3PlantedCarrierType {
		t.Fatalf("W3-12 CONTROL FAILED: the coverage check did not flag the planted unregistered carrier %q (got %v) — a scan that cannot detect its plant proves nothing (m18)", w3PlantedCarrierType, ctl)
	}

	// The clean pass: a live log holding a real person_amended.
	clean := w3NewStore(t)
	x, _, _ := w3RosterPair(t, clean, "Coverage A", "Coverage B")
	w3AmendLocal(t, clean, "site-a", 1, w3Base+1*w3Hour,
		PersonAmendment{PersonID: x, Phone: w3Str("555-0512")})
	inv := w3AmendmentFamilyTypes(t, clean, [][2]interface{}{{"site-a", int64(1)}})
	if len(inv) != 1 || inv[0] != w3EvtPersonAmended {
		t.Fatalf("W3-12: filtered inventory = %v, want exactly [%q] at W3 build time — an empty inventory makes this test vacuous; extra types belong to later chunks", inv, w3EvtPersonAmended)
	}
	if got := uncovered(clean, [][2]interface{}{{"site-a", int64(1)}}); len(got) != 0 {
		t.Fatalf("W3-12: unregistered amendment/correction carriers in the live log: %v — person_amended must be registered with the three §4.4 goods", got)
	}
}

// --- W3-11: one ordering spec, two implementations ----------------------

// TestW3KeyLessTruthTable — the Go half of the one ordering spec:
// store.Key.Less is lexicographic (SiteID, NodeEpoch, Seq), epoch-major.
// Pinned literals; every named rival comparator (seq-major, (site_id,
// seq)-only) is recomputed in-test on the discriminating pair and MUST
// disagree (standing rule 3, runtime-checked).
func TestW3KeyLessTruthTable(t *testing.T) {
	seqMajor := func(a, b store.Key) bool {
		if a.Seq != b.Seq {
			return a.Seq < b.Seq
		}
		return a.SiteID < b.SiteID
	}
	pairOnly := func(a, b store.Key) bool {
		if a.SiteID != b.SiteID {
			return a.SiteID < b.SiteID
		}
		return a.Seq < b.Seq
	}

	lowEpochHighSeq := store.Key{SiteID: "site-a", NodeEpoch: 1, Seq: 900}
	highEpochLowSeq := store.Key{SiteID: "site-a", NodeEpoch: 2, Seq: 3}
	// Non-degeneracy: both epoch-blind rivals must call this pair the
	// OTHER way, or the pair discriminates nothing.
	if seqMajor(lowEpochHighSeq, highEpochLowSeq) || pairOnly(lowEpochHighSeq, highEpochLowSeq) {
		t.Fatalf("W3-11 FIXTURE DEFECT: the epoch-blind rivals agree with epoch-major on the cross-epoch pair — the fixture cannot kill m5")
	}
	if !lowEpochHighSeq.Less(highEpochLowSeq) {
		t.Fatalf("W3-11 (m5, epoch-blind): Key%v.Less(Key%v) = false — epoch-major means epoch N+1/LOW seq beats epoch N/HIGH seq; keying that ignores the epoch \"would silently drop an entire season of records\" (envelope.go:44–47)", lowEpochHighSeq, highEpochLowSeq)
	}
	if highEpochLowSeq.Less(lowEpochHighSeq) {
		t.Fatalf("W3-11: Less is not antisymmetric on the cross-epoch pair")
	}

	siteAHigh := store.Key{SiteID: "site-a", NodeEpoch: 9, Seq: 900}
	siteBLow := store.Key{SiteID: "site-b", NodeEpoch: 1, Seq: 1}
	if !siteAHigh.Less(siteBLow) || siteBLow.Less(siteAHigh) {
		t.Fatalf("W3-11 (m17): site_id must compare FIRST — %v vs %v ordered wrongly (\"site-b\" > \"site-a\" decides regardless of epoch/seq)", siteAHigh, siteBLow)
	}

	early := store.Key{SiteID: "site-a", NodeEpoch: 1, Seq: 1}
	late := store.Key{SiteID: "site-a", NodeEpoch: 1, Seq: 2}
	if !early.Less(late) || late.Less(early) {
		t.Fatalf("W3-11: seq must order within a (site, epoch) stream")
	}
	if early.Less(early) {
		t.Fatalf("W3-11: Less must be irreflexive")
	}
}

// TestW3TAOracleOverFixtures — W3-11's T-A agreement oracle, run over the
// three mandated fixture families: W3-1's same-site cross-epoch pair,
// W3-4's contested-merge fixture, and W3-10's merge fixture — with
// pairwise-distinct values per fact and cannot-match = failure, never
// skip (both preconditions asserted at runtime inside w3RunTAOracle;
// the ledger's T-A validity-preconditions CONTROL row is discharged by
// those runtime guards). Kills m17 and m5.
func TestW3TAOracleOverFixtures(t *testing.T) {
	t.Run("cross-epoch (W3-1)", func(t *testing.T) {
		fx := w3BuildCrossEpochFixture(t)
		w3RunTAOracle(t, fx.st, []w3TAFixture{
			{fx.e1a.Key(), fx.personID, w3FactPhone, fx.phoneA1},
			{fx.e1b.Key(), fx.personID, w3FactPhone, fx.phoneA2},
			{fx.e2.Key(), fx.personID, w3FactPhone, fx.phoneB},
		})
	})
	t.Run("contested merge (W3-4)", func(t *testing.T) {
		fx := w3BuildContestedMerge(t)
		w3RunTAOracle(t, fx.st, []w3TAFixture{
			{fx.mergeToY.Key(), fx.x, w3FactSameAs, fx.y},
			{fx.mergeToZ.Key(), fx.x, w3FactSameAs, fx.z},
		})
	})
	t.Run("merge supersession (W3-10)", func(t *testing.T) {
		fx := w3BuildMergeSupersession(t)
		w3RunTAOracle(t, fx.st, []w3TAFixture{
			{fx.xPhone.Key(), fx.x, w3FactPhone, fx.phoneX},
			{fx.yPhone.Key(), fx.y, w3FactPhone, fx.phoneY},
			{fx.merge.Key(), fx.x, w3FactSameAs, fx.y},
		})
	})
}
