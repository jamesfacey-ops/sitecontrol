// syncstatus.go — C10: the sync-status UI's view model.
//
// Purpose (design §8.9): "a silent alerting system that people trust is more
// dangerous than one visibly out of contact." This file turns a
// repl.Snapshot into render-ready data. The one invariant every function
// here exists to protect: Healthy on a peerView may be set true in EXACTLY
// ONE place (the final default case in buildPeerView) — everywhere else only
// ever narrows it toward false. A stale cached "ok" per-stream state left
// over from the last successful pull, sitting next to a fresh transport
// error, must never read as healthy (see syncstatus_test.go's
// TestBuildSyncStatusViewStaleOkStreamStateDoesNotOverrideAFreshTransportError,
// and the fixture-can-fail proof beside it). The SelfHistoryPending case
// (§8.6 self-history recovery not yet verified from that peer) is one more
// narrows-toward-false branch, exactly that shape. So is the AGE CEILING
// (SYNC-HONESTY, 2026-09-09; see syncStaleMultiple): a last successful
// pull older than a multiple of the configured sync interval, with no
// error since, is a stalled ENGINE — and until that branch existed it was
// the one way the data could stop flowing that this file rendered green.
//
// ONE deliberate exception to "every new signal narrows toward false"
// (design/app-fixes-v1.md §2.1 piece 5, PI-6): the vacuous annotation —
// "peer reports no pre-wipe history for this post" — is a CLAUSE appended
// to the default Healthy verdict, NOT a verdict branch. It decorates
// Healthy=true deliberately (VERDICT-2 §2.1 am.1; APPFIX-ARBITER-VERDICT
// §2.1c) and must not be "corrected" into a narrowing case: an engine must
// not re-arm forever against a peer that verifiably answers empty, and a
// page must not go red on information that does not exist in the mesh — a
// permanently-red first install trains operators to ignore red, the same
// harm as a false green in mirror image. The party that can tell a first
// install from a re-image is the provisioning side, and the alarm belongs
// there.
//
// Y-04 (sc-test L12 rounds 1 and 2; RULED 2026-10-03, FIX-PLAN §4 W7): the
// gate page (/gate, /gate/vehicle) carries NO sync chip, and that now stands
// as a ruling rather than the pending "same one-field change, deferred" the
// per-page slot table in templates/_topbar.html records. The chip stays off
// the gate screen unless James asks for it; nothing here or in the templates
// is to grow one on its own. (Note only — no code change; recorded where the
// chip's view model lives so the next reader of this file finds the ruling.)
package web

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"sitecontrol/internal/humantime"
	"sitecontrol/internal/repl"
)

// syncStatusView is the whole render-ready sync-status section (§8.9).
// Available=false means the engine was never wired into this Server — that
// is rendered as an explicit refusal ("cannot currently tell"), never as
// silence and never as a false "ok" (see handleStatus/buildSyncSection).
type syncStatusView struct {
	Available  bool
	SiteID     string
	Peers      []peerView
	AllStreams []streamView // EVERY entry in Snapshot.Streams — nothing filtered out, including self-history/rejoin streams that own no configured peer (carry-forward row 26/§8.6's "must not be silent" note in engine.go's Rejoin).
	Applied    int64
	Duplicates int64

	// StaleMultiple / StaleAfter: the age ceiling this render applied to
	// the Healthy verdict (syncStaleMultiple × the configured sync
	// interval), carried on the view so the status page can STATE the
	// threshold beside the interval it was derived from instead of a
	// template repeating the number.
	StaleMultiple int
	StaleAfter    time.Duration

	// Uptime is how long this post's process has run (Snapshot.UptimeSec):
	// the "since this post started" in every not-heard-since verdict.
	Uptime time.Duration
}

// syncStaleMultiple is the age ceiling on the ONE Healthy verdict, as a
// multiple of the configured sync interval (SYNC-HONESTY, 2026-09-09).
//
// THE DEFECT. Healthy used to be minted whenever the last pull succeeded
// and no owned stream was bad — with no comparison against
// sync_interval_seconds anywhere in this package. A dead LINK reds within
// ~20 s regardless (a failed attempt sets LastError; engine.go's 10 s
// client timeout bounds the attempt), but a stalled ENGINE — the Run loop
// gone, no attempt ever made again — left the last successful pull's state
// in place forever, and this file rendered "ok — last heard 3 hours ago"
// IN GREEN (measured on /: data-health="ok", chip "1 peer(s) healthy").
// The page is the only alarm this system has, and it could not say "I
// might be stale".
//
// WHY THREE, NOT TWO. A healthy peer's contact age on a live engine is
// normally at most one interval plus the pull's own duration. PullAll is
// SEQUENTIAL over peers, so a healthy peer sitting behind a dead one in
// config order waits out the dead peer's attempt first — up to the 10 s
// client timeout — before its own pull runs: at the deployed 10 s interval
// that is a legitimate age of about interval + timeout = 20 s = 2× on a
// link that is working perfectly. A ceiling of 2× would therefore cry wolf
// on exactly the post that is doing its job, and an alarm officers learn
// to ignore is worse than none. 3× (30 s at the deployed interval) clears
// that case with one full interval of headroom and still reds a stalled
// engine within about half a minute — the same order as the dead-link
// path, so "the data stopped flowing" reds in comparable time whichever
// way it stopped. The ceiling is derived from the interval at render time:
// change sync_interval_seconds and the ceiling follows; there is no
// literal number of seconds here to drift from the config.
//
// Stated limits: the headroom argument assumes the interval is at least
// the fixed 10 s client timeout (engine.go). A post configured with a
// sub-10 s interval and a dead sibling peer could read STALE on a working
// link; that is a configuration to avoid, not a case this constant can
// absorb without hard-coding the timeout it was told not to. And the
// headroom is ONE dead sibling's worth: k dead siblings ahead in config
// order delay a working peer by about k × 10 s, so at a 10 s interval
// with k ≥ 2 a working link can read STALE for one render until its own
// pull lands (REVIEW-SYNC-HONESTY, 2026-09-09). Moot at two sites — one
// peer, no siblings — and noted here so a three-site deployment does not
// rediscover it as a defect.
const syncStaleMultiple = 3

// peerView is ONE peer's whole story, deliberately collapsed to a SINGLE
// verdict sentence. Carry-forward row 19: a frozen anchor, fresh contact,
// and a stalled/refused stream must render as ONE story, not as separate
// signals an operator could read as "reachable, so fine" sitting next to
// "log stalled, so broken".
type peerView struct {
	SiteID     string
	NeverSeen  bool // LastContactAgoSec == nil: no successful contact, ever
	ContactAgo *float64
	AnchorAgo  *float64
	LastError  string
	Drains     int64
	Streams    []streamView // this peer's own-authored streams only (subset of AllStreams)

	// SelfHistoryPending: this post's own §8.6 pre-re-image history is not
	// yet verified as recovered from this peer (repl.PeerSnapshot's field).
	// SelfHistoryVacuous: the recovery latch was satisfied by a verified
	// answer holding no PRE-WIPE history for this post — rendered as a
	// clause on the Healthy verdict, never as a verdict branch (file doc).
	SelfHistoryPending bool
	SelfHistoryVacuous bool

	// The D26 facts (repl.PeerSnapshot's doc; ruling R11). KeyRejected: the
	// peer refuses THIS post's key or identity — on the last pull
	// (ErrorKind) or on pokes (PokeRefused). CannotRead: the peer cannot get
	// this post's records — this post refuses its key (InboundKeyRefused) or
	// its copy of this post's stream is stuck (OwnCopyStuck).
	KeyRejected bool
	CannotRead  bool
	// NotReceiving: what this post records is known NOT to reach the peer —
	// CannotRead, or the peer refuses this post's identity (403: it does not
	// list this post, so it never pulls it). FIX3 F6 (L04-N1): a 401 alone
	// is NOT this — the peer's pulls of this post use the peer's own key and
	// may succeed (measured: all four peers held the records within 6 s).
	NotReceiving bool
	// InboundRefused: this post refuses the peer's key (InboundKeyRefused).
	InboundRefused bool
	// PokeTrouble words a failing poke that is not a refusal (FIX3 F7;
	// repl.PeerSnapshot.PokeError): shown beside the verdict, never a verdict
	// branch — pulls still carry the records both ways.
	PokeTrouble string

	// Lost (FIX15, James 2026-10-06: "a lost signal notification on the
	// sites to notify on the dispatch page if signal is dropped"): this post
	// has completed no sync with the peer for longer than the SAME age
	// ceiling the STALE verdict uses (syncStaleMultiple × the configured
	// interval) — measured from the last successful exchange, or from this
	// post's start when there has been none since. NOT lost: a peer whose
	// last pull was answered with a key/identity refusal (401/403) — the link
	// is up, so that is the key fault the verdict already words, never "lost
	// signal". A side fact for the strip and the Signal tile (lostsignal.go);
	// it never touches Healthy or the verdict.
	Lost bool

	// Healthy is true in exactly one branch of buildPeerView. Every other
	// branch leaves it false (the Go zero value) — so adding a NEW bad
	// signal in the future only ever has to add a case that leaves it
	// unset, never remember to explicitly clear it.
	Healthy bool
	Verdict string
}

// streamView is one (site_id, node_epoch) stream's rendered row.
type streamView struct {
	SiteID    string
	NodeEpoch int64
	State     string
	OK        bool // State == repl.StateOK, computed once here so the template never string-compares state literals itself
	Own       bool // SiteID == this site's own site_id (a §8.6 self-history/rejoin stream, not a live peer's own log)
	Watermark int64
	Head      int64
	Detail    string
	Label     string // human label, including the §8.5/§8.9 visible-refusal wording

	// Unreported: a row Snapshot synthesised for a peer that has reported
	// nothing since this post started (repl.StateUnreported, L12-Y05a). Its
	// head is unknown, so the table shows "—", never a 0 that reads as a
	// value.
	Unreported bool
}

// buildSyncStatusView is a pure function of a repl.Snapshot and the
// configured sync interval: no wall-clock read happens here (Snapshot()
// already converted instants to ages), so rendering the same Snapshot twice
// always reproduces the same page — tests need no sleep-and-poll to
// exercise a rendering behaviour, only a sleep to exercise the underlying
// Snapshot's own age arithmetic. interval is config.SyncInterval(), the ONE
// input the age ceiling is derived from (syncStaleMultiple) — never a
// literal number of seconds.
func buildSyncStatusView(snap repl.Snapshot, interval time.Duration) syncStatusView {
	view := syncStatusView{
		Available: true, SiteID: snap.SiteID,
		Applied: snap.Applied, Duplicates: snap.Duplicates,
		StaleMultiple: syncStaleMultiple, StaleAfter: syncStaleMultiple * interval,
		Uptime: time.Duration(snap.UptimeSec * float64(time.Second)),
	}

	byOwner := make(map[string][]streamView, len(snap.Peers))
	view.AllStreams = make([]streamView, 0, len(snap.Streams))
	for _, s := range snap.Streams {
		sv := streamView{
			SiteID: s.SiteID, NodeEpoch: s.NodeEpoch, State: s.State,
			OK: s.State == repl.StateOK, Own: s.SiteID == snap.SiteID,
			Watermark: s.Watermark, Head: s.Head, Detail: s.Detail,
			Label: stateLabel(s.State), Unreported: s.State == repl.StateUnreported,
		}
		view.AllStreams = append(view.AllStreams, sv)
		byOwner[s.SiteID] = append(byOwner[s.SiteID], sv)
	}
	sort.Slice(view.AllStreams, func(i, j int) bool {
		if view.AllStreams[i].SiteID != view.AllStreams[j].SiteID {
			return view.AllStreams[i].SiteID < view.AllStreams[j].SiteID
		}
		return view.AllStreams[i].NodeEpoch < view.AllStreams[j].NodeEpoch
	})
	for _, streams := range byOwner {
		sort.Slice(streams, func(i, j int) bool { return streams[i].NodeEpoch < streams[j].NodeEpoch })
	}

	view.Peers = make([]peerView, 0, len(snap.Peers))
	for _, p := range snap.Peers { // snap.Peers is already config-order (Snapshot's own doc comment)
		view.Peers = append(view.Peers, buildPeerView(p, byOwner[p.SiteID], interval, view.Uptime))
	}
	return view
}

// buildPeerView collapses one peer's contact/error/anchor/stream signals
// into a single verdict. See the file doc comment: Healthy is set true in
// exactly the final case below, nowhere else. interval is the configured
// sync interval; the age ceiling is syncStaleMultiple × interval.
func buildPeerView(p repl.PeerSnapshot, streams []streamView, interval, uptime time.Duration) peerView {
	pv := peerView{
		SiteID: p.SiteID, ContactAgo: p.LastContactAgoSec, AnchorAgo: p.AnchorAgoSec,
		LastError: p.LastError, Drains: p.Drains, NeverSeen: p.LastContactAgoSec == nil,
		Streams:            streams,
		SelfHistoryPending: p.SelfHistoryPending, SelfHistoryVacuous: p.SelfHistoryVacuous,
		KeyRejected: p.PokeRefused ||
			(p.LastError != "" && (p.ErrorKind == repl.ErrorKindKeyRejected || p.ErrorKind == repl.ErrorKindIdentityRejected)),
		CannotRead: p.InboundKeyRefused || p.OwnCopyStuck, InboundRefused: p.InboundKeyRefused,
	}
	pullKind, pokeKind := keyFaultKinds(p)
	pv.Lost = lostSignal(pv.ContactAgo, uptime, pullKind, interval)
	pv.NotReceiving = pv.CannotRead || pullKind == repl.ErrorKindIdentityRejected || pokeKind == repl.ErrorKindIdentityRejected
	if p.PokeError != "" && !p.PokeRefused {
		pv.PokeTrouble = "pokes to this peer are failing" + pokeTroubleWords(p.PokeErrorKind, p.PokeError) +
			" — it is not told of new records here at once, and gets them on its own routine pull instead"
	}

	// A synthesised "not reported since start" row (L12-Y05a) is neither a
	// stalled log nor a reported stream: it never selects the STALLED
	// verdict and never counts as "stream state observed".
	var badStream *streamView
	reported := 0
	for i := range streams {
		if streams[i].Unreported {
			continue
		}
		reported++
		if badStream == nil && !streams[i].OK {
			badStream = &streams[i]
		}
	}

	switch {
	case pv.KeyRejected:
		// James, 2026-10-04 (ruling R11, D26): a post whose pulls or pokes
		// its peers refuse with 401/403 says "peers reject this post's key"
		// on its own status page — checked FIRST, ahead of the never-seen
		// case, because after a restart a refused peer is also never seen
		// and "not heard from" names the wrong fault: the link is up.
		pv.Verdict = keyRejectedVerdict(pullKind, pokeKind, pv.NotReceiving, sinceText(pv.ContactAgo, uptime))
	case pv.CannotRead:
		// The measured L12-Y31b case and its cousin. This post's own calls
		// may all succeed — it pulls the peer and pokes it fine — while the
		// peer cannot read THIS post; before D26 that rendered "ok" here.
		why := "its copy of this post's records has not caught up for more than " + fmt.Sprint(repl.OwnCopyLagMultiple) + "× the sync interval — its pulls of this post are failing (a key mismatch, or the link is cut in one direction)"
		if p.InboundKeyRefused {
			why = "this post is REFUSING its requests: the key that peer holds for this post does not match this post's own peer_key"
		}
		pv.Verdict = fmt.Sprintf("PEER CANNOT READ THIS POST — %s. Check-ins and records made here are NOT reaching it (it holds this post's records up to %d; last heard %s)",
			why, p.OwnCopyHead, sinceText(pv.ContactAgo, uptime))
	case pv.NeverSeen:
		// No LastContactAgoSec means no /sync/streams exchange has succeeded
		// SINCE THIS PROCESS STARTED (contact is in-memory) — a cached-OK
		// stream row cannot exist without at least one prior success, but
		// this branch runs first regardless, so it can never be shadowed by
		// one. FIX2 cluster K (sc-test L08 T-074): this used to say "never
		// contacted … has ever succeeded", which after a restart is false —
		// the post holds that peer's earlier records — and read to an
		// officer as "this post has never heard from it" for a whole
		// partition.
		pv.Verdict = "not heard from " + sinceStart(uptime) + " — no sync exchange with this peer has succeeded since then; records this post already held from it are kept, but nothing newer has arrived" + errorClause(p)
	case pv.LastError != "":
		// The core "silent alerting system" guard: a fresh transport error
		// on the LAST attempt wins over whatever a PRIOR successful pull
		// left cached in Streams (still literally "ok" from that pull).
		// This case is checked before badStream on purpose — an error here
		// means the last exchange didn't even get far enough to report a
		// fresh per-stream state, so the cached state (if any) is stale by
		// definition and must not be allowed to read as current.
		//
		// FIX2 / L01 F-5: in words. This verdict is also the site page's
		// STALE line (analytics.go), and it used to end with the raw sync
		// error — the peer's base URL and the t1 timestamp parameter on an
		// operator page. The raw text stays in LastError, which only the
		// status page's technical "last error" line and the JSON show.
		pv.Verdict = fmt.Sprintf("last heard %s, but the most recent sync attempt FAILED%s",
			ageText(pv.ContactAgo), errorClause(p))
	case interval <= 0:
		// The ceiling cannot be derived without an interval. config's
		// validate() refuses sync_interval_seconds <= 0 at startup, so
		// production never reaches this branch; a caller that does (a
		// hand-built Config) is told so rather than handed a Healthy it did
		// not earn — one more branch that leaves Healthy unset, never a
		// silent "ceiling off" switch.
		pv.Verdict = fmt.Sprintf("last heard %s — cannot judge freshness: no sync interval is configured for this post (sync_interval_seconds)",
			ageText(pv.ContactAgo))
	case *pv.ContactAgo > (syncStaleMultiple * interval).Seconds():
		// THE STALLED-ENGINE CASE (syncStaleMultiple's doc): the last
		// exchange succeeded, nothing has been attempted since (LastError
		// is empty — that case ran first), and that success is now older
		// than the ceiling. Checked BEFORE badStream / SelfHistoryPending on
		// purpose: every verdict below this line opens with "reachable
		// (last heard …)", and reachable is not a claim this post can make
		// about a peer it has exchanged nothing with for longer than the
		// ceiling. ContactAgo is non-nil here — NeverSeen ran first.
		pv.Verdict = fmt.Sprintf(
			"STALE — last heard %s, which is more than %d× the configured sync interval (%s) with no sync error reported: this post has not completed a sync with this peer since then, so anything that peer recorded after that time has NOT reached this post",
			ageText(pv.ContactAgo), syncStaleMultiple, interval)
	case badStream != nil:
		// Carry-forward row 19, rendered as one sentence: reachable (fresh
		// contact) AND stalled (a non-ok owned stream) AND running on a
		// frozen anchor — all three clauses, together, never separately.
		// The anchor may legitimately be nil (no full drain
		// since process start — repl.Engine.Anchor's ok=false case): render
		// that as a meaningful clause, never as the nonsense "captured never
		// ago" ageText(nil) alone would produce (verifier fix 2026-08-19).
		anchorClause := "no full drain has ever completed for this peer"
		if pv.AnchorAgo != nil {
			anchorClause = "data captured " + ageText(pv.AnchorAgo)
		}
		pv.Verdict = fmt.Sprintf(
			"reachable (last heard %s) but its log is STALLED — %s. Deadline evaluation for this peer runs on %s (frozen anchor), not live.",
			ageText(pv.ContactAgo), badStream.Label, anchorClause)
	case pv.SelfHistoryPending:
		// §8.6 self-history recovery not yet verified from this peer
		// (design/app-fixes-v1.md §2.1 piece 5): a reachable peer whose
		// peer-log pulls all succeed can still be sitting on pre-wipe
		// history of OURS it has not delivered — pre-fix that rendered
		// green over an empty muster (probe 1). One more branch that
		// leaves Healthy unset.
		pv.Verdict = fmt.Sprintf(
			"reachable (last heard %s) but this post's OWN pre-re-image history is NOT yet recovered from this peer (§8.6) — pre-wipe check-ins may be missing from the muster; recovery retries every sync cycle",
			ageText(pv.ContactAgo))
	case reported == 0:
		pv.Verdict = fmt.Sprintf("reachable (last heard %s) — no stream state observed yet", ageText(pv.ContactAgo))
	default:
		pv.Healthy = true
		pv.Verdict = fmt.Sprintf("ok — last heard %s", ageText(pv.ContactAgo))
		if pv.SelfHistoryVacuous {
			// A clause on the ONE Healthy mint, deliberately NOT a
			// narrowing case — see the file doc's PI-6 paragraph before
			// "fixing" this into a not-Healthy branch.
			pv.Verdict += " — peer reports no pre-wipe history for this post"
		}
	}
	return pv
}

// keyFaultKinds splits PeerView.KeyRejected into what was refused: the
// PULL (ErrorKind of a standing LastError) and the POKE (PokeRefusedKind).
// Each is "", repl.ErrorKindKeyRejected (401) or ...IdentityRejected (403).
// A refused poke with no recorded kind (a snapshot from before FIX3) is
// read as the 403 it was always worded as.
func keyFaultKinds(p repl.PeerSnapshot) (pull, poke string) {
	if p.LastError != "" && (p.ErrorKind == repl.ErrorKindKeyRejected || p.ErrorKind == repl.ErrorKindIdentityRejected) {
		pull = p.ErrorKind
	}
	if p.PokeRefused {
		poke = p.PokeRefusedKind
		if poke == "" {
			poke = repl.ErrorKindIdentityRejected
		}
	}
	return pull, poke
}

// keyRejectedVerdict is the D26 key verdict, naming only what actually
// fails (FIX3 F6, FINDINGS-R5 L04-N1 / L01-2a / re-run A3). ⟦was: one
// sentence for every shape — "it refuses <what>, so nothing recorded here
// reaches it and nothing new from it arrives here" — which was false in
// both halves for a peer that still pulled this post, and called a poke's
// 401 a "403 — it does not list this post".⟧ Each half is claimed only
// when it holds:
//   - "nothing new from it arrives here": this post's PULLS are refused;
//   - "nothing recorded here reaches it": NotReceiving (a 403, or the
//     peer cannot read this post) — otherwise the honest clause is that
//     this post's records reach it only through its own routine pulls,
//     because the poke that would tell it at once is refused.
//
// The remedy follows the code: a 401 is a key mismatch, a 403 a peer list
// that does not name this post.
func keyRejectedVerdict(pullKind, pokeKind string, notReceiving bool, lastOK string) string {
	var what string
	switch {
	case pullKind == repl.ErrorKindIdentityRejected:
		what = "this post's identity (403 — it does not list this post as a peer)"
	case pullKind == repl.ErrorKindKeyRejected && pokeKind != "":
		what = "this post's key (401) on its sync requests and its pokes"
	case pullKind == repl.ErrorKindKeyRejected:
		what = "this post's key (401) on its sync requests"
	case pokeKind == repl.ErrorKindKeyRejected:
		what = "this post's pokes (401 — the key this post sends it does not match its peer_key)"
	default:
		what = "this post's pokes (403 — it does not list this post as a peer)"
	}
	var effects []string
	if pullKind != "" {
		effects = append(effects, "nothing new from it arrives here")
	}
	if notReceiving {
		effects = append(effects, "nothing recorded here reaches it")
	} else {
		effects = append(effects, "records made here reach it only through its own routine pulls of this post")
	}
	fix := "the key in this post's config for this peer must match the peer's own peer_key"
	if pullKind == repl.ErrorKindIdentityRejected || (pullKind == "" && pokeKind == repl.ErrorKindIdentityRejected) {
		fix = "the peer's config must list this post in its [[peers]]"
	}
	return fmt.Sprintf("PEER REJECTS THIS POST'S KEY — it refuses %s, so %s; the link is up, so this is a configuration mismatch: %s (last successful exchange: %s)",
		what, strings.Join(effects, " and "), fix, lastOK)
}

// pokeTroubleWords words a non-refusal poke failure's category (F7).
func pokeTroubleWords(kind, errText string) string {
	switch kind {
	case repl.ErrorKindUnreachable:
		if repl.ConnectionDropped(errText) { // fix4 H (R6-2.3) ⟦was: always "nothing answered"⟧
			return " (the connection to its address was dropped before it answered)"
		}
		return " (nothing answered at its address)"
	case repl.ErrorKindWrongSite:
		return " (a different post answered at its address)"
	default:
		return " (it, or something in front of it, answered but did not take the poke)"
	}
}

// stateLabel gives every repl stream state a human, §-cited label. An
// unrecognised future state is shown VERBATIM rather than hidden — a state
// this file doesn't know about must never silently disappear from the page.
func stateLabel(state string) string {
	switch state {
	case repl.StateOK:
		return "ok"
	case repl.StateSchemaMismatch:
		return "SCHEMA MISMATCH — queued at the author, refused here (§8.5); never applied"
	case repl.StateRegressed:
		return "PEER LOG REGRESSED — reported seq went backwards within this epoch; never legitimate (§8.9)"
	case repl.StateIdentityRejected:
		return "IDENTITY REJECTED — record failed the §8.4 authorship check"
	case repl.StateGapStalled:
		return "GAP STALLED — missing sequence; treated as corruption, never skipped"
	case repl.StateError:
		return "TRANSPORT/PROTOCOL ERROR"
	case repl.StateUnreported:
		return "NOT YET REPORTED since this post started — the watermark is what this post already holds; the peer's head is unknown"
	case repl.StateLeftBehind:
		return "LEFT BEHIND — a peer holds more of THIS post's own stream than this post's local watermark does (Tier 1, LEFT-BEHIND-DETECT.md); recovery is an operator action, not automatic"
	default:
		return state
	}
}

// errorClause words LastError's category (repl.ErrorKind*) for an operator —
// never the raw error, which carries the peer's base URL (L01 F-5). Empty
// when there is no error.
func errorClause(p repl.PeerSnapshot) string {
	if p.LastError == "" {
		return ""
	}
	switch p.ErrorKind {
	case repl.ErrorKindUnreachable:
		if repl.ConnectionDropped(p.LastError) { // fix4 H (R6-2.3)
			return ": the connection to that peer's address was dropped before it answered (it may have stopped mid-request, or the link is unstable)"
		}
		return ": nothing answered at that peer's address (it may be down, or the link is cut)"
	case repl.ErrorKindWrongSite:
		return ": a DIFFERENT post answered at that peer's address — check the peer's address in this post's config"
	case repl.ErrorKindKeyRejected, repl.ErrorKindIdentityRejected:
		return ": the peer refused this post's key"
	default:
		return ": the peer answered, but not with anything this post could use"
	}
}

// sinceStart is "since this post started N ago" — the honest scope of every
// in-memory contact age (Snapshot.UptimeSec). A zero uptime (a hand-built
// snapshot with no clock) drops the age rather than print "0 seconds ago".
func sinceStart(uptime time.Duration) string {
	if uptime <= 0 {
		return "since this post started"
	}
	return "since this post started " + humantime.Ago(uptime)
}

// sinceText is ageText for a contact age that may be nil because this
// process has not yet made contact: "never" becomes "none since this post
// started N ago".
func sinceText(ago *float64, uptime time.Duration) string {
	if ago == nil {
		return "none " + sinceStart(uptime)
	}
	return ageText(ago)
}

// ageText renders a *float64-seconds age (repl.Snapshot's units) via the
// ONE shared phrasing rule (internal/humantime — see its doc comment for why
// this used to be three different formats across C7 and C10). nil means no
// successful contact/drain has ever happened, distinct from "0 seconds ago"
// — never conflate the two, and never feed nil to humantime.Ago (it takes a
// duration, not an optional one; the nil check must happen here).
func ageText(ago *float64) string {
	if ago == nil {
		return "never"
	}
	return humantime.Ago(time.Duration(*ago * float64(time.Second)))
}
