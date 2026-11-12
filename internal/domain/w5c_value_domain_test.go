// w5c_value_domain_test.go — W5c-1, THE VALUE DOMAIN, in the domain —
// CORRECTED TO THE SCHEMA-11 SHAPE.
//
//	W5c-1 ⟦was⟧ Only `true` and absent are ever authored; revoke is `Clear`;
//	       a wire-received "false" is recorded AS IT ARRIVES.
//	W5c-1 ⟦SchemaVersion 11⟧ NOTHING is ever authored: the authoring input
//	       has no field for the fact, the payload has no key for it, and
//	       Clear:["pad_oriented"] is refused as "not an amendable field" —
//	       pre-append, log unchanged. A wire-received value still FOLDS
//	       (never refused, W3-2) and composes to NOTHING.
//
// ⟦SchemaVersion 11 — CORRECTED, NOT DELETED, 2026-09-05⟧ The value domain
// {"true", absent} was D-21.1's single boolean on the enrolling site's row;
// [[D-88]]'s ERRATA struck the fact and reversed D-10/D-21.1. The domain is
// now EMPTY on this post's authoring surfaces and the wire path keeps its
// W3-2 tolerance. Each old arm is inverted onto that shape below, and the
// mirror arms drive a SURVIVING fact (`position`) so "cannot author pad
// orientation" is never satisfied by an edge that can author nothing.
//
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31.
// Machinery and seams: w5c_seams_test.go.
//
// ⚠ THE ASYMMETRY IS STILL LOAD-BEARING. The edge refuses; the fold never
// does. A builder who "completes" the strike by refusing wire envelopes that
// still carry the old key forks the two posts' logs permanently —
// TestW5cWireReceivedPadOrientedFoldsAndComposesToNothing is the test that
// reds if they do.
package domain

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// w5cSeedPerson lands a person on the roster THROUGH THE WIRE, from a peer
// post, so no W5c fixture depends on `createPerson`'s Go signature. It
// returns the person id. The local post is "site-a" throughout; the wire
// arrives from "site-b", so the local stream stays clean for log reads.
func w5cSeedPerson(t *testing.T, st *store.Store, seq int64, name string, fields map[string]any) string {
	t.Helper()
	id := fmt.Sprintf("site-b-person-%04d", seq)
	env := w5cWirePersonCreated(t, "site-b", 7, seq, w5cBase, id, name, fields)
	w5cApply(t, st, env, "fixture")
	return id
}

// =======================================================================
// W5c-1 — NOTHING IS EVER AUTHORED
// =======================================================================

// TestW5cAmendmentEdgeCannotAuthorPadOrientationAtAll — W5c-1, the
// AUTHORING half at the domain edge, inverted. The two spellings that reach
// this edge are both closed: there is no PersonAmendment field to assert the
// fact with (reflection — never a selector, which would not compile), and
// Clear:["pad_oriented"] — the only way a caller can still NAME the fact — is
// refused as not amendable, PRE-APPEND. After both, no event on this post's
// log carries the key.
// ⟦was: TestW5cAmendmentEdgeRefusesEveryValueOutsideTheDomain — the value
// corpus is now driven at the officer's real form in internal/web, the only
// surface where a VALUE can still be typed against the stale key⟧
func TestW5cAmendmentEdgeCannotAuthorPadOrientationAtAll(t *testing.T) {
	st := w5cNewStore(t)
	w5cRequireStruckShape(t, st, "W5c-1 (edge)")
	id := w5cSeedPerson(t, st, 1, "Aurora, Ada", nil)

	// (A) the input shape: no field, under any name that carries the fact.
	rt := reflect.TypeOf(PersonAmendment{})
	for i := 0; i < rt.NumField(); i++ {
		if strings.Contains(rt.Field(i).Name, "Oriented") {
			t.Fatalf("W5c-1 (m5c-1′): PersonAmendment.%s still offers the struck fact to callers", rt.Field(i).Name)
		}
	}
	if _, ok := rt.FieldByName("Position"); !ok {
		t.Fatal("CONTROL FAILED: the reflection probe cannot see PersonAmendment.Position — it could not vouch for an absence")
	}

	// (B) the one remaining spelling, Clear, is refused pre-append.
	before := w5cStreamLen(t, st, "site-a", 1)
	_, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: id, Clear: []string{w5cKeyPadOriented}}, w5cBase, "default")
	if err == nil {
		t.Fatalf("W5c-1 (m5c-1′): the edge ACCEPTED Clear:[%q] — a struck fact is not an amendable field", w5cKeyPadOriented)
	}
	if !strings.Contains(err.Error(), "not an amendable field") {
		t.Errorf("W5c-1: Clear:[%q] was refused, but not as an unknown field: %v", w5cKeyPadOriented, err)
	}
	if after := w5cStreamLen(t, st, "site-a", 1); after != before {
		t.Errorf("W5c-1: the refused clear grew this post's log %d -> %d — every edge refusal is PRE-APPEND (W3-2)", before, after)
	}

	// (C) the log carries no pad value from this post, ever.
	if authored := w5cAuthoredPadValues(t, st, "site-a", 1); len(authored) != 0 {
		t.Errorf("W5c-1: this post's own stream carries %s values: %v", w5cKeyPadOriented, w5cSortedPairs(authored))
	}
	// And the composed person has no such key to show for it.
	if keys := w5cJSONKeys(t, w5cComposed(t, st, id)); keys[w5cKeyPadOriented] {
		t.Errorf("W5c-1: the composed person carries %q", w5cKeyPadOriented)
	}
}

// TestW5cAmendmentEdgeStillAuthorsTheSurvivingFacts — W5c-1's MIRROR, and
// it is not optional: without it every negative arm above is satisfied by an
// edge that refuses everything. The surviving neighbour `position` still
// authors, appends exactly once and composes.
// ⟦was: TestW5cAmendmentEdgeStillAuthorsTheOneLegalValue — the one legal
// value is gone with the fact; the mirror moves to a fact that survived⟧
func TestW5cAmendmentEdgeStillAuthorsTheSurvivingFacts(t *testing.T) {
	st := w5cNewStore(t)
	id := w5cSeedPerson(t, st, 1, "Aurora, Ada", nil)
	before := w5cStreamLen(t, st, "site-a", 1)

	env, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: id, Position: w5cStr("Rig Manager")}, w5cBase, "default")
	if err != nil {
		t.Fatalf("W5c-1 MIRROR: the edge REFUSED a surviving fact (%s): %v — an edge that refuses everything satisfies every negative arm of this file and proves nothing", w5cSurvivingFact, err)
	}
	if after := w5cStreamLen(t, st, "site-a", 1); after != before+1 {
		t.Fatalf("W5c-1 MIRROR: authoring %s moved the log %d -> %d, want exactly one append (event %v)", w5cSurvivingFact, before, after, env.Key())
	}
	if p := w5cComposed(t, st, id); p.Position == nil || *p.Position != "Rig Manager" {
		t.Fatalf("W5c-1 MIRROR: after authoring %s the composed person reads %s", w5cSurvivingFact, w5cPtrStr(p.Position))
	}
}

// TestW5cClearPadOrientedIsRefusedAsNotAmendable — W5c-1's last clause,
// inverted. ⟦was: "Revocation is Clear:[\"pad_oriented\"] composing to
// absent" — TestW5cRevocationIsClearComposingToAbsent⟧ There is nothing left
// to revoke: the clear is refused, and the refusal must be the UNKNOWN-FIELD
// one, so a reader of the message learns the fact is gone rather than
// guessing at a value-domain rule. The mirror proves the Clear MECHANISM is
// intact on a surviving fact: Clear:["position"] still composes to absent.
func TestW5cClearPadOrientedIsRefusedAsNotAmendable(t *testing.T) {
	st := w5cNewStore(t)
	id := w5cSeedPerson(t, st, 1, "Aurora, Ada", nil)
	if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: id, Position: w5cStr("Rig Manager")}, w5cBase, "default"); err != nil {
		t.Fatalf("fixture defect: could not author the surviving fact the mirror clears: %v", err)
	}
	before := w5cStreamLen(t, st, "site-a", 1)

	_, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: id, Clear: []string{w5cKeyPadOriented}}, w5cBase, "default")
	if err == nil || !strings.Contains(err.Error(), "not an amendable field") {
		t.Fatalf("W5c-1: Clear:[%q] must be refused as an unknown field (the fact is struck), got err=%v", w5cKeyPadOriented, err)
	}
	if after := w5cStreamLen(t, st, "site-a", 1); after != before {
		t.Fatalf("W5c-1: the refused clear grew the log %d -> %d", before, after)
	}

	// MIRROR: the Clear mechanism itself still works (W3-3: composes to
	// ABSENT, never empty-string).
	if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: id, Clear: []string{w5cSurvivingFact}}, w5cBase, "default"); err != nil {
		t.Fatalf("W5c-1 MIRROR: the edge REFUSED Clear:[%q] on a surviving fact: %v — the refusal above would then be an edge that refuses every clear", w5cSurvivingFact, err)
	}
	if p := w5cComposed(t, st, id); p.Position != nil {
		t.Fatalf("W5c-1 MIRROR: after Clear:[%q] the composed person still reads %s, want ABSENT (W3-3)", w5cSurvivingFact, w5cPtrStr(p.Position))
	}
}

// TestW5cWireReceivedPadOrientedFoldsAndComposesToNothing — W5c-1's SECOND
// CLAUSE, the asymmetry the lock warned is "correct and easily fixed away",
// inverted in subject and unchanged in principle: a peer's person_amended
// still carrying the stale key FOLDS (W3-2: refusing on the wire forks the
// two posts' logs permanently) and composes to NOTHING — the composed person
// carries no such key, and the amendment row exists in the side table (the
// event landed) with no such column.
// ⟦was: TestW5cWireReceivedFalseIsRecordedAsItArrives⟧
//
// EXPECTED RED: a builder who implements the strike in applyPersonAmended
// as a refusal of the old key (m5c-6). That build looks tidy and forks the
// two posts' logs the first time a divergent peer sends the key.
func TestW5cWireReceivedPadOrientedFoldsAndComposesToNothing(t *testing.T) {
	for i, arriving := range []string{"true", "false", "", "0", "no", "TRUE", "maybe"} {
		arriving := arriving
		t.Run(fmt.Sprintf("wire_%02d_%s", i, w5cLabel(arriving)), func(t *testing.T) {
			st := w5cNewStore(t)
			w5cRequireStruckShape(t, st, "W5c-1 (wire arm)")
			id := w5cSeedPerson(t, st, 1, "Aurora, Ada", nil)
			env := w5cWirePersonAmended(t, "site-b", 7, 2, w5cBase, id,
				map[string]any{w5cKeyPadOriented: arriving, w5cSurvivingFact: "Rig Manager"}, nil)
			w5cApply(t, st, env, "W5c-1 (wire arm)") // fatals on a refusing fold (m5c-6)

			p := w5cComposed(t, st, id)
			if keys := w5cJSONKeys(t, p); keys[w5cKeyPadOriented] {
				t.Fatalf("W5c-1 (wire arm): a peer sent %s=%q and this post's composed person carries the key — the strike left a landing place", w5cKeyPadOriented, arriving)
			}
			// The envelope LANDED: its surviving fact composed, so "composes
			// to nothing" is about the stale key and not about a dropped row.
			if p.Position == nil || *p.Position != "Rig Manager" {
				t.Fatalf("CONTROL FAILED (mirror): the same wire amendment's surviving fact %s did not compose (%s) — the fold did not land the row, so the absence above proves nothing", w5cSurvivingFact, w5cPtrStr(p.Position))
			}
			if cols := w5cTableColumns(t, st, "person_amendment"); w5cHasColumn(cols, w5cKeyPadOriented) {
				t.Fatalf("W5c-1: `person_amendment` still carries %q: %v", w5cKeyPadOriented, cols)
			}
		})
	}
}

// --- small helpers used only by this file --------------------------------

func w5cIsFalsy(v string) bool {
	for _, f := range w5cFalsyValues {
		if f == v {
			return true
		}
	}
	return false
}

// w5cLabel renders a corpus value as a subtest name.
func w5cLabel(v string) string {
	switch v {
	case "":
		return "empty"
	case " ":
		return "space"
	}
	r := strings.NewReplacer(" ", "_")
	return r.Replace(v)
}

func w5cSortedPairs(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, fmt.Sprintf("%s=%q", k, v))
	}
	sort.Strings(out)
	return out
}

var _ = w5cIsFalsy
var _ = w5cForbiddenValues
