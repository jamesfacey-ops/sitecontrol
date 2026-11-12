// w4_bounds_test.go — W4-5: bounds fire on the CORRECTION's payload
// instant, reusing W2's PRODUCED behaviours (future refusal when synced;
// warn-accept when unsynced; records-floor warning; absurd-age warning),
// exercised against a `visit_checked_out` target (TP §7.10's bounds
// bullet's own fixture choice). And the accepted instant is stored
// BYTE-FAITHFULLY — no clamp, no nudge, no discard on the correction path
// — while the refusal re-renders with the typed time (and everything
// typed) intact.
// design/WINDOW-BUILD-PLAN-v1.md "### W4", criteria LOCKED 2026-08-27.
//
// ⚠ COMPILE-BLOCKED PENDING BUILD — that IS the clean-tree red (go vet).
//
// KILLER MUTANT: (m5), both shapes — the correction POST skips §4.6
// entirely (a bounds bypass), or clamps/nudges/discards the accepted
// instant.
//
// The bounds constants are W2's named seams (w2FutureSkewAllowanceMS,
// w2AbsurdAgeHorizonMS); the warning marker and next-page rule are W2's
// SEAM W2-S-C — one spelling, no divergence.
package web

import (
	"net/http"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// TestW4CorrectionBoundsFutureRefusalWhenSynced — behaviour 1 on the
// correction path (m5's bypass shape): with clock_state == synced, a
// corrected instant beyond FUTURE_SKEW_ALLOWANCE is REFUSED, NOTHING is
// authored, and the refusal re-renders with the typed time and target
// intact (W4-5's fidelity clause on the refusal arm — TP §7.10).
func TestW4CorrectionBoundsFutureRefusalWhenSynced(t *testing.T) {
	srv, st, targetEnv, _ := w4CheckedOutWorld(t, w2Base-2*w2Hour, w2Base-2*w2Hour+10*w2Minute)
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	before := w2Watermark(t, st, "site-a", 1)
	wall := w2Wall(w2Base + w2FutureSkewAllowanceMS + w2Minute)
	rec, _ := w4PostCorrection(t, srv, w4CorrectionForm(targetEnv.Key(), wall, "Ida Aputi"))
	if rec.Code < 400 || rec.Code > 499 {
		t.Fatalf("W4-5 (m5, the bounds bypass): corrected instant now+allowance+1min on a SYNCED clock = %d, want a 4xx refusal — bounds apply to the payload instant exactly as to any entered value (TP §4.6/§4.7)", rec.Code)
	}
	if after := w2Watermark(t, st, "site-a", 1); after != before {
		t.Fatalf("W4-5: the refused correction authored %d event(s) — a refusal that writes is not a refusal", after-before)
	}
	body := rec.Body.String()
	// ⟦PLAN OWNER 2026-08-30 — W4-F1 repair, the gate's eleventh
	// structurally-incapable check. This read `strings.Contains(body, wall)`,
	// which the REFUSAL MESSAGE satisfies on its own: W2's copy quotes the
	// typed time back to the officer. So the typed-time discard survived —
	// the input re-rendered EMPTY, the officer's typing was genuinely lost,
	// the page's own promise ("everything typed is still on this page") was
	// false, and this assertion stayed green by reading the error sentence.
	//
	// "Re-renders with the typed time intact" means the value is in the FORM
	// CONTROL the officer types into, not merely somewhere in the bytes.
	// Asserted against the input's value attribute (templates/time_correct.html:56).
	// The gate's m5 discard shape is its produced failure mode.⟧
	if wantAttr := `name="corrected_occurred_at" required value="` + wall + `"`; !strings.Contains(body, wantAttr) {
		t.Fatalf("W4-5 (m5's discard shape on the refusal arm): the refusal did not re-render the typed time %q INTO THE FORM INPUT — the officer's typing must survive the refusal, and a refusal message that merely quotes the value back is not the input being repopulated (TP §7.10's bounds bullet; W2-5's produced rule). Wanted substring %q. Body:\n%s", wall, wantAttr, body)
	}
	if !strings.Contains(body, w4TargetParam(targetEnv.Key())) {
		t.Fatalf("W4-5: the refusal does not carry the typed target %q — everything typed is preserved. Body:\n%s", w4TargetParam(targetEnv.Key()), body)
	}
}

// TestW4CorrectionBoundsWarnAcceptWhenClockUntrusted — behaviour 2, the
// refusing-the-truth boundary: the SAME fixture value warn-accepts under
// unsynced AND unverified, the warning's PRESENCE is asserted, and the
// accepted payload instant is stored byte-exactly.
func TestW4CorrectionBoundsWarnAcceptWhenClockUntrusted(t *testing.T) {
	for _, state := range []store.ClockState{store.ClockUnsynced, store.ClockUnverified} {
		t.Run(string(state), func(t *testing.T) {
			srv, st, targetEnv, _ := w4CheckedOutWorld(t, w2Base-2*w2Hour, w2Base-2*w2Hour+10*w2Minute)
			st.Clock = w2Reading(w2Base, state)
			before := w2Watermark(t, st, "site-a", 1)
			wall := w2Wall(w2Base + w2FutureSkewAllowanceMS + w2Minute)
			want := w2ParseWall(t, wall)
			rec, cookie := w4PostCorrection(t, srv, w4CorrectionForm(targetEnv.Key(), wall, "Ida Aputi"))
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("W4-5: the identical future value was NOT accepted under clock_state=%s (status %d) — a slow RTC makes a TRUE entry future-dated; refusing there refuses the truth (TP §4.6 rule 1): %s", state, rec.Code, rec.Body.String())
			}
			corr := w4MintedCorrection(t, st, "site-a", 1, before)
			if got := w4PayloadCorrectedMS(t, corr); got != want {
				t.Fatalf("W4-5 (m5, clamp/nudge): warn-accepted corrected instant stored as %d, want %d byte-faithful", got, want)
			}
			next := w2NextOperatorPage(t, srv, rec, cookie)
			if !w2HasTimeWarning(next) {
				t.Fatalf("W4-5: the accept under clock_state=%s rendered NO visible warning (marker %q absent from the page the operator sees next) — a warn-accept whose warning never renders is the suppressed-warning shape", state, w2TimeWarnToken)
			}
		})
	}
}

// TestW4CorrectionBoundsRecordsFloorWarnsAndNeverBlocks — behaviour 4 on
// the correction path: a corrected instant below min(authored_at) warns
// (the "predates" copy), never blocks, and stores byte-exactly. The
// roster is authored at w2Base−2h, so w2Base−3h is below the floor while
// only 3 hours old — the absurd-age horizon cannot be what fires.
func TestW4CorrectionBoundsRecordsFloorWarnsAndNeverBlocks(t *testing.T) {
	srv, st, targetEnv, _ := w4CheckedOutWorld(t, w2Base-2*w2Hour, w2Base-2*w2Hour+10*w2Minute)
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	before := w2Watermark(t, st, "site-a", 1)
	wall := w2Wall(w2Base - 3*w2Hour)
	want := w2ParseWall(t, wall)
	rec, cookie := w4PostCorrection(t, srv, w4CorrectionForm(targetEnv.Key(), wall, "Ida Aputi"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("W4-5: a below-floor corrected instant was REFUSED (status %d) — the records floor warns and never blocks (TP §4.6 rule 2): %s", rec.Code, rec.Body.String())
	}
	corr := w4MintedCorrection(t, st, "site-a", 1, before)
	if got := w4PayloadCorrectedMS(t, corr); got != want {
		t.Fatalf("W4-5 (m5): below-floor corrected instant stored as %d, want %d byte-faithful", got, want)
	}
	next := w2NextOperatorPage(t, srv, rec, cookie)
	if !w2HasTimeWarning(next) {
		t.Fatalf("W4-5: no visible warning for a below-floor corrected instant (marker %q absent)", w2TimeWarnToken)
	}
	if !strings.Contains(strings.ToLower(w2StripNonMarkup(next)), w2RecordsFloorPhrase) {
		t.Fatalf("W4-5: the records-floor warning's own copy (%q) is absent — some other warning is standing in for it. Page:\n%s", w2RecordsFloorPhrase, next)
	}
}

// TestW4CorrectionBoundsAbsurdAgeWarnsAndAccepts — behaviour 6 on the
// correction path: a corrected instant older than the 30-day horizon
// warns and ACCEPTS, byte-exactly. The roster is authored at w2Base−40d
// so the 31-day-old instant sits ABOVE the records floor — the absurd-age
// bound is the only one that can fire.
func TestW4CorrectionBoundsAbsurdAgeWarnsAndAccepts(t *testing.T) {
	srv, st, targetEnv, _ := w4CheckedOutWorld(t, w2Base-40*w2Day, w2Base-2*w2Hour+10*w2Minute)
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	before := w2Watermark(t, st, "site-a", 1)
	wall := w2Wall(w2Base - w2AbsurdAgeHorizonMS - w2Day)
	want := w2ParseWall(t, wall)
	rec, cookie := w4PostCorrection(t, srv, w4CorrectionForm(targetEnv.Key(), wall, "Ida Aputi"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("W4-5: a 31-day-old corrected instant was REFUSED (status %d) — the absurd-age bound warns and accepts, never refuses: %s", rec.Code, rec.Body.String())
	}
	corr := w4MintedCorrection(t, st, "site-a", 1, before)
	if got := w4PayloadCorrectedMS(t, corr); got != want {
		t.Fatalf("W4-5 (m5): absurd-age corrected instant stored as %d, want %d byte-faithful", got, want)
	}
	if !w2HasTimeWarning(w2NextOperatorPage(t, srv, rec, cookie)) {
		t.Fatalf("W4-5: no visible warning for a corrected instant beyond the 30-day horizon (marker %q absent)", w2TimeWarnToken)
	}
}
