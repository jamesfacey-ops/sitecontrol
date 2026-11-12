// w3_edge_refusals_test.go — W3-2 (edge refusals fire, each produced,
// PRE-APPEND; the equivalent amendment arriving on the wire FOLDS — the
// fold never refuses) and W3-18 (kind homogeneity: same_as refuses a
// non-person target at the authoring edge).
// design/WINDOW-BUILD-PLAN-v1.md §3 W3, criteria LOCKED 2026-08-27.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS the clean-tree red.
//
// KILLER MUTANTS: m2 (a fold that refuses — the wire unknown-trade_id
// fixture), m6 (mint-then-refuse: edge refusal AFTER Author has appended —
// the log-length assertions), m24 (kind check dropped — the person→company
// fixture authors at the edge).
//
// SEAM S10 (recorded, the W1/W2 §7.2 pattern): the unknown-`trade_id`
// refusal limb locks HERE with its refusal red producible at W3; its
// ACCEPT side (a valid trade_id accepted and composed) cannot be
// constructed before W8's trade register exists and is DEFERRED to a named
// W8 rider — no W8 criterion text is drafted here.
package domain

import (
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// w3RefuseCase is one edge refusal; build prepares any standing state and
// returns the input that must be refused.
type w3RefuseCase struct {
	name  string
	build func(t *testing.T, st *store.Store) PersonAmendment
}

// TestW3EdgeRefusalsArePreAppend — W3-2: every refusal in the LOCKED set
// (no-op, name-to-empty, unknown trade id, and the re-hosted merge-edge
// checks: self-merge, standing prior merge, cycle — P-3 §5 — plus W3-18's
// kind check) is produced, and each is PRE-APPEND: after a refusal the log
// length is unchanged and no person_amended envelope exists (the W5b
// shape; VF §9 constraint 1 classifies these as pre-append input
// validation). A refusal that has already minted the event is m6.
func TestW3EdgeRefusalsArePreAppend(t *testing.T) {
	cases := []w3RefuseCase{
		{"no-op amendment (no field, no Clear)", func(t *testing.T, st *store.Store) PersonAmendment {
			x, _, _ := w3RosterPair(t, st, "Noop X", "Noop Y")
			return PersonAmendment{PersonID: x}
		}},
		{"name-to-empty", func(t *testing.T, st *store.Store) PersonAmendment {
			x, _, _ := w3RosterPair(t, st, "Empty X", "Empty Y")
			return PersonAmendment{PersonID: x, Name: w3Str("")}
		}},
		{"unknown trade_id (S10: refusal limb only at W3)", func(t *testing.T, st *store.Store) PersonAmendment {
			x, _, _ := w3RosterPair(t, st, "Trade X", "Trade Y")
			return PersonAmendment{PersonID: x, TradeID: w3Str("trade-does-not-exist")}
		}},
		{"self-merge", func(t *testing.T, st *store.Store) PersonAmendment {
			x, _, _ := w3RosterPair(t, st, "Self X", "Self Y")
			return PersonAmendment{PersonID: x, SameAs: w3Str(x)}
		}},
		{"standing prior merge", func(t *testing.T, st *store.Store) PersonAmendment {
			x, y, _ := w3RosterPair(t, st, "Prior X", "Prior Y")
			z, err := CreatePerson(st, "site-a", 1, "Prior Z", nil, nil, 0, "default")
			if err != nil {
				t.Fatal(err)
			}
			w3AmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour,
				PersonAmendment{PersonID: x, SameAs: w3Str(y)})
			return PersonAmendment{PersonID: x, SameAs: w3Str(z.PersonID)}
		}},
		{"cycle", func(t *testing.T, st *store.Store) PersonAmendment {
			x, y, _ := w3RosterPair(t, st, "Cycle X", "Cycle Y")
			w3AmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour,
				PersonAmendment{PersonID: x, SameAs: w3Str(y)})
			return PersonAmendment{PersonID: y, SameAs: w3Str(x)}
		}},
		{"kind check: non-person target (W3-18)", func(t *testing.T, st *store.Store) PersonAmendment {
			x, _, _ := w3RosterPair(t, st, "Kind X", "Kind Y")
			co, err := CreateCompany(st, "site-a", 1, "Kind Freight", 0, "default")
			if err != nil {
				t.Fatal(err)
			}
			return PersonAmendment{PersonID: x, SameAs: w3Str(co.CompanyID)}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := w3NewStore(t)
			in := c.build(t, st)
			before := w3Watermark(t, st, "site-a", 1)
			env, err := AmendPerson(st, "site-a", 1, in, 0, "default")
			if err == nil {
				t.Fatalf("W3-2: %s was ACCEPTED (envelope %v) — this refusal's failure mode was never produced", c.name, env.Key())
			}
			after := w3Watermark(t, st, "site-a", 1)
			if after != before {
				t.Fatalf("W3-2 (m6, mint-then-refuse): %s refused with error %q but the log grew (%d → %d) — the refusal must be PRE-APPEND", c.name, err, before, after)
			}
			for _, e := range w3EventsAfter(t, st, "site-a", 1, before) {
				if strings.Contains(string(e.Payload), `"`+w3EvtPersonAmended+`"`) {
					t.Fatalf("W3-2 (m6): %s refused but a person_amended envelope exists at %v", c.name, e.Key())
				}
			}
		})
	}
}

// TestW3WireEquivalentsFold — W3-2's other half: an equivalent amendment
// arriving ON THE WIRE folds; the fold NEVER refuses (edge refusals are
// pre-append input validation — a wire-received event passed the peer's
// edge or was minted by a divergent one, and refusing it at fold time
// forks the two logs forever). Kills m2 (a fold that refuses — the
// unknown-trade_id case is its named producer).
//
// W3-18's demoted fold half rides here as the explanatory-note fixture:
// a cross-kind same_as arriving by wire FOLDS (an edge defect, not a fold
// crash) — asserted as fold-accepts only, per the LOCKED demotion (FA-4 /
// S-5): no composed-value pin is criterion-grade for it.
func TestW3WireEquivalentsFold(t *testing.T) {
	st := w3NewStore(t)
	x, y, _ := w3RosterPair(t, st, "Wire X", "Wire Y")
	co, err := CreateCompany(st, "site-a", 1, "Wire Freight", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// A standing local merge x→y so the wire y→x below is cycle-FORMING —
	// exactly what the local edge refuses and the fold must not.
	w3AmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour,
		PersonAmendment{PersonID: x, SameAs: w3Str(y)})

	wire := []struct {
		name   string
		fields map[string]any
		person string
	}{
		{"unknown trade_id folds (m2's named producer)", map[string]any{"trade_id": "trade-does-not-exist"}, x},
		{"name-to-empty folds", map[string]any{"name": ""}, y},
		{"no-op amendment folds", map[string]any{}, y},
		{"cycle-forming same_as folds", map[string]any{"same_as": x}, y},
		{"cross-kind same_as folds (W3-18 explanatory note)", map[string]any{"same_as": co.CompanyID}, y},
	}
	for i, c := range wire {
		t.Run(c.name, func(t *testing.T) {
			env := w3WireAmendment(t, "site-b", 1, int64(i+1), w3Base+2*w3Hour, c.person, c.fields)
			applied, err := st.Apply(env)
			if err != nil {
				t.Fatalf("W3-2 (m2): the fold REFUSED a wire-received %s: %v — folds never refuse; refusing here forks the two posts' logs permanently", c.name, err)
			}
			if !applied {
				t.Fatalf("W3-2: wire %s was silently deduped rather than folded", c.name)
			}
		})
	}
}
