// vehicle_render.go — ⟦W7⟧ the web layer's ONE reading of a trip's frozen
// vehicle snapshot.
//
// ⚠⚠ THIS IS NOT YET W7-26's SHARED RENDER HELPER, and calling it that would
// be a false green. W7-26 asks for one helper at EVERY surface where a vehicle
// number would render, rendering the role-only case honestly from the COMPOSED
// record; DR-W7-18 rules which of those surfaces read the live record and
// which read the snapshot. What this file provides is the narrower thing the
// item-8 column change actually forced: one place where a nullable snapshot
// becomes display text, so that when the render helper lands there is a single
// site to replace rather than four.
package web

import "sitecontrol/internal/domain"

// vehicleSnapshotText reads trip.vehicle_entered — the number as it stood at
// dispatch — as display text.
//
// ⚠ An absent number renders as the EMPTY STRING and never as a stand-in
// wording. The empty string IS the absence (DR-W7-15) and a stand-in would be
// a magic string in the vehicle field, which [[D-11]] rules out in terms:
// "'No number available' must be a distinct recorded state." What an officer
// should SEE in that state is the render helper's business.
func vehicleSnapshotText(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// tripVehicleDisplay (SchemaVersion 13, R-17 — James: "there are no doors
// only vehicles") is the muster/trip-history reading of a trip's frozen
// vehicle, honest about the new unidentified fact alongside the existing
// snapshot. It distinguishes a movement that named NO vehicle at all
// (unidentified — this is the failure R-17 names: it used to render
// indistinguishably from vehicleSnapshotText's blank) from a named,
// role-only vehicle record with no number (unidentified=false,
// vehicleEntered nil — DR-W7-15/[[D-11]] STILL holds there: the empty
// string IS that absence, never a sentinel). Only the FIRST case is new;
// the second is vehicleSnapshotText, untouched, called from here so there
// is one function to update if a shared render helper (W7-26, still
// unbuilt) ever supersedes both. Mirrors domain.tripVehicleEnteredDisplay
// (tripauthor.go/trip.go); the two are not shared code only because they
// live in different packages reading different struct shapes.
func tripVehicleDisplay(unidentified bool, vehicleEntered *string) string {
	if unidentified {
		return domain.VehicleUnidentifiedText
	}
	return vehicleSnapshotText(vehicleEntered)
}
