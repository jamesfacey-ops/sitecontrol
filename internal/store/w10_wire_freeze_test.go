// w10_wire_freeze_test.go — W10's WIRE half: the REVERSE diff B-2 has never
// been made to run, mechanised.
//
// Criteria source: design/WINDOW-BUILD-PLAN-v1.md §3, `### W10 — Window
// integration and rehearsal`, LOCKED 2026-09-03 (reports/W10-FOLD-RECORD.md,
// gate P-4). Written from the LOCKED CRITERION TEXT ONLY.
//
// Criteria carried here:
//
//   - W10-5(b) — the REVERSE diff. "Derived from the tree rather than from
//     the row: every string literal that enters `Hash()` on the pinned tree
//     is on B-2's list or explicitly excluded with a reason." Its FALSIFIER
//     is explicit that a ONE-DIRECTION review cannot fail on the defect the
//     row's own history names: "*Every listed literal is in the tree* is
//     satisfiable while a literal that is **in the tree and not on the
//     list** goes unseen — which is precisely `cargo`/`cargo_note`, missed
//     *for a full revision*."
//   - DR-W10-6 — the "wire-shape, not hash-frozen" sub-heading: the
//     envelope's OWN JSON keys, enumerated. The ruling is NOT ADOPTED by the
//     fold (recorded on B-2's row as a recommendation with an owner and a
//     trigger), so nothing here asserts that B-2's row carries them. What is
//     asserted is the tree-side half the ruling rests on: the derived set is
//     exactly twelve and these are their spellings.
//
// ⚠ WHAT THIS FILE DOES NOT DO. It does not assert that B-2 has been signed,
// that the inventory file exists, or that any literal is "on B-2's row".
// Those are properties of a document and of a signature, not of this tree —
// see reports/window-build/W10-tests.md §2 for the split. This file is the
// TREE HALF of W10-5(b): the derived set, which is the input a reverse diff
// needs and which nothing in the repository previously pinned.
//
// ⚠ RULE 20 / CONTROL DISCIPLINE. Neither judgement below is a source scan —
// both derive from the compiled tree (encoding/json over the real Envelope,
// and the real Hash()). The controls therefore need no on-disk plant: the
// rival implementations and the 13-key fixture type are compiled into this
// file and the SAME judgement functions run over both, so a control red
// proves the same function the clean pass runs.
//
// PRODUCED REDS (see the report for transcripts):
//   - forwardOnlyReviewIsSatisfied / reverseDiffFindings run over
//     w10ThirteenKeyFixture: the forward rule returns SATISFIED and the
//     reverse rule returns one unlisted key. Different answers on one
//     fixture, both pinned as literals — DR-W10-11's non-degeneracy rule.
//   - hashMoves run over an envelope mutated in each of eleven positions.
//
// This file names no unbuilt symbol and compiles at e9cc5f5.
package store

import (
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// W10-5(b) / DR-W10-6 — the derived wire-key set, pinned as literals.
// ---------------------------------------------------------------------------

// w10WireKeys is the "wire-shape, not hash-frozen" set DR-W10-6 enumerates,
// quoted from the LOCKED criterion text and written here as literals so the
// check is a comparison against a pinned list and not against itself:
//
//	"the envelope's own JSON keys `site_id`, `node_epoch`, `seq`,
//	 `schema_version`, `origin_auth`, `authored_at`(`wall_ms`,
//	 `max_error_us`, `state`), `occurred_at`, `time_source`, `payload`"
//
// Nested keys are written parent-qualified so the set is flat and a key
// moving between levels is a difference rather than a wash.
var w10WireKeys = []string{
	"authored_at",
	"authored_at.max_error_us",
	"authored_at.state",
	"authored_at.wall_ms",
	"node_epoch",
	"occurred_at",
	"origin_auth",
	"payload",
	"schema_version",
	"seq",
	"site_id",
	"time_source",
}

// w10DerivedWireKeys derives the flat wire-key set of v from the TREE, one
// level of nesting qualified by its parent.
//
// ⚠ It stops at `json.RawMessage`, and that boundary is the criterion's, not
// a convenience. W10-5(b) splits the derivation in two: the payload's own
// field names and string values are in the HASH-FROZEN half ("every byte of
// e.Payload, written verbatim by h.Write(e.Payload)"), and DR-W10-6's twelve
// are "the envelope's OWN JSON keys". envelope.go says the same thing from
// the other side: "the store treats it as opaque, valid JSON". A derivation
// that descended into a payload would report a different set for every event
// type — measured: marshalling one probe payload adds `payload.type` and
// makes the set thirteen.
//
// THE DERIVATION IS FROM THE TREE: nothing here reads w10WireKeys, so the two
// are independent and can disagree.
func w10DerivedWireKeys(t *testing.T, v any) []string {
	t.Helper()
	var walk func(rt reflect.Type, prefix string, out *[]string)
	walk = func(rt reflect.Type, prefix string, out *[]string) {
		for rt.Kind() == reflect.Pointer {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct {
			t.Fatalf("w10DerivedWireKeys: %s is not a struct", rt)
		}
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := f.Tag.Get("json")
			if tag == "-" {
				continue
			}
			name, _, _ := strings.Cut(tag, ",")
			if name == "" {
				name = f.Name // encoding/json's own fallback
			}
			key := name
			if prefix != "" {
				key = prefix + "." + name
			}
			*out = append(*out, key)
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			// json.RawMessage is a []byte, so it is already a leaf by kind;
			// the check is written explicitly so the boundary is on the face
			// of the code rather than an accident of representation.
			if ft == reflect.TypeOf(json.RawMessage(nil)) {
				continue
			}
			if ft.Kind() == reflect.Struct {
				walk(ft, key, out)
			}
		}
	}
	var out []string
	walk(reflect.TypeOf(v), "", &out)
	sort.Strings(out)

	// Cross-check the reflected derivation against what encoding/json
	// actually emits at the TOP level, so a tag this walk misreads cannot
	// pass unnoticed. (Nested keys are not cross-checked here: the payload
	// boundary above means the marshalled form legitimately carries keys
	// this set does not.)
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for k := range top {
		found := false
		for _, d := range out {
			if d == k {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("encoding/json emits top-level key %q that the reflected derivation missed — "+
				"the derivation is wrong, not the tree", k)
		}
	}
	return out
}

// reverseDiffFindings is W10-5(b)'s CERTIFIED rule: derived-from-the-tree
// MINUS the list. A non-empty result is the criterion's own falsifier —
// "a literal that is in the tree and not on the list".
func reverseDiffFindings(derived, list []string) []string {
	on := map[string]bool{}
	for _, k := range list {
		on[k] = true
	}
	var missing []string
	for _, k := range derived {
		if !on[k] {
			missing = append(missing, k)
		}
	}
	return missing
}

// forwardOnlyReviewIsSatisfied is the NAMED RIVAL: W10-5(b)'s "one-direction
// review" — every LISTED literal is present in the tree. It is written out
// in full rather than described, because the non-degeneracy rule requires
// the rival to be RUN on the fixture, not asserted about.
func forwardOnlyReviewIsSatisfied(derived, list []string) bool {
	inTree := map[string]bool{}
	for _, k := range derived {
		inTree[k] = true
	}
	for _, k := range list {
		if !inTree[k] {
			return false
		}
	}
	return true
}

func TestW10TheEnvelopeWireKeySetIsExactlyTheTwelveDR_W10_6Enumerates(t *testing.T) {
	// A fully-populated envelope: every field non-zero, so no key can be
	// absent from the marshalled form for want of a value. (No omitempty is
	// in play today; populating anyway means this test does not silently
	// weaken if one is added.)
	env := Envelope{
		SiteID:        "site-a",
		NodeEpoch:     7,
		Seq:           41,
		SchemaVersion: SchemaVersion,
		OriginAuth:    "site-a",
		AuthoredAt: AuthoredAt{
			WallMS:     1_756_000_000_000,
			MaxErrorUS: 250,
			State:      ClockSynced,
		},
		OccurredAt: 1_755_900_000_000,
		TimeSource: "entered",
		Payload:    json.RawMessage(`{"type":"w10_probe"}`),
	}

	derived := w10DerivedWireKeys(t, env)

	if missing := reverseDiffFindings(derived, w10WireKeys); len(missing) != 0 {
		t.Errorf("REVERSE DIFF: %d wire key(s) in the tree and not on DR-W10-6's list: %v\n"+
			"derived set (%d): %v", len(missing), missing, len(derived), derived)
	}
	if dropped := reverseDiffFindings(w10WireKeys, derived); len(dropped) != 0 {
		t.Errorf("FORWARD DIFF: %d key(s) on DR-W10-6's list and NOT in the tree: %v\n"+
			"derived set (%d): %v", len(dropped), dropped, len(derived), derived)
	}
	if len(derived) != 12 {
		t.Errorf("the derived wire-key set is %d keys, want 12 (DR-W10-6, and W10-5(b)'s "+
			"CONTROL: \"Today it finds twelve: the envelope keys\"): %v", len(derived), derived)
	}
}

// w10ThirteenKeyFixture is THE PLANT. It is a local type, never the tree's
// Envelope, so nothing under app/ is mutated and no other agent's `go test`
// can see a modified tree — which matters tonight, with four windows'
// uncommitted work in this checkout.
//
// It is the shape B-2's row has already been caught by once: a wire key that
// exists in the tree and is not on the list ("cargo"/"cargo_note", missed for
// a full revision).
type w10ThirteenKeyFixture struct {
	SiteID        string          `json:"site_id"`
	NodeEpoch     int64           `json:"node_epoch"`
	Seq           int64           `json:"seq"`
	SchemaVersion int64           `json:"schema_version"`
	OriginAuth    string          `json:"origin_auth"`
	AuthoredAt    AuthoredAt      `json:"authored_at"`
	OccurredAt    int64           `json:"occurred_at"`
	TimeSource    string          `json:"time_source"`
	Payload       json.RawMessage `json:"payload"`
	CargoNote     string          `json:"cargo_note"` // the plant
}

func TestW10AForwardOnlyReviewCannotSeeTheKeyTheReverseDiffFinds(t *testing.T) {
	fix := w10ThirteenKeyFixture{
		SiteID: "site-a", NodeEpoch: 7, Seq: 41, SchemaVersion: SchemaVersion,
		OriginAuth: "site-a",
		AuthoredAt: AuthoredAt{WallMS: 1, MaxErrorUS: 2, State: ClockSynced},
		OccurredAt: 3, TimeSource: "default",
		Payload:   json.RawMessage(`{}`),
		CargoNote: "planted",
	}
	derived := w10DerivedWireKeys(t, fix)

	// The two rules, run on ONE fixture, must return DIFFERENT answers.
	// Pinned as literals, per the standing fixture non-degeneracy rule.
	if !forwardOnlyReviewIsSatisfied(derived, w10WireKeys) {
		t.Fatal("DEGENERATE FIXTURE: the named rival (a forward-only review) is NOT satisfied " +
			"by the 13-key fixture, so it does not demonstrate the blindness W10-5(b) names. " +
			"Strengthen the fixture before trusting the assertion below.")
	}
	got := reverseDiffFindings(derived, w10WireKeys)
	want := []string{"cargo_note"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("the certified rule (reverse diff) returned %v, want exactly %v — "+
			"if it returns the same answer as the rival, W10-5(b)'s check is a no-op", got, want)
	}
	// Both halves stated as values, so the report can quote them.
	t.Logf("NON-DEGENERACY, pinned: rival(forward-only) = SATISFIED · certified(reverse) = %v", got)
}

// ---------------------------------------------------------------------------
// W10-5(b) — the HASH-FROZEN half: "every string literal that enters Hash()".
// ---------------------------------------------------------------------------

// w10HashMutations enumerates, by name, every value W10-5(b) lists as
// hash-frozen — "the values Hash() formats (SiteID, NodeEpoch, Seq,
// SchemaVersion, OriginAuth, AuthoredAt.WallMS, AuthoredAt.MaxErrorUS,
// AuthoredAt.State, OccurredAt, TimeSource) plus every byte of e.Payload" —
// each with a single-field mutation. The list is the criterion's; the
// mutations are how it is made falsifiable.
func w10HashMutations() []struct {
	Name  string
	Apply func(*Envelope)
} {
	return []struct {
		Name  string
		Apply func(*Envelope)
	}{
		{"SiteID", func(e *Envelope) { e.SiteID = "site-b" }},
		{"NodeEpoch", func(e *Envelope) { e.NodeEpoch++ }},
		{"Seq", func(e *Envelope) { e.Seq++ }},
		{"SchemaVersion", func(e *Envelope) { e.SchemaVersion++ }},
		{"OriginAuth", func(e *Envelope) { e.OriginAuth = "site-b" }},
		{"AuthoredAt.WallMS", func(e *Envelope) { e.AuthoredAt.WallMS++ }},
		{"AuthoredAt.MaxErrorUS", func(e *Envelope) { e.AuthoredAt.MaxErrorUS++ }},
		{"AuthoredAt.State", func(e *Envelope) { e.AuthoredAt.State = ClockUnsynced }},
		{"OccurredAt", func(e *Envelope) { e.OccurredAt++ }},
		{"TimeSource", func(e *Envelope) { e.TimeSource = "default" }},
		{"Payload", func(e *Envelope) { e.Payload = json.RawMessage(`{"type":"w10_probe_2"}`) }},
	}
}

func w10BaseEnvelope() Envelope {
	return Envelope{
		SiteID:        "site-a",
		NodeEpoch:     7,
		Seq:           41,
		SchemaVersion: SchemaVersion,
		OriginAuth:    "site-a",
		AuthoredAt:    AuthoredAt{WallMS: 1_756_000_000_000, MaxErrorUS: 250, State: ClockSynced},
		OccurredAt:    1_755_900_000_000,
		TimeSource:    "entered",
		Payload:       json.RawMessage(`{"type":"w10_probe"}`),
	}
}

func TestW10EveryValueW10_5bNamesHashFrozenActuallyMovesTheDigest(t *testing.T) {
	base := w10BaseEnvelope()
	baseHash := base.Hash()

	// The guard against a reassuring zero, in `assert_epoch_refused`'s own
	// construction: an unmutated copy must hash EQUAL, or "every mutation
	// moved the digest" would be satisfied by a Hash() that is simply
	// non-deterministic.
	if again := w10BaseEnvelope().Hash(); again != baseHash {
		t.Fatalf("DEGENERATE: Hash() is not deterministic over an identical envelope (%s vs %s) — "+
			"the mutation arms below would pass for the wrong reason", baseHash, again)
	}

	for _, m := range w10HashMutations() {
		env := w10BaseEnvelope()
		m.Apply(&env)
		if got := env.Hash(); got == baseHash {
			t.Errorf("%s is NOT hash-frozen: mutating it leaves the digest at %s. "+
				"W10-5(b) lists it among the values Hash() formats.", m.Name, baseHash)
		}
	}
	if n := len(w10HashMutations()); n != 11 {
		t.Errorf("the hash-frozen arm count is %d, want 11 — W10-5(b) names ten formatted "+
			"values plus every byte of e.Payload", n)
	}
}

// TestW10TheDigestSeparatorIsNotDefeatableByFieldConcatenation is the
// non-degeneracy arm for the test above. Eleven passing mutation arms would
// still pass if Hash() concatenated its fields with no separator, in which
// case two DIFFERENT envelopes could share a digest — the property that
// actually matters at the seam-echo check, where a stream whose history
// shares a seq but not a prefix must be refused.
//
// ⚠ THE FIELDS ARE NODEEPOCH AND SEQ ON PURPOSE, AND THE FIRST VERSION OF
// THIS TEST GOT IT WRONG. It used SiteID and OriginAuth, which are NOT
// ADJACENT in Hash()'s format string (SiteID, NodeEpoch, Seq, SchemaVersion,
// OriginAuth): three numeric verbs sit between them, so moving a character
// across that boundary changes the bytes even with every separator removed.
// The plant caught it — a scratch copy with the \x00s deleted still passed —
// which is the whole reason a check is invalid until its failure mode has
// been produced. NodeEpoch and Seq ARE adjacent, and 1|23 versus 12|3
// collide exactly when the separator is gone.
func TestW10TheDigestSeparatorIsNotDefeatableByFieldConcatenation(t *testing.T) {
	a := w10BaseEnvelope()
	a.NodeEpoch, a.Seq = 1, 23

	b := w10BaseEnvelope()
	b.NodeEpoch, b.Seq = 12, 3

	// The NAMED RIVAL: an unseparated concatenation of the same two adjacent
	// fields, RUN rather than described.
	naiveConcat := func(e Envelope) string {
		return strconv.FormatInt(e.NodeEpoch, 10) + strconv.FormatInt(e.Seq, 10)
	}
	if naiveConcat(a) != naiveConcat(b) {
		t.Fatalf("DEGENERATE FIXTURE: the named rival (unseparated concatenation) already "+
			"distinguishes these two envelopes (%q vs %q), so this fixture proves nothing "+
			"about the separator", naiveConcat(a), naiveConcat(b))
	}
	if a.Hash() == b.Hash() {
		t.Errorf("two envelopes differing only by where the node_epoch/seq boundary falls hash "+
			"EQUAL (%s) — the digest is defeatable by concatenation, and (site_id, node_epoch, "+
			"seq) is the ordering and idempotency key", a.Hash())
	}
	t.Logf("NON-DEGENERACY, observed: rival(concat) = %q vs %q · certified(Hash) = %s vs %s",
		naiveConcat(a), naiveConcat(b), a.Hash()[:16], b.Hash()[:16])
}
