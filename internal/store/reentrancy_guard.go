// reentrancy_guard.go — defence in depth against fold re-entrancy (added
// during the adversarial test campaign's concurrency angle, 2026-08-20). The
// hazard: SetMaxOpenConns(1) (store.go) means at most one goroutine is ever
// inside a transaction — every other caller of Begin blocks until that
// transaction commits or rolls back. If a fold — running synchronously
// inside insertAndFold, on the SAME goroutine that holds the outer apply
// transaction — ever called back into Author or LocalTx, the inner Begin
// would block waiting for the very connection the outer transaction is
// holding, on that same goroutine, and neither side could ever proceed.
// That is a permanent hang, not a slow operation.
//
// This is NOT a response to an observed defect: the campaign's concurrency
// specialist read Projection.Apply's signature — func(tx *sql.Tx, env
// Envelope) error — and confirmed a fold has no parameter through which to
// reach *Store, then read all 9 Author call sites and both LocalTx call
// sites individually and confirmed none sits inside a fold's call graph. The
// hazard is closed today. The guard below is insurance against a future
// refactor quietly reopening it — "a loud panic beats a silent hang", and a
// hang here looks exactly like a network problem to an operator at a gate
// hut, which is the worst place for a silent failure mode.
//
// ⟦PANIC-1, 2026-09-30⟧ THE GUARD USED TO CONFLATE CONTENTION WITH
// REENTRANCY. Its only evidence was elapsed time: any wait past the timeout
// panicked, and the panic was fatal on every goroutine net/http does not
// recover — the §9.4 deadline scanner above all. MEASURED: round 1 (L07) and
// twice in round 2 (L11, ice-pad and ndb in the same second, ~45 s after a
// lane reset and a dispatch, five posts on one box) the scanner's LocalTx
// waited 10 s for a connection ANOTHER goroutine held and the post died with
// "could not acquire the store's single connection", systemd restarting it
// two seconds later — a post dark at the gate for a slow peer's sake. The
// wait was contention; the message asserted reentrancy.
//
// The guard now knows WHO holds the connection. Every tracked acquisition
// (Author, LocalTx, Transact, ApplyBatch, Watermark, DumpProjections — and
// EachEvent, the one read that keeps the connection across a caller's
// callback) records its goroutine and op on the store's connHolder for as
// long as it holds, and the timeout branch compares:
//   - the holder IS this goroutine → genuine reentrancy, a programming
//     error. Still a panic, still naming the rule, because the alternative
//     is the silent hang above. net/http recovers it per request; the
//     deadline scanner recovers it per tick (scan.go) so it is loud in the
//     log and never fatal to the post.
//   - the holder is ANOTHER goroutine, or nobody tracked (a read cursor
//     left open somewhere) → contention. Keep waiting — exactly what a plain
//     db.Begin() did before this guard existed — and say so in the log every
//     timeout period, naming the holder and how long it has held, so a long
//     hold is diagnosable from the post's own log instead of from a crash.
package store

import (
	"bytes"
	"database/sql"
	"fmt"
	"log"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// reentrancyGuardTimeout bounds how long Author and LocalTx will wait for
// the store's one connection before judging the wait — reentrancy or
// contention — rather than merely counting it. Ordinary concurrent
// contention always resolves: campaign finding F22 measured worst-case form
// latency at 539-576ms under a 15,000-event replication backlog (against a
// 3.7ms baseline), and F29 measured sustained concurrent writes at ~100ms
// average / 277ms max under a competing local-write probe. A genuine
// reentrant deadlock never resolves on its own. Any timeout comfortably
// above that measured range — this one has roughly two orders of magnitude
// of headroom — will not fire under real contention but will always
// eventually fire under real reentrancy. Var, not const, so a test can
// shrink it rather than wait out the real timeout.
var reentrancyGuardTimeout = 10 * time.Second

// connHolder is the store-wide record of who currently holds the single
// connection through a tracked acquisition — shared by pointer between a
// Store and its Transact views, so a view's nested acquisition and the pool
// store's judge the same fact. gid 0 means "nobody tracked": either the
// connection is free, or an untracked reader (a *sql.Rows still open, a
// QueryRow never scanned) has it.
type connHolder struct {
	mu    sync.Mutex
	gid   uint64
	op    string
	since time.Time
}

func (h *connHolder) set(gid uint64, op string) {
	h.mu.Lock()
	h.gid, h.op, h.since = gid, op, time.Now()
	h.mu.Unlock()
}

func (h *connHolder) clear() {
	h.mu.Lock()
	h.gid, h.op, h.since = 0, "", time.Time{}
	h.mu.Unlock()
}

func (h *connHolder) snapshot() (gid uint64, op string, since time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.gid, h.op, h.since
}

// goroutineID reads this goroutine's id off the first line of its own stack
// header ("goroutine 123 [running]:"). The runtime deliberately offers no
// API for it; this is the one sanctioned-by-convention way, used only on
// the acquisition and timeout paths (a stack header, not a stack walk —
// microseconds, against a Begin that already costs milliseconds). 0 on the
// unparsable case, which the timeout branch reads as "unknown, so treat as
// contention" — the never-fatal direction.
func goroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	line := bytes.TrimPrefix(buf[:n], []byte("goroutine "))
	if i := bytes.IndexByte(line, ' '); i > 0 {
		line = line[:i]
	}
	id, err := strconv.ParseUint(string(line), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// holdConn records this goroutine as the connection's holder for op and
// returns the release. Callers register the release BEFORE any deferred
// Rollback, so it runs after the transaction has actually ended (defers run
// last-in first-out).
func (s *Store) holdConn(op string) (release func()) {
	if s.holder == nil {
		return func() {}
	}
	s.holder.set(goroutineID(), op)
	return s.holder.clear
}

// begin acquires the store's single connection as a transaction for op:
// beginGuarded's bounded wait plus the holder record. The returned release
// must run after the transaction ends — `defer release()` first, THEN
// `defer tx.Rollback()`.
func (s *Store) begin(op string) (tx *sql.Tx, release func(), err error) {
	s.countRead()
	// ⟦FIX17c⟧ the read pool's label check leaves the writer alone while
	// anyone waits for or holds it, and a goroutine with a bound read view
	// reads a fresh snapshot after its write (readpool.go).
	p := s.pool
	if p != nil {
		p.writers.Add(1)
	}
	tx, err = beginGuarded(s, op)
	if err != nil {
		if p != nil {
			p.writers.Add(-1)
		}
		return nil, nil, err
	}
	held := s.holdConn(op)
	if p == nil {
		return tx, held, nil
	}
	return tx, func() {
		held()
		p.writers.Add(-1)
		p.writeDone()
	}, nil
}

// beginGuarded starts a transaction like Begin, but bounds the UNJUDGED wait
// for the store's single connection: every reentrancyGuardTimeout the wait
// is judged against the holder record (file header) — reentrancy panics,
// contention keeps waiting and logs.
//
// It deliberately does NOT use context.WithTimeout + BeginTx: database/sql
// ties a BeginTx transaction's whole subsequent lifetime to the ctx it was
// given (an internal goroutine rolls it back the instant ctx is done), so
// the same deadline that should only bound the WAIT for a connection would
// also silently cap how long the caller may then hold the transaction for —
// and there is no clean way to "detach" a *sql.Tx from its ctx once
// acquired. It also does not defer the WithTimeout cancel func: calling it
// right after a successful BeginTx fires ctx.Done() immediately and rolls
// back the transaction being handed back to the caller before the caller
// can use it — the exact bug this guard exists to prevent, self-inflicted.
//
// Instead: db.Begin() runs UNBOUNDED, exactly as before this guard existed,
// on a background goroutine; the caller waits for it in reentrancyGuardTimeout
// slices. An atomic CAS is the single arbiter of who "owns" the outcome —
// whichever side loses the race cleans up rather than leaking:
//   - fast path (the overwhelming majority of calls): Begin returns well
//     under the timeout, wins the CAS, sends its result, the caller
//     receives it immediately. The returned *sql.Tx carries no deadline of
//     its own, so it behaves exactly like a plain Begin() result.
//   - reentrancy path: Begin never returns (the connection it wants is held
//     by this same goroutine's own outer, still-open transaction). The
//     timeout fires, the holder record names THIS goroutine, the caller
//     wins the CAS and panics. Author/LocalTx's OWN deferred Rollback then
//     runs during the panic's unwind, freeing the connection — which lets
//     the abandoned background goroutine's Begin() finally return. It loses
//     the CAS (already claimed), so it rolls back its own just-acquired
//     transaction and exits, instead of leaking it forever on the store's
//     one connection. An earlier draft skipped this arbitration and used a
//     plain select between "send" and "give up", which raced: once given
//     up, a still-open channel-with-room send could still win the select,
//     silently discarding the late transaction without closing it. Caught
//     by testing it (a Windows temp-file-in-use failure on the very next
//     Open in the same test process), not by inspection.
//   - contention path: the timeout fires and the holder record names
//     another goroutine (or nobody tracked). Nothing is claimed; the wait
//     simply continues, one logged slice at a time, until Begin returns.
func beginGuarded(s *Store, op string) (*sql.Tx, error) {
	type result struct {
		tx  *sql.Tx
		err error
	}
	ch := make(chan result, 1)
	var claimed atomic.Bool
	go func() {
		tx, err := s.db.Begin()
		if claimed.CompareAndSwap(false, true) {
			ch <- result{tx, err}
			return
		}
		// The caller already gave up (timed out and panicked). Don't leak
		// the store's one connection forever on an abandoned transaction.
		if err == nil {
			_ = tx.Rollback()
		}
	}()
	self := goroutineID()
	started := time.Now()
	timer := time.NewTimer(reentrancyGuardTimeout)
	defer timer.Stop()
	for {
		select {
		case r := <-ch:
			return r.tx, r.err
		case <-timer.C:
		}
		waited := time.Since(started).Round(time.Millisecond)
		holderGID, holderOp, holderSince := uint64(0), "", time.Time{}
		if s.holder != nil {
			holderGID, holderOp, holderSince = s.holder.snapshot()
		}
		if holderGID != 0 && holderGID == self {
			if !claimed.CompareAndSwap(false, true) {
				// Begin() won the CAS in the instant before this timeout
				// fired (it is already committed to sending). Take that real
				// result rather than panicking on a connection that did
				// arrive — the send cannot block (buffered, capacity 1) so
				// this is a brief, bounded wait, not a second race.
				r := <-ch
				return r.tx, r.err
			}
			panic(fmt.Sprintf(
				"store: %s could not acquire the store's single connection within %s — "+
					"this goroutine ALREADY HOLDS IT (as %s, since %s ago): this store allows only one open "+
					"connection (store.go, SetMaxOpenConns(1)), so this is REENTRANCY, i.e. a fold called back "+
					"into Author or LocalTx on the store it is folding for — or a Transact callback reached for "+
					"the UNBOUND store instead of the view it was handed (transact.go). See "+
					"internal/store/reentrancy_guard.go.",
				op, waited, holderOp, time.Since(holderSince).Round(time.Millisecond)))
		}
		// Contention — another goroutine's transaction, or an untracked
		// reader, has the connection. Not a programming error on THIS path
		// and never fatal: keep waiting, as a plain Begin would, and leave
		// the diagnosis in the log where the crash used to be.
		holder := "an untracked reader (a *sql.Rows still open, or a QueryRow never scanned)"
		if holderGID != 0 {
			holder = fmt.Sprintf("%s on goroutine %d, held for %s", holderOp, holderGID, time.Since(holderSince).Round(time.Millisecond))
		}
		log.Printf("store: CONTENTION: %s has waited %s for the store's single connection — held by %s; not reentrancy (this is goroutine %d); still waiting",
			op, waited, holder, self)
		timer.Reset(reentrancyGuardTimeout)
	}
}
