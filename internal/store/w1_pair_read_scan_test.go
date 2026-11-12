// w1_pair_read_scan_test.go — W1-5: "The source scan for reads of either
// field (TP §7.1b), with its positive control — over internal/store +
// internal/repl + internal/domain, carrying exactly ONE pre-named exemption
// and never a growing allowlist: the §4.4 resolver seam — VF §4.2's
// Outcome/ResolveOccurredAt (W4's deliverable; named now so W4's landing
// turns the scan neither red nor into an allowlist) and, when §5-direction-1
// builds, the visit_span read path. Any other read of either field in any of
// the three packages is red." (WINDOW-BUILD-PLAN-v1 §3 W1, LOCKED 2026-08-27.)
//
// KILLER MUTANT: m5 — plant an OccurredAt read in the fold. The positive
// control below IS the produced failure mode (§5 CONTROL row pairing:
// W1-m5); the D-28 Part B boundary ("the clock orders nothing") is what the
// scan holds.
//
// What counts as a READ here — §4.4's line, made mechanical: a read is
// forbidden if its result can change which events are folded, in what order,
// or what a watermark says. So the scan flags comparator contexts
// (comparisons, if/switch/for conditions, sort calls, SQL
// WHERE/ORDER BY/GROUP BY/HAVING/CASE/ON) and deliberately does NOT flag
// carriage (assignment, hashing, INSERT args, Scan targets, wire
// marshalling) — the storage layer must carry the values to exist at all,
// and none of those contexts can order anything.
package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// w1ResolverSeam is THE one pre-named exemption (W1-5). It is a fixed set,
// not a growing allowlist: VF §4.2's resolver family, W4's deliverable.
// (The visit_span read path joins ONLY if §5 direction 1 ever builds — a
// future deliberate diff of this constant, per the criterion's own naming.)
//
// ⟦FIX16 integration, lead ruling 2026-10-06⟧ DepartureLead joins it as a
// deliberate diff, for James 2026-10-06: overdue runs from the typed time.
// It compares ONE dispatch event's own typed time (OccurredAt, entered) to
// that SAME event's own recorded time (authored_at) to work out how far
// before its recording the run left — the basis of a deadline. It never
// orders two events, never decides what is folded, and moves no watermark.
// The exemption is that one function only (domain/departure.go); its
// callers get no exemption, and TestW1DepartureLeadNeverOrdersEvents holds
// it to one envelope and keeps it, and the two functions built on it, out of
// every sort and every comparison in production code.
var w1ResolverSeam = map[string]bool{
	"Outcome":           true,
	"ResolveOccurredAt": true,
	"DepartureLead":     true,
}

func TestW1NoComparatorReadsOccurredAtOrTimeSourceInStoreReplOrDomain(t *testing.T) {
	root := w1ModuleRoot(t)
	dirs := []string{
		filepath.Join(root, "internal", "store"),
		filepath.Join(root, "internal", "repl"),
		filepath.Join(root, "internal", "domain"), // rev 3: omitting domain was false economy — every live fold comparator lives there
	}
	var hits []w1Hit
	for _, dir := range dirs {
		files := w1GoFiles(t, dir)
		// Each package's string consts travel with its scan, so a fragment
		// declared at package scope is judged inside the reader that splices it.
		consts := w1PackageStringConsts(t, files)
		for _, f := range files {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			hits = append(hits, w1PairReadHits(t, f, src, w1ResolverSeam, consts)...)
		}
	}
	if len(hits) != 0 {
		for _, h := range hits {
			t.Errorf("W1-5 forbidden read: %s:%d — %s", h.File, h.Line, h.Detail)
		}
		t.Fatal("OccurredAt/TimeSource read in a comparator context outside the pre-named resolver seam — D-28 Part B: the clock orders NOTHING (mutant m5)")
	}
}

// Positive control (standing rule): the scan must first prove it can detect
// a planted violation of each channel it claims to watch. This is the
// ledger's CONTROL row for W1-5, paired with mutant m5.
func TestW1PairReadScanPositiveControlDetectsAPlantedFoldRead(t *testing.T) {
	// (a) a planted Go comparator in a fold — the literal m5 shape.
	foldPlant := []byte(`package planted

func plantedFoldLess(a, b Envelope) bool {
	if a.TimeSource == "entered" {
		return true
	}
	return a.OccurredAt < b.OccurredAt
}
`)
	hits := w1PairReadHits(t, "fold_plant.go", foldPlant, w1ResolverSeam, nil)
	if len(hits) < 2 {
		t.Fatalf("control: planted fold comparator produced %d hits, want >= 2 (the if-condition on .TimeSource AND the comparison on .OccurredAt)", len(hits))
	}

	// (b) a planted sort call reading the field.
	sortPlant := []byte(`package planted

import "sort"

func plantedSort(evs []Envelope) {
	sort.Slice(evs, func(i, j int) bool { return evs[i].OccurredAt < evs[j].OccurredAt })
}
`)
	if got := w1PairReadHits(t, "sort_plant.go", sortPlant, w1ResolverSeam, nil); len(got) == 0 {
		t.Fatal("control: planted sort.Slice comparator on .OccurredAt not detected")
	}

	// (c) a planted SQL ordering read.
	sqlPlant := []byte("package planted\n\nfunc plantedQuery(q queryer) {\n\t_ = q.Query(`SELECT seq FROM event_log WHERE time_source = 'entered' ORDER BY occurred_at`)\n}\n")
	if got := w1PairReadHits(t, "sql_plant.go", sqlPlant, w1ResolverSeam, nil); len(got) == 0 {
		t.Fatal("control: planted SQL WHERE/ORDER BY read of the pair not detected")
	}

	// (d) the exemption is honoured — the SAME read inside the pre-named
	// resolver seam is sanctioned, so W4's landing turns this scan neither
	// red nor into an allowlist.
	seamPlant := []byte(`package planted

func ResolveOccurredAt(a, b Envelope) bool { return a.OccurredAt < b.OccurredAt }
`)
	if got := w1PairReadHits(t, "seam_plant.go", seamPlant, w1ResolverSeam, nil); len(got) != 0 {
		t.Fatalf("control: the pre-named resolver seam was flagged (%v) — the exemption must exempt exactly that seam", got)
	}

	// (e) the carriage boundary is honoured — assignment/Scan/INSERT carriage
	// must NOT be flagged, or the scan reds on the storage layer W1 itself
	// builds and gets weakened ad hoc mid-window.
	carriagePlant := []byte(`package planted

func plantedCarriage(env *Envelope, r ClockReading) {
	env.OccurredAt = r.WallMS
	env.TimeSource = "default"
	_ = []any{env.OccurredAt, env.TimeSource}
}
`)
	if got := w1PairReadHits(t, "carriage_plant.go", carriagePlant, w1ResolverSeam, nil); len(got) != 0 {
		t.Fatalf("control: carriage (assignment / value passing) was flagged (%v) — the scan boundary is §4.4's line, not every mention", got)
	}

	// (f) channel (d) sees SQL BUILT across statements, not only one literal
	// [MEASURED 2026-09-09, GUARD3B-FRAGMENT.md]: `q += \` AND occurred_at
	// <= ?\`` planted inside the real store.EachEvent left this scan GREEN
	// while the in-literal spelling turned it RED. EachEvent is not in the
	// resolver seam, so the fragment shape must RED here.
	fragmentPlant := []byte("package planted\n\nfunc EachEvent(db queryer, toSeq int64) {\n\tq := `SELECT seq FROM event_log WHERE site_id = ? AND seq >= ?`\n\tif toSeq > 0 {\n\t\tq += ` AND seq <= ?`\n\t}\n\tq += ` AND occurred_at <= ?`\n\tq += ` ORDER BY seq`\n\t_ = db.Query(q)\n}\n")
	got := w1PairReadHits(t, "fragment_plant.go", fragmentPlant, w1ResolverSeam, nil)
	if len(got) == 0 {
		t.Fatal("control: planted `q += \" AND occurred_at <= ?\"` (the EachEvent shape) not detected — channel (d) is per-literal again")
	}
	if !strings.Contains(got[0].Detail, "SQL read of occurred_at in EachEvent") || !strings.Contains(got[0].Detail, "concatenation") {
		t.Fatalf("control: the fragment hit must name the column, the reader and the construction; got %q", got[0].Detail)
	}
	// (g) …and the SAME fragment inside the pre-named seam is still exempt.
	seamFragment := []byte("package planted\n\nfunc ResolveOccurredAt(db queryer) {\n\tq := `SELECT seq FROM event_log WHERE site_id = ?`\n\tq += ` AND occurred_at <= ?`\n\t_ = db.Query(q)\n}\n")
	if got := w1PairReadHits(t, "seam_fragment.go", seamFragment, w1ResolverSeam, nil); len(got) != 0 {
		t.Fatalf("control: the resolver seam was flagged for a fragment-built read (%v) — the exemption must exempt exactly that seam", got)
	}
	// (h) carriage built by concatenation is still carriage: an INSERT column
	// list continued in a second literal is not a read.
	insertFragment := []byte("package planted\n\nfunc plantedInsert(db queryer) {\n\tq := `INSERT INTO event_log (site_id, node_epoch, seq`\n\tq += `, occurred_at, time_source) VALUES (?, ?, ?, ?, ?)`\n\t_ = db.Exec(q)\n}\n")
	if got := w1PairReadHits(t, "insert_fragment.go", insertFragment, w1ResolverSeam, nil); len(got) != 0 {
		t.Fatalf("control: an INSERT column list built by concatenation was flagged (%v) — the boundary is §4.4's line, not every mention", got)
	}
}
