// ownership.go — D-87 / D-89: WHICH event types modify another site's row,
// declared once, in the package that owns the payloads, and handed to the
// store's choke point (store/ownership.go).
//
// The funnel (store.Author) is free and unbypassable — one Author, one
// event_log INSERT, *sql.DB unexported (reports/witness/d86-ownership.md §0).
// What it lacks is knowledge: from bytes it cannot tell which row a payload
// MODIFIES, nor a target from a reference (an arrival's trip_ref, a
// dispatch's occupants, an ack's trip_id all legitimately name another
// site's rows). That knowledge is this file: one closed table classifying
// EVERY event type the module authors, and one resolver per gated type
// naming the table, id column and tag column of the row it modifies.
//
// THE SET, AND WHY THESE AND NOT OTHERS.
//
//   - D-89 (James): "modify only your own site's data" covers the ROSTER —
//     presence data: visits, check-ins, check-outs, arrivals. Person,
//     company, trade and vehicle records stay SHARED; any site may correct
//     them (D-84 stands; the gate never refuses, D-66). So the four
//     reference amendments are NOT gated, and gating one would be a defect.
//   - D-87 names the two own-site checks already in the tree as "precedent
//     to generalise, not invent": check-out (checkin.go, visit.site_id) and
//     cancel (tripauthor.go, trip.origin_site, §9.1). Both stay at their
//     edges; the funnel now enforces the same rule underneath them, so a
//     future caller that skips the edge still cannot land the event.
//   - arrival_amended and arrival_ref_retracted both modify trip_arrival —
//     the DESTINATION's own presence row (schema-reconcile.md rows 15/16:
//     "the arrival_amended half is the destination's own row either way";
//     row 16 "gets one from the choke point"). F4 §3.6's pre-D-87 allowance
//     that a retraction may come "from either post" does not survive rule
//     4: the origin whose truck is still out radios the destination, which
//     withdraws its own record.
//   - orientation_completed and orientation_retracted are PRESENCE-class
//     under D-88/D-89 (one flag per person, per site) but classify as
//     ownMinted, not ownPresence: neither payload names a site at all —
//     the row's tag is env.SiteID, structurally the authoring post's own
//     (orientation.go header; STEP4-ORIENTATION.md §0/§2.1). A resolver
//     would have nothing to check: there is no target id that could ever
//     resolve to a foreign site's row, so D-87 holds by construction, not
//     by the funnel — adding them to the gated set would be a ruling
//     (widening the hardcoded four in d87_choke_point_test.go), not a
//     classification, and none was asked for here.
//   - NOT gated, by ruling or by an open question that must not be guessed
//     at: trip_amended (J2 — D-96 recorded the orchestrator's reading that
//     trip FACTS are shared, "requiring James's confirmation before the
//     own-tag refusal is written"); occurred_at_corrected (reconcile row 14:
//     "measurement owed, flagged not ruled"); alert_ack_retracted (row 17:
//     an ack is not presence, "stays allowed unless ruled"). Each is listed
//     below as unruledOpen with its citation so the census can see it was
//     considered and not merely forgotten.
//   - The ice-road pair is NO exception: D-86's amendment rules their
//     relationship inject-and-accept, not mutual edit rights. No peer list,
//     no per-peer field (W9 waived).
//
// A NEW AUTHORED TYPE MUST BE CLASSIFIED HERE OR THE CENSUS FAILS
// (d87_choke_point_test.go): the census scans non-test source for every
// authored `Type:` literal and requires the set to EQUAL this table's keys.
// That is the "second write path added later fails the test" half of D-87.
package domain

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"sitecontrol/internal/store"
)

// ownershipClass is what rule 4 says about one event type.
type ownershipClass string

const (
	// ownPresence: the event MODIFIES a presence row; gated at the funnel.
	ownPresence ownershipClass = "presence (gated)"
	// sharedReference: the event modifies reference data; shared under D-89.
	sharedReference ownershipClass = "shared reference (D-89)"
	// ownMinted: the event CREATES its own row, born own-tagged; nothing to gate.
	ownMinted ownershipClass = "mints own row"
	// unruledOpen: the event modifies a row whose ownership under D-87 is an
	// open question on the record; NOT gated, and not to be guessed at.
	unruledOpen ownershipClass = "unruled — open question, not gated"
)

type ownershipRow struct {
	Class ownershipClass
	Why   string
}

// authoredEventOwnership classifies EVERY event type this module authors
// (domain's 17 and deadline's 2 — the census enumerates both packages).
// Keys use the payload's wire `type`.
var authoredEventOwnership = map[string]ownershipRow{
	// --- presence: gated ---------------------------------------------------
	"visit_checked_out":    {ownPresence, "modifies visit (visit.site_id); D-87's named precedent checkin.go"},
	"trip_cancelled":       {ownPresence, "modifies trip (trip.origin_site); D-87's named precedent tripauthor.go, §9.1"},
	evtArrivalAmended:      {ownPresence, "modifies trip_arrival (site_id) — the destination's own row; reconcile row 15"},
	evtArrivalRefRetracted: {ownPresence, "modifies trip_arrival's binding (site_id) — the destination's own row; reconcile row 16"},
	evtArrivalResolved:     {ownPresence, "James, 2026-09-30: officer resolution of parked arrivals (Option 1) — modifies trip_arrival's binding (site_id), the destination's own row, exactly as the retraction does; another post radios the post that recorded the arrival"},
	// --- reference: shared under D-89 ------------------------------------
	evtPersonAmended:  {sharedReference, "D-89: person records stay shared; D-84 stands"},
	evtCompanyAmended: {sharedReference, "D-89: company records stay shared"},
	evtTradeAmended:   {sharedReference, "D-89: trade records stay shared"},
	evtVehicleAmended: {sharedReference, "D-89: vehicle records stay shared"},
	// --- create: the row is born with this post's tag ---------------------
	"company_created": {ownMinted, "newID(siteID, company)"},
	evtTradeCreated:   {ownMinted, "newID(siteID, trade)"},
	"person_created":  {ownMinted, "newID(siteID, person)"},
	evtVehicleCreated: {ownMinted, "newID(siteID, vehicle)"},
	"trip_dispatched": {ownMinted, "newID(siteID, trip); occupants/vehicle are references"},
	"trip_arrival":    {ownMinted, "newID(siteID, arrival/visit); trip_ref is a reference — the accept itself (D-86)"},
	// ⟦was: "weather_phase_declared": {ownMinted, ...} — weather phases
	// removed, James 2026-10-04. Absent from this closed set, the type is
	// now REFUSED at authoring (G13 fail-closed); a legacy one already in a
	// v16 log replays inert (no fold reads it).⟧
	"alert_acknowledged":    {ownMinted, "own alert_ack row; trip_id is a reference"},
	evtOrientationCompleted: {ownMinted, "raw row PK is (env.SiteID, node_epoch, seq) — the payload names no site (orientation.go header; STEP4-ORIENTATION.md §0/§2.1: a cross-site assertion is unrepresentable on the wire)"},
	evtOrientationRetracted: {ownMinted, "derivation keys on env.SiteID — a wire retraction only ever acts on its own site's (person, site) row, inert elsewhere; D-87 by construction, not by resolver (orientation.go header; STEP4-ORIENTATION.md §0)"},
	// --- unruled: NOT gated, recorded as open -----------------------------
	evtTripAmended:         {unruledOpen, "J2 / D-96: trip FACTS read as shared reference; James's confirmation owed before an own-tag refusal is written"},
	evtOccurredAtCorrected: {unruledOpen, "reconcile row 14: refusal when the target is a presence event — measurement owed, flagged not ruled"},
	"alert_ack_retracted":  {unruledOpen, "reconcile row 17: an ack is not presence under D-89; stays allowed unless ruled"},
}

// registerOwnership hands the funnel one resolver per gated type, then
// (G13) acknowledges every OTHER classified type — sharedReference,
// ownMinted, unruledOpen — so the funnel's fail-closed default does not
// refuse them: only a type ABSENT from authoredEventOwnership altogether is
// refused. Called from Register, after the projections (the resolvers read
// projection tables, and trip_arrival's DDL rides its projection's Schema).
func registerOwnership(st *store.Store) error {
	for _, g := range []struct {
		typ      string
		resolver store.OwnerResolver
	}{
		{"visit_checked_out", rowTag("visit", "visit_id", "site_id", "visit_id")},
		{"trip_cancelled", rowTag("trip", "trip_id", "origin_site", "trip_id")},
		{evtArrivalAmended, rowTag("trip_arrival", "arrival_id", "site_id", "arrival_id")},
		{evtArrivalRefRetracted, rowTag("trip_arrival", "arrival_id", "site_id", "arrival_id")},
		{evtArrivalResolved, rowTag("trip_arrival", "arrival_id", "site_id", "arrival_id")},
	} {
		if authoredEventOwnership[g.typ].Class != ownPresence {
			return fmt.Errorf("domain: ownership: %q is registered as gated but classified %q", g.typ, authoredEventOwnership[g.typ].Class)
		}
		if err := st.RegisterOwnership(g.typ, g.resolver); err != nil {
			return err
		}
	}
	// G13: the rest of the closed set. ownPresence is skipped — those four
	// are already registered above, as gated, and RegisterAcknowledged
	// refuses a type already registered as gated (two opinions about one
	// type would be exactly the defect this guards against).
	for typ, row := range authoredEventOwnership {
		if row.Class == ownPresence {
			continue
		}
		if err := st.RegisterAcknowledged(typ); err != nil {
			return err
		}
	}
	return nil
}

// rowTag is the one resolver shape every gated type shares — the shape the
// two pre-existing edge checks already had (load the target row by id, read
// its tag column): SELECT <tagColumn> FROM <table> WHERE <idColumn> = ?,
// with the id taken from the payload's <payloadKey>. Identifiers are
// compile-time literals from registerOwnership, never input.
func rowTag(table, idColumn, tagColumn, payloadKey string) store.OwnerResolver {
	query := fmt.Sprintf(`SELECT %s FROM %s WHERE %s = ?`, tagColumn, table, idColumn)
	return func(tx *sql.Tx, payload json.RawMessage) (string, string, error) {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(payload, &fields); err != nil {
			return "", "", fmt.Errorf("domain: ownership: decode payload: %w", err)
		}
		var target string
		if raw, ok := fields[payloadKey]; ok {
			if err := json.Unmarshal(raw, &target); err != nil {
				return "", "", fmt.Errorf("domain: ownership: %s is not a string: %w", payloadKey, err)
			}
		}
		if target == "" {
			return "", "", nil // names no row → cannot be shown to be ours → refused
		}
		var owner string
		err := tx.QueryRow(query, target).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) {
			return target, "", nil
		}
		if err != nil {
			return target, "", fmt.Errorf("domain: ownership: read %s %s: %w", table, target, err)
		}
		return target, owner, nil
	}
}
