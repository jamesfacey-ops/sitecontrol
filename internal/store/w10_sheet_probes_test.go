// w10_sheet_probes_test.go — W10-3(c) as amended 2026-09-06 (the W10-SHEET-PROBES
// amendment, reports/witness/W10-SHEET-PROBES.md): the two WINDOW-CONTENT
// probes — W10-3(e), a contested amendment converging to the same winner at
// both posts (the only probe of D-92's `seen`), and W10-3(f), an orientation
// round-trip with the per-site key honoured — are on the SHEET, in §3, before
// the rehearsal.
//
// Criteria source: design/WINDOW-BUILD-PLAN-v1.md §3, `### W10 — Window
// integration and rehearsal`, W10-3 as amended by the plan owner 2026-09-06.
// Written from the amended criterion text only.
//
// WHY THIS CHECK EXISTS — the finding it closes (W10-ABORT-PATH.md §5,
// MEASURED by the w10-abort2 pass): two binaries both stamping SchemaVersion
// 11 with different wire shapes pass the version check; a key-level
// difference (a renamed or omitted `seen`) is SILENT — the comparator falls to
// the tiebreak, muster headings diverge between posts, and nothing on the
// night says so. W10-11(b) row 1 already mechanises "the probes are on the
// sheet" for (a) and (b); this file is the same mechanism for (e) and (f).
// Without it the gap could reopen on the next sheet edit and no check would
// say so.
//
// ⚠ THIS IS THE NINTH MEMBER OF THE LIVE-DOCUMENT CLASS (§218) declared atop
// w10_desk_documents_test.go: it reads today's design/ via w10DesignDir, FAILS
// rather than skips when design/ is absent, and is EXCLUDED FROM ANY PASS/FAIL
// COUNT. A citation of its result must name the directory it measured.
//
// ⚠ THE NEEDLES ARE CHOSEN SO A MENTION CANNOT DISCHARGE THE ROW. Each is
// counted inside §3 ONLY — the slice between the "## 3" and "## 4" headings,
// extracted BY HEADING and never by line number, because the sheet is
// scheduled to move again (B-4's two probes, B-6's lines). So the sheet's §5
// abort annotation, which also names `seen` and the orientation tables, cannot
// satisfy it. And each probe's two needles must sit in ONE box: (e)'s wire
// literal beside its non-degeneracy clause ("in knowledge"), because a `seen`
// probe whose second amendment is not made in knowledge is one where the
// certified rule and the tiebreak agree — a probe that cannot fail
// (DR-W10-11's defect class, applied to (e)); (f)'s route leg beside its
// peer-dump leg, because the route leg alone proves the key and the dump alone
// proves the fold, and the probe is the round trip. (The route leg is NOT a
// 404: GET / is a catch-all, web.go:158, so the `false` post shows only its
// status page — web_test.go:22-26. Corrected 2026-09-06, W10-SHEET-FIX.)
//
// ⚠ NOTHING HERE ASSERTS W10-3(a) OR (b). Those two are W10-11(b) row 1's,
// still RED on today's sheet (R14, DECLARED-REDS.md:186 — not R13, which is
// the five-commands check), and this file deliberately shares none of
// row 1's needles so that neither check can green the other.
package store_test

import (
	"strings"
	"testing"
)

// w10SheetSection3 returns the sheet's §3 — the lines from the "## 3" heading
// up to (not including) the "## 4" heading — or "" if either heading is
// absent, so a count over a missing section is a loud extraction failure and
// never a quiet 0.
func w10SheetSection3(sheet string) string {
	lines := strings.Split(sheet, "\n")
	start, end := -1, -1
	for i, l := range lines {
		if start < 0 {
			if strings.HasPrefix(l, "## 3") {
				start = i
			}
			continue
		}
		if strings.HasPrefix(l, "## 4") {
			end = i
			break
		}
	}
	if start < 0 || end < 0 {
		return ""
	}
	return strings.Join(lines[start:end], "\n")
}

// w10WindowContentNeedles is the amended W10-3(c)'s discharge for (e) and
// (f): one needle per clause, each counted with w10CountLines (grep -c) over
// §3, and paired per probe by w10SomeBoxCarriesAll.
type w10Needle struct {
	Needle string
	Clause string
}

func w10WindowContentProbes() []struct {
	Probe   string
	Needles []w10Needle
} {
	return []struct {
		Probe   string
		Needles []w10Needle
	}{
		{
			Probe: "W10-3(e)",
			Needles: []w10Needle{
				{"`seen`", "the probe names the wire literal it exercises"},
				{"in knowledge", "the non-degeneracy clause — the second amendment is made IN KNOWLEDGE of the first, or the two rules agree and the probe cannot fail"},
			},
		},
		{
			Probe: "W10-3(f)",
			Needles: []w10Needle{
				{"/orientation", "the marking route — served at the `true` post; at the `false` post only the status page comes back (the GET / catch-all — the program sends no 404)"},
				{`"orientation"`, "the dump block the peer must carry — the PRESENT-in-the-peer leg (DR-W10-9's shape)"},
			},
		},
	}
}

// w10SomeBoxCarriesAll reports whether some ONE checklist box in text — a
// "- [" line plus its continuation lines up to the next blank line, heading,
// or box — contains every needle.
func w10SomeBoxCarriesAll(text string, needles ...string) bool {
	var boxes []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			boxes = append(boxes, strings.Join(cur, "\n"))
			cur = nil
		}
	}
	for _, l := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(l, "- ["):
			flush()
			cur = append(cur, l)
		case len(cur) == 0:
			// outside any box
		case strings.TrimSpace(l) == "" || strings.HasPrefix(l, "#"):
			flush()
		default:
			cur = append(cur, l)
		}
	}
	flush()
	for _, b := range boxes {
		ok := true
		for _, n := range needles {
			if !strings.Contains(b, n) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// w10JudgeWindowContentProbes is the ONE judgement both the live assertion and
// the planted controls run: per probe, every needle present on >=1 line of §3
// AND all of that probe's needles in one box. It returns the failures as
// sentences so the live test and the controls report the same thing.
func w10JudgeWindowContentProbes(sheet string) (failures []string, sectionFound bool) {
	sec := w10SheetSection3(sheet)
	if sec == "" {
		return nil, false
	}
	for _, p := range w10WindowContentProbes() {
		var names []string
		for _, n := range p.Needles {
			names = append(names, n.Needle)
			if c := w10CountLines(sec, n.Needle); c < 1 {
				failures = append(failures, p.Probe+" NOT DISCHARGED: "+n.Needle+" appears on 0 lines of the sheet's §3 — "+n.Clause)
			}
		}
		if !w10SomeBoxCarriesAll(sec, names...) {
			failures = append(failures, p.Probe+" NOT DISCHARGED: no single §3 box carries all of "+strings.Join(names, " and ")+" — the probe's legs are split across boxes or absent")
		}
	}
	return failures, true
}

func TestW10TheSheetCarriesTheWindowContentProbesInSection3(t *testing.T) {
	dir := w10DesignDir(t)
	t.Logf("LIVE-DOCUMENT: measured against design/ at %s", dir)
	sheet := w10ReadDoc(t, dir, "WINDOW-CHECKLIST-v1.md")

	failures, found := w10JudgeWindowContentProbes(sheet)
	if !found {
		t.Fatalf("WINDOW-CHECKLIST-v1.md has no '## 3' … '## 4' section — the extraction failed, " +
			"and a count over an empty section is 0 for the wrong reason")
	}
	for _, f := range failures {
		t.Errorf("%s.\n  The rehearsal would exercise a probe the operator's sheet does not carry, and "+
			"the silent same-number skew (W10-ABORT-PATH.md §4 C) would go unprobed on the night. "+
			"Discharged when: the probe is written into §3 with both its legs in one box "+
			"(W10-3(c) as amended 2026-09-06).", f)
	}
	if len(failures) == 0 {
		t.Logf("W10-3(c) as amended: (e) and (f) are on the sheet's §3, each with both legs in one box, measured at %s", dir)
	}
}

// TestW10TheWindowContentProbeCheckAnswersBothWaysOnPlantedSheets runs the
// SAME judgement on planted sheets in both directions (rule 20: real files
// under t.TempDir()), so the assertion above is neither permanently red nor a
// function that returns discharged for everything. Three red directions are
// produced, because the check has three ways to be fooled and each is a
// sheet that has actually existed or could:
//
//	(1) the probes absent from §3 altogether — the 2026-09-02 sheet;
//	(2) THE MENTION TRAP — every needle present but OUTSIDE §3, which is what
//	    today's sheet becomes if Tests 3 and 4 are struck and the §5
//	    annotation kept;
//	(3) the legs split — each needle in §3 but no box carrying both.
func TestW10TheWindowContentProbeCheckAnswersBothWaysOnPlantedSheets(t *testing.T) {
	plant := func(lines ...string) string {
		return w10PlantDoc(t, "WINDOW-CHECKLIST-v1.md", strings.Join(lines, "\n")+"\n")
	}
	green := plant(
		"## 3 · Verify",
		"- [ ] __ Test 3 — set the name at the Pad; after it shows at the Ice Road, set it again there,",
		"  in knowledge of the first; both posts compose the second. The rule is the `seen` field.",
		"- [ ] __ Test 4 — record it at the Pad's `/orientation`; the Ice Road's `/orientation` shows only the status page;",
		`  the Ice Road's dump carries the row in its "orientation" block.`,
		"",
		"## 4 · After",
		"- [ ] __ Transcribe.",
	)
	if failures, found := w10JudgeWindowContentProbes(green); !found || len(failures) != 0 {
		t.Errorf("GREEN DIRECTION: the judgement still reports failures on a sheet carrying both probes "+
			"in §3 with both legs per box (found=%v): %v — the check is permanently red", found, failures)
	}

	bare := plant(
		"## 3 · Verify",
		"- [ ] __ Test 1 — a person crosses.",
		"- [ ] __ Test 2 — a trip crosses.",
		"",
		"## 4 · After",
		"- [ ] __ Transcribe.",
	)
	if failures, found := w10JudgeWindowContentProbes(bare); !found || len(failures) != 6 {
		t.Errorf("RED DIRECTION (1), the 2026-09-02 sheet: want 6 failures (4 needles + 2 box pairings), "+
			"got %d (found=%v): %v", len(failures), found, failures)
	}

	outside := plant(
		"## 3 · Verify",
		"- [ ] __ Test 1 — a person crosses.",
		"",
		"## 4 · After",
		"- [ ] __ Transcribe.",
		"",
		"## 5 · ABORT",
		"- [ ] __ the `seen` field; a correction made in knowledge; the `/orientation` page;",
		`  the "orientation" block — all named here, none of them a probe.`,
	)
	if failures, found := w10JudgeWindowContentProbes(outside); !found || len(failures) != 6 {
		t.Errorf("RED DIRECTION (2), THE MENTION TRAP: every needle is on the sheet but outside §3; want 6 "+
			"failures, got %d (found=%v): %v — a whole-sheet count would have said discharged",
			len(failures), found, failures)
	}
	// And the rival IS run, not described: a whole-sheet count says
	// discharged on (2), which is why §3-only is the certified rule.
	rivalSaysDischarged := true
	for _, p := range w10WindowContentProbes() {
		for _, n := range p.Needles {
			if w10CountLines(outside, n.Needle) < 1 {
				rivalSaysDischarged = false
			}
		}
	}
	if !rivalSaysDischarged {
		t.Errorf("the named rival (whole-sheet count) did NOT say discharged on the mention-trap sheet; " +
			"the fixture no longer separates the rival from the certified rule")
	}

	split := plant(
		"## 3 · Verify",
		"- [ ] __ Test 3 — the `seen` field decides.",
		"- [ ] __ Test 3b — the second amendment is made in knowledge of the first.",
		"- [ ] __ Test 4 — open `/orientation` at the Pad.",
		`- [ ] __ Test 4b — find the "orientation" block in the Ice Road's dump.`,
		"",
		"## 4 · After",
	)
	if failures, found := w10JudgeWindowContentProbes(split); !found || len(failures) != 2 {
		t.Errorf("RED DIRECTION (3), the legs split across boxes: every needle counts >=1 in §3, so want "+
			"exactly the 2 box-pairing failures, got %d (found=%v): %v", len(failures), found, failures)
	}

	noSection := plant("## 2 · The window", "- [ ] __ stop both.", "")
	if _, found := w10JudgeWindowContentProbes(noSection); found {
		t.Errorf("a sheet with no §3 reported a section — the extraction is not bound to its headings")
	}
	t.Logf("BOTH DIRECTIONS PRODUCED: green on a carrying sheet; red on the bare sheet (6), on the " +
		"mention-trap sheet (6, where the whole-sheet rival says discharged), and on the split sheet (2)")
}
