// outcome.go — the visible-failure resolver (design/visible-failure-v1.md
// rev 2 §4, P-5's accepted mechanism): per-FACT outcome for what one event
// asserted, computed at READ time from the INSERT-only side tables — never
// stored, never cached (§4.3 rule 2: a status stamped at write goes
// permanently wrong the moment the deferred winner arrives by
// replication). W3 builds it with `person_amended` as the first registered
// carrier; W4 and the clone chunks extend the registry, never the
// mechanism.
//
// ERRORS, NOT EMPTIES (§4.2): a key matching no known event, or an event
// whose type is not a registered carrier, returns a non-nil error — never
// ([]FactOutcome{}, nil). The §5 rendering layer depends on that
// (rendering 4 is loud, never blank).
package domain

import (
	"encoding/json"
	"fmt"
	"strings"

	"sitecontrol/internal/store"
)

// Status is one assertion's standing under fixed-order composition
// (visible-failure-v1 §4.2, verbatim).
type Status int

const (
	Effective  Status = iota // this event's assertion is what every surface shows
	Superseded               // recorded, kept, and not displayed anywhere
)

func (s Status) String() string {
	switch s {
	case Effective:
		return "Effective"
	case Superseded:
		return "Superseded"
	}
	return fmt.Sprintf("Status(%d)", int(s))
}

// FactOutcome is the officer-facing answer for ONE fact one event asserted
// (visible-failure-v1 §4.2, verbatim — no field added, no field renamed).
type FactOutcome struct {
	Fact     string // from the carrier's registered vocabulary (§4.4)
	Asserted string // canonical rendering of what this event claimed
	Status   Status
	Winner   store.Key // the event whose assertion is effective for this
	// fact. ALWAYS a real event's key, never the zero value;
	// Winner == the queried key iff Status == Effective.
}

// --- carrier registration (§4.4: the closed vocabulary) ------------------

// carrierRegistration supplies the three registration goods: the Fact
// vocabulary, the canonical Asserted rendering per fact, and the
// target/entity extraction from the carrier's side-table row. The two
// carrier CLASSES read different side tables, so each class binds its own
// pair of rendering/extraction funcs — an amendment carrier leaves the
// correction pair nil and vice versa; the Facts list is common to both.
// ⟦W5d, DR-7⟧ The third class pair joins here, bound over
// companyAmendmentRow, beside the existing person-amendment and correction
// pairs. It is a THIRD PAIR and deliberately not an interface or a generic:
// that is the shape this type's own comment already describes, and the
// alternative would refactor code two LOCKED chunks (W3, W4) depend on, at
// the first clone, for no operator-visible gain. The cost is honest and is
// accepted — a fourth and fifth pair land at W7 (vehicle) and W8 (trade),
// and by W8 the case for a generic will be stronger than it is now.
// ⟦LOCK — W8 OWED ACTS §42.8, 2026-09-03: this block and the block at the
// W9a fields below DISAGREED, forty lines apart, and a W8 builder opening
// this file met THIS one first. The neighbour is right: the true class total
// is SEVEN, not five, and `Facts []string` cannot hold the trip carrier's
// `occupant/<person_id>` PATTERN vocabulary at all. So the licence's stated
// precondition — "with all five classes visible" — was never met and cannot
// be. ⚖️ RULED at §42.3: W8 DOES NOT SPEND IT. The generic is not built at
// W8; the trade carrier clones this pair, as W5d and W7 did. The licence is
// NOT REVOKED, only NOT SPENT: its next moment is booked at §42.8 without a
// chunk name, because every chunk it has been booked to has had its own
// reason to decline it. This is a DOC-COMMENT edit; it changes no code and
// no test.⟧
// ⟦was: "W8 may make that refactor with all five classes visible; W5d may
// not."⟧
type carrierRegistration struct {
	// Facts is the carrier's fact vocabulary, in canonical order.
	Facts []string
	// Asserted renders what one amendment row claimed for one fact.
	Asserted func(row personAmendmentRow, fact string) string
	// TargetID extracts the entity an amendment row targeted.
	TargetID func(row personAmendmentRow) string
	// AssertedCompany renders what one COMPANY amendment row claimed for
	// one fact (⟦W5d, DR-7⟧ — the third class's rendering func).
	AssertedCompany func(row companyAmendmentRow, fact string) string
	// CompanyTargetID extracts the entity a company amendment row targeted
	// (⟦W5d, DR-7⟧ — the third class's extraction func).
	CompanyTargetID func(row companyAmendmentRow) string
	// AssertedCorrection renders what one correction row claimed.
	AssertedCorrection func(row occurredAtCorrectionRow, fact string) string
	// CorrectionTarget extracts the LOG KEY a correction row targeted
	// (item 14 targets an event, never an entity — recount §4b).
	CorrectionTarget func(row occurredAtCorrectionRow) store.Key

	// ⟦W9a, F4 §6/§11⟧ THE FOURTH AND FIFTH CLASS PAIRS. F4's two carriers
	// read two DIFFERENT side tables (`trip_amendment` / `arrival_amendment`),
	// so they are two distinct class pairs and not one — which is why the
	// true class total here is SEVEN and not the five DR-7's block above
	// tells W8 to size a generic for. Recorded at the point of use, so the
	// count can be checked against the code rather than believed.
	//
	// ⚠ `Facts` CANNOT HOLD THE TRIP CARRIER'S VOCABULARY, and the SHAPE
	// matters more than the count. `occupant/<person_id>` (§6) is a fact-name
	// PATTERN whose suffix is DATA; `Facts` is a plain static []string and is
	// the loop variable of both resolution passes. A vocabulary that depends
	// on the ROW cannot be a []string. So this carrier STATES THE PROPERTY —
	// `Facts` for the closed half, `Patterns` for the open half — and DERIVES
	// the per-row list through `TripRowFacts`. A generic sized for five
	// classes and a closed vocabulary would have to be stretched afterwards
	// to hold a requirement it was never asked to hold; the paragraph F4 §11
	// owes W8 names the requirement, not only the number.
	Patterns []string
	// TripRowFacts derives ONE trip-amendment row's fact vocabulary.
	TripRowFacts func(row tripAmendmentRow) []string
	// AssertedTrip renders what one trip-amendment row claimed for one fact.
	AssertedTrip func(row tripAmendmentRow, fact string) string
	// TripTargetID extracts the trip an amendment row targeted.
	TripTargetID func(row tripAmendmentRow) string
	// AssertedArrival renders what one arrival-amendment row claimed.
	AssertedArrival func(row arrivalAmendmentRow, fact string) string
	// ArrivalTargetID extracts the arrival an amendment row targeted.
	ArrivalTargetID func(row arrivalAmendmentRow) string

	// ⟦W7 (item 9)⟧ THE SIXTH CLASS PAIR — vehicle. DR-7's block above
	// predicted a pair landing here at W7 and this is it; the true class
	// total is now EIGHT, recorded at the point of use rather than believed,
	// because W8 is told to size a generic by that number.
	AssertedVehicle func(row vehicleAmendmentRow, fact string) string
	// VehicleTargetID extracts the vehicle an amendment row targeted.
	VehicleTargetID func(row vehicleAmendmentRow) string

	// ⟦W8 (item 10)⟧ THE SEVENTH CLASS PAIR — trade. DR-7's block above
	// predicted a pair landing here at W8 and this is it. ⚠ It is a CLONE and
	// not a generic, and that is a RULING rather than a shortcut: see the
	// §42.8 amendment on DR-7's block for the two measured grounds — the
	// class count was never five, and `Facts` cannot hold a pattern
	// vocabulary. The clone cost is accepted and stated.
	AssertedTrade func(row tradeAmendmentRow, fact string) string
	// TradeTargetID extracts the trade an amendment row targeted.
	TradeTargetID func(row tradeAmendmentRow) string
}

// carrierRegistry is the closed §4.4 vocabulary. W3 registers
// person_amended (the first carrier); occurred_at_corrected joins at W4
// (the fifth carrier by VF §4.4's count — the amendment-family clones land
// at W5d/W7/W8, F4's pair at W9). ⟦W8⟧ The event type this comment used to
// name as a permanent NON-carrier is now DELETED outright rather than merely
// unregistered: P-3 §5 scheduled it and D-47 set the bar, so there is no
// longer a spelling of it anywhere in this package to exempt.
var carrierRegistry = map[string]carrierRegistration{
	// W4 (item 14): the correction carrier — fact vocabulary
	// {"occurred_at"}, canonical Asserted rendering, target-KEY extraction
	// from the occurred_at_correction side table.
	evtOccurredAtCorrected: {
		Facts:              []string{factOccurredAt},
		AssertedCorrection: renderCorrectedInstant,
		CorrectionTarget:   func(row occurredAtCorrectionRow) store.Key { return row.Target },
	},
	evtPersonAmended: {
		Facts: personAmendableFacts,
		Asserted: func(row personAmendmentRow, fact string) string {
			if v, ok := row.Set[fact]; ok {
				return v
			}
			for _, c := range row.Cleared {
				if c == fact {
					if fact == "same_as" {
						return "un-merge" // Clear: ["same_as"] IS the un-merge (P-3 §5)
					}
					return "cleared"
				}
			}
			return ""
		},
		TargetID: func(row personAmendmentRow) string { return row.PersonID },
	},
	// W5d (item 12): the company amendment carrier — DR-7's third class
	// pair. Fact vocabulary {name, same_as} (W5d-22(a)), the canonical
	// Asserted rendering (the un-merge's own word included, so a surface
	// can say "the merge stands" in the un-merge's own terms), and
	// target-ENTITY extraction from the company_amendment side table.
	evtCompanyAmended: {
		Facts: companyAmendableFacts,
		AssertedCompany: func(row companyAmendmentRow, fact string) string {
			if v, ok := row.Set[fact]; ok {
				return v
			}
			for _, c := range row.Cleared {
				if c == fact {
					if fact == "same_as" {
						return "un-merge" // Clear: ["same_as"] IS the un-merge (P-3 §5)
					}
					return "cleared"
				}
			}
			return ""
		},
		CompanyTargetID: func(row companyAmendmentRow) string { return row.CompanyID },
	},
	// ⟦W9a (item 15), F4 §3.3⟧ THE SIXTH AND SEVENTH CARRIERS: the trip and
	// arrival amendment families. The trip carrier is the first with an OPEN
	// vocabulary — see carrierRegistration.Patterns above.
	evtTripAmended: {
		Facts:        tripAmendableScalarFacts,
		Patterns:     []string{factOccupantPrefix},
		TripRowFacts: func(row tripAmendmentRow) []string { return row.facts() },
		AssertedTrip: func(row tripAmendmentRow, fact string) string { return row.asserted(fact) },
		TripTargetID: func(row tripAmendmentRow) string { return row.TripID },
	},
	evtArrivalAmended: {
		Facts:           arrivalAmendableFacts,
		AssertedArrival: func(row arrivalAmendmentRow, fact string) string { return row.asserted(fact) },
		ArrivalTargetID: func(row arrivalAmendmentRow) string { return row.ArrivalID },
	},
	// ⟦W7 (item 9)⟧ THE VEHICLE AMENDMENT CARRIER. Fact vocabulary
	// {number, role, same_as} (DR-W7-6), the canonical Asserted rendering
	// including the un-merge's own word — so a surface can say "the join
	// stands" in the un-join's own terms rather than a generic supersession
	// line — and target-ENTITY extraction from the vehicle_amendment table.
	//
	// ⚠ REGISTERING IT HERE IS HALF THE JOB. Both `switch typ` dispatches
	// below gain the case in the SAME change: a carrier registered here and
	// missed in one dispatch DOES NOT FAIL LOUDLY — it falls to outcomeMiss,
	// whose IsRegisteredCarrier check is TRUE, so a real amendment with a
	// real side-table row is reported as having "no amendment record", and
	// the officer's confirmation block says the outcome could not be
	// determined for a change that recorded perfectly.
	evtVehicleAmended: {
		Facts: vehicleAmendableFacts,
		AssertedVehicle: func(row vehicleAmendmentRow, fact string) string {
			if v, ok := row.Set[fact]; ok {
				return v
			}
			for _, c := range row.Cleared {
				if c == fact {
					if fact == "same_as" {
						return "un-merge" // Clear: ["same_as"] IS the un-merge (P-3 §5)
					}
					return "cleared"
				}
			}
			return ""
		},
		VehicleTargetID: func(row vehicleAmendmentRow) string { return row.VehicleID },
	},
	// ⟦W8 (item 10)⟧ THE TRADE AMENDMENT CARRIER. Fact vocabulary
	// {name, phone_day, phone_night, phone_shared, same_as}, the canonical
	// Asserted rendering including the un-merge's own word — so a surface can
	// say "the merge stands" in the un-merge's own terms rather than a
	// generic supersession line — and target-ENTITY extraction from the
	// trade_amendment table.
	//
	// ⚠ REGISTERING IT HERE IS HALF THE JOB. Both `switch typ` dispatches
	// below gain the case in the SAME change: a carrier registered here and
	// missed in one dispatch DOES NOT FAIL LOUDLY — it falls to outcomeMiss,
	// whose IsRegisteredCarrier check is TRUE, so a real amendment with a
	// real side-table row is reported as having "no amendment record", and
	// the officer's confirmation block says the outcome could not be
	// determined for a change that recorded perfectly.
	evtTradeAmended: {
		Facts: tradeAmendableFacts,
		AssertedTrade: func(row tradeAmendmentRow, fact string) string {
			if v, ok := row.Set[fact]; ok {
				return v
			}
			for _, c := range row.Cleared {
				if c == fact {
					if fact == "same_as" {
						return "un-merge" // Clear: ["same_as"] IS the un-merge (P-3 §5)
					}
					return "cleared"
				}
			}
			return ""
		},
		TradeTargetID: func(row tradeAmendmentRow) string { return row.TradeID },
	},
}

// CarrierFactMatches reports whether `fact` belongs to `eventType`'s
// registered §4.4 vocabulary — the CLOSED half by equality, the OPEN half
// by PATTERN. It exists because the open half cannot be enumerated: a
// surface that needs to know "is this an occupant-membership fact?" must
// ask the property, never a second copy of the list.
func CarrierFactMatches(eventType, fact string) bool {
	reg, ok := carrierRegistry[eventType]
	if !ok {
		return false
	}
	for _, f := range reg.Facts {
		if f == fact {
			return true
		}
	}
	for _, p := range reg.Patterns {
		if strings.HasPrefix(fact, p) && len(fact) > len(p) {
			return true
		}
	}
	return false
}

// OccupantFactPersonID returns the RAW person id inside an
// `occupant/<person_id>` fact name, and whether `fact` is one. The prefix
// is the registry's, never a second spelling.
func OccupantFactPersonID(fact string) (string, bool) {
	if !strings.HasPrefix(fact, factOccupantPrefix) || len(fact) == len(factOccupantPrefix) {
		return "", false
	}
	return strings.TrimPrefix(fact, factOccupantPrefix), true
}

// IsRegisteredCarrier reports whether eventType is a registered §4.4
// carrier (SEAM W3-S-REGISTRY). The coverage test enumerates the live
// log's amendment/correction families against this predicate.
func IsRegisteredCarrier(eventType string) bool {
	_, ok := carrierRegistry[eventType]
	return ok
}

// --- Outcome -------------------------------------------------------------

// Outcome reports, for each fact the event at k asserted, whether that
// assertion is effective under the composition rule, and if not, which
// event supersedes it (visible-failure-v1 §4.2). Per-fact, never
// per-event (W3-9): an amendment that set {phone, name} can lose phone and
// keep name, and the officer must be told exactly that. For the person
// carrier the rule is resolveContest's since SchemaVersion 11 ([[D-92]]:
// causal, then ascending key); ⟦was: "under fixed-order composition"⟧.
//
// Scope of contest, per fact:
//   - same_as (the merge pointer) is contested among assertions targeting
//     the SAME record — resolving it through the merge map first would
//     erase the very fact under judgement.
//   - every VALUE fact is contested across the assertion sets a merge
//     JOINS (§6 case 5, W3-10): X's phone can be superseded by an event
//     that never targeted X, because a same_as merge joined X to Y.
//
// Pure read: recomputed on every call from the side-table record set in
// ascending key order; stores nothing. A key matching no event, or an
// unregistered carrier, is an ERROR (never an empty success) — the caller
// renders it as §5's rendering 4.
func Outcome(st *store.Store, k store.Key) ([]FactOutcome, error) {
	// The correction carrier first — a keyed point-read, so an
	// occurred_at_corrected event never pays the amendment table decode.
	// Its per-fact answer resolves against the ORIGINAL target's chain
	// (time_correct.go; W4-11's one-hop rule), fail-closed on dangling
	// targets and cycles exactly as ResolveOccurredAt is.
	if crow, err := correctionRowAt(st, k); err != nil {
		return nil, fmt.Errorf("domain: outcome: %w", err)
	} else if crow != nil {
		return correctionOutcome(st, k, *crow)
	}
	// ⟦W5d, DR-7⟧ THE AMENDMENT CLASSES, DISPATCHED ON THE QUERIED EVENT'S
	// OWN TYPE — never on a hardcoded constant. The branch selects the
	// registration and the row-reader TOGETHER, because they are chosen
	// together: a class reading the wrong side table finds no row and its
	// failure is then disguised as outcomeMiss's sanctioned "registered
	// carrier … has no amendment record" arm. That disguise is exactly
	// mutant (m8) — registering company_amended while the dispatch stays
	// hardcoded to evtPersonAmended — and it is why W5d-11 asserts the
	// POSITIVE answer rather than only the unregistered-type control.
	typ, err := eventTypeAt(st, k)
	if err != nil {
		return nil, err
	}
	var (
		facts []factResolution
		found bool
	)
	switch typ {
	case evtPersonAmended:
		facts, found, err = personFactResolutions(st, k)
	case evtCompanyAmended:
		facts, found, err = companyFactResolutions(st, k)
	// ⟦W9a-16(b)⟧ THE TWO NEW CARRIERS LAND IN BOTH `switch typ` DISPATCHES
	// IN THE SAME CHANGE. There are two, not one — this one and
	// Competitors' — and a carrier added here and missed there DOES NOT
	// FAIL LOUDLY: it falls to outcomeMiss, whose IsRegisteredCarrier check
	// is TRUE, so a real event with a real side-table row is reported as
	// having "no amendment record". That is the sanctioned arm reached by
	// the one route W5d's dispatch comment does not cover.
	case evtTripAmended:
		facts, found, err = tripFactResolutions(st, k)
	case evtArrivalAmended:
		facts, found, err = arrivalFactResolutions(st, k)
	// ⟦W7-10⟧ The vehicle carrier, landing in BOTH dispatches in one change.
	case evtVehicleAmended:
		facts, found, err = vehicleFactResolutions(st, k)
	// ⟦W8-14⟧ The trade carrier, landing in BOTH dispatches in one change.
	case evtTradeAmended:
		facts, found, err = tradeFactResolutions(st, k)
	}
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, outcomeMiss(st, k)
	}

	var out []FactOutcome
	for _, f := range facts {
		status := Effective
		if f.Winner != k {
			status = Superseded
		}
		out = append(out, FactOutcome{
			Fact:     f.Fact,
			Asserted: f.Asserted,
			Status:   status,
			Winner:   f.Winner,
		})
	}
	if len(out) == 0 {
		// A recorded amendment row that asserts nothing (a wire no-op).
		// An empty successful result does not exist in this API (§4.2).
		return nil, fmt.Errorf("domain: outcome: the event at %v asserts no fact", k)
	}
	return out, nil
}

// --- Competitors, and the ONE shared internal pass behind both ----------

// factResolution is the shared pass's answer for ONE fact the queried event
// asserted: what it claimed, whose assertion is effective, and which OTHER
// events contested it.
//
// ⟦W5d-9(d)⟧ Outcome and Competitors are both thin readings of this, so THE
// RULE THAT DECIDES WHICH EVENTS COMPETE IS WRITTEN ONCE. A second, narrower
// spelling of the contest scope ("assertions on the same target id") looks
// correct on every un-merged fixture and silently drops the competitor a
// same_as merge JOINED in — the same defect this project forbids for SQL
// ordering, one layer up.
type factResolution struct {
	Fact     string
	Asserted string
	// Winner is the assertion of this fact that stands over the contested
	// set — the queried key itself when nothing supersedes it. For EVERY
	// amendment carrier that is resolveContest's answer ([[D-92]],
	// SchemaVersion 11; the person carrier at step 2, the six clones at step
	// 3): causal dominance first, then the greatest key among the
	// undominated. ⟦was: "the LAST assertion of this fact in ascending key
	// order over the contested set"; then, at step 2: "The other carriers
	// still answer with the LAST assertion in ascending key order until their
	// own stage of the increment clones the rule"⟧
	Winner store.Key
	// Competitors are the OTHER events in that same contested set, in
	// ascending key order, the queried key excluded.
	Competitors []store.Key
}

// EventTypeAt is eventTypeAt for the web layer's ?recorded= confirmations
// (W4, 2026-10-03, T-199/R-080): a confirmation names a key from its URL,
// and a key that names an event of a DIFFERENT kind — or no event at all —
// must not be confirmed as the act the page is about. "" means no event, or
// none with a readable type.
func EventTypeAt(st *store.Store, k store.Key) (string, error) {
	return eventTypeAt(st, k)
}

// Exported names for the event kinds a ?recorded= confirmation checks.
// ⟦D22, fix2-fj 2026-10-04⟧ the person, company, vehicle and trip/arrival
// amendment kinds joined the first three, so each amend page confirms only
// its own kind (it used to confirm a company rename as a person amendment).
const (
	EventOccurredAtCorrected = evtOccurredAtCorrected
	EventTradeCreated        = evtTradeCreated
	EventTradeAmended        = evtTradeAmended
	EventPersonAmended       = evtPersonAmended
	EventCompanyAmended      = evtCompanyAmended
	EventVehicleAmended      = evtVehicleAmended
	EventTripAmended         = evtTripAmended
	EventArrivalAmended      = evtArrivalAmended
	// ⟦F11, fix3 2026-10-04⟧ the two arrivals review pages' own kinds.
	EventArrivalResolved     = evtArrivalResolved
	EventArrivalRefRetracted = evtArrivalRefRetracted
)

// eventTypeAt point-reads the payload type of the event at k. The one
// sanctioned envelope read is a primary-key point-read (§4.1). An absent
// event or an unreadable type returns ("", nil): the caller falls through
// to outcomeMiss, which is the single place that explains WHY no outcome
// exists and must keep saying it in its own words.
func eventTypeAt(st *store.Store, k store.Key) (string, error) {
	evs, err := st.Events(k.SiteID, k.NodeEpoch, k.Seq, 1)
	if err != nil {
		return "", fmt.Errorf("domain: outcome: %w", err)
	}
	if len(evs) == 0 || evs[0].Seq != k.Seq {
		return "", nil
	}
	var pl struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(evs[0].Payload, &pl); err != nil {
		return "", nil
	}
	return pl.Type, nil
}

// personFactResolutions is the person class's pass. found=false means this
// class holds no row for k — never an empty success.
func personFactResolutions(st *store.Store, k store.Key) ([]factResolution, bool, error) {
	rows, err := readPersonAmendments(st)
	if err != nil {
		return nil, false, fmt.Errorf("domain: outcome: %w", err)
	}
	var target *personAmendmentRow
	for i := range rows {
		if rows[i].Key == k {
			target = &rows[i]
			break
		}
	}
	if target == nil {
		return nil, false, nil
	}
	reg := carrierRegistry[evtPersonAmended]
	m := mergeMapFromRows(rows)
	rep := m.Resolve(reg.TargetID(*target))

	var out []factResolution
	for _, fact := range reg.Facts {
		if !target.asserts(fact) {
			continue
		}
		// ⟦SchemaVersion 11, [[D-92]]⟧ The contest scope is personContest's
		// (unchanged: same_as among assertions on the same record, value
		// facts across the merged group) and the winner is resolveContest's
		// — the ONE rule: an assertion another contestant's `seen` names is
		// dominated; among the undominated the greatest key wins. rows is
		// already in ascending key order, so the contest is too.
		// ⟦was: "the LAST assertion of this fact, in ascending key order,
		// over the contested set"⟧
		contest := personContest(rows, m, rep, target.PersonID, fact)
		winner, _ := resolveContest(contest)
		res := factResolution{Fact: fact, Asserted: reg.Asserted(*target, fact), Winner: winner}
		for _, r := range contest {
			if r.Key != k {
				res.Competitors = append(res.Competitors, r.Key)
			}
		}
		out = append(out, res)
	}
	return out, true, nil
}

// companyFactResolutions is the company class's pass — DR-7's third pair in
// use. Same contest scope, same order, over the company side table: the
// merge pointer is contested among assertions targeting the SAME record
// (resolving it first would erase the fact under judgement), and the value
// fact `name` is contested across the assertion sets a merge JOINS.
func companyFactResolutions(st *store.Store, k store.Key) ([]factResolution, bool, error) {
	rows, err := readCompanyAmendments(st)
	if err != nil {
		return nil, false, fmt.Errorf("domain: outcome: %w", err)
	}
	var target *companyAmendmentRow
	for i := range rows {
		if rows[i].Key == k {
			target = &rows[i]
			break
		}
	}
	if target == nil {
		return nil, false, nil
	}
	reg := carrierRegistry[evtCompanyAmended]
	m := companyMergeMapFromRows(rows)
	rep := m.Resolve(reg.CompanyTargetID(*target))

	var out []factResolution
	for _, fact := range reg.Facts {
		if !target.asserts(fact) {
			continue
		}
		// ⟦SchemaVersion 11, [[D-92]]⟧ The scope is amendmentContest's
		// (same_as among assertions on the same record, value facts across
		// the merged group) and the winner is resolveContest's — the ONE
		// rule. ⟦was: "the LAST assertion of this fact in ascending key
		// order over the contested set"⟧
		contest := amendmentContest(rows, m, rep, target.CompanyID, fact)
		winner, _ := resolveContest(contest)
		res := factResolution{Fact: fact, Asserted: reg.AssertedCompany(*target, fact), Winner: winner}
		for _, r := range contest {
			if r.Key != k {
				res.Competitors = append(res.Competitors, r.Key)
			}
		}
		out = append(out, res)
	}
	return out, true, nil
}

// tripFactResolutions is the TRIP amendment class's pass — the sixth
// carrier. The contest scope is the assertions targeting the SAME TRIP, so
// the row set is read SCOPED by trip_id in the one ascending-key order; a
// fact is contested only by another assertion of THE SAME FACT NAME, which
// for `occupant/<person_id>` means the same RAW person id (§6: removing one
// occupant does not contest with adding another).
//
// The vocabulary is DERIVED from the row, not iterated out of a static
// list — see carrierRegistration.Patterns.
func tripFactResolutions(st *store.Store, k store.Key) ([]factResolution, bool, error) {
	target, err := tripAmendmentAt(st, k)
	if err != nil {
		return nil, false, fmt.Errorf("domain: outcome: %w", err)
	}
	if target == nil {
		return nil, false, nil
	}
	reg := carrierRegistry[evtTripAmended]
	rows, err := readTripAmendmentsFor(st, reg.TripTargetID(*target))
	if err != nil {
		return nil, false, fmt.Errorf("domain: outcome: %w", err)
	}
	var out []factResolution
	for _, fact := range reg.TripRowFacts(*target) {
		// ⟦SchemaVersion 11, [[D-92]]⟧ resolveContest's winner over the
		// same-trip contest — the ONE rule. ⟦was: "the LAST assertion of
		// this fact in ascending key order"⟧
		contest := rowsAsserting(rows, fact)
		res := factResolution{Fact: fact, Asserted: reg.AssertedTrip(*target, fact), Winner: k}
		if len(contest) > 0 {
			res.Winner, _ = resolveContest(contest)
		}
		for _, r := range contest {
			if r.Key != k {
				res.Competitors = append(res.Competitors, r.Key)
			}
		}
		out = append(out, res)
	}
	return out, true, nil
}

// arrivalFactResolutions is the ARRIVAL claim class's pass — the seventh
// carrier. Same order, same contest rule, over the arrival's own rows.
func arrivalFactResolutions(st *store.Store, k store.Key) ([]factResolution, bool, error) {
	target, err := arrivalAmendmentAt(st, k)
	if err != nil {
		return nil, false, fmt.Errorf("domain: outcome: %w", err)
	}
	if target == nil {
		return nil, false, nil
	}
	reg := carrierRegistry[evtArrivalAmended]
	rows, err := readArrivalAmendmentsFor(st, reg.ArrivalTargetID(*target))
	if err != nil {
		return nil, false, fmt.Errorf("domain: outcome: %w", err)
	}
	var out []factResolution
	for _, fact := range reg.Facts {
		if !target.asserts(fact) {
			continue
		}
		// ⟦SchemaVersion 11, [[D-92]]⟧ resolveContest's winner over the
		// arrival's own rows — the ONE rule. ⟦was: "the LAST assertion of
		// this fact in ascending key order"⟧
		contest := rowsAsserting(rows, fact)
		res := factResolution{Fact: fact, Asserted: reg.AssertedArrival(*target, fact), Winner: k}
		if len(contest) > 0 {
			res.Winner, _ = resolveContest(contest)
		}
		for _, r := range contest {
			if r.Key != k {
				res.Competitors = append(res.Competitors, r.Key)
			}
		}
		out = append(out, res)
	}
	return out, true, nil
}

// Competitors reports, per fact the event at k asserted, the OTHER events
// that contested that fact within k's contest scope — in ascending key
// order, with k itself excluded. A fact with no competitors is absent from
// the map.
//
// ⟦W5d-9(b), per [[D-53]]⟧ This is the ONE exported domain API the FIX arm
// adds BESIDE the pinned FactOutcome, and beside is the whole point: for
// amendments the competitors are discarded in the resolve loop, so the
// struct genuinely cannot hold them — and FactOutcome must stay
// byte-stable, because W4's lock rests on its "no field added, no field
// renamed" guarantee. It shares one internal pass with Outcome (see
// factResolution), so the contest-scope rule exists in exactly one spelling.
//
// Errors, never empties (§4.2's own contract, inherited): a key matching no
// event, or one whose type is not an amendment carrier, is an error.
func Competitors(st *store.Store, k store.Key) (map[string][]store.Key, error) {
	typ, err := eventTypeAt(st, k)
	if err != nil {
		return nil, err
	}
	// The correction carrier is OUT OF SCOPE here, and says so rather than
	// answering vaguely: its contest scope is W4's one-hop target chain,
	// not this pass's, and W5d-9(b)'s obligation is the amendment classes'
	// rendering-2 arm. A caller that needs it is asking a question W5d did
	// not build an answer to.
	if typ == evtOccurredAtCorrected {
		return nil, fmt.Errorf("domain: competitors: %q at %v is the correction carrier — competitor enumeration is defined for the amendment carriers (W5d-9(b)); the correction class resolves against its own target chain", typ, k)
	}
	var (
		facts []factResolution
		found bool
	)
	switch typ {
	case evtPersonAmended:
		facts, found, err = personFactResolutions(st, k)
	case evtCompanyAmended:
		facts, found, err = companyFactResolutions(st, k)
	// ⟦W9a-16(b)⟧ The other half of the SAME change — see Outcome's switch.
	case evtTripAmended:
		facts, found, err = tripFactResolutions(st, k)
	case evtArrivalAmended:
		facts, found, err = arrivalFactResolutions(st, k)
	// ⟦W7-10⟧ The other half of the SAME change — see Outcome's switch.
	case evtVehicleAmended:
		facts, found, err = vehicleFactResolutions(st, k)
	// ⟦W8-14⟧ The other half of the SAME change — see Outcome's switch.
	case evtTradeAmended:
		facts, found, err = tradeFactResolutions(st, k)
	}
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, outcomeMiss(st, k)
	}
	out := map[string][]store.Key{}
	for _, f := range facts {
		if len(f.Competitors) > 0 {
			out[f.Fact] = f.Competitors
		}
	}
	return out, nil
}

// vehicleFactResolutions is the vehicle class's pass — the sixth class pair in
// use. Same contest scope, same order, over the vehicle side table: the join
// pointer is contested among assertions targeting the SAME record (resolving
// it first would erase the fact under judgement), and the value facts are
// contested across the assertion sets a join UNITES.
func vehicleFactResolutions(st *store.Store, k store.Key) ([]factResolution, bool, error) {
	rows, err := readVehicleAmendments(st)
	if err != nil {
		return nil, false, fmt.Errorf("domain: outcome: %w", err)
	}
	var target *vehicleAmendmentRow
	for i := range rows {
		if rows[i].Key == k {
			target = &rows[i]
			break
		}
	}
	if target == nil {
		return nil, false, nil
	}
	reg := carrierRegistry[evtVehicleAmended]
	m := vehicleMergeMapFromRows(rows)
	rep := m.Resolve(reg.VehicleTargetID(*target))

	var out []factResolution
	for _, fact := range reg.Facts {
		if !target.asserts(fact) {
			continue
		}
		// ⟦SchemaVersion 11, [[D-92]]⟧ The scope is amendmentContest's (the
		// join pointer among assertions about the SAME record — resolving
		// through the map first would erase the very fact under judgement;
		// value facts across the joined group) and the winner is
		// resolveContest's — the ONE rule. ⟦was: "the LAST assertion of this
		// fact in ascending key order over the contested set"⟧
		contest := amendmentContest(rows, m, rep, target.VehicleID, fact)
		winner, _ := resolveContest(contest)
		res := factResolution{Fact: fact, Asserted: reg.AssertedVehicle(*target, fact), Winner: winner}
		for _, r := range contest {
			if r.Key != k {
				res.Competitors = append(res.Competitors, r.Key)
			}
		}
		out = append(out, res)
	}
	return out, true, nil
}

// outcomeMiss explains WHY no outcome exists for k, always as an error:
// the event is absent, or its type is not a registered carrier. The one
// sanctioned envelope read is a primary-key point-read (§4.1) — here via
// the store's keyed range read, one row.
func outcomeMiss(st *store.Store, k store.Key) error {
	evs, err := st.Events(k.SiteID, k.NodeEpoch, k.Seq, 1)
	if err != nil {
		return fmt.Errorf("domain: outcome: %w", err)
	}
	if len(evs) == 0 || evs[0].Seq != k.Seq {
		return fmt.Errorf("domain: outcome: no event exists at %v", k)
	}
	var pl struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(evs[0].Payload, &pl); err != nil || pl.Type == "" {
		return fmt.Errorf("domain: outcome: the event at %v carries no readable type", k)
	}
	if !IsRegisteredCarrier(pl.Type) {
		return fmt.Errorf("domain: outcome: event type %q at %v is not a registered carrier (§4.4) — an unregistered type is an error, never an empty success", pl.Type, k)
	}
	return fmt.Errorf("domain: outcome: registered carrier %q at %v has no amendment record", pl.Type, k)
}
