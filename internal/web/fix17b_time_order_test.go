package web

// FIX17b (sc-stress F12, F6): a check-out typed earlier than the person's
// check-in is refused, naming the check-in time; an arrival (a return to
// origin, an expected arrival) typed before its run's departure is refused
// the same way; a time correction that would do either is warned about
// (W4: corrections warn and accept); and /time/correct takes an event key
// in the site:epoch:seq form the visits report prints.

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

func fix17bClock(ms int64) string { return time.UnixMilli(ms).In(time.Local).Format("15:04") }

func TestFix17bCheckOutTypedBeforeCheckInIsRefused(t *testing.T) {
	srv, st, persons := newW2Server(t, w2Base-6*60*w2Minute)
	inMS := w2Base - 60*w2Minute
	st.Clock = w2Reading(inMS, store.ClockSynced)
	w2CheckIn(t, st, persons[:1])
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	open, err := domain.OpenVisitsAt(st, "site-a")
	if err != nil || len(open) != 1 {
		t.Fatalf("open visits %v %v", open, err)
	}
	vid := open[0].VisitID

	before := w2Watermark(t, st, "site-a", 1)
	rec, _ := w2PostForm(t, srv, "/gate/checkout", url.Values{
		"visit_ids": {vid}, "time_source": {"entered"}, "occurred_at": {w2Wall(w2Base - 80*w2Minute)},
	})
	body := rec.Body.String()
	if rec.Code != http.StatusConflict || !strings.Contains(body, "was checked in here at "+fix17bClock(inMS)) || !strings.Contains(body, "nothing was recorded") {
		t.Fatalf("check-out typed before the check-in = %d, want 409 naming the check-in time %s:\n%s", rec.Code, fix17bClock(inMS), body)
	}
	if after := w2Watermark(t, st, "site-a", 1); after != before {
		t.Fatalf("the refused check-out authored %d event(s)", after-before)
	}
	// At or after the check-in it is an ordinary typed check-out.
	rec, _ = w2PostForm(t, srv, "/gate/checkout", url.Values{
		"visit_ids": {vid}, "time_source": {"entered"}, "occurred_at": {w2Wall(w2Base - 30*w2Minute)},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("check-out typed after the check-in = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFix17bArrivalTypedBeforeItsDepartureIsRefused(t *testing.T) {
	_, st, persons := newW2Server(t, w2Base-6*60*w2Minute)
	dest, mins := "site-b", int64(65)
	depMS := w2Base - 60*w2Minute
	trip, err := domain.Dispatch(st, "site-a", 1, domain.DispatchInput{
		VehicleID: w7SeedVehicleID(t, st, "site-a", "TRK-F12"), DriverPersonID: persons[0].PersonID,
		DestinationSite: &dest, DeadlineMinutes: &mins,
	}, depMS, "entered")
	if err != nil {
		t.Fatal(err)
	}
	// Back at the origin, typed before it left: refused, naming the departure.
	_, err = domain.RecordReturnToOrigin(st, "site-a", 1, trip.TripID, nil, nil, w2Base-90*w2Minute, "entered", false)
	if err == nil || !strings.Contains(err.Error(), "at "+fix17bClock(depMS)) || !strings.Contains(err.Error(), "nothing was recorded") {
		t.Fatalf("return typed before the departure: err = %v, want a refusal naming %s", err, fix17bClock(depMS))
	}
	// At the destination, typed before it left: refused, on the sender's clock.
	_, err = domain.RecordExpectedArrival(st, "site-b", 1, trip.TripID, nil, w2Base-70*w2Minute, "entered")
	if err == nil || !strings.Contains(err.Error(), "'s clock)") {
		t.Fatalf("arrival typed before the departure: err = %v, want a refusal naming the sending post's clock", err)
	}
	// A time the post's own clock stamps is never refused for this.
	if _, err := domain.RecordExpectedArrival(st, "site-b", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("a default-time arrival was refused: %v", err)
	}
}

func TestFix17bCorrectionPuttingCheckOutBeforeCheckInIsWarned(t *testing.T) {
	// The fixture's check-in is at w2Base−2h+5m, its typed check-out at
	// w2Base−2h+10m.
	srv, _, targetEnv, _ := w4CheckedOutWorld(t, w2Base-2*w2Hour, w2Base-2*w2Hour+10*w2Minute)
	for _, c := range []struct {
		correctedMS int64
		warned      bool
	}{{w2Base - 2*w2Hour, true}, {w2Base - w2Hour, false}} {
		rec, _ := w4PostCorrection(t, srv, w4CorrectionForm(targetEnv.Key(), w2Wall(c.correctedMS), "Ida Aputi"))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("correction to %s = %d (a correction warns, never refuses): %s", w2Wall(c.correctedMS), rec.Code, rec.Body.String())
		}
		loc := rec.Header().Get("Location")
		if got := strings.Contains(loc, timeWarnQueryKey+"="+timeWarnOrder); got != c.warned {
			t.Fatalf("correction to %s: order warning carried = %v, want %v (%s)", w2Wall(c.correctedMS), got, c.warned, loc)
		}
	}
	if timeWarningCopy(timeWarnOrder) == "" {
		t.Fatal("the order warning has no copy")
	}
}

func TestFix17bTimeCorrectAcceptsTheVisitsReportKeyForm(t *testing.T) {
	srv, _, targetEnv, _ := w4CheckedOutWorld(t, w2Base-2*w2Hour, w2Base-2*w2Hour+10*w2Minute)
	k := targetEnv.Key()
	reportForm := fmt.Sprintf("%s:%d:%d", k.SiteID, k.NodeEpoch, k.Seq) // as /reports/visits.csv prints it (domain keyString)
	form := w4CorrectionForm(k, w2Wall(w2Base-w2Hour), "Ida Aputi")
	form.Set("target", reportForm)
	rec, _ := w4PostCorrection(t, srv, form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a key copied from the visits report = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if _, ok := parseTypedEventKey("site-a:1"); ok {
		t.Fatal("a two-part key was read")
	}
}
