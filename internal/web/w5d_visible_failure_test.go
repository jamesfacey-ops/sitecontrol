// w5d_visible_failure_test.go — W5d-8 (W3-6 cloned verbatim against
// company: a losing-post un-merge does NOT take effect AND fails visibly to
// its author — VF §8 T9 both directions, T6b as the cry-wolf control, T10
// the standing-cost line on THIS surface) and W5d-9(a) (VF §5 rendering 2's
// AMENDMENT variant — the WINNING post's side, BUILT per [[D-53]]).
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, W5d LOCKED 2026-08-31.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS these tests' clean-tree red.
//
// KILLER MUTANTS: W3-m3 (the losing un-merge takes effect — the composed
// half), W3-m11 (silent-OK: an unconditional "Recorded." confirmation),
// W3-m12 (an unconditional supersession warning — the cry-wolf control),
// W3-m13 (a hardcoded or inverted standing-cost line), plus the
// amendment-side analogues of VF-m6 (one-sided visibility — only the loser
// is told) and VF-m7′ (the inverted renderer — the wording fires on the
// wrong side, or on no competitor at all), each produced red once.
//
// ⚠⚠ W5d-9's OBSERVABLE IS THE FIRST CLAUSE. [[D-53]] binding consequence 2:
// "The observable is part of the discharge, not just the means. … James
// chose FIX so that the winning officer is told; a discharge that delivers
// the machinery and not the telling delivers neither arm." Every listed
// discharge term is satisfiable below HTTP, so the assertions below are at
// HTTP level, against the RENDERED ELEMENT (W5-X2's extended form), never
// strings.Contains over the body.
//
// As deployed "site-b" > "site-a": Pad Site Control wins every contested
// fact and Ice Road is the losing post [DOC — VF §5/§10; DERIVED here from
// the fixture configs, never hardcoded].
//
// ⟦SchemaVersion 11, D-92 — W5d-8's LOSING fixture RE-RULED, not silently
// edited, exactly as W3-6 (b)'s was at step 2 of the increment⟧ The losing
// un-merge is now the GENUINELY CONCURRENT case (site_id decides only
// between authors who did not see each other); the construction this file
// used at 10 — peer's merge applied, then the POST — is an un-merge made
// IN KNOWLEDGE of the merge, and under D-92 it WINS. Both arms are
// witnessed below; the new mutant killed is m-D92a (a compose that reaches
// the site_id tiebreak on a causally ordered pair).
package web

import (
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// w5dDisplayNameOf derives a site's rendered name FROM THE CONFIG THE
// SERVER LOADS — "this post" when it is the server itself (L-1's same-post
// form), the destination display name for a peer, the bare id when config
// knows no better. Deliberately a test-side derivation, so the expectation
// is not computed by the production helper it is checking.
func w5dDisplayNameOf(cfg *config.Config, siteID string) string {
	if siteID == cfg.SiteID {
		return w5dRendering2SamePost
	}
	for _, d := range cfg.Destinations {
		if d.ID == siteID && d.DisplayName != "" {
			return d.DisplayName
		}
	}
	return siteID
}

// w5dAssertRendering2 asserts VF §5 rendering 2's amendment variant on the
// page the WINNING officer reads: a notice element saying, in words, that
// their amendment superseded a peer's, with the loser's post named in
// L-1's form. Asserted against the ELEMENT, with the wording pinned as a
// literal drawn from outside w3AdverseTokens.
// It returns the notice text it asserted against (lower-cased, tags
// stripped), so a caller can make a further ELEMENT-SCOPED assertion on the
// same sentence rather than a body-wide strings.Contains (W5-X2).
func w5dAssertRendering2(t *testing.T, page string, cfg *config.Config, loserSiteID string) string {
	t.Helper()
	want := strings.ToLower(w5dRendering2Stem + " " + w5dDisplayNameOf(cfg, loserSiteID))
	notice, ok := w5dNoticeCarrying(page, w5dRendering2Stem)
	if !ok {
		t.Fatalf("W5d-9(a) (VF-m6's amendment analogue — one-sided visibility): the WINNING officer's confirmation carries no notice element saying their amendment superseded a peer's. The pinned wording is %q; nobody at either post is told synchronously without it, and VF §6's case table accepts the LOSING post's passivity in case 2 precisely BECAUSE it assumes the winning post is surfaced. James chose FIX over ACCEPT to get exactly this ([[D-53]]). Notices rendered: %v", w5dRendering2Stem, w5dNoticeTexts(page))
	}
	if !strings.Contains(notice, want) {
		t.Fatalf("W5d-9(a) / L-1: the winning post's notice reads %q, want it to carry %q — the loser's post is named in L-1's form: the SAME-POST wording %q when winner and loser share a site_id, and the peer's config-DERIVED display name otherwise, never a hardcoded name", notice, want, w5dRendering2SamePost)
	}
	return notice
}

// --- W5d-8: T9, both directions -----------------------------------------

// TestW5dLosingCompanyUnMergeDoesNotTakeEffectAndFailsVisibly — W5d-8, T9
// direction (b): the merge is authored at site-b (the winning post) and
// arrives by wire; the un-merge is POSTed at site-a (the losing post). BOTH
// halves asserted: the composed state stays MERGED at the losing post, and
// the HTTP confirmation says in words that the merge stands, naming the
// winning post by its config-derived display name.
// ⟦SchemaVersion 11, D-92 — RE-RULED⟧ At 10 this fixture applied site-b's
// merge and then POSTed the un-merge; under fixed order the un-merge lost.
// Under D-92 that construction is an un-merge made IN KNOWLEDGE of the
// merge and it WINS — the reversed pin is
// TestW5dUnMergeMadeInKnowledgeOfThePeersMergeTakesEffect below. A LOSING
// un-merge at 11 is the genuinely concurrent case: both posts hold site-b's
// merge; site-a un-merges; cut off, site-b un-merges and re-merges the same
// pair; the two STANDING assertions — site-a's un-merge and site-b's
// re-merge — saw the same history and not each other, and the site_id
// tiebreak decides. The officer meets the loss on a re-GET of their own
// confirmation (read-time truth, VF §4.3 rule 2).
// ⟦was: site-b's merge applied, then the POST; the page read once⟧
func TestW5dLosingCompanyUnMergeDoesNotTakeEffectAndFailsVisibly(t *testing.T) {
	srv, st, x, y := w5dSiteAServer(t)
	roster := w3Stream(t, st, "site-a", 1)
	// Pad Site Control's merge X→Y arrives by wire; BOTH posts now hold it.
	merge := w5dRemoteAmend(t, roster, "site-b", w2Base+w2Hour,
		domain.CompanyAmendment{CompanyID: x.CompanyID, SameAs: &y.CompanyID})
	w3ApplyWire(t, st, []store.Envelope{merge})
	held := append(append([]store.Envelope{}, roster...), merge)

	form := url.Values{}
	form.Set(w5dFieldCompanyID, x.CompanyID)
	form.Set(w5dFieldClear, w5dFieldSameAs)
	rec, cookie := w5dPostAmend(t, srv, form)
	first, location := w3FollowRecorded(t, srv, rec, cookie)
	if tok, has := w3HasAdverse(first); has {
		t.Fatalf("fixture (VF §4.3 rule 2): before site-b's re-merge arrives the un-merge is Effective, yet its confirmation renders %q — a status stamped at write", tok)
	}

	// Cut off from site-a, Pad Site Control un-merges and re-merges the same
	// pair on its own edge. Its re-merge and site-a's un-merge are
	// concurrent; site-b's key wins the tie.
	chain := w5dRemoteAmendChain(t, held, "site-b", w2Base+2*w2Hour,
		domain.CompanyAmendment{CompanyID: x.CompanyID, Clear: []string{w5dFieldSameAs}},
		domain.CompanyAmendment{CompanyID: x.CompanyID, SameAs: &y.CompanyID})
	if len(chain) != 2 {
		t.Fatalf("fixture defect: site-b minted %d events, want 2 (un-merge, re-merge)", len(chain))
	}
	w3ApplyWire(t, st, chain)
	page := w2Get(t, srv, location, cookie).Body.String()

	// The un-merge itself is SUPERSEDED (per-fact, at read time).
	outs, err := domain.Outcome(st, w3RecordedKey(t, location))
	if err != nil {
		t.Fatalf("Outcome on the recorded company un-merge: %v", err)
	}
	sawSameAs := false
	for _, o := range outs {
		if o.Fact == w5dFieldSameAs {
			sawSameAs = true
			if o.Status != domain.Superseded {
				t.Fatalf("W5d-8 (W3-m3): the losing post's un-merge outcome is %v, want Superseded — site-b's CONCURRENT re-merge takes the site_id tiebreak (D-92: the one case fixed order still decides) ⟦was: \"site-b's merge is later in record order, permanently\"⟧", o.Status)
			}
		}
	}
	if !sawSameAs {
		t.Fatalf("W5d-8: Outcome on the un-merge reported no same_as fact (got %+v) — cannot-match is a failure, never a skip", outs)
	}

	// Half 1 — does NOT take effect (W3-m3): composed state still merged.
	c, err := domain.CompanyByID(st, x.CompanyID)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil || c.SameAs == nil || *c.SameAs != y.CompanyID {
		t.Fatalf("W5d-8 (W3-m3): the LOSING post's company un-merge took effect — composed same_as = %v, want the standing merge to %s (site-b's concurrent re-merge wins the tiebreak, D-92 ⟦was: \"later in record order, permanently\"⟧)", c, y.CompanyID)
	}
	m, err := domain.LoadCompanyMergeMap(st)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Resolve(x.CompanyID); got != y.CompanyID {
		t.Fatalf("W5d-8 (W3-m3): Resolve(%s) = %s after the losing un-merge, want the merged %s", x.CompanyID, got, y.CompanyID)
	}

	// Half 2 — fails VISIBLY to its author (W3-m11), asserted against the
	// rendered ELEMENT per W5-X2's extended form.
	notice, ok := w5dNoticeCarrying(page, w3RemainMergedPhrase)
	if !ok {
		t.Fatalf("W5d-8 (W3-m11, silent-OK): the losing company un-merge's confirmation has no notice element saying the merge stands (%q absent) — a correction that silently does nothing is the project's signature defect (D-28 Part B). Notices: %v", w3RemainMergedPhrase, w5dNoticeTexts(page))
	}
	winner := w3PrecedenceTaker(srv.Cfg) // "Pad Site Control", derived from config
	if winner == "this post" || winner == "" {
		t.Fatalf("fixture defect: site-a's config must derive a PEER as precedence-taker, got %q", winner)
	}
	if !strings.Contains(notice, strings.ToLower(winner)) {
		t.Fatalf("W5d-8: the adverse confirmation does not name the winning post %q (derived from the config, never hardcoded). Notice: %q", winner, notice)
	}
	// VF-m6's amendment analogue, inverted: the LOSING officer must NOT be
	// told they superseded anyone.
	if n, has := w5dNoticeCarrying(page, w5dRendering2Stem); has {
		t.Fatalf("W5d-9(a) (VF-m7′'s analogue — the INVERTED renderer): the LOSING post's confirmation carries the WINNING post's wording: %q", n)
	}
}

// TestW5dUnMergeMadeInKnowledgeOfThePeersMergeTakesEffect — D-92's reversal
// of W5d-8's fixture as it stood at 10: site-b's merge arrives, and site-a's
// officer, seeing it, un-merges. The un-merge WINS at the LESSER site_id:
// composed state un-merged, Resolve identity, Outcome Effective with the
// peer's merge Superseded BY the un-merge, and the confirmation is THIS
// surface's winning register — W5d-9(a)'s rendering 2 ([[D-53]]: the
// winning officer is told their un-merge took the place of the peer's
// merge, the peer named, the cross-post radio clause kept so the officer
// whose merge was superseded is told by somebody) with no adverse token.
// ⟦was: "site-b's merge is later in record order, permanently" — D-28 Part
// B, reversed by D-92⟧ Kills m-D92a on the company carrier.
func TestW5dUnMergeMadeInKnowledgeOfThePeersMergeTakesEffect(t *testing.T) {
	srv, st, x, y := w5dSiteAServer(t)
	merge := w5dRemoteAmend(t, w3Stream(t, st, "site-a", 1), "site-b", w2Base+3*w2Hour,
		domain.CompanyAmendment{CompanyID: x.CompanyID, SameAs: &y.CompanyID})
	w3ApplyWire(t, st, []store.Envelope{merge})

	form := url.Values{}
	form.Set(w5dFieldCompanyID, x.CompanyID)
	form.Set(w5dFieldClear, w5dFieldSameAs)
	rec, cookie := w5dPostAmend(t, srv, form)
	page, location := w3FollowRecorded(t, srv, rec, cookie)
	k := w3RecordedKey(t, location)
	if !k.Less(merge.Key()) {
		t.Fatalf("fixture defect: the un-merge %v is not the lesser key against the merge %v — fixed order would agree with D-92 here and the test would prove nothing", k, merge.Key())
	}

	outs, err := domain.Outcome(st, k)
	if err != nil {
		t.Fatalf("Outcome on the recorded un-merge: %v", err)
	}
	saw := false
	for _, o := range outs {
		if o.Fact == w5dFieldSameAs {
			saw = true
			if o.Status != domain.Effective {
				t.Fatalf("W5d-8 re-ruled (D-92; m-D92a): the un-merge made IN KNOWLEDGE of site-b's merge is %v, want Effective — \"later in record order, permanently\" was D-28 Part B, which D-92 reversed", o.Status)
			}
		}
	}
	if !saw {
		t.Fatalf("Outcome on the un-merge reported no same_as fact (got %+v) — cannot-match is a failure, never a skip", outs)
	}
	peer, err := domain.Outcome(st, merge.Key())
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range peer {
		if o.Fact == w5dFieldSameAs && (o.Status != domain.Superseded || o.Winner != k) {
			t.Fatalf("D-92: the peer's merge is (%v, winner %v), want (Superseded, %v) — superseded by a key LESS than its own, because that key's author saw it", o.Status, o.Winner, k)
		}
	}
	c, err := domain.CompanyByID(st, x.CompanyID)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil || c.SameAs != nil {
		t.Fatalf("D-92: composed company = %v after an un-merge made in knowledge of the merge, want same_as absent", c)
	}
	m, err := domain.LoadCompanyMergeMap(st)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Resolve(x.CompanyID); got != x.CompanyID {
		t.Fatalf("D-92: Resolve(%s) = %s, want identity — the company merge snapshot reads the same rule", x.CompanyID, got)
	}
	if tok, has := w3HasAdverse(page); has {
		t.Fatalf("D-92 (W3-m12's shape): the WINNING un-merge's confirmation renders supersession language (%q) — adverse words on a clean confirmation are the cry-wolf defect. Page:\n%s", tok, page)
	}
	notice := w5dAssertRendering2(t, page, srv.Cfg, "site-b")
	if !strings.Contains(notice, w4RecoveryPhrase) {
		t.Fatalf("D-53 / W5d-9(a): the cross-post winning notice lost its radio clause (%q) — the officer at Pad Site Control whose merge was just superseded is told by nobody without it: %q", w4RecoveryPhrase, notice)
	}
}

// w5dRemoteAmendChain authors company amendments IN ORDER as siteID on ONE
// scratch post primed with the given history — a real edge, so each stamps
// its own `seen` from what that post holds at that moment — and returns
// only the envelopes it minted. w3RemoteAmendChain's company twin.
func w5dRemoteAmendChain(t *testing.T, primer []store.Envelope, siteID string, clockMS int64, ins ...domain.CompanyAmendment) []store.Envelope {
	t.Helper()
	scratch := w3OpenRegistered(t, "w5d-scratch-chain-"+siteID+".db")
	w3ApplyWire(t, scratch, primer)
	scratch.Clock = w2Reading(clockMS, store.ClockSynced)
	before := len(w3Stream(t, scratch, siteID, 1))
	for _, in := range ins {
		if _, err := domain.AmendCompany(scratch, siteID, 1, in, 0, "default"); err != nil {
			t.Fatalf("remote company fixture amendment refused: %v", err)
		}
	}
	return w3Stream(t, scratch, siteID, 1)[before:]
}

// TestW5dWinningCompanyUnMergeTakesEffectQuietly — W5d-8, T9 direction (a):
// merge authored at site-a, un-merge POSTed at site-b (the winning post).
// The un-merge is Effective: composed state un-merged, and NO supersession
// language on the confirmation — the mirror negative that keeps direction
// (b)'s assertion honest, since an unconditional warning would pass both.
func TestW5dWinningCompanyUnMergeTakesEffectQuietly(t *testing.T) {
	_, stA, x, y := w5dSiteAServer(t)
	if _, err := domain.AmendCompany(stA, "site-a", 1,
		domain.CompanyAmendment{CompanyID: x.CompanyID, SameAs: &y.CompanyID}, 0, "default"); err != nil {
		t.Fatalf("site-a company merge fixture refused: %v", err)
	}
	srvB, stB := w5dSiteBServer(t, w3Stream(t, stA, "site-a", 1))

	form := url.Values{}
	form.Set(w5dFieldCompanyID, x.CompanyID)
	form.Set(w5dFieldClear, w5dFieldSameAs)
	rec, cookie := w5dPostAmend(t, srvB, form)
	page, location := w3FollowRecorded(t, srvB, rec, cookie)

	outs, err := domain.Outcome(stB, w3RecordedKey(t, location))
	if err != nil {
		t.Fatalf("Outcome on the recorded company un-merge: %v", err)
	}
	for _, o := range outs {
		if o.Fact == w5dFieldSameAs && o.Status != domain.Effective {
			t.Fatalf("W5d-8: the WINNING post's un-merge outcome is %v, want Effective", o.Status)
		}
	}
	c, err := domain.CompanyByID(stB, x.CompanyID)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil || c.SameAs != nil {
		t.Fatalf("W5d-8: the WINNING post's un-merge did not take effect — composed same_as = %v, want absent", c)
	}
	m, err := domain.LoadCompanyMergeMap(stB)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Resolve(x.CompanyID); got != x.CompanyID {
		t.Fatalf("W5d-8: Resolve(%s) = %s at the winning post after its un-merge, want identity", x.CompanyID, got)
	}
	if tok, has := w3HasAdverse(page); has {
		t.Fatalf("W5d-8 (W3-m12's shape): the winning post's EFFECTIVE un-merge renders supersession language (%q) — adverse words on a clean confirmation are the cry-wolf defect. Page:\n%s", tok, page)
	}
}

// TestW5dCleanCompanyAmendmentsRenderNoSupersession — W5d-8's T6b cry-wolf
// CONTROL, landing WITH the criterion (W5d-8 is not certifiable without
// it): a clean company amendment, a clean merge and a clean un-merge — no
// competitor anywhere — render NO supersession language AND, per D-53's
// observable, no rendering-2 wording either. Kills W3-m12 and the
// amendment-side VF-m7′ analogue: an unconditional banner would make every
// positive assertion above self-confirming.
func TestW5dCleanCompanyAmendmentsRenderNoSupersession(t *testing.T) {
	srv, _, x, y := w5dSiteAServer(t)
	steps := []struct {
		name string
		form url.Values
		// uncontested is TRUE when the step's asserted fact has NO earlier
		// assertion anywhere — which is what makes the rendering-2 absence
		// check meaningful. ⚠ MEASURED while writing these tests, and it is a
		// real interaction the lock does not state: a clean UN-MERGE is NOT
		// competitor-free. Clear:["same_as"] necessarily supersedes the merge
		// it reverses, on the same fact and at the same post — so under
		// [[D-53]]'s FIX arm its confirmation legitimately carries L-1's
		// SAME-POST rendering-2 wording, and asserting rendering-2 ABSENCE
		// there would fail a CORRECT build. T6b's LOCKED text is about
		// supersession LANGUAGE (w3AdverseTokens), which is asserted on all
		// three steps.
		uncontested bool
	}{
		{"clean amendment", url.Values{w5dFieldCompanyID: {x.CompanyID}, w5dFieldName: {"Clean Rename Ltd"}}, true},
		{"clean merge", url.Values{w5dFieldCompanyID: {x.CompanyID}, w5dFieldSameAs: {y.CompanyID}}, true},
		{"clean un-merge (supersedes its own merge, same post — see the note)", url.Values{w5dFieldCompanyID: {x.CompanyID}, w5dFieldClear: {w5dFieldSameAs}}, false},
	}
	for _, s := range steps {
		rec, cookie := w5dPostAmend(t, srv, s.form)
		page, _ := w3FollowRecorded(t, srv, rec, cookie)
		if tok, has := w3HasAdverse(page); has {
			t.Fatalf("W5d-8/T6b (W3-m12): the %s — nothing superseded at any post — renders supersession language (%q); the normal path stays terse. Page:\n%s", s.name, tok, page)
		}
		n, has := w5dNoticeCarrying(page, w5dRendering2Stem)
		if has && s.uncontested {
			t.Fatalf("W5d-9(a) (VF-m7′'s analogue): the %s — with NO earlier assertion of that fact anywhere — renders the winning-post wording: %q. An unconditional rendering-2 banner makes every winner assertion self-confirming, which is the cry-wolf defect one rendering over", s.name, n)
		}
		if !has && !s.uncontested {
			t.Fatalf("W5d-9(a) / L-1: the %s does NOT carry the winning-post wording, and it should: the un-merge supersedes the merge it reverses, at the SAME post, which is exactly L-1's same-post case and the second producer of that form. Notices: %v", s.name, w5dNoticeTexts(page))
		}
		if strings.Contains(w3PageText(page), w3Rendering4Phrase) {
			t.Fatalf("W5d-8/T6b: the %s renders the FAILURE path's %q — rendering 4 on a clean confirmation would make the surface's error path self-confirming", s.name, w3Rendering4Phrase)
		}
	}
}

// TestW5dStandingCostLineOnTheCompanySurface — W5d-8's T10 on THIS surface
// (W3-6's "T10 on its surface", cloned): the standing line renders on the
// COMPANY amendment surface under site-a's config AND site-b's config AND a
// swapped-ids config, and in every case names the GREATER site_id's display
// name as precedence-taker ("this post" when that is the rendering post
// itself). The expected winner is DERIVED from the same config the server
// loads, so a template naming a fixed post, or comparing lesser-wins, is
// red under at least one config. Kills W3-m13.
func TestW5dStandingCostLineOnTheCompanySurface(t *testing.T) {
	worlds := []struct {
		name  string
		build func(t *testing.T) (*Server, string)
	}{
		{"site-a (loses to peer)", func(t *testing.T) (*Server, string) {
			srv, _, x, _ := w5dSiteAServer(t)
			return srv, x.CompanyID
		}},
		{"site-b (wins over peer)", func(t *testing.T) (*Server, string) {
			_, stA, x, _ := w5dSiteAServer(t)
			srvB, _ := w5dSiteBServer(t, w3Stream(t, stA, "site-a", 1))
			return srvB, x.CompanyID
		}},
		{"swapped ids (this post wins; a hardcoded 'Pad Site Control' reds here)", func(t *testing.T) (*Server, string) {
			st := w3OpenRegistered(t, "w5d-swap.db")
			c, err := domain.CreateCompany(st, "site-z", 1, "Swap Company Ltd", 0, "default")
			if err != nil {
				t.Fatal(err)
			}
			return New(w3ConfigSwap(st.Path), st), c.CompanyID
		}},
	}
	for _, w := range worlds {
		t.Run(w.name, func(t *testing.T) {
			srv, companyID := w.build(t)
			form := url.Values{}
			form.Set(w5dFieldCompanyID, companyID)
			form.Set(w5dFieldName, "Standing Line Rename Ltd")
			rec, cookie := w5dPostAmend(t, srv, form)
			page, _ := w3FollowRecorded(t, srv, rec, cookie)

			sentence := w3StandingSentence(t, page)
			taker := strings.ToLower(w3PrecedenceTaker(srv.Cfg))
			if taker == "" {
				t.Fatalf("fixture defect: config derives no precedence-taker")
			}
			claim := taker + "'s correction " + w3PrecedencePhrase
			if !strings.Contains(sentence, claim) {
				t.Fatalf("W5d-8/T10 (W3-m13): under %s the company surface's standing line reads %q — the precedence claim %q (derived: greater site_id) is not what it asserts. D-28 orders this sentence written where officers meet it, and W5d builds a NEW officer-facing surface", w.name, sentence, claim)
			}
		})
	}
}

// --- W5d-9(a): the WINNING post's side, BOTH L-1 forms ------------------

// TestW5dWinningCompanyAmendmentTellsItsAuthor — W5d-9(a), the OBSERVABLE.
// The winning officer's confirmation page tells them, IN WORDS, that their
// amendment superseded a peer's — asserted against the rendered element,
// wording pinned as a literal drawn from outside w3AdverseTokens, on the
// response to the winner's OWN POST.
//
// BOTH L-1 forms are produced: the CROSS-POST form (winner and loser at
// different sites — the peer's config-derived display name) and the
// SAME-POST form (winner and loser sharing a site_id — "this post").
//
// Why this matters more here than anywhere else in W5, in the
// disposition's own words: the obligation exists BECAUSE an operator is not
// told — the winner is "the one person present at the moment the peer's
// loss becomes real, and the only one who can radio about it". Discharged
// below HTTP, neither arm of [[D-53]] is delivered and W7 and W8 inherit a
// "surfaced" winning post that is not surfaced.
func TestW5dWinningCompanyAmendmentTellsItsAuthor(t *testing.T) {
	t.Run("cross-post form (winner site-b, loser site-a)", func(t *testing.T) {
		_, stA, x, _ := w5dSiteAServer(t)
		// site-a's earlier name assertion — the one about to lose.
		if _, err := domain.AmendCompany(stA, "site-a", 1,
			domain.CompanyAmendment{CompanyID: x.CompanyID, Name: strPtrW5d("Ice Road's Spelling Ltd")}, 0, "default"); err != nil {
			t.Fatalf("site-a fixture amendment refused: %v", err)
		}
		srvB, stB := w5dSiteBServer(t, w3Stream(t, stA, "site-a", 1))

		form := url.Values{}
		form.Set(w5dFieldCompanyID, x.CompanyID)
		form.Set(w5dFieldName, "Pad Site Control's Spelling Ltd")
		rec, cookie := w5dPostAmend(t, srvB, form)
		page, location := w3FollowRecorded(t, srvB, rec, cookie)

		// Fixture precondition (rule 6): the POST must actually have WON,
		// or this test asserts the winner's rendering on a loser.
		out := w5dNameOutcome(t, stB, w3RecordedKey(t, location))
		if out.Status != domain.Effective {
			t.Fatalf("W5d-9(a) fixture defect: the site-b POST is %v, not Effective — \"site-b\" > \"site-a\" must make it the record-order winner, or this test is asserting the WINNER's rendering on a losing post", out.Status)
		}
		w5dAssertRendering2(t, page, srvB.Cfg, "site-a")
		// The winner's own page is NOT adverse: their assertion stands.
		if tok, has := w3HasAdverse(page); has {
			t.Fatalf("W5d-9(a): the WINNING post's confirmation carries the adverse register (%q) — the adverse words belong to the LOSING side, and mixing them is how a test that \"only greps for 'superseded'\" becomes m7′-blind. Page:\n%s", tok, page)
		}
	})

	t.Run("same-post form (L-1: winner and loser share a site_id)", func(t *testing.T) {
		st := w3OpenRegistered(t, "w5d-samepost.db")
		c, err := domain.CreateCompany(st, "site-a", 1, "Same Post Ltd", 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		// An earlier assertion by the SAME post.
		if _, err := domain.AmendCompany(st, "site-a", 1,
			domain.CompanyAmendment{CompanyID: c.CompanyID, Name: strPtrW5d("First Spelling Ltd")}, 0, "default"); err != nil {
			t.Fatalf("same-post fixture amendment refused: %v", err)
		}
		srv := New(w5dConfigSiteAAlt(st.Path), st)

		form := url.Values{}
		form.Set(w5dFieldCompanyID, c.CompanyID)
		form.Set(w5dFieldName, "Second Spelling Ltd")
		rec, cookie := w5dPostAmend(t, srv, form)
		page, location := w3FollowRecorded(t, srv, rec, cookie)

		out := w5dNameOutcome(t, st, w3RecordedKey(t, location))
		if out.Status != domain.Effective {
			t.Fatalf("W5d-9(a) fixture defect: the second same-post amendment is %v, not Effective — the later key must win", out.Status)
		}
		if out.Winner.SiteID != "site-a" {
			t.Fatalf("W5d-9(a) fixture defect: the winner's site is %q, want site-a — the SAME-POST case requires winner and loser to share a site_id", out.Winner.SiteID)
		}
		notice := w5dAssertRendering2(t, page, srv.Cfg, "site-a")

		// ⚠ THE CRITERION'S COPY CHANGED — this assertion was not moved to
		// make code pass. Until the W5 post-lock fix (operator-journey
		// critique F-1) this arm rendered the CROSS-POST sentence entire,
		// radio instruction included, and it passed: the fixture above is
		// literally amend-then-amend at one post, i.e. an officer correcting
		// their own typo, and the screen ordered them to radio the
		// correction to a peer who does not exist. D-53 justifies that
		// instruction by the winner being "the only one who can radio about
		// it"; in the same-post case there is no peer, so the premise is
		// absent and the order was false. The displacement confirmation
		// above still stands and is still asserted — only the radio clause
		// is gone, and this pins its absence so it cannot drift back.
		if strings.Contains(notice, "radio it through") {
			t.Fatalf("W5d-9(a) / F-1: the SAME-POST winner's notice reads %q — it orders a radio call with no recipient. The officer who sees this arm is correcting their OWN earlier amendment; there is no peer to tell, and a notice that cries wolf here is the one they will ignore when it fires CROSS-POST, which is the case D-53 was built for. The cross-post arm keeps the clause; this one must not.", notice)
		}
	})
}

// w5dNameOutcome returns the `name` fact's outcome for a recorded key.
func w5dNameOutcome(t *testing.T, st *store.Store, k store.Key) domain.FactOutcome {
	t.Helper()
	outs, err := domain.Outcome(st, k)
	if err != nil {
		t.Fatalf("Outcome(%v): %v", k, err)
	}
	for _, o := range outs {
		if o.Fact == w5dFieldName {
			return o
		}
	}
	t.Fatalf("Outcome(%v) reported no %q fact (got %+v) — cannot-match is a FAILURE, never a skip", k, w5dFieldName, outs)
	return domain.FactOutcome{}
}
