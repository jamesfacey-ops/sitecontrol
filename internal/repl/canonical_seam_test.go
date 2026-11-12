// canonical_seam_test.go — F9 regression cover at the replication layer: a
// valid-but-non-canonical payload travels the real pull path and must NOT
// make the seam-echo check report prefix_diverged on fully honest traffic.
// Before store.Author canonicalised payloads, the author stored the caller's
// exact bytes while the replica stored the wire re-encoding (whitespace
// stripped, <, >, & escaped), so the very next advancing pull compared two
// different rows at the seam and stalled the stream terminally — with a
// message accusing an un-bumped re-image that never happened.
package repl

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestNonCanonicalPayloadOnHonestTrafficDoesNotTripSeamEcho(t *testing.T) {
	sites := newMesh(t, 2)
	a, b := sites[0], sites[1]
	ctx := context.Background()

	// Author with formatting json.Marshal would never emit: insignificant
	// whitespace and a raw '<'. Valid JSON — store.Author accepts it.
	if _, err := a.st.Author("site-a", 1, json.RawMessage(`{"type":"tick", "k":"b<2"}`), 0, "default"); err != nil {
		t.Fatalf("author non-canonical: %v", err)
	}
	b.eng.PullAll(ctx)
	if wm := mustWatermark(t, b.st, "site-a", 1); wm != 1 {
		t.Fatalf("precondition: replica watermark = %d, want 1", wm)
	}

	// The next authored event forces an ADVANCING pull whose seam echo
	// attests the row at seq 1 — the row whose bytes diverged pre-fix.
	if _, err := a.st.Author("site-a", 1, tick("after"), 0, "default"); err != nil {
		t.Fatalf("author: %v", err)
	}
	b.eng.PullAll(ctx)

	if ss, ok := streamState(b.eng, "site-a", 1); !ok || ss.State != StateOK {
		t.Fatalf("stream (site-a,1) after honest non-canonical traffic = %+v, want %s (F9: seam echo fired on honest traffic)", ss, StateOK)
	}
	if wm := mustWatermark(t, b.st, "site-a", 1); wm != 2 {
		t.Fatalf("replica watermark = %d, want 2 — stream stalled on honest traffic", wm)
	}

	// Both sides must hold byte-identical rows at the seam (the property the
	// hash compare is standing in for).
	ae, err := a.st.Events("site-a", 1, 1, 1)
	if err != nil || len(ae) != 1 {
		t.Fatalf("author events: %v (n=%d)", err, len(ae))
	}
	be, err := b.st.Events("site-a", 1, 1, 1)
	if err != nil || len(be) != 1 {
		t.Fatalf("replica events: %v (n=%d)", err, len(be))
	}
	if !bytes.Equal(ae[0].Payload, be[0].Payload) {
		t.Fatalf("author stored %s but replica stored %s — bytes diverged in flight", ae[0].Payload, be[0].Payload)
	}
}
