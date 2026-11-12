// dump_compose_test.go — direct proof for ComposedPersonsFromDump
// (dump_compose.go): the dump-file entry point to the SAME composition
// composePersons already performs from a live store, added so a consumer
// with only a saved dump (cmd/dupreport, W3-14's constraint) can reach
// composed identity without ever naming person_amendment itself.
//
// These tests exercise the function directly, from raw JSON bytes, rather
// than only indirectly through cmd/dupreport's own suite — the function is
// new production code in a scanned package and earns its own proof.
package domain

import (
	"reflect"
	"strings"
	"testing"
)

func TestComposedPersonsFromDumpAppliesNameAndSameAsAmendments(t *testing.T) {
	dump := `{
		"person": [{"person_id":"p1","site_id":"site-a","name":"Bob"}],
		"person_amendment": [
			{"site_id":"site-a","node_epoch":1,"seq":1,"person_id":"p1","name":"Robert","same_as":"p2","cleared":null}
		]
	}`
	got, err := ComposedPersonsFromDump([]byte(dump))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 composed person, got %d: %+v", len(got), got)
	}
	p := got[0]
	if p.Name != "Robert" {
		t.Fatalf("want the amended name %q, got %q", "Robert", p.Name)
	}
	if p.SameAs == nil || *p.SameAs != "p2" {
		t.Fatalf("want same_as composed to %q, got %v", "p2", p.SameAs)
	}
}

func TestComposedPersonsFromDumpClearedSameAsComposesToAbsent(t *testing.T) {
	dump := `{
		"person": [{"person_id":"p1","site_id":"site-a","name":"Bob"}],
		"person_amendment": [
			{"site_id":"site-a","node_epoch":1,"seq":1,"person_id":"p1","same_as":"p2","cleared":null},
			{"site_id":"site-a","node_epoch":1,"seq":2,"person_id":"p1","cleared":"[\"same_as\"]"}
		]
	}`
	got, err := ComposedPersonsFromDump([]byte(dump))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 composed person, got %d", len(got))
	}
	if got[0].SameAs != nil {
		t.Fatalf("want same_as cleared to nil (ABSENT, not empty-string), got %v", *got[0].SameAs)
	}
}

// TestComposedPersonsFromDumpMissingAmendmentSectionComposesToBaseOnly
// proves the "old binary dump" tolerance: no "person_amendment" key at all
// composes to the created-state rows, unchanged, never an error.
func TestComposedPersonsFromDumpMissingAmendmentSectionComposesToBaseOnly(t *testing.T) {
	dump := `{"person": [{"person_id":"p1","site_id":"site-a","name":"Bob"}]}`
	got, err := ComposedPersonsFromDump([]byte(dump))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Bob" {
		t.Fatalf("want the base row unchanged, got %+v", got)
	}
}

// TestComposedPersonsFromDumpAmendmentForUnknownPersonComposesToNothing
// pins composePersons' own documented rule at this entry point too: "a
// reader cannot show a person it has never been told exists."
func TestComposedPersonsFromDumpAmendmentForUnknownPersonComposesToNothing(t *testing.T) {
	dump := `{
		"person": [{"person_id":"p1","site_id":"site-a","name":"Bob"}],
		"person_amendment": [
			{"site_id":"site-a","node_epoch":1,"seq":1,"person_id":"p-ghost","name":"Nobody","cleared":null}
		]
	}`
	got, err := ComposedPersonsFromDump([]byte(dump))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Bob" {
		t.Fatalf("an amendment for an absent person must compose to nothing, got %+v", got)
	}
}

// TestPersonAmendmentDumpRowCoversExactlyTheAmendableFacts binds the HAND-KEPT
// enumeration to the DERIVED one.
//
// ⚠ THE DEFECT THIS EXISTS FOR ALREADY HAPPENED, which is why it is here and
// not merely advisable. queryPersonAmendments walks personAmendableFacts, so
// the SQL source of this composition is derived and cannot go stale.
// personAmendmentDumpRow types its fields out BY HAND, so the dump source can
// — and did: repair/dup-report was cut before D-88 ERRATA / D-91 struck
// `pad_oriented`, carried a PadOriented field, and merged to the tip CLEANLY
// because the file is new on that branch. Nothing conflicted, so nothing
// asked. It was caught by TestW5cNoProductionCodeNamesPadOrientation — a scan
// for one struck NAME, which would have said nothing at all had the divergence
// been an ADDED fact rather than a struck one.
//
// ⭐ SO THIS TEST IS THE GENERAL FORM OF THAT CATCH: it fails in BOTH
// directions — a fact added to personAmendableFacts and not typed here (the
// dump path silently drops it) and a fact typed here that is no longer
// amendable (the dump path composes something the SQL path refuses).
//
// ⛔ IT DOES NOT COMPARE AGAINST A SECOND HAND-WRITTEN LIST. A literal list
// here would be a THIRD copy of the same enumeration and would go stale in the
// same way; this reads the struct's own json tags by reflection and the live
// facts from personAmendableFacts, so there is nothing to keep in step.
//
// PROVEN RED, BOTH ARMS [MEASURED 2026-09-09, this tree, restored by byte copy
// and verified by sha256 + empty porcelain + empty index + no untracked]:
//
//   - Re-adding a PadOriented field with the pad_oriented json tag reds the
//     "typed here but not amendable" arm, naming pad_oriented.
//   - Removing the Position field AND its set() line in toRow reds the
//     "amendable but not typed" arm, naming position.
//
// ⚠ REMOVING THE FIELD ALONE IS NOT A MUTANT FOR THIS TEST — toRow still
// references w.Position, so the package fails to BUILD and this test never
// runs. A build failure is not this guard going red, and recording it as one
// would credit the guard with a catch it did not make. The set() line must go
// with the field.
func TestPersonAmendmentDumpRowCoversExactlyTheAmendableFacts(t *testing.T) {
	// The non-fact columns: the key, the identity, and the two TEXT columns
	// that are not facts. Everything else in the struct must be a fact.
	notFacts := map[string]bool{
		"site_id": true, "node_epoch": true, "seq": true,
		"person_id": true, "cleared": true, "seen": true,
	}

	typed := map[string]bool{}
	rt := reflect.TypeOf(personAmendmentDumpRow{})
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		if tag == "" {
			t.Fatalf("precondition: field %s has no json tag — this test reads the wire names from the tags, so an untagged field is invisible to it", rt.Field(i).Name)
		}
		name := strings.Split(tag, ",")[0]
		if notFacts[name] {
			continue
		}
		typed[name] = true
	}
	if len(typed) == 0 {
		t.Fatal("precondition: no fact fields found on personAmendmentDumpRow — a zero from a method that cannot produce a one is not a zero")
	}

	live := map[string]bool{}
	for _, f := range personAmendableFacts {
		live[f] = true
	}

	for f := range live {
		if !typed[f] {
			t.Errorf("personAmendableFacts carries %q but personAmendmentDumpRow has no field for it — a dump would silently DROP that fact while the SQL path composes it, so the two sources of one composition disagree", f)
		}
	}
	for f := range typed {
		if !live[f] {
			t.Errorf("personAmendmentDumpRow types %q but it is not in personAmendableFacts — the dump path would compose a fact the SQL path refuses. If the fact was STRUCK, delete the field; a struck fact still decodes harmlessly because json.Unmarshal ignores unknown keys", f)
		}
	}
}
