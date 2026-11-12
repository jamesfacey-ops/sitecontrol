// b6_vacuous_edges_test.go — tester-go's edge witnesses for gate B-6's
// VACUOUS guard (reports/witness/TESTER-b6.md).
//
// b6_cutover_test.go covers the D-85 shape — every projection present, every
// one an empty list. A pair of dumps can carry zero compared rows in other
// shapes too, and they took the OTHER arm of the guard (presentBoth == 0,
// via the absent-from-both warning path) or a null rather than an empty
// list. RED observed on the 087711c tool (tester-go, 2026-09-06): every
// "nothing" shape below printed "IDENTICAL: 20 compared projections match"
// and exited 0. GREEN at 26ac994. The boundary on the other side is pinned
// as well, so an over-eager rewrite (per-projection, or "fewer than N") can
// not turn a real one-row agreement into VACUOUS: W10-2's clause is "zero
// compared rows is a failure", nothing more.
//
// The parse-error witness is ALREADY-TRUE on the old tool and is red only
// under a mutant (load() made lenient, returning an empty dump for
// unparseable bytes): it then reads VACUOUS instead of a parse error, which
// is a different diagnosis at 4am — "re-take the dumps from /opt/sitectl"
// versus "this file is not a dump".
package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// b6AllEmptyBut is a dump with every registered projection present and empty,
// except proj, which carries rows.
func b6AllEmptyBut(t *testing.T, proj string, rows []map[string]any) string {
	t.Helper()
	names, err := registeredProjections()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	d := map[string][]map[string]any{}
	for _, n := range names {
		d[n] = []map[string]any{}
	}
	d[proj] = rows
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestEveryShapeOfNothingIsVacuousAndNeverIdentical(t *testing.T) {
	names, err := registeredProjections()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	var nullParts []string
	for _, n := range names {
		nullParts = append(nullParts, fmt.Sprintf("%q:null", n))
	}
	shapes := map[string]string{
		"{} — no projection at all":                         "{}",
		"null — a JSON null document":                       "null",
		"rows only OUTSIDE the compare set":                 `{"weather_phase":[{"phase":2}],"alert_ack":[{"trip_id":"t"}]}`,
		"every projection present as null":                  "{" + strings.Join(nullParts, ",") + "}",
		"ignored rows + compare set present-but-empty":      b6AllEmptyBut(t, "weather_phase", []map[string]any{{"phase": 2}}),
		"CRLF-formatted empty against LF-formatted empty":   strings.ReplaceAll(b6AllEmptyBut(t, "trip", nil), ",", ",\r\n"),
	}
	// (C1) force ordering: iterate the shapes by sorted key, never map order.
	keys := make([]string, 0, len(shapes))
	for k := range shapes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		other := shapes[k]
		if strings.HasPrefix(k, "CRLF") {
			other = b6AllEmptyBut(t, "trip", nil)
		}
		code, out, errOut := runTool(t, saved(t, shapes[k]), saved(t, other))
		if code != 2 {
			t.Fatalf("%s: want exit 2 (the assert never ran), got %d\nstdout:\n%s\nstderr:\n%s", k, code, out, errOut)
		}
		if strings.Contains(out, "IDENTICAL") {
			t.Fatalf("%s: printed IDENTICAL over nothing — the D-85 false green in another shape:\n%s", k, out)
		}
		if !strings.Contains(out, "VACUOUS") {
			t.Fatalf("%s: want the VACUOUS verdict word, got:\n%s", k, out)
		}
	}
}

func TestOneComparedRowOnBothSidesIsTheMinimumNonVacuousIdentical(t *testing.T) {
	set, err := buildCompareSet()
	if err != nil {
		t.Fatal(err)
	}
	d := b6AllEmptyBut(t, "trip", []map[string]any{{"trip_id": "x"}})
	code, out, errOut := runTool(t, saved(t, d), saved(t, d))
	if code != 0 || !strings.Contains(out, "IDENTICAL") || strings.Contains(out, "VACUOUS") {
		t.Fatalf("one compared row on both sides is the smallest NON-vacuous agreement and must be IDENTICAL exit 0; got exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	want := fmt.Sprintf("compared: %d projections present in both dumps (rows a=1 b=1); 0 compare-set projections absent from both, not compared", len(set))
	if !strings.Contains(out, want) {
		t.Fatalf("want the reconciliation line\n  %s\nin:\n%s", want, out)
	}
}

func TestAnUnparseableDumpIsAParseErrorNotAVacuousRunAndNeverIdentical(t *testing.T) {
	for _, bad := range []string{"{", "", "[]", `"str"`, `{"trip":{}}`, "\uFEFF{}"} {
		code, out, _ := runTool(t, saved(t, bad), saved(t, bad))
		if code != 2 {
			t.Fatalf("%q on both sides: want exit 2, got %d\n%s", bad, code, out)
		}
		if strings.Contains(out, "IDENTICAL") {
			t.Fatalf("%q on both sides printed IDENTICAL:\n%s", bad, out)
		}
		if strings.Contains(out, "VACUOUS") {
			t.Fatalf("%q on both sides read as VACUOUS — that is the wrong diagnosis; a file that is not a dump must fail at load(), naming the parse error:\n%s", bad, out)
		}
	}
}
