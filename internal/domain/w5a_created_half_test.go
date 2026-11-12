// w5a_created_half_test.go — W5a's DOMAIN-side criteria:
//
//	W5a-1  behaviour half: an awkward Name survives every domain read path
//	       byte-identical
//	W5a-2  the created half lands (payload shape + the fold writes the parts)
//	W5a-3  Name is required and authoritative; nothing composes it from parts
//	W5a-4  every person-name sort reads Name — the TWO domain sort sites
//	W5a-5  divergence is lawful and silent (both fixtures)
//	W5a-7  a wire-received partless person_created folds clean
//	W5a-9  Position is nullable free text, round-tripped verbatim
//
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31.
// Seams and fixture machinery: w5a_seams_test.go (same package).
//
// ⚠ WHICH OF THESE ARE ALREADY TRUE AT 88dd6a6, restated because it decides
// what the ledger may book. W5a-1, W5a-3, W5a-4 (sort half), W5a-5
// (amendment half) and W5a-9 all carry ⟦NO PRE-CHUNK RED⟧: for them **a
// clean pass is not evidence** and the ledger row is the planted CONTROL plus
// the named mutant. W5a-2, W5a-5's CREATION half and W5a-7's wire-partless
// half are new behaviour and produce genuine pre-chunk RUNTIME reds, captured
// in reports/window-build/W5a-tests.md.
package domain

import (
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// =======================================================================
// W5a-2 — the created half lands
// =======================================================================

// TestW5aPersonCreatedCarriesNullableNamePartsAndPosition — W5a-2's payload
// half (SEAM W5a-S-CREATED / SEAM W5a-S-PAYLOAD).
//
// Asserted by REFLECTION rather than by a struct literal, deliberately: a
// literal would compile-block the whole domain package at 88dd6a6 and bury
// every runnable red in this file behind one compile error. Reflection makes
// the pre-chunk state a PRODUCED RUNTIME RED naming the missing field.
//
// EXPECTED RED (pre-chunk): "PersonCreated has no field LastName" ×3 — the
// struct is refdata.go:37–44's six fields.
// EXPECTED RED (m5a-2): a field renamed, or typed `string` rather than
// `*string`, or losing `omitempty` (which would put an empty part on the wire
// and into the row hash of every partless person ever created).
func TestW5aPersonCreatedCarriesNullableNamePartsAndPosition(t *testing.T) {
	typ := reflect.TypeOf(PersonCreated{})
	// Rule 6 — the arrangement this test needs: the type still carries the
	// pre-chunk fields it is being extended from.
	// ⟦LOCK — §82, 2026-09-03⟧ ⭐ RE-POINTED, NOT DISCHARGED.
	// ⟦was: "\tfor _, base := range []string{\"Type\", \"PersonID\", \"Name\", \"CompanyID\", \"WorkgroupID\", \"RecordedBy\"} {"⟧
	//
	// ⚠ THIS TEST'S SUBJECT IS STANDING AND WAS NEVER BROKEN. It asserts
	// LastName/FirstName/Position are `*string` with `omitempty` — "nil means
	// ABSENT, which is a different thing from empty-string (W3-3's
	// distinction, load-bearing not style)". That is a property that must hold
	// forever, not a statement about W5a's diff. It failed only at this Rule 6
	// guard, before reaching the assertion, because W8 renamed the field it
	// names. [MEASURED, S82-W5-CLUSTER §D2] retargeting this base alone makes
	// the test PASS.
	//
	// ⛔ DISCHARGING IT WOULD HAVE DELETED A LIVE STANDING ASSERTION with no
	// replacement — the one disposition §82's two branches would have led to,
	// and the reason each of the five was read rather than classified as a set.
	//
	// `TradeID` is the direct successor at the same position ⟦§61⟧.
	for _, base := range []string{"Type", "PersonID", "Name", "CompanyID", "TradeID", "RecordedBy"} {
		if _, ok := typ.FieldByName(base); !ok {
			t.Fatalf("fixture defect: PersonCreated has no field %s — this is not the type measured at refdata.go:37–44, so nothing below is about W5a", base)
		}
	}

	want := map[string]string{"LastName": w5aKeyLastName, "FirstName": w5aKeyFirstName, "Position": w5aKeyPosition}
	for _, goName := range []string{"LastName", "FirstName", "Position"} {
		f, ok := typ.FieldByName(goName)
		if !ok {
			t.Errorf("W5a-2 (m5a-2): PersonCreated has no field %s. W5a's build IS the created half — W3 already landed the PersonAmended half (person_amend.go:60/:61/:67), so a builder briefed from the plan's `:2028` alone finds the fields present and skips the half that is missing", goName)
			continue
		}
		if f.Type.Kind() != reflect.Pointer || f.Type.Elem().Kind() != reflect.String {
			t.Errorf("W5a-2 (m5a-2): PersonCreated.%s is %s, want *string — nil means ABSENT, which is a different thing from empty-string (W3-3's distinction, load-bearing not style)", goName, f.Type)
		}
		tag := f.Tag.Get("json")
		parts := strings.Split(tag, ",")
		if parts[0] != want[goName] {
			t.Errorf("W5a-2 (m5a-2): PersonCreated.%s json key is %q, want %q (SEAM W5a-S-PAYLOAD — the spelling W3 already landed for the same fact)", goName, parts[0], want[goName])
		}
		if !w5aTagHas(parts[1:], "omitempty") {
			t.Errorf("W5a-2 (m5a-2): PersonCreated.%s json tag %q lacks omitempty — without it every partless person_created carries three nulls into its payload bytes and therefore into its row hash", goName, tag)
		}
	}

	// omitempty is asserted BEHAVIOURALLY too: a tag assertion alone passes
	// over a field whose type makes omitempty inert.
	raw, err := json.Marshal(PersonCreated{Type: w5aEvtPersonCreated, PersonID: "p1", Name: "Solo"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range w5aPartKeys {
		if _, present := got[k]; present {
			t.Errorf("W5a-2 (m5a-2): a partless PersonCreated marshals %q into its payload (%s) — nil must be ABSENT on the wire", k, raw)
		}
	}
}

func w5aTagHas(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// TestW5aFoldWritesNamePartsAndPosition — W5a-2's fold half. The projection
// ROW must carry the three values, not merely the composed read: a composed
// read would also be satisfied by an amendment, and "the fold silently not
// writing a part" is (m5a-2)'s second shape.
//
// EXPECTED RED (pre-chunk, runtime): the raw `person` row carries no
// last_name/first_name/position — applyPersonCreated's INSERT is
// refdata.go:217–218's six columns and PersonCreated cannot even decode the
// keys.
func TestW5aFoldWritesNamePartsAndPosition(t *testing.T) {
	st := w5aNewStore(t)
	const id = "site-b-person-parts"
	w5aApply(t, st, w5aWirePersonCreated(t, "site-b", 1, 1, w5aBase, id, "Ferguson, Mary", map[string]any{
		w5aKeyLastName:  "Ferguson",
		w5aKeyFirstName: "Mary",
		w5aKeyPosition:  "Night shift foreman",
	}))

	row := w5aRawPersonRow(t, st, id)
	for _, c := range []struct {
		field string
		got   *string
		want  string
	}{
		{"last_name", row.LastName, "Ferguson"},
		{"first_name", row.FirstName, "Mary"},
		{"position", row.Position, "Night shift foreman"},
	} {
		if c.got == nil {
			t.Errorf("W5a-2 (m5a-2): the RAW `person` projection row carries no %s — the fold did not write the part it was sent", c.field)
			continue
		}
		if *c.got != c.want {
			t.Errorf("W5a-2 (m5a-2): raw `person`.%s = %q, want %q", c.field, *c.got, c.want)
		}
	}

	p := w5aPerson(t, st, id)
	if p.LastName == nil || *p.LastName != "Ferguson" || p.FirstName == nil || *p.FirstName != "Mary" || p.Position == nil || *p.Position != "Night shift foreman" {
		t.Errorf("W5a-2: the composed person does not carry the created-half parts: last=%s first=%s position=%s", w5aShow(p.LastName), w5aShow(p.FirstName), w5aShow(p.Position))
	}
	if p.Name != "Ferguson, Mary" {
		t.Errorf("W5a-2/W5a-3: composed Name = %q, want %q — Name is authoritative and is never recomputed from the parts (D-48)", p.Name, "Ferguson, Mary")
	}
}

func w5aShow(p *string) string {
	if p == nil {
		return "<absent>"
	}
	return `"` + *p + `"`
}

func w5aPersonCount(t *testing.T, st *store.Store) int {
	t.Helper()
	rows, err := dumpSection[Person](st, "person")
	if err != nil {
		t.Fatalf("reading the person projection: %v", err)
	}
	return len(rows)
}

// =======================================================================
// W5a-7 — requiredness lives at the intake edge, never in payload shape or
// fold. (The refusal-preservation half is W5a's web file.)
// =======================================================================

// TestW5aWirePersonCreatedWithNoPartsFoldsClean — W5a-7's WIRE half. Vacuous
// pre-chunk (there are no parts to omit) and mutant-first after W5a-2,
// exactly as the lock says. Its tooth after the build: the fold accepts an
// arrival with no parts and composes with the parts ABSENT — nil, never
// empty-string.
//
// EXPECTED RED (m5a-7): the parts requirement moved into the payload shape or
// the fold (fold-time refusal of a partless wire person) — w5aApply's own
// fatal fires, naming the criterion.
func TestW5aWirePersonCreatedWithNoPartsFoldsClean(t *testing.T) {
	st := w5aNewStore(t)
	const id = "site-b-person-partless"
	// Rule 6 — the arrangement: the payload genuinely carries none of the
	// three keys. A fixture that quietly sent `"last_name": ""` would be
	// testing empty-string, not absence.
	env := w5aWirePersonCreated(t, "site-b", 1, 1, w5aBase, id, "Legacy, Person", nil)
	var sent map[string]any
	if err := json.Unmarshal(env.Payload, &sent); err != nil {
		t.Fatal(err)
	}
	for _, k := range w5aPartKeys {
		if _, present := sent[k]; present {
			t.Fatalf("fixture defect: the partless fixture carries key %q — absence is what this test is about", k)
		}
	}

	w5aApply(t, st, env)
	p := w5aPerson(t, st, id)
	if p.Name != "Legacy, Person" {
		t.Errorf("W5a-7: composed Name = %q, want %q", p.Name, "Legacy, Person")
	}
	for _, c := range []struct {
		field string
		got   *string
	}{{"LastName", p.LastName}, {"FirstName", p.FirstName}, {"Position", p.Position}} {
		if c.got != nil {
			t.Errorf("W5a-7 (m5a-7): a partless arrival composed %s as %q — absent must stay ABSENT (nil), not become empty-string", c.field, *c.got)
		}
	}
}

// =======================================================================
// W5a-5 — divergence is lawful and silent
// =======================================================================

// TestW5aCreationDivergenceIsLawfulAndSilent — W5a-5's CREATION half, which
// is NEW and reds pre-chunk (M18).
//
// The fixture is WIRE-RECEIVED (SEAM W5a-S-DIVERGE / M18(i)): under W5a-8
// composition is one-way at our own authoring edge, so a locally-authored
// person_created whose parts disagree with its Name is unconstructible — a
// builder writing one either writes an impossible fixture or quietly weakens
// W5a-8 to build it. W3-2's rule already requires the fold to accept it.
//
// EXPECTED RED (pre-chunk, runtime): the parts arrive and are dropped, so
// LastName is absent and the divergence cannot yet exist.
// EXPECTED RED (m5a-5): a reconciliation pass, warning banner or repair that
// fires when parts disagree with Name.
func TestW5aCreationDivergenceIsLawfulAndSilent(t *testing.T) {
	buf := w5aCaptureLog(t)
	w5aRequireLogChannelWorks(t, buf)

	st := w5aNewStore(t)
	const id = "site-b-person-diverged"
	// Lawful under D-48: "name is authoritative, parts are extras that can
	// diverge." These parts do NOT compose to this Name, by construction.
	w5aApply(t, st, w5aWirePersonCreated(t, "site-b", 1, 1, w5aBase, id, "Ferguson, Mary", map[string]any{
		w5aKeyLastName:  "Nakamura",
		w5aKeyFirstName: "Yuki",
	}))

	p := w5aPerson(t, st, id)
	if p.Name != "Ferguson, Mary" {
		t.Errorf("W5a-5/W5a-3 (m5a-3): composed Name = %q, want %q — Name is authoritative and is NOT recomputed from the parts, however loudly they disagree", p.Name, "Ferguson, Mary")
	}
	if p.LastName == nil || *p.LastName != "Nakamura" {
		t.Errorf("W5a-5 (m5a-5): last_name composed as %s, want \"Nakamura\" — the divergence must SURVIVE; repairing it is the mutant", w5aShow(p.LastName))
	}
	if p.FirstName == nil || *p.FirstName != "Yuki" {
		t.Errorf("W5a-5 (m5a-5): first_name composed as %s, want \"Yuki\"", w5aShow(p.FirstName))
	}
	if w := w5aFindReconciliationWord(buf.String()); w != "" {
		t.Errorf("W5a-5 (m5a-5): folding a divergent creation logged %q. D-48's text is a PERMISSION — \"nothing needs to detect the disagreement, warn about it, or repair it\"; the prohibition asserted here is the DERIVED ruling on top of it: no warning, no reconciliation, no repair, no log line. Log:\n%s", w, buf.String())
	}
}

// TestW5aAmendmentDivergenceIsLawfulAndSilent — W5a-5's AMENDMENT half.
//
// ⟦NO PRE-CHUNK RED⟧ — divergence by amendment is already silent at 88dd6a6
// because nothing looks (person_amend.go:357–362's three independent facts).
// **This clean pass is NOT evidence.** Its ledger row is (m5a-5).
func TestW5aAmendmentDivergenceIsLawfulAndSilent(t *testing.T) {
	buf := w5aCaptureLog(t)
	w5aRequireLogChannelWorks(t, buf)

	st := w5aNewStore(t)
	p, err := CreatePerson(st, "site-a", 1, "Ferguson, Mary", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{
		PersonID: p.PersonID, LastName: w5aStr("Nakamura"), FirstName: w5aStr("Yuki"),
	}, 0, "default"); err != nil {
		t.Fatalf("W5a-5 (m5a-5): the amendment edge REFUSED a divergent part set (%v) — under D-48 this is a lawful record, not an error", err)
	}

	got := w5aPerson(t, st, p.PersonID)
	if got.Name != "Ferguson, Mary" {
		t.Errorf("W5a-5/W5a-3 (m5a-3): amending last_name rewrote Name to %q — name, last_name and first_name are three independent facts and nothing recomposes (D-48's own measured premise)", got.Name)
	}
	if got.LastName == nil || *got.LastName != "Nakamura" {
		t.Errorf("W5a-5 (m5a-5): last_name = %s after amendment, want \"Nakamura\"", w5aShow(got.LastName))
	}
	if w := w5aFindReconciliationWord(buf.String()); w != "" {
		t.Errorf("W5a-5 (m5a-5): a divergent amendment logged %q — no warning, no reconciliation, no repair, no log line. Log:\n%s", w, buf.String())
	}
}

// =======================================================================
// W5a-3 — Name is required and authoritative
// =======================================================================

// TestW5aNameIsRequiredAtTheAuthoringEdge — W5a-3's requiredness half.
//
// ⟦NO PRE-CHUNK RED⟧ — createPerson already refuses an empty name
// (refdata.go:332–334). **The clean pass is not evidence.** Requiredness
// stands on the plan (`:2029`) and on the code, NOT on D-48: D-48 rules
// `Name` AUTHORITATIVE and says nothing about requiredness (M20/cit-C-8 —
// citing a real ruling for an adjacent proposition is the defect the contract
// names, and the remedy is to reground, never to soften).
func TestW5aNameIsRequiredAtTheAuthoringEdge(t *testing.T) {
	st := w5aNewStore(t)
	before := w5aPersonCount(t, st)
	if _, err := CreatePerson(st, "site-a", 1, "", nil, nil, 0, "default"); err == nil {
		t.Fatalf("W5a-3: CreatePerson accepted an empty name — `Name` stays required (plan `:2029`; the guard is refdata.go:332–334)")
	}
	if after := w5aPersonCount(t, st); after != before {
		t.Errorf("W5a-3: a refused empty-name creation authored %d person row(s) — a refusal must leave no trace", after-before)
	}
}

// TestW5aNoReadPathComposesNameFromTheParts — W5a-3's authority half, in the
// only form that can fail: a person whose parts would compose to a DIFFERENT
// name than the one stored.
//
// ⟦NO PRE-CHUNK RED⟧ — nothing composes Name from parts, so there is nothing
// to break. **The clean pass is not evidence**; the divergent fixture plus
// (m5a-3) is.
func TestW5aNoReadPathComposesNameFromTheParts(t *testing.T) {
	st := w5aNewStore(t)
	p, err := CreatePerson(st, "site-a", 1, "Ferguson, Mary", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{
		PersonID: p.PersonID, LastName: w5aStr("Nakamura"), FirstName: w5aStr("Yuki"),
	}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	// Rule 6 — the arrangement: composing the parts must give a DIFFERENT
	// answer from the stored Name, or the certified rule and its rival agree
	// and the assertion certifies nothing.
	const composedFromParts = "Nakamura, Yuki"
	const stored = "Ferguson, Mary"
	if composedFromParts == stored {
		t.Fatal("fixture defect: the parts compose to the stored Name, so this fixture cannot tell the certified rule from its rival")
	}

	if byID := w5aPerson(t, st, p.PersonID); byID.Name != stored {
		t.Errorf("W5a-3 (m5a-3): PersonByID composed Name = %q, want the stored %q", byID.Name, stored)
	}
	all, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	var seen bool
	for _, q := range all {
		if q.PersonID != p.PersonID {
			continue
		}
		seen = true
		if q.Name == composedFromParts {
			t.Errorf("W5a-3 (m5a-3): Persons() returned the name COMPOSED FROM THE PARTS (%q) rather than the stored Name — the parts are extras that may diverge and are never the answer to \"what is this person called\" (D-48 derived ruling 1)", composedFromParts)
		} else if q.Name != stored {
			t.Errorf("W5a-3 (m5a-3): Persons() composed Name = %q, want the stored %q", q.Name, stored)
		}
	}
	if !seen {
		t.Fatalf("fixture defect: Persons() did not return %s", p.PersonID)
	}
}

// =======================================================================
// W5a-1 — behaviour half (domain read paths)
// =======================================================================

// w5aAwkwardNames are the shapes W5a-1's behaviour half names: commas,
// multiple spaces, particles, and a single token.
var w5aAwkwardNames = []string{
	"Van der Berg, Jan",
	"O'Brien,  Mary Kate", // two spaces after the comma, kept verbatim
	"Cher",                // a single token — no comma to split on
	"Ng",                  // a two-letter single token
	"de la Cruz-Smith, José María",
}

// TestW5aAwkwardNamesSurviveEveryDomainReadPath — W5a-1's behaviour half.
//
// ⟦NO PRE-CHUNK RED⟧ — nothing parses a name at 88dd6a6, so this passes on
// arrival. **The clean pass is not evidence.** Its ledger row is (m5a-1b) —
// a read path RENDERING a derived part where Name belongs — which (m5a-1)
// cannot produce, because (m5a-1) writes the PARTS and leaves Name
// byte-identical (M9/TE-6).
//
// The fixture makes (m5a-1b) killable: every person's parts DISAGREE with
// their Name, so a read path returning a derived part returns a different
// string.
func TestW5aAwkwardNamesSurviveEveryDomainReadPath(t *testing.T) {
	st := w5aNewStore(t)
	ids := make([]string, 0, len(w5aAwkwardNames))
	want := map[string]string{}
	for i, name := range w5aAwkwardNames {
		p, err := CreatePerson(st, "site-a", 1, name, nil, nil, 0, "default")
		if err != nil {
			t.Fatal(err)
		}
		// Divergent parts (lawful under D-48) so a part-rendering read path
		// is detectable.
		if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{
			PersonID:  p.PersonID,
			LastName:  w5aStr("PART-LAST-" + strconv.Itoa(i)),
			FirstName: w5aStr("PART-FIRST-" + strconv.Itoa(i)),
		}, 0, "default"); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.PersonID)
		want[p.PersonID] = name
	}

	check := func(path, id, got string) {
		if got != want[id] {
			t.Errorf("W5a-1 (m5a-1b): %s returned %q for the person whose Name is %q — every read path answering \"what is this person called\" returns the composed Name byte-identical, never a derived part", path, got, want[id])
		}
	}

	for _, id := range ids {
		check("PersonByID", id, w5aPerson(t, st, id).Name)
	}
	all, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	// Rule 6 — the arrangement: every fixture person came back.
	seen := map[string]bool{}
	for _, p := range all {
		if _, ours := want[p.PersonID]; ours {
			seen[p.PersonID] = true
			check("Persons", p.PersonID, p.Name)
		}
	}
	if len(seen) != len(ids) {
		t.Fatalf("fixture defect: Persons() returned %d of the %d fixture people", len(seen), len(ids))
	}

	// The muster is the read path an evacuation is run from, so it gets its
	// own pass rather than riding on Persons().
	board := MusterLive(w5aOpenVisitsFor(ids), all, nil, nil, AliasMap{}, AliasMap{}, 0)
	got := map[string]string{}
	for _, c := range board.Companies {
		for _, wg := range c.Trades {
			for _, m := range wg.Members {
				got[m.PersonID] = m.PersonName
			}
		}
	}
	if len(got) != len(ids) {
		t.Fatalf("fixture defect: the muster board holds %d of the %d fixture people, so it cannot be posing this case", len(got), len(ids))
	}
	for id, name := range got {
		check("MusterLive", id, name)
	}
}

// w5aOpenVisitsFor builds one OPEN visit per person, all in the same
// (no company, no workgroup) bucket so ordering within the bucket is
// observable.
func w5aOpenVisitsFor(personIDs []string) []Visit {
	visits := make([]Visit, 0, len(personIDs))
	for i, id := range personIDs {
		visits = append(visits, Visit{
			VisitID: "w5a-visit-" + strconv.Itoa(i), SiteID: "site-a",
			PersonID: id, IsOpen: 1,
		})
	}
	return visits
}

// =======================================================================
// W5a-9 — Position is nullable free text
// =======================================================================

// TestW5aPositionIsFreeTextRoundTripped — W5a-9's behaviour half. D-21.4:
// "nullable, free text" — no enum, no validation against a list.
//
// The criterion is ⟦NO PRE-CHUNK RED⟧ and its booked evidence is the planted
// enum in TestW5aPositionIsFreeTextWithNoTaxonomy (internal/store), NOT this
// pass. This round trip reds pre-chunk only incidentally, because the created
// half is unbuilt.
//
// EXPECTED RED (m5a-9): Position validated against an enum — any value below
// refused, or normalised on the way through.
func TestW5aPositionIsFreeTextRoundTripped(t *testing.T) {
	st := w5aNewStore(t)
	values := []string{
		"Foreman",
		"night-shift lead / relief operator", // punctuation
		"  Crane   op  ",                     // interior and edge whitespace, kept verbatim
		"Ingénieur — équipe nord",            // non-ASCII, em dash
		strings.Repeat("supervisor of ", 30), // long
		"'; DROP TABLE person; --",           // a value no taxonomy would allow
	}
	for i, v := range values {
		id := "site-b-position-" + strconv.Itoa(i)
		w5aApply(t, st, w5aWirePersonCreated(t, "site-b", 1, int64(i+1), w5aBase+int64(i)*w5aHour,
			id, "Position, Case"+strconv.Itoa(i), map[string]any{w5aKeyPosition: v}))
		p := w5aPerson(t, st, id)
		if p.Position == nil {
			t.Errorf("W5a-9 (m5a-9): position %q was dropped — free text means every value survives", v)
			continue
		}
		if *p.Position != v {
			t.Errorf("W5a-9 (m5a-9): position round-tripped as %q, want %q byte-identical — normalising is a taxonomy by another name", *p.Position, v)
		}
	}
	// Nullable: a person with no position composes as ABSENT.
	w5aApply(t, st, w5aWirePersonCreated(t, "site-b", 1, int64(len(values)+1), w5aBase,
		"site-b-position-none", "NoPosition, Person", nil))
	if p := w5aPerson(t, st, "site-b-position-none"); p.Position != nil {
		t.Errorf("W5a-9: a person with no position composed Position = %q, want absent (D-21.4: nullable)", *p.Position)
	}
}

// =======================================================================
// W5a-4 — every person-name sort reads Name (the TWO DOMAIN sites)
// =======================================================================

// THE NINE person-name ordering statements, MEASURED at 88dd6a6 across five
// files. They share NO comparator — there is no lessPersonName helper; each
// is an inline closure. This list is HAND-KEPT and is stated because nothing
// structural detects an unenumerated one.
//
//	1. internal/domain/muster.go:232  bucket members, by PersonName   <- here
//	2. internal/domain/muster.go:255  board.Summary, by PersonName    <- here
//	3. internal/web/gate.go:343       on-site rows, by row.Person
//	4. internal/web/gate.go:369       gate roster, by Person.Name
//	5. internal/web/trips.go:413      sortedPersons, by Person.Name
//	6. internal/web/intake.go:367     roster People, by Display
//	7. internal/web/intake.go:368     NeedsCompletion, by Display
//	8. internal/web/intake.go:371     Unconfirmed, by Display
//	9. internal/web/picker.go:211     picker rows, by Name (tiebreak Value)
//
// Sites 3–9 are asserted in internal/web/w5a_name_sort_test.go.
//
// ⚠ Collapsing the nine onto one shared comparator would also satisfy W5a-4
// and is a perfectly good build. It is an OPTION, not an obligation — a
// criteria lock does not prescribe the build (arbiter §2.5).
//
// ⚠ D-36/J-12's case-folded surname sort with a PersonID tiebreak is
// deliberately NOT asserted here: it is STRUCK from W5a-4 by the lock (M13)
// and recorded as a HOMELESS obligation in the Seams table. Measured, 0 of
// the 9 sorts case-fold and 1 of 9 carries a tiebreak, on Value rather than
// PersonID. Asserting it would make W5a-4 permanently red for another
// chunk's unbuilt work.

// w5aSortNames is the fixture's two Names, and w5aSortLastNames the two
// LastNames that DISAGREE with them. Sorting by Name gives [A, B]; sorting
// by LastName gives [B, A].
const (
	w5aSortNameA = "Aaron, Zed"
	w5aSortNameB = "Zeta, Ann"
	w5aSortLastA = "Zulu"  // disagrees with "Aaron"
	w5aSortLastB = "Alpha" // disagrees with "Zeta"
)

// w5aExpectedNameOrder is the CERTIFIED order, pinned as a LITERAL so a
// permutation-invariant-but-wrong comparator cannot pass.
var w5aExpectedNameOrder = []string{w5aSortNameA, w5aSortNameB}

// w5aSortFixture holds the two people and their ids in fixture order.
type w5aSortFixture struct {
	idA, idB string
}

// w5aBuildSortFixture creates the two people and amends their parts. With
// degenerate=true the parts AGREE with the Names, which is the deliberately
// degenerate fixture the self-check's positive control needs.
//
// The parts arrive by AMENDMENT rather than at creation, deliberately: the
// amendment path exists at 88dd6a6, so this criterion's fixture does not
// depend on W5a-2 landing and the sort assertion means the same thing before
// and after the build.
func w5aBuildSortFixture(t *testing.T, st *store.Store, degenerate bool) w5aSortFixture {
	t.Helper()
	lastA, lastB := w5aSortLastA, w5aSortLastB
	if degenerate {
		lastA, lastB = "Aaron", "Zeta"
	}
	var fx w5aSortFixture
	for _, c := range []struct {
		name, last string
		dst        *string
	}{{w5aSortNameA, lastA, &fx.idA}, {w5aSortNameB, lastB, &fx.idB}} {
		p, err := CreatePerson(st, "site-a", 1, c.name, nil, nil, 0, "default")
		if err != nil {
			t.Fatalf("fixture defect: creating %q: %v", c.name, err)
		}
		if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{
			PersonID: p.PersonID, LastName: w5aStr(c.last),
		}, 0, "default"); err != nil {
			t.Fatalf("fixture defect: amending %q: %v", c.name, err)
		}
		*c.dst = p.PersonID
	}
	return fx
}

// w5aReadBackSortPeople returns the two fixture people as READ BACK from the
// store — never the fixture constants, which is the whole point of the
// self-check.
func w5aReadBackSortPeople(t *testing.T, st *store.Store, fx w5aSortFixture) []Person {
	t.Helper()
	all, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	var out []Person
	for _, p := range all {
		if p.PersonID == fx.idA || p.PersonID == fx.idB {
			out = append(out, p)
		}
	}
	if len(out) != 2 {
		t.Fatalf("fixture defect: read back %d of the 2 fixture people", len(out))
	}
	return out
}

// w5aRivalOrderAgrees recomputes the LastName ordering FROM READ-BACK DATA
// and reports whether it equals the certified Name ordering. A non-empty
// `defect` means the fixture cannot pose the case at all.
func w5aRivalOrderAgrees(people []Person) (agrees bool, defect string) {
	if len(people) < 2 {
		return false, "the sort fixture holds fewer than two people"
	}
	byName := append([]Person{}, people...)
	sort.Slice(byName, func(i, j int) bool { return byName[i].Name < byName[j].Name })
	byLast := append([]Person{}, people...)
	for _, p := range byLast {
		if p.LastName == nil {
			return false, "person " + p.Name + " has no last_name read back, so the LastName rival cannot be computed"
		}
	}
	sort.Slice(byLast, func(i, j int) bool { return *byLast[i].LastName < *byLast[j].LastName })
	var a, b []string
	for i := range byName {
		a = append(a, byName[i].PersonID)
		b = append(b, byLast[i].PersonID)
	}
	return strings.Join(a, "|") == strings.Join(b, "|"), ""
}

// w5aRequireNonDegenerateSortFixture is the runtime self-check. It fails as
// an explicit FIXTURE DEFECT — never as a criterion failure — when the
// certified rule (sort by Name) and its named rival (sort by LastName) would
// return the same answer.
func w5aRequireNonDegenerateSortFixture(t *testing.T, people []Person) {
	t.Helper()
	agrees, defect := w5aRivalOrderAgrees(people)
	if defect != "" {
		t.Fatalf("fixture defect: %s", defect)
	}
	if agrees {
		t.Fatalf("fixture defect: sorting by Name and sorting by LastName return the SAME order on this fixture, so a build that keyed the sort on LastName would pass. Standing rule 3: a criterion certifying WHICH rule is in force needs a fixture on which the certified rule and every named rival disagree")
	}
}

// TestW5aSortFixtureSelfCheckPositiveControl — the self-check's own positive
// control (M3/F-9: "the self-check is itself a check and needs a positive
// control"). A deliberately degenerate fixture — parts AGREEING with Name —
// is built for real, read back for real, and the fixture-defect verdict is
// OBSERVED, before either sort test's pass counts.
//
// Without this, a self-check that could never fire would make every
// "non-degenerate" claim below true by construction — standing rule 5's exact
// shape.
func TestW5aSortFixtureSelfCheckPositiveControl(t *testing.T) {
	st := w5aNewStore(t)
	fx := w5aBuildSortFixture(t, st, true /* degenerate */)
	people := w5aReadBackSortPeople(t, st, fx)

	agrees, defect := w5aRivalOrderAgrees(people)
	if defect != "" {
		t.Fatalf("CONTROL FAILED: the degenerate fixture could not even be evaluated: %s", defect)
	}
	if !agrees {
		t.Fatalf("CONTROL FAILED: the self-check did NOT flag a deliberately degenerate fixture (parts agreeing with Name) — it can never fire, so W5a-4's non-degeneracy claim would be true by construction (M3)")
	}

	// And the mirror: the REAL fixture must not be flagged, or the check
	// flags everything and is equally useless.
	st2 := w5aNewStore(t)
	fx2 := w5aBuildSortFixture(t, st2, false)
	agrees2, defect2 := w5aRivalOrderAgrees(w5aReadBackSortPeople(t, st2, fx2))
	if defect2 != "" || agrees2 {
		t.Fatalf("CONTROL FAILED: the self-check flagged the NON-degenerate fixture (agrees=%v defect=%q)", agrees2, defect2)
	}
}

// TestW5aMusterSortsReadNameNotLastName — W5a-4 at sort sites 1 and 2
// (muster.go:232, the per-workgroup bucket; muster.go:255, the flat summary
// bucket). D-48 derived ruling 2: "the sort reads Name, never LastName."
//
// ⟦NO PRE-CHUNK RED⟧ — all nine sorts read Name at 88dd6a6 because no part is
// readable there. **The clean pass is not evidence.** The ledger row is
// (m5a-4) PRODUCED AT EACH of the nine sites: a mutant planted at any one of
// the other eight survives a test that pins only one surface (H13).
//
// EXPECTED RED (m5a-4): the bucket comparator keyed on LastName — the
// plausible "improvement" once the column exists — orders the two fixture
// people [Zeta, Ann; Aaron, Zed], and this test's pinned literal reds. Under
// that mutant two officers reading the same muster find the same person in
// different places.
func TestW5aMusterSortsReadNameNotLastName(t *testing.T) {
	st := w5aNewStore(t)
	fx := w5aBuildSortFixture(t, st, false)
	people := w5aReadBackSortPeople(t, st, fx)
	w5aRequireNonDegenerateSortFixture(t, people)

	all, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	visits := w5aOpenVisitsFor([]string{fx.idA, fx.idB})

	// --- SITE 1: muster.go:232, members within a workgroup bucket.
	// minCrewSize 0 keeps both people in the grouped section.
	board := MusterLive(visits, all, nil, nil, AliasMap{}, AliasMap{}, 0)
	var grouped []string
	for _, c := range board.Companies {
		for _, wg := range c.Trades {
			for _, m := range wg.Members {
				grouped = append(grouped, m.PersonName)
			}
		}
	}
	// Rule 6 — the arrangement: both fixture people are in the GROUPED
	// section, not folded into Summary, or site 1 is not being exercised.
	if len(grouped) != 2 || len(board.Summary) != 0 {
		t.Fatalf("fixture defect: sort site 1 (muster.go:232) needs both people in the grouped section; got %d grouped, %d in Summary", len(grouped), len(board.Summary))
	}
	w5aAssertNameOrder(t, "muster.go:232 (workgroup bucket members)", grouped)

	// --- SITE 2: muster.go:255, the flat summary bucket. A minCrewSize
	// above the crew size folds them there, which is the ONLY way to reach
	// that comparator.
	board = MusterLive(visits, all, nil, nil, AliasMap{}, AliasMap{}, 99)
	var summary []string
	for _, m := range board.Summary {
		summary = append(summary, m.PersonName)
	}
	if len(summary) != 2 || len(board.Companies) != 0 {
		t.Fatalf("fixture defect: sort site 2 (muster.go:255) needs both people folded into Summary; got %d in Summary, %d company groups", len(summary), len(board.Companies))
	}
	w5aAssertNameOrder(t, "muster.go:255 (flat summary bucket)", summary)
}

// w5aAssertNameOrder pins the certified order as a LITERAL at one site.
func w5aAssertNameOrder(t *testing.T, site string, got []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(w5aExpectedNameOrder, "|") {
		t.Errorf("W5a-4 (m5a-4) at %s: order = %v, want %v. Every person-name sort reads the composed Name, never LastName — a part may diverge from what is printed on the page, and ordering the roster by an invisible field puts the same person in different places on two officers' screens (D-48 derived ruling 2)", site, got, w5aExpectedNameOrder)
	}
}

// =======================================================================
// W5a-7 — the SECOND DOOR (D-52 / M15): gate add-on-demand
// =======================================================================

// TestW5aGateAddOnDemandNeedsNoNameParts is the test that discriminates
// (m5a-7)'s `createPerson` arm, and it is the reason that arm was added to
// the mutant's shape at all.
//
// "The intake edge" is ONE OF TWO DOORS. `person_created` is authored locally
// from `web/intake.go:567` AND from `domain/checkin.go:291` (gate
// add-on-demand), both through `CreatePersonRecordedBy` -> `createPerson`
// (refdata.go:327/:331). A requiredness check placed in `createPerson` sits
// ABOVE the fold and BELOW the edge: it would break gate add-on-demand while
// passing every intake-only test. This test is the one that goes red.
//
// ⟦NO PRE-CHUNK RED⟧ — add-on-demand works today and asks for no parts.
// **The clean pass is not evidence.** Its ledger row is (m5a-7).
//
// EXPECTED RED (m5a-7): the parts requirement moved into
// createPerson/CreatePersonRecordedBy — GateCheckIn returns
// "domain: gate check-in: add-on-demand person: …" and the person standing at
// the gate cannot be checked in at all.
func TestW5aGateAddOnDemandNeedsNoNameParts(t *testing.T) {
	st := w5aNewStore(t)
	before := w5aPersonCount(t, st)

	// Rule 6 — the arrangement: this really is the add-on-demand path, i.e.
	// the slot names a person who is NOT on the roster, so createPerson is
	// reached. A slot with an ExistingID would exercise nothing.
	//
	// The walk-in's name carries NO COMMA, deliberately, and the first draft
	// of this fixture had to be corrected: the planted (m5a-7) — a parts
	// requirement in createPerson, spelled as "the name must contain a
	// comma" — SURVIVED a comma-bearing fixture. It is also the truthful
	// shape: composeName's own doc says rows created at the gate rather than
	// at intake stay "First Last", and every existing gate fixture in this
	// repo uses that form ("Dana Driver", "Pat Passenger").
	const walkIn = "Wally Walkin"
	all, err := Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range all {
		if p.Name == walkIn {
			t.Fatalf("fixture defect: %q is already on the roster, so this is not the add-on-demand path", walkIn)
		}
	}

	if _, _, err := GateCheckIn(st, "site-a", 1, GateCheckInInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: "gate walk-in","⟧
		VehicleID: w7SeedVehicleID(t, st, "site-a", "gate walk-in"),
		Driver:    PersonSlot{NewName: walkIn},
	}, 0, "default"); err != nil {
		t.Fatalf("W5a-7 (m5a-7): gate add-on-demand REFUSED a person with no name parts: %v. Requiredness lives at the INTAKE edge — a check in createPerson/CreatePersonRecordedBy sits above the fold and below the edge, breaks the gate's second door (D-52), and passes every intake-only test", err)
	}

	if after := w5aPersonCount(t, st); after != before+1 {
		t.Fatalf("W5a-7: gate add-on-demand authored %d person rows, want 1", after-before)
	}
	all, err = Persons(st)
	if err != nil {
		t.Fatal(err)
	}
	var found *Person
	for i := range all {
		if all[i].Name == walkIn {
			found = &all[i]
		}
	}
	if found == nil {
		t.Fatalf("W5a-7: gate add-on-demand did not put %q on the roster", walkIn)
	}
	for _, c := range []struct {
		field string
		got   *string
	}{{"LastName", found.LastName}, {"FirstName", found.FirstName}, {"Position", found.Position}} {
		if c.got != nil {
			t.Errorf("W5a-7: gate add-on-demand invented %s = %q for a walk-in whose name was typed as one string — nothing composes parts out of a Name (W5a-8), and the gate never asked for them", c.field, *c.got)
		}
	}
}
