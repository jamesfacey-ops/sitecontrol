// projection.go — projection registration and the canonical dump.
//
// All state is a deterministic fold over the events present (§8.1): fold
// functions consume envelopes inside the apply transaction and write
// projection tables. Because the watermark enforces per-stream contiguity, a
// fold may rely on in-order delivery WITHIN a stream; ACROSS streams it must
// be order-independent — same events, any delivery order, same state. That
// property is what makes injected-event testing a valid substitute for the
// real sync engine (chunk-plan M4), and it is tested with permuted orders.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
)

// Projection is one registered fold target. Name keys the dump; Table/OrderBy
// define the canonical row order (OrderBy must be a unique key — the PK);
// Schema, if set, is idempotent DDL run at registration (how test folds and
// later chunks bring their own tables); Apply, if set, is the fold — called
// for EVERY applied event, in projection-name order, on the apply
// transaction. A nil Apply registers a dump-only table.
type Projection struct {
	Name    string
	Table   string
	OrderBy string
	Schema  string
	Apply   func(tx *sql.Tx, env Envelope) error

	// Finalize, if set, runs ONCE per applied batch, after every envelope's
	// Apply folds, on the same transaction — the seam for derived state that
	// is recomputed from raw tables rather than folded per event (F28: the
	// trip_state recompute ran per event and made a from-zero replay
	// quadratic; batching it per drain removes the multiplier without
	// touching what is computed). `applied` is the batch's applied envelopes
	// in application order; a Finalize must derive everything else from the
	// raw tables, so that state after a batch is a pure function of table
	// contents — exactly the property the per-event recompute had. Author()
	// is a batch of one: authoring semantics are unchanged.
	Finalize func(tx *sql.Tx, applied []Envelope) error
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\s*,\s*[A-Za-z_][A-Za-z0-9_]*)*$`)

// singleIdentRe validates one bare column name (no comma lists) — the
// Eq.Column check for DumpProjectionWhere.
var singleIdentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// RegisterProjection wires a projection into the fold loop and the dump.
// Startup-time only: registration is not safe concurrently with Apply/Author.
func (s *Store) RegisterProjection(p Projection) error {
	if p.Name == "" || p.Table == "" || p.OrderBy == "" {
		return fmt.Errorf("store: projection needs Name, Table and OrderBy (got %q, %q, %q)", p.Name, p.Table, p.OrderBy)
	}
	if !identRe.MatchString(p.Table) || !identRe.MatchString(p.OrderBy) {
		return fmt.Errorf("store: projection %q: Table/OrderBy must be plain identifiers", p.Name)
	}
	for _, q := range s.projections {
		if q.Name == p.Name {
			return fmt.Errorf("store: projection %q already registered", p.Name)
		}
	}
	if p.Schema != "" {
		if err := s.execWriter(p.Schema); err != nil {
			return fmt.Errorf("store: projection %q schema: %w", p.Name, err)
		}
	} else {
		s.wrote()
	}
	s.projections = append(s.projections, p)
	sort.Slice(s.projections, func(i, j int) bool { return s.projections[i].Name < s.projections[j].Name })
	return nil
}

// DumpProjections emits canonical JSON of all registered projections: one
// object keyed by projection name (keys sorted by encoding/json), each value
// the table's rows in OrderBy order, each row an object with sorted column
// keys. Two stores holding the same fold state produce byte-identical dumps —
// this is the substrate of demo step 6's dump-and-compare (chunk-plan C2/C12).
// All tables are read in one transaction, so the dump is a single snapshot.
func (s *Store) DumpProjections() ([]byte, error) {
	// ⟦A3, 2026-09-29⟧ a Transact view IS a single snapshot already — read it
	// rather than waiting on the pool for the connection it holds.
	// ⟦FIX17c⟧ outside a view: one read snapshot (readpool.go), off the writer.
	q, done, err := s.readTx("DumpProjections")
	if err != nil {
		return nil, fmt.Errorf("store: dump: %w", err)
	}
	defer done()

	out := make(map[string][]map[string]any, len(s.projections))
	for _, p := range s.projections {
		rows, err := dumpTable(q, p.Table, p.OrderBy)
		if err != nil {
			return nil, fmt.Errorf("store: dump projection %q: %w", p.Name, err)
		}
		out[p.Name] = rows
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("store: dump marshal: %w", err)
	}
	return b, nil
}

// Eq is one equality condition for DumpProjectionWhere. Column must be a
// single plain identifier (validated at query time, like Table/OrderBy at
// registration). A nil Value matches SQL NULL ("column IS NULL"); anything
// else compares with "column = ?" as a bound parameter — values never enter
// the SQL text.
type Eq struct {
	Column string
	Value  any
}

// DumpProjection emits canonical JSON of ONE registered projection's rows —
// the same array shape as that projection's section of DumpProjections, in
// the same OrderBy order. This is the narrow read for callers that need a
// single table: DumpProjections' whole-database snapshot made every point
// lookup cost a dump of every registered table (finding F30), and the only
// callers that genuinely need the all-tables snapshot are the dump-and-
// compare surfaces.
func (s *Store) DumpProjection(name string) ([]byte, error) {
	return s.DumpProjectionWhere(name)
}

// DumpProjectionWhere is DumpProjection narrowed by equality conditions —
// the sanctioned scoped read (a point lookup, an open-set filter) that keeps
// the *sql.DB unexported (C1 review M2) without making a full-table decode
// the only alternative. It is deliberately NOT a general query surface:
// conditions are conjoined column equalities on one registered projection,
// nothing more.
func (s *Store) DumpProjectionWhere(name string, conds ...Eq) ([]byte, error) {
	s.countRead()
	query, args, err := s.projectionQuery(name, conds)
	if err != nil {
		return nil, err
	}
	rows, err := dumpQuery(s.conn(), query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: dump projection %q: %w", name, err)
	}
	b, err := json.Marshal(rows)
	if err != nil {
		return nil, fmt.Errorf("store: dump projection %q marshal: %w", name, err)
	}
	return b, nil
}

// ProjectionRowsWhere is DumpProjectionWhere stopped before the encoding
// (FIX17 perf): the same query — so the same rows, in the same OrderBy order
// — handed back as the column names and each row's values exactly as the
// dump would encode them (dumpRows: TEXT as string, INTEGER int64, REAL
// float64, NULL nil). domain decodes these straight into its row types,
// which at season scale is several times cheaper than encoding them to JSON
// and decoding that again; domain/dump.go owns the proof that the two
// decodes agree, and falls back to the JSON one wherever it cannot show it.
func (s *Store) ProjectionRowsWhere(name string, conds ...Eq) (cols []string, rows [][]any, err error) {
	s.countRead()
	query, args, err := s.projectionQuery(name, conds)
	if err != nil {
		return nil, nil, err
	}
	cols, rows, err = dumpRows(s.conn(), query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("store: dump projection %q: %w", name, err)
	}
	return cols, rows, nil
}

// projectionQuery is DumpProjectionWhere's one statement: conjoined column
// equalities on one registered projection, ordered by its OrderBy.
func (s *Store) projectionQuery(name string, conds []Eq) (string, []any, error) {
	var proj *Projection
	for i := range s.projections {
		if s.projections[i].Name == name {
			proj = &s.projections[i]
			break
		}
	}
	if proj == nil {
		return "", nil, fmt.Errorf("store: projection %q is not registered", name)
	}
	query := fmt.Sprintf(`SELECT * FROM %s`, proj.Table)
	var args []any
	for i, c := range conds {
		if !singleIdentRe.MatchString(c.Column) {
			return "", nil, fmt.Errorf("store: dump projection %q: condition column %q must be a plain identifier", name, c.Column)
		}
		kw := " AND "
		if i == 0 {
			kw = " WHERE "
		}
		if c.Value == nil {
			query += kw + c.Column + " IS NULL"
		} else {
			query += kw + c.Column + " = ?"
			args = append(args, c.Value)
		}
	}
	query += fmt.Sprintf(` ORDER BY %s`, proj.OrderBy)
	return query, args, nil
}

// querier is what dumpQuery needs from *sql.Tx and *sql.DB alike.
type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func dumpTable(tx querier, table, orderBy string) ([]map[string]any, error) {
	// table and orderBy are registration-validated identifiers, not user input.
	return dumpQuery(tx, fmt.Sprintf(`SELECT * FROM %s ORDER BY %s`, table, orderBy))
}

func dumpQuery(q querier, query string, args ...any) ([]map[string]any, error) {
	cols, rows, err := dumpRows(q, query, args...)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows)) // non-nil: an empty projection dumps as [], never null
	for _, vals := range rows {
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			m[c] = vals[i]
		}
		out = append(out, m)
	}
	return out, nil
}

// dumpRows runs query and returns its columns and every row's values, TEXT
// normalised to string so dumps never depend on driver scan types.
func dumpRows(q querier, query string, args ...any) ([]string, [][]any, error) {
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	var out [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		for i := range vals {
			if b, ok := vals[i].([]byte); ok {
				vals[i] = string(b) // normalise TEXT-as-bytes so dumps never depend on driver scan types
			}
		}
		out = append(out, vals)
	}
	return cols, out, rows.Err()
}
