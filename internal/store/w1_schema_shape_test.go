// w1_schema_shape_test.go — W1-8's introspection half: "No CHECK constraint
// on any new column (schema introspection test)" plus the five-column shape
// of the new time basis. (WINDOW-BUILD-PLAN-v1 §3 W1, LOCKED 2026-08-27;
// TP §4.1 storage rule 1; schema-10 §5B.)
//
// KILLER MUTANT: m4 — add a CHECK to the new columns. The in-repo pattern to
// copy (event_time's clock_state CHECK) is explicitly forbidden: a widened
// CREATE TABLE IF NOT EXISTS … CHECK is a silent no-op on a populated SQLite
// DB and splits it from a fresh one, and the values sit inside Hash(), so
// any write-side rewrite breaks IntegrityCheck on untampered records.
//
// EXPECTED RED (pre-build): event_log carries the seven v9 columns and zero
// new ones at HEAD 89ecee6 — TestW1EventLogGainsExactlyFiveNewColumns fails
// with "0 new columns". That produced red is this file's ledger RED row.
package store

import (
	"regexp"
	"strings"
	"testing"
)

// The replicated time basis is five values (AuthoredAt's triple +
// OccurredAt + TimeSource) landing as matching event_log columns — not a
// registered projection (W1 "Changes"). Names are the builder's; the COUNT
// and the v9 baseline are the locked shape.
func TestW1EventLogGainsExactlyFiveNewColumns(t *testing.T) {
	s := newTestStore(t)
	newCols := w1NewEventLogColumns(t, s)
	if len(newCols) != 5 {
		t.Fatalf("event_log has %d columns beyond the v9 seven (%v), want exactly 5 — the AuthoredAt triple plus occurred_at + time_source (items 6 + 13)", len(newCols), newCols)
	}
}

// No CHECK constraint on any of the new columns: received bytes are stored
// exactly as sent (TP §4.1 rule 1). The v9 CHECKs (node_epoch >= 1, seq >= 1)
// are the only ones event_log may carry.
func TestW1NoCheckConstraintOnAnyNewTimeBasisColumn(t *testing.T) {
	s := newTestStore(t)
	newCols := w1NewEventLogColumns(t, s)
	var ddl string
	if err := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'event_log'`).Scan(&ddl); err != nil {
		t.Fatalf("read event_log DDL: %v", err)
	}
	checks := regexp.MustCompile(`(?is)CHECK\s*\(([^)]*)\)`).FindAllStringSubmatch(ddl, -1)
	for _, m := range checks {
		body := strings.ToLower(m[1])
		// v9 baseline CHECKs guard node_epoch and seq only.
		if strings.Contains(body, "node_epoch") || strings.Contains(body, "seq") {
			continue
		}
		t.Errorf("event_log carries a non-v9 CHECK constraint: CHECK(%s) — mutant m4: no CHECK on any new column", m[1])
	}
	for _, c := range newCols {
		for _, m := range checks {
			if strings.Contains(strings.ToLower(m[1]), strings.ToLower(c)) {
				t.Errorf("new column %q appears in CHECK(%s) — received bytes must be stored as sent (TP §4.1 rule 1, mutant m4)", c, m[1])
			}
		}
	}

	// Positive control for the CHECK-detection channel (a test asserting an
	// ABSENCE needs proof its observation channel works at all): the v9
	// CHECKs must be visible to the same regex over the same DDL read.
	baseline := 0
	for _, m := range checks {
		body := strings.ToLower(m[1])
		if strings.Contains(body, "node_epoch") || strings.Contains(body, "seq") {
			baseline++
		}
	}
	if baseline < 2 {
		t.Fatalf("CHECK-scan control: expected to observe the two v9 CHECKs (node_epoch >= 1, seq >= 1) in event_log's DDL, saw %d — the absence assertion above is not observing anything", baseline)
	}
}
