// w6_print_render_test.go — W6-2, W6-3 and W6-9(a)'s behavioural half,
// asserted ON THE RENDERED ELEMENTS THAT CARRY THE NAME (W6-X2).
//
// ⚠ `strings.Contains` OVER THE RESPONSE BODY IS FORBIDDEN IN EVERY W6
// CRITERION, and the reason is measured: the same page renders LIVE
// Person.Name in its exception sections (muster.html:415, :551, :586), so a
// body-contains assertion for the amended name is satisfied by the page that
// has the bug. W6's equivalent of `value="…"` is the element that carries the
// datum — the <li> items of <ul class="names"> inside
// section[data-muster-section="on-site"] (muster.html:317) and the summary
// table's first cell (:333) — extracted and compared as ORDERED LITERALS.
//
// ⭐ These are GENUINE PRODUCED PRE-CHUNK REDS. They reference no unbuilt Go
// symbol, so they compile and run against f33c4c7 and fail there as runtime
// failures: MusterReprint receives LIVE persons (web/muster.go:478) and
// indexes p.Name (muster.go:204-210), while applyAmendmentRow sets p.Name
// from the `name` fact (person_amend.go:376). A printed sheet renders the
// person's CURRENT name today.
//
// ⚠ SCOPE, from the criteria and not from a fixture's accident: the frozen
// half is asserted FOR A VISIT CARRYING A NON-EMPTY person_snapshot_name.
// For a visit whose snapshot is absent or empty, adjudication #7's ruled
// middle arm renders the LIVE name and the printed sheet DOES follow the
// amendment — deliberately, so an unnameable person is never dropped or
// blanked. That class is W6-5's frozen row (i), not a hole here.
package web

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// --- extraction: the CARRYING ELEMENTS, never the body ------------------

var (
	w6OnSiteSectionRe = regexp.MustCompile(`(?s)<section class="roster" data-muster-section="on-site">(.*?)</section>`)
	w6SummaryBlockRe  = regexp.MustCompile(`(?s)<div class="summary" data-muster-section="folded">(.*?)</div>\s*</div>`)
	w6NamesUlRe       = regexp.MustCompile(`(?s)<ul class="names">(.*?)</ul>`)
	w6LiRe            = regexp.MustCompile(`(?s)<li>(.*?)</li>`)
	w6SummaryRowRe    = regexp.MustCompile(`(?s)<tr><td>(.*?)</td><td>`)
	w6CompanyGroupRe  = regexp.MustCompile(`data-company-group="([^"]*)"`)
	// ⟦LOCK — §94 disposition (3) / §105, 2026-09-03⟧ STALE BASE / LIVE ASSERTION: the BASE
	// was re-pointed, the assertion KEPT. ⚠ muster.html:315 renders `data-trade-group`
	// (renamed in 19eafc4); this scanner was not re-pointed, matched nothing, and returned [].
	// ⛔ IT FEEDS A FIXTURE GUARD at :352, so TestW6PrintedLineOrderIsInvariantUnderANameAmendment
	// died there reporting 0 trade groups where it wanted 1, and its REAL
	// subject had never run at this base. ⚠ The message names the FIXTURE, and the fixture was
	// FINE — a precondition message naming a subject is a HYPOTHESIS, not a diagnosis (§105).
	// ⚠ The superseded line is preserved VERBATIM in reports/window-build/S100-ALIAS.md §2.3,
	// NOT here: it carries a W8-6 census term and this file is not allowlisted.
	w6TradeRe = regexp.MustCompile(`data-trade-group="([^"]*)"`)
)

// w6RosterNames extracts the ordered printed names of the ON SITE roster
// body and the pooled Summary — and NOTHING else on the page.
//
// ⚠ The `<ul class="names">` selector alone is NOT enough: the in-transit
// section at muster.html:415 uses the same class over LIVE roster names.
// Scoping to section[data-muster-section="on-site"] is what keeps this
// assertion about the region W6 changes.
func w6RosterNames(t *testing.T, body string) []string {
	t.Helper()
	var out []string
	if m := w6OnSiteSectionRe.FindStringSubmatch(body); m != nil {
		for _, ul := range w6NamesUlRe.FindAllStringSubmatch(m[1], -1) {
			for _, li := range w6LiRe.FindAllStringSubmatch(ul[1], -1) {
				out = append(out, w6Unescape(strings.TrimSpace(li[1])))
			}
		}
	}
	if m := w6SummaryBlockRe.FindStringSubmatch(body); m != nil {
		for _, row := range w6SummaryRowRe.FindAllStringSubmatch(m[1], -1) {
			out = append(out, w6Unescape(strings.TrimSpace(row[1])))
		}
	}
	return out
}

func w6Unescape(s string) string {
	r := strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&#34;", `"`, "&#39;", "'", "&#160;", " ")
	return r.Replace(s)
}

func w6EqualNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func w6Q(names []string) string { return fmt.Sprintf("%q", names) }

// --- the fixture --------------------------------------------------------

type w6Fixture struct {
	srv        *Server
	st         *store.Store
	ids        []string
	asRecorded []string // pinned, in PRINT order
	amended    []string
}

// w6BuildOrderFixture builds W6-3's fixture and asserts EVERY ONE of its
// four locked non-degeneracy conditions at runtime.
func w6BuildOrderFixture(t *testing.T, asRecorded, amended []string) w6Fixture {
	t.Helper()
	srv, st := newMusterTestServer(t, 0) // minCrewSize 0: no pooling, one bucket
	co, err := domain.CreateCompany(st, "site-a", 1, "Northwind Drilling", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "wg, err := domain.CreateTrade(st, "site-a", 1, co.CompanyID, "Rig Crew", 0, "default")"⟧
	wg, err := domain.CreateTrade(st, "site-a", 1, "Rig Crew", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, n := range asRecorded {
		p, err := domain.CreatePerson(st, "site-a", 1, n, &co.CompanyID, &wg.TradeID, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.PersonID)
	}
	// ONE check-in, so all three share ONE company/workgroup bucket
	// (condition (i)): PersonName is a sort key only WITHIN a bucket
	// (muster.go:254) or within Summary (:277). Three people in three
	// companies satisfy the "different orders" clause and their printed order
	// is ALREADY amendment-invariant — the natural fixture is exactly that
	// shape, and it would make W6-3's claimed pre-chunk red not produced.
	checkIn(t, st, "site-a", ids[0], ids[1:])
	return w6Fixture{srv: srv, st: st, ids: ids, asRecorded: asRecorded, amended: amended}
}

// w6AssertFixtureConditions checks W6-3's four locked clauses.
func w6AssertFixtureConditions(t *testing.T, fx w6Fixture) {
	t.Helper()
	// (ii) no two people share a PersonName, in EITHER set. muster.go's four
	// sorts are sort.Slice, NOT SliceStable, and D-36's PersonID tiebreak is
	// UNBUILT (DR-W6-2 declines it) — so a pinned ORDERED literal over a
	// comparator that is not a total order is under-determined.
	for _, set := range [][]string{fx.asRecorded, fx.amended} {
		seen := map[string]bool{}
		for _, n := range set {
			if seen[n] {
				t.Fatalf("FIXTURE DEFECT (W6-3 clause ii): %q appears twice in %v. The sorts are sort.Slice, not "+
					"SliceStable, and D-36's PersonID tiebreak is UNBUILT, so a pinned ordered literal over a "+
					"duplicate name is under-determined", n, set)
			}
			seen[n] = true
		}
	}
	// (iii) at least two names whose relative order DIFFERS under case
	// folding — DR-W6-2's tripwire, which is UNFOUNDED without this clause.
	found := ""
	for i := range fx.asRecorded {
		for j := range fx.asRecorded {
			if i == j {
				continue
			}
			a, b := fx.asRecorded[i], fx.asRecorded[j]
			if (a < b) != (strings.ToLower(a) < strings.ToLower(b)) {
				found = fmt.Sprintf("%q / %q", a, b)
			}
		}
	}
	if found == "" {
		t.Fatalf("FIXTURE DEFECT (W6-3 clause iii): no two of %v have a relative order that DIFFERS under case "+
			"folding, so the pinned literal is no more a [[D-36]] tripwire than W5a-4 already is [MEASURED: "+
			"W5a-4's own fixture is \"Aaron, Zed\"/\"Zeta, Ann\", a pair whose order is IDENTICAL under folding]",
			fx.asRecorded)
	}
	t.Logf("W6-3 clause (iii) satisfied by %s — byte order and case-folded order disagree, so a later D-36 fold "+
		"reaching muster.go:254/:277 lands as a LOUD RED on this locked criterion rather than as a silent "+
		"re-ordering of a life-safety document", found)
}

// w6SnapshotState reports how many of this site's open visits carry a
// non-empty snapshot (W6-3 clause (iv), W6-2's scope).
func w6SnapshotState(t *testing.T, st *store.Store) (carrying, total int) {
	t.Helper()
	vs, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		total++
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		if s, ok := m["person_snapshot_name"].(string); ok && s != "" {
			carrying++
		}
	}
	return carrying, total
}

// w6RequireNonEmptySnapshots is clause (iv), stated so it cannot be mistaken
// for a fixture failure BEFORE the build and cannot be skipped AFTER it.
func w6RequireNonEmptySnapshots(t *testing.T, st *store.Store) {
	t.Helper()
	carrying, total := w6SnapshotState(t, st)
	switch {
	case total == 0:
		t.Fatalf("FIXTURE DEFECT: the check-in produced no open visits at site-a")
	case carrying == 0:
		t.Logf("PRE-CHUNK STATE: none of the %d open visits carries a non-empty person_snapshot_name, because "+
			"the column does not exist at f33c4c7. The assertions below therefore red as W6's PRODUCED pre-chunk "+
			"reds. After the build this is (m6-1b) — the populate dropped.", total)
	case carrying != total:
		t.Fatalf("FIXTURE DEFECT (W6-3 clause iv / W6-2 scope): %d of %d open visits carry a non-empty snapshot. "+
			"The frozen invariance claims are scoped to visits carrying a NON-EMPTY snapshot; a mixed fixture "+
			"silently tests both arms at once and can no longer say which one answered", carrying, total)
	}
}

// w6PrintPages returns the two frozen surfaces and the live one. BOTH print
// routes are exercised: they route through the SAME MusterReprint call
// (web/muster.go:486), and a fixture that exercises only one of them tests
// one line of code twice.
func w6PrintPages(t *testing.T, srv *Server) (snapshot, reprint, live string) {
	t.Helper()
	code, snapshot := getMuster(t, srv, "/muster/print")
	if code != 200 {
		t.Fatalf("GET /muster/print = %d", code)
	}
	m := regexp.MustCompile(`href="(/muster/print\?visits=[^"]+)"`).FindStringSubmatch(snapshot)
	if m == nil {
		t.Fatalf("FIXTURE DEFECT: the snapshot sheet minted no ?visits= reprint link, so the reprint route " +
			"cannot be exercised. ⚠ W6-2, W6-3 and W6-9 are asserted THROUGH the reprint route deliberately " +
			"(DR-W6-1/SEAM-1), so a commit that removes it cannot go green silently")
	}
	href := strings.ReplaceAll(m[1], "&amp;", "&")
	code, reprint = getMuster(t, srv, href)
	if code != 200 {
		t.Fatalf("GET %s = %d", href, code)
	}
	code, live = getMuster(t, srv, "/muster")
	if code != 200 {
		t.Fatalf("GET /muster = %d", code)
	}
	return snapshot, reprint, live
}

// --- W6-2 / W6-9(a) behavioural: the un-freezing regression -------------
//
// EXPECTED RED at f33c4c7 (runtime, produced): both printed surfaces render
// the AMENDED name where the certified answer is the AS-RECORDED one.
// Kills (m6-1), (m6-1b).
func TestW6PrintedSheetRendersTheAsRecordedNameAfterAnAmendment(t *testing.T) {
	asRecorded := []string{"aaron, Zed", "Brown, Ann", "Carter, Mo"}
	amended := []string{"Adams, Bea", "Yates, Cal", "Novak, Sam"}
	fx := w6BuildOrderFixture(t, asRecorded, amended)
	w6AssertFixtureConditions(t, fx)

	for i, id := range fx.ids {
		if _, err := domain.AmendPerson(fx.st, "site-a", 1, domain.PersonAmendment{
			PersonID: id, Name: &amended[i],
		}, 0, "default"); err != nil {
			t.Fatalf("fixture amendment refused: %v", err)
		}
	}
	w6RequireNonEmptySnapshots(t, fx.st)

	// The CERTIFIED printed order: the as-recorded names, byte-sorted.
	// "Brown, Ann" < "Carter, Mo" < "aaron, Zed" (B=0x42, C=0x43, a=0x61).
	wantFrozen := []string{"Brown, Ann", "Carter, Mo", "aaron, Zed"}
	// The LIVE board's order after the amendment, pinned separately.
	wantLive := []string{"Adams, Bea", "Novak, Sam", "Yates, Cal"}

	snapshot, reprint, live := w6PrintPages(t, fx.srv)

	// --- rule 6: the LIVE half must actually have moved, or the frozen
	// assertion below is vacuous. This is also the plan's own W6-2 live half
	// ("live surfaces show the amended name") and it is ALREADY TRUE at
	// f33c4c7 — its clean pass is not evidence, it is what makes the frozen
	// red meaningful.
	if got := w6RosterNames(t, live); !w6EqualNames(got, wantLive) {
		t.Fatalf("PRECONDITION (W6-2's live half): /muster rendered %s, want %s. The amendment did not reach the "+
			"live board, so the frozen assertions below would pass for the wrong reason", w6Q(got), w6Q(wantLive))
	}

	for _, page := range []struct{ route, body string }{
		{"/muster/print (snapshot)", snapshot},
		{"/muster/print?visits=… (reprint)", reprint},
	} {
		got := w6RosterNames(t, page.body)
		if !w6EqualNames(got, wantFrozen) {
			hint := ""
			if w6EqualNames(got, wantLive) {
				hint = " — that is the LIVE roster: the printed sheet followed the amendment. (m6-1), the plan's " +
					"own named mutant: the frozen closure's snapshot arm dropped, so the printed read falls " +
					"through to the live name"
			}
			t.Errorf("W6-2 (m6-1, m6-1b), %s: the printed roster is %s, want the AS-RECORDED %s%s.\n"+
				"  A person's name is frozen onto their visit so a historical sheet stays true when that name "+
				"later changes. The reader of that sheet is a site control officer calling a roll at a camp gate, "+
				"and in an evacuation it is what a rescue crew reads.\n"+
				"  ⚠ Asserted on the <li> items of the ON SITE roster, never by strings.Contains over the body: "+
				"the amended name legitimately appears elsewhere on this same page (muster.html:415, :551, :586 "+
				"render live roster names), so a body-contains assertion is satisfied by the page that has the bug.",
				page.route, w6Q(got), w6Q(wantFrozen), hint)
		}
	}
}

// --- W6-3: the printed line ORDER is invariant under a name amendment ---
//
// ⚠ THIS IS NOT W6-2 OBSERVED TWICE. muster.go:254 and :277 sort on
// PersonName — the same string W6 replaces — so an amendment today changes
// BOTH what the sheet says and THE ORDER IT SAYS IT IN, and the second is
// invisible to a fixture that checks membership. Both fall to (m6-1);
// neither falls to the other's fixture.
//
// Kills (m6-1), (m6-3b) — W6-3's OWN mutant: the render reads the snapshot
// while the comparator keeps reading the live name, which is exactly what
// adjudication #7's third bullet forbids ("the sorts … then key on the
// resolver's output").
//
// ⚠ THE TRIPWIRE AND ITS DISARM INSTRUCTION, together, where the person who
// meets the red will be: IF THIS CRITERION REDS BECAUSE A COMPARATOR CHANGED
// RATHER THAN BECAUSE A NAME WAS AMENDED, IT IS NOT FALSIFIED — re-pin the
// literal and record the re-pin in the ledger, citing this line. Any other
// red is a failure.
//
// EXPECTED RED at f33c4c7 (runtime, produced): the printed order follows the
// amendment.
func TestW6PrintedLineOrderIsInvariantUnderANameAmendment(t *testing.T) {
	asRecorded := []string{"aaron, Zed", "Brown, Ann", "Carter, Mo"}
	amended := []string{"Adams, Bea", "Yates, Cal", "Novak, Sam"}
	fx := w6BuildOrderFixture(t, asRecorded, amended)
	w6AssertFixtureConditions(t, fx)

	// The order BEFORE the amendment, taken from the engine itself.
	_, beforeReprint, _ := w6PrintPages(t, fx.srv)
	before := w6RosterNames(t, beforeReprint)
	wantFrozen := []string{"Brown, Ann", "Carter, Mo", "aaron, Zed"}
	if !w6EqualNames(before, wantFrozen) {
		t.Fatalf("PRECONDITION: the printed order BEFORE any amendment is %s, want %s — the pinned literal does "+
			"not describe this tree's comparator, and every judgement below would be about the wrong order",
			w6Q(before), w6Q(wantFrozen))
	}
	// Clause (i), measured on the rendered page: one company, one workgroup.
	cg := w6CompanyGroupRe.FindAllStringSubmatch(beforeReprint, -1)
	wgm := w6TradeRe.FindAllStringSubmatch(beforeReprint, -1)
	if len(cg) != 1 || len(wgm) != 1 {
		t.Fatalf("FIXTURE DEFECT (W6-3 clause i): the sheet renders %d company group(s) and %d workgroup group(s), "+
			"want 1 and 1. PersonName is a sort key only WITHIN a bucket (muster.go:254) or within Summary (:277); "+
			"three people in three companies are ALREADY amendment-invariant and W6-3's pre-chunk red is not produced",
			len(cg), len(wgm))
	}

	for i, id := range fx.ids {
		if _, err := domain.AmendPerson(fx.st, "site-a", 1, domain.PersonAmendment{
			PersonID: id, Name: &amended[i],
		}, 0, "default"); err != nil {
			t.Fatalf("fixture amendment refused: %v", err)
		}
	}
	w6RequireNonEmptySnapshots(t, fx.st)

	_, afterReprint, afterLive := w6PrintPages(t, fx.srv)

	// --- RUNTIME NON-DEGENERACY, recomputed THROUGH THE SAME ENGINE ------
	// Not as a sorted name list: the bucketing, the minCrewSize pooling and
	// the three outer sorts are all part of what produces the printed order,
	// so a sorted list would assert a property of sort.Slice, not of the board.
	rival := w6RosterNames(t, afterLive)
	if err := w6RivalOrderDiffers(wantFrozen, rival); err != nil {
		t.Fatalf("FIXTURE DEFECT (W6-3 non-degeneracy): %v", err)
	}

	got := w6RosterNames(t, afterReprint)
	if !w6EqualNames(got, wantFrozen) {
		hint := ""
		if w6EqualNames(got, rival) {
			hint = " — that is the LIVE order: (m6-1)"
		}
		t.Errorf("W6-3 (m6-1, m6-3b): the printed line ORDER after a name amendment is %s, want the frozen %s%s.\n"+
			"  For a fixed visit set in which every visit carries a non-empty snapshot, the ordered sequence of "+
			"printed names is unchanged by any amendment to the live roster's name for a person already on that "+
			"sheet. It claims NOTHING about invariance under a change to the comparator.\n"+
			"  ⚠ (m6-3b): if the NAMES are right and the ORDER is wrong, the render reads the snapshot while the "+
			"comparator keeps reading the live name — frozen names in live order, which adjudication #7's third "+
			"bullet forbids in terms.\n"+
			"  ⚠ DISARM: if this reds because a COMPARATOR changed (a later [[D-36]] case-fold reaching "+
			"muster.go:254/:277) rather than because a NAME was amended, it is NOT falsified — re-pin the literal "+
			"and record the re-pin in the ledger, citing this line. Any other red is a failure.",
			w6Q(got), w6Q(wantFrozen), hint)
	}
}

// w6RivalOrderDiffers is W6-3's non-degeneracy self-check, factored out so
// its own POSITIVE CONTROL can drive it (W5a-4's three-piece construction:
// the criterion, its runtime self-check, and the self-check's own control).
func w6RivalOrderDiffers(certified, rival []string) error {
	if w6EqualNames(certified, rival) {
		return fmt.Errorf("the rival (live-name) board, recomputed through the SAME engine, renders %s — "+
			"IDENTICAL to the certified frozen order. The amendment does not cross a sort boundary, so this "+
			"fixture passes under the frozen rule and under the live rule alike and certifies nothing", w6Q(rival))
	}
	return nil
}

// TestW6OrderFixtureSelfCheckHasAPositiveControl — the self-check's own
// control. A deliberately degenerate fixture (an amendment that does not move
// anyone) is run once and the FIXTURE DEFECT observed, before the real
// fixture's pass counts.
//
// EXPECTED RED at f33c4c7: NONE — this is a control, and it passes by
// OBSERVING the defect. It fails only if the self-check cannot see a
// degenerate fixture, which would make W6-3's non-degeneracy claim decorative.
func TestW6OrderFixtureSelfCheckHasAPositiveControl(t *testing.T) {
	asRecorded := []string{"aaron, Zed", "Brown, Ann", "Carter, Mo"}
	// A DEGENERATE amendment: every name changes, and NOBODY moves.
	degenerate := []string{"aaron, Zedd", "Brown, Anne", "Carter, Moe"}
	fx := w6BuildOrderFixture(t, asRecorded, degenerate)
	for i, id := range fx.ids {
		if _, err := domain.AmendPerson(fx.st, "site-a", 1, domain.PersonAmendment{
			PersonID: id, Name: &degenerate[i],
		}, 0, "default"); err != nil {
			t.Fatalf("fixture amendment refused: %v", err)
		}
	}
	_, _, live := w6PrintPages(t, fx.srv)
	rival := w6RosterNames(t, live)
	certified := []string{"Brown, Anne", "Carter, Moe", "aaron, Zedd"}
	if !w6EqualNames(rival, certified) {
		t.Fatalf("CONTROL SETUP: the degenerate fixture rendered %s, want %s — it was supposed to leave the order "+
			"unchanged, and if it did not this control is not posing its case", w6Q(rival), w6Q(certified))
	}
	if err := w6RivalOrderDiffers(certified, rival); err == nil {
		t.Fatal("W6-3 SELF-CHECK CONTROL FAILED: a deliberately DEGENERATE fixture — an amendment that does not " +
			"move anyone — was NOT reported as a fixture defect. W6-3's runtime non-degeneracy check cannot see " +
			"the case it exists to catch, so W6-3's ordered literal could pass under the frozen rule and under " +
			"the live rule alike.")
	}
}
