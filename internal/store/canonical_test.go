// canonical_test.go — F9 regression cover: Author must canonicalise the
// payload to the WIRE encoding, so the bytes a node stores are the bytes a
// replica will store after the wire's json.NewEncoder rewrite, and the
// seam-echo hash can never disagree between honest nodes over formatting.
package store

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestAuthorCanonicalisesNonCanonicalPayloadToTheWireEncoding(t *testing.T) {
	s := newTestStore(t)

	// Valid JSON that json.Marshal would never emit: insignificant
	// whitespace plus a raw '<' (the encoder escapes it as <).
	raw := json.RawMessage(`{"type":"tick", "k":"b<2"}`)

	env, err := s.Author("site-a", 1, raw, 0, "default")
	if err != nil {
		t.Fatalf("author: %v", err)
	}

	// The canonical form is exactly what the wire encoder produces for a
	// RawMessage: compacted and HTML-escaped.
	wire, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Equal(wire, []byte(raw)) {
		t.Fatal("test premise broken: the chosen payload is already canonical")
	}

	if !bytes.Equal(env.Payload, wire) {
		t.Fatalf("returned envelope payload = %s, want wire-canonical %s", env.Payload, wire)
	}
	evs, err := s.Events("site-a", 1, 1, 1)
	if err != nil || len(evs) != 1 {
		t.Fatalf("events: %v (n=%d)", err, len(evs))
	}
	if !bytes.Equal(evs[0].Payload, wire) {
		t.Fatalf("stored payload = %s, want wire-canonical %s", evs[0].Payload, wire)
	}

	// The property the seam echo depends on: hashing the stored row must
	// equal hashing the row after a full wire round-trip (encode the
	// envelope as the pull server does, decode it as the pull client does).
	encoded, err := json.Marshal(evs[0])
	if err != nil {
		t.Fatalf("wire-encode envelope: %v", err)
	}
	var travelled Envelope
	if err := json.Unmarshal(encoded, &travelled); err != nil {
		t.Fatalf("wire-decode envelope: %v", err)
	}
	if evs[0].Hash() != travelled.Hash() {
		t.Fatalf("stored hash %s != wire-round-tripped hash %s — the seam echo would fire on honest traffic",
			evs[0].Hash(), travelled.Hash())
	}
}

func TestAuthorLeavesAlreadyCanonicalPayloadByteIdentical(t *testing.T) {
	s := newTestStore(t)
	canonical := tick("plain") // built via fmt with %q — already compact, no escapable runes
	if got, err := json.Marshal(canonical); err != nil || !bytes.Equal(got, []byte(canonical)) {
		t.Fatalf("test premise broken: tick() payload is not canonical (%s vs %s, err=%v)", canonical, got, err)
	}
	env, err := s.Author("site-a", 1, canonical, 0, "default")
	if err != nil {
		t.Fatalf("author: %v", err)
	}
	if !bytes.Equal(env.Payload, []byte(canonical)) {
		t.Fatalf("canonical payload was rewritten: %s -> %s", canonical, env.Payload)
	}
}

func TestAuthorStillRefusesEmptyAndInvalidPayloadsAfterCanonicalisation(t *testing.T) {
	s := newTestStore(t)
	// A nil RawMessage must stay a refusal — json.Marshal would silently
	// turn it into "null", which would smuggle an empty payload past
	// Validate if canonicalisation ran unguarded.
	if _, err := s.Author("site-a", 1, nil, 0, "default"); err == nil {
		t.Fatal("author accepted a nil payload")
	}
	if _, err := s.Author("site-a", 1, json.RawMessage(`{"broken":`), 0, "default"); err == nil {
		t.Fatal("author accepted invalid JSON")
	}
}
