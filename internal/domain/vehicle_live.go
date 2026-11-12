// vehicle_live.go — the ONE reading of "which truck is this open run RIGHT
// NOW", shared by every present-tense surface at every post.
//
// James, 2026-10-03: "the truck number and driver are not the same on the
// overdue truck between the ice road and ice pad." [MEASURED on lane L03]
// After a vehicle record was renumbered while its run was still out, the
// origin's muster (InTransitFrom) composed the number LIVE from the record
// and the destination's alert strip, inbound board, gate inbound row and
// /trips/detail read the frozen dispatch snapshot — one open run, two truck
// numbers, at the same instant. DR-W7-18 had already ruled all of those
// surfaces LIVE (W7-36(a)); only the in-transit block had been built that
// way. Every one of them now reads through LiveVehicles, so a post cannot
// compose the truck differently from another post holding the same events.
//
// ⚠ PAST-TENSE surfaces are untouched: MusterReprint's on-site roster, the
// Incomplete rows, closed trips on /trips/detail and the trip chain still
// read the frozen snapshot (DR-W7-18's FROZEN rows; analytics-v1.md:526–530).
package domain

import "sitecontrol/internal/store"

// LiveVehicles composes open runs' vehicles against one read of the vehicle
// merge map. Build one per page or scan (the map is derived per read, never
// cached between reads — item 9), then call Compose per trip.
type LiveVehicles struct {
	st    *store.Store
	merge AliasMap
	cache map[string]*Vehicle
}

// NewLiveVehicles reads the vehicle merge map once for a batch of Compose
// calls.
func NewLiveVehicles(st *store.Store) (*LiveVehicles, error) {
	merge, err := LoadVehicleMergeMap(st)
	if err != nil {
		return nil, err
	}
	return &LiveVehicles{st: st, merge: merge, cache: map[string]*Vehicle{}}, nil
}

func (l *LiveVehicles) vehicle(id string) (*Vehicle, error) {
	if v, ok := l.cache[id]; ok {
		return v, nil
	}
	v, err := VehicleByID(l.st, id)
	if err != nil {
		return nil, err
	}
	l.cache[id] = v
	return v, nil
}

// Compose returns the display text and the resolved identity of a composed
// trip's vehicle.
//
// ⟦W7-34(a)⟧ identity is WHICH TRUCK this run is, after merge resolution —
// what the muster headline counts. display is what an officer reads.
//
// ⚠ SchemaVersion 13 (R-17): an unidentified movement is handled FIRST — no
// record was named, so there is nothing to resolve and nothing recorded to
// fall back to. Its identity is the trip id, deliberately not "" and not
// shared across rows: two different unidentified trucks must not collapse
// onto one identity and undercount the trucks on the ice (D-6).
//
// ⚠ THE FALLBACK IS NOT A DEFAULT — it is what "which truck is this" means
// for a run whose vehicle record has not replicated here: the number as
// recorded, VERBATIM, both as display and identity. NormalizeVehicleID
// preserves punctuation, so normalising would not unify "TRK-100" and
// "TRK 100" anyway, and it would change a hook locked muster tests pin as a
// literal. STATED AS A LIMIT: two runs of one truck typed differently count
// as two here until both name the same record.
//
// ⚠ A truck with no number is shown by its ROLE and never by a stand-in:
// "no number available" is a distinct recorded state, never a magic string
// in the vehicle field ([[D-11]]).
//
// ⚠ A winning trip_amended VEHICLE assertion is displayed as asserted: it is
// a later human statement of which truck this run was, and it names no
// record, so the record behind VehicleID cannot speak for it. Identity stays
// the resolved record (the amendment does not re-point vehicle_id). Locally
// the amendment door refuses an OPEN trip, so this arm is reached only when a
// trip closed at one post is amended there before the arrival reaches this
// one — and then every post composing the same events shows the same text.
func (l *LiveVehicles) Compose(c ComposedTrip) (display, identity string, err error) {
	t := c.Trip
	if t.VehicleIsUnidentified() {
		return VehicleUnidentifiedText, t.TripID, nil
	}
	identity = ResolveVehicleID(l.merge, t.VehicleID)
	display = derefString(t.VehicleEntered)
	if identity == "" {
		return display, display, nil
	}
	if c.VehicleAmended {
		return display, identity, nil
	}
	v, err := l.vehicle(identity)
	if err != nil {
		return "", "", err
	}
	if v != nil {
		switch {
		case v.Number != nil:
			display = *v.Number
		case v.Role != nil:
			display = *v.Role
		default:
			display = ""
		}
	}
	return display, identity, nil
}
