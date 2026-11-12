// ra1_ownminted_probe_test.go — merged from RA1-OWNMINTED-RERUN.md's audit
// branch (app `audit/fable-ra1-rerun-probe` @ 0d790fd, Fable, 2026-09-13),
// per that report's 🟠 finding: the SchemaVersion-12 half of orientation's
// "ownMinted" classification is UNWATCHED. Four mutants on orientation.go
// were run at c01bc1e: M1 (completion row + derived pair keyed on a
// payload site_id) and M2 (same for retraction) are killed by three
// shipped tests; M3 (entry-on-completion visit minted under a payload
// site_id) and M3b (the visit tagged with the REFERENCED ARRIVAL's site —
// "the entry belongs to the arrival", a plausible refactor) both produced
// ZERO new failures. The step-4 ownership witnesses (see
// orientation_ownership_witness_test.go) plant a forged site and check
// completion/retraction/derived rows; nobody extended them when the fold
// started minting a PRESENCE row (the visit) alongside the completion.
//
// ONLY the assertion that kills M3b is merged here (the audit's own other
// three tests re-confirm M1/M2/the PK-collision refusal, already covered by
// shipped witnesses, and are not brought over per FALSIFIABILITY-2.md's
// witness-gaps brief: "merge THAT assertion", not the branch wholesale).
package domain

import (
	"database/sql"
	"encoding/json"
	"testing"

	"sitecontrol/internal/store"
)

func ra1Count(t *testing.T, st *store.Store, q string, args ...any) int {
	t.Helper()
	var n int
	if err := st.LocalTx(func(tx *sql.Tx) error { return tx.QueryRow(q, args...).Scan(&n) }); err != nil {
		t.Fatalf("count %q: %v", q, err)
	}
	return n
}

func ra1Author(t *testing.T, st *store.Store, site string, payload any) (store.Envelope, error) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return st.Author(site, 1, raw, 0, "default")
}

// ra1World: person diaz + quinn seeded by site-a; diaz holds site-a's
// orientation (site-a's own wire completion); site-a has an arrival with an
// OPEN visit for diaz. The post under test authors as site-b.
func ra1World(t *testing.T) *store.Store {
	t.Helper()
	st := w9bNewStore(t)
	w9bSeedPersons(t, st, "site-a", 1, "diaz", "quinn")
	next := orientWatermark(t, st, "site-a", 1) + 1
	w9bApply(t, st, w9bEnvelope("site-a", 1, next, w9bBase, orientWire(evtOrientationCompleted, "diaz", w9bStr("NPF"))))
	arr := w9bWireArrival("site-a-arr-1", "", []ArrivalVisit{{VisitID: "site-a-visit-1", PersonID: "diaz"}}, w9bStr("NPF"))
	w9bApply(t, st, w9bEnvelope("site-a", 1, next+1, w9bBase+w9bMinute, arr))
	if orientHeld(t, st, "diaz", "site-a") == nil {
		t.Fatal("FIXTURE: diaz does not hold site-a's orientation")
	}
	if ra1Count(t, st, `SELECT count(*) FROM visit WHERE visit_id='site-a-visit-1' AND site_id='site-a' AND is_open=1`) != 1 {
		t.Fatal("FIXTURE: site-a's open visit did not land")
	}
	return st
}

// TestRA1Rerun_CompletionFromBWithForgedSiteAndForeignRefsWritesOnlySiteBRows
// is RA1-OWNMINTED-RERUN.md's own probe: a raw, hand-authored
// orientation_completed at site-b carries a FORGED "site_id":"site-a" field
// (not a real wire key — proving the fold never reads it) PLUS a FOREIGN
// arrival_ref/trip_ref naming site-a's own arrival/trip, plus a visit. The
// one assertion this file exists for is the visit's site_id: it must be
// tagged with the AUTHORING ENVELOPE's site (site-b) — not the forged
// payload site_id (M3) and not the foreign arrival_ref's site (M3b, the
// plausible "the entry belongs to the arrival" refactor). Everything else
// in this test reconfirms site-a is completely untouched.
func TestRA1Rerun_CompletionFromBWithForgedSiteAndForeignRefsWritesOnlySiteBRows(t *testing.T) {
	st := ra1World(t)
	beforeA := ra1Count(t, st, `SELECT count(*) FROM visit WHERE site_id='site-a'`) +
		ra1Count(t, st, `SELECT count(*) FROM orientation_completion WHERE site_id='site-a'`) +
		ra1Count(t, st, `SELECT count(*) FROM trip_arrival WHERE site_id='site-a'`)
	p := map[string]any{
		"type": evtOrientationCompleted, "person_id": "diaz", "recorded_by": "Pad",
		"site_id":     "site-a",        // forged claim: not a wire key
		"arrival_ref": "site-a-arr-1",  // FOREIGN arrival (the edge would never attach it)
		"trip_ref":    "site-a-trip-1", // foreign reference
		"visit":       map[string]any{"visit_id": "site-b-visit-probe", "person_id": "diaz", "person_name": "diaz"},
	}
	env, err := ra1Author(t, st, "site-b", p)
	if err != nil {
		t.Fatalf("funnel: %v", err)
	}
	t.Logf("MEASURED: funnel passed a completion carrying site_id=site-a + foreign arrival_ref; landed at %v", env.Key())
	if got := ra1Count(t, st, `SELECT count(*) FROM orientation_completion WHERE site_id='site-b' AND node_epoch=? AND seq=?`, env.NodeEpoch, env.Seq); got != 1 {
		t.Fatalf("completion row not tagged site-b: %d", got)
	}
	if got := ra1Count(t, st, `SELECT count(*) FROM visit WHERE visit_id='site-b-visit-probe' AND site_id=?`, env.SiteID); got != 1 {
		t.Fatalf("THE MISSING GUARD'S ASSERTION: entry-on-completion visit is not tagged with the ENVELOPE's site (site-b): %d", got)
	}
	if orientHeld(t, st, "diaz", "site-b") == nil {
		t.Fatal("derived (diaz, site-b) missing")
	}
	afterA := ra1Count(t, st, `SELECT count(*) FROM visit WHERE site_id='site-a'`) +
		ra1Count(t, st, `SELECT count(*) FROM orientation_completion WHERE site_id='site-a'`) +
		ra1Count(t, st, `SELECT count(*) FROM trip_arrival WHERE site_id='site-a'`)
	if beforeA != afterA {
		t.Fatalf("CROSS-SITE WRITE: site-a row count moved %d -> %d", beforeA, afterA)
	}
	if ra1Count(t, st, `SELECT count(*) FROM visit WHERE visit_id='site-a-visit-1' AND site_id='site-a' AND is_open=1`) != 1 {
		t.Fatal("CROSS-SITE WRITE: site-a's open visit was closed by site-b's completion")
	}
	t.Log("MEASURED: every row written is site-b-tagged; site-a's arrival, visit and completion rows are byte-for-byte unmoved")
}
