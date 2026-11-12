package web

// fix16i_typed_departure_test.go — James 2026-10-06: "from the typed time".
// A checkout with a typed, earlier time out counts its allowance from the
// typed time at the gate's own scan, at the far end, and on a third post's
// dispatch view — one effective departure (domain.EffectiveDeparture).
//
// Fail-before: on 1cbf34f each test fails — every judge counted from the
// recording (FINDINGS-R4 D13's authored_at).

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
)

func fix16iNear(got, want time.Duration) bool {
	d := got - want
	return d > -10*time.Second && d < 10*time.Second
}

// People checked out at the gate with their own typed times out (FIX8: one
// trip per time) each count the 90-minute allowance from THEIR typed time.
func TestFix16iGateCheckoutWithTypedTimesCountsFromEachTypedTime(t *testing.T) {
	srv, st, vids, _ := fix8World(t)
	t1, t2 := w2Base-60*w2Minute, w2Base-30*w2Minute
	vals := fix8Stage(t, srv, vids, []string{fix8Wall(t1), fix8Wall(t1), fix8Wall(t2)})
	vals.Set("vehicle_unknown", "1")
	vals.Set("destination_id", "site-b")
	vals.Set("deadline_minutes", "90")
	vals = gateMarkDriver(vals, 0)
	before := w2Watermark(t, st, "site-a", 1)
	if rec, _ := w2PostForm(t, srv, "/gate/checkout/vehicle", vals); rec.Code != http.StatusSeeOther {
		t.Fatalf("check-out = %d: %s", rec.Code, rec.Body.String())
	}
	ds := fix8Dispatches(t, st, before)
	if len(ds) != 2 || ds[0].env.OccurredAt != t1 || ds[1].env.OccurredAt != t2 || ds[0].env.AuthoredAt.WallMS != w2Base {
		t.Fatalf("precondition: want two departures typed at %d and %d, recorded at %d: %+v", t1, t2, w2Base, ds)
	}

	if err := deadline.Register(st); err != nil {
		t.Fatal(err)
	}
	d, err := deadline.New(st, "site-a", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	d.WallNow = func() time.Time { return time.UnixMilli(w2Base) }
	board, err := d.Evaluate()
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64]time.Duration{t1: 30 * time.Minute, t2: 60 * time.Minute}
	if len(board.Alerts) != 2 {
		t.Fatalf("%d rows on the origin's board, want the 2 outbound runs", len(board.Alerts))
	}
	for _, a := range board.Alerts {
		var typed int64
		for _, x := range ds {
			var p struct {
				TripID string `json:"trip_id"`
			}
			if err := json.Unmarshal(x.env.Payload, &p); err != nil {
				t.Fatal(err)
			}
			if p.TripID == a.TripID {
				typed = x.env.OccurredAt
			}
		}
		if a.Status != deadline.StatusCountdown || !fix16iNear(a.Remaining, want[typed]) {
			t.Errorf("run typed out at %s: %q remaining %s, want %s (90 min from the typed time)",
				time.UnixMilli(typed).Format("15:04"), a.Status, a.Remaining, want[typed])
		}
	}
}

// A third post's dispatch view judges another post's run from the same
// typed time: typed 100 minutes back on a 90-minute allowance, it is red
// OVERDUE by about 10 minutes the moment it is recorded.
func TestFix16iDispatchOnAnotherPostAgreesWithTheTypedTime(t *testing.T) {
	srv, st, _ := fix14ThirdPost(t)
	var riding []string
	for _, n := range []string{"Typed, Terry", "Typed, Toni"} {
		p, err := domain.CreatePerson(st, "site-a", 1, n, nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		riding = append(riding, p.PersonID)
	}
	checkIn(t, st, "site-a", riding[0], riding[1:])
	dest, x := "site-b", int64(90)
	typed := time.Now().Add(-100 * time.Minute).UnixMilli()
	trip, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-116"), DriverPersonID: riding[0], OccupantPersonIDs: riding[1:],
		DestinationSite: &dest, DeadlineMinutes: &x,
	}, typed, "entered")
	if err != nil {
		t.Fatal(err)
	}

	view, err := srv.dispatchPastAllowance(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var got *deadline.Alert
	for i := range view.Rows {
		if view.Rows[i].TripID == trip.TripID {
			got = &view.Rows[i]
		}
	}
	if got == nil {
		t.Fatalf("the run typed out 100 min ago on a 90-min allowance is not on the dispatch strip: %+v", view.Rows)
	}
	if got.Status != deadline.StatusOverdueLive || !fix16iNear(got.OverdueBy, 10*time.Minute) || !got.DueAt.Equal(time.UnixMilli(typed).Add(90*time.Minute)) {
		t.Fatalf("dispatch row %q by %s due %v, want OVERDUE by about 10 min, due typed + 90", got.Status, got.OverdueBy, got.DueAt)
	}

	// The origin's own scan agrees on the same replicated bytes.
	d, err := deadline.New(st, "site-a", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	board, err := d.Evaluate()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range board.Alerts {
		if a.TripID == trip.TripID && (a.Status != deadline.StatusOverdueLive || !fix16iNear(a.OverdueBy, 10*time.Minute)) {
			t.Fatalf("origin's own scan: %q by %s, want OVERDUE by about 10 min like the dispatch view", a.Status, a.OverdueBy)
		}
	}
	if b := srv.recordedBasis(trip, time.Time{}); !b.Equal(time.UnixMilli(typed)) {
		t.Fatalf("the site page's basis is %v, want the typed time %v", b, time.UnixMilli(typed))
	}
}

// The overdue history (FIX16f) times a run from the same typed time as the
// countdown: typed out 50 min before it was recorded on a 30-min allowance
// and arrived 10 min after the recording, it went 30 min past its deadline,
// and the due time says it was timed from the entered time.
func TestFix16iOverdueHistoryTimesFromTheTypedTime(t *testing.T) {
	w := fix16fSeed(t)
	st := w.st
	rec := w.t0.Add(4 * time.Hour)
	w.at(rec)
	p, err := domain.CreatePerson(st, "site-a", 1, "Typed, Tara", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	checkIn(t, st, "site-a", p.PersonID, nil)
	dest, x := "site-b", int64(30)
	typed := rec.Add(-50 * time.Minute)
	tr, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-TYPED"), DriverPersonID: p.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &x,
	}, typed.UnixMilli(), "entered")
	if err != nil {
		t.Fatal(err)
	}
	w.at(rec.Add(10 * time.Minute))
	if _, err := domain.RecordExpectedArrival(st, "site-b", 1, tr.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	w.at(w.t0.Add(6 * time.Hour))
	body := fix7Do(t, w.server("site-c"), "GET", fix7DispatchHost, "/history/overdue", nil).Body.String()
	row, ok := fix16fOverdueRows(t, body)[tr.TripID]
	if !ok {
		t.Fatalf("the run typed out 50 min back on a 30-min allowance is not in the overdue history:\n%s", body)
	}
	due := typed.Add(30 * time.Minute)
	for _, s := range []string{"30 minutes late", historyWhen(due.UnixMilli()), "(timed from the entered time, " + historyWhen(typed.UnixMilli()) + ")"} {
		if !strings.Contains(row, s) {
			t.Errorf("row lacks %q:\n%s", s, row)
		}
	}
	if strings.Contains(row, "recorded") {
		t.Errorf("row still says the time counts from the recording:\n%s", row)
	}
}
