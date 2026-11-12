// w1_eventtime_gone_test.go — W1-3: "event_time is gone: the table, its
// writer, its exemption. No fold writes wall time; no projection column holds
// it; no ordering/correlation/deadline path reads it — the guard-three
// allowlist scan (A §6's defined surface: Go tokens + SQL columns +
// templates) passes, with a positive control (planted violation detected)."
// (WINDOW-BUILD-PLAN-v1 §3 W1, LOCKED 2026-08-27; PLAN-SEC-A §6 Amendment 1.)
//
// KILLER MUTANT / CONTROL PAIRING: m5 — a planted wall-time read/reference is
// the scan's paired positive control (the W1 ledger's CONTROL row records the
// pairing; §5 CONTROL: "W1-m5").
//
// EXPECTED RED (pre-build): the unfixed tree still holds
// internal/store/eventtime.go (the event_time table DDL, writeEventTime, the
// version-gate exemption) and log.go still calls writeEventTime — both the
// live-database check and the source scan below fail against HEAD 89ecee6.
// That produced red is this file's ledger RED row.
//
// Accepted residue, named per Amendment 1's shared allowlist discipline (the
// escapes a token scan cannot see, recorded rather than pretended away):
// `SELECT *`, whole-envelope JSON/template rendering, and helper reuse (a
// sanctioned rendering helper included from a new surface). The guard against
// those is the allowlist naming surfaces and being reviewed on template
// change — not this scan.
package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// w1V9EventLogColumns is the measured v9 event_log column set (store.go
// schema const at HEAD 89ecee6). Anything beyond these after W1 is a new
// wall-time-basis column and falls under guard scrutiny.
var w1V9EventLogColumns = map[string]bool{
	"site_id": true, "node_epoch": true, "seq": true, "schema_version": true,
	"origin_auth": true, "payload": true, "row_hash": true,
}

// w1NewEventLogColumns introspects a fresh store for the columns W1 adds to
// event_log (the five: the AuthoredAt triple + occurred_at + time_source,
// whatever the builder names them — discovered, not presumed).
func w1NewEventLogColumns(t *testing.T, s *Store) []string {
	t.Helper()
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info('event_log')`)
	if err != nil {
		t.Fatalf("introspect event_log: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if !w1V9EventLogColumns[name] {
			out = append(out, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// The deletion, proven against the live database: no event_time table, no
// event_time trigger, in a freshly self-created v10 schema.
func TestW1EventTimeTableAndTriggersAreGoneFromTheLiveSchema(t *testing.T) {
	s := newTestStore(t)
	rows, err := s.db.Query(`SELECT type, name FROM sqlite_master WHERE name LIKE 'event_time%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var leftovers []string
	for rows.Next() {
		var typ, name string
		if err := rows.Scan(&typ, &name); err != nil {
			t.Fatal(err)
		}
		leftovers = append(leftovers, typ+" "+name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("W1-3: the site-local event_time index must be deleted (item 6), but the schema still creates: %v", leftovers)
	}
}

// The deletion, proven at source: no live Go identifier or SQL/string
// reference to the event_time table, its writer, or its schema const
// anywhere in the production tree (comments excluded — narrating the
// deletion is allowed; referencing the deleted thing is not), and no
// reference in templates or the deploy tree (§6 step 3: grep the WHOLE
// deploy tree for deleted names, not just *.ps1 — the demo-asserts.sh
// counter-example).
func TestW1NoSourceReferencesEventTimeTableWriterOrExemption(t *testing.T) {
	root := w1ModuleRoot(t)
	goDirs := []string{
		filepath.Join(root, "internal", "store"),
		filepath.Join(root, "internal", "domain"),
		filepath.Join(root, "internal", "repl"),
		filepath.Join(root, "internal", "web"),
		filepath.Join(root, "internal", "deadline"),
		filepath.Join(root, "internal", "config"),
		filepath.Join(root, "internal", "humantime"),
		filepath.Join(root, "cmd", "sitectl"),
	}
	forbiddenIdents := map[string]bool{
		"writeEventTime":  true,
		"eventTimeSchema": true,
	}
	forbiddenInStrings := []string{"event_time"}

	var hits []w1Hit
	for _, f := range w1GoFiles(t, goDirs...) {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		hits = append(hits, w1ScanGoTokens(t, f, src, forbiddenIdents, forbiddenInStrings)...)
	}
	// Templates + the whole deploy tree, raw substring.
	rawRoots := []string{
		filepath.Join(root, "internal", "web", "templates"),
		filepath.Join(root, "deploy"),
	}
	for _, rr := range rawRoots {
		err := filepath.WalkDir(rr, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			hits = append(hits, w1ScanRaw(t, path, []string{"event_time"})...)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", rr, err)
		}
	}
	if len(hits) != 0 {
		for _, h := range hits {
			t.Errorf("W1-3 leftover: %s:%d — %s", h.File, h.Line, h.Detail)
		}
		t.Fatal("event_time (table/writer/exemption) still referenced by live source — the site-local index must be deleted whole (W1-3)")
	}
}

// Guard scan over the NEW wall-time-basis columns (guard three as restated by
// item 6 Amendment 1, plus guards one and two):
//
//	guard 1 — no index on any new column;
//	guard 2 — internal/deadline references neither the new columns nor the
//	          new envelope fields, at source level;
//	guard 3 — no projection DDL outside event_log holds a new column; no SQL
//	          read of a new column in an ordering/filtering context outside
//	          the ONE named reader allowlist (Amendment 1's single list).
func TestW1GuardScanNewTimeBasisColumnsHaveNoIndexNoProjectionCopyNoForbiddenReader(t *testing.T) {
	root := w1ModuleRoot(t)
	s := newTestStore(t)
	newCols := w1NewEventLogColumns(t, s)
	// The five-column count itself is W1-8a's assertion; this scan guards
	// however many exist.

	// Guard 1: no index whose definition mentions a new column.
	rows, err := s.db.Query(`SELECT name, COALESCE(sql,'') FROM sqlite_master WHERE type = 'index'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, sql string
		if err := rows.Scan(&name, &sql); err != nil {
			t.Fatal(err)
		}
		for _, c := range newCols {
			if strings.Contains(strings.ToLower(sql), c) {
				t.Errorf("guard 1 violated: index %s covers new time-basis column %s (no index on the time column)", name, c)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	// Guard 2: internal/deadline must not reference the field at all —
	// source-level assertion (columns AND Go field tokens).
	deadlineIdents := map[string]bool{
		"AuthoredAt": true, "OccurredAt": true, "TimeSource": true, "WallMS": true,
	}
	var deadlineStrings []string
	deadlineStrings = append(deadlineStrings, newCols...)
	for _, f := range w1GoFiles(t, filepath.Join(root, "internal", "deadline")) {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range w1ScanGoTokens(t, f, src, deadlineIdents, deadlineStrings) {
			t.Errorf("guard 2 violated: %s:%d — %s (internal/deadline must not reference the time basis)", h.File, h.Line, h.Detail)
		}
	}

	// Guard 3a: no CREATE TABLE / CREATE INDEX DDL outside internal/store's
	// event_log holds a new column name (no projection column holds wall
	// time). Scanned over every production Go file's string literals in the
	// packages that own DDL.
	ddlDirs := []string{
		filepath.Join(root, "internal", "domain"),
		filepath.Join(root, "internal", "repl"),
		filepath.Join(root, "internal", "web"),
		filepath.Join(root, "internal", "deadline"),
	}
	for _, f := range w1GoFiles(t, ddlDirs...) {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		content := string(src)
		if !strings.Contains(strings.ToUpper(content), "CREATE ") {
			continue
		}
		// ⟦PLAN OWNER 2026-08-29 — scope narrowed to what this guard's own
		// rule and comment say: DDL. It scanned the WHOLE FILE, so a column
		// name matched inside a code comment, or inside an unrelated string
		// constant, or as a substring of a longer identifier. All three fired
		// falsely against W4 (item 14): `time_source` in a doc comment,
		// `occurred_at` in `factOccurredAt` (a fact NAME for the Outcome
		// machinery, not DDL), and `occurred_at` inside the pinned payload
		// literal `corrected_occurred_at`.
		//
		// None is a projection column holding wall time, which is the rule.
		// W1-3's subject is `event_time` — the deleted site-local recording
		// table — and its clause is "no PROJECTION COLUMN holds it". W4's
		// corrected instant is a different datum and a B-2-frozen literal, so
		// neither locked criterion could give way; only the implementation
		// could. Now: DDL string literals only, whole identifiers only.
		// The planted-violation CONTROL below was re-run at this narrowing.⟧
		content = w1DDLLiterals(t, f, src)
		for _, c := range newCols {
			// ⟦PLAN OWNER 2026-08-29 — NARROWED to the rule this guard states.
			// It read `strings.Contains(content, c)`, which matched any
			// SUBSTRING anywhere in a file containing "CREATE " — including
			// comments, unrelated constants, and longer identifiers that
			// merely contain a column name. W4 (item 14) tripped it on
			// `corrected_occurred_at`, which contains `occurred_at`.
			//
			// That was a false positive against TWO locked criteria at once:
			// W1-3's own text scopes it to `event_time` — "no PROJECTION
			// COLUMN holds it" — while `corrected_occurred_at` is item 14's
			// payload datum (an officer's claim about when a DIFFERENT event
			// occurred), and it is one of W4's pinned payload literals, frozen
			// on gate B-2's list. So neither the column name nor the criterion
			// could give way; only the over-broad implementation could.
			//
			// Now matched at word boundaries, which is what "declares a column
			// named c" means in DDL. A column literally named `occurred_at`
			// still reds — proven by the planted control below, which is this
			// criterion's owed CONTROL row and was re-run at the narrowing.⟧
			if w1DeclaresColumn(content, c) {
				t.Errorf("guard 3 violated: %s declares or references new time-basis column %q outside event_log (no projection column holds wall time)", f, c)
			}
		}
	}

	// Guard 3b: within internal/store, any SQL that uses a new column in an
	// ordering/filtering context must sit inside Amendment 1's ONE named
	// reader allowlist — default-deny, membership an explicit diffable list,
	// exclusion-shaped. Extending this list is a visible, deliberate diff.
	allowedReaders := map[string]bool{
		// today's legitimate production event_log readers (Amendment 1):
		"Events": true, "Epochs": true, "TimeIndexCoverage": true, "verifyRowHashes": true,
		// the re-pointed point-wise API (item 6 keeps these, re-pointed):
		"EventTimeAt": true, "SeqAsOf": true, "AsOf": true,
		// Amendment 2's stream + mirror, named now so their landing extends
		// nothing:
		"EachEvent": true, "SeqFrom": true,
	}
	// The package's string consts travel with the scan: a fragment declared
	// at package scope is judged inside the reader that splices it.
	storeFiles := w1GoFiles(t, filepath.Join(root, "internal", "store"))
	storeConsts := w1PackageStringConsts(t, storeFiles)
	for _, f := range storeFiles {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range w1StoreSQLReadHitsOutsideAllowlist(t, f, src, newCols, allowedReaders, storeConsts) {
			t.Errorf("guard 3 violated: %s:%d — %s (reader not on Amendment 1's allowlist)", h.File, h.Line, h.Detail)
		}
	}
}

// Positive control (the standing rule: a scan is invalid until its failure
// mode has been produced). Both scan channels must detect a planted
// violation before any clean pass counts — this is m5's paired CONTROL row.
func TestW1EventTimeScanPositiveControlDetectsPlantedViolations(t *testing.T) {
	// Channel 1: Go token scan — planted writer call and planted SQL.
	planted := []byte(`package planted

const plantedSchema = ` + "`CREATE TABLE IF NOT EXISTS event_time (x TEXT)`" + `

func plant() { writeEventTime(nil, Key{}, ClockReading{}) }
`)
	hits := w1ScanGoTokens(t, "planted.go", planted,
		map[string]bool{"writeEventTime": true, "eventTimeSchema": true},
		[]string{"event_time"})
	if len(hits) < 2 {
		t.Fatalf("scan control: planted event_time SQL + writeEventTime call produced %d hits, want >= 2 — the W1-3 scan cannot see its own violation class", len(hits))
	}
	// The comment exclusion must NOT swallow live code: a comment-only
	// mention is legitimately ignored…
	commentOnly := []byte("package planted\n\n// the event_time table was deleted at v10; writeEventTime went with it\n")
	if got := w1ScanGoTokens(t, "comment.go", commentOnly,
		map[string]bool{"writeEventTime": true}, []string{"event_time"}); len(got) != 0 {
		t.Fatalf("scan control: comment-only mention flagged (%v) — the scan would then fail forever on the bump's own doc comment", got)
	}

	// Channel 2: raw scan (templates / deploy tree).
	dir := t.TempDir()
	tmpl := filepath.Join(dir, "planted.html")
	if err := os.WriteFile(tmpl, []byte("<td>{{ .FromEventTime }} event_time</td>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := w1ScanRaw(t, tmpl, []string{"event_time"}); len(got) == 0 {
		t.Fatal("scan control: planted template reference to event_time not detected")
	}

	// Channel 3: the guard-3b SQL-context detector.
	sqlPlant := []byte("package planted\n\nfunc badReader(q queryer) {\n\t_ = q.Query(`SELECT seq FROM event_log WHERE occurred_at <= ? ORDER BY occurred_at`)\n}\n")
	got := w1StoreSQLReadHitsOutsideAllowlist(t, "sqlplant.go", sqlPlant, []string{"occurred_at"}, map[string]bool{}, nil)
	if len(got) == 0 {
		t.Fatal("scan control: planted WHERE/ORDER BY read of occurred_at not detected by the guard-3 scan")
	}

	// Channel 3, FRAGMENT shapes [MEASURED 2026-09-09, GUARD3B-FRAGMENT.md]:
	// the same predicate spread across Go statements was GREEN under the
	// per-literal detector — `q += \` AND occurred_at <= ?\`` inside the real
	// store.EachEvent, and inside a planted non-allowlisted reader, both
	// passed while the in-literal spelling failed. Every plant below must RED
	// with an EMPTY allowlist; every negative must stay GREEN, because the
	// scan's boundary is the READ, not every mention of the column.
	fragmentPlants := map[string]string{
		"q += fragment (the store.EachEvent shape)":                                       "package planted\n\nfunc badReader(db queryer, toSeq int64) {\n\tq := `SELECT seq FROM event_log WHERE site_id = ? AND seq >= ?`\n\tif toSeq > 0 {\n\t\tq += ` AND seq <= ?`\n\t}\n\tq += ` AND occurred_at <= ?`\n\tq += ` ORDER BY seq`\n\t_ = db.Query(q)\n}\n",
		"q = q + fragment":                                                                "package planted\n\nfunc badReader(db queryer) {\n\tq := `SELECT seq FROM event_log WHERE site_id = ?`\n\tq = q + ` AND occurred_at <= ?`\n\t_ = db.Query(q)\n}\n",
		"concatenation inside the call argument":                                          "package planted\n\nfunc badReader(db queryer) {\n\tq := `SELECT seq FROM event_log WHERE site_id = ?`\n\t_ = db.Query(q + ` AND occurred_at <= ?`)\n}\n",
		"fragment variable declared before the base":                                      "package planted\n\nfunc badReader(db queryer) {\n\tfrag := ` AND occurred_at <= ?`\n\tq := `SELECT seq FROM event_log WHERE site_id = ?` + frag\n\t_ = db.Query(q)\n}\n",
		"fmt.Sprintf with the predicate as %s":                                            "package planted\n\nfunc badReader(db queryer) {\n\tq := fmt.Sprintf(`SELECT seq FROM event_log WHERE site_id = ? AND %s`, `occurred_at <= ?`)\n\t_ = db.Query(q)\n}\n",
		"fmt.Sprintf with an order KEY as %s (no operator: layer 2 alone)":                "package planted\n\nfunc badReader(db queryer) {\n\tq := fmt.Sprintf(`SELECT seq FROM event_log WHERE site_id = ? ORDER BY %s`, `occurred_at`)\n\t_ = db.Query(q)\n}\n",
		"strings.Builder":                                                                 "package planted\n\nfunc badReader(db queryer) {\n\tvar b strings.Builder\n\tb.WriteString(`SELECT seq FROM event_log WHERE site_id = ?`)\n\tb.WriteString(` AND occurred_at <= ?`)\n\t_ = db.Query(b.String())\n}\n",
		"order keys appended to a slice, joined after the ORDER BY literal (no operator)": "package planted\n\nfunc badReader(db queryer) {\n\tkeys := []string{`seq`}\n\tkeys = append(keys, `occurred_at`)\n\tq := `SELECT seq FROM event_log WHERE site_id = ? ORDER BY ` + strings.Join(keys, `, `)\n\t_ = db.Query(q)\n}\n",
		"predicates appended to a slice, joined into the WHERE":                           "package planted\n\nfunc badReader(db queryer) {\n\tvar where []string\n\twhere = append(where, `site_id = ?`)\n\twhere = append(where, `occurred_at <= ?`)\n\tq := `SELECT seq FROM event_log WHERE ` + strings.Join(where, ` AND `)\n\t_ = db.Query(q)\n}\n",
		"a built string returned, not queried":                                            "package planted\n\nfunc badReader() string {\n\tq := `SELECT seq FROM event_log WHERE site_id = ?`\n\treturn q + ` AND occurred_at <= ?`\n}\n",
		"same-file package const fragment":                                                "package planted\n\nconst timeClause = ` AND occurred_at <= ?`\n\nfunc badReader(db queryer) {\n\t_ = db.Query(`SELECT seq FROM event_log WHERE site_id = ?` + timeClause)\n}\n",
		"package const built from another const, holding the whole predicate":             "package planted\n\nconst (\n\tbaseSelect = `SELECT seq FROM event_log`\n\tbyTimeSQL  = baseSelect + ` WHERE site_id = ? AND occurred_at <= ?`\n)\n\nfunc badReader(db queryer) {\n\t_ = db.Query(byTimeSQL)\n}\n",
	}
	for name, src := range fragmentPlants {
		consts := w1StringConstsFromSources(t, []byte(src))
		got := w1StoreSQLReadHitsOutsideAllowlist(t, "fragment_plant.go", []byte(src), []string{"occurred_at"}, map[string]bool{}, consts)
		if len(got) == 0 {
			t.Errorf("scan control (fragment shape %q): the appended occurred_at predicate was NOT detected — the per-literal hole is open again", name)
			continue
		}
		for _, h := range got {
			if !strings.Contains(h.Detail, "occurred_at") || !strings.Contains(h.Detail, "badReader") {
				t.Errorf("scan control (fragment shape %q): hit does not name the column and the reader: %s", name, h.Detail)
			}
		}
	}
	// A const declared in ANOTHER file of the same package, spliced by name.
	constFile := []byte("package planted\n\nconst timeClause = ` AND occurred_at <= ?`\n")
	readerFile := []byte("package planted\n\nfunc badReader(db queryer) {\n\t_ = db.Query(`SELECT seq FROM event_log WHERE site_id = ?` + timeClause)\n}\n")
	crossConsts := w1StringConstsFromSources(t, constFile, readerFile)
	if got := w1StoreSQLReadHitsOutsideAllowlist(t, "reader.go", readerFile, []string{"occurred_at"}, map[string]bool{}, crossConsts); len(got) == 0 {
		t.Error("scan control (cross-file package const fragment): not detected")
	} else if !strings.Contains(got[0].Detail, "package const timeClause") {
		t.Errorf("scan control (cross-file package const fragment): the hit does not name the const it came through: %s", got[0].Detail)
	}
	// The allowlist still exempts a fragment-built reader BY NAME, as it does
	// a literal one — the fix widens what is seen, not who is sanctioned.
	if got := w1StoreSQLReadHitsOutsideAllowlist(t, "allowed.go", []byte(fragmentPlants["q += fragment (the store.EachEvent shape)"]), []string{"occurred_at"}, map[string]bool{"badReader": true}, nil); len(got) != 0 {
		t.Errorf("scan control: an allowlisted reader built by concatenation was flagged: %v", got)
	}
	fragmentNegatives := map[string]string{
		"INSERT column list built by concatenation":                    "package planted\n\nfunc okWriter(db queryer) {\n\tq := `INSERT INTO event_log (site_id, node_epoch, seq`\n\tq += `, occurred_at, time_source) VALUES (?, ?, ?, ?, ?)`\n\t_ = db.Exec(q)\n}\n",
		"select-list column via %s ahead of the WHERE":                 "package planted\n\nfunc okReader(db queryer) {\n\tq := fmt.Sprintf(`SELECT %s FROM event_log WHERE seq > ?`, `occurred_at`)\n\t_ = db.Query(q)\n}\n",
		"select-list continuation fragment":                            "package planted\n\nfunc okReader(db queryer) {\n\tq := `SELECT site_id`\n\tq += `, occurred_at`\n\tq += ` FROM event_log WHERE seq > ?`\n\t_ = db.Query(q)\n}\n",
		"an error string naming the column beside a filtered query":    "package planted\n\nfunc okReader(db queryer) error {\n\trows, err := db.Query(`SELECT seq FROM event_log WHERE site_id = ?`)\n\tif err != nil {\n\t\treturn fmt.Errorf(\"store: occurred_at unavailable on this node: %w\", err)\n\t}\n\treturn rows.Close()\n}\n",
		"a bare column-name const used as a map key":                   "package planted\n\nconst factOccurredAt = `occurred_at`\n\nfunc okReader() {\n\t_ = map[string]bool{factOccurredAt: true}\n}\n",
		"UPDATE assignment fragment (a write: SET closes the context)": "package planted\n\nfunc okWriter(db queryer) {\n\tq := `UPDATE scratch`\n\tq += ` SET occurred_at = ?`\n\t_ = db.Exec(q)\n}\n",
		"two unrelated statements in one function":                     "package planted\n\nfunc okBoth(db queryer) {\n\t_ = db.Query(`SELECT seq FROM event_log WHERE site_id = ? ORDER BY seq`)\n\t_ = db.Exec(`INSERT INTO event_log (site_id, occurred_at) VALUES (?, ?)`)\n}\n",
	}
	for name, src := range fragmentNegatives {
		consts := w1StringConstsFromSources(t, []byte(src))
		if got := w1StoreSQLReadHitsOutsideAllowlist(t, "negative.go", []byte(src), []string{"occurred_at"}, map[string]bool{}, consts); len(got) != 0 {
			t.Errorf("scan control (negative %q): carriage was flagged as a read — the guard would then red on the storage layer W1 itself builds: %v", name, got)
		}
	}
	// Twelve keyword-less strings a maintainer would plausibly write, pinned
	// GREEN [MEASURED by review, REVIEW-GUARD3B.md]: a since-removed "bare
	// predicate" layer turned every one of them RED while catching nothing in
	// the tree. A layer that cannot tell SQL from an error message must fail
	// here before it can land. Checked through BOTH consumers.
	keywordlessNegatives := map[string]string{
		"validation text with an operator":                  "package planted\n\nfunc ok() { panic(\"occurred_at > 0 required\") }\n",
		"log text with key=value":                           "package planted\n\nfunc ok() { _ = \"time_source=entered requires a clock reading\" }\n",
		"quoted operand":                                    "package planted\n\nfunc ok() { _ = \"authored_at_state = 'unknown' is not admissible\" }\n",
		"invariant text with !=":                            "package planted\n\nfunc ok() { _ = \"invariant: time_source != '' after fold\" }\n",
		"zero means unknown":                                "package planted\n\nfunc ok() { _ = \"occurred_at=0 means unknown\" }\n",
		"semicolon in prose":                                "package planted\n\nfunc ok() { _ = \"time_source=unknown; clock not trusted\" }\n",
		"identifier operand":                                "package planted\n\nfunc ok() { _ = \"invariant violated: occurred_at <= authored_at_wall_ms\" }\n",
		"a SQL comment inside an appended fragment":         "package planted\n\nfunc ok(db queryer) {\n\tq := `SELECT seq FROM event_log WHERE site_id = ?`\n\tq += ` AND seq <= ? -- never occurred_at <= ? here (D-90)`\n\t_ = db.Query(q)\n}\n",
		"a non-SQL sort spec":                               "package planted\n\nfunc ok() { _ = \"occurred_at desc\" }\n",
		"a URL":                                             "package planted\n\nfunc ok() { _ = \"/events?time_source=entered\" }\n",
		"a CHECK constraint on a watched column":            "package planted\n\nfunc ok(db queryer) {\n\t_ = db.Exec(`ALTER TABLE scratch ADD COLUMN occurred_at INTEGER NOT NULL DEFAULT 0 CHECK (occurred_at >= 0)`)\n}\n",
		"an UPDATE SET list continued in a second fragment": "package planted\n\nfunc ok(db queryer) {\n\tq := `UPDATE scratch SET note = ?`\n\tq += `, occurred_at = ?`\n\t_ = db.Exec(q)\n}\n",
	}
	for name, src := range keywordlessNegatives {
		for _, cols := range [][]string{{"occurred_at"}, {"time_source"}, {"authored_at_state"}} {
			if got := w1StoreSQLReadHitsOutsideAllowlist(t, "keywordless.go", []byte(src), cols, map[string]bool{}, nil); len(got) != 0 {
				t.Errorf("scan control (keyword-less negative %q, cols %v): guard 3b flagged a non-SQL string as a read: %v", name, cols, got)
			}
		}
		if got := w1PairReadHits(t, "keywordless.go", []byte(src), w1ResolverSeam, nil); len(got) != 0 {
			t.Errorf("scan control (keyword-less negative %q): W1-5 channel (d) flagged a non-SQL string as a read: %v", name, got)
		}
	}
}

// w1StoreSQLReadHitsOutsideAllowlist flags SQL that uses any of cols in an
// ordering/filtering context (after WHERE/ORDER BY/GROUP BY/HAVING/CASE/ON)
// inside functions NOT on the allowlist — whether the SQL is one literal or
// is built across several statements (w1SQLReadsInFunc). consts is the
// package's string-const table; nil for a single planted source.
func w1StoreSQLReadHitsOutsideAllowlist(t *testing.T, path string, src []byte, cols []string, allowed map[string]bool, consts map[string][]w1SQLPiece) []w1Hit {
	t.Helper()
	return w1SQLLitHits(t, path, src, cols, allowed, consts)
}

// w1DeclaresColumn reports whether src mentions column name c as a WHOLE
// identifier, not as a substring of a longer one. `corrected_occurred_at`
// contains `occurred_at` and is a different column; a guard that cannot tell
// them apart cannot state which rule it is enforcing.
func w1DeclaresColumn(src, c string) bool {
	isIdent := func(b byte) bool {
		return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
	}
	for i := 0; ; {
		j := strings.Index(src[i:], c)
		if j < 0 {
			return false
		}
		j += i
		beforeOK := j == 0 || !isIdent(src[j-1])
		end := j + len(c)
		afterOK := end == len(src) || !isIdent(src[end])
		if beforeOK && afterOK {
			return true
		}
		i = j + 1
	}
}

// w1DDLLiterals returns the concatenated STRING literals of a Go file that
// actually carry DDL — the surface this guard's own rule names ("no CREATE
// TABLE / CREATE INDEX DDL ... holds a new column name"). Comments, and
// string constants that are not DDL, are outside the rule and must not be
// scanned as if they were: a guard that cannot say which text it governs
// cannot say which rule it enforces.
func w1DDLLiterals(t *testing.T, path string, src []byte) string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("guard 3: parsing %s: %v", path, err)
	}
	var b strings.Builder
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if strings.Contains(strings.ToUpper(lit.Value), "CREATE ") {
			b.WriteString(lit.Value)
			b.WriteByte('\n')
		}
		return true
	})
	return b.String()
}
