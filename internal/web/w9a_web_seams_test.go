// w9a_web_seams_test.go — W9a (item 15) shared machinery for the web-layer
// criteria. NO ASSERTIONS LIVE HERE.
//
// The design decision every W9a file rests on is stated in full in
// internal/domain/w9a_seams_test.go: no W9a test names an unbuilt Go symbol,
// so amendments are authored as HAND-BUILT ENVELOPES carrying F4 §3.1's
// frozen wire JSON and applied through the real store.Apply, and every
// assertion is made at a surface or reader that exists TODAY. Every W9a web
// test therefore COMPILES AND RUNS against the pre-chunk tree and reds at
// RUNTIME naming its criterion.
//
// SEAM W9A-S-FORM, bound HERE, once. The criteria name the ROUTES
// (`POST /trips/amend`, `POST /arrivals/amend`) but no document names the
// FORM FIELDS. They are bound to the frozen PAYLOAD KEYS of F4 §3.1, which
// is the convention `person_amend`/`company_amend` already follow, plus one
// field the payload has no counterpart for: §7's explicit confirm. A builder
// who spells them otherwise takes it to the GATE rather than editing this
// file — the same disposition `w5d_sidetable_structural_test.go`'s SEAM
// W5D-S-COLS records.
package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// --- routes (F4 §3.5, §3.6, §9.4) ---------------------------------------

const (
	w9aTripAmendPath    = "/trips/amend"
	w9aArrivalAmendPath = "/arrivals/amend"
	// W9b's route. Named here because W9a-15's fixture authors it and the
	// criterion says so in terms; W9a builds nothing behind it.
	w9aArrivalUnrefPath = "/arrivals/unref"
)

// --- SEAM W9A-S-FORM ----------------------------------------------------

const (
	w9aFieldTripID          = "trip_id"
	w9aFieldArrivalID       = "arrival_id"
	w9aFieldDriver          = "driver_person_id"
	w9aFieldVehicle         = "vehicle_entered"
	w9aFieldDestKind        = "destination_kind"
	w9aFieldDestValue       = "destination_value"
	w9aFieldDeadlineMinutes = "deadline_minutes"
	w9aFieldCargoNote       = "cargo_note"
	// ⟦was: w9aFieldOverrideReason = "phase_override_reason" — weather phases removed, James 2026-10-04⟧
	w9aFieldOccupantsAdd    = "occupants_add"
	w9aFieldOccupantsRemove = "occupants_remove"
	w9aFieldClear           = "clear"
	w9aFieldOriginSite      = "origin_site"
	// §7's explicit confirm past the unsighted-removal warning. It has no
	// payload counterpart by design: the warning is an AUTHORING-time
	// affordance and nothing about it is replicated.
	w9aFieldConfirmUnsighted = "confirm_unsighted" // ⟦D-80⟧ RETIRED in production; kept here so tests can assert it is ABSENT and IGNORED
)

// --- frozen wire spellings (F4 §3.1) ------------------------------------

const (
	w9aEvtTripAmended    = "trip_amended"
	w9aEvtArrivalAmended = "arrival_amended"
)

const w9aMinute = int64(60_000)

func w9aStr(s string) *string { return &s }
func w9aI64(v int64) *int64   { return &v }

// --- the wire path ------------------------------------------------------

type w9aDestination struct {
	Kind            string
	Value           string
	DeadlineMinutes *int64
}

type w9aTripAmend struct {
	TripID          string
	Driver          *string
	Vehicle         *string
	Destination     *w9aDestination
	CargoNote       *string
	OccupantsAdd    []string
	OccupantsRemove []string
	Clear           []string
}

func (a w9aTripAmend) payload() map[string]any {
	m := map[string]any{"type": w9aEvtTripAmended, "trip_id": a.TripID}
	if a.Driver != nil {
		m["driver_person_id"] = *a.Driver
	}
	if a.Vehicle != nil {
		m["vehicle_entered"] = *a.Vehicle
	}
	if a.Destination != nil {
		d := map[string]any{"kind": a.Destination.Kind, "value": a.Destination.Value}
		if a.Destination.DeadlineMinutes != nil {
			d["deadline_minutes"] = *a.Destination.DeadlineMinutes
		}
		m["destination"] = d
	}
	if a.CargoNote != nil {
		m["cargo_note"] = *a.CargoNote
	}
	if len(a.OccupantsAdd) > 0 {
		m["occupants_add"] = a.OccupantsAdd
	}
	if len(a.OccupantsRemove) > 0 {
		m["occupants_remove"] = a.OccupantsRemove
	}
	if len(a.Clear) > 0 {
		m["clear"] = a.Clear
	}
	return m
}

func w9aEnvelope(t *testing.T, siteID string, epoch, seq, wallMS int64, payload map[string]any) store.Envelope {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return store.Envelope{
		SiteID: siteID, NodeEpoch: epoch, Seq: seq,
		SchemaVersion: store.SchemaVersion, OriginAuth: siteID,
		AuthoredAt: store.AuthoredAt{WallMS: wallMS, MaxErrorUS: 42, State: store.ClockSynced},
		OccurredAt: wallMS, TimeSource: "default", Payload: raw,
	}
}

// w9aApplyNext applies a hand-built envelope at the stream's next free seq
// and returns it, so a caller can replicate the same bytes to a peer.
func w9aApplyNext(t *testing.T, st *store.Store, siteID string, epoch, wallMS int64, payload map[string]any) store.Envelope {
	t.Helper()
	wm, err := st.Watermark(siteID, epoch)
	if err != nil {
		t.Fatal(err)
	}
	env := w9aEnvelope(t, siteID, epoch, wm+1, wallMS, payload)
	applied, err := st.Apply(env)
	if err != nil {
		t.Fatalf("w9a: apply %v: %v — folds never refuse (F4 §3.2: the fold is a bare INSERT)", env.Key(), err)
	}
	if !applied {
		t.Fatalf("w9a: apply %v: deduped unexpectedly", env.Key())
	}
	return env
}

// w9aSeedPerson creates a roster row with a CHOSEN id, so the criteria's
// person literals are the ids on the wire and in the fact names.
func w9aSeedPerson(t *testing.T, st *store.Store, siteID string, epoch int64, personID, name string) {
	t.Helper()
	w9aApplyNext(t, st, siteID, epoch, w2Base, map[string]any{
		"type": "person_created", "person_id": personID, "name": name,
	})
}

// --- fixture worlds -----------------------------------------------------

// w9aConfigWithSiteC is w3ConfigSiteA plus a THIRD controlled destination —
// W9a-15's `site-c` — because the locked fixture moves a trip from site-b to
// site-c and a destination the config does not know is refused at authoring.
func w9aConfigWithSiteC(dbPath string) *config.Config {
	cfg := w3ConfigSiteA(dbPath)
	cfg.Destinations = append(cfg.Destinations,
		config.Destination{ID: "site-c", DisplayName: "Far Pad", Controlled: true})
	cfg.Peers = append(cfg.Peers, config.Peer{SiteID: "site-c", BaseURL: "http://example.invalid"})
	return cfg
}

// w9aOriginServer is the ORIGIN post (site-a, "Ice Road") — the parameter
// §4's third refusal rendering turns on. ⟦D-78⟧ ⟦was: "which is where a
// cancel is authorable"⟧ — no post can cancel any more; the origin is still
// the post the non-origin rendering NAMES.
func w9aOriginServer(t *testing.T, name string) (*Server, *store.Store) {
	t.Helper()
	st := w3OpenRegistered(t, name)
	return New(w9aConfigWithSiteC(st.Path), st), st
}

// w9aDestinationServer is the DESTINATION post (site-b, "Pad Site Control"),
// the non-origin post for site-a's trip. ⟦D-78⟧ ⟦was: "which cannot cancel
// site-a's trip"⟧
func w9aDestinationServer(t *testing.T, name string) (*Server, *store.Store) {
	t.Helper()
	st := w3OpenRegistered(t, name)
	return New(w3ConfigSiteB(st.Path), st), st
}

// w9aDispatchOpen dispatches a controlled trip from origin to destination.
func w9aDispatchOpen(t *testing.T, st *store.Store, origin, destination string, epoch int64, vehicle, driver string, occupants []string, deadlineMinutes int64) domain.Trip {
	t.Helper()
	dest := destination
	trip, err := domain.Dispatch(st, origin, epoch, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: vehicle, DriverPersonID: driver, OccupantPersonIDs: occupants,"⟧
		VehicleID: w7SeedVehicleID(t, st, origin, vehicle), DriverPersonID: driver, OccupantPersonIDs: occupants,
		DestinationSite: &dest, DeadlineMinutes: w9aI64(deadlineMinutes),
	}, 0, "default")
	if err != nil {
		t.Fatalf("w9a fixture: dispatch: %v", err)
	}
	return trip
}

// w9aDegenerateTrip authors §10's gate check-in trip — destination and
// deadline both nil. It is "vastly the most numerous trip shape in any
// picker" (F4 §3.5) and the fixture W9a-9's fourth way needs.
func w9aDegenerateTrip(t *testing.T, st *store.Store, siteID string, epoch int64, driver string, occupants []string) domain.Trip {
	t.Helper()
	trip, err := domain.DispatchLocalCheckIn(st, siteID, epoch, domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "BUS 4", DriverPersonID: driver, OccupantPersonIDs: occupants,"⟧
		VehicleID: w7SeedVehicleID(t, st, siteID, "BUS 4"), DriverPersonID: driver, OccupantPersonIDs: occupants,
	}, 0, "default")
	if err != nil {
		t.Fatalf("w9a fixture: degenerate check-in dispatch: %v", err)
	}
	if _, err := domain.RecordExpectedArrival(st, siteID, epoch, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("w9a fixture: degenerate check-in arrival: %v", err)
	}
	return trip
}

// --- posting to the amendment surfaces ----------------------------------

// w9aAmend posts one amendment and returns what the officer READS next (the
// POST's own body, or the page a 303 leads to) together with whether the
// stream's watermark moved — "nothing authored" is asserted on the LOG, never
// on a status code.
func w9aAmend(t *testing.T, srv *Server, st *store.Store, path string, form url.Values) (page string, authored bool) {
	t.Helper()
	before := w2Watermark(t, st, srv.Cfg.SiteID, srv.Cfg.NodeEpoch)
	rec, cookie := w2PostForm(t, srv, path, form)
	page = w2NextOperatorPage(t, srv, rec, cookie)
	after := w2Watermark(t, st, srv.Cfg.SiteID, srv.Cfg.NodeEpoch)
	return page, after != before
}

// w9aRouteExists reports whether the mux has a handler for path — used so a
// missing route reds with a sentence naming the criterion instead of with an
// unexplained 200 from the catch-all.
func w9aRouteExists(t *testing.T, srv *Server, method, path string) bool {
	t.Helper()
	_, pattern := srv.Handler().(*http.ServeMux).Handler(mustRequest(t, method, path))
	return pattern != "" && pattern != "GET /" && !strings.HasPrefix(pattern, "GET / ")
}

func mustRequest(t *testing.T, method, path string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, "http://example.invalid"+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}
