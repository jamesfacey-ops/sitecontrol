// w2_route_walk_test.go — W2-7: the mechanical route-walk. Every
// registered POST route is in {takes-input} ∪ {exempt-with-written-reason}
// ∪ {authors-nothing-with-named-executable-proof} (TP §4.8's two tables).
//
// UNIVERSE (TP §7.7b's own wording — the LOCKED anchor): every
// mux.HandleFunc("POST …") / mux.Handle("POST …") literal across
// internal/web/*.go — THE DIRECTORY GLOB. A file list is never a permitted
// universe (the ninth self-confirming-scan instance died on exactly that:
// a plant can only land where a file-list walk already looks). The
// mechanism here is the glob plus a Go token scan for "POST /…" string
// literals (comments skipped), which is the registration-literal set
// itself; the repl engine's own mux (internal/repl/server.go:29) is NOT in
// this universe — it sits behind the "POST /sync/" mount, whose row below
// binds the ENTIRE mounted handler behaviourally.
//
// THE EXEMPTION TABLE IS CRITERIA-LOCK MATERIAL: w2RouteTable changes only
// at a chunk's criteria lock, each new row naming its reason there.
// Builders may not edit tests (plan §5 step 3), so no exemption can grow by
// a build's say-so. Symmetrically, a takes-input row NAMES its per-surface
// wiring test the way an authors-nothing row names its proof — a
// takes-input row without one is a failure (this is what the row-13
// tripwire enforces when the D-34 retro route lands after W2's gate: the
// walk discovers it unclassified and goes red until the table is amended
// at a criteria lock WITH a wiring test).
//
// Killed by mTP-5 (the narrow-universe walk — the reverse reconciliation
// below: every classified route must also be DISCOVERED) and m13 (the
// proof-blind walk). Positive controls: a planted unclassified POST
// registration, glob-discovered without this file naming the planted file;
// and a planted authors-nothing row whose named proof is absent.
// (WINDOW-BUILD-PLAN-v1 §3 W2, criteria LOCKED 2026-08-28.)
package web

import (
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// w2ModuleRoot ascends to go.mod — the scan root.
func w2ModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the package directory")
		}
		dir = parent
	}
}

// w2FoundRoute is one discovered POST registration literal.
type w2FoundRoute struct {
	Route string
	File  string
	Line  int
}

// w2PostRegistrationsInDir is the walk's UNIVERSE function: the *.go glob
// over one directory (non-test files), token-scanned for "POST /…" string
// literals. It takes ONLY a directory — the positive control drives it over
// a copied tree carrying a plant this test never names.
func w2PostRegistrationsInDir(t *testing.T, dir string) []w2FoundRoute {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var out []w2FoundRoute
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		file := fset.AddFile(path, fset.Base(), len(src))
		var sc scanner.Scanner
		sc.Init(file, src, nil, 0) // comments skipped: a comment narrating a route is not a registration
		for {
			pos, tok, lit := sc.Scan()
			if tok == token.EOF {
				break
			}
			if tok != token.STRING || !strings.HasPrefix(lit, `"POST /`) {
				continue
			}
			route := strings.TrimSuffix(strings.TrimPrefix(lit, `"POST `), `"`)
			out = append(out, w2FoundRoute{Route: route, File: path, Line: fset.Position(pos).Line})
		}
	}
	return out
}

// --- the classification (criteria-lock material) ------------------------

type w2Class int

const (
	w2TakesInput w2Class = iota
	w2Exempt
	w2AuthorsNothing
)

type w2Proof struct {
	Dir  string // package dir relative to the module root
	Test string // the named executable proof
}

type w2Row struct {
	Route       string
	Class       w2Class
	Reason      string    // exempt rows: the WRITTEN reason (TP §4.8 rows 9–12)
	WiringTests []w2Proof // takes-input rows: the named per-surface wiring tests
	Proofs      []w2Proof // authors-nothing rows: the named executable proofs
}

const w2WebDir = "internal/web"
const w2StoreDir = "internal/store"

var w2RouteTable = []w2Row{
	// --- takes-input (TP §4.8 rows 1–8) — each names its wiring test ---
	{Route: "/gate/checkin", Class: w2TakesInput, WiringTests: []w2Proof{
		{w2WebDir, "TestW2WiringGateCheckInNewBody"},
		{w2WebDir, "TestW2WiringGateCheckInLegacyBody"}, // row 1 is double-bodied: the legacy shape captures too (seam S-1)
	}},
	// ⟦R-25, James, 2026-09-13, added after W2's own criteria lock — the
	// row-13 tripwire's own precedent: a genuinely new authoring route
	// discovered unclassified by the walk, amended here WITH a wiring
	// test, exactly as this file's own header says it must be.⟧ A SECOND,
	// DISTINCT inbound check-in door (handleGateCheckInSingle) reaching the
	// SAME commit body (commitGateCheckIn) row 1 already reaches — see
	// gate.go's own comment on why it is a separate route rather than a
	// flag on /gate/checkin.
	{Route: "/gate/checkin/single", Class: w2TakesInput, WiringTests: []w2Proof{{w2WebDir, "TestW2WiringGateCheckInSingle"}}},
	{Route: "/gate/checkout/vehicle", Class: w2TakesInput, WiringTests: []w2Proof{{w2WebDir, "TestW2WiringGateVehicleCheckOut"}}},
	{Route: "/gate/checkout", Class: w2TakesInput, WiringTests: []w2Proof{{w2WebDir, "TestW2WiringGateCheckOut"}}},
	{Route: "/gate/resume", Class: w2TakesInput, WiringTests: []w2Proof{{w2WebDir, "TestW2WiringGateResume"}}},
	{Route: "/trips/dispatch", Class: w2TakesInput, WiringTests: []w2Proof{{w2WebDir, "TestW2WiringTripsDispatch"}}},
	{Route: "/trips/arrive", Class: w2TakesInput, WiringTests: []w2Proof{{w2WebDir, "TestW2WiringTripsArrive"}}},
	{Route: "/trips/arrive-unsolicited", Class: w2TakesInput, WiringTests: []w2Proof{{w2WebDir, "TestW2WiringTripsArriveUnsolicited"}}},
	// James, 2026-10-03: the outbound board's "Arrived back here" door — the
	// same time capture and the same trip_arrival event as /trips/arrive.
	{Route: "/trips/returned", Class: w2TakesInput, WiringTests: []w2Proof{{w2WebDir, "TestW2WiringTripsReturned"}}},
	// ⟦D-78, 2026-09-05⟧ ROW 8 (TP §4.8) IS STRUCK WITH ITS ROUTE.
	// ⟦was: {Route: "/trips/cancel", Class: w2TakesInput, WiringTests: []w2Proof{{w2WebDir, "TestW2WiringTripsCancel"}}}⟧
	// [[D-73]] (James, verbatim: "no trip will be cancled they will continue
	// on to the next post then leave that post with a second trip") and
	// [[D-78]] ("close the route — leave the state") removed the only
	// officer-facing caller of domain.CancelTrip. This file's header rules
	// that the table changes only at a criteria lock and never by a build's
	// say-so; the authority for this strike is James's ruling, recorded at
	// reports/JAMES-DECISIONS.md D-78 and carried onto TP §4.8 row 8. The W8
	// alias-deletion note above is the precedent for a route deleted WITH
	// its table amendment. ⚠ THE STRIKE WAS WATCHED RED FIRST: with the row
	// struck and the registration still present the walk reported
	// "UNCLASSIFIED POST route /trips/cancel" — the walk working; closing the
	// route is what turned it green. TestW2WiringTripsCancel is KEPT and
	// inverted: it proves the old POST authors nothing in all three time
	// shapes.
	// --- exempt, each with its written reason (TP §4.8 rows 9–12) ---
	{Route: "/roster/intake", Class: w2Exempt, Reason: "creating a reference record IS the act being recorded; its occurrence is its authoring by construction — default/equal is TRUE for these events (a person's arrival is rows 1/4/7/13, never intake)"},
	// ⟦was: {Route: "/weather/declare", Class: w2Exempt, Reason: "the board time is a different timescale, payload-carried; under D-43 …"} — a criteria-lock table AMENDMENT, not a silent deletion: the route itself was deleted with weather phases (James, 2026-10-04: "go ahead and delete the phase conditions")⟧
	// ⟦FIX16 H, 2026-10-06⟧ the roster import (roster_import.go): the preview
	// authors nothing; the commit is /roster/intake many rows at a time,
	// through the same domain calls, so it is exempt for the same reason.
	{Route: "/roster/import/preview", Class: w2Exempt, Reason: "a dry run: it parses an uploaded file and judges each row against the roster, authoring nothing (TestFix16RosterImportPreviewJudgesEveryRowAndRecordsNothing pins the watermark unchanged)"},
	{Route: "/roster/import/commit", Class: w2Exempt, Reason: "intake many rows at a time: creating a reference record IS the act being recorded, exactly as /roster/intake — its occurrence is its authoring by construction"},
	{Route: "/alerts/ack", Class: w2Exempt, Reason: "the acknowledgment IS the act performed live at the screen — it cannot occur before its authoring; AckedAtBoard is copied server-side, never typed"},
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ THE DEV-TOOLING ROW WAS
	// REMOVED HERE. It classified the reference-merge dev route that W8
	// deletes (P-3 §5's schedule, [[D-47]]'s bar).
	//
	// ⚠ THE AUTHORITY IS THE ROW'S OWN WRITTEN REASON, set at W2's criteria
	// lock (LOCKED 2026-08-28): it pre-stated this disposition in terms —
	// "when it goes, this row goes with it and the walk stays green". So the
	// removal is a criteria-lock amendment APPLIED, not one minted by a
	// build's say-so, which this file's header forbids at :16-24.
	//
	// ⚠ THE ROW WAS RED, established STRUCTURALLY: no non-test file in
	// internal/web carries that route's "POST …" registration literal any
	// more, so w2PostRegistrationsInDir never discovers it and
	// w2WalkProblems' reverse reconciliation reported "classified route … was
	// NEVER DISCOVERED by the universe glob … or a route deleted without a
	// criteria-lock table amendment". This IS that amendment. ⚠ REASONED
	// FROM THE UNIVERSE FUNCTION, NOT PRODUCED: package web does not compile
	// today, so the walk could not be run to watch it go red.
	//
	// ⚠ THE SUPERSEDED LINE IS PRESERVED VERBATIM — in
	// reports/window-build/ALIAS-DELETION-REPAIR.md §3, NOT in this file. A
	// ⟦was:⟧ line here would re-introduce the exact byte string W8-9(c-i)'s
	// census forbids, leaving this file an offender and the repair worth
	// nothing. That report sits outside app/, which is the census's walk root.
	// W3's amendment route — the row applied by W3's builder in the same
	// commit that registers the route, per VF §7.5 item 4 (TP §4.8's
	// successor-table amendment-route row; the walk going red on the
	// unclassified route first was the walk working, and the fix is this
	// row, never an exemption minted in the walk itself).
	{Route: "/person/amend", Class: w2Exempt, Reason: "amendment routes (W3): EXEMPT-with-written-reason — no typed value feeds the envelope; typed values are payload; the envelope pair describes the amendment act (default capture)"},
	// W4's correction route — the row applied by W4's builder in the same
	// commit that registers the route, per the LOCKED W4 Changes delta 7
	// (VF §7.5-4's successor-table correction-route row; the /person/amend
	// row above is the W3 precedent). Citing D-44, NEVER the stale
	// D-42-obligation-3 reading VF §7.5-4's own text still carries (W4
	// addendum Seam 3): D-44 superseded that reading on the storage
	// question.
	{Route: "/time/correct", Class: w2Exempt, Reason: "correction route (W4): EXEMPT-with-written-reason — the typed instant is payload (TP §4.8 row 10's payload-instant precedent), and the correction act's own envelope pair is default-captured, describing the act per D-44; no typed value feeds the envelope"},
	// W5d's company amendment route — the row applied by W5d's builder in
	// the SAME COMMIT that registers the route, per W5d-23(b) (VF §7.5-4's
	// successor-table amendment-route row; the /person/amend row above is
	// the W3 precedent this clones, and /time/correct the W4 one). The
	// reason is named at W5d's criteria lock, which is what makes this a
	// row applied rather than an exemption minted in passing. Gating is
	// D-51's, not D-46's — see internal/web/company_amend.go.
	{Route: "/company/amend", Class: w2Exempt, Reason: "amendment routes (W5d, item 12): EXEMPT-with-written-reason — no typed value feeds the envelope; typed values are payload; the envelope pair describes the amendment act (default capture)"},
	// ⟦W7, item 9⟧ THE VEHICLE ROUTES — the rows applied by W7's builder in
	// the SAME COMMIT that registers the routes. `W7-31(b)` is the AUTHORITY
	// for this append and is what makes it a row applied at a criteria lock
	// rather than an exemption minted in passing: w2RouteTable changes only
	// at a chunk's criteria lock, and a builder may not mint one by saying so.
	// The /person/amend row is the W3 precedent, /time/correct the W4 one and
	// /company/amend the W5d one. Gating is DR-W7-17's — see
	// internal/web/vehicle_amend.go, and note that it cites NEITHER D-51 NOR
	// D-46, because neither names vehicle.
	{Route: "/vehicle/amend", Class: w2Exempt, Reason: "amendment routes (W7, item 9): EXEMPT-with-written-reason — no typed value feeds the envelope; typed values are payload; the envelope pair describes the amendment act (default capture)"},
	// ⚠⚠ THE CREATION ROUTE IS NOT A FOURTH COPY OF THE AMENDMENT REASON, and
	// writing it as one would be a FALSE CLONE. It is reached FROM THE
	// CHECK-IN FORM, so a typed value DOES travel with it — which is exactly
	// why `vehicle_created` sits inside W2-8's co-mint false-provenance guard.
	// The classification and that guard must AGREE: the co-mint takes DEFAULT
	// capture, and this reason says so in its own words rather than borrowing
	// a sentence about payload.
	{Route: "/vehicle/create", Class: w2Exempt, Reason: "vehicle creation (W7, item 9): EXEMPT-with-written-reason — the number and role the officer types are PAYLOAD, and the creation act's own envelope pair is DEFAULT-CAPTURED, describing the act of recording the vehicle rather than any instant the officer stated. ⚠ Reached from the check-in form, so this route is a CO-MINT site under W2-8: the co-minted vehicle_created takes default capture too, never the trip's typed instant, or a vehicle would acquire a provenance nobody stated for it"},
	// --- authors-nothing, each with a NAMED EXECUTABLE PROOF ---
	{Route: "/gate/passenger/add", Class: w2AuthorsNothing, Proofs: []w2Proof{{w2WebDir, "TestTheAccumulateRoutesAuthorNothing"}}},
	{Route: "/gate/passenger/delete", Class: w2AuthorsNothing, Proofs: []w2Proof{{w2WebDir, "TestTheAccumulateRoutesAuthorNothing"}}},
	{Route: "/gate/passenger/undo", Class: w2AuthorsNothing, Proofs: []w2Proof{{w2WebDir, "TestTheAccumulateRoutesAuthorNothing"}}},
	// gate-restack, 2026-09-11: "Depart together" stages ticked roster
	// people onto the outbound accumulated list — same class as the three
	// above (parses input, changes an in-progress list, re-renders; never
	// reaches domain). Same proof, extended to drive this route too.
	{Route: "/gate/checkout/select", Class: w2AuthorsNothing, Proofs: []w2Proof{{w2WebDir, "TestTheAccumulateRoutesAuthorNothing"}}},
	// gate-checkin-spec, 2026-09-13: "Edit person" — reads ticked visits off
	// a FRESH open-visits read and redirects to /person/amend?person_id=,
	// or back to /gate with a refusal when the tick count is not exactly
	// one (gate_edit_select.go). Same class, same proof, extended again.
	{Route: "/gate/edit-selected", Class: w2AuthorsNothing, Proofs: []w2Proof{{w2WebDir, "TestTheAccumulateRoutesAuthorNothing"}}},
	{Route: "/login", Class: w2AuthorsNothing, Proofs: []w2Proof{{w2WebDir, "TestAuthRoutesAuthorNothing"}}},
	{Route: "/logout", Class: w2AuthorsNothing, Proofs: []w2Proof{{w2WebDir, "TestAuthRoutesAuthorNothing"}}},
	// The replication mount: applies, never authors. The row cites the
	// SUCCESSOR of TP §4.8's deleted proof name (seam S-2: the old
	// "…ApplyNeverStampsAPeersEvent" proof died with eventtime_test.go at
	// W1; its contract lives on as TestW1ApplyNeverRestampsAPeersTimeBasis,
	// internal/store/w1_bytes_as_sent_test.go:70) PLUS the
	// whole-mounted-handler behavioural assertion — every path on the repl
	// mux, including POST /sync/poke, never the one path (rev 3: the repl
	// source-scan alternative is STRUCK; the behavioural assertion is the
	// sole permitted discharge).
	{Route: "/sync/", Class: w2AuthorsNothing, Proofs: []w2Proof{
		{w2StoreDir, "TestW1ApplyNeverRestampsAPeersTimeBasis"},
		{w2WebDir, "TestW2SyncMountAuthorsNothing"},
	}},
	// ⟦LOCK — §95, 2026-09-03⟧ FIVE ROWS APPENDED. ⚠ THE AUTHORITY IS `§95`, and it is named here
	// for the same reason `W7-31(b)` is named on the vehicle rows above: this file's header (:16-24)
	// rules that `w2RouteTable` changes ONLY at a criteria lock and that "no exemption can grow by a
	// build's say-so". Without a named authority these five rows would be exemptions minted in
	// passing, which is the one thing this table exists to prevent.
	//
	// ⚠ §92 AUTHORISED THESE FIVE FIRST; §95 IS THE ONE THAT BINDS, AND THE LINEAGE IS KEPT RATHER
	// THAN DROPPED. §92(d) ruled "append all five as w2Exempt, each citing §92". §95 then re-stated
	// them at the W7+W8+W9 criteria lock — "Authority for the append: this ruling" — and CHANGED THE
	// REASON TEXT, which is why the citation moved. Both are the same plan owner at the same lock;
	// neither is a build's say-so.
	//
	// ⛔ §95 REFUSED S62's PROPOSED SHARED WORDING, "amendment precedent", for the three /…/amend
	// rows, and the refusal is the point of this block. W4 built `occurred_at_corrected` — a
	// CORRECTION EVENT THAT DOES CARRY A TYPED TIME — so "it is an amendment" is not by itself a
	// reason to be exempt: some corrections are exactly what this table exists to catch. Worse, that
	// wording COULD NEVER GO STALE. Each row instead carries the narrower fact S62 measured — THIS
	// HANDLER READS NO `occurred_at` — which goes visibly FALSE the moment anyone adds time-editing
	// to it, and the row must then be revisited.
	//
	// ⚠ THE WALK FOUND FIVE, NOT ONE. Only `/trade/create` was named when this was raised; the
	// other four were discovered by the walk itself and are recorded here so the count is the
	// MEASURED one rather than the reported one.
	//
	// ⚠⚠ EACH REASON STATES WHAT ITS OWN ROUTE DOES. A reason that near-copies its neighbour's is
	// the hand-kept-list defect one row down: it looks discharged and asserts nothing specific.
	// Two of these five are near-clones by appearance and are NOT by measurement — see their notes.
	//
	// ⛔ THE HOLE THIS LEAVES, STATED RATHER THAN HIDDEN (§95's own heading). All five rest on "this
	// handler reads no typed time", and NOTHING IN THIS WALK RE-MEASURES THAT. The table is a static
	// list of prose; the basis is a fact about code that can change WITHOUT TOUCHING THIS FILE, so
	// the row would go on reading true while its basis had gone false — the stale-base class of §94.
	// The re-measurement is DELIBERATELY MANUAL and dated in each row below, so a reader can see how
	// old it is. §95 commissioned a COST ESTIMATE for mechanising it; that estimate is
	// reports/window-build/S96-EXEMPTIONS.md §4, and the debt is RECORDED, NOT CLOSED.
	{Route: "/trade/create", Class: w2Exempt, Reason: "trade creation (W8, DR-W8-2; authority §95): EXEMPT-with-written-reason — the officer types a NAME on the trade page and that name is PAYLOAD; the creation act's own envelope pair is DEFAULT-CAPTURED, describing the moment the trade was added to this site's list rather than any instant the officer stated. ⚠ Unlike /vehicle/create this is NOT a co-mint site: the add-a-trade-during-check-in path runs through resolveOrCreateTradeByName under /roster/intake (checkin.go:304, intake.go:651), never through this route, so no arrival's typed time is anywhere near it and W2-8's co-mint false-provenance question does not arise here at all. ⚠ §95 BASIS, WRITTEN SO IT CAN GO STALE LOUDLY: [MEASURED 2026-09-03] internal/web/trade.go reads no `occurred_at` ANYWHERE in its 308 lines — no typed-time key, no time.Parse, no time_source — and the authoring call is domain.CreateTrade(…, name, 0, \"default\") at trade.go:227. If time-editing is ever added to this handler that sentence is FALSE and this row must be revisited"},
	{Route: "/trade/amend", Class: w2Exempt, Reason: "trade amendment (W8, DR-W8-5; authority §95): EXEMPT-with-written-reason — the amendable set is exactly {name, phone_day, phone_night, phone_shared, same_as}, and THREE of those are the positional handset numbers that exist ONLY by amendment because trade_created carries no phone key at all (trade_amend.go:26-28). Those values are payload; the envelope pair describes the act of correcting the trade's record, which occurred at the keyboard. ⚠ This carrier has no recorded_by (DR-W8-5) — the UNRULED branch of §10(a) Q6, not a ruling against attribution; the classification does not turn on it either way. ⚠ §95 BASIS, WRITTEN SO IT CAN GO STALE LOUDLY: [MEASURED 2026-09-03] internal/web/trade.go reads no `occurred_at` at all, so the envelope cannot receive a typed instant by this route; domain.AmendTrade(…, in, 0, \"default\") at trade.go:278 is default capture. ENVELOPE CORRECTION IS A DIFFERENT ROUTE — /time/correct, three rows up. Note what this row is NOT: it is not the shared \"amendment precedent\" wording §95 refused, because W4 proved a correction CAN carry a typed time"},
	{Route: "/trips/amend", Class: w2Exempt, Reason: "trip amendment (item 12; authority §95): EXEMPT-with-written-reason — the amendable facts are the driver, the vehicle, the destination, the cargo note, the phase override reason and the occupant list (TripAmendment, trip_amend_author.go:34-43). Every one is payload describing the TRIP; none is an instant. ⚠ The departure's own occurred_at was captured at /trips/dispatch and an amendment does not restate it — correcting WHO WAS ABOARD must never silently move WHEN THEY LEFT, so the envelope pair here records when the correction was made. ⚠ §95 BASIS, WRITTEN SO IT CAN GO STALE LOUDLY: [MEASURED 2026-09-03] internal/web/trip_amend.go reads no `occurred_at` at all across its 589 lines, and domain.AmendTrip(…, in, 0, \"default\") at trip_amend.go:390 is default capture. The trip and arrival halves SHARE THIS FILE, so this measurement and /arrivals/amend's are the same grep — and they are still TWO ROWS, because the amendable sets differ and a future edit could add time-editing to one handler and not the other"},
	{Route: "/arrivals/amend", Class: w2Exempt, Reason: "arrival amendment (item 12; authority §95): EXEMPT-with-written-reason — the amendable set is exactly TWO facts, the vehicle entered and the origin site (ArrivalAmendment, trip_amend_author.go:56-60), and both are payload. ⚠ It is deliberately NARROWER than /trips/amend and the difference is load-bearing here: the arrival's own occurred_at is the ENTERED value /trips/arrive-unsolicited captured (TP §4.8 row 7) and it is NOT in the amendable set, so no typed instant can reach this envelope by any route. ⚠ §95 BASIS, WRITTEN SO IT CAN GO STALE LOUDLY: [MEASURED 2026-09-03] internal/web/trip_amend.go reads no `occurred_at` at all, and domain.AmendArrival(…, in, 0, \"default\") at trip_amend.go:529 is default capture. THE SUBJECT IS AN ARRIVAL, NOT A TRIP: this row shares a file with /trips/amend and shares nothing else — a trip amendment can move six facts, an arrival amendment exactly two"},
	{Route: "/alerts/unack", Class: w2Exempt, Reason: "un-acknowledgment (W9c; authority §95): EXEMPT-with-written-reason. ⚠ This is NOT /alerts/ack's reason mirrored, and writing it as one would be a false clone: ack's exemption is that the acknowledgment IS the act performed live at the screen, whereas this route authors a RETRACTION — and ONE PER STANDING EFFECTIVE ACK, exactly N, over targets computed by deadline.EffectiveAcks (unack.go:106-110). The officer types no value at all: the only inputs are the trip id and an optional review_phase scope, so there is nothing for the envelope to carry but the moment the button was pressed, which is default capture. ⚠ §95 BASIS, WRITTEN SO IT CAN GO STALE LOUDLY: [MEASURED 2026-09-03] internal/web/unack.go reads no `occurred_at` at all across its 115 lines, and deadline.RetractAcks(…, targets, s.postAttribution(), 0, \"default\") at unack.go:110 is default capture. The retraction REMOVES rather than adds, over a COMPUTED target set rather than a named row, which is why this is its own sentence and not /alerts/ack's mirrored"},
	// ⟦LOCK AMENDMENT — SCHEMA-11 step 4 (D-88/D-95/D-91), 2026-09-06⟧ the two
	// orientation POSTs, applied with their routes as W3/W4/W5d/W9b's builders
	// applied theirs (unref.go's header records the practice). Recorded as an
	// amendment in reports/witness/STEP4-ORIENTATION.md §7.
	{Route: "/orientation/complete", Class: w2Exempt, Reason: "orientation completion (SCHEMA-11 step 4, D-88; authority §95): EXEMPT-with-written-reason — the officer ticks NAMES and those ids are PAYLOAD (one orientation_completed per ticked person, D-3); no instant is typed anywhere on the surface. The completion is the act performed at the desk — D-88 rule 4, taken AT that site — and the envelope pair describes when it was recorded, which is default capture. ⚠ §95 BASIS, WRITTEN SO IT CAN GO STALE LOUDLY: [MEASURED 2026-09-06] internal/web/orientation.go reads no `occurred_at`, no time_source and no typed time; the authoring call is domain.CompleteOrientations(…, ids, s.postAttribution(), 0, \"default\"). If time-editing is ever added to this handler that sentence is FALSE and this row must be revisited"},
	{Route: "/orientation/retract", Class: w2Exempt, Reason: "orientation retraction (SCHEMA-11 step 4, D-95; authority §95): EXEMPT-with-written-reason — a mis-tick withdrawn by the site that recorded it; the only input is the person id, so there is nothing for the envelope to carry but the moment the button was pressed, which is default capture. It REMOVES rather than adds, over this post's OWN (person, site) row only (D-87 by construction: the edge consults no other site's row). ⚠ §95 BASIS, WRITTEN SO IT CAN GO STALE LOUDLY: [MEASURED 2026-09-06] internal/web/orientation.go reads no `occurred_at` at all; domain.RetractOrientation(…, id, s.postAttribution(), 0, \"default\") is default capture"},
}

// w2TestFuncExists reports whether a test function with the exact name is
// declared in any *_test.go file of the package dir — "named executable
// proof PRESENT": an authors-nothing row whose proof is absent is a
// failure, not a pass (mutant m13's other half).
func w2TestFuncExists(t *testing.T, root, dirRel, name string) bool {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, dirRel, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^func ` + regexp.QuoteMeta(name) + `\(`)
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if re.Match(src) {
			return true
		}
	}
	return false
}

// w2WalkProblems is the walk's single judgement function — the real test
// and both positive controls drive THIS, so a control red proves the same
// code path the clean pass runs.
func w2WalkProblems(found []w2FoundRoute, table []w2Row, proofPresent func(dir, test string) bool) []string {
	rows := map[string]w2Row{}
	for _, r := range table {
		rows[r.Route] = r
	}
	var problems []string
	seen := map[string]bool{}
	for _, f := range found {
		seen[f.Route] = true
		row, ok := rows[f.Route]
		if !ok {
			problems = append(problems, fmt.Sprintf("UNCLASSIFIED POST route %s registered at %s:%d — silence is not exemption (TP §4.8): classify it takes-input, exempt-with-written-reason, or authors-nothing-with-proof at a criteria lock", f.Route, f.File, f.Line))
			continue
		}
		switch row.Class {
		case w2TakesInput:
			if len(row.WiringTests) == 0 {
				problems = append(problems, "takes-input route "+f.Route+" names NO wiring test — silence is not exemption for WIRING either (W2-7 rev 2, verdict R-7)")
			}
			for _, wt := range row.WiringTests {
				if !proofPresent(wt.Dir, wt.Test) {
					problems = append(problems, "takes-input route "+f.Route+" names wiring test "+wt.Test+" ("+wt.Dir+") which does not exist")
				}
			}
		case w2Exempt:
			if strings.TrimSpace(row.Reason) == "" {
				problems = append(problems, "exempt route "+f.Route+" carries no written reason — never a growing allowlist")
			}
		case w2AuthorsNothing:
			if len(row.Proofs) == 0 {
				problems = append(problems, "authors-nothing route "+f.Route+" names no executable proof")
			}
			for _, pr := range row.Proofs {
				if !proofPresent(pr.Dir, pr.Test) {
					problems = append(problems, "authors-nothing route "+f.Route+" cites proof "+pr.Test+" ("+pr.Dir+") which is ABSENT — an authors-nothing row without its named proof present is a failure, not a pass (mutant m13)")
				}
			}
		}
	}
	// Reverse reconciliation — the mTP-5 kill: a narrow universe that
	// discovers fewer routes than the classification claims is red, so a
	// walk reading one registration site cannot pass on the routes it
	// happens to see.
	for _, r := range table {
		if !seen[r.Route] {
			problems = append(problems, "classified route "+r.Route+" was NEVER DISCOVERED by the universe glob — the narrow-universe walk (mutant mTP-5), or a route deleted without a criteria-lock table amendment")
		}
	}
	return problems
}

// --- the walk itself ----------------------------------------------------

func TestW2RouteWalkEveryPOSTRouteClassified(t *testing.T) {
	root := w2ModuleRoot(t)
	found := w2PostRegistrationsInDir(t, filepath.Join(root, w2WebDir))
	if len(found) == 0 {
		t.Fatal("W2-7: the universe glob discovered ZERO POST registrations in internal/web — the walk is not reading what exists")
	}
	problems := w2WalkProblems(found, w2RouteTable, func(dir, test string) bool {
		return w2TestFuncExists(t, root, dir, test)
	})
	for _, p := range problems {
		t.Error("W2-7: " + p)
	}
	if len(problems) != 0 {
		t.Fatal("W2-7: the route-walk failed — an unwired or unclassified surface goes red instead of authoring lies")
	}
}

// --- positive control 1: the planted unclassified registration ---------

// TestW2RouteWalkControlDetectsPlantedUnclassifiedRoute: the walk must
// prove it detects a planted violation BEFORE its clean pass counts (plan
// §5 CONTROL row; TP §7.7b's mandatory control, paired with mTP-5). The
// plant's source file is placed in the scan path and DISCOVERED BY THE
// GLOB — this test hands the walk only a directory, never the planted
// file's name: a plant the walk is pointed at proves only that the walk
// sees what it scans, never that it scans what exists.
func TestW2RouteWalkControlDetectsPlantedUnclassifiedRoute(t *testing.T) {
	root := w2ModuleRoot(t)
	tmp := t.TempDir()
	// The copied real tree, so the plant hides among the genuine
	// registrations rather than sitting alone.
	real, err := filepath.Glob(filepath.Join(root, w2WebDir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range real {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmp, filepath.Base(p)), src, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	plant := []byte(`package web

import "net/http"

// planted fixture: an unclassified POST registration the walk must find.
func plantedRegister(mux *http.ServeMux, s *Server) {
	mux.HandleFunc("POST /planted/unclassified", func(w http.ResponseWriter, r *http.Request) {})
}
`)
	if err := os.WriteFile(filepath.Join(tmp, "zz_planted_registration.go"), plant, 0o644); err != nil {
		t.Fatal(err)
	}

	found := w2PostRegistrationsInDir(t, tmp) // the glob, handed ONLY the directory
	problems := w2WalkProblems(found, w2RouteTable, func(dir, test string) bool {
		return w2TestFuncExists(t, root, dir, test)
	})
	for _, p := range problems {
		if strings.Contains(p, "/planted/unclassified") && strings.Contains(p, "UNCLASSIFIED") {
			return // the control produced the walk's failure mode
		}
	}
	t.Fatalf("CONTROL FAILED: the walk did not go red on a planted unclassified POST registration discovered via the glob — the walk cannot be trusted to catch a real one. Problems reported: %v", problems)
}

// --- positive control 2: the proof-blind walk ---------------------------

// TestW2RouteWalkControlDetectsMissingProof: a planted authors-nothing row
// whose named proof is ABSENT goes red (mutant m13's pairing).
func TestW2RouteWalkControlDetectsMissingProof(t *testing.T) {
	root := w2ModuleRoot(t)
	found := w2PostRegistrationsInDir(t, filepath.Join(root, w2WebDir))
	// The planted table: identical, except /login's proof names a test
	// that does not exist anywhere.
	planted := make([]w2Row, len(w2RouteTable))
	copy(planted, w2RouteTable)
	for i := range planted {
		if planted[i].Route == "/login" {
			planted[i].Proofs = []w2Proof{{w2WebDir, "TestProofThatDoesNotExistAnywhere"}}
		}
	}
	problems := w2WalkProblems(found, planted, func(dir, test string) bool {
		return w2TestFuncExists(t, root, dir, test)
	})
	for _, p := range problems {
		if strings.Contains(p, "TestProofThatDoesNotExistAnywhere") && strings.Contains(p, "ABSENT") {
			return
		}
	}
	t.Fatalf("CONTROL FAILED: an authors-nothing row citing an absent proof did not go red — the proof-blind walk (mutant m13) would survive. Problems: %v", problems)
}

// --- positive control 3: the narrow universe itself ---------------------

// TestW2RouteWalkControlDetectsNarrowUniverse: mTP-5's own shape, produced
// — a universe that reads ONE registration file discovers a subset, and
// the reverse reconciliation (every classified route must be DISCOVERED)
// goes red rather than letting the walk pass on the routes it happens to
// see. This is the failure mode of round 2's actual defect (the struck
// citation registered 2 of 18 routes).
func TestW2RouteWalkControlDetectsNarrowUniverse(t *testing.T) {
	root := w2ModuleRoot(t)
	tmp := t.TempDir()
	src, err := os.ReadFile(filepath.Join(root, w2WebDir, "auth.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "auth.go"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	found := w2PostRegistrationsInDir(t, tmp) // one file's routes only
	problems := w2WalkProblems(found, w2RouteTable, func(dir, test string) bool {
		return w2TestFuncExists(t, root, dir, test)
	})
	for _, p := range problems {
		if strings.Contains(p, "NEVER DISCOVERED") {
			return // the narrow-universe failure mode, produced
		}
	}
	t.Fatalf("CONTROL FAILED: a one-file universe did not go red on the classified routes it missed — the narrow-universe walk (mTP-5) would survive. Problems: %v", problems)
}
