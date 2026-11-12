// s120_amend_separator_test.go — S120: the amendment list separator, pinned.
//
// THE GAP. `amendListSep` is the character that joins `occupants_add`,
// `occupants_remove` and `clear` into their stored columns. A mutation audit
// changed it from "," to ";" and the WHOLE TREE stayed green — `./...`
// byte-identical at PASS=1270 FAIL=24 SKIP=1 — while a probe on the same
// build proved the path is genuinely exercised (`joinAmendList` emitted a
// multi-element list twice in `-run TestW9a` alone). [MEASURED 2026-09-04]
//
// ⚠ WHY THE OBVIOUS TEST DOES NOT CATCH IT. Every existing consumer reads
// these columns back through `splitAmendList`, which splits on the SAME
// constant. Encode and decode move together, so every round-trip stays
// self-consistent under any separator — the classic shared-path blindness.
// The only thing that can pin it is an assertion on the STORED BYTES.
//
// ⚠ `AmendTrip` has ZERO test callers anywhere in the tree, so this file
// drives the FOLD directly with a raw envelope — the pattern
// `TestCheckoutFoldNeverClosesAForeignSitesVisit` (checkin_test.go:331)
// establishes, including its `applied == true` guard so a watermark drop
// cannot masquerade as a pass.
package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// TestW9aAmendListSeparatorIsPinnedInTheStoredBytes is the wire-level pin.
func TestW9aAmendListSeparatorIsPinnedInTheStoredBytes(t *testing.T) {
	st := newDomainTestStore(t)

	payload, err := json.Marshal(map[string]any{
		"type":             evtTripAmended,
		"trip_id":          "site-a-trip-s120",
		"occupants_add":    []string{"p-alpha", "p-bravo"},
		"occupants_remove": []string{"p-charlie", "p-delta"},
		"clear":            []string{"cargo_note", "phase_override_reason"},
	})
	if err != nil {
		t.Fatal(err)
	}
	applied, err := st.Apply(store.Envelope{
		SiteID: "site-a", NodeEpoch: 1, Seq: 1,
		SchemaVersion: store.SchemaVersion, OriginAuth: "site-a", Payload: payload,
	})
	if err != nil {
		t.Fatalf("applying the amendment envelope: %v", err)
	}
	// The template's guard: without this, a watermark refusal reads as a pass
	// because the assertions below would simply find nothing.
	if !applied {
		t.Fatal("the amendment envelope was not applied at all (watermark issue, not the rule under test)")
	}

	raw, err := st.DumpProjection("trip_amendment")
	if err != nil {
		t.Fatalf("dumping trip_amendment: %v", err)
	}
	got := string(raw)

	for _, want := range []string{"p-alpha,p-bravo", "p-charlie,p-delta", "cargo_note,phase_override_reason"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the stored trip_amendment row does not contain %q.\n"+
				"`amendListSep` is the WIRE representation of a multi-element amendment list. "+
				"Every reader splits on the same constant, so a changed separator round-trips "+
				"cleanly and is invisible to every behavioural test — the only thing that can "+
				"catch it is this assertion on the stored bytes. A post that wrote one "+
				"separator and a peer that split on another would silently read a two-person "+
				"occupant correction as ONE person with a punctuated id.\nstored row:\n%s",
				want, got)
		}
	}
}

// TestW9aAmendListRoundTripsAndDropsBlanks is the behavioural half — it pins
// what the pair GUARANTEES rather than what it stores.
//
// ⚠ It deliberately does NOT pin the separator: it round-trips, so it is
// blind to it by construction. Both halves are needed, and saying which is
// which is the point — this one would stay green under the mutant above.
func TestW9aAmendListRoundTripsAndDropsBlanks(t *testing.T) {
	if got := joinAmendList(nil); got != nil {
		t.Fatalf("joinAmendList(nil) = %v, want nil — absent is never the empty string: "+
			"nil means NOT AMENDED", got)
	}
	if got := joinAmendList([]string{"  ", ""}); got != nil {
		t.Fatalf("joinAmendList of only-blanks = %v, want nil", got)
	}
	joined, ok := joinAmendList([]string{" p-a ", "", "p-b"}).(string)
	if !ok {
		t.Fatalf("joinAmendList returned %T, want a string for a non-empty list", joined)
	}
	back := splitAmendList(joined)
	if len(back) != 2 || back[0] != "p-a" || back[1] != "p-b" {
		t.Fatalf("round trip gave %v, want [p-a p-b] — blanks are dropped and members trimmed "+
			"on both sides", back)
	}
}
