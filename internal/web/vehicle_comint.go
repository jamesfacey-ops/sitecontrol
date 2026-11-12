// vehicle_comint.go — ⟦W7-4(d)(i)⟧ the web layer's ONE co-mint of a vehicle
// record from a typed number, and the ONE partition of what can go wrong with
// it.
//
// ⚠⚠ WHY THIS FILE EXISTS AT ALL. DR-W7-4 rules that `DispatchInput`,
// `GateCheckInInput` and `RecordUnsolicitedArrival` take a vehicle_id and
// never a free-text string, and the guarantee that buys is "the typing moment
// IS creation". The three forms behind those doors still have a text box, so
// SOMETHING has to turn the typed number into a record — and where that
// happens decides whether the guarantee is real. It happens HERE, at the
// surface, before the door: by the time the domain is called the number has
// already become a record, the placeholder list has already been consulted at
// the one place that consults it, and the door is given an id.
//
// ⚠ WHAT THIS IS NOT: it is NOT the vehicle PICKER. W7's plan names a
// selection control — "a thing the system knows, selected rather than
// retyped" ([[D-12]]) — with `vehicleFoldKey` ranking its search, and that
// control does not exist. An officer still types, and a typo still mints a
// second record rather than being offered the first. What this file buys is
// that the typo is now a RECORD an officer can amend and merge (W7's whole
// item 9), instead of a string frozen into a trip forever. The picker remains
// owed; see reports/window-build/W7-DOORS.md.
package web

import (
	"errors"
	"log"
	"net/http"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// vehicleIDForTypedNumber resolves the number an officer typed to a vehicle
// record id, minting the record if this post holds none.
//
// It returns an EMPTY id and NO refusal for an empty box: the "names an
// entity" refusal lives at the authoring door, where W7-4(b) re-points it at
// the id and preserves its literal byte-for-byte, so an officer who dispatches
// with nothing typed meets the same sentence they always have — from the one
// place that owns that rule.
//
// On failure it returns the officer-facing sentence and the status it should
// carry, NEVER a raw error value. The two classes are told apart BY TYPE:
//
//   - a *domain.VehicleCreateRefusal is a REFUSAL — the officer typed
//     something the system will not accept as a vehicle number (the relocated
//     38-member placeholder list, or a record that identifies nothing). 409,
//     and the DOMAIN'S OWN authored sentence, because that sentence was
//     written for an officer to read and re-wording it here would give two
//     surfaces two answers to the same refusal.
//   - anything else is a FAULT — the store could not be read or written. 500,
//     authored copy, and the error goes to the LOG. A fault message is for the
//     person who reads the log, not the person at the gate.
//
// st is the store to resolve and mint THROUGH ⟦A3, 2026-09-29⟧: the gate
// doors call this from inside a store.Transact and must hand it the view
// they were given, never s.Store — a read or write on the pool from inside
// the commit waits forever on the store's one connection (transact.go). The
// console doors, which do not (yet) commit transactionally, pass s.Store.
func (s *Server) vehicleIDForTypedNumber(st *store.Store, number string) (id string, refusal string, status int) {
	vehicleID, err := domain.ResolveOrCreateVehicleByNumber(st, s.Cfg.SiteID, s.Cfg.NodeEpoch, number)
	if err == nil {
		return vehicleID, "", http.StatusOK
	}
	var createRefusal *domain.VehicleCreateRefusal
	if errors.As(err, &createRefusal) {
		return "", createRefusal.Reason, http.StatusConflict
	}
	log.Printf("vehicle co-mint: %v", err)
	return "", "Nothing has been recorded and the entries below are intact. This post could not read or create the vehicle record — try again, and if it happens twice radio it through.", http.StatusInternalServerError
}

// ⟦RETIRED — SchemaVersion 13, R-17, 2026-09-11⟧ vehicleUnknownRole /
// vehicleIDForUnknown lived here: a "Vehicle unknown" role-only vehicle
// record, minted FRESH per unidentified departure, never written to Number
// (so it could not become the "walk-in" collision class §9.2's correlation
// key bans). It worked, and it is gone — not because it was wrong, but
// because R-17's schema path (trip.vehicle_unidentified, store.go) removes
// the need to mint anything at all: the fact "no vehicle was identified"
// now lives directly on the trip, never on a manufactured vehicle record,
// which is also what stops the admin vehicle-merge screen
// (vehicle_amend.go, domain.Vehicles()' one non-test caller) accumulating
// one indistinguishable "Vehicle unknown" row per unidentified departure
// forever. This was their only call site (handleGateVehicleCheckOut); see
// gate.go's own comment at that call site for what replaced it.
