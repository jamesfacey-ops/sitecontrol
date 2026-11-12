// vehicle_amend.go — the officer-facing VEHICLE amendment surface (W7,
// item 9). One screen does all of it: correct a truck's number or role, join
// two duplicate vehicle records (same_as), un-join (clear = same_as).
//
// ⚠⚠ GATING IS `DR-W7-17`, AND THE AUTHORITY MATTERS MORE THAN THE MECHANISM.
// Both routes sit behind `s.requireLogin`. The three citations are separate
// and are not interchangeable:
//
//   - **[[D-63]]** is cited for WHO MAY USE IT — "the officer, from normal
//     screens". That is James's ruling and IT NAMES VEHICLE.
//   - **[[D-12]]** is cited for NOT BEHIND A DEVELOPER SWITCH. That is James's
//     ruling and IT NAMES VEHICLE.
//   - **The `requireLogin` MECHANISM is `DR-W7-17`**, W7's lock's own design
//     ruling, made on parity of built shape with /person/amend and
//     /company/amend.
//
// ⚠ IT CITES NEITHER [[D-51]] NOR [[D-46]]. Neither names vehicle, and D-51's
// whole existence is a ruling that this carry-across needs its OWN authority —
// so citing it here would be committing the defect at the site of its own
// prohibition. And the ERRATUM to D-63 applies: the reason vehicle needs its
// own authority is NOT that anyone previously carried a person ruling across
// without asking. DR-6 REFUSED to generalise and D-51 records that refusal as
// CORRECT; what broke was its factual premise. A REFUSAL IS NOT A COMMISSION.
//
// THERE IS NO SEPARATE MERGE AFFORDANCE. Vehicle `same_as` is an amendment
// like any other and is authored through this one POST.
//
// The POST appends — or refuses PRE-APPEND, since domain.AmendVehicle holds
// every refusal and a refused POST authors nothing — and then follows the
// house PRG pattern with the visible-failure confirmation contract: 303 back
// here with ?recorded=<site_id>.<epoch>.<seq>, and the GET computes the
// outcome AT RENDER TIME. Read-time truth, never POST-time cache.
//
// THE THREE ERROR CLASSES ARE KEPT APART (W7-15), and this is the third
// surface in a row that has had to say so. A two-way split over three classes
// does not merely lose detail: it puts the officer's MOST LIKELY mistake — a
// mistyped id, on a surface whose id is typed by hand — onto the internal-
// fault arm, which answers HTTP 500 and tells them to radio a post that is
// working perfectly. That is the "screen ordering a radio call with no
// recipient" shape, and it is what blocked W5d's commit.
//
// ⚠ NO ARM HERE RENDERS A RAW `err.Error()`. A refusal's text is AUTHORED COPY
// and belongs on the page; an internal error's text is not and does not.
package web

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"sitecontrol/internal/domain"
)

func (s *Server) registerVehicleAmendRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /vehicle/amend", s.requireLogin(s.handleVehicleAmendPage))
	mux.HandleFunc("POST /vehicle/amend", s.requireLogin(s.handleVehicleAmend))
	mux.HandleFunc("POST /vehicle/create", s.requireLogin(s.handleVehicleCreate))
}

// vehicleAmendFields lists the form's value fields, named after the payload
// keys — the wire spelling IS the form spelling, so nothing translates.
//
// ⚠ The set is EXACTLY {number, role, same_as} and nothing else (DR-W7-6).
// There is no `retired` control here and there never will be: DR-W7-8 asserts
// the field absent from the table, from the amendable set AND from every
// authoring surface, so its absence here is a criterion rather than an
// oversight. There is no `recorded_by` control either — attribution on an
// amendment is structural (DR-W7-7).
var vehicleAmendFields = []vehicleAmendField{
	{Key: "number", Label: "Unit or plate number"},
	{Key: "role", Label: "Role (for a truck with no number — e.g. the vac truck)"},
	{Key: "same_as", Label: "Same as (join into this vehicle id)"},
}

// vehicleAmendField carries the officer's typed Value back onto the form after
// a refusal, so the control itself is what preserves it rather than the
// refusal sentence quoting it back. A refusal that discards the typing makes
// the officer re-enter everything, and a body-text scan cannot see the
// difference because the sentence quotes the value.
type vehicleAmendField struct {
	Key   string
	Label string
	Value string
}

type vehicleAmendOutcomeRow struct {
	Fact             string
	Effective        bool
	UnMerge          bool
	WinnerPost       string
	WinnerAtThisPost bool
	SupersededPost   string
}

type vehicleAmendOutcomeView struct {
	// NotThisKind ⟦D22, fix2-fj 2026-10-04⟧: the key names an event of
	// another kind — see amendOutcomeView.NotThisKind.
	NotThisKind bool
	Key         string
	// Failed: the outcome could not be determined. The append DID succeed;
	// what failed is the read-back, and the officer must know the
	// confirmation is not vouching for the outcome.
	Failed bool
	Rows   []vehicleAmendOutcomeRow
}

// vehicleAmendPage is the render model.
type vehicleAmendPage struct {
	pageShell // top bar: post, alert banner (pageshell.go)

	SiteID string
	Cfg    any

	Fields []vehicleAmendField

	// Error is an AUTHORED fact-domain refusal sentence — never a raw error
	// string. The other two classes are their own fields so the page can say
	// WHICH of the three happened rather than flattening them: InputError is
	// an authored sentence too, and InternalError is a bare flag because a
	// real fault's text is exactly what must NOT reach the page.
	Error         string
	InputError    string
	InternalError bool

	VehicleID string
	Number    string
	Role      string
	SameAs    string
	Clear     string

	// Created is the confirmation of a vehicle_created, carrying the new id
	// so the officer can amend it without hunting for it.
	Created *domain.Vehicle
	// Duplicates are existing records whose number FOLDS to the same key as
	// the one just created — the prompt, not a merge. ⚠ Nothing here joins
	// anything: the fold key "is used for search ranking and for the prompt
	// in guard 2, and for nothing else. It never merges, never correlates,
	// never reaches a database column."
	Duplicates []domain.Vehicle

	// Vehicles is the picker's own list, so the id above can be chosen rather
	// than retyped — [[D-12]]: "a thing the system knows, selected rather
	// than retyped … identity typed twice is two identities."
	Vehicles []domain.Vehicle

	HasPeer       bool
	StandingOther string
	StandingTaker string

	Outcome *vehicleAmendOutcomeView
}

func (s *Server) buildVehicleAmendPage(refusal string, typed vehicleAmendPage, outcome *vehicleAmendOutcomeView) vehicleAmendPage {
	typed.pageShell = s.shell()
	hasPeer, other, taker := s.standingLine()
	typed.SiteID = s.Cfg.SiteID
	typed.Cfg = s.Cfg
	typed.Fields = make([]vehicleAmendField, len(vehicleAmendFields))
	copy(typed.Fields, vehicleAmendFields)
	for i := range typed.Fields {
		switch typed.Fields[i].Key {
		case "number":
			typed.Fields[i].Value = typed.Number
		case "role":
			typed.Fields[i].Value = typed.Role
		case "same_as":
			typed.Fields[i].Value = typed.SameAs
		}
	}
	typed.Error = refusal
	typed.HasPeer = hasPeer
	typed.StandingOther = other
	typed.StandingTaker = taker
	typed.Outcome = outcome
	// The picker list is best-effort: a surface that cannot list vehicles is
	// still a surface an officer can type an id into, and failing the whole
	// page over the list would take away the amendment as well as the picker.
	if vs, err := domain.Vehicles(s.Store); err == nil {
		typed.Vehicles = vs
	} else {
		log.Printf("vehicle amend: listing vehicles: %v", err)
	}
	return typed
}

// buildVehicleOutcomeView computes the confirmation block for one recorded
// key, at RENDER time.
func (s *Server) buildVehicleOutcomeView(recorded string) *vehicleAmendOutcomeView {
	if s.recordedNamesOtherKind(recorded, domain.EventVehicleAmended) {
		return &vehicleAmendOutcomeView{NotThisKind: true, Key: recorded}
	}
	k, ok := parseRecordedKey(recorded)
	if !ok {
		return &vehicleAmendOutcomeView{Failed: true}
	}
	outs, err := domain.Outcome(s.Store, k)
	if err != nil || len(outs) == 0 {
		return &vehicleAmendOutcomeView{Failed: true}
	}
	competitors, cerr := domain.Competitors(s.Store, k)
	if cerr != nil {
		// The outcome itself resolved; only the competitor enumeration did
		// not. Saying so is the honest answer — a confirmation that quietly
		// drops half of what it was asked to say is the silent-OK this
		// surface exists to refuse.
		log.Printf("vehicle amend: competitors %v: %v", k, cerr)
		return &vehicleAmendOutcomeView{Failed: true}
	}
	view := &vehicleAmendOutcomeView{}
	for _, o := range outs {
		row := vehicleAmendOutcomeRow{Fact: o.Fact, Effective: o.Status == domain.Effective}
		if row.Effective {
			if peers := competitors[o.Fact]; len(peers) > 0 {
				row.SupersededPost = s.supersededPostName(peers[len(peers)-1].SiteID)
			}
		} else {
			row.WinnerPost = s.peerDisplayName(o.Winner.SiteID)
			row.WinnerAtThisPost = o.Winner.SiteID == s.Cfg.SiteID
			row.UnMerge = o.Fact == "same_as" && o.Asserted == "un-merge"
		}
		view.Rows = append(view.Rows, row)
	}
	return view
}

func (s *Server) handleVehicleAmendPage(w http.ResponseWriter, r *http.Request) {
	var outcome *vehicleAmendOutcomeView
	if rec := noticeQuery(r).Get(recordedQueryKey); rec != "" {
		outcome = s.buildVehicleOutcomeView(rec)
	}
	page := vehicleAmendPage{VehicleID: strings.TrimSpace(r.URL.Query().Get("vehicle_id"))}
	if created := strings.TrimSpace(noticeQuery(r).Get(createdQueryKey)); created != "" {
		if v, err := domain.VehicleByID(s.Store, created); err == nil && v != nil {
			page.Created = v
			page.VehicleID = v.VehicleID
			if v.Number != nil {
				if dups, derr := domain.VehiclesFoldingTo(s.Store, *v.Number); derr == nil {
					for _, d := range dups {
						if d.VehicleID != v.VehicleID {
							page.Duplicates = append(page.Duplicates, d)
						}
					}
				}
			}
		}
	}
	s.renderPage(w, "vehicle amend", "vehicle_amend.html",
		s.buildVehicleAmendPage("", page, outcome))
}

// handleVehicleAmend authors one vehicle amendment. Every fact-domain refusal
// is domain.AmendVehicle's and is PRE-APPEND: a refused POST re-renders this
// page with the refusal and the officer's typing, an honest 409, and nothing
// in the log.
func (s *Server) handleVehicleAmend(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	typed := vehicleAmendPage{
		VehicleID: strings.TrimSpace(r.FormValue("vehicle_id")),
		Number:    strings.TrimSpace(r.FormValue("number")),
		Role:      strings.TrimSpace(r.FormValue("role")),
		SameAs:    strings.TrimSpace(r.FormValue("same_as")),
		Clear:     strings.TrimSpace(r.FormValue("clear")),
	}
	in := domain.VehicleAmendment{VehicleID: typed.VehicleID}
	// An empty form field is ABSENT — the officer did not touch it — never an
	// assertion of empty-string. Un-setting a field is `clear`, not an empty
	// submission, and that distinction is exactly DR-W7-15's subject.
	if typed.Number != "" {
		in.Number = &typed.Number
	}
	if typed.Role != "" {
		in.Role = &typed.Role
	}
	if typed.SameAs != "" {
		in.SameAs = &typed.SameAs
	}
	for _, c := range r.Form["clear"] {
		if c = strings.TrimSpace(c); c != "" {
			in.Clear = append(in.Clear, c)
		}
	}

	// No typed value here feeds the envelope: the amendment form types no
	// instant at all, and the amendment act's own envelope is default-
	// captured. That is the route's EXEMPT classification, and the row lands
	// in w2RouteTable in this same commit.
	env, err := domain.AmendVehicle(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, in, 0, "default")
	if err != nil {
		s.renderVehicleAmendError(w, err, typed)
		return
	}
	http.Redirect(w, r, sealNotices(fmt.Sprintf("/vehicle/amend?recorded=%s.%d.%d",
		env.SiteID, env.NodeEpoch, env.Seq)), http.StatusSeeOther)
}

// handleVehicleCreate authors one vehicle_created.
//
// ⚠⚠ THIS IS THE TYPING MOMENT, AND IT IS THE ONLY ONE. The placeholder
// refusal lives at domain.CreateVehicle, pre-append, and this route is how an
// officer reaches it. [[D-11]]: "'No number available' must be a distinct
// recorded state, NEVER a magic string in the vehicle field" — so the form
// offers a ROLE beside the number rather than inviting a stand-in, and a
// record naming neither is refused.
//
// ⚠ Its w2RouteTable reason is NOT a copy of the amendment routes' reason,
// and writing it as one would be a false clone: this route is reached from
// the check-in form, so a typed value DOES travel with it — which is why
// `vehicle_created` sits inside W2-8's co-mint guard and takes default
// capture. Its classification and that guard must agree, and the row says so.
func (s *Server) handleVehicleCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	typed := vehicleAmendPage{
		Number: strings.TrimSpace(r.FormValue("number")),
		Role:   strings.TrimSpace(r.FormValue("role")),
	}
	var number, role *string
	if typed.Number != "" {
		number = &typed.Number
	}
	if typed.Role != "" {
		role = &typed.Role
	}
	v, err := domain.CreateVehicle(s.Store, s.Cfg.SiteID, s.Cfg.NodeEpoch, number, role, 0, "default")
	if err != nil {
		s.renderVehicleAmendError(w, err, typed)
		return
	}
	http.Redirect(w, r, sealNotices("/vehicle/amend?created="+v.VehicleID), http.StatusSeeOther)
}

// renderVehicleAmendError is the THREE-WAY partition, in one place so the two
// authoring routes cannot drift into disagreeing about which class is which.
//
// ⚠ The classes are told apart BY TYPE, never by reading an error string —
// reading the string would be the same guess in a different place.
func (s *Server) renderVehicleAmendError(w http.ResponseWriter, err error, typed vehicleAmendPage) {
	var refusal *domain.VehicleAmendRefusal
	if errors.As(err, &refusal) {
		s.renderPageStatus(w, http.StatusConflict, "vehicle amend", "vehicle_amend.html",
			s.buildVehicleAmendPage(refusal.Reason, typed, nil))
		return
	}
	var createRefusal *domain.VehicleCreateRefusal
	if errors.As(err, &createRefusal) {
		s.renderPageStatus(w, http.StatusConflict, "vehicle amend", "vehicle_amend.html",
			s.buildVehicleAmendPage(createRefusal.Reason, typed, nil))
		return
	}
	// INPUT VALIDITY: the officer named a record this post cannot act on.
	// Same 409 as a refusal — nothing was recorded and the entry is the thing
	// to correct — carrying the domain's own authored sentence. ⚠ NOT the
	// internal arm: this post completed its work correctly, re-sending the
	// same id would fail identically forever, and answering a typo with 500
	// and a radio instruction is the miscarriage that blocked a commit once.
	var bad *domain.VehicleAmendInputError
	if errors.As(err, &bad) {
		typed.InputError = bad.Reason
		s.renderPageStatus(w, http.StatusConflict, "vehicle amend", "vehicle_amend.html",
			s.buildVehicleAmendPage("", typed, nil))
		return
	}
	// INTERNAL: the fault is LOGGED and the page shows authored copy, never an
	// err.Error() value — which is what makes its "try again … radio it
	// through" true, because it is now reached only by a real fault.
	log.Printf("vehicle amend: %v", err)
	typed.InternalError = true
	s.renderPageStatus(w, http.StatusInternalServerError, "vehicle amend", "vehicle_amend.html",
		s.buildVehicleAmendPage("", typed, nil))
}
