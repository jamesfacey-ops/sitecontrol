// w1_scanutil_test.go — shared scan machinery for W1's guard tests
// (WINDOW-BUILD-PLAN-v1 §3 W1, criteria LOCKED 2026-08-27).
//
// TEST-WRITER NOTE (forge flow §5 step 2): this file holds no assertions of
// its own; it is the observation channel for W1-3 and W1-5's scans and for
// W1-10's census-shape test. Every scan built on it carries its own positive
// control (§5 CONTROL row): the channel is proven able to detect a planted
// violation before any clean pass counts.
package store

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// w1ModuleRoot ascends from the test working directory (the package dir) to
// the directory holding go.mod — the scan root for source scans.
func w1ModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the package directory — scan root unresolvable")
		}
		dir = parent
	}
}

// w1Hit is one scan finding.
type w1Hit struct {
	File   string
	Line   int
	Detail string
}

// w1GoFiles lists non-test .go files under each dir (non-recursive per dir
// entry; pass every dir explicitly so the scan surface is a visible list).
func w1GoFiles(t *testing.T, dirs ...string) []string {
	t.Helper()
	var out []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read dir %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// w1ScanGoTokens tokenises Go SOURCE (comments skipped — a comment narrating
// a deletion is not a live reference; identifiers and string/SQL literals
// are) and reports:
//   - any IDENT exactly in forbiddenIdents,
//   - any STRING literal containing any of forbiddenInStrings (substring).
func w1ScanGoTokens(t *testing.T, path string, src []byte, forbiddenIdents map[string]bool, forbiddenInStrings []string) []w1Hit {
	t.Helper()
	fset := token.NewFileSet()
	file := fset.AddFile(path, fset.Base(), len(src))
	var sc scanner.Scanner
	sc.Init(file, src, nil /* ignore errors: scan continues */, 0)
	var hits []w1Hit
	for {
		pos, tok, lit := sc.Scan()
		if tok == token.EOF {
			break
		}
		switch tok {
		case token.IDENT:
			if forbiddenIdents[lit] {
				hits = append(hits, w1Hit{File: path, Line: fset.Position(pos).Line, Detail: "identifier " + lit})
			}
		case token.STRING:
			for _, sub := range forbiddenInStrings {
				if strings.Contains(lit, sub) {
					hits = append(hits, w1Hit{File: path, Line: fset.Position(pos).Line, Detail: "string literal contains " + sub})
				}
			}
		}
	}
	return hits
}

// w1ScanRaw does a plain substring scan of a non-Go artifact (template, shell,
// TOML). No comment stripping: a template or deploy-script reference to a
// deleted name is live either way (§6 step 3's demo-asserts.sh lesson).
func w1ScanRaw(t *testing.T, path string, substrings []string) []w1Hit {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var hits []w1Hit
	for i, line := range strings.Split(string(b), "\n") {
		for _, sub := range substrings {
			if strings.Contains(line, sub) {
				hits = append(hits, w1Hit{File: path, Line: i + 1, Detail: "contains " + sub})
			}
		}
	}
	return hits
}

// --- comparator-context scan (W1-5 / TP §7.1b) --------------------------------

// w1SQLReadContext matches a SQL string in which colToken appears AFTER a
// clause keyword that makes the read an ordering/filtering/branching input
// (WHERE / ORDER BY / GROUP BY / HAVING / CASE / ON). A column merely carried
// in a SELECT list or INSERT column list appears before any such keyword and
// is storage plumbing, not a read that can change what folds or orders.
func w1SQLReadContext(lit string, colTokens []string) []string {
	upper := strings.ToUpper(lit)
	kw := regexp.MustCompile(`\b(WHERE|ORDER\s+BY|GROUP\s+BY|HAVING|CASE|ON)\b`)
	loc := kw.FindStringIndex(upper)
	if loc == nil {
		return nil
	}
	tail := strings.ToLower(lit[loc[0]:])
	var found []string
	for _, c := range colTokens {
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(c) + `\b`).MatchString(tail) {
			found = append(found, c)
		}
	}
	return found
}

// --- concatenation-aware SQL read scan (guard 3b / W1-5 channel d) -----------
//
// [MEASURED 2026-09-09, reports/witness/GUARD3B-FRAGMENT.md] w1SQLReadContext
// judges ONE string literal at a time, and needs a WHERE / ORDER BY / GROUP BY
// / HAVING / CASE / ON keyword INSIDE that literal before it will look for the
// column. So these two spellings of the same SQL were judged differently:
//
//	q := `SELECT seq FROM event_log WHERE site_id = ? AND occurred_at <= ?` // RED
//
//	q := `SELECT seq FROM event_log WHERE site_id = ?`
//	q += ` AND occurred_at <= ?`                                            // was GREEN
//
// The second literal carries the column and no keyword, so per-literal it is
// nothing. That is the detector failing, not the tree: the rule it holds
// (D-90/D-92 — the clock orders nothing) does not care how many Go statements
// the SQL was spread across, and the fragment spelling is ordinary Go —
// store.EachEvent is built exactly that way. Both W1 consumers (guard 3b's
// allowlist scan and W1-5's comparator scan, channel d) now read the SQL a
// function BUILDS, not only the literals it CONTAINS, through
// w1SQLReadsInFunc, in two layers:
//
//	layer 1 — the per-literal w1SQLReadContext judgement, UNCHANGED, so no
//	          literal that was RED before this change can be GREEN after it;
//	layer 2 — every string the function assembles is expanded into a stream
//	          of pieces (each remembering its source line) and each stream is
//	          judged as ONE SQL text by a clause-context machine (w1SQLEnv.
//	          judge). Expansion follows `+`, `+=`, `:=`/`=` into a local
//	          string variable, `append` into a local slice and strings.Join
//	          over it, a strings.Builder's WriteString/WriteByte/WriteRune and
//	          fmt.Fprintf into it, fmt.Sprintf/Sprint (verbs substituted IN
//	          PLACE, so `SELECT %s FROM …` stays carriage), and identifiers
//	          naming a package-level string const/var of the SAME package
//	          (the consts table). Anything else is one opaque token. Streams
//	          are judged wherever they can leave the function — every call
//	          argument, every return value, every `+` expression — and every
//	          tracked variable when it is overwritten or the function ends.
//
// A third layer — a "bare predicate" rule flagging the column beside an
// operator in ANY keyword-less literal — was built, reviewed and REMOVED
// (reports/witness/REVIEW-GUARD3B.md): every committed control passed with
// it stubbed, so it caught nothing in the tree, while twelve plausible
// non-SQL strings ("occurred_at > 0 required", a URL, a sort spec, a CHECK
// constraint, a SQL comment inside a fragment) turned it RED. It had no
// notion of SQL-ness. Those twelve are pinned as negatives in the guard-3b
// control so no such layer comes back unnoticed.
//
// Shapes CLOSED (each is a plant in the two control tests):
//	q += frag · q = q + frag · db.Query(q + frag) · frag := …; base + frag ·
//	fmt.Sprintf("… WHERE %s", frag) and ("… ORDER BY %s", key) ·
//	b.WriteString(frag) · keys = append(keys, key); strings.Join(keys, ", ")
//	after an ORDER BY literal · a same-package const fragment spliced by name,
//	from the same file or another · the exact EachEvent shape.
//
// Shapes still MISSED (stated, not pretended away):
//	- an order KEY without an operator (`, occurred_at`, `occurred_at`) whose
//	  ORDER BY does not precede it in the SAME stream — e.g. keys collected in
//	  a slice that is joined into a string built later from an opaque base;
//	- a fragment handed to a helper that appends it (`addClause(&q, frag)`),
//	  with or without an operator: the helper's body is not modelled, so the
//	  fragment never joins q's stream (mutants E and F, GUARD3B-FRAGMENT.md);
//	- a const from ANOTHER package, a struct field, a map value, a method on
//	  a foreign query-builder type;
//	- a column NAME assembled from parts (`"occurred" + "_at"`) — deliberate
//	  evasion, out of scope for a drift guard;
//	- fmt verbs with explicit indexes (`%[2]s`) are substituted positionally.
//
// False-positive cost, MEASURED against the tip on 2026-09-09: zero hits over
// internal/store + internal/repl + internal/domain. The one known mode is
// layer 1's English-keyword mode widened across a stream: an "on" / "case" /
// "where" / "having" in an EARLIER piece of the same stream opens context
// for a later piece carrying the column — `"fold failed on node " + x + ":
// occurred_at missing"`, `"in case " + x + " occurred_at"`, and two
// fmt.Fprintf labels into one writer (`"on=%v\n"` then `"occurred_at=%d\n"`)
// all red at rank 2 [MEASURED by review]. Bounded: the keyword must precede
// the column inside one variable's, one writer's or one `+` chain's text,
// and layer 1 already reds the single-literal spelling of each.

// w1SQLPiece is one span of assembled SQL text and the line it came from.
type w1SQLPiece struct {
	Text string
	Line int    // 0 for an opaque placeholder
	Via  string // "" for a literal in the function; names the package const it was spliced from
}

// w1SQLRead is one SQL read of a watched column found by w1SQLReadsInFunc.
type w1SQLRead struct {
	Line   int
	Col    string
	Detail string // "" for the historic per-literal shape; otherwise names the construction
	rank   int    // 1 per-literal, 2 built stream — lowest wins on one line
}

func w1SQLOpaque() []w1SQLPiece { return []w1SQLPiece{{Text: " _ "}} }

// w1SQLJoin flattens slice elements with sep between them.
func w1SQLJoin(elems [][]w1SQLPiece, sep []w1SQLPiece) []w1SQLPiece {
	var out []w1SQLPiece
	for i, el := range elems {
		if i > 0 {
			out = append(out, sep...)
		}
		out = append(out, el...)
	}
	return out
}

// w1PackageStringConsts reads the package-level const/var string declarations
// of every file given (the non-test files of ONE package) into the table
// layer 2 splices a named fragment from. Only statically-evaluable
// initialisers count: string literals, `+` chains of them, and names of other
// such declarations (resolved to a fixpoint, so `a = b + "…"` works whatever
// the declaration order).
func w1PackageStringConsts(t *testing.T, files []string) map[string][]w1SQLPiece {
	t.Helper()
	fset := token.NewFileSet()
	var parsed []*ast.File
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		parsed = append(parsed, f)
	}
	return w1StringConstTable(fset, parsed)
}

// w1StringConstTable is w1PackageStringConsts over already-parsed files — the
// seam the positive controls drive with planted sources.
func w1StringConstTable(fset *token.FileSet, files []*ast.File) map[string][]w1SQLPiece {
	inits := map[string]ast.Expr{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != len(vs.Values) {
					continue
				}
				for i, n := range vs.Names {
					inits[n.Name] = vs.Values[i]
				}
			}
		}
	}
	out := map[string][]w1SQLPiece{}
	for pass := 0; pass < 16; pass++ {
		progress := false
		for name, e := range inits {
			if _, done := out[name]; done {
				continue
			}
			env := &w1SQLEnv{fset: fset, consts: out}
			pieces, ok := env.static(e)
			if !ok {
				continue
			}
			pos := fset.Position(e.Pos())
			via := fmt.Sprintf("package const %s (declared %s:%d)", name, filepath.Base(pos.Filename), pos.Line)
			stamped := make([]w1SQLPiece, len(pieces))
			for i, p := range pieces {
				if p.Via == "" {
					p.Via = via
				}
				stamped[i] = p
			}
			out[name] = stamped
			progress = true
		}
		if !progress {
			break
		}
	}
	return out
}

// w1SQLEnv is the per-function expansion state for layer 2.
type w1SQLEnv struct {
	fset   *token.FileSet
	consts map[string][]w1SQLPiece
	vars   map[string][][]w1SQLPiece // a string var holds one element; a slice var one per appended element
	cols   []string
	hits   map[string]w1SQLRead
}

func (env *w1SQLEnv) lit(v *ast.BasicLit) w1SQLPiece {
	s := v.Value
	switch v.Kind {
	case token.STRING:
		if u, err := strconv.Unquote(s); err == nil {
			s = u
		}
	case token.CHAR:
		if len(s) >= 2 {
			if r, _, _, err := strconv.UnquoteChar(s[1:len(s)-1], '\''); err == nil {
				s = string(r)
			}
		}
	}
	return w1SQLPiece{Text: s, Line: env.fset.Position(v.Pos()).Line}
}

// static expands an expression made only of string literals, `+`, parens and
// names of already-tabled package consts; ok is false on anything else.
func (env *w1SQLEnv) static(e ast.Expr) ([]w1SQLPiece, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return nil, false
		}
		return []w1SQLPiece{env.lit(v)}, true
	case *ast.ParenExpr:
		return env.static(v.X)
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return nil, false
		}
		x, ok := env.static(v.X)
		if !ok {
			return nil, false
		}
		y, ok := env.static(v.Y)
		if !ok {
			return nil, false
		}
		return append(append([]w1SQLPiece{}, x...), y...), true
	case *ast.Ident:
		p, ok := env.consts[v.Name]
		return p, ok
	}
	return nil, false
}

// expand renders any expression to the pieces of SQL text it may contribute.
func (env *w1SQLEnv) expand(e ast.Expr) []w1SQLPiece {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING || v.Kind == token.CHAR {
			return []w1SQLPiece{env.lit(v)}
		}
	case *ast.ParenExpr:
		return env.expand(v.X)
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			return append(append([]w1SQLPiece{}, env.expand(v.X)...), env.expand(v.Y)...)
		}
	case *ast.Ident:
		if elems, ok := env.vars[v.Name]; ok {
			return w1SQLJoin(elems, w1SQLOpaque())
		}
		if p, ok := env.consts[v.Name]; ok {
			// Attribute the splice to the reference line in THIS file; Via
			// keeps the declaration site.
			line := env.fset.Position(v.Pos()).Line
			out := make([]w1SQLPiece, len(p))
			for i, q := range p {
				out[i] = w1SQLPiece{Text: q.Text, Line: line, Via: q.Via}
			}
			return out
		}
	case *ast.CallExpr:
		return env.expandCall(v)
	case *ast.CompositeLit:
		var elems [][]w1SQLPiece
		for _, el := range v.Elts {
			elems = append(elems, env.expand(el))
		}
		if len(elems) > 0 {
			return w1SQLJoin(elems, w1SQLOpaque())
		}
	}
	return w1SQLOpaque()
}

func (env *w1SQLEnv) expandCall(c *ast.CallExpr) []w1SQLPiece {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return w1SQLOpaque()
	}
	recv, ok := sel.X.(*ast.Ident)
	if !ok {
		return w1SQLOpaque()
	}
	name := sel.Sel.Name
	switch {
	case recv.Name == "fmt" && name == "Sprintf" && len(c.Args) > 0:
		return env.expandFormat(c.Args[0], c.Args[1:])
	case recv.Name == "fmt" && (name == "Sprint" || name == "Sprintln"):
		var out []w1SQLPiece
		for _, a := range c.Args {
			out = append(out, env.expand(a)...)
		}
		return out
	case recv.Name == "strings" && name == "Join" && len(c.Args) == 2:
		return env.expandJoin(c.Args[0], c.Args[1])
	case recv.Name == "strings" && len(c.Args) >= 1 &&
		(name == "TrimSpace" || name == "ToUpper" || name == "ToLower" || name == "Trim" || name == "TrimSuffix" || name == "TrimPrefix"):
		return env.expand(c.Args[0])
	case name == "String" && len(c.Args) == 0:
		if elems, ok := env.vars[recv.Name]; ok { // b.String() on a tracked builder
			return w1SQLJoin(elems, nil)
		}
	}
	return w1SQLOpaque()
}

func (env *w1SQLEnv) expandJoin(list, sep ast.Expr) []w1SQLPiece {
	sepP := env.expand(sep)
	switch l := list.(type) {
	case *ast.Ident:
		if elems, ok := env.vars[l.Name]; ok {
			return w1SQLJoin(elems, sepP)
		}
	case *ast.CompositeLit:
		var elems [][]w1SQLPiece
		for _, el := range l.Elts {
			elems = append(elems, env.expand(el))
		}
		return w1SQLJoin(elems, sepP)
	}
	return w1SQLOpaque()
}

var w1SQLVerb = regexp.MustCompile(`%(?:\[[0-9]+\])?[-+# 0]*(?:\*|[0-9]+)?(?:\.(?:\*|[0-9]*))?[a-zA-Z%]`)

// expandFormat substitutes each fmt verb with its argument's expansion IN
// PLACE, so `SELECT %s FROM t WHERE x = ?` with a column argument stays a
// select-list carriage and `… WHERE %s` with a predicate argument is a read.
func (env *w1SQLEnv) expandFormat(format ast.Expr, args []ast.Expr) []w1SQLPiece {
	var out []w1SQLPiece
	next := 0
	for _, p := range env.expand(format) {
		if p.Line == 0 {
			out = append(out, p)
			continue
		}
		last := 0
		for _, m := range w1SQLVerb.FindAllStringIndex(p.Text, -1) {
			out = append(out, w1SQLPiece{Text: p.Text[last:m[0]], Line: p.Line, Via: p.Via})
			switch {
			case p.Text[m[0]:m[1]] == "%%":
				out = append(out, w1SQLPiece{Text: "%", Line: p.Line, Via: p.Via})
			case next < len(args):
				out = append(out, env.expand(args[next])...)
				next++
			default:
				out = append(out, w1SQLOpaque()...)
			}
			last = m[1]
		}
		out = append(out, w1SQLPiece{Text: p.Text[last:], Line: p.Line, Via: p.Via})
	}
	return out
}

// set records a fresh value for a local; the value it overwrites is judged
// first so a query built and used earlier in the same variable is not lost.
func (env *w1SQLEnv) set(name string, e ast.Expr) {
	if old, ok := env.vars[name]; ok {
		env.judge(w1SQLJoin(old, w1SQLOpaque()))
	}
	if cl, ok := e.(*ast.CompositeLit); ok { // clauses := []string{…}
		var elems [][]w1SQLPiece
		for _, el := range cl.Elts {
			elems = append(elems, env.expand(el))
		}
		env.vars[name] = elems
		return
	}
	env.vars[name] = [][]w1SQLPiece{env.expand(e)}
}

// extend appends pieces to a local's last element (`+=`, WriteString, Fprintf).
func (env *w1SQLEnv) extend(name string, pieces []w1SQLPiece) {
	elems := env.vars[name]
	if len(elems) == 0 {
		env.vars[name] = [][]w1SQLPiece{pieces}
		return
	}
	last := len(elems) - 1
	elems[last] = append(append([]w1SQLPiece{}, elems[last]...), pieces...)
	env.vars[name] = elems
}

func (env *w1SQLEnv) assign(a *ast.AssignStmt) {
	switch a.Tok {
	case token.ADD_ASSIGN:
		if id, ok := a.Lhs[0].(*ast.Ident); ok && len(a.Rhs) == 1 {
			env.extend(id.Name, env.expand(a.Rhs[0]))
		}
	case token.DEFINE, token.ASSIGN:
		if len(a.Lhs) != len(a.Rhs) {
			return
		}
		for i, l := range a.Lhs {
			id, ok := l.(*ast.Ident)
			if !ok {
				continue
			}
			if call, ok := a.Rhs[i].(*ast.CallExpr); ok {
				if fn, ok := call.Fun.(*ast.Ident); ok && fn.Name == "append" && len(call.Args) >= 1 {
					if base, ok := call.Args[0].(*ast.Ident); ok && base.Name == id.Name { // x = append(x, …)
						rest := call.Args[1:]
						if call.Ellipsis.IsValid() && len(rest) == 1 { // append(x, y...)
							if y, ok := rest[0].(*ast.Ident); ok {
								env.vars[id.Name] = append(env.vars[id.Name], env.vars[y.Name]...)
								continue
							}
						}
						for _, el := range rest {
							env.vars[id.Name] = append(env.vars[id.Name], env.expand(el))
						}
						continue
					}
				}
			}
			env.set(id.Name, a.Rhs[i])
		}
	}
}

// call routes builder writes into their receiver's stream and judges every
// other call's arguments — the sinks (db.Query, tx.Exec, a wrapper) are not
// enumerated, so a new wrapper cannot slip past by being new.
func (env *w1SQLEnv) call(c *ast.CallExpr) {
	if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
		if recv, ok := sel.X.(*ast.Ident); ok {
			switch sel.Sel.Name {
			case "WriteString", "WriteByte", "WriteRune":
				if len(c.Args) == 1 {
					env.extend(recv.Name, env.expand(c.Args[0]))
					return
				}
			case "Fprintf", "Fprint", "Fprintln":
				if recv.Name == "fmt" && len(c.Args) >= 2 {
					dst := c.Args[0]
					if u, ok := dst.(*ast.UnaryExpr); ok && u.Op == token.AND {
						dst = u.X
					}
					if id, ok := dst.(*ast.Ident); ok {
						if sel.Sel.Name == "Fprintf" {
							env.extend(id.Name, env.expandFormat(c.Args[1], c.Args[2:]))
						} else {
							var p []w1SQLPiece
							for _, a := range c.Args[1:] {
								p = append(p, env.expand(a)...)
							}
							env.extend(id.Name, p)
						}
						return
					}
				}
			}
		}
	}
	for _, a := range c.Args {
		env.judge(env.expand(a))
	}
}

func (env *w1SQLEnv) hit(r w1SQLRead) {
	k := strconv.Itoa(r.Line) + "|" + r.Col
	if old, ok := env.hits[k]; ok && old.rank <= r.rank {
		return
	}
	env.hits[k] = r
}

var (
	w1SQLToken        = regexp.MustCompile(`'(?:[^']|'')*'|"[^"]*"|[A-Za-z_][A-Za-z0-9_]*|[();]`)
	w1SQLLineComment  = regexp.MustCompile(`--[^\n]*`)
	w1SQLBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

// judge runs the clause-context machine over one assembled stream. Context
// opens at WHERE / HAVING / ON / ORDER BY / GROUP BY / CASE and closes at a
// statement or clause that cannot be a read (SELECT, INSERT, FROM, SET,
// VALUES, LIMIT, …); parentheses and CASE…END nest; a quoted SQL string is
// data; `;` starts over. A column token met while the context is open is a
// read, attributed to the piece — and so the line — that carried it.
func (env *w1SQLEnv) judge(pieces []w1SQLPiece) {
	type span struct{ start, end, idx int }
	var (
		b     strings.Builder
		spans []span
	)
	for i, p := range pieces {
		if i > 0 {
			b.WriteByte(' ')
		}
		start := b.Len()
		text := w1SQLBlockComment.ReplaceAllString(p.Text, " ")
		text = w1SQLLineComment.ReplaceAllString(text, " ")
		b.WriteString(text)
		spans = append(spans, span{start, b.Len(), i})
	}
	text := b.String()
	pieceAt := func(off int) w1SQLPiece {
		for _, s := range spans {
			if off >= s.start && off < s.end {
				return pieces[s.idx]
			}
		}
		return w1SQLPiece{}
	}
	type frame struct {
		read      bool
		openKW    string
		openPiece w1SQLPiece
	}
	stack := []frame{{}}
	top := func() *frame { return &stack[len(stack)-1] }
	open := func(kw string, p w1SQLPiece) { *top() = frame{read: true, openKW: kw, openPiece: p} }
	prev := ""
	for _, m := range w1SQLToken.FindAllStringIndex(text, -1) {
		tok := text[m[0]:m[1]]
		p := pieceAt(m[0])
		switch {
		case tok == "(":
			stack = append(stack, *top())
		case tok == ")":
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case tok == ";":
			stack = []frame{{}}
		case tok[0] == '\'' || tok[0] == '"':
			// quoted SQL string: data, never a column
		default:
			up := strings.ToUpper(tok)
			switch up {
			case "WHERE", "HAVING", "ON":
				open(up, p)
			case "BY":
				if prev == "ORDER" || prev == "GROUP" {
					open(prev+" BY", p)
				}
			case "CASE":
				stack = append(stack, *top())
				open("CASE", p)
			case "END":
				if len(stack) > 1 {
					stack = stack[:len(stack)-1]
				}
			case "SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "DROP", "ALTER", "PRAGMA", "WITH", "REPLACE",
				"VALUES", "SET", "FROM", "INTO", "RETURNING", "LIMIT", "OFFSET", "UNION", "EXCEPT", "INTERSECT",
				"BEGIN", "COMMIT", "ROLLBACK", "EXPLAIN":
				top().read = false
			default:
				if f := top(); f.read {
					for _, c := range env.cols {
						if strings.EqualFold(tok, c) {
							env.hit(w1SQLRead{Line: p.Line, Col: c, rank: 2, Detail: w1SQLDescribe(p, f.openKW, f.openPiece)})
						}
					}
				}
			}
			prev = up
		}
	}
}

func w1SQLDescribe(carrier w1SQLPiece, kw string, opener w1SQLPiece) string {
	at := func(p w1SQLPiece) string {
		s := "line " + strconv.Itoa(p.Line)
		if p.Via != "" {
			s += " (" + p.Via + ")"
		}
		return s
	}
	return " — SQL built by concatenation: the fragment at " + at(carrier) +
		" carries the column into the " + kw + " context opened at " + at(opener)
}

// w1SQLReadsInFunc reports every SQL read of a watched column inside fn —
// the per-literal judgement plus the built-stream layer — sorted by line,
// one entry per (line, column).
func w1SQLReadsInFunc(fset *token.FileSet, fn *ast.FuncDecl, cols []string, consts map[string][]w1SQLPiece) []w1SQLRead {
	env := &w1SQLEnv{fset: fset, consts: consts, vars: map[string][][]w1SQLPiece{}, cols: cols, hits: map[string]w1SQLRead{}}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.BasicLit:
			if v.Kind != token.STRING {
				return true
			}
			line := fset.Position(v.Pos()).Line
			for _, c := range w1SQLReadContext(v.Value, cols) { // layer 1, unchanged
				env.hit(w1SQLRead{Line: line, Col: c, rank: 1})
			}
		case *ast.AssignStmt:
			env.assign(v)
		case *ast.DeclStmt:
			if gd, ok := v.Decl.(*ast.GenDecl); ok {
				for _, s := range gd.Specs {
					if vs, ok := s.(*ast.ValueSpec); ok && len(vs.Names) == len(vs.Values) {
						for i, name := range vs.Names {
							env.set(name.Name, vs.Values[i])
						}
					}
				}
			}
		case *ast.CallExpr:
			env.call(v)
		case *ast.ReturnStmt:
			for _, r := range v.Results {
				env.judge(env.expand(r))
			}
		case *ast.BinaryExpr:
			if v.Op == token.ADD {
				env.judge(env.expand(v))
			}
		}
		return true
	})
	for _, elems := range env.vars {
		env.judge(w1SQLJoin(elems, w1SQLOpaque()))
	}
	out := make([]w1SQLRead, 0, len(env.hits))
	for _, h := range env.hits {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Col < out[j].Col
	})
	return out
}

// w1PairReadHits parses one Go source and reports every read of the
// OccurredAt/TimeSource pair in a COMPARATOR CONTEXT — the §4.4 forbidden
// boundary rendered as a scan:
//
//	(a) selector .OccurredAt / .TimeSource inside a comparison expression
//	    (== != < > <= >=),
//	(b) the same selector inside an if/switch/for condition or a switch tag,
//	(c) the same selector inside a sort.*/slices.* call's arguments,
//	(d) SQL using occurred_at/time_source after WHERE/ORDER BY/GROUP BY/
//	    HAVING/CASE/ON — whether the SQL is one literal or is BUILT across
//	    several (w1SQLReadsInFunc; consts is the package's string-const table
//	    from w1PackageStringConsts, nil for a single planted source).
//
// exemptFuncs is THE one pre-named exemption (W1-5): hits inside a function
// whose name appears there are sanctioned. Carriage (assignment, Fprintf into
// a hasher, INSERT args, Scan(&e.OccurredAt)) is not flagged: none of those
// contexts can change which events fold, in what order, or what a watermark
// says (§4.4's line).
func w1PairReadHits(t *testing.T, path string, src []byte, exemptFuncs map[string]bool, consts map[string][]w1SQLPiece) []w1Hit {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var hits []w1Hit
	selNames := map[string]bool{"OccurredAt": true, "TimeSource": true}
	sqlCols := []string{"occurred_at", "time_source"}

	containsForbiddenSel := func(n ast.Node) (string, bool) {
		var found string
		ast.Inspect(n, func(m ast.Node) bool {
			if s, ok := m.(*ast.SelectorExpr); ok && selNames[s.Sel.Name] {
				found = s.Sel.Name
				return false
			}
			return true
		})
		return found, found != ""
	}
	isCompare := func(op token.Token) bool {
		switch op {
		case token.EQL, token.NEQ, token.LSS, token.GTR, token.LEQ, token.GEQ:
			return true
		}
		return false
	}

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if exemptFuncs[fn.Name.Name] {
			continue // W1-5's single pre-named exemption: the VF §4.2 resolver seam
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.BinaryExpr:
				if isCompare(v.Op) {
					if name, ok := containsForbiddenSel(v); ok {
						hits = append(hits, w1Hit{File: path, Line: fset.Position(v.Pos()).Line,
							Detail: "comparison on ." + name + " in " + fn.Name.Name})
					}
				}
			case *ast.IfStmt:
				if v.Cond != nil {
					if name, ok := containsForbiddenSel(v.Cond); ok {
						hits = append(hits, w1Hit{File: path, Line: fset.Position(v.Pos()).Line,
							Detail: "if-condition on ." + name + " in " + fn.Name.Name})
					}
				}
			case *ast.SwitchStmt:
				if v.Tag != nil {
					if name, ok := containsForbiddenSel(v.Tag); ok {
						hits = append(hits, w1Hit{File: path, Line: fset.Position(v.Pos()).Line,
							Detail: "switch-tag on ." + name + " in " + fn.Name.Name})
					}
				}
			case *ast.ForStmt:
				if v.Cond != nil {
					if name, ok := containsForbiddenSel(v.Cond); ok {
						hits = append(hits, w1Hit{File: path, Line: fset.Position(v.Pos()).Line,
							Detail: "for-condition on ." + name + " in " + fn.Name.Name})
					}
				}
			case *ast.CallExpr:
				if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
					if pkg, ok := sel.X.(*ast.Ident); ok && (pkg.Name == "sort" || pkg.Name == "slices") {
						for _, arg := range v.Args {
							if name, ok := containsForbiddenSel(arg); ok {
								hits = append(hits, w1Hit{File: path, Line: fset.Position(v.Pos()).Line,
									Detail: pkg.Name + " call reading ." + name + " in " + fn.Name.Name})
							}
						}
					}
				}
			}
			return true
		})
		for _, r := range w1SQLReadsInFunc(fset, fn, sqlCols, consts) { // channel (d)
			hits = append(hits, w1Hit{File: path, Line: r.Line,
				Detail: "SQL read of " + r.Col + " in " + fn.Name.Name + r.Detail})
		}
	}
	return hits
}

// w1SQLLitHits parses Go source and flags every SQL read of any of cols after
// a filtering/ordering keyword — in one literal or built across several
// (w1SQLReadsInFunc) — attributed to its enclosing top-level function; hits
// inside allowed function names are sanctioned. consts is the package's
// string-const table (w1PackageStringConsts): a package-scope fragment is
// live only through the reader that embeds it, and is now judged THERE, as
// part of that reader's SQL, rather than skipped.
func w1SQLLitHits(t *testing.T, path string, src []byte, cols []string, allowed map[string]bool, consts map[string][]w1SQLPiece) []w1Hit {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var hits []w1Hit
	for _, decl := range f.Decls {
		fn, isFn := decl.(*ast.FuncDecl)
		if !isFn || fn.Body == nil {
			continue
		}
		name := fn.Name.Name
		if allowed[name] {
			continue
		}
		for _, r := range w1SQLReadsInFunc(fset, fn, cols, consts) {
			hits = append(hits, w1Hit{File: path, Line: r.Line,
				Detail: "SQL filtering/ordering read of " + r.Col + " in " + name + r.Detail})
		}
	}
	return hits
}

// w1StringConstsFromSources is w1PackageStringConsts over in-memory sources
// — the seam the positive controls drive with planted files.
func w1StringConstsFromSources(t *testing.T, srcs ...[]byte) map[string][]w1SQLPiece {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	for i, src := range srcs {
		f, err := parser.ParseFile(fset, fmt.Sprintf("planted%d.go", i), src, 0)
		if err != nil {
			t.Fatalf("parse planted source %d: %v", i, err)
		}
		files = append(files, f)
	}
	return w1StringConstTable(fset, files)
}

// --- signature census helpers (W1-10) -----------------------------------------

// w1FuncSig describes one parsed top-level function's parameter shape.
type w1FuncSig struct {
	Found      bool
	ParamTypes string // comma-joined type expressions, one entry PER NAME
	Variadic   bool
	// PointerOptionParam is true when any parameter is a pointer to a type
	// whose name matches the options/params/config shim family — m11's
	// "defaulted options pointer" shape.
	PointerOptionParam bool
	PointerParams      int // count of pointer-typed parameters of any kind
}

var w1OptShim = regexp.MustCompile(`(?i)(opt|option|config|param)`)

// w1ParseFuncSigs extracts the signatures of the named top-level funcs (and
// methods) from one file.
func w1ParseFuncSigs(t *testing.T, path string, names map[string]bool) map[string]w1FuncSig {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return w1ParseFuncSigsFromSource(t, path, src, names)
}

// w1ParseFuncSigsFromSource is the same extraction over in-memory source —
// the seam the census-shape positive control drives.
func w1ParseFuncSigsFromSource(t *testing.T, path string, src []byte, names map[string]bool) map[string]w1FuncSig {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	out := map[string]w1FuncSig{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || !names[fn.Name.Name] {
			continue
		}
		sig := w1FuncSig{Found: true}
		var parts []string
		for _, field := range fn.Type.Params.List {
			typeStr := types.ExprString(field.Type)
			n := len(field.Names)
			if n == 0 {
				n = 1
			}
			for i := 0; i < n; i++ {
				parts = append(parts, typeStr)
			}
			if _, ok := field.Type.(*ast.Ellipsis); ok {
				sig.Variadic = true
			}
			if star, ok := field.Type.(*ast.StarExpr); ok {
				sig.PointerParams += n
				elem := types.ExprString(star.X)
				// *store.Store / *string / *int64 are the established, legit
				// pointer params; an added *SomethingOptions/-Params/-Config
				// is the m11 shim.
				base := elem
				if i := strings.LastIndex(elem, "."); i >= 0 {
					base = elem[i+1:]
				}
				if base != "Store" && base != "string" && base != "int64" && w1OptShim.MatchString(base) {
					sig.PointerOptionParam = true
				}
			}
		}
		sig.ParamTypes = strings.Join(parts, ",")
		out[fn.Name.Name] = sig
	}
	return out
}
