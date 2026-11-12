// readpool.go — FIX17c (2026-10-07): a read never waits on a write's disk
// flush.
//
// Until FIX17c every read and every write on a post shared the store's ONE
// connection, so a write stuck in its commit's fsync held every page behind
// it — /muster, the evacuation page, included. sc-stress measured routine
// 3-7 s stalls on the staging VM and, in one 80 s host disk freeze, every
// route stalled for up to 78 s (sc-stage/STRESS-REPORT.md, rounds 2-3).
//
// WRITES ARE UNCHANGED: Author, Transact, ApplyBatch, LocalTx and startup DDL
// still run on the one writer connection (SetMaxOpenConns(1), store.go), with
// synchronous at its FULL default, and every read made INSIDE a write — a
// Transact view, a fold's *sql.Tx, a LocalTx callback — stays on the writer's
// transaction.
//
// READS MOVE to a second *sql.DB on the same file, opened query_only, so it
// can never write. In WAL mode a reader works from a snapshot of the last
// COMMITTED transaction and takes no lock a writer waits for, nor waits for
// one. A writer stuck in fsync has not yet published its commit — SQLite syncs
// the WAL BEFORE it updates the wal-index header that makes the commit visible
// (wal.c, sqlite3WalFrames) — so a reader keeps serving the state before it,
// and nothing a reader sees (or replication serves from it) can be lost to a
// crash in that fsync.
//
// THE ORDERING ARGUMENT. A read that starts after a commit has returned sees
// that commit; a read that starts while a commit is in flight sees the state
// before it, exactly as if it had been scheduled just ahead of the write — an
// order the single connection could already produce. So no caller can
// observe an interleaving it could not observe before; it simply stops
// waiting. Read-your-writes follows: a POST commits and then redirects, and
// the GET it redirects to opens its snapshot after that commit returned.
//
// ONE SNAPSHOT PER PAGE. ReadView binds a read transaction to the calling
// goroutine — the web layer opens one per GET (web/readview.go) — so every
// read one render makes comes from one snapshot instead of from whatever was
// committed by the time each statement ran. It opens lazily at the first
// read. If the bound goroutine itself writes, the snapshot is dropped when the
// write releases the writer and the next read opens a fresh one, which sees
// that write. Outside a view each read statement takes its own snapshot from
// the pool, as each took the single connection before.
//
// THE POOL IS OFF — every read on the writer, exactly as before FIX17c — for
// an in-memory database (a second connection would open a different,
// empty database), and on a store whose writer connection holds TEMP schema:
// TEMP objects are per connection, and the scoped-read probes (W5d/W6/W8/W9a
// and FIX17's perf census) shadow tables with a TEMP VIEW on the writer, so
// their reads must stay where the shadow is. The product never creates TEMP
// objects.
//
// THE MEMO (memo.go) needs to know which committed state a snapshot shows.
// Two facts name it exactly:
//   - the commit window. seq is odd from just before a writer commit until
//     after memo.gen has advanced for it. A snapshot established between two
//     equal, even readings of seq shows the state memo.gen names, because no
//     commit can have become visible inside that interval;
//   - a pool connection's PRAGMA data_version, which moves whenever that
//     connection starts a read after ANY other connection — this post's writer
//     or another process — committed. A snapshot whose data_version equals
//     the one its connection recorded beside a known label shows the same
//     state, whatever seq is doing — which is what lets a page be served from
//     the memo while a commit is stuck in its fsync.
//
// Another process's commit (the memo's second-process case) moves only the
// writer connection's data_version, so a snapshot labelled through the
// commit window is also checked against that: if the writer is busy, or
// another process has committed, the snapshot is left unlabelled and the
// page builds without the memo — never from a value it cannot vouch for.
package store

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"time"
)

// readPoolIdle is how many read connections are kept open between requests.
// Kept connections matter beyond their open cost: a connection the memo
// has labelled before can still be served from the memo while a commit is
// stuck (file comment), a fresh one cannot — measured on the seeded season
// with four officers' pages polling through a 20 s stuck commit, three kept
// connections left one page in four rebuilding from scratch every load.
// The pool has NO hard cap on open connections, deliberately: a request
// holds its connection while it waits for a memo build another request is
// running, and a capped pool would let enough such waiters starve the
// builder of the connection it needs. Concurrency on a post is a handful of
// browsers; each connection costs one SQLite page cache.
const readPoolIdle = 4

// writerCheckWait bounds how long a label check waits for the writer
// connection to read its data_version. The check never queues behind a
// write: a writer that is busy is a reason to leave the snapshot unlabelled.
const writerCheckWait = 2 * time.Millisecond

type readPool struct {
	db *sql.DB

	// seq is the commit window (file comment): odd while a writer commit is
	// in flight, advanced by 2 when another process's commit is noticed.
	seq atomic.Uint64
	// ext counts the other processes' commits noticed so far.
	ext atomic.Uint64
	// shadow: the writer connection holds TEMP schema, so reads stay on it.
	shadow atomic.Bool
	// writers counts acquisitions of the writer connection in progress
	// (waiting or holding): while any is, the label check does not try it.
	writers atomic.Int32

	mu    sync.Mutex
	views map[uint64]*readView // goroutine id -> its bound view
	bound atomic.Int64         // len(views), read without mu on every read

	// known is, per pool connection, the data_version it showed beside the
	// last label it was given (see the file comment). Keyed by the driver
	// connection; pruned wholesale if it ever outgrows the connections a
	// post plausibly opens.
	knownMu sync.Mutex
	known   map[any]knownLabel

	// writerDV is the writer connection's data_version at the last check.
	writerMu sync.Mutex
	writerDV int64
}

type knownLabel struct {
	dataVersion int64
	gen         int64
	ext         uint64
}

// readView is one goroutine's snapshot. Only its own goroutine touches it.
type readView struct {
	conn  *sql.Conn
	tx    *sql.Tx
	id    any // the driver connection, the key into readPool.known
	stale bool
	used  bool // a read has been served from this snapshot
	retry bool // the label check noticed another process's commit

	// label (memo.go): gen is the state the snapshot shows when labelled.
	labelled bool
	checked  bool // the writer check for a commit-window label has run
	s0, s1   uint64
	e0       uint64
	gen      int64
	head     memoHead
	dv       int64
}

// openReadPool opens the query_only pool beside the writer, or returns nil
// for a database another connection cannot share (in-memory).
func openReadPool(path string, writer *sql.DB) (*readPool, error) {
	if path == ":memory:" || path == "" {
		return nil, nil
	}
	db, err := sql.Open("sqlite", readDSN(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxIdleConns(readPoolIdle)
	db.SetMaxOpenConns(0)
	p := &readPool{db: db, views: map[uint64]*readView{}, known: map[any]knownLabel{}}
	if err := writer.QueryRow(`PRAGMA data_version`).Scan(&p.writerDV); err != nil {
		db.Close()
		return nil, err
	}
	return p, nil
}

// usable reports whether reads go to the pool at all.
func (p *readPool) usable() bool { return p != nil && !p.shadow.Load() }

// ReadView binds one read snapshot to the calling goroutine until the
// returned end runs: every read this goroutine makes through the Store
// outside a write comes from that one snapshot. end is idempotent and must
// run on the same goroutine. Inside an existing view, inside a Transact
// view, or on a store without a pool, it binds nothing.
func (s *Store) ReadView() (end func()) {
	p := s.pool
	if s.tx != nil || p == nil {
		return func() {}
	}
	gid := goroutineID()
	p.mu.Lock()
	if _, ok := p.views[gid]; ok {
		p.mu.Unlock()
		return func() {} // the outer view owns it
	}
	v := &readView{}
	p.views[gid] = v
	p.bound.Add(1)
	p.mu.Unlock()
	done := false
	return func() {
		if done {
			return
		}
		done = true
		p.mu.Lock()
		delete(p.views, gid)
		p.bound.Add(-1)
		p.mu.Unlock()
		v.close()
	}
}

func (v *readView) close() {
	if v.tx != nil {
		_ = v.tx.Rollback()
		v.tx = nil
	}
	if v.conn != nil {
		_ = v.conn.Close()
		v.conn = nil
	}
}

// boundView is the calling goroutine's view, or nil.
func (p *readPool) boundView() *readView {
	if p.bound.Load() == 0 {
		return nil
	}
	gid := goroutineID()
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.views[gid]
}

// writeDone marks the calling goroutine's view stale after it used the
// writer: its next read opens a snapshot that shows the write.
func (p *readPool) writeDone() {
	if v := p.boundView(); v != nil {
		v.stale = true
	}
}

// reader is the read target outside a write: the bound view's snapshot, or
// the pool (one snapshot per statement). nil means read on the writer.
func (s *Store) reader() dbtx {
	p := s.pool
	if !p.usable() {
		return nil
	}
	if !s.latest {
		if v := p.boundView(); v != nil {
			if tx, err := s.viewTx(v); err == nil {
				return tx
			}
		}
	}
	return p.db
}

// readTx is a read-only transaction for one multi-statement read
// (Watermark, EventsAndHead, DumpProjections): the Transact view's own, the
// bound snapshot, a pool snapshot, or — pool off — the writer as before.
func (s *Store) readTx(op string) (q dbtx, done func(), err error) {
	if s.tx != nil {
		return s.tx, func() {}, nil
	}
	if p := s.pool; p.usable() {
		s.countRead()
		if !s.latest {
			if v := p.boundView(); v != nil {
				if tx, err := s.viewTx(v); err == nil {
					return tx, func() {}, nil
				}
			}
		}
		tx, err := p.db.Begin()
		if err != nil {
			return nil, nil, err
		}
		return tx, func() { _ = tx.Rollback() }, nil
	}
	tx, release, err := s.begin(op)
	if err != nil {
		return nil, nil, err
	}
	return tx, func() { _ = tx.Rollback(); release() }, nil
}

// LocalRead runs fn on a read-only transaction — the read half of LocalTx,
// for callers that only SELECT from site-local tables (or any table) and so
// need not queue for the writer. Inside a Transact view fn reads the view's
// transaction; in a bound view, its snapshot; otherwise a pool snapshot.
// With the pool off it is LocalTx, exactly as before. fn must not write: the
// pool's connections are query_only and refuse.
func (s *Store) LocalRead(fn func(tx *sql.Tx) error) error {
	if s.tx != nil {
		return fn(s.tx)
	}
	p := s.pool
	if !p.usable() {
		return s.LocalTx(fn)
	}
	s.countRead()
	if !s.latest {
		if v := p.boundView(); v != nil {
			if tx, err := s.viewTx(v); err == nil {
				return fn(tx)
			}
		}
	}
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return fn(tx)
}

// Latest is this Store reading the newest committed state statement by
// statement, ignoring the calling goroutine's bound view — for the one
// question a snapshot cannot answer: has the store moved past what this
// snapshot shows? Reads only.
func (s *Store) Latest() *Store {
	l := *s
	l.latest = true
	return &l
}

// viewTx is v's transaction for a read, opening (or, after the goroutine
// wrote, reopening) its snapshot.
func (s *Store) viewTx(v *readView) (*sql.Tx, error) {
	if v.tx == nil || v.stale {
		if err := s.openView(v); err != nil {
			return nil, err
		}
	}
	v.used = true
	return v.tx, nil
}

// openView opens a fresh snapshot for v, recording what its label needs.
func (s *Store) openView(v *readView) error {
	v.close()
	p := s.pool
	ctx := context.Background()
	conn, err := p.db.Conn(ctx)
	if err != nil {
		return err
	}
	var id any
	_ = conn.Raw(func(dc any) error { id = dc; return nil })
	*v = readView{conn: conn, id: id}
	v.s0 = p.seq.Load()
	v.e0 = p.ext.Load()
	if s.memo != nil {
		v.gen = int64(s.memo.gen.Load())
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		v.close()
		return err
	}
	v.tx = tx
	// The first statement establishes the snapshot (a deferred transaction
	// takes its read mark at its first read).
	if err := tx.QueryRow(`PRAGMA data_version`).Scan(&v.dv); err != nil {
		v.close()
		return err
	}
	if err := tx.QueryRow(`PRAGMA schema_version`).Scan(&v.head.schema); err != nil {
		v.close()
		return err
	}
	v.s1 = p.seq.Load()
	// Same connection, same data_version as beside a known label: nothing
	// has been committed since, by anyone — the snapshot shows that state.
	p.knownMu.Lock()
	k, ok := p.known[id]
	p.knownMu.Unlock()
	if ok && k.dataVersion == v.dv && k.ext == p.ext.Load() {
		v.gen, v.labelled, v.checked = k.gen, true, true
	}
	return nil
}

// viewLabel is the memo key for v's snapshot, if it has one (file comment).
func (s *Store) viewLabel(v *readView) (gen int64, head memoHead, ok bool) {
	if !v.checked {
		v.checked = true
		p := s.pool
		clean, noticed := s.noOtherWriter()
		v.retry = noticed
		v.labelled = v.s0 == v.s1 && v.s0%2 == 0 && clean && p.ext.Load() == v.e0
		if v.labelled {
			p.knownMu.Lock()
			if len(p.known) > 64 {
				clear(p.known)
			}
			p.known[v.id] = knownLabel{dataVersion: v.dv, gen: v.gen, ext: v.e0}
			p.knownMu.Unlock()
		}
	}
	return v.gen, v.head, v.labelled
}

// noOtherWriter reports that no other process has committed since the last
// check, reading the writer connection's data_version — which only another
// connection's commit moves (this post's readers never commit). A noticed
// commit advances the generation and the commit window, so every snapshot
// labelled before it is refused (noticed); a busy writer is reported as
// "cannot say".
func (s *Store) noOtherWriter() (clean, noticed bool) {
	p := s.pool
	if p.writers.Load() > 0 {
		return false, false
	}
	p.writerMu.Lock()
	defer p.writerMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), writerCheckWait)
	conn, err := s.db.Conn(ctx)
	cancel()
	if err != nil {
		return false, false
	}
	defer conn.Close()
	var dv int64
	if err := conn.QueryRowContext(context.Background(), `PRAGMA data_version`).Scan(&dv); err != nil {
		return false, false
	}
	if dv == p.writerDV {
		return true, false
	}
	p.writerDV = dv
	s.wrote()
	p.ext.Add(1)
	p.seq.Add(2)
	return false, true
}

// commit commits a writer transaction inside the commit window (file
// comment), advancing the memo generation for it when changed. BeforeCommit,
// the stall tests' seam, runs inside the window with the transaction's
// writes complete and not yet visible — where a real fsync stall sits.
func (s *Store) commit(tx *sql.Tx, changed bool) error {
	p := s.pool
	if p != nil {
		p.seq.Add(1)
		defer p.seq.Add(1)
	}
	if s.BeforeCommit != nil {
		s.BeforeCommit()
	}
	if debugCommitStall != nil {
		debugCommitStall()
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if changed {
		s.wrote()
	}
	return nil
}

// execWriter runs one autocommit statement on the writer (startup DDL)
// inside the commit window, advancing the memo generation.
func (s *Store) execWriter(q string) error {
	p := s.pool
	if p != nil {
		p.seq.Add(1)
		defer p.seq.Add(1)
	}
	if _, err := s.db.Exec(q); err != nil {
		return err
	}
	s.wrote()
	s.noteTempSchema(s.db)
	return nil
}

// noteTempSchema turns the pool off for good once the writer connection
// holds TEMP schema (file comment). q must be on the writer.
func (s *Store) noteTempSchema(q dbtx) {
	p := s.pool
	if p == nil || p.shadow.Load() {
		return
	}
	var v int64
	if err := q.QueryRow(`PRAGMA temp.schema_version`).Scan(&v); err == nil && v != 0 {
		p.shadow.Store(true)
	}
}

// debugCommitStall is BeforeCommit for a whole binary: set only in a build
// tagged sc_stallhook (stallhook_debug.go), for measuring a stuck commit
// on a running post. Nil in every product build.
var debugCommitStall func()

func (p *readPool) close() error {
	if p == nil {
		return nil
	}
	return p.db.Close()
}
