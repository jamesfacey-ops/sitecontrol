// w1_v9_refusal_test.go — W1-2's replication half, in the package that owns
// the visible refusal: the current binary pulling a stream of envelopes
// stamped the LITERAL PREDECESSOR schema_version refuses them at ApplyBatch
// and surfaces the §8.5 refusal per stream — "re-proven at v10, not assumed
// from the v9 tests (the D21-REPLAY §5.4 behaviour re-established on the new
// binary)". (WINDOW-BUILD-PLAN-v1 §3 W1, LOCKED 2026-08-27.)
//
// ⟦SchemaVersion 13 — RE-PINNED AGAIN 2026-09-11, R-17⟧ The edge this
// criterion is defined at moved with the increment again: the binary is v13
// and the literal predecessor — the exact wire traffic a not-yet-upgraded
// post would send during the both-posts-down window's ragged edge — is 12.
// That is [[D-39]]'s refusal at log.go's ApplyBatch gate, confirmed to fire
// on 13. ⟦was: "the binary is v12 … the literal predecessor … is 11", itself
// re-pinned 2026-09-11 (R-16) from "the binary is v11 … the literal
// predecessor … is 10"⟧
//
// ⟦SchemaVersion 14 — RE-PINNED AGAIN 2026-09-13, R-18⟧ the binary is v14
// and the literal predecessor is 13, confirmed to fire on 14. ⟦was: "the
// binary is v13 … the literal predecessor … is 12"⟧
//
// ⟦SchemaVersion 15 — RE-PINNED AGAIN 2026-09-13, R-20 + R-23 (the rejoin
// path)⟧ the binary is v15 and the literal predecessor is 14, confirmed to
// fire on 15. ⟦was: "the binary is v14 … the literal predecessor … is 13"⟧
//
// ⟦SchemaVersion 16 — RE-PINNED AGAIN 2026-10-04, R3 alert_kind (fix round
// 2)⟧ the binary is v16 and the literal predecessor is 15, confirmed to fire
// on 16. ⟦was: "the binary is v15 … the literal predecessor … is 14"⟧
//
// The existing v9 suite proves this only for SchemaVersion+1 — a version
// that has never been a real predecessor. This fixture serves the literal
// predecessor.
//
// KILLER MUTANT: m6 — a version gate that admits a v9 envelope at
// ApplyBatch (or a best-effort apply path around the typed refusal).
//
// EXPECTED RED (pre-build): store.SchemaVersion is still 9 at HEAD 89ecee6,
// so the literal-9 stream IS current and applies cleanly — the watermark
// assertion fails with watermark=3. That produced red is this file's ledger
// RED row.
package repl

import (
	"context"
	"net/http/httptest"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/store"
)

func TestW1V16BinaryRefusesLiteralV15EnvelopesAtApplyAndSurfacesTheRefusal(t *testing.T) {
	r := &rig{siteID: "site-x"}
	r.setStreams(StreamHead{NodeEpoch: 1, Head: 3})
	r.setEvents(1, mkStream("site-x", 1, 3, 15 /* the literal predecessor version */, "site-x"))
	rs := httptest.NewServer(r.handler())
	defer rs.Close()

	eng, st := engineWithPeers(t, "site-b",
		config.Peer{SiteID: "site-x", BaseURL: rs.URL, Key: "k"},
	)
	eng.PullAll(context.Background())

	if wm := mustWatermark(t, st, "site-x", 1); wm != 0 {
		t.Fatalf("v15-stamped stream was applied by the v16 binary: watermark=%d, want 0 (W1-2, mutant m6)", wm)
	}
	evs, err := st.Events("site-x", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 0 {
		t.Fatalf("event_log holds %d v15 rows — the log must never hold events of a foreign schema version", len(evs))
	}
	ss, ok := streamState(eng, "site-x", 1)
	if !ok || ss.State != StateSchemaMismatch {
		t.Fatalf("stream (site-x,1) state = %+v, want %s — the refusal must be VISIBLE, never a silent stall (§8.5)", ss, StateSchemaMismatch)
	}
	// Sanity pin: the binary this test ran under is actually v16 — without
	// this, the whole test can go green against a v17 tree for the wrong
	// version edge.
	if store.SchemaVersion != 16 {
		t.Fatalf("store.SchemaVersion = %d; this criterion is defined at the 15→16 edge", store.SchemaVersion)
	}
}
