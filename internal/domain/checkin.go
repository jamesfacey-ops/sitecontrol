// checkin.go — C8's gate check-in/check-out (chunk-plan C8, design §10/§6).
//
// Two things live here, both deliberately NOT inside trip.go (that file is
// C6's, off limits to this chunk per the build brief):
//
//   - VisitCheckedOut: the explicit gate check-out action. Design §8.1 is
//     unambiguous that this is an ORDINARY OWN-SITE LOG EVENT, not a bare
//     UPDATE — "Check-out and visit corrections replicate as ordinary
//     own-site log events... a correction is a new fact, never an update
//     shipped to a peer." This is DIFFERENT from the implicit checkout a
//     trip_dispatched event already performs on its occupants (trip.go,
//     applyTripDispatched) — that one is a side effect of "this person is now
//     travelling," authored by C6; this one is the front-door "this person
//     is leaving the site" action, authored by C8.
//   - GateCheckIn: the one-form check-in over the degenerate-trip machinery
//     (§10) — resolve/create every person slot on demand, then author the
//     SAME dispatch+arrival pair C6's own forms author. No second mechanism.
package domain

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"sitecontrol/internal/store"
)

// --- check-out: event, projection, authoring ---

type VisitCheckedOut struct {
	Type       string  `json:"type"` // "visit_checked_out"
	VisitID    string  `json:"visit_id"`
	RecordedBy *string `json:"recorded_by,omitempty"`
}

// VisitCheckout is the raw audit row: one per authored checkout event,
// keyed by visit (an already-checked-out visit refuses a second checkout at
// authoring time — CheckOutVisit below — so this table is never more than
// one row per visit in ordinary operation; INSERT OR IGNORE in the fold is
// defensive against a directly-injected duplicate, matching trip_cancel's
// own stance).
type VisitCheckout struct {
	VisitID    string  `json:"visit_id"`
	SiteID     string  `json:"site_id"`
	RecordedBy *string `json:"recorded_by"`
}

func visitCheckoutProjection() store.Projection {
	return store.Projection{
		Name:    "visit_checkout",
		Table:   "visit_checkout",
		OrderBy: "visit_id",
		Schema: `CREATE TABLE IF NOT EXISTS visit_checkout (
			visit_id    TEXT PRIMARY KEY,
			site_id     TEXT NOT NULL,
			recorded_by TEXT
		) STRICT;`,
		Apply: applyVisitCheckedOut,
	}
}

// applyVisitCheckedOut records the raw event — a BARE INSERT into
// visit_checkout. It no longer closes the visit itself.
//
// ⟦SchemaVersion 15, R-20⟧ ROW 28 — FALSIFIED A SECOND TIME. This comment
// used to be the "ROW 28 GUARD": visit.is_open was the one derived value
// mutated incrementally (`UPDATE visit SET is_open = 0 WHERE visit_id = ?
// AND site_id = ? AND is_open = 1` here; predicate closes in trip.go's
// dispatch and arrival folds), "safe because same-site events cannot
// reorder relative to each other". F8 found that false for PEERS; the fix
// constrained DELIVERY (epoch-major order in repl/engine.go's drainSite)
// and this guard was rewritten to say the premise was "true again — by
// construction". It was not: a premise repaired by constraining delivery
// is guarded only by whoever remembers the constraint, and the §8.6
// self-history RETRY that landed later delivers a site's OWN earlier epoch
// after its later one — at the AUTHOR, not a replica, and in the under-
// report direction (D87-D92-COHERENCE.md, measured through the real
// engine). The dependence on order is now REMOVED rather than re-guarded:
// visit.is_open is derived per (site, person) in the visit projection's
// Finalize (visit_open.go) from the raw tables, and this table is one of
// them. A check-out row naming a visit under its own site tag closes that
// visit whatever order the two arrived in; a check-out that lands before
// its visit (dangling) is picked up when the visit's own envelope
// re-derives the pair.
//
// D-87 is kept where it was: the fold stores env.SiteID — THIS event's
// authoring site — and the derivation counts a check-out only when that
// tag equals the visit's, so a foreign site's check-out can never close a
// visit it does not own. Check-out is same-site by design (§8.1:
// "replicate as ordinary own-site log events"), and this is what keeps
// that true structurally rather than by convention.
func applyVisitCheckedOut(tx *sql.Tx, env store.Envelope) error {
	var p VisitCheckedOut
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return fmt.Errorf("domain: checkout fold: %w", err)
	}
	if p.Type != "visit_checked_out" {
		return nil
	}
	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO visit_checkout (visit_id, site_id, recorded_by) VALUES (?, ?, ?)`,
		p.VisitID, env.SiteID, p.RecordedBy,
	); err != nil {
		return fmt.Errorf("domain: checkout fold: insert %s: %w", p.VisitID, err)
	}
	return nil
}

// VisitCheckouts is the raw check-out audit dump — one row per authored
// visit_checked_out event. Exported (U2) so a caller can assert that a
// BATCHED check-out request still produced one event per person: the batch
// is of the request, never of the events.
func VisitCheckouts(st *store.Store) ([]VisitCheckout, error) {
	return dumpSection[VisitCheckout](st, "visit_checkout")
}

// CheckOutVisit authors the gate check-out action (design §8.1). Refused —
// nothing written — if the visit is unknown here, belongs to another site
// (check-out is own-site-authored only), or is already closed.
func CheckOutVisit(st *store.Store, siteID string, nodeEpoch int64, visitID string, recordedBy *string, occurredAtMS int64, timeSource string) error {
	if visitID == "" {
		return fmt.Errorf("domain: check out: visit_id is required")
	}
	v, err := VisitByID(st, visitID)
	if err != nil {
		return fmt.Errorf("domain: check out: %w", err)
	}
	if v == nil {
		return fmt.Errorf("domain: check out: visit %s is not known at this site", visitID)
	}
	if v.SiteID != siteID {
		return fmt.Errorf("domain: check out: visit %s belongs to %s — check-out is site-local, own-site-authored only (§8.1)", visitID, siteNameAt(st, v.SiteID))
	}
	if v.IsOpen != 1 {
		return fmt.Errorf("domain: check out: visit %s is already checked out", visitID)
	}
	if p := endBeforeVisitStart(st, *v, occurredAtMS, timeSource, "check-out"); p != "" {
		return fmt.Errorf("domain: check out: nothing was recorded — %s", p) // FIX17b, sc-stress F12
	}
	payload, err := json.Marshal(VisitCheckedOut{Type: "visit_checked_out", VisitID: visitID, RecordedBy: recordedBy})
	if err != nil {
		return fmt.Errorf("domain: check out: %w", err)
	}
	if _, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource); err != nil {
		return fmt.Errorf("domain: check out: %w", err)
	}
	return nil
}

// CheckOutVisits authors a BATCH of gate check-outs in one request — the
// U2 answer to the measured worst path in design/bulk-movement-v1.md: local
// check-out of 26 people cost 52 server round trips (one POST + one
// redirect-GET per row) because the only entry point took a single
// visit_id.
//
// THIS BATCHES THE REQUEST, NOT THE EVENTS. Every visit still gets its own
// visit_checked_out event through CheckOutVisit above — same payload, same
// per-person accuracy, same replication shape. Nothing about what a peer
// receives changes; only the number of times a browser has to ask.
//
// ALL-OR-NOTHING, ON PURPOSE. Every id is validated (known here, this
// site's, still open) BEFORE anything is authored, and a batch with any bad
// id authors NOTHING and names every problem at once. The direction that
// matters: refusing the whole batch leaves people marked ON SITE who the
// operator meant to check out — the muster over-reports, which wastes
// search time but never hides a live person. Authoring a partial batch and
// reporting "some of these worked" would leave the operator guessing which
// half, and guessing is what this system never does. A batch that refuses
// because one row went stale means the page is stale: reload and re-tick.
//
// A visit id repeated in one batch is REFUSED rather than silently
// deduplicated — no form can produce it, so it means the caller is confused
// about what it is asking for.
func CheckOutVisits(st *store.Store, siteID string, nodeEpoch int64, visitIDs []string, recordedBy *string, occurredAtMS int64, timeSource string) (int, error) {
	at := make([]VisitCheckoutAt, len(visitIDs))
	for i, id := range visitIDs {
		at[i] = VisitCheckoutAt{VisitID: id, OccurredAtMS: occurredAtMS, TimeSource: timeSource}
	}
	return CheckOutVisitsAt(st, siteID, nodeEpoch, at, recordedBy)
}

// VisitCheckoutAt is one person's check-out in a batch, with ITS OWN time
// pair ⟦FIX8, James, 2026-10-05: "check in and out on each entry on
// differing times"⟧. No schema change: every visit already gets its own
// visit_checked_out event, and an event's (OccurredAt, TimeSource) pair is
// its envelope's — so a per-person time is just a per-event pair.
type VisitCheckoutAt struct {
	VisitID      string
	OccurredAtMS int64
	TimeSource   string
}

// CheckOutVisitsAt is CheckOutVisits with a time pair per visit. Same
// all-or-nothing validation, same one-event-per-visit authoring; only the
// time on each event can differ.
func CheckOutVisitsAt(st *store.Store, siteID string, nodeEpoch int64, visits []VisitCheckoutAt, recordedBy *string) (int, error) {
	visitIDs := make([]string, len(visits))
	for i, v := range visits {
		visitIDs[i] = v.VisitID
	}
	if len(visitIDs) == 0 {
		return 0, fmt.Errorf("domain: check out: no visits selected — tick the people who are leaving")
	}
	seen := make(map[string]bool, len(visitIDs))
	var problems, timeProblems []string
	for i, id := range visitIDs {
		switch {
		case id == "":
			problems = append(problems, "an empty visit_id was submitted")
			continue
		case seen[id]:
			problems = append(problems, fmt.Sprintf("visit %s was named twice in one check-out", id))
			continue
		}
		seen[id] = true
		v, err := VisitByID(st, id)
		if err != nil {
			return 0, fmt.Errorf("domain: check out: %w", err)
		}
		switch {
		case v == nil:
			problems = append(problems, fmt.Sprintf("visit %s is not known at this site", id))
		case v.SiteID != siteID:
			problems = append(problems, fmt.Sprintf("visit %s belongs to %s — check-out is site-local, own-site-authored only (§8.1)", id, siteNameAt(st, v.SiteID)))
		case v.IsOpen != 1:
			problems = append(problems, fmt.Sprintf("visit %s is already checked out", id))
		default:
			// FIX17b (sc-stress F12): a typed check-out before the check-in.
			if p := endBeforeVisitStart(st, *v, visits[i].OccurredAtMS, visits[i].TimeSource, "check-out"); p != "" {
				timeProblems = append(timeProblems, p)
			}
		}
	}
	if len(problems) > 0 {
		return 0, fmt.Errorf("domain: check out: nothing was recorded — %s. Reload the on-site list and tick again",
			strings.Join(append(problems, timeProblems...), "; "))
	}
	if len(timeProblems) > 0 {
		return 0, fmt.Errorf("domain: check out: nothing was recorded — %s", strings.Join(timeProblems, "; "))
	}
	for _, v := range visits {
		id := v.VisitID
		if err := CheckOutVisit(st, siteID, nodeEpoch, id, recordedBy, v.OccurredAtMS, v.TimeSource); err != nil {
			// Validation above already cleared every id, so reaching here
			// means the store itself failed mid-batch. Say exactly how far
			// it got rather than implying none of it happened.
			return 0, fmt.Errorf("domain: check out: the batch failed partway through, at visit %s — the check-outs before it ARE recorded, the rest are not; reload the on-site list: %w", id, err)
		}
	}
	return len(visitIDs), nil
}

// --- gate check-in: one form, add-on-demand, the degenerate trip ---

// PersonSlot is one driver/occupant field on the gate check-in form: either
// an EXISTING roster pick (ExistingID, from client-side autocomplete) or a
// NEW off-roster person to add on demand (NewName, plus an optional company
// and trade — left blank, the new person simply has no company/trade on
// file, same as any roster person created without one).
//
// ⟦[[D-84]], 2026-09-05⟧ THE NEW PERSON'S COMPANY AND TRADE NOW ARRIVE THE
// WAY INTAKE'S DO: a PICK from the list (CompanyID / TradeID, an existing
// row's id) OR the add-new escape (NewCompany / NewTrade, a typed name). At
// most one of each pair is set. ⟦was: "NewCompany always mints a FRESH
// company (CreateCompany's own no-dedup stance, §8.7 case 2 — "Kakivik"
// minted twice is sanctioned, not an error); attaching a new person to an
// EXISTING company by search is not built here"⟧ — the escape's typed name
// now RESOLVES to a listed company under the same case/space fold intake's
// pre-check uses, and mints only a genuinely new one (resolveOrCreate
// CompanyByName, below). D-84's shape is "a folded duplicate answers
// 'choose it from the list'"; at the gate, which never refuses ([[D-66]]),
// the answer is to resolve to it — exactly what [[D-76]] did for trades.
type PersonSlot struct {
	ExistingID string
	NewName    string
	// CompanyID / TradeID: the picked EXISTING row, for a NEW person.
	CompanyID string
	TradeID   string
	// NewCompany / NewTrade: the add-new escape's typed name, for a NEW
	// person. Resolved by fold to a listed row first; minted only if none.
	NewCompany string
	NewTrade   string
	// LastName / FirstName / Position / Phone ⟦gate-checkin-spec, James,
	// 2026-09-13: "last name, first name, company, position, phone number"⟧
	// are the gate's open-box fields for a NEW person, empty meaning absent —
	// the same convention CompanyID/TradeID above already use. D-52 still
	// governs REQUIREDNESS, not availability: none of these are required
	// here, so the gate's edge still asks for nothing MANDATORY; it merely
	// carries what the officer chose to type, exactly as web/intake.go's
	// Last/First/Phone boxes already do for the OTHER creation door. See
	// resolvePersonSlot, which composes these into a PersonDetails only when
	// minting a new person — an all-empty PersonSlot here produces the exact
	// PersonDetails{} CreatePersonRecordedBy always passed, so a submission
	// that never uses these fields authors byte-for-byte what it did before.
	LastName  string
	FirstName string
	Position  string
	Phone     string
}

func (s PersonSlot) isNew() bool { return s.ExistingID == "" }

// resolvePersonSlot turns one form slot into a person id, creating
// company/trade/person on demand through the ORDINARY authoring path —
// CreateCompanyRecordedBy/CreateTradeRecordedBy/CreatePersonRecordedBy,
// the attributed siblings of the functions any other caller uses (chunk-plan
// M6: "must genuinely create the person through the ordinary authoring path
// — not a special-cased side write"). recordedBy is threaded through from
// GateCheckInInput (F2): this is THE creation site that matters — a person
// added here can land on an evacuation muster, so who added them must be on
// the row, same nullable contract as everywhere else (nil if no officer is
// picked, never a fabricated placeholder). An EXISTING slot is checked
// against the current roster so a stale/tampered id refuses cleanly instead
// of authoring a dangling reference.
//
// THE CO-MINT RULE (TP §4.8 row 1's design ruling; criterion W2-8): every
// reference event minted here — person_created, company_created,
// trade_created — carries `default`/now with rule W's equality, NEVER
// the caller's entered arrival time. "The record's creation occurred at
// the keyboard; the person's arrival occurred earlier" — wiring the
// entered time onto a record-creation event is false provenance on a
// co-minted event (mutant m4). GateCheckIn's (occurredAtMS, timeSource)
// pair therefore deliberately does NOT reach this function: the entered
// value goes to the arrival pair and nowhere else.
//
// hasOrientation ⟦R-12, 2026-09-10⟧ is THIS SITE's own config.HasOrientation(),
// the same flag GateCheckInInput already carries for the arrival split. At an
// orientation post the add-on-demand branch below is REFUSED before it mints
// anything (GateNewPersonRefusal): James — "no one will be placed in the
// system without the oriantation" — so a person is created ONLY by the
// enrolment that records their first orientation (web/intake.go), never by a
// gate slot that would then hold the freshly-minted record out. At a road
// post (false) the branch is unchanged: nobody owes an orientation there
// (D-88) and the gate keeps adding whoever is standing at it (D-66, R-9).
// validatePersonSlotWithoutMinting is resolvePersonSlot's READ-ONLY half,
// pulled out so GateCheckIn (below) can run it — for every slot — before it
// resolves the vehicle or mints anything at all. It runs every check
// resolvePersonSlot runs before its first possible Author call (existing-id
// lookup, blank-name, the R-12 orientation refusal, and the picked-not-typed
// company/trade existence checks) and stops there: it never reaches
// resolveOrCreateCompanyByName, resolveOrCreateTradeByName or
// CreatePersonRecordedBy, so calling it can never mint anything.
//
// ⟦comint-on-refusal, 2026-09-11⟧ WHY THIS EXISTS: reordering GateCheckIn to
// resolve the vehicle before persons (see resolveVehicleIDBeforeMinting)
// fixed the reported defect (a refused vehicle no longer leaves an orphan
// person+company) but, on its own, TRADES it for the same defect the other
// way round — a blank driver or an off-roster name at an orientation post
// would then refuse AFTER the vehicle had already been minted
// (TestARefusedPostAuthorsNothing and
// TestGateAddOnDemandIsRefusedAtAnOrientationPostAndAuthorsNothing). Running every person-side check that can be done
// WITHOUT minting, for every slot, before the vehicle is touched at all,
// closes both directions: nothing mints — vehicle, company, trade, or person
// — until every routine refusal reason this door has has already been ruled
// out. What is NOT covered: a genuine race between this check and the mint
// pass immediately below it (a company deleted, a person removed, a second
// officer's concurrent request) — same-request events are not
// interleaved by anything else in this process, so this is a same-class,
// much narrower residual than the reported one, not a new one.
func validatePersonSlotWithoutMinting(st *store.Store, siteID string, slot PersonSlot, hasOrientation bool) error {
	if !slot.isNew() {
		p, err := PersonByID(st, slot.ExistingID)
		if err != nil {
			return fmt.Errorf("domain: gate check-in: %w", err)
		}
		if p == nil {
			return fmt.Errorf("domain: gate check-in: person %s is not on the roster", slot.ExistingID)
		}
		return nil
	}
	if slot.NewName == "" {
		return fmt.Errorf("domain: gate check-in: a person field was left blank")
	}
	if hasOrientation {
		return &GateNewPersonRefusal{Name: slot.NewName, SiteID: siteID, SiteName: siteNameAt(st, siteID)}
	}
	if slot.CompanyID != "" {
		c, err := CompanyByID(st, slot.CompanyID)
		if err != nil {
			return fmt.Errorf("domain: gate check-in: %w", err)
		}
		if c == nil {
			return fmt.Errorf("domain: gate check-in: company %s is not on this site's list", slot.CompanyID)
		}
	}
	if slot.TradeID != "" {
		w, err := TradeByID(st, slot.TradeID)
		if err != nil {
			return fmt.Errorf("domain: gate check-in: %w", err)
		}
		if w == nil {
			return fmt.Errorf("domain: gate check-in: trade %s is not in the trade register", slot.TradeID)
		}
	}
	return nil
}

// RetiredNameKey is the ONE comparison key for "does this name match a
// RETIRED record" — R6's commit-time check (refuseRetiredNamesake) and the
// gate's typed-name refusal (web/gate.go resolveTypedPersonDir) both call it,
// so the two surfaces cannot disagree about which spellings are the retired
// name.
//
// James, 2026-10-04 (Q-FG1): the retired-name check normalises whitespace
// and comma spacing. ⟦was: exact after trim and case-fold only — "Ashton,  Joss"
// (double space) minted a duplicate that DISPLAYS identically to the retired
// "Ashton, Joss", because HTML collapses the run; "Ashton,Joss" and "Ashton
// Joss" minted too (L12-2)⟧. A comma is treated as a separator like a space,
// so "Ashton, Joss" = "Ashton,Joss" = "Ashton,  Joss" = "Ashton Joss"; runs
// collapse, the ends trim, and case folds. Word ORDER is kept: "Joss Ashton"
// stays a different key, exactly as the typed-name resolver treats it — a
// swap is a different spelling the officer can see, not an invisible one.
func RetiredNameKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(name, ",", " ")), " "))
}

// RetiredNamesake returns the RETIRED roster record that name matches under
// RetiredNameKey, or nil. It is nil whenever a LIVE record carries the name
// exactly (trimmed, case-insensitively — the typed-name resolver's own
// adoption rule): a decline against a live, selectable namesake is the
// design of 9f0623b and still mints. A live record that only matches under
// the looser key does NOT lift the refusal — the officer typed the retired
// record's own spelling, and the live one is offered by its own spelling.
func RetiredNamesake(persons []Person, name string) *Person {
	key := RetiredNameKey(name)
	if key == "" {
		return nil
	}
	exact := strings.TrimSpace(name)
	var hit *Person
	for i := range persons {
		p := &persons[i]
		if !p.IsRetired() {
			if strings.EqualFold(strings.TrimSpace(p.Name), exact) {
				return nil
			}
			continue
		}
		if hit == nil && RetiredNameKey(p.Name) == key {
			hit = p
		}
	}
	return hit
}

// RetiredNameRefusal is THE retired-name refusal (L02, R6, F12): one
// sentence, whichever door refused — the gate's typed-name resolver, R6's
// commit-time re-check of a stale decline, or the backstop for a stale
// picked id. Error() is the officer's sentence — name only, never the id
// (L02, 2026-10-04: the id meant nothing to the officer reading it).
// PersonID rides for the amend LINK the gate forms render beside it.
//
// ⟦F12, James, 2026-10-04: the L02 amend link must appear on EVERY
// retired-name refusal surface⟧ ⟦was: refuseRetiredNamesake returned a plain
// error with a second, different sentence, so the JS-on vehicle door and the
// stale-decline refusal showed no link — the web layer's typed refusal was
// bypassed whenever the row arrived as new_kind=new (L12-5)⟧.
type RetiredNameRefusal struct {
	Typed    string
	PersonID string
}

func (e *RetiredNameRefusal) Error() string {
	return fmt.Sprintf("%q is on the roster but has been RETIRED, so this entry does not check them in and will not add a second record for the name. If they are back, un-retire them on the amend page (clear retired) and check them in again", e.Typed)
}

// refuseRetiredNamesake is R6's commit-time check (see resolvePersonSlot):
// a *RetiredNameRefusal when name matches a RETIRED person under
// RetiredNameKey and no live one carries it exactly (RetiredNamesake).
func refuseRetiredNamesake(st *store.Store, name string) error {
	name = strings.TrimSpace(name)
	persons, err := Persons(st)
	if err != nil {
		return fmt.Errorf("domain: gate check-in: %w", err)
	}
	if p := RetiredNamesake(persons, name); p != nil {
		return &RetiredNameRefusal{Typed: name, PersonID: p.PersonID}
	}
	return nil
}

func resolvePersonSlot(st *store.Store, siteID string, nodeEpoch int64, slot PersonSlot, recordedBy *string, hasOrientation bool) (string, error) {
	if !slot.isNew() {
		// Point lookup (F30) — one PK read per slot, not a roster dump per
		// check-in.
		p, err := PersonByID(st, slot.ExistingID)
		if err != nil {
			return "", fmt.Errorf("domain: gate check-in: %w", err)
		}
		if p == nil {
			return "", fmt.Errorf("domain: gate check-in: person %s is not on the roster", slot.ExistingID)
		}
		// ⟦R-118 / [[D-21.5]], 2026-10-03⟧ a RETIRED record is not selectable
		// for a new check-in. The pickers no longer offer it and the typed-
		// name resolver no longer adopts it (web/gate.go); this is the
		// backstop for a stale page or a crafted id, so the rule holds at the
		// authoring edge whatever surface fed it. Pre-append: nothing is
		// minted. The remedy is the amend page's Clear of `retired`, which is
		// D-21.5's "reversible". Presence paths — arrivals off a trip, the
		// resume door — never pass through this slot and are untouched.
		if p.IsRetired() {
			// F12: the same typed refusal (and so the same sentence and
			// amend link) as every other retired-name door.
			return "", &RetiredNameRefusal{Typed: p.Name, PersonID: p.PersonID}
		}
		return slot.ExistingID, nil
	}
	if slot.NewName == "" {
		return "", fmt.Errorf("domain: gate check-in: a person field was left blank")
	}
	// ⟦R6, James, 2026-10-04: re-check retired at commit and refuse the
	// decline, so no namesake is minted⟧ A "No — this is a different person"
	// decline (or an add-new) from a tab opened BEFORE the namesake was
	// retired arrives as a NEW name, so the typed-name resolver's retired
	// refusal never saw it and the commit minted a second record under the
	// retired name — the very duplicate D-21.5's retirement exists to clear
	// up. Checked here, at the commit, against the roster as it is now: a new
	// name that matches a retired record under RetiredNameKey (Q-FG1) and
	// that NO live record carries exactly is refused. A live namesake still lets a decline through (it was declined
	// against a person who is still selectable — the design of 9f0623b).
	// Pre-append, before any company/trade co-mint: nothing is minted.
	if err := refuseRetiredNamesake(st, slot.NewName); err != nil {
		return "", err
	}
	// ⟦R-12⟧ FIRST, before any company/trade co-mint: the refusal must leave
	// no trace — a company row minted for a person who was then refused is a
	// duplicate heading on the muster sheet nobody asked for (intake.go's
	// own rule, applied to this door).
	if hasOrientation {
		return "", &GateNewPersonRefusal{Name: slot.NewName, SiteID: siteID, SiteName: siteNameAt(st, siteID)}
	}
	var companyID, tradeID *string
	// ⟦[[D-84]]⟧ A PICKED company is an existing row and is checked to be one
	// (a stale/tampered id refuses cleanly instead of authoring a dangling
	// reference — the same posture the ExistingID branch above takes).
	if slot.CompanyID != "" {
		c, err := CompanyByID(st, slot.CompanyID)
		if err != nil {
			return "", fmt.Errorf("domain: gate check-in: %w", err)
		}
		if c == nil {
			return "", fmt.Errorf("domain: gate check-in: company %s is not on this site's list", slot.CompanyID)
		}
		id := slot.CompanyID
		companyID = &id
	} else if slot.NewCompany != "" {
		id, err := resolveOrCreateCompanyByName(st, siteID, nodeEpoch, slot.NewCompany, recordedBy)
		if err != nil {
			return "", fmt.Errorf("domain: gate check-in: add-on-demand company: %w", err)
		}
		companyID = &id
	}
	// ⚠⚠ THE ADD-IT ESCAPE, AND TWO THINGS CHANGED HERE AT W8. Both are
	// [[D-71]] obligation 2 — "the escape MUST NOT REFUSE" — and both were
	// live defects, not tidy-ups.
	//
	//  1. IT IS NO LONGER NESTED INSIDE THE NEW-COMPANY BRANCH. A trade is
	//     not scoped to a company ⟦DR-W8-2⟧, so an officer adding a trade for
	//     a person whose company is ALREADY on the list had no way through at
	//     all: the branch simply never ran and the typed trade was dropped
	//     silently.
	//  2. A NAME ALREADY ON THE LIST RESOLVES TO IT INSTEAD OF REFUSING.
	//     CreateTrade refuses a duplicate name (site-wide uniqueness,
	//     ⟦DR-W8-11⟧), and a refusal here FAILED THE WHOLE CHECK-IN — the
	//     mutant (m8-74) names exactly this. On a gate at −40°, an officer
	//     who types a trade that is already known must not be told to start
	//     again. The person is created, tagged to the EXISTING trade, and
	//     EXACTLY ONE event is appended.
	//
	// ⚠ [[D-66]]: the gate never refuses anyone. The escape's job is to get
	// the person onto the roster, and the trade is a default on their file —
	// never a gate.
	if slot.TradeID != "" {
		// ⟦[[D-84]]⟧ the PICKED trade — W8-23(c)'s "the officer is offered
		// the trade list" finally has a pick to carry. Checked to exist; a
		// merged-away row is accepted and resolves at the read, exactly as
		// AmendPerson's unknown-trade limb accepts one.
		w, err := TradeByID(st, slot.TradeID)
		if err != nil {
			return "", fmt.Errorf("domain: gate check-in: %w", err)
		}
		if w == nil {
			return "", fmt.Errorf("domain: gate check-in: trade %s is not in the trade register", slot.TradeID)
		}
		id := slot.TradeID
		tradeID = &id
	} else if slot.NewTrade != "" {
		id, err := resolveOrCreateTradeByName(st, siteID, nodeEpoch, slot.NewTrade, recordedBy)
		if err != nil {
			return "", fmt.Errorf("domain: gate check-in: add-on-demand trade: %w", err)
		}
		tradeID = &id
	}
	// ⟦gate-checkin-spec, 2026-09-13⟧ CreatePersonWithDetails with an
	// all-empty PersonDetails is CreatePersonRecordedBy — see createPerson,
	// which both wrappers call with PersonDetails{} today. Building the
	// struct from slot's (possibly all-empty) new fields is therefore not a
	// behaviour change for any caller that never sets them; it only starts
	// carrying facts the open box now collects. D-52 is unmoved: nothing
	// here REQUIRES any of the four, so the gate's edge still asks for
	// nothing mandatory.
	details := PersonDetails{}
	if slot.LastName != "" {
		v := slot.LastName
		details.LastName = &v
	}
	if slot.FirstName != "" {
		v := slot.FirstName
		details.FirstName = &v
	}
	if slot.Position != "" {
		v := slot.Position
		details.Position = &v
	}
	if slot.Phone != "" {
		v := slot.Phone
		details.Phone = &v
	}
	p, err := CreatePersonWithDetails(st, siteID, nodeEpoch, slot.NewName, companyID, tradeID, recordedBy, details, 0, "default")
	if err != nil {
		return "", fmt.Errorf("domain: gate check-in: add-on-demand person: %w", err)
	}
	return p.PersonID, nil
}

// GateNewPersonRefusal ⟦R-12, 2026-09-10⟧ is the pre-append refusal of an
// off-roster name at a post that administers an orientation. Typed rather
// than a message (orientation.go's OrientationRefusal, the house rule) so
// web/gate.go composes the officer's sentence with this post's DISPLAY name
// and the next action; the Error() text is the log's, not the screen's.
// NOTHING is authored on it — no person, no company, no trade, no vehicle.
type GateNewPersonRefusal struct {
	Name   string // the typed, off-roster name
	SiteID string // this post — the site whose orientation nobody has recorded for them
	// SiteName is SiteID as the officer reads it (display name); empty falls back to SiteLabel.
	SiteName string
}

func (e *GateNewPersonRefusal) Error() string {
	return fmt.Sprintf("domain: gate check-in: %q is not on the roster and %s administers an orientation — nobody is added to the system without one (R-12); enrol them at intake, which records it, then check them in; nothing was recorded", e.Name, func() string {
		if e.SiteName != "" {
			return e.SiteName
		}
		return SiteLabel(e.SiteID)
	}())
}

// sameCompanyName is the gate escape's "these two spellings are one company"
// rule ⟦[[D-84]]⟧: case and leading/trailing whitespace fold, nothing else —
// the SAME comparison intake's own add-a-company pre-check makes
// (web/intake.go) before it refuses a duplicate with "pick it there rather
// than adding a second one". createCompany itself has NO duplicate check
// (refdata.go's stance, unchanged here); this rule lives at the gate's
// resolve step only, so the gate resolves what intake would refuse and
// neither door mints a second spelling of a listed company.
func sameCompanyName(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// resolveOrCreateCompanyByName is the add-new escape's resolution step for a
// COMPANY, the sibling of resolveOrCreateTradeByName below and built for the
// same reason: with a picker on the gate ([[D-84]]), "not listed — add a new
// company" followed by the name of a company that IS listed is the
// gloves-on-at-04:10 path [[D-71]] describes, and the answer is the listed
// row — never a refusal ([[D-66]]) and never a second row with the same
// heading on the muster sheet. A name already on the list resolves to THAT
// company (through the merge snapshot) and authors NOTHING; a name that is
// not is created, authoring exactly one `company_created`.
func resolveOrCreateCompanyByName(st *store.Store, siteID string, nodeEpoch int64, name string, recordedBy *string) (string, error) {
	existing, err := Companies(st)
	if err != nil {
		return "", err
	}
	for _, c := range existing {
		if !sameCompanyName(c.Name, name) {
			continue
		}
		m, err := LoadCompanyMergeMap(st)
		if err != nil {
			return "", err
		}
		return m.Resolve(c.CompanyID), nil
	}
	created, err := CreateCompanyRecordedBy(st, siteID, nodeEpoch, name, recordedBy, 0, "default")
	if err != nil {
		return "", err
	}
	return created.CompanyID, nil
}

// --- [[D-84]]: a confirmed gate-side correction to a KNOWN person's record ---

// PersonRecordChange is one officer-CONFIRMED change to an already-enrolled
// person's company and/or trade, made at the gate ([[D-84]] and its
// amendment: "Their record — from now on"). Each half names at most one of:
// an existing row to set (CompanyID / TradeID), a clear (ClearCompany /
// ClearTrade), or the add-new escape's typed name (NewCompany / NewTrade),
// which resolves by fold to a listed row first and mints only a genuinely
// new one.
//
// ⚠ It is applied through AmendPerson — the ORDINARY person amendment edge —
// so it authors a replicated `person_amended` event, reaches the peer, and
// composes for every future check-in and muster heading exactly like an
// amendment made on /person/amend. It is not a local edit and not a second
// mechanism. The confirmation that protects it is the web layer's; nothing
// here re-checks it, because the domain cannot see the checkbox.
type PersonRecordChange struct {
	PersonID     string
	CompanyID    string
	ClearCompany bool
	NewCompany   string
	TradeID      string
	ClearTrade   bool
	NewTrade     string
	// Position / Phone ⟦gate-checkin-spec, 2026-09-13⟧: unlike Company/Trade
	// these are free text with no fold-by-name resolution or minting — a
	// value here is applied to the record verbatim, through the SAME
	// confirm-tick shape D-84 already gives Company/Trade (the web layer's
	// AmendConfirmed; the domain still cannot see the checkbox).
	Position      string
	ClearPosition bool
	Phone         string
	ClearPhone    bool
}

// ApplyPersonRecordChanges authors each change in turn. A change whose
// result equals the record as it stands authors NOTHING — AmendPerson
// refuses a no-op amendment, and "confirmed, but already so" must be silent
// rather than refused (a refused commit re-renders the form with the record
// already updated and the tick still set, and the retry must go through).
//
// ⚠ ORDERING IS THE CONTRACT ([[D-84]] amendment): the caller applies these
// BEFORE the visit is authored, so buildArrivalVisits snapshots the AMENDED
// record — "the amendment updates the record, and the visit follows". No
// path lets a visit carry a value the record does not have.
func ApplyPersonRecordChanges(st *store.Store, siteID string, nodeEpoch int64, changes []PersonRecordChange, recordedBy *string) error {
	for _, ch := range changes {
		if err := applyPersonRecordChange(st, siteID, nodeEpoch, ch, recordedBy); err != nil {
			return err
		}
	}
	return nil
}

// stringFactChanged reports whether a proposed free-text fact differs from
// the record's current optional value. Deliberately generic — no field name
// in it — so it carries no taxonomy a scan could key on: it is a CHANGE
// check ("is the proposed value different from what is on file"), never a
// branch on WHAT the content IS, which is the distinction D-21.4/W5b draw
// for Position and Phone ("nullable, free text — no enum, no validation
// against a list, no branch on its content").
func stringFactChanged(cur *string, next string) bool {
	return cur == nil || *cur != next
}

func applyPersonRecordChange(st *store.Store, siteID string, nodeEpoch int64, ch PersonRecordChange, recordedBy *string) error {
	p, err := PersonByID(st, ch.PersonID)
	if err != nil {
		return fmt.Errorf("domain: gate record change: %w", err)
	}
	if p == nil {
		return fmt.Errorf("domain: gate record change: person %s is not on the roster", ch.PersonID)
	}
	in := PersonAmendment{PersonID: ch.PersonID}
	switch {
	case ch.NewCompany != "":
		id, err := resolveOrCreateCompanyByName(st, siteID, nodeEpoch, ch.NewCompany, recordedBy)
		if err != nil {
			return fmt.Errorf("domain: gate record change: company: %w", err)
		}
		if p.CompanyID == nil || *p.CompanyID != id {
			in.CompanyID = &id
		}
	case ch.CompanyID != "":
		c, err := CompanyByID(st, ch.CompanyID)
		if err != nil {
			return fmt.Errorf("domain: gate record change: %w", err)
		}
		if c == nil {
			return fmt.Errorf("domain: gate record change: company %s is not on this site's list", ch.CompanyID)
		}
		if p.CompanyID == nil || *p.CompanyID != ch.CompanyID {
			id := ch.CompanyID
			in.CompanyID = &id
		}
	case ch.ClearCompany:
		if p.CompanyID != nil {
			in.Clear = append(in.Clear, "company_id")
		}
	}
	switch {
	case ch.NewTrade != "":
		id, err := resolveOrCreateTradeByName(st, siteID, nodeEpoch, ch.NewTrade, recordedBy)
		if err != nil {
			return fmt.Errorf("domain: gate record change: trade: %w", err)
		}
		if p.TradeID == nil || *p.TradeID != id {
			in.TradeID = &id
		}
	case ch.TradeID != "":
		// Existence is AmendPerson's own unknown-trade-id limb; not repeated.
		if p.TradeID == nil || *p.TradeID != ch.TradeID {
			id := ch.TradeID
			in.TradeID = &id
		}
	case ch.ClearTrade:
		if p.TradeID != nil {
			in.Clear = append(in.Clear, "trade_id")
		}
	}
	// POSITION / PHONE ⟦gate-checkin-spec, 2026-09-13⟧: free text, applied
	// verbatim — no fold-by-name resolution, unlike Company/Trade above. The
	// "did it change" check goes through stringFactChanged rather than a
	// direct *p.Position != ch.Position comparison written here: D-21.4/W5b
	// rule Position and Phone free text with NO TAXONOMY, and
	// TestW5aPositionIsFreeTextWithNoTaxonomy's scan cannot structurally
	// distinguish an enum/allow-list test spelled as an equality compare
	// from a plain "is this different from the proposed value" check — both
	// are `<Position-bearing expr> != <some other variable>` in the AST. A
	// generic helper with no field-specific name in it is the fix the
	// scan's OWN measured-correction story already points at (it went broad
	// specifically because a narrow, literal-only rule missed a
	// loop-variable-shaped enum test) — see stringFactChanged.
	switch {
	case ch.Position != "":
		if stringFactChanged(p.Position, ch.Position) {
			v := ch.Position
			in.Position = &v
		}
	case ch.ClearPosition:
		if p.Position != nil {
			in.Clear = append(in.Clear, "position")
		}
	}
	switch {
	case ch.Phone != "":
		if stringFactChanged(p.Phone, ch.Phone) {
			v := ch.Phone
			in.Phone = &v
		}
	case ch.ClearPhone:
		if p.Phone != nil {
			in.Clear = append(in.Clear, "phone")
		}
	}
	if in.CompanyID == nil && in.TradeID == nil && in.Position == nil && in.Phone == nil && len(in.Clear) == 0 {
		return nil // already so — nothing to author
	}
	if _, err := AmendPerson(st, siteID, nodeEpoch, in, 0, "default"); err != nil {
		return fmt.Errorf("domain: gate record change: %w", err)
	}
	return nil
}

// resolveOrCreateTradeByName is the add-it escape's own resolution step, and
// the "resolve" half is what makes the escape an escape rather than a second
// way to be refused.
//
// A name already on this site's list resolves to THAT trade and authors
// NOTHING; a name that is not is created, authoring exactly one
// `trade_created`. Either way the caller gets a trade id and the check-in
// continues.
//
// ⚠ THE MATCH IS ON THE COMPOSED NAME, not the created-state row: a trade
// renamed by amendment answers to its current name, which is the name the
// officer is reading off the same list. ⚠ It resolves through the MERGE
// SNAPSHOT too, so typing the name of a trade that has been merged away tags
// the person to the canonical one rather than reviving the duplicate.
func resolveOrCreateTradeByName(st *store.Store, siteID string, nodeEpoch int64, name string, recordedBy *string) (string, error) {
	existing, err := Trades(st)
	if err != nil {
		return "", err
	}
	// [[D-76]]: the match FOLDS case and leading/trailing whitespace through
	// sameTradeName — the SAME rule createTrade refuses by. It has to be the
	// same rule: if the resolve step matched exactly while the create step
	// folded, "welder" would slip past this loop and be REFUSED below, which
	// is the [[D-66]] break (MEASURED while building the D-76 witness: the
	// check-in failed with the duplicate error). Folding here is D-66 working
	// better — more spellings resolve, none refuse, none mint.
	for _, tr := range existing {
		if !sameTradeName(tr.Name, name) {
			continue
		}
		m, err := LoadTradeMergeMap(st)
		if err != nil {
			return "", err
		}
		return m.Resolve(tr.TradeID), nil
	}
	created, err := CreateTradeRecordedBy(st, siteID, nodeEpoch, name, recordedBy, 0, "default")
	if err != nil {
		return "", err
	}
	return created.TradeID, nil
}

// GateCheckInInput is the C8 gate check-in form's whole content.
//
// ⟦W7-4(d)(i), 2026-09-03⟧ ⟦was: "VehicleEntered string"⟧ — THE FOURTH
// AUTHORING DOOR, and the busiest operator path in the application. DR-W7-4
// makes it name a vehicle RECORD like the other three: the number is typed
// once, when the record is created (the surface co-mints it — see
// web/gate.go), and `trip.vehicle_entered` is derived from the record.
// A build that converted the other three and left this one would pass a
// three-door fixture set in full while `POST /gate/checkin` still took a
// string, which is why TestW7GateCheckInInputTakesAVehicleIDNotAString exists.
type GateCheckInInput struct {
	VehicleID string
	// ⟦TRIAL — §97/§107, 2026-09-03⟧ ResolveVehicle mints-or-resolves the vehicle record, and it is
	// passed STRAIGHT THROUGH to `DispatchLocalCheckIn` so the mint happens inside `dispatchTrip`,
	// after that door's own validations. ⭐ ONE MECHANISM REUSED, not a second one: this door does
	// not resolve anything itself.
	// ⚠ A func, not a string — `TestW7GateCheckInInputTakesAVehicleIDNotAString` is a REFLECTION
	// census over this struct and bars a free-text vehicle STRING under any spelling. Checked, not
	// assumed: it tests `reflect.String`, which a func is not.
	ResolveVehicle func() (string, error)
	// VehicleUnidentified (SchemaVersion 13, R-17): see DispatchInput's own
	// field — passed straight through to DispatchLocalCheckIn, which is the
	// choke point that actually enforces it. This door does not enforce its
	// own copy of the rule beyond resolveVehicleIDBeforeMinting's early
	// mirror (below), kept in sync by hand with dispatchTrip's, the same
	// posture that function's own comment already documents for the rest of
	// the refusal.
	VehicleUnidentified bool
	Driver              PersonSlot
	Occupants           []PersonSlot
	RecordedBy          *string // C11: the session officer, or nil if none picked yet (§10)
	// RecordChanges ⟦[[D-84]]⟧: the officer-CONFIRMED corrections to KNOWN
	// people's company/trade, applied through AmendPerson AFTER the slots
	// resolve and BEFORE the arrival is authored — so the visit snapshots
	// the amended record. See ApplyPersonRecordChanges.
	RecordChanges []PersonRecordChange
	// HasOrientation ⟦gate-orientation-block, 2026-09-09⟧ is THIS SITE's own
	// config.HasOrientation(), handed straight through to RecordExpectedArrival
	// so buildArrivalVisits splits this vehicle's occupants exactly as the two
	// trip-arrival doors already do (D-98/D-91, SCHEMA-11 step 5): an occupant
	// with no orientation row for this site is aboard-but-NOT-entered — no
	// visit, a trip_arrival_not_entered row — and the handler tells the
	// officer so, by name, with the next action (web/notentered.go). James,
	// 2026-09-09: "they will not be allowed on site untill oriantation is
	// completed". There is no override and none is to be built. The zero
	// value is the road-post behaviour and what every pre-existing caller
	// gets: everyone enters. Never a peer's flag, never the vehicle's origin
	// (orientation.go: "the site is the envelope's, never the payload's").
	HasOrientation bool
	// OrientationsToComplete ⟦D3 fix, WINDOW-AUDIT.md⟧: person ids (already
	// resolved to an EXISTING roster record — R-12 refuses any new name at
	// an orientation post before this door ever reaches this field) whose
	// officer ticked the cross-site warning's "record this site's
	// orientation now" box. Authored INSIDE this door, once every refusal
	// GateCheckIn itself can produce has been ruled out — see the call site
	// in GateCheckIn for why. The caller's own row-identity resolution
	// (staleness across an accumulate form, "which posted ids actually name
	// a slot on this vehicle") stays the caller's job; this field carries
	// only the ids that survived it.
	OrientationsToComplete []string
}

// GateCheckIn is the one-form check-in over the degenerate-trip machinery
// (design §10): resolve/create every person slot, then author a degenerate
// TripDispatched (no destination, no deadline) immediately followed by its
// own TripArrival — EXACTLY the mechanism C6 built for a real inbound trip,
// per §10's "no second mechanism" rule. TripDispatched's own check-out side
// effect (trip.go, applyTripDispatched: closes any of this occupant's OTHER
// open visits AT THIS SITE) is usually a no-op here — a fresh local check-in
// has no prior open trip of these occupants to close — BUT NOT ALWAYS: if an
// occupant already holds an open visit here (an operator re-running an
// already on-site person through the gate form — the routine stale-visit
// correction, not a mis-click, and benign on its own), authoring Dispatch
// below closes THAT prior visit before RecordExpectedArrival below opens its
// replacement (F1).
//
// CRASH WINDOW (chunk-carry-forward row 31; text corrected per F1's
// adversarial verification): these are unavoidably TWO separate store.Author
// calls, each its own committed transaction — store.Author authors exactly
// one event per call, and internal/store/log.go is off limits to this chunk
// (see build report for why a shared-transaction atomic author was not
// added). If the process dies between the two calls, the trip is left OPEN,
// degenerate, and never-arrived — and because Dispatch's own fold
// UNCONDITIONALLY closes any pre-existing open visit for these occupants at
// this site (the paragraph above), the crash loss is not merely "a new
// arrival not yet counted": if any occupant was already validly,
// confirmedly on site before this check-in began, that prior visit is gone
// too, and the muster is short exactly them until the check-in is resumed.
// This chunk's chosen strategy is VISIBLE + RECOVERABLE, not atomic:
// IncompleteCheckIns below surfaces exactly this state instead of it
// vanishing as a silent orphan — now on BOTH /gate and /muster, including
// the printed muster sheet (F1) — and ResumeCheckIn completes it by
// re-running ONLY the arrival half — never by re-running Dispatch, which
// would double-author the trip.
//
// ⟦A3, 2026-09-29⟧ THE SHARED-TRANSACTION AUTHOR NOW EXISTS: store.Transact
// (internal/store/transact.go) hands a caller a Store view on which every
// Author joins ONE transaction. This function is unchanged — it still makes
// two Author calls, and called on a plain Store they are still two commits
// with the window above between them — but BOTH web doors that reach it
// (commitGateCheckIn and legacyGateCheckIn, web/gate.go) now call it inside
// Transact, so at a live post the trip and its arrival commit together and
// a process death between them leaves NOTHING rather than a half-finished
// check-in. The recovery path is deliberately kept: IncompleteCheckIns and
// ResumeCheckIn still list and complete a degenerate trip with no arrival,
// because such trips already exist on live logs, replicate from other
// posts' histories, and remain reachable by any caller that authors on a
// plain Store (every test fixture that reproduces the crash shape does
// exactly that). What changes is only that the web doors can no longer
// CREATE the state.
func GateCheckIn(st *store.Store, siteID string, nodeEpoch int64, in GateCheckInInput, occurredAtMS int64, timeSource string) (Trip, Arrival, error) {
	// ⟦comint-on-refusal, 2026-09-11⟧ NOTHING MINTS UNTIL EVERY SLOT AND THE
	// VEHICLE HAVE BOTH BEEN VALIDATED. Reported live (DEMO-PAIR-UPDATE.md,
	// outer docs repo commit 4a55bc5): an empty vehicle box refused the
	// check-in "vehicle id is required" AFTER a brand-new person and company
	// had already been authored (resolvePersonSlot's add-on-demand mint ran
	// unconditionally, before this door ever reached dispatchTrip's own
	// deferred vehicle co-mint — §97/§107 only ever protected the vehicle
	// side), and a retry minted a SECOND roster row for the same human.
	// resolvePersonSlot's own R-12 comment already states the principle this
	// violated — "the refusal must leave no trace" — it just never covered
	// this refusal source.
	//
	// The fix is two passes, not a reorder: reordering alone (mint the
	// vehicle first) just moves the same defect onto the vehicle — a blank
	// driver or an off-roster name at an orientation post would then refuse
	// AFTER the vehicle had minted (caught by TestARefusedPostAuthorsNothing
	// and TestGateAddOnDemandIsRefusedAtAnOrientationPostAndAuthorsNothing).
	// So: validate every person slot WITHOUT minting (existing-id lookup,
	// blank name, the R-12 orientation refusal, picked-company/trade
	// existence — see validatePersonSlotWithoutMinting), THEN resolve the
	// vehicle (its own mint, deferred here exactly as dispatchTrip defers it
	// for a real departure), and only once BOTH have cleared does the real
	// resolvePersonSlot pass below run, minting whatever add-on-demand
	// company/trade/person a slot still needs. This does not touch
	// dispatchTrip/Dispatch (the real-departure path): its own vehicle
	// deferral, its pinned refusal text, and
	// TestS97DispatchStillRefusesWhenNoVehicleIDIsEverProduced are
	// unchanged — resolveVehicleIDBeforeMinting reuses the identical
	// check-and-message rather than a second one.
	//
	// ⚠ RESIDUAL, NARROWER THAN THE REPORTED DEFECT: a genuine race between
	// this validation pass and the mint pass immediately below it (a company
	// or person deleted, or a stale roster id, from a DIFFERENT concurrent
	// request) is not covered — same-request events are not interleaved by
	// anything else in this process, so this is not a new hazard, only an
	// unclosed corner of the existing one.
	if err := validatePersonSlotWithoutMinting(st, siteID, in.Driver, in.HasOrientation); err != nil {
		return Trip{}, Arrival{}, err
	}
	for _, slot := range in.Occupants {
		if err := validatePersonSlotWithoutMinting(st, siteID, slot, in.HasOrientation); err != nil {
			return Trip{}, Arrival{}, err
		}
	}
	vehicleID, verr := resolveVehicleIDBeforeMinting(in.VehicleID, in.ResolveVehicle, in.VehicleUnidentified)
	if verr != nil {
		return Trip{}, Arrival{}, verr
	}
	driverID, err := resolvePersonSlot(st, siteID, nodeEpoch, in.Driver, in.RecordedBy, in.HasOrientation)
	if err != nil {
		return Trip{}, Arrival{}, err
	}
	occupantIDs := make([]string, 0, len(in.Occupants))
	for _, slot := range in.Occupants {
		id, err := resolvePersonSlot(st, siteID, nodeEpoch, slot, in.RecordedBy, in.HasOrientation)
		if err != nil {
			return Trip{}, Arrival{}, err
		}
		occupantIDs = append(occupantIDs, id)
	}
	// ⟦[[D-84]]⟧ THE CONFIRMED RECORD CHANGES GO HERE — after every slot has
	// resolved, before the trip and its arrival are authored — so that
	// buildArrivalVisits (inside RecordExpectedArrival below) snapshots the
	// AMENDED record. It sits at the same position as the co-mints above:
	// reference authoring that a later refusal inside dispatchTrip does not
	// roll back (the store authors one event per call; see the CRASH WINDOW
	// note). An amendment left behind by a refused vehicle number is still a
	// true, officer-confirmed fact about the person, and the re-rendered
	// form shows it already applied.
	if err := ApplyPersonRecordChanges(st, siteID, nodeEpoch, in.RecordChanges, in.RecordedBy); err != nil {
		return Trip{}, Arrival{}, err
	}

	// DispatchLocalCheckIn, not Dispatch: since D-4, Dispatch authors
	// DEPARTURES and refuses a trip with no destination. This is the one
	// legitimate destination-less trip in the system — the arrival half of
	// a check-in — and the very next statement authors its arrival.
	trip, err := DispatchLocalCheckIn(st, siteID, nodeEpoch, DispatchInput{
		// ⟦comint-on-refusal, 2026-09-11⟧ ⟦was: "VehicleID: in.VehicleID, ResolveVehicle:
		// in.ResolveVehicle,"⟧ — the resolver already ran, above, before any person slot. Passing
		// the resolved id (and no resolver) means dispatchTrip's own vehicle branches are both
		// no-ops here: vehicleID is already non-empty UNLESS VehicleUnidentified (SchemaVersion 13,
		// R-17) — the higher layer's resolveVehicleIDBeforeMinting made the SAME decision this
		// door's dispatchTrip call would make on its own, so neither its early "no id and no
		// resolver" refusal nor its deferred resolver call can fire a second time either way.
		VehicleID: vehicleID, VehicleUnidentified: in.VehicleUnidentified, DriverPersonID: driverID,
		OccupantPersonIDs: occupantIDs, RecordedBy: in.RecordedBy,
		// DestinationSite / DeadlineMinutes both nil: the degenerate local
		// check-in trip (§10).
	}, occurredAtMS, timeSource)
	if err != nil {
		return Trip{}, Arrival{}, err
	}
	// ⟦D3 fix, WINDOW-AUDIT.md⟧ author the ticked orientation completion(s)
	// HERE — after DispatchLocalCheckIn has succeeded (every refusal this
	// door can produce for THIS submission — a blank/refused vehicle, a
	// blank driver, an R-12 off-roster name on ANY row — has already been
	// ruled out above) and BEFORE RecordExpectedArrival below, whose
	// buildArrivalVisits choke point reads OrientationHeld live. The pre-fix
	// ordering authored the tick before this whole door ran
	// (web/gate.go's completeTickedOrientations): a refusal ANYWHERE in this
	// function still left the ticked person's orientation recorded, and if
	// they had ever been held out at this gate before, entry-on-completion
	// (§3.4) put them on the muster from a check-in the officer was told
	// recorded nothing ("Nothing was recorded" — yet something was).
	// RecordExpectedArrival cannot refuse on the trip just dispatched above
	// (this call's own brand-new, unarrived trip), so this is the latest
	// point that is still early enough for buildArrivalVisits to see it.
	if err := completeOrientationsBeforeArrival(st, siteID, nodeEpoch, in.OrientationsToComplete, in.RecordedBy); err != nil {
		return trip, Arrival{}, err
	}
	// ⟦gate-orientation-block, 2026-09-09⟧ ⟦was: no trailing argument — the
	// variadic's zero length, which reads as false at EVERY site, so this door
	// entered the un-oriented at an orientation=true post while POST
	// /trips/arrive held them out (MEASURED at f7afc3a,
	// reports/witness/ORIENTATION-ENFORCEMENT.md)⟧.
	arrival, err := RecordExpectedArrival(st, siteID, nodeEpoch, trip.TripID, in.RecordedBy, occurredAtMS, timeSource, in.HasOrientation)
	if err != nil {
		// The crash window, minus the crash: dispatch succeeded, arrival did
		// not. Same visible/recoverable posture as a real process death — the
		// caller sees the error, and IncompleteCheckIns will list this trip.
		return trip, Arrival{}, err
	}
	return trip, arrival, nil
}

// resolveVehicleIDBeforeMinting is GateCheckIn's own copy of dispatchTrip's
// vehicle id validate-or-mint step (tripauthor.go), run here BEFORE any
// person slot resolves rather than after every OTHER validation the way
// dispatchTrip runs it. It is a deliberate duplicate, not a shared call,
// because dispatchTrip's version is pinned byte-for-byte for the general
// Dispatch()/departure path (TestS97DispatchStillRefusesWhenNoVehicleIDIsEverProduced,
// TestEmptyAndPlaceholderVehicleIDsAreRefusedForDistinctStatedReasons) and
// this door's ordering requirement does not apply there:
// a departure's OTHER validations (destination, deadline) can
// still refuse after a real vehicle mint, and that is untouched. The two
// copies must keep the SAME refusal text ("vehicle id is required") and the
// same "empty + no resolver" / "resolver yields nothing" split — if
// dispatchTrip's version ever changes, change this one to match.
//
// SchemaVersion 13 (R-17): the SAME third option as dispatchTrip's,
// duplicated for the SAME reason as the rest of this function — kept in
// sync by hand, not shared. unidentified=true returns ("", nil): an
// explicit non-error empty result the caller (GateCheckIn) reads off its OWN
// copy of the flag when building DispatchInput, never inferred from the
// empty string alone.
func resolveVehicleIDBeforeMinting(vehicleIDIn string, resolve func() (string, error), unidentified bool) (string, error) {
	vehicleID := strings.TrimSpace(vehicleIDIn)
	if unidentified {
		if vehicleID != "" || resolve != nil {
			return "", fmt.Errorf("domain: dispatch: a vehicle cannot be both named and marked unidentified")
		}
		return "", nil
	}
	if vehicleID == "" && resolve == nil {
		return "", fmt.Errorf("domain: dispatch: vehicle id is required")
	}
	if vehicleID == "" && resolve != nil {
		id, rerr := resolve()
		if rerr != nil {
			return "", fmt.Errorf("domain: dispatch: %w", rerr)
		}
		if vehicleID = strings.TrimSpace(id); vehicleID == "" {
			return "", fmt.Errorf("domain: dispatch: vehicle id is required")
		}
	}
	return vehicleID, nil
}

// IncompleteCheckIns lists open, degenerate (no destination, no deadline),
// THIS-SITE-originated trips with no arrival yet — the row-31 crash-recovery
// list. A GateCheckIn that authored its trip_dispatched but died before its
// trip_arrival lands here instead of vanishing.
func IncompleteCheckIns(st *store.Store, siteID string) ([]Trip, error) {
	// Open-first (F30): the open trip_state set is bounded by trips in
	// flight; point-look-up each and keep this site's stuck degenerate ones,
	// instead of dumping every trip and state of the season per gate render.
	open, err := openTripStates(st)
	if err != nil {
		return nil, err
	}
	var out []Trip
	for _, s := range open {
		t, err := TripByID(st, s.TripID)
		if err != nil {
			return nil, err
		}
		// DestinationUncontrolled must ALSO be nil: an open uncontrolled run
		// (destination_site NULL like a degenerate trip, 2026-08-20) is a
		// crew recorded OUT at a pad, not a crashed check-in — offering to
		// "resume" it would check the whole Conneco crew back onto this
		// muster with one click. Its return goes through the per-person
		// return manifest instead (RecordExpectedArrivalExcluding).
		if t != nil && t.OriginSite == siteID && t.DestinationSite == nil &&
			t.DestinationUncontrolled == nil && t.DeadlineMinutes == nil {
			// ⟦W9a-2 consumer 10⟧ The SELECTION stays RAW — this is the
			// crash-recovery list, and what makes a row belong here is the
			// raw shape of the trip event that was authored (all three
			// destination/deadline fields absent), not what a later
			// correction says about it. What the row RENDERS — vehicle,
			// driver, occupant count — is COMPOSED, because those are the
			// facts §9.2 row 10 names and an officer resuming a crashed
			// check-in needs the corrected ones.
			c, cerr := EffectiveTrip(st, t.TripID)
			if cerr != nil {
				return nil, cerr
			}
			if c != nil {
				out = append(out, c.Trip)
				continue
			}
			out = append(out, *t)
		}
	}
	return out, nil
}

// ResumeCheckIn completes an interrupted GateCheckIn (row 31). It is exactly
// RecordExpectedArrival — the ordinary C6 arrival path, not a special-cased
// recovery mechanism — called again for a trip IncompleteCheckIns found.
//
// hasOrientation ⟦gate-orientation-block, 2026-09-09⟧ is this site's own
// config.HasOrientation(), the same trailing variadic RecordExpectedArrival
// takes and for the same reason (existing callers keep compiling; zero
// length reads false). A resumed check-in is an arrival like any other, so
// an un-oriented occupant of the crashed vehicle is held out here exactly
// as they would have been had the first attempt not died — the crash is
// not a door around the rule. ⟦was: no such parameter; the second of the
// two gate doors ORIENTATION-ENFORCEMENT.md measured open⟧.
func ResumeCheckIn(st *store.Store, siteID string, nodeEpoch int64, tripID string, recordedBy *string, occurredAtMS int64, timeSource string, hasOrientation ...bool) (Arrival, error) {
	return RecordExpectedArrival(st, siteID, nodeEpoch, tripID, recordedBy, occurredAtMS, timeSource, hasOrientation...)
}

// ResumeCheckInCompleting is ResumeCheckIn with the resume door's ticked
// orientations ⟦A3 / H-6, 2026-09-29⟧: "Add <this post>'s orientation for X
// now, and check them in" for occupants of the crashed vehicle who lack it
// (web/gate.go handleGateResume; the same tick GateCheckIn carries on
// GateCheckInInput.OrientationsToComplete). The order is the D3 fix's order,
// brought to this door: EVERY refusal the arrival can produce FIRST
// (planExpectedArrival — unknown trip, already arrived, no occupants), THEN
// the tick is authored (CompleteOrientationsSkippingHeld, the one path for
// "ticking an orientation at the gate"), THEN the arrival, whose
// buildArrivalVisits reads OrientationHeld live and so enters them.
// ⟦was: web/gate.go completeOrientationsForTrip ran BEFORE ResumeCheckIn, so
// a resume refused as already arrived — a stale tab, L03 G-121 — had
// already recorded the orientation, and entry-on-completion (d91 §3.4) had
// already opened a visit off that same arrival: the officer read "recording
// it again enters nobody" as somebody entered⟧.
//
// orientationsToComplete is trusted only as far as "actually aboard this
// trip": it is intersected with the trip's occupant rows (which the plan has
// already read), never a posted id honoured beyond that — the scoping
// completeOrientationsForTrip did, now inside the door with the refusals it
// must follow. At a post with no orientation concept the caller passes nil
// (D-88), and hasOrientation false holds nobody out, exactly as
// ResumeCheckIn. Callers that tick nothing keep using ResumeCheckIn; this
// entry point exists so that door's one extra write sits behind the
// arrival's refusals instead of in front of them.
func ResumeCheckInCompleting(st *store.Store, siteID string, nodeEpoch int64, tripID string, orientationsToComplete []string, recordedBy *string, occurredAtMS int64, timeSource string, hasOrientation bool) (Arrival, error) {
	plan, err := planExpectedArrival(st, siteID, tripID, nil, hasOrientation)
	if err != nil {
		return Arrival{}, err
	}
	if len(orientationsToComplete) > 0 {
		aboard := make(map[string]bool, len(plan.personIDs))
		for _, id := range plan.personIDs {
			aboard[id] = true
		}
		var ids []string
		for _, id := range orientationsToComplete {
			if aboard[id] {
				ids = append(ids, id)
			}
		}
		if err := completeOrientationsBeforeArrival(st, siteID, nodeEpoch, ids, recordedBy); err != nil {
			return Arrival{}, err
		}
	}
	return authorExpectedArrival(st, siteID, nodeEpoch, plan, recordedBy, occurredAtMS, timeSource, hasOrientation)
}
