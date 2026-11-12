// g13_fail_closed_test.go — closing the coverage gaps an independent
// tester's mutants found in G13's fail-closed refusal (2026-09-08): two of
// four planted mutants produced byte-identical PASSes because nothing in
// the shipped suite watched refuseForeignRow's new UNCLASSIFIED branch on
// purpose — A and B were only caught as a side effect of breaking ordinary
// gated authoring elsewhere. This file adds purpose-built witnesses for the
// two that got through, plus the "bare store is inert" property the whole
// mechanism's fail-closed default rests on.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

// TestG13UnregisteredTypeIsRefusedWithTheTypedError kills mutant C: turning
// `&UnregisteredTypeError{...}` into an equivalent-looking `fmt.Errorf(...)`
// passed every test that existed before this one, because nothing asserted
// the ERROR'S TYPE — only its neighbours' behaviour (a resolver's own
// error, a gated refusal). errors.As is the assertion; a substring check on
// Error() would not have caught the swap (measured precedent elsewhere in
// this campaign: swapping all 260 live %w sites for %s left five tests
// literally named "...IsWrapped" green).
func TestG13UnregisteredTypeIsRefusedWithTheTypedError(t *testing.T) {
	st := d87Probe(t) // probe_modify gated; probe_create/probe_free acknowledged
	_, err := st.Author("camp-7", 1, d87Payload("probe_never_classified", "x"), 0, "default")
	var unreg *UnregisteredTypeError
	if !errors.As(err, &unreg) {
		t.Fatalf("want *UnregisteredTypeError, got %v (%T)", err, err)
	}
	if unreg.EventType != "probe_never_classified" || unreg.SiteID != "camp-7" {
		t.Errorf("refusal = %+v, want EventType=probe_never_classified SiteID=camp-7", *unreg)
	}
	// The two refusals must stay tellable apart — an unclassified type is
	// NOT the same finding as a foreign-owned one.
	var own *OwnershipError
	if errors.As(err, &own) {
		t.Fatalf("an unclassified type was refused as *OwnershipError, not *UnregisteredTypeError: %v", err)
	}
	if wm := d87WM(t, st, "camp-7"); wm != 0 {
		t.Errorf("refused, yet camp-7's watermark is %d", wm)
	}
	if events, _ := st.Events("camp-7", 1, 1, 0); len(events) != 0 {
		t.Errorf("refused, yet camp-7's stream holds %d event(s)", len(events))
	}
}

// TestG13ABareStoreAuthorsAnythingUntouchedUntilSomethingIsRegistered pins
// "a bare store is inert" (ownership.go's refuseForeignRow doc comment) as
// a fact a test watches rather than an observation carried in a comment.
// Before G13 this covered every caller that never reached domain.Register;
// today the same default still holds for a store nobody has called
// RegisterOwnership or RegisterAcknowledged on AT ALL — the mechanism arms
// on the first registration of EITHER kind, never before, and this test's
// control proves the pass below is that default firing, not a permanent
// bypass this test failed to exercise.
func TestG13ABareStoreAuthorsAnythingUntouchedUntilSomethingIsRegistered(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "bare.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	if _, err := st.Author("camp-1", 1, d87Payload("nobody_has_classified_this", "x"), 0, "default"); err != nil {
		t.Fatalf("a bare store refused an unclassified type: %v", err)
	}
	if _, err := st.Author("camp-1", 1, []byte(`{"id":"x"}`), 0, "default"); err != nil {
		t.Fatalf("a bare store refused a typeless payload: %v", err)
	}

	// CONTROL: the instant ANYTHING is registered — one acknowledged type,
	// not the one used above — the funnel arms, and the SAME unclassified
	// type from before is now refused.
	if err := st.RegisterAcknowledged("some_other_type"); err != nil {
		t.Fatal(err)
	}
	_, err = st.Author("camp-1", 1, d87Payload("nobody_has_classified_this", "y"), 0, "default")
	var unreg *UnregisteredTypeError
	if !errors.As(err, &unreg) {
		t.Fatalf("CONTROL FAILED: after one registration the funnel should have armed; got %v (%T) — the pass above would prove nothing", err, err)
	}
}

// TestG13RegisterOwnershipRefusesAnAlreadyAcknowledgedType is the reciprocal
// of TestD87RegisterOwnershipRefusesADuplicateAndListsSorted, added with
// this coverage close: before it, RegisterOwnership had no opinion about a
// type RegisterAcknowledged had already claimed, so a type COULD land in
// BOTH maps with no error — and refuseForeignRow's owners-then-acknowledged
// check order would then decide its fate. The tester's mutant D swapped
// that order and nothing caught it, because the two maps are disjoint by
// construction at the one real call site (domain.registerOwnership's class
// partition), so the swap was never exercised either way. This test proves
// the NEW guard makes disjointness a store-level GUARANTEE rather than one
// caller's care — so the swap cannot matter for any caller, present or
// future, and does not need its own witness at the refuseForeignRow level.
func TestG13RegisterOwnershipRefusesAnAlreadyAcknowledgedType(t *testing.T) {
	st := d87Probe(t) // probe_create, probe_free already RegisterAcknowledged
	noop := func(*sql.Tx, json.RawMessage) (string, string, error) { return "", "", nil }
	if err := st.RegisterOwnership("probe_free", noop); err == nil {
		t.Fatal("an already-acknowledged type was accepted as gated")
	}
	if got := st.OwnershipGatedTypes(); len(got) != 1 || got[0] != "probe_modify" {
		t.Errorf("the rejected registration changed the gated set: %v, want [probe_modify]", got)
	}
	// CONTROL: probe_free is left exactly as it was — still usable as the
	// acknowledged (ungated) type it always was, not half-registered.
	if _, err := st.Author("camp-7", 1, d87Payload("probe_free", "row-1"), 0, "default"); err != nil {
		t.Errorf("CONTROL: probe_free stopped authoring after its rejected re-registration: %v", err)
	}
}

// TestG13RegisterAcknowledgedRefusesAnAlreadyGatedType is the OTHER half of
// the same invariant. The guard itself is pre-existing (store/ownership.go,
// predates this coverage close) but no test in the shipped suite had ever
// exercised it.
func TestG13RegisterAcknowledgedRefusesAnAlreadyGatedType(t *testing.T) {
	st := d87Probe(t) // probe_modify already RegisterOwnership'd
	if err := st.RegisterAcknowledged("probe_modify"); err == nil {
		t.Fatal("an already-gated type was accepted as acknowledged")
	}
	// CONTROL: probe_modify is still gated, not silently demoted.
	d87MustAuthor(t, st, "camp-9", d87Payload("probe_create", "row-9"))
	_, err := st.Author("camp-7", 1, d87Payload("probe_modify", "row-9"), 0, "default")
	var own *OwnershipError
	if !errors.As(err, &own) {
		t.Errorf("CONTROL: probe_modify stopped being gated after its rejected acknowledgement: %v (%T)", err, err)
	}
}
