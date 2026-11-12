// engine.go — the puller client: routine pull loop, poke-then-pull fast path,
// the ingest identity rule, per-stream sync status, and the §9.3 anchor.
package repl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/store"
)

// Stream states surfaced per (site_id, node_epoch) — carry-forward row 4,
// NARROWED by the F8 fix: the schema-refusal catch is per-stream, and one
// refused or failed stream stalls only its own SITE's delivery — a stalled
// epoch also holds back that site's LATER epochs (epoch-major delivery,
// drainSite), because applying them first is the unsafe cross-epoch reorder.
// Isolation across SITES/peers is unchanged: one bad peer never stalls a
// good peer. C10 renders these; this package only produces them.
const (
	StateOK               = "ok"
	StateSchemaMismatch   = "schema_mismatch"   // §8.5 visible refusal: queued at the author, stalled here
	StateIdentityRejected = "identity_rejected" // §8.4 ingest rule violation
	StateGapStalled       = "gap_stalled"       // *store.GapError on the pull path = corruption or a bug (row 5)
	StateError            = "error"             // transport/protocol failure

	// StatePrefixDiverged is the seam-echo refusal (C3a addendum 3, CRITICAL):
	// the author's envelope at from−1 is not the row this node holds at that
	// seq — the two histories share a sequence number but not a prefix. The
	// one path here is a node rebuilt blank under an UNBUMPED epoch whose new
	// history has grown past our watermark: identity passes (it IS that
	// site), Apply sees contiguous seqs, and the regression detector sees
	// head ≥ peak, so this check is the only thing standing between that
	// splice and permanent silent cross-site divergence. Terminal and loud:
	// watermark untouched, nothing applied.
	StatePrefixDiverged = "prefix_diverged"

	// StateRegressed is the C3b §8.9 "peer log regressed" state: an author
	// answering, within the SAME node_epoch, with a head below one it has
	// already reported. A genuine re-image always announces a HIGHER epoch
	// instead (§8.6), so a same-epoch regression is never legitimate — it is
	// its own distinct surfaced status, never folded into any state above.
	StateRegressed = "peer_log_regressed"

	// StateLeftBehind is the Tier 1 left-behind-author alarm
	// (LEFT-BEHIND-DETECT.md, SYNCSTATUS-HONESTY.md item 3): checkLeftBehind
	// re-asks a peer, POST-latch, what it holds of OUR OWN site's streams
	// (the same GET /sync/streams?site=<own> exchange the §8.6 self-history
	// drain already uses) and finds the peer announcing MORE of our own
	// stream than our local watermark holds — a genuinely left-behind
	// author (restored from an older backup, or re-imaged without an epoch
	// bump, while a peer still holds a higher head), the case
	// STREAM-DIVERGENCE.md measured: once selfDrained latches, nothing ever
	// asks again, so this reads "ok" forever without this state.
	//
	// Deliberately its OWN value, never StateIdentityRejected: that already
	// means a distinct thing (a §8.4 authorship/forgery refusal on the pull
	// path), and it is what the boot-time self-history retry itself
	// produces for the SAME underlying fact WHILE it is still retrying (see
	// pullStream's identityOK exception, bounded by
	// env.NodeEpoch < e.cfg.NodeEpoch) — reusing it here would blur "a
	// record was refused" with "a peer holds more than us", two different
	// facts that happen to share a cause. StateLeftBehind is what covers the
	// gap that state's retry stops watching the instant it latches.
	//
	// Detection only (Tier 1): nothing here pulls the tail or bumps the
	// epoch — the R-23/AF3 same-epoch self-history bound is unchanged and
	// unconsulted. Recovery is Tier 2, an operator running the existing
	// epoch-bump-and-rejoin procedure; see the doc.
	StateLeftBehind = "left_behind"

	// StateUnreported is a row Snapshot SYNTHESISES, never one setStream
	// writes (sc-test L12-Y05a = L16-P1b, FIX2 cluster K): a configured peer
	// that has reported no stream since this process started — down, cut, or
	// refusing us — used to have NO row at all, and with every peer down the
	// table was empty, "not even its own". The row shows what this post
	// already HOLDS of that peer's log (its local watermark); the head is
	// unknown until the peer answers. Never OK: nothing about it is confirmed.
	StateUnreported = "unreported"
)

// Error kinds for PeerSnapshot.ErrorKind (FIX2 cluster K, L01 F-5): the plain
// category of LastError, so a page can say what went wrong in words instead
// of printing the raw transport error with the peer's base URL and the t1
// timestamp parameter. LastError keeps the raw text for /debug/sync-status.
const (
	ErrorKindUnreachable      = "unreachable"       // no answer: refused, timed out, DNS, reset
	ErrorKindKeyRejected      = "key_rejected"      // the peer answered 401: it refuses THIS post's key
	ErrorKindIdentityRejected = "identity_rejected" // the peer answered 403: it refuses THIS post's identity
	ErrorKindWrongSite        = "wrong_site"        // a different post answered at the peer's address
	ErrorKindBadAnswer        = "bad_answer"        // any other non-200, or an answer this post could not read
)

// OwnCopyLagMultiple is how many sync intervals a peer may go on holding
// LESS of this post's current stream than this post held when the lag was
// first seen, before the peer is reported as NOT RECEIVING this post's
// records (D26, sc-test L12-Y31b). Three, for the same reason the status
// page's age ceiling is three (web/syncstatus.go syncStaleMultiple): a
// healthy peer catches up within one poke, at worst one tick plus one
// sibling's timeout, so 3× never cries wolf on a working link and still
// reports a stuck one inside about half a minute at the deployed 10 s.
const OwnCopyLagMultiple = 3

// StreamKey identifies one replicated stream.
type StreamKey struct {
	SiteID    string `json:"site_id"`
	NodeEpoch int64  `json:"node_epoch"`
}

// StreamStatus is the per-stream sync state (§8.9 substrate).
type StreamStatus struct {
	StreamKey
	Watermark int64  `json:"watermark"` // last contiguously-applied seq (the store's)
	Head      int64  `json:"head"`      // last head the author reported
	State     string `json:"state"`
	Detail    string `json:"detail,omitempty"`

	// Own reports whether this row is THIS post's own stream (SiteID ==
	// the engine's own site_id), i.e. a §8.6 self-history/rejoin row, never
	// a live peer-reported observation. SYNC-HONESTY (2026-09-14,
	// STREAM-DIVERGENCE.md): the raw JSON used to carry no such label,
	// while the HTML page already flagged it ("own — rejoin recovery") —
	// so a script or operator reading /debug/sync-status directly, instead
	// of the rendered page, had no way to tell this row apart from a
	// genuine peer echo. See Snapshot() for how this row's Watermark/Head
	// are kept live.
	Own bool `json:"own"`
}

// PeerSnapshot is the per-peer view for the status surface. Ages are seconds
// (monotonic-derived); null means "never".
type PeerSnapshot struct {
	SiteID            string   `json:"site_id"`
	LastContactAgoSec *float64 `json:"last_contact_ago_seconds"`
	LastError         string   `json:"last_error,omitempty"`
	Drains            int64    `json:"drains"`
	AnchorAgoSec      *float64 `json:"anchor_ago_seconds"`

	// SelfHistoryPending / SelfHistoryVacuous surface the §8.6 self-history
	// latch (see peerState.selfDrained): pending means an armed engine has
	// not yet verified that THIS peer delivered everything it claims to
	// hold for our own site's pre-wipe history; vacuous means the latch was
	// satisfied by a verified answer holding zero PRE-WIPE streams for us
	// (streams with NodeEpoch < our config-seeded epoch — post-wipe
	// echo-back does not count).
	//
	// Deliberately NO omitempty on either (W1, design/app-fixes-v1.md §2.1
	// piece 5): false must be ON THE WIRE. Field-absent is the
	// provisioning-side detector's "unfixed or stale binary" signal, so
	// eliding false would make verified recovery wire-identical to a binary
	// without the fix — the exact version-skew false green this exists to
	// kill.
	SelfHistoryPending bool `json:"self_history_pending"`
	SelfHistoryVacuous bool `json:"self_history_vacuous"`

	// ErrorKind is LastError's plain category (ErrorKind* above); empty
	// when LastError is.
	ErrorKind string `json:"error_kind,omitempty"`

	// The D26 facts (sc-test L12-Y31b; ruling R11). James, 2026-10-04: a
	// post whose key its peers refuse says so on its own status page and
	// chip instead of "N peer(s) healthy". Before this, a post whose records
	// NO peer could read showed itself fully healthy and logged nothing,
	// because every call it MAKES succeeded — only the peers saw the fault.
	//
	// PokeRefused: this post's last poke to the peer was answered 401/403 —
	// the peer refuses this post's key (or identity). Set by the poke. A 403
	// is cleared only by an accepted poke: a successful pull does not clear
	// it, because a peer that does not list this post accepts its pulls (any
	// holder of the key may read) yet refuses its pokes. A 401 IS cleared by a
	// successful pull: the pull presents the very same key to the very same
	// requireKey, so its 200 proves the key is accepted now. ⟦FIX3 F2,
	// FINDINGS-R5 re-run A3: was "cleared only by an accepted poke" for both
	// — after the key was fixed the refusal outlived it until this post next
	// wrote (100 s idle, measured), a false STALE on a current peer.⟧ And
	// while it stands, every routine cycle re-probes with one poke
	// (pullPeer), so a fixed peer list clears a 403 within a tick too.
	PokeRefused bool `json:"poke_refused"`
	// PokeRefusedKind is the refusal's category (ErrorKindKeyRejected for a
	// 401, ErrorKindIdentityRejected for a 403); empty when !PokeRefused. The
	// page words the two differently and must never call a 401 a 403 (F6).
	PokeRefusedKind string `json:"poke_refused_kind,omitempty"`
	// PokeError / PokeErrorKind: this post's last poke to the peer failed
	// WITHOUT a key or identity refusal — a 503, a transport failure, a proxy
	// that strips POST (FIX3 F7; r4 L16-04a). Durable until a poke is
	// accepted (re-probed every routine cycle while set), and deliberately
	// NOT last_error: pulls still carry the records, only the fast path is
	// lost, so a failed poke must not turn a current peer STALE. ⟦was: the
	// poke wrote last_error, which the next successful pull erased, and
	// logged nothing.⟧
	PokeError     string `json:"poke_error,omitempty"`
	PokeErrorKind string `json:"poke_error_kind,omitempty"`
	// InboundKeyRefused: THIS post is refusing the peer's sync requests
	// (401 at requireKey, the peer named by its X-SC-From header) more
	// recently than it last accepted one — the key the peer holds for this
	// post does not match this post's peer_key, so the peer cannot read
	// anything recorded here. This is the measured L12-Y31b mechanism.
	InboundKeyRefused bool `json:"inbound_key_refused"`
	// OwnCopyHead / OwnCopyBehindSec: what the peer last told this post it
	// holds of this post's CURRENT stream (checkLeftBehind's per-cycle ask),
	// and — non-null only while it holds less than this post held when the
	// lag was first seen — for how long. OwnCopyStuck is that age past
	// OwnCopyLagMultiple × the sync interval: this post's records are not
	// reaching that peer, whatever the cause (key mismatch or a one-way cut).
	OwnCopyHead      int64    `json:"own_copy_head"`
	OwnCopyBehindSec *float64 `json:"own_copy_behind_seconds"`
	OwnCopyStuck     bool     `json:"own_copy_stuck"`
}

// Snapshot is the whole diagnostic state: what /debug/sync-status emits and
// what C10 will render. It is locally derived state and is deliberately NOT
// part of any projection dump (§13.3 excludes it from dump-and-compare).
type Snapshot struct {
	SiteID     string         `json:"site_id"`
	Peers      []PeerSnapshot `json:"peers"`
	Streams    []StreamStatus `json:"streams"`
	Applied    int64          `json:"applied"`    // count of Apply(...) == true over the engine's life
	Duplicates int64          `json:"duplicates"` // count of idempotent no-op deliveries

	// UptimeSec is how long this process has been running. Every per-peer
	// age above is in-memory and starts at "never" on each start, so "never
	// contacted" after a restart meant "not since this post started" — and
	// read to an officer as "this post has never heard from it" for a whole
	// partition (sc-test L08 T-074). Pages say "since this post started N
	// ago" with this instead.
	UptimeSec float64 `json:"uptime_seconds"`
}

type peerState struct {
	// formerWarned (FIX16 H): per retired site, whether the last
	// drainFormer attempt from this peer failed (gates its log line).
	// Guarded by Engine.mu.
	formerWarned map[string]bool

	cfg config.Peer

	// runMu serialises actual pull work for this peer (routine loop, manual
	// PullPeer, and the poke coalescer all funnel through it).
	runMu sync.Mutex
	// coalescer latch: a poke burst folds into "one more pull after this one".
	cmu     sync.Mutex
	pending bool
	running bool

	// The SENDER-side poke coalescer (W7 / Y-17, 2026-10-03), the mirror of
	// cmu/pending/running above: a burst of own writes folds into at most
	// one poke in flight to this peer plus one more pending. Needed the
	// moment the poke went live on every committed write (store.OnAuthored
	// → PokePeers): the old one-goroutine-per-poke shape would have parked
	// one blocked HTTP request per write per unreachable peer for the full
	// 10 s client timeout — 420 writes in a 34 s burst (L12 Y-30) is 1 680
	// goroutines against a dead link. The trailing poke is what matters:
	// the peer's pull after it reads the head as it stands then, so the
	// last write of the burst is always covered.
	pmu         sync.Mutex
	pokePending bool
	pokeRunning bool

	// Guarded by Engine.mu:
	lastContact time.Time
	lastError   string
	drains      int64
	// The §9.3 anchor. On every pull cycle that drains ALL of this author's
	// announced streams to their heads, lastDrain <- the instant that
	// cycle STARTED. Anchor() exposes it: a B-local instant at which every
	// event the author had already written was about to be delivered — so
	// an event NOT delivered by that cycle was authored after it, and
	// G = receipt − lastDrain is never LESS than the event's true time in
	// flight (erring early, never late). Consequences, deliberate and
	// load-bearing:
	//   - routine path: G = receipt − (start of the previous drained cycle)
	//     ≤ one sync interval plus that cycle's own duration;
	//   - paginated catch-up: batches never touch the slot, and every
	//     envelope the catch-up delivers inherits the PRE-OUTAGE drain
	//     instant — a partition-expired trip can never earn a fresh
	//     countdown from batch arithmetic (§9.3's pinned anchor);
	//   - a "refreshed at drain END" rule would be wrong: an event authored
	//     during a cycle, after its head was read, is delivered by the NEXT
	//     cycle and would get a G shorter than its true time in flight.
	//     That is why the slot records the START.
	// R10, James 2026-10-04 (FINDINGS-R4 D6): anchor at the latest drain,
	// not the one before it. ⟦was: two slots, prev <- last, last <- END of
	// cycle, and Anchor() exposed PREV — G ≈ two sync intervals, so the
	// destination turned OVERDUE 10–20 s before the origin on every trip
	// (5 lanes, MEASURED). Recording the start makes the latest slot safe
	// to expose, which the end-of-cycle instant was not.⟧
	// These are process-local monotonic instants (time.Time carries Go's
	// monotonic reading). They are deliberately NOT persisted to the store:
	// no wall-clock column exists there by design, and a monotonic instant is
	// meaningless across a restart — after a restart the anchor is absent
	// until the first full drain completes, so everything that drain
	// delivers is captured without an anchor and C7's consumers err early
	// (§9.3's safe direction) rather than trusting a resurrected instant.
	lastDrain time.Time

	// errLog and holdLog gate the F14 observability log lines (campaign
	// finding: sustained sync errors were visible only on the status page,
	// never in journalctl). errLog tracks this peer's reachability
	// (notePeerError / the recovery branch in drainSite); holdLog tracks
	// whether this peer's own later epochs are currently being held back by
	// a stalled earlier one (drainSite's epoch-major loop). Both log once on
	// a state CHANGE and at most once per sustainedErrorLogInterval while
	// the SAME bad state persists -- never once per pull cycle.
	errLog  logGate
	holdLog logGate

	// The §8.6 self-history latch (design/app-fixes-v1.md §2.1). Guarded by
	// Engine.mu like lastError/drains above — BOTH writes (Rejoin's and
	// pullPeer's retry arm) take the lock explicitly; production ordering is
	// not the guard.
	//
	// selfDrained latches true when one drainSite(ctx, this peer,
	// e.cfg.SiteID) call returned (drainedAll=true, err=nil): the verified
	// store outcome "this peer delivered everything it claims to hold for
	// our own site", through the full ingest defences. In-memory only,
	// deliberately not persisted — same rationale as the anchor slots
	// above: a restart re-runs Rejoin anyway, and a resurrected latch after
	// a wipe would be exactly the false green this latch exists to kill.
	//
	// selfVacuous is true iff the latch was satisfied by a verified answer
	// containing zero PRE-WIPE streams for our site (announced streams with
	// NodeEpoch < e.cfg.NodeEpoch): the peer answered, correctly identified
	// itself, and announced no pre-wipe history for us. Post-wipe
	// (current-epoch) streams do NOT count — a peer echoing back our own
	// post-wipe authoring is not evidence of recovery (W7). Set at the same
	// instant as selfDrained, under the same lock.
	//
	// selfLog gates the retry's own steady-state failure lines
	// (noteSelfHistoryError) — its OWN channel, never lastError/errLog: a
	// self-history-only failure must not toggle this peer's reachability
	// verdict (W3).
	selfDrained bool
	selfVacuous bool
	selfLog     logGate

	// The D26 state (PeerSnapshot's doc), guarded by Engine.mu.
	//   errKind      — lastError's category (ErrorKind*), set with it.
	//   pokeRefused  — the last poke was answered 401/403; pokeLog gates its
	//                  log line separately from errLog, so a peer that takes
	//                  our pulls but refuses our pokes cannot flap errLog
	//                  between "rejected" and "accepted" once per write.
	//                  pokeRefusedKind is which of the two (FIX3 F2/F6).
	//   pokeErr      — the last poke failed some other way (FIX3 F7); same
	//                  gate, its own states, so it logs once per transition.
	//   inRefusedAt / inOKAt — the last time this post's server refused /
	//                  accepted this peer's sync request; inLog gates the line.
	//   ownBehindSince / ownBehindTarget — when this peer was first seen
	//                  holding less of our current stream than we did, and
	//                  what we held then. The lag ends when the peer reaches
	//                  that target, so a peer that keeps up with a long burst
	//                  of writes (always a little behind, always moving)
	//                  never accrues age; only a peer that is STUCK does.
	//   ownCopyHead  — the peer's last-reported copy of our current stream.
	//   ownLagLog    — gates the not-receiving line.
	errKind         string
	pokeRefused     bool
	pokeRefusedKind string
	pokeErr         string
	pokeErrKind     string
	pokeLog         logGate
	reconnectPoke   bool // a peer-log exchange failed since the last answered one: poke on the next (P1a)
	inRefusedAt     time.Time
	inOKAt          time.Time
	inLog           logGate
	ownBehindSince  time.Time
	ownBehindTarget int64
	ownCopyHead     int64
	ownLagLog       logGate
}

// Engine is one site's replication engine: it SERVES its own log (server.go)
// and PULLS every configured peer's log. Nothing in it assumes any particular
// peer count — peers are a config list (chunk-plan C4 note).
type Engine struct {
	cfg *config.Config
	st  *store.Store

	// Client performs all outbound exchanges. Replaceable in tests.
	Client *http.Client
	// PageLimit is the client-side pull page size (server caps at
	// serverMaxPage regardless). Tests shrink it to force pagination.
	PageLimit int64

	// OnApplied, if set before any pulling starts, fires synchronously for
	// every envelope the pull path APPLIES (never for duplicates or
	// refusals), carrying the §9.3 anchor as it stood at delivery — read
	// BEFORE the drain that delivers the envelope can rotate the slots.
	//
	// Why this exists (the drain-relative currency property): Anchor() is
	// valid only for events the LATEST drain delivered; every rotation
	// silently invalidates it for older uncaptured events, and the store
	// deliberately records no receipt time, so a later scan can never detect
	// that it missed the rotation. A consumer that needs G at receipt (C7:
	// remaining-at-receipt) must therefore capture it here, synchronously,
	// not by polling Anchor() afterwards. The hook runs on the pull
	// goroutine inside the page loop: keep it fast, and never pull the same
	// peer synchronously from inside it (requestPull is safe — it is async).
	OnApplied func(env store.Envelope, anchor time.Time, anchorOK bool)

	peers map[string]*peerState // keyed by peer site_id; built once in New

	// Lifecycle: baseCtx bounds all engine-spawned async work (pokes,
	// coalesced poke-pulls); Shutdown cancels it and waits for the work to
	// finish. Synchronous calls (PullAll/PullPeer) are bounded by their
	// caller's ctx instead.
	baseCtx context.Context
	stop    context.CancelFunc
	wg      sync.WaitGroup

	mu      sync.Mutex // guards streams, maxHead, counters, rejoinArmed, and peerState's mu-tagged fields
	streams map[StreamKey]*StreamStatus
	// rejoinArmed is set by Rejoin on entry and never cleared. Only an
	// ARMED engine's pullPeer retries the §8.6 self-history drain: this
	// keeps PullAll's contract exactly "every configured peer's log" for
	// any caller that never entered the §8.6 sequence (design
	// app-fixes-v1.md §2.4 — the peer-log-only replay semantics), and makes
	// the retry precisely completion of Rejoin's own contract. Production
	// always arms: main.go calls Rejoin unconditionally before serving.
	rejoinArmed bool
	// maxHead remembers the highest head ever reported per stream — the
	// C3b §8.9 regression detector's memory. Keyed by StreamKey (site_id +
	// node_epoch), so a genuine re-image (a new, higher epoch) is a new key
	// and can never trigger a false regression against the old epoch's peak.
	maxHead    map[StreamKey]int64
	applied    int64
	duplicates int64

	// streamLog gates the F14 per-stream observability log line (see
	// setStream): one entry per StreamKey, same state-change-or-heartbeat
	// discipline as peerState's errLog/holdLog.
	streamLog map[StreamKey]*logGate

	// leftBehindBy records, per own-stream key, WHICH peers currently hold
	// more of that stream than we do (checkLeftBehind's alarm branch adds
	// the peer; its healthy branch removes it). The own row is ONE row
	// written from N peers' points of view, one after another in the same
	// PullAll cycle, so a healthy report from one peer must not erase an
	// alarm a different peer raised moments earlier — the row clears only
	// when the set is empty (W7, 2026-10-03; see checkLeftBehind).
	leftBehindBy map[StreamKey]map[string]bool

	// started is this process's start (New), the base of Snapshot.UptimeSec.
	started time.Time
}

// HTTPStatusError is a non-200 answer from a peer. The peer was REACHED and
// answered, so this is never a transport failure; notePeerError classifies
// 401 (this post's key refused) and 403 (this post's identity refused) from
// it as configuration mismatches, not outages (W7 / Y-31, 2026-10-03 — the
// L12 lane found a wrong peer key reported under "PEER UNREACHABLE … check
// the link", the one diagnosis that was wrong). Error() keeps the exact text
// doJSON always produced, which /debug/sync-status and the status page
// surface as last_error.
type HTTPStatusError struct {
	Method, Path string
	Code         int
	Status       string // e.g. "401 Unauthorized"
	Body         string // first 512 bytes of the peer's answer
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("repl: %s %s: %s: %s", e.Method, e.Path, e.Status, e.Body)
}

func New(cfg *config.Config, st *store.Store) *Engine {
	baseCtx, stop := context.WithCancel(context.Background())
	e := &Engine{
		cfg:       cfg,
		st:        st,
		Client:    &http.Client{Timeout: 10 * time.Second},
		PageLimit: defaultPageLimit,
		peers:     make(map[string]*peerState, len(cfg.Peers)),
		streams:   make(map[StreamKey]*StreamStatus),
		maxHead:   make(map[StreamKey]int64),
		streamLog: make(map[StreamKey]*logGate),
		baseCtx:   baseCtx,
		stop:      stop,

		leftBehindBy: make(map[StreamKey]map[string]bool),
		started:      time.Now(),
	}
	for _, p := range cfg.Peers {
		e.peers[p.SiteID] = &peerState{cfg: p}
	}
	return e
}

// Run is the ROUTINE pull timer: pull every peer each sync interval until ctx
// is cancelled. This is the ONLY place in the engine that creates a timer —
// a process that never calls Run has no routine timer at all, which is what
// makes the poke-latency test structurally incapable of passing off a timer
// fire (chunk-plan C3a not-done-if).
func (e *Engine) Run(ctx context.Context) {
	t := time.NewTicker(e.cfg.SyncInterval())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.baseCtx.Done(): // Shutdown stops the routine loop too
			return
		case <-t.C:
			e.PullAll(ctx)
		}
	}
}

// Shutdown stops the engine: the routine loop (if running) exits, in-flight
// engine-spawned async work (pokes, coalesced poke-pulls) is cancelled and
// awaited, and later pokes to this engine become no-ops. Synchronous
// PullAll/PullPeer calls still in a caller's hands are the caller's to bound
// via their own ctx. Idempotent.
func (e *Engine) Shutdown() {
	e.stop()
	e.wg.Wait()
}

// PullAll synchronously pulls every configured peer, sequentially. A failing
// peer never aborts the others; failures land in the status surface.
func (e *Engine) PullAll(ctx context.Context) {
	for _, p := range e.cfg.Peers {
		e.pullPeer(ctx, e.peers[p.SiteID])
	}
}

// PullPeer synchronously pulls one peer by site id.
func (e *Engine) PullPeer(ctx context.Context, siteID string) error {
	ps, ok := e.peers[siteID]
	if !ok {
		return fmt.Errorf("repl: %q is not a configured peer", siteID)
	}
	return e.pullPeer(ctx, ps)
}

// Author appends an event on this site's own stream, then pokes every peer —
// the §8.9 fast path. Records are never pushed; the poke carries nothing and
// each poked peer pulls from its own watermark.
//
// ⟦W7 / Y-17, 2026-10-03⟧ This is the engine-level convenience the tests
// use; NOTHING in the product calls it. Every door authors through
// store.Author — alone, or inside a Transact commit (store/transact.go) —
// and until today that path reached no poke at all, so the "normally ≤1-3 s"
// fast path the design promised was dead and replication was tick-only:
// single-write latencies of 3-17 s measured over 38 samples on the L12 lane
// (sc-test L12 Y-17, rounds 1 and 2). The poke is now wired at the store:
// store.OnAuthored fires once per COMMITTED authoring transaction, after the
// commit, and cmd/sitectl/main.go sets it to PokePeers. When that hook is
// wired, this method does not poke a second time — the store already did.
func (e *Engine) Author(payload json.RawMessage, occurredAtMS int64, timeSource string) (store.Envelope, error) {
	env, err := e.st.Author(e.cfg.SiteID, e.cfg.NodeEpoch, payload, occurredAtMS, timeSource)
	if err != nil {
		return env, err
	}
	if e.st.OnAuthored == nil {
		e.PokePeers()
	}
	return env, nil
}

// PokePeers pokes every peer asynchronously and returns at once — it is the
// store.OnAuthored hook (cmd/sitectl/main.go), running on the authoring
// goroutine right after a commit, so nothing here may block or fail the
// write (which is already durable). Per peer, pokes coalesce into at most
// one in flight plus one pending (pokePeer); a failed poke is recorded as
// that peer's last_error but never retried here — the routine pull timer is
// the backstop, exactly as before (best-effort by ruling: FIX-PLAN §3 Y-17).
func (e *Engine) PokePeers() {
	if e.baseCtx.Err() != nil {
		return // shut down: no new outbound work
	}
	for _, p := range e.cfg.Peers {
		e.pokePeer(e.peers[p.SiteID])
	}
}

// pokePeer is the sender-side coalescer (see peerState.pmu): the mirror of
// requestPull on the receiving side. A poke carries nothing, so two pokes
// queued behind an in-flight one are worth exactly one — the peer's pull
// after the trailing poke reads the head as it stands then.
func (e *Engine) pokePeer(ps *peerState) {
	ps.pmu.Lock()
	ps.pokePending = true
	if ps.pokeRunning {
		ps.pmu.Unlock()
		return
	}
	ps.pokeRunning = true
	ps.pmu.Unlock()
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		for {
			ps.pmu.Lock()
			if !ps.pokePending || e.baseCtx.Err() != nil {
				ps.pokeRunning = false
				ps.pmu.Unlock()
				return
			}
			ps.pokePending = false
			ps.pmu.Unlock()
			var resp PokeResponse
			err := e.doJSON(e.baseCtx, ps, http.MethodPost, "/sync/poke", url.Values{}, &resp)
			e.notePokeResult(ps, err)
		}
	}()
}

// notePokeResult records one poke's outcome (never retried here — the tick
// is the backstop, and it re-probes while a failure stands: pullPeer). A
// poke answered 401/403 sets pokeRefused and writes a gated PEER REFUSES THIS
// POST'S POKES line (D26 / R11). Any other failure — a 503, a transport
// error — sets pokeErr and writes a gated POKES ARE FAILING line (FIX3 F7):
// before, it left only a last_error the next successful pull erased, and no
// line at all. Neither touches last_error any more: that is the PULL's
// verdict, and a poke that fails while pulls succeed must not turn a
// current peer STALE (F2's harm by another door). An accepted poke clears
// both, with one clearing line if a failure was logged.
func (e *Engine) notePokeResult(ps *peerState, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	if err == nil {
		wasRefused, wasFailed := ps.pokeRefused, ps.pokeErr != ""
		ps.pokeRefused, ps.pokeRefusedKind = false, ""
		ps.pokeErr, ps.pokeErrKind = "", ""
		if (wasRefused || wasFailed) && ps.pokeLog.shouldLog("", now) {
			if wasRefused {
				log.Printf("repl: PEER ACCEPTS THIS POST'S POKES AGAIN: peer %s -- the key/identity refusal on pokes has cleared", ps.cfg.SiteID)
			} else {
				log.Printf("repl: POKES REACH PEER %s AGAIN: its poke was accepted -- it is told of this post's records at once again", ps.cfg.SiteID)
			}
		}
		return
	}
	kind := errorKind(err)
	if kind != ErrorKindKeyRejected && kind != ErrorKindIdentityRejected {
		ps.pokeErr, ps.pokeErrKind = "poke: "+err.Error(), kind
		if ps.pokeLog.shouldLog("poke_"+kind, now) {
			log.Printf("repl: POKES ARE FAILING (not a key refusal): peer %s: the poke got %s -- %s; this post's records still reach it on its own routine pull (within one sync interval if that works), only the fast path is lost; see /status", ps.cfg.SiteID, httpStatusOf(err), pokeFailureWords(kind, err.Error()))
		}
		return
	}
	ps.pokeErr, ps.pokeErrKind = "", ""
	ps.pokeRefused, ps.pokeRefusedKind = true, kind
	if ps.pokeLog.shouldLog(kind, now) {
		log.Printf("repl: PEER REFUSES THIS POST'S POKES (not an outage): peer %s answered %s -- %s; this post's records reach it only on its own routine pull, if at all; see /status", ps.cfg.SiteID, httpStatusOf(err), refusalReason(e.cfg.SiteID, ps.cfg.SiteID, kind))
	}
}

// ConnectionDropped reports whether a transport error's text is a connection
// that was ACCEPTED and then closed with no answer (EOF, reset) rather than
// one nothing answered at all. Both stay ErrorKindUnreachable — the remedy
// is the same place (the link or the peer process) — but "nothing answered"
// overstates a drop: something did answer (fix4 H, R6-2.3; L02 observation).
func ConnectionDropped(errText string) bool {
	return strings.HasSuffix(errText, ": EOF") || strings.HasSuffix(errText, "unexpected EOF") ||
		strings.Contains(errText, "connection reset by peer")
}

// pokeFailureWords names a non-refusal poke failure for the log (F7).
func pokeFailureWords(kind, errText string) string {
	switch kind {
	case ErrorKindUnreachable:
		if ConnectionDropped(errText) {
			return "the connection to the peer's address was dropped before it answered (the peer may have stopped mid-request, or the link is unstable)"
		}
		return "nothing answered at the peer's address (it may be down, or the link is cut)"
	case ErrorKindWrongSite:
		return "a different post answered at the peer's address"
	default:
		return "the peer (or something in front of it) answered, but did not take the poke"
	}
}

// errorKind classifies a failed exchange (ErrorKind*). An HTTPStatusError is
// an answer; a *url.Error is the transport (nothing answered); anything else
// — an undecodable body, a different site_id — is a bad answer.
func errorKind(err error) string {
	var se *HTTPStatusError
	if errors.As(err, &se) {
		switch se.Code {
		case http.StatusUnauthorized:
			return ErrorKindKeyRejected
		case http.StatusForbidden:
			return ErrorKindIdentityRejected
		}
		return ErrorKindBadAnswer
	}
	var we *wrongSiteError
	if errors.As(err, &we) {
		return ErrorKindWrongSite
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return ErrorKindUnreachable
	}
	return ErrorKindBadAnswer
}

func httpStatusOf(err error) string {
	var se *HTTPStatusError
	if errors.As(err, &se) {
		return se.Status
	}
	return "an error"
}

// refusalReason is the one sentence naming what a 401/403 means, shared by
// the pull-side (notePeerError) and poke-side lines.
func refusalReason(own, peer, kind string) string {
	if kind == ErrorKindIdentityRejected {
		return fmt.Sprintf("it REFUSED THIS POST'S IDENTITY — %s does not list this post (%s) in its own [[peers]], or this post asked it for a stream it does not serve (§8.4, no transitive relay)", peer, own)
	}
	return fmt.Sprintf("it REFUSED THIS POST'S PEER KEY — the `key` in this post's [[peers]] block for %s does not match %s's own `peer_key`", peer, peer)
}

// wrongSiteError is drainSite's "a different post answered" refusal, typed
// so errorKind can tell it from a transport failure. Error() is the exact
// text drainSite always produced.
type wrongSiteError struct{ baseURL, got, want string }

func (e *wrongSiteError) Error() string {
	return fmt.Sprintf("repl: peer at %s claims site_id %q, expected %q", e.baseURL, e.got, e.want)
}

// NoteInbound records, for the configured peer a sync request names in its
// X-SC-From header, whether this post's server accepted its key (D26, the
// L12-Y31b mechanism: a post whose own peer_key changed refuses every peer's
// pull, yet every call it MAKES still succeeds, so nothing else here could
// see it). An unknown or absent X-SC-From is ignored — it names nobody this
// post could report on. The header is unauthenticated, so a forged refused
// request can at worst make this post report a key problem it does not
// have: the narrowing-toward-false direction, never a false green.
func (e *Engine) NoteInbound(from string, accepted bool) {
	ps, ok := e.peers[from]
	if !ok {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	if accepted {
		ps.inOKAt = now
		if ps.inLog.shouldLog("", now) {
			log.Printf("repl: PEER CAN READ THIS POST AGAIN: peer %s presented the right key for this post -- its pulls of this post's records are accepted again", ps.cfg.SiteID)
		}
		return
	}
	ps.inRefusedAt = now
	prev := ps.inLog.lastState
	if ps.inLog.shouldLog(peerKeyRejectedState, now) {
		verb := "PEER CANNOT READ THIS POST"
		if prev == peerKeyRejectedState {
			verb = "PEER STILL CANNOT READ THIS POST"
		}
		log.Printf("repl: %s (key mismatch, not an outage): peer %s asked for this post's records and this post REFUSED it with 401 -- the `key` in %s's [[peers]] block for this post (%s) does not match this post's own `peer_key`; nothing recorded at this post reaches %s until one side is corrected; see /status", verb, ps.cfg.SiteID, ps.cfg.SiteID, e.cfg.SiteID, ps.cfg.SiteID)
	}
}

// requestPull schedules an asynchronous, coalesced pull of one peer — the
// receiving half of poke-then-pull. A burst of pokes folds into at most one
// pull already running plus one more pending.
func (e *Engine) requestPull(siteID string) {
	ps, ok := e.peers[siteID]
	if !ok || e.baseCtx.Err() != nil {
		return // unknown peer, or shut down: pokes become no-ops
	}
	ps.cmu.Lock()
	ps.pending = true
	if ps.running {
		ps.cmu.Unlock()
		return
	}
	ps.running = true
	ps.cmu.Unlock()
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		for {
			ps.cmu.Lock()
			if !ps.pending || e.baseCtx.Err() != nil {
				ps.running = false
				ps.cmu.Unlock()
				return
			}
			ps.pending = false
			ps.cmu.Unlock()
			e.pullPeer(e.baseCtx, ps)
		}
	}()
}

// pullPeer runs one full pull cycle against one peer: list the author's
// streams, pull each to its head, and — only if EVERY announced stream
// drained — record the §9.3 anchor slots.
func (e *Engine) pullPeer(ctx context.Context, ps *peerState) error {
	ps.runMu.Lock()
	defer ps.runMu.Unlock()

	// The §8.6 self-history RETRY (design/app-fixes-v1.md §2.1 piece 4):
	// on an ARMED engine (Rejoin ran), a peer whose self-history latch has
	// not set yet is re-asked for our own site's history BEFORE its
	// peer-log drain, every cycle, until one drain verifiably completes.
	// This is completion of Rejoin's own contract on the routine path — a
	// boot-time recovery failure (offline install, link down) heals on the
	// next cycle the link exists, instead of never.
	//
	// ⚠ ORDER (R-20, 2026-09-13). This drain lands our OWN EARLIER epoch
	// AFTER everything this incarnation has already authored and folded on
	// the current epoch — a site's earlier stream delivered after its later
	// one, at the AUTHOR. The epoch-major rule in drainSite orders a site's
	// epochs within ONE peer's announcement; it cannot order our own
	// authoring against a stream we did not yet hold. app-fixes-v1 §2.4
	// weighed only CROSS-SITE interleaving here and called it a non-
	// invariant; the SAME-SITE cross-epoch case it did not weigh produced a
	// wrong muster at the author (D87-D92-COHERENCE.md, measured through
	// this engine: the epoch-1 dispatch closed the epoch-2 visit). The
	// presence folds are therefore order-independent across a site's own
	// epochs since SchemaVersion 15 (domain/visit_open.go: is_open is a
	// set-function of the raw rows) — this path no longer needs any
	// ordering promise from anyone, and carry-forward row 28 is recorded
	// FALSIFIED a second time rather than re-guarded.
	e.mu.Lock()
	needSelf := e.rejoinArmed && !ps.selfDrained
	e.mu.Unlock()
	if needSelf {
		if drained, preWipe, err := e.drainSite(ctx, ps, e.cfg.SiteID); err == nil && drained {
			e.mu.Lock()
			ps.selfDrained = true
			ps.selfVacuous = preWipe == 0
			e.mu.Unlock()
			log.Printf("repl: REJOIN: self-history recovered from peer %s (retry after failed boot-time rejoin)", ps.cfg.SiteID)
		} else {
			// W3: the gated steady-state channel — never notePeerError,
			// never lastError. NOTE: err is nil on a partial drain
			// (err == nil && !drained); noteSelfHistoryError composes its
			// detail from both values and handles nil err.
			e.noteSelfHistoryError(ps, err, drained)
		}
	}

	// R10: the cycle's START is the anchor candidate (see peerState) —
	// taken before the stream discovery, so every event this cycle can
	// miss was authored after it.
	cycleStart := time.Now()
	drainedAll, _, err := e.drainSite(ctx, ps, ps.cfg.SiteID)
	if err != nil {
		return err
	}

	// Tier 1 left-behind check (StateLeftBehind, LEFT-BEHIND-DETECT.md),
	// POST-latch only: while needSelf above is still true, the boot-time
	// self-history retry is already asking this exact question every cycle
	// (and pulling — this call deliberately never does). Once it latches,
	// THIS reuses the very same routine pull cycle rather than a second
	// timer: one more GET /sync/streams per peer per sync_interval_seconds
	// is the identical cost the pre-latch retry already paid every cycle,
	// just paid for the rest of the process's life instead of stopping at
	// the latch — no new goroutine, no new ticker.
	e.mu.Lock()
	leftBehindCheck := e.rejoinArmed && ps.selfDrained
	e.mu.Unlock()
	if leftBehindCheck {
		e.checkLeftBehind(ctx, ps)
	}

	// ⟦FIX16 H⟧ every retired post's stream this peer holds — so a post
	// rebuilt blank after a retirement gets that history back from any
	// live post. Same pull, same seam echo, same ApplyBatch contiguity and
	// idempotence as every other stream; a copy already held is a no-op.
	e.drainFormer(ctx, ps)

	if drainedAll {
		// This pull drained to the author's current head: B now holds
		// everything A had written when the cycle started (see peerState).
		e.mu.Lock()
		ps.lastDrain = cycleStart
		ps.drains++
		e.mu.Unlock()
	}
	return nil
}

// drainSite asks peer ps what it holds for `site` and pulls every announced
// stream to its head. This is the ONE stream-discovery-and-catch-up
// routine: pullPeer calls it for a peer's own log (site == ps.cfg.SiteID,
// every sync interval, all season), and the §8.4 self-history exception
// (site == e.cfg.SiteID, our own identity) reaches it from Rejoin at boot
// and from pullPeer's armed retry — the identical routine catch-up path,
// never a second, parallel implementation. The pull/ingest path itself
// special-cases nothing; the ONE caller-keyed branch here is the transport
// arm's error reporting (W3): a peer-log drain failure is a reachability
// fact (notePeerError, as always), while a self-history drain failure is
// reported by the CALLER (Rejoin's loud FAILED line, or the retry's
// noteSelfHistoryError channel) — a self-history-only failure must never
// toggle the peer's reachability verdict. Site-keying inside drainSite is
// existing house style, not novelty: the noteEpochHold call below is
// already keyed site == ps.cfg.SiteID. The identity-echo mismatch arm
// stays unconditional — a peer answering with the wrong identity is a
// mis-wired peer, not a self-history-only failure.
//
// preWipe counts the announced streams that carry
// NodeEpoch < e.cfg.NodeEpoch — pre-wipe streams only (W7). The filter
// lives HERE so both latch sites (Rejoin and pullPeer's retry) share the
// identical predicate by construction; the count is meaningful only for
// the self-history call (site == e.cfg.SiteID), and the peer-log call
// sites ignore it.
//
// Caller holds ps.runMu.
func (e *Engine) drainSite(ctx context.Context, ps *peerState, site string) (drainedAll bool, preWipe int, err error) {
	var sr StreamsResponse
	q := url.Values{"site": {site}}
	if err := e.doJSON(ctx, ps, http.MethodGet, "/sync/streams", q, &sr); err != nil {
		if site == ps.cfg.SiteID {
			e.notePeerError(ps, err) // peer-log drain: reachability verdict as today
		}
		return false, 0, err // self-history / retired-stream drain: the CALLER reports (Rejoin's FAILED line, noteSelfHistoryError, drainFormer)
	}
	if sr.SiteID != ps.cfg.SiteID {
		err := &wrongSiteError{baseURL: ps.cfg.BaseURL, got: sr.SiteID, want: ps.cfg.SiteID}
		e.notePeerError(ps, err)
		return false, 0, err
	}
	e.mu.Lock()
	ps.lastContact = time.Now()
	wasBad := ps.errLog.lastState
	ps.lastError = ""
	ps.errKind = ""
	if wasBad != "" && ps.errLog.shouldLog("", time.Now()) {
		if wasBad == peerKeyRejectedState {
			log.Printf("repl: PEER ACCEPTS THIS POST AGAIN: peer %s (%s) answered this post's sync request -- the key/identity refusal has cleared; sync resumed; see /debug/sync-status", ps.cfg.SiteID, ps.cfg.BaseURL)
		} else {
			log.Printf("repl: PEER REACHABLE AGAIN: peer %s (%s) -- sync resumed; see /debug/sync-status", ps.cfg.SiteID, ps.cfg.BaseURL)
		}
	}
	// FIX3 F2 (FINDINGS-R5 re-run A3, MEDIUM): this request carried the same
	// key a poke carries, through the same requireKey, and was answered — so
	// a 401 poke refusal is disproved now, not at this post's next write.
	// A 403 is not: a peer that does not list this post still serves its
	// pulls. That one is re-probed below.
	if ps.pokeRefused && ps.pokeRefusedKind == ErrorKindKeyRejected {
		ps.pokeRefused, ps.pokeRefusedKind = false, ""
		if ps.pokeLog.shouldLog("", time.Now()) {
			log.Printf("repl: PEER ACCEPTS THIS POST'S KEY AGAIN: peer %s answered this post's sync request with the key its pokes carry -- the 401 refusal on pokes has cleared", ps.cfg.SiteID)
		}
	}
	// One poke, asynchronously, when (a) this is the first answered exchange
	// since a failed one (P1a/P2b: after a heal the far end may have nothing
	// new to write, so it waited for its own tick, ~10.5 s, to pull what this
	// post wrote meanwhile; a restarted peer is covered too, since this post's
	// pulls failed while it was down). Not on the first exchange after this
	// post starts: both ends would poke each other at boot for nothing. Or (b) a
	// poke failure still stands (F2/F7: re-probe, so a fixed peer clears
	// within a tick without a write here). Never on a steady healthy link.
	// ⟦FIX16 H⟧ keyed on the PEER-LOG drain (site == the peer's own id),
	// which for the two callers that existed before is exactly "site !=
	// our own id"; a retired-stream drain (drainFormer) neither pokes nor
	// consumes the reconnect poke.
	poke := site == ps.cfg.SiteID && (ps.reconnectPoke || ps.pokeRefused || ps.pokeErr != "")
	if site == ps.cfg.SiteID {
		ps.reconnectPoke = false
	}
	e.mu.Unlock()
	if poke && e.baseCtx.Err() == nil { // shut down: no new outbound work (PokePeers' rule)
		e.pokePeer(ps)
	}

	// EPOCH-MAJOR DELIVERY (campaign finding F8). A site's history is
	// ordered (node_epoch, seq), epoch-major (§8.3) — but this loop used to
	// continue past a failed epoch, so a transient failure on an epoch-1
	// pull let epoch-2 events of the SAME site apply first. That cross-epoch
	// reorder breaks the row-28 premise ("same-site events cannot reorder
	// relative to each other"): a visit created in epoch 1 and checked out
	// in epoch 2 applied checkout-first at the replica, the checkout no-oped
	// against the missing row, and the later arrival inserted is_open=1 —
	// a departed person held on the replica's muster while sync status read
	// ok. One link blip during a paginated multi-epoch catch-up was enough.
	//
	// So: pull this site's epochs in ascending order and STOP at the first
	// epoch that does not drain to its head — later epochs are held back,
	// never pulled ahead of an earlier one. This deliberately NARROWS
	// carry-forward row 4's per-stream isolation to CROSS-SITE only: an
	// old-epoch stall (transient failure, or a terminal one like a §8.5
	// schema mismatch) now stalls that whole SITE's delivery, which is
	// correct — applying its later epochs first is precisely the unsafe
	// reorder — while one bad peer still never stalls a good peer (PullAll's
	// per-peer isolation is untouched, and drainSite only ever handles one
	// site). The sort is local: announcement order is wire input, and
	// ordering is now a correctness invariant, so it is not trusted.
	//
	// ⟦SchemaVersion 15, R-20 — 2026-09-13⟧ "the row-28 premise" above is
	// FALSIFIED A SECOND TIME, and this rule is no longer what keeps the
	// muster right. This sort orders a site's epochs within one peer's
	// announcement; it never could order a post's OWN authoring against its
	// own earlier epoch recovered later through the self-history retry
	// (pullPeer) — which is a site's earlier stream after its later one,
	// at the author, and produced a wrong muster there (D87-D92-
	// COHERENCE.md). The visit folds now derive is_open from the raw rows
	// (domain/visit_open.go) and are order-independent across a site's own
	// epochs, so the muster is right under ANY delivery order. Epoch-major
	// delivery stays: it is still the right order to deliver in, and the
	// hold-back is still what keeps a §8.5 mismatch on an old epoch from
	// letting newer state past it — but nothing about presence rests on it.
	streams := append([]StreamHead(nil), sr.Streams...)
	sort.Slice(streams, func(i, j int) bool { return streams[i].NodeEpoch < streams[j].NodeEpoch })
	for _, sh := range streams {
		if sh.NodeEpoch < e.cfg.NodeEpoch {
			preWipe++ // W7: only pre-wipe streams count toward the vacuous predicate
		}
	}
	drainedAll = true
	stalledAt := -1
	for i, sh := range streams {
		// authorCurrent: this is the PEER's own log and this is the highest
		// epoch it announces — its current, config-seeded one (handleStreams
		// announces it even when empty). Only there can "author head below
		// what we hold" mean lost history; see pullStream (W7 / Y-25).
		authorCurrent := site == ps.cfg.SiteID && i == len(streams)-1
		if !e.pullStream(ctx, ps, site, sh.NodeEpoch, authorCurrent) {
			drainedAll = false
			stalledAt = i
			break // epoch-major: never pull a later epoch past a stalled earlier one
		}
	}
	if site == ps.cfg.SiteID {
		// Only the ongoing peer-log case (site == ps.cfg.SiteID) gets the
		// F14 hold log here, deliberately: self-history pulls (site ==
		// e.cfg.SiteID) DO repeat — Rejoin's boot-time drain plus
		// pullPeer's armed retry every cycle until the latch sets — but
		// their repeated stalls are already gated per-stream by setStream's
		// streamLog, and the retry's own failures have their own gated
		// channel (noteSelfHistoryError's selfLog). Adding the hold log to
		// the self-history case would double-report the same stall.
		e.noteEpochHold(ps, streams, stalledAt, drainedAll)
	}
	return drainedAll, preWipe, nil
}

// checkLeftBehind is the Tier 1 left-behind-author check (StateLeftBehind;
// LEFT-BEHIND-DETECT.md, SYNCSTATUS-HONESTY.md item 3). Called by pullPeer
// once a peer's self-history latch has closed, every routine cycle: ask
// THAT SAME peer what it holds of OUR OWN site's streams — the identical
// GET /sync/streams?site=<own> exchange drainSite's self-history arm uses —
// and compare the announced head against our local watermark for that exact
// (site, epoch). Nothing here pulls a page or applies anything; recovering
// the tail is Tier 2, gated, an operator's own decision (the doc) — this
// function only ever reads.
//
// Scope is deliberately every announced epoch, not just the current one:
// the same "peer head > our local watermark" comparison also catches the
// one-family-over case SYNCSTATUS-HONESTY.md item 3 asks about — a peer
// announcing OUR site at an epoch ABOVE our own configured node_epoch.
// node_epoch is config-seeded and only ever bumped by THIS post's own
// re-image (§8.2), so a peer legitimately holding a higher epoch of "us"
// cannot happen without this post's own config already being behind some
// other record of its own identity — store.Watermark returns 0 for an
// epoch this store has never seen (log.go: "a stream never heard from opens
// at zero"), so that case trips the exact same comparison below with no
// special-casing needed, and is reported as its own family in the detail
// string.
//
// A transport or identity-mismatch failure here is silently skipped: the
// SAME peer's ordinary peer-log drain runs earlier in this very pullPeer
// cycle and already reports reachability (notePeerError) — a second,
// redundant "unreachable" line from this call would just restate that same
// fact under a different name.
func (e *Engine) checkLeftBehind(ctx context.Context, ps *peerState) {
	var sr StreamsResponse
	q := url.Values{"site": {e.cfg.SiteID}}
	if err := e.doJSON(ctx, ps, http.MethodGet, "/sync/streams", q, &sr); err != nil {
		return
	}
	if sr.SiteID != ps.cfg.SiteID {
		return // identity mismatch: the peer-log drain in this same cycle already surfaces this
	}
	for _, sh := range sr.Streams {
		key := StreamKey{SiteID: e.cfg.SiteID, NodeEpoch: sh.NodeEpoch}
		wm, err := e.st.Watermark(key.SiteID, key.NodeEpoch)
		if err != nil {
			continue // store read failure: leave the row as last known (Snapshot's own-row refresh takes the same stance)
		}
		if sh.Head <= wm {
			// Healthy FROM THIS PEER: it holds no more of our own stream
			// than we do (see the false-positive reasoning above and in
			// the doc — at the SAME epoch this is the only legitimate
			// outcome). Written through setStream, not skipped, so a PRIOR
			// left-behind alarm on this exact key correctly clears (logged
			// as SYNC RECOVERED by the existing F14 gate) once an operator's
			// epoch-bump-and-rejoin recovery actually lands the missing
			// tail — but ONLY once no peer is still ahead of us, and never
			// over a different live fact about this row.
			//
			// ⟦W7, 2026-10-03⟧ The own row is one row written from N peers'
			// points of view, in config order, within one PullAll. One peer
			// holding no more than us says nothing about a DIFFERENT peer
			// that does (the alarm it raised seconds earlier in this same
			// cycle), nor about the self-history retry's live failure on
			// this row (pullPeer's needSelf arm: identity_rejected from a
			// peer whose latch is still open). Pre-fix, a healthy report
			// from the last peer in config order overwrote both every
			// cycle — SYNC ERROR then SYNC RECOVERED, ten seconds apart,
			// forever, and the cycle always ENDED green. So: refresh an OK
			// (or absent) row; clear a left_behind alarm only when the set
			// of peers still ahead of us is empty; leave any other state
			// alone — the arm that raised it re-asserts or clears it
			// itself. (The peer's copy-head written here is NOT run through
			// the §8.9 regression detector — see setStream, Y-15.)
			e.mu.Lock()
			if set := e.leftBehindBy[key]; set != nil {
				delete(set, ps.cfg.SiteID)
			}
			cur, stillAhead := e.streams[key], len(e.leftBehindBy[key])
			refresh := cur == nil || cur.State == StateOK || (cur.State == StateLeftBehind && stillAhead == 0)
			e.mu.Unlock()
			if refresh {
				e.setStream(key, wm, sh.Head, StateOK, "")
			}
			continue
		}
		e.mu.Lock()
		if e.leftBehindBy[key] == nil {
			e.leftBehindBy[key] = make(map[string]bool)
		}
		e.leftBehindBy[key][ps.cfg.SiteID] = true
		e.mu.Unlock()
		family := fmt.Sprintf("within our current node_epoch %d", e.cfg.NodeEpoch)
		switch {
		case sh.NodeEpoch > e.cfg.NodeEpoch:
			family = fmt.Sprintf("at node_epoch %d, ABOVE our configured node_epoch %d — this post's own config is behind some other record of its own identity", sh.NodeEpoch, e.cfg.NodeEpoch)
		case sh.NodeEpoch < e.cfg.NodeEpoch:
			family = fmt.Sprintf("at a pre-wipe node_epoch %d", sh.NodeEpoch)
		}
		e.setStream(key, wm, sh.Head, StateLeftBehind, fmt.Sprintf(
			"peer %s holds head %d of THIS post's own stream (site %s) %s, but this post's local watermark is only %d — this post is LEFT BEHIND of its own history (LEFT-BEHIND-DETECT.md, Tier 1 detection only); recovery is an operator running the epoch-bump-and-rejoin procedure, never automatic",
			ps.cfg.SiteID, sh.Head, key.SiteID, family, wm))
	}
	e.noteOwnCopy(ps, sr.Streams)
}

// noteOwnCopy is the other direction of the same answer (D26, sc-test
// L12-Y31b; ruling R11): the peer holding LESS of this post's current stream
// than this post does, and not catching up. Lag after a write is normal for
// up to a poke or a tick; a lag that outlives OwnCopyLagMultiple sync
// intervals means this post's records are not reaching that peer — the peer
// cannot read this post (key mismatch) or a one-way cut stops its pulls —
// and this post, whose own calls all succeed, had no other way to know.
// See peerState.ownBehindSince for why the lag is measured against a fixed
// target rather than "behind right now".
func (e *Engine) noteOwnCopy(ps *peerState, announced []StreamHead) {
	mine, err := e.st.Watermark(e.cfg.SiteID, e.cfg.NodeEpoch)
	if err != nil {
		return // store read failure: leave the last known state
	}
	var copyHead int64 // a peer that announces nothing of our current epoch holds none of it
	for _, sh := range announced {
		if sh.NodeEpoch == e.cfg.NodeEpoch {
			copyHead = sh.Head
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	ps.ownCopyHead = copyHead
	if !ps.ownBehindSince.IsZero() && copyHead >= ps.ownBehindTarget {
		ps.ownBehindSince = time.Time{} // caught up with what we held when the lag began
	}
	if ps.ownBehindSince.IsZero() && copyHead < mine {
		ps.ownBehindSince, ps.ownBehindTarget = now, mine
	}
	limit := OwnCopyLagMultiple * e.cfg.SyncInterval()
	if limit > 0 && !ps.ownBehindSince.IsZero() && now.Sub(ps.ownBehindSince) > limit { // limit 0: an unvalidated test config (validate refuses a 0 interval)
		prev := ps.ownLagLog.lastState
		if ps.ownLagLog.shouldLog("own_copy_stuck", now) {
			verb := "THIS POST'S RECORDS ARE NOT REACHING PEER"
			if prev == "own_copy_stuck" {
				verb = "THIS POST'S RECORDS STILL NOT REACHING PEER"
			}
			log.Printf("repl: %s %s: it holds this post's stream only up to %d while this post is at %d, for %s (more than %d sync intervals) -- check-ins recorded here are not reaching it; its pulls of this post are failing (a key mismatch, or a one-way cut); see /status", verb, ps.cfg.SiteID, copyHead, mine, now.Sub(ps.ownBehindSince).Round(time.Second), OwnCopyLagMultiple)
		}
	} else if ps.ownBehindSince.IsZero() && ps.ownLagLog.shouldLog("", now) {
		log.Printf("repl: THIS POST'S RECORDS ARE REACHING PEER %s AGAIN: it holds this post's stream up to %d", ps.cfg.SiteID, copyHead)
	}
}

// Rejoin implements the §8.6 rejoin sequence for a blank (re-imaged or
// replaced) node: own-history first, then every peer's log — both through
// the identical drainSite/pullStream routine catch-up path pullPeer uses
// every sync interval; Rejoin adds no parallel recovery logic of its own.
// It is synchronous and is meant to be called ONCE, before Run() starts the
// routine timer and before any Author call — that ordering is what makes
// the own-prior-epoch-first sequence (§8.6) observed during recovery,
// rather than merely eventual. Once it returns, authoring resumes at
// whatever node_epoch this site's config carries — config-seeded, never
// inferred (§8.2): a re-image bumps node_epoch in the TOML before this
// binary starts, so "resumes authoring at the config-seeded node_epoch+1"
// needs no arithmetic here (see
// TestBlankNodeRejoinsRecoveringOwnHistoryFirstThenPeerLogsThroughRoutineCatchUp,
// which authors immediately after Rejoin and asserts the resulting envelope
// carries the bumped epoch, fresh at seq 1).
//
// Safe to call unconditionally, even on a node that was never wiped: if no
// peer holds any history under our own site_id, the self-history step pulls
// zero streams and is a no-op — every delivered record is idempotent
// through the ordinary Apply rule regardless (a node resuming after an
// ordinary restart, not a re-image, just re-confirms what it already has).
//
// Rejoin also ARMS the self-history retry (rejoinArmed): a per-peer latch
// (peerState.selfDrained) records, for each peer, whether one self-history
// drain has verifiably completed — the watermark reached everything that
// peer claims to hold for us. A peer whose boot-time drain fails stays
// un-latched, and an armed engine's pullPeer re-runs the drain every cycle
// until it succeeds, surfacing the interim as SelfHistoryPending on the
// status page. A second Rejoin call is idempotent with respect to the
// latch: it re-runs the self-history drains and can only SET latches,
// never clear them; clearing requires a process restart (the latch is
// deliberately in-memory — see peerState.selfDrained).
func (e *Engine) Rejoin(ctx context.Context) {
	e.mu.Lock()
	e.rejoinArmed = true
	e.mu.Unlock()
	// Own-history-first (§8.6): ask every peer what they hold of OUR OWN
	// site's log — the C3b self-history exception (§8.4) — and pull it all
	// BEFORE touching any peer's own log.
	for _, p := range e.cfg.Peers {
		ps := e.peers[p.SiteID]
		ps.runMu.Lock()
		drained, preWipe, err := e.drainSite(ctx, ps, e.cfg.SiteID)
		ps.runMu.Unlock()
		if err == nil && drained {
			// The latch (§2.3): one verified complete drain — the peer
			// answered, identified itself, and every announced stream
			// pulled to the head IT announced. Vacuous iff the verified
			// answer held zero PRE-WIPE streams for us (preWipe counted
			// inside drainSite, so both latch sites share the predicate).
			e.mu.Lock()
			ps.selfDrained = true
			ps.selfVacuous = preWipe == 0
			e.mu.Unlock()
		}
		if err != nil {
			// Review finding: this must not be silent. A boot-time
			// recovery failure needs a LOUD signal an operator can see
			// without polling an endpoint by hand — and this line is the
			// SOLE boot-time signal: drainSite's transport arm leaves
			// self-history failures to its caller (W3), so no competing
			// PEER UNREACHABLE line fires for them. Rejoin continues to
			// the next peer regardless (same per-peer isolation PullAll
			// gives ordinary catch-up) — self-history may be recoverable
			// from a DIFFERENT peer even if this one failed. The failed
			// drain stays visible as SelfHistoryPending and is retried by
			// every routine pull cycle (pullPeer's armed retry).
			log.Printf("repl: REJOIN: self-history recovery from peer %s FAILED: %v — this site's own prior-epoch history may be incomplete (§8.6)", p.SiteID, err)
			// Seed the retry's gated channel from this outcome, so the
			// FIRST steady-state retry failure does not immediately
			// duplicate what this line just said — the gate stays silent
			// until the heartbeat interval elapses or the state changes.
			e.mu.Lock()
			ps.selfLog.lastState = selfHistoryFailedState
			ps.selfLog.lastLogAt = time.Now()
			e.mu.Unlock()
		}
	}
	// Then every peer's own log, through the ordinary routine catch-up path
	// — the identical code Run()'s ticker calls every sync interval.
	e.PullAll(ctx)

	// A blank node whose recovery fails at boot must not start authoring
	// silently. Rejoin deliberately carries no error return (layered
	// status, not exceptions — matching PullAll/Run's existing shape), so
	// this loud console log, over the EXISTING Snapshot surface (no new
	// state), is currently the only visible signal before C10 exists.
	snap := e.Snapshot()
	for _, ps := range snap.Peers {
		if ps.LastError != "" {
			log.Printf("repl: REJOIN: peer %s unreachable/erroring after rejoin: %s", ps.SiteID, ps.LastError)
		}
	}
	for _, ss := range snap.Streams {
		if ss.State != StateOK && ss.State != StateUnreported { // unreported: the peer line above already names it
			log.Printf("repl: REJOIN: stream (%s, epoch %d) unhealthy after rejoin: state=%s detail=%s", ss.SiteID, ss.NodeEpoch, ss.State, ss.Detail)
		}
	}
}

// pullStream pages one (site, epoch) stream from the client's watermark to
// the author's head. `site` is normally ps.cfg.SiteID (the peer's own log);
// Rejoin's self-history step calls it with site == e.cfg.SiteID (our own
// identity) to recover our own prior-epoch history from ps — see
// drainSite. Returns true only if the stream drained (watermark reached the
// head reported alongside the final page).
//
// authorCurrent says this is the peer's OWN log at the highest epoch it
// announces — its current one. Only then is an announced head BELOW our
// watermark a lost-history alarm (StateRegressed, W7 / Y-25); for every
// other stream it is ordinary lag or recovery in progress and reads ok.
func (e *Engine) pullStream(ctx context.Context, ps *peerState, site string, epoch int64, authorCurrent bool) bool {
	key := StreamKey{SiteID: site, NodeEpoch: epoch}
	wm, err := e.st.Watermark(key.SiteID, epoch)
	if err != nil {
		e.setStream(key, 0, 0, StateError, err.Error())
		return false
	}
	for {
		pageStart := wm
		var pr PullResponse
		q := url.Values{
			"site":  {key.SiteID},
			"epoch": {strconv.FormatInt(epoch, 10)},
			"from":  {strconv.FormatInt(wm+1, 10)},
			"limit": {strconv.FormatInt(e.PageLimit, 10)},
		}
		if err := e.doJSON(ctx, ps, http.MethodGet, "/sync/pull", q, &pr); err != nil {
			e.setStream(key, wm, 0, StateError, err.Error())
			return false
		}
		if pr.SiteID != ps.cfg.SiteID || pr.NodeEpoch != epoch {
			e.setStream(key, wm, pr.Head, StateIdentityRejected,
				fmt.Sprintf("response claims (site %q, epoch %d), requested (%q, %d)", pr.SiteID, pr.NodeEpoch, key.SiteID, epoch))
			return false
		}
		// The seam-echo check (addendum 3, CRITICAL): before applying anything
		// from a page that would ADVANCE us (an event at wm+1 is present),
		// prove the author's history and ours agree at the seam — the row at
		// wm, which both sides hold. Every other defence waves the splice
		// through: the §8.4 triple passes (a rebuilt site is authentically
		// itself), Apply's contiguity check passes (new-history wm+1 IS
		// wm+1), and the §8.9 regression detector sees head ≥ peak. Gated on
		// an advancing page — not on the reported head, which a splicing
		// server could understate — so an honest peer that is merely BEHIND
		// on this stream (serves nothing new; C3b self-history pulls hit
		// this) is never challenged for a row it does not claim to have.
		// Known residual, deliberate: an equal-head rebuild that never
		// authors past wm serves no advancing page and stays invisible here —
		// it also never spreads; dump-compare/ops territory, and the
		// chain-hash in /sync/streams is the production successor.
		if advancing := pageAdvances(pr.Events, wm); advancing && wm >= 1 {
			if pr.PrevHash == "" {
				e.setStream(key, wm, pr.Head, StateError,
					fmt.Sprintf("author serves seq %d but did not attest seq %d — an author holds every row below its head, so an advancing page without a seam echo is a protocol violation", wm+1, wm))
				return false
			}
			mine, err := e.st.Events(key.SiteID, epoch, wm, 1)
			if err != nil || len(mine) != 1 || mine[0].Seq != wm {
				e.setStream(key, wm, pr.Head, StateError,
					fmt.Sprintf("cannot read own row at seq %d for the seam check: %v", wm, err))
				return false
			}
			if mine[0].Hash() != pr.PrevHash {
				e.setStream(key, wm, pr.Head, StatePrefixDiverged,
					fmt.Sprintf("author's envelope at seq %d is not the row this node holds — the stream shares a sequence number but not a prefix (silent splice refused; a same-epoch rebuild, §8.6: a re-image must announce node_epoch+1)", wm))
				return false
			}
		}
		// F28: the served page is applied as ONE batch — raw folds, the
		// per-drain scoped derived-state recompute, and every watermark
		// advance commit in a single transaction (store.ApplyBatch), instead
		// of one transaction per envelope. Identity is screened first and the
		// batch is the longest clean prefix; a rejection is reported only
		// after that prefix has landed — the same order the per-envelope
		// loop produced (it too applied everything before the bad record).
		batch := pr.Events
		rejected := false
		var rejMsg string
		for i, env := range pr.Events {
			// The §8.4 ingest identity rule: a record is accepted only if it
			// bears the authenticated delivering peer's identity — both the
			// stream identity (site_id) and the origin-identity claim
			// (origin_auth). This condition is UNCHANGED from C3a.
			identityOK := env.SiteID == ps.cfg.SiteID && env.OriginAuth == ps.cfg.SiteID
			if !identityOK && site == e.cfg.SiteID && env.SiteID == e.cfg.SiteID && env.OriginAuth == e.cfg.SiteID && env.NodeEpoch < e.cfg.NodeEpoch {
				// C3b §8.4 single sanctioned exception: we deliberately
				// asked THIS peer for OUR OWN site's history (site ==
				// e.cfg.SiteID identifies a self-history/rejoin pull —
				// config.Load rejects a peer whose site_id equals our own,
				// so no ORDINARY pull ever reaches this branch). Any
				// authenticated peer may deliver records bearing our own
				// site_id; nothing else is relaxed — a third site's
				// records, or a record whose origin_auth doesn't match
				// site_id, are still rejected below exactly as before.
				//
				// §8.4/§8.6 scope this to our OWN PRIOR-epoch stream only
				// (review finding, post-independent-verification): without
				// the env.NodeEpoch < e.cfg.NodeEpoch bound, a rigged or
				// compromised peer could forge records into our own
				// CURRENT (not-yet-authored) or a FUTURE epoch under cover
				// of a self-history pull — records the whole mesh would
				// later treat as genuine self-authorship, and that would
				// desync our own next Author() seq. See
				// TestSelfHistoryIngestRejectsRecordsAtOrAboveOurOwnCurrentEpoch
				// (confirmed to fail without this bound, pass with it).
				//
				// ⚠ ONLY THE EPOCH IS BOUNDED HERE — THE CONTENT IS NOT, and
				// cannot be (R-23, 2026-09-13). A peer serving forged
				// self-history can put a stranger's OPEN VISIT under our
				// tag through this arm (CLEAN-NEGATIVES-RECHECK.md, plant
				// C2b, measured), and a wiped post cannot tell genuine
				// recovered history from forged: the wipe destroyed the
				// evidence it would need. The data is deliberately NOT
				// refused — the peer's copy is our only backup, and a post
				// wiped mid-shift must get its whole shift back. Instead,
				// every visit this arm lands is MARKED on the muster and
				// the gate roster as RESTORED, never presented as observed
				// at this gate: domain.Visit.RestoredFromPeer reads "our
				// tag AND node_epoch below the running epoch" — exactly
				// the bound this arm enforces, so the marking is derived
				// from the same fact the admission is.
				identityOK = true
			}
			// ⟦FIX16 H⟧ a RETIRED post's stream relayed by a live peer: the
			// record must claim exactly the retired site it was asked for,
			// and that site must be in THIS post's [[former_peers]]. Live
			// origins are untouched — their records still come only from
			// their author.
			if !identityOK && e.isFormer(site) && env.SiteID == site && env.OriginAuth == site {
				identityOK = true
			}
			if !identityOK || env.NodeEpoch != epoch {
				rejected = true
				rejMsg = fmt.Sprintf("record %v (origin_auth %q) delivered by peer %q — rejected, not applied (§8.4)", env.Key(), env.OriginAuth, ps.cfg.SiteID)
				batch = pr.Events[:i]
				break
			}
		}
		results, aerr := e.st.ApplyBatch(batch)
		// Post-commit accounting and the C7 capture hook. OnApplied MUST fire
		// after the batch transaction has committed: the hook writes through
		// LocalTx, which would deadlock inside an open store transaction
		// (SetMaxOpenConns(1), local.go's contract). It still fires
		// synchronously on this pull goroutine, once per applied envelope, in
		// application order, before the next page is requested. The anchor is
		// unchanged by the deferral: slots rotate only at the END of a
		// drained pull cycle (pullPeer), so any read inside this loop —
		// per-envelope or per-page — returns the same pre-rotation value,
		// the one valid for these envelopes. The receipt instant the hook
		// takes shifts later by at most one page's apply time, which SHRINKS
		// remaining-at-receipt — the early-erring, fail-safe direction.
		var anchor time.Time
		var anchorOK bool
		if e.OnApplied != nil && len(results) > 0 {
			anchor, anchorOK = e.Anchor(ps.cfg.SiteID)
		}
		for i, applied := range results {
			if applied {
				wm = batch[i].Seq
				e.mu.Lock()
				e.applied++
				e.mu.Unlock()
				if e.OnApplied != nil {
					e.OnApplied(batch[i], anchor, anchorOK)
				}
			} else {
				e.mu.Lock()
				e.duplicates++
				e.mu.Unlock()
			}
		}
		if aerr != nil {
			var sv *store.SchemaVersionError
			var gap *store.GapError
			switch {
			case errors.As(aerr, &sv):
				// §8.5 visible refusal. The "queue" is the author's own log
				// plus this stalled watermark — durability comes from
				// re-pulling through the identical catch-up path after the
				// next re-image, so nothing is stored locally (row 4).
				e.setStream(key, wm, pr.Head, StateSchemaMismatch, sv.Error())
			case errors.As(aerr, &gap):
				// The author's log holds a contiguous prefix per stream,
				// always (row 5) — a gap on the honest pull path therefore
				// means corruption or a bug. Loud stall, never a skip.
				e.setStream(key, wm, pr.Head, StateGapStalled, gap.Error())
			default:
				e.setStream(key, wm, pr.Head, StateError, aerr.Error())
			}
			return false
		}
		if rejected {
			e.setStream(key, wm, pr.Head, StateIdentityRejected, rejMsg)
			return false
		}
		if wm >= pr.Head {
			if authorCurrent && pr.Head < wm {
				// ⟦W7 / Y-25, 2026-10-03⟧ The author announces LESS of its
				// own current stream than we already hold of it. Our
				// watermark for a peer's stream only ever advanced by
				// pulling from that peer itself (no transitive relay,
				// server.go), and an author's log is a contiguous prefix
				// that only grows within an epoch — so this is the author
				// having LOST its own history: re-imaged, or restored from
				// an older backup, WITHOUT the §8.6 epoch bump. A genuine
				// re-image announces a HIGHER epoch (a new key, head 0, no
				// comparison at all). The maxHead detector below cannot see
				// this case: it ignores head 0 (the error paths pass 0 for
				// "unknown"), and its memory is per-process, so after OUR
				// restart it has nothing to compare against — measured on
				// the L12 lane (Y-25): a post re-imaged without the bump
				// read "ok … peer reports no pre-wipe history" from every
				// other post while its own muster stood empty. Stateless
				// here, from the store's watermark, so it holds across our
				// restarts too. Nothing was applied (the page was empty —
				// from = wm+1 is past the author's head), nothing is rolled
				// back, and drainedAll goes false: the anchor never rotates
				// on an author we cannot trust. The row clears on its own
				// the moment the author's head is back at or above ours.
				e.setStream(key, wm, pr.Head, StateRegressed, fmt.Sprintf(
					"author %s announces head %d for its own CURRENT stream (node_epoch %d), but this post already holds %d of that very stream — an author's log only grows within an epoch, so the author has LOST its own history (re-imaged or restored from an older backup WITHOUT the §8.6 epoch bump; a genuine re-image announces a higher epoch instead, so this is never legitimate). Nothing was applied and this post's %d rows are untouched; recovery is an operator bumping node_epoch at %s and letting it rejoin — its missing history comes back from this post's copy (§8.6)",
					ps.cfg.SiteID, pr.Head, epoch, wm, wm, ps.cfg.SiteID))
				return false
			}
			e.setStream(key, wm, pr.Head, StateOK, "")
			return true
		}
		if wm == pageStart {
			// The server claims a head beyond our watermark but the page
			// advanced nothing — empty, or nothing but re-served duplicates.
			// Protocol anomaly either way: stall THIS stream loudly rather
			// than spin. The wedge matters doubly because pullPeer holds this
			// peer's runMu — a hot loop here would stall every later peer in
			// a sequential PullAll, breaking per-stream/per-peer isolation.
			e.setStream(key, wm, pr.Head, StateError,
				fmt.Sprintf("author reports head %d but the served page advanced nothing past %d", pr.Head, wm))
			return false
		}
	}
}

// pageAdvances reports whether a served page contains the event that would
// advance the given watermark — the only kind of page the seam check guards,
// because only an applied event can splice.
func pageAdvances(events []store.Envelope, wm int64) bool {
	for _, env := range events {
		if env.Seq == wm+1 {
			return true
		}
	}
	return false
}

// doJSON performs one authenticated exchange. Every request carries t1 (our
// local send stamp); every response carries the t1/t2/t3 echoes, which are
// decoded into out's embedded Echo and dropped with it — carried on every
// exchange, displayed nowhere, stored nowhere beyond the exchange (§8.2).
func (e *Engine) doJSON(ctx context.Context, ps *peerState, method, path string, q url.Values, out any) error {
	q.Set("t1", strconv.FormatInt(time.Now().UnixNano(), 10))
	req, err := http.NewRequestWithContext(ctx, method, ps.cfg.BaseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("repl: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+ps.cfg.Key)
	req.Header.Set(HeaderFrom, e.cfg.SiteID)
	resp, err := e.Client.Do(req)
	if err != nil {
		return fmt.Errorf("repl: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &HTTPStatusError{Method: method, Path: path, Code: resp.StatusCode, Status: resp.Status, Body: string(body)}
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("repl: %s %s: decode: %w", method, path, err)
	}
	return nil
}

// sustainedErrorLogInterval is the F14 heartbeat cap: while a bad state
// (peer-unreachable, a stream error, or an epoch hold) persists unchanged,
// at most one log line is written per interval, on top of the transition
// lines that always log immediately. Chosen against the deploy configs'
// sync_interval_seconds=10 (deploy/site-a.toml, deploy/site-b.toml): a
// per-cycle log would write 360 lines/hour for a single down peer -- the
// exact "peer down for an hour must not write a line every 10 seconds" case
// this exists to prevent. 5 minutes caps that to ~12 lines/hour: frequent
// enough that an operator tailing journalctl during a live incident sees
// confirmation the condition is STILL true within a few minutes, rare
// enough it never floods.
const sustainedErrorLogInterval = 5 * time.Minute

// peerUnreachableState and epochHoldState are logGate keys -- coarse, fixed
// labels, deliberately NOT the underlying error text (which can vary cycle
// to cycle for the same condition, e.g. "connection refused" vs a timeout)
// so the gate detects the real transition instead of flapping on
// message-text noise. The logged message text still varies per line; only
// the gate key is fixed.
const (
	peerUnreachableState = "unreachable"
	epochHoldState       = "held"

	// peerKeyRejectedState is errLog's key for a peer that ANSWERED — 401
	// (our key refused) or 403 (our identity refused) — a configuration
	// mismatch on a working link, deliberately a different gate state from
	// unreachable so the two facts transition-log against each other
	// (W7 / Y-31, 2026-10-03).
	peerKeyRejectedState = "key_rejected"

	// selfHistoryFailedState is peerState.selfLog's one gate key for the
	// armed retry's failures (noteSelfHistoryError). Deliberately COARSE —
	// one key for both the transport failure and the partial drain — so
	// alternation between the two does not defeat the gate the way the
	// pre-W3 flap defeated errLog; the logged message carries the varying
	// detail.
	selfHistoryFailedState = "self_history_failed"
)

// logGate is the F14 state-transition-or-heartbeat log discipline, shared by
// per-stream state (setStream), peer reachability (notePeerError / the
// recovery branch in drainSite), and the epoch-major hold (noteEpochHold).
// "" always means healthy/cleared.
type logGate struct {
	lastState string
	lastLogAt time.Time
}

// shouldLog reports whether `state` should be written now, and updates the
// gate. Logs immediately on any transition (including bad-to-different-bad --
// a materially different fact) or the first-ever call with a bad state;
// otherwise logs at most once per sustainedErrorLogInterval while the SAME
// state persists; never logs while state=="" persists (healthy needs no
// heartbeat).
func (g *logGate) shouldLog(state string, now time.Time) bool {
	if state == g.lastState {
		if state == "" || now.Sub(g.lastLogAt) < sustainedErrorLogInterval {
			return false
		}
		g.lastLogAt = now
		return true
	}
	g.lastState = state
	g.lastLogAt = now
	return true
}

func (e *Engine) setStream(key StreamKey, wm, head int64, state, detail string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	// C3b §8.9 regression detection: checked for every reported head, ahead
	// of whatever state the caller computed from this page — a regressed
	// head is an anomaly about the AUTHOR, orthogonal to what this
	// particular page contained, so it takes priority over a same-call
	// gap/identity/schema state. It never touches the watermark: wm is
	// passed through unchanged, so a regression surfaces loudly without
	// ever rolling anything back (nothing here can move wm backwards; only
	// the idempotent Apply loop advances it, and only forward).
	//
	// ⟦W7 / Y-15, 2026-10-03⟧ … and ONLY for a PEER's log (key.SiteID is not
	// our own site_id). The head on an OWN-stream row is never the author's
	// announcement — we ARE the author — it is a PEER's COPY-head of our
	// stream, read by the §8.6 self-history drain (Rejoin at boot, pullPeer's
	// armed retry) and by checkLeftBehind every cycle, one peer after
	// another. Copy-heads legitimately differ between peers and legitimately
	// lag our local head by up to a sync interval after every write, so
	// comparing one peer's copy-head against another's (or against one taken
	// before a later write) is not regression detection, it is lag detection
	// — and it fired "peer_log_regressed … never legitimate" on each post's
	// OWN stream after its own writes, 29 of 29 such lines over two L12
	// rounds, and froze the own row on /debug/sync-status (state regressed,
	// stale watermark) for a whole 34 s write burst (sc-test L12 Y-15). A
	// peer holding MORE of our stream than we do is the left-behind case
	// (StateLeftBehind, checkLeftBehind); a peer holding less is lag; neither
	// is a regression of OUR log, and the author of a log cannot regress
	// against itself. maxHead is therefore never consulted or recorded for
	// an own-stream key. A state the caller already computed as regressed
	// (pullStream's lost-history arm, Y-25) keeps its own, more specific
	// detail.
	// ⟦FIX16 H⟧ nor for a RETIRED post's stream: it is relayed by several
	// live peers, a peer still catching it up reports less than another,
	// and that is lag between two copies, not a regression of a log.
	if head > 0 && key.SiteID != e.cfg.SiteID && !e.isFormer(key.SiteID) {
		if prev := e.maxHead[key]; prev != 0 && head < prev {
			if state != StateRegressed {
				detail = fmt.Sprintf("peer log regressed (§8.9): reported head %d < previously seen %d within node_epoch %d — a genuine re-image announces a higher epoch instead, so this is never legitimate", head, prev, key.NodeEpoch)
			}
			state = StateRegressed
		} else if head > prev {
			e.maxHead[key] = head
		}
	}
	e.streams[key] = &StreamStatus{StreamKey: key, Watermark: wm, Head: head, State: state, Detail: detail}

	// F14: log this stream's state, gated by logGate -- a transition (any
	// change, including bad-to-different-bad, e.g. error -> prefix_diverged)
	// logs immediately; the SAME state persisting logs at most once per
	// sustainedErrorLogInterval; StateOK persisting never logs again. This is
	// the ONLY place per-stream sync state changes, so it is the single
	// choke point for this class of log line (unreachable-peer and
	// epoch-hold are gated separately, at their own choke points).
	g := e.streamLog[key]
	if g == nil {
		g = &logGate{}
		e.streamLog[key] = g
	}
	prevState := g.lastState
	// SYNC-HONESTY (2026-09-14): logGate's heartbeat exemption is keyed on
	// state == "" (see logGate.shouldLog) -- correct for notePeerError and
	// noteEpochHold, which both pass "" for their healthy/cleared state, but
	// WRONG here: a healthy stream reports the literal state string "ok",
	// never "". Without this guard, a stream that pulls successfully every
	// cycle calls shouldLog("ok", ...) every cycle too, so once
	// sustainedErrorLogInterval elapses the "same state persists" branch
	// fires and re-logs "SYNC RECOVERED ... (was ok)" -- a manufactured
	// recovery for a stream that never left ok. Measured live on both demo
	// posts, every ~5 minutes, since 09-13. A transition to ok FROM ok is
	// not a recovery -- only a REAL transition (from "", from an error
	// state, etc.) is. Skip the gate entirely in that case: no log, no gate
	// mutation, so a genuine later error->ok transition still logs RECOVERED
	// normally (see stream_divergence_recovered_heartbeat_test.go).
	if state == StateOK && prevState == StateOK {
		return
	}
	if g.shouldLog(state, time.Now()) {
		logStreamState(key, prevState, state, detail)
	}
}

// logStreamState writes one F14 log line for a stream state transition or
// heartbeat. It only formats -- see logGate/setStream for whether to log.
func logStreamState(key StreamKey, prevState, state, detail string) {
	switch {
	case state == StateOK && prevState == "":
		// FINDINGS-R5 log cosmetic: this printed "(was )" — there is no
		// earlier state since this post started, so say that instead.
		log.Printf("repl: SYNC RECOVERED: stream site=%s epoch=%d ok (first report since this post started) -- /debug/sync-status has the full history", key.SiteID, key.NodeEpoch)
	case state == StateOK:
		log.Printf("repl: SYNC RECOVERED: stream site=%s epoch=%d back to ok (was %s) -- /debug/sync-status has the full history", key.SiteID, key.NodeEpoch, prevState)
	case prevState == state:
		log.Printf("repl: SYNC ERROR (ongoing): stream site=%s epoch=%d still %s: %s -- see /debug/sync-status (the web status page) for full detail", key.SiteID, key.NodeEpoch, state, detail)
	default:
		log.Printf("repl: SYNC ERROR: stream site=%s epoch=%d entered state=%s: %s -- see /debug/sync-status (the web status page) for full detail", key.SiteID, key.NodeEpoch, state, detail)
	}
}

// notePeerError records a failed peer-log exchange as this peer's last_error
// and writes the gated F14 reachability line. ⟦W7 / Y-31, 2026-10-03⟧ A peer
// that ANSWERED 401 or 403 is not unreachable — the link is up and the peer's
// process is running; what is wrong is a key or a peer list on one side —
// so those two answers get their own gate state and their own line that
// names the actual fault. The L12 lane measured the old shape: a corrupted
// peer key was reported only as "PEER UNREACHABLE … check the link and the
// peer's own process", the one diagnosis that could not have been right.
func (e *Engine) notePeerError(ps *peerState, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	ps.lastError = err.Error()
	ps.errKind = errorKind(err)
	ps.reconnectPoke = true // the next answered exchange is a reconnect: poke then (P1a)
	gateState := peerUnreachableState
	var reason string
	if ps.errKind == ErrorKindKeyRejected || ps.errKind == ErrorKindIdentityRejected {
		gateState = peerKeyRejectedState
		reason = "the peer answered " + httpStatusOf(err) + ": " + refusalReason(e.cfg.SiteID, ps.cfg.SiteID, ps.errKind)
	}
	prevState := ps.errLog.lastState
	if !ps.errLog.shouldLog(gateState, time.Now()) {
		return
	}
	switch {
	case gateState == peerKeyRejectedState && prevState == peerKeyRejectedState:
		log.Printf("repl: PEER STILL REFUSING THIS POST (not an outage): peer %s (%s): %s; the link is UP, so this is a CONFIGURATION mismatch — no routine or poke sync with this peer can succeed until it is corrected; last error: %s -- see /debug/sync-status", ps.cfg.SiteID, ps.cfg.BaseURL, reason, ps.lastError)
	case gateState == peerKeyRejectedState:
		log.Printf("repl: PEER KEY REJECTED (not an outage): peer %s (%s): %s; the link is UP, so this is a CONFIGURATION mismatch, not a dead peer — no routine or poke sync with this peer can succeed until it is corrected; last error: %s -- see /debug/sync-status", ps.cfg.SiteID, ps.cfg.BaseURL, reason, ps.lastError)
	case prevState == peerUnreachableState:
		log.Printf("repl: PEER STILL UNREACHABLE: peer %s (%s): %s -- see /debug/sync-status (the web status page) for last-contact age and history", ps.cfg.SiteID, ps.cfg.BaseURL, ps.lastError)
	default:
		log.Printf("repl: PEER UNREACHABLE: peer %s (%s): %s -- check the link and the peer's own process; see /debug/sync-status (the web status page) for detail", ps.cfg.SiteID, ps.cfg.BaseURL, ps.lastError)
	}
}

// noteSelfHistoryError reports one failed §8.6 self-history RETRY (pullPeer's
// armed retry arm) on its own gated channel (W3, design/app-fixes-v1.md
// §2.3). It deliberately touches NEITHER ps.lastError NOR errLog and never
// calls notePeerError: the peer's reachability verdict must never be toggled
// by a self-history-only failure — the measured pre-W3 shape was a false
// "PEER UNREACHABLE" flap, 2 log events per cycle, every one naming the
// wrong fault for a peer whose peer-log drain succeeds in the same cycle.
// Status honesty is carried by SelfHistoryPending, not by log lines.
//
// err is nil on a partial drain (err == nil && !drained — the streams
// exchange succeeded but some announced stream did not pull to its head);
// the detail is composed from both values.
func (e *Engine) noteSelfHistoryError(ps *peerState, err error, drained bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	detail := "partial drain: some announced stream did not pull to the head the peer announced (see the stream rows)"
	if err != nil {
		detail = err.Error()
	} else if drained {
		return // defensive: a successful drain has nothing to report
	}
	if ps.selfLog.shouldLog(selfHistoryFailedState, time.Now()) {
		log.Printf("repl: SELF-HISTORY RECOVERY still failing from peer %s: %s — this post's own pre-wipe history is NOT yet recovered (§8.6); retrying every sync cycle; see /debug/sync-status", ps.cfg.SiteID, detail)
	}
}

// noteEpochHold is the F14 log for the epoch-major hold (campaign finding: a
// stalled early epoch silently holds back a site's LATER epochs --
// drainSite's loop -- which now stalls a whole site's ingest, so it must not
// be silent). Called once per drainSite call for the ongoing peer-log case
// only (see the call site). Same transition-or-heartbeat gate as everything
// else in this file.
func (e *Engine) noteEpochHold(ps *peerState, streams []StreamHead, stalledAt int, drainedAll bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	if drainedAll || stalledAt < 0 || stalledAt >= len(streams)-1 {
		// Nothing held back this cycle -- either a full drain, or the stall
		// was on the last (or only) announced epoch, so there was no later
		// epoch to hold. shouldLog("") is a no-op unless the PREVIOUS state
		// was "held", in which case it logs the clearing transition once.
		if ps.holdLog.shouldLog("", now) {
			log.Printf("repl: EPOCH HOLD CLEARED: peer %s later epoch(s) are draining again -- see /debug/sync-status", ps.cfg.SiteID)
		}
		return
	}
	held := make([]int64, 0, len(streams)-stalledAt-1)
	for _, sh := range streams[stalledAt+1:] {
		held = append(held, sh.NodeEpoch)
	}
	prevState := ps.holdLog.lastState
	if ps.holdLog.shouldLog(epochHoldState, now) {
		verb := "entered"
		if prevState == epochHoldState {
			verb = "still in"
		}
		log.Printf("repl: EPOCH HOLD (%s): peer %s epoch %d is stalled, holding back its own later epoch(s) %v (epoch-major delivery, section 8.3) -- this stalls the WHOLE SITE's ingest from this peer, not just the stalled epoch; see /debug/sync-status for the stalled epoch's own error detail", verb, ps.cfg.SiteID, streams[stalledAt].NodeEpoch, held)
	}
}

// Anchor reports the §9.3 anchor for an author site: the START of the most
// recent pull cycle that drained that author to its head (R10 — see
// peerState for why the start, and why the latest cycle is safe to expose).
// ok=false means the anchor does not exist yet (no full drain since process
// start) — consumers must err early (§9.3), never assume freshness.
func (e *Engine) Anchor(siteID string) (time.Time, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	ps, ok := e.peers[siteID]
	if !ok || ps.lastDrain.IsZero() {
		return time.Time{}, false
	}
	return ps.lastDrain, true
}

// Uptime is how long this engine (this process) has run: the scope of every
// in-memory contact age (Snapshot.UptimeSec).
func (e *Engine) Uptime() time.Duration { return time.Since(e.started) }

// Snapshot returns the whole diagnostic state, for /debug/sync-status and
// C10's rendering.
func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	now := time.Now()
	snap := Snapshot{SiteID: e.cfg.SiteID, Applied: e.applied, Duplicates: e.duplicates,
		Peers: make([]PeerSnapshot, 0, len(e.cfg.Peers)), Streams: make([]StreamStatus, 0, len(e.streams))}
	for _, p := range e.cfg.Peers { // config order: deterministic
		ps := e.peers[p.SiteID]
		psnap := PeerSnapshot{SiteID: p.SiteID, LastError: ps.lastError, Drains: ps.drains,
			// Pending only on an ARMED engine: an embedding that serves
			// without ever calling Rejoin emits false/false — a stated
			// precondition (production arms unconditionally pre-serve).
			SelfHistoryPending: e.rejoinArmed && !ps.selfDrained,
			SelfHistoryVacuous: ps.selfDrained && ps.selfVacuous,
		}
		if !ps.lastContact.IsZero() {
			v := now.Sub(ps.lastContact).Seconds()
			psnap.LastContactAgoSec = &v
		}
		if !ps.lastDrain.IsZero() {
			v := now.Sub(ps.lastDrain).Seconds()
			psnap.AnchorAgoSec = &v
		}
		psnap.ErrorKind = ps.errKind
		if psnap.LastError == "" {
			psnap.ErrorKind = ""
		}
		psnap.PokeRefused = ps.pokeRefused
		psnap.PokeRefusedKind = ps.pokeRefusedKind
		psnap.PokeError, psnap.PokeErrorKind = ps.pokeErr, ps.pokeErrKind
		psnap.InboundKeyRefused = !ps.inRefusedAt.IsZero() && ps.inRefusedAt.After(ps.inOKAt)
		psnap.OwnCopyHead = ps.ownCopyHead
		if !ps.ownBehindSince.IsZero() {
			v := now.Sub(ps.ownBehindSince).Seconds()
			psnap.OwnCopyBehindSec = &v
			limit := OwnCopyLagMultiple * e.cfg.SyncInterval()
			psnap.OwnCopyStuck = limit > 0 && now.Sub(ps.ownBehindSince) > limit
		}
		snap.Peers = append(snap.Peers, psnap)
	}
	snap.UptimeSec = now.Sub(e.started).Seconds()
	for _, ss := range e.streams {
		snap.Streams = append(snap.Streams, *ss)
	}
	e.mu.Unlock()

	// SYNC-HONESTY (2026-09-14, STREAM-DIVERGENCE.md): a post's OWN stream
	// row is written in exactly one place, setStream, reached only from the
	// §8.6 self-history drain (Rejoin at boot, or pullPeer's armed retry
	// while the latch is unset) — see pullStream/setStream. Nothing that
	// AUTHORS ever touches e.streams: every web handler writes through
	// store.Author, never setStream. So once every peer's self-history
	// latch closes, nothing ever asks a peer about our own stream again,
	// and this row is the BOOT-TIME snapshot FOREVER, even while this post
	// keeps authoring — the demo pair's 14-vs-18 reading, measured live:
	// both DBs held the identical log, only site-a's own row was wrong
	// about site-a.
	//
	// Fixed here, at READ time (not by ever re-asking a peer): refresh an
	// own-site row's Watermark/Head from OUR OWN local watermark, the one
	// source that cannot lag itself — a post's own authoring is always
	// synchronous with its own store write, so it can never be "behind"
	// its own log. Deliberately scoped to State==StateOK only: a row mid
	// self-history FAILURE (StateIdentityRejected, StateSchemaMismatch,
	// etc.) is a real, current fact being actively retried every cycle by
	// the armed retry (see pullPeer's needSelf arm) — that failure must
	// keep showing, never be papered over with a manufactured "ok". Own is
	// set on every row regardless, so a raw JSON reader can always tell
	// this row apart from a live peer-reported one (previously true only
	// on the HTML page's "(own — rejoin recovery)" label, never on the
	// wire).
	snap.Streams = append(snap.Streams, e.unreportedRows(snap.Streams)...)
	for i := range snap.Streams {
		ss := &snap.Streams[i]
		ss.Own = ss.SiteID == e.cfg.SiteID
		if ss.Own && ss.State == StateOK {
			if wm, err := e.st.Watermark(ss.SiteID, ss.NodeEpoch); err == nil {
				ss.Watermark, ss.Head = wm, wm
			}
			// err != nil: leave the cached values — a store read failure
			// here must not crash the status page or blank a row that was
			// last known good.
		}
	}
	sortStreams(snap.Streams)
	return snap
}

// unreportedRows fills the table's gaps (sc-test L12-Y05a = L16-P1b): this
// post's OWN stream and every configured peer get a row even when nothing has
// been reported since this process started. Before, the table held only
// streams some peer had reported since boot, so a post restarted while a peer
// was down had no row for that peer's stream (data held locally), and with
// every peer down it showed no rows at all, not even its own — "what does
// this post hold" went blank exactly when an operator needed it.
//
// The own row is StateOK and is then refreshed from the local watermark by
// Snapshot's own-row loop, like any own row: a post's own log is always
// current at the post itself. A peer's rows are StateUnreported, one per
// stream this post holds of that peer (watermark = what it holds; head
// unknown), or one epoch-0 row when it holds nothing. The store is read only
// when there is a gap to fill, which a converged mesh never has.
func (e *Engine) unreportedRows(have []StreamStatus) []StreamStatus {
	seen := map[string]bool{}
	for _, ss := range have {
		seen[ss.SiteID] = true
	}
	var missing []string
	if !seen[e.cfg.SiteID] {
		missing = append(missing, e.cfg.SiteID)
	}
	for _, p := range e.cfg.Peers {
		if !seen[p.SiteID] {
			missing = append(missing, p.SiteID)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	held := map[string][]store.Stream{}
	if ws, err := e.st.Watermarks(); err == nil {
		for _, w := range ws {
			held[w.SiteID] = append(held[w.SiteID], w)
		}
	}
	var out []StreamStatus
	for _, site := range missing {
		if site == e.cfg.SiteID {
			out = append(out, StreamStatus{StreamKey: StreamKey{SiteID: site, NodeEpoch: e.cfg.NodeEpoch}, State: StateOK})
			continue
		}
		const detail = "no report from this peer since this post started — the watermark is what this post already holds of its log; its current head is unknown until it answers"
		if len(held[site]) == 0 {
			out = append(out, StreamStatus{StreamKey: StreamKey{SiteID: site}, State: StateUnreported,
				Detail: "no report from this peer since this post started, and this post holds none of its records"})
			continue
		}
		for _, w := range held[site] {
			out = append(out, StreamStatus{StreamKey: StreamKey{SiteID: site, NodeEpoch: w.NodeEpoch},
				Watermark: w.AppliedSeq, State: StateUnreported, Detail: detail})
		}
	}
	return out
}

func sortStreams(ss []StreamStatus) {
	for i := 1; i < len(ss); i++ { // tiny n; insertion sort avoids an import
		for j := i; j > 0 && (ss[j].SiteID < ss[j-1].SiteID ||
			(ss[j].SiteID == ss[j-1].SiteID && ss[j].NodeEpoch < ss[j-1].NodeEpoch)); j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}

// StatusHandler serves the Snapshot as JSON — the status/diagnostic surface
// C10's sync-status UI reads. No rendering here (C10 owns that).
func (e *Engine) StatusHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(e.Snapshot())
	})
}

// isFormer reports whether site is a RETIRED post in this post's own
// config ([[former_peers]], FIX16 H).
func (e *Engine) isFormer(site string) bool {
	for _, f := range e.cfg.FormerPeers {
		if f.SiteID == site && site != "" {
			return true
		}
	}
	return false
}

// drainFormer pulls every retired post's stream peer ps serves (FIX16 H).
// A failure here never changes the peer's reachability verdict — the
// peer's own log was just drained — and is logged once per peer and site
// until it succeeds (a peer on an older build refuses with 403).
// Caller holds ps.runMu.
func (e *Engine) drainFormer(ctx context.Context, ps *peerState) {
	for _, f := range e.cfg.FormerPeers {
		_, _, err := e.drainSite(ctx, ps, f.SiteID)
		e.mu.Lock()
		if ps.formerWarned == nil {
			ps.formerWarned = map[string]bool{}
		}
		warned := ps.formerWarned[f.SiteID]
		ps.formerWarned[f.SiteID] = err != nil
		e.mu.Unlock()
		if err != nil && !warned {
			log.Printf("repl: RETIRED STREAM: peer %s did not serve retired post %s's records (%v) -- another peer may; see /debug/sync-status", ps.cfg.SiteID, f.SiteID, err)
		}
	}
}
