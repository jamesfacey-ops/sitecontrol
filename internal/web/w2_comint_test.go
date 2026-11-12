// w2_comint_test.go — W2-8: the row-1 co-mint rule. Every co-minted
// reference event — person_created, company_created AND workgroup_created —
// receives `default`/now with the rule-W equality, never the entered
// arrival time (false-provenance guard; TP §4.8 row 1's design ruling:
// "the record's creation occurred at the keyboard; the person's arrival
// occurred earlier").
//
// The fixture is the criterion's own (rev 3, finding W2-R2-2): a row-1
// check-in whose new person carries a typed NEW company AND a typed NEW
// workgroup — THREE typed fields. The workgroup mint is nested inside the
// new-company branch (checkin.go:267–279), so a two-field fixture never
// executes the call site at checkin.go:274 and (m4)'s third application is
// undischargeable.
//
// Why this can fail at all: the measured forwarding chain
// (checkin.go:250–284 → refdata.go:241/285/321) hands GateCheckIn's
// (occurredAtMS, timeSource) pair VERBATIM to all three reference
// creators — the moment W2 wires the posted override into GateCheckIn, the
// entered value lands on all three co-mints unless the builder threads
// `default` per co-mint. Killed by (m4), applied per co-mint — three
// applications, one register row.
// (WINDOW-BUILD-PLAN-v1 §3 W2, criteria LOCKED 2026-08-28.)
package web

import (
	"net/http"
	"testing"
)

func TestW2CoMintedReferenceEventsStayDefaultOnEnteredCheckIn(t *testing.T) {
	srv, st, _ := newW2Server(t, w2Base-2*w2Hour)
	_, form := gateBuild(t, srv, gateInbound, "TRK-14", []string{"Nova Newton"}, 0)
	// ⟦LOCK — §82, 2026-09-03⟧ THE FORM KEY, AND THIS IS THE ROOT CAUSE — not the event name.
	// ⚠⚠ `occupant_workgroup` IS A DEAD KEY. The gate form emits `occupant_trade` (gate.html:508,
	// :708), so this line was posting a field the server does not read: the typed trade never
	// arrived, the add-on-demand branch never ran, and NO trade was co-minted. The assertion below
	// then reported "0 … events minted" — which reads as a fold defect and is nothing of the kind.
	// ⭐ §89's class in one line: form keys are plain map strings, so NOTHING sees them go stale —
	// no compiler, no linker, no census. Renaming the field could not have flagged this.
	// ⚠ Fixing only the event name below would have left the test RED for this reason instead, and
	// fixing only this line would have left it red on the name. BOTH halves were required.
	// ⟦was: "// new company and new workgroup ride the row's company/workgroup fields"⟧
	// The three typed fields: NewName came off the accumulate step; the
	// new company and new trade ride the row's company/trade fields
	// (rowPersonSlot forwards them on the kind=new path).
	// ⟦was: "form.Set(\"occupant_workgroup\", \"Night Shift\")"⟧
	// ⟦[[D-84]], 2026-09-05⟧ the row's company/trade are now intake-shaped: a
	// PICKER plus an escape box read only when the picker says "not listed"
	// (gate.go newPersonSlot, the same rule as intake.go). The typed names
	// below reach the co-mints only under that pick — as the served form
	// posts them. ⟦was: the two text fields alone⟧
	form.Set("occupant_company_id", newRef)
	form.Set("occupant_company", "Aurora Logistics")
	form.Set("occupant_trade_id", newRef)
	form.Set("occupant_trade", "Night Shift")

	before := w2Watermark(t, st, "site-a", 1)
	enteredMS := w2Base - 45*w2Minute
	wall := w2Wall(enteredMS)
	want := w2ParseWall(t, wall)
	form.Set(w2TimeSourceField, "entered")
	form.Set(w2OccurredAtField, wall)
	rec, _ := w2PostForm(t, srv, "/gate/checkin", form)
	if rec.Code != http.StatusSeeOther {
		// ⟦LOCK — §82, 2026-09-03⟧ ⟦was: "a new person/company/workgroup"⟧
		t.Fatalf("W2-8: entered check-in with a new person/company/trade = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	minted := w2EventsFrom(t, st, "site-a", 1, before)

	// NON-VACUITY ARM (and this test's red against the pre-chunk tree):
	// the override must actually be live on the arrival pair, or the
	// co-mint assertions below are green by having nothing to guard
	// against.
	for _, typ := range []string{"trip_dispatched", "trip_arrival"} {
		evs := w2EventsOfType(t, minted, typ)
		if len(evs) == 0 {
			t.Fatalf("W2-8: no %s minted", typ)
		}
		for _, e := range evs {
			if e.TimeSource != "entered" || e.OccurredAt != want {
				t.Fatalf("W2-8 (non-vacuity): the arrival pair's %s carries (%q, %d), want (\"entered\", %d) — the override is not live, so nothing here could catch a co-mint leak (49abf65's shipped state)", typ, e.TimeSource, e.OccurredAt, want)
			}
		}
	}

	// THE CRITERION: all three co-minted reference events carry
	// default/now with the rule-W equality — never the entered time.
	// ⟦LOCK — §82, 2026-09-03⟧ ⟦was: "\"workgroup_created\""⟧ — the event is `trade_created`
	// (`trade_amend.go:67`); `workgroup_created` is folded NOWHERE in production, so this arm was
	// asking for an event no fold can mint. ⚠ THE CRITERION IS UNCHANGED: all three co-minted
	// reference events must carry default/now. Only the third one's NAME moved.
	for _, typ := range []string{"person_created", "company_created", "trade_created"} {
		evs := w2EventsOfType(t, minted, typ)
		if len(evs) != 1 {
			// ⟦LOCK — §82, 2026-09-03⟧ ⟦was: "(the workgroup mint is nested inside the new-company branch, checkin.go:267–279)"⟧
			// ⚠ THAT PARENTHESIS WAS DOUBLY STALE AND WOULD HAVE MISDIRECTED THE NEXT READER: the
			// trade mint is NOT nested in the new-company branch any more, and `:267–279` now points
			// at the COMPANY mint. W8 un-nested it DELIBERATELY, and `checkin.go:285-291` records
			// why — nesting was a live defect: "an officer adding a trade for a person whose company
			// is ALREADY on the list had no way through at all … the typed trade was dropped
			// silently". A debugger sent to the new-company branch would have found nothing there.
			t.Fatalf("W2-8: %d %s events minted, want exactly 1 — the three-field fixture must reach ALL THREE co-mints (the trade mint is its OWN top-level branch, `if slot.NewTrade != \"\"` at checkin.go:304, un-nested from the new-company branch by W8/DR-W8-2)", len(evs), typ)
		}
		e := evs[0]
		if e.TimeSource != "default" {
			t.Fatalf("W2-8: co-minted %s carries time_source=%q, want \"default\" — the entered ARRIVAL time was wired onto a record-CREATION event: false provenance on a co-minted event (mutant m4, %s application)", typ, e.TimeSource, typ)
		}
		if e.OccurredAt != e.AuthoredAt.WallMS {
			t.Fatalf("W2-8: co-minted %s breaks rule W: OccurredAt=%d != AuthoredAt.WallMS=%d — the record's creation occurred at the keyboard, exactly then", typ, e.OccurredAt, e.AuthoredAt.WallMS)
		}
		if e.OccurredAt == want {
			t.Fatalf("W2-8: co-minted %s carries the entered arrival instant %d — the person's arrival occurred earlier; asserting it as the record's creation time is false provenance (mutant m4)", typ, want)
		}
	}
}
