// muster_witness_y1_test.go — witness tests for the y1-muster assignment
// (reports/witness/_BRIEF.md): P5a and P5b, the census's two surviving
// plants on muster.go's trade-label raw-id fallback.
//
// Both plants replace `r.tradeLabel = r.tradeID` with `r.tradeLabel = ""`
// — one inside MusterLive's resolve closure (muster.go:239, the LIVE
// board), one inside MusterReprint's resolve closure (muster.go:308, the
// REPRINT). The fallback fires as the LAST resort, when a visit's trade
// snapshot id names no trade this resolver can put a name to (MusterLive:
// resolved via tradeAlias but absent from the current trades list and
// carrying no snapshot name either; MusterReprint: no snapshot name at
// all) — the trade analogue of the person raw-id fallback this file
// already documents and pins at muster.go:216-224 ("want the raw id …
// don't drop or blank the row"). The trade heading has no such comment and
// no such test, and the census found nothing that notices if it goes
// blank instead of showing the raw id.
//
// Both tests assert the actual heading text an officer reads
// (muster.html:316's {{.TradeName}} on the printed trade-group heading),
// not the internal resolvedRef the code uses to get there. The two are
// kept as separate tests, one per site, because MusterLive and
// MusterReprint are two independently-reachable code paths (the census
// planted two mutants, not one) and a single test covering both would
// hide a kill on one site behind a kill on the other.
package domain

import "testing"

// TestWitnessP5aMusterLiveTradeHeadingFallsBackToRawID witnesses P5a
// (muster.go:239). A visit's trade snapshot id resolves (through an empty
// tradeAlias, i.e. no merge) to an id that is in NEITHER the live trades
// list NOR backed by a snapshot name — e.g. a trade this store has not
// (yet) replicated, or one that was never authored. The live board's
// trade heading for that crew must be the raw trade id, never blank: a
// blank heading, printed on the board read aloud during a roll call, is
// indistinguishable from "no trade section" to the officer reading it.
func TestWitnessP5aMusterLiveTradeHeadingFallsBackToRawID(t *testing.T) {
	const ghostTradeID = "TR-GHOST-99"
	visit := Visit{
		VisitID:             "v-live-1",
		SiteID:              "site-a",
		PersonID:            "p-1",
		CompanySnapshotID:   strPtr("C1"),
		CompanySnapshotName: strPtr("Acme Co"),
		TradeSnapshotID:     strPtr(ghostTradeID),
		TradeSnapshotName:   nil, // no snapshot name recorded at check-in
		IsOpen:              1,
	}
	persons := []Person{{PersonID: "p-1", SiteID: "site-a", Name: "Alice"}}
	companies := []Company{{CompanyID: "C1", Name: "Acme Co"}}
	// trades deliberately does NOT contain ghostTradeID, so the
	// `tradeName[r.tradeID]` lookup at muster.go:236 misses and the
	// resolver falls through to the raw-id line under test.
	var trades []Trade

	board := MusterLive([]Visit{visit}, persons, companies, trades, AliasMap{}, AliasMap{}, 1)

	if len(board.Companies) != 1 || len(board.Companies[0].Trades) != 1 {
		t.Fatalf("expected exactly one company with one trade group, got %+v", board)
	}
	got := board.Companies[0].Trades[0].TradeName
	if got != ghostTradeID {
		t.Fatalf("live muster trade heading = %q, want raw trade id %q — the officer reading this heading must see something identifying the trade, never a blank heading", got, ghostTradeID)
	}
	// The per-member label (also printed, on the summary table and as the
	// data-trade-group attribute) is the SAME ref for every member of this
	// bucket, so pin it too — it is the same site, checked at both places
	// the heading's underlying value surfaces.
	gotMember := board.Companies[0].Trades[0].Members[0].TradeName
	if gotMember != ghostTradeID {
		t.Fatalf("live muster member TradeName = %q, want raw trade id %q", gotMember, ghostTradeID)
	}
}

// TestWitnessP5bMusterReprintTradeHeadingFallsBackToRawID witnesses P5b
// (muster.go:308). MusterReprint takes no AliasMap and consults no live
// trades list at all (design §8.8-iv: a reprint must never re-resolve) —
// its trade label is snapshot name, or else the raw snapshot id. A visit
// whose snapshot recorded a trade id but no trade name (the roster could
// not name that trade at check-in) must still print the raw id on the
// reprint, never a blank heading — the same officer-facing guarantee as
// the live board, on the OTHER of the two sites the census names.
func TestWitnessP5bMusterReprintTradeHeadingFallsBackToRawID(t *testing.T) {
	const ghostTradeID = "TR-GHOST-77"
	visit := Visit{
		VisitID:             "v-reprint-1",
		SiteID:              "site-a",
		PersonID:            "p-1",
		CompanySnapshotID:   strPtr("C1"),
		CompanySnapshotName: strPtr("Acme Co"),
		TradeSnapshotID:     strPtr(ghostTradeID),
		TradeSnapshotName:   nil, // no snapshot name recorded at check-in
		IsOpen:              1,
	}
	persons := []Person{{PersonID: "p-1", SiteID: "site-a", Name: "Alice"}}

	board := MusterReprint([]Visit{visit}, persons, 1)

	if len(board.Companies) != 1 || len(board.Companies[0].Trades) != 1 {
		t.Fatalf("expected exactly one company with one trade group, got %+v", board)
	}
	got := board.Companies[0].Trades[0].TradeName
	if got != ghostTradeID {
		t.Fatalf("reprint muster trade heading = %q, want raw trade id %q — the printed sheet must never show a blank trade heading", got, ghostTradeID)
	}
	gotMember := board.Companies[0].Trades[0].Members[0].TradeName
	if gotMember != ghostTradeID {
		t.Fatalf("reprint muster member TradeName = %q, want raw trade id %q", gotMember, ghostTradeID)
	}
}
