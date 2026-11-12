// muster_order_test.go — ⟦§101, 2026-09-03⟧ THE PRODUCED FAILURE for the muster's
// non-total sort comparators.
//
// WHY THESE TESTS EXIST AT ALL, stated here because a tiebreak nobody has seen
// matter is a line somebody deletes as noise in six months. muster.go sorted the
// board on a DISPLAY NAME alone, with no id tiebreak, where every neighbouring
// sort in this codebase carries one (intake.go:381 by ID, trade.go:199 by TradeID,
// picker.go:215 by Value, trips.go:433/746 by PersonID/TripID, in_transit.go:375
// by PersonID). muster.go was the outlier, on the one document that is read aloud
// during an evacuation.
//
// ⚠ TIES ARE DESIGNED FOR, NOT HYPOTHETICAL. ids.go's own file comment makes
// duplicate reference data with matching NAMES and different IDS the expected
// steady state — "two sites minting a 'Kakivik' company at the same time can
// never collide on ID even though the name matches" — and MusterReprint takes no
// AliasMap BY DESIGN, so on a printed sheet the duplicates are never merged away
// and the tie is live.
//
// ⭐ WHAT IS AND IS NOT WRONG, because the distinction is the whole finding.
// `sort.Slice` is deterministic for a given input, and `visit_id` persists, so the
// SAME DATA PRINTS THE SAME ORDER at every post, every time. The sheet does NOT
// reorder between two prints of one roster. What is wrong is that the order among
// EQUALLY-NAMED groups is settled by a crypto/rand hex id (ids.go:22-27) rather
// than by any rule, and therefore MOVES when an unrelated person checks in or out
// — an officer sees two sections swap on a life-safety document for a reason that
// is nowhere on it. That is what each test below reproduces.
//
// THE FAITHFUL MODEL. `OpenVisitsAt` reads ORDER BY visit_id (tripauthor.go:794,
// store/projection.go:159), so the ONLY faithful input order is visit-id order.
// openVisitsInStoreOrder sorts on exactly that, and every fixture below states its
// visit ids explicitly rather than minting random ones — the randomness is the
// SUBJECT here, so it may not also be the method.
package domain

import (
	"sort"
	"testing"
)

// openVisitsInStoreOrder models OpenVisitsAt's ORDER BY visit_id.
func openVisitsInStoreOrder(visits ...Visit) []Visit {
	out := append([]Visit(nil), visits...)
	sort.Slice(out, func(i, j int) bool { return out[i].VisitID < out[j].VisitID })
	return out
}

// companyOrder names the company sections in the order they are rendered,
// by ID — the sections are all NAMED the same in these fixtures, which is
// the point, so the name cannot identify which one moved.
func companyOrder(b MusterBoard) []string {
	var out []string
	for _, c := range b.Companies {
		out = append(out, c.CompanyID)
	}
	return out
}

func tradeOrder(b MusterBoard) []string {
	var out []string
	for _, c := range b.Companies {
		for _, w := range c.Trades {
			out = append(out, w.TradeID)
		}
	}
	return out
}

// summaryOrder names the pooled rows as the sheet prints them: the summary
// table renders PersonName, CompanyName and TradeName per row (muster.html:333),
// so two people with ONE name and DIFFERENT companies are distinguishable on
// paper and their row order is observable.
func summaryOrder(b MusterBoard) []string {
	var out []string
	for _, m := range b.Summary {
		out = append(out, m.PersonName+" | "+m.CompanyName)
	}
	return out
}

func sameOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestMusterCompanySectionOrderIsUnchangedByAnUnrelatedCheckIn — muster.go's
// `sort.Slice(board.Companies, … CompanyName < CompanyName)`.
//
// TWO POSTS EACH MINTED A "Kakivik" COMPANY (§8.7's designed-for duplicate).
// A THIRD Kakivik-A worker then checks in. Nothing about company B changed, and
// nothing about the ORDER of the two sections should change either.
func TestMusterCompanySectionOrderIsUnchangedByAnUnrelatedCheckIn(t *testing.T) {
	coA, coB := "site-a-company-aaaa", "site-b-company-bbbb"
	before := openVisitsInStoreOrder(
		mkVisit("v-500", "p-bob", coB, "Kakivik", "t-b", "Inspection"),
		mkVisit("v-900", "p-ann", coA, "Kakivik", "t-a", "Inspection"),
	)
	// The unrelated check-in: one more Kakivik-A worker, whose visit id happens
	// to sort first. It is UNRELATED to company B in every sense except that it
	// is on the same sheet.
	after := openVisitsInStoreOrder(append(append([]Visit(nil), before...),
		mkVisit("v-100", "p-cal", coA, "Kakivik", "t-a", "Inspection"))...)

	first := companyOrder(MusterReprint(before, nil, 0))
	second := companyOrder(MusterReprint(after, nil, 0))
	if !sameOrder(first, second) {
		t.Errorf("the two identically-named COMPANY sections swapped places because an unrelated person checked in.\n"+
			"  before the check-in: %v\n  after  the check-in: %v\n"+
			"muster.go sorts board.Companies on CompanyName alone, so two companies with one name are a TIE, and the tie is "+
			"settled by whichever visit id happened to open the bucket first — a crypto/rand hex string (ids.go:22-27). "+
			"An officer reading the sheet sees two sections swap for a reason that appears nowhere on it.", first, second)
	}
}

// TestMusterTradeSectionOrderIsUnchangedByAnUnrelatedCheckIn — muster.go's
// per-company `sort.Slice(wgs, … TradeName < TradeName)`. Same shape, one level
// down: ONE company, two trade records that carry the same recorded name.
func TestMusterTradeSectionOrderIsUnchangedByAnUnrelatedCheckIn(t *testing.T) {
	const co = "site-a-company-1111"
	trA, trB := "site-a-trade-aaaa", "site-b-trade-bbbb"
	before := openVisitsInStoreOrder(
		mkVisit("v-500", "p-bob", co, "Kakivik", trB, "Inspection"),
		mkVisit("v-900", "p-ann", co, "Kakivik", trA, "Inspection"),
	)
	after := openVisitsInStoreOrder(append(append([]Visit(nil), before...),
		mkVisit("v-100", "p-cal", co, "Kakivik", trA, "Inspection"))...)

	first := tradeOrder(MusterReprint(before, nil, 0))
	second := tradeOrder(MusterReprint(after, nil, 0))
	if !sameOrder(first, second) {
		t.Errorf("the two identically-named TRADE sections swapped places because an unrelated person checked in.\n"+
			"  before the check-in: %v\n  after  the check-in: %v\n"+
			"muster.go sorts a company's Trades on TradeName alone.", first, second)
	}
}

// TestMusterSummaryRowOrderIsUnchangedByAnUnrelatedCheckOut — muster.go's
// `sort.Slice(board.Summary, … PersonName < PersonName)`.
//
// TWO MEN NAMED JOHN SMITH, in different companies, both on the pooled summary.
// The summary table prints PersonName, CompanyName and TradeName per row, so
// which John is on which row is VISIBLE — and a third person checking OUT must
// not swap them.
func TestMusterSummaryRowOrderIsUnchangedByAnUnrelatedCheckOut(t *testing.T) {
	const minGroupSize = 3 // both crews are below it, so both pool into Summary
	coX, coY := "site-a-company-xxxx", "site-a-company-yyyy"
	persons := []Person{
		{PersonID: "p-jx", SiteID: "site-a", Name: "John Smith"},
		{PersonID: "p-jy", SiteID: "site-a", Name: "John Smith"},
		{PersonID: "p-xav", SiteID: "site-a", Name: "Xavier Ecks"},
		{PersonID: "p-yol", SiteID: "site-a", Name: "Yolanda Yu"},
	}
	jx := mkVisit("v-950", "p-jx", coX, "Kakivik", "t-x", "Inspection")
	jy := mkVisit("v-500", "p-jy", coY, "Nanuq", "t-y", "Welding")
	yol := mkVisit("v-550", "p-yol", coY, "Nanuq", "t-y", "Welding")
	// Xavier's visit is what holds company X's bucket open FIRST. He is on
	// neither John's crew and his checking out says nothing about either.
	xav := mkVisit("v-100", "p-xav", coX, "Kakivik", "t-x", "Inspection")

	before := openVisitsInStoreOrder(xav, jx, jy, yol)
	after := openVisitsInStoreOrder(jx, jy, yol) // Xavier checked out

	first := summaryOrder(MusterReprint(before, persons, minGroupSize))
	second := summaryOrder(MusterReprint(after, persons, minGroupSize))
	firstJohns, secondJohns := onlyJohns(first), onlyJohns(second)
	if !sameOrder(firstJohns, secondJohns) {
		t.Errorf("the two John Smith rows swapped places on the pooled summary because an UNRELATED person checked out.\n"+
			"  before the check-out: %v\n  after  the check-out: %v\n"+
			"muster.go sorts board.Summary on PersonName alone. The two rows are NOT interchangeable — the summary table "+
			"prints the company and trade beside each name (muster.html:333), so swapping them reassigns which John Smith "+
			"belongs to which crew on the sheet an officer reads aloud.", firstJohns, secondJohns)
	}
}

// onlyJohns narrows to the rows under test, so Xavier's own departure (a real
// and expected change) cannot be mistaken for the swap being measured.
func onlyJohns(rows []string) []string {
	var out []string
	for _, r := range rows {
		if len(r) >= 10 && r[:10] == "John Smith" {
			out = append(out, r)
		}
	}
	return out
}
