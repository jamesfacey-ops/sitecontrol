// s120_vehicle_amend_test.go — S120: the AmendVehicle authoring edge.
//
// ⛔ THE GAP THIS CLOSES IS NOT ONE ARM. [MEASURED 2026-09-04]
// `grep -rn 'AmendVehicle(' --include='*_test.go' .` returned **ZERO**: the
// whole edge — ten refusal arms and two preconditions, ~150 lines of guard —
// had no test caller anywhere in the tree. A mutation audit found it by
// breaking Arm 6 (the identifies-nothing refusal) and watching the tree stay
// green; ⚠ testing only Arm 6 would have fixed the mutant and left the defect.
//
// EVERY ARM IS DRIVEN THROUGH THE REAL EXPORTED EDGE, and every refusal case
// also asserts that NOTHING WAS AUTHORED — a refusal that silently appends is
// the failure these arms exist to prevent, and a message check alone cannot
// see it.
//
// ⚠ ACCEPTING ARMS ARE INCLUDED DELIBERATELY (§62's produced condition): a
// file that only shows refusals is satisfied by a build that refuses
// everything, which would make every vehicle correction impossible.
//
// ARM 10 (the cycle) IS NOT COVERED HERE — stated rather than quietly
// omitted. `vehicle_amend.go` says the cycle "forms only on the wire" because
// edges refuse cycles locally and folds never refuse, so exercising it needs
// primed scratch edges rather than the edge alone. It is W7-14's own fixture
// shape and belongs with that test, not this one.
package domain

import (
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

func s120Str(s string) *string { return &s }

// s120Vehicle mints a vehicle with the given number/role on site-a.
func s120Vehicle(t *testing.T, st *store.Store, number, role *string) Vehicle {
	t.Helper()
	v, err := CreateVehicle(st, "site-a", 1, number, role, 0, "default")
	if err != nil {
		t.Fatalf("fixture: creating vehicle: %v", err)
	}
	return v
}

func s120Watermark(t *testing.T, st *store.Store) int64 {
	t.Helper()
	wm, err := st.Watermark("site-a", 1)
	if err != nil {
		t.Fatalf("watermark: %v", err)
	}
	return wm
}

// TestW7AmendVehicleRefusalArms drives every refusal arm of the edge.
func TestW7AmendVehicleRefusalArms(t *testing.T) {
	for _, c := range []struct {
		arm  string
		want string // a distinctive fragment of the refusal
		// build returns the amendment to attempt, given a store seeded by the
		// case itself. Each case owns its fixture so one arm cannot make
		// another vacuous.
		build func(t *testing.T, st *store.Store) VehicleAmendment
	}{
		{
			arm: "precondition — no vehicle id", want: "a vehicle id is required",
			build: func(t *testing.T, st *store.Store) VehicleAmendment {
				return VehicleAmendment{Number: s120Str("TRUCK 9")}
			},
		},
		{
			arm:  "precondition — a vehicle this post has never been told about",
			want: "is not a vehicle on this post",
			build: func(t *testing.T, st *store.Store) VehicleAmendment {
				return VehicleAmendment{VehicleID: "site-a-vehicle-nosuch", Number: s120Str("TRUCK 9")}
			},
		},
		{
			arm: "1 — the amendment changes nothing", want: "changes nothing",
			build: func(t *testing.T, st *store.Store) VehicleAmendment {
				v := s120Vehicle(t, st, s120Str("TRUCK 1"), nil)
				return VehicleAmendment{VehicleID: v.VehicleID}
			},
		},
		{
			arm: "2 — a blank NUMBER assertion is not a clear", want: "cannot be amended to blank",
			build: func(t *testing.T, st *store.Store) VehicleAmendment {
				v := s120Vehicle(t, st, s120Str("TRUCK 1"), s120Str("water"))
				return VehicleAmendment{VehicleID: v.VehicleID, Number: s120Str("   ")}
			},
		},
		{
			arm: "2 — a blank ROLE assertion is not a clear", want: "cannot be amended to blank",
			build: func(t *testing.T, st *store.Store) VehicleAmendment {
				v := s120Vehicle(t, st, s120Str("TRUCK 1"), s120Str("water"))
				return VehicleAmendment{VehicleID: v.VehicleID, Role: s120Str("")}
			},
		},
		{
			arm: "3 — the placeholder guard at the amendment door", want: "does not identify a vehicle",
			build: func(t *testing.T, st *store.Store) VehicleAmendment {
				v := s120Vehicle(t, st, s120Str("TRUCK 1"), nil)
				return VehicleAmendment{VehicleID: v.VehicleID, Number: s120Str("walk-in")}
			},
		},
		{
			arm:  "4 — a Clear naming a fact outside the amendable set",
			want: "is not an amendable vehicle field",
			build: func(t *testing.T, st *store.Store) VehicleAmendment {
				v := s120Vehicle(t, st, s120Str("TRUCK 1"), s120Str("water"))
				return VehicleAmendment{VehicleID: v.VehicleID, Clear: []string{"site_id"}}
			},
		},
		{
			arm: "5 — the same fact both set and cleared", want: "both set and cleared",
			build: func(t *testing.T, st *store.Store) VehicleAmendment {
				v := s120Vehicle(t, st, s120Str("TRUCK 1"), s120Str("water"))
				return VehicleAmendment{VehicleID: v.VehicleID, Number: s120Str("TRUCK 2"), Clear: []string{"number"}}
			},
		},
		{
			// ⛔ THE MUTATED ARM. Clearing BOTH content facts leaves a row
			// nobody can pick, search for or read on a muster.
			arm:  "6 — clearing both facts would leave the record identifying nothing",
			want: "neither a number nor a role",
			build: func(t *testing.T, st *store.Store) VehicleAmendment {
				v := s120Vehicle(t, st, s120Str("TRUCK 1"), s120Str("water"))
				return VehicleAmendment{VehicleID: v.VehicleID, Clear: []string{"number", "role"}}
			},
		},
		{
			// The other reachable shape of Arm 6, and the one a
			// clearing-only test would miss: the record has ONLY a role, and
			// the amendment clears it.
			arm:  "6 — clearing the only surviving fact",
			want: "neither a number nor a role",
			build: func(t *testing.T, st *store.Store) VehicleAmendment {
				v := s120Vehicle(t, st, nil, s120Str("water truck"))
				return VehicleAmendment{VehicleID: v.VehicleID, Clear: []string{"role"}}
			},
		},
		{
			arm: "7 — the trivial self-loop", want: "cannot be merged with itself",
			build: func(t *testing.T, st *store.Store) VehicleAmendment {
				v := s120Vehicle(t, st, s120Str("TRUCK 1"), nil)
				return VehicleAmendment{VehicleID: v.VehicleID, SameAs: s120Str(v.VehicleID)}
			},
		},
		{
			// ⚠ Arm 8's WRONG-KIND sentence. It must be a DIFFERENT sentence
			// from the unknown-target one below — that distinction IS mutant
			// (m7-13b), and a single refusal for both cannot tell a kind
			// check from an existence check.
			arm: "8 — same_as pointing at a PERSON record", want: "is a person record, not a vehicle",
			build: func(t *testing.T, st *store.Store) VehicleAmendment {
				v := s120Vehicle(t, st, s120Str("TRUCK 1"), nil)
				p, err := CreatePerson(st, "site-a", 1, "Vega, Ana", nil, nil, 0, "default")
				if err != nil {
					t.Fatalf("fixture: creating a person: %v", err)
				}
				return VehicleAmendment{VehicleID: v.VehicleID, SameAs: s120Str(p.PersonID)}
			},
		},
		{
			arm:  "8 — same_as pointing at an id seeded NOWHERE",
			want: "is not a record this post has ever been told about",
			build: func(t *testing.T, st *store.Store) VehicleAmendment {
				v := s120Vehicle(t, st, s120Str("TRUCK 1"), nil)
				return VehicleAmendment{VehicleID: v.VehicleID, SameAs: s120Str("site-a-vehicle-neverseen")}
			},
		},
		{
			arm: "9 — a standing prior merge", want: "clear same_as (the un-merge) first",
			build: func(t *testing.T, st *store.Store) VehicleAmendment {
				a := s120Vehicle(t, st, s120Str("TRUCK 1"), nil)
				b := s120Vehicle(t, st, s120Str("TRUCK 2"), nil)
				c := s120Vehicle(t, st, s120Str("TRUCK 3"), nil)
				if _, err := AmendVehicle(st, "site-a", 1,
					VehicleAmendment{VehicleID: a.VehicleID, SameAs: s120Str(b.VehicleID)}, 0, "default"); err != nil {
					t.Fatalf("fixture: the FIRST merge must be accepted: %v", err)
				}
				return VehicleAmendment{VehicleID: a.VehicleID, SameAs: s120Str(c.VehicleID)}
			},
		},
	} {
		t.Run(c.arm, func(t *testing.T) {
			st := newDomainTestStore(t)
			in := c.build(t, st)
			before := s120Watermark(t, st)

			_, err := AmendVehicle(st, "site-a", 1, in, 0, "default")
			if err == nil {
				t.Fatalf("AmendVehicle ACCEPTED the amendment this arm exists to refuse.\n"+
					"arm: %s\nwanted a refusal containing %q", c.arm, c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("the refusal does not say what it is for.\narm: %s\ngot:  %v\nwant a message containing: %q\n"+
					"⚠ The message IS the guard's product: an officer meets it at a post, and a "+
					"refusal that does not name the reason sends them looking for the wrong thing.",
					c.arm, err, c.want)
			}
			// A refusal must leave NO TRACE. Checked separately from the
			// message because a build can refuse loudly and append anyway.
			if after := s120Watermark(t, st); after != before {
				t.Fatalf("a REFUSED amendment authored %d event(s) — a refusal never calls Author.\narm: %s",
					after-before, c.arm)
			}
		})
	}
}

// TestW7AmendVehicleAcceptsTheLegitimateCorrections is the accepting half.
//
// ⚠ WITHOUT THIS the refusal table above is satisfied by a build that refuses
// every amendment — which would make a mis-typed plate uncorrectable forever.
// The two clearing cases are Arm 6's own accepting side, and they are the
// distinction Arm 6 is computed over: clearing one fact is safe exactly when
// the other survives.
func TestW7AmendVehicleAcceptsTheLegitimateCorrections(t *testing.T) {
	for _, c := range []struct {
		name string
		seed func(t *testing.T, st *store.Store) Vehicle
		in   func(v Vehicle) VehicleAmendment
		want func(t *testing.T, after Vehicle)
	}{
		{
			name: "a corrected number",
			seed: func(t *testing.T, st *store.Store) Vehicle { return s120Vehicle(t, st, s120Str("TRUKC 12"), nil) },
			in: func(v Vehicle) VehicleAmendment {
				return VehicleAmendment{VehicleID: v.VehicleID, Number: s120Str("TRUCK 12")}
			},
			want: func(t *testing.T, a Vehicle) {
				if a.Number == nil || *a.Number != "TRUCK 12" {
					t.Fatalf("number = %v, want TRUCK 12", a.Number)
				}
			},
		},
		{
			name: "clearing the NUMBER while the role survives",
			seed: func(t *testing.T, st *store.Store) Vehicle {
				return s120Vehicle(t, st, s120Str("TRUCK 1"), s120Str("water truck"))
			},
			in: func(v Vehicle) VehicleAmendment {
				return VehicleAmendment{VehicleID: v.VehicleID, Clear: []string{"number"}}
			},
			want: func(t *testing.T, a Vehicle) {
				if a.Number != nil {
					t.Fatalf("number = %q, want cleared", *a.Number)
				}
				if a.Role == nil || *a.Role != "water truck" {
					t.Fatalf("role = %v, want it to SURVIVE the clear", a.Role)
				}
			},
		},
		{
			name: "clearing the ROLE while the number survives",
			seed: func(t *testing.T, st *store.Store) Vehicle {
				return s120Vehicle(t, st, s120Str("TRUCK 1"), s120Str("water truck"))
			},
			in: func(v Vehicle) VehicleAmendment {
				return VehicleAmendment{VehicleID: v.VehicleID, Clear: []string{"role"}}
			},
			want: func(t *testing.T, a Vehicle) {
				if a.Role != nil {
					t.Fatalf("role = %q, want cleared", *a.Role)
				}
				if a.Number == nil || *a.Number != "TRUCK 1" {
					t.Fatalf("number = %v, want it to SURVIVE the clear", a.Number)
				}
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			st := newDomainTestStore(t)
			v := c.seed(t, st)
			before := s120Watermark(t, st)
			if _, err := AmendVehicle(st, "site-a", 1, c.in(v), 0, "default"); err != nil {
				t.Fatalf("AmendVehicle REFUSED a legitimate correction: %v", err)
			}
			if after := s120Watermark(t, st); after != before+1 {
				t.Fatalf("an accepted amendment authored %d event(s), want exactly 1", after-before)
			}
			got, err := VehicleByID(st, v.VehicleID)
			if err != nil || got == nil {
				t.Fatalf("reading the amended vehicle back: %v", err)
			}
			c.want(t, *got)
		})
	}
}
