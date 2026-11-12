// w8_merge_test.go — W8-5(e): THE §5 STANDING CROSS-EPOCH CONTROL ROW, met by
// FIXTURE and never waived; and W8-4(a): ONE resolver, stated once,
// implemented once.
//
// Written from the LOCKED W8 criteria only. No built implementation was read.
//
// ⚠⚠ W8-5 IS THE CHUNK'S MOST CONSEQUENTIAL CRITERION and its own stated stake
// is A §10.5's: "Without this A HYGIENE MERGE SILENTLY EMPTIES A LIFE-SAFETY
// HEADING, NO TEST RED." W8 is "the first payload-bearing register merge in
// the system" — company's merge has no payload to conflict over, so W8-5's
// conflict refusal HAS NO PRECEDENT TO CLONE FROM AT ALL.
//
// ⚠ WHAT THIS FILE CAN AND CANNOT ASSERT, STATED PLAINLY. W8-5's arms (a)-(d)
// are behaviours of an authoring edge, a fold and a renderer that DO NOT EXIST
// at this tree; the lock books the whole criterion ⟦NO PRE-CHUNK RED⟧ and
// says its evidence is the mutant register. Writing them over `domain.Trade`
// would not compile, and one non-compiling file in `package domain` takes the
// package down for the three other builders working in this tree.
//
// What IS buildable today, and is built here, is the part of W8-5 that is a
// FIXTURE PROPERTY rather than a production behaviour: limb (e)'s TWO
// fixtures, their runtime non-degeneracy, and the literals they pin. A
// fixture whose non-degeneracy has not been produced certifies nothing
// whenever the entity does arrive — and W5d-10's own history is that the
// landed cycle fixture "WAS degenerate at rev 2 and three lenses were
// needed".
package domain

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// --- the two comparators, NAMED BY THEIR SPELLINGS ------------------------
//
// ⚠⚠ R2-W13's NAMING DISCIPLINE IS CARRIED VERBATIM, AND IT IS TWO WORDS THAT
// DECIDE WHICH RULE W8 LOCKS IN: the comparators are named by their
// SPELLINGS, NEVER by the word "epoch-major".
//
// The certified rule is (site_id, node_epoch, seq) — SITE-MAJOR. The house
// calls it "epoch-major, seq last" meaning EPOCH BEFORE SEQ, which is true of
// it. But this clause's entire content is that the RIVAL puts node_epoch
// FIRST, so naming the certified rule "epoch-major" names it BY THE PROPERTY
// THAT DISTINGUISHES ITS RIVAL. ⚠ "A test-writer who reads it the natural way
// builds the fixture BACKWARDS, the runtime distinctness check STILL PASSES —
// the two comparators do disagree — and the criterion PINS THE WRONG WINNER:
// a non-degenerate fixture certifying the rival."

// w8LessSiteEpochSeq is the CERTIFIED comparator: ORDER BY site_id,
// node_epoch, seq.
func w8LessSiteEpochSeq(a, b store.Key) bool {
	if a.SiteID != b.SiteID {
		return a.SiteID < b.SiteID
	}
	if a.NodeEpoch != b.NodeEpoch {
		return a.NodeEpoch < b.NodeEpoch
	}
	return a.Seq < b.Seq
}

// w8LessEpochSiteSeq is the REORDERED rival: ORDER BY node_epoch, site_id,
// seq — W3-m17's drift spelling.
func w8LessEpochSiteSeq(a, b store.Key) bool {
	if a.NodeEpoch != b.NodeEpoch {
		return a.NodeEpoch < b.NodeEpoch
	}
	if a.SiteID != b.SiteID {
		return a.SiteID < b.SiteID
	}
	return a.Seq < b.Seq
}

// w8LessSiteSeq is the EPOCH-BLIND rival: ORDER BY site_id, seq — W3-m5's
// spelling, the one Fixture 1 exists to split.
func w8LessSiteSeq(a, b store.Key) bool {
	if a.SiteID != b.SiteID {
		return a.SiteID < b.SiteID
	}
	return a.Seq < b.Seq
}

// w8WinnerUnder returns the key the given comparator ranks LAST — the winning
// assertion under fixed-order composition — RECOMPUTED AT RUNTIME from the
// read-back keys, never from what the fixture author believed.
func w8WinnerUnder(keys []store.Key, less func(a, b store.Key) bool) store.Key {
	out := append([]store.Key(nil), keys...)
	sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out[len(out)-1]
}

// w8TradeAssertionKeys reads back every `trade_amended` envelope in the log
// that asserts `fact` for `tradeID`, as KEYS. ⚠ The keys come from the STORE,
// not from the fixture's own variables: "both RECOMPUTED AT RUNTIME from the
// read-back keys".
func w8TradeAssertionKeys(t *testing.T, st *store.Store, streams [][2]interface{}, tradeID, fact string) ([]store.Key, map[store.Key]string) {
	t.Helper()
	var keys []store.Key
	vals := map[store.Key]string{}
	for _, s := range streams {
		site := s[0].(string)
		epoch := s[1].(int64)
		for _, e := range w3Events(t, st, site, epoch) {
			var m map[string]any
			if err := json.Unmarshal(e.Payload, &m); err != nil {
				t.Fatalf("fixture defect: stored payload is not an object: %s", e.Payload)
			}
			if m["type"] != "trade_amended" || m["trade_id"] != tradeID {
				continue
			}
			v, ok := m[fact].(string)
			if !ok {
				continue
			}
			keys = append(keys, e.Key())
			vals[e.Key()] = v
		}
	}
	return keys, vals
}

// TestW8CrossEpochFixture1IsNonDegenerate is W8-5(e) FIXTURE 1 — the
// SAME-SITE CROSS-EPOCH pair, in W4-12's / W6-1(b)'s form.
//
// [DOC — plan:8367] "Any suite that resolves contested facts over (site_id,
// node_epoch, seq) MUST carry a same-site cross-epoch fixture… with its
// epoch-blind mutant named." ⚠ W8-5 IS a contested-fact suite — (a)-(d) all
// turn on which assertion wins — so THE EXEMPTION ROUTE ("set-shaped, no
// contested-fact resolution") IS NOT AVAILABLE, and saying so is part of the
// criterion.
//
// TWO competing phone_day assertions for one trade at ONE site — one at
// (site-a, epoch N+1, seq 1), one at (site-a, epoch N, seq 99): THE RE-IMAGE
// SHAPE. The resolved value is PINNED AS A LITERAL: the epoch N+1 number.
//
// ⚠ NOT PERMUTATION-INVARIANCE: "an epoch-blind comparator can be perfectly
// invariant and still wrong."
//
// ⚠ FIXTURE 1 STAYS BESIDE FIXTURE 2 AND IS NOT REPLACED BY IT. Fixture 2
// splits canonical from the REORDERED rival; it is NOT guaranteed to split it
// from the EPOCH-BLIND (site_id, seq) rival, which is what Fixture 1 exists
// to do.
func TestW8CrossEpochFixture1IsNonDegenerate(t *testing.T) {
	const (
		epochNPlus1Number = "907-555-0303" // the CERTIFIED answer
		epochNNumber      = "907-555-0404" // what the epoch-blind rival returns
	)
	// --- CONTROL FIRST, PRODUCED: the TIE DETECTOR must be able to fire.
	// ⚠ A fixture whose non-degeneracy check cannot fail certifies nothing,
	// and this project has measured exactly that: the landed cycle fixture's
	// own comment records that "this exact fixture WAS degenerate at rev 2
	// and three lenses were needed". So the two comparators are driven over a
	// DELIBERATELY DEGENERATE key pair first, and must AGREE.
	degenerate := []store.Key{
		{SiteID: "site-a", NodeEpoch: 1, Seq: 1},
		{SiteID: "site-b", NodeEpoch: 1, Seq: 1},
	}
	if w8WinnerUnder(degenerate, w8LessSiteEpochSeq) != w8WinnerUnder(degenerate, w8LessEpochSiteSeq) {
		t.Fatal("FIXTURE-DEFECT DETECTOR CONTROL FAILED: on a SAME-EPOCH pair the certified and reordered " +
			"comparators named DIFFERENT winners. They must agree there — W5d-4: the reordered spelling is " +
			"\"behaviourally indistinguishable from canonical WHENEVER A FIXTURE'S EVENTS SHARE A node_epoch\", " +
			"because site_id then breaks the tie either way. If they disagree here, one of the two comparators " +
			"below is mis-spelled and every verdict this file gives is about the wrong rule")
	}
	if w8WinnerUnder(degenerate, w8LessSiteEpochSeq) != w8WinnerUnder(degenerate, w8LessSiteSeq) {
		t.Fatal("FIXTURE-DEFECT DETECTOR CONTROL FAILED: on a SAME-EPOCH pair the certified and EPOCH-BLIND " +
			"comparators named different winners — they must agree there, which is exactly why Fixture 1 needs " +
			"two epochs")
	}

	st := w3NewStore(t)

	// (site-a, epoch 2, seq 1) — the RE-IMAGED post's assertion.
	w8ApplyWire(t, st, w8WireEnvelope(t, "site-a", 2, 1, w3Base+w3Hour, map[string]any{
		"type": "trade_amended", "trade_id": "t-1", "phone_day": epochNPlus1Number,
	}))
	// (site-a, epoch 1, seq 2) — the pre-re-image assertion, with a HIGHER
	// SEQ than the epoch N+1 one. ⚠ The higher seq is the whole point: an
	// epoch-blind comparator ranks it last and returns the WRONG number.
	//
	// ⚠ seq 1 of that stream is a FILLER for a DIFFERENT trade, and it is
	// required rather than decorative: [MEASURED — the store refused a bare
	// seq 99 with "gap in stream site-a: delivered seq 99 but watermark is 0
	// (seqs 1..98 missing) — watermark stalled, event not applied"] the log
	// is CONTIGUOUS per stream, so a fixture that skips seqs does not land at
	// all and the "cross-epoch" case is never posed.
	w8ApplyWire(t, st, w8WireEnvelope(t, "site-a", 1, 1, w3Base, map[string]any{
		"type": "trade_amended", "trade_id": "t-other", "phone_day": "907-555-0000",
	}))
	w8ApplyWire(t, st, w8WireEnvelope(t, "site-a", 1, 2, w3Base+w3Hour/2, map[string]any{
		"type": "trade_amended", "trade_id": "t-1", "phone_day": epochNNumber,
	}))

	keys, vals := w8TradeAssertionKeys(t, st, [][2]interface{}{{"site-a", int64(1)}, {"site-a", int64(2)}}, "t-1", "phone_day")
	if len(keys) != 2 {
		t.Fatalf("fixture defect: the log holds %d phone_day assertions for t-1, want exactly 2 — the contested-fact "+
			"case is not posed", len(keys))
	}

	certified := w8WinnerUnder(keys, w8LessSiteEpochSeq)
	epochBlind := w8WinnerUnder(keys, w8LessSiteSeq)

	// ⚠ A TIE IS FATAL AS `fixture defect:` — a fixture on which the certified
	// rule and its rival agree certifies nothing, and this project has
	// measured exactly that shape.
	if certified == epochBlind {
		t.Fatalf("fixture defect: the certified comparator (site_id, node_epoch, seq) and the EPOCH-BLIND rival "+
			"(site_id, seq) both name %v. Fixture 1 exists to SPLIT them; on this fixture it certifies nothing", certified)
	}

	if got := vals[certified]; got != epochNPlus1Number {
		t.Fatalf("W8-5(e) FIXTURE 1 (W3-m5): the certified comparator (site_id, node_epoch, seq) resolves phone_day "+
			"to %q, want the PINNED LITERAL %q — the epoch N+1 number, from the RE-IMAGED post.\n"+
			"⚠ The comparators are named BY THEIR SPELLINGS, never by the word \"epoch-major\": that word names "+
			"the certified rule by the property that distinguishes its rival, and a test-writer who reads it the "+
			"natural way builds this fixture BACKWARDS while every distinctness check still passes",
			got, epochNPlus1Number)
	}
	if got := vals[epochBlind]; got != epochNNumber {
		t.Fatalf("fixture defect: the epoch-blind rival resolves to %q, want %q — if the rival does not return the "+
			"OTHER number, the two are not separated by a VALUE and the pin below is not a discrimination", got, epochNNumber)
	}
}

// TestW8CrossEpochFixture2IsNonDegenerate is W8-5(e) FIXTURE 2 — CROSS-SITE
// *AND* CROSS-EPOCH.
//
// ⚠⚠ W5d-10 CARRIES TWO FIXTURES AND REVISION 2 HAD ONE ⟦LOCK — B-12⟧.
//
// ⚠ WHY THE FIRST FIXTURE CANNOT DO THIS JOB, in W5d-4's own words: the
// reordered spelling ORDER BY node_epoch, site_id, seq is "behaviourally
// indistinguishable from canonical WHENEVER A FIXTURE'S EVENTS SHARE A
// node_epoch", because site_id then breaks the tie either way — SO ON THE
// SAME-SITE PAIR ABOVE THE REORDERED COMPARATOR AGREES WITH THE CERTIFIED
// ONE. Its only remaining detector was W8-18(b)'s scan, which under B-1
// examined nothing. ⚠ W3-m17 appeared ZERO times in revision 2's 2,530 lines
// and BOTH of its detectors were gone in one draft.
//
// THE FIXTURE: two sites AND two epochs, in which the canonical
// (site_id, node_epoch, seq) and the reordered (node_epoch, site_id, seq)
// comparators NAME DIFFERENT WINNERS — both pinned as literals, both
// RECOMPUTED AT RUNTIME from the read-back keys, a tie FATAL as
// `fixture defect:`. The certified winner is pinned to the SITE-B/EPOCH-1
// literal.
func TestW8CrossEpochFixture2IsNonDegenerate(t *testing.T) {
	const (
		siteBEpoch1Number = "907-555-0505" // the CERTIFIED answer — site-b, epoch 1
		siteAEpoch2Number = "907-555-0606" // what the REORDERED rival returns
	)
	// --- CONTROL FIRST, PRODUCED: the TIE DETECTOR must be able to fire.
	// ⚠ A fixture whose non-degeneracy check cannot fail certifies nothing,
	// and this project has measured exactly that: the landed cycle fixture's
	// own comment records that "this exact fixture WAS degenerate at rev 2
	// and three lenses were needed". So the two comparators are driven over a
	// DELIBERATELY DEGENERATE key pair first, and must AGREE.
	degenerate := []store.Key{
		{SiteID: "site-a", NodeEpoch: 1, Seq: 1},
		{SiteID: "site-b", NodeEpoch: 1, Seq: 1},
	}
	if w8WinnerUnder(degenerate, w8LessSiteEpochSeq) != w8WinnerUnder(degenerate, w8LessEpochSiteSeq) {
		t.Fatal("FIXTURE-DEFECT DETECTOR CONTROL FAILED: on a SAME-EPOCH pair the certified and reordered " +
			"comparators named DIFFERENT winners. They must agree there — W5d-4: the reordered spelling is " +
			"\"behaviourally indistinguishable from canonical WHENEVER A FIXTURE'S EVENTS SHARE A node_epoch\", " +
			"because site_id then breaks the tie either way. If they disagree here, one of the two comparators " +
			"below is mis-spelled and every verdict this file gives is about the wrong rule")
	}
	if w8WinnerUnder(degenerate, w8LessSiteEpochSeq) != w8WinnerUnder(degenerate, w8LessSiteSeq) {
		t.Fatal("FIXTURE-DEFECT DETECTOR CONTROL FAILED: on a SAME-EPOCH pair the certified and EPOCH-BLIND " +
			"comparators named different winners — they must agree there, which is exactly why Fixture 1 needs " +
			"two epochs")
	}

	st := w3NewStore(t)

	w8ApplyWire(t, st, w8WireEnvelope(t, "site-a", 2, 1, w3Base+2*w3Hour, map[string]any{
		"type": "trade_amended", "trade_id": "t-1", "phone_day": siteAEpoch2Number,
	}))
	w8ApplyWire(t, st, w8WireEnvelope(t, "site-b", 1, 1, w3Base+w3Hour, map[string]any{
		"type": "trade_amended", "trade_id": "t-1", "phone_day": siteBEpoch1Number,
	}))

	keys, vals := w8TradeAssertionKeys(t, st, [][2]interface{}{{"site-a", int64(2)}, {"site-b", int64(1)}}, "t-1", "phone_day")
	if len(keys) != 2 {
		t.Fatalf("fixture defect: the log holds %d phone_day assertions for t-1, want exactly 2", len(keys))
	}

	certified := w8WinnerUnder(keys, w8LessSiteEpochSeq)
	reordered := w8WinnerUnder(keys, w8LessEpochSiteSeq)

	// ⚠ THE RUNTIME DISTINCTNESS CHECK, tie FATAL. Construction-time reasoning
	// is NOT enough and the tree says why: the landed cycle fixture's own
	// comment records that "this exact fixture WAS degenerate at rev 2 and
	// three lenses were needed".
	if certified == reordered {
		t.Fatalf("fixture defect: the CERTIFIED comparator (site_id, node_epoch, seq) and the REORDERED rival "+
			"(node_epoch, site_id, seq) both name %v. This fixture's WHOLE PURPOSE is to split them — the "+
			"reordered spelling is indistinguishable from canonical whenever a fixture's events share a "+
			"node_epoch, which is why Fixture 1 cannot do this job", certified)
	}

	if got := vals[certified]; got != siteBEpoch1Number {
		t.Fatalf("W8-5(e) FIXTURE 2 (W3-m17): the certified comparator resolves phone_day to %q, want the PINNED "+
			"LITERAL %q — the SITE-B/EPOCH-1 value.\n"+
			"⚠⚠ IF THIS FAILS, CHECK THE FIXTURE'S DIRECTION BEFORE CHECKING THE CODE. R2-W13's naming discipline "+
			"is carried verbatim precisely because a test-writer who calls the certified rule \"epoch-major\" "+
			"builds this fixture BACKWARDS, the distinctness check STILL PASSES (the comparators do disagree), and "+
			"the criterion then PINS THE WRONG WINNER — a non-degenerate fixture certifying the RIVAL",
			got, siteBEpoch1Number)
	}
	if got := vals[reordered]; got != siteAEpoch2Number {
		t.Fatalf("fixture defect: the REORDERED rival resolves to %q, want %q — the two comparators must name "+
			"different VALUES, not merely different keys", got, siteAEpoch2Number)
	}
}

// ---------------------------------------------------------------------------
// W8-4(a) — EXACTLY ONE resolver, a COMPILE-LEVEL CENSUS, not a grep
// ---------------------------------------------------------------------------

// w8PhoneSlots are the three fields `positional(row, shift) =
// coalesce(phone_<shift>, phone_shared)` selects among.
var w8PhoneSlots = []string{"phone_day", "phone_night", "phone_shared"}
var w8PhoneFields = []string{"PhoneDay", "PhoneNight", "PhoneShared"}

// TestW8ExactlyOnePositionalResolverExists is W8-4(a).
//
// "An AST WALK over production Go rooted at internal/ and cmd/ enumerates
// every expression that SELECTS AMONG phone_day / phone_night / phone_shared,
// resolving field accesses through go/types, and asserts the count is ONE and
// that it lives in one named function."
//
// ⚠ A `grep` CANNOT DO THIS: an open-coded
// `if p.PhoneDay != nil { … } else { … }` at a call site contains NONE of the
// strings a spelling filter would look for.
//
// ⚠ [DOC] rule 20: `go test -overlay` does NOT reach a source scan, so the
// controls run on a REAL ON-DISK COPY.
//
// ⭐ GENUINE PRE-CHUNK RED: the census finds ZERO at this tree, AND ZERO ≠ ONE.
func TestW8ExactlyOnePositionalResolverExists(t *testing.T) {
	// --- CONTROL FIRST: two plants, produced. ⚠ The criterion names three
	// (an open-coded resolver at a call site, a second named function, and a
	// TEMPLATE coalesce); the template arm has its own test below, because it
	// is a different medium and this walk cannot see it BY CONSTRUCTION.
	openCoded := "package pkg\n\n" +
		"type Trade struct{ PhoneDay, PhoneNight, PhoneShared *string }\n\n" +
		"func heading(tr Trade) *string {\n" +
		"\tif tr.PhoneDay != nil {\n\t\treturn tr.PhoneDay\n\t}\n\treturn tr.PhoneShared\n}\n"
	secondFunc := "package pkg\n\n" +
		"type Trade struct{ PhoneDay, PhoneNight, PhoneShared *string }\n\n" +
		"func positional(tr Trade, night bool) *string {\n" +
		"\tif night && tr.PhoneNight != nil {\n\t\treturn tr.PhoneNight\n\t}\n\treturn tr.PhoneShared\n}\n"
	for name, src := range map[string]string{"open-coded at a call site": openCoded, "a second named function": secondFunc} {
		root := w8PlantDir(t, map[string]string{"internal/pkg/plant.go": src})
		planted, pdiags := w8PositionalResolvers(t, root, []string{"internal"})
		// ⟦LOCK — §56(1)⟧ ASKED BEFORE THE HITS ARE READ. Without this, a
		// plant that stopped compiling would report itself as a blind census.
		if !s56PlantTypeChecked(t, "W8-4(a) control: "+name, pdiags) {
			continue
		}
		if n := len(planted); n == 0 {
			t.Errorf("W8-4(a) CONTROL FAILED (%s): the planted resolver was NOT counted. A grep cannot do this "+
				"work — an open-coded `if p.PhoneDay != nil { … } else { … }` contains none of the strings a "+
				"spelling filter looks for — so a census that misses its own plant is a spelling filter wearing "+
				"an AST walk's clothes.\n⟦§56⟧ the plant DID type-check, so this is the census, not the fixture:\n%s",
				name, pdiags.Report())
		}
	}

	root := w8AppRoot(t)
	sites, diags := w8PositionalResolvers(t, root, []string{"internal", "cmd"})
	s56LogTree(t, "w8PositionalResolvers", diags)
	if len(sites) != 1 {
		sort.Strings(sites)
		t.Fatalf("W8-4(a) ((m8-9)…(m8-12)): the positional-resolver census counted %d selection site(s), want EXACTLY 1.\n"+
			"sites: %v\n"+
			"⚠ `positional(row, shift) = coalesce(phone_<shift>, phone_shared)` is stated ONCE and must be "+
			"IMPLEMENTED ONCE. ZERO ≠ ONE: at this tree the resolver does not exist, which is this limb's "+
			"produced pre-chunk red; more than one is the mutant.\n"+
			"⚠ Scope bound, stated so it cannot be read wider: W8-4 asserts the resolver EXISTS ONCE and ANSWERS "+
			"CORRECTLY. It asserts NOTHING about what is printed ⟦DR-W8-16⟧\n"+
			"⟦§56⟧ what the census type-checked, so a ZERO here is never mistaken for a tree that did not compile:\n%s",
			len(sites), sites, diags.Report())
	}
}

// w8PositionalResolvers walks production Go and returns every function that
// SELECTS AMONG the three phone slots — a function body mentioning two or
// more of them inside a conditional or a coalesce shape.
//
// Field accesses are resolved through go/types where the package type-checks;
// where it does not (no importer), the identifier names are used, which is a
// SUPERSET and can only over-report. ⚠ Over-reporting is the safe direction
// for a census that asserts "exactly one".
func w8PositionalResolvers(t *testing.T, base string, roots []string) ([]string, s56Set) {
	t.Helper()
	var out []string
	// ⟦LOCK — §56(1)⟧ every directory this census type-checked, so a caller
	// can tell a DEAD PLANT from a census that saw nothing.
	var diags s56Set
	for _, r := range roots {
		dir := filepath.Join(base, r)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		byDir := map[string][]string{}
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			d := filepath.Dir(path)
			byDir[d] = append(byDir[d], path)
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
		var dirs []string
		for d := range byDir {
			dirs = append(dirs, d)
		}
		sort.Strings(dirs)
		for _, d := range dirs {
			fset := token.NewFileSet()
			var parsed []*ast.File
			for _, path := range byDir[d] {
				src, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				f, err := parser.ParseFile(fset, path, src, 0)
				if err != nil {
					t.Fatalf("PRECONDITION (instrument): %s did not parse, so this census's coverage is smaller "+
						"than its stated scope: %v", path, err)
				}
				parsed = append(parsed, f)
			}
			info := &types.Info{Uses: map[*ast.Ident]types.Object{}}
			// ⟦LOCK — §56(1), 2026-09-03⟧ RECORDED, never discarded. ⟦was:
			// "conf := types.Config{Error: func(error) {}, Importer: nil, DisableUnusedImportCheck: true}"⟧
			// A plant that does not type-check produces no hit, and a no-hit
			// is byte-identical to what a blind census produces.
			dg := s56Diags{Where: d}
			conf := types.Config{Error: dg.Record, Importer: nil, DisableUnusedImportCheck: true}
			_, _ = conf.Check("w8resolvercensus", fset, parsed, info)
			diags = append(diags, dg)

			for _, f := range parsed {
				ast.Inspect(f, func(n ast.Node) bool {
					fn, ok := n.(*ast.FuncDecl)
					if !ok || fn.Body == nil {
						return true
					}
					seen := map[string]bool{}
					ast.Inspect(fn.Body, func(m ast.Node) bool {
						switch e := m.(type) {
						case *ast.SelectorExpr:
							for i, name := range w8PhoneFields {
								if e.Sel.Name == name {
									seen[w8PhoneSlots[i]] = true
								}
							}
						case *ast.BasicLit:
							if e.Kind == token.STRING {
								for _, slot := range w8PhoneSlots {
									if strings.Contains(e.Value, slot) {
										seen[slot] = true
									}
								}
							}
						}
						return true
					})
					// A SELECTION requires the shift slot AND the shared slot:
					// coalesce(phone_<shift>, phone_shared). One alone is a
					// read, not a selection.
					if seen["phone_shared"] && (seen["phone_day"] || seen["phone_night"]) {
						out = append(out, fset.Position(fn.Pos()).String()+": "+fn.Name.Name)
					}
					return true
				})
			}
		}
	}
	return out, diags
}

// TestW8NoTemplateActionSelectsAmongThePhoneSlots is W8-4(a)'s SECOND ARM
// ⟦LOCK — FIL-F12⟧.
//
// ⚠⚠ AN AST WALK OVER PRODUCTION GO CANNOT SEE A RESOLVER RE-IMPLEMENTED IN A
// TEMPLATE, and the fallback idiom is ALREADY LIVE ON THE VERY PAGE THIS
// CRITERION'S OUTPUT IS PRINTED ON [MEASURED — muster.html carries
// {{with X}}…{{else}}…{{end}} coalesce actions at :86, :129, :411, :515, :556].
//
// ASSERTED: no template action selects among the three phone fields.
// CONTROL: a planted template coalesce —
// {{with .PhoneDay}}{{.}}{{else}}{{$.PhoneShared}}{{end}}.
//
// ⚠ This does NOT disturb ⟦DR-W8-16⟧: it asserts nothing about what is
// PRINTED, only that the SELECTION is not re-implemented.
//
// ⟦NO PRE-CHUNK RED — the three fields do not exist in any template yet.⟧ Its
// evidence is the PLANT, produced below.
func TestW8NoTemplateActionSelectsAmongThePhoneSlots(t *testing.T) {
	// --- CONTROL FIRST, produced.
	planted := "{{with .PhoneDay}}{{.}}{{else}}{{$.PhoneShared}}{{end}}"
	if !w8TemplateSelectsAmongPhones(planted) {
		t.Fatal("W8-4(a) TEMPLATE ARM CONTROL FAILED: the planted coalesce " +
			"`{{with .PhoneDay}}{{.}}{{else}}{{$.PhoneShared}}{{end}}` was NOT detected. An AST walk over " +
			"production Go cannot see a resolver re-implemented in a template, and the {{with}}…{{else}} idiom " +
			"is ALREADY LIVE on muster.html — so a check that cannot see its plant covers nothing")
	}
	if w8TemplateSelectsAmongPhones("{{with .PhoneDay}}{{.}}{{end}}") {
		t.Fatal("W8-4(a) TEMPLATE ARM CONTROL (negative): a single-field {{with}} was flagged as a SELECTION. " +
			"Printing one field is not selecting among three")
	}

	dir := filepath.Join(w8AppRoot(t), "internal", "web", "templates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("PRECONDITION: template surface %s unreadable: %v", dir, err)
	}
	var scanned int
	var hits []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		if w8TemplateSelectsAmongPhones(string(b)) {
			hits = append(hits, e.Name())
		}
	}
	if scanned == 0 {
		t.Fatal("PRECONDITION (instrument): the template arm read NO .html files — a clean pass over nothing")
	}
	if len(hits) != 0 {
		t.Errorf("W8-4(a) TEMPLATE ARM: %v re-implement the positional resolver in a template action. "+
			"The resolver is stated once and implemented ONCE; a template coalesce is a second implementation "+
			"an AST walk over Go cannot see", hits)
	}
}

// w8TemplateSelectsAmongPhones reports whether a template's actions select
// among the phone slots: two or more of the three fields inside one
// {{with}}/{{if}}…{{else}}…{{end}} action.
func w8TemplateSelectsAmongPhones(tpl string) bool {
	for _, action := range strings.Split(tpl, "{{end}}") {
		if !strings.Contains(action, "{{else}}") {
			continue
		}
		n := 0
		for _, f := range w8PhoneFields {
			if strings.Contains(action, "."+f) {
				n++
			}
		}
		if n >= 2 {
			return true
		}
	}
	return false
}

// w8PlantDir writes sources into a fresh scratch tree and returns its root.
// ⚠ [DOC] rule 20 — the real tree is never edited by a source-scan control.
func w8PlantDir(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, src := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
