// l1b_time_correct_same_post_test.go — the class's FIFTH instance: the
// TIME-CORRECTION surface's losing arm on a SAME-POST winner. Discharged by
// the plan owner before the bump, on the ruling recorded at
// reports/W5-POSTLOCK-AMENDMENTS.md §26; found and left unowned at §24.6.
//
// ⚠ THE COPY THIS TEST PINS CHANGED BY RULING, NOT TO MAKE CODE PASS.
// Until this pass, a correction superseded by a LATER CORRECTION AT THE
// SAME POST told the officer it "can only be authored at Ice Road A —
// radio it through" while they were standing at Ice Road A. PRODUCED
// 2026-09-01 before a line was edited; the rendered bytes are in §26.
//
// ⚠ THIS IS NOT L-1. L-1's subject is the two amendment surfaces and it is
// discharged (§24, `l1_same_post_amend_test.go`). This is the same CLASS —
// a screen ordering a radio call with no recipient — on a fifth screen, and
// the reason it is fixed in the same window rather than after is
// uniformity: with the two amendment surfaces and their un-merge twins
// corrected, leaving this one would mean the same officer, making the same
// kind of mistake, gets a true answer or a false one depending only on
// which screen they were standing at.
//
// The fix is a COPY AND SIGNALLING change only. Which instant is effective,
// what is recorded and what supersedes what are identical before and after,
// and the test asserts the resolved instant and the winner's key before it
// looks at any wording.
//
// KILLER MUTANTS, each produced red once against the finished build:
//   - m-L1B-a: the same-post arm renders the cross-post sentence (the
//     defect as found).
//   - m-L1B-b: the same-post arm drops the supersession statement with the
//     radio clause, leaving a silent-OK.
//   - m-L1B-c: the signal is inverted, so the CROSS-post arm loses the
//     recovery sentence — the case VF §6 case 4 exists for.
//
// The assertion helpers are L-1's own (`l1AssertSamePostLosingNotice` /
// `l1AssertCrossPostLosingNotice`), reused deliberately and not re-written:
// one shape across all three surfaces is the property this pass is for.
// ⚠ A consequence worth knowing before reading a failure here: the shared
// helpers carry L-1's OWN mutant labels, so a red on this surface prints
// "m-L1a" / "m-L1b" / "m-L1c". The mutant ids above are this file's names
// for the same three injections applied to the correction surface; the
// message's label names the helper that caught it, not the mutant that was
// injected. The helpers were left unrenamed on purpose — renaming them
// would edit L-1's discharged assertions to no operator benefit.
package web

import (
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// TestL1BTimeCorrectSamePostLosingArmDoesNotOrderARadioCall — correct one
// event's time, then correct it again AT THE SAME POST, then re-read the
// first correction's own confirmation (read-time truth, VF §4.3 rule 2).
func TestL1BTimeCorrectSamePostLosingArmDoesNotOrderARadioCall(t *testing.T) {
	const (
		mistypedMS = w2Base - 2*w2Hour + 10*w2Minute
		firstMS    = w2Base - 2*w2Hour + 20*w2Minute // the correction that LOSES
		secondMS   = w2Base - 2*w2Hour + 35*w2Minute // the same post's later one
	)
	srv, st, targetEnv, _ := w4CheckedOutWorld(t, w2Base-2*w2Hour, mistypedMS)
	target := targetEnv.Key()

	beforeFirst := w2Watermark(t, st, "site-a", 1)
	rec, cookie := w4PostCorrection(t, srv, w4CorrectionForm(target, w2Wall(firstMS), "Ida Aputi"))
	_, location, _ := w4FollowRecorded(t, srv, rec, cookie)
	loser := w4MintedCorrection(t, st, "site-a", 1, beforeFirst)

	beforeSecond := w2Watermark(t, st, "site-a", 1)
	rec2, _ := w4PostCorrection(t, srv, w4CorrectionForm(target, w2Wall(secondMS), "Ida Aputi"))
	if rec2.Code != 303 {
		t.Fatalf("the second same-post correction POST = %d, want 303: %s", rec2.Code, rec2.Body.String())
	}
	winner := w4MintedCorrection(t, st, "site-a", 1, beforeSecond)

	// ⚠ FIXTURE PRECONDITION, asserted before any wording is looked at.
	// Getting the arm wrong is how L-1 read as discharged for days: a
	// same-post rendering assertion made on a cross-post arm passes.
	eff, _, err := w4ResolveBudgetedWeb(t, st, target)
	if err != nil {
		t.Fatalf("ResolveOccurredAt: %v", err)
	}
	if eff.Winner != winner.Key() {
		t.Fatalf("fixture defect: the effective correction is %v, want the SECOND same-post correction %v — the later key at one site must win", eff.Winner, winner.Key())
	}
	if eff.Winner.SiteID != srv.Cfg.SiteID {
		t.Fatalf("fixture defect: the winner's site is %q, want this post's %q — the SAME-POST case requires winner and loser to share a site_id", eff.Winner.SiteID, srv.Cfg.SiteID)
	}
	l1BLoserIsSuperseded(t, st, loser.Key(), srv)

	// BEHAVIOUR, identical before and after this copy edit: the later
	// same-post correction is the one in effect, byte-faithfully.
	if eff.InstantUTCms != secondMS {
		t.Fatalf("this is a COPY change: the resolved instant is %d, want the later same-post correction's %d — resolution must be identical before and after", eff.InstantUTCms, secondMS)
	}

	again := w2Get(t, srv, location, cookie)
	if again.Code != 200 {
		t.Fatalf("re-GET %s = %d: %s", location, again.Code, again.Body.String())
	}
	page := again.Body.String()
	l1Dump(t, "timecorrect-samepost", page)
	l1AssertSamePostLosingNotice(t, page, srv, "superseded")

	// The officer's own value is still handed back to them — the fix drops
	// the radio clause, not the record of what they typed. The expected
	// string is derived TEST-SIDE from the posted wall value (the surface
	// displays `2006-01-02 15:04` where the form posts `…T15:04`), never
	// from timeCorrectWall, so the production renderer is not asked to
	// certify itself.
	own := strings.Replace(w2Wall(firstMS), "T", " ", 1)
	notice, _ := w5dNoticeCarrying(page, "superseded")
	if !strings.Contains(notice, strings.ToLower(own)) {
		t.Fatalf("L-1's class / §26: the same-post losing arm no longer returns the officer's own typed value %q — only the recovery sentence was ruled false, not this. Notice: %q", own, notice)
	}
}

// TestL1BTimeCorrectCrossPostLosingArmStillRadios — the arm this pass must
// NOT touch, and the reason the split exists: a correction beaten by a
// PEER post is exactly the case radioing is the right instruction for.
//
// ⟦SchemaVersion 11, D-92 — RE-RULED, not silently edited⟧ At 10 this
// fixture applied site-b's correction FIRST and then POSTed site-a's; under
// D-92 that construction is a correction made IN KNOWLEDGE of the peer's,
// which WINS and orders no radio call (the reversed pin follows). A
// cross-post LOSING arm at 11 is the GENUINELY CONCURRENT case: site-a's
// officer POSTs first, site-b's correction is authored at a post that has
// not seen it and arrives afterwards, and the site_id tiebreak decides —
// the partition conflict radioing exists for. The officer meets it on a
// re-GET of their own confirmation (read-time truth, VF §4.3 rule 2).
// ⟦was: site-b applied, then the POST; the page read once⟧
func TestL1BTimeCorrectCrossPostLosingArmStillRadios(t *testing.T) {
	const (
		winnerMS = w2Base - 2*w2Hour + 20*w2Minute
		loserMS  = w2Base - 2*w2Hour + 40*w2Minute
	)
	srv, st, target := w4UncorrectedWorld(t)
	held := w3Stream(t, st, "site-a", 1) // what Pad Site Control holds when the link drops

	rec, cookie := w4PostCorrection(t, srv, w4CorrectionForm(target, w2Wall(loserMS), "Ida Aputi"))
	first, location, _ := w4FollowRecorded(t, srv, rec, cookie)
	if tok, has := w3HasAdverse(first); has {
		t.Fatalf("fixture (VF §4.3 rule 2): before the peer's correction arrives the local one is Effective, yet its confirmation renders %q", tok)
	}

	winnerEnv := w4RemoteCorrection(t, held, "site-b", w2Base-w2Hour, target, winnerMS, "Pat Bell")
	w3ApplyWire(t, st, []store.Envelope{winnerEnv})

	eff, _, err := w4ResolveBudgetedWeb(t, st, target)
	if err != nil {
		t.Fatalf("ResolveOccurredAt: %v", err)
	}
	if eff.Winner != winnerEnv.Key() || eff.Winner.SiteID == srv.Cfg.SiteID {
		t.Fatalf("fixture defect: the winner is %v (site %q), want the PEER's %v — this subtest is not cross-post (D-92: a local correction that SAW the peer's wins and is not a losing arm)", eff.Winner, eff.Winner.SiteID, winnerEnv.Key())
	}

	page := w2Get(t, srv, location, cookie).Body.String()
	l1Dump(t, "timecorrect-crosspost", page)
	l1AssertCrossPostLosingNotice(t, page, srv, "site-b", "superseded")
}

// TestL1BTimeCorrectCrossPostCorrectionMadeInKnowledgeOrdersNoRadioCall —
// the D-92 reversal of the fixture above as it stood at 10: site-b's
// correction is already held, and site-a's officer corrects the same event.
// The local correction WINS; the confirmation names what it superseded and
// carries NO radio instruction and no adverse token — the correction that
// stands was made at this post. A build that renders the radio clause on
// every cross-post pair regardless of outcome fails here.
// ⟦was: at 10 this construction was the LOSING arm — "site-b > site-a"⟧
func TestL1BTimeCorrectCrossPostCorrectionMadeInKnowledgeOrdersNoRadioCall(t *testing.T) {
	const (
		peerMS  = w2Base - 2*w2Hour + 20*w2Minute
		localMS = w2Base - 2*w2Hour + 40*w2Minute
	)
	srv, st, target, peerEnv := w4LosingWorld(t, peerMS, "Pat Bell")

	rec, cookie := w4PostCorrection(t, srv, w4CorrectionForm(target, w2Wall(localMS), "Ida Aputi"))
	page, _, recorded := w4FollowRecorded(t, srv, rec, cookie)

	eff, _, err := w4ResolveBudgetedWeb(t, st, target)
	if err != nil {
		t.Fatalf("ResolveOccurredAt: %v", err)
	}
	if eff.Winner != recorded || eff.InstantUTCms != localMS {
		t.Fatalf("D-92 (m-D92a): resolved = (%d, winner %v), want the local correction (%d, %v) — made in knowledge of %v, it wins regardless of site_id", eff.InstantUTCms, eff.Winner, localMS, recorded, peerEnv.Key())
	}
	if !recorded.Less(peerEnv.Key()) {
		t.Fatalf("fixture defect: the local key %v is not less than the peer's %v — fixed order would then agree with D-92", recorded, peerEnv.Key())
	}

	l1Dump(t, "timecorrect-crosspost-causal", page)
	text := w3PageText(page)
	if strings.Contains(text, w4RecoveryPhrase) {
		t.Fatalf("D-92 / L-1's class: the WINNING correction's confirmation orders a radio call (%q) — there is nobody to radio; the correction that stands was made here. Page:\n%s", w4RecoveryPhrase, page)
	}
	if tok, has := w3HasAdverse(page); has {
		t.Fatalf("D-92: the WINNING correction's confirmation renders supersession language (%q) — the cry-wolf defect. Page:\n%s", tok, page)
	}
	if !strings.Contains(text, "supersedes") {
		t.Fatalf("D-92 / T5: the confirmation does not name what it superseded — rendering 2 is owed to the officer who beat a peer's correction. Page:\n%s", page)
	}
}

// l1BLoserIsSuperseded asserts the queried correction's own per-fact
// outcome is Superseded with a winner at THIS post — the second,
// independent reading of the arm (the resolver's, not the page's).
func l1BLoserIsSuperseded(t *testing.T, st *store.Store, k store.Key, srv *Server) {
	t.Helper()
	outs, err := domain.Outcome(st, k)
	if err != nil {
		t.Fatalf("Outcome(%v): %v", k, err)
	}
	for _, o := range outs {
		if o.Fact != "occurred_at" {
			continue
		}
		if o.Status != domain.Superseded {
			t.Fatalf("fixture defect: the first correction's outcome is %v, want Superseded — this test asserts the LOSING arm", o.Status)
		}
		if o.Winner.SiteID != srv.Cfg.SiteID {
			t.Fatalf("fixture defect: Outcome's winner site is %q, want this post's %q", o.Winner.SiteID, srv.Cfg.SiteID)
		}
		return
	}
	t.Fatalf("Outcome(%v) reported no occurred_at fact (got %+v) — cannot-match is a FAILURE, never a skip", k, outs)
}
