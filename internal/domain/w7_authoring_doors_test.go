// w7_authoring_doors_test.go — W7-4(d)(i), the AUTHORING-INPUT census, and
// W7-4(b)'s preserved refusal literal.
//
// design/WINDOW-BUILD-PLAN-v1.md `### W7 —` at :7421, criteria LOCKED
// 2026-09-03. Written from the CRITERION TEXT ONLY.
//
// ⚠⚠ THE INSTRUMENT IS AN EXACT FIELD-SET / PARAMETER-LIST LITERAL, NOT A
// DATAFLOW QUERY, and ⟦LOCK — FAL-F6 / FIL-F4 / verdict §4.2⟧ is why. The
// superseded wording asked for "no parameter and no input-struct field of
// string type WHOSE VALUE REACHES `trip.vehicle_entered`" — a DATAFLOW query,
// which `go/types` cannot answer. [MEASURED — `app/go.mod`] the module
// requires only `BurntSushi/toml`, `golang.org/x/sys`, `golang.org/x/text`
// and `modernc.org/sqlite`, so `golang.org/x/tools` is NOT a dependency and
// `go/ssa` / `go/callgraph` are unavailable; no scan in this tree performs
// reachability. A test-writer handed that census builds the nearest thing
// they can — a name or selector match — which returns ZERO HITS on a build
// that routes the value through a local alias. That is a no-op remedy dressed
// as a widening, and this file does not build it.
//
// ⚠⚠ THE MECHANISM IS STATED PER SUBJECT, BECAUSE THEY ARE NOT THE SAME
// MECHANISM (⟦LOCK — F6b⟧):
//
//  1. `DispatchInput`  — a REFLECTION census over the struct's exported fields.
//  2. `RecordUnsolicitedArrival` — NOT a struct census: it has NO input struct
//     [MEASURED 2026-09-03, `tripauthor.go:557` — a positional parameter list,
//     and `grep 'UnsolicitedArrivalInput'` over `internal/` returns ZERO].
//     A `go/types` census over the FUNCTION SIGNATURE's parameter list,
//     pinned as an exact ordered literal list of (name, type) pairs.
//     ⚠ Reflection cannot do this — a function's parameter names are not
//     reflectable — which is the second reason a single-mechanism wording
//     could not have been built.
//  3. `GateCheckInInput` — ⚠⚠ A FOURTH AUTHORING DOOR, and the strings
//     `GateCheckIn`, `GateCheckInInput` and `checkin.go` appeared NOWHERE in
//     revision 1's 2,185 lines. A build that converts `DispatchInput` and
//     `RecordUnsolicitedArrival` to ids and leaves this one alone passes a
//     three-door fixture set IN FULL, and `DR-W7-4`'s load-bearing guarantee
//     — "the typing moment IS creation" — is FALSE AT THE GATE, on
//     `POST /gate/checkin`, the busiest operator path in the application.
//
// ⚠ `w5d_payload_test.go:84` is cited for the PINNED-EXACT-LITERAL-LIST
// DISCIPLINE and for nothing else — never for the mechanism. [MEASURED —
// `internal/domain/w5d_payload_test.go:78-85`] the cited form is a JSON
// PAYLOAD-KEY census (`sort.Strings(got)` over unmarshalled map keys), not a
// struct-field census; the two differ wherever a tag renames a field or
// `omitempty` hides one.
//
// ⚠ THE PARAMETER LIST IS QUOTED, NOT COUNTED, DELIBERATELY: the
// fix-verification that raised this finding described it as "eight positional
// parameters" and then listed NINE — the hand-kept-enumeration class arriving
// inside the correction to a citation. This census pins the LIST; it asserts
// NO ARITY.
//
// ⚠ THIS FILE NAMES NO UNBUILT GO SYMBOL. `DispatchInput` and
// `GateCheckInInput` exist today (that is the point — they carry the
// free-text field W7 removes), and `RecordUnsolicitedArrival` is read from
// SOURCE. So it compiles and runs at e9cc5f5.
//
// EXPECTED RED at e9cc5f5 (all runtime):
//   - TestW7DispatchInputTakesAVehicleIDNotAString: `VehicleEntered string`
//     is present and `VehicleID` is absent.
//   - TestW7RecordUnsolicitedArrivalSignatureTakesAVehicleID: the parameter
//     list still reads `vehicleEntered string`.
//   - TestW7GateCheckInInputTakesAVehicleIDNotAString: ⭐ the FOURTH DOOR —
//     `GateCheckInInput.VehicleEntered string` exists.
//
// ⟦NO PRE-CHUNK RED⟧:
//   - TestW7DispatchNamesAnEntityRefusalLiteralIsPreserved (W7-4(b), B-14's
//     ruling (ii)) — it PASSES today and must: the literal is preserved
//     BYTE-FOR-BYTE. Its plant is any rewording of that sentence.
//
// KILLER MUTANTS (§5):
//
//	(m7-4d) a free-text vehicle string RESTORED as an authoring input — the
//	        shape under which "the refusal moved" silently becomes "the
//	        refusal was removed"                     -> W7-4(d)(i)
//	(m7-4h) the vehicle-required refusal DELETED rather than RE-POINTED
//	                                                 -> W7-4(b)
package domain

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// w7FreeTextVehicleFields are the spellings that mean "a caller-supplied
// vehicle STRING". ⚠ Stated as a set and derived over, never as a single
// name: a build that renames the field to `Vehicle` or `VehicleNumber` and
// keeps it a `string` has done nothing DR-W7-4 asks for, and a census
// looking only for `VehicleEntered` would report it clean.
var w7FreeTextVehicleFields = []string{
	"VehicleEntered", "Vehicle", "VehicleNumber", "VehicleText", "VehicleString",
}

// w7VehicleIDField is the spelling DR-W7-4 requires instead. §7 row 2 freezes
// `vehicle_id` as the wire key; this is its Go carrier.
const w7VehicleIDField = "VehicleID"

// w7ExportedFieldNames is the reflection census. Shared by the assertions and
// by their control.
func w7ExportedFieldNames(v any) []string {
	rt := reflect.TypeOf(v)
	var out []string
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.PkgPath != "" {
			continue // unexported
		}
		out = append(out, f.Name)
	}
	sort.Strings(out)
	return out
}

// w7StringTypedField reports whether the named field exists AND is a plain
// `string`. ⚠ Both halves matter: DR-W7-4's subject is a caller-supplied
// STRING, and a `*string` or a named id type is a different thing.
func w7StringTypedField(v any, name string) (exists bool, isString bool) {
	rt := reflect.TypeOf(v)
	f, ok := rt.FieldByName(name)
	if !ok {
		return false, false
	}
	return true, f.Type.Kind() == reflect.String
}

// =========================================================================
// W7-4(d)(i) subject 1 — DispatchInput
// =========================================================================
func TestW7DispatchInputTakesAVehicleIDNotAString(t *testing.T) {
	// --- CONTROL FIRST: the reflection census must SEE a free-text string
	// field and must tell it from a non-string one. A census that reports
	// every struct as clean passes the assertion by construction.
	type ctlDirty struct {
		VehicleEntered string
		Other          int
	}
	type ctlClean struct {
		VehicleID string
		Other     int
	}
	if ok, isStr := w7StringTypedField(ctlDirty{}, "VehicleEntered"); !ok || !isStr {
		t.Fatalf("W7-4(d)(i) CONTROL FAILED: the field census did not see a planted free-text "+
			"`VehicleEntered string` (exists=%v string=%v) — (m7-4d)", ok, isStr)
	}
	if ok, _ := w7StringTypedField(ctlClean{}, "VehicleEntered"); ok {
		t.Fatal("W7-4(d)(i) CONTROL FAILED (negative arm): the census reported `VehicleEntered` present on a " +
			"struct that does not carry it — a census that flags the correct build is one a builder weakens")
	}
	if got := w7ExportedFieldNames(ctlClean{}); strings.Join(got, ",") != "Other,VehicleID" {
		t.Fatalf("W7-4(d)(i) CONTROL FAILED: the exact-field-set census returned %v", got)
	}

	// --- THE ASSERTION ---------------------------------------------------
	fields := w7ExportedFieldNames(DispatchInput{})

	for _, name := range w7FreeTextVehicleFields {
		ok, isStr := w7StringTypedField(DispatchInput{}, name)
		if ok && isStr {
			t.Errorf("W7-4(d)(i) (m7-4d): `DispatchInput.%s` is a caller-supplied `string`. After W7 NO "+
				"trip-authoring entry point takes a caller-supplied vehicle STRING — `DispatchInput` takes a "+
				"`vehicle_id` (DR-W7-4). The field set is %v.\n"+
				"⚠ [[D-11]] and the plan agree ONLY under this reading: the guard is not REMOVED from dispatch, "+
				"it is removed from NOWHERE — the typing moment IS creation, so the one place a placeholder can "+
				"be typed is the one place it is refused. If any authoring path still accepts a free-text "+
				"vehicle string, the move IS a removal and D-11 forbids it.", name, fields)
		}
	}
	if ok, _ := w7StringTypedField(DispatchInput{}, w7VehicleIDField); !ok {
		t.Errorf("W7-4(d)(i): `DispatchInput` has no `%s` field — the field set is %v. `PLAN-SEC-A-schema.md:723`: "+
			"\"every dispatch names an entity\", and W7-4(b) re-points the names-an-entity refusal AT THE ID.",
			w7VehicleIDField, fields)
	}
}

// =========================================================================
// W7-4(d)(i) subject 3 — GateCheckInInput, THE FOURTH DOOR
// =========================================================================
//
// ⭐ This is the door that makes the census non-vacuous. `GateCheckInInput` is
// constructed by two handlers on `POST /gate/checkin` and passed into
// `DispatchLocalCheckIn`; a build that converts the other three and leaves
// this one passes every three-door fixture set in full.
func TestW7GateCheckInInputTakesAVehicleIDNotAString(t *testing.T) {
	fields := w7ExportedFieldNames(GateCheckInInput{})
	for _, name := range w7FreeTextVehicleFields {
		ok, isStr := w7StringTypedField(GateCheckInInput{}, name)
		if ok && isStr {
			t.Errorf("W7-4(d)(i) (m7-4d), THE FOURTH DOOR: `GateCheckInInput.%s` is a free-text `string`, on an "+
				"EXPORTED function that passes it into `DispatchLocalCheckIn`, constructed by two handlers on "+
				"`POST /gate/checkin` — the busiest operator path in the application. The field set is %v.\n"+
				"⚠ W7-4(b)'s THREE doors are FOUR: `Dispatch`, `DispatchLocalCheckIn`, "+
				"`RecordUnsolicitedArrival` AND `GateCheckIn`.", name, fields)
		}
	}
	if ok, _ := w7StringTypedField(GateCheckInInput{}, w7VehicleIDField); !ok {
		t.Errorf("W7-4(d)(i): `GateCheckInInput` has no `%s` field — the field set is %v", w7VehicleIDField, fields)
	}
}

// =========================================================================
// W7-4(d)(i) subject 2 — RecordUnsolicitedArrival's SIGNATURE
// =========================================================================
//
// ⚠ There is nothing here for a reflection census to reflect over: the
// function takes a POSITIONAL PARAMETER LIST and there is no input struct.
// The parameter list is QUOTED as an exact ordered literal, and NO ARITY is
// asserted.
func TestW7RecordUnsolicitedArrivalSignatureTakesAVehicleID(t *testing.T) {
	// --- CONTROL FIRST: the signature census must read (name, type) pairs
	// off a real declaration, must handle a GROUPED parameter list
	// (`a, b string` is two parameters sharing one type expression), and must
	// return nothing for a function that is not there.
	dir := t.TempDir()
	ctlPath := filepath.Join(dir, "ctl.go")
	if err := os.WriteFile(ctlPath, []byte(
		"package p\n\nfunc Target(st *S, siteID string, nodeEpoch int64, vehicleEntered, originSite string) error "+
			"{ return nil }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := w7FuncParams(t, ctlPath, "Target")
	if !ok {
		t.Fatal("W7-4(d)(i) CONTROL FAILED: the signature census did not find a function that is declared")
	}
	want := "st *S, siteID string, nodeEpoch int64, vehicleEntered string, originSite string"
	if strings.Join(got, ", ") != want {
		t.Fatalf("W7-4(d)(i) CONTROL FAILED: the signature census read %q, want %q — ⚠ a GROUPED parameter list "+
			"(`vehicleEntered, originSite string`) is TWO parameters, and a census that reports it as one "+
			"cannot see which of them is the vehicle", strings.Join(got, ", "), want)
	}
	if _, ok := w7FuncParams(t, ctlPath, "NotDeclared"); ok {
		t.Fatal("W7-4(d)(i) CONTROL FAILED (negative arm): the census reported a function that is not declared")
	}

	// --- THE ASSERTION ---------------------------------------------------
	root := w7DomainDir(t)
	params, found := w7FuncParamsInDir(t, root, "RecordUnsolicitedArrival")
	if !found {
		t.Fatalf("W7-4(d)(i) FIXTURE DEFECTIVE: `RecordUnsolicitedArrival` was not found in %s — the census is "+
			"asserting over nothing", root)
	}

	joined := strings.Join(params, ", ")
	for _, bad := range []string{"vehicleEntered string", "vehicle string", "vehicleNumber string"} {
		if strings.Contains(joined, bad) {
			t.Errorf("W7-4(d)(i) (m7-4d): `RecordUnsolicitedArrival`'s parameter list still carries `%s`.\n"+
				"  measured list: (%s)\n"+
				"⚠ Its `vehicleEntered string` parameter is the subject, and the assertion is that it becomes a "+
				"`vehicle_id`, not a free-text string.\n"+
				"⚠ AND THE ARRIVAL SITE CARRIES ITS OWN WRITTEN, LOAD-BEARING RATIONALE THAT W7 CONTRADICTS "+
				"(tripauthor.go:565-572: \"here the reason is sharper than at dispatch: this value IS the "+
				"correlation key … Refusing at both ends also keeps the rule symmetric\"). Under DR-W7-4 that "+
				"rationale is not overridden by fiat — it is DISSOLVED: the value is no longer typed at this "+
				"door, so there is nothing here to refuse. THE COMMENT MUST BE REWRITTEN IN THE SAME CHANGE; a "+
				"comment justifying a removed guard is the artefact a future reader will restore the guard from.",
				bad, joined)
		}
	}
	if !strings.Contains(joined, "vehicleID string") && !strings.Contains(joined, "vehicleID ") {
		t.Errorf("W7-4(d)(i): `RecordUnsolicitedArrival`'s parameter list carries no `vehicleID`.\n"+
			"  measured list: (%s)", joined)
	}
}

// =========================================================================
// W7-4(b) — the names-an-entity refusal is RE-POINTED, NOT DELETED
// =========================================================================
//
// ⟦NO PRE-CHUNK RED⟧ — it passes today and MUST. ⟦LOCK — B-14, plan-owner
// ruling 2026-09-02⟧ rules that arm 1's literal is PRESERVED BYTE-FOR-BYTE,
// "because the sentence is still true of the new guard and because preserving
// it keeps the locked census repair a ONE-ENTRY DELETION instead of a
// re-authoring".
//
// ⚠ THE PLANT THAT WOULD FALSIFY IT: reword the sentence — which is exactly
// what "improving arm 1's operator-facing wording" would do, and which
// ⟦LOCK — B-14⟧ says "is a separate, non-blocking change and must NOT be done
// inside W7".
//
// ⚠ THIS TEST'S SUBJECT IS THE LITERAL, NOT THE GUARD. The guard's own
// behaviour (a dispatch naming no vehicle is REFUSED, on the `vehicle_id`)
// cannot be driven from here without naming the unbuilt input field; that
// half is recorded as a shortfall at reports/window-build/W7-tests.md §5.
func TestW7DispatchNamesAnEntityRefusalLiteralIsPreserved(t *testing.T) {
	const preserved = "domain: dispatch: vehicle id is required"

	// --- CONTROL FIRST: the literal census must see the string when it is
	// there and must NOT see a reworded one.
	dir := t.TempDir()
	present := filepath.Join(dir, "present.go")
	if err := os.WriteFile(present, []byte(
		"package p\n\nvar e = \"domain: dispatch: vehicle id is required\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reworded := filepath.Join(dir, "reworded.go")
	if err := os.WriteFile(reworded, []byte(
		"package p\n\nvar e = \"domain: dispatch: pick a vehicle before dispatching\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !w7FileHasStringLiteral(t, present, preserved) {
		t.Fatalf("W7-4(b) CONTROL FAILED: the literal census did not see %q where it is declared", preserved)
	}
	if w7FileHasStringLiteral(t, reworded, preserved) {
		t.Fatalf("W7-4(b) CONTROL FAILED (negative arm): the census reported %q present in a file that only "+
			"carries a REWORDED sentence — that is (m7-4h)'s own shape and the census must separate them",
			preserved)
	}

	// --- THE ASSERTION ---------------------------------------------------
	var found bool
	for _, path := range w7DomainGoFiles(t) {
		if w7FileHasStringLiteral(t, path, preserved) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("W7-4(b) (m7-4h): the refusal literal %q is GONE from `internal/domain`.\n"+
			"⚠⚠ ⟦LOCK — B-14⟧ rules arm 1 RE-POINTED AT THE `vehicle_id`, NOT DELETED, WITH ITS LITERAL "+
			"PRESERVED BYTE-FOR-BYTE. What moves to vehicle creation is the PLACEHOLDER and EMPTY-NUMBER "+
			"refusals; the NAMES-AN-ENTITY refusal STAYS at the authoring doors. A build that reads \"remove the "+
			"refusals\" literally removes both — and then SQLite accepts `\"\"` for a `NOT NULL TEXT` column, so a "+
			"trip is written with NO VEHICLE and `muster.html:409` renders an empty `<code>` ON THE SECTION "+
			"[[D-36]] KEPT THE VEHICLE FOR.\n"+
			"⚠ Preserving the literal is also what keeps `W5e-3`'s locked census repair a ONE-ENTRY DELETION "+
			"instead of a re-authoring (SEAM-W7-18).", preserved)
	}
}

// --- source machinery (NO assertions) ------------------------------------

func w7DomainDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func w7DomainGoFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(w7DomainDir(t))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		out = append(out, filepath.Join(w7DomainDir(t), e.Name()))
	}
	if len(out) == 0 {
		t.Fatal("fixture defect: no production .go files found in internal/domain")
	}
	sort.Strings(out)
	return out
}

// w7FuncParams reads one file and returns `name type` for each parameter of
// `fn`, EXPANDING grouped parameters. ⚠ An unparseable file is a NAMED FAULT.
func w7FuncParams(t *testing.T, path, fn string) ([]string, bool) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Errorf("W7-4(d)(i) FAULT — %s was NOT scanned, so this census's coverage is smaller than its stated "+
			"scope: %v", path, err)
		return nil, false
	}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Name == nil || fd.Name.Name != fn || fd.Recv != nil {
			continue
		}
		var out []string
		for _, field := range fd.Type.Params.List {
			typ := w7ExprString(field.Type)
			if len(field.Names) == 0 {
				out = append(out, typ)
				continue
			}
			for _, n := range field.Names {
				out = append(out, n.Name+" "+typ)
			}
		}
		return out, true
	}
	return nil, false
}

func w7FuncParamsInDir(t *testing.T, dir, fn string) ([]string, bool) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, n := range names {
		if params, ok := w7FuncParams(t, filepath.Join(dir, n), fn); ok {
			return params, true
		}
	}
	return nil, false
}

// w7ExprString renders a type expression the way the source spells it.
func w7ExprString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		return "*" + w7ExprString(v.X)
	case *ast.SelectorExpr:
		return w7ExprString(v.X) + "." + v.Sel.Name
	case *ast.ArrayType:
		return "[]" + w7ExprString(v.Elt)
	case *ast.Ellipsis:
		return "..." + w7ExprString(v.Elt)
	case *ast.MapType:
		return "map[" + w7ExprString(v.Key) + "]" + w7ExprString(v.Value)
	default:
		return "?"
	}
}

// w7FileHasStringLiteral reports whether `path` declares `want` as a STRING
// LITERAL. ⚠ Literals only, never whole-file text: a comment quoting the
// sentence would otherwise satisfy the assertion, and a comment is not a
// refusal.
func w7FileHasStringLiteral(t *testing.T, path, want string) bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Errorf("W7-4(b) FAULT — %s was NOT scanned, so this census's coverage is smaller than its stated "+
			"scope: %v", path, err)
		return false
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if strings.Contains(lit.Value, want) {
			found = true
		}
		return true
	})
	return found
}
