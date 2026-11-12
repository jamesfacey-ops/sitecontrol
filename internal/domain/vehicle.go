// vehicle.go — W7 (item 9): the `vehicle_created` event, the `vehicle`
// projection, the composed read path, the creation edge with the RELOCATED
// placeholder guard, and `vehicleFoldKey`.
//
// design/WINDOW-BUILD-PLAN-v1.md `### W7 —`, criteria LOCKED 2026-09-03.
// This is the THIRD clone of W3's mechanism-defining build (person_amend.go)
// after W5d's company clone; the amendment half lives in vehicle_amend.go.
//
// What differs from the company side, and why — stated here because a clone
// author's first instinct is to copy the whole shape across:
//
//   - `recorded_by` is ABSENT from the entity, from the created payload and
//     from the amendment carrier (DR-W7-7). ⚠ This is a DECLARED DIVERGENCE,
//     not an omission: `person` and `company` BOTH carry the column and
//     `PersonCreated`/`CompanyCreated` both carry the field. Attribution for a
//     vehicle is structural — env.SiteID — and a vehicle is a thing, not an
//     act by an officer.
//   - `retired` is ABSENT (DR-W7-8), and the clone source CONTAINS the field
//     W7 must not copy: `person_amendment` carries one. Nothing here retires a
//     vehicle, and this file rules nothing about whether one ever should.
//   - No reason, note or comment field on either payload (DR-W7-20): [[D-49]]
//     reaches `vehicle_amended` by its own text and does NOT reach
//     `vehicle_created`, but D-49's STATED reason — a payload field on a
//     replicated shape is free inside this window — is shape-agnostic, so the
//     conclusion holds for both halves under this lock's own ruling.
//   - Both content facts are NULLABLE. A vehicle may have a `role` and no
//     `number` (the vac truck), which is the whole subject of item 8; a bare
//     string could not represent that and a default would be a value nobody
//     typed.
//
// The load-bearing rules are W3's, unchanged and not restated: composition
// order, the edge/fold asymmetry, Clear-composes-as-absent, the derived-per-
// read merge snapshot, and the never-stored supersession rule. See
// person_amend.go's file comment.
package domain

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"sitecontrol/internal/store"
)

// --- event payload -------------------------------------------------------

// VehicleCreated is the wire payload (type "vehicle_created"). Lowercase
// snake_case keys per the measured house convention. The frozen literals for
// gate B-2's wire review are the type name `vehicle_created` and the field
// names `vehicle_id`, `number` and `role` — and the two fact names freeze
// TWICE over, because the same spellings are simultaneously the Clear VALUE
// vocabulary on the amendment carrier (§7 row 3).
//
// ⚠ `recorded_by` is DELIBERATELY ABSENT (DR-W7-7) and so is any reason /
// note / comment field (DR-W7-20). Both absences are §7 REMOVE-category rows,
// i.e. spellings that must NOT appear, and both are asserted by W7-7 and
// W7-29 rather than left to a reader's inference.
//
// `omitempty` on the two facts is the OMISSION spelling of absence (§7 row
// 2b): a numberless vehicle carries no `number` key into its payload bytes
// and therefore none into its row hash, and no marshaller here ever emits
// `null`.
type VehicleCreated struct {
	Type      string  `json:"type"` // "vehicle_created"
	VehicleID string  `json:"vehicle_id"`
	Number    *string `json:"number,omitempty"`
	Role      *string `json:"role,omitempty"`
}

const evtVehicleCreated = "vehicle_created"

// --- the projection ------------------------------------------------------

// vehicleProjection registers the created-state entity table. The declared
// column set is EXACT (DR-W7-1, W7-7(a)): the id, structural attribution, and
// one column per content fact. `site_id` is NOT NULL because attribution is
// by post ([[D-5]]); `number` and `role` are nullable with NO default.
//
// ⚠ NO status-shaped column may ever join it and NO `retired` column may
// ever join it — the first because supersession is computed at read and never
// stored, the second because DR-W7-8 asserts the shape the spec specifies.
func vehicleProjection() store.Projection {
	return store.Projection{
		Name:    "vehicle",
		Table:   "vehicle",
		OrderBy: "vehicle_id",
		Schema: `CREATE TABLE IF NOT EXISTS vehicle (
			vehicle_id TEXT PRIMARY KEY,
			site_id    TEXT NOT NULL,
			number     TEXT,
			role       TEXT
		) STRICT;`,
		Apply: applyVehicleCreated,
	}
}

func applyVehicleCreated(tx *sql.Tx, env store.Envelope) error {
	var p VehicleCreated
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return fmt.Errorf("domain: vehicle fold: %w", err)
	}
	if p.Type != evtVehicleCreated {
		return nil
	}
	// Plain INSERT, no ON CONFLICT: vehicle_id is site-prefixed and minted
	// fresh per creation, so a genuine primary-key collision here would mean a
	// broken id generator, not a legitimate duplicate. Two posts minting a
	// record for the same physical truck is a DUPLICATE — distinct ids, the
	// same truck — which is what `same_as` exists to join and is not a
	// conflict at all.
	_, err := tx.Exec(`INSERT INTO vehicle (vehicle_id, site_id, number, role) VALUES (?, ?, ?, ?)`,
		p.VehicleID, env.SiteID, p.Number, p.Role)
	if err != nil {
		return fmt.Errorf("domain: vehicle fold: insert %s: %w", p.VehicleID, err)
	}
	return nil
}

// --- the composed shape --------------------------------------------------

// Vehicle is the COMPOSED vehicle: the created-state row plus every
// `vehicle_amended` assertion folded over it in ascending (site_id,
// node_epoch, seq) order. Vehicles() and VehicleByID() below return this
// shape, never the raw projection row (W7-8, W3-4's own instance).
//
// Number and Role are pointers because nil means ABSENT — never asserted, or
// cleared — which is a different thing from the empty string (W3-3's rule on
// this entity's facts, DR-W7-15). The distinction is load-bearing: a vehicle
// with a role and no number is the ordinary shape after item 8, and a build
// that collapses the two cannot tell "nobody has told us its number" from
// "somebody typed nothing".
type Vehicle struct {
	VehicleID string  `json:"vehicle_id"`
	SiteID    string  `json:"site_id"`
	Number    *string `json:"number,omitempty"`
	Role      *string `json:"role,omitempty"`
	// SameAs is the merge pointer (P-3 option 1a): the composed effective
	// `same_as` assertion, or nil when un-merged. "Merged?" is answered by
	// THIS field and nothing else; resolution surfaces derive from it per read
	// (LoadVehicleMergeMap, vehicle_amend.go).
	SameAs *string `json:"same_as,omitempty"`
}

// Vehicles returns the COMPOSED vehicles (W7-8). This is THE one composed
// vehicle read path — every reader resolves through it or through
// VehicleByID, never through the raw projection row (W7-19, W3-4 cloned).
func Vehicles(st *store.Store) ([]Vehicle, error) {
	base, err := dumpSection[Vehicle](st, "vehicle")
	if err != nil {
		return nil, err
	}
	amendments, err := readVehicleAmendments(st)
	if err != nil {
		return nil, err
	}
	return composeVehicles(base, amendments), nil
}

// VehicleByID returns the COMPOSED vehicle, or nil when no such row exists
// here. The M-1 scoped shape IS cloned (W7-9(a)): the dispatch edge resolves a
// vehicle per authored trip and the picker resolves one per rendered row, so a
// whole-table decode per lookup is exactly the cost M-1 removed on the person
// side and re-added would be paid on the hottest path in the application.
func VehicleByID(st *store.Store, id string) (*Vehicle, error) {
	v, err := oneRow[Vehicle](st, "vehicle", store.Eq{Column: "vehicle_id", Value: id})
	if err != nil || v == nil {
		return v, err
	}
	amendments, err := readVehicleAmendmentsFor(st, id)
	if err != nil {
		return nil, err
	}
	// ⟦SchemaVersion 11, [[D-92]]⟧ composed through the ONE per-fact rule
	// (composeVehicle → resolveContest). ⟦was: "for _, r := range amendments
	// { applyVehicleAmendmentRow(v, r) }"⟧
	composeVehicle(v, amendments)
	return v, nil
}

// --- the creation edge ---------------------------------------------------

// VehicleCreateRefusal is a CREATION-EDGE REFUSAL, distinguished from an
// internal failure BY TYPE rather than by reading an error string — the
// company edge's own reason, which W5d states at length and which W7 inherits
// rather than re-derives: a refusal's text is AUTHORED COPY and belongs on the
// page, an internal error's text is not and does not.
type VehicleCreateRefusal struct {
	// Reason is the officer-facing sentence, without prefix or suffix.
	Reason string
}

// ⚠ THE PREFIX SAYS "add vehicle", NOT "create vehicle", AND THE REASON IS A
// MEASURED FALSE POSITIVE RATHER THAN A PREFERENCE. W5-X1's DDL guard
// (`w5c_source_scans_test.go:898–908`, cloned at W5a) flags ANY `+`
// concatenation whose joined literals contain the substring `"CREATE "`,
// upper-cased — so `"domain: create vehicle: refused — " + e.Reason` reds a
// LOCKED criterion for a string that is not DDL and never touches a database.
// The wording carries no operator cost (the page renders `Reason`, never this
// string) and "add" is what the surface's own button says. ⚠ The scan's
// coarseness is a FINDING and is recorded, not silently absorbed: W8's trade
// clone will meet it the moment it writes a "create trade" refusal.
func (e *VehicleCreateRefusal) Error() string {
	return "domain: add vehicle: refused — " + e.Reason + "; nothing was recorded"
}

func refuseVehicleCreate(format string, args ...any) error {
	return &VehicleCreateRefusal{Reason: fmt.Sprintf(format, args...)}
}

// CreateVehicle authors one `vehicle_created` event. It is THE typing moment
// for a vehicle number, and therefore THE one place the placeholder guard
// fires (DR-W7-5, W7-25).
//
// ⚠ THE GUARD IS RELOCATED, NEVER REMOVED AND NEVER DUPLICATED. [[D-11]]:
// "the placeholder refusal stays. It exists to stop `walk-in` and its ~40
// siblings; the exception is an explicit state, not a hole in that guard."
// Under DR-W7-4 the number is not typed at dispatch or at arrival any more, so
// there is nothing at those doors to refuse — the guard is removed from
// NOWHERE. This is the one call site; W7-25(e) is the census that keeps it so.
//
// ⚠ AND IT KEYS ON NormalizeVehicleID, NEVER ON vehicleFoldKey (DR-W7-5).
// Keying the relocated guard on the fold key would stop refusing TWELVE of the
// thirty-eight members — `on foot`, `vehicle id`, `vehicle-id`, and the nine
// that fold to the empty string, which IsPlaceholderVehicleID returns false
// for by its own guard. Under the fold key `?` and `-` become acceptable
// vehicle numbers, and `-` was in the real data.
//
// number and role are both optional and both may be nil: a vehicle with a role
// and no number is the ordinary shape after item 8. What is refused is a
// record that identifies NOTHING — neither a number nor a role.
func CreateVehicle(st *store.Store, siteID string, nodeEpoch int64, number, role *string, occurredAtMS int64, timeSource string) (Vehicle, error) {
	num := trimmedOrNil(number)
	rol := trimmedOrNil(role)
	if num == nil && rol == nil {
		return Vehicle{}, refuseVehicleCreate("a vehicle record needs a number or a role — a record that says neither identifies nothing and cannot be picked, searched for or read on a muster")
	}
	if err := refuseIfPlaceholderNumber(num); err != nil {
		return Vehicle{}, &VehicleCreateRefusal{Reason: err.Error()}
	}
	v := Vehicle{VehicleID: newID(siteID, "vehicle"), SiteID: siteID, Number: num, Role: rol}
	payload, err := json.Marshal(VehicleCreated{
		Type: evtVehicleCreated, VehicleID: v.VehicleID, Number: num, Role: rol,
	})
	if err != nil {
		return Vehicle{}, fmt.Errorf("domain: create vehicle: %w", err)
	}
	if _, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource); err != nil {
		return Vehicle{}, fmt.Errorf("domain: create vehicle: %w", err)
	}
	return v, nil
}

// trimmedOrNil normalises an optional caller-supplied fact to the OMISSION
// spelling of absence: a nil pointer, a pointer to "" and a pointer to spaces
// all mean the officer said nothing, and all arrive as nil. DR-W7-15: the
// empty string IS the absence, not a sentinel for it — so there is nothing
// here for W7-5's scan to find, and nothing for a reader to have to decode.
func trimmedOrNil(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return &t
}

// refuseIfPlaceholderNumber is THE ONE PLACE the ~40-value placeholder list is
// consulted. Both typing moments for a vehicle number — creation and
// amendment — go through it, so the guard is ONE guard rather than two copies
// that can drift.
//
// ⚠ IT KEYS ON NormalizeVehicleID, NEVER ON vehicleFoldKey (DR-W7-5), and the
// difference is measured rather than stylistic: of the thirty-eight members,
// TWELVE stop being refused under the fold key — "on foot", "vehicle id",
// "vehicle-id", and the nine that fold to the empty string, which
// IsPlaceholderVehicleID returns false for by its own guard. Under the fold
// key "?" and "-" become acceptable vehicle numbers, and "-" was in the real
// data.
//
// ⚠⚠ THIS IS NOT YET THE RELOCATION `W7-25(e)` ASKS FOR, and calling it that
// would be a false green. The census wants EXACTLY ONE production call site of
// IsPlaceholderVehicleID; the two authoring doors still call it, because under
// DR-W7-4 the guard may only leave dispatch and arrival once the number stops
// being typed there — and that act is routed to the gate. Removing it while
// the string is still an authoring input would make the move A REMOVAL, which
// [[D-11]] forbids in terms. See reports/window-build/W7-build.md §8.
func refuseIfPlaceholderNumber(number *string) error {
	if number == nil {
		return nil
	}
	if IsPlaceholderVehicleID(*number) {
		return vehicleIDRefusal(*number)
	}
	return nil
}

// --- the fold key --------------------------------------------------------

// vehicleFoldKey is the SEARCH-AND-PROMPT form of a vehicle number: every
// non-alphanumeric rune dropped, the rest casefolded, so `607-E0245`,
// `607 E0245` and `607e0245` all fold together.
//
// ⚠⚠ IT IS A DIFFERENT FUNCTION FROM NormalizeVehicleID AND NEITHER BECOMES
// THE OTHER (W7-11). This one exists for SEARCH RANKING in the picker and for
// the duplicate prompt at creation, "and for nothing else. It never merges,
// never correlates, never reaches a database column." NormalizeVehicleID is
// the CORRELATION comparison form and DELIBERATELY PRESERVES PUNCTUATION.
//
// Widening NormalizeVehicleID into this is the natural way to build it and it
// would silently change correlation semantics for EVERY TRIP IN THE SYSTEM:
// `TRK-14` and `TRK 14` would begin to correlate, so an arrival typed with a
// space would bind a run dispatched with a hyphen. That is mutant (m7-11), and
// W7-11(a)'s one-declaration census plus W7-2(b)'s pinned literals are what
// keep the two apart.
//
// ⚠ It is DECLARED EXACTLY ONCE, in this package, and shared. A second copy in
// `internal/deadline` or `internal/web` is the drift the spec's own "two
// consumers" makes likely, and W7-11(a) counts declarations over `internal/`
// and `cmd/` for that reason.
func vehicleFoldKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(norm.NFC.String(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// VehiclesFoldingTo returns the composed vehicles whose number folds to the
// same key as `number` — the DUPLICATE PROMPT at creation.
//
// ⚠ It answers a QUESTION FOR A HUMAN and nothing more: it never merges, and
// no fold, correlation or muster path calls it. The officer decides whether
// two records that read alike are one truck; this only asks.
//
// ⚠ THE SPEC NAMES TWO CONSUMERS AND THIS BUILD HAS ONE. The second is the
// PICKER's search ranking, which lives on the vehicle-selection control the
// check-in form does not yet have — that control belongs with the authoring
// change routed at reports/window-build/W7-build.md §2. `vehicleFoldKey` is
// deliberately left with one caller rather than given a second exported
// spelling nobody calls: an exported key function with no consumer is a
// second declaration waiting to drift, which is the exact thing W7-11(a)
// counts.
func VehiclesFoldingTo(st *store.Store, number string) ([]Vehicle, error) {
	key := vehicleFoldKey(number)
	if key == "" {
		return nil, nil
	}
	all, err := Vehicles(st)
	if err != nil {
		return nil, err
	}
	var out []Vehicle
	for _, v := range all {
		if v.Number != nil && vehicleFoldKey(*v.Number) == key {
			out = append(out, v)
		}
	}
	return out, nil
}

// --- the co-mint edge ----------------------------------------------------

// ResolveOrCreateVehicleByNumber turns a number an officer TYPED into the id
// of a vehicle RECORD, minting the record when this post holds none with that
// number. It is the bridge DR-W7-4 requires between a form that still has a
// text box and three authoring doors that take only an id.
//
// ⚠⚠ THIS IS NOT THE FREE-TEXT DOOR IN A NEW PLACE, AND THE DIFFERENCE IS THE
// WHOLE OF DR-W7-4. The ruling is that "the typing moment IS creation": a
// number reaches the system exactly once, when a vehicle record is created,
// and every later act names the record. So a vehicle-authoring function taking
// a typed number is the CORRECT shape — CreateVehicle below has always taken
// one — while a TRIP-authoring function taking one is the shape W7-4(d)(i)
// forbids. What this function hands back is an id, and an id is all the doors
// can now be given.
//
// ⚠ IT IS ALSO WHY THE PLACEHOLDER GUARD IS STILL LOAD-BEARING RATHER THAN
// BYPASSED. Every path through here that mints ends in CreateVehicle, which
// consults refuseIfPlaceholderNumber — so an officer under time pressure
// typing "N/A" at the gate is refused exactly as they were before the
// relocation, once, at the moment the record would have been made. [[D-11]]'s
// "the placeholder refusal stays" holds through this edge, not around it.
//
// ⚠ THE MATCH KEY IS NormalizeVehicleID, NEVER vehicleFoldKey (W7-11(c)).
// NormalizeVehicleID is the CORRELATION identity — casefold, collapse
// whitespace, NFC, punctuation PRESERVED — so a number typed here resolves to
// the record an arrival keying on the same string would have correlated with.
// The fold key is deliberately looser (it drops punctuation) and its job is to
// ASK a human about near-duplicates (VehiclesFoldingTo, the duplicates
// prompt), never to decide silently that "607-E0245" and "607 E0245" are one
// truck. Keying this edge on the fold key would make that decision without
// asking, on the busiest operator path in the application.
//
// ⚠ AN EMPTY NUMBER RETURNS AN EMPTY ID AND NO REFUSAL. That is deliberate:
// the "names an entity" refusal belongs at the authoring door, where W7-4(b)
// re-points it and where its literal is preserved byte-for-byte, so an officer
// who dispatches with an empty box still meets the same sentence they always
// have. Refusing here as well would put the same rule in two places and let
// them drift.
//
// ⚠ MERGES ARE FOLLOWED. A number still carried by a record that has since
// been merged away resolves to the merge TARGET, so a run does not attach to a
// duplicate the estate has already declared dead. This is the same act
// resolveOrCreateTradeByName performs for trades (checkin.go) and it is cloned
// from it deliberately.
//
// ⚠⚠ MEASURED LIMITATION, RECORDED RATHER THAN HIDDEN: if a record whose
// number IS a placeholder already exists on this post — it cannot be created
// or amended into that state here, so it could only arrive by replication from
// a pre-relocation post — this edge will RESOLVE to it rather than refuse. It
// is not guarded here because W7-25(e) asserts IsPlaceholderVehicleID has
// EXACTLY ONE production call site, and a second one at this edge would turn
// the relocation into a duplication, which is mutant (m7-25d) by name.
func ResolveOrCreateVehicleByNumber(st *store.Store, siteID string, nodeEpoch int64, number string) (string, error) {
	typed := strings.TrimSpace(number)
	if typed == "" {
		return "", nil
	}
	key := NormalizeVehicleID(typed)
	existing, err := Vehicles(st)
	if err != nil {
		return "", err
	}
	for _, v := range existing {
		if v.Number == nil || NormalizeVehicleID(*v.Number) != key {
			continue
		}
		m, err := LoadVehicleMergeMap(st)
		if err != nil {
			return "", err
		}
		return ResolveVehicleID(m, v.VehicleID), nil
	}
	// The co-mint. `0, "default"` and NOT the officer's entered arrival time:
	// W2-8's rule, and TP §4.8 row 1's design ruling behind it — "the record's
	// creation occurred at the keyboard; the person's arrival occurred
	// earlier". A vehicle record co-minted at a gate check-in is the same
	// false-provenance risk as a co-minted company or trade, and it is
	// answered the same way (checkin.go's three co-mints all pass this pair).
	created, err := CreateVehicle(st, siteID, nodeEpoch, &typed, nil, 0, "default")
	if err != nil {
		return "", err
	}
	return created.VehicleID, nil
}
