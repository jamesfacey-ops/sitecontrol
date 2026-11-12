package web

// fix17_perf_test.go — FIX17 (perf, 2026-10-06): the officer pages' read
// cost at scale, asserted by COUNTING, never by timing.
//
// At season scale (~120 days, ~14.9k visits, a trip per gate check-in) GET
// /trips took 16.8 s at Ice Road, /muster 8.7 s and /gate 1.6 s. Three root
// causes, each with a guard here:
//
//  1. the shift panel on /gate and /status replayed the WHOLE event log on
//     every render, twice (the visit report, then the analytics walk). Now
//     the replay is resumed (domain/log_follow.go) and its result reused
//     between writes (store.Memo): a render with nothing new walks no event,
//     and a render after one write walks only that write's events;
//  2. /trips composed every trip of the season (four reads each) to keep the
//     few open pad runs, and every page decoded the season's trip table
//     several times. A render with nothing new now emits a handful of trip
//     rows, not ~3x the table;
//  3. the muster's not-accounted-for section read the visits of each trip,
//     of each arrival and of each person with no index to serve them — a
//     scan of the season's visit table per trip. The three reads must plan
//     as index searches.
//
// The instrument is the scoped-read probes' (w9a_consumers_test.go): a TEMP
// VIEW shadowing a table, whose select list runs one column through a
// counting function, so production's own SQL runs unchanged and the counter
// reports the rows it emitted. The views are dropped around every write — an
// INSERT would hit the view — and re-created before the next render.

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	msqlite "modernc.org/sqlite"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

const fix17ProbeFn = "fix17_rows_probe"

var (
	fix17ProbeOnce sync.Once
	fix17Counts    sync.Map // table -> *atomic.Int64
)

func fix17Counter(table string) *atomic.Int64 {
	c, _ := fix17Counts.LoadOrStore(table, new(atomic.Int64))
	return c.(*atomic.Int64)
}

func fix17RegisterProbe() {
	fix17ProbeOnce.Do(func() {
		msqlite.MustRegisterScalarFunction(fix17ProbeFn, 2,
			func(_ *msqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
				if table, ok := args[0].(string); ok {
					fix17Counter(table).Add(1)
				}
				return args[1], nil
			})
	})
}

// fix17Probe shadows each table with a counting view (probe on its
// probeCol, which no read filters or orders on) and returns the function
// that drops them again.
func fix17Probe(t *testing.T, st *store.Store, probes map[string]string) (drop func()) {
	t.Helper()
	err := st.LocalTx(func(tx *sql.Tx) error {
		for table, probeCol := range probes {
			rows, err := tx.Query(`SELECT name FROM pragma_table_info(?)`, table)
			if err != nil {
				return err
			}
			var sel []string
			found := false
			for rows.Next() {
				var c string
				if err := rows.Scan(&c); err != nil {
					rows.Close()
					return err
				}
				if c == probeCol {
					found = true
					c = fmt.Sprintf("%s('%s', %s) AS %s", fix17ProbeFn, table, c, c)
				}
				sel = append(sel, c)
			}
			rows.Close()
			if !found {
				return fmt.Errorf("table %s has no column %s", table, probeCol)
			}
			if _, err := tx.Exec("CREATE TEMP VIEW " + table + " AS SELECT " + strings.Join(sel, ", ") + " FROM main." + table); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("FIX17 CONTROL FAILED (instrument): installing the probe views: %v", err)
	}
	return func() {
		if err := st.LocalTx(func(tx *sql.Tx) error {
			for table := range probes {
				if _, err := tx.Exec("DROP VIEW temp." + table); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("FIX17: dropping the probe views: %v", err)
		}
	}
}

// fix17Seed builds a mid-size season over two posts: people check in and
// out every round (a degenerate trip each), one in ten is sent from site-b
// to site-a on a controlled run that arrives. Returns the trip count.
func fix17Seed(t *testing.T, st *store.Store, tick func(), people, rounds int) (trips int) {
	t.Helper()
	sites := []string{"site-a", "site-b"}
	vehicles := map[string]string{}
	vehicle := func(site, no string) string {
		if id, ok := vehicles[site+no]; ok {
			return id
		}
		id := w7SeedVehicleID(t, st, site, no)
		vehicles[site+no] = id
		return id
	}
	var ps []domain.Person
	for i := 0; i < people; i++ {
		ps = append(ps, fix9Person(t, st, sites[i%2], fmt.Sprintf("Surname%04d", i), fmt.Sprintf("Given%04d", i),
			fmt.Sprintf("Company %02d", i%8), fmt.Sprintf("(907) 555-%04d", i)))
		tick()
	}
	dest, mins := "site-a", int64(90)
	for round := 0; round < rounds; round++ {
		for i, p := range ps {
			trip, err := domain.DispatchLocalCheckIn(st, sites[i%2], 1, domain.DispatchInput{
				VehicleID: vehicle(sites[i%2], fmt.Sprintf("TRK-%03d", i%20)), DriverPersonID: p.PersonID}, 0, "default")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := domain.RecordExpectedArrival(st, sites[i%2], 1, trip.TripID, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
			trips++
			tick()
		}
		openB, err := domain.OpenVisitsAt(st, "site-b")
		if err != nil {
			t.Fatal(err)
		}
		for i, v := range openB {
			if i%10 == 0 {
				trip, err := domain.Dispatch(st, "site-b", 1, domain.DispatchInput{
					VehicleID: vehicle("site-b", fmt.Sprintf("RUN-%03d", i%6)), DriverPersonID: v.PersonID,
					DestinationSite: &dest, DeadlineMinutes: &mins}, 0, "default")
				if err != nil {
					t.Fatal(err)
				}
				trips++
				tick()
				if _, err := domain.RecordExpectedArrival(st, "site-a", 1, trip.TripID, nil, 0, "default"); err != nil {
					t.Fatal(err)
				}
			} else if err := domain.CheckOutVisit(st, "site-b", 1, v.VisitID, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
			tick()
		}
		openA, err := domain.OpenVisitsAt(st, "site-a")
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range openA {
			if err := domain.CheckOutVisit(st, "site-a", 1, v.VisitID, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
			tick()
		}
	}
	return trips
}

func TestFix17OfficerPagesReadBoundedRowsAtScale(t *testing.T) {
	fix17RegisterProbe()
	st, setClock := anStore(t)
	ms := time.Date(2026, 9, 1, 6, 0, 0, 0, time.Local).UnixMilli()
	tick := func() { ms += 7_000; setClock(ms) }
	tick()
	trips := fix17Seed(t, st, tick, 60, 6)
	events := 0
	streams, err := st.Watermarks()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range streams {
		events += int(m.AppliedSeq)
	}
	if trips < 300 || events < 1000 {
		t.Fatalf("PRECONDITION: the fixture is too small to tell a bounded read from a whole-table one (%d trips, %d events)", trips, events)
	}

	cfg := &config.Config{
		SiteID: "site-b", DisplayName: "Ice Camp B", ListenAddr: ":0", NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		Peers:         []config.Peer{{SiteID: "site-a", BaseURL: "http://example.invalid"}},
		Destinations:  []config.Destination{{ID: "site-a", DisplayName: "Ice Pad", Controlled: true, TravelMinutes: 65}},
		LoginPassword: testLoginPassword,
	}
	srv := New(cfg, st)
	cookie := loginCookie(t, srv, testLoginPassword)
	get := func(path string) {
		t.Helper()
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		srv.Root().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, rec.Code)
		}
	}
	pages := []string{"/gate", "/status", "/trips", "/muster", "/muster/print", "/gate/vehicle"}
	probes := map[string]string{"event_log": "payload", "trip": "origin_site"}
	measure := func(path string) (logRows, tripRows int64) {
		l0, t0 := fix17Counter("event_log").Load(), fix17Counter("trip").Load()
		get(path)
		return fix17Counter("event_log").Load() - l0, fix17Counter("trip").Load() - t0
	}

	// CONTROL: the instrument sees a whole-log walk and a whole-table read.
	drop := fix17Probe(t, st, probes)
	if l := measureEachEvent(t, st); l != int64(events) {
		t.Fatalf("FIX17 CONTROL FAILED (instrument): a whole-log EachEvent walk counted %d event rows, want %d", l, events)
	}
	t0 := fix17Counter("trip").Load()
	if _, err := st.DumpProjectionWhere("trip"); err != nil {
		t.Fatal(err)
	}
	if got := fix17Counter("trip").Load() - t0; got != int64(trips) {
		t.Fatalf("FIX17 CONTROL FAILED (instrument): a whole-table trip read counted %d rows, want %d", got, trips)
	}

	// Warm every page once, then render each again with nothing new.
	for _, p := range pages {
		get(p)
	}
	t.Logf("fixture: %d trips, %d events", trips, events)
	for _, p := range pages {
		logRows, tripRows := measure(p)
		t.Logf("GET %-14s nothing new: %5d event rows, %5d trip rows", p, logRows, tripRows)
		if logRows != 0 {
			t.Errorf("FIX17 (1): GET %s with nothing new walked %d event rows of %d; want 0 — the log replay behind the shift panel "+
				"is resumed and its result reused between writes (log_follow.go, store.Memo), never re-walked per render", p, logRows, events)
		}
		if tripRows > int64(trips/10) {
			t.Errorf("FIX17 (2): GET %s with nothing new read %d trip rows (the season holds %d); want at most %d — "+
				"whole-table trip reads are reused between writes, and /trips composes only the open runs", p, tripRows, trips, trips/10)
		}
	}

	// One write — a check-in — then the gate page the officer lands on.
	drop()
	before, _, err := domain.VisitReport(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	tick()
	p := fix9Person(t, st, "site-b", "Arrival", "Late", "Company 00", "")
	tick()
	trip, err := domain.DispatchLocalCheckIn(st, "site-b", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-b", "TRK-LATE"), DriverPersonID: p.PersonID}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RecordExpectedArrival(st, "site-b", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// The muster and gate after a write, by connection reads: everything on
	// a post queues for the store's ONE connection, so a page's read count —
	// not its wall time, which the host's load decides — is what put the
	// muster at 6-8 s idle and 17 s p95 under five officers (sc-stress F1,
	// F8). The not-accounted-for section read ~6 rows' worth of queries per
	// arrived run of the season; it now reads each table once. The first
	// render after the write rebuilds what the write invalidated; the bound
	// is a tenth of the season's trips, against ~6 per trip before.
	for _, p := range []string{"/muster", "/muster/print", "/gate", "/status", "/trips"} {
		if p == "/muster" || p == "/gate" {
			tick()
			if _, err := domain.CreatePerson(st, "site-b", 1, "Write "+p, nil, nil, 0, "default"); err != nil {
				t.Fatal(err)
			}
		}
		r0 := st.Reads()
		get(p)
		reads := st.Reads() - r0
		t.Logf("GET %-14s after a write: %5d connection reads", p, reads)
		if reads > int64(trips/10) {
			t.Errorf("FIX17 (muster/gate): GET %s after one write took the store's connection %d times (the season holds %d trips); "+
				"want at most %d — per-trip reads on this page queue behind every officer's request on the one connection",
				p, reads, trips, trips/10)
		}
	}

	drop = fix17Probe(t, st, probes)
	for _, p := range []string{"/gate", "/status"} {
		logRows, _ := measure(p)
		t.Logf("GET %-14s after one check-in: %5d event rows", p, logRows)
		if logRows > 20 {
			t.Errorf("FIX17 (1): GET %s after one check-in walked %d event rows of %d; want at most 20 — only the new events "+
				"(log_follow.go resumes each stream where it stopped)", p, logRows, events+4)
		}
	}
	after, _, err := domain.VisitReport(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)+1 {
		t.Errorf("FIX17 (1): the visit report holds %d visits after one check-in, want %d — a resumed replay must still see the new events", len(after), len(before)+1)
	}

	// (3) the three per-trip / per-arrival / per-person visit reads plan as
	// index searches. The statements are the store's own (projectionQuery's
	// shape: SELECT * … WHERE … ORDER BY the projection's key).
	drop()
	for _, c := range []struct{ q, search string }{
		{`SELECT * FROM visit WHERE trip_id = ? ORDER BY visit_id`, "(trip_id=?)"},
		{`SELECT * FROM visit WHERE site_id = ? AND node_epoch = ? AND seq = ? ORDER BY visit_id`, "(site_id=? AND node_epoch=? AND seq=?)"},
		{`SELECT * FROM visit WHERE person_id = ? ORDER BY visit_id`, "(person_id=?)"},
	} {
		q := c.q
		var plan []string
		if err := st.LocalTx(func(tx *sql.Tx) error {
			args := make([]any, strings.Count(q, "?"))
			for i := range args {
				args[i] = "x"
			}
			rows, err := tx.Query("EXPLAIN QUERY PLAN "+q, args...)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id, parent, notused int
				var detail string
				if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
					return err
				}
				plan = append(plan, detail)
			}
			return rows.Err()
		}); err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(plan, " | ")
		if !strings.Contains(joined, "SEARCH visit USING ") || !strings.Contains(joined, c.search) {
			t.Errorf("FIX17 (3): %q plans as %q; want a search on every equality, %s — the muster's not-accounted-for "+
				"section runs it per trip of the season, so anything less scans the season's visits per trip "+
				"(store.go: idx_visit_trip, idx_visit_key, idx_visit_person)", q, joined, c.search)
		}
	}
}

// measureEachEvent walks the whole log once through the store's one audit
// read and returns how many event rows the probe counted.
func measureEachEvent(t *testing.T, st *store.Store) int64 {
	t.Helper()
	streams, err := st.Watermarks()
	if err != nil {
		t.Fatal(err)
	}
	before := fix17Counter("event_log").Load()
	for _, m := range streams {
		if err := st.EachEvent(m.SiteID, m.NodeEpoch, 1, 0, func(store.Envelope) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	return fix17Counter("event_log").Load() - before
}
