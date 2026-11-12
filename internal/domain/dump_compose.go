// dump_compose.go — the composed-read API's DUMP-FILE entry point.
//
// Persons(st) (refdata.go) is the one composed read path for a caller that
// holds a live *store.Store. cmd/dumpcompare's D-56 boundary means a whole
// class of tool has no live store at all — only a saved dump file — and
// until this file existed, that class of tool had NO WAY to reach composed
// identity without decoding the person_amendment side table itself, which
// W3-14 forbids to everything outside this package (internal/store/
// w3_source_scans_test.go's TestW3ConsumerScan: no non-domain package may
// name the table or its event-type constants, in an identifier OR inside a
// string literal — the allowlist is empty by design).
//
// ComposedPersonsFromDump is that entry point: same composition
// (composePersons, person_amend.go) that Persons(st) performs from a live
// store, driven instead by the raw bytes st.DumpProjections() / `sitectl
// -dump` already produce. A consumer that only has a dump file now composes
// through this function; it never needs to know person_amendment exists.
package domain

import (
	"encoding/json"
	"fmt"

	"sitecontrol/internal/store"
)

// ComposedPersonsFromDump decodes and composes []Person from one saved
// dump's raw bytes — the {"person": [...], "person_amendment": [...], ...}
// object a whole-database dump carries. Only those two sections are read;
// every other key (visit, company, trip, ...) is ignored, exactly as
// Persons(st) ignores every projection but its own two.
//
// A dump with no "person_amendment" section composes as "no amendments
// recorded" rather than an error — the same tolerance a live store gets
// when it genuinely holds none (an old binary's dump predates the side
// table). A dump with no "person" section composes to an empty result; a
// caller that considers an entirely absent "person" section a shape error
// (as opposed to a legitimately person-less store) must check for that key
// itself before calling this — this function's job is composition, not
// validating that the file it was handed is a real dump at all.
func ComposedPersonsFromDump(dumpBytes []byte) ([]Person, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(dumpBytes, &raw); err != nil {
		return nil, fmt.Errorf("domain: ComposedPersonsFromDump: decode dump: %w", err)
	}
	var base []Person
	if b, ok := raw["person"]; ok {
		if err := json.Unmarshal(b, &base); err != nil {
			return nil, fmt.Errorf("domain: ComposedPersonsFromDump: decode \"person\" section: %w", err)
		}
	}
	sideTable := personAmendmentProjection().Name // the one spelling; never a second literal
	var amendments []personAmendmentRow
	if b, ok := raw[sideTable]; ok {
		var wireRows []personAmendmentDumpRow
		if err := json.Unmarshal(b, &wireRows); err != nil {
			return nil, fmt.Errorf("domain: ComposedPersonsFromDump: decode %q section: %w", sideTable, err)
		}
		amendments = make([]personAmendmentRow, 0, len(wireRows))
		for _, w := range wireRows {
			amendments = append(amendments, w.toRow())
		}
	}
	// The dump's row order for this projection IS ascending (site_id,
	// node_epoch, seq) already — personAmendmentProjection's own OrderBy —
	// so no re-sort is needed here, unlike a caller that unions RAW rows
	// from several dumps before composing once (this function composes one
	// dump at a time; a caller combining several posts' dumps composes each
	// independently and unions the resulting []Person, not the raw rows —
	// exactly the shape cmd/dupreport's union step now takes).
	return composePersons(base, amendments), nil
}

// personAmendmentDumpRow is the person_amendment side table's DUMPED JSON
// shape — the wire counterpart of the SQL row queryPersonAmendments scans
// from a live store's `person_amendment` table (personAmendmentProjection's
// CREATE TABLE columns, person_amend.go). Declared here, not anywhere a
// non-domain package could reach it, for the same W3-14 reason the whole
// file exists.
//
// ⟦SchemaVersion 11 — G15, 2026-09-09⟧ `pad_oriented` IS STRUCK and is NOT a
// field here. ⛔ It was one, on repair/dup-report: that branch was cut from
// bbff17e, BEFORE D-88 ERRATA / D-91 struck the fact, and the merge to the tip
// was textually CLEAN because this file is new on the branch — nothing
// conflicted, so nothing asked. TestW5cNoProductionCodeNamesPadOrientation
// caught it.
//
// ⚠ IT WAS NOT MERELY A NAMING VIOLATION. toRow() composed the struck fact
// into personAmendmentRow.Set, which is exactly what the SQL read path is
// forbidden to do — see
// TestD92TheFoldLandsAReplicatedEnvelopeThatStillCarriesPadOriented,
// which asserts no row's Set carries the key. The two SOURCES of one
// composition would have disagreed about a struck fact.
//
// ⛔ DO NOT WRAP A TEST NAME ACROSS TWO COMMENT LINES WITH A HYPHEN. The
// citation checkers match `Test[A-Z][A-Za-z0-9_]{4,}`, which stops at the
// hyphen, so the fragment becomes a name that does not exist — a GHOST, and
// the checkers red on it. Both names above were written that way first.
//
// ⭐ A DUMP THAT STILL CARRIES THE KEY IS STILL ACCEPTED: json.Unmarshal
// ignores unknown keys (DisallowUnknownFields is deliberately not set), so a
// divergent peer's dump decodes and the struck fact is simply not composed —
// the same tolerance the fold gives a stale envelope.
//
// ⛔ THIS LIST IS HAND-KEPT AND THE SQL SIDE IS DERIVED. queryPersonAmendments
// walks personAmendableFacts; these fields are typed out by hand, so the two
// can diverge silently — they already did.
// TestPersonAmendmentDumpRowCoversExactlyTheAmendableFacts binds them.
type personAmendmentDumpRow struct {
	SiteID    string  `json:"site_id"`
	NodeEpoch int64   `json:"node_epoch"`
	Seq       int64   `json:"seq"`
	PersonID  string  `json:"person_id"`
	Name      *string `json:"name"`
	LastName  *string `json:"last_name"`
	FirstName *string `json:"first_name"`
	Phone     *string `json:"phone"`
	CompanyID *string `json:"company_id"`
	TradeID   *string `json:"trade_id"`
	Role      *string `json:"role"`
	Position  *string `json:"position"`
	Retired   *string `json:"retired"`
	SameAs    *string `json:"same_as"`
	Cleared   *string `json:"cleared"` // a JSON-encoded []string, or null/absent — same shape queryPersonAmendments scans from the TEXT column
}

// toRow converts one dumped row into the same personAmendmentRow shape
// composePersons already folds, so this file adds no second composition
// mechanism — only a second SOURCE (JSON dump bytes) for the one
// composition function already has.
func (w personAmendmentDumpRow) toRow() personAmendmentRow {
	r := personAmendmentRow{
		Key:      store.Key{SiteID: w.SiteID, NodeEpoch: w.NodeEpoch, Seq: w.Seq},
		PersonID: w.PersonID,
		Set:      map[string]string{},
	}
	set := func(fact string, v *string) {
		if v != nil {
			r.Set[fact] = *v
		}
	}
	set("name", w.Name)
	set("last_name", w.LastName)
	set("first_name", w.FirstName)
	set("phone", w.Phone)
	set("company_id", w.CompanyID)
	set("trade_id", w.TradeID)
	set("role", w.Role)
	set("position", w.Position)
	set("retired", w.Retired)
	set("same_as", w.SameAs)
	if w.Cleared != nil && *w.Cleared != "" {
		var cleared []string
		if err := json.Unmarshal([]byte(*w.Cleared), &cleared); err == nil {
			r.Cleared = cleared
		}
	}
	return r
}
