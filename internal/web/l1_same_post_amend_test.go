// l1_same_post_amend_test.go — L-1, the LOSING arm's same-post wording on
// BOTH amendment surfaces (person and company). Discharged by the plan
// owner before the bump; ruled at reports/W5-POSTLOCK-AMENDMENTS.md §23 and
// regraded at reports/OWED-CENSUS.md — find it by row text ("L-1 — 'this
// post' wording…"), not by line number: that file is edited concurrently
// and the row has already moved twice.
//
// ⚠ THE COPY THESE TESTS PIN CHANGED BY RULING, NOT TO MAKE CODE PASS.
// Until this pass, the losing arm on a SAME-POST supersession told the
// officer their change "can only be authored at Ice Road — radio it
// through" while they were standing at Ice Road. All FOUR instances were
// PRODUCED 2026-09-01 before a line was edited — both surfaces × both
// losing renderings (value fact and un-merge). The record booked two and
// marked the un-merge pair an inference; it is not one. The rendered bytes
// are in §24 of the post-lock record. That is the third instance in this
// window of one class — a screen ordering a radio call with no recipient — after
// W5d's build critique blocked a commit over one and the operator-journey
// critique's F-1 found the second on the company WINNER's arm.
//
// The fix is a COPY AND SIGNALLING change only. Which value stands, what is
// recorded and what supersedes what are untouched, and each same-post case
// below asserts the resolution outcome as well as the wording, so a future
// edit that "fixes" the sentence by changing the answer fails here.
//
// KILLER MUTANTS, each produced red once against the finished build:
//   - m-L1a: the losing arm renders the cross-post sentence on the
//     same-post arm (the defect as found) — killed by the no-radio
//     assertion, which fatals first; the no-own-name assertion is a second,
//     independent guard on the same arm and would kill it alone.
//   - m-L1b: the same-post arm drops the supersession statement along with
//     the radio clause, leaving a silent-OK — killed by the adverse-token
//     assertion, which is the project's signature defect (D-28 Part B).
//   - m-L1c: the branch fires on the WRONG arm and the CROSS-post case
//     loses its radio instruction — killed by the cross-post subtests,
//     which are the case the visible-failure design exists for.
//
// As deployed "site-b" > "site-a": Pad Site Control is the cross-post
// winner and Ice Road the loser [DERIVED here from the fixture configs,
// never hardcoded]. The same-post fixtures do not contest across posts at
// all — they are amend-then-amend at ONE post, i.e. an officer correcting
// their own typing, which is what makes the radio order absurd there.
//
// ⟦SchemaVersion 11, D-92 — the CROSS-POST arm RE-RULED, not silently
// edited⟧ "site-b > site-a" now decides only between authors who did not
// see each other. The cross-post losing fixture is re-built as that
// concurrent case, and the construction it used at 10 — peer's assertion
// applied, then the local POST — is a correction made IN KNOWLEDGE of the
// peer's, which WINS under D-92 and orders no radio call; both arms are
// witnessed below.
package web

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// l1Dump writes a rendered page verbatim to $L1_CAPTURE_DIR. It is off
// unless the variable is set, and it exists because L-1's discharge turned
// on comparing BYTES before and after the edit rather than on reading the
// template: with it, `L1_CAPTURE_DIR=<dir> go test -run TestL1` re-produces
// the eight pages §24 records, so the claim "the cross-post arm is
// byte-identical" is re-checkable and not merely asserted.
func l1Dump(t *testing.T, name, page string) {
	t.Helper()
	dir := os.Getenv("L1_CAPTURE_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".html"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
}

// l1AssertSamePostLosingNotice makes the four assertions L-1's discharge
// consists of, against the rendered NOTICE ELEMENT (W5-X2's extended form,
// never strings.Contains over the whole body):
//
//	(1) the supersession is still STATED — the adverse register is present,
//	    because the officer must still learn their change did not take
//	    effect, and a fix that only deleted the false clause would leave the
//	    silent-OK this surface exists to refuse;
//	(2) no radio instruction — there is no recipient;
//	(3) this post's OWN configured display name is absent. This is the byte
//	    that made the defect: peerDisplayName answers for the winner's site
//	    id with the post's own name, so the officer at Ice Road was told to
//	    radio "Ice Road", which reads as somewhere else entirely;
//	(4) the same-post form is named in the winner arm's own words.
//
// The expectations are DERIVED from the config the server loaded.
func l1AssertSamePostLosingNotice(t *testing.T, page string, srv *Server, carrying string) {
	t.Helper()
	notice, ok := w5dNoticeCarrying(page, carrying)
	if !ok {
		t.Fatalf("L-1 (m-L1b, silent-OK): the losing arm's confirmation carries no notice element saying %q — the supersession must still be stated; only the radio clause is dropped. Notices: %v", carrying, w5dNoticeTexts(page))
	}
	if _, has := w3HasAdverse(page); !has {
		t.Fatalf("L-1 (m-L1b): the same-post losing arm carries none of the adverse register %v — a change that silently does nothing is the project's signature defect (D-28 Part B). Notice: %q", w3AdverseTokens, notice)
	}
	if strings.Contains(notice, w4RecoveryPhrase) {
		t.Fatalf("L-1 (m-L1a): the SAME-POST losing arm reads %q — it orders a radio call with no recipient. The assertion that stands was authored at THIS post, by this officer or the one beside them; there is nobody to raise. A notice that cries wolf here is the one they will ignore when it fires CROSS-POST, which is the case the visible-failure design was built for. Ruled at W5-POSTLOCK-AMENDMENTS.md §23", notice)
	}
	own := strings.ToLower(srv.Cfg.DisplayName)
	if own == "" {
		t.Fatalf("fixture defect: this post's config must carry a DisplayName, or assertion (3) proves nothing")
	}
	if strings.Contains(notice, own) {
		t.Fatalf("L-1 (m-L1a): the SAME-POST losing arm names this post's own display name %q (%q). The officer is standing at it; naming it reads as a different post and is exactly what made the instruction impossible to follow", srv.Cfg.DisplayName, notice)
	}
	if !strings.Contains(notice, w5dRendering2SamePost) {
		t.Fatalf("L-1: the SAME-POST losing arm does not say %q (%q) — the same-post form is the winner arm's own spelling and this arm must match it", w5dRendering2SamePost, notice)
	}
}

// l1AssertCrossPostLosingNotice is the mirror negative, and it is the point
// of the whole split: on a real cross-post supersession the radio
// instruction MUST still fire, naming the winning peer by its
// config-derived display name.
func l1AssertCrossPostLosingNotice(t *testing.T, page string, srv *Server, peerSiteID, carrying string) {
	t.Helper()
	notice, ok := w5dNoticeCarrying(page, carrying)
	if !ok {
		t.Fatalf("L-1 (m-L1c): the CROSS-POST losing arm carries no notice element saying %q. Notices: %v", carrying, w5dNoticeTexts(page))
	}
	if !strings.Contains(notice, w4RecoveryPhrase) {
		t.Fatalf("L-1 (m-L1c): the CROSS-POST losing arm lost its recovery sentence (%q) — %q. This is the case radioing IS the right instruction for; L-1 narrows the same-post arm and must not touch this one", w4RecoveryPhrase, notice)
	}
	want := strings.ToLower(w5dDisplayNameOf(srv.Cfg, peerSiteID))
	if want == w5dRendering2SamePost {
		t.Fatalf("fixture defect: %q derives as the same-post form under this config — the cross-post subtest is not cross-post", peerSiteID)
	}
	if !strings.Contains(notice, want) {
		t.Fatalf("L-1 (m-L1c): the CROSS-POST losing arm does not name the winning post %q (derived from config, never hardcoded) — %q", want, notice)
	}
}

// l1SamePostWinner asserts the FIXTURE, not the build: the earlier
// assertion must have lost to a winner at THIS post. Without it a
// same-post rendering assertion can pass on a cross-post arm, which is the
// precise failure mode that let L-1 read as discharged for days — the
// criterion body checked, the arm it applies to not.
func l1SamePostWinner(t *testing.T, st *store.Store, srv *Server, k store.Key, fact string) {
	t.Helper()
	outs, err := domain.Outcome(st, k)
	if err != nil {
		t.Fatalf("Outcome(%v): %v", k, err)
	}
	for _, o := range outs {
		if o.Fact != fact {
			continue
		}
		if o.Status != domain.Superseded {
			t.Fatalf("L-1 fixture defect: %q at %v is %v, want Superseded — this test asserts the LOSING arm", fact, k, o.Status)
		}
		if o.Winner.SiteID != srv.Cfg.SiteID {
			t.Fatalf("L-1 fixture defect: the winner's site is %q, want this post's %q — the SAME-POST case requires winner and loser to share a site_id", o.Winner.SiteID, srv.Cfg.SiteID)
		}
		return
	}
	t.Fatalf("L-1: Outcome(%v) reported no %q fact (got %+v) — cannot-match is a FAILURE, never a skip", k, fact, outs)
}

// --- the PERSON surface -------------------------------------------------

// TestL1PersonSamePostLosingAmendmentDoesNotOrderARadioCall — the value-fact
// losing arm. The officer amends a name, someone at the same post amends it
// again, and the officer re-reads their own confirmation (read-time truth,
// VF §4.3 rule 2 — the outcome is recomputed on every GET, so this page is
// reachable by nothing more exotic than a browser back-button).
func TestL1PersonSamePostLosingAmendmentDoesNotOrderARadioCall(t *testing.T) {
	srv, st, x, _ := w3SiteAServer(t)

	first := url.Values{}
	first.Set(w3FieldPersonID, x.PersonID)
	first.Set(w3FieldName, "Alpha One")
	rec, cookie := w3PostAmend(t, srv, first)
	_, location := w3FollowRecorded(t, srv, rec, cookie)

	second := url.Values{}
	second.Set(w3FieldPersonID, x.PersonID)
	second.Set(w3FieldName, "Beta Two")
	rec2, _ := w2PostForm(t, srv, w3AmendPath, second)
	if rec2.Code != 303 {
		t.Fatalf("the second same-post amendment POST = %d, want 303: %s", rec2.Code, rec2.Body.String())
	}

	l1SamePostWinner(t, st, srv, w3RecordedKey(t, location), w3FieldName)

	// BEHAVIOUR, unchanged by this copy edit: the later same-post
	// assertion is what stands.
	p, err := domain.PersonByID(st, x.PersonID)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil || p.Name != "Beta Two" {
		t.Fatalf("L-1 is a COPY change: the composed name is %v, want %q — resolution must be identical before and after", p, "Beta Two")
	}

	again := w2Get(t, srv, location, cookie)
	if again.Code != 200 {
		t.Fatalf("re-GET %s = %d", location, again.Code)
	}
	page := again.Body.String()
	l1Dump(t, "person-samepost-value", page)
	l1AssertSamePostLosingNotice(t, page, srv, "superseded")
}

// TestL1PersonSamePostLosingUnMergeDoesNotOrderARadioCall — the un-merge
// losing arm: merge, un-merge, re-merge, all at one post. The un-merge
// loses to the post's own later merge.
func TestL1PersonSamePostLosingUnMergeDoesNotOrderARadioCall(t *testing.T) {
	srv, st, x, y := w3SiteAServer(t)

	merge := url.Values{}
	merge.Set(w3FieldPersonID, x.PersonID)
	merge.Set(w3FieldSameAs, y.PersonID)
	rec, cookie := w3PostAmend(t, srv, merge)
	if rec.Code != 303 {
		t.Fatalf("the merge POST = %d, want 303: %s", rec.Code, rec.Body.String())
	}

	unmerge := url.Values{}
	unmerge.Set(w3FieldPersonID, x.PersonID)
	unmerge.Set(w3FieldClear, w3FieldSameAs)
	rec2, _ := w2PostForm(t, srv, w3AmendPath, unmerge)
	_, location := w3FollowRecorded(t, srv, rec2, cookie)

	rec3, _ := w2PostForm(t, srv, w3AmendPath, merge)
	if rec3.Code != 303 {
		t.Fatalf("the re-merge POST = %d, want 303: %s", rec3.Code, rec3.Body.String())
	}

	l1SamePostWinner(t, st, srv, w3RecordedKey(t, location), w3FieldSameAs)

	// BEHAVIOUR, unchanged: the records are still merged.
	p, err := domain.PersonByID(st, x.PersonID)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil || p.SameAs == nil || *p.SameAs != y.PersonID {
		t.Fatalf("L-1 is a COPY change: composed same_as = %v, want the standing merge to %s", p, y.PersonID)
	}

	again := w2Get(t, srv, location, cookie)
	if again.Code != 200 {
		t.Fatalf("re-GET %s = %d", location, again.Code)
	}
	page := again.Body.String()
	l1Dump(t, "person-samepost-unmerge", page)
	l1AssertSamePostLosingNotice(t, page, srv, w3RemainMergedPhrase)
}

// TestL1PersonCrossPostLosingArmStillRadios — the arm L-1 must NOT touch.
//
// ⟦SchemaVersion 11, D-92 — RE-RULED, not silently edited⟧ At 10 this
// fixture applied site-b's assertion FIRST and then POSTed site-a's; under
// fixed order site-a lost. Under D-92 that same construction is a
// correction made IN KNOWLEDGE of site-b's — site-a's `seen` names it — and
// site-a WINS (the reversed pin is
// TestL1PersonCrossPostCorrectionMadeInKnowledgeWinsAndOrdersNoRadioCall).
// A cross-post LOSING arm at 11 is the GENUINELY CONCURRENT case: site-a's
// officer POSTs first, site-b's assertion is authored at a post that has
// not seen it and arrives afterwards, and the site_id tiebreak decides —
// the partition conflict radioing exists for. The officer meets it on a
// re-GET of their own confirmation (read-time truth, W3-16).
// ⟦was: site-b applied, then the POST; the page read once⟧
func TestL1PersonCrossPostLosingArmStillRadios(t *testing.T) {
	t.Run("value fact", func(t *testing.T) {
		srv, st, x, _ := w3SiteAServer(t)
		roster := w3Stream(t, st, "site-a", 1) // what site-b holds when the link drops

		form := url.Values{}
		form.Set(w3FieldPersonID, x.PersonID)
		form.Set(w3FieldName, "Local Loser")
		rec, cookie := w3PostAmend(t, srv, form)
		first, location := w3FollowRecorded(t, srv, rec, cookie)
		if tok, has := w3HasAdverse(first); has {
			t.Fatalf("fixture (W3-16, m22): before the peer's assertion arrives the local amendment is Effective, yet its confirmation renders %q", tok)
		}

		winning := "Peer Winner"
		remote := w3RemoteAmend(t, roster, "site-b", w2Base+w2Hour,
			domain.PersonAmendment{PersonID: x.PersonID, Name: &winning})
		w3ApplyWire(t, st, []store.Envelope{remote})
		l1CrossPostWinner(t, st, w3RecordedKey(t, location), w3FieldName, "site-b")

		page := w2Get(t, srv, location, cookie).Body.String()
		l1Dump(t, "person-crosspost-value", page)
		l1AssertCrossPostLosingNotice(t, page, srv, "site-b", "superseded")
	})

	t.Run("un-merge", func(t *testing.T) {
		srv, st, x, y := w3SiteAServer(t)
		roster := w3Stream(t, st, "site-a", 1)
		// The merge BOTH posts hold: authored at site-b, replicated to site-a.
		merge := w3RemoteAmend(t, roster, "site-b", w2Base+w2Hour,
			domain.PersonAmendment{PersonID: x.PersonID, SameAs: &y.PersonID})
		w3ApplyWire(t, st, []store.Envelope{merge})
		held := append(append([]store.Envelope{}, roster...), merge)

		// site-a's officer un-merges...
		form := url.Values{}
		form.Set(w3FieldPersonID, x.PersonID)
		form.Set(w3FieldClear, w3FieldSameAs)
		rec, cookie := w3PostAmend(t, srv, form)
		_, location := w3FollowRecorded(t, srv, rec, cookie)

		// ...while, cut off, site-b's officer un-merges and re-merges. The two
		// STANDING assertions — site-a's un-merge and site-b's re-merge — saw
		// the same history and not each other; site-b's key wins the tie.
		chain := w3RemoteAmendChain(t, held, "site-b", w2Base+2*w2Hour,
			domain.PersonAmendment{PersonID: x.PersonID, Clear: []string{w3FieldSameAs}},
			domain.PersonAmendment{PersonID: x.PersonID, SameAs: &y.PersonID})
		w3ApplyWire(t, st, chain)
		l1CrossPostWinner(t, st, w3RecordedKey(t, location), w3FieldSameAs, "site-b")

		page := w2Get(t, srv, location, cookie).Body.String()
		l1Dump(t, "person-crosspost-unmerge", page)
		l1AssertCrossPostLosingNotice(t, page, srv, "site-b", w3RemainMergedPhrase)
	})
}

// TestL1PersonCrossPostCorrectionMadeInKnowledgeWinsAndOrdersNoRadioCall —
// the D-92 reversal of the fixture above as it stood at 10: site-b's
// assertion arrives FIRST and site-a's officer corrects it. The correction
// WINS (its `seen` names site-b's key), the confirmation is the terse
// "Recorded. Effective:" register, and there is no radio instruction — the
// winner is standing at this post. A build that renders the radio clause on
// every cross-post pair regardless of outcome fails here.
// ⟦was: at 10 this construction was the LOSING arm — "site-b > site-a"⟧
func TestL1PersonCrossPostCorrectionMadeInKnowledgeWinsAndOrdersNoRadioCall(t *testing.T) {
	t.Run("value fact", func(t *testing.T) {
		srv, st, x, _ := w3SiteAServer(t)
		earlier := "Peer Earlier"
		remote := w3RemoteAmend(t, w3Stream(t, st, "site-a", 1), "site-b", w2Base+3*w2Hour,
			domain.PersonAmendment{PersonID: x.PersonID, Name: &earlier})
		w3ApplyWire(t, st, []store.Envelope{remote})

		form := url.Values{}
		form.Set(w3FieldPersonID, x.PersonID)
		form.Set(w3FieldName, "Local Corrected")
		rec, cookie := w3PostAmend(t, srv, form)
		page, location := w3FollowRecorded(t, srv, rec, cookie)
		l1Dump(t, "person-crosspost-value-causal", page)

		l1AssertCausalWinner(t, st, w3RecordedKey(t, location), remote.Key(), w3FieldName)
		p, err := domain.PersonByID(st, x.PersonID)
		if err != nil {
			t.Fatal(err)
		}
		if p == nil || p.Name != "Local Corrected" {
			t.Fatalf("D-92: composed name = %v, want the correction made in knowledge of site-b's, %q", p, "Local Corrected")
		}
		l1AssertEffectiveNotice(t, page, w3FieldName)
	})

	t.Run("un-merge", func(t *testing.T) {
		srv, st, x, y := w3SiteAServer(t)
		merge := w3RemoteAmend(t, w3Stream(t, st, "site-a", 1), "site-b", w2Base+3*w2Hour,
			domain.PersonAmendment{PersonID: x.PersonID, SameAs: &y.PersonID})
		w3ApplyWire(t, st, []store.Envelope{merge})

		form := url.Values{}
		form.Set(w3FieldPersonID, x.PersonID)
		form.Set(w3FieldClear, w3FieldSameAs)
		rec, cookie := w3PostAmend(t, srv, form)
		page, location := w3FollowRecorded(t, srv, rec, cookie)
		l1Dump(t, "person-crosspost-unmerge-causal", page)

		l1AssertCausalWinner(t, st, w3RecordedKey(t, location), merge.Key(), w3FieldSameAs)
		p, err := domain.PersonByID(st, x.PersonID)
		if err != nil {
			t.Fatal(err)
		}
		if p == nil || p.SameAs != nil {
			t.Fatalf("D-92: composed same_as = %v, want absent — the un-merge made in knowledge of the merge takes effect", p)
		}
		l1AssertEffectiveNotice(t, page, w3FieldSameAs)
	})
}

// l1CrossPostWinner asserts the FIXTURE of a cross-post losing arm at 11:
// the recorded assertion lost, and to the named PEER — the concurrent
// case. It is l1SamePostWinner's mirror.
func l1CrossPostWinner(t *testing.T, st *store.Store, k store.Key, fact, peer string) {
	t.Helper()
	outs, err := domain.Outcome(st, k)
	if err != nil {
		t.Fatalf("Outcome(%v): %v", k, err)
	}
	for _, o := range outs {
		if o.Fact != fact {
			continue
		}
		if o.Status != domain.Superseded {
			t.Fatalf("L-1 fixture defect (D-92): %q at %v is %v, want Superseded — the CROSS-POST losing arm needs a genuinely concurrent pair; a local amendment that SAW the peer's wins under D-92 and is not a losing arm", fact, k, o.Status)
		}
		if o.Winner.SiteID != peer {
			t.Fatalf("L-1 fixture defect: the winner's site is %q, want the peer %q", o.Winner.SiteID, peer)
		}
		return
	}
	t.Fatalf("L-1: Outcome(%v) reported no %q fact (got %+v) — cannot-match is a FAILURE, never a skip", k, fact, outs)
}

// l1AssertCausalWinner asserts D-92's verdict pair: the local key Effective,
// the peer's key Superseded BY the local key — which is the LESSER key.
func l1AssertCausalWinner(t *testing.T, st *store.Store, local, peer store.Key, fact string) {
	t.Helper()
	if !local.Less(peer) {
		t.Fatalf("fixture defect: the local key %v is not less than the peer's %v — fixed order would then agree with D-92 and the test would prove nothing", local, peer)
	}
	for _, c := range []struct {
		name string
		k    store.Key
		want domain.Status
		win  store.Key
	}{{"local correction", local, domain.Effective, local}, {"peer's earlier assertion", peer, domain.Superseded, local}} {
		outs, err := domain.Outcome(st, c.k)
		if err != nil {
			t.Fatalf("Outcome(%v): %v", c.k, err)
		}
		found := false
		for _, o := range outs {
			if o.Fact != fact {
				continue
			}
			found = true
			if o.Status != c.want || o.Winner != c.win {
				t.Fatalf("D-92 (m-D92a, fixed order resurrected): %s %q is (%v, winner %v), want (%v, %v) — the correction made in knowledge of the earlier one wins regardless of site_id", c.name, fact, o.Status, o.Winner, c.want, c.win)
			}
		}
		if !found {
			t.Fatalf("Outcome(%v) reported no %q fact (got %+v)", c.k, fact, outs)
		}
	}
}

// l1AssertEffectiveNotice: the terse success register naming the fact, no
// adverse token anywhere on the page, and no radio instruction — the
// winner is standing at this post.
func l1AssertEffectiveNotice(t *testing.T, page, fact string) {
	t.Helper()
	notice, ok := w5dNoticeCarrying(page, "effective")
	if !ok {
		t.Fatalf("D-92 / VF §5: the WINNING correction's confirmation carries no notice saying \"effective\". Notices: %v", w5dNoticeTexts(page))
	}
	if !strings.Contains(notice, fact) {
		t.Fatalf("D-92: the effective notice does not name the fact %q: %q", fact, notice)
	}
	if tok, has := w3HasAdverse(page); has {
		t.Fatalf("D-92 (m12's shape): the WINNING correction's confirmation renders supersession language (%q) — adverse words on a clean confirmation are the cry-wolf defect. Page:\n%s", tok, page)
	}
	if strings.Contains(w3PageText(page), w4RecoveryPhrase) {
		t.Fatalf("D-92 / L-1: the WINNING correction's confirmation orders a radio call (%q) — there is nothing to radio; the officer's own correction stands. Page:\n%s", w4RecoveryPhrase, page)
	}
}

// --- the COMPANY surface ------------------------------------------------

// TestL1CompanySamePostLosingAmendmentDoesNotOrderARadioCall — the same
// defect one surface over. ⚠ L-1 is NOT discharged by fixing one of them:
// the census read as carried while being half-delivered once already.
func TestL1CompanySamePostLosingAmendmentDoesNotOrderARadioCall(t *testing.T) {
	srv, st, x, _ := w5dSiteAServer(t)

	first := url.Values{}
	first.Set(w5dFieldCompanyID, x.CompanyID)
	first.Set(w5dFieldName, "Alpha Ltd")
	rec, cookie := w5dPostAmend(t, srv, first)
	_, location := w3FollowRecorded(t, srv, rec, cookie)

	second := url.Values{}
	second.Set(w5dFieldCompanyID, x.CompanyID)
	second.Set(w5dFieldName, "Beta Ltd")
	rec2, _ := w2PostForm(t, srv, w5dAmendPath, second)
	if rec2.Code != 303 {
		t.Fatalf("the second same-post company amendment POST = %d, want 303: %s", rec2.Code, rec2.Body.String())
	}

	l1SamePostWinner(t, st, srv, w3RecordedKey(t, location), w5dFieldName)

	c, err := domain.CompanyByID(st, x.CompanyID)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil || c.Name != "Beta Ltd" {
		t.Fatalf("L-1 is a COPY change: the composed name is %v, want %q — resolution must be identical before and after", c, "Beta Ltd")
	}

	again := w2Get(t, srv, location, cookie)
	if again.Code != 200 {
		t.Fatalf("re-GET %s = %d", location, again.Code)
	}
	page := again.Body.String()
	l1Dump(t, "company-samepost-value", page)
	l1AssertSamePostLosingNotice(t, page, srv, "superseded")
}

// TestL1CompanySamePostLosingUnMergeDoesNotOrderARadioCall — the company
// un-merge losing arm, same shape as the person one.
func TestL1CompanySamePostLosingUnMergeDoesNotOrderARadioCall(t *testing.T) {
	srv, st, x, y := w5dSiteAServer(t)

	merge := url.Values{}
	merge.Set(w5dFieldCompanyID, x.CompanyID)
	merge.Set(w5dFieldSameAs, y.CompanyID)
	rec, cookie := w5dPostAmend(t, srv, merge)
	if rec.Code != 303 {
		t.Fatalf("the merge POST = %d, want 303: %s", rec.Code, rec.Body.String())
	}

	unmerge := url.Values{}
	unmerge.Set(w5dFieldCompanyID, x.CompanyID)
	unmerge.Set(w5dFieldClear, w5dFieldSameAs)
	rec2, _ := w2PostForm(t, srv, w5dAmendPath, unmerge)
	_, location := w3FollowRecorded(t, srv, rec2, cookie)

	rec3, _ := w2PostForm(t, srv, w5dAmendPath, merge)
	if rec3.Code != 303 {
		t.Fatalf("the re-merge POST = %d, want 303: %s", rec3.Code, rec3.Body.String())
	}

	l1SamePostWinner(t, st, srv, w3RecordedKey(t, location), w5dFieldSameAs)

	c, err := domain.CompanyByID(st, x.CompanyID)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil || c.SameAs == nil || *c.SameAs != y.CompanyID {
		t.Fatalf("L-1 is a COPY change: composed same_as = %v, want the standing merge to %s", c, y.CompanyID)
	}

	again := w2Get(t, srv, location, cookie)
	if again.Code != 200 {
		t.Fatalf("re-GET %s = %d", location, again.Code)
	}
	page := again.Body.String()
	l1Dump(t, "company-samepost-unmerge", page)
	l1AssertSamePostLosingNotice(t, page, srv, w3RemainMergedPhrase)
}

// TestL1CompanyCrossPostLosingArmStillRadios — the company mirror negative.
//
// ⟦SchemaVersion 11, D-92 — RE-RULED, not silently edited, exactly as the
// person arm was at step 2 of the increment⟧ At 10 this fixture applied
// site-b's assertion FIRST and then POSTed site-a's; under D-92 that
// construction is a correction made IN KNOWLEDGE of the peer's and site-a
// WINS (the reversed pin is
// TestL1CompanyCrossPostCorrectionMadeInKnowledgeWinsAndTellsItsAuthor).
// The losing arm is the GENUINELY CONCURRENT case: site-a's officer POSTs
// first, site-b's assertion is authored at a post that has not seen it and
// arrives afterwards, and the site_id tiebreak decides. The officer meets
// it on a re-GET of their own confirmation (read-time truth).
// ⟦was: site-b applied, then the POST; the page read once⟧
func TestL1CompanyCrossPostLosingArmStillRadios(t *testing.T) {
	t.Run("value fact", func(t *testing.T) {
		srv, st, x, _ := w5dSiteAServer(t)
		roster := w3Stream(t, st, "site-a", 1) // what site-b holds when the link drops

		form := url.Values{}
		form.Set(w5dFieldCompanyID, x.CompanyID)
		form.Set(w5dFieldName, "Local Loser Ltd")
		rec, cookie := w5dPostAmend(t, srv, form)
		first, location := w3FollowRecorded(t, srv, rec, cookie)
		if tok, has := w3HasAdverse(first); has {
			t.Fatalf("fixture (VF §4.3 rule 2): before the peer's assertion arrives the local amendment is Effective, yet its confirmation renders %q", tok)
		}

		winning := "Peer Winner Ltd"
		remote := w5dRemoteAmend(t, roster, "site-b", w2Base+w2Hour,
			domain.CompanyAmendment{CompanyID: x.CompanyID, Name: &winning})
		w3ApplyWire(t, st, []store.Envelope{remote})
		l1CrossPostWinner(t, st, w3RecordedKey(t, location), w5dFieldName, "site-b")

		page := w2Get(t, srv, location, cookie).Body.String()
		l1Dump(t, "company-crosspost-value", page)
		l1AssertCrossPostLosingNotice(t, page, srv, "site-b", "superseded")
	})

	t.Run("un-merge", func(t *testing.T) {
		srv, st, x, y := w5dSiteAServer(t)
		roster := w3Stream(t, st, "site-a", 1)
		// The merge BOTH posts hold: authored at site-b, replicated to site-a.
		merge := w5dRemoteAmend(t, roster, "site-b", w2Base+w2Hour,
			domain.CompanyAmendment{CompanyID: x.CompanyID, SameAs: &y.CompanyID})
		w3ApplyWire(t, st, []store.Envelope{merge})
		held := append(append([]store.Envelope{}, roster...), merge)

		// site-a's officer un-merges...
		form := url.Values{}
		form.Set(w5dFieldCompanyID, x.CompanyID)
		form.Set(w5dFieldClear, w5dFieldSameAs)
		rec, cookie := w5dPostAmend(t, srv, form)
		_, location := w3FollowRecorded(t, srv, rec, cookie)

		// ...while, cut off, site-b's officer un-merges and re-merges. The two
		// STANDING assertions — site-a's un-merge and site-b's re-merge — saw
		// the same history and not each other; site-b's key wins the tie.
		chain := w5dRemoteAmendChain(t, held, "site-b", w2Base+2*w2Hour,
			domain.CompanyAmendment{CompanyID: x.CompanyID, Clear: []string{w5dFieldSameAs}},
			domain.CompanyAmendment{CompanyID: x.CompanyID, SameAs: &y.CompanyID})
		w3ApplyWire(t, st, chain)
		l1CrossPostWinner(t, st, w3RecordedKey(t, location), w5dFieldSameAs, "site-b")

		page := w2Get(t, srv, location, cookie).Body.String()
		l1Dump(t, "company-crosspost-unmerge", page)
		l1AssertCrossPostLosingNotice(t, page, srv, "site-b", w3RemainMergedPhrase)
	})
}

// TestL1CompanyCrossPostCorrectionMadeInKnowledgeWinsAndTellsItsAuthor — the
// D-92 reversal of the company fixture above as it stood at 10: site-b's
// assertion arrives FIRST and site-a's officer corrects it. The correction
// WINS (its `seen` names site-b's key) and the confirmation is THIS
// surface's winning register: W5d-9(a)'s rendering 2 ([[D-53]] FIX — the
// winning officer is told their amendment took the place of the peer's),
// naming the peer, with no adverse token. ⚠ Unlike the person twin
// (TestL1PersonCrossPostCorrectionMadeInKnowledgeWinsAndOrdersNoRadioCall,
// whose surface renders only the terse register), the company surface's
// cross-post rendering 2 KEEPS the radio clause by ruling — W5d-9(a)'s own
// same-post test pins "the cross-post arm keeps the clause" — and here the
// clause has a real recipient: the officer at Pad Site Control whose
// amendment was just superseded and whose screen will not tell them. That
// is D-53's instruction, not L-1's defect (a radio order with NO recipient).
// ⟦was: at 10 this construction was the LOSING arm — "site-b > site-a"⟧
func TestL1CompanyCrossPostCorrectionMadeInKnowledgeWinsAndTellsItsAuthor(t *testing.T) {
	t.Run("value fact", func(t *testing.T) {
		srv, st, x, _ := w5dSiteAServer(t)
		earlier := "Peer Earlier Ltd"
		remote := w5dRemoteAmend(t, w3Stream(t, st, "site-a", 1), "site-b", w2Base+3*w2Hour,
			domain.CompanyAmendment{CompanyID: x.CompanyID, Name: &earlier})
		w3ApplyWire(t, st, []store.Envelope{remote})

		form := url.Values{}
		form.Set(w5dFieldCompanyID, x.CompanyID)
		form.Set(w5dFieldName, "Local Corrected Ltd")
		rec, cookie := w5dPostAmend(t, srv, form)
		page, location := w3FollowRecorded(t, srv, rec, cookie)
		l1Dump(t, "company-crosspost-value-causal", page)

		l1AssertCausalWinner(t, st, w3RecordedKey(t, location), remote.Key(), w5dFieldName)
		c, err := domain.CompanyByID(st, x.CompanyID)
		if err != nil {
			t.Fatal(err)
		}
		if c == nil || c.Name != "Local Corrected Ltd" {
			t.Fatalf("D-92: composed company = %v, want the correction made in knowledge of site-b's, %q", c, "Local Corrected Ltd")
		}
		notice := w5dAssertRendering2(t, page, srv.Cfg, "site-b")
		if !strings.Contains(notice, w4RecoveryPhrase) {
			t.Fatalf("D-53 / W5d-9(a): the cross-post winning notice lost its radio clause (%q) — the officer at the superseded post is not being told by any screen, and this is the case the clause exists for: %q", w4RecoveryPhrase, notice)
		}
		if tok, has := w3HasAdverse(page); has {
			t.Fatalf("D-92 (VF-m7′'s shape): the WINNING correction's confirmation renders the adverse register (%q). Page:\n%s", tok, page)
		}
	})

	t.Run("un-merge", func(t *testing.T) {
		srv, st, x, y := w5dSiteAServer(t)
		merge := w5dRemoteAmend(t, w3Stream(t, st, "site-a", 1), "site-b", w2Base+3*w2Hour,
			domain.CompanyAmendment{CompanyID: x.CompanyID, SameAs: &y.CompanyID})
		w3ApplyWire(t, st, []store.Envelope{merge})

		form := url.Values{}
		form.Set(w5dFieldCompanyID, x.CompanyID)
		form.Set(w5dFieldClear, w5dFieldSameAs)
		rec, cookie := w5dPostAmend(t, srv, form)
		page, location := w3FollowRecorded(t, srv, rec, cookie)
		l1Dump(t, "company-crosspost-unmerge-causal", page)

		l1AssertCausalWinner(t, st, w3RecordedKey(t, location), merge.Key(), w5dFieldSameAs)
		c, err := domain.CompanyByID(st, x.CompanyID)
		if err != nil {
			t.Fatal(err)
		}
		if c == nil || c.SameAs != nil {
			t.Fatalf("D-92: composed company = %v, want same_as absent — the un-merge made in knowledge of the merge takes effect", c)
		}
		notice := w5dAssertRendering2(t, page, srv.Cfg, "site-b")
		if !strings.Contains(notice, w4RecoveryPhrase) {
			t.Fatalf("D-53 / W5d-9(a): the cross-post winning notice lost its radio clause (%q): %q", w4RecoveryPhrase, notice)
		}
		if tok, has := w3HasAdverse(page); has {
			t.Fatalf("D-92 (VF-m7′'s shape): the WINNING un-merge's confirmation renders the adverse register (%q). Page:\n%s", tok, page)
		}
	})
}
