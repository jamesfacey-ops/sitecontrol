// w3_compose_test.go — W3-1 (causal-then-ascending-key composition ⟦re-locked at SchemaVersion 11 under D-92; was: ascending-key⟧ + the standing
// cross-epoch CONTROL fixture), W3-3 (Clear un-sets to ABSENT), W3-8 (the
// composed-read verification pass / zero-composition mutant).
// design/WINDOW-BUILD-PLAN-v1.md §3 W3, criteria LOCKED 2026-08-27.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD (AmendPerson, PersonAmendment,
// Person.Phone/SameAs) — that IS these tests' clean-tree red.
//
// KILLER MUTANTS: m1 (compose sorted by OccurredAt/AuthoredAt — the
// chronological rival's pinned answer differs), m5 (epoch-blind
// comparator, (site_id, seq)-only or seq-major — provably invariant under
// W3-1's scramble and still wrong on the pinned cross-epoch winner
// ⟦SchemaVersion 11, D-92: this file's cross-epoch chain is CAUSAL — e2 saw
// e1b — so m5 SURVIVES it at 11; its compose-path kill is
// TestW3ConcurrentCrossEpochPairStillPinsTheEpochMajorWinner in
// w3_causal_compose_test.go, and W3-11's truth table kills it at the
// comparator⟧), m7 (Clear composes as empty-string, not absent), m14 (zero composition —
// Persons()/PersonByID return the created-state person untouched).
package domain

import (
	"encoding/json"
	"testing"

	"sitecontrol/internal/store"
)

// w3CrossEpochFixture is W3-1's same-site cross-epoch conflict, shared with
// W3-11's T-A oracle run (the §5 standing cross-epoch CONTROL row).
type w3CrossEpochFixture struct {
	st       *store.Store
	personID string
	// the three phone amendments, in authoring order:
	e1a, e1b store.Envelope // epoch 1, ascending seq; e1b carries the HIGH seq and the LATEST clock
	e2       store.Envelope // epoch 2, seq 1 (LOW), earlier clock than e1b
	phoneA1  string
	phoneA2  string // the epoch-blind AND chronological rivals' winner
	phoneB   string // the PINNED epoch-major winner
}

func w3BuildCrossEpochFixture(t *testing.T) w3CrossEpochFixture {
	t.Helper()
	fx := w3CrossEpochFixture{
		phoneA1: "555-0001", phoneA2: "555-0002", phoneB: "555-0100",
	}
	fx.st = w3NewStore(t)
	p, err := CreatePerson(fx.st, "site-a", 1, "Cross Epoch", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	fx.personID = p.PersonID
	// Epoch 1: two amendments, raising seq; the LAST carries the LATEST
	// clock so a chronological compose picks it.
	fx.e1a = w3AmendLocal(t, fx.st, "site-a", 1, w3Base+1*w3Hour,
		PersonAmendment{PersonID: fx.personID, Phone: w3Str(fx.phoneA1)})
	fx.e1b = w3AmendLocal(t, fx.st, "site-a", 1, w3Base+3*w3Hour,
		PersonAmendment{PersonID: fx.personID, Phone: w3Str(fx.phoneA2)})
	// Epoch 2 (the re-image), seq 1 — LOW seq, clock EARLIER than e1b's.
	fx.e2 = w3AmendLocal(t, fx.st, "site-a", 2, w3Base+2*w3Hour,
		PersonAmendment{PersonID: fx.personID, Phone: w3Str(fx.phoneB)})

	// ⟦SchemaVersion 11, D-92⟧ This chain is CAUSAL, and the fixture says so
	// (rule 6): e1b's author saw e1a standing and e2's author saw e1b —
	// every amendment here is authored at the one post that holds the
	// others. So at 11 the epoch-major pin below is reached by DOMINANCE,
	// and an epoch-blind ORDER BY (m5) survives THIS fixture; the
	// compose-path kill for m5 is
	// TestW3ConcurrentCrossEpochPairStillPinsTheEpochMajorWinner
	// (w3_causal_compose_test.go), where e2 arrives naming nothing. W3-11's
	// truth table still kills m5 at the comparator. The rivals recomputed
	// below stay distinct from the pin, so the fixture still certifies m1.
	if seen := w3SeenOf(t, fx.e1b); !w3KeysEqual(seen, []store.Key{fx.e1a.Key()}) {
		t.Fatalf("fixture (D-92): e1b names %v, want [%v]", seen, fx.e1a.Key())
	}
	if seen := w3SeenOf(t, fx.e2); !w3KeysEqual(seen, []store.Key{fx.e1b.Key()}) {
		t.Fatalf("fixture (D-92): e2 names %v, want [%v] — the re-imaged post authored over the history it holds", seen, fx.e1b.Key())
	}

	// --- runtime non-degeneracy (standing rule 3): recompute every named
	// rival from the READ-BACK log, never from the constants above. -------
	evs := append(w3Events(t, fx.st, "site-a", 1), w3Events(t, fx.st, "site-a", 2)...)
	type pa struct {
		env   store.Envelope
		phone string
	}
	var amendments []pa
	for _, e := range evs {
		var pl struct {
			Type     string  `json:"type"`
			PersonID string  `json:"person_id"`
			Phone    *string `json:"phone"`
		}
		if err := json.Unmarshal(e.Payload, &pl); err != nil {
			t.Fatal(err)
		}
		if pl.Type == w3EvtPersonAmended && pl.PersonID == fx.personID && pl.Phone != nil {
			amendments = append(amendments, pa{env: e, phone: *pl.Phone})
		}
	}
	if len(amendments) != 3 {
		t.Fatalf("fixture defect: %d phone amendments in log, want 3", len(amendments))
	}
	pick := func(less func(a, b store.Envelope) bool) string {
		best := amendments[0]
		for _, c := range amendments[1:] {
			if less(best.env, c.env) {
				best = c
			}
		}
		return best.phone
	}
	rivals := map[string]string{
		"chronological max(AuthoredAt) [m1]": pick(func(a, b store.Envelope) bool {
			return a.AuthoredAt.WallMS < b.AuthoredAt.WallMS
		}),
		"chronological max(OccurredAt) [m1]": pick(func(a, b store.Envelope) bool {
			return a.OccurredAt < b.OccurredAt
		}),
		"seq-major (epoch-blind) [m5]": pick(func(a, b store.Envelope) bool {
			return a.Seq < b.Seq
		}),
		"(site_id, seq)-only (epoch-blind) [m5]": pick(func(a, b store.Envelope) bool {
			if a.SiteID != b.SiteID {
				return a.SiteID < b.SiteID
			}
			return a.Seq < b.Seq
		}),
	}
	if fx.e2.NodeEpoch <= fx.e1b.NodeEpoch || fx.e2.Seq >= fx.e1b.Seq {
		t.Fatalf("fixture defect: the cross-epoch pair is not (epoch N+1, LOW seq) vs (epoch N, HIGH seq): e2=%v e1b=%v", fx.e2.Key(), fx.e1b.Key())
	}
	w3RequireDistinct(t, "W3-1 cross-epoch", fx.phoneB, rivals)
	return fx
}

// TestW3CrossEpochComposePinsEpochMajorWinner — the §5 standing cross-epoch
// CONTROL row: epoch N+1 / LOW seq beats epoch N / HIGH seq, the winner
// PINNED as a literal, epoch-blind mutant m5 named. Three consecutive
// designs shipped cross-site-only conflict fixtures on which an epoch-blind
// comparator agreed with the real one on every test; this fixture is the
// remedy, and its non-degeneracy is asserted at runtime.
func TestW3CrossEpochComposePinsEpochMajorWinner(t *testing.T) {
	fx := w3BuildCrossEpochFixture(t)
	p := w3Person(t, fx.st, fx.personID)
	if p.Phone == nil {
		t.Fatalf("W3-1: composed phone absent, want the pinned epoch-major winner %q", fx.phoneB)
	}
	if *p.Phone != fx.phoneB {
		t.Fatalf("W3-1 (m1/m5): composed phone = %q, want the PINNED epoch-major winner %q (epoch N+1, LOW seq) — %q is the epoch-blind/chronological rivals' answer", *p.Phone, fx.phoneB, fx.phoneA2)
	}
}

// TestW3ComposeScrambleInvariance — W3-1's first clause: permuting
// AuthoredAt/OccurredAt VALUES across the amendments (over a fixed stream —
// TE-3: arrival order is untouched; m10 is W3-5's) changes nothing about
// the composed output, byte-compared.
func TestW3ComposeScrambleInvariance(t *testing.T) {
	fx := w3BuildCrossEpochFixture(t)
	epoch1 := w3Events(t, fx.st, "site-a", 1)
	epoch2 := w3Events(t, fx.st, "site-a", 2)

	amendKeys := map[store.Key]bool{fx.e1a.Key(): true, fx.e1b.Key(): true, fx.e2.Key(): true}
	type basis struct {
		auth store.AuthoredAt
		occ  int64
		src  string
	}
	var bases []basis
	collect := func(evs []store.Envelope) {
		for _, e := range evs {
			if amendKeys[e.Key()] {
				bases = append(bases, basis{e.AuthoredAt, e.OccurredAt, e.TimeSource})
			}
		}
	}
	collect(epoch1)
	collect(epoch2)
	if len(bases) != 3 {
		t.Fatalf("fixture defect: %d permutable bases, want 3", len(bases))
	}

	composed := func(perm []int) []byte {
		st := w3NewStore(t)
		i := 0
		applyPermuted := func(evs []store.Envelope) {
			for _, e := range evs {
				if amendKeys[e.Key()] {
					b := bases[perm[i]]
					e.AuthoredAt, e.OccurredAt, e.TimeSource = b.auth, b.occ, b.src
					i++
				}
				w3Apply(t, st, e)
			}
		}
		applyPermuted(epoch1)
		applyPermuted(epoch2)
		p := w3Person(t, st, fx.personID)
		all, err := Persons(st)
		if err != nil {
			t.Fatal(err)
		}
		out, err := json.Marshal(struct {
			One *Person
			All []Person
		}{p, all})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	want := composed([]int{0, 1, 2})
	for _, perm := range [][]int{{1, 2, 0}, {2, 0, 1}, {2, 1, 0}, {0, 2, 1}, {1, 0, 2}} {
		if got := composed(perm); string(got) != string(want) {
			t.Fatalf("W3-1 (m1): composed output changed under time-basis permutation %v —\nwant %s\ngot  %s\ncomposition read a clock; it must be causal, then ascending-key — never a clock ⟦re-locked D-92; was: \"ascending-key only\"⟧", perm, want, got)
		}
	}
}

// TestW3ClearComposesAsAbsent — W3-3: a cleared field composes as ABSENT
// (nil), never as empty-string. Load-bearing for W3-5/W3-6: Clear
// ["same_as"] is the un-merge (P-3 §5).
func TestW3ClearComposesAsAbsent(t *testing.T) {
	st := w3NewStore(t)
	p, err := CreatePerson(st, "site-a", 1, "Clear Case", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w3AmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour,
		PersonAmendment{PersonID: p.PersonID, Phone: w3Str("555-0199")})
	if got := w3Person(t, st, p.PersonID); got.Phone == nil || *got.Phone != "555-0199" {
		t.Fatalf("W3-3 fixture: phone did not compose before the Clear (got %+v)", got.Phone)
	}
	w3AmendLocal(t, st, "site-a", 1, w3Base+2*w3Hour,
		PersonAmendment{PersonID: p.PersonID, Clear: []string{w3FactPhone}})
	got := w3Person(t, st, p.PersonID)
	if got.Phone != nil {
		if *got.Phone == "" {
			t.Fatalf("W3-3 (m7): cleared phone composed as EMPTY-STRING (&\"\"), want absent (nil) — Clear un-sets, it does not blank")
		}
		t.Fatalf("W3-3: cleared phone still composed as %q, want absent (nil)", *got.Phone)
	}
}

// TestW3ComposedReadPass — W3-8: Persons()/PersonByID() return the COMPOSED
// person. Goes red under the zero-composition mutant (m14, the TPR2TE-1
// shape): person_amended ships, appends, folds — and the readers return the
// created-state person untouched.
func TestW3ComposedReadPass(t *testing.T) {
	st := w3NewStore(t)
	p, err := CreatePerson(st, "site-a", 1, "Created Name", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	w3AmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour, PersonAmendment{
		PersonID: p.PersonID,
		Name:     w3Str("Composed Name"),
		Phone:    w3Str("555-0142"),
	})

	one := w3Person(t, st, p.PersonID)
	if one.Name != "Composed Name" || one.Phone == nil || *one.Phone != "555-0142" {
		t.Fatalf("W3-8 (m14, zero composition): PersonByID returned name=%q phone=%v, want the composed (\"Composed Name\", \"555-0142\") — the created-state row untouched is this chunk's cheapest deadly mutant", one.Name, one.Phone)
	}

	all, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, q := range all {
		if q.PersonID != p.PersonID {
			continue
		}
		found = true
		if q.Name != "Composed Name" || q.Phone == nil || *q.Phone != "555-0142" {
			t.Fatalf("W3-8 (m14): Persons() returned name=%q phone=%v for the amended person, want composed values", q.Name, q.Phone)
		}
		if q.Name == "Created Name" {
			t.Fatalf("W3-8 (m14): Persons() still shows the created-state name")
		}
	}
	if !found {
		t.Fatalf("W3-8: amended person missing from Persons()")
	}
}
