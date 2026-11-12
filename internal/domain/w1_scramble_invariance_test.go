// w1_scramble_invariance_test.go — W1-4 and W1-11 (WINDOW-BUILD-PLAN-v1 §3
// W1, LOCKED 2026-08-27; TP §7.1a; D-28 Part B: the clock orders NOTHING).
//
// W1-4: scramble-invariance — permute the new time-basis values across a
// fixed multi-site stream; fold output, ordering, and watermarks
// byte-identical under every permutation. The fixture carries rev 3's THREE
// mandatory properties: (P1) a cross-site (node_epoch, seq) tie whose
// OccurredAt order contradicts trip_id order; (P2) a wire-crossing
// OccurredAt inversion (an entered past time applied after later-authored
// events — doubling as the watermark probe); (P3) a contested-fact tie —
// two person_amended events asserting the same fact whose OccurredAt order
// contradicts the fold's winner (decided by resolveContest: dominance, then
// — neither side here stamps `seen`, so both are genuinely concurrent — the
// greater (site_id, node_epoch, seq) key, never clock), the winner
// byte-identical AND PINNED under every permutation. ⟦was: two
// weather_phase_declared events tied on board time, the winner read through
// domain/weather.go CurrentPhase — weather phases removed, James 2026-10-04;
// person_amended is the same resolveContest ([[D-92]]) carrier, so the
// property P3 pins is unchanged, only the carrier moved⟧ ⟦was: "the fold's winner (decided by
// phase, then key — domain/weather.go phaseDeclLess)" — phaseDeclLess is
// STRUCK (SchemaVersion 11, [[D-92]], ruling 2026-09-08 (G14)); re-ruled,
// not silently edited⟧
//
// W1-11: the §5 standing cross-epoch CONTROL — a same-site pair where epoch
// N+1 / LOW seq contests epoch N / HIGH seq and the resolved winner is
// PINNED to the epoch-major outcome. Invariance alone cannot catch an
// epoch-blind comparator: it is perfectly invariant and perfectly wrong.
// The stake is the repo's own sentence: pair-keying "would dedup a re-imaged
// site's fresh low sequence numbers as already-applied and silently drop an
// entire season of records" (internal/store/envelope.go:44–47).
//
// ⚠ COMPILE-BLOCKED PENDING BUILD (the new Envelope fields).
//
// KILLER MUTANTS: m5 — a fold comparator reading OccurredAt/TimeSource
// (any permutation then shifts a dump byte or a winner); mTP-1 — a fold
// comparator breaking the contested-fact tie by OccurredAt (the P3
// pinned winner flips to the "tie-loser" amendment, red even unpermuted);
// m7 — epoch-blind ordering ((site_id, seq)-only or seq-major), which the
// W1-11 pinned winner catches: seq-major picks epoch 1/seq 5 over the real
// winner epoch 2/seq 1.
package domain

import (
	"bytes"
	"fmt"
	"testing"

	"sitecontrol/internal/store"
)

// w1TimeBasis is one event's five permutable values.
type w1TimeBasis struct {
	authWallMS   int64
	authMaxErrUS int64
	authState    store.ClockState
	occurredAt   int64
	timeSource   string
}

// w1Fixture is one delivery-ordered fixture event.
type w1Fixture struct {
	site    string
	epoch   int64
	seq     int64
	payload []byte
	basis   w1TimeBasis
}

func w1BuildEnvelope(f w1Fixture, b w1TimeBasis) store.Envelope {
	env := store.Envelope{
		SiteID: f.site, NodeEpoch: f.epoch, Seq: f.seq,
		SchemaVersion: store.SchemaVersion, OriginAuth: f.site,
		Payload: f.payload,
	}
	env.AuthoredAt.WallMS = b.authWallMS
	env.AuthoredAt.MaxErrorUS = b.authMaxErrUS
	env.AuthoredAt.State = b.authState
	env.OccurredAt = b.occurredAt
	env.TimeSource = b.timeSource
	return env
}

// w1ApplyPermuted folds the fixture into a fresh store with the time bases
// permuted by perm (perm[i] = index of the basis event i receives).
func w1ApplyPermuted(t *testing.T, fx []w1Fixture, perm []int) *store.Store {
	t.Helper()
	st := newDomainTestStore(t)
	for i, f := range fx {
		env := w1BuildEnvelope(f, fx[perm[i]].basis)
		if applied, err := st.Apply(env); err != nil {
			t.Fatalf("apply %v under perm %v: %v", env.Key(), perm, err)
		} else if !applied {
			t.Fatalf("apply %v under perm %v: unexpectedly deduped", env.Key(), perm)
		}
	}
	return st
}

func w1Perms(n int) [][]int {
	identity := make([]int, n)
	rot1 := make([]int, n)
	rot2 := make([]int, n)
	rev := make([]int, n)
	for i := 0; i < n; i++ {
		identity[i] = i
		rot1[i] = (i + 1) % n
		rot2[i] = (i + 2) % n
		rev[i] = n - 1 - i
	}
	return [][]int{identity, rot1, rot2, rev}
}

func strptr(s string) *string { return &s }

func TestW1ScrambleInvarianceAcrossTheThreeMandatoryFixtureProperties(t *testing.T) {
	dest := "site-b"
	desta := "site-a"
	fx := []w1Fixture{
		// P1 half 1: site-a (1,1), trip_id sorts LAST ("zz…"), OccurredAt EARLIEST
		// of the pair — OccurredAt order contradicts trip_id order.
		{"site-a", 1, 1, mustJSON(t, TripDispatched{
			// ⟦LOCK — W7 seam repair, 2026-09-03⟧ ⟦was: "VehicleEntered: \"TRUCK 9\","⟧
			Type: "trip_dispatched", TripID: "zz-trip-w1-0001", VehicleEntered: strptr("TRUCK 9"),
			DriverPersonID: "w1-person-a1", DestinationSite: &dest,
		}), w1TimeBasis{100_000, 250, store.ClockSynced, 100_000, "default"}},
		// P1 half 2: site-b (1,1) — the CROSS-SITE (node_epoch, seq) TIE —
		// trip_id sorts FIRST ("aa…"), OccurredAt LATER.
		{"site-b", 1, 1, mustJSON(t, TripDispatched{
			// ⟦LOCK — W7 seam repair, 2026-09-03⟧ ⟦was: "VehicleEntered: \"TRUCK 4\","⟧
			Type: "trip_dispatched", TripID: "aa-trip-w1-0001", VehicleEntered: strptr("TRUCK 4"),
			DriverPersonID: "w1-person-b1", DestinationSite: &desta,
		}), w1TimeBasis{200_000, 500, store.ClockUnsynced, 900_000, "entered"}},
		// P3 half 1: an amendment of the P2 person's phone, OccurredAt
		// LATEST of the tie — an OccurredAt-breaking comparator would crown
		// THIS loser. ⟦was: a weather_phase_declared (phase 2) at this key —
		// weather phases removed, James 2026-10-04⟧
		{"site-a", 1, 2, mustJSON(t, PersonAmended{
			Type: "person_amended", PersonID: "site-b-person-w1-late", Phone: strptr("tie-loser"),
		}), w1TimeBasis{300_000, 300, store.ClockSynced, 950_000, "entered"}},
		// P3 half 2: the same fact amended concurrently — the real winner
		// under resolveContest's greatest-key tiebreak (site-b's key >
		// site-a's; neither amendment stamps `seen`, so both are genuinely
		// concurrent) — with the EARLIER OccurredAt, contradicting that
		// winner. ⟦was: a weather_phase_declared (phase 3) at this key⟧
		{"site-b", 1, 2, mustJSON(t, PersonAmended{
			Type: "person_amended", PersonID: "site-b-person-w1-late", Phone: strptr("tie-winner"),
		}), w1TimeBasis{250_000, 100, store.ClockSynced, 120_000, "entered"}},
		// P2: the wire-crossing inversion — an ENTERED far-past OccurredAt on
		// the event applied LAST, after every later-authored event. Doubles
		// as the watermark probe: the watermark must advance straight past it.
		{"site-b", 1, 3, mustJSON(t, PersonCreated{
			Type: "person_created", PersonID: "site-b-person-w1-late", Name: "W1 Wire Crosser",
		}), w1TimeBasis{400_000, 900, store.ClockUnsynced, 5_000, "entered"}},
	}

	var baseDump []byte
	for pi, perm := range w1Perms(len(fx)) {
		st := w1ApplyPermuted(t, fx, perm)
		dump, err := st.DumpProjections()
		if err != nil {
			t.Fatal(err)
		}
		if pi == 0 {
			baseDump = dump
		} else if !bytes.Equal(dump, baseDump) {
			t.Fatalf("W1-4: fold output differs under permutation %v — some fold read the time basis (mutant m5; D-28 Part B: the clock orders nothing)", perm)
		}
		// Watermarks byte-identical (and the P2 inversion crossed: site-b
		// reached 3 with the far-past entered event applied last).
		for _, want := range []struct {
			site string
			ep   int64
			wm   int64
		}{{"site-a", 1, 2}, {"site-b", 1, 3}} {
			wm, err := st.Watermark(want.site, want.ep)
			if err != nil {
				t.Fatal(err)
			}
			if wm != want.wm {
				t.Fatalf("W1-4: watermark(%s,%d)=%d under perm %v, want %d — the watermark consulted the time basis (mutant m5)", want.site, want.ep, wm, perm, want.wm)
			}
		}
		// P3 PINNED winner, asserted as the expected value under EVERY
		// permutation (mutant mTP-1): site-b's amendment, by
		// resolveContest's greatest-key tiebreak, never the
		// OccurredAt-favoured site-a one. ⟦was: read through CurrentPhase,
		// want phase 3 by (site-b,1,2)⟧
		p, err := PersonByID(st, "site-b-person-w1-late")
		if err != nil {
			t.Fatal(err)
		}
		if p == nil || p.Phone == nil || *p.Phone != "tie-winner" {
			t.Fatalf("W1-4/mTP-1: contested-fact winner under perm %v = %+v; want phone \"tie-winner\" from (site-b,1,2) — a comparator broke the tie by OccurredAt", perm, p)
		}
	}
}

// W1-11 — the standing cross-epoch CONTROL, on W1's own suite. Same site,
// epoch 2 / seq 1 contests epoch 1 / seq 5 — neither declaration stamps
// `seen`, so both are genuinely concurrent and resolveContest goes straight
// to the (site, node_epoch, seq) key, epoch-major. ⟦was: two
// weather_phase_declared events sharing board time and phase — weather
// phases removed, James 2026-10-04; the pair is now two person_amended
// events asserting the same fact, the same resolveContest carrier⟧ The
// winner is PINNED; and the fixture's OccurredAt values FAVOUR the loser, so
// an occurred-at comparator and a seq-major comparator both crown the wrong
// row — each a named mutant (m5/mTP-1 and m7). ⟦was: "(same board time, same
// phase, so the weather winner falls through to the (site, node_epoch, seq)
// key — epoch-major)" — phaseDeclLess (clock, then phase, then key) is
// STRUCK (SchemaVersion 11, [[D-92]], G14): resolveContest never consults
// board time or phase; re-ruled, not silently edited⟧
func TestW1SameSiteCrossEpochLowSeqBeatsHighSeqPinnedWinner(t *testing.T) {
	fillers := make([]w1Fixture, 0, 6)
	// Filler 1 is the contested person (⟦was: a fifth company — the weather
	// pair needed no target record⟧); 2-4 stay company fillers.
	fillers = append(fillers, w1Fixture{"site-c", 1, 1, mustJSON(t, PersonCreated{
		Type: "person_created", PersonID: "site-c-person-w1-contested", Name: "W1 Contested",
	}), w1TimeBasis{10_001, 10, store.ClockSynced, 10_001, "default"}})
	for i := int64(2); i <= 4; i++ {
		fillers = append(fillers, w1Fixture{"site-c", 1, i, mustJSON(t, CompanyCreated{
			Type: "company_created", CompanyID: fmt.Sprintf("site-c-company-w1-%d", i), Name: fmt.Sprintf("W1 Filler %d", i),
		}), w1TimeBasis{10_000 + i, 10, store.ClockSynced, 10_000 + i, "default"}})
	}
	fx := append(fillers,
		// epoch 1, HIGH seq — the pre-re-image row. OccurredAt LATEST in the
		// whole fixture: every wrong comparator picks it.
		w1Fixture{"site-c", 1, 5, mustJSON(t, PersonAmended{
			Type: "person_amended", PersonID: "site-c-person-w1-contested", Phone: strptr("epoch1-highseq-loser"),
		}), w1TimeBasis{500_000, 20, store.ClockSynced, 999_000, "entered"}},
		// epoch 2, LOW seq — the re-imaged site's fresh stream. This is the
		// epoch-major winner.
		w1Fixture{"site-c", 2, 1, mustJSON(t, PersonAmended{
			Type: "person_amended", PersonID: "site-c-person-w1-contested", Phone: strptr("epoch2-lowseq-winner"),
		}), w1TimeBasis{600_000, 30, store.ClockSynced, 50_000, "entered"}},
	)

	var baseDump []byte
	for pi, perm := range w1Perms(len(fx)) {
		st := w1ApplyPermuted(t, fx, perm)
		dump, err := st.DumpProjections()
		if err != nil {
			t.Fatal(err)
		}
		if pi == 0 {
			baseDump = dump
		} else if !bytes.Equal(dump, baseDump) {
			t.Fatalf("W1-4 (cross-epoch fixture): fold output differs under permutation %v (mutant m5)", perm)
		}
		p, err := PersonByID(st, "site-c-person-w1-contested")
		if err != nil {
			t.Fatal(err)
		}
		// PINNED, not merely invariant: epoch-major — epoch 2/seq 1 beats
		// epoch 1/seq 5. A seq-major or (site_id, seq)-only comparator picks
		// (1,5); an occurred-at comparator picks (1,5) too (occ 999000 >
		// 50000). Both are red here (mutants m7, m5/mTP-1).
		if p == nil || p.Phone == nil || *p.Phone != "epoch2-lowseq-winner" {
			t.Fatalf("W1-11: cross-epoch winner under perm %v = %+v; want phone \"epoch2-lowseq-winner\" from (site-c, epoch 2, seq 1) — an epoch-blind comparator reverts a re-imaged site's fresh record (mutant m7; envelope.go:44–47's season-loss stake)", perm, p)
		}
		wm1, err := st.Watermark("site-c", 1)
		if err != nil {
			t.Fatal(err)
		}
		wm2, err := st.Watermark("site-c", 2)
		if err != nil {
			t.Fatal(err)
		}
		if wm1 != 5 || wm2 != 1 {
			t.Fatalf("W1-11: watermarks (epoch1=%d, epoch2=%d) under perm %v, want (5, 1) — epoch 2 is a NEW stream, never a duplicate of epoch 1's numbering", wm1, wm2, perm)
		}
	}
}
