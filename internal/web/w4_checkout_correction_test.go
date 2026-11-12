// w4_checkout_correction_test.go — W4-4: the measured impossibility, made
// possible and exercised END-TO-END: a mistyped CHECKOUT time on a
// departed person (D-34's refusal blocking re-entry) is corrected against
// its `visit_checked_out` target WITHOUT fabricating any check-in/
// check-out pair — HTTP POST through the correction surface to the
// resolved read — and D-34's refusal itself is untouched.
// design/WINDOW-BUILD-PLAN-v1.md "### W4", criteria LOCKED 2026-08-27.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS the clean-tree red (go vet).
//
// KILLER MUTANT: VF-m4 — the zero-resolution mutant: the correction type
// ships, appends, and NO reader substitutes anything (the twice-produced
// CRITICAL's cheapest form; 4/4 green on the pre-chunk tree).
package web

import (
	"net/http"
	"testing"

	"sitecontrol/internal/domain"
)

// TestW4CheckoutCorrectionEndToEnd — W4-4 (VF-m4).
func TestW4CheckoutCorrectionEndToEnd(t *testing.T) {
	const (
		mistypedMS  = w2Base - 2*w2Hour + 10*w2Minute // what the officer typed at checkout, wrongly
		correctedMS = w2Base - w2Hour + 15*w2Minute   // what actually happened — the pinned resolved value
	)
	srv, st, targetEnv, persons := w4CheckedOutWorld(t, w2Base-2*w2Hour, mistypedMS)
	departed := persons[0] // the driver of the checked-out visit's vehicle

	// The person is DEPARTED and D-34's refusal holds BEFORE the
	// correction: naming them on an outbound vehicle is refused, and
	// nothing is authored (the only pre-W4 "fix" was fabricating exactly
	// such a pair — the false history D-34 exists to prevent).
	probeRefusal := func(when string) {
		t.Helper()
		before := w2Watermark(t, st, "site-a", 1)
		// ⟦R-28, team-lead, 2026-09-16⟧ RETARGETED: the departing form is
		// reached via the roster's own tick-and-select door now — persons[1]
		// (Pat Passenger, per w4CheckedOutWorld's own fixture) is the one
		// still ON SITE, so THEY seed the tick; departed.Name (persons[0])
		// is then typed, exactly as before, to pose this probe's actual
		// case — a name the roster's own tick could never offer, because
		// they hold no open visit to tick.
		_, vals := gateBuildOutbound(t, srv, "TRK-9", []string{visitIDForCrewName(t, st, persons[1].Name)}, []string{departed.Name}, 0)
		vals.Set("destination_id", "conneco")
		rec, _ := w2PostForm(t, srv, "/gate/checkout/vehicle", vals)
		if rec.Code < 400 || rec.Code > 499 {
			t.Fatalf("W4-4 (%s): outbound checkout naming the departed %q = %d, want a 4xx refusal — D-34's presence refusal must hold", when, departed.Name, rec.Code)
		}
		if after := w2Watermark(t, st, "site-a", 1); after != before {
			t.Fatalf("W4-4 (%s): the refused outbound authored %d event(s)", when, after-before)
		}
	}
	probeRefusal("before the correction")

	// The mistyped checkout is corrected THROUGH THE SURFACE — one HTTP
	// POST, one new event, no fabricated pair.
	before := w2Watermark(t, st, "site-a", 1)
	visitsBefore, err := domain.Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	rec, cookie := w4PostCorrection(t, srv, w4CorrectionForm(targetEnv.Key(), w2Wall(correctedMS), "Ida Aputi"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("W4-4: the correction POST = %d, want 303 (an in-bounds corrected instant warns at most, never blocks): %s", rec.Code, rec.Body.String())
	}
	_, _, recorded := w4FollowRecorded(t, srv, rec, cookie)

	// Exactly ONE event minted, and it is the correction — no
	// check-in/check-out pair was fabricated.
	minted := w2EventsFrom(t, st, "site-a", 1, before)
	if len(minted) != 1 {
		var types []string
		for _, e := range minted {
			types = append(types, w2PayloadType(t, e))
		}
		t.Fatalf("W4-4: the correction POST minted %d events (%v), want exactly 1 — correcting a checkout must not fabricate history (D-34)", len(minted), types)
	}
	corr := minted[0]
	if got := w2PayloadType(t, corr); got != w4EvtCorrected {
		t.Fatalf("W4-4: the minted event's type = %q, want %q", got, w4EvtCorrected)
	}
	if corr.Key() != recorded {
		t.Fatalf("W4-4: ?recorded=%v does not name the minted correction %v", recorded, corr.Key())
	}
	if got := w4PayloadCorrectedMS(t, corr); got != correctedMS {
		t.Fatalf("W4-4/W4-5: payload corrected instant = %d, want %d byte-faithful", got, correctedMS)
	}
	// The correction's own envelope end-to-end (W4-6's rule through the
	// real route): default act, rule W, corrected instant in payload only.
	if corr.TimeSource != "default" || corr.OccurredAt != corr.AuthoredAt.WallMS || corr.OccurredAt == correctedMS {
		t.Fatalf("W4-4/W4-6: the minted correction's envelope = (time_source %q, occurred %d, authored %d) — want the D-44 act shape: default, OccurredAt == AuthoredAt, corrected instant in the payload only", corr.TimeSource, corr.OccurredAt, corr.AuthoredAt.WallMS)
	}

	// TO THE RESOLVED READ (VF-m4's kill): the effective occurred instant
	// for the checkout is now the corrected value; the target row itself
	// still carries what was first entered.
	eff, chain, err := w4ResolveBudgetedWeb(t, st, targetEnv.Key())
	if err != nil {
		t.Fatalf("W4-4: ResolveOccurredAt(checkout): %v", err)
	}
	if !eff.Corrected || eff.InstantUTCms != correctedMS || eff.Winner != corr.Key() {
		t.Fatalf("W4-4 (VF-m4, the zero-resolution mutant): resolved = (corrected=%v, %d, winner %v), want (true, %d, %v) — the type shipping while no reader substitutes anything is exactly the twice-produced CRITICAL", eff.Corrected, eff.InstantUTCms, eff.Winner, correctedMS, corr.Key())
	}
	if len(chain) != 1 || chain[0].Status != domain.Effective {
		t.Fatalf("W4-4: chain = %+v, want the one Effective correction", chain)
	}
	tgt, err := st.Events(targetEnv.SiteID, targetEnv.NodeEpoch, targetEnv.Seq, 1)
	if err != nil || len(tgt) == 0 {
		t.Fatalf("W4-4: target point-read failed: %v", err)
	}
	if tgt[0].OccurredAt != mistypedMS || tgt[0].TimeSource != "entered" {
		t.Fatalf("W4-4/W4-1: the target's own envelope now reads (%q, %d), want the original (\"entered\", %d) — the original is never erased", tgt[0].TimeSource, tgt[0].OccurredAt, mistypedMS)
	}

	// The visit record is untouched: same visit rows, still closed, and
	// D-34's refusal STILL holds after the correction.
	visitsAfter, err := domain.Visits(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(visitsAfter) != len(visitsBefore) {
		t.Fatalf("W4-4: visit rows changed %d → %d under a time correction — the correction rewrites no visit history", len(visitsBefore), len(visitsAfter))
	}
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 {
		t.Fatalf("W4-4: %d open visits after the correction, want 1 (only the un-departed fixture person) — the corrected visit must stay closed; a time correction never re-enters anyone", len(open))
	}
	probeRefusal("after the correction")
}
