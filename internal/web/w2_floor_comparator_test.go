// w2_floor_comparator_test.go — W2-4 behaviour 4's COMPARATOR-IDENTITY
// clause, made falsifiable (closes finding F-W2-1, W2-falsification.md:
// mTP-6 limb 1 survived the whole suite in both natural forms because
// every prior fixture's log rows shared one authored instant — min == max
// and occurred_at == authored_at, so no rival aggregate could disagree).
//
// The criterion (LOCKED, WINDOW-BUILD-PLAN-v1 §3 W2, W2-4.4): the
// records-floor warning fires iff entered < min(authored_at) over the
// local event_log — specifically MIN, specifically authored_at, not any
// other aggregate or column (TP §4.6 rule 2's DEFINED comparator;
// store.MinAuthoredAtWallMS is the one implementation).
//
// The SPREAD LOG, chosen so every plausible rival answers differently
// from min(authored_at), each value stated (all offsets from w2Base):
//
//	rows                       authored_at   occurred_at
//	roster (first by rowid)      −2h           −2h        (default)
//	floor-marker person          −4h  ← MIN    −90min     (entered)
//	late company (last by rowid) −1h           −1h        (default)
//
//	min(authored_at)              = −4h  (1_799_985_600_000)  the criterion
//	MAX(authored_at)              = −1h  (1_799_996_400_000)  rival 1
//	MIN(occurred_at)              = −2h  (1_799_992_800_000)  rival 2
//	first row by insertion order  = −2h  (1_799_992_800_000)  rival 3
//	last row by insertion order   = −1h  (1_799_996_400_000)  rival 4
//
// The probe value entered = −3h sits STRICTLY between min(authored_at)
// and every rival, so under the true comparator NO warning is due (−3h >
// −4h) while under every rival a warning fires (−3h < −2h ≤ each rival):
// the ABSENCE assertion is what each rival reds. Non-degeneracy is
// verified by construction IN the test (the self-check below recomputes
// all four rivals from the read-back envelopes and refuses a fixture
// where any rival equals the true min or fails to sit above the probe) —
// this repo has already shipped one degenerate fixture where rule and
// rival agreed, and it took three lenses to catch.
// (WINDOW-BUILD-PLAN-v1 §3 W2, criteria LOCKED 2026-08-28.)
package web

import (
	"strings"
	"testing"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// TestW2RecordsFloorComparatorIsMinOfAuthoredAt — the spread-log fixture
// with the absence assertion (F-W2-1's prescribed repair). Kills mTP-6
// limb 1 in every plausible form: MAX(authored_at), MIN(occurred_at),
// first-row-by-rowid, last-row-by-rowid.
func TestW2RecordsFloorComparatorIsMinOfAuthoredAt(t *testing.T) {
	srv, st, _ := newW2Server(t, w2Base-2*w2Hour) // roster: authored −2h, default

	// The unique min(authored_at) row, its occurred_at deliberately
	// DIVERGENT (entered, −90min): authored_at and occurred_at disagree on
	// this row, which is exactly what makes the column identity visible.
	// (The store never bounds; web-side bounds are not on this path.)
	st.Clock = w2Reading(w2Base-4*w2Hour, store.ClockSynced)
	if _, err := domain.CreatePerson(st, "site-a", 1, "Floor Marker",
		nil, nil, w2Base-90*w2Minute, "entered"); err != nil {
		t.Fatal(err)
	}

	// The last row by insertion order, authored ABOVE the min — so
	// "last row" and "MAX" both disagree with min(authored_at).
	st.Clock = w2Reading(w2Base-w2Hour, store.ClockSynced)
	if _, err := domain.CreateCompany(st, "site-a", 1,
		"Latest Rowid Freight", 0, "default"); err != nil {
		t.Fatal(err)
	}
	st.Clock = w2Reading(w2Base, store.ClockSynced)

	const (
		wantMin = w2Base - 4*w2Hour // the criterion's answer
		probe   = w2Base - 3*w2Hour // strictly between min and every rival
	)

	// --- non-degeneracy self-check: recompute every rival from the log --
	all := w2EventsFrom(t, st, "site-a", 1, 0)
	if len(all) == 0 {
		t.Fatal("fixture defect: empty log")
	}
	minAuth, maxAuth, minOcc := all[0].AuthoredAt.WallMS, all[0].AuthoredAt.WallMS, all[0].OccurredAt
	firstAuth, lastAuth := all[0].AuthoredAt.WallMS, all[len(all)-1].AuthoredAt.WallMS
	for _, e := range all {
		if e.AuthoredAt.WallMS < minAuth {
			minAuth = e.AuthoredAt.WallMS
		}
		if e.AuthoredAt.WallMS > maxAuth {
			maxAuth = e.AuthoredAt.WallMS
		}
		if e.OccurredAt < minOcc {
			minOcc = e.OccurredAt
		}
	}
	if minAuth != wantMin || probe <= minAuth {
		t.Fatalf("fixture defect: min(authored_at)=%d, want %d with probe %d above it", minAuth, wantMin, probe)
	}
	for name, rival := range map[string]int64{
		"MAX(authored_at)":   maxAuth,
		"MIN(occurred_at)":   minOcc,
		"first row by rowid": firstAuth,
		"last row by rowid":  lastAuth,
	} {
		if rival == minAuth || rival <= probe {
			t.Fatalf("fixture defect: DEGENERATE — rival %s = %d does not sit strictly above probe %d and apart from min(authored_at) %d; a fixture where the rule and a rival agree certifies nothing (the shipped-degenerate-fixture lesson)", name, rival, probe, minAuth)
		}
	}

	// --- the discriminating POST: entered = −3h, ABOVE the true floor ---
	_, form := gateBuild(t, srv, gateInbound, "TRK-14", []string{"Dana Driver", "Pat Passenger"}, 0)
	before := w2Watermark(t, st, "site-a", 1)
	wall := w2Wall(probe)
	form.Set(w2TimeSourceField, "entered")
	form.Set(w2OccurredAtField, wall)
	rec, cookie := w2PostForm(t, srv, "/gate/checkin", form)
	minted := w2EventsFrom(t, st, "site-a", 1, before)
	arrivals := w2EventsOfType(t, minted, "trip_arrival")
	if len(arrivals) == 0 {
		t.Fatalf("W2-4.4: an entered time ABOVE min(authored_at) was not accepted (status %d): %s", rec.Code, rec.Body.String())
	}
	want := w2ParseWall(t, wall)
	for _, ev := range arrivals {
		if ev.TimeSource != "entered" || ev.OccurredAt != want {
			t.Fatalf("W2-4.4: stored as (%q, %d), want (\"entered\", %d)", ev.TimeSource, ev.OccurredAt, want)
		}
	}
	// THE ABSENCE ASSERTION — the clause's producible failure. entered =
	// −3h is above min(authored_at) = −4h, below every rival aggregate
	// (−2h/−1h), inside every other bound (past, 3h old, synced clock): a
	// warning here can ONLY mean the comparator is not min(authored_at).
	next := w2NextOperatorPage(t, srv, rec, cookie)
	if w2HasTimeWarning(next) {
		t.Fatalf("W2-4.4 comparator identity (F-W2-1 / mTP-6 limb 1): a warning fired for entered = min(authored_at)+1h — the floor fires iff entered < min(authored_at), over authored_at, under MIN; every rival (MAX, occurred_at, first/last row) sits above this probe by construction, so this warning means the comparator is NOT min(authored_at). Page:\n%s", next)
	}

	// --- liveness arm: the floor is LIVE on this exact spread log -------
	// entered = −5h < min(authored_at) = −4h → the warning fires, naming
	// the floor. This arm discriminates nothing (every rival also sits
	// above −5h) — it exists so the absence arm cannot pass vacuously on a
	// fixture where the comparator was never reached at all.
	_, form2 := gateBuild(t, srv, gateInbound, "TRK-15", []string{"Chris Crew", "Robin Rider"}, 0)
	// ⟦R-26⟧ this test is about the records floor, not company — Robin
	// Rider is off-roster, so give the row one so the commit is not
	// refused for the reason R-26 added.
	gateFillNewRowCompanies(t, form2, "R-26 Test Co")
	before2 := w2Watermark(t, st, "site-a", 1)
	wall2 := w2Wall(w2Base - 5*w2Hour)
	form2.Set(w2TimeSourceField, "entered")
	form2.Set(w2OccurredAtField, wall2)
	rec2, cookie2 := w2PostForm(t, srv, "/gate/checkin", form2)
	if got := w2EventsOfType(t, w2EventsFrom(t, st, "site-a", 1, before2), "trip_arrival"); len(got) == 0 {
		t.Fatalf("W2-4.4: the below-floor liveness entry was not accepted (status %d) — the floor never blocks: %s", rec2.Code, rec2.Body.String())
	}
	next2 := w2NextOperatorPage(t, srv, rec2, cookie2)
	if !w2HasTimeWarning(next2) || !strings.Contains(strings.ToLower(w2StripNonMarkup(next2)), w2RecordsFloorPhrase) {
		t.Fatalf("W2-4.4: no records-floor warning for entered = min(authored_at)−1h on the spread log — the absence arm above is only meaningful while this arm proves the floor live on the SAME fixture. Page:\n%s", next2)
	}
}
