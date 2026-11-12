// w2_wiring_test.go — W2-6: one wiring test per takes-input HANDLER BODY —
// nine tests over the eight routes, row 1 double-bodied (new shape +
// legacy shape) — each with three arms against its own body's parse:
//
//   - override:    that body's POST with an override produces an `entered`
//     event END-TO-END — stored row asserted, never a handler
//     return, including stored OccurredAt == the posted
//     value's parse (kills the half-wire (0, postedTimeSource);
//     mutants m12/m7 per body). RED against the pre-chunk tree:
//     zero capture exists at 49abf65, so the stored row reads
//     default/now — the RED row IS the shipped defect.
//   - untouched:   the same body's POST with time_source=default and a
//     DWELL-SHAPED echoed value (echo != AuthoredAt) stores
//     `default` with the rule-W equality and never stores the
//     echo — W2-1's behaviour discharged per body (TP §7.6's
//     dwell fixture; mutants m7 and m3/mTP-4 die here).
//   - signal-less: the same body's POST with NO time_source field at all
//     is REFUSED 400 and authors NOTHING — W2-9's behaviour
//     per body (TP §4.5: the server never infers; mutant
//     mTP-3 per body). RED against the pre-chunk tree (the
//     handlers 303 today).
//
// Row 3's single time input applies to EVERY visit_checked_out minted by
// that POST (TP §4.8 row 3, §8 Q6) — wantCount pins the multi-mint.
// Row 13 (the D-34 retro path) binds conditionally and its route does not
// exist at 49abf65; W2-7's walk is the standing tripwire.
// (WINDOW-BUILD-PLAN-v1 §3 W2, criteria LOCKED 2026-08-28.)
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// w2Surface binds one takes-input handler body to a buildable fixture.
// build returns a ready server plus the base form values of a VALID POST
// for that body (no time fields yet — the arms add or omit those), the
// payload types the entered value must land on (TP §4.8's entered-value
// column for that row), and how many events of each such type the POST
// must mint (0 = at least one).
type w2Surface struct {
	route     string
	build     func(t *testing.T) (*Server, *store.Store, url.Values)
	wantTypes []string
	wantCount map[string]int
}

// --- the nine fixtures --------------------------------------------------

// Row 1a — POST /gate/checkin, NEW body shape (passenger_list=1 →
// handleGateCheckIn, gate.go:1041).
func w2BuildGateCheckInNew(t *testing.T) (*Server, *store.Store, url.Values) {
	t.Helper()
	srv, st, _ := newW2Server(t, w2Base-2*w2Hour)
	_, vals := gateBuild(t, srv, gateInbound, "TRK-14", []string{"Dana Driver", "Pat Passenger"}, 0)
	return srv, st, vals
}

// Row 1b — POST /gate/checkin, LEGACY shape (no passenger_list marker →
// legacyGateCheckIn, gate.go:1351 — the body seam S-1 deliberately keeps
// live: a legacy-shape POST that carries time_source=entered CAPTURES the
// override end-to-end; it is a capturing takes-input surface, not a
// tolerated fossil).
func w2BuildGateCheckInLegacy(t *testing.T) (*Server, *store.Store, url.Values) {
	t.Helper()
	srv, st, _ := newW2Server(t, w2Base-2*w2Hour)
	return srv, st, w2LegacyCheckInBody("T-LEGACY", "Dana Driver", "Pat Passenger")
}

// Row 1c ⟦R-25, 2026-09-13⟧ — POST /gate/checkin/single
// (handleGateCheckInSingle, gate.go), the SECOND inbound check-in door: its
// own served <form>, rendered on a fresh page with just a name typed into
// it, exactly the "no vehicle id" rhythm R-25 offers a one-click commit
// for. singleCheckInFormValues (gate_checkin_single_test.go) is
// gateFormValues' own scrape, over THIS form's markup rather than
// vehicle-form-in's. Reuses "Chris Crew" (a fresh newW2Server per call, so
// no cross-fixture collision with row 2's own use of the same name).
//
// ⟦R-28 revision 2, team-lead, 2026-09-16⟧ item 1: RETARGETED from getGate
// (GET /gate/vehicle) to getGateIndividual (GET /gate) — this form's own
// vehicle-page copy is gone ("individual mode already does that"); it now
// lives only on gate.html's own top CHECK IN section, which posts to this
// SAME route (see w2_served_forms_test.go's census, amended the same way).
func w2BuildGateCheckInSingle(t *testing.T) (*Server, *store.Store, url.Values) {
	t.Helper()
	srv, st, _ := newW2Server(t, w2Base-2*w2Hour)
	_, page := getGateIndividual(t, srv)
	vals := singleCheckInFormValues(t, page)
	vals.Set("new_last", "Chris Crew")
	vals.Set("new_first", "")
	return srv, st, vals
}

// Row 2 — POST /gate/checkout/vehicle (handleGateVehicleCheckOut).
func w2BuildGateVehicleCheckOut(t *testing.T) (*Server, *store.Store, url.Values) {
	t.Helper()
	srv, st, persons := newW2Server(t, w2Base-2*w2Hour)
	st.Clock = w2Reading(w2Base-w2Hour, store.ClockSynced)
	w2CheckIn(t, st, persons)
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	// ⟦R-28, team-lead, 2026-09-16⟧ RETARGETED: the departing form is
	// reached via the roster's own tick-and-select door now, not present
	// on a fresh page — see gateBuildOutbound's own comment.
	_, vals := gateBuildOutbound(t, srv, "TRK-14", []string{visitIDForCrewName(t, st, "Dana Driver")}, []string{"Chris Crew"}, 0)
	vals.Set("destination_id", "conneco")
	return srv, st, vals
}

// Row 3 — POST /gate/checkout (handleGateCheckOut): the ticked list; TWO
// visits ticked so the one-input-to-every-minted-event rule is observable.
func w2BuildGateCheckOut(t *testing.T) (*Server, *store.Store, url.Values) {
	t.Helper()
	srv, st, persons := newW2Server(t, w2Base-2*w2Hour)
	st.Clock = w2Reading(w2Base-w2Hour, store.ClockSynced)
	w2CheckIn(t, st, persons[:2])
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("fixture: %d open visits, want 2", len(open))
	}
	vals := url.Values{}
	for _, v := range open {
		vals.Add("visit_ids", v.VisitID)
	}
	return srv, st, vals
}

// Row 4 — POST /gate/resume (handleGateResume): the dispatch half only, as
// a process death between GateCheckIn's two Author calls leaves it.
func w2BuildGateResume(t *testing.T) (*Server, *store.Store, url.Values) {
	t.Helper()
	srv, st, persons := newW2Server(t, w2Base-2*w2Hour)
	st.Clock = w2Reading(w2Base-w2Hour, store.ClockSynced)
	trip, err := domain.DispatchLocalCheckIn(st, "site-a", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "T99", DriverPersonID: persons[0].PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "T99"), DriverPersonID: persons[0].PersonID,
		OccupantPersonIDs: []string{persons[1].PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	return srv, st, url.Values{"trip_id": {trip.TripID}}
}

// Row 5 — POST /trips/dispatch (handleDispatch).
func w2BuildTripsDispatch(t *testing.T) (*Server, *store.Store, url.Values) {
	t.Helper()
	srv, st, persons := newW2Server(t, w2Base-2*w2Hour)
	st.Clock = w2Reading(w2Base-w2Hour, store.ClockSynced)
	w2CheckIn(t, st, persons[:2])
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	return srv, st, url.Values{
		"vehicle":             {"BUS 7"},
		"driver_person_id":    {persons[0].PersonID},
		"occupant_person_ids": {persons[1].PersonID},
		"destination_id":      {"site-b"},
		"deadline_minutes":    {"60"},
	}
}

// Row 6 — POST /trips/arrive (handleExpectedArrival): an open inbound
// expectation authored by the peer's stream (the sanctioned foreign-stream
// injection, trips_test.go's shape).
func w2BuildTripsArrive(t *testing.T) (*Server, *store.Store, url.Values) {
	t.Helper()
	srv, st, _ := newW2Server(t, w2Base-2*w2Hour)
	st.Clock = w2Reading(w2Base-w2Hour, store.ClockSynced)
	p, err := domain.CreatePerson(st, "site-b", 1, "Nadeau, Pierre", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	dest, dl := "site-a", int64(90)
	trip, err := domain.Dispatch(st, "site-b", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "TRK-100", DriverPersonID: p.PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-b", "TRK-100"), DriverPersonID: p.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &dl,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	return srv, st, url.Values{"trip_id": {trip.TripID}}
}

// James, 2026-10-03 — POST /trips/returned (handleReturnedToOrigin): a
// turned-back truck, recorded back at its ORIGIN (this server, site-a) —
// so the trip is site-a's own, sent toward site-b.
func w2BuildTripsReturned(t *testing.T) (*Server, *store.Store, url.Values) {
	t.Helper()
	srv, st, _ := newW2Server(t, w2Base-2*w2Hour)
	st.Clock = w2Reading(w2Base-w2Hour, store.ClockSynced)
	p, err := domain.CreatePerson(st, "site-a", 1, "Turner, Bea", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	dest, dl := "site-b", int64(90)
	trip, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-RTN"), DriverPersonID: p.PersonID,
		DestinationSite: &dest, DeadlineMinutes: &dl,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	return srv, st, url.Values{"trip_id": {trip.TripID}}
}

// Row 7 — POST /trips/arrive-unsolicited (handleUnsolicitedArrival).
func w2BuildTripsArriveUnsolicited(t *testing.T) (*Server, *store.Store, url.Values) {
	t.Helper()
	srv, st, persons := newW2Server(t, w2Base-2*w2Hour)
	return srv, st, url.Values{
		"vehicle":     {"MYSTERY 2"},
		"origin_site": {"site-b"},
		"person_ids":  {persons[0].PersonID},
	}
}

// Row 8 — POST /trips/cancel: an open away trip. ⟦D-78⟧ The route is
// CLOSED; the fixture is kept so the inverted test below can drive the old
// body at the old path and prove it authors nothing.
func w2BuildTripsCancel(t *testing.T) (*Server, *store.Store, url.Values) {
	t.Helper()
	srv, st, persons := newW2Server(t, w2Base-2*w2Hour)
	st.Clock = w2Reading(w2Base-w2Hour, store.ClockSynced)
	w2CheckIn(t, st, persons[:2])
	dest, dl := "site-b", int64(60)
	trip, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "AWAY 1", DriverPersonID: persons[0].PersonID,"⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "AWAY 1"), DriverPersonID: persons[0].PersonID,
		DestinationSite: &dest, DeadlineMinutes: &dl,
	}, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	return srv, st, url.Values{"trip_id": {trip.TripID}}
}

// --- the three arms -----------------------------------------------------

// w2OverrideArm: an entered override captures end-to-end on this body.
func w2OverrideArm(t *testing.T, s w2Surface) {
	srv, st, form := s.build(t)
	before := w2Watermark(t, st, "site-a", 1)
	enteredMS := w2Base - 30*w2Minute
	wall := w2Wall(enteredMS)
	want := w2ParseWall(t, wall)
	form.Set(w2TimeSourceField, "entered")
	form.Set(w2OccurredAtField, wall)
	rec, _ := w2PostForm(t, srv, s.route, form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("W2-6 %s override: POST = %d, want 303 (an in-bounds entered value warns at most, never blocks): %s", s.route, rec.Code, rec.Body.String())
	}
	minted := w2EventsFrom(t, st, "site-a", 1, before)
	for _, typ := range s.wantTypes {
		evs := w2EventsOfType(t, minted, typ)
		if len(evs) == 0 {
			t.Fatalf("W2-6 %s override: the POST minted no %s event", s.route, typ)
		}
		if want := s.wantCount[typ]; want != 0 && len(evs) != want {
			t.Fatalf("W2-6 %s override: %d %s events minted, want %d — the single time input applies to EVERY event this POST mints (TP §4.8 row 3)", s.route, len(evs), typ, want)
		}
		for _, e := range evs {
			if e.TimeSource != "entered" {
				t.Fatalf("W2-6 %s override: stored %s carries time_source=%q, want \"entered\" — the posted signal never reached the stored row (mutant m12: this body still passes the literal 0, \"default\"; this is 49abf65's shipped defect)", s.route, typ, e.TimeSource)
			}
			if e.OccurredAt != want {
				t.Fatalf("W2-6 %s override: stored %s OccurredAt=%d, want the posted value's parse %d (%q) — an entered event whose value is not the typed value is the half-wire (0, postedTimeSource) (verdict R-3)", s.route, typ, e.OccurredAt, want, wall)
			}
		}
	}
}

// w2UntouchedArm: W2-1 per body, in the dwell shape — the echoed value
// differs from AuthoredAt by form dwell and is never stored.
func w2UntouchedArm(t *testing.T, s w2Surface) {
	srv, st, form := s.build(t)
	before := w2Watermark(t, st, "site-a", 1)
	echoMS := w2Base - 3*w2Minute // render-instant echo, 3 min of dwell ago
	form.Set(w2TimeSourceField, "default")
	form.Set(w2OccurredAtField, w2Wall(echoMS))
	rec, _ := w2PostForm(t, srv, s.route, form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("W2-6 %s untouched: POST = %d, want 303: %s", s.route, rec.Code, rec.Body.String())
	}
	minted := w2EventsFrom(t, st, "site-a", 1, before)
	echoParsed := w2ParseWall(t, w2Wall(echoMS))
	for _, typ := range s.wantTypes {
		evs := w2EventsOfType(t, minted, typ)
		if len(evs) == 0 {
			t.Fatalf("W2-6 %s untouched: the POST minted no %s event", s.route, typ)
		}
		for _, e := range evs {
			if e.TimeSource != "default" {
				t.Fatalf("W2-1/W2-6 %s untouched: stored %s carries time_source=%q, want \"default\" — an untouched form forwarded as entered is mutant m7 (and dwell-derived branding is m3/mTP-4)", s.route, typ, e.TimeSource)
			}
			if e.OccurredAt != e.AuthoredAt.WallMS {
				t.Fatalf("W2-1/W2-6 %s untouched: rule-W equality broken on %s: OccurredAt=%d != AuthoredAt.WallMS=%d — the equality is exact, never a tolerance", s.route, typ, e.OccurredAt, e.AuthoredAt.WallMS)
			}
			if e.OccurredAt == echoParsed {
				t.Fatalf("W2-1/W2-6 %s untouched: stored %s OccurredAt equals the echoed render instant %d — the echoed value must NEVER be stored (TP §4.5)", s.route, typ, echoParsed)
			}
		}
	}
}

// w2SignalLessArm: W2-9 per body — no time_source field at all is refused
// 400 and authors nothing; the server never infers the missing signal.
func w2SignalLessArm(t *testing.T, s w2Surface) {
	srv, st, form := s.build(t)
	form.Del(w2TimeSourceField)
	form.Del(w2OccurredAtField)
	before := w2Watermark(t, st, "site-a", 1)
	rec, _ := w2PostForm(t, srv, s.route, form)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("W2-9 %s signal-less: POST with no %s field = %d, want 400 — the natural fallback (infer the missing signal from the values) is the forbidden derivation; the server refuses, never infers (TP §4.5; mutant mTP-3)", s.route, w2TimeSourceField, rec.Code)
	}
	if after := w2Watermark(t, st, "site-a", 1); after != before {
		t.Fatalf("W2-9 %s signal-less: the refused POST authored %d event(s) (watermark %d -> %d) — a 400 that writes is not a refusal", s.route, after-before, before, after)
	}
}

// --- the nine wiring tests ---------------------------------------------

func w2RunSurface(t *testing.T, s w2Surface) {
	t.Helper()
	t.Run("override", func(t *testing.T) { w2OverrideArm(t, s) })
	t.Run("untouched", func(t *testing.T) { w2UntouchedArm(t, s) })
	t.Run("signal-less", func(t *testing.T) { w2SignalLessArm(t, s) })
}

func TestW2WiringGateCheckInNewBody(t *testing.T) {
	w2RunSurface(t, w2Surface{
		route:     "/gate/checkin",
		build:     w2BuildGateCheckInNew,
		wantTypes: []string{"trip_dispatched", "trip_arrival"}, // the arrival pair — one physical event, one instant (TP §4.8 row 1)
	})
}

func TestW2WiringGateCheckInLegacyBody(t *testing.T) {
	w2RunSurface(t, w2Surface{
		route:     "/gate/checkin",
		build:     w2BuildGateCheckInLegacy,
		wantTypes: []string{"trip_dispatched", "trip_arrival"},
	})
}

// ⟦R-25, 2026-09-13⟧ the SAME arrival pair as row 1, reached the SECOND
// way — see w2BuildGateCheckInSingle.
func TestW2WiringGateCheckInSingle(t *testing.T) {
	w2RunSurface(t, w2Surface{
		route:     "/gate/checkin/single",
		build:     w2BuildGateCheckInSingle,
		wantTypes: []string{"trip_dispatched", "trip_arrival"},
	})
}

func TestW2WiringGateVehicleCheckOut(t *testing.T) {
	w2RunSurface(t, w2Surface{
		route:     "/gate/checkout/vehicle",
		build:     w2BuildGateVehicleCheckOut,
		wantTypes: []string{"trip_dispatched"}, // the departure's OccurredAt (TP §4.8 row 2)
	})
}

func TestW2WiringGateCheckOut(t *testing.T) {
	w2RunSurface(t, w2Surface{
		route:     "/gate/checkout",
		build:     w2BuildGateCheckOut,
		wantTypes: []string{"visit_checked_out"},
		wantCount: map[string]int{"visit_checked_out": 2}, // row 3: one input, EVERY minted visit_checked_out
	})
}

func TestW2WiringGateResume(t *testing.T) {
	w2RunSurface(t, w2Surface{
		route:     "/gate/resume",
		build:     w2BuildGateResume,
		wantTypes: []string{"trip_arrival"},
	})
}

func TestW2WiringTripsDispatch(t *testing.T) {
	w2RunSurface(t, w2Surface{
		route:     "/trips/dispatch",
		build:     w2BuildTripsDispatch,
		wantTypes: []string{"trip_dispatched"},
	})
}

func TestW2WiringTripsArrive(t *testing.T) {
	w2RunSurface(t, w2Surface{
		route:     "/trips/arrive",
		build:     w2BuildTripsArrive,
		wantTypes: []string{"trip_arrival"},
	})
}

// James, 2026-10-03: the turned-back truck's door captures time exactly as
// /trips/arrive does (captureTimeOrRefuse -> the trip_arrival envelope).
func TestW2WiringTripsReturned(t *testing.T) {
	w2RunSurface(t, w2Surface{
		route:     "/trips/returned",
		build:     w2BuildTripsReturned,
		wantTypes: []string{"trip_arrival"},
	})
}

func TestW2WiringTripsArriveUnsolicited(t *testing.T) {
	w2RunSurface(t, w2Surface{
		route:     "/trips/arrive-unsolicited",
		build:     w2BuildTripsArriveUnsolicited,
		wantTypes: []string{"trip_arrival"},
	})
}

// TestW2WiringTripsCancel — ⟦D-78, 2026-09-05⟧ ROW 8 IS CLOSED, and this
// test is INVERTED rather than deleted or weakened.
// ⟦was: w2RunSurface(t, w2Surface{route: "/trips/cancel", build: w2BuildTripsCancel, wantTypes: []string{"trip_cancelled"}})⟧
// D-73: no trip is cancelled. D-78 closed POST /trips/cancel. The three
// arms' positive — "this POST mints trip_cancelled carrying the posted
// signal" — cannot hold for a route that does not exist, so the SAME three
// bodies (override / untouched / signal-less) are driven at the SAME path,
// and each must author NOTHING and leave the trip open. The name is kept:
// the route-walk table cited it while the row existed, and a reader of that
// table's strike note can still find the proof.
func TestW2WiringTripsCancel(t *testing.T) {
	arms := []struct {
		name  string
		shape func(url.Values)
	}{
		{"override", func(f url.Values) {
			f.Set(w2TimeSourceField, "entered")
			f.Set(w2OccurredAtField, w2Wall(w2Base-30*w2Minute))
		}},
		{"untouched", func(f url.Values) {
			f.Set(w2TimeSourceField, "default")
			f.Set(w2OccurredAtField, w2Wall(w2Base-3*w2Minute))
		}},
		{"signal-less", func(f url.Values) {
			f.Del(w2TimeSourceField)
			f.Del(w2OccurredAtField)
		}},
	}
	for _, arm := range arms {
		t.Run(arm.name, func(t *testing.T) {
			srv, st, form := w2BuildTripsCancel(t)
			arm.shape(form)
			before := w2Watermark(t, st, "site-a", 1)
			rec, _ := w2PostForm(t, srv, "/trips/cancel", form)
			if after := w2Watermark(t, st, "site-a", 1); after != before {
				t.Fatalf("D-78 /trips/cancel %s: the CLOSED route authored %d event(s) (watermark %d -> %d; response %d) — no officer-facing path may reach domain.CancelTrip: %s",
					arm.name, after-before, before, after, rec.Code, rec.Body.String())
			}
			state, err := domain.TripStateByID(st, form.Get("trip_id"))
			if err != nil {
				t.Fatal(err)
			}
			if state == nil || state.State != domain.TripStateOpen {
				t.Fatalf("D-78 /trips/cancel %s: after the old POST the trip reads %v, want %s", arm.name, state, domain.TripStateOpen)
			}
		})
	}
}

// --- W2-2: the deep parse chain, once, on the new gate check-in ---------

// TestW2EnteredValueByteExactThroughParse — W2-2: site-local wall string
// in, parsed UTC ms stored, identical on read-back. The per-surface value
// equality is the override arms above; this is the ONE deep fixture
// (criterion rev-2, verdict R-12(b)), on POST /gate/checkin new body shape,
// with an odd-minute value so a seconds-truncation or zone slip cannot
// round to a false pass. Kills m8 (the typed value dropped) and m2 (the
// clamp).
func TestW2EnteredValueByteExactThroughParse(t *testing.T) {
	srv, st, form := w2BuildGateCheckInNew(t)
	before := w2Watermark(t, st, "site-a", 1)
	enteredMS := w2Base - 17*w2Minute
	wall := w2Wall(enteredMS)
	want := w2ParseWall(t, wall)
	form.Set(w2TimeSourceField, "entered")
	form.Set(w2OccurredAtField, wall)
	rec, _ := w2PostForm(t, srv, "/gate/checkin", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("W2-2: entered check-in = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	// Read back TWICE — once now, once through a fresh Events call — and
	// assert the stored value is the parse EXACTLY both times.
	for pass := 0; pass < 2; pass++ {
		minted := w2EventsFrom(t, st, "site-a", 1, before)
		for _, typ := range []string{"trip_dispatched", "trip_arrival"} {
			evs := w2EventsOfType(t, minted, typ)
			if len(evs) != 1 {
				t.Fatalf("W2-2: %d %s events, want 1", len(evs), typ)
			}
			e := evs[0]
			if e.TimeSource != "entered" {
				t.Fatalf("W2-2: stored %s time_source=%q, want \"entered\" (mutant m8: handler-layer discard)", typ, e.TimeSource)
			}
			if e.OccurredAt != want {
				t.Fatalf("W2-2: stored %s OccurredAt=%d, want %d — the parse of the typed wall string %q, byte-exact, no clamp and no re-derivation (mutants m8/m2)", typ, e.OccurredAt, want, wall)
			}
		}
	}
}

// --- W2-9's deep fixture: the pinned 400 on the legacy shape ------------

// TestW2MissingSignalLegacy400PreservesPostedState — W2-9's deep fixture
// (the measured stale-pre-bump body, seam S-1) with the response PINNED:
// the 400 re-renders the form with every posted field and the typed values
// intact (TP §4.6 rule 6; gate.go:1009–1012's doctrine — the refused
// traffic is an officer's stale pre-bump tab holding a typed passenger
// list, and a bare http.Error wall would convert the bump itself into lost
// records). W2-5's second application; mutant m11's second application
// kills the state-discarding shape.
func TestW2MissingSignalLegacy400PreservesPostedState(t *testing.T) {
	srv, st, _ := newW2Server(t, w2Base-2*w2Hour)
	form := w2LegacyCheckInBody("T-STALE-TAB", "Dana Driver", "Pat Passenger", "Chris Crew")
	before := w2Watermark(t, st, "site-a", 1)
	rec, _ := w2PostForm(t, srv, "/gate/checkin", form)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("W2-9 deep: signal-less legacy POST = %d, want 400 (TP §4.5; seam S-1's recorded default: TP §4.5 wins, narrowly)", rec.Code)
	}
	if after := w2Watermark(t, st, "site-a", 1); after != before {
		t.Fatalf("W2-9 deep: the refused legacy POST authored %d event(s)", after-before)
	}
	body := w2StripNonMarkup(rec.Body.String())
	for _, typed := range []string{"T-STALE-TAB", "Dana Driver", "Pat Passenger", "Chris Crew"} {
		if !strings.Contains(body, typed) {
			t.Fatalf("W2-9 deep: the 400 response does not carry the posted %q — a refusal that discards the officer's typed work converts the bump into lost records (mutant m11, second application). Body:\n%s", typed, body)
		}
	}
}
