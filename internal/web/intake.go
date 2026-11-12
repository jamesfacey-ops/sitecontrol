// intake.go — PERSON INTAKE: the deliberate enrolment path, and the roster
// screen it lives on.
//
// James: "there will be an intake process for all new people that will
// include name first and last, company, work group and phone number", and
// "when we add a person it should be like the current site control form so
// when we type a name there is a dropdown of people that we select that will
// auto import all the relevant information."
//
// THE SPLIT THIS FILE EXISTS TO CREATE. Enrolment and check-in are two
// different acts and were previously one:
//
//	INTAKE (here)      — slow, deliberate, five fields, done once per person.
//	                     Company and trade are PICKED FROM THE EXISTING
//	                     REFERENCE DATA, so a crew shares one company row.
//	CHECK-IN (/gate)   — fast, at the window, at -30, with a bus outside.
//	                     Type a few characters, pick the person, next.
//	                     Everything about them is already on their roster row
//	                     and rides along structurally (buildArrivalVisits
//	                     snapshots it at authoring time).
//
// WHY THIS WAS NOT A SCHEMA CHANGE, AND WHY IT IS ONE NOW. As first built,
// nothing here added an event type, a projection, a column or a payload
// field: it authored company_created / trade_created / person_created
// through the SAME CreateCompanyRecordedBy / CreateTradeRecordedBy /
// CreatePersonRecordedBy calls the gate already uses. W5a (name parts) and
// W5b (the phone) changed that in the only way that keeps this form honest —
// by giving the facts it collects somewhere to live, in the payload and the
// projection, rather than by adding boxes over a schema that cannot hold
// them. store.SchemaVersion is still untouched HERE: the W5 window carries
// exactly one increment, W1's 9 → 10, and every sub-chunk rides it
// (reports/W5-SCHEMAVERSION-ADJUDICATION.md).
//
// THE PHONE NUMBER (W5b, item 2) — this was the note that said there was no
// such box and that its absence was deliberate. It arrived when the schema
// decision was made, exactly as that note said it would. Two things about it
// are worth reading before touching this file:
//
//   - It is REQUIRED HERE AND NOWHERE ELSE. [[D-52]] rules add-on-demand at
//     the gate EXEMPT, so "phone is enrolment-mandatory" is true of THIS
//     HANDLER and false of the system. The check therefore lives in
//     handleIntake and must never move down into domain.createPerson, which
//     both doors share.
//   - It is stored VERBATIM. No digit-stripping, no country code, no
//     truncation. The operator reads back what they typed.
//
// WHAT THE NAME IS. Person.Name is ONE string (refdata.go). Intake captures
// two boxes and composes the canonical "Last, First" — which is the exact
// display format James asked the person box to show, so the gate dropdown
// renders `Name + ", " + Company` and gets "Last, First, Company" for free.
// This is a CONVENTION INSIDE ONE FIELD, not a structural split; see
// composeName below and the report's §5.1 for what it does and does not buy.
package web

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// newRef is the sentinel a company/trade <select> carries for "not
// listed — I typed it in the box below". It is deliberately not "" : ""
// is the empty prompt (nothing chosen), and those two must never collapse
// into each other, or an operator who chose nothing would silently mint a
// company from an empty text box.
const newRef = "__new__"

func (s *Server) registerIntakeRoutes(mux *http.ServeMux) {
	// GET stays UNGATED, exactly as GET /gate is (C11, design §12:
	// accountability, not confidentiality — no reason to gate a read).
	mux.HandleFunc("GET /roster", s.handleRosterPage)
	// POST authors company/trade/person rows, so it is gated exactly as
	// POST /gate/checkin is (C11's requireLogin). D-5 retired requireOfficer:
	// attribution is by POST, not person, so there is no picker to withhold
	// a first submission behind — see auth.go's postAttribution.
	mux.HandleFunc("POST /roster/intake", s.requireLogin(s.handleIntake))
}

// --- view types ---

// intakeTrade / intakeCompany are the reference data offered by the two
// pickers.
//
// ⚠ W8: THE TRADE LIST IS SITE-WIDE AND NO LONGER HANGS OFF A COMPANY. A
// trade is not scoped to a company ⟦DR-W8-2⟧, so there is no per-company
// <optgroup>, no cross-company pairing to refuse, and no "belongs to a
// different company" arm. What replaced them is a plainer thing: one flat
// list of this site's trades, and a person's company and trade are two
// independent defaults on their file.
type intakeTrade struct {
	ID   string
	Name string
	// Origin ⟦F-SETUP-1, 2026-10-03⟧: the display name of the post that
	// added this row, filled ONLY when another trade row carries the same
	// name — two same-named options are otherwise indistinguishable on the
	// picker. Empty on a uniquely named row.
	Origin string
}

type intakeCompany struct {
	ID   string
	Name string
	// Headcount is how many people are already enrolled under this company.
	// It is an ORDERING/CONFIDENCE aid on the picker, not a filter: the
	// company a crew of forty already uses should be easy to spot next to a
	// near-duplicate somebody minted at the window last week.
	Headcount int
	// Origin ⟦F-SETUP-1⟧: as intakeTrade.Origin — the post that added the
	// row, shown only when the name is shared by more than one row.
	Origin string
}

// rosterEntry is one enrolled person as this screen shows them.
type rosterEntry struct {
	PersonID string
	// Display is Person.Name verbatim — for intake-created people that is
	// already "Last, First". It is never recomposed here: the roster row is
	// the authority on how a person's name reads.
	Display string
	Company string
	Trade   string
	// EnrolledAt is person.site_id — the site that AUTHORED the row. This is
	// structural (store.Envelope.SiteID, D-5: "Origin is the authoring site,
	// structurally... never a payload"), so it cannot be wrong and needed no
	// new field. It is what makes a person enrolled at the Pad visible as
	// the anomaly it is.
	EnrolledAt string
	// NeedsCompany: no company on file. Derived, never stored — there is no
	// "incomplete" flag anywhere, and there must not be one: a stored flag
	// can disagree with the row it describes, a derived one cannot.
	NeedsCompany bool
	// Retired ⟦R-118 / [[D-21.5]], 2026-10-03⟧: the record has been retired
	// from the pickers. The roster is the RECORD and still lists them —
	// "never delete their history" — marked, so the retirement is visible
	// and reversible (Clear retired on /person/amend) rather than silent.
	Retired bool
	// Unconfirmed: this record was authored at a post that is NOT the site's
	// entrance, and nothing has yet linked it to a record from the entrance.
	//
	// James: "if it sees someone new it should flag them for sure — not make
	// a new entry." THE FLAG IS ON THE RECORD, NEVER ON THE PERSON. Someone
	// standing at the window at -30 is present, goes on the roster and goes
	// on the muster, full stop — omitting a present person is the dangerous
	// direction and the one thing this system exists to prevent. What is
	// provisional is their IDENTITY: almost certainly they are already
	// enrolled at the entrance and the link is behind.
	//
	// DERIVED FROM TWO THINGS THAT ALREADY EXIST, so it needed no new field,
	// no new event and no schema change:
	//   1. person.site_id — the authoring post, structural and unforgeable
	//      (D-5), compared against cfg.entrance_site_id;
	//   2. the composed `same_as` (W3, item 4) — a merge touching this id
	//      in either direction means a human has already reconciled it.
	// Clearing the flag is therefore exactly the merge mechanism (a
	// same_as amendment on /person/amend), not a second concept beside it.
	Unconfirmed bool
}

// intakeForm is the sticky form state. A refusal must never cost the
// operator their typing — the same rule the gate's vehicle form follows.
type intakeForm struct {
	Last  string
	First string
	// Phone is the personal phone number (W5b, item 2). It is REQUIRED on
	// this form and on no other surface — see handleIntake's refusal and
	// D-52. It is carried and re-rendered VERBATIM: what the officer typed
	// comes back into the box byte for byte, including spaces, dashes,
	// parentheses and extensions.
	Phone      string
	CompanyID  string
	NewCompany string
	TradeID    string
	NewTrade   string
	// SameName is the explicit affirmation that a person with this exact
	// name is a DIFFERENT human from the one already on the roster. See
	// handleIntake's duplicate check for why intake refuses by default
	// where the gate does not.
	SameName bool
}

type rosterData struct {
	SiteID string
	Cfg    *config.Config

	Banner    []deadline.Alert
	BannerErr string

	// EntranceKnown / IsEntrance / EntranceSiteID: whether this deployment
	// has been told which post is the only entrance, and whether this is it.
	// Unset is rendered as "nobody has said", never as "this is not it".
	EntranceKnown  bool
	IsEntrance     bool
	EntranceSiteID string

	// Sync is the REAL replication state, from the same snapshot
	// /debug/sync-status renders — not a signal invented for this page. A
	// post with no engine wired reports Available=false, which the shared
	// bar renders as "cannot tell" rather than as "healthy".
	Sync syncStatusView

	// HasOrientation / Post ⟦R-12, 2026-09-10⟧: this post's OWN config flag
	// and display name. At an orientation post this form is the ORIENTATION
	// FORM — enrolment records the completion in the same submission
	// (handleIntake) — and the page says so; at a road post neither word
	// appears. Never a peer's flag.
	HasOrientation bool
	Post           string
	// Candidates ⟦R-12 + R-10⟧: on the duplicate refusal at an orientation
	// post, the existing records the typed name matched, each with the offer
	// to record THIS post's orientation against it instead of enrolling
	// again — the officer confirms a NAMED person (company and origin site
	// shown) and the tick lands on the preexisting record through the
	// ordinary marking route. Nil on every other render and at every road
	// post, where that route is not registered.
	Candidates []intakeCandidate

	Companies []intakeCompany
	// Trades is this SITE's trade list, flat and sorted by name — see
	// intakeTrade for why it is not nested under Companies any more.
	Trades []intakeTrade
	People []rosterEntry
	// NeedsCompletion is the derived queue: everybody with no company on
	// file. This is the other half of the bus-versus-completeness answer —
	// the window stays fast, and the incompleteness it creates is VISIBLE
	// and finite rather than silent.
	NeedsCompletion []rosterEntry
	// Unconfirmed is the identity-reconciliation queue — records authored
	// away from the entrance that nothing has linked to an entrance record
	// yet. See rosterEntry.Unconfirmed.
	Unconfirmed []rosterEntry

	Total         int
	EnrolledHere  int
	FromElsewhere int

	Form   intakeForm
	Error  string
	Notice string
	// OfferSameName renders the "different person, same name" affirmation,
	// and ONLY the refusal that needs it sets it. A permanently visible
	// "do it anyway" tick is a click-through trainer, and this project has
	// already ruled that guarding everything equally is guarding nothing.
	OfferSameName bool
	// IntakeOpen ⟦R-004x / M-15, 2026-10-03⟧: at a post that is not the
	// entrance the form sits inside a closed <details>; a REFUSAL re-renders
	// it OPEN, so the sticky values and the "different person, same name"
	// tick the refusal just told the officer to use are on screen rather
	// than behind a collapsed bar (L04 R-004x, measured twice: the banner
	// said "tick …" and the box was not visible). The plain page load and
	// the post-enrolment landing stay closed — the demotion is unchanged.
	IntakeOpen bool
}

// intakeCandidate is one existing record offered on the duplicate refusal
// (R-10: confirm before adopting — the name, the company that tells two
// same-name people apart, and where they were enrolled). HoldsHere: the
// tick is not offered because this post's orientation is already recorded
// for them; the row says so instead.
type intakeCandidate struct {
	PersonID  string
	Name      string
	Company   string
	Site      string
	HoldsHere bool
}

// --- name composition ---

// composeName turns two boxes into the ONE string Person.Name holds, in the
// canonical "Last, First" order James specified for the person box.
//
// THIS IS A CONVENTION, NOT A SCHEMA SPLIT, and the difference matters:
//   - it buys the display format, surname-order sorting (because "Last,
//     First" sorts by surname as a plain string compare), and a comma that
//     makes the two halves separable by eye;
//   - it does NOT buy a queryable surname, and it does not apply to rows
//     created before intake existed or created at the window, which stay
//     "First Last". The roster will read mixed until those are completed.
//
// Whitespace is collapsed so "  Van  der Berg " and "Van der Berg" are the
// same person to the gate's exact-match resolver, which compares against
// Person.Name after trimming.
func composeName(last, first string) string {
	last, first = collapseSpaces(last), collapseSpaces(first)
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

func collapseSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

// typedNameParts records the two boxes composeName was just given as the
// created-state parts that travel beside the composed name (W5a, item 1).
// It is the SAME two values, collapsed the same way — the direction is boxes
// to Name and boxes to parts, never Name back to parts (W5a-8).
//
// It sits here, beside composeName and taking plain strings, because this is
// where "what the two boxes become" belongs: one place shows both things
// they become, and neither is derived from the other.
//
// Position (D-21.4) is deliberately absent: intake has no position box, so
// an enrolment leaves it unset rather than inventing one.
func typedNameParts(last, first string) domain.PersonDetails {
	last, first = collapseSpaces(last), collapseSpaces(first)
	return domain.PersonDetails{LastName: &last, FirstName: &first}
}

// --- the page ---

func (s *Server) handleRosterPage(w http.ResponseWriter, r *http.Request) {
	s.renderRoster(w, r, http.StatusOK, intakeForm{}, "", noticeQuery(r).Get(enrolledQueryKey), false)
}

// renderRoster builds and writes the whole roster/intake page. Every path
// that shows it goes through here — the page load, a refusal, and the
// post-enrolment landing — so a refusal cannot render a different page from
// the one the operator was working in.
func (s *Server) renderRoster(w http.ResponseWriter, r *http.Request, status int, form intakeForm, refusal, enrolledID string, offerSameName bool) {
	s.renderRosterWith(w, r, status, form, refusal, enrolledID, offerSameName, nil)
}

// renderRosterWith is renderRoster plus candidates ⟦R-12 + R-10⟧: the
// duplicate refusal's tick offer (intakeCandidates), nil on every other
// render. A sibling rather than a widened signature so the existing callers
// — and the test that drives renderRoster directly — are untouched.
func (s *Server) renderRosterWith(w http.ResponseWriter, r *http.Request, status int, form intakeForm, refusal, enrolledID string, offerSameName bool, candidates []intakeCandidate) {
	persons, err := domain.Persons(s.Store)
	if err != nil {
		s.gateFail(w, "persons", err)
		return
	}
	companies, err := domain.Companies(s.Store)
	if err != nil {
		s.gateFail(w, "companies", err)
		return
	}
	trades, err := domain.Trades(s.Store)
	if err != nil {
		s.gateFail(w, "trades", err)
		return
	}
	// BOTH DIRECTIONS. A reconciliation may name the local record as the
	// merged-away duplicate (the usual shape) or as the record another was
	// merged INTO. Either way a human has decided, and the flag has done
	// its job.
	//
	// W3 (item 4, P-3 §6): the reconciliation signal is the COMPOSED
	// `same_as` — an officer's authored, reversible merge on the amendment
	// surface — read from the same composed persons this page already
	// holds. The shared legacy reference-merge table was deliberately NOT
	// consulted: person merging through it was superseded at W3 and the
	// table itself is deleted at W8, per P-3 §5's schedule, and deriving
	// this flag from two mechanisms would let them disagree. Clearing the flag is therefore exactly the
	// merge amendment — still not a second concept beside it.
	reconciled := make(map[string]bool, len(persons))
	for _, p := range persons {
		if p.SameAs != nil {
			reconciled[p.PersonID] = true
			reconciled[*p.SameAs] = true
		}
	}

	companyName := make(map[string]string, len(companies))
	for _, c := range companies {
		companyName[c.CompanyID] = c.Name
	}
	tradeName := make(map[string]string, len(trades))
	for _, wg := range trades {
		tradeName[wg.TradeID] = wg.Name
	}
	headcount := make(map[string]int, len(companies))
	for _, p := range persons {
		if p.CompanyID != nil {
			headcount[*p.CompanyID]++
		}
	}

	data := rosterData{
		SiteID:         s.Cfg.SiteID,
		Cfg:            s.Cfg,
		EntranceKnown:  s.Cfg.EntranceKnown(),
		IsEntrance:     s.Cfg.IsEntrance(),
		EntranceSiteID: s.Cfg.EntranceSiteID,
		Sync:           s.buildSyncSection(),
		HasOrientation: s.Cfg.HasOrientation(), // ⟦R-12⟧ this post's own flag
		Post:           s.Cfg.PostName(),
		Candidates:     candidates,
		Form:           form,
		Error:          refusal,
		// OfferSameName is `|| form.SameName` and the second half is NOT
		// decoration — without it a refusal fired AFTER the officer ticked
		// the box re-renders a page with no same_name control at all, and
		// the browser's next POST cannot carry the tick. The officer is then
		// told a second time that the person "is already on the roster … do
		// not enrol them again", having already said this is a different
		// human. PRODUCED before this line existed: duplicate refusal →
		// affirm → phone refusal (W5b, and the first refusal ever to sit
		// BELOW the duplicate check) → the affirmation is gone → the
		// duplicate refusal again. See W5-POSTLOCK-AMENDMENTS.md `A-3`.
		//
		// It does NOT reintroduce the permanently visible "yes, do it
		// anyway" tick the template's own comment forbids: the control still
		// appears only on the refusal that needs it OR on a page where the
		// officer has already ticked it themselves, re-rendered CHECKED so
		// they can see — and revoke — their own answer. A hidden carrier
		// would preserve the affirmation invisibly, which on this form is
		// the worse half of the trade.
		OfferSameName: offerSameName || form.SameName,
		IntakeOpen:    refusal != "",
		Total:         len(persons),
	}
	data.Banner, data.BannerErr = s.banner()

	// The company picker, sorted by name. Person ids and company ids are
	// crypto/rand hex, so the stored order is effectively random — the same
	// defect the gate roster and the trips picker were both fixed for. A
	// picker that lists a fixed prefix of an unsorted set shows a RANDOM
	// subset, which is how an operator concludes the system lost a company.
	// The trade picker: ONE flat, name-sorted, site-wide list. Sorted for
	// the same reason the company picker is — trade ids are crypto/rand hex,
	// so a list in stored order is a random one, which is how an operator
	// concludes the system lost a trade.
	// ⟦F-SETUP-1, 2026-10-03⟧ SAME NAME, MORE THAN ONE ROW: label each such
	// option with the post that added it, under the ONE fold each register
	// uses (domain.SameTradeName / SameCompanyName). A reseeded cluster has
	// every trade and company name twice (an ice-pad and an npf id), and two
	// identical options give the officer nothing to choose BY; origin is the
	// one fact that separates them. No row is dropped and nothing is merged
	// — the pair may be two companies — so this is labelling, never a filter.
	for i, wg := range trades {
		t := intakeTrade{ID: wg.TradeID, Name: wg.Name}
		for j, other := range trades {
			if i != j && domain.SameTradeName(wg.Name, other.Name) {
				t.Origin = s.peerDisplayName(wg.SiteID)
				break
			}
		}
		data.Trades = append(data.Trades, t)
	}
	sort.Slice(data.Trades, func(i, j int) bool {
		if data.Trades[i].Name != data.Trades[j].Name {
			return data.Trades[i].Name < data.Trades[j].Name
		}
		return data.Trades[i].ID < data.Trades[j].ID
	})
	for i, c := range companies {
		ic := intakeCompany{ID: c.CompanyID, Name: c.Name, Headcount: headcount[c.CompanyID]}
		for j, other := range companies {
			if i != j && domain.SameCompanyName(c.Name, other.Name) {
				ic.Origin = s.peerDisplayName(c.SiteID)
				break
			}
		}
		data.Companies = append(data.Companies, ic)
	}
	sort.Slice(data.Companies, func(i, j int) bool {
		if data.Companies[i].Name != data.Companies[j].Name {
			return data.Companies[i].Name < data.Companies[j].Name
		}
		return data.Companies[i].ID < data.Companies[j].ID
	})

	for _, p := range persons {
		e := rosterEntry{PersonID: p.PersonID, Display: p.Name, EnrolledAt: p.SiteID, Retired: p.IsRetired()}
		if p.CompanyID != nil {
			e.Company = companyName[*p.CompanyID]
			if e.Company == "" {
				// Company row not replicated here yet: show the id rather
				// than a blank, the same rule buildMuster follows for an
				// unreplicated person row — never drop the fact.
				e.Company = *p.CompanyID
			}
		} else {
			e.NeedsCompany = true
		}
		if p.TradeID != nil {
			e.Trade = tradeName[*p.TradeID]
			if e.Trade == "" {
				e.Trade = *p.TradeID
			}
		}
		// The flag, derived. Note what it is NOT keyed on: it is not "this
		// post authored it" but "a post that is not the entrance authored
		// it" — so a record made at the Pad is flagged at BOTH posts once it
		// replicates, which is the point. Whoever does the reconciling is
		// most likely sitting at the entrance.
		e.Unconfirmed = s.Cfg.EntranceKnown() && p.SiteID != s.Cfg.EntranceSiteID && !reconciled[p.PersonID]
		if p.SiteID == s.Cfg.SiteID {
			data.EnrolledHere++
		}
		data.People = append(data.People, e)
		if e.NeedsCompany {
			data.NeedsCompletion = append(data.NeedsCompletion, e)
		}
		if e.Unconfirmed {
			data.Unconfirmed = append(data.Unconfirmed, e)
		}
		if p.PersonID == enrolledID {
			data.Notice = enrolledNotice(e)
			if s.Cfg.HasOrientation() {
				data.Notice += s.enrolledOrientationSentence(p.PersonID)
			}
		}
	}
	sort.Slice(data.People, func(i, j int) bool { return data.People[i].Display < data.People[j].Display })
	sort.Slice(data.NeedsCompletion, func(i, j int) bool {
		return data.NeedsCompletion[i].Display < data.NeedsCompletion[j].Display
	})
	sort.Slice(data.Unconfirmed, func(i, j int) bool {
		return data.Unconfirmed[i].Display < data.Unconfirmed[j].Display
	})

	data.FromElsewhere = data.Total - data.EnrolledHere

	s.renderPageStatus(w, status, "roster", "intake.html", data)
}

// enrolledNotice echoes the enrolment back in the past tense, naming exactly
// what was written (UI brief: "a button says exactly what happens, and the
// confirmation echoes it in the past tense"). It names the company because
// the company is the half an operator is most likely to have got wrong, and
// it is what the muster groups by.
func enrolledNotice(e rosterEntry) string {
	out := "Enrolled " + e.Display
	if e.Company != "" {
		out += " — " + e.Company
	}
	if e.Trade != "" {
		out += " / " + e.Trade
	}
	out += "."
	return out
}

// enrolledOrientationSentence ⟦R-12⟧ is the past-tense echo of the SECOND
// event an orientation post's enrolment authors, read LIVE off the derived
// table rather than assumed: a store fault between the two Author calls
// (handleIntake) leaves a person on the roster with no orientation row, and
// the confirmation must say that — with the next action — rather than
// "recorded".
func (s *Server) enrolledOrientationSentence(personID string) string {
	post := s.Cfg.PostName()
	held, err := domain.OrientationHeld(s.Store, personID, s.Cfg.SiteID)
	if err == nil && held != nil {
		return " " + post + "'s orientation recorded."
	}
	return " " + post + "'s orientation is NOT recorded — record it on the orientation page before they are checked in."
}

// intakeCandidates ⟦R-12 + R-10⟧ turns the duplicate matches into the
// per-candidate tick offer, at an orientation post ONLY: elsewhere the route
// the offer posts to is not registered (web.go), so this returns nil and the
// template renders nothing. A candidate already holding this post's
// orientation is marked rather than offered; if the holders cannot be read
// the offer is still made and the marking edge's own AlreadyHeld refusal
// answers it — a refusal, never a false write.
func (s *Server) intakeCandidates(byName []personMatch, companies []domain.Company) []intakeCandidate {
	if !s.Cfg.HasOrientation() {
		return nil
	}
	companyName := make(map[string]string, len(companies))
	for _, c := range companies {
		companyName[c.CompanyID] = c.Name
	}
	holds := map[string]bool{}
	// ⟦R4, James, 2026-10-04⟧ through the merge: a candidate whose record
	// inherits a merged-away record's completion already holds it.
	merge, err := domain.LoadPersonMergeMap(s.Store)
	if err == nil {
		if held, err := domain.OrientationHoldersAt(s.Store, merge, s.Cfg.SiteID); err == nil {
			for id := range held {
				holds[id] = true
			}
		}
	}
	out := make([]intakeCandidate, 0, len(byName))
	for _, m := range byName {
		c := intakeCandidate{PersonID: m.Person.PersonID, Name: m.Person.Name, Site: m.Person.SiteID, HoldsHere: holds[merge.Resolve(m.Person.PersonID)]}
		if m.Person.CompanyID != nil {
			c.Company = companyName[*m.Person.CompanyID]
		}
		out = append(out, c)
	}
	return out
}

// --- the enrolment ---

func (s *Server) handleIntake(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderRoster(w, r, http.StatusBadRequest, intakeForm{}, "That form did not arrive intact. Nothing was recorded — enter the person again.", "", false)
		return
	}
	form := intakeForm{
		Last:       r.FormValue("last_name"),
		First:      r.FormValue("first_name"),
		Phone:      r.FormValue("phone"),
		CompanyID:  r.FormValue("company_id"),
		NewCompany: r.FormValue("new_company"),
		TradeID:    r.FormValue("trade_id"),
		NewTrade:   r.FormValue("new_trade"),
		SameName:   r.FormValue("same_name") != "",
	}
	refuse := func(msg string) {
		s.renderRoster(w, r, http.StatusConflict, form, msg, "", false)
	}
	// refuseDuplicate is refuse plus the one affirmation this form offers —
	// and, at an orientation post, the tick offer against each candidate
	// ⟦R-12 + R-10⟧.
	refuseDuplicate := func(msg string, cands []intakeCandidate) {
		s.renderRosterWith(w, r, http.StatusConflict, form, msg, "", true, cands)
	}

	// EVERYTHING IS VALIDATED BEFORE ANYTHING IS AUTHORED. A refusal must
	// leave no trace: half an intake — a company row minted for a person who
	// was then refused — is a duplicate company nobody asked for, on the
	// table the muster groups by.
	name := composeName(form.Last, form.First)
	if collapseSpaces(form.Last) == "" || collapseSpaces(form.First) == "" {
		refuse("Both the last name and the first name are needed. Intake is the one place a person's details are captured, and the muster is read aloud from them — a half-entered name is read aloud too.")
		return
	}
	// James, 2026-10-03: the roster enrolment form takes the same
	// 120-character name limit W3 put on the gate (maxNewPersonNameRunes),
	// in the same words (overlongNameSentence), measured on the name as it
	// would be STORED ("Last, First"). Server-side, before anything authors,
	// so it holds identically with scripting on or off (this form has no
	// script); the typed form is re-rendered for the officer to shorten.
	if n := utf8.RuneCountInString(name); n > maxNewPersonNameRunes {
		refuse("The " + strings.TrimPrefix(overlongNameSentence(n, "the form below"), "the ") + ".")
		return
	}

	companies, err := domain.Companies(s.Store)
	if err != nil {
		s.gateFail(w, "companies", err)
		return
	}
	trades, err := domain.Trades(s.Store)
	if err != nil {
		s.gateFail(w, "trades", err)
		return
	}
	persons, err := domain.Persons(s.Store)
	if err != nil {
		s.gateFail(w, "persons", err)
		return
	}

	// COMPANY. Required here and optional at the window, and that asymmetry
	// IS the answer to the bus problem: the deliberate lane insists, the
	// fast lane does not, and the derived completion queue joins them up. A
	// person with no company cannot be grouped on the muster, and the muster
	// is read aloud during an evacuation.
	newCompany := collapseSpaces(form.NewCompany)
	switch {
	case form.CompanyID == "":
		refuse("Choose a company, or add one. A person with no company on file cannot be grouped on the muster, and the muster is what gets read aloud during an evacuation.")
		return
	case form.CompanyID == newRef && newCompany == "":
		refuse("Type the new company's name, or pick one from the list.")
		return
	case form.CompanyID == newRef:
		for _, c := range companies {
			if domain.SameCompanyName(c.Name, newCompany) {
				refuse(fmt.Sprintf("%q is already on the list — pick it there rather than adding a second one. Two company rows with the same name split one crew into two groups on the muster.", c.Name))
				return
			}
		}
	default:
		found := false
		for _, c := range companies {
			if c.CompanyID == form.CompanyID {
				found = true
				break
			}
		}
		if !found {
			refuse("That company is no longer on the list — it may have arrived from the other post while this form was open. Reload the roster and pick again. Nothing was recorded.")
			return
		}
	}

	// TRADE. Optional: a person with no trade on file is ordinary, and the
	// muster labels that bucket "(no trade recorded)" rather than losing the
	// person.
	//
	// ⚠ W8 DELETED THE CROSS-COMPANY ARM, and the deletion is a consequence
	// of the model rather than a relaxation: a trade is not scoped to a
	// company ⟦DR-W8-2⟧, so "that work group belongs to a different company"
	// is not a thing that can be true. The duplicate-name arm survives and is
	// now SITE-WIDE, matching the register's own uniqueness rule ⟦DR-W8-11⟧;
	// the unknown-id arm survives unchanged.
	newTrade := collapseSpaces(form.NewTrade)
	switch {
	case form.TradeID == newRef && newTrade == "":
		refuse("Type the new trade's name, or choose one from the list, or leave the trade empty.")
		return
	case form.TradeID == newRef:
		// ⟦R-087⟧ through domain.SameTradeName — the fold the register's own
		// refusal (CreateTrade) uses — so this pre-check and that refusal
		// fire on exactly the same pairs; a pair the pre-check let through
		// and the register refused used to surface as a 500 here.
		for _, wg := range trades {
			if domain.SameTradeName(wg.Name, newTrade) {
				refuse(fmt.Sprintf("%q is already on this site's trade list — choose it from the list rather than adding a second one.", wg.Name))
				return
			}
		}
	case form.TradeID != "":
		found := false
		for _, wg := range trades {
			if wg.TradeID == form.TradeID {
				found = true
				break
			}
		}
		if !found {
			refuse("That trade is no longer on the list. Reload the roster and pick again. Nothing was recorded.")
			return
		}
	}

	// DUPLICATE IDENTITY. The gate deliberately allows an off-roster name
	// through, because at the window over-reporting is the safe direction.
	// INTAKE IS NOT THE WINDOW. Here the likeliest cause of an unrecognised
	// name is that the person is already enrolled at the other post and the
	// link is behind — and a second identity for one human is the one error
	// the alias machinery cannot repair, because MusterLive alias-resolves
	// company and trade ids and NEVER person ids (muster.go). So intake
	// refuses, names the existing rows and their origin sites, and lets the
	// operator override explicitly when it really is a second person with
	// the same name — which is not exotic on a work crew.
	//
	// THE MATCH IS THE ONE SHARED RULE, personMatchCandidates
	// (person_match.go: samePerson: picker.go's sameName plus the deterministic
	// folds, each with its reason and its misses stated there) ⟦was:
	// TrimSpace + EqualFold here, the same rule less the inner-whitespace
	// collapse, which composeName has already done to both parts — so for
	// this door the change is what the refusal SAYS, not when it fires⟧.
	// The refusal now names EVERY candidate with their company and origin
	// site, and says what the phone adds: the same number leans "one
	// person", a different number leans "two people". The phone is a signal
	// inside a name match and never a key — a number shared under a
	// different name is not a duplicate (crews share dispatch lines) and
	// raises nothing here. Nothing decides identity: the officer reads the
	// candidates and either stops or affirms.
	if !form.SameName {
		if byName := personMatchCandidates(persons, name, form.Phone); len(byName) > 0 {
			refuseDuplicate(intakeDuplicateRefusal(byName, companies, s.Cfg.SiteName), s.intakeCandidates(byName, companies))
			return
		}
	}

	// PHONE (W5b, item 2). Required HERE and nowhere else, and the asymmetry
	// is [[D-52]]'s ruling rather than an oversight: this is the deliberate
	// lane, where the roster is built in advance and there is time to ask.
	// The gate's add-on-demand door is EXEMPT — an officer with a person
	// standing in front of them is not sent to find a phone number — so the
	// check lives at this edge and NOT in domain.createPerson, which both
	// doors share and which would therefore break the busiest path in the
	// system while passing every test on this one.
	//
	// IT IS LAST AMONG THE REFUSALS ON PURPOSE, and the reason is the
	// standing rule three screens above rather than taste. Every check in
	// this handler is above the authoring line; this one is the LAST thing
	// before it, so the boundary the rule protects is visible in one glance
	// and moving this block one step further down is exactly the mutant the
	// rule exists to forbid. Ordering it last also leaves every refusal this
	// form already gave — no company, an unlisted work group, a duplicate
	// name — firing exactly as it did before W5b: a new requirement should
	// not silently change which fault an officer is told about first.
	//
	// "Blank" is judged on the COLLAPSED value, so a box holding only spaces
	// is refused rather than enrolled with an unusable number. What is
	// STORED is the RAW box, verbatim (W5b-0) — the collapse decides whether
	// the officer typed anything, never what gets recorded.
	//
	// Nullable and mandatory are both correct at once: the column stays
	// nullable because a wire-received person with no phone must fold clean
	// (W3-2, and refusing there would fork the two posts' logs permanently),
	// and mandatory here because this is the door where a number can actually
	// be obtained. The result is honest gaps, clustered on the people who
	// arrived at a gate, rather than a fabricated placeholder.
	if collapseSpaces(form.Phone) == "" {
		refuse("A phone number is needed. Intake is the deliberate lane — the roster is built here in advance, and a number is the only way to reach somebody who has not turned up for a muster. Anyone added at the window in a hurry is not asked for one, so this form is where the roster gets them.")
		return
	}

	// --- authoring starts here; everything above refused without writing ---
	// D-5: recorded_by is this SITE CONTROL POST, from config — never a
	// named person, never nil.
	recordedBy := s.postAttribution()

	// ⟦FIX17b, sc-stress F10⟧ ONE TRANSACTION for the whole enrolment — a
	// new company, a new trade, the person and (at an orientation post) this
	// post's completion land together or not at all. ⟦was: up to four
	// separate commits; on a full disk the person landed and the completion
	// did not ("enrolled but … orientation was NOT recorded")⟧
	var p domain.Person
	failed := "create person"
	err = s.Store.Transact("intake", func(st *store.Store) error {
		companyID := form.CompanyID
		if companyID == newRef {
			c, err := domain.CreateCompanyRecordedBy(st, s.Cfg.SiteID, s.Cfg.NodeEpoch, newCompany, recordedBy, 0, "default")
			if err != nil {
				failed = "create company"
				return err
			}
			companyID = c.CompanyID
		}

		var tradeID *string
		if form.TradeID == newRef {
			wg, err := domain.CreateTradeRecordedBy(st, s.Cfg.SiteID, s.Cfg.NodeEpoch, newTrade, recordedBy, 0, "default")
			if err != nil {
				failed = "create trade"
				return err
			}
			tradeID = &wg.TradeID
		} else if form.TradeID != "" {
			id := form.TradeID
			tradeID = &id
		}

		// W5a (item 1): intake KEEPS the two parts it has always collected. Both
		// are non-empty here — the half-entered-name refusal above is what makes
		// that true, and that refusal is the ONLY place requiredness lives.
		//
		// The parts may later diverge from `name` by amendment, lawfully and
		// silently (D-48) — nothing here or downstream detects, warns about or
		// repairs that. `name` remains what every read path answers with, and
		// what every person-name sort orders by.
		//
		// W5b (item 2): the phone travels beside them, VERBATIM — the raw box,
		// not a collapsed or normalised copy of it. It is non-empty here because
		// the refusal above is what makes that true, and that refusal is the ONLY
		// place the requirement lives (D-52).
		details := typedNameParts(form.Last, form.First)
		phone := form.Phone
		details.Phone = &phone
		var err error
		p, err = domain.CreatePersonWithDetails(st, s.Cfg.SiteID, s.Cfg.NodeEpoch, name, &companyID, tradeID, recordedBy,
			details, 0, "default")
		if err != nil {
			return err
		}

		// ⟦R-12, 2026-09-10⟧ THE NEW-PERSON FORM IS THE ORIENTATION FORM. James:
		// "no one will be placed in the system without the oriantation. the site
		// oriantations should just be a check box for the current site to add to
		// a preexisting person in the overall system". At a post that administers
		// an orientation, enrolment records THIS post's completion in the same
		// submission — the person is placed in the system BY being oriented here,
		// and no other door creates one at such a post (the gate's add-on-demand
		// slot refuses, checkin.go). Two Author calls, in this order: the
		// completion's edge checks PersonByID, so the person must exist first —
		// inside the enrolment's one transaction (FIX17b), so a failure of the
		// completion takes the person with it and nothing half-lands. ⟦was: "If
		// the process dies between them the roster holds an un-oriented person"
		// — the completion's own failure was only logged⟧ At a road
		// post nothing is recorded: nobody owes an orientation there (D-88), and
		// the person is held out at every orientation post until that post ticks
		// its own box. The completion is authored through the ordinary edge, so
		// it replicates and folds exactly as a tick from /orientation does.
		if s.Cfg.HasOrientation() {
			if _, err := domain.CompleteOrientation(st, s.Cfg.SiteID, s.Cfg.NodeEpoch, p.PersonID, recordedBy, 0, "default"); err != nil {
				failed = "record orientation"
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.gateFail(w, failed, err)
		return
	}

	// Post/redirect/GET. This route AUTHORS, so a refresh must not be able
	// to enrol the same person twice — the gate's accumulate routes can
	// render directly because they write nothing, and this one cannot.
	http.Redirect(w, r, sealNotices("/roster?enrolled="+p.PersonID), http.StatusSeeOther)
}

// intakeDuplicateRefusal words the duplicate refusal: every candidate with
// their company and origin site — the company is the field that actually
// tells two same-named people apart, so it is CONTEXT for the officer and
// never a reason to drop a candidate — and what the phone says where it
// says anything.
func intakeDuplicateRefusal(byName []personMatch, companies []domain.Company, siteName func(string) string) string {
	companyName := make(map[string]string, len(companies))
	for _, c := range companies {
		companyName[c.CompanyID] = c.Name
	}
	who := make([]string, 0, len(byName))
	for _, m := range byName {
		line := m.Person.Name
		if m.Person.CompanyID != nil {
			if n := companyName[*m.Person.CompanyID]; n != "" {
				line += " — " + n
			}
		}
		line += " (enrolled at " + siteName(m.Person.SiteID)
		switch {
		case m.SamePhone:
			line += "; same phone number"
		case m.DifferentPhone:
			line += "; different phone number on file, so possibly a different person"
		}
		// ⟦R-118 / D-21.5⟧ a retired candidate is still named — the likeliest
		// reading of "enrol somebody whose name is on the roster, retired"
		// is that they are back, and the remedy is to un-retire the record,
		// not to mint a second one.
		if m.Person.IsRetired() {
			line += "; RETIRED — if this is them, un-retire that record (clear retired on the amend page) rather than enrolling again"
		}
		line += ")"
		who = append(who, line)
	}
	return "Possibly already on the roster: " + strings.Join(who, "; ") +
		". If they are already enrolled, do not enrol them again — a second record for one person is a second person on the muster, and the muster does not merge them. If this really is a different person with the same name, tick \"different person, same name\" and enrol them."
}
