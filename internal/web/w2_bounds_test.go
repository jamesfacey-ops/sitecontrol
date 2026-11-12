// w2_bounds_test.go — W2-4: all EIGHT bounds behaviours PRODUCED, each one
// test, all server-side behaviours driven by HTTP-level POSTs (TP §4.6
// rule 5 — a browser-only bound is not a bound). Plus W2-5's first
// application: the refusal re-render with every posted field and the typed
// time intact.
//
// The discriminating pair at the heart of the criterion (behaviours 1+2,
// asserted on the SAME fixture value): future refusal fires ONLY when
// clock_state == synced; the identical input warn-accepts when
// unsynced/unverified — a slow RTC makes a TRUE entry future-dated
// relative to the box, and refusing there refuses the truth (TP §4.6
// rule 1; the round-1 defect class). An inverted condition survives unless
// both arms exist (mutant m1).
//
// Constants are the S-5 recorded design defaults, bound as NAMED constants
// in w2_seams_test.go (never magic numbers): FUTURE_SKEW_ALLOWANCE = 5 min,
// absurd-age horizon = 30 days.
// (WINDOW-BUILD-PLAN-v1 §3 W2, criteria LOCKED 2026-08-28.)
package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// w2FutureBeyondMS is THE shared fixture value for behaviours 1 and 2:
// beyond the allowance by one whole minute.
const w2FutureBeyondOffset = w2FutureSkewAllowanceMS + w2Minute

// w2GateEntry is one posted gate entry with everything a bounds assertion
// needs to observe it.
type w2GateEntry struct {
	srv    *Server
	st     *store.Store
	before int64 // own-stream watermark before the POST
	rec    *httptest.ResponseRecorder
	cookie *http.Cookie
	wall   string // the typed wall string, exactly as posted
}

// w2PostGateEntered builds a fresh two-passenger gate entry, sets the
// clock to (w2Base, state), and posts it with an entered override of
// enteredMS.
func w2PostGateEntered(t *testing.T, state store.ClockState, enteredMS int64) w2GateEntry {
	t.Helper()
	srv, st, form := w2BuildGateCheckInNew(t)
	st.Clock = w2Reading(w2Base, state)
	before := w2Watermark(t, st, "site-a", 1)
	wall := w2Wall(enteredMS)
	form.Set(w2TimeSourceField, "entered")
	form.Set(w2OccurredAtField, wall)
	rec, cookie := w2PostForm(t, srv, "/gate/checkin", form)
	return w2GateEntry{srv: srv, st: st, before: before, rec: rec, cookie: cookie, wall: wall}
}

// --- behaviour 1: future refusal fires when synced ----------------------

// TestW2FutureBoundRefusesBeyondAllowanceWhenSynced — behaviour 1: with
// clock_state == synced, an entered value more than FUTURE_SKEW_ALLOWANCE
// ahead of now is REFUSED with the reason shown, and nothing is authored.
// (TP §4.6 rule 1. Mutant m1 is the inverted condition — its other half is
// the warn-accept test below, on the SAME value.)
func TestW2FutureBoundRefusesBeyondAllowanceWhenSynced(t *testing.T) {
	e := w2PostGateEntered(t, store.ClockSynced, w2Base+w2FutureBeyondOffset)
	if e.rec.Code < 400 || e.rec.Code > 499 {
		t.Fatalf("W2-4.1: entered now+allowance+1min on a SYNCED clock = %d, want a 4xx refusal with the reason shown (TP §4.6 rule 1)", e.rec.Code)
	}
	if after := w2Watermark(t, e.st, "site-a", 1); after != e.before {
		t.Fatalf("W2-4.1: the refused future entry authored %d event(s) — a refusal that writes is not a refusal", after-e.before)
	}
	// The reason is SHOWN — on the form's own refusal notice, the one place
	// the gate's refusal doctrine renders it (gate_form_test.go's
	// vehicleError instrument: page furniture must not stand in for it).
	reason := vehicleError(t, e.rec.Body.String(), gateInbound)
	if strings.TrimSpace(reason) == "" {
		t.Fatal("W2-4.1: the refusal carries no visible reason")
	}
}

// --- behaviour 2: the identical input warn-accepts when untrusted -------

// TestW2FutureBeyondAllowanceWarnAcceptsWhenClockUntrusted — behaviour 2,
// the "refusing the truth" boundary: the SAME fixture value warn-accepts
// under unsynced AND unverified; the warning's PRESENCE is asserted, not
// just the accept (mutant m16, first application), and the accepted value
// is stored byte-exactly.
func TestW2FutureBeyondAllowanceWarnAcceptsWhenClockUntrusted(t *testing.T) {
	for _, state := range []store.ClockState{store.ClockUnsynced, store.ClockUnverified} {
		t.Run(string(state), func(t *testing.T) {
			e := w2PostGateEntered(t, state, w2Base+w2FutureBeyondOffset)
			want := w2ParseWall(t, e.wall)
			minted := w2EventsFrom(t, e.st, "site-a", 1, e.before)
			arrivals := w2EventsOfType(t, minted, "trip_arrival")
			if len(arrivals) == 0 {
				t.Fatalf("W2-4.2: the identical future value was NOT accepted under clock_state=%s (status %d) — refusing there refuses the truth on the design's own recurring premise: a slow RTC makes a true entry future-dated (mutant m1, the inverted bounds condition): %s", state, e.rec.Code, e.rec.Body.String())
			}
			for _, ev := range arrivals {
				if ev.TimeSource != "entered" || ev.OccurredAt != want {
					t.Fatalf("W2-4.2/W2-4.7: warn-accepted value stored as (%q, %d), want (\"entered\", %d) byte-faithful (mutant m2/mTP-2)", ev.TimeSource, ev.OccurredAt, want)
				}
			}
			next := w2NextOperatorPage(t, e.srv, e.rec, e.cookie)
			if !w2HasTimeWarning(next) {
				t.Fatalf("W2-4.2: the accept under clock_state=%s rendered NO visible warning (marker %q absent from the page the operator sees next) — a warn-accept whose warning never renders is mutant m16", state, w2TimeWarnToken)
			}
		})
	}
}

// --- behaviour 3: within-allowance silent accept ------------------------

// TestW2WithinAllowanceFutureAcceptsSilentlyByteExact — behaviour 3 (TP
// §7.2's mandated fixture, billed to W2 by W1's lock seam): synced,
// entered = now + (FUTURE_SKEW_ALLOWANCE − 1min) → accepted, NO warning,
// stored byte-exactly. Kills mTP-2: both the silent nudge and the
// over-eager warning inside the allowance.
func TestW2WithinAllowanceFutureAcceptsSilentlyByteExact(t *testing.T) {
	e := w2PostGateEntered(t, store.ClockSynced, w2Base+w2FutureSkewAllowanceMS-w2Minute)
	want := w2ParseWall(t, e.wall)
	minted := w2EventsFrom(t, e.st, "site-a", 1, e.before)
	arrivals := w2EventsOfType(t, minted, "trip_arrival")
	if len(arrivals) == 0 {
		t.Fatalf("W2-4.3: a within-allowance future value was not accepted (status %d): %s", e.rec.Code, e.rec.Body.String())
	}
	for _, ev := range arrivals {
		if ev.TimeSource != "entered" || ev.OccurredAt != want {
			t.Fatalf("W2-4.3: within-allowance value stored as (%q, %d), want (\"entered\", %d) — a silent nudge is mutant mTP-2", ev.TimeSource, ev.OccurredAt, want)
		}
	}
	next := w2NextOperatorPage(t, e.srv, e.rec, e.cookie)
	if w2HasTimeWarning(next) {
		t.Fatalf("W2-4.3: a warning rendered INSIDE the allowance on a synced clock — the over-eager warning is mTP-2's second limb. Page:\n%s", next)
	}
}

// --- behaviours 4+5: the records floor ----------------------------------

// TestW2RecordsFloorWarnsAndNeverBlocks — behaviour 4: the warning fires
// iff entered < min(authored_at) over the local event_log (the DEFINED
// comparator — TP §4.6 rule 2; a builder never improvises it), never
// blocks, and the value stores byte-exactly. The fixture's roster rows are
// authored at w2Base−2h, so entered = w2Base−3h is below the floor while
// only 3 hours old (the absurd-age horizon cannot be what fires). Kills
// mTP-6 limbs 1 and 3 and m16's second application.
func TestW2RecordsFloorWarnsAndNeverBlocks(t *testing.T) {
	e := w2PostGateEntered(t, store.ClockSynced, w2Base-3*w2Hour)
	want := w2ParseWall(t, e.wall)
	minted := w2EventsFrom(t, e.st, "site-a", 1, e.before)
	arrivals := w2EventsOfType(t, minted, "trip_arrival")
	if len(arrivals) == 0 {
		t.Fatalf("W2-4.4: an entered time below the records floor was BLOCKED (status %d) — the floor is a warning, NEVER a refusal; the refusing floor is mTP-6 limb 3, the round-1 struck defect for this exact bound: %s", e.rec.Code, e.rec.Body.String())
	}
	for _, ev := range arrivals {
		if ev.TimeSource != "entered" || ev.OccurredAt != want {
			t.Fatalf("W2-4.4/W2-4.7: floor-warned value stored as (%q, %d), want (\"entered\", %d) byte-faithful", ev.TimeSource, ev.OccurredAt, want)
		}
	}
	next := w2NextOperatorPage(t, e.srv, e.rec, e.cookie)
	if !w2HasTimeWarning(next) {
		t.Fatalf("W2-4.4: no visible warning for a value below min(authored_at) (marker %q absent) — a suppressed due warning is mutant m16", w2TimeWarnToken)
	}
	if !strings.Contains(strings.ToLower(w2StripNonMarkup(next)), w2RecordsFloorPhrase) {
		t.Fatalf("W2-4.4: the warning does not say the time %s this box's records (TP §4.6 rule 2's own copy) — a warning that does not name the floor is not the records-floor warning. Page:\n%s", w2RecordsFloorPhrase, next)
	}
}

// TestW2RecordsFloorSuppressedOnEmptyLog — behaviour 5: no records, no
// floor. On a store whose event_log is EMPTY, an entered past value (1h
// old — no other bound can fire) is accepted with NO warning. Kills
// mTP-6's second limb (a floor that fires on an empty log — the
// uncomputable original bound resurrected).
func TestW2RecordsFloorSuppressedOnEmptyLog(t *testing.T) {
	srv, st := newW2EmptyServer(t)
	if wm := w2Watermark(t, st, "site-a", 1); wm != 0 {
		t.Fatalf("fixture defect: event_log not empty (watermark %d) — an empty log is this behaviour's whole premise", wm)
	}
	form := w2LegacyCheckInBody("FIRST TRUCK", "Nova Newton")
	enteredMS := w2Base - w2Hour
	wall := w2Wall(enteredMS)
	form.Set(w2TimeSourceField, "entered")
	form.Set(w2OccurredAtField, wall)
	rec, cookie := w2PostForm(t, srv, "/gate/checkin", form)
	minted := w2EventsFrom(t, st, "site-a", 1, 0)
	arrivals := w2EventsOfType(t, minted, "trip_arrival")
	if len(arrivals) == 0 {
		t.Fatalf("W2-4.5: the first-ever entry was refused (status %d) — with no records there is no floor: %s", rec.Code, rec.Body.String())
	}
	want := w2ParseWall(t, wall)
	for _, e := range arrivals {
		if e.TimeSource != "entered" || e.OccurredAt != want {
			t.Fatalf("W2-4.5: stored as (%q, %d), want (\"entered\", %d)", e.TimeSource, e.OccurredAt, want)
		}
	}
	next := w2NextOperatorPage(t, srv, rec, cookie)
	if w2HasTimeWarning(next) {
		t.Fatalf("W2-4.5: a records-floor warning fired on an EMPTY log — no records, no floor (mTP-6 limb 2). Page:\n%s", next)
	}
}

// --- behaviour 6: absurd age warns and accepts --------------------------

// TestW2AbsurdAgeWarnsAndAccepts — behaviour 6: an entered time older than
// the 30-day horizon warns and ACCEPTS, never refuses. The roster is
// authored at w2Base−40d so the 31-day-old entry sits ABOVE the records
// floor — the absurd-age bound is the only one that can fire. Kills m10
// (the refusing shape) and m16's third application (the suppressed
// warning).
func TestW2AbsurdAgeWarnsAndAccepts(t *testing.T) {
	srv, st, _ := newW2Server(t, w2Base-40*w2Day)
	_, form := gateBuild(t, srv, gateInbound, "TRK-14", []string{"Dana Driver", "Pat Passenger"}, 0)
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	before := w2Watermark(t, st, "site-a", 1)
	enteredMS := w2Base - w2AbsurdAgeHorizonMS - w2Day
	wall := w2Wall(enteredMS)
	form.Set(w2TimeSourceField, "entered")
	form.Set(w2OccurredAtField, wall)
	rec, cookie := w2PostForm(t, srv, "/gate/checkin", form)
	minted := w2EventsFrom(t, st, "site-a", 1, before)
	arrivals := w2EventsOfType(t, minted, "trip_arrival")
	if len(arrivals) == 0 {
		t.Fatalf("W2-4.6: a 31-day-old entered time was REFUSED (status %d) — the absurd-age bound warns and accepts, never refuses (mutant m10): %s", rec.Code, rec.Body.String())
	}
	want := w2ParseWall(t, wall)
	for _, e := range arrivals {
		if e.TimeSource != "entered" || e.OccurredAt != want {
			t.Fatalf("W2-4.6/W2-4.7: absurd-age value stored as (%q, %d), want (\"entered\", %d) byte-faithful", e.TimeSource, e.OccurredAt, want)
		}
	}
	next := w2NextOperatorPage(t, srv, rec, cookie)
	if !w2HasTimeWarning(next) {
		t.Fatalf("W2-4.6: no visible warning for a value beyond the 30-day horizon (marker %q absent) — m16's third application", w2TimeWarnToken)
	}
}

// --- behaviour 7: warn-accepted values are byte-faithful ----------------

// TestW2EveryWarnAcceptedValueIsByteFaithful — behaviour 7: the §7.2
// fidelity assertion applied ON EACH warn-accept path, as its own test so
// the ledger row for this behaviour cannot be discharged by a warn test
// that forgot the value. Kills m2/mTP-2 on the warn paths.
func TestW2EveryWarnAcceptedValueIsByteFaithful(t *testing.T) {
	cases := []struct {
		name    string
		state   store.ClockState
		entered int64
	}{
		{"future-beyond-allowance-unsynced", store.ClockUnsynced, w2Base + w2FutureBeyondOffset},
		{"below-records-floor", store.ClockSynced, w2Base - 3*w2Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := w2PostGateEntered(t, c.state, c.entered)
			want := w2ParseWall(t, e.wall)
			minted := w2EventsFrom(t, e.st, "site-a", 1, e.before)
			for _, typ := range []string{"trip_dispatched", "trip_arrival"} {
				evs := w2EventsOfType(t, minted, typ)
				if len(evs) == 0 {
					t.Fatalf("W2-4.7 %s: not accepted (status %d)", c.name, e.rec.Code)
				}
				for _, ev := range evs {
					if ev.OccurredAt != want || ev.TimeSource != "entered" {
						t.Fatalf("W2-4.7 %s: stored %s (%q, %d), want (\"entered\", %d) — a warn-accept path that adjusts the value is the silent clamp wearing a warning (m2)", c.name, typ, ev.TimeSource, ev.OccurredAt, want)
					}
				}
			}
		})
	}
	// The absurd-age warn path's fidelity is asserted in
	// TestW2AbsurdAgeWarnsAndAccepts (it needs the 40-day fixture); this
	// test covers the two warn-accept paths reachable on the standard
	// fixture so all three warn-accept behaviours carry the assertion.
}

// --- behaviour 8: the default path is never bounded ---------------------

// TestW2DefaultPathIsNeverBounded — behaviour 8: default-path authoring
// under a clock reading arbitrarily far wrong, in EVERY clock_state, is
// never refused and never warned — "authoring is never refused for a clock
// reason" (TP §4.6 scope rule; schema-10 §5B.4 rule 1). Bounds leaking
// onto the default path is mutant m9 — the second shape of the
// refusing-the-truth class.
func TestW2DefaultPathIsNeverBounded(t *testing.T) {
	wrongClocks := []struct {
		name   string
		wallMS int64
	}{
		{"clock-ten-years-fast", w2Base + 10*365*w2Day},
		{"clock-decades-slow", int64(631_152_000_000)}, // 1990 — a dead RTC's confident wrong answer
	}
	states := []store.ClockState{store.ClockSynced, store.ClockUnsynced, store.ClockUnverified}
	for _, wc := range wrongClocks {
		for _, state := range states {
			t.Run(wc.name+"/"+string(state), func(t *testing.T) {
				srv, st, form := w2BuildGateCheckInNew(t)
				st.Clock = w2Reading(wc.wallMS, state)
				before := w2Watermark(t, st, "site-a", 1)
				form.Set(w2TimeSourceField, "default")
				form.Set(w2OccurredAtField, w2Wall(w2Base)) // the echoed render instant, wildly different from the wrong clock
				rec, cookie := w2PostForm(t, srv, "/gate/checkin", form)
				minted := w2EventsFrom(t, st, "site-a", 1, before)
				arrivals := w2EventsOfType(t, minted, "trip_arrival")
				if len(arrivals) == 0 {
					t.Fatalf("W2-4.8: default-path authoring REFUSED under %s/%s (status %d) — authoring is never refused for a clock reason (mutant m9): %s", wc.name, state, rec.Code, rec.Body.String())
				}
				for _, e := range arrivals {
					if e.TimeSource != "default" || e.OccurredAt != e.AuthoredAt.WallMS {
						t.Fatalf("W2-4.8: default row (%q, OccurredAt=%d, AuthoredAt=%d) — rule W must hold verbatim even on a wrong clock: the record is honestly wrong, never bounded", e.TimeSource, e.OccurredAt, e.AuthoredAt.WallMS)
					}
				}
				next := w2NextOperatorPage(t, srv, rec, cookie)
				if w2HasTimeWarning(next) {
					t.Fatalf("W2-4.8: a bounds warning fired on the DEFAULT path under %s/%s — bounds apply to entered values ONLY (mutant m9, warning shape)", wc.name, state)
				}
			})
		}
	}
}

// --- W2-5: the refusal re-render keeps everything -----------------------

// TestW2FutureRefusalReRendersEveryPostedFieldAndTypedTime — W2-5, first
// application, on the future-refusal path of the form-heavy gate screen: a
// refusal must not cost the operator the list (gate.go:1009–1012's own
// doctrine) — every posted field AND the typed time intact in the refusal
// response body. Kills m11 (the state-discarding refusal).
func TestW2FutureRefusalReRendersEveryPostedFieldAndTypedTime(t *testing.T) {
	e := w2PostGateEntered(t, store.ClockSynced, w2Base+w2FutureBeyondOffset)
	if e.rec.Code < 400 || e.rec.Code > 499 {
		t.Fatalf("W2-5: precondition — the future entry must be refused when synced (got %d); behaviour 1's test owns that failure, this one owns the re-render", e.rec.Code)
	}
	body := w2StripNonMarkup(e.rec.Body.String())
	for _, posted := range []string{"TRK-14", "Dana Driver", "Pat Passenger"} {
		if !strings.Contains(body, posted) {
			t.Fatalf("W2-5: the refusal discarded the posted %q — losing a page of typing at -30 with a bus idling is a far worse failure than the one being prevented (mutant m11). Body:\n%s", posted, body)
		}
	}
	if !strings.Contains(body, e.wall) {
		t.Fatalf("W2-5: the refusal does not re-render the TYPED TIME %q — the officer must correct it, not re-remember it (TP §4.6 rule 6). Body:\n%s", e.wall, body)
	}
}
