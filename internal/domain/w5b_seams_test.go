// w5b_seams_test.go — THE ONE place W5b's domain tests bind the Go and wire
// spellings the LOCKED criteria leave undetermined, plus the shared fixture
// machinery. This file holds NO assertions of its own.
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31,
// sub-chunk W5b (item 2 — the personal phone).
//
// ⚠ THIS FILE REFERENCES NO UNBUILT GO SYMBOL, DELIBERATELY. W5b's build
// changes `PersonCreated`'s shape and threads a value through the shared
// `createPerson` signature (W5-X3's collision list, `refdata.go:319–334`).
// A test that authored a person WITH a phone through a GUESSED Go signature
// would (a) compile-block the whole domain package, burying every other red
// behind one error, and (b) bind a signature seam a test-writer cannot know.
// Every created-half fixture below is therefore a WIRE-RECEIVED envelope
// (`w5bWirePersonCreated` + `store.Apply`), a REFLECTION census over
// `PersonCreated`, or a drive through the real HTTP intake edge (in
// `internal/web`). Consequence: every W5b file compiles and runs against
// `88dd6a6`, so each pre-chunk failure is a produced RUNTIME red naming its
// criterion, not a compile error.
//
// SHAPE PRESUMPTIONS (SEAMS), each bound once, disputes go to the gate:
//
//   - SEAM W5b-S-PAYLOAD: `person_created` gains the wire key `phone` —
//     lowercase, the spelling W3 already landed for the SAME fact on
//     `person_amended` (`person_amend.go:62`) and the spelling W5-X4(b)
//     names verbatim for the projection column. Near-DOC-pinned; recorded
//     as a seam only because this package asserts it as a literal.
//   - SEAM W5b-S-CREATED: the Go field on `domain.PersonCreated` is `Phone`,
//     typed `*string` with `,omitempty` — W5b-0's own words ("`PersonCreated`
//     carries `Phone` as nullable (`*string`, `omitempty`)").
//   - SEAM W5b-S-COL: the projection column is `person.phone`, `TEXT`,
//     nullable, no default — DOC-pinned by W5-X4(b), which names `phone`
//     (W5b) in the permitted union verbatim. Asserted in `internal/store`.
//   - SEAM W5b-S-GATEDOOR: the gate's add-on-demand door is reached through
//     the exported `GateCheckIn` with a `PersonSlot{NewName: …}` — the
//     unexported `resolvePersonSlot` (`checkin.go:291`) is D-52's measured
//     second door, and `GateCheckIn` is the only stable exported way in.
//     Its signature is frozen by ~30 existing call sites [MEASURED].
//
// NOT SEAMS — DOC-pinned or MEASURED, cited at each use: that a fold never
// refuses a wire-received event (W3-2, `person_amend.go:9–19`); that
// `Clear: ["phone"]` composes phone as ABSENT rather than empty-string
// (W3-3); that the amendable fact set already contains "phone"
// (`person_amend.go:78–81`).
package domain

import (
	"encoding/json"
	"reflect"
	"testing"

	"sitecontrol/internal/store"
)

// --- wire spellings (SEAM W5b-S-PAYLOAD) ---------------------------------

const (
	w5bEvtPersonCreated = "person_created"
	w5bEvtPersonAmended = "person_amended"
	w5bKeyPhone         = "phone"
	w5bKeyClear         = "clear"
)

// w5bGoFieldPhone is SEAM W5b-S-CREATED's Go spelling. Consulted by
// reflection only — never referenced as a selector — so this package
// compiles both before and after the builder lands the field.
const w5bGoFieldPhone = "Phone"

const w5bBase = int64(1_800_000_000_000) // a fixed audit "now"

func w5bStr(s string) *string { return &s }

// w5bReading parks the audit clock at wallMS.
func w5bReading(wallMS int64) store.Clock {
	return func() store.ClockReading {
		return store.ClockReading{WallMS: wallMS, MaxErrorUS: 42, State: store.ClockSynced}
	}
}

// w5bNewStore opens a registered store with the clock parked at w5bBase.
func w5bNewStore(t *testing.T) *store.Store {
	t.Helper()
	st := newDomainTestStore(t)
	st.Clock = w5bReading(w5bBase)
	return st
}

// --- wire envelopes ------------------------------------------------------

// w5bEnvelope wraps a payload map as an envelope arriving from `siteID`.
func w5bEnvelope(t *testing.T, siteID string, epoch, seq, authoredAtMS int64, payload map[string]any) store.Envelope {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	env := store.Envelope{
		SiteID: siteID, NodeEpoch: epoch, Seq: seq,
		SchemaVersion: store.SchemaVersion, OriginAuth: siteID,
		OccurredAt: authoredAtMS, TimeSource: "default",
		Payload: json.RawMessage(raw),
	}
	env.AuthoredAt.WallMS = authoredAtMS
	env.AuthoredAt.MaxErrorUS = 42
	env.AuthoredAt.State = store.ClockSynced
	return env
}

// w5bWirePersonCreated hand-builds a `person_created` envelope arriving from
// another post. `fields` carries the optional keys using SEAM W5b-S-PAYLOAD
// spellings; a key ABSENT from the map is a genuinely absent key on the wire,
// which is exactly what the phoneless fixture needs — the distinction
// between "absent" and "null" is the one W5b-3 turns on.
func w5bWirePersonCreated(t *testing.T, siteID string, epoch, seq, authoredAtMS int64, personID, name string, fields map[string]any) store.Envelope {
	t.Helper()
	payload := map[string]any{
		"type":      w5bEvtPersonCreated,
		"person_id": personID,
		"name":      name,
	}
	for k, v := range fields {
		payload[k] = v
	}
	return w5bEnvelope(t, siteID, epoch, seq, authoredAtMS, payload)
}

// w5bWirePersonAmended hand-builds a `person_amended` envelope arriving from
// another post. `clear` is written under the lowercase wire key (P-3 §7 rev
// 3, `person_amend.go:47–49`) and only when non-empty.
func w5bWirePersonAmended(t *testing.T, siteID string, epoch, seq, authoredAtMS int64, personID string, fields map[string]any, clear []string) store.Envelope {
	t.Helper()
	payload := map[string]any{
		"type":      w5bEvtPersonAmended,
		"person_id": personID,
	}
	for k, v := range fields {
		payload[k] = v
	}
	if len(clear) > 0 {
		payload[w5bKeyClear] = clear
	}
	return w5bEnvelope(t, siteID, epoch, seq, authoredAtMS, payload)
}

// w5bApply applies one wire envelope and REQUIRES it to land. A refusal here
// is itself a red: folds never refuse (W3-2) — refusing at fold time would
// fork the two posts' logs permanently, which is (m5b-3)'s and (m5b-4)'s
// shared shape.
func w5bApply(t *testing.T, st *store.Store, env store.Envelope, criterion string) {
	t.Helper()
	applied, err := st.Apply(env)
	if err != nil {
		t.Fatalf("%s: THE FOLD REFUSED %v: %v — a wire-received event folds clean whatever it carries or omits (W3-2). Mandatoriness is an EDGE property of one handler (D-52); a refusal here forks the two posts' logs permanently", criterion, env.Key(), err)
	}
	if !applied {
		t.Fatalf("fixture defect: apply %v deduped — that seq is already in the log", env.Key())
	}
}

// w5bComposed fetches the COMPOSED person (base row + every amendment folded
// in ascending key order) or fails.
func w5bComposed(t *testing.T, st *store.Store, id string) *Person {
	t.Helper()
	p, err := PersonByID(st, id)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatalf("person %s: no row", id)
	}
	return p
}

// w5bRawPersonRow returns the RAW `person` projection row (not the composed
// person) — the only read that can tell "the fold WROTE the column" from
// "composition supplied the value from an amendment". W5b-0's "the fold
// writes it" needs exactly this discrimination.
func w5bRawPersonRow(t *testing.T, st *store.Store, id string) Person {
	t.Helper()
	rows, err := dumpSection[Person](st, "person")
	if err != nil {
		t.Fatalf("reading the raw `person` projection: %v", err)
	}
	for _, r := range rows {
		if r.PersonID == id {
			return r
		}
	}
	t.Fatalf("fixture defect: no raw `person` projection row for %s (rows: %d)", id, len(rows))
	return Person{}
}

func w5bPersonCount(t *testing.T, st *store.Store) int {
	t.Helper()
	rows, err := dumpSection[Person](st, "person")
	if err != nil {
		t.Fatalf("reading the raw `person` projection: %v", err)
	}
	return len(rows)
}

// w5bShow renders a *string for a failure message, distinguishing nil
// (ABSENT) from the empty string — W3-3's load-bearing distinction.
func w5bShow(p *string) string {
	if p == nil {
		return "<absent>"
	}
	return "\"" + *p + "\""
}

// --- the W5b-0 precondition (standing rule 6) ----------------------------

// w5bPersonCreatedHasPhone reports whether `PersonCreated` carries the field
// SEAM W5b-S-CREATED names, WITHOUT naming it as a selector — so this file
// compiles on the pre-chunk tree.
func w5bPersonCreatedHasPhone() (present bool, typeName, jsonTag string) {
	f, ok := reflect.TypeOf(PersonCreated{}).FieldByName(w5bGoFieldPhone)
	if !ok {
		return false, "", ""
	}
	return true, f.Type.String(), f.Tag.Get("json")
}

// w5bRequireCreatedHalf is W5b-3's and W5b-4's PRECONDITION GUARD, and it is
// the reason those two criteria cannot book a degenerate pass.
//
// ⚠ THE LOCK'S OWN WARNING, MADE MECHANICAL. W5b-3 is "the purest degenerate
// criterion in the document": on the pre-chunk tree a phoneless wire person
// folds clean **because there is no phone field to omit** — the rule it
// certifies (nullable column, no stall) and the rival it excludes (no column
// at all) return the SAME answer on its own fixture. The lock therefore
// requires the fixture to be "run against a tree where `PersonCreated.Phone`
// and `person.phone` EXIST (i.e. after W5b-0)", with the pre-W5b-0 pass
// "explicitly not booked as evidence".
//
// A comment cannot enforce that. This does: the arrangement the scenario
// needs is CHECKED and fails loudly when it does not hold (standing rule 6),
// so the pre-W5b-0 state produces a PRECONDITION RED — never a green a gate
// could mistake for evidence.
//
// It proves BOTH halves by construction rather than by inspection: the Go
// field by reflection, and the projection column by authoring a phone over
// the wire and reading it back off the RAW row.
func w5bRequireCreatedHalf(t *testing.T, st *store.Store, criterion string) {
	t.Helper()
	present, typeName, tag := w5bPersonCreatedHasPhone()
	if !present {
		t.Fatalf("PRECONDITION NOT MET (%s, W5b-0): domain.PersonCreated has no %s field, so a phoneless fixture omits nothing and this criterion's pass would certify NOTHING — the lock's own words: it passes \"because there is no phone field to omit\". THIS RED IS THE PRE-CHUNK STATE, and it is NOT this criterion's ledger evidence; the evidence is the planted mutant, re-run once W5b-0 has landed",
			criterion, w5bGoFieldPhone)
	}
	if typeName != "*string" {
		t.Errorf("W5b-0 (SEAM W5b-S-CREATED): PersonCreated.%s is %s, want *string — nullable means a pointer, so ABSENT is distinguishable from empty-string (W3-3)", w5bGoFieldPhone, typeName)
	}
	if !w5bTagHasOmitempty(tag) {
		t.Errorf("W5b-0 (SEAM W5b-S-CREATED): PersonCreated.%s has json tag %q, want %q with `,omitempty` — without it a phoneless person marshals `\"phone\":null` and the absent/null distinction leaves the wire", w5bGoFieldPhone, tag, w5bKeyPhone)
	}

	// The column half, proved by behaviour: author a phone over the wire and
	// read it back off the RAW projection row. If the column is missing, or
	// the fold does not write it, this says so — and says which.
	const probeID = "w5b-precondition-probe"
	const probeValue = "555-PRECONDITION"
	if w5bRawRowPhoneAbsentFor(t, st, probeID, probeValue) {
		t.Fatalf("PRECONDITION NOT MET (%s, W5b-0): a wire `person_created` carrying %s=%q left the RAW `person` projection row with no phone — either the column does not exist or the fold does not write it. This criterion's pass on such a tree is not evidence (m5b-5)",
			criterion, w5bKeyPhone, probeValue)
	}
}

// w5bRawRowPhoneAbsentFor authors one probe person over the wire and reports
// whether the raw row came back without the phone.
func w5bRawRowPhoneAbsentFor(t *testing.T, st *store.Store, personID, phone string) bool {
	t.Helper()
	env := w5bWirePersonCreated(t, "site-probe", 1, w5bNextSeq(t, st, "site-probe", 1), w5bBase,
		personID, "Probe, Precondition", map[string]any{w5bKeyPhone: phone})
	w5bApply(t, st, env, "W5b-0 precondition probe")
	raw := w5bRawPersonRow(t, st, personID)
	return raw.Phone == nil || *raw.Phone != phone
}

// w5bNextSeq returns the next free seq on (siteID, epoch) so a fixture can
// add envelopes to a stream another fixture already used. Apply inserts only
// at watermark+1, so guessing wrongly is a silent dedupe, not a failure —
// which is precisely how a fixture stops posing its case.
func w5bNextSeq(t *testing.T, st *store.Store, siteID string, epoch int64) int64 {
	t.Helper()
	wm, err := st.Watermark(siteID, epoch)
	if err != nil {
		t.Fatalf("watermark(%s, %d): %v", siteID, epoch, err)
	}
	return wm + 1
}

func w5bTagHasOmitempty(tag string) bool {
	for _, part := range w5bSplitTag(tag) {
		if part == "omitempty" {
			return true
		}
	}
	return false
}

func w5bSplitTag(tag string) []string {
	var out []string
	cur := ""
	for _, r := range tag {
		if r == ',' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}
