// w4_visible_failure_test.go — W4-7: a losing-post correction does not
// take effect AND fails visibly (D-42 obligation 4 / D-28 Part B's build
// obligation). W4-7 = VF T1 + T4 (VF §8's equation), with T4b on the
// failure path, T5 on the WINNING post's mirror (supersession named,
// adverse register absent — inside W4's lock even though VF §8's summary
// equation omits it; reports/W4-RENDERING2-DISPOSITION.md §2), and T6 as
// the cry-wolf control. Nothing here contests which post wins — D-28
// B-i's priced order is untouched; the obligation is that the loser SEES
// it.
// design/WINDOW-BUILD-PLAN-v1.md "### W4", criteria LOCKED 2026-08-27.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS the clean-tree red (go vet).
//
// KILLER MUTANTS: (m3)=VF-m5 (silent losing-post apply — the blind-303),
// VF-m5b (success-shaped error path — the intake.go:359 shape), VF-m6
// (one-sided visibility — only the losing path implemented), VF-m7
// (unconditional warning — T6), VF-m7′ (the inverted renderer —
// supersession language on the WINNING confirmation; a T5 that only greps
// for "supersedes" is m7′-blind, so T5 asserts the mirror negatives too).
//
// As deployed "site-b" > "site-a": Pad Site Control wins every contested
// fact and Ice Road is the losing post [DOC — VF §5/§10; derived here
// from the fixture configs, never hardcoded].
//
// ⟦SchemaVersion 11, D-92 — W4-7's LOSING fixture RE-RULED, not silently
// edited⟧ "site-b > site-a" now decides only between authors who did not
// see each other. At 10 the losing fixture applied site-b's correction and
// THEN POSTed site-a's; under D-92 that POST is a correction made IN
// KNOWLEDGE of the peer's (its `seen` names it) and it WINS — the reversed
// pin is TestW4CorrectionMadeInKnowledgeOfThePeersTakesEffectAndOrdersNoRadioCall.
// The losing arm is re-built as the GENUINELY CONCURRENT case: site-a POSTs
// first, site-b corrects the same event cut off, and the site_id tiebreak
// decides — the officer meets the loss on a re-GET (read-time truth, VF
// §4.3 rule 2). The new mutant this file kills: m-D92a (a resolver that
// reaches the tiebreak on a causally ordered pair — D-28 Part B
// resurrected).
package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// w4UncorrectedWorld: a site-a post holding a checked-out visit whose
// checkout time is the correction target, with NO correction yet held by
// anyone. Returns the server, store and target key. The server clock is
// w2Base, so a POST made here carries a LATER clock than a peer correction
// authored at w2Base−1h — clock order contradicts key order per VF T1's
// discipline, in both constructions built on this world.
func w4UncorrectedWorld(t *testing.T) (*Server, *store.Store, store.Key) {
	t.Helper()
	st := w3OpenRegistered(t, "w4-losing.db")
	p, err := domain.CreatePerson(st, "site-a", 1, "Correction Target", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	st.Clock = w2Reading(w2Base-2*w2Hour, store.ClockSynced)
	w2CheckIn(t, st, []domain.Person{p})
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil || len(open) != 1 {
		t.Fatalf("fixture: open visits = %d (%v), want 1", len(open), err)
	}
	st.Clock = w2Reading(w2Base-w2Hour-30*w2Minute, store.ClockSynced)
	before := w2Watermark(t, st, "site-a", 1)
	if _, err := domain.CheckOutVisits(st, "site-a", 1, []string{open[0].VisitID}, nil, w2Base-2*w2Hour+10*w2Minute, "entered"); err != nil {
		t.Fatalf("fixture checkout refused: %v", err)
	}
	outs := w2EventsOfType(t, w2EventsFrom(t, st, "site-a", 1, before), "visit_checked_out")
	if len(outs) != 1 {
		t.Fatalf("fixture: %d visit_checked_out events, want 1", len(outs))
	}
	target := outs[0].Key()
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	return New(w3ConfigSiteA(st.Path), st), st, target
}

// w4LosingWorld: w4UncorrectedWorld with site-b's correction ALREADY
// replicated in, authored at an EARLIER clock than a POST here will carry.
//
// ⟦SchemaVersion 11, D-92⟧ Under D-92 this is no longer a LOSING world for
// a site-a POST: a correction made at a post that already holds the peer's
// is made IN KNOWLEDGE of it and wins. The helper keeps its historical name
// — it is the fixture of the reversed pin, and renaming it would edit a
// discharged file's call sites for no operator gain — and the concurrent
// losing world is built inline by the losing tests. ⟦was: "with site-b's
// WINNING correction already replicated in"⟧
func w4LosingWorld(t *testing.T, winnerMS int64, winnerRecordedBy string) (*Server, *store.Store, store.Key, store.Envelope) {
	t.Helper()
	srv, st, target := w4UncorrectedWorld(t)
	winner := w4RemoteCorrection(t, w3Stream(t, st, "site-a", 1), "site-b", w2Base-w2Hour, target, winnerMS, winnerRecordedBy)
	w3ApplyWire(t, st, []store.Envelope{winner})
	return srv, st, target, winner
}

// TestW4LosingCorrectionDoesNotTakeEffectAndFailsVisibly — T4 (m3/VF-m5,
// the at-rest half of W4-3a made officer-visible): POST the losing
// correction at a post already holding the winner; follow the 303. BOTH
// halves asserted: the resolved read still yields the WINNER's instant
// (the losing correction does not take effect), and the confirmation
// names the supersession in words — the adverse register, the winning
// post's config-derived display name, and the recovery sentence.
// ⟦SchemaVersion 11, D-92 — RE-RULED⟧ At 10 this fixture applied site-b's
// correction and then POSTed site-a's; under fixed order site-a lost. Under
// D-92 that construction is a correction made IN KNOWLEDGE of the peer's and
// it WINS (the reversed pin follows this test). A LOSING correction at 11 is
// the genuinely concurrent case: site-a's officer POSTs first; cut off, Pad
// Site Control corrects the same event without having seen it; the two are
// both heads and the site_id tiebreak decides. The officer meets the loss on
// a re-GET of their own confirmation (read-time truth, VF §4.3 rule 2).
// ⟦was: site-b's correction applied, then the POST; the page read once⟧
func TestW4LosingCorrectionDoesNotTakeEffectAndFailsVisibly(t *testing.T) {
	const (
		winnerMS = w2Base - 2*w2Hour + 20*w2Minute // pinned effective value
		loserMS  = w2Base - 2*w2Hour + 40*w2Minute // the losing officer's typed value
	)
	srv, st, target := w4UncorrectedWorld(t)
	held := w3Stream(t, st, "site-a", 1) // what Pad Site Control holds when the link drops
	before := w2Watermark(t, st, "site-a", 1)

	rec, cookie := w4PostCorrection(t, srv, w4CorrectionForm(target, w2Wall(loserMS), "Ida Aputi"))
	first, location, recorded := w4FollowRecorded(t, srv, rec, cookie)
	loser := w4MintedCorrection(t, st, "site-a", 1, before)
	if loser.Key() != recorded {
		t.Fatalf("W4-7: ?recorded=%v does not name the minted correction %v", recorded, loser.Key())
	}
	if tok, has := w3HasAdverse(first); has {
		t.Fatalf("fixture (VF §4.3 rule 2): before the peer's correction arrives the local one is Effective, yet its confirmation renders %q — a status stamped at write", tok)
	}

	// Cut off from site-a, Pad Site Control corrects the same event over the
	// history it holds — which does not include site-a's correction — at an
	// EARLIER clock. Neither author saw the other; site-b's key wins the tie.
	winner := w4RemoteCorrection(t, held, "site-b", w2Base-w2Hour, target, winnerMS, "Pat Bell")
	w3ApplyWire(t, st, []store.Envelope{winner})
	var peerPayload domain.OccurredAtCorrected
	if err := json.Unmarshal(winner.Payload, &peerPayload); err != nil || len(peerPayload.Seen) != 0 {
		t.Fatalf("fixture defect (D-92): the peer's correction carries seen=%v, want none — it must be authored without having seen site-a's, or it is not a concurrent pair (%v)", peerPayload.Seen, err)
	}
	page := w2Get(t, srv, location, cookie).Body.String()

	// Half 1 — does NOT take effect: the resolved read still yields the
	// winner's instant; the loser's Outcome is Superseded with the
	// winner's key (W4-3a's at-rest half).
	eff, _, err := w4ResolveBudgetedWeb(t, st, target)
	if err != nil {
		t.Fatalf("W4-7: ResolveOccurredAt: %v", err)
	}
	if eff.InstantUTCms != winnerMS || eff.Winner != winner.Key() {
		t.Fatalf("W4-7 (m3, the silent losing-post apply): resolved = (%d, winner %v), want (%d, %v) — a correction CONCURRENT with the peer's loses the site_id tiebreak (D-92: the one case fixed order still decides) ⟦was: \"a correction authored at the losing post may not take effect (D-28 Part B)\"⟧", eff.InstantUTCms, eff.Winner, winnerMS, winner.Key())
	}
	louts, err := domain.Outcome(st, loser.Key())
	if err != nil {
		t.Fatalf("W4-7: Outcome(loser): %v", err)
	}
	sawFact := false
	for _, o := range louts {
		if o.Fact == "occurred_at" {
			sawFact = true
			if o.Status != domain.Superseded || o.Winner != winner.Key() {
				t.Fatalf("W4-7: loser's Outcome = (%v, winner %v), want (Superseded, %v)", o.Status, o.Winner, winner.Key())
			}
		}
	}
	if !sawFact {
		t.Fatalf("W4-7: Outcome(loser) reported no occurred_at fact (%+v) — cannot-match is a failure, never a skip", louts)
	}

	// Half 2 — fails VISIBLY to its author (VF-m5's blind-303): adverse
	// register in words, the winning post named (derived), the recovery
	// sentence present.
	text := w3PageText(page)
	if !strings.Contains(text, "superseded") {
		t.Fatalf("W4-7 (VF-m5, silent-OK): the losing correction's confirmation carries no %q — a correction that silently does nothing is the project's signature defect (D-28 Part B). Page:\n%s", "superseded", page)
	}
	winnerName := w4DisplayName(t, srv.Cfg, winner.Key().SiteID)
	if !strings.Contains(text, strings.ToLower(winnerName)) {
		t.Fatalf("W4-7: the adverse confirmation does not name the winning post %q (derived from config, never hardcoded). Page:\n%s", winnerName, page)
	}
	if !strings.Contains(text, w4RecoveryPhrase) {
		t.Fatalf("W4-7: the adverse confirmation carries no recovery sentence (%q) — without it, §6 case 4 is an officer in a loop the design built. Page:\n%s", w4RecoveryPhrase, page)
	}
}

// TestW4CorrectionMadeInKnowledgeOfThePeersTakesEffectAndOrdersNoRadioCall
// — D-92's reversal of W4-7's fixture as it stood at 10: site-b's
// correction is already held, and site-a's officer, seeing it, corrects the
// same event. The local correction WINS at the LESSER key: its `seen` names
// the peer's; the resolved instant is the local one; Effective sits on the
// local entry and NOT on the last key of the chain; the peer's Outcome is
// Superseded by the local key; and the confirmation is rendering 2 — it
// names the peer's correction it supersedes, carries no adverse token and
// orders no radio call, because the correction that stands was made here.
// ⟦was: at 10 this exact construction was W4-7's LOSING fixture — "a
// correction authored at the losing post may not take effect (D-28 Part
// B)"; D-28 Part B is REVERSED by D-92⟧ Kills m-D92a.
func TestW4CorrectionMadeInKnowledgeOfThePeersTakesEffectAndOrdersNoRadioCall(t *testing.T) {
	const (
		peerMS  = w2Base - 2*w2Hour + 20*w2Minute
		localMS = w2Base - 2*w2Hour + 40*w2Minute
	)
	srv, st, target, peer := w4LosingWorld(t, peerMS, "Pat Bell")
	before := w2Watermark(t, st, "site-a", 1)

	rec, cookie := w4PostCorrection(t, srv, w4CorrectionForm(target, w2Wall(localMS), "Ida Aputi"))
	page, _, recorded := w4FollowRecorded(t, srv, rec, cookie)
	local := w4MintedCorrection(t, st, "site-a", 1, before)
	if local.Key() != recorded {
		t.Fatalf("?recorded=%v does not name the minted correction %v", recorded, local.Key())
	}
	// The fixture states its own arrangement (rule 6): the local key is the
	// LESSER one, so fixed order names the peer and disagrees with the rule.
	if !local.Key().Less(peer.Key()) {
		t.Fatalf("fixture defect: the local correction %v is not the lesser key against the peer's %v — fixed order would then agree with D-92 and the test would prove nothing", local.Key(), peer.Key())
	}
	var pl domain.OccurredAtCorrected
	if err := json.Unmarshal(local.Payload, &pl); err != nil {
		t.Fatal(err)
	}
	if len(pl.Seen) != 1 || pl.Seen[0].SiteID != peer.SiteID || pl.Seen[0].NodeEpoch != peer.NodeEpoch || pl.Seen[0].Seq != peer.Seq {
		t.Fatalf("D-92 (the dropped stamp): the local correction carries seen=%v, want exactly the peer's key %v — the correction chain's one head as this post held it", pl.Seen, peer.Key())
	}

	eff, chain, err := w4ResolveBudgetedWeb(t, st, target)
	if err != nil {
		t.Fatalf("ResolveOccurredAt: %v", err)
	}
	if eff.InstantUTCms != localMS || eff.Winner != local.Key() {
		t.Fatalf("W4-7 re-ruled (D-92; m-D92a, fixed order resurrected): resolved = (%d, winner %v), want (%d, %v) — the correction made IN KNOWLEDGE of the peer's wins regardless of site_id; the peer's instant is D-28 Part B's answer, which D-92 reversed", eff.InstantUTCms, eff.Winner, localMS, local.Key())
	}
	if len(chain) != 2 || !chain[0].Key.Less(chain[1].Key) {
		t.Fatalf("VF §4.2: the chain is %+v, want the two corrections in ascending key order", chain)
	}
	if chain[0].Key != local.Key() || chain[0].Status != domain.Effective || chain[1].Key != peer.Key() || chain[1].Status != domain.Superseded {
		t.Fatalf("D-92: chain statuses are %+v, want Effective on the LESSER (local) key and Superseded on the greater (peer) key — Effective is no longer \"the last entry\"", chain)
	}
	pouts, err := domain.Outcome(st, peer.Key())
	if err != nil {
		t.Fatalf("Outcome(peer): %v", err)
	}
	saw := false
	for _, o := range pouts {
		if o.Fact == "occurred_at" {
			saw = true
			if o.Status != domain.Superseded || o.Winner != local.Key() {
				t.Fatalf("D-92: the peer's Outcome is (%v, winner %v), want (Superseded, %v) — superseded by a key LESS than its own, because that key's author saw it", o.Status, o.Winner, local.Key())
			}
		}
	}
	if !saw {
		t.Fatalf("Outcome(peer) reported no occurred_at fact (%+v) — cannot-match is a failure, never a skip", pouts)
	}

	// The page: rendering 2 (T5's register) — the peer it beat is named,
	// no adverse token, no radio order.
	text := w3PageText(page)
	if tok, has := w3HasAdverse(page); has {
		t.Fatalf("D-92 (VF-m7′'s shape): the WINNING correction's confirmation renders supersession language (%q) — adverse words on a clean confirmation are the cry-wolf defect. Page:\n%s", tok, page)
	}
	if strings.Contains(text, w4RecoveryPhrase) {
		t.Fatalf("D-92 / L-1's class: the WINNING correction's confirmation orders a radio call (%q) — there is nothing to radio; the correction that stands was made here. Page:\n%s", w4RecoveryPhrase, page)
	}
	if !strings.Contains(text, "supersedes") {
		t.Fatalf("D-92 / T5: the confirmation does not say what it supersedes — the officer who beat a peer's correction is owed rendering 2, not the terse register. Page:\n%s", page)
	}
	if peerName := strings.ToLower(w4DisplayName(t, srv.Cfg, peer.SiteID)); !strings.Contains(text, peerName) {
		t.Fatalf("D-92 / T5: rendering 2 does not name the superseded post %q (derived from config, never hardcoded). Page:\n%s", peerName, page)
	}
}

// TestW4WinningCorrectionNamesSupersededPeer — T5, the winning post's
// mirror (VF-m6, VF-m7′; reports/W4-RENDERING2-DISPOSITION.md's closing
// brief): POST the winning correction at site-b where the loser's has
// already replicated in. Assert the confirmation names what it supersedes
// — rendering 2's verb, the SUPERSEDED post by config-derived display
// name, and the officer suffix (gated on the loser's non-empty
// recorded_by) — AND the mirror negatives: the posted value is effective
// at rest and the adverse register is ABSENT from the body. A T5 that
// only greps for "supersedes" passes under m7′ and is invalid under
// standing rule 1.
func TestW4WinningCorrectionNamesSupersededPeer(t *testing.T) {
	const (
		loserMS  = w2Base - 2*w2Hour + 40*w2Minute // Ice Road's earlier correction
		winnerMS = w2Base - 2*w2Hour + 20*w2Minute // the value the winning officer posts
	)
	// Build the site-a history: person, check-in, entered checkout, and
	// site-a's OWN correction (the loser-to-be) with non-empty
	// recorded_by — then stand up the site-b post with all of it
	// replicated in.
	stA := w3OpenRegistered(t, "w4-winning-src.db")
	p, err := domain.CreatePerson(stA, "site-a", 1, "Mirror Target", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	stA.Clock = w2Reading(w2Base-2*w2Hour, store.ClockSynced)
	w2CheckIn(t, stA, []domain.Person{p})
	open, err := domain.OpenVisitsAt(stA, "site-a")
	if err != nil || len(open) != 1 {
		t.Fatalf("fixture: open visits = %d (%v), want 1", len(open), err)
	}
	beforeA := w2Watermark(t, stA, "site-a", 1)
	if _, err := domain.CheckOutVisits(stA, "site-a", 1, []string{open[0].VisitID}, nil, w2Base-2*w2Hour+10*w2Minute, "entered"); err != nil {
		t.Fatalf("fixture checkout refused: %v", err)
	}
	outsA := w2EventsOfType(t, w2EventsFrom(t, stA, "site-a", 1, beforeA), "visit_checked_out")
	if len(outsA) != 1 {
		t.Fatalf("fixture: %d visit_checked_out events, want 1", len(outsA))
	}
	target := outsA[0].Key()
	stA.Clock = w2Reading(w2Base-w2Hour, store.ClockSynced)
	loserEnv, err := domain.CorrectOccurredAt(stA, "site-a", 1, domain.TimeCorrection{
		Target: target, CorrectedOccurredAtMS: loserMS, RecordedBy: "Riley Okpik",
	})
	if err != nil {
		t.Fatalf("fixture losing correction refused: %v", err)
	}

	srvB, stB := w3SiteBServer(t, w3Stream(t, stA, "site-a", 1))
	stB.Clock = w2Reading(w2Base, store.ClockSynced)
	beforeB := w2Watermark(t, stB, "site-b", 1)

	rec, cookie := w4PostCorrection(t, srvB, w4CorrectionForm(target, w2Wall(winnerMS), "Pat Bell"))
	page, _, recorded := w4FollowRecorded(t, srvB, rec, cookie)
	winner := w4MintedCorrection(t, stB, "site-b", 1, beforeB)
	if winner.Key() != recorded {
		t.Fatalf("W4-7/T5: ?recorded=%v does not name the minted correction %v", recorded, winner.Key())
	}

	// Mirror negative 1: the posted value IS effective at rest.
	eff, _, err := w4ResolveBudgetedWeb(t, stB, target)
	if err != nil {
		t.Fatalf("W4-7/T5: ResolveOccurredAt: %v", err)
	}
	if eff.InstantUTCms != winnerMS || eff.Winner != winner.Key() || eff.RecordedBy != "Pat Bell" {
		t.Fatalf("W4-7/T5: resolved = (%d, winner %v, %q), want (%d, %v, %q)", eff.InstantUTCms, eff.Winner, eff.RecordedBy, winnerMS, winner.Key(), "Pat Bell")
	}

	text := w3PageText(page)
	// Supersession NAMED (VF-m6, one-sided visibility): rendering 2's
	// verb, the superseded post's derived display name, and the officer
	// suffix — the loser's recorded_by is non-empty, so the suffix is due.
	if !strings.Contains(text, w4SupersedesPhrase) {
		t.Fatalf("W4-7/T5 (VF-m6): the winning confirmation does not name the supersession (%q absent) — the winning officer is the one person present at the moment the peer's loss becomes real (VF §5 rendering 2, §6 case 2). Page:\n%s", w4SupersedesPhrase, page)
	}
	loserName := w4DisplayName(t, srvB.Cfg, loserEnv.Key().SiteID)
	if !strings.Contains(text, strings.ToLower(loserName)) {
		t.Fatalf("W4-7/T5 (VF-m6): the winning confirmation does not attribute the superseded correction to %q (derived from config). Page:\n%s", loserName, page)
	}
	if !strings.Contains(text, strings.ToLower("Riley Okpik")) {
		t.Fatalf("W4-7/T5: the officer suffix is missing — the superseded correction's recorded_by (%q) is non-empty, so rendering 2's correction-carrier suffix is due. Page:\n%s", "Riley Okpik", page)
	}
	// Mirror negative 2 (VF-m7′, the inverted renderer): the adverse
	// register is ABSENT from the winner's body. "supersedes" contains no
	// adverse token, so this and the assertion above can hold together on
	// correct code — and cannot both hold under m7′.
	if tok, has := w3HasAdverse(page); has {
		t.Fatalf("W4-7/T5 (VF-m7′): the WINNING confirmation renders the adverse register (%q) — the inverted renderer tells both officers they lost. Page:\n%s", tok, page)
	}
	if !strings.Contains(text, "effective") {
		t.Fatalf("W4-7/T5: the winning confirmation does not present the posted value as effective (no %q). Page:\n%s", "effective", page)
	}
}

// TestW4CleanCorrectionRendersNoSupersession — T6, the cry-wolf control
// (VF-m7): a correction with NO competitor renders no supersession
// language anywhere — in either register — and no failure rendering. An
// unconditional warning would make T4 self-confirming and burn the
// confirmation's trust (attack 2).
func TestW4CleanCorrectionRendersNoSupersession(t *testing.T) {
	const correctedMS = w2Base - 2*w2Hour + 20*w2Minute
	srv, st, targetEnv, _ := w4CheckedOutWorld(t, w2Base-2*w2Hour, w2Base-2*w2Hour+10*w2Minute)
	before := w2Watermark(t, st, "site-a", 1)
	rec, cookie := w4PostCorrection(t, srv, w4CorrectionForm(targetEnv.Key(), w2Wall(correctedMS), "Ida Aputi"))
	page, _, recorded := w4FollowRecorded(t, srv, rec, cookie)
	corr := w4MintedCorrection(t, st, "site-a", 1, before)
	if corr.Key() != recorded {
		t.Fatalf("W4-7/T6: ?recorded=%v does not name the minted correction %v", recorded, corr.Key())
	}
	if tok, has := w3HasAdverse(page); has {
		t.Fatalf("W4-7/T6 (VF-m7): a competitor-less correction renders supersession language (%q) — the unconditional warning is the cry-wolf shape. Page:\n%s", tok, page)
	}
	if strings.Contains(w3PageText(page), strings.ToLower(w4SupersedesPhrase)) {
		t.Fatalf("W4-7/T6 (VF-m7): a competitor-less correction renders %q — no competitor, no supersession language, in EITHER register. Page:\n%s", w4SupersedesPhrase, page)
	}
	if strings.Contains(w3PageText(page), w3Rendering4Phrase) {
		t.Fatalf("W4-7/T6: the clean confirmation renders the FAILURE path's %q — rendering 4 everywhere would make T4b self-confirming. Page:\n%s", w3Rendering4Phrase, page)
	}
}

// TestW4CorrectionConfirmationFailurePathRendersLoudly — T4b (VF-m5b):
// on THIS surface, a garbled ?recorded= value and an induced Outcome
// error (a well-formed key matching no event) BOTH render VF §5's
// rendering 4 — "outcome could not be determined", adverse-shaped, in
// words — and the terse success register is ABSENT. The append DID
// succeed; what failed is the read-back, and the officer must know the
// confirmation is not vouching for the outcome. The house precedent of
// silently ignoring an unmatched query key (intake.go:359) is explicitly
// not followed here.
func TestW4CorrectionConfirmationFailurePathRendersLoudly(t *testing.T) {
	const correctedMS = w2Base - 2*w2Hour + 20*w2Minute
	srv, _, targetEnv, _ := w4CheckedOutWorld(t, w2Base-2*w2Hour, w2Base-2*w2Hour+10*w2Minute)
	rec, cookie := w4PostCorrection(t, srv, w4CorrectionForm(targetEnv.Key(), w2Wall(correctedMS), "Ida Aputi"))
	page, location, _ := w4FollowRecorded(t, srv, rec, cookie)

	// Guard: the REAL confirmation must not render the failure path, or
	// both probes below pass vacuously.
	if strings.Contains(w3PageText(page), w3Rendering4Phrase) {
		t.Fatalf("W4-7/T4b guard: the clean confirmation already renders %q — rendering 4 everywhere makes this test self-confirming", w3Rendering4Phrase)
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
			if got.Code != http.StatusOK {
				t.Fatalf("W4-7/T4b: %s GET = %d — the confirmation page must render (the append DID succeed); what failed is the read-back, and the officer must be told IN WORDS", p.name, got.Code)
			}
			text := w3PageText(got.Body.String())
			if !strings.Contains(text, w3Rendering4Phrase) {
				t.Fatalf("W4-7/T4b (VF-m5b): %s renders no %q — an outcome block that fails quietly is success-shaped (VF attack 4). Page:\n%s", p.name, w3Rendering4Phrase, got.Body.String())
			}
			for _, tok := range w3TerseSuccessTokens {
				if strings.Contains(text, strings.ToLower(tok)) {
					t.Fatalf("W4-7/T4b (VF-m5b): %s still renders the terse success register (%q) beside the failure rendering — the confirmation may not vouch for an outcome it could not determine", p.name, tok)
				}
			}
		})
	}
}
