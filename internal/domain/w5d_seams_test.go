// w5d_seams_test.go — THE ONE place W5d's domain tests bind every Go
// spelling the LOCKED criteria leave undetermined
// (design/WINDOW-BUILD-PLAN-v1.md, `### W5 —` CRITERIA LOCKED 2026-08-31,
// sub-chunk `#### W5d — item 12`; test-writer note, plan §5 step 2), plus
// the shared company fixture machinery. This file holds NO assertions.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — every symbol under "SHAPE
// PRESUMPTIONS" is unbuilt at the clean tree (HEAD 88dd6a6; `company_amended`
// and its family are ZERO hits tree-wide [MEASURED]). That compile failure
// IS the ledger RED for the resolver-level tests (plan §5 step 2: "capture
// the go vet output as its red").
//
// W5d clones W3's mechanism against company, so the person-side helpers in
// w3_seams_test.go (w3NewStore, w3Apply, w3AuthorRemote, w3RequireDistinct,
// w3BudgetedResolve, w3Base/w3Hour, …) are REUSED, not re-created: the
// machinery is chunk-global, the FIXTURES are per-clone (plan's W3-11 ride
// wording). Where a person-typed helper cannot serve company — the T-A
// oracle's composed-value accessor is the only one — a company twin is
// written below and the reason is stated at it.
//
// SHAPE PRESUMPTIONS (SEAMS), each bound ONCE, disputes go to the gate:
//
//   - SEAM W5D-S-EDGE: the company authoring edge is
//     domain.AmendCompany(st *store.Store, siteID string, nodeEpoch int64,
//     in domain.CompanyAmendment, occurredAtMS int64, timeSource string,
//     ) (store.Envelope, error)
//     — AmendPerson's positional shape, cloned. A refusal returns a non-nil
//     error and NEVER calls Author (W5d-17 / W3-2's pre-append clause,
//     asserted rather than assumed).
//   - SEAM W5D-S-INPUT: domain.CompanyAmendment carries EXACTLY
//     CompanyID string; Name, SameAs *string; Clear []string
//     — the amendable set is {name, same_as} and NOTHING else (W5d-22(a),
//     locked as literals; `retired` is absent per D-41.1 / W5d-2, and
//     trade_id/phone/role/pad_oriented/position/last_name/first_name have
//     no company limb).
//   - SEAM W5D-S-COMPANY: the composed domain.Company (Companies()/
//     CompanyByID()) gains SameAs *string (nil = absent — the
//     absent-not-empty distinction W5d-18 turns on is carried by the
//     pointer, so this is load-bearing, not style). Name STAYS a bare
//     string: `company.name` is TEXT NOT NULL and CompanyCreated.Name is a
//     bare string [MEASURED — refdata.go:109, :25], and W5d-22(d) pins what
//     a wire-received Clear:["name"] composes to (below).
//   - SEAM W5D-S-NAMECLEARED: a company whose name has been cleared FROM
//     THE WIRE composes with Name == "" — the empty string is the only
//     representable "absent" on a non-pointer field, and W5d-22(d) requires
//     the composition and its rendered label to be PINNED rather than
//     discovered. The reader-visible consequence is asserted at the muster's
//     third preference arm (W5d-14(b-i)), which is where "no name" is
//     observable. If the builder instead makes Company.Name a *string, this
//     seam and w5dCompanyName below are the ONE place that changes.
//   - SEAM W5D-S-RESOLVER: the P-3 §5 derived resolution snapshot for
//     company is
//     domain.LoadCompanyMergeMap(st *store.Store) (domain.AliasMap, error)
//     — LoadPersonMergeMap's analogue over the surviving AliasMap read
//     shape, derived per read, caching nothing (W5d-5b). It has TWO
//     consumers, both W5d's: muster.go's company line re-points to it, and
//     the company edge's CYCLE refusal walks it (W5d-22(b) arm 10).
//   - SEAM W5D-S-MUSTER: DR-4/H6 require the company and workgroup
//     resolvers to be able to DISAGREE, and one `aliasMap` parameter cannot
//     do that (domain/muster.go:107 feeds both :122 and :133) — so the
//     signature changes:
//     domain.MusterLive(visits []Visit, persons []Person,
//     companies []Company, workgroups []Workgroup,
//     companyMerge AliasMap, workgroupAlias AliasMap,
//     minCrewSize int) MusterBoard
//     The FIFTH parameter is the composed company merge map
//     (LoadCompanyMergeMap); the SIXTH stays the legacy ref_alias map and
//     serves workgroup ONLY, until W8 deletes it (P-3 §5's schedule, cited
//     never restated). Positional order mirrors the code order
//     (company :122 before workgroup :133).
//     ⚠ This changes four EXISTING call sites in domain/muster_test.go
//     (:175, :199, :353, :384) and the only non-test caller
//     (web/muster.go:326/:332) — the builder's edit, flagged for the gate
//     in the report, not made here.
//   - SEAM W5D-S-COMPETITORS: W5d-9(b)'s "one exported domain API beside
//     the pinned FactOutcome — per-fact competitor enumeration for a
//     queried key" is
//     domain.Competitors(st *store.Store, k store.Key) (map[string][]store.Key, error)
//     — fact name → the keys of the OTHER events that contested that fact
//     within k's contest scope, k itself excluded, ascending key order; a
//     fact with no competitors may be absent or empty (tests treat both as
//     the empty set). Errors, never empties, on an absent or unregistered
//     key — Outcome's own §4.2 contract.
//     ⚠ DR-7 records `Competitors() []FactOutcome` as an OPTION the builder
//     MAY take and explicitly NOT as an obligation (the arbiter vetoed
//     prescribing a return type), so this is a SEAM and not an inference:
//     a test must call something. The map shape is chosen because it uses
//     only EXISTING types, adds no struct beside the byte-stable
//     FactOutcome, and carries the competitor's own KEY — which
//     FactOutcome structurally cannot (its Winner field names the winner,
//     so a competitor's identity would be lost). If the gate prefers the
//     []FactOutcome option, w5dCompetitorKeys below is the ONE adapter
//     that changes.
//   - SEAM W5D-S-PAYLOAD: wire literals, per the measured lowercase
//     snake_case convention and B-2's freeze list for item 12: type
//     "company_amended"; keys company_id, name, same_as, and clear
//     (LOWERCASE on the wire — DECIDED at P-3 §7 rev 3). recorded_by is
//     NOT a key (W5d-2b; struck from the freeze list at rev 4).
//   - SEAM W5D-S-FACTS: Outcome Fact names for this carrier are the payload
//     field spellings — "name" and "same_as", in that canonical order
//     (personAmendableFacts' convention, cloned).
//
// NOT seams (DOC-pinned, cited): FactOutcome/Status/Outcome (VF §4.2
// verbatim, byte-stable per W5d-9(c)); ascending-key composition
// (W5d-16 = W3-1); "site-b" > "site-a" (VF §1/§10).
package domain

import (
	"encoding/json"
	"sort"
	"testing"

	"sitecontrol/internal/store"
)

const (
	w5dEvtCompanyAmended = "company_amended" // SEAM W5D-S-PAYLOAD
	w5dFactName          = "name"            // SEAM W5D-S-FACTS
	w5dFactSameAs        = "same_as"
)

// w5dAmendableFacts is W5d-22(a)'s amendable set, AS LITERALS, in canonical
// order. This slice is the criterion's own content: {name, same_as} and
// nothing else.
var w5dAmendableFacts = []string{"name", "same_as"}

// w5dNonCompanyFacts are the person/trip-side fact spellings that must have
// NO company limb (W5d-22(a); `retired` additionally by D-41.1 / W5d-2).
var w5dNonCompanyFacts = []string{
	"retired", "trade_id", "phone", "role", "pad_oriented",
	"position", "last_name", "first_name",
}

// --- company fixture machinery ------------------------------------------

// w5dCompanyPair mints two companies on origin (site-a, epoch 1) and
// returns their ids with the full site-a stream, for priming remote scratch
// stores — w3RosterPair's company twin.
func w5dCompanyPair(t *testing.T, st *store.Store, nameA, nameB string) (idA, idB string, stream []store.Envelope) {
	t.Helper()
	ca, err := CreateCompany(st, "site-a", 1, nameA, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	cb, err := CreateCompany(st, "site-a", 1, nameB, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return ca.CompanyID, cb.CompanyID, w3Events(t, st, "site-a", 1)
}

// w5dAmendLocal authors one company amendment on st with the clock parked
// at clockMS and returns its envelope; a refusal is a FIXTURE failure here,
// never an assertion (the refusal tests call AmendCompany directly).
func w5dAmendLocal(t *testing.T, st *store.Store, siteID string, epoch, clockMS int64, in CompanyAmendment) store.Envelope {
	t.Helper()
	st.Clock = w3Reading(clockMS)
	env, err := AmendCompany(st, siteID, epoch, in, 0, "default")
	if err != nil {
		t.Fatalf("fixture company amendment refused: %v", err)
	}
	return env
}

// w5dWireAmendment hand-builds a company_amended envelope for the cases the
// authoring edge REFUSES (W5d-22(c)'s folds-never-refuse half needs events
// no edge will mint) and for the cross-epoch/cross-site fixtures that need
// a chosen key. fields uses SEAM W5D-S-PAYLOAD key spellings.
func w5dWireAmendment(t *testing.T, siteID string, epoch, seq, authoredAtMS int64, companyID string, fields map[string]any) store.Envelope {
	t.Helper()
	payload := map[string]any{"type": w5dEvtCompanyAmended, "company_id": companyID}
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

// w5dCompany fetches the COMPOSED company or fails.
func w5dCompany(t *testing.T, st *store.Store, id string) *Company {
	t.Helper()
	c, err := CompanyByID(st, id)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatalf("company %s: no row", id)
	}
	return c
}

// w5dCompanies reads the composed company list.
func w5dCompanies(t *testing.T, st *store.Store) []Company {
	t.Helper()
	cs, err := Companies(st)
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

// w5dCompanyName is the ONE place a composed company's name is read, so
// SEAM W5D-S-NAMECLEARED has a single point of change. Returns
// (value, present) — present is false for the cleared/never-named case.
func w5dCompanyName(c *Company) (string, bool) {
	if c.Name == "" {
		return "", false
	}
	return c.Name, true
}

// w5dMergeMap loads the derived company resolution snapshot
// (SEAM W5D-S-RESOLVER).
func w5dMergeMap(t *testing.T, st *store.Store) AliasMap {
	t.Helper()
	m, err := LoadCompanyMergeMap(st)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// w5dCompetitorKeys is the ONE adapter over SEAM W5D-S-COMPETITORS (see the
// seam note above: if the gate prefers DR-7's recorded []FactOutcome
// option, only this function changes). Returns fact → sorted competitor
// key strings.
func w5dCompetitorKeys(t *testing.T, st *store.Store, k store.Key) map[string][]string {
	t.Helper()
	raw, err := Competitors(st, k)
	if err != nil {
		t.Fatalf("Competitors(%v): %v — the per-fact competitor enumeration errored on a real, registered company amendment (W5d-9(b))", k, err)
	}
	out := map[string][]string{}
	for fact, keys := range raw {
		var ks []string
		for _, kk := range keys {
			ks = append(ks, kk.String())
		}
		sort.Strings(ks)
		out[fact] = ks
	}
	return out
}

// --- the T-A agreement oracle, company side (W5d-10) --------------------
//
// The ORACLE MACHINERY is chunk-global and the FIXTURES are per-clone
// (W3-11's ride wording). w3RunTAOracle cannot be reused verbatim for one
// reason and one only: its composed-value accessor w3ComposedFactValue is
// PERSON-typed (it reads Person.Phone / Person.Name through
// LoadPersonMergeMap). Everything else below — the two validity
// preconditions, the iff, the Winner convention — is w3RunTAOracle's own
// text against the company accessors.

// w5dTAFixture is one asserted fact of one fixture event.
type w5dTAFixture struct {
	Key       store.Key
	CompanyID string // the entity the event targeted
	Fact      string
	Asserted  string
}

// w5dComposedFactValue reads the fact's composed/RESOLVED value. The `name`
// VALUE fact is read through the ONE derived resolution snapshot — a merge
// joins assertion sets, so the resolved record's composed value is what
// every surface shows (VF §6 case 5). The `same_as` fact is the merge
// POINTER and is read on the entity's OWN composed record (resolving it
// first would erase the very fact under test).
func w5dComposedFactValue(t *testing.T, st *store.Store, companyID, fact string) (string, bool) {
	t.Helper()
	if fact == w5dFactSameAs {
		c := w5dCompany(t, st, companyID)
		if c.SameAs == nil {
			return "", false
		}
		return *c.SameAs, true
	}
	m := w5dMergeMap(t, st)
	c := w5dCompany(t, st, m.Resolve(companyID))
	if fact == w5dFactName {
		return w5dCompanyName(c)
	}
	t.Fatalf("w5dComposedFactValue: no accessor bound for fact %q (extend SEAM W5D-S-FACTS)", fact)
	return "", false
}

// w5dRunTAOracle is VF §8's T-A agreement oracle over COMPANY fixtures: for
// every fixture asserted-fact, Status == Effective IFF the composed/
// resolved value equals that event's asserted value. Its two validity
// preconditions are asserted at RUNTIME, before any outcome is trusted —
// this is the CONTROL row W5d-10 owes ("recorded as met before the
// oracle's results are trusted"), enforced at every run rather than
// recorded once: (1) pairwise-distinct values per fact; (2) an unmatched
// fact is a FAILURE, never a skip.
func w5dRunTAOracle(t *testing.T, st *store.Store, fixtures []w5dTAFixture) {
	t.Helper()
	if len(fixtures) == 0 {
		t.Fatalf("T-A FIXTURE DEFECT: the oracle was handed ZERO fixtures — an oracle with nothing to judge passes by construction")
	}
	seen := map[string]map[string]store.Key{}
	for _, f := range fixtures {
		if seen[f.Fact] == nil {
			seen[f.Fact] = map[string]store.Key{}
		}
		if prev, dup := seen[f.Fact][f.Asserted]; dup {
			t.Fatalf("T-A FIXTURE DEFECT (precondition 1): fact %q asserted value %q twice (%v and %v) — with duplicate values the iff is unsatisfiable on correct code and the predictable repair greens over W3-m11/W3-m17", f.Fact, f.Asserted, prev, f.Key)
		}
		seen[f.Fact][f.Asserted] = f.Key
	}
	for _, f := range fixtures {
		out := w5dFact(t, st, f.Key, f.Fact)
		val, present := w5dComposedFactValue(t, st, f.CompanyID, f.Fact)
		matches := present && val == f.Asserted
		if (out.Status == Effective) != matches {
			t.Fatalf("W5d-10 T-A disagreement (W3-m17/W3-m5): event %v fact %q asserted %q; Outcome says %v but the composed/resolved read says %q (present=%v) — the Go comparator and the SQL ORDER BY have drifted", f.Key, f.Fact, f.Asserted, out.Status, val, present)
		}
		if out.Status == Effective && out.Winner != f.Key {
			t.Fatalf("W5d-10/W5d-19: fact %q Effective but Winner %v != queried key %v (the §4.2 convention: Winner == queried key iff Effective)", f.Fact, out.Winner, f.Key)
		}
		if out.Status == Superseded {
			if out.Winner == (store.Key{}) {
				t.Fatalf("W5d-10: fact %q Superseded with a ZERO Winner — Winner is always a real event's key (VF §4.2)", f.Fact)
			}
			if out.Winner == f.Key {
				t.Fatalf("W5d-10: fact %q Superseded yet Winner == the queried key %v", f.Fact, f.Key)
			}
		}
	}
}

// w5dFact returns the FactOutcome for fact from Outcome(k); a missing fact
// or an Outcome error is a FAILURE, never a skip (T-A precondition 2).
func w5dFact(t *testing.T, st *store.Store, k store.Key, fact string) FactOutcome {
	t.Helper()
	outs, err := Outcome(st, k)
	if err != nil {
		t.Fatalf("Outcome(%v): %v", k, err)
	}
	for _, o := range outs {
		if o.Fact == fact {
			return o
		}
	}
	t.Fatalf("Outcome(%v) reported no %q fact (got %+v) — cannot-match is a FAILURE, never a skip (W5d-10, T-A precondition 2)", k, fact, outs)
	return FactOutcome{}
}

// w5dSameAsOf renders a composed company's same_as for messages.
func w5dSameAsOf(c *Company) string {
	if c.SameAs == nil {
		return "<absent>"
	}
	return *c.SameAs
}
