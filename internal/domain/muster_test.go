// muster_test.go — C9 done-when: the JET regression test (numeric, not
// textual, crew-size comparison) and the live-vs-reprint asymmetry a dev
// alias must produce (design §8.8-iv).
package domain

import (
	"testing"

	"sitecontrol/internal/store"
)

// mkVisit builds a synthetic OPEN visit with the given snapshot fields,
// bypassing authoring entirely — buildMuster only reads Visit/Person rows,
// so this is enough to drive it directly without a store, matching this
// project's convention of testing folds/views over injected data (trip_test.go).
func mkVisit(visitID, personID, companyID, companyName, tradeID, tradeName string) Visit {
	return Visit{
		VisitID: visitID, SiteID: "site-a", PersonID: personID,
		CompanySnapshotID: &companyID, CompanySnapshotName: &companyName,
		TradeSnapshotID: &tradeID, TradeSnapshotName: &tradeName,
		IsOpen: 1,
	}
}

// crewOfSize builds a company/workgroup crew of exactly n open visits, all
// in one workgroup, so MusterReprint's per-workgroup bucket size is exactly n.
func crewOfSize(n int, companyID, tradeID string) ([]Visit, []Person) {
	var visits []Visit
	var persons []Person
	for i := 0; i < n; i++ {
		id := companyID + "-" + tradeID + "-p" + itoa(i)
		visits = append(visits, mkVisit(id+"-visit", id, companyID, companyID+" Co", tradeID, tradeID+" Crew"))
		persons = append(persons, Person{PersonID: id, SiteID: "site-a", Name: "Worker " + id})
	}
	return visits, persons
}

// itoa avoids pulling in strconv just for test ids.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// TestMusterFoldingIsNumericNotTextual is THE JET regression test named in
// the chunk brief: the Access system this project replaces used Nz(), which
// made JET compare crew sizes AS TEXT, so "13" < "2" evaluated true and a
// 13-person crew was silently folded onto the shared summary sheet. This
// test asserts the numeric behaviour explicitly at sizes where text and
// numeric comparison DISAGREE, so it would fail under a text-comparison
// implementation — it does not merely check "some folding happens."
func TestMusterFoldingIsNumericNotTextual(t *testing.T) {
	cases := []struct {
		size           int
		threshold      int
		wantFolded     bool
		textCompareBug bool // documents what a "< as string" bug would (wrongly) do, for the comment trail only
	}{
		// The canonical named failure: threshold 5, size 13. Under TEXT
		// comparison "13" < "5" is TRUE (lexicographic: '1' < '5'), so a
		// text-comparison bug would fold this crew. Numerically 13 >= 5, so
		// it must NOT fold.
		{size: 13, threshold: 5, wantFolded: false, textCompareBug: true},

		// F21 (test-suite audit mutation M2): the THRESHOLD BOUNDARY itself
		// — a crew of EXACTLY minCrewSize. §11/muster.go's own wording is
		// "crews BELOW MusterMinCrewSize" (strictly less than), so an exact
		// match must get its own listed group, never fold. `<` -> `<=` in
		// buildMuster's fold condition stayed green with no case at the
		// boundary; this one pins it on both sides.
		{size: 5, threshold: 5, wantFolded: false}, // 5 is NOT < 5: must NOT fold
		{size: 4, threshold: 5, wantFolded: true},  // 4 < 5: must fold — the boundary's other side

		// Brief-mandated coverage: 2, 10, 100 against thresholds 3 and 5.
		{size: 2, threshold: 3, wantFolded: true},   // 2 < 3 numerically: correctly folds
		{size: 2, threshold: 5, wantFolded: true},   // 2 < 5 numerically: correctly folds
		{size: 10, threshold: 3, wantFolded: false}, // 10 >= 3; "10" < "3" is also true as text (bug would ALSO wrongly fold this one)
		{size: 10, threshold: 5, wantFolded: false}, // 10 >= 5; "10" < "5" also true as text — another bug case
		{size: 100, threshold: 3, wantFolded: false},
		{size: 100, threshold: 5, wantFolded: false},
		{size: 13, threshold: 3, wantFolded: false}, // 13 >= 3; "13" < "3" true as text too
	}
	for _, c := range cases {
		visits, persons := crewOfSize(c.size, "co1", "wg1")
		board := MusterReprint(visits, persons, c.threshold)

		foundAsGroup := len(board.Companies) == 1 && len(board.Companies[0].Trades) == 1 &&
			len(board.Companies[0].Trades[0].Members) == c.size
		inSummary := len(board.Summary) == c.size && len(board.Companies) == 0

		switch {
		case c.wantFolded && !inSummary:
			t.Fatalf("size=%d threshold=%d: want folded into Summary, got Companies=%+v Summary=%d",
				c.size, c.threshold, board.Companies, len(board.Summary))
		case !c.wantFolded && !foundAsGroup:
			t.Fatalf("size=%d threshold=%d: want its OWN group (numeric %d >= %d), got Companies=%+v Summary=%d — this is the named JET failure if it folded",
				c.size, c.threshold, c.size, c.threshold, board.Companies, len(board.Summary))
		}
		if board.Total != c.size {
			t.Fatalf("size=%d threshold=%d: board.Total = %d, want %d", c.size, c.threshold, board.Total, c.size)
		}
	}
}

// TestMusterGroupsCompanyThenTrade is the basic §11 shape assert: two
// workgroups under one company, both above threshold, render as two nested
// groups under one company group, not flattened or merged.
func TestMusterGroupsCompanyThenTrade(t *testing.T) {
	var visits []Visit
	var persons []Person
	v1, p1 := crewOfSize(6, "co1", "wgA")
	v2, p2 := crewOfSize(7, "co1", "wgB")
	visits = append(append(visits, v1...), v2...)
	persons = append(append(persons, p1...), p2...)

	board := MusterReprint(visits, persons, 5)
	if len(board.Companies) != 1 {
		t.Fatalf("companies = %d, want 1 (both workgroups share co1)", len(board.Companies))
	}
	if got := len(board.Companies[0].Trades); got != 2 {
		t.Fatalf("workgroups under co1 = %d, want 2", got)
	}
	if len(board.Summary) != 0 {
		t.Fatalf("summary = %d, want 0 (both crews are above threshold 5)", len(board.Summary))
	}
}

// TestReprintNeverResolvesAliasEvenWhenOneExists is the live-vs-reprint
// asymmetry (design §8.8-iv, chunk-plan C9's "the other trap"): after two
// duplicate reference rows are merged, the LIVE board must show one merged
// crew while a REPRINT of the same underlying visits — visits that predate
// the merge — must still show two. This proves it at the type level:
// MusterReprint is handed no AliasMap at all, so there is no way for it to
// re-resolve, and this test would fail if a future edit gave it one and used
// it.
//
// ⟦W5d-5 — POST-LOCK AMENDMENT (plan owner, 2026-09-01). The LIVE half below
// is INVERTED against the version this test shipped with, deliberately, and
// it is the DOMAIN twin of the amendment post-lock §6 applied to
// internal/web/muster_test.go's
// TestMusterPrintReprintNeverResolvesEvenAfterADevAlias.⟧ It used to assert
// that a `ref_alias` row — the C5/M1 dev-alias
// mechanism — merged the two company groups on the live board. **W5d-5
// requires the opposite in terms**: the muster's company grouping re-points
// to composed `company_amended.same_as`, and W3-4's exemption for that
// grouping (granted only "because its composed source does not exist until
// W5d") EXPIRES at W5d.
//
// ⚠ Why this survived W5d's build while its web twin did not, which is the
// part worth reading: W5d's builder was granted a MECHANICAL call-site
// exception for `MusterLive`'s new signature (post-lock §2 — "pass the new
// argument", "no assertion may be added, removed, weakened or re-worded").
// Under that grant the only argument that preserved what this test observed
// was the legacy `ref_alias` map, passed into BOTH parameters, so the
// assertion stayed green under a wiring production never uses. That was the
// correct move under the grant; the grant is what was wrong, and post-lock
// §2's bound now carries the sentence the W5d gate wrote for it: *a
// mechanical-argument-passing grant is not behaviour-neutral when the new
// parameter is the one whose behaviour changed.* Found by the W5d gate
// (reports/window-build/W5d-falsification.md §6 G-3), ruled and applied in
// reports/W5-POSTLOCK-AMENDMENTS.md.
//
// ⚠ The REPRINT half is UNTOUCHED and must stay so. It is a DIFFERENT
// guarantee — D-27's frozen snapshot, the C9 not-done-if "a reprint that
// re-resolves fails" — it still holds, and after this amendment it holds
// against BOTH merge mechanisms at once rather than one.
func TestReprintNeverResolvesAliasEvenWhenOneExists(t *testing.T) {
	st := newDomainTestStore(t)

	// Two companies minted independently — the design's own §8.7 case 2
	// worked example: "Kakivik" at one post, "Kakavik" (misspelled) at
	// another. Both coexist; no arbitration at authoring time.
	kakivik, err := CreateCompany(st, "site-a", 1, "Kakivik", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	kakavik, err := CreateCompany(st, "site-a", 1, "Kakavik", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	alice, err := CreatePerson(st, "site-a", 1, "Alice", &kakivik.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := CreatePerson(st, "site-a", 1, "Bob", &kakavik.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	// The pre-alias sheet: two visits, snapshotted BEFORE any alias exists.
	kakivikName, kakavikName := "Kakivik", "Kakavik"
	preAliasVisits := []Visit{
		{VisitID: "v1", SiteID: "site-a", PersonID: alice.PersonID, CompanySnapshotID: &kakivik.CompanyID, CompanySnapshotName: &kakivikName, IsOpen: 1},
		{VisitID: "v2", SiteID: "site-a", PersonID: bob.PersonID, CompanySnapshotID: &kakavik.CompanyID, CompanySnapshotName: &kakavikName, IsOpen: 1},
	}
	persons := []Person{alice, bob}

	// Sanity: before any alias exists, both live and reprint show two groups.
	emptyAliasMap := AliasMap{}
	liveBefore := MusterLive(preAliasVisits, persons, []Company{kakivik, kakavik}, nil, emptyAliasMap, emptyAliasMap, 0)
	if len(liveBefore.Companies) != 2 {
		t.Fatalf("pre-alias live: companies = %d, want 2", len(liveBefore.Companies))
	}

	// ⚠⚠ ⟦RETIRED — §60, 2026-09-03⟧ KIND C (sites 10–11) — ⛔ NO SUBSTITUTION, then (1).
	// Superseded lines preserved verbatim:
	//   ⟦was: "// The dev alias route's own mechanism: an ordinary CreateAlias call"⟧
	//   ⟦was: "// (chunk-plan C5/M1) linking the duplicate to the canonical."⟧
	//   ⟦was: "if _, err := CreateAlias(st, \"site-a\", 1, kakavik.CompanyID, kakivik.CompanyID, 0, \"default\"); err != nil {"⟧
	//   ⟦was: "\tt.Fatalf(\"create alias: %v\", err)"⟧
	//   ⟦was: "}"⟧
	//   ⟦was: "aliasMap, err := LoadAliasMap(st)"⟧
	//   ⟦was: "if err != nil {"⟧
	//   ⟦was: "\tt.Fatal(err)"⟧
	//   ⟦was: "}"⟧
	//   ⟦was: "if got := aliasMap.Resolve(kakavik.CompanyID); got != kakivik.CompanyID {"⟧
	//   ⟦was: "\tt.Fatalf(\"alias map resolve = %s, want %s\", got, kakivik.CompanyID)"⟧
	//   ⟦was: "}"⟧
	//
	// WHAT IT PROVED: it built the DISTRACTOR — a live second-mechanism edge over the same
	// duplicate pair — so that the negative arm below meant something.
	// WHICH RULING REMOVED ITS SUBJECT: P-3 §5. ⛔ §60 BARS the tempting substitution here:
	// putting a composed AmendCompany edge at this line MERGES the pair at the negative arm
	// and reds it — the assertion inverts, in the direction that goes green once the arm is
	// "fixed" to expect one group. The whole content was the CONTRAST between two
	// mechanisms; collapsing them destroys it.
	//
	// ⭐ DISPOSITION (2) WAS EVALUATED, AND REJECTED — ⚠️ THIS IS A JUDGEMENT BETWEEN TWO OF
	// §60'S OWN DISPOSITIONS AND IS FLAGGED FOR THE PLAN OWNER, NOT SETTLED HERE.
	// A surviving rival DOES exist in principle: scope §5 measured that `Apply` has NO
	// event-type allowlist, so a retired `ref_alias_created` envelope can still be injected
	// at the WIRE level and would fold to nothing — which would re-arm this exact negative
	// arm with a constructible distractor.
	// ⚠ IT WAS NOT TAKEN, because it contradicts §60's OWN disposition for sites 7–8, where
	// a fixture was required to STOP injecting that retired type ("determinism over a
	// vocabulary that no longer contains the term"). Adding an injection here while removing
	// one there would apply two opposite readings of D-47 in one pass. If the plan owner
	// prefers the wire-level rival, this arm is re-armable in about ten lines and the
	// superseded text above is the starting point.

	companies, err := Companies(st)
	if err != nil {
		t.Fatal(err)
	}

	// ⟦W5d-5⟧ The company merge map, loaded the way PRODUCTION loads it:
	// internal/web/muster.go:344 passes LoadCompanyMergeMap's answer as
	// MusterLive's companyMerge argument and LoadAliasMap's as the workgroup
	// one. Passing `aliasMap` into both — which is what stood here — is a
	// wiring no caller has.
	companyMerge, err := LoadCompanyMergeMap(st)
	if err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — §60, 2026-09-03⟧ Precondition (rule 6), KEPT and re-pointed — it is NOT part
	// of the retirement above. Its live purpose survives: it proves the merge asserted
	// further down is caused BY THE AMENDMENT and not by something already in the map.
	// ⟦was: "// Precondition (rule 6): the `ref_alias` row above must NOT have reached"⟧
	// ⟦was: "// the composed merge map, or the negative arm below would be green for"⟧
	// ⟦was: "// the wrong reason — two groups because nothing merges anything, rather"⟧
	// ⟦was: "// than because company grouping stopped reading `ref_alias`."⟧
	if got := companyMerge.Resolve(kakavik.CompanyID); got != kakavik.CompanyID {
		t.Fatalf("precondition: the composed company merge map already resolves %s → %s before any "+
			"company_amended.same_as edge exists — the merge asserted below would then be green for the wrong "+
			"reason", kakavik.CompanyID, got)
	}

	// ⚠⚠ ⟦RETIRED — §60, 2026-09-03⟧ KIND C (the negative arm belonging to sites 10–11).
	// Superseded lines preserved verbatim:
	//   ⟦was: "// ⟦W5d-5⟧ LIVE, post-ref_alias: STILL TWO groups. This is the half that"⟧
	//   ⟦was: "// inverted, and it is the whole content of the re-point — company"⟧
	//   ⟦was: "// grouping no longer reads `ref_alias` at all. (Workgroup grouping still"⟧
	//   ⟦was: "// does, until W8 deletes the mechanism; DR-4 is why the two resolvers are"⟧
	//   ⟦was: "// separate parameters, and passing aliasMap as the workgroup argument"⟧
	//   ⟦was: "// here is production's wiring, not a shortcut.)"⟧
	//   ⟦was: "liveAfterAlias := MusterLive(preAliasVisits, persons, companies, nil, companyMerge, aliasMap, 0)"⟧
	//   ⟦was: "if len(liveAfterAlias.Companies) != 2 {"⟧
	//   ⟦was: "\tt.Fatalf(\"post-ref_alias LIVE: companies = %d, want 2 — after W5d-5 a ref_alias row must NOT merge company groups on the live board\", len(liveAfterAlias.Companies))"⟧
	//   ⟦was: "}"⟧
	//   ⟦was: "for _, want := range []string{\"Kakivik\", \"Kakavik\"} { … both duplicates must still stand as their own groups }"⟧
	//
	// WHAT IT PROVED: that a live second-mechanism edge did NOT merge the company groups.
	// WHICH RULING REMOVED ITS SUBJECT: P-3 §5 — with the distractor above unconstructible,
	// this arm would assert "two groups when nothing merges anything", which the PRE-alias
	// sanity check thirty lines up already makes, and would read as coverage while testing
	// nothing about the re-point.
	// ⭐ COVERAGE COST — NAMED: the specific protection lost is "company grouping does not
	// read a SECOND resolution path". ⚠ Not lost entirely: the kind-partition that makes a
	// second path unconstructible is asserted directly in w5d_muster_test.go, where the
	// COMPANY and TRADE merge maps are required to be DISJOINT (§60 (3), re-armed there).

	// ⟦W5d-5⟧ ...and the mechanism that DOES merge them now: a composed
	// `company_amended.same_as` edge over the same duplicate pair. Asserting
	// only the negative above would leave a build that merges NOTHING green —
	// it would certify the DELETION of company grouping rather than its
	// re-pointing.
	if _, err := AmendCompany(st, "site-a", 1, CompanyAmendment{
		CompanyID: kakavik.CompanyID, SameAs: &kakivik.CompanyID,
	}, 0, "default"); err != nil {
		t.Fatalf("amend company (composed same_as merge): %v", err)
	}
	companyMerge, err = LoadCompanyMergeMap(st)
	if err != nil {
		t.Fatal(err)
	}
	if got := companyMerge.Resolve(kakavik.CompanyID); got != kakivik.CompanyID {
		t.Fatalf("composed company merge map resolves %s → %s, want %s — the same_as edge did not land", kakavik.CompanyID, got, kakivik.CompanyID)
	}
	companies, err = Companies(st)
	if err != nil {
		t.Fatal(err)
	}

	// LIVE, post-merge: the two pre-merge visits now resolve to ONE company group.
	// ⟦was: "liveAfter := MusterLive(preAliasVisits, persons, companies, nil, companyMerge, aliasMap, 0)"⟧
	liveAfter := MusterLive(preAliasVisits, persons, companies, nil, companyMerge, emptyAliasMap, 0)
	if len(liveAfter.Companies) != 1 {
		t.Fatalf("post-merge LIVE: companies = %d, want 1 (Kakivik/Kakavik must merge on the live board through composed same_as)", len(liveAfter.Companies))
	}
	if got := liveAfter.Companies[0].CompanyName; got != "Kakivik" {
		t.Fatalf("post-merge LIVE merged group is labelled %q, want %q (the canonical survivor)", got, "Kakivik")
	}
	if got := len(liveAfter.Companies[0].Trades[0].Members); got != 2 {
		t.Fatalf("post-merge LIVE merged group has %d members, want 2", got)
	}

	// REPRINT of the SAME pre-alias visit rows, called AFTER the alias
	// exists: must STILL show two groups — the named not-done-if ("a reprint
	// that re-resolves fails"). MusterReprint never even sees a merge map.
	reprintAfter := MusterReprint(preAliasVisits, persons, 0)
	if len(reprintAfter.Companies) != 2 {
		t.Fatalf("post-alias REPRINT: companies = %d, want 2 — a reprint must reproduce what was printed then, never re-resolve", len(reprintAfter.Companies))
	}
}

// TestMusterOnlyIncludesOpenVisits: §11 is a view over OPEN visits; a
// checked-out visit must not appear on either board.
func TestMusterOnlyIncludesOpenVisits(t *testing.T) {
	name := "Co"
	open := Visit{VisitID: "v-open", SiteID: "site-a", PersonID: "p1", CompanySnapshotID: strPtr("c1"), CompanySnapshotName: &name, IsOpen: 1}
	closed := Visit{VisitID: "v-closed", SiteID: "site-a", PersonID: "p2", CompanySnapshotID: strPtr("c1"), CompanySnapshotName: &name, IsOpen: 0}
	persons := []Person{{PersonID: "p1", Name: "P1"}, {PersonID: "p2", Name: "P2"}}

	board := MusterReprint([]Visit{open, closed}, persons, 0)
	if board.Total != 1 {
		t.Fatalf("total = %d, want 1 (checked-out visit must be excluded)", board.Total)
	}
}

func strPtr(s string) *string { return &s }

// TestVisitsByIDFreezesExactSet: the reprint route's filter must return
// exactly the requested ids, ignoring anything else in the store — this is
// what lets a printed sheet's roster survive later check-ins/check-outs.
func TestVisitsByIDFreezesExactSet(t *testing.T) {
	name := "Co"
	all := []Visit{
		{VisitID: "v1", CompanySnapshotName: &name},
		{VisitID: "v2", CompanySnapshotName: &name},
		{VisitID: "v3", CompanySnapshotName: &name},
	}
	got, err := VisitsByID(all, []string{"v3", "v1"})
	if err != nil {
		t.Fatalf("VisitsByID: unexpected error: %v", err)
	}
	if len(got) != 2 || got[0].VisitID != "v3" || got[1].VisitID != "v1" {
		t.Fatalf("VisitsByID = %+v, want [v3 v1] in requested order", got)
	}
	if got, err := VisitsByID(all, nil); got != nil || err != nil {
		t.Fatalf("VisitsByID(nil ids) = %+v, %v, want nil, nil", got, err)
	}
}

// TestVisitsByIDErrorsRatherThanSilentlyDroppingAnUnknownID is the
// verification-round fix: a muster sheet is a safety artifact, so a caller
// asking for an id that no longer resolves must be TOLD, not handed a
// shorter slice that quietly omits someone.
func TestVisitsByIDErrorsRatherThanSilentlyDroppingAnUnknownID(t *testing.T) {
	name := "Co"
	all := []Visit{{VisitID: "v1", CompanySnapshotName: &name}}

	got, err := VisitsByID(all, []string{"unknown"})
	if err == nil {
		t.Fatal("expected an error for an unresolvable visit id, got nil")
	}
	if got != nil {
		t.Fatalf("VisitsByID on error = %+v, want nil (never a partial result)", got)
	}

	// A mix of known and unknown ids must ALSO error — partial success is
	// exactly the silent-drop failure mode being guarded against here.
	got, err = VisitsByID(all, []string{"v1", "unknown"})
	if err == nil {
		t.Fatal("expected an error when even ONE requested id is unresolvable, got nil")
	}
	if got != nil {
		t.Fatalf("VisitsByID on partial-miss error = %+v, want nil, not the 1 match it did find", got)
	}
}

// TestMusterKeepsAPersonWhoseRosterRowHasNotYetReplicated is F17 (test-suite
// audit mutation M15): buildMuster's fallback — "roster row missing/not
// replicated yet: still show the id, never drop the person" (muster.go) —
// was guarded only by that comment; replacing it with a skip left the whole
// suite green, because no test ever posed an open visit for a person absent
// from the persons projection. This is the exact failure the muster exists
// to prevent: someone on site, missing from the printed evacuation sheet.
//
// The scenario is built the way it actually happens, not by deleting a row
// from a table: buildArrivalVisits (tripauthor.go) refuses to author an
// arrival for anyone absent from the AUTHORING site's OWN local roster, so
// a single site can never produce a visit that outruns its own person event
// — the only way to reach this state is across sites. A REPLICA pulling two
// independent site streams during a from-zero catch-up can apply one site's
// trip_arrival envelope before it has reached the other site's
// person_created envelope: per-stream isolation preserves each site's own
// order but promises nothing about the order BETWEEN sites (the same seam
// F8 found broken; determinism_test.go's foreignRefStream/interleave use
// this identical technique to pose it). This constructs that: two
// hand-built envelopes on two different site streams, applied to a blank
// replica store in the order a stalled pull would actually deliver them.
func TestMusterKeepsAPersonWhoseRosterRowHasNotYetReplicated(t *testing.T) {
	replica := newDomainTestStore(t)

	// Person P was created at site-x. This event is real and will arrive —
	// just not yet.
	personID := "site-x-person-9999999999999999"
	personEnv := store.Envelope{
		SiteID: "site-x", NodeEpoch: 1, Seq: 1,
		SchemaVersion: store.SchemaVersion, OriginAuth: "site-x",
		Payload: mustJSON(t, PersonCreated{Type: "person_created", PersonID: personID, Name: "Not Yet Replicated"}),
	}

	// P travelled to site-y and was checked in there. site-y's own local
	// roster already had P by the time it authored this (buildArrivalVisits
	// requires it) — this is a genuine trip_arrival payload shape, just
	// hand-built here (as foreignRefStream does) because a REPLICA needs to
	// see two independent site streams to pose the reorder.
	companyID, companyName := "site-y-company-1", "Northern Air"
	tradeID, tradeName := "site-y-workgroup-1", "Flight Crew"
	arrivalEnv := store.Envelope{
		SiteID: "site-y", NodeEpoch: 1, Seq: 1,
		SchemaVersion: store.SchemaVersion, OriginAuth: "site-y",
		Payload: mustJSON(t, TripArrival{
			Type: "trip_arrival", ArrivalID: "site-y-arrival-1",
			VehicleEntered: strPtr("TRK-1"), OriginSite: strPtr("site-x"),
			Visits: []ArrivalVisit{{
				VisitID: "site-y-visit-1", PersonID: personID,
				CompanyID: &companyID, CompanyName: &companyName,
				TradeID: &tradeID, TradeName: &tradeName,
			}},
		}),
	}

	// The replica's from-zero pull has drained site-y's stream but has not
	// yet reached site-x's — a legitimate mid-catch-up state, not an error.
	if _, err := replica.Apply(arrivalEnv); err != nil {
		t.Fatalf("apply arrival: %v", err)
	}

	visits, err := Visits(replica)
	if err != nil {
		t.Fatal(err)
	}
	persons, err := Persons(replica)
	if err != nil {
		t.Fatal(err)
	}
	if len(persons) != 0 {
		t.Fatalf("precondition broken: persons = %+v, want none yet (person_created not applied)", persons)
	}

	board := MusterLive(visits, persons, nil, nil, AliasMap{}, AliasMap{}, 0)
	if board.Total != 1 {
		t.Fatalf("board.Total = %d, want 1 — the person must still be shown, never silently dropped", board.Total)
	}
	var found *MusterMember
	for i := range board.Companies {
		for _, wg := range board.Companies[i].Trades {
			for j := range wg.Members {
				if wg.Members[j].PersonID == personID {
					found = &wg.Members[j]
				}
			}
		}
	}
	if found == nil {
		t.Fatalf("person %s is missing entirely from the muster board — this is F17: someone on site, missing from the evacuation muster", personID)
	}
	if found.PersonName != personID {
		t.Fatalf("PersonName = %q, want the raw id %q (roster row not yet replicated: fall back to the id, don't drop or blank the row)", found.PersonName, personID)
	}

	// The seam heals: site-x's stream catches up. The SAME visit, unchanged,
	// must now show the real name — proving this is a genuine fallback and
	// not a permanent id-display quirk.
	if _, err := replica.Apply(personEnv); err != nil {
		t.Fatalf("apply person: %v", err)
	}
	persons, err = Persons(replica)
	if err != nil {
		t.Fatal(err)
	}
	board = MusterLive(visits, persons, nil, nil, AliasMap{}, AliasMap{}, 0)
	if board.Total != 1 {
		t.Fatalf("after catch-up: board.Total = %d, want 1", board.Total)
	}
	if got := board.Companies[0].Trades[0].Members[0].PersonName; got != "Not Yet Replicated" {
		t.Fatalf("after catch-up: PersonName = %q, want the real name now that the roster row has arrived", got)
	}
}
