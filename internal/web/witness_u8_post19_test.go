// witness_u8_post19_test.go — witness phase, u8-post19 assignment
// (reports/witness/u8-post19.md). Kills the post-19eafc4 census survivor on
// the unsolicited-arrival path (reports/witness/u2-census-rerun.md,
// post19_unreached.tsv):
//
//	internal/web/trips.go:1238-1240  Server.handleUnsolicitedArrival  `if vehErr != "" {`
//
// An unsolicited arrival is a vehicle reaching a post nobody dispatched —
// the accountability case this whole system exists for — and nothing in
// the suite exercised what an officer sees when the vehicle number they
// typed is itself refused. See internal/domain/witness_u8_post19_test.go
// for this path's other two unreached blocks, one level down in
// RecordUnsolicitedArrival.
package web

import (
	"net/http"
	"net/url"
	"testing"

	"sitecontrol/internal/domain"
)

// TestWitnessU8HandleUnsolicitedArrivalPlaceholderVehicleReturnsCleanRefusal
// kills trips.go:1238-1240. A placeholder vehicle number ("walk-in") is
// refused by the domain's vehicle co-mint (vehicle_comint.go), which hands
// the web layer a clean officer-facing sentence via vehErr/vehStatus. The
// handler's job at this arm is to put THAT sentence on the wire verbatim —
// not the domain error's wrapped text ("domain: unsolicited arrival:
// vehicle: ..."), which is what an officer would see if this arm were
// deleted and every error fell through to refuse(w, err) instead.
//
// ⛔ Both paths answer 409 — VehicleCreateRefusal and refuse() agree on the
// status — so a status-only assertion proves nothing here (the same trap
// the phase hit at legacyGateCheckIn). The assertion has to be on the body,
// and it is whole-body EQUALITY: this handler renders through bare
// http.Error with no template, so the body genuinely IS the notice, and a
// Contains check would stay green under a mutation that wraps this sentence
// inside a bigger one.
func TestWitnessU8HandleUnsolicitedArrivalPlaceholderVehicleReturnsCleanRefusal(t *testing.T) {
	srv, st, persons := newTripsTestServer(t)

	rec := postForm(t, srv, "/trips/arrive-unsolicited", url.Values{
		"time_source": {"default"},
		"vehicle":     {"walk-in"},
		"origin_site": {"site-a"},
		"person_ids":  {persons[0].PersonID, persons[1].PersonID},
	})

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (VehicleCreateRefusal is a 409): body=%s",
			rec.Code, http.StatusConflict, rec.Body.String())
	}
	wantBody := `"walk-in" does not identify a vehicle. Enter the plate or unit number of the vehicle these people arrived in — ` +
		`it is what links them to that vehicle on the muster, and an evacuation reads it. ` +
		`(Everyone arrives in a vehicle at this post; there is no placeholder for 'no vehicle'.)` + "\n"
	if got := rec.Body.String(); got != wantBody {
		t.Fatalf("body = %q\nwant the CLEAN vehErr sentence %q\n"+
			"— a domain-wrapped body here means the vehErr arm (trips.go:1238) was bypassed for refuse(w, err)",
			got, wantBody)
	}

	// Identity, not just the response: the refused co-mint attempt must
	// leave nothing behind — no arrival, and no vehicle record (the
	// tripauthor.go comment's own invariant: "a refused arrival must leave
	// no vehicle record behind").
	arrs, err := domain.Arrivals(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(arrs) != 0 {
		t.Fatalf("refused arrival authored %d trip_arrival event(s), want 0: %+v", len(arrs), arrs)
	}
	vs, err := domain.Vehicles(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Fatalf("refused arrival left %d vehicle record(s) behind, want 0: %+v", len(vs), vs)
	}
}
