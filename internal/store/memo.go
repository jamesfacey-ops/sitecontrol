// memo.go — FIX17 (perf, 2026-10-06): reuse of a derived read between writes.
//
// At season scale (~120 days, ~14k visits, a trip row per gate check-in) the
// officer pages re-derived the same whole-table and whole-log reads on every
// request: the shift panel on /gate and /status replayed every event in the
// log, /trips and the muster decoded every trip row several times. None of
// those values can change between two writes, so Memo keeps the last one
// built and hands it back until the store is written again.
//
// THE KEY IS THE STORE'S HEAD, NOT A CLOCK. Two numbers name "the data as it
// stands":
//   - gen, this process's write generation, advanced after EVERY commit that
//     can have changed a row through this Store: an Author or a Transact
//     commit, an ApplyBatch commit that applied anything, a LocalTx commit
//     that changed any row (total_changes), and startup DDL. An append is a
//     write, so every append invalidates;
//   - PRAGMA data_version on the store's connection, which SQLite advances
//     whenever ANOTHER connection commits — a second process beside a running
//     service (the admin tool, a test's side connection) can never be served
//     a value built before its write;
//   - PRAGMA schema_version of main and of temp, which move on any DDL — a
//     TEMP VIEW shadowing a table (the scoped-read probes' instrument) reads
//     differently from the table it shadows.
//
// A value is reused only when all of them match the ones it was built under,
// and a build that raced a write is not kept (gen is re-read after the
// build).
// Nothing here reads a clock or expires: a memoised read must be a pure
// function of the stored rows, and every caller's own time arithmetic stays
// outside it.
//
// WHAT IS HELD, AND FOR HOW LONG ⟦FIX17b, memory⟧: only values built at the
// CURRENT head. A write drops every entry (wrote) — none could be served
// again — and at most memoMaxEntries are kept, the least recently used
// going first. ⟦was: an entry stayed until its own key was asked for again,
// so a report built once at the start of a shift held its season-sized
// tables all day; sc-stress measured 221-278 MB per post at season scale⟧
//
// Never inside a Transact view: a view reads its own uncommitted writes, so
// nothing it builds may outlive it, and nothing built outside it describes
// what it sees. A Store built without Open (tests) has no memo and simply
// builds every time.
package store

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

type memoTable struct {
	gen      atomic.Uint64
	mu       sync.Mutex
	entries  map[string]memoEntry
	inflight map[string]*memoCall
	retained map[string]any
	tick     uint64 // under mu
}

// memoCall is one build in progress. Callers asking for the same key at the
// same head while it runs wait for it rather than building again: after a
// write, five officers' muster renders would otherwise each rebuild the same
// season-sized reads at once on the store's one connection (sc-stress F8).
type memoCall struct {
	gen  int64
	head memoHead
	done chan struct{}
	val  any
	err  error
}

type memoEntry struct {
	gen  int64
	head memoHead
	val  any
	used uint64 // memoTable.tick at the last hit or store, for eviction
}

// memoMaxEntries bounds the table. The product asks for a couple of dozen
// distinct keys (sections, reports per site-type map, the replays); the cap
// is the backstop for a key spelled from data.
const memoMaxEntries = 48

// memoHead is the connection-side half of the key (file comment).
type memoHead struct{ dataVersion, schema, tempSchema int64 }

func (s *Store) memoHead() (memoHead, error) {
	var h memoHead
	for _, q := range []struct {
		pragma string
		into   *int64
	}{{`PRAGMA data_version`, &h.dataVersion}, {`PRAGMA schema_version`, &h.schema}, {`PRAGMA temp.schema_version`, &h.tempSchema}} {
		if err := s.db.QueryRow(q.pragma).Scan(q.into); err != nil {
			return memoHead{}, err
		}
	}
	return h, nil
}

// memoLabel names the state the caller's reads will see: with the read
// pool, its bound snapshot's label — opening a view for the build when the
// goroutine has none, so the build reads ONE snapshot and the label is
// exact (readpool.go); with the pool off, the generation and the writer
// connection's head, as before. ok false: the snapshot cannot be named.
func (s *Store) memoLabel() (gen int64, head memoHead, ok bool, end func(), err error) {
	p := s.pool
	if !p.usable() {
		gen = int64(s.memo.gen.Load())
		head, err = s.memoHead()
		return gen, head, err == nil, func() {}, err
	}
	end = s.ReadView()
	v := p.boundView()
	if v == nil {
		return 0, memoHead{}, false, end, nil
	}
	if v.tx == nil || v.stale {
		if err := s.openView(v); err != nil {
			return 0, memoHead{}, false, end, err
		}
	}
	gen, head, ok = s.viewLabel(v)
	if !ok && v.retry && !v.used {
		// Another process's commit was noticed just now, and nothing has
		// been read from this snapshot yet: take a fresh one and name it.
		if err := s.openView(v); err != nil {
			return 0, memoHead{}, false, end, err
		}
		gen, head, ok = s.viewLabel(v)
	}
	return gen, head, ok, end, nil
}

func newMemoTable() *memoTable { return &memoTable{entries: map[string]memoEntry{}} }

// wrote advances the write generation. Called after a commit returns, so a
// reader can never pair the new generation with rows from before the write.
func (s *Store) wrote() {
	if s.memo != nil {
		s.memo.gen.Add(1)
		// Nothing built before this write can be served again: let it go
		// now rather than when its key is next asked for.
		s.memo.mu.Lock()
		clear(s.memo.entries)
		s.memo.mu.Unlock()
	}
}

// memoEntries is how many values the table holds (tests).
func (s *Store) memoEntries() int {
	if s.memo == nil {
		return 0
	}
	s.memo.mu.Lock()
	defer s.memo.mu.Unlock()
	return len(s.memo.entries)
}

// Memo returns the value build produced for key at the store's current
// head, building it if there is none. The value is SHARED between callers:
// a caller must treat it as read-only and copy anything it hands on (the
// domain wrappers return copies of slices). build's error is returned and
// nothing is kept.
func (s *Store) Memo(key string, build func() (any, error)) (any, error) {
	if s.tx != nil || s.memo == nil || s.latest {
		return build()
	}
	gen, head, ok, end, err := s.memoLabel()
	defer end()
	if err != nil {
		return nil, fmt.Errorf("store: memo %s: %w", key, err)
	}
	if !ok {
		// ⟦FIX17c⟧ a snapshot that cannot be named (readpool.go) builds
		// for itself and keeps nothing.
		return build()
	}
	s.memo.mu.Lock()
	if e, ok := s.memo.entries[key]; ok {
		if e.gen == gen && e.head == head {
			s.memo.tick++
			e.used = s.memo.tick
			s.memo.entries[key] = e
			s.memo.mu.Unlock()
			return e.val, nil
		}
		delete(s.memo.entries, key) // built before another connection's write
	}
	if c, ok := s.memo.inflight[key]; ok && c.gen == gen && c.head == head {
		s.memo.mu.Unlock()
		<-c.done
		return c.val, c.err
	}
	c := &memoCall{gen: gen, head: head, done: make(chan struct{})}
	if s.memo.inflight == nil {
		s.memo.inflight = map[string]*memoCall{}
	}
	s.memo.inflight[key] = c
	s.memo.mu.Unlock()

	defer func() {
		s.memo.mu.Lock()
		if s.memo.inflight[key] == c {
			delete(s.memo.inflight, key)
		}
		if c.err == nil && int64(s.memo.gen.Load()) == gen {
			s.memo.store(key, memoEntry{gen: gen, head: head, val: c.val})
		}
		s.memo.mu.Unlock()
		close(c.done)
	}()
	c.err = errMemoPanic // a panicking build leaves its waiters an error, not a hang
	c.val, c.err = build()
	if c.err != nil {
		return nil, c.err
	}
	return c.val, nil
}

// store keeps e under key, first evicting the least recently used entry
// when the table is full. Caller holds mu.
func (m *memoTable) store(key string, e memoEntry) {
	if _, ok := m.entries[key]; !ok && len(m.entries) >= memoMaxEntries {
		oldest, first := "", true
		for k, v := range m.entries {
			if first || v.used < m.entries[oldest].used {
				oldest, first = k, false
			}
		}
		delete(m.entries, oldest)
	}
	m.tick++
	e.used = m.tick
	m.entries[key] = e
}

var errMemoPanic = errors.New("store: memo: the build panicked")

// Reads is how many reads have taken the store's one connection since Open:
// every projection read, log walk, watermark census and transaction begun
// (FIX17). Everything on a post queues for that one connection, so this
// count — not wall time, which the host's load decides — is what a page's
// cost regression shows up in (web/fix17_perf_test.go). 0 on a Store built
// without Open.
func (s *Store) Reads() int64 {
	if s.reads == nil {
		return 0
	}
	return s.reads.Load()
}

func (s *Store) countRead() {
	if s.reads != nil {
		s.reads.Add(1)
	}
}

// Retained returns the one value init made for key on this Store, making it
// on first use — for state a reader keeps current ITSELF across writes (an
// incremental replay of the append-only log: domain/log_follow.go), as
// opposed to Memo's values, which a write discards. The value must do its
// own locking. A Transact view, and a Store built without Open, get a fresh
// init() every call: nothing they read may outlive them.
func (s *Store) Retained(key string, init func() any) any {
	if s.tx != nil || s.memo == nil {
		return init()
	}
	s.memo.mu.Lock()
	defer s.memo.mu.Unlock()
	if s.memo.retained == nil {
		s.memo.retained = map[string]any{}
	}
	v, ok := s.memo.retained[key]
	if !ok {
		v = init()
		s.memo.retained[key] = v
	}
	return v
}
