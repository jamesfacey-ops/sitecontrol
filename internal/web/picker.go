// picker.go — U2's person picker: the view model behind templates/_picker.html.
//
// WHAT IT REPLACES. Every `<select multiple size="5">` in this app used to
// render the ENTIRE season roster (1,279 people at scale, measured in
// design/bulk-movement-v1.md), in person_id order — and person ids are
// crypto/rand hex, so the order was measured RANDOM with respect to name
// (0 of 200 sampled positions agreed with a name sort). Picking 26 people
// out of that meant ~256 scroll increments through a five-row window, and a
// single stray plain click (not ctrl-held) silently cleared the whole
// in-progress selection with no error and no confirmation.
//
// The replacement is a checkbox (or radio) list that is:
//   - SORTED — by company, then trade, then name, everywhere;
//   - SCOPED — see the safe-default note on each caller. The list offered
//     for an action is the list of people that action can actually apply to;
//   - SEARCHABLE — a filter box, revealed only when JS is available. It
//     HIDES rows, never unchecks them: a ticked person who scrolls out of
//     the filter is still submitted, so searching can never quietly shrink
//     a selection;
//   - INCAPABLE OF SILENTLY DISCARDING A SELECTION — a checkbox list has no
//     "plain click clears everything" mode at all, and every submit routes
//     through the consolidated confirm() that names each person by name.
//
// NO UNSCOPED "SELECT ALL" EXISTS ANYWHERE IN HERE, deliberately:
// bulk-movement-v1.md calls an unscoped select-all "the single worst
// failure mode possible here". The only select-all controls are bound to
// ONE company or ONE trade, are rendered from the snapshot names the
// visit already carries, and state their own scope and count in their
// label.
//
// TWO TICK-BOXES, ONE NAME. A row renders the person's name and nothing
// else, while what it submits is an id the operator never sees. Two rows in
// one company/trade subgroup whose names fold to the same thing therefore
// used to draw two identical tick-boxes — and for check-out, ticking the
// wrong one removes a person who is still on site from the evacuation
// muster, with no reopen path in this module. groupBuilder.build now
// detects that pair (markSameNames), tags each row with the tail of its
// PERSON id, and hands the subgroup a same-name entry the template renders
// as a visible note. The note is the substance: the tag tells an officer
// nothing they can choose BY, and two tagged rows with no explanation read
// as "two records — tick both", which under-counts the muster. What the
// note says is per action (personPicker.SameName), because the safe error
// runs the opposite way for an action that adds people and one that
// removes them. ⚠ NONE OF THIS CLOSES THE DEFECT: the duplicate is made
// visible and the officer is told which way the safe error lies. A real
// discriminator — the CHECK-IN TIME — is available with no schema change
// (visit.trip_id → trip_arrival → event_log → ResolveOccurredAt; measured
// 2026-09-04 at 24–39 ms for 150 open visits) and is the plan owner's
// choice, not built here: it would be the first per-event time rendered on
// any page, and how an untrusted-clock time is worded is undecided. It is
// also NOT a complete answer: everyone on one arrival shares one time (a
// bus is one trip_arrival), so a time separates a pair ACROSS arrivals and
// never within one. The note is therefore the primary deliverable, and the
// person-id tail is the fallback that still separates a pair when nothing
// else does. refSameNameRows is the seam where a time drops in.
package web

import (
	"sort"
	"strings"

	"sitecontrol/internal/domain"
)

// pickerRow is one person as one tick-box. Value is whatever the action's
// existing form field has always carried — a person_id for the trip forms,
// a visit_id for gate check-out — so no POST target or field name changes.
type pickerRow struct {
	Value string
	Name  string
	// PersonID is carried for one reason: when two rows in a subgroup share
	// a name, the tail of the PERSON id is what gets shown. Person rather
	// than visit because the person id is already an officer-facing value
	// — gate.go's resolveTypedPerson prints person ids in its refusal, and
	// the person amendment form takes one as its input — while no template
	// renders a visit id anywhere, so a visit-id tail would correlate with
	// nothing on any screen, receipt or sheet.
	PersonID string
	// Ref is EMPTY on every row whose name is unique in its subgroup — a
	// marker that fires on every row is a marker nobody reads. On a
	// same-name row it is the whole phrase rendered after the name, in
	// words ("person id …f5e3"), so the template parenthesises it and
	// nothing else: whatever fact separates the pair, Go owns its wording.
	// Today that fact is the person-id tail (refSameNameRows). It is a
	// separate field so Name stays the bare roster name for the sort, the
	// confirm and every other consumer.
	Ref string
	// When is the sign-in time and date on an ON-SITE row, `hh:nn mm/dd`
	// (gate_signin.go). EMPTY on every picker that is not the gate's on-site
	// roster, and an empty When renders NOTHING — no span, no bytes — so the
	// season-scale pickers this template also serves pay nothing for it.
	// Display only: nothing sorts or groups by it (D-90/D-92, TP §4.4).
	// Name stays bare.
	When string
	// Origin is the site id of the post that ADDED this person
	// (personOriginSite), set by refSameNameRows on a same-name row only —
	// beside Ref, never instead of it. Templates render it by display name
	// ("added at Ice Pad", sitelabel), so this stays a raw id here.
	// James, 2026-10-04 (Q-R5-3): JS-off same-name lists show the origin
	// post. ⟦was: the person-id tail alone told a same-name pair apart
	// (L11-2d) — readable only by someone who reads ids⟧
	Origin string
}

// pickerSameName is one name shared by more than one row in a subgroup:
// the spelling on its first row and how many rows carry it.
type pickerSameName struct {
	Name  string
	Count int
}

// pickerSubgroup is one trade inside a company.
type pickerSubgroup struct {
	Label string
	Rows  []pickerRow
	// SameName lists every name carried by more than one row here. The
	// template renders one note per entry, at the subgroup, before the
	// rows — so the officer reads what a tagged pair means before they
	// read the pair.
	SameName []pickerSameName
}

// pickerGroup is one company. Its subgroups are its trades.
type pickerGroup struct {
	Label string
	Count int
	Subs  []pickerSubgroup
}

// pickerSection is one BAND of a picker: a set of company groups under a
// heading that says why these people are the likely ones. Sections are how
// an ADD-direction picker is scoped without anybody being removed from it —
// see the ordering-not-filtering note on personPicker.Sections.
type pickerSection struct {
	Label string
	Note  string
	// Collapsed renders the section inside a <details>, closed. It is still
	// in the document, still submits, still works with JavaScript off, and
	// the search filter opens it when a match is inside.
	Collapsed bool
	Count     int
	Groups    []pickerGroup
}

// personPicker is one rendered list. SafeDefault is not decoration: it is
// the sentence the operator reads that says which way a missed tick fails,
// and it is asserted by TestEveryPickerTellsTheOperatorWhichWayAMissedTickFails.
//
// CITATION CORRECTED 2026-08-21 (V-11): this comment used to cite a test
// that has never existed anywhere in the repository. A comment that names a
// test as the guarantee for a life-safety sentence, and names one that does
// not exist, is worse than silence — it is the reason nobody went looking.
// The dead name is deliberately not repeated here: that whole class is now
// checked by TestEveryTestNameCitedInACommentActuallyExists, which would
// flag it again, and a guard with an exemption for "I meant it this time"
// is a guard somebody eventually exempts their way past.
type personPicker struct {
	// FieldName is the EXISTING form field this picker submits under —
	// occupant_person_ids, person_ids, visit_ids, driver_person_id. Never
	// renamed: the whole chunk is a re-composition of existing surfaces.
	FieldName string
	// Mode is "checkbox" (any number) or "radio" (exactly one, e.g. the
	// driver). Radio pickers offer no select-all: "all drivers" is not a
	// thing.
	Mode string
	// IDPrefix scopes this picker's DOM ids and its script hooks. Unique
	// per picker on a page.
	IDPrefix string
	// Required marks the control required in the browser. On a radio group
	// this genuinely means "one must be chosen"; a checkbox group has no
	// such HTML semantic, so a checkbox picker relies on the domain
	// refusal instead (both are exercised by tests).
	Required bool
	// Verb is what the button will do to the ticked people, in the
	// operator's words ("check out", "put on this vehicle").
	Verb string
	// SafeDefault: which direction an unticked box fails in, and why that
	// is the safe one for THIS action.
	SafeDefault string
	// Empty: what to say when the scope contains nobody. Never a blank box.
	Empty string
	// SameName: what two boxes carrying one name MEAN for this action, and
	// what to do when the officer cannot tell which is which. Rendered at
	// the subgroup, only when such a pair exists there. Per action, like
	// SafeDefault, and for the same reason: for an action that removes
	// people from the muster the safe error is to tick neither, and for
	// one that adds people that same advice is the omission that hides a
	// live person. Asserted on the served pages by
	// TestTheGatePageTellsTheOfficerWhatTwoBoxesWithOneNameMean and
	// TestEveryConsolePickerSaysWhatTwoBoxesWithOneNameMean.
	SameName string
	// Sections carry the scoping, and the two directions scope DIFFERENTLY
	// on purpose:
	//
	//   REMOVE-direction actions (dispatch, check-out) are genuinely
	//   FILTERED to the people recorded on site here. Leaving somebody out
	//   of such a list can only ever fail by not removing them, which
	//   over-reports the muster — the safe direction — and the domain
	//   refuses a check-out of a visit that is not open here anyway.
	//
	//   ADD-direction actions (arrival) are ORDERED, NEVER FILTERED. The
	//   people the record says are signed in somewhere else come first,
	//   under a heading that says so, and the rest of the roster follows in
	//   a collapsed section. Nobody is removed, because for an action that
	//   adds people to the muster, a name missing from the picker is the
	//   same failure as a name nobody ticked: a person who is physically
	//   here and on no list.
	Sections []pickerSection
}

// Total is the number of people this picker offers — rendered on its face
// so the operator can see the scope is a scope and not the whole roster.
func (p personPicker) Total() int {
	n := 0
	for _, sec := range p.Sections {
		n += sec.Count
	}
	return n
}

// section wraps groups into one band and counts it.
func section(label, note string, collapsed bool, groups []pickerGroup) pickerSection {
	sec := pickerSection{Label: label, Note: note, Collapsed: collapsed, Groups: groups}
	for _, g := range groups {
		sec.Count += g.Count
	}
	return sec
}

// groupBuilder accumulates company -> trade -> rows and sorts the whole
// thing by name at every level. Everything unattributed sorts LAST under an
// explicit label — never under a blank heading that reads as an oversight.
type groupBuilder struct {
	rows map[string]map[string][]pickerRow
}

const (
	noCompany = "No company recorded"
	// ⟦W8-22(ii)⟧ The no-trade heading is domain's ONE spelling, shared
	// rather than respelled: two spellings of one idea is what W8 unified,
	// and a local literal here would re-create it in the same change that
	// removed it.
	noTrade = domain.NoTradeRecorded
)

func newGroupBuilder() *groupBuilder {
	return &groupBuilder{rows: map[string]map[string][]pickerRow{}}
}

func (b *groupBuilder) add(company, trade string, row pickerRow) {
	if company == "" {
		company = noCompany
	}
	if trade == "" {
		trade = noTrade
	}
	if b.rows[company] == nil {
		b.rows[company] = map[string][]pickerRow{}
	}
	b.rows[company][trade] = append(b.rows[company][trade], row)
}

// lessLabel sorts alphabetically but always sinks the two explicit
// "not recorded" buckets to the bottom of their level.
func lessLabel(a, b string) bool {
	rank := func(s string) int {
		if s == noCompany || s == noTrade {
			return 1
		}
		return 0
	}
	if ra, rb := rank(a), rank(b); ra != rb {
		return ra < rb
	}
	return a < b
}

func (b *groupBuilder) build() []pickerGroup {
	companies := make([]string, 0, len(b.rows))
	for c := range b.rows {
		companies = append(companies, c)
	}
	sort.Slice(companies, func(i, j int) bool { return lessLabel(companies[i], companies[j]) })

	out := make([]pickerGroup, 0, len(companies))
	for _, c := range companies {
		wgs := make([]string, 0, len(b.rows[c]))
		for w := range b.rows[c] {
			wgs = append(wgs, w)
		}
		sort.Slice(wgs, func(i, j int) bool { return lessLabel(wgs[i], wgs[j]) })

		g := pickerGroup{Label: c}
		for _, w := range wgs {
			rows := b.rows[c][w]
			sort.Slice(rows, func(i, j int) bool {
				if rows[i].Name != rows[j].Name {
					return rows[i].Name < rows[j].Name
				}
				return rows[i].Value < rows[j].Value // stable for two people with one name
			})
			// After the sort, so the tiebreak above still decides the
			// order of a same-name pair: the tag lives in Ref, not in
			// Name, precisely so it cannot reorder anything.
			same := markSameNames(rows)
			g.Subs = append(g.Subs, pickerSubgroup{Label: w, Rows: rows, SameName: same})
			g.Count += len(rows)
		}
		out = append(out, g)
	}
	return out
}

// onSiteGroups builds the picker groups for an action that applies to
// PEOPLE CURRENTLY ON SITE HERE, grouped by the company/trade the visit
// itself recorded.
//
// HISTORY RULE (design §8.8-i, and gate.go's file comment): its input is
// []onSiteRow — the output of buildOnSiteRows, the deliberately-shaped
// function whose own signature makes it INCAPABLE of re-resolving a visit's
// company through anything but the snapshot already on the row. This
// function inherits that guarantee by construction: it cannot be handed a
// Company, a Trade or an AliasMap either, so no edit here can start
// showing a person's CURRENT company under a heading that claims to say
// where they were when they checked in.
//
// valueOf chooses what the tick-box submits: the person id (the trip forms'
// long-standing occupant_person_ids / driver_person_id) or the visit id
// (gate check-out's visit_ids). Neither field name changed.
func onSiteGroups(rows []onSiteRow, valueOf func(onSiteRow) string) []pickerGroup {
	b := newGroupBuilder()
	for _, r := range rows {
		b.add(r.Company, r.Trade, pickerRow{Value: valueOf(r), Name: r.Person, PersonID: r.PersonID, When: r.SignedIn})
	}
	return b.build()
}

func pickByPerson(r onSiteRow) string { return r.PersonID }
func pickByVisit(r onSiteRow) string  { return r.VisitID }

// sameName reports whether two rows read as one name to the officer and to
// the gate. gate.go's resolveTypedPerson matches a typed name with
// TrimSpace + EqualFold, and a browser collapses inner whitespace before
// the label is seen, so both are folded here. A byte-exact key left
// "Robert stone" and "Robert  Stone" undecorated while they rendered
// near-identically.
func sameName(a, b string) bool {
	return strings.EqualFold(foldSpaces(a), foldSpaces(b))
}

// foldSpaces trims and collapses every run of whitespace to one space —
// what the browser does to the label anyway.
func foldSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

// markSameNames finds every set of rows in one subgroup that share a name,
// has each set's rows tagged (refSameNameRows), and returns one entry per
// set for the template's note. Rows whose name is unique are left exactly
// as they were. Detection and tagging are deliberately two functions: the
// note comes from the detection alone and does not depend on what the tag
// is, or on there being one.
func markSameNames(rows []pickerRow) []pickerSameName {
	var out []pickerSameName
	for _, set := range sameNameSets(rows) {
		if len(set) < 2 {
			continue
		}
		refSameNameRows(rows, set)
		out = append(out, pickerSameName{Name: rows[set[0]].Name, Count: len(set)})
	}
	return out
}

// sameNameSets groups the rows' indices by name under sameName, in
// first-seen order, singletons included.
//
// Sets are found by comparing against each set's first row with sameName
// rather than through a map key, because EqualFold has no single canonical
// form to key on. A subgroup is one trade inside one company, so the
// quadratic shape costs nothing measurable.
func sameNameSets(rows []pickerRow) [][]int {
	var sets [][]int
	for i, r := range rows {
		placed := false
		for k, set := range sets {
			if sameName(rows[set[0]].Name, r.Name) {
				sets[k] = append(set, i)
				placed = true
				break
			}
		}
		if !placed {
			sets = append(sets, []int{i})
		}
	}
	return sets
}

// refSameNameRows fills Ref on every row of ONE same-name set.
//
// THIS IS THE DISCRIMINATOR SEAM. The detection above does not care what
// goes here and the template renders Ref as given, so a per-visit fact
// drops in at this one function without touching either. Today the fact
// is the tail of the person id, widened until unique (personRefs).
//
// The check-in time is the fact an officer could actually choose by, and
// it is reachable with NO schema change — visit.trip_id → trip_arrival
// (node_epoch, seq) → event_log → ResolveOccurredAt; two same-name people
// resolved to 06:12 vs 14:47, one arrival row read each, 24–39 ms for a
// full derivation over 150 open visits (measured 2026-09-04). It is NOT
// built here because rendering it is the plan owner's call: it would be the
// first per-event time on any page, and the wording of an untrusted-clock
// time is an open design question. When it lands it goes here as: fill Ref
// from the time where it SEPARATES the set, and fall through to the
// person-id tail where it does not — everyone on one arrival shares one
// time, because a bus is one trip_arrival, so two colliding names off one
// vehicle show an identical time and still need the fallback. Three other
// things change with it: onSiteRow/pickerRow carry the fact, the four
// personPicker.SameName sentences stop saying "the tail of its person id",
// and TestPersonIDsSharingAShortTailAreStillToldApart keeps holding the
// fallback to uniqueness.
func refSameNameRows(rows []pickerRow, set []int) {
	ids := make([]string, len(set))
	for j, i := range set {
		ids[j] = rows[i].PersonID
		if ids[j] == "" {
			// Nothing in this package builds a row without a person id,
			// but a tag made from nothing is no tag at all: fall back to
			// the submitted value, which is at least distinct per row.
			ids[j] = rows[i].Value
		}
	}
	for j, ref := range personRefs(ids) {
		rows[set[j]].Ref = "person id " + ref
		rows[set[j]].Origin = personOriginSite(rows[set[j]].PersonID)
	}
}

// personOriginSite is the site id of the post that minted personID — the
// "<site_id>-person-<hex>" prefix domain.newID writes (ids.go), so it reads
// the same at every post and needs no roster lookup. "" for an id of any
// other shape: a missing origin renders nothing, never a guess.
func personOriginSite(personID string) string {
	if i := strings.LastIndex(personID, "-person-"); i > 0 {
		return personID[:i]
	}
	return ""
}

// personRefs returns, for the ids of a set of rows sharing one name, the
// shortest right-hand tail — never fewer than four characters — at which
// every id in the set reads differently, each prefixed with an ellipsis
// when it is not the whole id.
//
// WIDENED UNTIL UNIQUE, NOT FIXED. An id ends in 16 random hex characters
// (ids.go), so a fixed 4-character tail collides for one pair in 65,536 —
// and a collision there renders the pair identical again, which is the
// exact defect this exists to make visible. Widening stops at the whole
// id. Two rows carrying the SAME person id — one human, one record, two
// open visits — cannot be told apart by any tail and are not: either box
// is then the same person, and the note still says what to do.
func personRefs(ids []string) []string {
	width := 4
	for {
		tails := make([]string, len(ids))
		seen := map[string]bool{}
		distinct, whole := true, true
		for i, id := range ids {
			if r := []rune(id); len(r) > width {
				tails[i] = "…" + string(r[len(r)-width:])
				whole = false
			} else {
				tails[i] = id
			}
			if seen[tails[i]] {
				distinct = false
			}
			seen[tails[i]] = true
		}
		if distinct || whole {
			return tails
		}
		width++
	}
}

// rosterGroups builds picker groups from the CURRENT roster — for an
// action that applies to somebody who is not on site yet, i.e. an arrival.
// Reading today's company/trade is correct precisely because nothing
// historical is being displayed: the check-in about to happen will snapshot
// these same current values onto the new visit. skip drops people already
// listed in an earlier section, so nobody appears twice (two tick-boxes for
// one person would post their id twice).
func rosterGroups(persons []domain.Person, companyName, tradeName map[string]string, skip map[string]bool) []pickerGroup {
	b := newGroupBuilder()
	for _, p := range persons {
		if skip[p.PersonID] {
			continue
		}
		// ⟦R-118 / [[D-21.5]], 2026-10-03⟧ a RETIRED record is not offered
		// for an arrival: this band is the roster-as-picker, and retirement
		// is exactly "no longer selectable". It is a filter on the ROSTER
		// band only — the on-site-at-a-peer and out-at-a-pad bands above it
		// come from visits and still list a retired person who is recorded
		// somewhere, because presence is never hidden (the ordering-not-
		// filtering rule stands for everyone the record says is out there).
		if p.IsRetired() {
			continue
		}
		company, trade := "", ""
		if p.CompanyID != nil {
			company = companyName[*p.CompanyID]
		}
		if p.TradeID != nil {
			trade = tradeName[*p.TradeID]
		}
		b.add(company, trade, pickerRow{Value: p.PersonID, Name: p.Name, PersonID: p.PersonID})
	}
	return b.build()
}

// arrivalSections orders the arrival picker: the people the record says are
// signed in SOMEWHERE ELSE right now come first, under headings that say
// where, and the remainder of the roster follows in a collapsed section.
//
// ORDERING, NOT FILTERING — and that is the safety rule, not a convenience.
// An arrival ADDS people to the muster, so the dangerous failure is
// omission: a person who is physically here and on no list. A picker that
// dropped anyone would make that omission unfixable at the moment it
// matters, so nobody is dropped. What the scoping buys is that the 20-odd
// people who could plausibly be on this vehicle are at the top, in name
// order, instead of scattered at random through 1,279 rows.
//
// Every read here is site-scoped and indexed (OpenVisitsAt per peer, F30) —
// no full visit dump is done for this.
func (s *Server) arrivalSections(persons []domain.Person, personName, companyName, tradeName map[string]string) ([]pickerSection, error) {
	seen := map[string]bool{}
	var sections []pickerSection

	for _, peer := range s.peerSiteIDs() {
		visits, err := domain.OpenVisitsAt(s.Store, peer)
		if err != nil {
			return nil, err
		}
		rows := buildOnSiteRows(visits, peer, personName)
		if len(rows) == 0 {
			continue
		}
		for _, r := range rows {
			seen[r.PersonID] = true
		}
		sections = append(sections, section(
			"Recorded on site at "+s.Cfg.SiteName(peer),
			"The record says these people are signed in at "+s.Cfg.SiteName(peer)+" — the likeliest crew on a vehicle arriving from there.",
			false, onSiteGroups(rows, pickByPerson)))
	}

	outAt, err := domain.OutAtUncontrolled(s.Store)
	if err != nil {
		return nil, err
	}
	person := make(map[string]domain.Person, len(persons))
	for _, p := range persons {
		person[p.PersonID] = p
	}
	pads := newGroupBuilder()
	padCount := 0
	for _, o := range outAt {
		for _, ref := range o.Persons {
			if seen[ref.PersonID] {
				continue
			}
			seen[ref.PersonID] = true
			padCount++
			company, trade := "", ""
			if p, ok := person[ref.PersonID]; ok {
				if p.CompanyID != nil {
					company = companyName[*p.CompanyID]
				}
				if p.TradeID != nil {
					trade = tradeName[*p.TradeID]
				}
			}
			pads.add(company, trade, pickerRow{Value: ref.PersonID, Name: ref.Name, PersonID: ref.PersonID})
		}
	}
	if padCount > 0 {
		sections = append(sections, section(
			"Recorded out at a pad",
			"Checked out to an uncontrolled destination with no gate and no timer. They check in normally on return, on any vehicle.",
			false, pads.build()))
	}

	rest := rosterGroups(persons, companyName, tradeName, seen)
	if len(rest) > 0 {
		label := "Everyone else on the roster"
		note := "Nobody here has an open record anywhere, so none of them is an expected arrival — but anyone can walk in off any vehicle, so all of them are still listed and still tickable. Open this section, or search: the search box looks in here too."
		if len(sections) == 0 {
			// No peer or pad records at all: this is not a fallback, it is
			// the whole list, so it must not arrive collapsed.
			label = "Roster"
			note = "Nobody is recorded on site at a peer or out at a pad, so there is nothing to narrow this to yet."
			sections = append(sections, section(label, note, false, rest))
		} else {
			sections = append(sections, section(label, note, true, rest))
		}
	}
	return sections, nil
}

// refDataNames returns companyID->name and tradeID->name for the
// roster picker's headings. Small tables (15 companies / 45 trades at
// the season scale measured in bulk-movement-v1.md).
func refDataNames(s *Server) (map[string]string, map[string]string, error) {
	companies, err := domain.Companies(s.Store)
	if err != nil {
		return nil, nil, err
	}
	trades, err := domain.Trades(s.Store)
	if err != nil {
		return nil, nil, err
	}
	cn := make(map[string]string, len(companies))
	for _, c := range companies {
		cn[c.CompanyID] = c.Name
	}
	wn := make(map[string]string, len(trades))
	for _, w := range trades {
		wn[w.TradeID] = w.Name
	}
	return cn, wn, nil
}
