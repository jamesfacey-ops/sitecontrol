// muster_person_boundary_test.go — D-28's safety clause, pinned as BEHAVIOUR
// at the domain seam: "the muster NEVER gains alias resolution. A merge
// never changes who is shown on-site." (JAMES-DECISIONS.md:1503), narrowed
// to PERSONS and re-affirmed by D-50 consequence 5 (:2640): "nothing here
// licenses person resolution on the muster or the historical roster."
//
// WHY A BEHAVIOURAL PIN WHEN THREE GUARDS ALREADY EXIST. W6-10's two scans
// (internal/store/w6_source_scans_test.go:1243 and internal/web/
// w6_web_merge_resolve_test.go:1206) flag the SHAPE of a call — an
// AliasMap.Resolve whose argument derives from a PersonID, or a
// LoadPersonMergeMap on the read path. W3-7's HTTP pin (internal/web/
// w3_reader_surfaces_test.go, TestW3MusterNeverResolvesPeopleThroughMerges)
// is behavioural, but its fixture puts ONE of the two merged records on
// site, so it observes a RELABEL and cannot observe a COUNT change.
// [MEASURED, REPORT.md on the repair/muster-alias branch] a fold that reads
// the composed Person.SameAs off the roster slice — no Resolve, no loader —
// and drops a merged-away record's open row whenever its target also has
// one leaves both scans AND the W3 pin GREEN while a human vanishes from the
// sheet. This file is the pin that reddens on it.
//
// WHY THE CLAUSE IS RIGHT, so nobody "fixes" this as an over-count defect:
// D-28 says a wrong merge "is a certainty, not a risk" (43 duplicate
// identities in the NPF extract, 24 inside one employer). Today a wrong
// merge costs a second line — an over-count, a false alarm an officer
// resolves by eye. Fold persons on the roster body and the same wrong merge
// makes a real human VANISH from the sheet during an evacuation. The failure
// DIRECTION is the design. §213(1b) (W5-POSTLOCK-AMENDMENTS.md, 2026-09-04,
// pending its own D-number) rules that merges are ADJUDICATED; whether it
// supersedes D-50 consequence 5 for the roster body is the plan owner's
// question, put to him in reports/laptop-pickup/K-muster-merge-tradeoff.md.
// Until he rules, this pin stands, and a build that folds must red it.
package domain

import (
	"reflect"
	"testing"

	"sitecontrol/internal/store"
)

// pbFixture is one store with one company; every visit pbOpenVisit builds
// is OPEN and snapshots that company, so the ONLY thing that can separate
// two lines is identity.
type pbFixture struct {
	st      *store.Store
	company Company
}

func newPBFixture(t *testing.T) pbFixture {
	t.Helper()
	st := newDomainTestStore(t)
	co, err := CreateCompany(st, "site-a", 1, "Kakivik", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return pbFixture{st: st, company: co}
}

func (f pbFixture) person(t *testing.T, name string) Person {
	t.Helper()
	p, err := CreatePerson(f.st, "site-a", 1, name, &f.company.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (f pbFixture) openVisit(visitID, personID string) Visit {
	name := f.company.Name
	return Visit{VisitID: visitID, SiteID: "site-a", PersonID: personID,
		CompanySnapshotID: &f.company.CompanyID, CompanySnapshotName: &name, IsOpen: 1}
}

// adjudicate authors the officer's merge through the ONLY same_as edge
// (AmendPerson, P-3 §5) — the real mechanism, not a hand-built map.
func (f pbFixture) adjudicate(t *testing.T, dup, canon string) {
	t.Helper()
	if _, err := AmendPerson(f.st, "site-a", 1, PersonAmendment{PersonID: dup, SameAs: &canon}, 0, "default"); err != nil {
		t.Fatalf("fixture: adjudicated person merge %s → %s refused: %v", dup, canon, err)
	}
}

// live renders the LIVE board with exactly the inputs handleMusterLive
// hands MusterLive: the COMPOSED roster (Person.SameAs populated), composed
// companies and trades, and the company and trade merge maps loaded per
// read. minGroupSize 0, so every line sits in a named group.
func (f pbFixture) live(t *testing.T, visits []Visit) MusterBoard {
	t.Helper()
	persons, err := Persons(f.st)
	if err != nil {
		t.Fatal(err)
	}
	companies, err := Companies(f.st)
	if err != nil {
		t.Fatal(err)
	}
	trades, err := Trades(f.st)
	if err != nil {
		t.Fatal(err)
	}
	companyMerge, err := LoadCompanyMergeMap(f.st)
	if err != nil {
		t.Fatal(err)
	}
	tradeMerge, err := LoadTradeMergeMap(f.st)
	if err != nil {
		t.Fatal(err)
	}
	return MusterLive(visits, persons, companies, trades, companyMerge, tradeMerge, 0)
}

func (f pbFixture) reprint(t *testing.T, visits []Visit) MusterBoard {
	t.Helper()
	persons, err := Persons(f.st)
	if err != nil {
		t.Fatal(err)
	}
	return MusterReprint(visits, persons, 0)
}

// pbLines flattens a board to its member lines in render order.
func pbLines(b MusterBoard) []MusterMember {
	var out []MusterMember
	for _, c := range b.Companies {
		for _, w := range c.Trades {
			out = append(out, w.Members...)
		}
	}
	return append(out, b.Summary...)
}

// pbAssertTwoOwnLines is the shape D-28 requires of the sheet for two
// checked-in records: two lines, each carrying its OWN raw PersonID and its
// OWN live name, both visits in the print link, Total 2.
func pbAssertTwoOwnLines(t *testing.T, what string, b MusterBoard, a, c Person) {
	t.Helper()
	if b.Total != 2 {
		t.Fatalf("%s: Total = %d, want 2 — a merge changed WHO IS COUNTED on-site (D-28). Lines: %+v", what, b.Total, pbLines(b))
	}
	got := map[string]MusterMember{}
	for _, m := range pbLines(b) {
		got[m.VisitID] = m
	}
	want := map[string]MusterMember{
		"v-01": {VisitID: "v-01", PersonID: a.PersonID, PersonName: a.Name, CompanyName: "Kakivik", TradeName: NoTradeRecorded},
		"v-02": {VisitID: "v-02", PersonID: c.PersonID, PersonName: c.Name, CompanyName: "Kakivik", TradeName: NoTradeRecorded},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: lines by visit = %+v\n want %+v — every checked-in record keeps its own row, id and name (D-28/D-50 c5)", what, got, want)
	}
	ids := b.VisitIDs()
	if len(ids) != 2 {
		t.Fatalf("%s: the print link freezes %d visit(s) %v, want both — a row missing here is a person missing from every reprint of this sheet", what, len(ids), ids)
	}
}

// TestMusterAnAdjudicatedPersonMergeChangesNothingOnTheSheet is D-28's
// clause as a value: two records, both checked in; the officer adjudicates
// that they are one person; the merge LANDS (the composed roster row says
// so, and the person merge map — which surfaces OFF the muster may consult
// — resolves it); and the live sheet, the reprint, and the print link are
// BYTE-IDENTICAL to what they were before the merge. Then the same rows in
// the opposite order render the same board, which is what lets two posts
// print one sheet from one set of replicated facts.
//
// EXPECTED at HEAD: PASS — this pins shipped behaviour. Its failure mode was
// PRODUCED (REPORT.md): under a SameAs-read fold planted in MusterLive it
// reds with "after the adjudicated merge: Total = 1, want 2".
func TestMusterAnAdjudicatedPersonMergeChangesNothingOnTheSheet(t *testing.T) {
	f := newPBFixture(t)
	canon := f.person(t, "Vega, Ana")
	dup := f.person(t, "Vega, Anna")
	visits := []Visit{f.openVisit("v-01", canon.PersonID), f.openVisit("v-02", dup.PersonID)}

	liveBefore := f.live(t, visits)
	reprintBefore := f.reprint(t, visits)
	pbAssertTwoOwnLines(t, "PRECONDITION, before any merge", liveBefore, canon, dup)

	f.adjudicate(t, dup.PersonID, canon.PersonID)

	// The merge LANDED, in exactly the shape production hands MusterLive
	// (a composed Person with SameAs set) AND on the surface that is
	// allowed to resolve it — so a green below is the muster declining to
	// resolve, not a merge that never happened (rule 6: the fixture must
	// pose the case).
	composed, err := PersonByID(f.st, dup.PersonID)
	if err != nil {
		t.Fatal(err)
	}
	if composed == nil || composed.SameAs == nil || *composed.SameAs != canon.PersonID {
		t.Fatalf("PRECONDITION: the composed roster row for %s does not carry same_as → %s (got %+v)", dup.PersonID, canon.PersonID, composed)
	}
	pm, err := LoadPersonMergeMap(f.st)
	if err != nil {
		t.Fatal(err)
	}
	if got := pm.Resolve(dup.PersonID); got != canon.PersonID {
		t.Fatalf("PRECONDITION: LoadPersonMergeMap resolves %s → %s, want %s", dup.PersonID, got, canon.PersonID)
	}

	liveAfter := f.live(t, visits)
	pbAssertTwoOwnLines(t, "after the adjudicated merge", liveAfter, canon, dup)
	if !reflect.DeepEqual(liveAfter, liveBefore) {
		t.Fatalf("after the adjudicated merge the LIVE board differs from before it — a merge changed what the muster shows (D-28):\n before %+v\n after  %+v", liveBefore, liveAfter)
	}
	if reprintAfter := f.reprint(t, visits); !reflect.DeepEqual(reprintAfter, reprintBefore) {
		t.Fatalf("after the adjudicated merge the REPRINT differs from before it:\n before %+v\n after  %+v", reprintBefore, reprintAfter)
	}

	// Both posts: the same replicated rows, read in the other order.
	reversed := []Visit{visits[1], visits[0]}
	if got := f.live(t, reversed); !reflect.DeepEqual(got, liveAfter) {
		t.Fatalf("the live board depends on visit input order — two posts could print different sheets from the same rows:\n forward  %+v\n reversed %+v", liveAfter, got)
	}
}

// TestMusterTwoHumansWithOneNameStayTwoLines is the arm §213(1b) forbids
// automating: two DIFFERENT humans who share a name, no adjudication of any
// kind, must be two lines — the muster neither merges by name nor by
// anything else it can see. (A build that folded on the rendered name
// would fail here and pass the adjudicated-merge test above; the two are
// deliberately separate arms.)
func TestMusterTwoHumansWithOneNameStayTwoLines(t *testing.T) {
	f := newPBFixture(t)
	a := f.person(t, "Vega, Ana")
	b := f.person(t, "Vega, Ana")
	if a.PersonID == b.PersonID {
		t.Fatal("fixture: two CreatePerson calls minted one id")
	}
	visits := []Visit{f.openVisit("v-01", a.PersonID), f.openVisit("v-02", b.PersonID)}
	pbAssertTwoOwnLines(t, "two humans, one name, no adjudication", f.live(t, visits), a, b)
}
