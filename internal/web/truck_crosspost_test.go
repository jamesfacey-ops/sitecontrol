// truck_crosspost_test.go — James, 2026-10-03: "the truck number and driver
// are not the same on the overdue truck between the ice road and ice pad."
//
// [MEASURED on lane L03] A run dispatched Ice Road -> Ice Pad, then its
// vehicle record renumbered (TRK-72 -> TRK-92) while it was still out: Ice
// Road's muster said "TRK-92 -> Ice Pad", while Ice Pad's alert strip, inbound
// board, gate inbound row and /trips/detail all said TRK-72. The origin's
// in-transit block composed the truck LIVE (DR-W7-18); every other
// present-tense surface read the frozen dispatch snapshot, though DR-W7-18
// rules them LIVE too (W7-36(a), never built).
//
// ONE SHARED STORE, TWO POSTS — w9a_consumers_test.go's shape: both servers
// read the same events, which is the fully-replicated state, so any
// disagreement between them is a composition difference and nothing else.
// The replication half (separate stores, events shipped across) is
// domain/truck_crosspost_test.go.
package web

import (
	"strings"
	"testing"
	"time"

	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

type truckWorld struct {
	st     *store.Store
	origin *Server // site-a, "Ice Road" — dispatched the run
	dest   *Server // site-b, "Pad Site Control" — expects it, owns the alert
	trip   domain.Trip
}

const (
	truckDriverID   = "p-truck-driver"
	truckDriverName = "Torstad, Owen"
	truckRiderID    = "p-truck-rider"
)

func newTruckWorld(t *testing.T) *truckWorld {
	t.Helper()
	st := w3OpenRegistered(t, "truck-crosspost.db")
	w := &truckWorld{st: st}
	w.origin = New(w3ConfigSiteA(st.Path), st)
	w.dest = New(w3ConfigSiteB(st.Path), st)
	if err := deadline.Register(st); err != nil {
		t.Fatal(err)
	}
	d, err := deadline.New(st, "site-b", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	w.dest.Alerts = d
	w9aSeedPerson(t, st, "site-r", 1, truckDriverID, truckDriverName)
	w9aSeedPerson(t, st, "site-r", 1, truckRiderID, "Ravton, Val")
	w.trip = w9aDispatchOpenOverdue(t, st, w.dest, "TRK-72", truckDriverID, []string{truckRiderID})
	return w
}

// truckSurface is one place, at one post, that shows this run's truck and
// driver. region narrows the page to the run's own block so a number shown
// elsewhere on the page (a register <select>, the audit chain) cannot satisfy
// or spoil the assertion.
type truckSurface struct {
	name   string
	srv    func(w *truckWorld) *Server
	path   string
	region func(w *truckWorld, page string) string
}

// truckDetailRow is the FIRST table row naming the trip — the "All trips
// known here" row, whose Vehicle cell is the one at issue (the audit chain
// beneath it legitimately quotes the dispatch-time number).
var truckDetailRow = func(w *truckWorld, page string) string {
	for _, tr := range strings.Split(page, "<tr")[1:] {
		if end := strings.Index(tr, "</tr>"); end >= 0 {
			tr = tr[:end]
		}
		if strings.Contains(tr, w.trip.TripID) {
			return tr
		}
	}
	return ""
}

func truckSurfaces() []truckSurface {
	origin := func(w *truckWorld) *Server { return w.origin }
	dest := func(w *truckWorld) *Server { return w.dest }
	road := func(_ *truckWorld, page string) string {
		return w9aRegionByAttr(page, `data-muster-section="in-transit"`)
	}
	return []truckSurface{
		{"Ice Road /muster — on the road", origin, "/muster", road},
		{"Ice Road /muster/print — on the road", origin, "/muster/print", road},
		{"Ice Road /trips/detail — the run's row", origin, "/trips/detail", truckDetailRow},
		{"Ice Pad alert strip", dest, "/trips", func(_ *truckWorld, page string) string { return w9aRegionByClass(page, "banner-notice") }},
		{"Ice Pad /trips inbound board", dest, "/trips", func(_ *truckWorld, page string) string {
			// James, 2026-10-04 (cluster I): the headings name the post by display name ⟦was: "site-b"⟧
			return w9aRegionByHeading(page, "Inbound — open expectations for Pad Site Control")
		}},
		{"Ice Pad /gate/vehicle inbound row", dest, "/gate/vehicle", func(_ *truckWorld, page string) string {
			return w9aRegionByHeading(page, "On the road to Pad Site Control")
		}},
		{"Ice Pad /trips/detail — the run's row", dest, "/trips/detail", truckDetailRow},
	}
}

// assertEverySurface fails once per surface that does not name want as the
// truck, names stale anywhere in the run's block, or loses the driver.
func (w *truckWorld) assertEverySurface(t *testing.T, want, stale string) {
	t.Helper()
	for _, s := range truckSurfaces() {
		page := w9aGetOK(t, s.srv(w), s.path)
		reg := s.region(w, page)
		if strings.TrimSpace(reg) == "" {
			t.Errorf("%s: the run's block was not found on %s — the extractor matches nothing, so nothing below would be checked", s.name, s.path)
			continue
		}
		if !strings.Contains(reg, want) {
			t.Errorf("%s names the overdue truck differently: want %q in\n%s", s.name, want, reg)
		}
		if stale != "" && strings.Contains(reg, stale) {
			t.Errorf("%s still names the truck %q after it became %q — two posts, one run, two trucks:\n%s", s.name, stale, want, reg)
		}
		if !strings.Contains(reg, truckDriverName) {
			t.Errorf("%s does not name the driver %q:\n%s", s.name, truckDriverName, reg)
		}
	}
}

// TestOverdueTruckReadsTheSameAtIceRoadAndIcePadAfterARenumber is the exact
// mismatch James saw, with the pre-renumber state as its own positive
// control (every extractor finds the block and the shared number).
func TestOverdueTruckReadsTheSameAtIceRoadAndIcePadAfterARenumber(t *testing.T) {
	w := newTruckWorld(t)
	w.assertEverySurface(t, "TRK-72", "")
	if t.Failed() {
		t.Fatal("positive control failed — the fixture or an extractor is wrong, so the renumber half would prove nothing")
	}

	n := "TRK-92"
	if _, err := domain.AmendVehicle(w.st, "site-a", 1, domain.VehicleAmendment{VehicleID: w.trip.VehicleID, Number: &n}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	w.assertEverySurface(t, "TRK-92", "TRK-72")

	// The alert row itself, not only its rendering.
	b, err := w.dest.Alerts.Evaluate()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, a := range b.Alerts {
		if a.TripID == w.trip.TripID {
			found = true
			if a.Vehicle != "TRK-92" || a.Driver != truckDriverName || a.Status != deadline.StatusOverdueLive {
				t.Errorf("alert row = {vehicle %q, driver %q, status %v}, want TRK-92 / %s / overdue", a.Vehicle, a.Driver, a.Status, truckDriverName)
			}
		}
	}
	if !found {
		t.Fatal("no alert row for the overdue run")
	}
}

// TestOverdueTruckReadsTheSameAtBothPostsAfterASameAsJoin: two records that
// an officer joins ("Same as", D-28/D-63) are one truck at every post, named
// by the surviving record.
func TestOverdueTruckReadsTheSameAtBothPostsAfterASameAsJoin(t *testing.T) {
	w := newTruckWorld(t)
	survivor := w7SeedVehicleID(t, w.st, "site-a", "TRK-77")
	if _, err := domain.AmendVehicle(w.st, "site-a", 1, domain.VehicleAmendment{VehicleID: w.trip.VehicleID, SameAs: &survivor}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	w.assertEverySurface(t, "TRK-77", "TRK-72")
}

// TestPastTenseSurfacesKeepTheNumberTheRunWentOutWith is the other half of
// DR-W7-18, so the fix above cannot be satisfied by rewriting history: once
// the run has ARRIVED, /trips/detail shows the number it was dispatched under.
func TestPastTenseSurfacesKeepTheNumberTheRunWentOutWith(t *testing.T) {
	w := newTruckWorld(t)
	if _, err := domain.RecordExpectedArrival(w.st, "site-b", 1, w.trip.TripID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	n := "TRK-92"
	if _, err := domain.AmendVehicle(w.st, "site-a", 1, domain.VehicleAmendment{VehicleID: w.trip.VehicleID, Number: &n}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	for _, srv := range []*Server{w.origin, w.dest} {
		row := truckDetailRow(w, w9aGetOK(t, srv, "/trips/detail"))
		if !strings.Contains(row, "TRK-72") || strings.Contains(row, "TRK-92") {
			t.Errorf("%s: a CLOSED run's row must keep the number it went out with (TRK-72):\n%s", srv.Cfg.SiteID, row)
		}
	}
}
