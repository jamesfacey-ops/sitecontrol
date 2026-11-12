// w5c_seams_test.go — W5c's fixture machinery and the SEAMS it binds. NO
// ASSERTIONS LIVE HERE; every criterion is asserted in one of:
//
//	w5c_value_domain_test.go    W5c-1 (the edge, the wire, the revocation)
//	w5c_created_half_test.go    W5c-4 (payload shape + the fold)
//	w5c_no_consultation_test.go W5c-2 (b) (the non-degenerate fixture)
//
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31.
//
// ⟦SchemaVersion 11 — CORRECTED, NOT DELETED, 2026-09-05⟧ `pad_oriented` is
// STRUCK from person_created, person_amended, `person` and `person_amendment`
// ([[D-88]] + ERRATA, [[D-91]]: one flag on the ENROLLING site's row, named
// after a site, cannot hold a per-site orientation; D-88 reverses D-10 and
// D-21.1, which put it there). Every W5c criterion is re-pointed at the NEW
// shape — the ABSENCE of the fact and the inertness of its stale key on the
// wire — never weakened and never removed (the DECLARED-REDS rule). The
// spellings below are therefore kept as the spellings of a STRUCK fact: the
// wire fixtures plant them as a stale key a divergent peer might still send,
// and the assertions prove that nothing on this post can author, store,
// compose or render them. Per-site orientation is re-built in a later stage
// of this same increment as the ORIENTING post's own record.
//
// ⚠ THIS FILE NAMES NO UNBUILT GO SYMBOL, and neither does any W5c domain
// file. W5c's build adds `PadOriented` to `PersonCreated` and threads a
// value through `createPerson`'s shared signature (W5-X3's collision list,
// refdata.go:319–334). A test that named `PersonCreated.PadOriented` as a
// selector, or guessed the new authoring signature, would (a) compile-block
// the whole domain package and bury every other red behind one error, and
// (b) bind a signature seam a test-writer cannot know. Every created-half
// fixture here is therefore a WIRE-RECEIVED ENVELOPE (w5cWirePersonCreated +
// store.Apply) or a REFLECTION CENSUS. Consequence: these files compile and
// run against `88dd6a6`, and every pre-chunk failure is a produced RUNTIME
// red naming its criterion. W5a's and W5b's writers made the same call for
// the same reason.
//
// --- SEAMS bound here, once; disputes go to the gate --------------------
//
// SEAM W5c-S-FACT — the fact's spellings: Go field `PadOriented`, wire key
// and column `pad_oriented`. NOT a free choice: W3 landed both on the
// amendment side (person_amend.go:66, :149) and W5-X4(b) names the column
// spelling verbatim. The created half is ruled to match by DR-8.
//
// SEAM W5c-S-TYPE — `PersonCreated.PadOriented` is `*string`, tagged
// `,omitempty`. NOT a free choice and NOT this writer's: DR-8 rules it in
// terms ("PersonCreated.PadOriented is *string, matching PersonAmended"),
// and (m5c-5) is registered against `*bool` precisely because a bool makes
// `false` REPRESENTABLE and reopens W5c-1 through the created half.
// `,omitempty` is the house spelling of "nullable" on every optional payload
// field (refdata.go:37–44) and is what makes "absent" a wire fact rather
// than a null.
//
// SEAM W5c-S-DOMAIN — the value domain is `{"true", absent}`, and the ONLY
// authorable value is the exact lowercase four-byte string `true`. DR-8
// again, verbatim: "D-21.1 is honoured by the value domain — {"true",
// absent} — and by the absence of a keyed table, not by the Go type."
//
// SEAM W5c-S-FALSY — a FALSY input must not compose to `"true"`. See
// w5cFalsyValues for the reasoning and for the alternative reading it
// declines; this is the one place in W5c where the criterion's letter and
// its evident meaning could come apart, and it is recorded rather than
// resolved silently.
package domain

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// --- the fact's spellings, as literals -----------------------------------

const (
	w5cKeyPadOriented     = "pad_oriented" // SEAM W5c-S-FACT (wire + column) — the STRUCK spelling, planted as a stale key
	w5cGoFieldPadOriented = "PadOriented"  // SEAM W5c-S-TYPE, consulted BY REFLECTION only — must be ABSENT since SchemaVersion 11
	w5cGoTypePadOriented  = "*string"      // SEAM W5c-S-TYPE (DR-8) — ⟦was⟧ the declared type; kept as the record of what was struck
	w5cOriented           = "true"         // SEAM W5c-S-DOMAIN: ⟦was⟧ the ONE authorable value; now the stale value the fixtures plant
	w5cEvtPersonCreated   = "person_created"
	w5cEvtPersonAmended   = "person_amended"
	w5cKeyClear           = "clear"
	// w5cSurvivingFact is a neighbouring amendable fact that DID survive the
	// strike. The mirror arms drive it so "cannot author pad orientation" is
	// never satisfied by an edge or a form that can author nothing at all.
	w5cSurvivingFact = "position"
)

const w5cBase = int64(1_800_000_000_000) // a fixed audit "now"

func w5cStr(s string) *string { return &s }

// w5cForbiddenValues is the corpus W5c-1 asserts over. It is deliberately
// wider than the five values the criterion enumerates ("false", "", "0",
// "no" … "or any other value"), and it mixes shapes a form, a raw POST and a
// careless normaliser would each produce.
var w5cForbiddenValues = []string{
	"false", "False", "FALSE", "0", "no", "No", "off", "n",
	"", " ", "null", "nil", "yes", "1", "x", "true false", "TRUE", "True",
	" true", "true ", "oriented",
}

// w5cFalsyValues is the sub-corpus that MEANS "not oriented".
//
// ⚠ SEAM W5c-S-FALSY, and it is the one reading I had to make. W5c-1's
// letter is "only `true` and absent are ever authored", which a build that
// COERCED "false" to "true" would satisfy — while recording the exact
// opposite of what the officer said. The criterion's own next clause is that
// revocation is `Clear:["pad_oriented"]` composing to ABSENT, so absent is
// what "not oriented" means in this system and coercion to `true` cannot be
// the intended reading. Asserted as: after a falsy assertion the composed
// value is nil. The declined alternative — asserting only the letter — is
// recorded so the gate can overrule this rather than discover it.
var w5cFalsyValues = []string{"false", "False", "FALSE", "0", "no", "No", "off", "n"}

// --- store machinery ------------------------------------------------------

func w5cReading(wallMS int64) store.Clock {
	return func() store.ClockReading {
		return store.ClockReading{WallMS: wallMS, MaxErrorUS: 42, State: store.ClockSynced}
	}
}

// w5cNewStore opens a registered store with the clock parked at w5cBase, so
// nothing in a fixture varies with wall time.
func w5cNewStore(t *testing.T) *store.Store {
	t.Helper()
	st := newDomainTestStore(t)
	st.Clock = w5cReading(w5cBase)
	return st
}

// --- wire envelopes -------------------------------------------------------

func w5cEnvelope(t *testing.T, siteID string, epoch, seq, authoredAtMS int64, payload map[string]any) store.Envelope {
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

// w5cWirePersonCreated hand-builds a `person_created` envelope arriving from
// another post. A key ABSENT from `fields` is a genuinely absent key on the
// wire — which is the whole distinction W5c's value domain turns on.
func w5cWirePersonCreated(t *testing.T, siteID string, epoch, seq, authoredAtMS int64, personID, name string, fields map[string]any) store.Envelope {
	t.Helper()
	payload := map[string]any{"type": w5cEvtPersonCreated, "person_id": personID, "name": name}
	for k, v := range fields {
		payload[k] = v
	}
	return w5cEnvelope(t, siteID, epoch, seq, authoredAtMS, payload)
}

// w5cWirePersonAmended hand-builds a `person_amended` envelope arriving from
// another post. `clear` is written under the lowercase wire key (P-3 §7 rev
// 3, person_amend.go:47–49) and only when non-empty.
func w5cWirePersonAmended(t *testing.T, siteID string, epoch, seq, authoredAtMS int64, personID string, fields map[string]any, clear []string) store.Envelope {
	t.Helper()
	payload := map[string]any{"type": w5cEvtPersonAmended, "person_id": personID}
	for k, v := range fields {
		payload[k] = v
	}
	if len(clear) > 0 {
		payload[w5cKeyClear] = clear
	}
	return w5cEnvelope(t, siteID, epoch, seq, authoredAtMS, payload)
}

// w5cApply applies one wire envelope and REQUIRES it to land. A refusal here
// is itself a red: the fold NEVER refuses (W3-2), and W5c-1's asymmetry
// clause says so in terms — the value-domain guard is PRE-APPEND at the
// authoring edge and the wire path is unchanged. Refusing at fold time forks
// the two posts' logs permanently.
func w5cApply(t *testing.T, st *store.Store, env store.Envelope, criterion string) {
	t.Helper()
	applied, err := st.Apply(env)
	if err != nil {
		t.Fatalf("%s: THE FOLD REFUSED %v: %v — W5c-1's asymmetry is explicit: constraining the edge NARROWS an already-shipped authoring surface, so the refusal is PRE-APPEND and the WIRE PATH IS UNCHANGED. A wire-received value is recorded as it arrives. The guard must not be extended into the fold.", criterion, env.Key(), err)
	}
	if !applied {
		t.Fatalf("fixture defect: apply %v deduped — that seq is already in the log", env.Key())
	}
}

// --- read-back ------------------------------------------------------------

// w5cComposed fetches the COMPOSED person (base row + every amendment folded
// in ascending key order) or fails.
func w5cComposed(t *testing.T, st *store.Store, id string) *Person {
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

// w5cRawPersonRow returns the RAW `person` projection row — the only read
// that can tell "the CREATED-half fold WROTE the column" from "composition
// supplied the value from an amendment". W5c-4 needs exactly that
// discrimination.
func w5cRawPersonRow(t *testing.T, st *store.Store, id string) Person {
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

// w5cStreamLen is the number of events on this post's own stream. Author
// appends only at watermark+1, so the watermark IS the length.
func w5cStreamLen(t *testing.T, st *store.Store, siteID string, epoch int64) int64 {
	t.Helper()
	wm, err := st.Watermark(siteID, epoch)
	if err != nil {
		t.Fatalf("watermark: %v", err)
	}
	return wm
}

// w5cAuthoredPadValues reads THE LOG — not a projection — and returns every
// `pad_oriented` value carried by an event on this post's own stream, with
// the key of the event that carried it.
//
// This is the assertion surface W5c-1 actually names: "only `true` and
// absent are ever AUTHORED". A projection read cannot tell an authored value
// from a composed one, and cannot see a value that was authored and later
// cleared. The log can.
func w5cAuthoredPadValues(t *testing.T, st *store.Store, siteID string, epoch int64) map[string]string {
	t.Helper()
	out := map[string]string{}
	envs, err := st.Events(siteID, epoch, 0, 10_000)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	for _, env := range envs {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(env.Payload, &raw); err != nil {
			continue // not an object-shaped payload
		}
		r, ok := raw[w5cKeyPadOriented]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(r, &s); err != nil {
			s = string(r) // a non-string value: report it as its JSON text
		}
		out[env.Key().String()] = s
	}
	return out
}

// w5cTableColumns lists a projection table's columns by raw introspection
// (pragma_table_info over the exported *sql.Tx seam) — the read that can tell
// "the column is gone" from "the column is empty", which is the distinction
// every SchemaVersion-11 W5c assertion turns on.
func w5cTableColumns(t *testing.T, st *store.Store, table string) []string {
	t.Helper()
	var cols []string
	err := st.LocalTx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				return err
			}
			cols = append(cols, c)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("introspecting %s: %v", table, err)
	}
	if len(cols) == 0 {
		t.Fatalf("fixture defect: table %s has no columns — the store is not registered", table)
	}
	return cols
}

func w5cHasColumn(cols []string, name string) bool {
	for _, c := range cols {
		if c == name {
			return true
		}
	}
	return false
}

// w5cJSONKeys returns the top-level keys of v's JSON encoding — the byte-level
// question "does this record carry the key at all", which a Go selector can
// no longer even ask once the field is struck.
func w5cJSONKeys(t *testing.T, v any) map[string]bool {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("not an object: %s", b)
	}
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

// w5cRequireStruckShape is the PRECONDITION GUARD (standing rule 6) every
// SchemaVersion-11 W5c assertion needs: the Go field is gone from BOTH
// payload structs and the authoring input, and the column is gone from BOTH
// tables. Without it, "a stale key lands nowhere" would be trivially true of
// a tree where the key still has somewhere to land — the old green, read as
// the new one.
func w5cRequireStruckShape(t *testing.T, st *store.Store, criterion string) {
	t.Helper()
	for _, typ := range []reflect.Type{reflect.TypeOf(PersonCreated{}), reflect.TypeOf(PersonAmended{}), reflect.TypeOf(PersonAmendment{}), reflect.TypeOf(Person{})} {
		if _, ok := typ.FieldByName(w5cGoFieldPadOriented); ok {
			t.Fatalf("PRECONDITION for %s: %s still declares a %s field — the SchemaVersion-11 strike ([[D-88]] ERRATA) has not landed, so this scenario cannot pose its case", criterion, typ.Name(), w5cGoFieldPadOriented)
		}
		for i := 0; i < typ.NumField(); i++ {
			if strings.HasPrefix(typ.Field(i).Tag.Get("json"), w5cKeyPadOriented) {
				t.Fatalf("PRECONDITION for %s: %s.%s still carries the json key %q", criterion, typ.Name(), typ.Field(i).Name, w5cKeyPadOriented)
			}
		}
	}
	for _, table := range []string{"person", "person_amendment"} {
		if w5cHasColumn(w5cTableColumns(t, st, table), w5cKeyPadOriented) {
			t.Fatalf("PRECONDITION for %s: `%s` still carries the %q column — the strike has not landed", criterion, table, w5cKeyPadOriented)
		}
	}
}

// w5cPtrStr renders a *string for a failure message without a nil panic.
func w5cPtrStr(p *string) string {
	if p == nil {
		return "<absent>"
	}
	return "\"" + *p + "\""
}

func w5cSameStr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
