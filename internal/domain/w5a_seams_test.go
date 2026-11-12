// w5a_seams_test.go — THE ONE place W5a's domain tests bind the Go and wire
// spellings the LOCKED criteria leave undetermined, plus the shared fixture
// machinery. This file holds NO assertions of its own.
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31.
//
// ⚠ THIS FILE REFERENCES NO UNBUILT GO SYMBOL, AND THAT IS A DELIBERATE
// CHOICE, not an accident. W5a's build changes `PersonCreated`'s shape and
// threads three values through `createPerson`'s signature (W5-X3's collision
// list, `refdata.go:319–334`). A test that authored a person WITH parts
// through a guessed Go signature would compile-block the whole domain package
// and, worse, would bind a signature seam this writer cannot know. Every
// created-half fixture below is therefore built as a WIRE-RECEIVED envelope
// (`w5aWirePersonCreated` + `store.Apply`) or driven through the HTTP intake
// edge in `internal/web`, so:
//
//	(1) the tests COMPILE AND RUN against `88dd6a6` and their clean-tree
//	    failures are genuine PRODUCED RUNTIME REDS, not compile errors; and
//	(2) the builder is left free to choose the authoring signature, which is
//	    a build decision a criteria lock does not make (arbiter §2.5).
//
// The one exception is `CreatePerson`, called with its CURRENT signature in
// W5a-3's empty-name assertion. That is not a guess: ~50 existing non-W5a
// test call sites use it and builders may not edit tests, so the wrapper's
// shape is effectively frozen [MEASURED — `grep -c 'domain.CreatePerson('`
// over internal/web/*_test.go and internal/domain/*_test.go].
//
// SHAPE PRESUMPTIONS (SEAMS), each bound once, disputes go to the gate:
//
//   - SEAM W5a-S-PAYLOAD: `person_created` gains the wire keys
//     `last_name`, `first_name`, `position` — lowercase snake_case, the
//     spellings W3 already landed for the SAME three facts on
//     `person_amended` (`person_amend.go:60/:61/:67`) and the spellings
//     W5-X4(b) names verbatim for the projection columns. Near-DOC-pinned;
//     recorded as a seam only because this file asserts them as literals.
//   - SEAM W5a-S-CREATED: the Go field names on `domain.PersonCreated` are
//     `LastName`, `FirstName`, `Position`, typed `*string` with
//     `,omitempty` — W5a-2's own words ("nullable (`*string`, `omitempty`)").
//   - SEAM W5a-S-DIVERGE: a wire-received `person_created` whose parts do
//     not compose to its `name` FOLDS CLEAN and is composed as it arrived.
//     This is W5a-5's M18(i) construction: under W5a-8 composition is
//     one-way at our own edge, so a locally-authored divergent creation is
//     unconstructible and the lawful fixture is a wire arrival from another
//     post — which W3-2's folds-never-refuse rule already requires the fold
//     to accept.
//
// NOT seams (DOC-pinned, cited at each point of use): the projection column
// names (W5-X4(b)); `Name` authoritative and parts divergent (D-48);
// `Position` nullable free text (D-21.4); the nine sort sites and the four
// person-name compare sites (the lock's own enumerations, re-measured here).
package domain

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// --- wire spellings (SEAM W5a-S-PAYLOAD) -------------------------------

const (
	w5aEvtPersonCreated = "person_created"
	w5aKeyLastName      = "last_name"
	w5aKeyFirstName     = "first_name"
	w5aKeyPosition      = "position"
)

// w5aPartKeys is the three keys W5a adds, in the order W5-X4(b) names them.
var w5aPartKeys = []string{w5aKeyLastName, w5aKeyFirstName, w5aKeyPosition}

const w5aBase = int64(1_800_000_000_000) // w3Base's twin: a fixed audit "now"
const w5aHour = int64(3_600_000)

func w5aStr(s string) *string { return &s }

// w5aReading parks the audit clock at wallMS.
func w5aReading(wallMS int64) store.Clock {
	return func() store.ClockReading {
		return store.ClockReading{WallMS: wallMS, MaxErrorUS: 42, State: store.ClockSynced}
	}
}

// w5aNewStore opens a registered store with the clock parked at w5aBase.
func w5aNewStore(t *testing.T) *store.Store {
	t.Helper()
	st := newDomainTestStore(t)
	st.Clock = w5aReading(w5aBase)
	return st
}

// w5aWirePersonCreated hand-builds a `person_created` envelope arriving from
// another post (SEAM W5a-S-DIVERGE). `fields` carries the optional keys using
// SEAM W5a-S-PAYLOAD spellings; a nil/absent key is a genuinely absent key on
// the wire, which is what the partless fixture needs.
func w5aWirePersonCreated(t *testing.T, siteID string, epoch, seq, authoredAtMS int64, personID, name string, fields map[string]any) store.Envelope {
	t.Helper()
	payload := map[string]any{
		"type":      w5aEvtPersonCreated,
		"person_id": personID,
		"name":      name,
	}
	for k, v := range fields {
		payload[k] = v
	}
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

// w5aApply applies one wire envelope and requires it to land. A refusal here
// is itself a red: folds never refuse (W3-2), and W5a-7's own mutant (m5a-7)
// is "the parts requirement moved into the payload shape or the fold".
func w5aApply(t *testing.T, st *store.Store, env store.Envelope) {
	t.Helper()
	applied, err := st.Apply(env)
	if err != nil {
		t.Fatalf("W5a-7 (m5a-7): the fold REFUSED %v: %v — a wire-received person_created folds clean whatever parts it carries; requiredness lives at the intake edge, never in payload shape or fold", env.Key(), err)
	}
	if !applied {
		t.Fatalf("apply %v: deduped unexpectedly (fixture defect: the seq is already in the log)", env.Key())
	}
}

// w5aPerson fetches the COMPOSED person or fails.
func w5aPerson(t *testing.T, st *store.Store, id string) *Person {
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

// w5aRawPersonRow returns the RAW projection row (not the composed person) —
// the only read that can tell "the fold wrote the column" from "composition
// supplied the value".
func w5aRawPersonRow(t *testing.T, st *store.Store, id string) Person {
	t.Helper()
	rows, err := dumpSection[Person](st, "person")
	if err != nil {
		t.Fatalf("reading the raw person projection: %v", err)
	}
	for _, r := range rows {
		if r.PersonID == id {
			return r
		}
	}
	t.Fatalf("fixture defect: no raw `person` projection row for %s (rows: %d)", id, len(rows))
	return Person{}
}

// --- the silence channel (W5a-5) ---------------------------------------

// w5aCaptureLog redirects the standard logger into a buffer for the duration
// of the test. Its own failure mode is PRODUCED by w5aRequireLogChannelWorks
// before any silence is asserted: a channel that captures nothing would make
// "no log line" true by construction, which is standing rule 5's exact shape.
func w5aCaptureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOut, prevFlags, prevPrefix := log.Writer(), log.Flags(), log.Prefix()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
		log.SetPrefix(prevPrefix)
	})
	return &buf
}

// w5aRequireLogChannelWorks is w5aCaptureLog's positive control.
func w5aRequireLogChannelWorks(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	const marker = "w5a-log-channel-control"
	log.Printf("%s", marker)
	if !strings.Contains(buf.String(), marker) {
		t.Fatalf("CONTROL FAILED: the log capture channel did not record a line written straight through it — every \"no log line\" assertion below would be true by construction (standing rule 5)")
	}
	buf.Reset()
}

// w5aReconciliationWords are the tokens a reconciliation pass, warning
// banner or repair would use. D-48's DERIVED prohibition (labelled as
// derived at W5a-5: the ruling itself is a PERMISSION — "nothing NEEDS to
// detect the disagreement") is that none of them ever appears.
var w5aReconciliationWords = []string{
	"reconcil", "diverg", "mismatch", "disagree", "inconsist",
	"repair", "recompose", "conflict", "warning", "warn:",
}

// w5aFindReconciliationWord returns the first reconciliation word present in
// s, case-insensitively, or "".
func w5aFindReconciliationWord(s string) string {
	low := strings.ToLower(s)
	for _, w := range w5aReconciliationWords {
		if strings.Contains(low, w) {
			return w
		}
	}
	return ""
}
