// w4_correction_event_test.go — W4-1's behavioural half (the correction is
// a new event; the target row is BYTE-IDENTICAL after N corrections),
// W4-2 (the original and every intermediate remain queryable AT REST —
// full chain, ascending key order, Status-marked, entry zero via the §4.1
// point-read), and W4-6 (the correction's own envelope describes the
// correction ACT per D-44; payload pinned to the five keys; payload bytes
// hash-covered, five applications of m7).
// design/WINDOW-BUILD-PLAN-v1.md "### W4", criteria LOCKED 2026-08-27.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD (CorrectOccurredAt, TimeCorrection,
// ResolveOccurredAt, ResolvedTime, Correction) — that IS the clean-tree
// red, captured via go vet.
//
// KILLER MUTANTS: m1 (apply-as-edit — byte-identity), m4 (chain
// truncation — only the latest returned), m6 (the D-42-literal envelope —
// the act stamped `entered`, or the corrected instant copied into the
// envelope), m7 (a payload field dropped from the hash input — five
// applications, one per pinned key).
package domain

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"sitecontrol/internal/store"
)

// TestW4TargetRowByteIdenticalAfterNCorrections — W4-1 (m1): after THREE
// corrections of one target, the target's stored envelope — every field,
// every payload byte, and its Hash() — is identical to what it was before
// the first correction. Asserted, not assumed; and non-vacuously: the
// corrections must have TAKEN EFFECT on the resolved read, or a build with
// no correction machinery at all would pass this trivially (VF-m4's shape
// guarded in-fixture).
func TestW4TargetRowByteIdenticalAfterNCorrections(t *testing.T) {
	const (
		c1MS = w3Base - 9*w3Hour
		c2MS = w3Base - 8*w3Hour
		c3MS = w3Base - 7*w3Hour
	)
	st := w3NewStore(t)
	target := w4Target(t, st, "site-a", 1, "Byte Identity Target")

	beforeEnv, err := st.Events(target.SiteID, target.NodeEpoch, target.Seq, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(beforeEnv) == 0 || beforeEnv[0].Seq != target.Seq {
		t.Fatalf("fixture defect: target row unreadable before corrections")
	}
	wantHash := beforeEnv[0].Hash()

	w4CorrectLocal(t, st, "site-a", 1, w3Base+1*w3Hour, target.Key(), c1MS, "Officer One")
	w4CorrectLocal(t, st, "site-a", 1, w3Base+2*w3Hour, target.Key(), c2MS, "Officer Two")
	w4CorrectLocal(t, st, "site-a", 1, w3Base+3*w3Hour, target.Key(), c3MS, "Officer Three")

	// Non-vacuity: the corrections took effect at the resolved read.
	eff, _, err := w4Resolve(t, st, target.Key())
	if err != nil {
		t.Fatalf("W4-1: ResolveOccurredAt after 3 corrections: %v", err)
	}
	if !eff.Corrected || eff.InstantUTCms != c3MS {
		t.Fatalf("W4-1 fixture (VF-m4's shape): resolved = (corrected=%v, %d), want (true, %d) — with no substitution this byte-identity test proves nothing", eff.Corrected, eff.InstantUTCms, c3MS)
	}

	afterEnv, err := st.Events(target.SiteID, target.NodeEpoch, target.Seq, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterEnv) == 0 || afterEnv[0].Seq != target.Seq {
		t.Fatalf("W4-1 (m1): the target row is GONE after corrections — append-only does not hold")
	}
	if !reflect.DeepEqual(beforeEnv[0], afterEnv[0]) {
		t.Fatalf("W4-1 (m1, apply-as-edit): the target row changed under correction —\nbefore: %+v\nafter:  %+v\nD-42 obligation 1: the correction is a NEW event; the target row is never edited", beforeEnv[0], afterEnv[0])
	}
	if got := afterEnv[0].Hash(); got != wantHash {
		t.Fatalf("W4-1 (m1): target Hash() changed %s → %s — some hashed byte of the target was rewritten", wantHash, got)
	}
	if afterEnv[0].OccurredAt != w4OriginalMS || afterEnv[0].TimeSource != "entered" {
		t.Fatalf("W4-1/W4-2 (D-42 obligation 2): the target's own envelope assertion reads (%q, %d), want (\"entered\", %d) — the original is never erased", afterEnv[0].TimeSource, afterEnv[0].OccurredAt, w4OriginalMS)
	}
}

// TestW4ChainReturnsEveryCorrectionAndEntryZero — W4-2 (m4): after three
// same-stream corrections, ResolveOccurredAt's chain returns ALL THREE in
// ascending key order, each Status-marked ([Superseded, Superseded,
// Effective]), with every intermediate instant queryable; entry zero (the
// target's own envelope assertion) is NOT in the chain and IS readable via
// the §4.1 point-read. All four instants pairwise distinct, or a truncated
// or reordered chain could alias a correct one.
func TestW4ChainReturnsEveryCorrectionAndEntryZero(t *testing.T) {
	const (
		c1MS = w3Base - 9*w3Hour
		c2MS = w3Base - 8*w3Hour
		c3MS = w3Base - 7*w3Hour
	)
	instants := []int64{w4OriginalMS, c1MS, c2MS, c3MS}
	for i := range instants {
		for j := i + 1; j < len(instants); j++ {
			if instants[i] == instants[j] {
				t.Fatalf("FIXTURE DEFECT: instants %d and %d equal (%d) — a chain that drops or reorders entries could still match", i, j, instants[i])
			}
		}
	}

	st := w3NewStore(t)
	target := w4Target(t, st, "site-a", 1, "Chain Target")
	e1 := w4CorrectLocal(t, st, "site-a", 1, w3Base+1*w3Hour, target.Key(), c1MS, "Officer One")
	e2 := w4CorrectLocal(t, st, "site-a", 1, w3Base+2*w3Hour, target.Key(), c2MS, "Officer Two")
	e3 := w4CorrectLocal(t, st, "site-a", 1, w3Base+3*w3Hour, target.Key(), c3MS, "Officer Three")

	eff, chain, err := w4Resolve(t, st, target.Key())
	if err != nil {
		t.Fatalf("W4-2: ResolveOccurredAt: %v", err)
	}
	if len(chain) != 3 {
		t.Fatalf("W4-2 (m4, chain truncation): chain has %d entr(ies), want 3 — every correction remains queryable at rest; \"the chain returns only the latest\" is the named mutant", len(chain))
	}
	wantKeys := []store.Key{e1.Key(), e2.Key(), e3.Key()}
	wantMS := []int64{c1MS, c2MS, c3MS}
	wantBy := []string{"Officer One", "Officer Two", "Officer Three"}
	for i, c := range chain {
		if c.Key != wantKeys[i] {
			t.Fatalf("W4-2: chain[%d].Key = %v, want %v (ascending key order)", i, c.Key, wantKeys[i])
		}
		if c.InstantUTCms != wantMS[i] {
			t.Fatalf("W4-2: chain[%d].InstantUTCms = %d, want %d — the intermediate value must remain queryable", i, c.InstantUTCms, wantMS[i])
		}
		if c.RecordedBy != wantBy[i] {
			t.Fatalf("W4-2: chain[%d].RecordedBy = %q, want %q", i, c.RecordedBy, wantBy[i])
		}
		want := Superseded
		if i == len(chain)-1 {
			want = Effective
		}
		if c.Status != want {
			t.Fatalf("W4-2: chain[%d].Status = %v, want %v — each entry is Status-marked", i, c.Status, want)
		}
		if i > 0 && !chain[i-1].Key.Less(c.Key) {
			t.Fatalf("W4-2: chain not in ascending key order at %d: %v !< %v", i, chain[i-1].Key, c.Key)
		}
	}
	if !eff.Corrected || eff.InstantUTCms != c3MS || eff.Winner != e3.Key() || eff.RecordedBy != "Officer Three" {
		t.Fatalf("W4-2: effective = %+v, want (Corrected, %d, winner %v, %q)", eff, c3MS, e3.Key(), "Officer Three")
	}

	// Entry zero: the target's own envelope assertion, via the §4.1
	// point-read — never in the chain, never in any side table.
	evs, err := st.Events(target.SiteID, target.NodeEpoch, target.Seq, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) == 0 || evs[0].Seq != target.Seq {
		t.Fatalf("W4-2: the §4.1 point-read cannot reach entry zero")
	}
	if evs[0].OccurredAt != w4OriginalMS || evs[0].TimeSource != "entered" {
		t.Fatalf("W4-2 (D-42 obligation 2): entry zero reads (%q, %d), want (\"entered\", %d) — what was first entered survives", evs[0].TimeSource, evs[0].OccurredAt, w4OriginalMS)
	}
}

// TestW4CorrectionEnvelopeDescribesTheActD44 — W4-6 (m6): the correction's
// OWN envelope describes the correction ACT — TimeSource is exactly
// "default", OccurredAt == AuthoredAt.WallMS exactly (rule W, one clock
// reading), and the corrected instant appears in the PAYLOAD ONLY —
// asserted ABSENT from the envelope (one instant, one home; the two-stores
// shape is the defect this window deletes — D-44). The D-42-literal
// reading ("entered" on the act's envelope, or the corrected instant in
// the envelope's OccurredAt) is the named mutant.
func TestW4CorrectionEnvelopeDescribesTheActD44(t *testing.T) {
	const correctedMS = w3Base - 9*w3Hour
	const actClockMS = w3Base + 2*w3Hour
	if correctedMS == actClockMS {
		t.Fatalf("FIXTURE DEFECT: corrected instant equals the act's clock — the two-stores shape would be undetectable")
	}
	st := w3NewStore(t)
	target := w4Target(t, st, "site-a", 1, "Envelope Target")
	env := w4CorrectLocal(t, st, "site-a", 1, actClockMS, target.Key(), correctedMS, "Riley Okpik")

	if env.TimeSource != "default" {
		t.Fatalf("W4-6 (m6, the D-42-literal envelope): the correction act's TimeSource = %q, want \"default\" — the officer typed a PAYLOAD value; they did not override the correction's own timestamp (declined UI, VF §5; D-44)", env.TimeSource)
	}
	if env.OccurredAt != env.AuthoredAt.WallMS {
		t.Fatalf("W4-6 (m6): the act's OccurredAt (%d) != AuthoredAt.WallMS (%d) — rule W: one clock reading", env.OccurredAt, env.AuthoredAt.WallMS)
	}
	if env.OccurredAt == correctedMS || env.AuthoredAt.WallMS == correctedMS {
		t.Fatalf("W4-6 (m6): the corrected instant %d appears in the ENVELOPE — one instant, one home; storing it twice is the two-stores shape D-44 exists to refuse", correctedMS)
	}
	if env.AuthoredAt.WallMS != actClockMS {
		t.Fatalf("W4-6 fixture: the act was authored at %d, want the parked clock %d", env.AuthoredAt.WallMS, actClockMS)
	}

	// Payload values: the target key triple, the corrected instant, the
	// officer — through the pinned key literals.
	var pl map[string]json.RawMessage
	if err := json.Unmarshal(env.Payload, &pl); err != nil {
		t.Fatal(err)
	}
	var gotSite, gotBy, gotType string
	var gotEpoch, gotSeq, gotMS int64
	mustField := func(key string, dst any) {
		raw, ok := pl[key]
		if !ok {
			t.Fatalf("W4-6: payload carries no %q — the pinned key set is {type, %s, %s, %s, %s, %s}", key, w4KeyTargetSiteID, w4KeyTargetEpoch, w4KeyTargetSeq, w4KeyCorrectedAt, w4KeyRecordedBy)
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			t.Fatalf("W4-6: payload %q: %v", key, err)
		}
	}
	mustField("type", &gotType)
	mustField(w4KeyTargetSiteID, &gotSite)
	mustField(w4KeyTargetEpoch, &gotEpoch)
	mustField(w4KeyTargetSeq, &gotSeq)
	mustField(w4KeyCorrectedAt, &gotMS)
	mustField(w4KeyRecordedBy, &gotBy)
	if gotType != w4EvtCorrected {
		t.Fatalf("W4-6: payload type %q, want %q", gotType, w4EvtCorrected)
	}
	tk := target.Key()
	if gotSite != tk.SiteID || gotEpoch != tk.NodeEpoch || gotSeq != tk.Seq {
		t.Fatalf("W4-6: payload target = (%s,%d,%d), want %v", gotSite, gotEpoch, gotSeq, tk)
	}
	if gotMS != correctedMS {
		t.Fatalf("W4-6: payload %s = %d, want %d byte-faithful", w4KeyCorrectedAt, gotMS, correctedMS)
	}
	if gotBy != "Riley Okpik" {
		t.Fatalf("W4-6: payload %s = %q, want %q", w4KeyRecordedBy, gotBy, "Riley Okpik")
	}

	// Payload fields are EXACTLY {type + the five pinned keys} — no reason
	// field, no note field, nothing else (operative default #15; B-2's
	// wire-freeze literals).
	var keys []string
	for k := range pl {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := []string{w4KeyCorrectedAt, w4KeyRecordedBy, w4KeyTargetEpoch, w4KeyTargetSeq, w4KeyTargetSiteID, "type"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("W4-6 (#15): payload keys = %v, want EXACTLY %v — a reason/note field (or any other extra) is refused by the recorded operative default, and these bytes are hashed and unchangeable post-bump", keys, want)
	}
}

// TestW4PayloadFieldsPinnedAndHashCovered — W4-6's hash-coverage clause
// (m7, applied per field — FIVE applications, one per pinned key): for
// each pinned payload key, an envelope whose payload lacks that key hashes
// DIFFERENTLY, so no field can be dropped from the hash input without a
// digest a second post also holds going wrong.
func TestW4PayloadFieldsPinnedAndHashCovered(t *testing.T) {
	const correctedMS = w3Base - 9*w3Hour
	st := w3NewStore(t)
	target := w4Target(t, st, "site-a", 1, "Hash Target")
	env := w4CorrectLocal(t, st, "site-a", 1, w3Base+1*w3Hour, target.Key(), correctedMS, "Riley Okpik")
	baseHash := env.Hash()

	var pl map[string]json.RawMessage
	if err := json.Unmarshal(env.Payload, &pl); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{w4KeyTargetSiteID, w4KeyTargetEpoch, w4KeyTargetSeq, w4KeyCorrectedAt, w4KeyRecordedBy} {
		if _, ok := pl[key]; !ok {
			t.Fatalf("W4-6 (m7): authored payload carries no %q — cannot apply the field-drop to a field that is not there", key)
		}
		dropped := map[string]json.RawMessage{}
		for k, v := range pl {
			if k != key {
				dropped[k] = v
			}
		}
		raw, err := json.Marshal(dropped)
		if err != nil {
			t.Fatal(err)
		}
		mutant := env
		mutant.Payload = json.RawMessage(raw)
		if mutant.Hash() == baseHash {
			t.Fatalf("W4-6 (m7, application %q): dropping payload field %q did NOT flip Hash() — the field is outside the hash input, so a peer could alter it without breaking the digest", key, key)
		}
	}
}
