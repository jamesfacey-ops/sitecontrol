// d92_step3_helpers_test.go — the shared machinery of step 3's per-carrier
// D-92 witnesses (w5d_causal_compose_test.go, w8_causal_compose_test.go,
// w7_causal_compose_test.go, w9a_causal_compose_test.go,
// w4_causal_resolution_test.go). This file holds NO assertions.
//
// The person carrier's witnesses (w3_causal_compose_test.go, step 2) are the
// template; these helpers are the carrier-agnostic pieces of them, written
// once because the six clones read ONE rule (resolveContest, generic since
// step 3) and their witnesses should be told apart by fixture, never by
// helper drift.
package domain

import (
	"encoding/json"
	"reflect"
	"testing"

	"sitecontrol/internal/store"
)

// d92SeenOf decodes the `seen` list off any carrier's payload bytes.
func d92SeenOf(t *testing.T, env store.Envelope) []store.Key {
	t.Helper()
	var pl struct {
		Seen []SeenRef `json:"seen"`
	}
	if err := json.Unmarshal(env.Payload, &pl); err != nil {
		t.Fatal(err)
	}
	var out []store.Key
	for _, r := range pl.Seen {
		out = append(out, r.key())
	}
	return out
}

// d92HasSeenKey reports whether the raw payload object carries a `seen`
// key at all — the omitempty half of the wire convention.
func d92HasSeenKey(t *testing.T, env store.Envelope) bool {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(env.Payload, &raw); err != nil {
		t.Fatal(err)
	}
	_, has := raw["seen"]
	return has
}

// d92Remote authors EXACTLY ONE event as siteID at a scratch post primed
// with `primer` (w3AuthorRemote, which runs a REAL edge so the stamp is the
// scratch post's own honest reading), and returns it.
func d92Remote(t *testing.T, primer []store.Envelope, siteID string, clockMS int64, author func(rs *store.Store)) store.Envelope {
	t.Helper()
	evs := w3AuthorRemote(t, primer, siteID, 1, clockMS, author)
	if len(evs) != 1 {
		t.Fatalf("fixture defect: %s minted %d events, want exactly 1", siteID, len(evs))
	}
	return evs[0]
}

// d92RequireNoSeenField asserts an authoring-edge INPUT type offers no
// field a caller could supply the stamp through.
func d92RequireNoSeenField(t *testing.T, input any) {
	t.Helper()
	rt := reflect.TypeOf(input)
	if _, ok := rt.FieldByName("Seen"); ok {
		t.Fatalf("D-92: %s offers a Seen field — the stamp is the authoring post's own honest reading of its record set and must not be supplied by a caller", rt.Name())
	}
}

// d92RequireCausalArrangement is the rule-6 guard every causal fixture
// states: the local key is the LESSER one (so fixed order names the peer and
// disagrees with the rule), the local author saw exactly the peer's key, and
// the peer's author saw nothing standing.
func d92RequireCausalArrangement(t *testing.T, local, peer store.Envelope) {
	t.Helper()
	if !local.Key().Less(peer.Key()) {
		t.Fatalf("fixture defect: the local correction %v is not the lesser key against %v — the fixed-order rival would then agree with the rule and the fixture certifies nothing", local.Key(), peer.Key())
	}
	if seen := d92SeenOf(t, local); !w3KeysEqual(seen, []store.Key{peer.Key()}) {
		t.Fatalf("D-92 (the dropped stamp): the local correction carries seen=%v, want exactly [%v] — the key its author saw standing", seen, peer.Key())
	}
	if seen := d92SeenOf(t, peer); len(seen) != 0 {
		t.Fatalf("fixture defect: the peer's assertion carries seen=%v, want none (it was authored over the bare record)", seen)
	}
}

// d92RequireConcurrentArrangement is the rule-6 guard of every tiebreak
// fixture: each side saw exactly `shared` (the history both held when the
// link dropped) and NOT the other.
func d92RequireConcurrentArrangement(t *testing.T, a, b store.Envelope, shared []store.Key) {
	t.Helper()
	for _, e := range []store.Envelope{a, b} {
		if seen := d92SeenOf(t, e); !w3KeysEqual(seen, shared) {
			t.Fatalf("fixture defect: %v carries seen=%v, want %v — a genuinely concurrent pair saw the same history and not each other", e.Key(), seen, shared)
		}
	}
}
