// time_parser_forgiving_web_test.go — the forgiving clock shapes driven
// through the REAL door: an actual POST /gate/checkin, not just a call to
// parseWallTime. A shape that parses but never reaches the handler is not
// shipped (this file's whole reason to exist) — time_parser_forgiving_test.go
// covers the parser's own table; this file proves the wiring.
//
// Reuses W2's fixture plumbing (w2_seams_test.go, w2_wiring_test.go):
// newW2Server / w2BuildGateCheckInNew / w2PostForm / w2Watermark /
// w2EventsFrom / w2EventsOfType are the SAME helpers those tests bind, so
// this stays consistent with the rest of the suite rather than growing a
// second dialect.
package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/store"
)

// TestForgivingTimeAcceptedThroughRealGateCheckIn drives "6am" through an
// actual POST /gate/checkin (not the bare parser) and asserts the minted
// event's stored OccurredAt is exactly today's (site-local) 06:00 — byte
// exact, the same discipline TestW2EnteredValueByteExactThroughParse
// applies to the strict wire format.
func TestForgivingTimeAcceptedThroughRealGateCheckIn(t *testing.T) {
	srv, st, form := w2BuildGateCheckInNew(t)
	// A controlled "now" clear of every TP §4.6 bound: mid-afternoon, so
	// entering "6am" is same-day, in the past, and nowhere near the
	// absurd-age horizon — the point of this test is the FORGIVING PARSE,
	// not the bounds (those are w2_bounds_test.go's job).
	fixedNow := time.Date(2027, time.March, 15, 14, 0, 0, 0, time.Local)
	st.Clock = w2Reading(fixedNow.UnixMilli(), store.ClockSynced)

	before := w2Watermark(t, st, "site-a", 1)
	form.Set(w2TimeSourceField, "entered")
	form.Set(w2OccurredAtField, "6am")
	rec, _ := w2PostForm(t, srv, "/gate/checkin", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf(`POST /gate/checkin occurred_at="6am" = %d, want 303 (accepted): %s`, rec.Code, rec.Body.String())
	}

	want := time.Date(2027, time.March, 15, 6, 0, 0, 0, time.Local).UnixMilli()
	minted := w2EventsFrom(t, st, "site-a", 1, before)
	for _, typ := range []string{"trip_dispatched", "trip_arrival"} {
		evs := w2EventsOfType(t, minted, typ)
		if len(evs) != 1 {
			t.Fatalf("occurred_at=\"6am\": %d %s events, want 1", len(evs), typ)
		}
		e := evs[0]
		if e.TimeSource != "entered" {
			t.Fatalf("occurred_at=\"6am\": stored %s time_source=%q, want \"entered\"", typ, e.TimeSource)
		}
		if e.OccurredAt != want {
			t.Fatalf("occurred_at=\"6am\": stored %s OccurredAt=%d, want %d (today 06:00 site-local) — the forgiving parse never reached the store", typ, e.OccurredAt, want)
		}
	}
}

// TestForgivingTimeRefusedThroughRealGateCheckIn is the refusal half at
// the same real door: "13pm" (am/pm only names hours 1-12 — genuinely
// invalid, not merely unusual) must be refused with nothing authored, and
// the refusal must name what was typed so the officer can fix it without
// guessing.
func TestForgivingTimeRefusedThroughRealGateCheckIn(t *testing.T) {
	srv, st, form := w2BuildGateCheckInNew(t)
	st.Clock = w2Reading(time.Date(2027, time.March, 15, 14, 0, 0, 0, time.Local).UnixMilli(), store.ClockSynced)

	before := w2Watermark(t, st, "site-a", 1)
	form.Set(w2TimeSourceField, "entered")
	form.Set(w2OccurredAtField, "13pm")
	rec, _ := w2PostForm(t, srv, "/gate/checkin", form)
	if rec.Code < 400 || rec.Code > 499 {
		t.Fatalf(`POST /gate/checkin occurred_at="13pm" = %d, want a 4xx refusal (13pm is invalid: am/pm only names hours 1-12): %s`, rec.Code, rec.Body.String())
	}
	if after := w2Watermark(t, st, "site-a", 1); after != before {
		t.Fatalf("occurred_at=\"13pm\": the refused entry authored %d event(s) — a refusal that writes is not a refusal", after-before)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "13pm") {
		t.Fatalf("occurred_at=\"13pm\": refusal does not name what was typed — an officer cannot fix a blind refusal\nbody: %s", body)
	}
}
