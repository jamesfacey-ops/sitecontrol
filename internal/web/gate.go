// gate.go — C8's gate check-in / check-out UI (chunk-plan C8, design §10),
// rebuilt to design/gate-one-vehicle-many-passengers-v1.md: ONE VEHICLE,
// MANY PASSENGERS, IN AND OUT. This is deliberately a SEPARATE page/route
// tree from C6's /trips (trips.go) — a gate officer's screen, not the
// dispatch/inbound operator's screen.
//
// THE SHAPE JAMES SPECIFIED. Vehicle id -> add one passenger at a time,
// each staying visible, editable and individually deletable -> mark which
// one was driving -> one final button that writes the whole vehicle.
// NOTHING IS WRITTEN UNTIL THAT BUTTON. There is no partial state
// anywhere: a vehicle is either absent from the database or fully in it.
//
// NO DOMAIN CHANGE, NO SCHEMA CHANGE, NO SchemaVersion BUMP. The domain
// already models the driver as a flagged occupant — domain.Dispatch
// prepends DriverPersonID into the occupant list (trip.go,
// applyTripDispatched, and again in tripauthor.go at authoring time), and
// domain.GateCheckInInput is already {vehicle, driver slot, occupant
// slots}. The accumulate-then-commit model is not something added to the
// domain; it is what GateCheckIn already is. Only this package changes.
//
// THE ACCUMULATOR CARRIES ITS OWN STATE IN THE FORM. With scripting off,
// each "add" is a POST that re-renders the same page with every
// accumulated passenger present as real form fields plus one fresh empty
// box; the server holds nothing and writes nothing. With scripting on the
// same markup is built in the DOM and the round trip is skipped — the same
// shape both ways, which is why the JS-off path cannot rot unnoticed. Form
// fields are not "client-side storage" in the sense the locked constraint
// means (no localStorage/sessionStorage/IndexedDB, nothing alert-critical
// living only in a tab); they are the contents of a form the operator is
// looking at.
//
// THE HISTORY TEST (design §8.8-i, chunk brief): buildOnSiteRows below reads
// ONLY Visit (its own point-in-time snapshot fields) and a PersonName lookup
// — never Companies()/Trades()/an AliasMap. That is the same structural
// instinct as domain.MusterReprint (internal/domain/muster.go): the
// function's own signature makes it INCAPABLE of re-resolving a visit's
// company/trade through anything but the snapshot already on the row, no
// matter what changes in the roster afterward. See gate_test.go's
// TestOnSiteListNeverReResolvesCompanyEvenAfterAlias for the executable
// proof — it links an alias AFTER check-in and asserts the on-site list still
// shows the pre-alias name.
package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// gateInbound / gateOutbound are the two directions of the ONE interaction.
// James: "the trip back out should be the same as in as the driver will
// likely change." The MECHANICS are identical; the DEFAULTS AND THE
// CONFIRMATION ARE NOT, and every place they differ is derived separately
// for each direction rather than copied across. See the derivations on
// each control below and in templates/gate.html.
const (
	gateInbound  = "in"
	gateOutbound = "out"
)

// registerGateRoutes mounts the C8 gate surface. Called from Handler().
// GET /gate stays UNGATED (C11, design §12: accountability, not
// confidentiality — no reason to gate a read); every POST that authors an
// event is wrapped in C11's requireLogin. D-5 retired the second wrapper
// (requireOfficer): each of these calls postAttribution, which answers from
// config and never needed a session choice to be made first.
func (s *Server) registerGateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /gate", s.handleGatePage)
	// ⟦R-27/R-28⟧ the whole-vehicle group entry this page used to open
	// with — see handleGateVehiclePage's own comment.
	mux.HandleFunc("GET /gate/vehicle", s.handleGateVehiclePage)

	// THE ACCUMULATE ROUTES AUTHOR NOTHING. They parse the in-progress
	// vehicle form, change the passenger list, and render the same page
	// back. They never reach domain, never call postAttribution, and never
	// touch the store beyond the reads GET /gate already does.
	//
	// They are therefore deliberately NOT wrapped in requireLogin. Gating a
	// route that authors nothing would only mean that typing a passenger's
	// name into a list can bounce the operator off the page in the middle
	// of a bus; the commit — which does author — is gated exactly as
	// before. TestTheAccumulateRoutesAuthorNothing is the executable form
	// of that claim.
	mux.HandleFunc("POST /gate/passenger/add", s.handlePassengerAdd)
	mux.HandleFunc("POST /gate/passenger/delete", s.handlePassengerDelete)
	mux.HandleFunc("POST /gate/passenger/undo", s.handlePassengerUndo)
	// ⟦James, 2026-09-11: "tick people ... then name the vehicle"⟧ SAME
	// CLASS AS THE THREE ABOVE — it reads ticked visit ids off the roster's
	// own form, resolves each against a FRESH open-visits read, and adds
	// the ones still on site to the outbound accumulated list. It authors
	// nothing (see handleGateCheckOutSelectForDeparture): the write is
	// still the existing /gate/checkout/vehicle commit below, unchanged.
	mux.HandleFunc("POST /gate/checkout/select", s.handleGateCheckOutSelectForDeparture)
	// ⟦gate-checkin-spec, James, 2026-09-13: "an edit person button"⟧ SAME
	// CLASS — it reads ticked visit ids off the roster's own form and
	// redirects; see gate_edit_select.go. It authors nothing either.
	mux.HandleFunc("POST /gate/edit-selected", s.handleGateEditSelected)

	// The commits. All three author; all three stay gated (requireLogin).
	mux.HandleFunc("POST /gate/checkin", s.requireLogin(s.handleGateCheckIn))
	// ⟦R-25, James, 2026-09-13: "if a guard wants to just check in one
	// person at a time then use that"⟧ the SECOND inbound rhythm — see
	// handleGateCheckInSingle's own comment. A DISTINCT route (not a flag on
	// /gate/checkin) so the accumulate-then-commit route's own shape stays
	// exactly what W2's route-table lock already pins it to.
	mux.HandleFunc("POST /gate/checkin/single", s.requireLogin(s.handleGateCheckInSingle))
	mux.HandleFunc("POST /gate/checkout/vehicle", s.requireLogin(s.handleGateVehicleCheckOut))

	// The pre-existing individual/ad-hoc check-out picker. It COEXISTS with
	// the outbound vehicle form above and is not replaced by it: it lists
	// people the SYSTEM recorded arriving (never a manifest, so D-3 does not
	// touch it), its one-form shape solved a measured 52-round-trip problem
	// (a PRESENTATION change — see handleGateCheckOut on what did and did
	// not license the domain edit that went with it, S7-1), and it cannot
	// answer "who was driving" — which is the whole reason the outbound
	// vehicle form exists.
	mux.HandleFunc("POST /gate/checkout", s.requireLogin(s.handleGateCheckOut))
	mux.HandleFunc("POST /gate/resume", s.requireLogin(s.handleGateResume))
}

// --- roster payload embedded once per page load (carry-forward row 24:
// "C8 must fetch the roster ONCE per form load and filter client-side" — this
// embedding IS that one fetch; the ≤2-round-trip budget forces it). ---

type rosterPerson struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// OnSite: the record says this person currently holds an open visit
	// here. It is an ORDERING AID ONLY (design §10, and picker.go's own
	// rule): the outbound autocomplete puts on-site people first and marks
	// the rest, but it never removes anybody from the list, and it never
	// enters anybody without the operator affirming them individually
	// (D-3). Nothing on the inbound side reads it.
	OnSite bool `json:"onsite,omitempty"`
	// Origin ⟦D21, James 2026-10-04⟧: the display name of the post that
	// added this record, present ONLY when another live record carries the
	// same name. Display only — never identity (the pick still goes by id).
	Origin string `json:"origin,omitempty"`
	// Company is the person's CURRENT roster company, for DISPLAY IN THE
	// PICKER ONLY. James: "the only things needed to be displayed on the
	// person box is the name and company in last, first, company format" —
	// intake stores Name as "Last, First", so the dropdown renders
	// Name + ", " + Company and reads "Last, First, Company".
	//
	// TWO THINGS IT IS NOT.
	//  1. It is NOT what gets recorded. The visit's company snapshot is
	//     taken from the roster row at authoring time by
	//     domain.buildArrivalVisits, exactly as before; nothing in this
	//     package hands it to the domain.
	//  2. It is NOT part of identity. gate.html sets the input's value from
	//     it.name alone, never from the rendered label, so resolvePick's
	//     "the visible text must still agree with the picked person" rule is
	//     untouched — a picked row whose company label changed underneath it
	//     still resolves by id, and an edited NAME still falls through to
	//     'new' the way it always did.
	//
	// It is live-resolved (the current roster row), which is correct here
	// and would be wrong on the muster: this is a picker helping an operator
	// tell two people apart RIGHT NOW, not a point-in-time record. The
	// history rule lives in buildOnSiteRows and is untouched by this field —
	// see the file comment.
	Company string `json:"company,omitempty"`
	// ⟦[[D-84]]⟧ CompanyID / TradeID: what the SCRIPTED row pre-selects once
	// a roster person is picked — "show, do not ask". Same status as Company
	// above: the person's CURRENT record, never identity, never what gets
	// recorded (the visit still snapshots from the record at commit,
	// server-side).
	//
	// ⚠ IDS ONLY — NO TRADE NAME. James ruled the person box shows "name
	// and company, nothing else", and TestThePersonBoxNeverShowsAWorkgroup
	// locks the payload to that: no trade name rides in it. The ROW's record
	// display (D-84's surface, not the box's) takes the trade's name from
	// the picker option list the page already renders — one source for the
	// register's names, and the box's payload unchanged in what it can show.
	CompanyID string `json:"company_id,omitempty"`
	TradeID   string `json:"trade_id,omitempty"`
	// ⚠ LAST/FIRST ARE DELIBERATELY NOT HERE, though CompanyID/TradeID/
	// Position (below) are carried the same "show, do not ask" way.
	// TestW5aDivergentAwkwardNamesRenderByNameAndSilently (W5a-1/W5a-3)
	// forbids rendering a person's LastName/FirstName ANYWHERE on /gate when
	// they diverge from Name — lawful under D-48 — and this payload embeds
	// EVERY roster person, not just the on-site table's scoped set (which is
	// where rosterDisplayName's own fallback is used safely, gated to people
	// who hold an open visit). So a scripted pick leaves the open box's
	// "Last name" box holding the person's full Name (exactly what the one
	// box it replaces always showed) and "First name" blank — cosmetic only:
	// Typed for an EXISTING pick comes from p.Name directly (handlePassenger
	// Add), never from recomposing these two boxes.
	// Position ⟦gate-checkin-spec, 2026-09-13⟧: same "show, do not ask" role
	// as CompanyID/TradeID — what the SCRIPTED row pre-fills once a roster
	// person is picked, so a JS-cloned row shows the record's real value
	// instead of leaving it blank. Never part of personLabel()'s dropdown
	// text.
	Position string `json:"position,omitempty"`
	// ⟦R-24, James, 2026-09-13: "the screen will not be visable to anyone
	// but the guard so dont keep it hidden"⟧ Phone joined Position here on
	// the SAME "show, do not ask" terms.
	//
	// ⚠ WAS DELIBERATELY OMITTED, from 2026-09-13 until this ruling — see
	// TestNoPhoneNumberReachesTheMusterPrintOrRoster (intake_test.go
	// ⟦was: "TestNoPhoneNumberIsAskedForOrKeptAnywhere"⟧) for the full
	// history. That test used to sweep /gate too and would fail the moment a phone number
	// reached this payload; James, shown the resulting empty Phone box on
	// every pick, ruled the gate screen is the officer's own working
	// surface — not something handed around like a printed muster — and
	// struck /gate from the sweep so this field could exist.
	// ⚠ CORRECTED (PII-1): that ruling was recorded with the premise that
	// /gate is "behind a login". It is NOT — GET /gate and /gate/vehicle are
	// ungated by design (§12). The real rule: phone (and the whole roster
	// payload) is served ONLY to a request with an authenticated session;
	// an unauthenticated GET gets an empty roster, and the sweep covers /gate
	// and /gate/vehicle logged out. Do not cite "behind a login" for /gate.
	// The other three swept surfaces (/roster, /muster, /muster/print)
	// still forbid a phone number outright and are UNCHANGED by this.
	// A JS-cloned known row's Phone box now starts filled exactly like
	// Position's — see recordChanges' own comment for why a box the script
	// leaves blank (an unresolvable/off-roster pick) is still always safe:
	// a blank Position or Phone box asserts NOTHING at commit
	// (rowDiffersFromRecord's own rule) — it is never read as "clear the
	// record's value", only a NON-blank box that disagrees with the record
	// is.
	Phone string `json:"phone,omitempty"`
	// OrientNeeded / OrientHeld ⟦X-R12-JSON / F-L03-R2-1, 2026-09-30⟧: the
	// SAME two flags decorateRows sets on a server-rendered KNOWN row
	// (passengerRow.NeedsOrientation / HoldsOrientation, from THIS post's
	// orientation register), so the scripted "Add person" can render the
	// R-12 "NOT ORIENTED HERE" warning + tick — or the "on file" box —
	// before commit, as the scripting-off render always has. Without them
	// a JS-cloned row carried no orientation state at all and the officer
	// learned of the hold-out only from the post-commit NOT ENTERED banner.
	// Set ONLY at a post that owns an orientation (D-88: a road post's page
	// carries zero orientation vocabulary, so omitempty keeps the keys out
	// of its payload entirely) and, like every field here, only to an
	// authenticated request (PII-1). A register read that fails sets
	// neither, exactly as decorateRows' own `err == nil &&` does.
	OrientNeeded bool `json:"orient_needed,omitempty"`
	OrientHeld   bool `json:"orient_held,omitempty"`
	// OrientFrom ⟦R4, James, 2026-10-04⟧: passengerRow.OrientationCarriedFrom
	// for the scripted row — the merged-away record's name when the
	// orientation is inherited through a merge, so JS-on says what JS-off says.
	OrientFrom string `json:"orient_from,omitempty"`
}

// refOption is one row of the gate's company or trade picker — the intake
// shape ([[D-84]]: "reuse the intake shape"), flat and name-sorted, listing
// EVERY row exactly as intake.go's data.Companies / data.Trades do.
type refOption struct {
	ID   string
	Name string
	// Origin ⟦D21, James 2026-10-04⟧: the display name of the post that
	// added this row, set ONLY when another row of the same register reads
	// the same under the register's own fold — after a partition both
	// sides mint "Pipefitter", and the officer needs a fact to choose BY.
	// Same rule and same word as intake's picker ("added at <post>").
	Origin string
}

// Label is the option's visible text: the name, then "(added at <post>)"
// when Origin is set. A method, not a template {{if}}, because the passenger
// row's parallel-array guard
// (TestW8TradeFieldIsEmittedOnEveryRowNeverConditionally) reads any {{if}}
// before a field as a conditional emission.
func (o refOption) Label() string {
	if o.Origin == "" {
		return o.Name
	}
	return o.Name + " (added at " + o.Origin + ")"
}

type onSiteRow struct {
	VisitID string
	// PersonID: which person this open visit belongs to. Carried so the
	// trip console's dispatch pickers can be scoped to the on-site set from
	// this same guarded row type rather than reaching for the roster (U2).
	// It is an id, not a re-resolution: it says nothing about the person's
	// company, which is what the history rule protects.
	PersonID string
	Person   string
	Company  string // SNAPSHOT ONLY — see the file comment
	Trade    string // SNAPSHOT ONLY
	// SignedIn is the sign-in time and date the row SHOWS, `hh:nn mm/dd`
	// (gate_signin.go), written after buildOnSiteRows by attachSignIns — the
	// builder's signature is untouched and still cannot re-resolve anything.
	// Display only: nothing sorts or decides by it (D-90/D-92).
	SignedIn string
	// Restored (SchemaVersion 15, R-23): this open visit was NOT observed
	// by this post — it is under our own site tag at a node_epoch below the
	// running one, so it reached this database only through the §8.4
	// self-history recovery, i.e. the peer's copy of our history
	// (domain.Visit.RestoredFromPeer). Set by markRestoredRows after
	// buildOnSiteRows, which stays epoch-blind (picker.go reads PEERS'
	// rosters through it, where the notion does not apply). Display only:
	// the row is still on site and still tickable.
	Restored bool
	// Elsewhere (M-6 / T-L08-C — James, 2026-10-03): the OTHER posts whose
	// record also holds this person on site right now, one tag each
	// (muster.go's elsewhereTag; domain.AlsoOnSiteElsewhere states the
	// rule). Set by markElsewhereRows after buildOnSiteRows, like Restored.
	// Display only: the row is still on site, still counted, still
	// tickable — the ruling is a read-time flag, not a cross-site write.
	Elsewhere []elsewhereTag
}

// markElsewhereRows sets onSiteRow.Elsewhere from the tags keyed by visit id
// and returns how many rows carry at least one — the roster's own note.
func markElsewhereRows(rows []onSiteRow, tags map[string][]elsewhereTag) int {
	n := 0
	for i := range rows {
		if t := tags[rows[i].VisitID]; len(t) > 0 {
			rows[i].Elsewhere = t
			n++
		}
	}
	return n
}

// markRestoredRows sets onSiteRow.Restored from the visit rows the list was
// built from, for THIS post's identity — R-23: the roster must distinguish
// OBSERVED AT THIS GATE from RESTORED FROM THE PEER. Returns how many it
// marked, for the roster's own note.
func markRestoredRows(rows []onSiteRow, visits []domain.Visit, siteID string, nodeEpoch int64) int {
	restored := make(map[string]bool, len(visits))
	for _, v := range visits {
		if v.RestoredFromPeer(siteID, nodeEpoch) {
			restored[v.VisitID] = true
		}
	}
	n := 0
	for i := range rows {
		if restored[rows[i].VisitID] {
			rows[i].Restored = true
			n++
		}
	}
	return n
}

// --- the on-site roster TABLE (GATE-ACCESS-STYLE, James, 2026-09-12) ------
//
// "make it like the access database boxes ... i also like the onsite
// roster displayed below it with the information of last name, first
// name, company, time and i would like to add date as well" —
// design/ACCESS-APP-LAYOUT.md §2's lstOnSite (LastName, FirstName,
// Company, Time In). This replaces the on-site list's GROUPED picker
// rendering (onSiteGroups, still used by nothing on this page as of this
// chunk, but untouched and still exercised by the driver/occupant/arrival
// pickers that share it) with a FLAT, column-per-fact table — Access's own
// roster is not grouped by company either.

// rosterTableRow is one row of that table. Last/First are a DISPLAY SPLIT
// of onSiteRow.Person (the one authoritative Name, D-48) — never
// domain.Person.LastName/FirstName, which are optional parts that may
// diverge from Name and which no read path may consult for anything Name
// already answers (identity matching, the roster, the muster, the gate's
// exact-match resolver — refdata.go's own list, and this display IS "the
// roster"). Time/Date are the same split, of gate_signin.go's combined
// "hh:nn mm/dd".
type rosterTableRow struct {
	Value    string // the checkbox's value — unchanged, still a visit id (visit_ids)
	PersonID string
	Last     string
	First    string
	Company  string
	Time     string
	Date     string
	// Ref carries picker.go's refSameNameRows tag (the tail of a person id)
	// on a row whose Last+First is shared with another row on this same
	// flat list — the identical safety property onSiteGroups' picker gave
	// two same-name rows in one company/trade subgroup, just not scoped to
	// a subgroup any more (this table has none).
	Ref string
	// Origin: the pickerRow's Origin (Q-R5-3), rendered "added at <post>"
	// beside Ref.
	Origin string
	// Restored: onSiteRow.Restored, carried onto the table row (R-23).
	Restored bool
	// Elsewhere: onSiteRow.Elsewhere, carried onto the table row (M-6).
	Elsewhere []elsewhereTag
}

// rosterDisplayName picks what the roster table's Last/First columns show
// for one person. It NEVER PARSES Name to get there — D-48: "nothing
// composes Name from the parts, anywhere, ever," and the converse holds
// just as hard: no read path may DERIVE a part FROM Name either. That is
// enforced on every production .go file in this module, this one included,
// by internal/store's TestW5aNothingParsesAName (an AST scan for
// strings.Index/LastIndex/slicing over a name-bearing expression) —
// ⟦was: an earlier version of this function split on a comma or the last
// space, which is exactly the shape that scan exists to catch; it never
// shipped past this chunk's own RED run⟧.
//
// So: lastName/firstName are the AUTHORED facts (domain.Person.LastName/
// FirstName — W5a's own optional parts, independent of Name and never
// derived from it) — used only when BOTH are on file, because intake.go's
// enrolment door always writes them as a pair and a partial pair from an
// amendment touching just one would otherwise pair a NEW authored part
// with a STALE guess. Every walk-in (D-66) and every pre-W5a person has
// neither (LastName/FirstName are `omitempty` and were never backfilled),
// which is most of this table at any given gate — for them, and for that
// mismatched-pair case, the WHOLE Name shows in Last with First blank:
// one column short, never two blank cells, which on a muster is the worst
// possible bug (nobody may read as absent from the roster for lacking a
// split).
//
// ⟦D-48 / Y-20c, ruling 2026-10-03: "D-48 stands; the gate check-out list
// renders `name` like every other surface"⟧ ⟦was: the authored pair whenever
// BOTH parts were on file⟧ — the parts are shown ONLY while, read together,
// they ARE the Name ("Last, First" exactly, the shape intake writes both
// from). Amending Last or First alone leaves Name unchanged by design (D-48),
// and the old rule then showed a SECOND surname for one on-site person here
// while the muster, roster and picker all showed Name. Now a part that
// disagrees with Name is never shown: the whole Name sits in Last. Nothing
// here derives a part from Name (no parse, no split) — the parts are only
// compared, read-only, against the one authoritative fact.
func rosterDisplayName(name, lastName, firstName string) (last, first string) {
	if lastName != "" && firstName != "" && lastName+", "+firstName == name {
		return lastName, firstName
	}
	return name, ""
}

// splitSignInDisplay separates gate_signin.go's combined "hh:nn mm/dd"
// (signInLayout) into Time and Date (James, 2026-09-12: "i would like to
// add date as well" — as its own column, not folded into Time). The dash
// gate_signin.go renders when a row's instant could not be resolved
// (signInUnavailable) has no space, so it renders whole in Time with Date
// blank — still a dash, never a guess.
func splitSignInDisplay(signedIn string) (t, d string) {
	if i := strings.IndexByte(signedIn, ' '); i >= 0 {
		return signedIn[:i], signedIn[i+1:]
	}
	return signedIn, ""
}

// buildRosterTableRows is the flat shape of the on-site list. Order is
// UNTOUCHED from buildOnSiteRows (name order; D-90/D-92 forbid sorting by
// the sign-in time), so this function only reshapes each row for display;
// it moves nothing. lastName/firstName are keyed by PersonID, the same
// live (never visit-snapshot) lookup Name itself already goes through —
// see rosterDisplayName for why they are read as-is and never derived.
//
// The same-name safety property (picker.go's markSameNames/
// refSameNameRows) is preserved exactly by routing through pickerRow: two
// open visits sharing a name still get told apart by a person-id tail,
// same wording, same fallback, now on a flat list instead of inside one
// company/trade subgroup.
func buildRosterTableRows(rows []onSiteRow, lastName, firstName map[string]string) ([]rosterTableRow, []pickerSameName) {
	prows := make([]pickerRow, len(rows))
	for i, r := range rows {
		prows[i] = pickerRow{Value: r.VisitID, Name: r.Person, PersonID: r.PersonID}
	}
	sameNames := markSameNames(prows) // tags prows[i].Ref in place; never reorders
	out := make([]rosterTableRow, len(rows))
	for i, pr := range prows {
		last, first := rosterDisplayName(pr.Name, lastName[pr.PersonID], firstName[pr.PersonID])
		tm, dt := splitSignInDisplay(rows[i].SignedIn)
		out[i] = rosterTableRow{
			Value: pr.Value, PersonID: pr.PersonID,
			Last: last, First: first, Company: rows[i].Company,
			Time: tm, Date: dt, Ref: pr.Ref, Origin: pr.Origin,
			Restored:  rows[i].Restored,
			Elsewhere: rows[i].Elsewhere,
		}
	}
	return out, sameNames
}

// rosterTableView is gate.html's on-site roster table — the flat,
// Access-style replacement for personPicker's grouped rendering on this
// one page (buildRosterTableRows). FieldName/Verb/SafeDefault/Empty keep
// meaning exactly what they did on the predecessor CheckOutPicker: nothing
// about what this form submits, or how the confirm script names people
// before submitting, changed.
type rosterTableView struct {
	FieldName   string
	IDPrefix    string
	Verb        string
	SafeDefault string
	// SameNameAdvice is what a tagged pair MEANS for this action, rendered
	// once per name in SameNames — personPicker.SameName's counterpart.
	SameNameAdvice string
	SameNames      []pickerSameName
	Rows           []rosterTableRow
	Total          int
	Empty          string
	// Restored (R-23): how many Rows carry the RESTORED tag — the roster
	// says so once, above the table, as well as per row.
	Restored int
	// Elsewhere (M-6 / T-L08-C): how many Rows are also recorded on site at
	// another post — said once above the table, and per row.
	Elsewhere int
	// TimeOut ⟦FIX8, James, 2026-10-05⟧ renders each row's own optional
	// Time out box (time_out_<visit id>), read by handleGateCheckOut and
	// carried by handleGateCheckOutSelectForDeparture.
	TimeOut bool
}

type incompleteRow struct {
	TripID    string
	Vehicle   string
	Driver    string
	Occupants int

	// NeedsOrientation ⟦orientation-doors, gap 1, 2026-09-11⟧ lists the
	// occupants THIS trip's resume would, right now, hold out for lack of
	// this post's own orientation — the /gate/resume door's equivalent of
	// passengerRow.NeedsOrientation, computed the SAME live way
	// (domain.OrientationHeld against s.Cfg.SiteID, never a peer's) in
	// incompleteCheckInRows (web.go). Always empty at a road post
	// (HasOrientation() false) by construction there, and empty for a
	// caller (muster.go) that shares incompleteCheckInRows but has no tick
	// UI of its own — the field just goes unread there, exactly like
	// Occupants goes unrendered by the print sheet's own subset of fields.
	NeedsOrientation []resumeOrientationRow
}

// resumeOrientationRow is one occupant named by incompleteRow.NeedsOrientation
// — display-only, posted back as `add_orientation_person` = PersonID if the
// officer ticks it (gate.html, handleGateResume).
type resumeOrientationRow struct {
	PersonID string
	Name     string
}

// passengerRow is ONE accumulated passenger on the in-progress vehicle.
// Nothing about it is in the database; it exists only as form fields on the
// page the operator is looking at, and it is re-emitted verbatim on every
// accumulate round trip.
//
// Typed is the VISIBLE, EDITABLE field and it is the authority on identity.
// Kind/Value are the script's resolved pick, honoured only while Value
// still names a person whose roster name agrees with Typed — see
// rowPersonSlot, which is the server-side twin of gate.html's resolvePick
// rule that a stale selection is discarded rather than re-derived.
type passengerRow struct {
	Index    int
	Kind     string // "existing" | "new"
	Value    string // resolved person id, or the new person's name
	Typed    string // what the operator can see and edit
	IsDriver bool
	// Time ⟦FIX8 option C, James, 2026-10-05⟧ is this person's own
	// optional "Time in"/"Time out" (occupant_time), typed exactly as
	// posted. Blank means the form-level time. Rows whose effective times
	// differ are recorded as one trip per time (gate_split.go).
	Time string

	// ⟦[[D-84]]⟧ THE COMPANY/TRADE CONTROLS, IN INTAKE'S SHAPE. CompanyID /
	// TradeID are the two <select>s ("" = none on file, an id, or newRef =
	// "not listed — add a new one"); Company / Trade are the add-new
	// escape's text boxes, read ONLY when the matching select says newRef —
	// exactly intake.go's rule for company_id/new_company and
	// trade_id/new_trade. ⟦was: Company / Trade were the only two controls,
	// free text, and for a known person the commit discarded both⟧.
	CompanyID string
	Company   string
	TradeID   string
	Trade     string
	// AmendConfirmed is the officer's tick on "change their record". It is
	// the CONFIRMATION [[D-84]] makes the safeguard: a known person's
	// company/trade may be changed at the gate, but a select that differs
	// from the record WITHOUT this tick is refused at commit, never applied
	// and never silently dropped. It travels like the driver flag — a
	// per-row control whose form field is `amend_confirm` = row index, NOT
	// an `occupant_*` array, because an unticked checkbox submits nothing
	// and an optional member of the positional zip is exactly the SHORT
	// case checkIndexAlignment refuses.
	AmendConfirmed bool

	// AddOrientation ⟦D-88 cross-site warning, 2026-09-10⟧ is the officer's
	// tick on the cross-site orientation warning: "add THIS post's
	// orientation for them and check them in". James: "so now if someone
	// shows up already in the system at site 1 but is coming to site 2 ...
	// we need a warning ... with a add oriantation check box in the warning
	// for the current site." It travels exactly like AmendConfirmed — a
	// per-row control keyed by row index (`add_orientation` = row index),
	// NOT an `occupant_*` array, because an unticked checkbox submits
	// nothing.
	//
	// ⛔ DEFAULTS TO UNTICKED, and left unticked the commit proceeds exactly
	// as R-9's existing mechanism: the person is held out (no visit), named
	// in the post-redirect not-entered notice — nothing here weakens that.
	// Ticked, the commit authors a REAL orientation_completed for them
	// (completeTickedOrientations, gate.go) — the same event, path and
	// authorship as POST /orientation/complete — BEFORE the check-in door
	// runs, so the existing R-9 choke point (domain.OrientationHeld, read
	// live by buildArrivalVisits) already sees them oriented and enters them
	// normally. No new "gate shortcut" record, no schema change.
	AddOrientation bool

	// Position / Phone ⟦gate-checkin-spec, James, 2026-09-13: "position,
	// phone number"⟧ ride the row exactly like Company/Trade above: prefilled
	// FROM THE RECORD for a known person (decorateRow's fromRecord), typed
	// verbatim for a new one. Unlike Company/Trade they are free text with no
	// fold-by-name resolution — see PersonRecordChange.Position/Phone and
	// domain.PersonSlot.Position/Phone. A KNOWN person's change still needs
	// AmendConfirmed, the same D-84 tick Company/Trade already share; there
	// is no second tick per fact.
	Position string
	Phone    string
	// NewLast / NewFirst ⟦gate-checkin-spec, 2026-09-13⟧ are the open
	// box's own Last/First boxes, carried RAW — never derived from Typed and
	// never fed back into it (D-48: composition is one-way, boxes to Name via
	// composeName, exactly as web/intake.go's Last/First already work).
	// Meaningful ONLY for a row that will ADD a person (IsNew()): a known
	// row's identity already has whatever Last/First its record carries, and
	// decorateRow does not touch these two fields for a known row (nothing to
	// amend here — D-84's amend surface for this row is Company/Trade/
	// Position/Phone, not name parts).
	NewLast  string
	NewFirst string

	// AddOrientationFor ⟦orientation-doors, gap 2, 2026-09-11⟧ is the person
	// id the checkbox above was RENDERED against — posted alongside the tick
	// as `orient_target` = "<row index>:<person id>" (gate.html), and read
	// back here purely for the STALENESS check at commit
	// (completeTickedOrientations): rowPersonSlot/decorateRow's own rule is
	// "the hidden id is honoured only while it still names a person whose
	// roster name agrees with the visible text" — this is that same rule
	// applied to the tick instead of the pick. Between the accumulate step
	// that showed the warning and the final submit, the row's typed text can
	// be edited so the row now resolves to nobody, or — the sharper case —
	// to a DIFFERENT known person who also happens to need this post's
	// orientation; either way the tick named an identity this row no longer
	// has, and completeTickedOrientations must not author a completion for
	// whoever the row resolves to NOW under a tick the officer made for
	// somebody else.
	//
	// Left "" by a body that never posted `orient_target` at all — a
	// hand-crafted or pre-this-fix body — completeTickedOrientations falls
	// back to its older, coarser rule (trust perRow's resolution alone), so
	// this is additive: it cannot make an already-working tick stop working.
	AddOrientationFor string

	// --- derived for DISPLAY by decorateRows; never posted, never read back ---
	// Known is the resolved roster person, when the row names one whose
	// roster name agrees with the visible text (rowPersonSlot's own rule).
	// nil = this row will ADD a person, and is therefore ASKED ([[D-66]]).
	Known *domain.Person
	// OrgLabel is what the row SHOWS for a known person — their company and
	// trade off the record — or the not-on-the-roster line for a new one.
	OrgLabel string
	// Asked opens the change/ask block: always for a new person; for a known
	// person only while a change is in progress (a select differs from the
	// record, or the confirm tick is set), so an untouched known row shows
	// its record and nothing else.
	Asked bool
	// Candidates are the roster people a NEW row's typed name READS AS
	// under personMatchCandidates (person_match.go: samePerson: sameName plus
	// the deterministic folds, each with its reason and its misses stated there) —
	// derived for display by decorateRow, never posted. Nil on a known row,
	// and on a new row nobody resembles. Each is offered as a "Use <name>"
	// button (applyRowPick) that switches the row to that record, with the
	// record's company and trade beside the name — the field that actually
	// separates two people who share one; NONE IS EVER PRE-PICKED, and a
	// row whose officer ignores every offer still commits exactly as typed
	// ([[D-66]]). This is the person clone of the vehicle duplicate prompt
	// (VehiclesFoldingTo): a loose key that ASKS, beside a strict one
	// (resolveTypedPerson) that resolves — and only the strict one decides.
	// It also lists BOTH people when two roster rows share a name exactly:
	// the strict key refuses to guess between them, and this is the pick.
	Candidates []rowCandidate

	// NeedsOrientation ⟦D-88 cross-site warning, 2026-09-10⟧: this row names
	// a KNOWN person (Known != nil) who does not hold THIS post's
	// orientation right now (domain.OrientationHeld, read live in
	// decorateRows — the SAME read buildArrivalVisits will make at commit,
	// never a snapshot). Always false on a road post (HasOrientation()
	// false — D-88 has no orientation concept there) and always false on
	// the outbound form (checking OUT owes no orientation). Drives the
	// inline cross-site warning and its AddOrientation checkbox in
	// gate.html; never posted, never read back.
	NeedsOrientation bool

	// HoldsOrientation ⟦gate-checkin-spec, James, 2026-09-13: "default to
	// checked if the current sites oriantation is completed"⟧ is the other
	// side of NeedsOrientation's same live read (decorateRows) — true for a
	// KNOWN row that DOES hold this post's orientation right now. It renders
	// the checkbox CHECKED and effectively inert: gate.html marks that
	// control `disabled` when this is true, so it is never posted and
	// AddOrientation stays false for the row, and completeOrientationsBefore
	// Arrival/CompleteOrientationsSkippingHeld (domain) already no-ops on an
	// id that already holds — belt and braces, not the only guard. The
	// actual entry decision is unaffected either way: buildArrivalVisits
	// reads OrientationHeld LIVE at commit, never this flag, so a person who
	// holds the orientation is admitted regardless of what any checkbox says.
	// Always false when NeedsOrientation's own preconditions do not hold
	// (outbound, a road post, or a new row) — the two are mutually exclusive
	// on a known row and both false on every other kind.
	HoldsOrientation bool

	// OrientationCarriedFrom ⟦R4, James, 2026-10-04⟧ is the display name of
	// the merged-away record whose completion this row's canonical record
	// inherits — "" when the record holds it itself. Shown beside the "on
	// file" box so an inherited orientation is never silent.
	OrientationCarriedFrom string
}

// rowCandidate is one "possibly already on the roster" offer on a new row.
type rowCandidate struct {
	PersonID string
	Name     string
	Org      string // company · trade off the record — decorateRow's label
	// Ref ⟦L04 LOW, 2026-10-03⟧: the person-id tail (picker.go's
	// refSameNameRows) on a candidate whose name AND label another offer on
	// the same row also carries — otherwise two "Use X — Co · Trade"
	// buttons had identical text and identical accessible names, and the
	// officer could not tell which record either one picks.
	Ref string
	// Origin is the adding post's site id beside Ref (Q-R5-3; pickerRow.Origin).
	Origin string
	// Retired marks a RETIRED record (decorateRows drops it on the inbound
	// form — F12).
	Retired bool
}

// dropRetiredCandidates removes RETIRED records from an inbound row's
// "Possibly already on the roster" offers. F12, James, 2026-10-04: the
// retired-name seams. ⟦was: the vehicle door offered "Use <retired person>",
// and the commit then refused the very record it had offered (L12 obs)⟧ — a
// retired record is not selectable for a new check-in (R-118 / D-21.5), the
// same rule the pickers and the type-ahead already follow. The OUTBOUND form
// keeps them: leaving never consults retired (D8).
func dropRetiredCandidates(row *passengerRow) {
	var kept []rowCandidate
	for _, c := range row.Candidates {
		if !c.Retired {
			kept = append(kept, c)
		}
	}
	row.Candidates = kept
	tagSameCandidates(row.Candidates)
}

// tagSameCandidates sets Ref on every candidate whose visible text — name
// and label — another candidate on the same row shares (rowCandidate.Ref).
func tagSameCandidates(cs []rowCandidate) {
	for i := range cs {
		cs[i].Ref, cs[i].Origin = "", ""
	}
	if len(cs) < 2 {
		return
	}
	prows := make([]pickerRow, len(cs))
	for i, c := range cs {
		prows[i] = pickerRow{Value: c.PersonID, Name: c.Name + " — " + c.Org, PersonID: c.PersonID}
	}
	markSameNames(prows)
	for i := range cs {
		cs[i].Ref = prows[i].Ref
		cs[i].Origin = prows[i].Origin
	}
}

// IsNew reports whether the row will ADD a person rather than check in a
// known one. Template-facing; the confirm control is hidden on such a row
// because there is no record to amend.
func (r passengerRow) IsNew() bool { return r.Known == nil }

// ChangeLabel is the <summary> text of the row's change/ask block.
//
// ⟦gate-checkin-spec, 2026-09-13⟧ Position/Phone now share this same block —
// see gate.html's p-org-change — so the label names all four facts, not just
// the two it used to. OrgLabel itself is UNCHANGED ("Company · Trade" only):
// widening it would touch rowCandidate.Org, the duplicate-offer text an
// officer scans at the window, which a prior ruling deliberately kept free
// of a phone number (gate_script's personLabel comment: identification text
// read aloud at −30 is not where a phone number belongs). Position/Phone get
// their own small label instead — see gate.html.
func (r passengerRow) ChangeLabel() string {
	if r.Known == nil {
		return "Company, trade, position and phone (optional)"
	}
	return "Change company, trade, position or phone"
}

// vehicleForm is one direction's in-progress vehicle. Exactly one of the
// two on a gate page can be in progress at a time — see gateData.InProgress
// for why that is a safety property rather than a limitation.
type vehicleForm struct {
	Direction string // gateInbound | gateOutbound
	Vehicle   string
	// VehicleUnknown ⟦James, 2026-09-11⟧: outbound only (see gate.html's
	// $outbound guard — the field is never rendered inbound, so it is never
	// posted from that direction in practice). Ticked, the vehicle text
	// field above is ignored and handleGateVehicleCheckOut mints an
	// explicit, distinct "Vehicle unknown" role-only record instead of
	// resolving a typed number — see vehicleIDForUnknown (vehicle_comint.go).
	VehicleUnknown bool
	Rows           []passengerRow
	// Removed carries the row a delete just took out, so the page can offer
	// "Removed Ben Bartlett — undo". It is emitted by the delete render
	// only: the next add drops it, so the affordance is offered exactly
	// once, next to the deletion it undoes.
	Removed     *passengerRow
	RemovedName string
	// GroupDrivers / GroupDriverPick ⟦FIX8 option C⟧: when the rows' times
	// differ, each time is its own trip and needs its own driver — the
	// per-time pickers to render (computed at render, gate_split.go) and
	// the picks posted back (group_driver_<key> = row identity).
	GroupDrivers    []groupDriverView
	GroupDriverPick map[string]string
	// DriverCleared is set when the deleted row was the flagged driver.
	// NOTHING IS AUTO-PROMOTED: promoting whoever moved into the slot would
	// put a name in an incident report that nobody chose, which is the
	// pre-selected-driver defect wearing a different hat.
	DriverCleared string
	// Error is a refusal to show ABOVE the list the operator still has.
	// A refused commit must never cost them the typing.
	Error string
	// ErrorAmendPersonID is set only when Error is the retired-name refusal
	// (domain.RetiredNameRefusal, retiredAmendPersonID): the record's id, carried in the amend link's URL
	// so the sentence itself stays id-free (L02) and recovery still works
	// with scripting off.
	ErrorAmendPersonID string
	// Outbound only — see handleGateVehicleCheckOut.
	DestinationID string
	DeadlineMin   string
	// ⟦was: OverrideReason string — the weather-phase override; removed with
	// weather phases, James 2026-10-04⟧
	// TimeSource / OccurredAtWall are the TP §4.5 time control's state,
	// carried through every accumulate round trip and every refusal
	// re-render exactly like the passenger rows: a refusal must not cost
	// the operator the typed time any more than the typed list (TP §4.6
	// rule 6). TimeSource "" renders as the untouched default.
	TimeSource     string
	OccurredAtWall string
}

// HasDriver reports whether exactly one row carries the flag.
func (f vehicleForm) HasDriver() bool {
	for _, r := range f.Rows {
		if r.IsDriver {
			return true
		}
	}
	return false
}

// Count is what the commit button names back to the operator. It is the one
// number in the whole interaction the operator can check against the people
// standing in front of them, and it is shown at the moment it becomes final.
func (f vehicleForm) Count() int { return len(f.Rows) }

type gateData struct {
	SiteID string
	// Shift (FIX16 G): the read-only Shift totals panel (shift_totals.go).
	Shift shiftTotalsView
	// Cfg carries the site's DISPLAY NAME to the shared top bar — see
	// tripsData.Cfg (U1 carry-forward note 1).
	Cfg *config.Config
	// HasOrientation ⟦R-12, 2026-09-10⟧: this post's OWN config flag, for ONE
	// sentence — the "not on the roster" note under an open passenger box
	// says "this will add them" at a road post and "enrol them first (that
	// records the orientation)" at an orientation post, where the commit
	// refuses an off-roster name (domain.GateNewPersonRefusal). Never a
	// peer's flag.
	HasOrientation bool
	RosterJSON     template.JS
	// Companies / Trades ⟦[[D-84]]⟧: the two picker lists every passenger
	// row and the Add-person box render, in intake's shape.
	Companies []refOption
	Trades    []refOption
	OnSite    []onSiteRow
	// SignInErr is set when the sign-in dates for the on-site rows could
	// not be read (gate_signin.go). Rendered as a notice on the roster;
	// the names and the count are unaffected and the page still serves.
	SignInErr string
	// ElsewhereErr (M-6 / T-L08-C): set when the "also on site at <peer>"
	// flags could not be read. Rendered as a notice on the roster, so a
	// missing flag is never mistaken for "nobody is double-recorded".
	ElsewhereErr string
	Incomplete   []incompleteRow
	// Inbound (James, 2026-08-21): the vehicles on the road TOWARD this
	// post, under the arriving-vehicle form — "so when ice road checks
	// someone in it populates on the bottom list, and pad site control can
	// click on a vehicle and it will bring up the list of people in it,
	// with a check in button on the bottom to check everyone in".
	//
	// Same rows the operator console's inbound board reads
	// (domain.OpenExpectations) and the same arrival write
	// (POST /trips/arrive) — see inbound.go, which explains what is
	// genuinely new here (the surface and the per-person control) and what
	// is deliberately not (everything else). A failure building it is loud
	// on the page but never fatal to it: the gate must still be able to
	// enter a vehicle by hand when the list cannot be built.
	Inbound    []inboundVehicleRow
	InboundErr string
	// RoadWork (James, 2026-10-03): the WORKING-ON-THE-ROAD runs, rendered
	// beside the inbound list on the vehicle page — this post's own with the
	// scan's verdict and the "back from the road" check-in, other posts' with
	// the "arrived here instead" door (web/road_work.go). RoadWorkChoice is
	// the outbound form's extra option, present only where this post's
	// config offers the category. Same loud-never-fatal posture as Inbound.
	RoadWork       []roadWorkRow
	RoadWorkErr    string
	RoadWorkChoice roadWorkChoice
	// C7 (§9.4): the persistent banner rides every screen, this one
	// included — populated via Server.banner() (web.go), the same helper
	// trips.go/muster.go/status.html use. Rendered by the shared
	// "alert_banner" partial (templates/banner.html).
	Banner    []deadline.Alert
	BannerErr string

	// Post: the site control post this box is (D-5) — rendered by the
	// post_chip partial in the shared top bar. Its presence on a page's
	// data value is also what mounts the Log out control there, so it
	// doubles as "this page rendered for an authenticated session".
	Post string
	// Anonymous ⟦S-034, 2026-10-03⟧: this render has no authenticated
	// session — post_chip then says "Not logged in" rather than "Recording
	// as … / Log out". Display only; every authoring route is requireLogin.
	Anonymous bool

	// RosterTable (GATE-ACCESS-STYLE, James, 2026-09-12: "the onsite roster
	// displayed below it with the information of last name, first name,
	// company, time and i would like to add date as well") is the on-site
	// list as ONE form, same as its predecessor CheckOutPicker (U2) — still
	// one POST for the whole batch, not 96 individual ones
	// (design/bulk-movement-v1.md;
	// TestScriptedBatchCheckOutStaysWithinTheRoundTripBudget) — but FLAT
	// and column-per-fact instead of grouped by company/trade, per
	// design/ACCESS-APP-LAYOUT.md §2's lstOnSite.
	RosterTable rosterTableView

	// In / Out are the two vehicle forms. InProgress is "", gateInbound or
	// gateOutbound.
	//
	// ONLY ONE MAY BE IN PROGRESS AT A TIME, and that is a safety
	// requirement, not a simplification. With scripting off the accumulated
	// list lives in the submitted form's own fields, so a POST from one
	// direction's form carries nothing belonging to the other — touching
	// the outbound "add" button with twenty inbound passengers accumulated
	// would silently discard all twenty. So while one direction has rows,
	// the other renders as a pointer to it instead of a live form. One
	// truck is at the window at a time; this makes the page say so.
	InProgress string
	In         vehicleForm
	Out        vehicleForm

	// TimeWarnings are TP §4.6's warn-accept notices, carried across the
	// accept path's redirect as query codes (timecapture.go) and rendered
	// here as visible time-warning notices — a warn-accept whose warning
	// never renders is a suppressed warning (mutant m16's shape).
	TimeWarnings []string
	// DupWarnings are the "possible existing match" advisories for an
	// add-on-demand person this check-in just created (dupadvisory.go) —
	// same warn-accept shape as TimeWarnings, carried across the same
	// redirect. Never blocks anything; see dupadvisory.go's header for why
	// the query string carries ids, not names.
	DupWarnings []string
	// AboardWarnings (L17-15, fix3 2026-10-04) name a person this check-in
	// entered who is still recorded aboard another post's open run
	// (aboardwarn.go). Warn-accept: nothing was blocked.
	AboardWarnings []string
	// NotEntered ⟦gate-orientation-block, 2026-09-09⟧ are the occupants the
	// check-in or resume that just redirected here held OUT for lack of this
	// site's orientation (notentered.go) — the same warn-accept carriage as
	// the two above, but the copy names the NEXT ACTION, not just the fact.
	// Empty at an orientation=false post by construction: the domain never
	// partitions there.
	NotEntered []notEnteredNotice
	// StaleOrientTicks ⟦orientation-doors, gap 2, 2026-09-11⟧ names anyone
	// whose cross-site-warning tick went stale between accumulate and
	// commit and so was NOT authored (staletick.go) — same warn-accept
	// carriage as NotEntered above, but keyed to the TICK rather than to
	// the vehicle's final occupant list, because the person it names may no
	// longer be an occupant of this vehicle at all (see staletick.go's
	// header for why NotEntered alone cannot cover this case).
	StaleOrientTicks []staleOrientTickNotice
	// EditSelectNotice ⟦gate-checkin-spec, 2026-09-13⟧: the roster's "Edit
	// person" refusal, when the tick count was not exactly one
	// (gate_edit_select.go) — a REFUSAL rather than an accepted warning, so
	// unlike the notices above nothing here was ever recorded either way.
	EditSelectNotice string

	// DispatchDestinations feeds the OUTBOUND form's destination control —
	// config.DestinationList(), the same list the operator console's
	// dispatch buttons come from.
	DispatchDestinations []config.Destination
	// DispatchPrefill: each controlled destination's configured travel
	// minutes (config.TravelMinutesTo) — the outbound form's deadline
	// prefill, the same numbers the console's dispatch buttons use.
	// ⟦was: Phase/PhaseErr too — the weather-phase framing and gate for the
	// outbound form (weather-phases-v1 §4), and the prefill was read at the
	// CURRENT phase; removed with weather phases, James 2026-10-04⟧
	DispatchPrefill map[string]int64

	// GateMode ⟦R-28, team-lead, 2026-09-16⟧ names which of the two pages
	// this render is — "individual" or "vehicle" — read by _topbar.html to
	// point the header's small "switch mode" link at the OTHER one. It
	// never gates anything: both pages stay reachable regardless of this
	// value, exactly as they already were (renderGate/gateReturnPath's own
	// dispatch is unchanged) — this is display-only, set once per handler
	// below, never read by any authoring path.
	GateMode string
	// ArrivedNotice ⟦R-28⟧ is the one-click arrival's own result line —
	// "TRK-14 arrived: 10 checked in" (plus a held-out sentence, reusing
	// NotEntered above, when this post has orientation and someone lacked
	// it) — carried across the /trips/arrive redirect the same warn-accept
	// way TimeWarnings/DupWarnings/NotEntered already are. nil when this
	// render is not the page right after an arrival (arrivedNoticeFrom
	// returns nil whenever the query carries neither field it looks for).
	ArrivedNotice *arrivedNotice
}

// buildOnSiteRows is the history-test-critical function — see the file
// comment. Its signature is the guarantee: it cannot be handed a Company,
// Trade, or AliasMap, so it cannot be edited into re-resolving one by
// accident.
func buildOnSiteRows(visits []domain.Visit, siteID string, personName map[string]string) []onSiteRow {
	var out []onSiteRow
	for _, v := range visits {
		if v.SiteID != siteID || v.IsOpen != 1 {
			continue
		}
		row := onSiteRow{VisitID: v.VisitID, PersonID: v.PersonID, Person: personName[v.PersonID]}
		if row.Person == "" {
			row.Person = v.PersonID // roster row missing/not replicated yet: still show the id
		}
		if v.CompanySnapshotName != nil {
			row.Company = *v.CompanySnapshotName
		}
		if v.TradeSnapshotName != nil {
			row.Trade = *v.TradeSnapshotName
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Person < out[j].Person })
	return out
}

func (s *Server) gateFail(w http.ResponseWriter, what string, err error) {
	log.Printf("gate: %s: %v", what, err)
	http.Error(w, dbFailBody(err), http.StatusInternalServerError)
}

// handleGatePage ⟦R-27, James, 2026-09-16: "designed for persons that are
// retired and still hunt and peck for a keyboard ... checking in a single
// person should be on the top half ... the bottom half should be the onsite
// roster"⟧ is now the STREAMLINED individual page — one person at a time,
// one big button, the roster below. The whole-vehicle group entry this page
// used to open with MOVED to GET /gate/vehicle (handleGateVehiclePage,
// below) unchanged in behaviour — see gate_vehicle.html's own file comment.
func (s *Server) handleGatePage(w http.ResponseWriter, r *http.Request) {
	s.renderGateIndividual(w, r, http.StatusOK, vehicleForm{})
}

// handleGateVehiclePage ⟦R-27/R-28⟧ serves the pre-R-27 gate page, UNCHANGED
// in behaviour, at its new address — "making room" for R-28's linked-pair
// vehicle mode without building it. Every route it posts to
// (/gate/checkin, /gate/passenger/*, /gate/checkout*, /gate/resume,
// /gate/edit-selected) is the SAME route the individual page's own
// checkout-confirm reuses (see gate_vehicle_form's Compact mode) — see
// renderGate's own comment for how a refusal is routed back to the page the
// operator was actually on.
func (s *Server) handleGateVehiclePage(w http.ResponseWriter, r *http.Request) {
	s.renderGatePage(w, r, http.StatusOK, vehicleForm{}, "gate_vehicle.html")
}

// renderGateIndividual always renders the R-27 individual page — used by
// handleGatePage (a fresh GET, nothing posted yet) and by any handler that
// knows for certain it is answering that page's own POST (carries
// gate_page=individual — see renderGate below).
func (s *Server) renderGateIndividual(w http.ResponseWriter, r *http.Request, status int, form vehicleForm) {
	s.renderGatePage(w, r, status, form, "gate.html")
}

// renderGate is the shared refusal/re-render entry point EVERY existing
// gate handler already calls (handlePassengerAdd/Delete/Undo,
// commitGateCheckIn, handleGateVehicleCheckOut, handleGateCheckOut,
// handleGateCheckOutSelectForDeparture, handleGateResume, ...) — none of
// those call sites changed. What changed is that TWO pages now share this
// door: the individual page's own checkout-confirm box posts to the SAME
// routes the vehicle page's forms do (Compact mode on gate_vehicle_form,
// reused rather than a second departure path — see that template's own
// comment). A refusal must re-render the page the operator was ACTUALLY
// on, never the other one, so every form on the individual page carries a
// hidden gate_page=individual marker (see gate.html) that this function
// reads back off the very request it is refusing. A request that never
// carried the marker — every vehicle-page form, unchanged — falls through
// to the vehicle page exactly as it always rendered "gate.html" (now
// "gate_vehicle.html") before this split existed.
func (s *Server) renderGate(w http.ResponseWriter, r *http.Request, status int, form vehicleForm) {
	if r.FormValue("gate_page") == "individual" {
		s.renderGateIndividual(w, r, status, form)
		return
	}
	s.renderGatePage(w, r, status, form, "gate_vehicle.html")
}

// gateReturnPath is renderGate's own dispatch rule, restated for the
// handlers that redirect (303) on SUCCESS rather than re-rendering (a
// refusal): commitGateCheckIn, handleGateVehicleCheckOut and
// handleGateCheckOut all route BOTH pages' forms through one door each
// (the single check-in route and the outbound commit/remove routes are
// shared — see gate_vehicle_form's own Compact comment), and a successful
// action must land the officer back on the page they were actually
// working, never always "/gate". Every vehicle-page form is unmarked and
// gets its unchanged default, "/gate/vehicle".
func gateReturnPath(r *http.Request) string {
	if r.FormValue("gate_page") == "individual" {
		return "/gate"
	}
	return "/gate/vehicle"
}

// renderGatePage builds and writes the whole gate page, with `form` as the
// in-progress vehicle (a zero vehicleForm means nothing is in progress), to
// whichever of the two templates the caller names. Every path that shows a
// gate page goes through here — the page load, each accumulate step, and
// every refusal — so a refusal cannot accidentally render a different page
// from the one the operator was working in. The data assembled is the same
// superset for both pages (gate.html and gate_vehicle.html each read only
// the parts they render); splitting the DATA rather than the BUILDING is
// what keeps this one function instead of two drifting copies.
func (s *Server) renderGatePage(w http.ResponseWriter, r *http.Request, status int, form vehicleForm, templateName string) {
	// W4: every refusal this door re-renders reaches the officer through
	// the one mapper (refusal.go) — a raw "domain: dispatch: …" never does.
	form.Error = officerText(form.Error)
	persons, err := domain.Persons(s.Store)
	if err != nil {
		s.gateFail(w, "persons", err)
		return
	}
	// Same name-ordering fix as trips.go: person ids are crypto/rand hex, so the
	// default order is effectively random. See the comment there.
	sort.Slice(persons, func(i, j int) bool { return persons[i].Name < persons[j].Name })
	personName := make(map[string]string, len(persons))
	// personLastName/personFirstName (GATE-ACCESS-STYLE, 2026-09-12): the
	// roster table's Last/First columns read these AUTHORED facts
	// (domain.Person.LastName/FirstName — W5a's own optional parts,
	// omitempty) directly, never by parsing Name — D-48 forbids deriving a
	// part from Name, and TestW5aNothingParsesAName's AST scan enforces it
	// on every production .go file, this one included. See
	// rosterDisplayName for what a person with neither part on file shows.
	personLastName := make(map[string]string, len(persons))
	personFirstName := make(map[string]string, len(persons))
	for _, p := range persons {
		personName[p.PersonID] = p.Name
		if p.LastName != nil {
			personLastName[p.PersonID] = *p.LastName
		}
		if p.FirstName != nil {
			personFirstName[p.PersonID] = *p.FirstName
		}
	}

	// Indexed open-set read (F30): buildOnSiteRows' own site/open filter is
	// now a no-op guard — the read already returns exactly this site's open
	// visits, at headcount cost rather than season cost.
	visits, err := domain.OpenVisitsAt(s.Store, s.Cfg.SiteID)
	if err != nil {
		s.gateFail(w, "visits", err)
		return
	}
	onSite := buildOnSiteRows(visits, s.Cfg.SiteID, personName)
	// R-23: which of these this post never saw walk in (the peer's copy of
	// our own earlier records, recovered after a rebuild). Marked, counted,
	// still listed, still tickable.
	restoredOnSite := markRestoredRows(onSite, visits, s.Cfg.SiteID, s.Cfg.NodeEpoch)
	// M-6 / T-L08-C (James, 2026-10-03): which of these the record ALSO holds
	// on site at a peer right now — flagged, counted, still listed, still
	// tickable; nothing is written anywhere. Loud on failure, never fatal to
	// the gate (a truck at the window must still be entered): the roster
	// then says the flags could not be read rather than showing none.
	elsewhereOnSite, elsewhereErr := 0, ""
	if tags, err := s.elsewhereTags(s.Cfg.SiteID, visits); err != nil {
		log.Printf("gate: also on site elsewhere: %v", err)
		elsewhereErr = err.Error()
	} else {
		elsewhereOnSite = markElsewhereRows(onSite, tags)
	}
	// The sign-in date on each row (James, 2026-09-10; gate_signin.go).
	// Loud on failure, never fatal: the roster still lists everyone.
	signInErr := s.decorateSignIns(onSite, visits)
	if signInErr != "" {
		log.Printf("gate: sign-in times: %s", signInErr)
	}
	onSiteNow := make(map[string]bool, len(onSite))
	for _, row := range onSite {
		onSiteNow[row.PersonID] = true
	}

	// Company NAMES for the picker label only (see rosterPerson.Company).
	// One dump, not one lookup per person: this runs on every gate render,
	// including every accumulate step of a 26-person bus.
	companies, err := domain.Companies(s.Store)
	if err != nil {
		s.gateFail(w, "companies", err)
		return
	}
	companyName := make(map[string]string, len(companies))
	for _, c := range companies {
		companyName[c.CompanyID] = c.Name
	}
	// ⟦[[D-84]]⟧ Trade NAMES too — the row shows a known person's trade off
	// the record, and the picker lists the register. Same one-dump-per-render
	// stance as companies above.
	trades, err := domain.Trades(s.Store)
	if err != nil {
		s.gateFail(w, "trades", err)
		return
	}
	tradeName := make(map[string]string, len(trades))
	for _, t := range trades {
		tradeName[t.TradeID] = t.Name
	}
	// The two picker lists, in intake's shape: EVERY row, name-sorted, id as
	// the tiebreak (intake.go builds its data.Companies / data.Trades the
	// same way, for the same reason — ids are random hex, so stored order is
	// a random list).
	// James, 2026-10-04 (D21): same-name rows carry the "(added at <post>)"
	// origin label here exactly as at intake — reuse, not a second rule.
	companyOptions := make([]refOption, 0, len(companies))
	for i, c := range companies {
		o := refOption{ID: c.CompanyID, Name: c.Name}
		for j, other := range companies {
			if i != j && domain.SameCompanyName(c.Name, other.Name) {
				o.Origin = s.peerDisplayName(c.SiteID)
				break
			}
		}
		companyOptions = append(companyOptions, o)
	}
	tradeOptions := make([]refOption, 0, len(trades))
	for i, t := range trades {
		o := refOption{ID: t.TradeID, Name: t.Name}
		for j, other := range trades {
			if i != j && domain.SameTradeName(t.Name, other.Name) {
				o.Origin = s.peerDisplayName(t.SiteID)
				break
			}
		}
		tradeOptions = append(tradeOptions, o)
	}
	// The same rule for the person type-ahead: a name shared by two live
	// (non-retired) records gets the post that added each.
	personNameCount := map[string]int{}
	for _, p := range persons {
		if !p.IsRetired() {
			personNameCount[foldPersonName(p.Name)]++
		}
	}
	sortRefOptions(companyOptions)
	sortRefOptions(tradeOptions)

	roster := struct {
		Persons []rosterPerson `json:"persons"`
	}{Persons: []rosterPerson{}} // [] not null when logged out: the script does roster.persons || []
	// ⟦PII-1, D-56⟧ /gate and /gate/vehicle are UNGATED by design (§12), so
	// this payload — name + employer + phone for EVERY person — would go to
	// anyone who can reach the port. The real rule (R-24's "behind a login"
	// premise was false: this handler has no session check): the roster
	// payload is emitted only to a request with an authenticated session
	// (authedSession, which also honours the demo X-SC-Proxy-Auth path).
	// Logged out, persons is empty: the page still renders and the script
	// reads an empty list, so type-ahead simply offers nothing — and a
	// logged-out visitor cannot author anything anyway.
	_, authed := s.authedSession(w, r)
	// ⟦X-R12-JSON⟧ THIS post's orientation register, read once per render
	// (one query, not one per person) and only where it can be used — see
	// rosterPerson.OrientNeeded. orientKnown false (a road post, a logged-out
	// request, or a failed read) leaves both flags unset on every person.
	var heldHere map[string]bool
	var heldFrom map[string]string // ⟦R4⟧ canonical id → merged-away record's name
	var orientMerge domain.AliasMap
	orientKnown := false
	if authed && s.Cfg.HasOrientation() {
		merge, err := domain.LoadPersonMergeMap(s.Store)
		var holders map[string]domain.OrientationHolding
		if err == nil {
			// ⟦R4, James, 2026-10-04⟧ keyed by CANONICAL id and counting a
			// completion recorded under any record merged into it.
			holders, err = domain.OrientationHoldersAt(s.Store, merge, s.Cfg.SiteID)
		}
		if err == nil {
			heldHere = make(map[string]bool, len(holders))
			for id, h := range holders {
				heldHere[id] = true
				if h.CarriedFrom != "" {
					if heldFrom == nil {
						heldFrom = map[string]string{}
					}
					heldFrom[id] = s.mergedRecordName(h.CarriedFrom)
				}
			}
			orientMerge = merge
			orientKnown = true
		}
	}
	for _, p := range persons {
		if !authed {
			break
		}
		// ⟦R-118 / [[D-21.5]], 2026-10-03⟧ a RETIRED record is not in the
		// type-ahead: "the person stops being selectable". The on-site table
		// above is built from VISITS and still lists a retired person who is
		// here; only the picker payload drops them. (W5's one-line touch on
		// this file; the rule itself is domain.Person.IsRetired.)
		if p.IsRetired() {
			continue
		}
		rp := rosterPerson{ID: p.PersonID, Name: p.Name, OnSite: onSiteNow[p.PersonID]}
		if personNameCount[foldPersonName(p.Name)] > 1 {
			rp.Origin = s.peerDisplayName(p.SiteID)
		}
		if p.CompanyID != nil {
			// A company row this site has not replicated yet leaves the
			// label blank rather than showing a hex id in a dropdown the
			// operator reads at a glance — the name is still there, and the
			// roster screen is where an unresolved reference is worth
			// showing in full.
			rp.Company = companyName[*p.CompanyID]
			rp.CompanyID = *p.CompanyID
		}
		if p.TradeID != nil {
			rp.TradeID = *p.TradeID // the id only — see rosterPerson.TradeID
		}
		// Position ⟦gate-checkin-spec, 2026-09-13⟧: carried; see
		// rosterPerson.Position's comment for why Last/First are not.
		if p.Position != nil {
			rp.Position = *p.Position
		}
		// Phone ⟦R-24, 2026-09-13⟧: carried the same way, now that James has
		// ruled the gate screen may show it — see rosterPerson.Phone's own
		// comment. Authenticated requests only (PII-1, loop guard above).
		if p.Phone != nil {
			rp.Phone = *p.Phone
		}
		if orientKnown {
			// ⟦X-MERGE-WARN, 2026-10-03⟧ through the merge, exactly as the
			// commit reads it (domain buildArrivalVisits: merge.Resolve before
			// OrientationHeld) — see decorateRows.
			rp.OrientHeld = heldHere[orientMerge.Resolve(p.PersonID)]
			rp.OrientNeeded = !rp.OrientHeld
			rp.OrientFrom = heldFrom[orientMerge.Resolve(p.PersonID)]
		}
		roster.Persons = append(roster.Persons, rp)
	}

	// ⟦[[D-84]]⟧ SHOW, DO NOT ASK: resolve each accumulated row to its roster
	// person (by the commit's own identity rule) so the row can display the
	// record and pre-select the pickers from it. Done here, at the one
	// render choke point, so every re-render — add, delete, undo, a refused
	// commit — shows the same thing the commit will snapshot.
	s.decorateRows(&form, companyName, tradeName)
	form.GroupDrivers = s.groupDriverViews(form)
	if !authed {
		redactRecordFields(&form)
	}
	// encoding/json's default Marshal escapes <, >, & to </>/&
	// (its documented HTML-safe default), so this blob can never contain a
	// literal "</script>" no matter what a person's name is — safe to embed
	// verbatim inside the inline <script> block below.
	rosterJSON, err := json.Marshal(roster)
	if err != nil {
		s.gateFail(w, "roster json", err)
		return
	}

	incomplete, err := s.incompleteCheckInRows(personName) // row 31 (F1: also shown on /muster)
	if err != nil {
		s.gateFail(w, "incomplete check-ins", err)
		return
	}
	if !authed {
		// ⟦F-L03-R3-2⟧ same rule as redactRecordFields: an anonymous page
		// names nobody's orientation status. The resume form itself stays —
		// its POST is requireLogin.
		for i := range incomplete {
			incomplete[i].NeedsOrientation = nil
		}
	}

	// ⟦R-28⟧ GateMode names the page BY TEMPLATE, never by session — a
	// direct GET /gate/vehicle from a bookmark or the switch-mode link
	// renders as "vehicle" regardless of which mode the current session
	// logged in under, matching how neither page has ever been gated by
	// session state. templateName is one of the two literal strings
	// registerGateRoutes' own two handlers pass; anything else (there is
	// no third caller) reads as "individual", the safer/older default.
	gateMode := "individual"
	if templateName == "gate_vehicle.html" {
		gateMode = "vehicle"
	}
	data := gateData{
		TimeWarnings:         timeWarningsFrom(r),
		DupWarnings:          dupWarningsFrom(r, personName),
		AboardWarnings:       s.aboardWarningsFrom(r, personName),
		NotEntered:           notEnteredNoticesFrom(r, personName, s.Cfg.DisplayName),
		StaleOrientTicks:     staleOrientTickNoticesFrom(r, personName, s.Cfg.DisplayName),
		EditSelectNotice:     editSelectNoticeFrom(r),
		SiteID:               s.Cfg.SiteID,
		Cfg:                  s.Cfg,
		HasOrientation:       s.Cfg.HasOrientation(), // ⟦R-12⟧ this post's own flag, for the open-box note
		RosterJSON:           template.JS(rosterJSON),
		Companies:            companyOptions,
		Trades:               tradeOptions,
		OnSite:               onSite,
		SignInErr:            signInErr,
		ElsewhereErr:         elsewhereErr,
		Incomplete:           incomplete,
		DispatchDestinations: s.Cfg.DestinationList(),
		GateMode:             gateMode,
		ArrivedNotice:        arrivedNoticeFrom(r),
		Shift:                s.shiftTotalsFor(r),
	}

	// The inbound list (James, 2026-08-21). Loud on failure, never fatal:
	// an officer with a truck at the window must still be able to enter it
	// by hand if this list cannot be built.
	if inbound, err := s.inboundVehicleRows(); err != nil {
		log.Printf("gate: inbound list: %v", err)
		data.InboundErr = refusalText(err)
	} else {
		data.Inbound = inbound
	}
	// James, 2026-10-03: the working-on-the-road list, off its own scan read
	// (the inbound list above runs one too; the vehicle page is rendered
	// once per truck, not per tick, so a second Evaluate is cheap and keeps
	// the two builders independent). Loud, never fatal, like Inbound.
	data.RoadWorkChoice = roadWorkChoiceFor(s.Cfg)
	{
		alertOf := map[string]deadline.Alert{}
		if s.Alerts != nil {
			if board, err := s.Alerts.Evaluate(); err != nil {
				log.Printf("gate: road-work list: deadline scan: %v", err)
			} else {
				for _, a := range board.Alerts {
					alertOf[a.TripID] = a
				}
			}
		}
		if rw, err := s.roadWorkRows(alertOf); err != nil {
			log.Printf("gate: road-work list: %v", err)
			data.RoadWorkErr = refusalText(err)
		} else {
			data.RoadWork = rw
		}
	}

	// The deadline prefill for the OUTBOUND form: one configured number per
	// controlled destination. ⟦was: read through the weather-phase fold at
	// the current phase, with a PhaseErr slot — weather phases removed, James
	// 2026-10-04⟧
	data.DispatchPrefill = map[string]int64{}
	for _, d := range data.DispatchDestinations {
		if !d.Controlled {
			continue // an uncontrolled run carries no deadline at all
		}
		if m, ok := s.Cfg.TravelMinutesTo(d.ID); ok {
			data.DispatchPrefill[d.ID] = m
		}
	}

	switch form.Direction {
	case gateInbound:
		form.Direction = gateInbound
		data.In = form
		if len(form.Rows) > 0 || strings.TrimSpace(form.Vehicle) != "" || form.Error != "" {
			data.InProgress = gateInbound
		}
	case gateOutbound:
		data.Out = form
		if len(form.Rows) > 0 || strings.TrimSpace(form.Vehicle) != "" || form.Error != "" {
			data.InProgress = gateOutbound
		}
	}
	data.In.Direction = gateInbound
	data.Out.Direction = gateOutbound

	// SAFE DEFAULT for the check-in correction picker — derived for THIS
	// action, not copied from the arrival forms on the console.
	//
	// WHAT THIS PICKER IS, SINCE D-4. It is NOT a departure path. Under D-4
	// nobody leaves this site except in a vehicle, to somewhere, so a
	// departure is always the departing-vehicle form above. What this list
	// does is take a name OFF the muster with no vehicle trail, and the only
	// honest reason to do that is that the check-in was wrong: the wrong
	// person was picked, somebody was entered twice, or somebody was
	// recorded arriving who never got out of the truck. See
	// handleGateCheckOut for why it is kept rather than removed.
	//
	// The safe default is unchanged and its derivation still holds. Removing
	// somebody from the muster is the dangerous direction, so every box
	// starts UNTICKED: a box left unticked by mistake leaves that person
	// recorded as still on site, the muster OVER-reports, and the cost is
	// wasted search time. The inverse default — pre-ticked, untick the ones
	// who are staying — would mean one hurried submit, or one stale page
	// rendered before somebody walked back in, silently clears a person who
	// is still standing on the pad off the muster. That is the direction
	// that hides a live person, and bulk-movement-v1.md names it as the
	// sharpest risk in this whole change: the inbound board's checkboxes
	// default the other way, and copying that shape here BECAUSE IT IS THE
	// SAME WIDGET is exactly the mistake to avoid.
	rosterRows, rosterSameNames := buildRosterTableRows(onSite, personLastName, personFirstName)
	data.RosterTable = rosterTableView{
		FieldName: "visit_ids", IDPrefix: "checkout-picker",
		TimeOut:   true,
		Verb:        "remove from the muster at " + s.Cfg.SiteID,
		SafeDefault: "correctly checked in and staying on site. Nothing is pre-ticked: a missed tick leaves the wrong name on the muster, while a wrong tick removes somebody who IS here — a person nobody goes looking for. This does not record a departure: a departure is the departing-vehicle form above. Two rows carrying one name are two records, each showing the tail of its person id; if you cannot tell which of them is the mistaken record, tick neither — a name left on the muster is the safe error.",
		// TWO ROWS, ONE NAME. Two open visits under two person ids sharing
		// a name used to draw two identical tick-boxes here, and the wrong
		// tick removes a person who is still on site from the muster with
		// no reopen path. The rows now carry a person-id tail, but a tail
		// is not something an officer can choose BY; this sentence is what
		// they can act on. It points at the over-count, which is the safe
		// direction, and it says in terms that a pair is never "tick
		// both". ⚠ The duplicate is made visible; the discriminator an
		// officer could choose BY — the check-in time — is available with
		// no schema change and is the plan owner's choice (picker.go,
		// refSameNameRows). Even with it, two names off one vehicle share
		// one time, so this sentence is what the officer has in that case
		// too.
		SameNameAdvice: "These records share one name, and each row shows the tail of its person id so they can be told apart. Tick only the record you know is the mistaken check-in — never both. If you cannot tell which one is wrong, tick neither: a name left on the muster is the safe error, and a name wrongly removed is a person nobody goes looking for.",
		Empty:          "Nobody on site. There is nothing to correct.",
		SameNames:      rosterSameNames,
		Rows:           rosterRows,
		Total:          len(rosterRows),
		Restored:       restoredOnSite,
		Elsewhere:      elsewhereOnSite,
	}

	data.Banner, data.BannerErr = s.banner() // C7: the banner rides every screen (§9.4)
	data.Post = s.Cfg.PostName()             // D-5
	data.Anonymous = !authed                 // S-034

	s.renderPageStatus(w, status, "gate", templateName, data)
}

// sortRefOptions orders a picker list by name, then id — intake.go's order.
// foldPersonName is the gate's own same-name fold for people (TrimSpace +
// EqualFold, inner whitespace collapsed as a browser would).
func foldPersonName(n string) string {
	return strings.ToLower(strings.Join(strings.Fields(n), " "))
}

func sortRefOptions(opts []refOption) {
	sort.Slice(opts, func(i, j int) bool {
		if opts[i].Name != opts[j].Name {
			return opts[i].Name < opts[j].Name
		}
		return opts[i].ID < opts[j].ID
	})
}

// noneOnFile is what the row shows for a known person with no company or no
// trade on their record, and the label of the pickers' empty option.
const noneOnFile = "none on file"

// notOnRoster is what the row shows for a person who is not on the roster
// and will be ADDED by the commit (the arriving form) — the [[D-66]] state.
const notOnRoster = "Not on the roster — this will add them"

// decorateRows ⟦[[D-84]]⟧ resolves every accumulated row to the roster
// person it names and fills the DISPLAY fields (Known, OrgLabel, Asked)
// from the record. It is the render-time twin of rowPersonSlot and applies
// the SAME identity rule: the hidden id is honoured only while it still
// names a person whose roster name agrees with the visible text; otherwise
// the typed text is resolved afresh — exactly one case-insensitive match is
// that person, none is a new person, more than one is left for the commit
// to refuse loudly.
//
// THREE THINGS IT DOES TO THE ROW'S POSTED FIELDS, each stated because each
// is a deliberate exception to "rows are re-emitted verbatim":
//
//  1. A FRESHLY resolved known person (the row arrived with no agreeing id —
//     the JS-off add, or a name edited to somebody else) has their id
//     written into Value and their record's company/trade written into
//     the pickers. That is the scripted path's `adopt` done server-side,
//     and it is what makes "show, do not ask" true with scripting off: the
//     pickers start AT the record, so an untouched row commits with no
//     change and no confirmation needed.
//  2. A STALE pick that now resolves to a NEW person (the name was edited
//     away from the picked person) has Value cleared and the pickers reset
//     to none — the old person's pre-selection must not become the new
//     person's company and trade by inheritance.
//  3. A STABLE row (id agrees with the text, or a deliberate off-roster
//     add) keeps its posted pickers untouched, so an officer's in-progress
//     change survives every accumulate step and every refused commit.
//
// companyTradeNameMaps is the two-dump lookup renderGate's own body builds
// inline, before it calls decorateRows — factored out ONLY for
// handleGateCheckInSingle (R-25), which needs the identical maps to
// decorate its one synthetic row without going through a full page render.
// renderGate is deliberately left as it was: it is a central, heavily
// tested function, and duplicating ten lines here costs less than the risk
// of reshaping its own error path.
func (s *Server) companyTradeNameMaps() (companyName, tradeName map[string]string, err error) {
	companies, err := domain.Companies(s.Store)
	if err != nil {
		return nil, nil, err
	}
	companyName = make(map[string]string, len(companies))
	for _, c := range companies {
		companyName[c.CompanyID] = c.Name
	}
	trades, err := domain.Trades(s.Store)
	if err != nil {
		return nil, nil, err
	}
	tradeName = make(map[string]string, len(trades))
	for _, t := range trades {
		tradeName[t.TradeID] = t.Name
	}
	return companyName, tradeName, nil
}

// Nothing here writes to the store; it reads the roster the page already
// loads.
func (s *Server) decorateRows(f *vehicleForm, companyName, tradeName map[string]string) {
	var cached []domain.Person
	loaded := false
	all := func() ([]domain.Person, error) {
		if !loaded {
			p, err := domain.Persons(s.Store)
			if err != nil {
				return nil, err
			}
			cached, loaded = p, true
		}
		return cached, nil
	}
	lookup := func(id string) (*domain.Person, error) { return domain.PersonByID(s.Store, id) }
	// ⟦D-88 cross-site warning, 2026-09-10⟧ meaningful ONLY inbound (checking
	// OUT owes no orientation) and ONLY at a post that owns an orientation
	// concept at all (D-88: a road post has none — HasOrientation() false).
	checkOrientation := f.Direction == gateInbound && s.Cfg.HasOrientation()
	// ⟦X-MERGE-WARN, 2026-10-03⟧ the person-merge map, loaded once, so the
	// warning reads the register THROUGH THE MERGE exactly as the commit
	// does. ⟦was: OrientationHeld(..., Known.PersonID, ...) on the RAW id⟧ —
	// a merged-away id whose canonical record holds this post's orientation
	// was warned "NOT ORIENTED HERE … will not be checked in … held out
	// exactly as today", then ENTERED by the commit, because
	// buildArrivalVisits resolves the merge before its read (L05 r3, pad:
	// Fendahl → Dunby). The warning now says what the commit will do. A map
	// that cannot be read leaves both flags unset, like a failed register read.
	var merge domain.AliasMap
	mergeOK := false
	if checkOrientation {
		if m, err := domain.LoadPersonMergeMap(s.Store); err == nil {
			merge, mergeOK = m, true
		}
	}
	for i := range f.Rows {
		decorateRow(&f.Rows[i], lookup, all, companyName, tradeName)
		if f.Direction != gateOutbound {
			dropRetiredCandidates(&f.Rows[i])
		}
		if checkOrientation && mergeOK && f.Rows[i].Known != nil {
			// The SAME read buildArrivalVisits makes live at commit
			// (tripauthor.go) — never a snapshot, never cached across rows:
			// two rows naming the same person must agree.
			// ⟦R4⟧ through the merge in both directions, as the commit reads.
			held, err := domain.OrientationHeldThroughMerge(s.Store, merge, f.Rows[i].Known.PersonID, s.Cfg.SiteID)
			f.Rows[i].NeedsOrientation = err == nil && held == nil
			f.Rows[i].HoldsOrientation = err == nil && held != nil
			f.Rows[i].OrientationCarriedFrom = ""
			if err == nil && held != nil && held.CarriedFrom != "" {
				f.Rows[i].OrientationCarriedFrom = s.mergedRecordName(held.CarriedFrom)
			}
		}
	}
}

// redactRecordFields ⟦PII-1b, 2026-09-30⟧ blanks, for a request with NO
// authenticated session, every field decorateRows copied off a roster
// RECORD: a known row's company, trade, position, phone and its
// "company · trade" label, and each duplicate-offer candidate's label.
//
// The accumulate routes (/gate/passenger/add, delete, undo,
// /gate/checkout/select) are ungated by design — they author nothing — and
// every one of them renders through renderGatePage. PII-1 emptied the
// roster JSON for a logged-out caller, but decorateRow still filled an
// exactly-typed roster name's row FROM THE RECORD, so one anonymous POST per
// name (names being readable off the ungated /roster and /trips) harvested
// the whole directory's phone + employer + trade: the D-56 exposure again,
// one person at a time. A logged-out caller cannot commit anyway (every
// commit is requireLogin), so a blank here costs nothing; the name, the
// row's identity (Kind/Value) and anything the caller typed on a NEW row
// are left alone. Logged in, this is never called — behaviour unchanged.
func redactRecordFields(f *vehicleForm) {
	for i := range f.Rows {
		row := &f.Rows[i]
		if row.Known != nil {
			row.CompanyID, row.Company, row.TradeID, row.Trade = "", "", "", ""
			row.Position, row.Phone = "", ""
			row.OrgLabel = ""
		}
		for j := range row.Candidates {
			row.Candidates[j].Org = ""
		}
		tagSameCandidates(row.Candidates) // blank labels can collide where full ones did not
		// ⟦F-L03-R3-2, ruling 2026-10-03: "orientation flags logged-out:
		// hide from anonymous responses"⟧ the NOT ORIENTED HERE block (and
		// the "on file" box) read this post's orientation register for a
		// named person — one anonymous POST per name read the whole register.
		// Nothing is held out by this: the commit, which an anonymous caller
		// cannot reach anyway, still reads OrientationHeld live.
		row.NeedsOrientation, row.HoldsOrientation = false, false
		row.OrientationCarriedFrom = ""
	}
}

// mergedRecordName is a person's roster name, or "another record" when it
// cannot be read — never a raw id on an officer's screen.
func (s *Server) mergedRecordName(id string) string {
	if p, err := domain.PersonByID(s.Store, id); err == nil && p != nil && strings.TrimSpace(p.Name) != "" {
		return p.Name
	}
	return "another record"
}

func decorateRow(row *passengerRow, lookup func(string) (*domain.Person, error), persons func() ([]domain.Person, error), companyName, tradeName map[string]string) {
	row.Known = nil
	typed := strings.TrimSpace(row.Typed)
	label := func(p *domain.Person) string {
		co, tr := noneOnFile, noneOnFile
		if p.CompanyID != nil {
			if n := companyName[*p.CompanyID]; n != "" {
				co = n
			}
		}
		if p.TradeID != nil {
			if n := tradeName[*p.TradeID]; n != "" {
				tr = n
			}
		}
		return co + " · " + tr
	}
	fromRecord := func(p *domain.Person) {
		row.CompanyID, row.Company, row.TradeID, row.Trade = "", "", "", ""
		if p.CompanyID != nil {
			row.CompanyID = *p.CompanyID
		}
		if p.TradeID != nil {
			row.TradeID = *p.TradeID
		}
		// Position / Phone ⟦gate-checkin-spec, 2026-09-13⟧: same "pickers
		// start AT the record" rule as Company/Trade above — SHOW, DON'T ASK
		// ([[D-84]]). A known row's NewLast/NewFirst are NOT reset
		// here: they are meaningless for a known row (nothing amends name
		// parts through this door) and are never read once row.Known != nil.
		row.Position, row.Phone = "", ""
		if p.Position != nil {
			row.Position = *p.Position
		}
		if p.Phone != nil {
			row.Phone = *p.Phone
		}
	}
	finishKnown := func(p *domain.Person) {
		row.Known = p
		row.OrgLabel = label(p)
		// The change block stays shut on an untouched row; it opens while a
		// change is in progress so the officer can see what they are about
		// to confirm — and so a refused, unconfirmed change is on screen.
		row.Asked = row.AmendConfirmed || rowDiffersFromRecord(*row, p)
	}
	finishNew := func() {
		row.Known = nil
		row.OrgLabel = notOnRoster
		row.Asked = true // [[D-66]]: a new person is ASKED
		// The duplicate OFFER (person_match.go). Optional in the same sense
		// the post-commit advisory is: a roster read that fails costs the
		// note, never the row — an officer with a truck at the window still
		// gets the person added. No phone is passed: the gate collects none
		// (D-52), so only the name arm can fire here.
		row.Candidates = nil
		if typed == "" {
			return
		}
		roster, err := persons()
		if err != nil {
			return
		}
		for _, m := range personMatchCandidates(roster, typed, "") {
			p := m.Person
			row.Candidates = append(row.Candidates, rowCandidate{PersonID: p.PersonID, Name: p.Name, Org: label(&p), Retired: p.IsRetired()})
		}
		tagSameCandidates(row.Candidates)
	}
	if typed == "" {
		finishNew()
		return
	}
	// A deliberate off-roster add (the scripted path's kind=new): stable.
	if row.Kind == "new" && strings.EqualFold(strings.TrimSpace(row.Value), typed) {
		finishNew()
		return
	}
	stale := false
	if row.Kind != "new" && strings.TrimSpace(row.Value) != "" {
		p, err := lookup(strings.TrimSpace(row.Value))
		if err == nil && p != nil && strings.EqualFold(strings.TrimSpace(p.Name), typed) {
			finishKnown(p) // stable: the pickers are the officer's
			return
		}
		stale = true
	}
	roster, err := persons()
	if err != nil {
		finishNew()
		return
	}
	slot, err := resolveTypedPerson(typed, roster)
	if err != nil {
		// Ambiguous: the commit will refuse it by name. Shown as an ask so
		// nothing is claimed about a record this row cannot yet name.
		if stale {
			row.Value = ""
			row.CompanyID, row.Company, row.TradeID, row.Trade = "", "", "", ""
			row.Position, row.Phone = "", ""
		}
		finishNew()
		return
	}
	if slot.ExistingID != "" {
		for i := range roster {
			if roster[i].PersonID == slot.ExistingID {
				p := &roster[i]
				row.Value = p.PersonID // adopt, as the script does
				fromRecord(p)          // the pickers start AT the record
				finishKnown(p)
				return
			}
		}
	}
	// A new person. A stale pick's pre-selection belonged to somebody else.
	if stale {
		row.Value = ""
		row.CompanyID, row.Company, row.TradeID, row.Trade = "", "", "", ""
		row.Position, row.Phone = "", ""
	}
	finishNew()
}

// hydrateBlankFromRecord ⟦H-1 / F-L02-1, 2026-09-29⟧ fills every
// record-derived field the individual page's row left BLANK — company,
// trade, position, phone — from the known person's record. It is the
// individual door's own reading of "a blank asserts nothing" ([[D-84]]):
// that page has no trade, position or phone box at all, and its scripted
// pick hides the company picker behind a read-only line without ever
// setting it, so a scripted pick posts new_company_id="" and no trade. The
// stable-pick branch of decorateRow (rightly, for the vehicle page's
// in-progress changes) keeps posted pickers as they are, and recordChanges
// reads a blank company/trade as CLEAR — so every scripted pick of a known
// person with an employer was refused, coaching a "change their record"
// tick this page does not and must not have (FIX-CONSTRAINTS F-L02-1: tick
// + blank company = ClearCompany, an erased employer). A NON-blank posted
// value is left alone: a genuine disagreement is still refused, honestly.
//
// Only the individual door calls this. The vehicle page's pickers are
// visible, carry the record from the moment the row is added (its script
// clones them from the roster, the JS-off render adopts them in
// decorateRow), and "none on file" + the explicit tick there is a real,
// confirmed clear — unchanged.
func hydrateBlankFromRecord(row *passengerRow, p *domain.Person) {
	fill := func(dst *string, v *string) {
		if strings.TrimSpace(*dst) == "" && v != nil {
			*dst = *v
		}
	}
	if row.CompanyID == "" {
		row.Company = ""
		fill(&row.CompanyID, p.CompanyID)
	}
	if row.TradeID == "" {
		row.Trade = ""
		fill(&row.TradeID, p.TradeID)
	}
	fill(&row.Position, p.Position)
	fill(&row.Phone, p.Phone)
}

// rowDiffersFromRecord reports whether the row's pickers, as posted, name
// something other than the person's record — the condition under which a
// commit needs the confirmation tick. The add-new escape with a typed name
// counts as a difference (it will be resolved at commit); the escape with a
// BLANK name does not — the officer picked "not listed" and typed nothing,
// which is treated as untouched rather than refused ([[D-66]]/[[D-71]]: the
// escape never refuses).
func rowDiffersFromRecord(row passengerRow, p *domain.Person) bool {
	cur := func(id *string) string {
		if id == nil {
			return ""
		}
		return *id
	}
	differs := func(sel, typedNew, current string) bool {
		if sel == newRef {
			return strings.TrimSpace(typedNew) != ""
		}
		return sel != current
	}
	// Position / Phone ⟦gate-checkin-spec, 2026-09-13⟧: plain free text, no
	// select/escape pair and no fold-by-name resolution. A BLANK box asserts
	// NOTHING here — the same "empty is absent, not an assertion of
	// empty-string" rule handlePersonAmend already follows — so it is never
	// a difference, only a NON-blank box that disagrees with the record is.
	// This is also what makes a JS-cloned known row safe when the script had
	// no phone number to pre-fill it with (rosterPerson's own comment, D-56):
	// the box renders blank, and blank never proposes a change, let alone a
	// silent clear of a real value the officer never saw or touched.
	factDiffers := func(typedFact, current string) bool {
		typedFact = strings.TrimSpace(typedFact)
		return typedFact != "" && typedFact != current
	}
	return differs(row.CompanyID, row.Company, cur(p.CompanyID)) ||
		differs(row.TradeID, row.Trade, cur(p.TradeID)) ||
		factDiffers(row.Position, cur(p.Position)) ||
		factDiffers(row.Phone, cur(p.Phone))
}

// at returns sl[i], or "" if i is out of range — parallel-array form fields
// (occupant_kind / occupant_value / ...) can legitimately arrive with
// mismatched lengths if a browser ever drops a disabled/empty field.
func at(sl []string, i int) string {
	if i < len(sl) {
		return sl[i]
	}
	return ""
}

// --- the accumulator -------------------------------------------------

// isPassengerListForm reports whether this POST came from the accumulating
// vehicle form rather than from a pre-existing (or hand-crafted) check-in
// body. The marker is what makes the two shapes distinguishable WITHOUT
// guessing: an unchecked radio group submits nothing at all, so "no
// driver_index" cannot by itself tell "the new form with nobody flagged"
// apart from "an old-shaped body". The marker can tell them apart, and
// every stricter rule below (a driver is mandatory, indices must be
// contiguous, a blank row is refused rather than skipped) hangs off it.
func isPassengerListForm(r *http.Request) bool {
	return r.FormValue("passenger_list") == "1"
}

// parseVehicleForm reads the in-progress vehicle out of a POST body. It
// validates nothing and refuses nothing: an accumulate step must be able to
// carry a half-finished, internally inconsistent list forward, because the
// operator is in the middle of building it.
func parseVehicleForm(r *http.Request) vehicleForm {
	f := vehicleForm{
		Direction:      r.FormValue("direction"),
		Vehicle:        r.FormValue("vehicle"),
		VehicleUnknown: r.FormValue("vehicle_unknown") != "",
		DestinationID:  r.FormValue("destination_id"),
		DeadlineMin:    r.FormValue("deadline_minutes"),
		TimeSource:     r.FormValue(timeSourceField),
		OccurredAtWall: r.FormValue(occurredAtField),
	}
	if f.Direction != gateOutbound {
		f.Direction = gateInbound
	}

	kinds := r.Form["occupant_kind"]
	values := r.Form["occupant_value"]
	typed := r.Form["occupant_typed"]
	companyIDs := r.Form["occupant_company_id"]
	companies := r.Form["occupant_company"]
	tradeIDs := r.Form["occupant_trade_id"]
	trades := r.Form["occupant_trade"]
	// Position / Phone / Last / First ⟦gate-checkin-spec, 2026-09-13⟧: FOUR
	// more parallel arrays, same rule as every one above — every row emits
	// every field, unconditionally (W8-23(c)), so occupantArrayFields' body-
	// derived sweep (checkIndexAlignment) covers them automatically; nothing
	// there needed editing to pick them up.
	positions := r.Form["occupant_position"]
	phones := r.Form["occupant_phone"]
	lastNames := r.Form["occupant_last"]
	firstNames := r.Form["occupant_first"]
	times := r.Form["occupant_time"] // ⟦FIX8⟧ same parallel-array rule
	// Iterate the LONGEST of the parallel arrays. Iterating one alone would
	// drop any row whose other fields never arrived — which is exactly the
	// shape that used to lose server-rendered occupants (V-05).
	rows := len(kinds)
	for _, n := range []int{len(values), len(typed), len(companyIDs), len(companies), len(tradeIDs), len(trades), len(positions), len(phones), len(lastNames), len(firstNames), len(times)} {
		if n > rows {
			rows = n
		}
	}
	// driver_index names a ROW POSITION in the body as submitted. It is
	// converted to a flag on the row object immediately, and from here on
	// the flag travels with the PERSON: deleting an earlier row renumbers
	// everything, and re-emitting a stale number would silently re-point
	// the driver at whoever moved into that slot.
	driverIdx := -1
	if v := strings.TrimSpace(r.FormValue("driver_index")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			driverIdx = n
		}
	}
	// amend_confirm ⟦[[D-84]]⟧ travels the same way as driver_index: a ROW
	// POSITION per ticked box, converted to a flag on the row at once.
	confirmed := map[int]bool{}
	for _, v := range r.Form["amend_confirm"] {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			confirmed[n] = true
		}
	}
	// add_orientation ⟦D-88 cross-site warning, 2026-09-10⟧ travels the same
	// way: a ROW POSITION per ticked box.
	addOrientation := map[int]bool{}
	for _, v := range r.Form["add_orientation"] {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			addOrientation[n] = true
		}
	}
	// orient_target ⟦orientation-doors, gap 2, 2026-09-11⟧ is a SEPARATE,
	// self-identifying field from add_orientation on purpose — never folded
	// into that field's own value, which existing witnessed tests
	// (gate_crosssite_warning_test.go) already assert as the bare row index
	// (`value="1"`). Each entry is "<row index>:<person id>", posted once
	// per row currently showing the warning (gate.html), ticked or not — see
	// passengerRow.AddOrientationFor.
	orientTarget := map[int]string{}
	for _, v := range r.Form["orient_target"] {
		idx, id, ok := strings.Cut(strings.TrimSpace(v), ":")
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(idx); err == nil {
			orientTarget[n] = id
		}
	}
	for k, v := range r.Form {
		if strings.HasPrefix(k, groupDriverPrefix) && len(v) > 0 {
			if f.GroupDriverPick == nil {
				f.GroupDriverPick = map[string]string{}
			}
			f.GroupDriverPick[strings.TrimPrefix(k, groupDriverPrefix)] = v[0]
		}
	}
	for i := 0; i < rows; i++ {
		f.Rows = append(f.Rows, passengerRow{
			Index:             i,
			Kind:              at(kinds, i),
			Value:             at(values, i),
			Typed:             at(typed, i),
			CompanyID:         at(companyIDs, i),
			Company:           at(companies, i),
			TradeID:           at(tradeIDs, i),
			Trade:             at(trades, i),
			Position:          at(positions, i),
			Phone:             at(phones, i),
			NewLast:           at(lastNames, i),
			NewFirst:          at(firstNames, i),
			Time:              strings.TrimSpace(at(times, i)),
			IsDriver:          i == driverIdx,
			AmendConfirmed:    confirmed[i],
			AddOrientation:    addOrientation[i],
			AddOrientationFor: orientTarget[i],
		})
	}
	return f
}

// renumber re-labels the rows 0..N-1 after an insert or a delete. The
// driver flag is untouched: it lives on the row, not on the number.
func (f *vehicleForm) renumber() {
	for i := range f.Rows {
		f.Rows[i].Index = i
	}
}

// checkIndexAlignment is §3's structural guard on the parallel arrays.
//
// occupant_typed / occupant_kind / occupant_value / occupant_company /
// occupant_trade arrive as PARALLEL ARRAYS. A row that emitted a
// different set of them would shift every LATER row's fields by one —
// silently attaching one person's name to another person's company, and at
// 26 rows that is both likelier and far harder to spot than at four. So
// every row also emits occupant_index, and a submit whose indices are not
// exactly 0..N-1 in order is REFUSED WHOLE rather than half-attributed.
//
// This is deliberately a check on the received body, not on the parsed
// rows: parseVehicleForm has already normalised the lengths, which is
// precisely the normalisation that would hide the shift.
func checkIndexAlignment(r *http.Request, rows int) error {
	got := r.Form["occupant_index"]
	if len(got) != rows {
		return fmt.Errorf("This vehicle was not submitted intact: %d passenger rows arrived but %d row numbers did. Nothing has been recorded. Reload the gate page and enter the vehicle again — a partly-arrived list can attach one person's name to another person's company, and no page or sheet afterwards would show it", rows, len(got))
	}
	// ⚠⚠ THE PARITY ARM, AND THE GUARD ABOVE WAS BLIND IN EXACTLY ONE
	// DIRECTION WITHOUT IT — MEASURED, NOT PREDICTED. `rows` is the MAXIMUM
	// of the parallel arrays (parseVehicleForm), and `occupant_index` has
	// length `rows`. So the length check above fires when an array is LONGER
	// than the index list and is BLIND WHEN ONE IS SHORTER — and a field
	// emitted only on the rows that need it is EXACTLY THE SHORT CASE. Three
	// rows, three indices, two companies: the check above passed, the middle
	// row took the THIRD person's company, and nothing said so.
	//
	// The rule this arm states is the one the paragraph above already
	// describes and did not enforce: EVERY occupant array present in the body
	// has the SAME LENGTH as occupant_index.
	//
	// ⚠ IT IS ONE GUARD OVER ALL THE ARRAYS, DERIVED FROM THE BODY rather
	// than from a list of field names. A guard written for `occupant_trade`
	// alone would not be a fix — the next parallel field added would arrive
	// unguarded, which is how this hole opened in the first place.
	//
	// ⚠ AND IT IS ON THE RECEIVED BODY, never on the parsed rows:
	// parseVehicleForm has already normalised the lengths, AND THAT
	// NORMALISATION IS PRECISELY WHAT WOULD HIDE THE SHIFT.
	for _, field := range occupantArrayFields(r) {
		if n := len(r.Form[field]); n != rows {
			return fmt.Errorf("This vehicle was not submitted intact: %d passenger rows arrived but only %d of them carried every field. Nothing has been recorded. Reload the gate page and enter the vehicle again — a partly-arrived list can attach one person's details to another person's row, and no page or sheet afterwards would show it", rows, n)
		}
	}
	for i, v := range got {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n != i {
			return fmt.Errorf("This vehicle was not submitted intact: passenger row %d arrived numbered %q. Nothing has been recorded. Reload the gate page and enter the vehicle again", i+1, v)
		}
	}
	return nil
}

// occupantArrayFields returns every `occupant_*` array present in the
// submitted body EXCEPT `occupant_index` itself, which is the yardstick the
// others are measured against.
//
// ⚠ DERIVED FROM THE BODY, NOT LISTED. A hand-kept list of the parallel
// fields is the enumeration this project has measured short nine times,
// always short — and here "short" means one field silently unguarded and one
// person's details on another person's row.
func occupantArrayFields(r *http.Request) []string {
	var out []string
	for k := range r.Form {
		if k == "occupant_index" || !strings.HasPrefix(k, "occupant_") {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out) // a stable refusal: the same body always names the same field first
	return out
}

// handlePassengerAdd appends the open box's person to the list and renders
// the page back with the box empty and the list one longer.
//
// SAFE DEFAULT — "Add person", derived for each direction:
//
//	INBOUND (this commit will ADD people to the muster). A spurious add
//	records somebody who is not here: the muster OVER-reports and search
//	time is wasted. Safe direction. So this button may be fast, may be the
//	form's default submit (Enter adds), and needs no confirmation.
//
//	OUTBOUND (this commit will REMOVE people from the muster). A spurious
//	add here would record somebody as having left while they are still on
//	site — the dangerous direction. But it cannot happen by pressing this
//	button: the button adds whatever is in the box, and an empty box adds
//	nobody. Reaching the dangerous outcome requires the operator to type a
//	specific person's name into a departing vehicle, which is a different
//	and much rarer error than forgetting one. What DOES change outbound is
//	the resolution rule at commit: a typed name that is on nobody's roster
//	is REFUSED there rather than creating a person (see rowPersonSlot).
//	So this button stays fast in both directions, for reasons derived
//	separately in each.
//
// It is deliberately NOT post-redirect-GET. A redirect cannot carry the
// accumulated list forward, and the usual objection to a bare POST — F5
// re-submits — is harmless here: the body is "accumulated rows + the one
// name in the open box" and the response is "accumulated rows + 1, open box
// empty", so re-posting the identical body yields the identical list. The
// accumulate step is idempotent in the browser's own terms. The COMMIT
// stays post-redirect-GET, because that one is a write and must not be
// replayed by a refresh.
func (s *Server) handlePassengerAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	f := parseVehicleForm(r)
	// A candidate PICK is an accumulate step of exactly this route's class —
	// it rewrites one row in the officer's list and authors nothing — so it
	// rides this registration rather than a fourth one (the W2 route table
	// is criteria-lock material, w2_route_walk_test.go). `pick` is a submit
	// button's own value and travels only when that button was pressed.
	if pick := strings.TrimSpace(r.FormValue("pick")); pick != "" {
		s.applyRowPick(&f, pick)
		s.renderGate(w, r, http.StatusOK, f)
		return
	}
	row, ok := s.openBoxRow(r)
	if !ok {
		// Not an error, and deliberately not styled as one: pressing Add
		// with an empty box (or Enter in the vehicle field) is a fumble, not
		// a mistake, and the list is unharmed.
		f.Error = ""
		s.renderGate(w, r, http.StatusOK, f)
		return
	}
	f.Rows = append(f.Rows, row)
	f.renumber()
	s.renderGate(w, r, http.StatusOK, f)
}

// openBoxRow reads the open box's CURRENT content and builds the row it
// describes — the exact construction handlePassengerAdd's own accumulate
// step uses, factored out so handleGateCheckInSingle (R-25) can build the
// SAME row without requiring the officer to have pressed "Add person"
// first. ok is false when nothing was typed — the "empty box" fumble,
// never an error; see both callers' own handling of it.
//
// ⟦gate-checkin-spec, James, 2026-09-13: "last name, first name"⟧ REPLACES
// the single "type a name" box (new_typed) with two: new_last stays the
// autocomplete-attached field (gate.html/gate_script — Access's own
// type-ahead is by last name too, design/ACCESS-APP-LAYOUT.md §3.1), and
// new_first is a plain companion box. Two cases:
//
//	EXISTING (new_kind=="existing", a roster id in new_value — set by
//	the script's mousedown pick, exactly as before): Typed becomes the
//	record's CANONICAL Name, fetched here, never a re-composition of the
//	two boxes. composeName("Smith","John") cannot be trusted to
//	reproduce a pre-convention record's stored "John Smith" (composeName
//	itself: "does NOT apply to rows created before intake existed"), and
//	decorateRow's own stale-pick check would silently demote a real pick
//	to a NEW (duplicate) person the moment the composed text disagreed
//	with the record by so much as a comma. ⟦C-2, 2026-09-29⟧ ONLY while the
//	boxes still read as that Name (the script's pick puts the whole Name
//	in the Last box): once they name anybody else, the id is dropped and
//	the boxes are resolved like any typed name — see the body.
//
//	OTHERWISE (no id was posted — the ordinary scripting-off case, and
//	also a scripted pick that simply never happened): Typed is the two
//	boxes composed via gateComposeName — NOT web/intake.go's composeName;
//	see that function's own comment for why the gate needs its own.
//	`kind`/`value` are left EXACTLY as posted (the served default is
//	kind="existing", value="" — unchanged from before this door had two
//	boxes instead of one), so decorateRow's later exact-match resolution
//	still runs and can still adopt a roster record it recognises;
//	forcing kind="new" here regardless, as an earlier version of this
//	handler did, broke that resolution for every ordinary no-JS add of
//	an already-enrolled person: decorateRow's "stable, pickers are the
//	officer's" branch requires kind != "new", and skipping it made EVERY
//	later accumulate step re-adopt the row from the record — silently
//	discarding a company/trade change the officer had just posted but
//	not yet confirmed (caught by
//	TestD84AChangeWithoutTheConfirmationIsRefusedAndAmendsNothing and
//	TestD84UndoRestoresAChangeInProgress going RED). Composition is
//	one-way, boxes to Name, never Name back to parts (D-48/W5a-8), so a
//	NEW row also keeps the raw boxes on NewLast/NewFirst for
//	PersonDetails at creation (newPersonSlot).
//
//	A lookup MISS (an id was posted but names nobody — stale or
//	tampered) is the one case that DOES force kind/value to "new","": a
//	dangling id must never ride forward as if it still named somebody.
func (s *Server) openBoxRow(r *http.Request) (row passengerRow, ok bool) {
	kind := r.FormValue("new_kind")
	value := strings.TrimSpace(r.FormValue("new_value"))
	lastName := r.FormValue("new_last")
	firstName := r.FormValue("new_first")
	typed := ""
	composed := gateComposeName(lastName, firstName)
	if kind == "existing" && value != "" {
		if p, err := domain.PersonByID(s.Store, value); err == nil && p != nil {
			if strings.EqualFold(strings.TrimSpace(p.Name), composed) {
				typed = p.Name
			} else {
				// ⟦C-2 / F-L02-3, 2026-09-29⟧ THE VISIBLE BOXES NO LONGER
				// NAME THE PERSON THIS ID DOES. With scripting off, a refused
				// commit re-renders the resolved id into the hidden field and
				// nothing clears it when the officer then types somebody else
				// — honouring it here re-checked-in the PREVIOUS person under
				// the new person's typed name, silently, and the person at the
				// window never reached the muster. The id is dropped back to
				// the served default ("existing", no value), so decorateRow
				// resolves the typed text afresh — exactly one exact match is
				// that person, none is a new person, several are refused by
				// name. A scripted pick is unaffected: the script's adopt()
				// writes the record's own Name into the Last box and disables
				// First, so the composed text IS the canonical Name. Blank
				// boxes with an id posted are no exception: nothing typed is
				// the empty-box fumble below, never "whoever the id was".
				value = ""
			}
		} else {
			kind, value = "new", "" // a stale/tampered id: never carry it forward
		}
	}
	if typed == "" {
		typed = composed
	}
	if typed == "" {
		return passengerRow{}, false
	}
	return passengerRow{
		Kind:  kind,
		Value: value,
		Typed: typed,
		// ⟦[[D-84]]⟧ the open box's pickers and escape boxes ride onto the
		// row. For a person who resolves to the roster, decorateRows then
		// overwrites them FROM THE RECORD (the box's values are the ask for
		// a NEW person, and this person is not new).
		CompanyID: r.FormValue("new_company_id"),
		Company:   r.FormValue("new_company"),
		TradeID:   r.FormValue("new_trade_id"),
		Trade:     r.FormValue("new_trade"),
		Position:  strings.TrimSpace(r.FormValue("new_position")),
		Phone:     strings.TrimSpace(r.FormValue("new_phone")),
		NewLast:   strings.TrimSpace(lastName),
		NewFirst:  strings.TrimSpace(firstName),
		// AddOrientation/AddOrientationFor ⟦R-27, James, 2026-09-16⟧: the
		// individual page's single-person form (gate.html) is the only
		// served page that puts these two field names on the open box
		// itself — gate_vehicle.html's own open box carries neither, so
		// this is additive and a no-op there (empty FormValue, same as
		// today). Read the SAME way resolveTickedOrientations already
		// expects a row to carry them (this file, below): a checkbox that
		// submits nothing when unticked, and a target id that is honoured
		// only while it still names row 0's resolved person — see that
		// function's own staleness comment.
		AddOrientation:    r.FormValue("add_orientation") != "",
		AddOrientationFor: strings.TrimSpace(r.FormValue("orient_target")),
	}, true
}

// applyRowPick switches accumulated row `idx` to the roster person the
// officer chose from its duplicate-candidate offer (passengerRow.Candidates):
// `pick` is "<row index>:<person id>", the value of the "Use <name>" button.
//
// THE OFFICER CHOSE; THIS ONLY CARRIES THE CHOICE. The row's visible text
// becomes the record's roster name — never left as typed with an id pinned
// under it, because "the visible text is the authority on identity"
// (rowPersonSlot) would otherwise re-resolve the typed spelling at commit
// and quietly undo the pick. The pickers are set FROM THE RECORD, as
// decorateRow does for an adopted exact match, so the row commits as an
// untouched known person and no [[D-84]] change is implied.
//
// Nothing is written. A pick that no longer lines up with the page — a
// row index the list has moved past, an id the roster no longer holds — is
// refused in words with the list intact, exactly as a stale delete is.
func (s *Server) applyRowPick(f *vehicleForm, pick string) {
	idxText, id, ok := strings.Cut(pick, ":")
	idx, err := strconv.Atoi(strings.TrimSpace(idxText))
	if !ok || err != nil || idx < 0 || idx >= len(f.Rows) {
		f.Error = "That row could not be switched to the roster record — the page had moved on. Nothing was changed; check the list and try again."
		return
	}
	p, err := domain.PersonByID(s.Store, strings.TrimSpace(id))
	if err != nil || p == nil {
		f.Error = "That roster record could not be found, so the row was left as typed. Nothing was changed — pick them from the roster list, or leave the row and they will be added."
		return
	}
	row := &f.Rows[idx]
	row.Kind, row.Value, row.Typed = "existing", p.PersonID, p.Name
	row.AmendConfirmed = false
	row.CompanyID, row.Company, row.TradeID, row.Trade = "", "", "", ""
	if p.CompanyID != nil {
		row.CompanyID = *p.CompanyID
	}
	if p.TradeID != nil {
		row.TradeID = *p.TradeID
	}
	// Position / Phone ⟦gate-checkin-spec, 2026-09-13⟧: same "start AT the
	// record" rule the company/trade pickers just got above. The open box's
	// NewLast/NewFirst are left as they were — they were never this
	// person's to begin with, and decorateRow never reads them for a known row.
	row.Position, row.Phone = "", ""
	if p.Position != nil {
		row.Position = *p.Position
	}
	if p.Phone != nil {
		row.Phone = *p.Phone
	}
}

// handleGateCheckOutSelectForDeparture is the roster's "Depart together"
// button (James, 2026-09-11): it turns a set of ticked on-site people into
// accumulated rows on the OUTBOUND vehicle form — the exact shape
// applyRowPick already produces for a single resolved roster pick — so the
// officer lands on the existing departing-vehicle form with those people
// already listed, picks the driver among them (the form's own existing
// per-row radio), names the vehicle (or marks it unknown), and the EXISTING
// /gate/checkout/vehicle commit is what authors the one Dispatch trip. This
// function authors NOTHING: same class as handlePassengerAdd, and
// registered ungated for the same reason (see registerGateRoutes).
//
// SCOPING (team-lead brief, 2026-09-11: "a posted id must never be trusted
// beyond that"): every ticked visit id is checked against a FRESH read of
// THIS SITE's own open visits, taken here rather than trusted from the
// page. A ticked id that is not in that fresh set — the person already
// left in the seconds since the page was rendered, or the id never named a
// real open visit here at all — is not added, and is named to the officer
// rather than silently dropped (staletick.go's precedent: "the officer must
// never be left believing" something happened that did not). Everyone else
// still ticked is added; a partial failure costs one name off the intended
// list, never the whole action.
func (s *Server) handleGateCheckOutSelectForDeparture(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	f := vehicleForm{Direction: gateOutbound}
	ticked := r.Form["visit_ids"]
	if len(ticked) == 0 {
		f.Error = "Nothing was ticked on the roster, so nobody was added to the departing vehicle. Tick who is leaving, then use this button again."
		s.renderGate(w, r, http.StatusOK, f)
		return
	}
	visits, err := domain.OpenVisitsAt(s.Store, s.Cfg.SiteID)
	if err != nil {
		s.gateFail(w, "open visits", err)
		return
	}
	byVisit := make(map[string]domain.Visit, len(visits))
	for _, v := range visits {
		byVisit[v.VisitID] = v
	}
	seen := map[string]bool{}
	var missing []string
	for _, raw := range ticked {
		vid := strings.TrimSpace(raw)
		if vid == "" || seen[vid] {
			continue
		}
		seen[vid] = true
		v, ok := byVisit[vid]
		if !ok {
			// Not open here right now — but it may still name a real
			// person this post can identify (the common case: somebody
			// else's correction, or a race, closed the visit in the
			// seconds between the tick and this submit). VisitByID reads
			// by primary key regardless of open/closed state, so the
			// officer is told WHO rather than a bare id — staletick.go's
			// own lesson, applied here. Still scoped to THIS post: a visit
			// id belonging to another site names nobody.
			if closed, verr := domain.VisitByID(s.Store, vid); verr == nil && closed != nil && closed.SiteID == s.Cfg.SiteID {
				if closed.PersonSnapshotName != nil && *closed.PersonSnapshotName != "" {
					missing = append(missing, *closed.PersonSnapshotName)
				} else {
					missing = append(missing, vid)
				}
			} else {
				missing = append(missing, vid)
			}
			continue
		}
		p, perr := domain.PersonByID(s.Store, v.PersonID)
		if perr != nil || p == nil {
			missing = append(missing, v.PersonID)
			continue
		}
		row := passengerRow{Kind: "existing", Value: p.PersonID, Typed: p.Name,
			Time: strings.TrimSpace(r.FormValue(rowTimeOutPrefix + vid))}
		// Match decorateRow's own fromRecord shape (D-84): the row's
		// pickers start AT the record, so a freshly-added row shows as
		// untouched rather than opening the "change" block over a blank
		// company/trade the officer never asked to clear.
		if p.CompanyID != nil {
			row.CompanyID = *p.CompanyID
		}
		if p.TradeID != nil {
			row.TradeID = *p.TradeID
		}
		f.Rows = append(f.Rows, row)
	}
	f.renumber()
	// ⟦FIX8 option C, 2026-10-05⟧ each ticked row's own Time out rides onto
	// that person's staged row, and a changed roster form-level time onto
	// the vehicle form's own. Nothing is judged here: the commit groups the
	// rows by time (one departure per time) and applies every bound.
	if r.FormValue(timeSourceField) == "entered" {
		f.TimeSource, f.OccurredAtWall = "entered", strings.TrimSpace(r.FormValue(occurredAtField))
	}
	// ⟦R-27, James, 2026-09-16⟧ exactly one person ticked has exactly one
	// possible driver — the same "one name, one action, no other
	// candidate" reasoning handleGateCheckInSingle already applies inbound
	// (this file, above). NOT pre-selection in the sense
	// TestNoDriverIsPreSelectedOnTheGateForm guards against: that guard is
	// about a server DEFAULT choosing among several rows nobody affirmed.
	// Here there is exactly one row because the officer ticked exactly one
	// name; two or more rows still leave the radio group untouched,
	// `required`, with no default.
	if len(f.Rows) == 1 {
		f.Rows[0].IsDriver = true
	}
	if len(missing) > 0 {
		f.Error = fmt.Sprintf(
			"%s not added — no longer found on this post's on-site roster right now (they may just have been checked out, or the tick no longer names a real visit here). %s added below; nothing has been recorded yet.",
			namesArePlural(missing),
			func() string {
				if len(f.Rows) == 0 {
					return "Nobody was"
				}
				return "Everybody else ticked was"
			}(),
		)
	}
	s.renderGate(w, r, http.StatusOK, f)
}

// handlePassengerDelete removes one row.
//
// SAFE DEFAULT — the per-row delete, derived for each direction:
//
//	INBOUND. Nothing is on the muster yet, so deleting a row costs
//	RE-TYPING, not a person's absence. It is not the same act as an errant
//	tick in the check-out picker, which removes a live person from the
//	sheet — same word, different consequence class.
//
//	OUTBOUND. Identical: this vehicle has not been written either, so the
//	deleted person simply stays on the muster exactly as they were. A
//	deletion outbound fails in the OVER-reporting direction (somebody is
//	still shown on site who has left), which is the safe one.
//
// THEREFORE NO CONFIRMATION DIALOG, in either direction, and that is a
// safety decision rather than an omission: confirming a low-consequence
// control that is pressed repeatedly during a long entry trains
// click-through, which degrades the confirm on the commit that matters.
// Guarding everything equally is the same as guarding nothing. What is
// offered instead is an inline "Removed X — undo", which costs one click
// and cannot train anything.
func (s *Server) handlePassengerDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	f := parseVehicleForm(r)
	idx, err := strconv.Atoi(strings.TrimSpace(r.FormValue("delete_index")))
	if err != nil || idx < 0 || idx >= len(f.Rows) {
		f.Error = "That row could not be deleted — the page had moved on. Nothing was changed; check the list and try again."
		s.renderGate(w, r, http.StatusOK, f)
		return
	}
	removed := f.Rows[idx]
	f.Rows = append(f.Rows[:idx], f.Rows[idx+1:]...)
	f.renumber()
	f.Removed = &removed
	f.RemovedName = strings.TrimSpace(removed.Typed)
	if removed.IsDriver {
		// NEVER AUTO-PROMOTE. Promoting whoever now sits in the deleted
		// row's place would put a name in an incident report that nobody
		// chose — the pre-selected-driver defect in a different costume. The
		// flag is cleared, the clearing is said out loud, and the commit
		// goes back to refusing until somebody answers the question again.
		f.DriverCleared = removed.Typed
	}
	s.renderGate(w, r, http.StatusOK, f)
}

// handlePassengerUndo puts a just-deleted row back where it was. It must
// NOT repopulate the open box, which would clobber whatever the operator
// has half-typed in it.
func (s *Server) handlePassengerUndo(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	f := parseVehicleForm(r)
	row := passengerRow{
		Kind:           r.FormValue("removed_kind"),
		Value:          r.FormValue("removed_value"),
		Typed:          strings.TrimSpace(r.FormValue("removed_typed")),
		CompanyID:      r.FormValue("removed_company_id"),
		Company:        r.FormValue("removed_company"),
		TradeID:        r.FormValue("removed_trade_id"),
		Trade:          r.FormValue("removed_trade"),
		Time:           strings.TrimSpace(r.FormValue("removed_time")),
		IsDriver:       r.FormValue("removed_driver") == "1",
		AmendConfirmed: r.FormValue("removed_amend") == "1",
		AddOrientation: r.FormValue("removed_add_orientation") == "1",
		// ⟦orientation-doors, gap 2, 2026-09-11⟧ restores the identity the
		// tick was for, exactly like every other picker above — an undo
		// must not silently drop the gap-2 protection any more than it may
		// drop a change in progress.
		AddOrientationFor: r.FormValue("removed_orient_target"),
	}
	if row.Typed == "" {
		s.renderGate(w, r, http.StatusOK, f)
		return
	}
	at := len(f.Rows)
	if n, err := strconv.Atoi(strings.TrimSpace(r.FormValue("removed_index"))); err == nil && n >= 0 && n <= len(f.Rows) {
		at = n
	}
	if row.IsDriver {
		// The row was the driver when it was deleted, and deleting it
		// cleared the flag. Restoring the row restores the flag to the SAME
		// PERSON — never to a position — but only if nobody has been marked
		// in the meantime, because the later answer is the operator's more
		// recent one.
		for _, existing := range f.Rows {
			if existing.IsDriver {
				row.IsDriver = false
				break
			}
		}
	}
	f.Rows = append(f.Rows[:at], append([]passengerRow{row}, f.Rows[at:]...)...)
	f.renumber()
	s.renderGate(w, r, http.StatusOK, f)
}

// --- resolving a row to a person ------------------------------------

// parsePersonSlot decodes one driver/occupant form entry. kind == "new" is
// what the client-side confirm() gate authorizes (gate.html); the server
// does not re-derive it, it trusts the submitted kind/value the same way
// every other form in this codebase trusts its POST body (refusals happen
// downstream in domain, e.g. an unknown "existing" id is refused there).
func parsePersonSlot(kind, value, company, trade string) domain.PersonSlot {
	if kind == "new" {
		return domain.PersonSlot{
			NewName: strings.TrimSpace(value), NewCompany: strings.TrimSpace(company),
			NewTrade: strings.TrimSpace(trade),
		}
	}
	return domain.PersonSlot{ExistingID: value}
}

// resolveTypedPerson resolves a display name TYPED INTO THE FORM against
// the roster. It exists for the scripting-off path, and for the JS-on path
// whenever the visible text no longer agrees with the hidden id.
//
// Three outcomes, and the middle one is the whole design:
//
//	exactly one match  -> that person's id. Safe: the name is unambiguous.
//	no match           -> a NEW person, which is what the INBOUND form is
//	                      for ("the gate page adds people who are not on the
//	                      roster too"). Re-derived rather than assumed
//	                      safe: creating a person ADDS somebody to the
//	                      muster, so the failure direction is
//	                      over-reporting — wasted search time, never a
//	                      person nobody looks for. The OUTBOUND form
//	                      refuses this outcome instead; see rowPersonSlot.
//	more than one      -> REFUSED, loudly, naming the collision. Two
//	                      roster people sharing a display name is not
//	                      exotic on a work crew, and picking either one is
//	                      a guess about WHO is on site. The system never
//	                      guesses about identity; the operator resolves it
//	                      with the roster browser, or the names get
//	                      disambiguated on the roster.
//
// Matching is exact after trimming, and case-insensitive so that a name
// typed in a hurry still resolves to the person rather than creating a
// near-duplicate.
func resolveTypedPerson(typed string, persons []domain.Person) (domain.PersonSlot, error) {
	return resolveTypedPersonDir(typed, persons, false)
}

// setVehicleFormError puts a refused commit's sentence on the form, and the
// amend link's record id when the refusal is the retired-name one.
func (s *Server) setVehicleFormError(f *vehicleForm, err error) {
	f.Error = s.typedNameRefusalText(err)
	f.ErrorAmendPersonID = retiredAmendPersonID(err)
}

// typedNameRefusalText is err's sentence, with an ambiguous typed name's
// records named by the post that added each (ambiguousNameRefusal), in this
// post's display names.
func (s *Server) typedNameRefusalText(err error) string {
	var amb *ambiguousNameRefusal
	if errors.As(err, &amb) {
		return amb.sentence(s.peerDisplayName)
	}
	return err.Error()
}

// ambiguousNameRefusal is the typed-name refusal for a name two or more
// LIVE records carry exactly.
//
// James, 2026-10-04 (Q-R5-3): JS-off same-name lists show the origin post.
// ⟦was: the sentence listed the records' raw person ids
// ("ice-pad-person-b8d4…, ice-road-person-4bb6…", L11-2d) — the only thing
// that told the pair apart, and readable only by someone who reads ids⟧.
// It now says which post added each, by display name.
type ambiguousNameRefusal struct {
	Typed   string
	Persons []domain.Person
}

func (e *ambiguousNameRefusal) Error() string { return e.sentence(domain.SiteLabel) }

func (e *ambiguousNameRefusal) sentence(siteName func(string) string) string {
	origins := make([]string, 0, len(e.Persons))
	for _, p := range e.Persons {
		site := p.SiteID
		if site == "" {
			site = personOriginSite(p.PersonID)
		}
		if site == "" {
			origins = append(origins, "origin not recorded")
			continue
		}
		origins = append(origins, "added at "+siteName(site))
	}
	return fmt.Sprintf(
		"%d people on the roster are called %q (%s) — this entry cannot say which of them is here, and guessing would put the wrong person on the muster. Pick the right one from the roster list, or give them distinguishable names on the roster first",
		len(e.Persons), e.Typed, strings.Join(origins, "; "))
}

// retiredAmendPersonID is the amend link's record id when err is the
// retired-name refusal (domain.RetiredNameRefusal), else "". Every gate
// surface that renders a refused commit sets ErrorAmendPersonID from it —
// F12, James, 2026-10-04: the L02 amend link must appear on EVERY
// retired-name refusal surface.
func retiredAmendPersonID(err error) string {
	var rr *domain.RetiredNameRefusal
	if errors.As(err, &rr) {
		return rr.PersonID
	}
	return ""
}

// resolveTypedPersonDir is resolveTypedPerson with the direction made
// explicit. departing is true for the OUTBOUND (vehicle check-out) form.
//
// ⟦D8, James, 2026-10-04: check-out must never consult retired⟧ Retirement
// means "no longer selectable for a new check-in" (D-21.5); it never meant
// "cannot leave". A partition can put somebody both retired AND on site (one
// post retires them while another checks them in), and refusing them on the
// way out refused the whole truck's departure with words that sent the
// officer to the amend page. Leaving is a presence path, exactly like the
// arrival and resume doors domain/checkin.go already exempts, so a departing
// row adopts a retired record when it is the only record with that name. The
// outbound commit's own presence check still refuses anyone not on site, so
// this cannot check out a retired person who is not here.
func resolveTypedPersonDir(typed string, persons []domain.Person, departing bool) (domain.PersonSlot, error) {
	typed = strings.TrimSpace(typed)
	if typed == "" {
		return domain.PersonSlot{}, nil
	}
	var hits, retired []domain.Person
	for _, p := range persons {
		if strings.EqualFold(strings.TrimSpace(p.Name), typed) {
			// ⟦R-118 / [[D-21.5]], 2026-10-03⟧ a RETIRED record is not
			// adopted — it is no longer selectable. Kept aside so the
			// zero-hit case below can say WHY the name does not resolve
			// instead of minting a second record for the same name, which
			// is the duplicate D-21.5 exists to clear up.
			if p.IsRetired() {
				retired = append(retired, p)
				continue
			}
			hits = append(hits, p)
		}
	}
	switch len(hits) {
	case 0:
		if departing && len(retired) == 1 {
			return domain.PersonSlot{ExistingID: retired[0].PersonID}, nil
		}
		if !departing {
			// Plain words, no record id (L02, 2026-10-04: the id meant
			// nothing to the officer reading it). The id rides the error
			// for the amend LINK only (domain.RetiredNameRefusal).
			// James, 2026-10-04 (Q-FG1): matched under the ONE normaliser
			// the domain's commit-time check uses (domain.RetiredNameKey),
			// so "Ashton,  Joss" / "Ashton,Joss" / "Ashton Joss" are the
			// retired "Ashton, Joss" here too. ⟦was: len(retired) > 0 — the
			// exact-after-trim/case list only⟧
			if p := domain.RetiredNamesake(persons, typed); p != nil {
				return domain.PersonSlot{}, &domain.RetiredNameRefusal{Typed: typed, PersonID: p.PersonID}
			}
		}
		if len(retired) > 1 {
			return domain.PersonSlot{}, fmt.Errorf(
				"%d retired people on the roster are called %q — this entry cannot say which of them is leaving. Tick them from the On site now list instead, which knows exactly who is here",
				len(retired), typed)
		}
		return domain.PersonSlot{NewName: typed}, nil
	case 1:
		return domain.PersonSlot{ExistingID: hits[0].PersonID}, nil
	default:
		return domain.PersonSlot{}, &ambiguousNameRefusal{Typed: typed, Persons: hits}
	}
}

// personSlotFrom builds one driver/occupant slot for the PRE-EXISTING body
// shape (driver_kind/driver_value/driver_typed and the bare occupant
// arrays), preferring the RESOLVED id the roster browser wrote and falling
// back to the text the operator typed.
//
// The order matters. `value` is id-resolved and duplicate-name safe, so it
// wins whenever it is present. `typed` is the scripting-off path — and it
// is also the guarantee that a script failure cannot silently shorten a
// check-in: the typed text arrives whatever the script did or did not do,
// so a row the operator filled in is never dropped for want of a hidden
// field. That is the server-side half of V-05's loud-failure requirement;
// gate.html carries the client-side half.
func personSlotFrom(kind, value, typed, company, trade string, persons []domain.Person) (domain.PersonSlot, error) {
	if strings.TrimSpace(value) != "" {
		return parsePersonSlot(kind, value, company, trade), nil
	}
	slot, err := resolveTypedPerson(typed, persons)
	if err != nil {
		return domain.PersonSlot{}, err
	}
	if slot.NewName != "" {
		slot.NewCompany = strings.TrimSpace(company)
		slot.NewTrade = strings.TrimSpace(trade)
	}
	return slot, nil
}

// rowPersonSlot turns one ACCUMULATED row into a person slot.
//
// THE VISIBLE TEXT IS THE AUTHORITY ON IDENTITY. The hidden id is an
// optimisation, honoured only while it still names a person whose roster
// name agrees with what the operator can read on the row. This is the
// server-side twin of gate.html's resolvePick rule ("if the text changed
// after a click, the stale selection is discarded and the field falls
// through rather than ever re-deriving an id from what's on screen"), and
// it exists because the accumulated rows are EDITABLE: with scripting off,
// correcting a name on a row would otherwise leave the previously-resolved
// id in place and check in a different person under the corrected name,
// with nothing on any screen or sheet to show it.
//
// `lookup` is a point lookup (F30) so the roster is dumped only when a row
// actually needs typed-name resolution.
func rowPersonSlot(row passengerRow, lookup func(string) (*domain.Person, error), persons func() ([]domain.Person, error)) (domain.PersonSlot, error) {
	return rowPersonSlotDir(row, lookup, persons, false)
}

// rowPersonSlotDir is rowPersonSlot with the direction explicit; departing
// (the outbound form) honours a RETIRED id and name — see
// resolveTypedPersonDir for D8.
func rowPersonSlotDir(row passengerRow, lookup func(string) (*domain.Person, error), persons func() ([]domain.Person, error), departing bool) (domain.PersonSlot, error) {
	typed := strings.TrimSpace(row.Typed)
	if typed == "" {
		return domain.PersonSlot{}, fmt.Errorf("Passenger %d has no name. Type the name, or delete the row — an unnamed row cannot go on a muster, and skipping it silently is how a vehicle arrives holding fewer people than it did", row.Index+1)
	}
	// A deliberate off-roster add: the operator was shown the "not on the
	// roster — this will add them" state and left the name as it was.
	if row.Kind == "new" && strings.EqualFold(strings.TrimSpace(row.Value), typed) {
		return newPersonSlot(typed, row), nil
	}
	if row.Kind != "new" && strings.TrimSpace(row.Value) != "" {
		p, err := lookup(strings.TrimSpace(row.Value))
		if err != nil {
			return domain.PersonSlot{}, err
		}
		// Honour the id only while it still agrees with the visible name.
		// This is what keeps a duplicate display name resolvable: the
		// operator picked ONE of two identically-named people, and their
		// id — not the name — is what gets checked in.
		// ⟦R-118⟧ …and only while the record is not retired; a retired id
		// falls through to resolveTypedPerson, which says why. A DEPARTING
		// row honours it (D8: leaving never consults retired).
		if p != nil && (departing || !p.IsRetired()) && strings.EqualFold(strings.TrimSpace(p.Name), typed) {
			return domain.PersonSlot{ExistingID: p.PersonID}, nil
		}
	}
	all, err := persons()
	if err != nil {
		return domain.PersonSlot{}, err
	}
	slot, err := resolveTypedPersonDir(typed, all, departing)
	if err != nil {
		return domain.PersonSlot{}, err
	}
	if slot.NewName != "" {
		return newPersonSlot(typed, row), nil
	}
	return slot, nil
}

// gateComposeName joins the open box's two RAW boxes into one display name —
// DELIBERATELY NOT web/intake.go's composeName, and the difference is
// load-bearing, not stylistic.
//
// composeName additionally collapses INTERNAL whitespace runs (its own
// comment: "so that a name typed in a hurry still resolves to the person
// rather than creating a near-duplicate"), which is the right policy at
// intake's identity fold but the WRONG one here: it would upgrade a sloppily
// typed near-duplicate ("PAT  passenger", a doubled space) into an EXACT,
// case-and-whitespace match against "Pat Passenger" (resolveTypedPerson
// compares via strings.EqualFold(strings.TrimSpace(p.Name), typed), which is
// blind to case but NOT to a collapsed run of internal spaces), silently
// adopting the roster record instead of showing the "possibly already on
// the roster" offer and letting the officer decide ([[D-66]]: the gate never
// refuses OR decides for the operator). See
// TestGateOffersTheRosterRecordForASloppilyTypedNameAndStillAddsThem, which
// depends on exactly this boundary.
//
// So this trims only the LEADING/TRAILING whitespace of each box — the same
// single TrimSpace the one box it replaces (new_typed) always got — and
// leaves whatever is between two words exactly as typed.
func gateComposeName(last, first string) string {
	last, first = strings.TrimSpace(last), strings.TrimSpace(first)
	switch {
	case last == "" && first == "":
		return ""
	case first == "":
		return last
	case last == "":
		return first
	}
	return last + ", " + first
}

// newPersonSlot builds the ADD-ON-DEMAND slot for a row that names nobody on
// the roster, carrying the row's company and trade in intake's shape
// ⟦[[D-84]]⟧: a picked id, or — when the picker says "not listed" — the
// escape box's typed name, or nothing. The escape box is read ONLY when its
// picker says newRef, exactly as intake.go reads new_company/new_trade;
// text left in a box under a picker that names a row is not a second
// answer. ⟦was: NewCompany/NewTrade straight from the two free-text boxes⟧.
func newPersonSlot(typed string, row passengerRow) domain.PersonSlot {
	slot := domain.PersonSlot{NewName: typed}
	switch row.CompanyID {
	case "":
	case newRef:
		slot.NewCompany = collapseSpaces(row.Company)
	default:
		slot.CompanyID = row.CompanyID
	}
	switch row.TradeID {
	case "":
	case newRef:
		slot.NewTrade = collapseSpaces(row.Trade)
	default:
		slot.TradeID = row.TradeID
	}
	// LastName / FirstName ⟦gate-checkin-spec, 2026-09-13⟧: the open box's OWN
	// raw boxes, carried through untouched — never derived by splitting
	// `typed` (D-48). Position / Phone: verbatim, no collapseSpaces (W5b: no
	// space/dash/paren removal on Phone, and Position gets the same courtesy
	// for consistency — neither is matched against a canonical list the way
	// Company/Trade are).
	slot.LastName = strings.TrimSpace(row.NewLast)
	slot.FirstName = strings.TrimSpace(row.NewFirst)
	slot.Position = strings.TrimSpace(row.Position)
	slot.Phone = strings.TrimSpace(row.Phone)
	return slot
}

// recordChanges ⟦[[D-84]]⟧ turns the pickers on every KNOWN person's row into
// the confirmed record changes the commit will apply, and REFUSES a row
// whose pickers differ from the record without the confirmation tick.
//
// perRow is vehicleSlots' per-row resolution, so this consults the SAME
// identity the commit checks in — never a second lookup by name. A row that
// resolved to a NEW person is skipped: a new person is ASKED, not amended
// ([[D-66]]), and their company/trade already ride the slot.
//
// WHY AN UNCONFIRMED DIFFERENCE IS A REFUSAL AND NOT A SILENT NO-OP. The
// alternative — commit with the record as it was — is the exact defect D-84
// closes wearing a new control: something the officer set on the row, and
// the commit throws it away. The tick is what makes a change deliberate at
// −65 °F; a differing picker WITHOUT the tick is either a fat-finger (the
// record must not change) or a change the officer forgot to confirm (the
// record must not silently NOT change). Either way the honest answer is to
// say so and keep the list. This is the same class as "Mark who was
// driving" — a question the officer must answer, never a person refused.
//
// The add-new escape with a BLANK name is treated as untouched, never
// refused ([[D-71]] obligation 2: the escape never refuses).
func (s *Server) recordChanges(f vehicleForm, perRow []domain.PersonSlot) ([]domain.PersonRecordChange, error) {
	var (
		companies []domain.Company
		trades    []domain.Trade
		loaded    bool
	)
	load := func() error {
		if loaded {
			return nil
		}
		var err error
		if companies, err = domain.Companies(s.Store); err != nil {
			return err
		}
		if trades, err = domain.Trades(s.Store); err != nil {
			return err
		}
		loaded = true
		return nil
	}
	cur := func(id *string) string {
		if id == nil {
			return ""
		}
		return *id
	}
	var out []domain.PersonRecordChange
	for i, row := range f.Rows {
		if i >= len(perRow) || perRow[i].ExistingID == "" {
			continue
		}
		p, err := domain.PersonByID(s.Store, perRow[i].ExistingID)
		if err != nil {
			return nil, err
		}
		if p == nil {
			continue // the domain refuses the slot itself
		}
		if !rowDiffersFromRecord(row, p) {
			continue
		}
		if err := load(); err != nil {
			return nil, err
		}
		ch := domain.PersonRecordChange{PersonID: p.PersonID}
		changed := false
		// COMPANY.
		switch row.CompanyID {
		case cur(p.CompanyID):
		case "":
			// ⟦L12 / H-1 on the vehicle door, 2026-10-03⟧ a BLANK picker
			// asserts nothing unless the officer ticked "change their
			// record": only "none on file" + the tick is a confirmed clear
			// (FIX-CONSTRAINTS F-L02-1 — the tick alone with a blank is the
			// erased-employer hazard, so the tick is what makes it a clear).
			// ⟦was: every blank was a ClearCompany, so a crafted or JS-off
			// body that left a picked person's company blank was refused as
			// a record change⟧ — the individual door's hydrateBlankFromRecord
			// reading, brought to every door. Position/Phone already read a
			// blank this way.
			if row.AmendConfirmed {
				ch.ClearCompany, changed = true, true
			}
		case newRef:
			if name := collapseSpaces(row.Company); name != "" {
				// Resolve by fold NOW, read-only, so "already so" is decided
				// here and a genuinely new name is the only mint. The domain
				// re-checks find-first-then-mint, so a race mints nothing
				// twice.
				id := ""
				for _, c := range companies {
					if strings.EqualFold(strings.TrimSpace(c.Name), name) {
						id = c.CompanyID
						break
					}
				}
				switch {
				case id == "":
					ch.NewCompany, changed = name, true
				case id != cur(p.CompanyID):
					ch.CompanyID, changed = id, true
				}
			}
		default:
			known, picked, onRecord := false, "", ""
			for _, c := range companies {
				if c.CompanyID == row.CompanyID {
					known, picked = true, c.Name
				}
				if c.CompanyID == cur(p.CompanyID) {
					onRecord = c.Name
				}
			}
			if !known {
				return nil, fmt.Errorf("Passenger %d (%s): the company picked for them is no longer on the list — it may have changed while this page was open. Nothing has been recorded and the list below is intact; reload the gate page and pick again", row.Index+1, strings.TrimSpace(row.Typed))
			}
			// ⟦F-R2-L02-1, 2026-10-03⟧ the list can carry several rows with
			// one name (three "Tundra Services" — one per post that minted
			// it, F-SETUP-1). Picking the record's company BY NAME is the
			// record's company, not a change: refusing it made "put their
			// details back" a guess among identical options.
			// A TICKED pick of the other same-name row is still a deliberate change.
			if row.AmendConfirmed || onRecord == "" || !strings.EqualFold(strings.TrimSpace(picked), strings.TrimSpace(onRecord)) {
				ch.CompanyID, changed = row.CompanyID, true
			}
		}
		// TRADE.
		switch row.TradeID {
		case cur(p.TradeID):
		case "":
			if row.AmendConfirmed { // a blank asserts nothing without the tick — see COMPANY
				ch.ClearTrade, changed = true, true
			}
		case newRef:
			if name := collapseSpaces(row.Trade); name != "" {
				id := ""
				for _, t := range trades {
					if strings.EqualFold(strings.TrimSpace(t.Name), name) {
						id = t.TradeID
						break
					}
				}
				switch {
				case id == "":
					ch.NewTrade, changed = name, true
				case id != cur(p.TradeID):
					ch.TradeID, changed = id, true
				}
			}
		default:
			known, picked, onRecord := false, "", ""
			for _, t := range trades {
				if t.TradeID == row.TradeID {
					known, picked = true, t.Name
				}
				if t.TradeID == cur(p.TradeID) {
					onRecord = t.Name
				}
			}
			if !known {
				return nil, fmt.Errorf("Passenger %d (%s): the trade picked for them is no longer on the list — it may have changed while this page was open. Nothing has been recorded and the list below is intact; reload the gate page and pick again", row.Index+1, strings.TrimSpace(row.Typed))
			}
			if row.AmendConfirmed || onRecord == "" || !strings.EqualFold(strings.TrimSpace(picked), strings.TrimSpace(onRecord)) { // same-name row = the record's (see COMPANY)
				ch.TradeID, changed = row.TradeID, true
			}
		}
		// POSITION / PHONE ⟦gate-checkin-spec, 2026-09-13; Phone joined the
		// roster JSON under R-24, same day⟧: plain free text, applied
		// verbatim — no fold-by-name resolution or minting like
		// Company/Trade above. UNLIKE Company/Trade, a BLANK box is NEVER a
		// clear — it asserts nothing, the same "empty is absent" rule
		// rowDiffersFromRecord's own comment explains. Both now ride the
		// roster JSON a JS-cloned pick reads from (rosterPerson.Position /
		// .Phone), so a known person picked with scripting on renders both
		// boxes filled whenever the record has a value on file — but an
		// off-roster pick, or a record with nothing on file, still leaves a
		// box blank, and treating THAT blank as "clear it" would silently
		// erase a real value the officer never saw, let alone chose to
		// remove. There is no clear-Position/clear-Phone path at the gate at
		// all; /person/amend's own `clear` field is where that belongs.
		if v := strings.TrimSpace(row.Position); v != "" && v != cur(p.Position) {
			ch.Position, changed = v, true
		}
		if v := strings.TrimSpace(row.Phone); v != "" && v != cur(p.Phone) {
			ch.Phone, changed = v, true
		}
		if !changed {
			continue // the escape resolved to what the record already says
		}
		if !row.AmendConfirmed {
			// ⟦F-R2-L02-1 / A2, 2026-10-03⟧ ⟦was: "… and \"change their
			// record\" is not ticked. … Tick it if their record should change
			// … or put their current details back"⟧ — no served page shows
			// that tick any more (every gate_vehicle_form call site passes
			// HideOrgDetail, and the individual page never had one), so the
			// refusal coached a control the officer could not find, beside a
			// company list holding three identical "Tundra Services". It now
			// names what the record says and the two things that work. The
			// rule is unchanged: an unconfirmed difference is still refused.
			return nil, fmt.Errorf("Passenger %d (%s): the company, trade, position or phone sent with them is not what their record says (their record: %s). Nothing has been recorded. A check-in at the gate does not change anyone's record: set their details back to the record's — or leave Company blank, which keeps what the record says — and check them in again. To change their record, use Edit person first", row.Index+1, strings.TrimSpace(row.Typed), recordLabel(p, companies, trades))
		}
		out = append(out, ch)
	}
	return out, nil
}

// recordLabel is a person's record as the refusal names it: "Company ·
// Trade", "none on file" for either half the record lacks, then position
// and phone when the record carries them — what the officer must put back.
func recordLabel(p *domain.Person, companies []domain.Company, trades []domain.Trade) string {
	co, tr := noneOnFile, noneOnFile
	if p.CompanyID != nil {
		for _, c := range companies {
			if c.CompanyID == *p.CompanyID {
				co = c.Name
				break
			}
		}
	}
	if p.TradeID != nil {
		for _, t := range trades {
			if t.TradeID == *p.TradeID {
				tr = t.Name
				break
			}
		}
	}
	out := co + " · " + tr
	if p.Position != nil && strings.TrimSpace(*p.Position) != "" {
		out += " · " + strings.TrimSpace(*p.Position)
	}
	if p.Phone != nil && strings.TrimSpace(*p.Phone) != "" {
		out += " · " + strings.TrimSpace(*p.Phone)
	}
	return out
}

// vehicleSlots resolves every accumulated row and returns the driver slot,
// the other occupants in row order, and ⟦[[D-84]]⟧ the per-row slots in
// ROW order (driver included) for recordChanges to read against.
func (s *Server) vehicleSlots(f vehicleForm) (domain.PersonSlot, []domain.PersonSlot, []domain.PersonSlot, error) {
	var cached []domain.Person
	loaded := false
	all := func() ([]domain.Person, error) {
		if !loaded {
			p, err := domain.Persons(s.Store)
			if err != nil {
				return nil, err
			}
			cached, loaded = p, true
		}
		return cached, nil
	}
	lookup := func(id string) (*domain.Person, error) { return domain.PersonByID(s.Store, id) }

	var driver domain.PersonSlot
	var occupants []domain.PersonSlot
	perRow := make([]domain.PersonSlot, 0, len(f.Rows))
	haveDriver := false
	for _, row := range f.Rows {
		slot, err := rowPersonSlotDir(row, lookup, all, f.Direction == gateOutbound)
		if err != nil {
			return domain.PersonSlot{}, nil, nil, err
		}
		perRow = append(perRow, slot)
		if row.IsDriver {
			driver, haveDriver = slot, true
			continue
		}
		occupants = append(occupants, slot)
	}
	if !haveDriver {
		return domain.PersonSlot{}, nil, nil, fmt.Errorf("Mark who was driving. Nothing has been saved — the list below is intact. The driver flag is what an incident report is written from, and nobody is promoted into it automatically because a name nobody chose is worse than no name at all")
	}
	return driver, occupants, perRow, nil
}

// requireCompanyForNewPersons ⟦R-26, James, 2026-09-13: "the onle required
// information is last name first name and company"⟧ refuses an inbound
// commit that would ADD a new person with no company at all — neither
// picked from the list (perRow[i].CompanyID) nor typed through the add-new
// escape (perRow[i].NewCompany, already collapsed by newPersonSlot). This
// is ONE MORE required field, always suppliable on the spot; it is not a
// reason the escape itself refuses — see the doc comment above the two
// commits, which reads the supersession narrowly.
//
// Scoped to NEW slots only (ExistingID == ""): a KNOWN person's company
// already lives on their record, resolved from it by decorateRow/fromRecord
// before this door ever runs, so nothing new is asked of them here — see
// web/intake.go's own "required here, optional at the window" comment for
// why enrolment (the deliberate lane) already enforces this and check-in
// (the fast lane) is only catching up to it now, for NEW persons only.
//
// hasOrientation SKIPS this check entirely when true, on purpose — NOT a
// second orientation rule, an ORDERING one. resolvePersonSlot's own R-12
// comment already refuses EVERY new slot at an orientation post
// (GateNewPersonRefusal), unconditionally, before anything mints; a road
// post (false) is the ONLY place a gate check-in can ever create a person
// at all. Since this web-layer check runs BEFORE the domain door, checking
// company here regardless would sometimes beat the orientation refusal to
// the punch and tell the officer the wrong reason — company, when the real
// and prior reason is "nobody is added without an orientation". Company
// simply never needs checking at an orientation post: no new person can
// reach that far regardless of what this function does.
//
// rows and perRow are vehicleSlots' own inputs/outputs, in the same row
// order — never a second resolution, and never reached by the outbound
// door, which does not call vehicleSlots through this path (see
// commitGateCheckIn's own call site).
func requireCompanyForNewPersons(rows []passengerRow, perRow []domain.PersonSlot, hasOrientation bool) error {
	if hasOrientation {
		return nil
	}
	for i, row := range rows {
		if i >= len(perRow) {
			continue
		}
		slot := perRow[i]
		if slot.ExistingID != "" {
			continue // a known person's company already lives on their record
		}
		if slot.CompanyID != "" || slot.NewCompany != "" {
			continue
		}
		return fmt.Errorf("Passenger %d (%s): a company is required to check them in — pick one from the list, or choose “not listed” and type one; they are never stuck. Nothing has been recorded and the list below is intact", row.Index+1, strings.TrimSpace(row.Typed))
	}
	return nil
}

// maxNewPersonNameRunes ⟦L04 LOW, 2026-10-03⟧ bounds the name a gate
// check-in may CREATE a person under. A 528-character "name" was accepted
// and stored verbatim, and then has to fit every muster line and printed
// sheet after it. 120 characters is far past any real "Last, First" and far
// short of a paste accident. Only a NEW person is measured: an existing
// record is whatever the roster already holds, and the gate never renames.
const maxNewPersonNameRunes = 120

// overlongNameSentence is the ONE wording of the name-length refusal, used
// by the gate (per passenger row) and by /roster intake (James, 2026-10-03:
// "the same 120-character name limit, same message wording"). `kept` names
// what survives the refusal on that page ("the list below", "the form
// below").
func overlongNameSentence(n int, kept string) string {
	return fmt.Sprintf("the name typed is %d characters long — longer than any real name (the limit is %d), so it looks like something was pasted or typed by accident. Nothing has been recorded and %s is intact; check the name and shorten it", n, maxNewPersonNameRunes, kept)
}

// refuseOverlongNewNames refuses, before anything authors, a row that would
// create a person under a name longer than maxNewPersonNameRunes.
// Skipped at an orientation post for requireCompanyForNewPersons' reason: no
// new person can be created there at all, and R-12's refusal must win.
//
// James, 2026-10-04 (L02 LOW): on the one-person page there is no "Passenger
// 1" and no "list below" — single says so, and the sentence names the form.
func refuseOverlongNewNames(rows []passengerRow, perRow []domain.PersonSlot, hasOrientation, single bool) error {
	if hasOrientation {
		return nil
	}
	for i, row := range rows {
		if i >= len(perRow) || perRow[i].ExistingID != "" {
			continue
		}
		if n := utf8.RuneCountInString(perRow[i].NewName); n > maxNewPersonNameRunes {
			if single {
				// The sentence OPENS the notice here (after the template's
				// "Nothing was recorded."), so it starts with a capital —
				// intake.go's own reading of the shared wording. ⟦was: "Nothing
				// was recorded. the name typed is …" (C's F13 sweep)⟧
				return errors.New("The " + strings.TrimPrefix(overlongNameSentence(n, "what you typed"), "the "))
			}
			return fmt.Errorf("Passenger %d: %s", row.Index+1, overlongNameSentence(n, "the list below"))
		}
	}
	return nil
}

// The refusal strings in this file start with a capital on purpose, unlike
// the domain's. They are rendered as sentences straight after "Nothing was
// recorded." in the form's own error notice, where a lowercase opening reads
// as a fragment; the domain's refusals are wrapped in "domain: ..." prefixes
// and never appear as the first word of a sentence.
//
// commitPreconditions are the checks BOTH directions share, in the order a
// refusal is cheapest to fix. They are shared because they are genuinely
// the same question in both directions (did this body arrive intact?), not
// because the two forms look alike — every check whose SAFE ANSWER differs
// by direction is derived separately, at its own site.
// checkAlignment is false ONLY for the R-25 single-person quick check-in
// (handleGateCheckInSingle): that path's one row is built fresh from the
// open box, never from the POSTed occupant_* parallel arrays checkIndex
// Alignment polices, so there is no shift for it to catch — the row IS the
// whole submission. Both ordinary commits (inbound and outbound) still pass
// true, unchanged.
func (s *Server) commitPreconditions(r *http.Request, f vehicleForm, checkAlignment bool) error {
	if len(f.Rows) == 0 {
		return fmt.Errorf("No people on this vehicle yet. Add at least the driver before recording it")
	}
	if !checkAlignment {
		return nil
	}
	return checkIndexAlignment(r, len(f.Rows))
}

// ⟦R-26, James, 2026-09-13: told a hard requirement breaks the ~16 tests
// below, including one enforcing his own earlier "the gate never refuses
// anyone" / "the escape never refuses": "Refuse the check-in — company
// really is required."⟧ COMPANY IS NOW A HARD REFUSAL for a check-in that
// would CREATE a new person with none — superseding, narrowly, the
// judgment call this comment used to record (kept in git history, not
// prose, so a diff of THIS commit is the provenance).
//
// ⛔ READ THE SUPERSESSION NARROWLY. What changed: a check-in that would add
// a new person with NO company at all — neither picked from the list nor
// typed through the add-new escape — is now refused. What did NOT change:
// [[D-66]] ("the gate never refuses anyone") and [[D-71]] obligation 2 ("the
// escape never refuses") both still stand. The escape itself — picking
// "not listed" and TYPING a name — never refuses: a folded duplicate still
// resolves to the existing row (resolveOrCreateCompanyByName, checkin.go)
// rather than erroring, exactly as before. James required ONE MORE PIECE OF
// INFORMATION THE OFFICER CAN ALWAYS SUPPLY ON THE SPOT — he did not
// license the gate to refuse for any OTHER reason. D-66 was already
// narrower than its own wording (R-9 refuses an un-oriented person); this
// narrows it once more, deliberately, exactly as R-26 states.
//
// Scoped to NEW slots only (perRow[i].ExistingID == "" — see
// requireCompanyForNewPersons, below): an EXISTING person's company already
// lives on their record. web/intake.go has required company at enrolment
// since before this ruling ("A person with no company on file cannot be
// grouped on the muster, and the muster is what gets read aloud during an
// evacuation") — that asymmetry (intake's deliberate lane insists; the gate's
// fast lane, until now, did not) is intake.go's own documented reasoning for
// why the two doors could differ; R-26 removes the asymmetry for check-in's
// own new-person creation without touching intake, which already enforced
// it. See requireCompanyForNewPersons for exactly which rows this reaches.
//
// The label in the template (a "(required)" note, matching Position/Phone's
// own "(optional)" tone) now says something that is actually true.

// --- the two commits -------------------------------------------------

// handleGateCheckIn writes the whole inbound vehicle: one trip_dispatched
// plus its trip_arrival, one visit per person, the flagged row recorded as
// DriverPersonID. This is the FIRST and ONLY write in the whole inbound
// interaction.
//
// SAFE DEFAULT — the inbound commit, derived from scratch:
//
//	Committing an inbound trip ADDS people to the muster.
//	* A person OMITTED from the list is physically on site with NO RECORD
//	  AT ALL — absent from the evacuation sheet, and nobody will ever look
//	  for them. This is the DANGEROUS direction.
//	* A person wrongly INCLUDED is recorded on site when they are not: the
//	  muster over-reports and somebody is searched for who is not there.
//	  Wasted search time. Safe direction.
//	* After the commit, NOTHING WILL EVER FLAG THE OMISSION. There is no
//	  later process that notices a missing arrival, because the system has
//	  no expectation to compare against — under D-3 manifests are advisory
//	  and are never loaded.
//	Therefore this is the last moment anyone can catch a missed person, and
//	it presses hardest: the confirmation states the count back AND names
//	the consequence in the dangerous direction ("anyone not on this list
//	will not be on the muster"). See templates/gate.html, where the strings
//	live, and gate_direction_test.go, which asserts each direction
//	independently rather than through a shared helper.
//
// A REFUSAL MUST NOT COST THE OPERATOR THE LIST. Everything below
// re-renders the same page, with every accumulated row intact, and says
// what to fix. Losing a page of typing at -30 with a bus idling would be a
// far worse failure than the one being prevented.
func (s *Server) handleGateCheckIn(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if !isPassengerListForm(r) {
		s.legacyGateCheckIn(w, r)
		return
	}
	f := parseVehicleForm(r)
	f.Direction = gateInbound
	s.commitGateCheckIn(w, r, f, true)
}

// handleGateCheckInSingle ⟦R-25, James, 2026-09-13: "if a vehicle id is
// added use that as a group but if a guard wants to just check in one
// person at a time then use that."⟧ is the SECOND check-in rhythm
// alongside handleGateCheckIn's accumulate-then-commit one: no vehicle id
// typed means the officer wants ONE person checked in in ONE action, not
// "add person" followed by a separate "check in all 1 people" click —
// James: "today a single person costs two steps... that friction is what
// he is removing."
//
// It shares gate.html's open box (Last/First/Company/Position/Phone/Trade
// — openBoxRow, the exact construction handlePassengerAdd itself uses for
// an ordinary accumulate step) so nothing about WHAT is asked changes,
// only how many clicks land it: gate.html only offers this action's button
// when the Vehicle field is blank, Vehicle unknown is unticked and nothing
// has been accumulated yet — i.e. exactly the "no vehicle id" half of
// James's rule. A vehicle id typed (or Vehicle unknown ticked, or a second
// person already added) is the OTHER half, and stays on the unchanged
// accumulate-then-commit route.
//
// D-4 STILL HOLDS: this still records a vehicle, never a vehicle-less
// arrival — an UNIDENTIFIED one (R-17/R-18's existing, no-schema-change
// mechanism), exactly as if the officer had ticked "Vehicle unknown"
// themselves; it does not silently override a vehicle id that DID arrive
// in the body (a hand-crafted or replayed POST), it just never makes the
// officer tick the box for the case this route exists for.
//
// ALL-OR-NOTHING SURVIVES UNTOUCHED, and this is the point of routing
// through commitGateCheckIn rather than a parallel implementation: this is
// not a new authoring path, it is the SAME body handleGateCheckIn itself
// runs, with a one-row list built fresh from the open box instead of from
// prior accumulate steps. Nothing is authored until the whole submission
// validates — the co-mint fix (reports/witness/COMINT-ON-REFUSAL.md) and
// the D3 window fix (reports/witness/WINDOW-FIXES.md) both run inside that
// shared body, unmodified by this function existing at all.
func (s *Server) handleGateCheckInSingle(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	f := parseVehicleForm(r)
	f.Direction = gateInbound
	row, ok := s.openBoxRow(r)
	if !ok {
		// The same fumble handlePassengerAdd treats an empty box as: not an
		// error, nothing happens, the page re-renders exactly as it was.
		f.Error = ""
		s.renderGate(w, r, http.StatusOK, f)
		return
	}
	// ⟦[[D-84]]⟧ SHOW, DON'T ASK — off the same render choke point every
	// OTHER row reaches this way (renderGate -> decorateRows, between "Add
	// person" and the officer ever seeing a commit button). This route
	// skips that round trip entirely, so it must do the SAME resolution
	// itself: an exact-match existing person's Company/Trade/Position/Phone
	// come FROM THE RECORD, never from an open box nobody was asked to
	// fill in — otherwise recordChanges reads every blank box as a claimed
	// change against the record and refuses a check-in that changed
	// nothing — caught RED by
	// TestGateCheckInSingleAnExistingPersonNeedsNoConfirmationToBeUnchanged.
	companyName, tradeName, err := s.companyTradeNameMaps()
	if err != nil {
		s.gateFail(w, "companies/trades", err)
		return
	}
	lookup := func(id string) (*domain.Person, error) { return domain.PersonByID(s.Store, id) }
	all := func() ([]domain.Person, error) { return domain.Persons(s.Store) }
	decorateRow(&row, lookup, all, companyName, tradeName)
	if row.Known != nil {
		hydrateBlankFromRecord(&row, row.Known)
		// ⟦C-2 / F-L02-3, 2026-09-29⟧ pin a tick that arrived with no target
		// (the scripting-off form: orient_target renders empty until a
		// refusal re-renders it) to the person it was ticked AGAINST, so the
		// re-render carries that id and a later retype to somebody else is
		// caught by resolveTickedOrientations' staleness rule instead of
		// quietly transferring the tick. Same person on this commit, so it
		// changes nothing about what this commit authors.
		if row.AddOrientation && row.AddOrientationFor == "" {
			row.AddOrientationFor = row.Known.PersonID
		}
	}
	// The sole occupant is trivially the driver — one name, one action, one
	// person; there is no OTHER candidate this route could ever offer for
	// the officer to choose among, so there is nothing "nobody chose"
	// (vehicleSlots' own concern) about setting it here.
	row.IsDriver = true
	f.Rows = []passengerRow{row}
	// The rhythm's own precondition, applied defensively rather than
	// trusted from the button that is meant to be the only way here: a
	// blank vehicle AND an unticked "unknown" is what this whole route
	// exists to turn into an explicit R-17/R-18 unidentified vehicle. A
	// vehicle id that DID arrive (hand-crafted or replayed) is honoured as
	// a real, named vehicle instead — never silently overridden, per D-4.
	if f.Vehicle == "" && !f.VehicleUnknown {
		f.VehicleUnknown = true
	}
	s.commitGateCheckIn(w, r, f, false)
}

// rehydrateStaleRows ⟦M-14 / L03 G-102, 2026-09-29⟧ re-runs decorateRow on
// every accumulated row whose hidden id no longer agrees with its visible
// name — the JS-off edit of an added row's name to somebody else — BEFORE a
// commit reads any row. rowPersonSlot already re-resolves such a row's
// IDENTITY from the text at commit, but the row's company/trade/position/
// phone pickers still held the PREVIOUS person's record, so recordChanges
// compared them against the NEW person's and refused over fields the
// vehicle page does not even show. decorateRow's own stale branch is the
// fix already written for the render: a re-resolved known person's pickers
// start AT their record, a re-resolved new person's are reset to none. Only
// stale rows are touched; a stable row — including an in-progress,
// ticked [[D-84]] change — is left exactly as posted.
func (s *Server) rehydrateStaleRows(f *vehicleForm) error {
	lookup := func(id string) (*domain.Person, error) { return domain.PersonByID(s.Store, id) }
	var stale []int
	for i, row := range f.Rows {
		v := strings.TrimSpace(row.Value)
		if row.Kind == "new" {
			continue
		}
		if v == "" {
			// ⟦F-L03-R3-1, 2026-09-30⟧ a named row with NO id — what the
			// scripted form now posts for a row retyped in place (the served
			// default, kind=existing with no value) — is resolved from its
			// text at commit, and its pickers must start at whoever that
			// text names, exactly as decorateRow does for the scripting-off
			// add: otherwise the previous person's (or a new person's)
			// pickers are compared against the resolved record and refused.
			if strings.TrimSpace(row.Typed) != "" {
				stale = append(stale, i)
			}
			continue
		}
		p, err := lookup(v)
		if err == nil && p != nil && strings.EqualFold(strings.TrimSpace(p.Name), strings.TrimSpace(row.Typed)) {
			continue
		}
		stale = append(stale, i)
	}
	if len(stale) == 0 {
		return nil
	}
	companyName, tradeName, err := s.companyTradeNameMaps()
	if err != nil {
		return err
	}
	all := func() ([]domain.Person, error) { return domain.Persons(s.Store) }
	for _, i := range stale {
		decorateRow(&f.Rows[i], lookup, all, companyName, tradeName)
	}
	return nil
}

// gateCommitMu ⟦M-1 / G-028, 2026-10-03⟧ serialises the inbound check-in
// commits — the live form (commitGateCheckIn, both rhythms) and the legacy
// body — from the first roster read to the end of the transaction.
//
// THE DEFECT. Identity is resolved BEFORE the write transaction opens
// (vehicleSlots -> resolveTypedPerson on the ordinary store), and a typed
// name that matches nobody becomes a new person inside it. Three identical
// "Zqpair, Lee" check-ins submitted together all read "nobody by that name",
// so each minted its own person and opened its own visit: one human, three
// muster lines (L01: on site 9 -> 12; L02 rounds 1-3). The transaction alone
// does not close it — each one is internally consistent; the find that
// decided "new" simply ran outside it.
//
// Held across find AND mint, the second submit's find runs after the first
// one's commit, sees the person it minted, and checks THAT person in — the
// routine re-check-in of somebody already on site (one open visit, the safe
// direction). One process per post, so a process-wide lock is the whole
// race; cross-post races are replication's business, not this door's.
// Refusals render under the lock too: they are rare, and a lock released on
// one path but not another is the kind of bug this file cannot afford.
var gateCommitMu sync.Mutex

// commitGateCheckIn is the ONE inbound authoring body, shared by
// handleGateCheckIn (the accumulate-then-commit rhythm) and
// handleGateCheckInSingle (R-25's one-action rhythm). checkAlignment
// selects commitPreconditions' own guard — see that function's comment for
// why the single-person route passes false.
func (s *Server) commitGateCheckIn(w http.ResponseWriter, r *http.Request, f vehicleForm, checkAlignment bool) {
	// ⟦M-1 / G-028⟧ find-then-mint, serialised — see gateCommitMu.
	gateCommitMu.Lock()
	defer gateCommitMu.Unlock()
	if err := s.rehydrateStaleRows(&f); err != nil {
		s.gateFail(w, "companies/trades", err)
		return
	}
	// TP §4.5's explicit signal + TP §4.6's bounds, BEFORE anything else
	// can author: a missing signal or an out-of-bounds entered value is
	// refused with the whole accumulated list — and the typed time —
	// re-rendered intact.
	tc, cerr := s.captureTime(r)
	if cerr != nil {
		f.Error = cerr.renderMessage()
		s.renderGate(w, r, cerr.status, f)
		return
	}
	if err := s.commitPreconditions(r, f, checkAlignment); err != nil {
		f.Error = err.Error()
		s.renderGate(w, r, http.StatusConflict, f)
		return
	}
	driver, occupants, perRow, err := s.vehicleSlots(f)
	if err != nil {
		s.setVehicleFormError(&f, err)
		s.renderGate(w, r, http.StatusConflict, f)
		return
	}
	// ⟦FIX8 option C, 2026-10-05⟧ rows grouped by their effective Time in;
	// each distinct time is its own arrival (gate_split.go), authored below
	// inside the one Transact. Refusals here leave the list intact.
	groups, gerr := s.splitByTime(f, tc)
	if gerr != nil {
		f.Error = gerr.msg
		s.renderGate(w, r, gerr.status, f)
		return
	}
	for _, g := range groups {
		tc.Warnings = mergeWarnings(tc.Warnings, g.TC.Warnings)
	}
	// James, 2026-10-03: duplicate-submit lock covers deliberate declines
	// (declinelock.go). An identical minting submission inside the window
	// lands on the first one's outcome and authors nothing — so a decline
	// sent twice resolves to the person the first one minted, with one
	// open visit, not two namesakes.
	fingerprint, mints := checkInFingerprint(s.Cfg.SiteID, f, driver, occupants)
	if mints {
		if target, dup := s.duplicateOutcome(fingerprint); dup {
			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		}
	}
	// ⟦R-26⟧ a new person with no company at all is refused HERE — before
	// anything authors, list intact, same posture as every other
	// commitGateCheckIn precondition. commitGateCheckIn is the inbound-only
	// body (see its own doc comment), so this never reaches the outbound
	// door, where vehicleSlots' "new" slots are a different refusal
	// entirely (outboundPersonID: "not on the roster, check them in
	// first") — this check must never run there, and does not, because
	// outbound never calls this function. hasOrientation lets an
	// orientation post's OWN refusal (R-12, inside domain.GateCheckIn,
	// below) win instead — see requireCompanyForNewPersons' own comment.
	if err := requireCompanyForNewPersons(f.Rows, perRow, s.Cfg.HasOrientation()); err != nil {
		f.Error = err.Error()
		s.renderGate(w, r, http.StatusConflict, f)
		return
	}
	if err := refuseOverlongNewNames(f.Rows, perRow, s.Cfg.HasOrientation(), !checkAlignment); err != nil {
		f.Error = err.Error()
		s.renderGate(w, r, http.StatusConflict, f)
		return
	}
	// ⟦[[D-84]]⟧ the confirmed record changes — decided here, BEFORE anything
	// authors, and refused here (list intact) if a picker differs from a
	// known person's record without the tick.
	changes, err := s.recordChanges(f, perRow)
	if err != nil {
		f.Error = err.Error()
		s.renderGate(w, r, http.StatusConflict, f)
		return
	}
	// ⟦D-88 cross-site warning, 2026-09-10⟧ resolve which rows the officer
	// ticked to record THIS post's orientation for — see
	// resolveTickedOrientations. ⟦D3 fix, WINDOW-AUDIT.md⟧ resolution only:
	// nothing is authored here any more. The ids ride GateCheckInInput and
	// are authored INSIDE that door, once every refusal it can produce has
	// been ruled out — see GateCheckIn's own comment for why authoring the
	// tick here, before the door even runs, was the defect (a refused
	// check-in still left the tick recorded, and entry-on-completion could
	// put the ticked person on the muster from a check-in the officer was
	// told recorded nothing).
	orientIDs, staleOrientTicks := s.resolveTickedOrientations(f, perRow)
	// Taken NOW, before this vehicle's own writes — dupAdvisoryPersonIDs'
	// own contract requires a pre-commit roster read, so a match can only
	// be a pre-existing entry, never a person this same commit is about to
	// create (see dupadvisory.go).
	dupIDs := s.dupAdvisoryPersonIDs(driver, occupants)
	// ⟦W7-4(d)(i), 2026-09-03⟧ THE TYPING MOMENT IS CREATION. ⟦was:
	// "VehicleEntered: f.Vehicle"⟧ — the typed number went straight through
	// this door into the trip. It now becomes a vehicle RECORD first, here,
	// and the door is given that record's id. An empty box yields an empty id
	// and the domain's own "vehicle id is required" refusal below, unchanged.
	// ⟦TRIAL — §97/§107⟧ ⟦was: the co-mint ran HERE, above the door, so a check-in refused inside
	// `GateCheckIn` had already authored a `vehicle_created`⟧ — it is now handed to the door and
	// runs after its validations.
	// ⟦SchemaVersion 13, R-17, 2026-09-11⟧ VEHICLE UNKNOWN, now available on
	// check-in too (the checkbox used to render outbound only — see
	// gate.html's own comment on the shared vehicle-form partial). Ticked,
	// f.Vehicle is ignored and no resolver runs at all: GateCheckIn is
	// handed VehicleUnidentified directly, and resolveVehicleIDBeforeMinting
	// (checkin.go) is what actually enforces the "cannot be both named and
	// marked unidentified" refusal — the same posture the outbound door
	// already has for its own ResolveVehicle/VehicleUnidentified pair.
	// ⟦A3, 2026-09-29⟧ ONE COMMIT, ONE TRANSACTION: the vehicle co-mint, any
	// add-on-demand person/company/trade, the confirmed record changes, the
	// ticked orientations, the degenerate trip AND its arrival now land
	// together or not at all (store.Transact, transact.go). GateCheckIn's
	// own validate-then-mint ordering is unchanged; what this adds is that a
	// process death between its two Author calls — the row-31 crash window
	// its CRASH WINDOW note describes — can no longer leave this door's
	// check-in half-finished. ⚠ Everything inside the callback authors and
	// reads through `st`, the view it is handed, never s.Store (the
	// deadlock rule in transact.go); the refusal re-render below runs AFTER
	// the transaction has been discarded, on the ordinary store.
	var vehErr string
	var vehStatus int
	var arrival domain.Arrival
	err = s.Store.Transact("gate check-in", func(st *store.Store) error {
		var resolveVehicle func() (string, error)
		if !f.VehicleUnknown {
			resolveVehicle = func() (string, error) {
				id, e, code := s.vehicleIDForTypedNumber(st, f.Vehicle)
				if e != "" {
					vehErr, vehStatus = e, code
					return "", fmt.Errorf("vehicle: %s", e)
				}
				return id, nil
			}
		}
		in := domain.GateCheckInInput{
			ResolveVehicle:      resolveVehicle,
			VehicleUnidentified: f.VehicleUnknown,
			Driver:              driver,
			Occupants:           occupants,
			RecordedBy:          s.postAttribution(), // D-5: this box's post
			RecordChanges:       changes,             // [[D-84]]: applied inside the door, before the arrival
			// ⟦gate-orientation-block, 2026-09-09⟧ D-98/D-91: this post's OWN flag,
			// never a peer's. At an orientation=true post an un-oriented occupant
			// is held out (no visit) and named in the redirect below; at a road
			// post this is false and everyone enters. See notentered.go.
			HasOrientation: s.Cfg.HasOrientation(),
			// ⟦D3 fix, WINDOW-AUDIT.md⟧ authored inside GateCheckIn itself, after
			// every refusal it can produce has cleared — see resolveTickedOrientations
			// above and GateCheckIn's own comment.
			OrientationsToComplete: orientIDs,
		}
		// ⟦FIX8 option C⟧ one GateCheckIn per distinct time, all in this
		// one transaction (a single group is exactly the pre-split call).
		// Record changes and ticked orientations ride the first group only
		// — they are facts about people, authored once, and the first
		// group's door authors them before any arrival reads them.
		for gi, g := range groups {
			gin := in
			gin.Driver = perRow[g.Driver]
			gin.Occupants = nil
			for _, n := range g.Rows {
				if n != g.Driver {
					gin.Occupants = append(gin.Occupants, perRow[n])
				}
			}
			if gi > 0 {
				gin.RecordChanges = nil
				gin.OrientationsToComplete = nil
			}
			_, a, err := domain.GateCheckIn(st, s.Cfg.SiteID, s.Cfg.NodeEpoch, gin, g.TC.OccurredAtMS, g.TC.TimeSource)
			if err != nil {
				return err
			}
			arrival.NotEntered = append(arrival.NotEntered, a.NotEntered...)
		}
		return nil
	})
	if err != nil {
		// ⟦TRIAL — §97/§107⟧ a co-mint refusal keeps ITS OWN words and status. Folding it into the
		// generic 409 would tell the officer the check-in was refused rather than that the VEHICLE
		// NUMBER was — the wrong reason, on the busiest form in the application.
		if vehErr != "" {
			f.Error = vehErr
			s.renderGate(w, r, vehStatus, f)
			return
		}
		f.Error = s.gateRefusalCopy(err)
		// F12: R6's commit-time refusal of a stale decline (and the
		// stale-picked-id backstop) is the typed retired refusal, so it
		// carries the amend link like the typed-name one.
		f.ErrorAmendPersonID = retiredAmendPersonID(err)
		s.renderGate(w, r, http.StatusConflict, f)
		return
	}
	// The held-out ride the redirect so the NEXT page names them (a notice
	// that dies with the redirect is a notice nobody was shown — the
	// time-warning rule, applied to the one outcome an officer must act on).
	// ⟦orientation-doors, gap 2, 2026-09-11⟧ staleOrientTicks rides the same
	// way — see completeTickedOrientations and staletick.go.
	target := staleOrientTickRedirect(dupWarnRedirect(timeWarnRedirect(gateReturnPath(r), tc), dupIDs), staleOrientTicks)
	target = notEnteredRedirect(target, arrival.NotEntered)
	target = aboardWarnRedirect(target, s.aboardAfterCheckIn(append([]domain.PersonSlot{driver}, occupants...)))
	if mints {
		s.rememberOutcome(fingerprint, target)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// gateRefusalCopy is the officer's sentence for a refused check-in. Every
// refusal but two renders err.Error() verbatim, as this door always has; the
// ones composed here are ⟦R-12⟧'s off-roster name at an orientation post
// (domain.GateNewPersonRefusal) and ⟦D3 fix, WINDOW-AUDIT.md⟧ a ticked
// orientation's own refusal (domain.OrientationRefusal, now authored INSIDE
// GateCheckIn — see completeOrientationsBeforeArrival there), because both
// are refusals an officer must ACT on and the domain knows this post only
// by its id. They name who, why, the page that fixes it (the first), and
// that the list is intact; the template's own "Nothing was recorded." leads
// either way. No override is offered and none exists.
func (s *Server) gateRefusalCopy(err error) string {
	var newPerson *domain.GateNewPersonRefusal
	if errors.As(err, &newPerson) {
		post := s.Cfg.PostName()
		// ⟦L02 r3 LOW, 2026-10-03⟧ a NEW person whose name a roster record
		// already carries got here only because the officer answered "No —
		// this is a different person" — and "<name> is not on the roster",
		// with that very record in the list below, read as a contradiction.
		// Say what is true: the roster's <name> is somebody else, by the
		// officer's own answer.
		if namesake, err := domain.Persons(s.Store); err == nil {
			for _, p := range namesake {
				if strings.EqualFold(strings.TrimSpace(p.Name), strings.TrimSpace(newPerson.Name)) {
					return fmt.Sprintf("You said this %s is a different person from the %s already on the roster, so they would be a new person. Nobody is added to the system without an orientation, and nobody is entered at %s without %s's orientation — there is no override. Orient them, then enrol them on the roster page (/roster), which records %s's orientation as part of the enrolment, then check them in here. If they ARE the %s on the roster, type the name again and use that record.",
						newPerson.Name, newPerson.Name, post, post, post, newPerson.Name)
				}
			}
		}
		return fmt.Sprintf("%s is not on the roster. Nobody is added to the system without an orientation, and nobody is entered at %s without %s's orientation — there is no override. Orient them, then enrol them on the roster page (/roster), which records %s's orientation as part of the enrolment, then check them in here. If they are already in the system under another spelling, find them on the roster rather than adding a second record. The list below is intact.",
			newPerson.Name, post, post, post)
	}
	var orient *domain.OrientationRefusal
	if errors.As(err, &orient) {
		return s.orientationRefusalCopy(orient)
	}
	return refusalText(err)
}

// resolveTickedOrientations ⟦D-88 cross-site warning, 2026-09-10⟧ resolves
// which rows the officer ticked to record THIS post's orientation for — the
// ids ride GateCheckInInput.OrientationsToComplete and are authored INSIDE
// that door (⟦D3 fix, WINDOW-AUDIT.md⟧ completeOrientationsBeforeArrival),
// once every refusal GateCheckIn itself can produce has already been ruled
// out. This function AUTHORS NOTHING: it is the accumulate form's own row
// resolution PLUS the gap-2 staleness check, since only this door has an
// accumulate lifecycle across which a row's identity can drift.
//
// perRow is vehicleSlots' per-row resolution (driver included, row order).
// A tick only ever renders on a row decorateRows found Known (see
// gate.html), but a row can go stale between render and submit — the typed
// name edited away from the picked person. ⟦orientation-doors, gap 2,
// 2026-09-11⟧ TWO stale shapes are possible, and AddOrientationFor (the
// person id the checkbox was rendered against, posted as `orient_target`)
// is what tells them apart from an ordinary re-render:
//
//  1. The row now resolves to NOBODY known (perRow[i].ExistingID == "") —
//     the original code's case. Nothing to carry through; the tick is
//     dropped.
//  2. The row now resolves to a DIFFERENT known person (ExistingID is
//     non-empty but disagrees with AddOrientationFor) — NOT caught before
//     the orientation-doors fix, and the sharper defect: carrying it
//     through blindly would have granted an orientation completion to a
//     person the officer never confirmed, attributed as if they had.
//
// Either way this is a REFUSAL OF THE TICK, NOT OF THE VEHICLE — refusing a
// whole bus over one stale checkbox is worse than the ordinary R-9
// hold-out it falls back to. staleTargets carries the ORIGINAL ticked
// identity (AddOrientationFor) of every such row back to the caller, so the
// post-commit redirect can tell the officer BY NAME that their tick did not
// apply — the officer must never be left believing an orientation was
// recorded when it was not. A body that never posts `orient_target`
// (hand-crafted, or predates the orientation-doors fix) has
// AddOrientationFor == "" on every row, which this treats as "not tracked"
// and falls back to the pre-fix rule (trust perRow alone, no notice) — see
// passengerRow.AddOrientationFor.
func (s *Server) resolveTickedOrientations(f vehicleForm, perRow []domain.PersonSlot) (ids, staleTargets []string) {
	if !s.Cfg.HasOrientation() {
		return nil, nil // D-88: this post has no orientation concept to tick
	}
	staleSeen := map[string]bool{}
	for i, row := range f.Rows {
		if !row.AddOrientation || i >= len(perRow) {
			continue
		}
		id := perRow[i].ExistingID
		if row.AddOrientationFor != "" && row.AddOrientationFor != id {
			if !staleSeen[row.AddOrientationFor] {
				staleSeen[row.AddOrientationFor] = true
				staleTargets = append(staleTargets, row.AddOrientationFor)
			}
			continue
		}
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	// ⟦X-MERGE-WARN, 2026-10-03⟧ the tick is authored for the record the
	// commit will READ: buildArrivalVisits resolves a merged-away id to its
	// canonical record before OrientationHeld, and decorateRows now warns on
	// that same resolution — so a tick recorded under the raw duplicate id
	// would leave the person warned, ticked AND held out. The staleness
	// check above stays on the raw ids the page posted. An unreadable map
	// leaves the ids raw — today's behaviour, never a guess.
	if len(ids) > 0 {
		if merge, err := domain.LoadPersonMergeMap(s.Store); err == nil {
			for i := range ids {
				ids[i] = merge.Resolve(ids[i])
			}
		}
	}
	return ids, staleTargets
}

// completeOrientationsFor / completeOrientationsForTrip ⟦orientation-doors,
// 2026-09-11⟧ stood here: the web layer's own call into
// domain.CompleteOrientationsSkippingHeld for the one door that did not run
// through GateCheckIn (/gate/resume), scoped to the trip's occupants.
// ⟦A3 / H-6, 2026-09-29⟧ RETIRED — not because the path was wrong but
// because of WHERE it ran: before the resume door's own refusals, so a
// refused resume had already recorded the tick (FINDINGS H-6). The tick now
// rides into domain.ResumeCheckInCompleting (checkin.go), which does the
// identical scoping and authors through the identical path, AFTER every
// refusal — the same shape GateCheckIn's D3 fix gave the check-in doors.
// The officer-facing sentence for a ticked orientation's own refusal
// (orientationRefusalCopy) is applied by handleGateResume on the way out.

// scopeToKnown intersects a door's posted `add_orientation_person` ids
// against the ExistingID of a set of already-resolved slots — the resume
// and legacy doors' equivalent of the accumulate form's row resolution.
// Neither door has a server-resolved "row" to key a tick to (resume's
// occupants are a trip's fixed, already-dispatched set; the legacy body has
// no accumulate step at all), so a posted id is trusted only as far as
// "actually names one of these slots" — never blindly, the same guarantee
// the accumulate form gets from resolving identity server-side rather than
// reading a client-supplied one outright.
func scopeToKnown(slots []domain.PersonSlot, posted []string) []string {
	if len(posted) == 0 {
		return nil
	}
	valid := make(map[string]bool, len(slots))
	for _, sl := range slots {
		if sl.ExistingID != "" {
			valid[sl.ExistingID] = true
		}
	}
	var ids []string
	for _, id := range posted {
		if valid[id] {
			ids = append(ids, id)
		}
	}
	return ids
}

// handleGateVehicleCheckOut writes the whole OUTBOUND vehicle: one trip,
// the flagged row recorded as DriverPersonID, every occupant checked out of
// this site by the dispatch fold.
//
// WHAT THIS CLOSES. Until now the gate officer's only exit path was POST
// /gate/checkout, which reads visit ids and NOTHING ELSE — no vehicle, no
// driver. The driver flag exists so that after an incident the report can
// say who was at the wheel, and James's own reason for wanting the outbound
// leg to look like the inbound one is that "the driver will likely change"
// between them. An exit path that cannot record a driver defeats the flag
// on exactly the leg where it changes.
//
// SAFE DEFAULT — the outbound commit, DERIVED INDEPENDENTLY. This is not
// "the same as inbound", and it must never be written as if it were:
// bulk-movement-v1.md's rule is that the safe default depends on whether an
// action ADDS or REMOVES somebody from the muster, not on the widget, and
// one widget serving both directions is exactly the case that rule guards.
// This project has already reused a widget by analogy once (the pad-return
// manifest, "same ARRIVAL derivation as the inbound board above") and
// shipped an unguarded surface.
//
//	Committing an outbound trip REMOVES people from the muster.
//	* A person OMITTED merely stays marked on site after leaving: the
//	  muster over-reports, somebody is searched for who has gone. Wasted
//	  search time. SAFE direction.
//	* A person wrongly INCLUDED is recorded as having left while still on
//	  site — present, absent from the sheet. Dangerous. But reaching it
//	  requires the operator to actively type a specific person's name into
//	  a departing vehicle, which is a different and much rarer error than
//	  forgetting one.
//	* Unlike inbound, the omission IS self-correcting in practice: the
//	  person stays on the muster, and the next check-in or the next muster
//	  read surfaces them.
//	Therefore outbound does NOT carry inbound's "will not be on the muster"
//	warning — it would be false here, and a warning that is false on one
//	form trains operators to read past it on the form where it is true. It
//	still states the count back, because the count is cheap and catches the
//	wrongly-included case, which is the dangerous one in THIS direction.
//
// The one thing identical in both directions is the driver flag, because
// its purpose (the incident report) is direction-independent.
//
// AND ONE MECHANICAL DIFFERENCE THAT FALLS OUT OF THE SAME DERIVATION: a
// typed name that matches nobody on the roster CREATES a person inbound
// (over-reporting, safe) and is REFUSED outbound. Checking out a person the
// system has never heard of records a departure that cannot correspond to
// any arrival; the honest answer is that they were never checked in, and
// the operator is told to do that first.
func (s *Server) handleGateVehicleCheckOut(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	f := parseVehicleForm(r)
	f.Direction = gateOutbound
	if !isPassengerListForm(r) {
		http.Error(w, "the outbound gate form submits an accumulated passenger list; this body carries none", http.StatusBadRequest)
		return
	}
	if err := s.rehydrateStaleRows(&f); err != nil {
		s.gateFail(w, "companies/trades", err)
		return
	}
	tc, cerr := s.captureTime(r)
	if cerr != nil {
		f.Error = cerr.renderMessage()
		s.renderGate(w, r, cerr.status, f)
		return
	}
	if err := s.commitPreconditions(r, f, true); err != nil {
		f.Error = err.Error()
		s.renderGate(w, r, http.StatusConflict, f)
		return
	}
	driverSlot, occupantSlots, perRow, err := s.vehicleSlots(f)
	if err != nil {
		s.setVehicleFormError(&f, err)
		s.renderGate(w, r, http.StatusConflict, f)
		return
	}
	// ⟦[[D-84]]⟧ THE LEAVING FORM RENDERS THE SAME ROW, so it honours the
	// same tick: a confirmed change made while checking a crew OUT amends the
	// record exactly as one made checking them in — and an unconfirmed
	// difference is refused here too. Silently ignoring the row's pickers on
	// this form would be the discarded-box defect back under another name.
	changes, err := s.recordChanges(f, perRow)
	if err != nil {
		f.Error = err.Error()
		s.renderGate(w, r, http.StatusConflict, f)
		return
	}
	driverID, err := outboundPersonID(driverSlot, "the driver")
	if err != nil {
		f.Error = err.Error()
		s.renderGate(w, r, http.StatusConflict, f)
		return
	}
	occupantIDs := make([]string, 0, len(occupantSlots))
	for _, slot := range occupantSlots {
		id, err := outboundPersonID(slot, "a passenger")
		if err != nil {
			f.Error = err.Error()
			s.renderGate(w, r, http.StatusConflict, f)
			return
		}
		occupantIDs = append(occupantIDs, id)
	}

	// ⟦FIX8 option C, 2026-10-05⟧ rows grouped by their effective Time out;
	// each distinct time is its own departure (gate_split.go). A named
	// vehicle with differing times, or a group with no driver, is refused
	// here, list intact. Then an ENTERED time out earlier than anyone in
	// that group's recorded time in is refused — nobody leaves before they
	// arrived (gate_rowtime.go).
	groups, gerr := s.splitByTime(f, tc)
	if gerr != nil {
		f.Error = gerr.msg
		s.renderGate(w, r, gerr.status, f)
		return
	}
	rowIDs := make([]string, len(perRow))
	for i, slot := range perRow {
		id, err := outboundPersonID(slot, "a passenger")
		if err != nil {
			f.Error = err.Error()
			s.renderGate(w, r, http.StatusConflict, f)
			return
		}
		rowIDs[i] = id
	}
	for _, g := range groups {
		ids := make([]string, 0, len(g.Rows))
		for _, n := range g.Rows {
			ids = append(ids, rowIDs[n])
		}
		if rerr := s.refuseOutBeforeInPersons(ids, g.TC); rerr != nil {
			f.Error = rerr.msg
			s.renderGate(w, r, rerr.status, f)
			return
		}
		tc.Warnings = mergeWarnings(tc.Warnings, g.TC.Warnings)
	}

	// EVERYBODY ON A DEPARTING VEHICLE MUST ALREADY BE ON SITE.
	//
	// DERIVED FOR THE REMOVE DIRECTION, ON ITS OWN. Committing here REMOVES
	// people from the muster. The failure this guards is a departure
	// recorded for somebody who never arrived: it authors a trip claiming
	// they left, and — because a controlled destination creates an inbound
	// expectation with a deadline at the far end — it puts a person nobody
	// has ever seen onto the receiving post's board with a timer running
	// against them. Nothing downstream would question it.
	//
	// WHAT THE DOMAIN DOES WITHOUT THIS, established rather than assumed:
	// the vehicle form's commit is domain.Dispatch, which validates roster
	// MEMBERSHIP (ValidateRoster) but not PRESENCE. Its fold closes each
	// occupant's open visit at this site, and closing a visit that does not
	// exist is a no-op — so a person who was never here is accepted in
	// silence. (outboundPersonID above refuses somebody who is not on the
	// ROSTER, which is a different and much narrower check: it catches a
	// typo, never a wrong person.) The ticked-list check-out has no such
	// hole, because it is driven by visit ids and CheckOutPeople refuses an
	// already-closed one by name.
	//
	// This refusal is therefore the only thing standing between a mis-click
	// and a departure with no arrival behind it. It names everybody
	// involved, changes nothing, and leaves the accumulated list intact —
	// re-typing a bus because of a refusal would be a worse failure than
	// the one being prevented.
	//
	// NOTE THE ASYMMETRY, and it is deliberate: there is no matching
	// "already on site" refusal on the ARRIVING commit, because that case
	// is benign by construction. Re-running an on-site person through the
	// check-in form closes their prior visit and opens a replacement
	// (domain.GateCheckIn's own comment: "the routine stale-visit
	// correction... benign on its own"), so no duplicate visit exists to
	// prevent and the person stays on the muster throughout — the SAFE
	// direction. Refusing it would block a correction operators actually
	// make.
	{
		openVisits, err := domain.OpenVisitsAt(s.Store, s.Cfg.SiteID)
		if err != nil {
			s.gateFail(w, "open visits", err)
			return
		}
		onSite := make(map[string]bool, len(openVisits))
		for _, v := range openVisits {
			onSite[v.PersonID] = true
		}
		var absent []string
		for _, id := range append([]string{driverID}, occupantIDs...) {
			if onSite[id] {
				continue
			}
			name := id
			if p, err := domain.PersonByID(s.Store, id); err == nil && p != nil {
				name = p.Name
			}
			absent = append(absent, name)
		}
		if len(absent) > 0 {
			f.Error = fmt.Sprintf(
				"%s not recorded as being on site, so this vehicle cannot be checked OUT. Nothing has been recorded and the list below is intact. If this vehicle is ARRIVING, use the check-in — checking it out would record a departure for people who were never here, and at a destination with a gate it would start a deadline timer against them. If they really are here and were missed, check them in first.",
				namesArePlural(absent))
			s.renderGate(w, r, http.StatusConflict, f)
			return
		}
	}

	// A DEPARTING VEHICLE MUST RECORD WHERE IT WENT, and this is not
	// tidiness — it is what keeps the muster's one incompleteness warning
	// believable.
	//
	// FOUND BY BUILDING IT, then confirmed against the code: a trip with no
	// destination, no deadline and no arrival is BYTE-IDENTICAL to a
	// check-in that crashed between its two events. domain.IncompleteCheckIns
	// selects exactly that shape (open, this site's, DestinationSite nil,
	// DestinationUncontrolled nil, DeadlineMinutes nil), so a
	// destination-less departure appears on /gate under "Incomplete
	// check-ins — needs attention" with a RESUME button that would check
	// the departed crew back onto this muster with one click, and on the
	// printed muster as MAY BE INCOMPLETE. Every routine departure, six
	// days a week. That is alarm fatigue on the one warning that stops a
	// sheet being silently short.
	//
	// Any destination — controlled or a pad — removes the ambiguity,
	// because IncompleteCheckIns requires all three fields to be nil. So
	// the gate asks, and refuses to guess.
	//
	// (The operator console's own "Local check-out only (no destination)"
	// button had the identical defect. D-4 removed it: the button is gone
	// from trips.html, applyDispatchDestination refuses an empty
	// destination, and domain.Dispatch refuses a destination-less trip
	// outright — so this refusal is now the OUTER of three, kept because it
	// is the only one that can re-render the accumulated list intact.)
	if strings.TrimSpace(f.DestinationID) == "" {
		f.Error = "Say where this vehicle is going. Nothing has been recorded and the list below is intact. A departure with no destination is indistinguishable from a check-in that crashed half-way — it would appear on this page as an incomplete check-in offering to put these people back on the muster, and the printed muster would read MAY BE INCOMPLETE."
		s.renderGate(w, r, http.StatusConflict, f)
		return
	}

	// ⟦W7-4(d)(i), 2026-09-03⟧ ⟦was: "// NO TrimSpace on the vehicle: the
	// entered text is stored verbatim (M8); comparison alone normalises." /
	// "VehicleEntered: f.Vehicle,"⟧ — M8's verbatim-storage rule has not been
	// dropped, it has MOVED with the typing: the number is stored verbatim on
	// the vehicle RECORD (CreateVehicle trims only leading/trailing space, as
	// every typed reference name here does), and the trip carries the record's
	// id plus a snapshot derived from it.
	// VEHICLE UNKNOWN ⟦James, 2026-09-11⟧: "vehicle should not be a reason
	// to not check someone out". A ticked box on the outbound form takes
	// this branch instead of resolving a typed number.
	//
	// ⟦SchemaVersion 13, R-17, 2026-09-11⟧ ⟦was: "vehicleID, vehErr, vehStatus
	// = s.vehicleIDForUnknown()"⟧ — that minted a fresh, distinct,
	// explicitly-labelled vehicle RECORD per departure (never a shared
	// string, so it could not become the "walk-in" collision class), which
	// worked but accumulated one indistinguishable "Vehicle unknown" row
	// per unidentified departure forever on the admin vehicle-merge screen
	// (internal/web/vehicle_amend.go, domain.Vehicles()' one non-test
	// caller). R-17's schema path removes the need to mint anything at
	// all: VehicleUnidentified records the fact directly on the trip, no
	// vehicle record is created, and vehicleIDForUnknown/vehicleUnknownRole
	// (vehicle_comint.go) are retired — this was their only call site.
	// f.Vehicle is simply ignored when the box is ticked, so a stray typed
	// fragment left behind by an edit cannot silently override an explicit
	// "I don't know" or vice versa — one control wins, by name, every time.
	//
	// ⟦A3 / H-4, 2026-09-29⟧ ⟦was: "vehicleID, vehErr, vehStatus =
	// s.vehicleIDForTypedNumber(f.Vehicle)" HERE, above applyDispatchDestination
	// and domain.Dispatch — so a departure refused further down (a blank or
	// bad deadline, ⟦was: Phase 3 with no override,⟧ an unknown destination, a
	// roster miss) had already minted, and replicated, a vehicle record
	// under "Nothing was recorded" (FINDINGS H-4; L03 G-069, G-079(a); the
	// DEFAULT JS-on departure hit it first try via G-070)⟧. The co-mint is
	// now handed to Dispatch as ResolveVehicle — the §97 deferral this
	// door's sibling /trips/dispatch already uses (L07 T-PHANTOM: seven
	// refusal kinds, zero rows) — and runs only after every refusal
	// dispatchTrip can produce. A co-mint refusal keeps ITS OWN words and
	// status (vehErr below), exactly as handleDispatch restores them.
	var vehErr string
	var vehStatus int
	in := domain.DispatchInput{
		VehicleUnidentified: f.VehicleUnknown,
		DriverPersonID:      driverID,
		OccupantPersonIDs:   occupantIDs,
		// ⟦[[D-84]] / A3 H-5⟧ applied INSIDE Dispatch, after every refusal
		// and the co-mint, immediately before the trip — see
		// DispatchInput.RecordChanges. ⟦was: domain.ApplyPersonRecordChanges
		// called here, before domain.Dispatch, so a refused departure had
		// already amended the person's record (FINDINGS H-5; L03 G-079(b))⟧.
		RecordChanges: changes,
		// F4 + F11: this handler is reachable by a raw, hand-crafted POST,
		// so it sets every membership check, exactly as handleDispatch does.
		ValidateRoster: true,
		// The CONTROLLED destination list, not the peer list — see
		// handleDispatch (trips.go) for why the two differ since D-77. This
		// door names a destination only through destination_id, which
		// applyDispatchDestination already resolves against DestinationList;
		// this is the domain backstop brought into line with that resolution.
		KnownDestinationSites:         s.Cfg.ControlledDestinationIDs(),
		KnownUncontrolledDestinations: s.Cfg.UncontrolledDestinationIDs(),
		// ⟦was: PhaseGate (s.Cfg.DispatchAllowed + f.OverrideReason) — weather
		// phases removed, James 2026-10-04⟧
		RecordedBy: s.postAttribution(), // D-5: this box's post
	}
	if _, err := applyDispatchDestination(s.Cfg, f.DestinationID, "", f.DeadlineMin, &in); err != nil {
		f.Error = err.Error()
		s.renderGate(w, r, http.StatusConflict, f)
		return
	}
	// ⟦A3, 2026-09-29⟧ ONE COMMIT, ONE TRANSACTION: the co-mint, the record
	// changes and the trip land together or not at all (store.Transact).
	// Everything inside the callback authors and reads through `st`, the
	// view it is handed, never s.Store (transact.go's deadlock rule); the
	// refusal re-render runs after the transaction has been discarded.
	err = s.Store.Transact("gate check-out", func(st *store.Store) error {
		if !f.VehicleUnknown {
			in.ResolveVehicle = func() (string, error) {
				id, e, code := s.vehicleIDForTypedNumber(st, f.Vehicle)
				if e != "" {
					vehErr, vehStatus = e, code
					return "", fmt.Errorf("vehicle: %s", e)
				}
				return id, nil
			}
		}
		// ⟦FIX8 option C⟧ one Dispatch per distinct time, all in this one
		// transaction. A single group is exactly the pre-split call. The
		// confirmed record changes ride the first trip only (applying them
		// twice would author them twice); each trip's deadline is judged
		// from its own typed time plus the allowance (James 2026-10-06:
		// from the typed time — domain.EffectiveDeparture) ⟦was: the D13
		// basis, its envelope's authored_at⟧.
		for gi, g := range groups {
			gin := in
			gin.DriverPersonID = rowIDs[g.Driver]
			gin.OccupantPersonIDs = nil
			for _, n := range g.Rows {
				if n != g.Driver {
					gin.OccupantPersonIDs = append(gin.OccupantPersonIDs, rowIDs[n])
				}
			}
			if gi > 0 {
				gin.RecordChanges = nil
			}
			if _, err := domain.Dispatch(st, s.Cfg.SiteID, s.Cfg.NodeEpoch, gin, g.TC.OccurredAtMS, g.TC.TimeSource); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if vehErr != "" {
			f.Error = vehErr
			s.renderGate(w, r, vehStatus, f)
			return
		}
		f.Error = err.Error()
		s.renderGate(w, r, http.StatusConflict, f)
		return
	}
	http.Redirect(w, r, timeWarnRedirect(gateReturnPath(r), tc), http.StatusSeeOther)
}

// outboundPersonID is the REMOVE-direction identity rule. See the
// derivation on handleGateVehicleCheckOut: creating a person in order to
// immediately record them as having left is a record with no arrival behind
// it, so it is refused by name rather than authored.
func outboundPersonID(slot domain.PersonSlot, what string) (string, error) {
	if slot.ExistingID != "" {
		return slot.ExistingID, nil
	}
	return "", fmt.Errorf("%q is not on the roster, so %s cannot be checked out under that name. Nothing has been recorded and the list below is intact. Somebody who was never checked in has no visit to close — check them in at the gate first if they are here, or correct the spelling", strings.TrimSpace(slot.NewName), what)
}

// legacyGateCheckIn is the PRE-EXISTING check-in body shape: a driver slot
// (driver_kind/driver_value/driver_typed) plus bare occupant arrays with no
// row numbers and no driver flag. It is kept working, unchanged, for the
// same reason handleGateCheckOut still accepts a single `visit_id` beside
// `visit_ids`: any deployed muscle memory, bookmark or raw POST that used
// it keeps its exact meaning.
//
// It is NOT a fallback for the new form. The new form always carries
// passenger_list=1, so a body reaching here is one that never had a
// passenger list, and the stricter rules the marker turns on (a driver is
// mandatory, row numbers must be contiguous, a blank row is refused rather
// than skipped) are exactly the rules the old shape has no way to satisfy.
func (s *Server) legacyGateCheckIn(w http.ResponseWriter, r *http.Request) {
	// ⟦M-1 / G-028⟧ the same serialisation as the live form — see gateCommitMu.
	gateCommitMu.Lock()
	defer gateCommitMu.Unlock()
	// TP §4.5's explicit signal + bounds, exactly as on the new body: the
	// legacy SHAPE is kept live, but it is a capturing takes-input surface,
	// not a tolerated fossil — a legacy POST carrying time_source=entered
	// captures end-to-end, and one carrying no signal at all is refused 400
	// (seam S-1's recorded default: TP §4.5 wins, narrowly). The refusal
	// re-renders the gate page with every posted field rebuilt into the
	// form — the refused traffic is by construction an officer's stale
	// pre-bump tab holding typed work, and a bare error wall would convert
	// the bump itself into lost records (TP §4.6 rule 6).
	tc, cerr := s.captureTime(r)
	if cerr != nil {
		f := legacyVehicleForm(r)
		f.Error = cerr.renderMessage()
		s.renderGate(w, r, cerr.status, f)
		return
	}
	// The roster is only loaded for the typed-name fallback, so it costs
	// nothing on the path where every slot arrives resolved.
	var persons []domain.Person
	needRoster := strings.TrimSpace(r.FormValue("driver_value")) == ""
	for i, v := range r.Form["occupant_value"] {
		if strings.TrimSpace(v) == "" && strings.TrimSpace(at(r.Form["occupant_typed"], i)) != "" {
			needRoster = true
		}
	}
	if len(r.Form["occupant_typed"]) > len(r.Form["occupant_value"]) {
		needRoster = true
	}
	if needRoster {
		var err error
		if persons, err = domain.Persons(s.Store); err != nil {
			s.gateFail(w, "persons", err)
			return
		}
	}

	driver, err := personSlotFrom(
		r.FormValue("driver_kind"), r.FormValue("driver_value"), r.FormValue("driver_typed"),
		r.FormValue("driver_company"), r.FormValue("driver_trade"), persons)
	if err != nil {
		http.Error(w, refusalText(errors.New(s.typedNameRefusalText(err))), http.StatusConflict)
		return
	}

	kinds := r.Form["occupant_kind"]
	values := r.Form["occupant_value"]
	typed := r.Form["occupant_typed"]
	companies := r.Form["occupant_company"]
	trades := r.Form["occupant_trade"]
	// Iterate the LONGEST of the parallel arrays. Iterating `kinds` alone
	// would drop any row whose hidden inputs never arrived — which is
	// exactly the shape that used to lose server-rendered occupants.
	rows := len(kinds)
	for _, n := range []int{len(values), len(typed), len(companies), len(trades)} {
		if n > rows {
			rows = n
		}
	}
	var occupants []domain.PersonSlot
	for i := 0; i < rows; i++ {
		occ, err := personSlotFrom(at(kinds, i), at(values, i), at(typed, i),
			at(companies, i), at(trades, i), persons)
		if err != nil {
			http.Error(w, refusalText(errors.New(s.typedNameRefusalText(err))), http.StatusConflict)
			return
		}
		if occ.ExistingID == "" && occ.NewName == "" {
			// A row left blank — skip it, do not refuse.
			continue
		}
		occupants = append(occupants, occ)
	}

	// ⟦orientation-doors, gap 1, 2026-09-11⟧ the SAME tick the accumulate
	// form's cross-site warning offers, extended to this body —
	// reports/witness/CROSSSITE-WARNING.md's declared gap 1. No LIVE
	// template renders an inline warning for this shape (this handler's own
	// header: isPassengerListForm's marker means every current page emits
	// passenger_list=1, so a body reaching here is hand-crafted or a
	// genuinely stale pre-redesign tab that cannot be made to render one
	// retroactively) — this closes "no way to tick" at the protocol level
	// instead: a caller that HAS the person id and wants to tick it can,
	// through the identical authoring path, rather than this shape being
	// structurally unable to ever offer one. Scoped (scopeToKnown) to this
	// vehicle's own resolved driver+occupants, never a posted id trusted
	// beyond "actually named by this vehicle". ⟦D3 fix, WINDOW-AUDIT.md⟧
	// resolved here, authored INSIDE GateCheckIn below (same as the live
	// form) — this door had the identical pre-fix defect (the tick used to
	// author before this door's own vehicle validation ran).
	orientIDs := scopeToKnown(append([]domain.PersonSlot{driver}, occupants...), r.Form["add_orientation_person"])
	// ⟦W7-4(d)(i), 2026-09-03⟧ ⟦was: "VehicleEntered: r.FormValue(\"vehicle\")"⟧
	// — the same co-mint as the live form above, because a stale tab posting
	// the legacy body must not reach a door the live form cannot.
	// ⟦TRIAL — §97/§107⟧ THE LEGACY BODY GETS THE SAME DEFERRAL AS THE LIVE FORM, and this site's own
	// existing comment is the reason it must: "a stale tab posting the legacy body must not reach a
	// door the live form cannot." ⚠ Fixing only the live form would leave the defect reachable from
	// exactly the request nobody is watching.
	// Taken NOW, before this vehicle's own writes — same contract as the
	// live form's call (see dupadvisory.go and handleGateCheckIn).
	dupIDs := s.dupAdvisoryPersonIDs(driver, occupants)
	// ⟦A3, 2026-09-29⟧ the same one-commit transaction as the live form
	// (commitGateCheckIn) — "a stale tab posting the legacy body must not
	// reach a door the live form cannot", and must not reach a weaker one
	// either. Inside the callback: `st`, never s.Store (transact.go).
	var vehErr string
	var vehStatus int
	var arrival domain.Arrival
	err = s.Store.Transact("gate check-in (legacy body)", func(st *store.Store) error {
		resolveVehicle := func() (string, error) {
			id, e, code := s.vehicleIDForTypedNumber(st, r.FormValue("vehicle"))
			if e != "" {
				vehErr, vehStatus = e, code
				return "", fmt.Errorf("vehicle: %s", e)
			}
			return id, nil
		}
		in := domain.GateCheckInInput{
			ResolveVehicle: resolveVehicle,
			Driver:         driver,
			Occupants:      occupants,
			RecordedBy:     s.postAttribution(),    // D-5: this box's post
			HasOrientation: s.Cfg.HasOrientation(), // ⟦gate-orientation-block⟧ same rule as the live form
			// ⟦D3 fix, WINDOW-AUDIT.md⟧ authored inside GateCheckIn itself, after
			// every refusal it can produce has cleared — same field the live
			// form uses.
			OrientationsToComplete: orientIDs,
		}
		var err error
		_, arrival, err = domain.GateCheckIn(st, s.Cfg.SiteID, s.Cfg.NodeEpoch, in, tc.OccurredAtMS, tc.TimeSource)
		return err
	})
	if err != nil {
		// ⟦TRIAL — §97/§107⟧ same as the live form: the co-mint's own refusal survives.
		if vehErr != "" {
			http.Error(w, vehErr, vehStatus)
			return
		}
		// ⟦R-12⟧ the stale tab gets the same composed sentence as the live
		// form for the one refusal the officer must act on; everything else
		// stays verbatim, as it was.
		http.Error(w, s.gateRefusalCopy(err), http.StatusConflict)
		return
	}
	http.Redirect(w, r, notEnteredRedirect(dupWarnRedirect(timeWarnRedirect("/gate", tc), dupIDs), arrival.NotEntered), http.StatusSeeOther)
}

// legacyVehicleForm rebuilds a refusal re-render from a LEGACY-shaped
// check-in body (driver slot + bare occupant arrays), so a refused stale
// tab's typed work lands back in the live form instead of vanishing behind
// an error wall. Nothing here validates or resolves anything — it is a
// faithful transcription of what was posted, for the operator to fix and
// resubmit.
func legacyVehicleForm(r *http.Request) vehicleForm {
	f := vehicleForm{
		Direction:      gateInbound,
		Vehicle:        r.FormValue("vehicle"),
		TimeSource:     r.FormValue(timeSourceField),
		OccurredAtWall: r.FormValue(occurredAtField),
	}
	addRow := func(kind, value, typed, company, trade string, isDriver bool) {
		if strings.TrimSpace(typed) == "" && strings.TrimSpace(value) == "" {
			return // a blank legacy row carries nothing to preserve
		}
		if typed == "" {
			typed = value // the visible field is what the operator edits
		}
		f.Rows = append(f.Rows, passengerRow{
			Kind: kind, Value: value, Typed: typed,
			Company: company, Trade: trade, IsDriver: isDriver,
		})
	}
	addRow(r.FormValue("driver_kind"), r.FormValue("driver_value"), r.FormValue("driver_typed"),
		r.FormValue("driver_company"), r.FormValue("driver_trade"), true)
	kinds := r.Form["occupant_kind"]
	values := r.Form["occupant_value"]
	typed := r.Form["occupant_typed"]
	companies := r.Form["occupant_company"]
	trades := r.Form["occupant_trade"]
	rows := len(kinds)
	for _, n := range []int{len(values), len(typed), len(companies), len(trades)} {
		if n > rows {
			rows = n
		}
	}
	for i := 0; i < rows; i++ {
		addRow(at(kinds, i), at(values, i), at(typed, i), at(companies, i), at(trades, i), false)
	}
	f.renumber()
	return f
}

// handleGateCheckOut takes EITHER form of the same request:
//
//   - visit_ids — the U2 batch: the on-site picker's ticked rows, any
//     number of them, in one POST. This is the fix for the only path that
//     measured badly in round trips (52 for 26 people, one POST + one
//     redirect-GET per row).
//   - visit_id — the original single-visit field, unchanged in name and
//     meaning. Any deployed muscle memory, bookmark or raw POST that used it
//     still works exactly as before.
//
// Both go through domain.CheckOutVisits, so there is ONE authoring path and
// the single case is simply a batch of one. That batching is of the REQUEST
// only: every visit still gets its own visit_checked_out event, so
// per-person accuracy and replication semantics are untouched.
//
// WHAT LICENSED THE DOMAIN EDIT, stated because the sentence above does not
// and has been read as if it did (S7-1, scope-creep). THE ROUND-TRIP
// MEASUREMENT DID NOT. A handler-local loop over the pre-existing
// domain.CheckOutVisit would have measured 2 round trips as well: the 52->2
// win comes entirely from consolidating 96 one-button <form>s into one
// form, which is a presentation-layer change requiring no domain edit at
// all. TestScriptedBatchCheckOutStaysWithinTheRoundTripBudget counts HTTP
// dispatches, and a loop would have satisfied it.
//
// What actually required the domain is the ALL-OR-NOTHING
// validate-then-author refusal rule — every id checked (empty, duplicate,
// unknown, other-site, already-closed) and the WHOLE batch refused before
// anything is written. That is a statement about what the system records,
// and a handler is the one place it must not live, because the next caller
// gets a half-applied batch. A handler loop authors the first fifteen and
// then stops.
//
// The distinction matters beyond tidiness: a builder who reads "a bad
// round-trip measurement licenses an edit to a deployed domain" has been
// handed a licence far wider than the facts support, and this is a
// replicated, schema-versioned sibling package.
//
// WHAT THIS IS SINCE D-4, AND WHAT IT IS NOT. It used to be described as
// "the individual / ad-hoc departure ... somebody leaving off-vehicle or a
// correction". D-4 deletes the first half of that sentence: at this
// operation — Arctic North Slope, winter, private property — there are no
// privately owned vehicles and no pedestrians, so there is no such thing as
// leaving off-vehicle. Every departure is the departing-vehicle form
// (POST /gate/checkout/vehicle), which records the vehicle and the driver.
//
// THIS PATH IS KEPT, AND ONLY FOR CORRECTING A MISTAKEN CHECK-IN — which is
// a different act from a departure, sharing a code path by accident of
// history. The reason to keep it is that a check-in can be wrong (the wrong
// name picked off the roster, somebody entered twice, somebody recorded
// arriving who never got out of the truck), and there is no other way to
// take that name off the muster. An uncorrectable phantom is the failure
// D-3 names as the dangerous one in the other direction: during an
// evacuation people search for somebody who was never there.
//
// It is HONESTLY WORSE than a departure and the copy says so: it removes a
// person from the muster with NO vehicle trail, so an incident report
// afterwards cannot say how they left, because the answer is that they did
// not. Use it when the arrival record is wrong; never to record somebody
// leaving.
//
// Note the one ragged edge this leaves, recorded rather than hidden: when a
// departing vehicle was committed with somebody missing from its passenger
// list, this list is also the only remedy — and correcting it here leaves
// that person with no vehicle trail even though they left in one. There is
// no "add a person to an already-authored trip" path, and inventing one is
// out of scope here.
//
// The event authored is still visit_checked_out (no schema change): what
// changed is what the operator is told this control is for.
func (s *Server) handleGateCheckOut(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	// TP §4.5/§4.6: the form-level time input, applied to every
	// visit_checked_out this POST mints (TP §4.8 row 3; §8 Q6) ⟦FIX8,
	// 2026-10-05⟧ EXCEPT where that person's own row carries a Time out
	// box — each visit is its own event, so each can carry its own time
	// (gate_rowtime.go). All-or-nothing: any refusal records nobody.
	tc, ok := s.captureTimeOrRefuse(w, r)
	if !ok {
		return
	}
	visitIDs := append([]string{}, r.Form["visit_ids"]...)
	if single := r.FormValue("visit_id"); single != "" {
		visitIDs = append(visitIDs, single)
	}
	at, warnings, rerr := s.rosterCheckoutTimes(r, visitIDs, tc)
	if rerr != nil {
		http.Error(w, rerr.msg, rerr.status)
		return
	}
	if _, err := domain.CheckOutVisitsAt(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, at, s.postAttribution()); err != nil {
		refuse(w, err)
		return
	}
	tc.Warnings = warnings
	http.Redirect(w, r, timeWarnRedirect(gateReturnPath(r), tc), http.StatusSeeOther)
}

// handleGateResume is row 31's recovery action: complete an interrupted
// check-in by re-running ONLY the arrival half (domain.ResumeCheckIn ==
// RecordExpectedArrival) — never Dispatch again, which would double-author
// the trip.
//
// It survives this redesign unchanged and it must: the crash window it
// recovers is between GateCheckIn's TWO Author calls, which is a property
// of the domain, not of the form. What the redesign removes is the OTHER
// source of half-finished vehicles — there is no longer any way to write
// twenty of a bus's twenty-six people, because nothing is written until the
// commit. So the MAY BE INCOMPLETE box keeps its exact original meaning and
// is never fired by a routine bus.
func (s *Server) handleGateResume(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	tripID := r.FormValue("trip_id")
	if tripID == "" {
		http.Error(w, "trip_id is required", http.StatusBadRequest)
		return
	}
	tc, ok := s.captureTimeOrRefuse(w, r) // TP §4.8 row 4: the arrival's OccurredAt
	if !ok {
		return
	}
	// ⟦orientation-doors, gap 1, 2026-09-11⟧ the SAME tick the accumulate
	// form's cross-site warning offers, extended to this door —
	// reports/witness/CROSSSITE-WARNING.md's declared gap 1. The warning
	// itself is rendered on /gate under "Incomplete check-ins" (gate.html,
	// incompleteRow.NeedsOrientation — web.go's incompleteCheckInRows) for
	// exactly the occupants THIS trip's resume would, right now, hold out.
	// D-88: a post with no orientation concept has nothing to tick.
	//
	// ⟦A3 / H-6, 2026-09-29⟧ ⟦was: "if err := s.completeOrientationsForTrip(
	// tripID, r.Form[\"add_orientation_person\"]); err != nil { refuse(w, err);
	// return }" HERE, BEFORE domain.ResumeCheckIn, with the note "the tick
	// must land before the door that reads OrientationHeld runs"⟧. True, but
	// it landed before the door's REFUSALS ran too: a resume refused as
	// already arrived (a stale second tab — L03 G-121) had already recorded
	// the ticked orientation, and entry-on-completion had already opened a
	// visit off that arrival, while the 409 read "recording it again enters
	// nobody" (FINDINGS H-6). The tick now rides INTO the door —
	// domain.ResumeCheckInCompleting: every refusal first, then the tick
	// (through the identical authoring path, CompleteOrientationsSkippingHeld,
	// scoped to the trip's own occupants as before), then the arrival that
	// reads it — and the whole resume is ONE transaction (store.Transact),
	// so nothing of a refused one survives. ⟦gate-orientation-block,
	// 2026-09-09⟧ this post's own flag rides the resume too: the crash
	// window is not a door around the orientation rule.
	var orientIDs []string
	if s.Cfg.HasOrientation() {
		orientIDs = r.Form["add_orientation_person"]
	}
	var arrival domain.Arrival
	err := s.Store.Transact("gate resume", func(st *store.Store) error {
		var err error
		arrival, err = domain.ResumeCheckInCompleting(st, s.Cfg.SiteID, s.Cfg.NodeEpoch, tripID, orientIDs, s.postAttribution(), tc.OccurredAtMS, tc.TimeSource, s.Cfg.HasOrientation())
		return err
	})
	if err != nil {
		// A ticked orientation's own refusal keeps the officer's sentence it
		// always had on this door (orientationRefusalCopy); every other
		// refusal is the domain's own text, verbatim, as before.
		var ref *domain.OrientationRefusal
		if errors.As(err, &ref) {
			refuse(w, errors.New(s.orientationRefusalCopy(ref)))
			return
		}
		refuse(w, err)
		return
	}
	http.Redirect(w, r, notEnteredRedirect(timeWarnRedirect("/gate", tc), arrival.NotEntered), http.StatusSeeOther)
}

// namesArePlural renders a refusal's subject so the sentence reads as
// English either way: "Pat Passenger is", "Pat Passenger and Chris Crew
// are", "A, B and C are". A refusal an operator has to parse twice at -30 is
// a refusal that gets clicked past.
func namesArePlural(names []string) string {
	switch len(names) {
	case 0:
		return "Nobody is"
	case 1:
		return names[0] + " is"
	case 2:
		return names[0] + " and " + names[1] + " are"
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1] + " are"
	}
}
