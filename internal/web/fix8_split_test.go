// fix8_split_test.go — ⟦FIX8 option C, James, 2026-10-05⟧ rows with
// differing times are recorded as one trip per time, inside one Transact
// (gate_split.go). A named vehicle with differing times is refused; every
// time-group needs a driver.
package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// fix8Stage ticks visits on the roster with their own Time out boxes and
// returns the departing vehicle form's body, as "Check out selected"
// staged it.
func fix8Stage(t *testing.T, srv *Server, vids []string, times []string) url.Values {
	t.Helper()
	form := url.Values{}
	for i, v := range vids {
		form.Add("visit_ids", v)
		if times[i] != "" {
			form.Set("time_out_"+v, times[i])
		}
	}
	rec, _ := w2PostForm(t, srv, "/gate/checkout/select", form)
	if rec.Code != http.StatusOK {
		t.Fatalf("/gate/checkout/select = %d: %s", rec.Code, rec.Body.String())
	}
	return gateFormValues(t, rec.Body.String(), formIDFor(gateOutbound))
}

type fix8Dispatch struct {
	env       store.Envelope
	driver    string
	occupants []string
	unident   bool
	deadline  *int64
}

func fix8Dispatches(t *testing.T, st *store.Store, after int64) []fix8Dispatch {
	t.Helper()
	var out []fix8Dispatch
	for _, e := range w2EventsOfType(t, w2EventsFrom(t, st, "site-a", 1, after), "trip_dispatched") {
		var p struct {
			Driver    string   `json:"driver_person_id"`
			Occupants []string `json:"occupant_person_ids"`
			Unident   bool     `json:"vehicle_unidentified"`
			Deadline  *int64   `json:"deadline_minutes"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatal(err)
		}
		out = append(out, fix8Dispatch{env: e, driver: p.Driver, occupants: p.Occupants, unident: p.Unident, deadline: p.Deadline})
	}
	return out
}

func fix8PersonIDs(t *testing.T, st *store.Store) map[string]string {
	t.Helper()
	ps, err := domain.Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	for _, p := range ps {
		m[p.Name] = p.PersonID
	}
	return m
}

// Three people out at two times with Vehicle unknown → two departures, each
// at its own entered instant, each with its own driver, each judged against
// its own deadline from that instant (dispatchTrip's D13 rule: the payload
// carries the minutes, the instant is the trip's own envelope).
func TestFix8SplitCheckOutRecordsOneTripPerTime(t *testing.T) {
	srv, st, vids, _ := fix8World(t)
	id := fix8PersonIDs(t, st)
	t1, t2 := w2Base-60*w2Minute, w2Base-30*w2Minute
	vals := fix8Stage(t, srv, vids, []string{fix8Wall(t1), fix8Wall(t1), fix8Wall(t2)})
	vals.Set("vehicle_unknown", "1")
	vals.Set("destination_id", "site-b")
	vals.Set("deadline_minutes", "90")
	vals = gateMarkDriver(vals, 0)
	before := w2Watermark(t, st, "site-a", 1)
	rec, _ := w2PostForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("split check-out = %d: %s", rec.Code, rec.Body.String())
	}
	ds := fix8Dispatches(t, st, before)
	if len(ds) != 2 {
		t.Fatalf("%d departures recorded, want 2 (one per time)", len(ds))
	}
	want := []struct {
		at     int64
		driver string
		occ    []string
	}{{t1, id["Dana Driver"], []string{id["Dana Driver"], id["Pat Passenger"]}}, {t2, id["Chris Crew"], []string{id["Chris Crew"]}}} // the payload's occupants include the driver
	for i, w := range want {
		d := ds[i]
		if d.env.OccurredAt != w.at || d.env.TimeSource != "entered" {
			t.Fatalf("departure %d stored (%d, %q), want (%d, entered)", i, d.env.OccurredAt, d.env.TimeSource, w.at)
		}
		if d.driver != w.driver || strings.Join(d.occupants, ",") != strings.Join(w.occ, ",") || !d.unident {
			t.Fatalf("departure %d: driver %s occupants %v unidentified %v, want %s %v true", i, d.driver, d.occupants, d.unident, w.driver, w.occ)
		}
		if d.deadline == nil || *d.deadline != 90 {
			t.Fatalf("departure %d deadline = %v, want 90 minutes from its own instant", i, d.deadline)
		}
	}
	open, _ := domain.OpenVisitsAt(st, "site-a")
	if len(open) != 0 {
		t.Fatalf("%d visits still open after everyone left", len(open))
	}
}

func TestFix8SplitNamedVehicleWithDifferingTimesIsRefused(t *testing.T) {
	srv, st, vids, _ := fix8World(t)
	vals := fix8Stage(t, srv, vids, []string{fix8Wall(w2Base - 60*w2Minute), "", fix8Wall(w2Base - 30*w2Minute)})
	vals.Set("vehicle", "TRK-F8-NAMED")
	vals.Set("destination_id", "conneco")
	vals = gateMarkDriver(vals, 0)
	before := w2Watermark(t, st, "site-a", 1)
	rec, _ := w2PostForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusConflict {
		t.Fatalf("= %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if msg := vehicleError(t, rec.Body.String(), gateOutbound); !strings.Contains(msg, "one vehicle moves once") || !strings.Contains(msg, "Vehicle unknown") {
		t.Fatalf("refusal does not say one vehicle moves once / Vehicle unknown: %q", msg)
	}
	if after := w2Watermark(t, st, "site-a", 1); after != before {
		t.Fatalf("refused split authored %d event(s)", after-before)
	}
}

// A time-group of two without the marked driver is refused naming its time,
// the page then offers that group's own driver picker, and a pick commits.
func TestFix8SplitDriverlessGroupIsRefusedThenPicked(t *testing.T) {
	srv, st, vids, _ := fix8World(t)
	id := fix8PersonIDs(t, st)
	t1, t2 := w2Base-60*w2Minute, w2Base-30*w2Minute
	vals := fix8Stage(t, srv, vids, []string{fix8Wall(t1), fix8Wall(t2), fix8Wall(t2)})
	vals.Set("vehicle_unknown", "1")
	vals.Set("destination_id", "conneco")
	vals = gateMarkDriver(vals, 0)
	before := w2Watermark(t, st, "site-a", 1)
	rec, _ := w2PostForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusConflict {
		t.Fatalf("driverless group = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()
	if msg := vehicleError(t, page, gateOutbound); !strings.Contains(msg, "Nobody is marked as driving") || !strings.Contains(msg, wallDisplay(t2)) {
		t.Fatalf("refusal does not name the driverless group's time: %q", msg)
	}
	if after := w2Watermark(t, st, "site-a", 1); after != before {
		t.Fatalf("refused split authored %d event(s)", after-before)
	}
	key := strconv.FormatInt(t2, 10)
	if !strings.Contains(page, `name="group_driver_`+key+`"`) || !strings.Contains(page, "Driver for the people leaving at "+wallDisplay(t2)) {
		t.Fatalf("the re-rendered form offers no driver picker for the %s group", wallDisplay(t2))
	}
	vals.Set("group_driver_"+key, "existing:"+id["Chris Crew"])
	rec, _ = w2PostForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("with the group driver picked = %d: %s", rec.Code, rec.Body.String())
	}
	ds := fix8Dispatches(t, st, before)
	if len(ds) != 2 || ds[1].driver != id["Chris Crew"] || strings.Join(ds[1].occupants, ",") != id["Pat Passenger"]+","+id["Chris Crew"] && strings.Join(ds[1].occupants, ",") != id["Chris Crew"]+","+id["Pat Passenger"] {
		t.Fatalf("departures %+v: want the %s group driven by Chris with Pat aboard", ds, wallDisplay(t2))
	}
}

// Blank rows inherit the form-level time (the JS-off apply-to-all).
func TestFix8SplitBlankRowsInheritTheFormTime(t *testing.T) {
	srv, st, vids, _ := fix8World(t)
	own, all := w2Base-60*w2Minute, w2Base-45*w2Minute
	vals := fix8Stage(t, srv, vids, []string{fix8Wall(own), "", ""})
	vals.Set("time_source", "entered")
	vals.Set("occurred_at", fix8Wall(all))
	vals.Set("vehicle_unknown", "1")
	vals.Set("destination_id", "conneco")
	vals = gateMarkDriver(vals, 1)
	before := w2Watermark(t, st, "site-a", 1)
	rec, _ := w2PostForm(t, srv, "/gate/checkout/vehicle", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("= %d: %s", rec.Code, rec.Body.String())
	}
	ds := fix8Dispatches(t, st, before)
	if len(ds) != 2 || ds[0].env.OccurredAt != own || ds[1].env.OccurredAt != all || ds[1].env.TimeSource != "entered" {
		t.Fatalf("departures %+v: want one at the row's own time and one at the form time", ds)
	}
	if len(ds[1].occupants) != 2 {
		t.Fatalf("the form-time departure carries %d occupant(s) (driver included), want 2", len(ds[1].occupants))
	}
}

// The vehicle/group CHECK-IN door splits the same way: one arrival per time.
func TestFix8SplitCheckInRecordsOneArrivalPerTime(t *testing.T) {
	srv, st, persons := newW2Server(t, w2Base-6*60*w2Minute)
	page, vals := gateBuild(t, srv, gateInbound, "TRK-F8-IN", []string{persons[0].Name, persons[1].Name, persons[2].Name}, 0)
	_ = page
	t1, t2 := w2Base-90*w2Minute, w2Base-40*w2Minute
	vals["occupant_time"] = []string{fix8Wall(t1), "", fix8Wall(t2)}
	vals.Set("vehicle", "")
	vals.Set("vehicle_unknown", "1")
	vals.Set("time_source", "entered")
	vals.Set("occurred_at", fix8Wall(t1))
	before := w2Watermark(t, st, "site-a", 1)
	rec, _ := w2PostForm(t, srv, "/gate/checkin", vals)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("split check-in = %d: %s", rec.Code, rec.Body.String())
	}
	arr := w2EventsOfType(t, w2EventsFrom(t, st, "site-a", 1, before), "trip_arrival")
	if len(arr) != 2 || arr[0].OccurredAt != t1 || arr[1].OccurredAt != t2 || arr[0].TimeSource != "entered" || arr[1].TimeSource != "entered" {
		t.Fatalf("arrivals %d, want 2 at %d and %d, entered", len(arr), t1, t2)
	}
	open, _ := domain.OpenVisitsAt(st, "site-a")
	in, err := domain.VisitSignInTimes(st, "site-a", open)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{persons[0].PersonID: t1, persons[1].PersonID: t1, persons[2].PersonID: t2}
	for _, v := range open {
		if in[v.VisitID] != want[v.PersonID] {
			t.Fatalf("%s signed in at %d, want %d", v.PersonID, in[v.VisitID], want[v.PersonID])
		}
	}
	if len(open) != 3 {
		t.Fatalf("%d open visits, want 3", len(open))
	}
}

// D1 — nobody on no muster: after a split departure to a controlled post,
// every person is named on this post's muster (on the road), nobody is
// both on site and gone, and nobody has vanished.
func TestFix8SplitCheckOutKeepsEveryoneOnTheMuster(t *testing.T) {
	srv, st, vids, _ := fix8World(t)
	vals := fix8Stage(t, srv, vids, []string{fix8Wall(w2Base - 60*w2Minute), "", fix8Wall(w2Base - 30*w2Minute)})
	vals.Set("vehicle_unknown", "1")
	vals.Set("destination_id", "site-b")
	vals.Set("deadline_minutes", "600")
	vals = gateMarkDriver(vals, 0)
	if rec, _ := w2PostForm(t, srv, "/gate/checkout/vehicle", vals); rec.Code != http.StatusSeeOther {
		t.Fatalf("= %d: %s", rec.Code, rec.Body.String())
	}
	cookie := loginCookie(t, srv, testLoginPassword)
	page := w2Get(t, srv, "/muster", cookie).Body.String()
	for _, n := range []string{"Dana Driver", "Pat Passenger", "Chris Crew"} {
		if !strings.Contains(page, n) {
			t.Fatalf("%s is on no muster section after the split departure", n)
		}
	}
	if open, _ := domain.OpenVisitsAt(st, "site-a"); len(open) != 0 {
		t.Fatalf("%d still on site", len(open))
	}
}

// Replication: the other post, given site-a's stream, reports each person's
// own departure time.
func TestFix8SplitTimesReplicateToTheOtherPost(t *testing.T) {
	srv, st, vids, _ := fix8World(t)
	id := fix8PersonIDs(t, st)
	t1, t2 := w2Base-60*w2Minute, w2Base-30*w2Minute
	vals := fix8Stage(t, srv, vids, []string{fix8Wall(t1), fix8Wall(t1), fix8Wall(t2)})
	vals.Set("vehicle_unknown", "1")
	vals.Set("destination_id", "conneco")
	vals = gateMarkDriver(vals, 0)
	if rec, _ := w2PostForm(t, srv, "/gate/checkout/vehicle", vals); rec.Code != http.StatusSeeOther {
		t.Fatalf("= %d: %s", rec.Code, rec.Body.String())
	}
	other, err := store.Open(filepath.Join(t.TempDir(), "post-b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	if err := domain.Register(other); err != nil {
		t.Fatal(err)
	}
	evs, err := st.Events("site-a", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.ApplyBatch(evs); err != nil {
		t.Fatal(err)
	}
	recs, _, err := domain.VisitReport(other, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{id["Dana Driver"]: t1, id["Pat Passenger"]: t1, id["Chris Crew"]: t2}
	seen := 0
	for _, r := range recs {
		w, ok := want[r.PersonID]
		if !ok || r.ClosedBy == domain.VisitStillOpen {
			continue
		}
		seen++
		if r.ClosedAtMS != w {
			t.Fatalf("the other post reports %s out at %d, want %d", r.PersonName, r.ClosedAtMS, w)
		}
	}
	if seen != 3 {
		t.Fatalf("the other post holds %d of 3 departures", seen)
	}
}
