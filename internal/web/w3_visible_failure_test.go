// w3_visible_failure_test.go — W3-6 (a losing-post un-merge does NOT take
// effect AND fails visibly to its author — VF §8 T9 both directions, T6b
// as the cry-wolf control, T10 the standing-cost line) and W3-15 (the
// confirmation's failure path — VF rendering 4, produced once on THIS
// surface). design/WINDOW-BUILD-PLAN-v1.md §3 W3, LOCKED 2026-08-27.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS the clean-tree red.
//
// KILLER MUTANTS: m3 (the losing un-merge takes effect — the composed
// half), m11 (silent-OK — an unconditional "Recorded." confirmation),
// m12 (unconditional supersession warning — T6b), m13 (hardcoded or
// inverted standing line — T10), m21 (success-shaped error path — a
// garbled key or Outcome error renders bare success), and — since
// SchemaVersion 11 under [[D-92]] — m-D92a (a compose that reaches the
// site_id tiebreak on a CAUSALLY ordered pair: D-28 Part B resurrected;
// killed by TestW3UnMergeMadeInKnowledgeOfThePeersMergeTakesEffect).
//
// ⟦SchemaVersion 11, D-92 — W3-6 (b) RE-RULED, not silently edited⟧ The
// losing un-merge is now the GENUINELY CONCURRENT case (site_id decides
// only between authors who did not see each other); the construction this
// file used at 10 — peer's merge applied, then the POST — is an un-merge
// made IN KNOWLEDGE of the merge, and under D-92 it WINS. Both arms are
// witnessed below.
//
// As deployed "site-b" > "site-a": Pad Site Control wins every contested
// fact and Ice Road is the losing post [DOC — VF §5/§10, measured there
// from the deploy TOMLs; derived here from the fixture configs, never
// hardcoded].
package web

import (
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// TestW3LosingUnMergeDoesNotTakeEffectAndFailsVisibly — T9 direction (b):
// merge authored at site-b (the winning post), un-merge POSTed at site-a
// (the losing post). BOTH halves asserted: the composed state stays
// MERGED at the losing post, and the HTTP confirmation says in words that
// the merge stands — "the two records remain merged on every screen" —
// naming the winning post by its config-derived display name.
//
// ⟦SchemaVersion 11, D-92 — RE-RULED⟧ At 10 this fixture applied site-b's
// merge and then POSTed the un-merge; under fixed order the un-merge lost.
// Under D-92 that construction is an un-merge made IN KNOWLEDGE of the
// merge and it WINS — the reversed pin is
// TestW3UnMergeMadeInKnowledgeOfThePeersMergeTakesEffect below. A LOSING
// un-merge at 11 is the genuinely concurrent case: both posts hold site-b's
// merge; site-a un-merges; cut off, site-b un-merges and re-merges the same
// pair; the two STANDING assertions — site-a's un-merge and site-b's
// re-merge — saw the same history and not each other, and the site_id
// tiebreak decides. The officer meets the loss on a re-GET of their own
// confirmation (read-time truth, W3-16 / m22).
// ⟦was: site-b's merge applied, then the POST; the page read once⟧
func TestW3LosingUnMergeDoesNotTakeEffectAndFailsVisibly(t *testing.T) {
	srv, st, x, y := w3SiteAServer(t)
	roster := w3Stream(t, st, "site-a", 1)
	// Pad Site Control's merge X→Y arrives by wire; BOTH posts now hold it.
	merge := w3RemoteAmend(t, roster, "site-b", w2Base+w2Hour,
		domain.PersonAmendment{PersonID: x.PersonID, SameAs: &y.PersonID})
	w3ApplyWire(t, st, []store.Envelope{merge})
	held := append(append([]store.Envelope{}, roster...), merge)

	form := url.Values{}
	form.Set(w3FieldPersonID, x.PersonID)
	form.Set(w3FieldClear, w3FieldSameAs)
	rec, cookie := w3PostAmend(t, srv, form)
	first, location := w3FollowRecorded(t, srv, rec, cookie)
	if tok, has := w3HasAdverse(first); has {
		t.Fatalf("fixture (W3-16, m22): before site-b's re-merge arrives the un-merge is Effective, yet its confirmation renders %q — a status stamped at write", tok)
	}

	// Cut off from site-a, Pad Site Control un-merges and re-merges the same
	// pair on its own edge. Its re-merge and site-a's un-merge are
	// concurrent; site-b's key wins the tie.
	chain := w3RemoteAmendChain(t, held, "site-b", w2Base+2*w2Hour,
		domain.PersonAmendment{PersonID: x.PersonID, Clear: []string{w3FieldSameAs}},
		domain.PersonAmendment{PersonID: x.PersonID, SameAs: &y.PersonID})
	if len(chain) != 2 {
		t.Fatalf("fixture defect: site-b minted %d events, want 2 (un-merge, re-merge)", len(chain))
	}
	w3ApplyWire(t, st, chain)
	page := w2Get(t, srv, location, cookie).Body.String()

	// The un-merge itself is SUPERSEDED (per-fact, at read time).
	outs, err := domain.Outcome(st, w3RecordedKey(t, location))
	if err != nil {
		t.Fatalf("Outcome on the recorded un-merge: %v", err)
	}
	sawSameAs := false
	for _, o := range outs {
		if o.Fact == w3FieldSameAs {
			sawSameAs = true
			if o.Status != domain.Superseded {
				t.Fatalf("W3-6 (m3): the losing post's un-merge outcome is %v, want Superseded — site-b's CONCURRENT re-merge takes the site_id tiebreak (D-92: the one case fixed order still decides) ⟦was: \"site-b's merge is later in record order, permanently\"⟧", o.Status)
			}
		}
	}
	if !sawSameAs {
		t.Fatalf("W3-6: Outcome on the un-merge reported no same_as fact (got %+v) — cannot-match is a failure, never a skip", outs)
	}

	// Half 1 — does NOT take effect (m3): composed state still merged.
	p, err := domain.PersonByID(st, x.PersonID)
	if err != nil {
		t.Fatal(err)
	}
	if p.SameAs == nil || *p.SameAs != y.PersonID {
		t.Fatalf("W3-6 (m3): the LOSING post's un-merge took effect — composed same_as = %v, want the standing merge to %s (site-b's concurrent re-merge wins the tiebreak, D-92 ⟦was: \"later in record order, permanently\"⟧)", p.SameAs, y.PersonID)
	}
	m, err := domain.LoadPersonMergeMap(st)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Resolve(x.PersonID); got != y.PersonID {
		t.Fatalf("W3-6 (m3): Resolve(%s) = %s after the losing un-merge, want the merged %s", x.PersonID, got, y.PersonID)
	}

	// Half 2 — fails VISIBLY to its author (m11): the confirmation says
	// the merge stands, in words, naming the winning post.
	text := w3PageText(page)
	if !strings.Contains(text, w3RemainMergedPhrase) {
		t.Fatalf("W3-6 (m11, silent-OK): the losing un-merge's confirmation does not say the merge stands (%q absent) — a correction that silently does nothing is the project's signature defect (D-28 Part B). Page:\n%s", w3RemainMergedPhrase, page)
	}
	winner := w3PrecedenceTaker(srv.Cfg) // "Pad Site Control", derived from config
	if winner == "this post" || winner == "" {
		t.Fatalf("fixture defect: site-a's config must derive a PEER as precedence-taker, got %q", winner)
	}
	if !strings.Contains(text, strings.ToLower(winner)) {
		t.Fatalf("W3-6: the adverse confirmation does not name the winning post %q (derived from the config, never hardcoded). Page:\n%s", winner, page)
	}
}

// TestW3UnMergeMadeInKnowledgeOfThePeersMergeTakesEffect — D-92's reversal
// of W3-6 (b)'s fixture as it stood at 10: site-b's merge arrives, and
// site-a's officer, seeing it, un-merges. The un-merge WINS at the LESSER
// site_id: composed state un-merged, Resolve identity, Outcome Effective
// with the peer's merge Superseded BY the un-merge, and the confirmation is
// the terse register — no supersession language, no radio instruction.
// ⟦was: "site-b's merge is later in record order, permanently" — D-28 Part
// B, reversed by D-92⟧ Kills m-D92a.
func TestW3UnMergeMadeInKnowledgeOfThePeersMergeTakesEffect(t *testing.T) {
	srv, st, x, y := w3SiteAServer(t)
	merge := w3RemoteAmend(t, w3Stream(t, st, "site-a", 1), "site-b", w2Base+3*w2Hour,
		domain.PersonAmendment{PersonID: x.PersonID, SameAs: &y.PersonID})
	w3ApplyWire(t, st, []store.Envelope{merge})

	form := url.Values{}
	form.Set(w3FieldPersonID, x.PersonID)
	form.Set(w3FieldClear, w3FieldSameAs)
	rec, cookie := w3PostAmend(t, srv, form)
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
		if o.Fact == w3FieldSameAs {
			saw = true
			if o.Status != domain.Effective {
				t.Fatalf("W3-6 re-ruled (D-92; m-D92a): the un-merge made IN KNOWLEDGE of site-b's merge is %v, want Effective — \"later in record order, permanently\" was D-28 Part B, which D-92 reversed", o.Status)
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
		if o.Fact == w3FieldSameAs && (o.Status != domain.Superseded || o.Winner != k) {
			t.Fatalf("D-92: the peer's merge is (%v, winner %v), want (Superseded, %v) — superseded by a key LESS than its own, because that key's author saw it", o.Status, o.Winner, k)
		}
	}
	p, err := domain.PersonByID(st, x.PersonID)
	if err != nil {
		t.Fatal(err)
	}
	if p.SameAs != nil {
		t.Fatalf("D-92: composed same_as = %q after an un-merge made in knowledge of the merge, want absent", *p.SameAs)
	}
	m, err := domain.LoadPersonMergeMap(st)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Resolve(x.PersonID); got != x.PersonID {
		t.Fatalf("D-92: Resolve(%s) = %s, want identity — the merge snapshot reads the same rule", x.PersonID, got)
	}
	if tok, has := w3HasAdverse(page); has {
		t.Fatalf("D-92 (m12's shape): the WINNING un-merge's confirmation renders supersession language (%q) — adverse words on a clean confirmation are the cry-wolf defect. Page:\n%s", tok, page)
	}
	if strings.Contains(w3PageText(page), w4RecoveryPhrase) {
		t.Fatalf("D-92: the WINNING un-merge's confirmation orders a radio call (%q) — there is nothing to radio; the un-merge stands. Page:\n%s", w4RecoveryPhrase, page)
	}
}

// w3RemoteAmendChain authors amendments IN ORDER as siteID on ONE scratch
// post primed with the given history — a real edge, so each stamps its own
// `seen` from what that post holds at that moment — and returns only the
// envelopes it minted.
func w3RemoteAmendChain(t *testing.T, primer []store.Envelope, siteID string, clockMS int64, ins ...domain.PersonAmendment) []store.Envelope {
	t.Helper()
	scratch := w3OpenRegistered(t, "w3-scratch-chain-"+siteID+".db")
	w3ApplyWire(t, scratch, primer)
	scratch.Clock = w2Reading(clockMS, store.ClockSynced)
	before := len(w3Stream(t, scratch, siteID, 1))
	for _, in := range ins {
		if _, err := domain.AmendPerson(scratch, siteID, 1, in, 0, "default"); err != nil {
			t.Fatalf("remote fixture amendment refused: %v", err)
		}
	}
	return w3Stream(t, scratch, siteID, 1)[before:]
}

// TestW3WinningPostUnMergeTakesEffectQuietly — T9 direction (a): merge
// authored at site-a, un-merge POSTed at site-b (the winning post). The
// un-merge is Effective: composed state un-merged, and NO supersession
// language on the confirmation (the mirror negative that keeps direction
// (b)'s assertion honest — an unconditional warning would pass both
// otherwise; see also T6b).
func TestW3WinningPostUnMergeTakesEffectQuietly(t *testing.T) {
	// Author the roster and site-a's merge at the site-a post, then stand
	// up the site-b post with all of it replicated in.
	_, stA, x, y := w3SiteAServer(t)
	if _, err := domain.AmendPerson(stA, "site-a", 1, domain.PersonAmendment{PersonID: x.PersonID, SameAs: &y.PersonID}, 0, "default"); err != nil {
		t.Fatalf("site-a merge fixture refused: %v", err)
	}
	srvB, stB := w3SiteBServer(t, w3Stream(t, stA, "site-a", 1))

	form := url.Values{}
	form.Set(w3FieldPersonID, x.PersonID)
	form.Set(w3FieldClear, w3FieldSameAs)
	rec, cookie := w3PostAmend(t, srvB, form)
	page, location := w3FollowRecorded(t, srvB, rec, cookie)

	// The un-merge is EFFECTIVE at the winning post.
	outs, err := domain.Outcome(stB, w3RecordedKey(t, location))
	if err != nil {
		t.Fatalf("Outcome on the recorded un-merge: %v", err)
	}
	for _, o := range outs {
		if o.Fact == w3FieldSameAs && o.Status != domain.Effective {
			t.Fatalf("W3-6: the WINNING post's un-merge outcome is %v, want Effective", o.Status)
		}
	}

	p, err := domain.PersonByID(stB, x.PersonID)
	if err != nil {
		t.Fatal(err)
	}
	if p.SameAs != nil {
		t.Fatalf("W3-6: the WINNING post's un-merge did not take effect — composed same_as = %q, want absent", *p.SameAs)
	}
	m, err := domain.LoadPersonMergeMap(stB)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Resolve(x.PersonID); got != x.PersonID {
		t.Fatalf("W3-6: Resolve(%s) = %s at the winning post after its un-merge, want identity", x.PersonID, got)
	}
	if tok, has := w3HasAdverse(page); has {
		t.Fatalf("W3-6 (m12's shape): the winning post's EFFECTIVE un-merge renders supersession language (%q) — adverse words on a clean confirmation are the cry-wolf defect. Page:\n%s", tok, page)
	}
}

// TestW3CleanAmendmentAndUnMergeRenderNoSupersession — T6b, the cry-wolf
// control landing WITH W3 (W3-6 is not certifiable without it): a clean
// amendment, a clean merge, and a clean un-merge — no competitor
// anywhere — render NO supersession language. Kills m12 (the
// unconditional warning, which would make direction (b)'s grep
// self-confirming and burn the confirmation's trust — gate.go:696-698's
// recorded lesson).
func TestW3CleanAmendmentAndUnMergeRenderNoSupersession(t *testing.T) {
	srv, _, x, y := w3SiteAServer(t)
	steps := []struct {
		name string
		form url.Values
	}{
		{"clean amendment", url.Values{w3FieldPersonID: {x.PersonID}, w3FieldPhone: {"555-0177"}}},
		{"clean merge", url.Values{w3FieldPersonID: {x.PersonID}, w3FieldSameAs: {y.PersonID}}},
		{"clean un-merge", url.Values{w3FieldPersonID: {x.PersonID}, w3FieldClear: {w3FieldSameAs}}},
	}
	for _, s := range steps {
		rec, cookie := w3PostAmend(t, srv, s.form)
		page, _ := w3FollowRecorded(t, srv, rec, cookie)
		if tok, has := w3HasAdverse(page); has {
			t.Fatalf("W3-6/T6b (m12): the %s — no competitor at any post — renders supersession language (%q); the normal path stays terse (attack 2). Page:\n%s", s.name, tok, page)
		}
		if strings.Contains(w3PageText(page), w3Rendering4Phrase) {
			t.Fatalf("W3-6/T6b: the %s renders the FAILURE path's %q — rendering 4 on a clean confirmation would make W3-15 self-confirming", s.name, w3Rendering4Phrase)
		}
	}
}

// TestW3StandingCostLineDerivesGreaterSiteID — T10 (m13): the standing
// line renders on the amendment surface under site-a's config AND
// site-b's config AND a swapped-ids config, and in every case names the
// GREATER site_id's display name as precedence-taker ("this post" when
// that is the rendering post itself) — the expected winner is DERIVED
// from the same config the server loads, never hardcoded, so a template
// naming a fixed post or comparing lesser-wins is red under at least one
// config.
func TestW3StandingCostLineDerivesGreaterSiteID(t *testing.T) {
	worlds := []struct {
		name  string
		build func(t *testing.T) (*Server, string)
	}{
		{"site-a (loses to peer)", func(t *testing.T) (*Server, string) {
			srv, _, x, _ := w3SiteAServer(t)
			return srv, x.PersonID
		}},
		{"site-b (wins over peer)", func(t *testing.T) (*Server, string) {
			_, stA, x, _ := w3SiteAServer(t)
			srvB, _ := w3SiteBServer(t, w3Stream(t, stA, "site-a", 1))
			return srvB, x.PersonID
		}},
		{"swapped ids (this post wins; hardcoded 'Pad Site Control' reds here)", func(t *testing.T) (*Server, string) {
			st := w3OpenRegistered(t, "w3-swap.db")
			p, err := domain.CreatePerson(st, "site-z", 1, "Swap Person", nil, nil, 0, "default")
			if err != nil {
				t.Fatal(err)
			}
			return New(w3ConfigSwap(st.Path), st), p.PersonID
		}},
	}
	for _, w := range worlds {
		t.Run(w.name, func(t *testing.T) {
			srv, personID := w.build(t)
			form := url.Values{}
			form.Set(w3FieldPersonID, personID)
			form.Set(w3FieldPhone, "555-0161")
			rec, cookie := w3PostAmend(t, srv, form)
			page, _ := w3FollowRecorded(t, srv, rec, cookie)

			sentence := w3StandingSentence(t, page)
			taker := strings.ToLower(w3PrecedenceTaker(srv.Cfg))
			if taker == "" {
				t.Fatalf("fixture defect: config derives no precedence-taker")
			}
			// The taker's name must be what stands immediately before the
			// precedence claim (VF §5's worked wording at both posts) —
			// containment alone cannot distinguish the inverted line,
			// which names both parties too.
			claim := taker + "'s correction " + w3PrecedencePhrase
			if !strings.Contains(sentence, claim) {
				t.Fatalf("W3-6/T10 (m13): under %s the standing line reads %q — the precedence claim %q (derived: greater site_id) is not what it asserts; round 1's own text WAS this mutant, inverted, on the one sentence D-28 orders written where officers meet it", w.name, sentence, claim)
			}
		})
	}
}

// TestW3ConfirmationFailurePathRendersLoudly — W3-15 (VF T4b's shape on
// THIS surface, per seam S2: W3 builds the first ?recorded= parse): a
// garbled ?recorded= value and an induced Outcome error (a well-formed
// key matching no event) BOTH render VF §5's rendering 4 — "outcome could
// not be determined", adverse-shaped, in words — and the terse success
// register is ABSENT. Kills m21 (the success-shaped error path — the
// intake.go:359 silently-ignore precedent, explicitly not followed here).
func TestW3ConfirmationFailurePathRendersLoudly(t *testing.T) {
	srv, _, x, _ := w3SiteAServer(t)
	form := url.Values{}
	form.Set(w3FieldPersonID, x.PersonID)
	form.Set(w3FieldPhone, "555-0166")
	rec, cookie := w3PostAmend(t, srv, form)
	page, location := w3FollowRecorded(t, srv, rec, cookie)

	// Guard: the REAL confirmation must not render the failure path, or
	// both probes below pass vacuously.
	if strings.Contains(w3PageText(page), w3Rendering4Phrase) {
		t.Fatalf("W3-15 guard: the clean confirmation already renders %q — rendering 4 everywhere is m12's shape and makes this test self-confirming", w3Rendering4Phrase)
	}

	probes := []struct {
		name string
		url  string
	}{
		// ⟦D23, fix2-fj 2026-10-04⟧ James, 2026-10-04 (FINDINGS-R4 cluster J): ?recorded= renders only off a sealed redirect, so this arm is reached through one; a hand-typed value renders nothing (fix2_fj_sealed_notices_test.go).
		// VF-m5b's criterion is unchanged: a redirect THIS process wrote
		// whose read-back fails still renders rendering 4.
		{"garbled ?recorded=", sealNotices(w3RecordedParamRe.ReplaceAllString(location, "recorded=not-a-key"))},
		{"induced Outcome error (no such event)", sealNotices(w3RecordedParamRe.ReplaceAllString(location, "recorded=site-a.1.999999"))},
	}
	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			got := w2Get(t, srv, p.url, cookie)
			if got.Code != 200 {
				t.Fatalf("W3-15: %s GET = %d — the confirmation page must render (the append DID succeed); what failed is the read-back, and the officer must be told IN WORDS", p.name, got.Code)
			}
			text := w3PageText(got.Body.String())
			if !strings.Contains(text, w3Rendering4Phrase) {
				t.Fatalf("W3-15 (m21): %s renders no %q — an outcome block that fails quietly is success-shaped (VF attack 4; the intake.go:359 shape is exactly what a builder copies without this test). Page:\n%s", p.name, w3Rendering4Phrase, got.Body.String())
			}
			for _, tok := range w3TerseSuccessTokens {
				if strings.Contains(text, strings.ToLower(tok)) {
					t.Fatalf("W3-15 (m21): %s still renders the terse success register (%q) beside the failure rendering — the confirmation may not vouch for an outcome it could not determine", p.name, tok)
				}
			}
		})
	}
}
