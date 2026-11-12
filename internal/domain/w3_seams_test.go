// w3_seams_test.go — THE ONE place W3's domain tests bind every Go spelling
// the LOCKED criteria leave undetermined (design/WINDOW-BUILD-PLAN-v1.md §3
// W3, criteria LOCKED 2026-08-27; test-writer note, plan §5 step 2), plus
// the shared fixture machinery. This file holds NO assertions of its own.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — every symbol under "SHAPE PRESUMPTIONS"
// below is unbuilt at the clean tree (HEAD 0648c82). That compile failure IS
// the ledger RED for the resolver-level tests (plan §5 step 2: "capture the
// go vet output as its red").
//
// SHAPE PRESUMPTIONS (SEAMS), each bound once, disputes go to the gate:
//
//   - SEAM W3-S-EDGE: the authoring edge is
//     domain.AmendPerson(st *store.Store, siteID string, nodeEpoch int64,
//     in domain.PersonAmendment, occurredAtMS int64, timeSource string,
//     ) (store.Envelope, error)
//     following the repo's Create* positional idiom (refdata.go). A refusal
//     returns a non-nil error and NEVER calls Author (W3-2's pre-append
//     clause — asserted, not assumed).
//   - SEAM W3-S-INPUT: domain.PersonAmendment carries AT LEAST these fields
//     with these types (the builder may add the rest of the amendable set —
//     CONSOLIDATED §2 item 4 — without touching tests):
//     PersonID string; Name, Phone, TradeID, SameAs *string;
//     Clear []string
//   - SEAM W3-S-PERSON: the composed Person (Persons()/PersonByID()) gains
//     Phone *string   and   SameAs *string
//     (nil = absent — W3-3's absent-not-empty distinction depends on the
//     pointer, so this is load-bearing, not style).
//   - SEAM W3-S-RESOLVER: the P-3 §5 derived resolution snapshot loader is
//     domain.LoadPersonMergeMap(st *store.Store) (domain.AliasMap, error)
//     returning the existing AliasMap read shape ("the AliasMap read shape
//     and hop-capped Resolve survive over a new loader" [DOC — P-3 §5]).
//     The map itself must be iterable/chase-able (it is a map type), which
//     is what lets the W3-17 harness walk it under its OWN step budget
//     (W4-9's fixed mechanical form).
//   - SEAM W3-S-REGISTRY: the VF §4.4 carrier registry is observable as
//     domain.IsRegisteredCarrier(eventType string) bool
//     (W3-12's coverage test needs a predicate; the three registration
//     goods themselves are exercised through Outcome).
//   - SEAM W3-S-PAYLOAD: wire payload literals, per the measured lowercase
//     snake_case convention (checkin.go struct tags) and schema-10 §5A.2's
//     published shape: type "person_amended"; keys person_id, name,
//     last_name, first_name, phone, company_id, trade_id, role,
//     position, retired, same_as, clear (LOWERCASE on the wire — DECIDED
//     at P-3 §7 rev 3, R2-C1b) and, since SchemaVersion 11, seen ([[D-92]]).
//     ⟦was: "…role, pad_oriented, position…" — struck at 11, D-88 ERRATA⟧
//   - SEAM W3-S-FACTS: Outcome Fact names for this carrier are the payload
//     field spellings: "phone", "name", "same_as" (VF §4.2's own examples).
//
// NOT seams (DOC-pinned, cited): store.Key.Less's signature and FactOutcome/
// Status/Outcome (VF §4.2 verbatim); causal-then-ascending-key composition (W3-1, re-locked at 11 under D-92 ⟦was: ascending-key⟧);
// "site-b" > "site-a" (VF §1/§10, measured from the deploy TOMLs).
package domain

import (
	"encoding/json"
	"fmt"
	"testing"

	"sitecontrol/internal/store"
)

const (
	w3EvtPersonAmended = "person_amended" // SEAM W3-S-PAYLOAD
	w3FactPhone        = "phone"          // SEAM W3-S-FACTS
	w3FactName         = "name"
	w3FactSameAs       = "same_as"
)

const w3Base = int64(1_800_000_000_000) // w2Base's twin: fixed audit "now"
const w3Hour = int64(3_600_000)

// w3Reading is w2Reading's domain-package twin: a fixed audit-clock reading.
func w3Reading(wallMS int64) store.Clock {
	return func() store.ClockReading {
		return store.ClockReading{WallMS: wallMS, MaxErrorUS: 42, State: store.ClockSynced}
	}
}

// w3Str returns a pointer to s (amendment field literals).
func w3Str(s string) *string { return &s }

// w3NewStore opens a registered store with the clock parked at w3Base.
func w3NewStore(t *testing.T) *store.Store {
	t.Helper()
	st := newDomainTestStore(t)
	st.Clock = w3Reading(w3Base)
	return st
}

// w3Events reads the (siteID, epoch) stream from seq 1.
func w3Events(t *testing.T, st *store.Store, siteID string, epoch int64) []store.Envelope {
	t.Helper()
	evs, err := st.Events(siteID, epoch, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

// w3EventsAfter reads the stream events with Seq > afterSeq.
func w3EventsAfter(t *testing.T, st *store.Store, siteID string, epoch, afterSeq int64) []store.Envelope {
	t.Helper()
	evs, err := st.Events(siteID, epoch, afterSeq+1, 0)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

// w3Watermark is the stream's high-water seq.
func w3Watermark(t *testing.T, st *store.Store, siteID string, epoch int64) int64 {
	t.Helper()
	wm, err := st.Watermark(siteID, epoch)
	if err != nil {
		t.Fatal(err)
	}
	return wm
}

// w3Apply applies one wire envelope and requires it to land (folds never
// refuse — a refusal here is itself a W3-2 red when the fixture says so).
func w3Apply(t *testing.T, st *store.Store, env store.Envelope) {
	t.Helper()
	applied, err := st.Apply(env)
	if err != nil {
		t.Fatalf("apply %v: %v (the fold refused — folds never refuse, W3-2 / mutant m2)", env.Key(), err)
	}
	if !applied {
		t.Fatalf("apply %v: deduped unexpectedly", env.Key())
	}
}

// w3ApplyAll applies envelopes in the given order.
func w3ApplyAll(t *testing.T, st *store.Store, envs []store.Envelope) {
	t.Helper()
	for _, e := range envs {
		w3Apply(t, st, e)
	}
}

// w3AuthorRemote primes a scratch store with primer envelopes (the shared
// replicated history), parks its clock at clockMS, runs author against it,
// and returns the envelopes the author minted on the (siteID, epoch)
// stream — the wire-shaped output of a REAL authoring edge at another post.
func w3AuthorRemote(t *testing.T, primer []store.Envelope, siteID string, epoch, clockMS int64, author func(st *store.Store)) []store.Envelope {
	t.Helper()
	scratch := newDomainTestStore(t)
	w3ApplyAll(t, scratch, primer)
	scratch.Clock = w3Reading(clockMS)
	before := w3Watermark(t, scratch, siteID, epoch)
	author(scratch)
	return w3EventsAfter(t, scratch, siteID, epoch, before)
}

// w3AmendLocal authors one amendment on st with the clock at clockMS and
// returns its envelope; a refusal is fatal (fixture, not assertion).
func w3AmendLocal(t *testing.T, st *store.Store, siteID string, epoch, clockMS int64, in PersonAmendment) store.Envelope {
	t.Helper()
	st.Clock = w3Reading(clockMS)
	env, err := AmendPerson(st, siteID, epoch, in, 0, "default")
	if err != nil {
		t.Fatalf("fixture amendment refused: %v", err)
	}
	return env
}

// w3WireAmendment hand-builds a person_amended envelope for the cases the
// authoring edge would REFUSE (W3-2's folds-never-refuse half needs events
// no edge will mint). fields uses SEAM W3-S-PAYLOAD key spellings.
func w3WireAmendment(t *testing.T, siteID string, epoch, seq, authoredAtMS int64, personID string, fields map[string]any) store.Envelope {
	t.Helper()
	payload := map[string]any{"type": w3EvtPersonAmended, "person_id": personID}
	for k, v := range fields {
		payload[k] = v
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	env := store.Envelope{
		SiteID: siteID, NodeEpoch: epoch, Seq: seq,
		SchemaVersion: store.SchemaVersion, OriginAuth: siteID,
		OccurredAt: authoredAtMS, TimeSource: "default",
		Payload: json.RawMessage(raw),
	}
	env.AuthoredAt.WallMS = authoredAtMS
	env.AuthoredAt.MaxErrorUS = 42
	env.AuthoredAt.State = store.ClockSynced
	return env
}

// w3Person fetches the composed person or fails.
func w3Person(t *testing.T, st *store.Store, id string) *Person {
	t.Helper()
	p, err := PersonByID(st, id)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatalf("person %s: no row", id)
	}
	return p
}

// w3MergeMap loads the derived resolution snapshot (SEAM W3-S-RESOLVER).
func w3MergeMap(t *testing.T, st *store.Store) AliasMap {
	t.Helper()
	m, err := LoadPersonMergeMap(st)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// w3BudgetedResolve is W4-9's fixed mechanical form applied to the merge
// snapshot (W3-17: "the step-budgeted resolver invocation asserting
// budget-not-exhausted"): the harness walks the SNAPSHOT under its OWN step
// budget first — exhaustion is a red attributed to the calling test, never
// a suite-`-timeout` abort — and only then invokes the resolver for the
// answer, so a de-collapsed, unbounded resolver (mutant m23) reds here
// instead of hanging the binary.
func w3BudgetedResolve(t *testing.T, m AliasMap, id string) string {
	t.Helper()
	budget := 2*len(m) + 8
	cur := id
	for i := 0; i < budget; i++ {
		next, ok := m[cur]
		if !ok || next == cur {
			return m.Resolve(id)
		}
		cur = next
	}
	t.Fatalf("W3-17 / m23: step budget (%d) EXHAUSTED walking the merge snapshot from %q — the snapshot still contains an uncollapsed cycle, so the termination guarantee's delivery is gone (collapse removed/bypassed); this red is the named test's own, not a timeout abort", budget, id)
	return ""
}

// w3Fact returns the FactOutcome for fact from Outcome(k); a missing fact
// or an Outcome error is a FAILURE, never a skip (T-A precondition 2).
func w3Fact(t *testing.T, st *store.Store, k store.Key, fact string) FactOutcome {
	t.Helper()
	outs, err := Outcome(st, k)
	if err != nil {
		t.Fatalf("Outcome(%v): %v", k, err)
	}
	for _, o := range outs {
		if o.Fact == fact {
			return o
		}
	}
	t.Fatalf("Outcome(%v) reported no %q fact (got %+v) — cannot-match is a FAILURE, never a skip (W3-11, T-A precondition 2)", k, fact, outs)
	return FactOutcome{}
}

// --- the T-A agreement oracle (W3-11) -----------------------------------

// w3TAFixture is one asserted fact of one fixture event.
type w3TAFixture struct {
	Key      store.Key
	PersonID string // the entity the event targeted
	Fact     string
	Asserted string
}

// w3ComposedFactValue reads the fact's composed/RESOLVED value. VALUE
// facts (phone, name) are read through the ONE derived resolution
// snapshot — a merge joins assertion sets, so the resolved record's
// composed value is what every surface shows (VF §6 case 5). The same_as
// fact itself is the merge POINTER and is read on the entity's own
// composed record (resolving it first would erase the very fact under
// test). Returns (value, present).
func w3ComposedFactValue(t *testing.T, st *store.Store, personID, fact string) (string, bool) {
	t.Helper()
	if fact == w3FactSameAs {
		p := w3Person(t, st, personID)
		if p.SameAs == nil {
			return "", false
		}
		return *p.SameAs, true
	}
	m := w3MergeMap(t, st)
	p := w3Person(t, st, m.Resolve(personID))
	switch fact {
	case w3FactPhone:
		if p.Phone == nil {
			return "", false
		}
		return *p.Phone, true
	case w3FactName:
		return p.Name, true
	}
	t.Fatalf("w3ComposedFactValue: no accessor bound for fact %q (extend SEAM W3-S-FACTS)", fact)
	return "", false
}

// w3RunTAOracle is VF §8's T-A agreement oracle: for every fixture
// asserted-fact, Status == Effective IFF the composed/resolved value equals
// that event's asserted value. Its two validity preconditions are asserted
// at RUNTIME, before any outcome is trusted (the CONTROL row §5's C-8
// demands): (1) pairwise-distinct values per fact; (2) an unmatched fact is
// a failure (w3Fact fatals), never a skip.
func w3RunTAOracle(t *testing.T, st *store.Store, fixtures []w3TAFixture) {
	t.Helper()
	seen := map[string]map[string]store.Key{}
	for _, f := range fixtures {
		if seen[f.Fact] == nil {
			seen[f.Fact] = map[string]store.Key{}
		}
		if prev, dup := seen[f.Fact][f.Asserted]; dup {
			t.Fatalf("T-A FIXTURE DEFECT (precondition 1): fact %q asserted value %q twice (%v and %v) — with duplicate values the iff is unsatisfiable on correct code and the predictable repair greens over m11/m17", f.Fact, f.Asserted, prev, f.Key)
		}
		seen[f.Fact][f.Asserted] = f.Key
	}
	for _, f := range fixtures {
		out := w3Fact(t, st, f.Key, f.Fact)
		val, present := w3ComposedFactValue(t, st, f.PersonID, f.Fact)
		matches := present && val == f.Asserted
		if (out.Status == Effective) != matches {
			t.Fatalf("W3-11 T-A disagreement (m17/m5): event %v fact %q asserted %q; Outcome says %v but the composed/resolved read says %q (present=%v) — the Go comparator and the SQL ORDER BY have drifted", f.Key, f.Fact, f.Asserted, out.Status, val, present)
		}
		if out.Status == Effective && out.Winner != f.Key {
			t.Fatalf("W3-11/W3-9: fact %q Effective but Winner %v != queried key %v (the §4.2 convention: Winner == queried key iff Effective)", f.Fact, out.Winner, f.Key)
		}
		if out.Status == Superseded {
			if out.Winner == (store.Key{}) {
				t.Fatalf("W3-11: fact %q Superseded with a ZERO Winner — Winner is always a real event's key (VF §4.2)", f.Fact)
			}
			if out.Winner == f.Key {
				t.Fatalf("W3-11: fact %q Superseded yet Winner == the queried key %v", f.Fact, f.Key)
			}
		}
	}
}

// w3RosterPair mints two persons on origin (site-a, epoch 1) and returns
// their ids with the full site-a stream for priming remote scratch stores.
func w3RosterPair(t *testing.T, st *store.Store, nameA, nameB string) (idA, idB string, stream []store.Envelope) {
	t.Helper()
	pa, err := CreatePerson(st, "site-a", 1, nameA, nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	pb, err := CreatePerson(st, "site-a", 1, nameB, nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return pa.PersonID, pb.PersonID, w3Events(t, st, "site-a", 1)
}

// w3RequireDistinct is the shared fixture-defect guard for pinned-literal
// fixtures (standing rule 3): every named rival answer must differ from the
// certified answer, recomputed from the fixture's own read-back data.
func w3RequireDistinct(t *testing.T, criterion, certified string, rivals map[string]string) {
	t.Helper()
	for name, got := range rivals {
		if got == certified {
			t.Fatalf("%s FIXTURE DEFECT: DEGENERATE — rival %q returns %q, the same answer as the certified rule; a fixture where the rule and a rival agree certifies nothing (standing rule 3; the W3-17 rev-2 lesson)", criterion, name, got)
		}
	}
}

var _ = fmt.Sprintf // keep fmt imported if helpers shuffle
