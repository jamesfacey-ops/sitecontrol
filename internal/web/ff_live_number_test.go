// ff_live_number_test.go — fix round 2, stream FF (FINDINGS-R4 cluster F and
// the averages), at the surfaces:
//
//   - D11 (L17-20 = L18 SITE-3): the destination's /analytics/site page names
//     an open run's truck LIVE after a mid-trip renumber, like the origin's
//     site page and the destination's own /trips inbound board.
//   - L10 LIVE-2: a reprint names one OPEN run by one truck — the departed
//     line ("left on …") and the on-the-road block agree; a CLOSED run's
//     departed line keeps the number it went out with.
//   - D7 (R8, James 2026-10-04): the "trips arrived" tile leaves a
//     turned-back truck out and says "N turned back".
//   - D14 (R5, James 2026-10-04): the footnote says the average includes the
//     gate's "Remove — no departure".
//   - L18 AVG-1: a .5 average rounds half-up from the exact mean.
//
// Every test here was RED on fork/next 2ba9188 and is GREEN with the change.
package web

import (
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

// ffInbound is the destination site page's "On the road to here" runs —
// from its hook up to the outbound list's hook.
func ffInbound(t *testing.T, page string) string {
	t.Helper()
	at := strings.Index(page, `data-an-runs="inbound"`)
	end := strings.Index(page, `data-an-runs="outbound"`)
	if at < 0 || end < at {
		t.Fatalf("no inbound runs section on the site page:\n%s", page)
	}
	return page[at:end]
}

func ffOutbound(t *testing.T, page string) string {
	t.Helper()
	at := strings.Index(page, `data-an-runs="outbound"`)
	end := strings.Index(page, `data-an-runs="away"`)
	if at < 0 || end < at {
		t.Fatalf("no outbound runs section on the site page:\n%s", page)
	}
	return page[at:end]
}

// TestAnalyticsSitePageOfTheDestinationNamesTheTruckLive — D11. The run
// TRK-72 site-a → site-b is renumbered TRK-92 while out. Both posts' site
// pages for site-b (inbound) and for site-a (outbound) must say TRK-92 and
// never TRK-72. The pre-renumber state is the positive control.
func TestAnalyticsSitePageOfTheDestinationNamesTheTruckLive(t *testing.T) {
	w := newTruckWorld(t)
	contact := 1.0
	w.origin.SyncSnapshot = anHealthySnapshot("site-a", "site-b", &contact)
	w.dest.SyncSnapshot = anHealthySnapshot("site-b", "site-a", &contact)

	check := func(want, stale string) {
		t.Helper()
		for _, srv := range []*Server{w.origin, w.dest} {
			in := ffInbound(t, w9aGetOK(t, srv, "/analytics/site/site-b"))
			out := ffOutbound(t, w9aGetOK(t, srv, "/analytics/site/site-a"))
			for name, reg := range map[string]string{"site-b inbound": in, "site-a outbound": out} {
				if !strings.Contains(reg, w.trip.TripID) && !strings.Contains(reg, truckDriverName) {
					t.Errorf("%s: the run is not on the %s list — nothing below would be checked:\n%s", srv.Cfg.SiteID, name, reg)
					continue
				}
				if !strings.Contains(reg, want) {
					t.Errorf("%s /analytics/site %s: want the truck %q in\n%s", srv.Cfg.SiteID, name, want, reg)
				}
				if stale != "" && strings.Contains(reg, stale) {
					t.Errorf("%s /analytics/site %s still names %q after the renumber to %q (D11):\n%s", srv.Cfg.SiteID, name, stale, want, reg)
				}
			}
		}
	}
	check("TRK-72", "")
	if t.Failed() {
		t.Fatal("positive control failed — the extractor or the fixture is wrong")
	}
	n := "TRK-92"
	if _, err := domain.AmendVehicle(w.st, "site-a", 1, domain.VehicleAmendment{VehicleID: w.trip.VehicleID, Number: &n}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	check("TRK-92", "TRK-72")
}

// ffDepartedLine is the reprint's departed-list text, whitespace-folded.
func ffDepartedLine(t *testing.T, page string) string {
	t.Helper()
	bare := styleRe.ReplaceAllString(page, "")
	at := strings.Index(bare, `data-muster-section="checked-out-since"`)
	if at < 0 {
		t.Fatalf("the reprint carries no departed list:\n%s", tallySnippet(page))
	}
	sec := bare[at:]
	if end := strings.Index(sec, "</section>"); end >= 0 {
		sec = sec[:end]
	}
	return strings.Join(strings.Fields(sec), " ")
}

// TestReprintNamesAnOpenRunByOneTruck — LIVE-2's repro. A reprint is minted
// with the rider on site at site-a; the rider is dispatched to site-b on
// TRK-H; the truck is renumbered TRK-J while out. The kept reprint must name
// TRK-J in the departed line, as its on-the-road block does. After the run
// ARRIVES the departed line is history and names TRK-H again (DR-W7-18's
// frozen half), so the fix cannot be met by always reading live.
func TestReprintNamesAnOpenRunByOneTruck(t *testing.T) {
	st := w3OpenRegistered(t, "ff-live2.db")
	origin := New(w3ConfigSiteA(st.Path), st)
	st.Clock = w2Reading(w2Base, store.ClockSynced)
	w9aSeedPerson(t, st, "site-a", 1, "p-l10c12", "L10C12, Crew")
	checkIn(t, st, "site-a", "p-l10c12", nil)
	href := w6fixReprintHref(t, w9aGetOK(t, origin, "/muster"))

	st.Clock = w2Reading(w2Base+60_000, store.ClockSynced)
	trip := w9aDispatchOpen(t, st, "site-a", "site-b", 1, "TRK-H", "p-l10c12", nil, 60)
	n := "TRK-J"
	if _, err := domain.AmendVehicle(st, "site-a", 1, domain.VehicleAmendment{VehicleID: trip.VehicleID, Number: &n}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	page := w9aGetOK(t, origin, href)
	road := w9aRegionByAttr(page, `data-muster-section="in-transit"`)
	if !strings.Contains(road, "TRK-J") {
		t.Fatalf("precondition: the reprint's on-the-road block must name the live truck TRK-J:\n%s", road)
	}
	dep := ffDepartedLine(t, page)
	if !strings.Contains(dep, "L10C12, Crew — left on TRK-J to") {
		t.Errorf("the departed line for the OPEN run must name the truck the road block names (TRK-J) — one run, one truck (LIVE-2):\n%s", dep)
	}
	if strings.Contains(dep, "TRK-H") {
		t.Errorf("the departed line still names the as-dispatched TRK-H while the road block says TRK-J (LIVE-2):\n%s", dep)
	}

	// Closed: the departed line is history — the number it went out with.
	when := time.UnixMilli(w2Base + 120_000)
	st.Clock = w2Reading(when.UnixMilli(), store.ClockSynced)
	if _, err := domain.RecordExpectedArrival(st, "site-b", 1, trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	dep = ffDepartedLine(t, w9aGetOK(t, origin, href))
	if !strings.Contains(dep, "left on TRK-H to") || strings.Contains(dep, "TRK-J") {
		t.Errorf("once the run has arrived, the departed line must keep the as-dispatched TRK-H (DR-W7-18 frozen half):\n%s", dep)
	}
}

// TestAnalyticsTripsArrivedLeavesTurnedBackTrucksOutAndSaysHowMany — D7/R8
// at the page: the run TRK-72 turns back to site-a. The tile reads 0 / 0
// trips arrived and "1 / 1 turned back".
func TestAnalyticsTripsArrivedLeavesTurnedBackTrucksOutAndSaysHowMany(t *testing.T) {
	w := newTruckWorld(t)
	contact := 1.0
	w.origin.SyncSnapshot = anHealthySnapshot("site-a", "site-b", &contact)
	if _, err := domain.RecordReturnToOrigin(w.st, "site-a", 1, w.trip.TripID, nil, nil, 0, "default", true); err != nil {
		t.Fatal(err)
	}
	page := strings.Join(strings.Fields(styleRe.ReplaceAllString(w9aGetOK(t, w.origin, "/analytics"), "")), " ")
	at := strings.Index(page, `trips arrived`)
	if at < 0 {
		t.Fatalf("no trips-arrived tile:\n%s", page)
	}
	tile := page[strings.LastIndex(page[:at], `<div class="an-tile`):]
	tile = tile[:strings.Index(tile, "in transit now")]
	if !strings.Contains(tile, `<div class="an-num">0 <small>/ 0</small></div>`) {
		t.Errorf("trips arrived must read 0 / 0 — the only run turned back and never reached site-b (D7, R8):\n%s", tile)
	}
	if !strings.Contains(tile, `data-an-turned-back="1">1 / 1 turned back`) {
		t.Errorf("the tile must say the run turned back (\"1 / 1 turned back\"):\n%s", tile)
	}
}

// TestAnalyticsFootnoteSaysTheAverageIncludesRemoveNoDeparture — D14/R5.
func TestAnalyticsFootnoteSaysTheAverageIncludesRemoveNoDeparture(t *testing.T) {
	w := anRoadWorld(t)
	_, page := anSiteGet(t, w.srv, "GET", "/analytics", true)
	flat := strings.Join(strings.Fields(styleRe.ReplaceAllString(page, "")), " ")
	if !strings.Contains(flat, "Avg time on site") || !strings.Contains(flat, "includes visits closed by the gate's “Remove — no departure”") {
		t.Errorf("the average's footnote must say it includes \"Remove — no departure\" (R5):\n%s", flat)
	}
}

// TestFmtAvgRoundsAHalfMinuteTieUp — AVG-1: 508 minutes over 8 visits is
// 63.5, shown as 64 min. On 2ba9188 the running mean drifted to 63.4999…
// and the page said 63.
func TestFmtAvgRoundsAHalfMinuteTieUp(t *testing.T) {
	fig := ffAvgOver(t, []int64{83, 86, 2, 46, 122, 46, 82, 41})
	if got := fmtAvg(fig); got != "1h 04m (n=8)" {
		t.Errorf("fmtAvg = %q, want \"1h 04m (n=8)\" (508/8 = 63.5 rounds half-up)", got)
	}
}

// ffAvgOver builds the average through BuildAnalytics' own accumulator by
// way of the exported fields it fills — the sum and n — so the web layer is
// tested over the same value the domain produces.
func ffAvgOver(t *testing.T, xs []int64) domain.MinutesAvg {
	t.Helper()
	var m domain.MinutesAvg
	for _, x := range xs {
		m.N++
		m.Sum += x
		m.Mean = (m.Mean*float64(m.N-1) + float64(x)) / float64(m.N) // the drifting form, on purpose: fmtAvg must not read it
	}
	return m
}
