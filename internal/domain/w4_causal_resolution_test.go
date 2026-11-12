// w4_causal_resolution_test.go — the correction carrier under [[D-92]] at
// SchemaVersion 11: W4-3 RE-LOCKED — resolution is CAUSAL, then
// latest-per-target in record order. This is the SEVENTH payload to carry
// `seen`, and the one that is NOT a mechanical clone of the amendment
// template, so the witnesses pin what dominance MEANS here:
//
//   - The contest is the ORIGINAL event's single `occurred_at` fact, and
//     the contestants are every correction in the original's chain — W4-11
//     unchanged: target references LOCATE the chain, they are NOT the
//     causal reference (a form is opened on whatever key the officer
//     navigated from, which is a UI accident, not knowledge).
//   - The causal reference is `seen`, stamped by CorrectOccurredAt from the
//     chain's undominated heads as the authoring post resolves it.
//   - ResolveOccurredAt's winner is resolveContest's; the chain is still
//     returned in ascending key order (VF §4.2) with Effective on the winner
//     WHEREVER it sits — "the last entry" is no longer the rule.
//   - A dangling target stamps whatever is present (nothing, usually) and
//     still lands; the read stays fail-closed until the target replicates.
//
// KILLER MUTANTS, each PRODUCED red once against this tree before commit
// (reports/witness/STEP3-CLONE.md): m-D92a (shared), m-D92b-correction
// (CorrectOccurredAt drops the stamp), m-D92c-correction (ResolveOccurredAt
// falls back to "last in the chain").
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// w4RemoteCorrect authors one correction as siteID at a scratch post primed
// with `primer`, through the REAL edge (so its stamp is that post's own
// reading of the chain), and returns it.
func w4RemoteCorrect(t *testing.T, primer []store.Envelope, siteID string, clockMS int64, target store.Key, correctedMS int64, recordedBy string) store.Envelope {
	t.Helper()
	return d92Remote(t, primer, siteID, clockMS, func(rs *store.Store) {
		if _, err := CorrectOccurredAt(rs, siteID, 1, TimeCorrection{Target: target, CorrectedOccurredAtMS: correctedMS, RecordedBy: recordedBy}); err != nil {
			t.Fatal(err)
		}
	})
}

// w4RequireChainStatuses asserts the chain is in ascending key order and
// Effective sits on exactly `winner`.
func w4RequireChainStatuses(t *testing.T, chain []Correction, winner store.Key) {
	t.Helper()
	effective := 0
	for i, c := range chain {
		if i > 0 && !chain[i-1].Key.Less(c.Key) {
			t.Fatalf("VF §4.2: the chain is not in ascending key order: %+v", chain)
		}
		if c.Key == winner {
			if c.Status != Effective {
				t.Fatalf("D-92: the winner %v is %v in the chain, want Effective", winner, c.Status)
			}
			effective++
		} else if c.Status != Superseded {
			t.Fatalf("D-92: %v is %v in the chain, want Superseded (the winner is %v)", c.Key, c.Status, winner)
		}
	}
	if effective != 1 {
		t.Fatalf("D-92: %d Effective entries in the chain, want exactly 1 (%v)", effective, winner)
	}
}

// TestW4CausalCorrectionMadeInKnowledgeOfThePeersWins — site-b (greater
// site_id, later clock, greater corrected instant) corrects the target over
// the bare record; site-a receives it and corrects again. The LESSER-keyed
// correction wins: its `seen` names the peer's; the resolved instant is the
// local one; Effective sits on the FIRST chain entry, not the last; the
// peer's Outcome is Superseded by the local key; a third post in either
// delivery order agrees. Every named rival (latest-by-corrected-instant,
// latest-by-act-clock, and fixed order) names the peer.
func TestW4CausalCorrectionMadeInKnowledgeOfThePeersWins(t *testing.T) {
	const (
		peerMS  = w4OriginalMS + 2*w3Hour
		localMS = w4OriginalMS + 1*w3Hour
	)
	st := w3NewStore(t)
	target := w4Target(t, st, "site-a", 1, "Causal Target")
	primer := w3Events(t, st, "site-a", 1)

	peer := w4RemoteCorrect(t, primer, "site-b", w3Base+3*w3Hour, target.Key(), peerMS, "Pat Bell")
	w3Apply(t, st, peer)
	local := w4CorrectLocal(t, st, "site-a", 1, w3Base+1*w3Hour, target.Key(), localMS, "Ida Aputi")
	d92RequireCausalArrangement(t, local, peer)
	w4RequireOrderRivalsDisagree(t, st, target.Key(), localMS, [][2]any{{"site-a", int64(1)}, {"site-b", int64(1)}})

	eff, chain, err := w4Resolve(t, st, target.Key())
	if err != nil {
		t.Fatalf("ResolveOccurredAt: %v", err)
	}
	if eff.InstantUTCms != localMS || eff.Winner != local.Key() || !eff.Corrected || eff.RecordedBy != "Ida Aputi" {
		t.Fatalf("W4-3 re-locked (D-92; m-D92a/m-D92c-correction): resolved = (%d, winner %v, by %q), want (%d, %v, %q) — the correction made IN KNOWLEDGE of the peer's wins regardless of site_id; the peer's instant is D-28 Part B's answer", eff.InstantUTCms, eff.Winner, eff.RecordedBy, localMS, local.Key(), "Ida Aputi")
	}
	if len(chain) != 2 {
		t.Fatalf("chain has %d entries, want 2: %+v", len(chain), chain)
	}
	w4RequireChainStatuses(t, chain, local.Key())
	if chain[0].Key != local.Key() {
		t.Fatalf("D-92: Effective is expected on the FIRST entry (the lesser key) here, got chain %+v", chain)
	}
	if out := w4OccurredAtFact(t, st, peer.Key()); out.Status != Superseded || out.Winner != local.Key() {
		t.Fatalf("D-92: the peer's Outcome is (%v, winner %v), want (Superseded, %v) — superseded by a key LESS than its own, because that key's author saw it", out.Status, out.Winner, local.Key())
	}
	if out := w4OccurredAtFact(t, st, local.Key()); out.Status != Effective || out.Winner != local.Key() {
		t.Fatalf("D-92: the local correction's Outcome is (%v, %v), want (Effective, itself)", out.Status, out.Winner)
	}
	for i, order := range [][]store.Envelope{{local, peer}, {peer, local}} {
		third := w3NewStore(t)
		w3ApplyAll(t, third, primer)
		w3ApplyAll(t, third, order)
		e, ch, err := w4Resolve(t, third, target.Key())
		if err != nil {
			t.Fatal(err)
		}
		if e.InstantUTCms != localMS || e.Winner != local.Key() {
			t.Fatalf("W3-5 under D-92: a third post receiving order %d resolves (%d, %v), want (%d, %v) — the rule is a pure function of the record set", i, e.InstantUTCms, e.Winner, localMS, local.Key())
		}
		w4RequireChainStatuses(t, ch, local.Key())
	}
}

// TestW4GenuinelyConcurrentCorrectionsFallToTheSiteIDTiebreak — both posts
// hold only the target; each corrects it without seeing the other. Neither
// `seen` names anything; the greater site_id wins at both posts, and the
// clock and the corrected instant (both greater on the loser) decide
// nothing.
func TestW4GenuinelyConcurrentCorrectionsFallToTheSiteIDTiebreak(t *testing.T) {
	const (
		localMS = w4OriginalMS + 2*w3Hour // the loser's — the greater instant
		peerMS  = w4OriginalMS + 1*w3Hour // the tiebreak winner's
	)
	st := w3NewStore(t)
	target := w4Target(t, st, "site-a", 1, "Concurrent Target")
	primer := w3Events(t, st, "site-a", 1) // what BOTH posts hold when the link drops

	local := w4CorrectLocal(t, st, "site-a", 1, w3Base+3*w3Hour, target.Key(), localMS, "Ida Aputi")
	peer := w4RemoteCorrect(t, primer, "site-b", w3Base+1*w3Hour, target.Key(), peerMS, "Pat Bell")
	w3Apply(t, st, peer)
	d92RequireConcurrentArrangement(t, local, peer, nil)
	w4RequireOrderRivalsDisagree(t, st, target.Key(), peerMS, [][2]any{{"site-a", int64(1)}, {"site-b", int64(1)}})

	for name, post := range map[string]*store.Store{"site-a's post": st, "site-b's post": func() *store.Store {
		p := w3NewStore(t)
		w3ApplyAll(t, p, primer)
		w3Apply(t, p, peer)
		w3Apply(t, p, local)
		return p
	}()} {
		eff, chain, err := w4Resolve(t, post, target.Key())
		if err != nil {
			t.Fatal(err)
		}
		if eff.InstantUTCms != peerMS || eff.Winner != peer.Key() {
			t.Fatalf("D-92 tiebreak at %s: resolved (%d, %v), want the greater site_id's (%d, %v) — neither author saw the other, so site_id decides (D-28 Part B's one surviving case)", name, eff.InstantUTCms, eff.Winner, peerMS, peer.Key())
		}
		w4RequireChainStatuses(t, chain, peer.Key())
	}
	if out := w4OccurredAtFact(t, st, local.Key()); out.Status != Superseded || out.Winner != peer.Key() {
		t.Fatalf("D-92 tiebreak: the local correction's Outcome is (%v, %v), want (Superseded, %v)", out.Status, out.Winner, peer.Key())
	}
}

// TestW4CorrectionOfACorrectionStampsTheHeadAndCompetesInTheOriginalsChain
// — W4-11's one-hop rule under D-92: site-a corrects by targeting the
// PEER'S CORRECTION rather than the original. The target reference finds
// the original (as before); the stamp — computed from the original's chain
// — names the peer's correction as the head; the correction competes in
// the original's chain and WINS by dominance. Resolving through either key
// gives the same answer.
func TestW4CorrectionOfACorrectionStampsTheHeadAndCompetesInTheOriginalsChain(t *testing.T) {
	const (
		peerMS  = w4OriginalMS + 2*w3Hour
		localMS = w4OriginalMS + 1*w3Hour
	)
	st := w3NewStore(t)
	target := w4Target(t, st, "site-a", 1, "One Hop Target")
	primer := w3Events(t, st, "site-a", 1)
	peer := w4RemoteCorrect(t, primer, "site-b", w3Base+3*w3Hour, target.Key(), peerMS, "Pat Bell")
	w3Apply(t, st, peer)

	local := w4CorrectLocal(t, st, "site-a", 1, w3Base+1*w3Hour, peer.Key(), localMS, "Ida Aputi")
	d92RequireCausalArrangement(t, local, peer)

	for _, via := range []struct {
		name string
		key  store.Key
	}{{"the original", target.Key()}, {"the peer's correction", peer.Key()}, {"the local correction", local.Key()}} {
		eff, chain, err := w4Resolve(t, st, via.key)
		if err != nil {
			t.Fatalf("ResolveOccurredAt via %s: %v", via.name, err)
		}
		if eff.InstantUTCms != localMS || eff.Winner != local.Key() {
			t.Fatalf("D-92 / W4-11 via %s: resolved (%d, %v), want (%d, %v) — the correction of a correction competes in the ORIGINAL's chain and wins by dominance", via.name, eff.InstantUTCms, eff.Winner, localMS, local.Key())
		}
		if len(chain) != 2 {
			t.Fatalf("via %s: the chain has %d entries, want the original's 2: %+v", via.name, len(chain), chain)
		}
		w4RequireChainStatuses(t, chain, local.Key())
	}
	if out := w4OccurredAtFact(t, st, peer.Key()); out.Status != Superseded || out.Winner != local.Key() {
		t.Fatalf("D-92: the peer's Outcome is (%v, %v), want (Superseded, %v)", out.Status, out.Winner, local.Key())
	}
}

// TestW4CorrectionSeenIsStampedFromEveryHeadAndTheFirstOmitsIt — after a
// concurrent pair (two heads) a third local correction names BOTH heads,
// ascending; the side-table row reads the list back; the first correction
// of an event carries no `seen` key (W4-6's exact-key pin still holds for
// it); TimeCorrection has no Seen field; and a correction of a target this
// post does not hold LANDS, stamping nothing.
func TestW4CorrectionSeenIsStampedFromEveryHeadAndTheFirstOmitsIt(t *testing.T) {
	st := w3NewStore(t)
	target := w4Target(t, st, "site-a", 1, "Stamp Target")
	primer := w3Events(t, st, "site-a", 1)
	local := w4CorrectLocal(t, st, "site-a", 1, w3Base+1*w3Hour, target.Key(), w4OriginalMS+1*w3Hour, "Ida Aputi")
	peer := w4RemoteCorrect(t, primer, "site-b", w3Base+2*w3Hour, target.Key(), w4OriginalMS+2*w3Hour, "Pat Bell")
	w3Apply(t, st, peer)
	third := w4CorrectLocal(t, st, "site-a", 1, w3Base+3*w3Hour, target.Key(), w4OriginalMS+3*w3Hour, "Riley Okpik")

	want := []store.Key{local.Key(), peer.Key()}
	if !want[0].Less(want[1]) {
		want[0], want[1] = want[1], want[0]
	}
	if seen := d92SeenOf(t, third); !w3KeysEqual(seen, want) {
		t.Fatalf("D-92 (m-D92b-correction): the third correction carries seen=%v, want exactly the two heads %v", seen, want)
	}
	rows, err := correctionsTargeting(st, target.Key())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Key == third.Key() {
			found = true
			if !w3KeysEqual(r.Seen, want) {
				t.Fatalf("D-92: occurred_at_correction.seen reads back %v, want %v — the fold or the read lost the reference", r.Seen, want)
			}
		}
	}
	if !found {
		t.Fatalf("no occurred_at_correction row at %v", third.Key())
	}
	if d92HasSeenKey(t, local) {
		t.Fatalf("D-92 / W4-6: the FIRST correction of the event carries a `seen` key — an author who saw no correction standing must omit it, so W4-6's exact-key pin holds for it")
	}
	d92RequireNoSeenField(t, TimeCorrection{})
	eff, chain, err := w4Resolve(t, st, target.Key())
	if err != nil {
		t.Fatal(err)
	}
	if eff.Winner != third.Key() {
		t.Fatalf("D-92: the third correction, naming both heads, should stand; winner = %v", eff.Winner)
	}
	w4RequireChainStatuses(t, chain, third.Key())

	// A target this post does not hold: legal, lands, stamps nothing.
	dangling := store.Key{SiteID: "site-z", NodeEpoch: 1, Seq: 7}
	env, err := CorrectOccurredAt(st, "site-a", 1, TimeCorrection{Target: dangling, CorrectedOccurredAtMS: w4OriginalMS, RecordedBy: "Ida Aputi"})
	if err != nil {
		t.Fatalf("TP §4.7: a correction of a not-yet-replicated target was REFUSED: %v — next-seq is predictable and the dangling state is legal", err)
	}
	if d92HasSeenKey(t, env) {
		t.Fatalf("D-92: a correction of a target this post does not hold carries seen=%v, want none — nothing of it is present to have been seen", d92SeenOf(t, env))
	}
}
