// fix16_integ_test.go — the FIX16 integration's fold-ins, each across two
// streams that were built apart:
//   - the dispatch reports' late runs (stream E) time every run from the
//     TYPED departure (stream I, James 2026-10-06);
//   - those late runs and the overdue history (stream F) agree, run for run,
//     for the same period;
//   - the reports' lost-signal line reads this post's signal_outage record
//     (stream F) and says "not available" before it begins.
package web

import (
	"database/sql"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// fix16IntegWorld is f16eWorld (two days at site-b, one run 30 min late on
// Monday, one turned back inside its allowance on Tuesday) plus two runs
// with a TYPED time out, recorded with time_source "entered":
//
//	typedOpen:  recorded Tue 14:00, typed 12:50, 60-min allowance, still out.
//	            Due 13:50 by the typed time — late at 15:00. By the
//	            recording it would be due 15:00 and not yet late.
//	typedNight: recorded Tue 00:10, typed Mon 23:30, 60-min allowance,
//	            arrives at site-a Tue 00:45 — 15 min late by the typed time,
//	            25 min to spare by the recording. It LEFT on Monday.
//
// The deadline projections are registered so the overdue history can read
// the acknowledgment log.
func fix16IntegWorld(t *testing.T) (srv *Server, st *store.Store, typedOpen, typedNight string) {
	t.Helper()
	srv, st, setClock := f16eWorld(t)
	if err := deadline.Register(st); err != nil {
		t.Fatal(err)
	}
	var ps []string
	for _, n := range []string{"Typed, Tess", "Typed, Tom"} {
		p, err := domain.CreatePerson(st, "site-b", 1, n, nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		ps = append(ps, p.PersonID)
	}
	send := func(recorded, typed int64, driver string) string {
		setClock(recorded)
		checkIn(t, st, "site-b", driver, nil)
		dest, allow := "site-a", int64(60)
		trip, err := domain.Dispatch(st, "site-b", 1, domain.DispatchInput{
			VehicleID: w7SeedVehicleID(t, st, "site-b", "TRK-INT"), DriverPersonID: driver,
			DestinationSite: &dest, DeadlineMinutes: &allow,
		}, typed, "entered")
		if err != nil {
			t.Fatal(err)
		}
		return trip.TripID
	}
	typedNight = send(f16eAt(1, 0, 10), f16eAt(0, 23, 30), ps[0])
	setClock(f16eAt(1, 0, 45))
	if _, err := domain.RecordExpectedArrival(st, "site-a", 1, typedNight, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	typedOpen = send(f16eAt(1, 14, 0), f16eAt(1, 12, 50), ps[1])
	setClock(f16eAt(1, 15, 0))
	return srv, st, typedOpen, typedNight
}

// Every due time on the reports counts from the typed departure: the run
// typed 70 min back on a 60-min allowance is late at once, and the night
// run is late by the typed time though inside its allowance by the
// recording.
func TestFix16IntegLateRunsCountFromTheTypedTime(t *testing.T) {
	srv, st, typedOpen, typedNight := fix16IntegWorld(t)
	late, _, err := domain.RunsPastAllowance(st, f16eAt(1, 15, 0))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]domain.LateRun{}
	for _, l := range late {
		byID[l.TripID] = l
	}
	o, ok := byID[typedOpen]
	if !ok || !o.Open || o.DueMS != f16eAt(1, 13, 50) || o.OverByMS != 70*60_000 {
		t.Errorf("run typed out 12:50 on 60 min, still out at 15:00: %+v (listed %v), want OPEN, due 13:50, 70 min over", o, ok)
	}
	n, ok := byID[typedNight]
	if !ok || n.Open || n.DueMS != f16eAt(1, 0, 30) || n.OverByMS != 15*60_000 {
		t.Errorf("night run typed 23:30, arrived 00:45: %+v (listed %v), want arrived, due 00:30, 15 min late", n, ok)
	}

	// The daily report for Monday lists the night run: it LEFT on Monday.
	mon := time.UnixMilli(f16eAt(0, 0, 0))
	v, err := srv.buildDaily("site-b", mon, time.UnixMilli(f16eAt(1, 15, 0)))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range v.Late {
		if l.TripID == typedNight {
			found = true
			if !strings.Contains(l.Due, "00:30") {
				t.Errorf("night run's due on the daily report = %q, want 00:30 (typed 23:30 + 60 min)", l.Due)
			}
		}
	}
	if !found {
		t.Errorf("Monday's daily report does not list the night run that left Monday 23:30: %+v", v.Late)
	}
}

// The dispatch reports' late runs and the overdue history name the SAME
// runs for the same period — each day, the week, and every day's site-b
// report — once the history's own extras (cancelled runs and runs on the
// list only for an OVERDUE acknowledgment) are set aside.
func TestFix16IntegLateRunsAgreeWithTheOverdueHistory(t *testing.T) {
	srv, _, _, _ := fix16IntegWorld(t)
	now := time.UnixMilli(f16eAt(1, 15, 0))
	history := func(from, to string) []string {
		r := httptest.NewRequest("GET", "/history/overdue?from="+from+"&to="+to, nil)
		d := srv.buildOverdueHistory(r, now)
		if d.Err != "" || len(d.Filter.Problems) > 0 {
			t.Fatalf("overdue history %s..%s: %s %v", from, to, d.Err, d.Filter.Problems)
		}
		var ids []string
		for _, row := range d.Rows {
			// The history's own measure: "… late" or "… past due, and still open".
			past := strings.HasSuffix(row.Late, " late") || strings.Contains(row.Late, " past due")
			if row.outcomeCode == domain.RunCancelled || !past {
				continue
			}
			ids = append(ids, row.TripID)
		}
		sort.Strings(ids)
		return ids
	}
	ids := func(ls []lateRunView) []string {
		var out []string
		for _, l := range ls {
			out = append(out, l.TripID)
		}
		sort.Strings(out)
		return out
	}
	total := 0
	for day, date := range []string{"2026-09-28", "2026-09-29"} {
		v, err := srv.buildDaily("site-b", time.UnixMilli(f16eAt(day, 0, 0)), now)
		if err != nil {
			t.Fatal(err)
		}
		got, want := ids(v.Late), history(date, date)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: daily report's late runs %v, overdue history %v", date, got, want)
		}
		total += len(got)
	}
	if total != 3 {
		t.Errorf("%d late runs over the two days, want 3 (Monday's 30-min-late run and both typed runs)", total)
	}
	w, err := srv.buildWeekly(time.UnixMilli(f16eAt(0, 0, 0)))
	if err != nil {
		t.Fatal(err)
	}
	if want := history("2026-09-28", "2026-10-04"); w.Late != len(want) {
		t.Errorf("weekly roll-up counts %d runs past their allowance, the overdue history lists %d (%v)", w.Late, len(want), want)
	}
}

// The reports' lost-signal line reads this post's record: "not available"
// with no record, or for a period wholly before it began; a count and
// "recorded since" once it covers the period.
func TestFix16IntegReportSignalReadsTheLostSignalRecord(t *testing.T) {
	srv, st, _ := f16eWorld(t)
	mon, tue := time.UnixMilli(f16eAt(0, 0, 0)), time.UnixMilli(f16eAt(1, 0, 0))
	now := time.UnixMilli(f16eAt(1, 15, 0))

	v, err := srv.buildDaily("site-b", tue, now)
	if err != nil {
		t.Fatal(err)
	}
	if v.Signal.Available || !strings.Contains(v.Signal.Detail, "not available") {
		t.Errorf("no signal_outage table here: %+v, want not available", v.Signal)
	}

	if err := RegisterSignalHistory(st); err != nil {
		t.Fatal(err)
	}
	// The recorder first ran Tue 06:00; it saw site-a lost 08:00–08:20.
	if err := st.LocalTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO signal_outage (peer_site, lost_from_ms, never_heard, detected_ms, ended_ms, opened_run_ms, closed_run_ms)
		                   VALUES ('site-a', ?, 0, ?, ?, ?, ?)`,
			f16eAt(1, 8, 0), f16eAt(1, 8, 1), f16eAt(1, 8, 20), f16eAt(1, 6, 0), f16eAt(1, 6, 0))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	v, err = srv.buildDaily("site-b", mon, now)
	if err != nil {
		t.Fatal(err)
	}
	if v.Signal.Available || !strings.Contains(v.Signal.Detail, "recorded only since 29 Sep 2026 06:00") {
		t.Errorf("Monday, before the record began: %+v, want not available, recorded only since Tue 06:00", v.Signal)
	}
	v, err = srv.buildDaily("site-b", tue, now)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Signal.Available || v.Signal.Value != "1" || !strings.Contains(v.Signal.Detail, "recorded since 29 Sep 2026 06:00") ||
		!strings.Contains(v.Signal.Detail, "before that is not available") {
		t.Errorf("Tuesday: %+v, want 1 gap, recorded since Tue 06:00, the earlier part not available", v.Signal)
	}
	w, err := srv.buildWeekly(mon)
	if err != nil {
		t.Fatal(err)
	}
	if !w.Signal.Available || w.Signal.Value != "1" {
		t.Errorf("week: %+v, want 1 gap", w.Signal)
	}
	for _, row := range w.Sites {
		want := "0"
		if row.Site == "site-a" || row.Site == srv.Cfg.SiteID {
			want = "1" // site-a's own row, and this post's (its record is of everyone it lost)
		}
		if row.Signal.Value != want {
			t.Errorf("week, %s: %q gaps, want %s", row.Site, row.Signal.Value, want)
		}
	}
	code, body := f16eGet(t, srv, "", "/analytics/reports/daily?site=site-b&date=2026-09-29", true)
	if code != 200 || !strings.Contains(body, `data-rp="signal">1<`) || !strings.Contains(body, "recorded since") {
		t.Errorf("daily page %d: the lost-signal row does not show the record", code)
	}
}

// D-48 (TestW5aNothingParsesAName): search by last or first name reads the
// person's STORED parts, never a part derived from the whole name. A person
// whose stored last name is two words is found by either word under Last
// and not by a first-name word under Last; a person with no parts on record
// is matched on the whole name as written. Fail-before: splitName took the
// final word as the last name, so "Kit Nameonly Carver" (no stored parts)
// was not found under last=nameonly.
func TestFix16IntegSearchByNamePartReadsTheStoredParts(t *testing.T) {
	w := newFix9World(t)
	st := w.srv.Store
	last, first := "O'Neil Smith", "Mary Ann"
	if _, err := domain.CreatePersonWithDetails(st, "site-b", 1, "Mary Ann O'Neil Smith", nil, nil, nil,
		domain.PersonDetails{LastName: &last, FirstName: &first}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := domain.CreatePerson(st, "site-b", 1, "Kit Nameonly Carver", nil, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		q       url.Values
		present []string
		absent  []string
	}{
		{url.Values{"last": {"o'neil"}}, []string{"Mary Ann O&#39;Neil Smith"}, []string{"Kit Nameonly Carver"}},
		{url.Values{"last": {"smith"}, "first": {"mary ann"}}, []string{"Mary Ann O&#39;Neil Smith"}, nil},
		// "Mary" is her stored FIRST name: it does not match under Last.
		{url.Values{"last": {"mary"}}, nil, []string{"Mary Ann O&#39;Neil Smith"}},
		// No parts on record: the whole name, as written, under either filter.
		{url.Values{"last": {"nameonly"}}, []string{"Kit Nameonly Carver"}, nil},
		{url.Values{"first": {"carver"}}, []string{"Kit Nameonly Carver"}, nil},
		// The fix9 people still answer by their stored parts.
		{url.Values{"last": {"ashton"}}, []string{"Ashton, Joss"}, []string{"Driver, Dana"}},
		{url.Values{"first": {"dana"}}, []string{"Driver, Dana"}, []string{"Ashton, Joss"}},
	}
	for _, c := range cases {
		people := fix9Group(fix9Must(t, w, c.q), "people")
		for _, n := range c.present {
			if !strings.Contains(people, n) {
				t.Errorf("%s: people group lacks %q:\n%s", c.q.Encode(), n, people)
			}
		}
		for _, n := range c.absent {
			if strings.Contains(people, n) {
				t.Errorf("%s: people group carries %q:\n%s", c.q.Encode(), n, people)
			}
		}
	}
}
