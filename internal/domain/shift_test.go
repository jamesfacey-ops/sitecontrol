// shift_test.go — FIX16 G: the shift window and the shift totals. Expected
// numbers are hand-derived in the comments (anSeedSiteA's log), not read
// back from the code under test.
package domain

import (
	"testing"
	"time"

	"sitecontrol/internal/store"
)

func TestShiftWindowDefaultAndConfigured(t *testing.T) {
	at := vrAt
	cases := []struct {
		name       string
		now        int64
		starts     []int
		start, end int64
	}{
		{"default, day shift", at(0, 10, 0), nil, at(0, 6, 0), at(0, 18, 0)},
		{"default, evening is the night shift", at(0, 20, 0), nil, at(0, 18, 0), at(1, 6, 0)},
		{"default, after midnight is still last evening's shift (crosses midnight)", at(1, 3, 0), nil, at(0, 18, 0), at(1, 6, 0)},
		{"default, exactly on a change belongs to the new shift", at(0, 6, 0), nil, at(0, 6, 0), at(0, 18, 0)},
		{"default, one minute before a change is the old shift", at(0, 17, 59), nil, at(0, 6, 0), at(0, 18, 0)},
		{"configured 07:30/19:30, 07:00 is still the night shift", at(0, 7, 0), []int{19*60 + 30, 7*60 + 30}, at(-1, 19, 30), at(0, 7, 30)},
		{"configured 07:30/19:30, 08:00", at(0, 8, 0), []int{7*60 + 30, 19*60 + 30}, at(0, 7, 30), at(0, 19, 30)},
		{"three shifts, 15:00", at(0, 15, 0), []int{0, 8 * 60, 16 * 60}, at(0, 8, 0), at(0, 16, 0)},
		{"three shifts, 23:30 ends at the next midnight", at(0, 23, 30), []int{0, 8 * 60, 16 * 60}, at(0, 16, 0), at(1, 0, 0)},
		{"one change a day is a 24-hour shift", at(0, 5, 0), []int{6 * 60}, at(-1, 6, 0), at(0, 6, 0)},
	}
	for _, c := range cases {
		s, e := ShiftWindow(time.UnixMilli(c.now), time.UTC, c.starts)
		if s != c.start || e != c.end {
			t.Errorf("%s: window = %s..%s, want %s..%s", c.name,
				time.UnixMilli(s).UTC().Format("01-02 15:04"), time.UnixMilli(e).UTC().Format("01-02 15:04"),
				time.UnixMilli(c.start).UTC().Format("01-02 15:04"), time.UnixMilli(c.end).UTC().Format("01-02 15:04"))
		}
	}
}

func TestParseShiftStart(t *testing.T) {
	for in, want := range map[string]int{"06:00": 360, "18:30": 1110, "00:00": 0, "23:59": 1439} {
		if got, err := ParseShiftStart(in); err != nil || got != want {
			t.Errorf("ParseShiftStart(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "6:00", "24:00", "06:60", "0600", "06:00:00", "six"} {
		if _, err := ParseShiftStart(bad); err == nil {
			t.Errorf("ParseShiftStart(%q) accepted", bad)
		}
	}
}

// anSeedShiftEvening adds an evening and a past-midnight set of events to
// site-a's seeded log (clock continues forward from 13:00 on day 0):
//
//	day0 22:00 Eve checks in; 23:30 Eve signs out
//	day1 01:00 Fay checks in
func anSeedShiftEvening(t *testing.T, st *store.Store) {
	t.Helper()
	setClock := vrFixedClock(st, store.ClockSynced)
	truck := w7SeedVehicleID(t, st, "site-a", "TRUCK 1")
	setClock(vrAt(0, 21, 0))
	cos, err := Companies(st)
	if err != nil || len(cos) == 0 {
		t.Fatalf("companies: %v", err)
	}
	trs, err := Trades(st)
	if err != nil || len(trs) == 0 {
		t.Fatalf("trades: %v", err)
	}
	var ps []Person
	for _, n := range []string{"Eve Evening", "Fay Fresh"} {
		p, err := CreatePerson(st, "site-a", 1, n, &cos[0].CompanyID, &trs[0].TradeID, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		ps = append(ps, p)
	}
	setClock(vrAt(0, 22, 0))
	vrCheckIn(t, st, "site-a", truck, ps[0].PersonID)
	setClock(vrAt(0, 23, 30))
	if err := CheckOutVisit(st, "site-a", 1, vrVisitOf(t, st, "site-a", ps[0].PersonID, true).VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	setClock(vrAt(1, 1, 0))
	vrCheckIn(t, st, "site-a", truck, ps[1].PersonID)
}

// Seeded log (anSeedSiteA + the evening): day0 10:00 Ann, Bob, Cal in; 12:00
// Ann out; 13:00 Bob dispatched to site-b; day0 22:00 Eve in, 23:30 Eve out;
// day1 01:00 Fay in. On site at the end: Cal, Fay.
func TestShiftTotalsByShift(t *testing.T) {
	st, _ := anSeedSiteA(t)
	anSeedShiftEvening(t, st)
	utc := time.UTC
	get := func(now int64, starts []int) ShiftTotals {
		t.Helper()
		got, err := BuildShiftTotals(st, "site-a", time.UnixMilli(now), utc, starts)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	// Day shift 06:00-18:00 on day 0: Ann/Bob/Cal in, Ann out, Bob's dispatch
	// (a departure for Bob's visit, so a second sign-out), one vehicle out.
	// NB: Bob's visit is closed BY DISPATCH, which is a recorded departure.
	d := get(vrAt(0, 15, 0), nil)
	if d.SignIns != 3 || d.SignOuts != 2 || d.VehiclesOut != 1 || d.VehiclesIn != 0 {
		t.Errorf("day shift: %+v, want in 3 out 2 vehOut 1 vehIn 0", d)
	}
	// Night shift 18:00 day0 - 06:00 day1, read after midnight: Eve in/out,
	// Fay in; nothing from the day shift leaks in.
	n := get(vrAt(1, 2, 0), nil)
	if n.SignIns != 2 || n.SignOuts != 1 || n.VehiclesOut != 0 || n.VehiclesIn != 0 || n.OnSite != 2 {
		t.Errorf("night shift (crosses midnight): %+v, want in 2 out 1 vehOut 0 vehIn 0 onSite 2", n)
	}
	if n.StartMS != vrAt(0, 18, 0) || n.EndMS != vrAt(1, 6, 0) {
		t.Errorf("night window = %d..%d", n.StartMS, n.EndMS)
	}
	// The same instant on a configured 23:00 change: only Fay (01:00) is in
	// the shift that began 23:00; Eve's 23:30 sign-out is in it too.
	c := get(vrAt(1, 2, 0), []int{7 * 60, 23 * 60})
	if c.SignIns != 1 || c.SignOuts != 1 {
		t.Errorf("configured 07:00/23:00: %+v, want in 1 out 1", c)
	}
	// On-site is "right now" whatever the window.
	if d.OnSite != 2 {
		t.Errorf("on site = %d, want 2 (Cal, Fay)", d.OnSite)
	}
}

// The panel's counts are analytics' "today" counts: with a single change at
// midnight the shift IS the local day, so every figure must equal the
// analytics page's, for both sites, including a vehicle arrival.
func TestShiftTotalsAgreeWithAnalyticsToday(t *testing.T) {
	a, trip := anSeedSiteA(t)
	anSeedShiftEvening(t, a)
	b := newDomainTestStore(t)
	setB := vrFixedClock(b, store.ClockSynced)
	evs, err := a.Events("site-a", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if _, err := b.Apply(ev); err != nil {
			t.Fatal(err)
		}
	}
	setB(vrAt(0, 14, 0))
	if _, err := RecordExpectedArrival(b, "site-b", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(vrAt(0, 23, 45))
	fig, err := BuildAnalytics(b, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	for _, site := range []string{"site-a", "site-b"} {
		got, err := BuildShiftTotals(b, site, now, time.UTC, []int{0})
		if err != nil {
			t.Fatal(err)
		}
		want := anSite(t, fig, site)
		if got.SignIns != want.VisitsToday || got.VehiclesOut != want.TravelOutToday+want.RoadWorkOutToday ||
			got.VehiclesIn != want.TravelInToday || got.OnSite != want.OnSiteNow {
			t.Errorf("%s: shift %+v disagrees with analytics today %+v", site, got, want)
		}
		if got.SignOuts != want.SignOutsToday {
			t.Errorf("%s: sign-outs %d vs analytics %d", site, got.SignOuts, want.SignOutsToday)
		}
	}
	// Hand-derived, so agreement is not two copies of one bug: site-a day 0
	// has 4 sign-ins (Ann, Bob, Cal, Eve; Fay is day 1), sign-outs
	// Ann, Bob (dispatch), Eve = 3, one vehicle out; site-b one vehicle in.
	ga, _ := BuildShiftTotals(b, "site-a", now, time.UTC, []int{0})
	gb, _ := BuildShiftTotals(b, "site-b", now, time.UTC, []int{0})
	if ga.SignIns != 4 || ga.SignOuts != 3 || ga.VehiclesOut != 1 || gb.VehiclesIn != 1 {
		t.Errorf("hand-derived: site-a %+v site-b %+v, want a in 4 out 3 vehOut 1, b vehIn 1", ga, gb)
	}
}
