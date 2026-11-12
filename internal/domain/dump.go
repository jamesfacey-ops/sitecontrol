// dump.go — the one sanctioned way domain code reads projection state
// OUTSIDE a fold's *sql.Tx. Store.DB is deliberately unexported (C1 review
// M2: "the single-writer-connection decision stays a store-internal
// default"), so creation-time checks (trade uniqueness, alias cycle
// detection) and read paths (alias resolution, roster listing) go through
// the store's exported projection reads (DumpProjection /
// DumpProjectionWhere — DumpProjections' whole-database snapshot is only for
// the dump-and-compare surfaces), rather than a second connection pool or a
// raw-SQL backdoor into store's tables.
package domain

import (
	"encoding/json"
	"fmt"
	"slices"

	"sitecontrol/internal/store"
)

// dumpSection decodes one named projection's rows into typed rows, via the
// store's single-projection dump. It reads exactly one table: the older
// whole-database DumpProjections snapshot behind every read made each point
// lookup pay for a dump of every registered table (finding F30) — the
// all-tables call now belongs only to the dump-and-compare surfaces.
func dumpSection[T any](st *store.Store, name string) ([]T, error) {
	return dumpSectionWhere[T](st, name)
}

// dumpSectionWhere is dumpSection narrowed by store.Eq equality conditions —
// the typed face of the store's scoped read (a point lookup by primary key,
// an open-set filter). Row order is the projection's OrderBy, exactly as the
// full section would deliver the same rows.
//
// FIX17 (perf): the rows are read with store.ProjectionRowsWhere — the dump's
// own query and values, minus the JSON — and decoded by decodeRowsDirect,
// which gives exactly what json.Unmarshal of the dump would for the row
// shapes it accepts, and declines everything else. A declined decode, and
// any value it does not recognise, takes the original path: the very bytes
// DumpProjectionWhere would have produced, through json.Unmarshal, so its
// errors and edge cases are unchanged.
func dumpSectionWhere[T any](st *store.Store, name string, conds ...store.Eq) ([]T, error) {
	cols, vals, err := st.ProjectionRowsWhere(name, conds...)
	if err != nil {
		return nil, fmt.Errorf("domain: dump: %w", err)
	}
	if rows, ok := decodeRowsDirect[T](cols, vals); ok {
		return rows, nil
	}
	maps := make([]map[string]any, 0, len(vals))
	for _, row := range vals {
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			m[c] = row[i]
		}
		maps = append(maps, m)
	}
	b, err := json.Marshal(maps)
	if err != nil {
		return nil, fmt.Errorf("domain: dump: store: dump projection %q marshal: %w", name, err)
	}
	var rows []T
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, fmt.Errorf("domain: decode projection %q: %w", name, err)
	}
	return rows, nil
}

// memoSection is dumpSection reused between writes (FIX17 perf; store/memo.go
// says what "between writes" means and why it is exact). It is for the
// season-sized tables the officer pages read whole several times per request
// — a trip row per gate check-in makes trip, trip_state and trip_arrival
// ~14k rows each at season scale, and decoding one through the canonical
// dump costs ~100 ms. The caller gets its OWN slice (a shallow copy: callers
// sort and filter in place; no reader writes through a row's pointers), so
// the shared value is never mutated. Rows, order and nil-ness are exactly
// dumpSection's.
//
// Deliberately NOT used for the reference-data and amendment tables: their
// readers are the scoped-read probes' subjects (w5d/w6/w8/w9a), and they are
// small.
func memoSection[T any](st *store.Store, name string) ([]T, error) {
	v, err := st.Memo("domain.section."+name, func() (any, error) {
		return dumpSection[T](st, name)
	})
	if err != nil {
		return nil, err
	}
	return slices.Clone(v.([]T)), nil
}
