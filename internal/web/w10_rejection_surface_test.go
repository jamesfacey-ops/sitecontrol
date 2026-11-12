// w10_rejection_surface_test.go — W10-6(b)'s TWO SURFACES, and W10-9's
// absent clock-verdict channel.
//
// Criteria source: design/WINDOW-BUILD-PLAN-v1.md §3, `### W10 — Window
// integration and rehearsal`, LOCKED 2026-09-03 (reports/W10-FOLD-RECORD.md,
// gate P-4). Written from the LOCKED CRITERION TEXT ONLY.
//
// Criteria carried here:
//
//   - W10-6(b) — "the surface is named, and BOTH renderings are observed".
//     ASSERTION: "the OPERATOR-FACING PAGE displaying the label
//     `IDENTITY REJECTED — record failed the §8.4 authorship check` on
//     `GET /`, and (separately) the JSON state `identity_rejected` at
//     `GET /debug/sync-status`."
//     FALSIFIER: "The evidence is the JSON state only."
//     Non-degeneracy, measured by the criterion itself: "`AllStreams`
//     carries EVERY entry in Snapshot.Streams — nothing filtered out …
//     and `stateLabel`'s default arm renders an unrecognised state
//     VERBATIM. So the surface is structurally capable."
//
//   - W10-9 — "the clock-verdict channel exists before the window". The
//     criterion's own measurement, quoted: "`grep -rl 'synced'
//     internal/web/templates/` returns nothing and `sitectl` gains no verb
//     … [MEASURED 2026-09-03: zero template hits; two flags.]"
//
// ⚠ THE HALVES THIS FILE CANNOT REACH, stated rather than implied.
//   - W10-6(a) is a LAB RUN in the single-post configuration, with a
//     mandatory recovery command. Nothing here executes it and nothing here
//     may be read as evidence that it happened.
//   - W10-6(c) derives its control set from the PRINTED SHEET. Not code.
//   - W10-9's own falsifier has a third conjunct — "while §3's clock line is
//     ticked WITHOUT the word 'substitute' written beside the answers" —
//     which is a property of a filled-in sheet. Only the first two conjuncts
//     are mechanised here, and the test name says so.
//     See reports/window-build/W10-tests.md §2 for the full split.
//
// ⚠ W10-9's HALF IS A SENTINEL, NOT AN EXPECTED RED — polarity inverted at
// §108 (S101). It was ORIGINALLY WRITTEN WITH THE OPPOSITE POLARITY, asserting
// that the channel EXISTS and therefore PERMANENTLY RED by design. It is
// now GREEN while W10-9's ruled gap holds, and REDS when the channel is built.
// ⟦was: "TestW10AClockVerdictChannelExistsOnThisTree"⟧ — the pre-§108 name, which
// said EXISTS where the sentinel now says ABSENT. ⭐ ⟦LOCK — §137, 2026-09-03⟧ restored
// to the file: §132 had to relocate it to the report because the citations guard could
// not tell a RECORD from a CITATION; §137 taught it that ⟦was:⟧ means retired.
// ⚠ Do NOT "correct" it to the live name — the sentence above is about the OLD polarity,
// and the current name asserts the opposite, so the correction would contradict itself.
// The reasoning is on the sentinel itself; the short form is that a permanent
// red is indistinguishable, to any reader and to any grep, from a regression.
//
// ⚠ RULE 20. The template scan reads files ON DISK, so the controls plant
// into real directories under t.TempDir(); nothing under app/ is written.
package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/repl"
)

// ---------------------------------------------------------------------------
// W10-6(b) — both renderings.
// ---------------------------------------------------------------------------

// w10IdentityRejectedLabel is the operator-facing wording, pinned as a
// literal from the LOCKED criterion text — not read back from stateLabel,
// which would make the assertion compare the tree to itself.
const w10IdentityRejectedLabel = "IDENTITY REJECTED — record failed the §8.4 authorship check"

// w10RejectedSnapshot is one post whose peer stream has been refused by the
// §8.4 authorship check — the state W10-6's single-post control produces.
func w10RejectedSnapshot() repl.Snapshot {
	return repl.Snapshot{
		SiteID: "site-b",
		Peers: []repl.PeerSnapshot{
			{SiteID: "site-a", LastContactAgoSec: w10Secs(3), Drains: 9},
		},
		Streams: []repl.StreamStatus{
			{
				StreamKey: repl.StreamKey{SiteID: "site-a", NodeEpoch: 4},
				Watermark: 120, Head: 121,
				State:  repl.StateIdentityRejected,
				Detail: "origin_auth did not match the delivering peer",
			},
		},
	}
}

func w10Secs(v float64) *float64 { return &v }

func TestW10TheIdentityRejectionRendersOnTheOperatorPageAndNotOnlyInJSON(t *testing.T) {
	snap := w10RejectedSnapshot()

	// --- surface 1: the operator-facing page. The REAL embedded template,
	// through the REAL view builder — not a string comparison against
	// stateLabel's return value, which would prove nothing about rendering.
	view := buildSyncStatusView(snap, testSyncInterval)
	var page bytes.Buffer
	if err := tmpl.ExecuteTemplate(&page, "sync_status", struct{ Sync syncStatusView }{Sync: view}); err != nil {
		t.Fatalf("rendering the sync_status template: %v", err)
	}
	body := page.String()
	if !strings.Contains(body, w10IdentityRejectedLabel) {
		t.Errorf("the operator-facing page does NOT carry %q.\nrendered:\n%s", w10IdentityRejectedLabel, body)
	}

	// ⚠ THE LABEL REACHES THE PAGE BY TWO ROUTES, AND A BARE Contains CANNOT
	// TELL THEM APART. That was found by producing the failure: a scratch
	// copy whose stream row rendered {{.State}} instead of {{.Label}} STILL
	// passed a whole-body Contains, because buildPeerView splices
	// badStream.Label into the peer's verdict sentence
	// (internal/web/syncstatus.go, the log-STALLED branch). So each route is
	// pinned separately below, and a red names which one went.
	cell := regexp.MustCompile(`<td class="state-(?:ok|bad)">([^<]*)</td>`)
	var cells []string
	for _, m := range cell.FindAllStringSubmatch(body, -1) {
		cells = append(cells, m[1])
	}
	if len(cells) == 0 {
		t.Fatalf("no stream-state cell rendered at all — the row this criterion is about is "+
			"absent from the page.\nrendered:\n%s", body)
	}
	inStreamRow := false
	for _, c := range cells {
		if strings.TrimSpace(c) == w10IdentityRejectedLabel {
			inStreamRow = true
		}
	}
	if !inStreamRow {
		t.Errorf("ROUTE 1 (the stream table) does NOT carry the label. Cells rendered: %q. "+
			"The operator reads this table; a label that survives only in the peer verdict "+
			"sentence is a different sentence in a different place.", cells)
	}
	verdictLine := ""
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "last heard from") {
			verdictLine = line
		}
	}
	if verdictLine == "" {
		t.Errorf("ROUTE 2 (the peer verdict sentence) is absent from the page — the fixture no " +
			"longer produces a peer verdict, so this test measures one route where it claims two")
	}
	// The reassuring-zero guard the sheet itself carries: an empty stream
	// table renders "no stream state observed yet", which §2 of the sheet
	// says is NOT a pass. If that string is present, the label above was
	// found in a page that showed the operator nothing.
	if strings.Contains(body, "no stream state observed yet") {
		t.Errorf("the rendered page fell through to the empty-table arm — the stream never " +
			"reached AllStreams, so this fixture proves nothing about the rejection")
	}

	// --- surface 2: the JSON. `Engine.StatusHandler` writes
	// `json.NewEncoder(w).Encode(e.Snapshot())` and nothing else
	// (internal/repl/engine.go, StatusHandler), so marshalling the Snapshot
	// is byte-faithful to what GET /debug/sync-status serves.
	blob, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	jsonBody := string(blob)
	if !strings.Contains(jsonBody, `"`+repl.StateIdentityRejected+`"`) {
		t.Errorf("the JSON surface does not carry the raw state %q:\n%s", repl.StateIdentityRejected, jsonBody)
	}

	// --- W10-6(b)'s FALSIFIER, made mechanical: "The evidence is the JSON
	// state ONLY." The two surfaces must NOT be interchangeable, or a run
	// that recorded the JSON could be read as having recorded the page.
	if strings.Contains(jsonBody, w10IdentityRejectedLabel) {
		t.Errorf("the JSON surface carries the operator LABEL as well as the raw state — the " +
			"two surfaces are no longer distinguishable, so W10-6(b)'s falsifier " +
			"(\"the evidence is the JSON state only\") can no longer fire")
	}
	if strings.Contains(body, `"`+repl.StateIdentityRejected+`"`) {
		t.Errorf("the rendered page carries the raw JSON state spelling; the two surfaces are " +
			"not distinguishable in the other direction either")
	}

	t.Logf("W10-6(b) BOTH SURFACES, observed: stream-table cells=%q · peer-verdict line present=%v · "+
		"JSON carries raw state=%v · JSON carries the label=%v",
		cells, verdictLine != "",
		strings.Contains(jsonBody, `"`+repl.StateIdentityRejected+`"`),
		strings.Contains(jsonBody, w10IdentityRejectedLabel))
}

// TestW10AnUnrecognisedStreamStateSurvivesToThePageVerbatim is W10-6(b)'s
// non-degeneracy arm. The certified rule is stateLabel's default arm
// ("a state this file doesn't know about must never silently disappear from
// the page"); the named rivals are the two ways it could be written wrong.
// All three are RUN on one fixture and must return DIFFERENT answers.
func TestW10AnUnrecognisedStreamStateSurvivesToThePageVerbatim(t *testing.T) {
	const future = "quarantined_pending_officer_review" // a state no arm knows

	// Rival A: drop what you do not recognise.
	rivalDrop := func(state string) string {
		switch state {
		case repl.StateOK, repl.StateSchemaMismatch, repl.StateRegressed,
			repl.StateIdentityRejected, repl.StateGapStalled, repl.StateError:
			return stateLabel(state)
		default:
			return ""
		}
	}
	// Rival B: collapse what you do not recognise into a generic word.
	rivalGeneric := func(state string) string {
		if rivalDrop(state) == "" {
			return "unknown"
		}
		return stateLabel(state)
	}

	certified := stateLabel(future)
	if certified != future {
		t.Errorf("stateLabel(%q) = %q, want the state VERBATIM", future, certified)
	}
	if rivalDrop(future) == certified || rivalGeneric(future) == certified {
		t.Fatalf("DEGENERATE FIXTURE: a rival returns the same answer as the certified rule "+
			"(drop=%q generic=%q certified=%q)", rivalDrop(future), rivalGeneric(future), certified)
	}

	// And it must reach the PAGE, not merely the function.
	snap := w10RejectedSnapshot()
	snap.Streams[0].State = future
	var page bytes.Buffer
	if err := tmpl.ExecuteTemplate(&page, "sync_status",
		struct{ Sync syncStatusView }{Sync: buildSyncStatusView(snap, testSyncInterval)}); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(page.String(), future) {
		t.Errorf("an unrecognised state did not reach the rendered page — AllStreams or the "+
			"template filtered it.\nrendered:\n%s", page.String())
	}
	t.Logf("NON-DEGENERACY, pinned: certified=%q · rival(drop)=%q · rival(generic)=%q",
		certified, rivalDrop(future), rivalGeneric(future))
}

// ---------------------------------------------------------------------------
// W10-9 — the clock-verdict channel.
// ---------------------------------------------------------------------------

func w10WebModuleRoot(t *testing.T) string {
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
			t.Fatalf("no go.mod above %q", dir)
		}
		dir = parent
	}
}

// w10ClockVerdictTemplateHits is the JUDGEMENT FUNCTION, shared by the
// assertion and by its green-direction control. It is W10-9's own command —
// `grep -rl 'synced' internal/web/templates/` — over a real directory.
//
// "synced" is the substring on purpose: store's frozen vocabulary is
// synced | unsynced | unverified, and the first two both contain it, so a
// template rendering EITHER verdict is a hit. A template that could only
// ever print "synced" is what W10-9's control calls "a check that cannot
// fail"; that is a property of the rendering, not of this scan, and it is
// asserted separately below.
//
// ⚠ FIDELITY, repaired by S101 — the red above is UNCHANGED by it.
// The command this function claims to be is `grep -rl`, and `-r` is
// RECURSIVE. This walked ONE directory level and skipped `e.IsDir()`, so a
// verdict template under `templates/partials/` was invisible to it. That is
// harmless in the current polarity — a non-recursive miss is a subset of a
// recursive one, so today's red stands either way — but it is blind to the
// EXACT event this check exists to notice: the channel being built. Produced
// as a failure before repairing: a plant at `partials/status.html` was NOT
// seen by the old scan and IS seen by this one.
//
// ⛔ AND THE EMPTINESS GUARD COUNTED THE WRONG THING. `len(entries) == 0`
// counts DIRECTORY ENTRIES, but only `.html` files are ever read, so a
// directory holding one README and no templates cleared the guard while
// scanning ZERO files. It now counts FILES ACTUALLY SCANNED, which is the
// quantity the "for the wrong reason" warning was always about. This matters
// far more if this check is ever INVERTED into a still-absent sentinel: in
// that polarity both holes are FALSE GREENS, and one of them reports "not
// built yet" about a tree where it has already been built.
func w10ClockVerdictTemplateHits(t *testing.T, templatesDir string) []string {
	t.Helper()
	return w10GuardedScan(t, templatesDir)
}

// w10GuardedScan is the scan PLUS its guards, with the failure sink swappable
// so the GUARDS THEMSELVES can be pinned. ⛔ Mutation M3 in the §108 pass
// deleted the emptiness guard — `if scanned == 0` → `if false` — and the suite
// stayed GREEN; a guard indistinguishable from its own absence is decoration.
// TestW10TheEmptinessGuardRefusesAScanThatReadNothing now kills that mutant.
func w10GuardedScan(rep w10Reporter, templatesDir string) []string {
	hits, scanned, err := w10ScanTemplates(templatesDir)
	if err != nil {
		rep.Fatalf("scanning %s: %v", templatesDir, err)
		return nil // see w10RecordedFailures: a recording sink does not abort
	}
	if scanned == 0 {
		rep.Fatalf("%s held NO .html files to scan — a scan over no files returns no hits for "+
			"the wrong reason", templatesDir)
		return nil
	}
	return hits
}

// TestW10TheEmptinessGuardRefusesAScanThatReadNothing pins the guard's REFUSAL
// BRANCH, not merely the quantity it reads. Both are needed and neither
// substitutes for the other: the sibling test proves the guard counts .html
// files rather than directory entries, and this one proves the guard actually
// FIRES on that count — the half that mutation M3 showed was unpinned.
func TestW10TheEmptinessGuardRefusesAScanThatReadNothing(t *testing.T) {
	// A directory with entries but no templates: the shape that cleared the
	// original `len(entries) == 0` guard while reading nothing.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.txt"), []byte("not a template"), 0o600); err != nil {
		t.Fatal(err)
	}
	blind := &w10RecordedFailures{}
	if hits := w10GuardedScan(blind, dir); hits != nil {
		t.Fatalf("the guard returned hits %v after refusing — a caller would use them", hits)
	}
	if len(blind.fatals) != 1 {
		t.Fatalf("⛔ THE EMPTINESS GUARD DID NOT FIRE over a directory holding zero .html files. "+
			"Without it, a moved or emptied templates directory reads as 'no clock verdict "+
			"present' and the sentinel goes FALSE GREEN. fatals = %v", blind.fatals)
	}
	if !strings.Contains(blind.fatals[0], "for the wrong reason") {
		t.Fatalf("the guard fired but not with its own diagnosis: %q", blind.fatals[0])
	}

	// A missing directory must refuse too, and by the OTHER branch.
	gone := &w10RecordedFailures{}
	if hits := w10GuardedScan(gone, filepath.Join(dir, "does-not-exist")); hits != nil {
		t.Fatalf("the guard returned hits %v for a missing directory", hits)
	}
	if len(gone.fatals) != 1 {
		t.Fatalf("a MISSING templates directory did not refuse: fatals = %v", gone.fatals)
	}

	// And the real directory must pass both guards, or every green here is
	// meaningless.
	quiet := &w10RecordedFailures{}
	w10GuardedScan(quiet, filepath.Join(w10WebModuleRoot(t), "internal", "web", "templates"))
	if len(quiet.fatals) != 0 {
		t.Fatalf("the REAL templates directory tripped a guard: %v", quiet.fatals)
	}
	t.Logf("GUARD REFUSAL PINNED: zero-.html refuses (%d fatal), missing dir refuses (%d fatal), "+
		"real dir passes clean.", len(blind.fatals), len(gone.fatals))
}

// w10ScanTemplates is the PURE scan behind the guard above: the hits, and the
// number of .html files it ACTUALLY READ.
//
// ⚠ IT IS SPLIT OUT SO THE GUARD CAN BE PINNED, and it was split out because
// the guard was measured UNPINNED. Mutation M3 in the §108 pass disabled the
// emptiness guard outright — `if scanned == 0` → `if false` — and the whole
// suite stayed GREEN. ⛔ A guard no test can distinguish from its own absence
// is decoration. `scanned` is the quantity the guard reads, so returning it
// is what lets TestW10TheTemplateScanGuardCountsFilesReadNotEntries pin it.
func w10ScanTemplates(templatesDir string) (hits []string, scanned int, err error) {
	err = filepath.WalkDir(templatesDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".html") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		scanned++
		if strings.Contains(string(b), "synced") {
			rel, relErr := filepath.Rel(templatesDir, p)
			if relErr != nil {
				rel = d.Name()
			}
			hits = append(hits, rel)
		}
		return nil
	})
	return hits, scanned, err
}

// TestW10TheTemplateScanGuardCountsFilesReadNotEntries pins the QUANTITY the
// emptiness guard reads, and it exists because the original guard read the
// WRONG one: `len(entries) == 0` over os.ReadDir, when only .html files are
// ever opened. A directory holding one README and no templates therefore
// cleared the guard while scanning ZERO files — "no hits for the wrong
// reason", which is the exact thing the guard's own message warns about.
//
// ⚠ This is the case that makes DERIVED necessary but NOT SUFFICIENT. The
// enumeration is derived (os.ReadDir/WalkDir at runtime, no hand-kept list, so
// it cannot rot short) and it was STILL wrong, because the guard protecting it
// counted a different thing from the thing being counted.
func TestW10TheTemplateScanGuardCountsFilesReadNotEntries(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.txt"), []byte("not a template"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("fixture: reading %s: %v", dir, err)
	}
	if len(entries) == 0 {
		t.Fatal("fixture defect: the directory is empty, so the two quantities cannot be told apart")
	}

	hits, scanned, err := w10ScanTemplates(dir)
	if err != nil {
		t.Fatalf("scanning %s: %v", dir, err)
	}
	if scanned != 0 {
		t.Fatalf("the scan reported reading %d .html files in a directory that holds none", scanned)
	}
	if len(hits) != 0 {
		t.Fatalf("the scan reported hits over zero files read: %v", hits)
	}
	if len(entries) == scanned {
		t.Fatal("entries and files-read are equal here, so this fixture cannot show the guard " +
			"reads the right quantity")
	}
	t.Logf("GUARD QUANTITY PINNED: directory entries = %d but .html files read = %d. The old "+
		"guard tested the first and passed; the guard tests the second and fatals.",
		len(entries), scanned)

	// The real directory must be on the other side of the guard, or the guard
	// would fire on every run and this pin would be worthless.
	root := w10WebModuleRoot(t)
	_, realScanned, err := w10ScanTemplates(filepath.Join(root, "internal", "web", "templates"))
	if err != nil {
		t.Fatalf("scanning the real templates dir: %v", err)
	}
	if realScanned == 0 {
		t.Fatal("the REAL templates directory scans zero .html files — the guard would fatal on " +
			"every run and every green in this file would be meaningless")
	}
	t.Logf("REAL DIRECTORY: %d .html files scanned, so the guard is dormant in normal operation.", realScanned)
}

// w10Reporter is the SENTINEL'S FAILURE SINK, and it is an interface for one
// reason: the control below must run the sentinel's OWN assertion against a
// planted tree and WATCH IT FIRE, without that firing failing the run doing
// the watching.
//
// ⛔ THE OBVIOUS WAY TO DO THAT DOES NOT WORK, and it was tried first: wrap
// the call in `t.Run` and check the returned bool. Go marks the PARENT failed
// when a subtest fails, so the control became a permanently-red test — which
// is precisely the defect §108 inverted this file to remove. Swapping the
// SINK, rather than the test, keeps the real judgement function, the real
// branch and the real message composition under test and lets the control
// pass.
type w10Reporter interface {
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// w10RecordedFailures is the control's sink. *testing.T is the real one.
//
// ⚠ Errorf and Fatalf are recorded SEPARATELY because they mean different
// things here: Errorf is the sentinel reporting the channel was built (a
// finding), Fatalf is a guard reporting the scan could not be trusted (a
// refusal). A control that conflated them could not tell "fired" from
// "refused to run".
//
// ⛔ Fatalf here does NOT abort, because a plain struct cannot runtime.Goexit.
// Callers must therefore RETURN after reporting a fatal — see w10GuardedScan —
// or the control would go on to observe results the real *testing.T would
// never have produced.
type w10RecordedFailures struct {
	msgs   []string
	fatals []string
}

func (r *w10RecordedFailures) Errorf(format string, args ...any) {
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

func (r *w10RecordedFailures) Fatalf(format string, args ...any) {
	r.fatals = append(r.fatals, fmt.Sprintf(format, args...))
}

// w10ClockVerdictSentinel is the SENTINEL'S OWN ASSERTION, extracted so that
// the control below can exercise THE ASSERTION ITSELF rather than a copy of
// it. ⛔ A control that re-implements the assertion proves only that the copy
// fires.
func w10ClockVerdictSentinel(t *testing.T, rep w10Reporter, templatesDir, measuredAt string) {
	t.Helper()
	hits := w10ClockVerdictTemplateHits(t, templatesDir)
	if len(hits) != 0 {
		rep.Errorf("THE CLOCK-VERDICT CHANNEL NOW EXISTS: %v\n"+
			"⚠ THIS IS NOT A DEFECT. It is the event this sentinel was armed to catch, and it is\n"+
			"GOOD NEWS — W10-9 wanted this built ('an operator who cannot read the verdict cannot\n"+
			"catch it at the only moment it is catchable'). What it means is that W10-9's NARROWING\n"+
			"ARM no longer applies, and four things downstream are now STALE:\n"+
			"  1. §3's clock line is classified SIMULATED under W10-2(a) per OPS-F9/FAL-F3.\n"+
			"     Reclassify it — the action is now performable against the program's own verdict.\n"+
			"  2. The printed sheet instructs the operator to run `timedatectl` and to WRITE\n"+
			"     'substitute' BESIDE THE ANSWERS. That instruction is now wrong; §3 must be re-cut.\n"+
			"  3. W10-9(c)'s calibration — `timedatectl` against the program's stored AuthoredAt.State,\n"+
			"     same box, same minute — becomes PERFORMABLE, so it is no longer discharged by the\n"+
			"     'the substitute is UNCALIBRATED' sentence.\n"+
			"  4. W10-9's falsifier's FIRST conjunct is now false, so the criterion may TICK.\n"+
			"Then RETIRE this sentinel: it has done its job and has nothing left to hold open.\n"+
			"MEASURED AT: %s", hits, measuredAt)
	}
}

// TestW10TheClockVerdictChannelIsStillAbsentAsW10Dash9Recorded is a SENTINEL
// — §82's FOURTH disposition, ruled at §108: a test that holds open a gap
// which has already been MEASURED and DISPOSITIONED, and which is GREEN for
// exactly as long as that ruling stands.
//
// ⚠ IT USED TO BE A PERMANENT RED, and that was wrong for three reasons, all
// of them in the LOCKED criterion text rather than in anybody's taste:
//
//  1. ⛔ W10-9 DOES NOT TREAT THE UNBUILT CHANNEL AS A FAILURE. Its narrowing
//     arm rules the opposite: "If the channel is not built, W10-2(a) marks §3's
//     clock line SIMULATED … W10-2 remains green on its other lines … The
//     criterion NARROWS; it never becomes a tick." The old test encoded W10-9's
//     assertion arm and ignored W10-9's own disposition of the very state it
//     was measuring.
//  2. ⛔ THE FALSIFIER IS A THREE-CONJUNCT AND THIS FILE CAN ONLY REACH TWO.
//     The third — "while §3's clock line is ticked WITHOUT the word 'substitute'
//     written beside the answers" — is a property of a FILLED-IN PAPER SHEET.
//     The old test therefore reddened on two of three conjuncts of an AND, and
//     two thirds of a conjunction is not a falsification.
//  3. ⛔ OPS-F9 RE-AIMED THAT FALSIFIER PRECISELY BECAUSE revision 1 "fired on
//     CORRECT operator behaviour", and "a criterion that reds on correct
//     behaviour teaches its reader to ignore it". The old test reproduced IN
//     CODE the exact failure mode OPS-F9 was raised to remove from the prose.
//
// ⚠ And a permanent red is not free. It is indistinguishable, to any reader
// and to any grep, from a regression — it consumes the signal a repair pass
// exists to read.
//
// GREEN here means the ruled gap still holds. RED means somebody BUILT the
// channel, which is good news and a prompt to revisit a downstream ruling.
func TestW10TheClockVerdictChannelIsStillAbsentAsW10Dash9Recorded(t *testing.T) {
	root := w10WebModuleRoot(t)
	dir := filepath.Join(root, "internal", "web", "templates")
	w10ClockVerdictSentinel(t, t, dir, root)
	t.Logf("SENTINEL GREEN at %s: no template renders the clock verdict, so W10-9's narrowing "+
		"arm still applies and §3's clock line remains SIMULATED against `timedatectl` "+
		"(W10-2(a), per OPS-F9/FAL-F3).", root)
}

// TestW10TheClockVerdictSentinelFiresWhenTheChannelIsBuilt PRODUCES THE
// SENTINEL'S FAILURE rather than asserting it would happen. ⛔ Required at
// §108, and the plant is deliberately at partials/status.html — A
// SUBDIRECTORY — because that is the exact case where the pre-§108 scan went
// BLIND: it used a one-level os.ReadDir that skipped e.IsDir(), so it returned
// [] for a tree where the channel HAD ALREADY BEEN BUILT. Inverted on top of
// that scan, this sentinel would have been a FALSE GREEN reporting "not built
// yet" about a tree where it was built. The scan is recursive now (`grep -rl`
// always was), and this control is what keeps it that way.
func TestW10TheClockVerdictSentinelFiresWhenTheChannelIsBuilt(t *testing.T) {
	dir := t.TempDir()
	// The shape of the real directory: top-level templates, none of them a
	// verdict. The sentinel must be SILENT here.
	if err := os.WriteFile(filepath.Join(dir, "banner.html"),
		[]byte(`{{define "banner"}}<div>nothing about clocks here</div>{{end}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	quiet := &w10RecordedFailures{}
	w10ClockVerdictSentinel(t, quiet, dir, dir)
	if len(quiet.msgs) != 0 {
		t.Fatalf("CONTROL FAILED: the sentinel fired on a directory with NO verdict template, so "+
			"it would be red on every tree — the permanent red §108 removed. Fired with: %v", quiet.msgs)
	}

	// Now build the channel, in the place the old one-level scan could not see.
	sub := filepath.Join(dir, "partials")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "status.html"),
		[]byte(`{{define "status"}}<p>clock: {{.ClockState}} (synced|unsynced|unverified)</p>{{end}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fired := &w10RecordedFailures{}
	w10ClockVerdictSentinel(t, fired, dir, dir)
	if len(fired.msgs) != 1 {
		t.Fatalf("⛔ THE SENTINEL DID NOT FIRE on a verdict template at partials/status.html — it "+
			"is BLIND to a channel built in a subdirectory, which is the false green §108 required "+
			"to be produced. Check that the scan is still recursive. Fired: %v", fired.msgs)
	}
	// The message must NAME the subdirectory path, or a reader cannot act on it.
	if want := filepath.Join("partials", "status.html"); !strings.Contains(fired.msgs[0], want) {
		t.Fatalf("the sentinel fired but its message does not name %q, so it cannot be acted on:\n%s",
			want, fired.msgs[0])
	}
	t.Logf("FAILURE PRODUCED (§108): the sentinel is SILENT with no verdict template and FIRES on "+
		"a verdict planted at %s — the subdirectory case the pre-§108 one-level scan returned [] "+
		"for. Both directions observed by running the SENTINEL ITSELF against a swapped sink.",
		filepath.Join("partials", "status.html"))
}

// TestW10TheClockVerdictScanCanGoGreen pins the JUDGEMENT FUNCTION's
// discrimination directly, at the TOP level, independently of the sentinel
// that consumes it. ⚠ It is not redundant with the control above: that one
// proves the SENTINEL fires on a SUBDIRECTORY plant, this one proves the SCAN
// itself separates "no verdict" from "verdict" without a subdirectory in play,
// so a regression in either the recursion or the matching is attributable.
//
// ⚠ Its old wording called this "the OTHER DIRECTION" of a permanently-red
// check; after §108 the sentinel's normal state is GREEN, so the mirror
// failure W10-11(b) row 6 names — a check that can never go red — is now the
// one that matters here, and it is what this pins.
//
// The plant is written into a REAL on-disk directory (rule 20: `go test
// -overlay` does not reach a file scan), and it is under t.TempDir(), so
// nothing under app/ is touched.
func TestW10TheClockVerdictScanCanGoGreen(t *testing.T) {
	dir := t.TempDir()
	// A copy of the shape the real directory has, minus any verdict.
	if err := os.WriteFile(filepath.Join(dir, "banner.html"),
		[]byte(`{{define "banner"}}<div>nothing about clocks here</div>{{end}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if hits := w10ClockVerdictTemplateHits(t, dir); len(hits) != 0 {
		t.Fatalf("the scan reported hits on a directory with no verdict: %v", hits)
	}
	// Now plant the channel W10-9 requires.
	if err := os.WriteFile(filepath.Join(dir, "status.html"),
		[]byte(`{{define "status"}}<p>clock: {{.ClockState}} (synced|unsynced|unverified)</p>{{end}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	hits := w10ClockVerdictTemplateHits(t, dir)
	if len(hits) != 1 || hits[0] != "status.html" {
		t.Fatalf("the scan did not go green on a planted verdict: hits = %v", hits)
	}
	t.Logf("DISCRIMINATION PINNED: the same judgement function returns [] with no verdict and "+
		"%v with one, so the sentinel's green is a state of the tree and not a check that can "+
		"never fire", hits)
}
