// w5d_payload_test.go — W5d-2b (`company_amended` carries NO `recorded_by`
// payload field: merge and amendment attribution is STRUCTURAL — env.SiteID
// — never a payload field) and B-2's freeze list for item 12, asserted
// against the MARSHALLED BYTES that actually enter the hash.
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, W5d LOCKED 2026-08-31.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS these tests' clean-tree red.
//
// KILLER MUTANT: (m5) — a `recorded_by` payload field minted on
// company_amended. ⚠ CompanyCreated DOES carry RecordedBy
// (refdata.go:26 [MEASURED]), so a clone author has a tempting precedent
// pointing the WRONG way — which is why this is a criterion and not a note.
// PersonAmended carries no such field (person_amend.go:56–71), D-5 rates
// the structural carrier stronger ("structurally and unforgeably… a payload
// field can be wrong"), and D-47 records the person-level attribution field
// as considered and deliberately NOT built.
package domain

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// w5dPlantedAmendedWithRecordedBy is W5d-2b's CONTROL plant: the payload
// shape a clone author copying CompanyCreated would produce.
type w5dPlantedAmendedWithRecordedBy struct {
	Type       string   `json:"type"`
	CompanyID  string   `json:"company_id"`
	Name       *string  `json:"name,omitempty"`
	SameAs     *string  `json:"same_as,omitempty"`
	Clear      []string `json:"clear,omitempty"`
	RecordedBy *string  `json:"recorded_by,omitempty"`
}

// w5dAttributionProblems flags any payload key that carries officer/post
// attribution — the detector shared by the CONTROL and the real assertion.
func w5dAttributionProblems(keys []string) []string {
	var problems []string
	for _, k := range keys {
		if k == "recorded_by" || k == "site_id" || k == "recorded_at" {
			problems = append(problems, "payload key "+k+" carries attribution — attribution on an amendment carrier is STRUCTURAL (env.SiteID), never a payload field (D-5, S7; `recorded_by` was STRUCK from B-2's item-12 freeze list at rev 4, so freezing it would create a ruled-out field as a frozen ghost)")
		}
	}
	sort.Strings(problems)
	return problems
}

// TestW5dCompanyAmendedCarriesNoRecordedBy — W5d-2b, asserted structurally
// (the payload shape) AND on the marshalled bytes B-2 freezes.
func TestW5dCompanyAmendedCarriesNoRecordedBy(t *testing.T) {
	// --- CONTROL FIRST: the planted recorded_by field -------------------
	planted := w5dWireFactsOf(w5dPlantedAmendedWithRecordedBy{})
	if p := w5dAttributionProblems(planted); len(p) != 1 {
		t.Fatalf("W5d-2b CONTROL FAILED: the attribution detector reported %d problem(s) on a payload shape carrying `recorded_by` (%v), want 1 — a detector that cannot detect its plant proves nothing (m5)", len(p), p)
	}

	// --- the real payload shape -----------------------------------------
	keys := w5dWireFactsOf(CompanyAmended{})
	if p := w5dAttributionProblems(keys); len(p) != 0 {
		t.Fatalf("W5d-2b (m5): %v — CompanyAmended's keys are %v", p, keys)
	}

	// --- the MARSHALLED BYTES, which is what B-2 freezes and what the
	// hash covers: a real amendment asserting BOTH facts and a Clear. ----
	st := w3NewStore(t)
	x, y, _ := w5dCompanyPair(t, st, "Freeze X Ltd", "Freeze Y Ltd")
	env := w5dAmendLocal(t, st, "site-a", 1, w3Base+1*w3Hour,
		CompanyAmendment{CompanyID: x, Name: w3Str("Freeze Name Ltd"), SameAs: w3Str(y)})

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(env.Payload, &raw); err != nil {
		t.Fatalf("W5d-2b: the authored payload is not an object: %v (%s)", err, env.Payload)
	}
	var got []string
	for k := range raw {
		got = append(got, k)
	}
	sort.Strings(got)
	want := []string{"company_id", "name", "same_as", "type"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("W5d-2b / B-2's freeze list (item 12): the authored company_amended payload's keys are %v, want exactly %v — the frozen literals are the event-type name `company_amended`, the field names `company_id`/`name`/`same_as`, and the lowercase `clear` key when a Clear is present. `recorded_by` is EXPLICITLY NOT on the list and `retired` is asserted ABSENT (D-41.1); a freeze review that carries either forward re-creates a ruled-out field as a frozen ghost", got, want)
	}

	// The event-type literal itself, as a literal.
	var typ string
	if err := json.Unmarshal(raw["type"], &typ); err != nil || typ != "company_amended" {
		t.Fatalf("W5d-2b / B-2: the payload type is %q (err=%v), want the frozen literal %q", typ, err, "company_amended")
	}

	// The lowercase `clear` wire key (DECIDED at P-3 §7 rev 3), and the
	// note S3 makes: the same field-name spellings are simultaneously the
	// Clear VALUE vocabulary, so each literal freezes twice over.
	clearEnv := w5dAmendLocal(t, st, "site-a", 1, w3Base+2*w3Hour,
		CompanyAmendment{CompanyID: x, Clear: []string{w5dFactSameAs}})
	if !strings.Contains(string(clearEnv.Payload), `"clear":["same_as"]`) {
		t.Fatalf("W5d-2b / B-2: the un-merge payload is %s, want the LOWERCASE `clear` key carrying the field-name spelling `same_as` as its VALUE — each item-12 literal freezes twice over (S3)", clearEnv.Payload)
	}

	// --- attribution is STRUCTURAL: the envelope, not the payload -------
	if env.SiteID != "site-a" {
		t.Fatalf("W5d-2b: the amendment's envelope SiteID is %q, want the authoring post %q — attribution on an amendment carrier is carried structurally and unforgeably by the envelope (D-5)", env.SiteID, "site-a")
	}
	if env.OriginAuth != "site-a" {
		t.Fatalf("W5d-2b: the amendment's OriginAuth is %q, want %q", env.OriginAuth, "site-a")
	}
}
