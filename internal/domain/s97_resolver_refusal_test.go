// s97_resolver_refusal_test.go — ⟦TRIAL — §97 / §102, 2026-09-03⟧
//
// ⭐ THIS TEST IS THE CONDITION §102 ATTACHED TO ITS AUTHORISATION, and it is not a formality.
//
// §97's shape defers the vehicle co-mint into `Dispatch` so that no authoring happens until every
// validation has passed. To do that, `"domain: dispatch: vehicle id is required"` had to stop firing
// on `VehicleID == ""` alone — a caller supplying a `ResolveVehicle` callback has no id YET. The
// condition underneath the refusal therefore WIDENED from `id == ""` to `id == "" && resolver == nil`.
//
// ⚠⚠ THE PINNED LITERAL IS BYTE-IDENTICAL EITHER WAY, AND THAT IS THE HAZARD. §102: "the pinned
// literal stays byte-identical while the condition underneath widens — that is §89's class exactly, a
// string-keyed coupling the compiler cannot see. Without that test the refusal has not moved; it has
// quietly stopped firing, and the identical message is the thing that would hide it."
//
// So the guarantee is asserted directly: a caller who supplies a resolver that yields NOTHING is still
// refused, by the same words, having authored nothing.
package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestS97DispatchStillRefusesWhenNoVehicleIDIsEverProduced(t *testing.T) {
	const wantLiteral = "vehicle id is required"
	dest := "site-b"
	deadline := int64(60)

	for _, tc := range []struct {
		name string
		with func(*DispatchInput)
	}{
		// The ORIGINAL path, unchanged: no id and no resolver. This is the arm the widened
		// condition must not have disturbed.
		{"neither an id nor a resolver", func(in *DispatchInput) {}},
		// ⭐ THE NEW PATHS — the ones the widening created, and the reason this test exists.
		{"a resolver that yields an empty id", func(in *DispatchInput) {
			in.ResolveVehicle = func() (string, error) { return "", nil }
		}},
		{"a resolver that yields whitespace", func(in *DispatchInput) {
			in.ResolveVehicle = func() (string, error) { return "   ", nil }
		}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			st := newDomainTestStore(t)
			driver, _ := seedTwoPersons(t, st, "site-a")
			before, err := st.Watermark("site-a", 1)
			if err != nil {
				t.Fatal(err)
			}

			in := DispatchInput{
				DriverPersonID:  driver.PersonID,
				DestinationSite: &dest,
				DeadlineMinutes: &deadline,
			}
			tc.with(&in)

			_, err = Dispatch(st, "site-a", 1, in, 0, "default")
			if err == nil {
				t.Fatalf("%s: Dispatch ACCEPTED a departure that never produced a vehicle id. The refusal has "+
					"not MOVED, it has stopped firing — and because the message is unchanged wherever it does "+
					"still fire, nothing else in this suite would have shown that", tc.name)
			}
			if !strings.Contains(err.Error(), wantLiteral) {
				t.Fatalf("%s: refused with %q, which does not carry the pinned literal %q — §39.4(ii) preserves "+
					"that text byte-for-byte, and a refusal that arrives under different words is a different "+
					"refusal", tc.name, err.Error(), wantLiteral)
			}
			after, err := st.Watermark("site-a", 1)
			if err != nil {
				t.Fatal(err)
			}
			if after != before {
				t.Fatalf("%s: refused but authored %d event(s) (watermark %d -> %d) — the whole point of the "+
					"deferral is that nothing is written until every validation has passed",
					tc.name, after-before, before, after)
			}
		})
	}

	// ⚠ A resolver that FAILS is a different case from one that yields nothing, and it must not be
	// silently converted into the "required" refusal — the caller's own error has to survive, or a
	// co-mint refusal (a placeholder number, say) would reach the operator as "vehicle id is
	// required", which is not why it was refused.
	t.Run("a resolver that errors keeps its own error", func(t *testing.T) {
		st := newDomainTestStore(t)
		driver, _ := seedTwoPersons(t, st, "site-a")
		sentinel := errors.New("co-mint refused: \"walk-in\" does not identify a vehicle")
		before, _ := st.Watermark("site-a", 1)
		_, err := Dispatch(st, "site-a", 1, DispatchInput{
			DriverPersonID:  driver.PersonID,
			DestinationSite: &dest,
			DeadlineMinutes: &deadline,
			ResolveVehicle:  func() (string, error) { return "", sentinel },
		}, 0, "default")
		if err == nil {
			t.Fatal("a failing resolver was accepted")
		}
		if !errors.Is(err, sentinel) {
			t.Fatalf("the resolver's own error did not survive: got %q. A co-mint refusal reported as "+
				"%q would tell the operator the wrong reason", err.Error(), wantLiteral)
		}
		if after, _ := st.Watermark("site-a", 1); after != before {
			t.Fatalf("a failing resolver authored %d event(s)", after-before)
		}
	})
}
