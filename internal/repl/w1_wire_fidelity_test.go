// w1_wire_fidelity_test.go — W1-6: the storage-and-wire fidelity round-trip
// (TP §7.2, W1's half): "the parsed typed value stored byte-exactly at A,
// queryable byte-exactly at B after replication; `entered` in, `entered`
// stored, `entered` at B; fixture includes a prior `default` event with a
// LATER OccurredAt than the entered event (the wire-crossing inversion —
// doubling as W1-4's watermark probe); the green is a storage/transport
// claim ONLY, never a rendering claim." (WINDOW-BUILD-PLAN-v1 §3 W1, LOCKED
// 2026-08-27.)
//
// The within-allowance fixture is W2's, NOT here — the W1/W2 boundary seam
// recorded at criteria lock (TP §7.2's within-allowance row certifies at
// W2-4 / mTP-2).
//
// ⚠ COMPILE-BLOCKED PENDING BUILD (the pair-bearing Store.Author + the new
// Envelope fields). The presumed Author spelling is the same one
// internal/store/w1_pairauthor_test.go binds; this file repeats it in ONE
// local helper for the same gate-adjudicable reason.
//
// KILLER MUTANTS: m8 — the discard mutant (`entered` stored with
// OccurredAt = AuthoredAt, the typed value thrown away); m9 — apply-path
// re-stamp (B overwrites the pair/basis from its own clock at apply). B's
// clock is pinned to a DIFFERENT instant than A's and than the typed value
// (the discriminating precondition PLAN-SEC-A §6 demands), so either mutant
// produces a visible inequality.
package repl

import (
	"context"
	"encoding/json"
	"testing"

	"sitecontrol/internal/store"
)

func w1ReplAuthor(t *testing.T, st *store.Store, siteID string, epoch int64, payload json.RawMessage, occurredAtMS int64, timeSource string) store.Envelope {
	t.Helper()
	env, err := st.Author(siteID, epoch, payload, occurredAtMS, timeSource)
	if err != nil {
		t.Fatalf("author (%s, occurred=%d, source=%q): %v", siteID, occurredAtMS, timeSource, err)
	}
	return env
}

func TestW1EnteredTimeRoundTripsStorageAndWireByteExactly(t *testing.T) {
	sites := newMesh(t, 2)
	a, b := sites[0], sites[1]

	const (
		aWall = int64(1_800_000_000_000)
		bWall = int64(1_900_000_000_000) // B's clock differs from A's — the re-stamp discriminator
		typed = int64(1_700_000_000_123) // the officer's typed PAST instant
	)
	a.st.Clock = func() store.ClockReading {
		return store.ClockReading{WallMS: aWall, MaxErrorUS: 300, State: store.ClockSynced}
	}
	b.st.Clock = func() store.ClockReading {
		return store.ClockReading{WallMS: bWall, MaxErrorUS: 5, State: store.ClockSynced}
	}

	// Fixture, at A: FIRST a default event (rule W stamps it at aWall — the
	// LATER OccurredAt), THEN the entered event carrying the typed PAST
	// value. Replicated in stream order, the entered past time crosses the
	// wire AFTER a later-occurring event: the wire-crossing inversion.
	defEnv := w1ReplAuthor(t, a.st, "site-a", 1, []byte(`{"type":"tick","k":"prior-default"}`), 0, "default")
	entEnv := w1ReplAuthor(t, a.st, "site-a", 1, []byte(`{"type":"tick","k":"entered-past"}`), typed, "entered")

	// Fidelity AT A (accept-path byte-exactness, TP §7.2): the typed value
	// is the stored value — not clamped, not discarded, not nudged.
	if entEnv.OccurredAt != typed {
		t.Fatalf("W1-6: A stored OccurredAt=%d for the entered event, want the typed %d exactly (mutant m8's clamp half)", entEnv.OccurredAt, typed)
	}
	if entEnv.TimeSource != "entered" {
		t.Fatalf("W1-6: A stored TimeSource=%q, want \"entered\"", entEnv.TimeSource)
	}
	if entEnv.OccurredAt == entEnv.AuthoredAt.WallMS {
		t.Fatal("W1-6 fixture defect: typed value equals the authoring stamp — the discard mutant (m8) would be indistinguishable from fidelity")
	}
	if defEnv.OccurredAt <= entEnv.OccurredAt {
		t.Fatalf("W1-6 fixture defect: the prior default event must carry a LATER OccurredAt (%d) than the entered event (%d) — the wire-crossing inversion is mandatory", defEnv.OccurredAt, entEnv.OccurredAt)
	}

	// Across the real wire: B pulls A.
	b.eng.PullAll(context.Background())

	// The inversion crossed and the watermark advanced past it (W1-4's probe).
	wm, err := b.st.Watermark("site-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if wm != 2 {
		t.Fatalf("W1-6: B's watermark(site-a,1)=%d, want 2 — the entered-past event must replicate as an ordinary event, ordering untouched", wm)
	}

	evs, err := b.st.Events("site-a", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("B holds %d events of site-a's stream, want 2", len(evs))
	}
	gotDef, gotEnt := evs[0], evs[1]

	// `entered` in, `entered` stored, `entered` at B — value byte-exact.
	if gotEnt.OccurredAt != typed || gotEnt.TimeSource != "entered" {
		t.Fatalf("W1-6: at B the entered event reads (occ=%d, source=%q), want (%d, entered) — the typed value must survive transport untouched (mutant m8)", gotEnt.OccurredAt, gotEnt.TimeSource, typed)
	}
	// Never re-stamped at apply (m9): every one of the five values B holds
	// equals what A sent — and cannot equal B's own clock, which was pinned
	// elsewhere.
	for _, pair := range []struct {
		name string
		sent store.Envelope
		got  store.Envelope
	}{{"default event", defEnv, gotDef}, {"entered event", entEnv, gotEnt}} {
		if pair.got.AuthoredAt.WallMS != pair.sent.AuthoredAt.WallMS ||
			pair.got.AuthoredAt.MaxErrorUS != pair.sent.AuthoredAt.MaxErrorUS ||
			pair.got.AuthoredAt.State != pair.sent.AuthoredAt.State ||
			pair.got.OccurredAt != pair.sent.OccurredAt ||
			pair.got.TimeSource != pair.sent.TimeSource {
			t.Fatalf("W1-6/m9: %s re-stamped in transit/apply: B holds (auth=%d/%d/%s occ=%d src=%q), A sent (auth=%d/%d/%s occ=%d src=%q)",
				pair.name,
				pair.got.AuthoredAt.WallMS, pair.got.AuthoredAt.MaxErrorUS, pair.got.AuthoredAt.State, pair.got.OccurredAt, pair.got.TimeSource,
				pair.sent.AuthoredAt.WallMS, pair.sent.AuthoredAt.MaxErrorUS, pair.sent.AuthoredAt.State, pair.sent.OccurredAt, pair.sent.TimeSource)
		}
		if pair.got.AuthoredAt.WallMS == bWall {
			t.Fatalf("W1-6/m9: %s carries B's local clock reading as its authoring time — the apply path re-stamped a peer's event", pair.name)
		}
		// Hash equality is the transport-fidelity seal: with all five values
		// inside Hash() (W1-1), any in-flight rewrite disagrees here.
		if pair.got.Hash() != pair.sent.Hash() {
			t.Fatalf("W1-6: %s hash differs across the wire (A %s, B %s) — some hashed value was rewritten in transit", pair.name, pair.sent.Hash(), pair.got.Hash())
		}
	}
	// NOTE (locked scope): this green is a STORAGE/TRANSPORT claim only. It
	// says nothing about how any surface renders `entered` — the
	// render-as-claim test is TP §7's tracked debt 1, owed at the first
	// consumer build, and must never be cited against this test's green.
}
