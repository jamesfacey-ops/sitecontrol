// shift_totals.go — FIX16 G: the read-only "Shift totals" panel on a post's
// own gate pages and status page. Figures: domain.BuildShiftTotals, which is
// the analytics "today" counting over the shift window. Never mounted on the
// dispatch host (that host serves only analytics pages, dispatch_surface.go,
// so /gate and /status never render there; the guard here is belt and braces).
package web

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"sitecontrol/internal/domain"
)

type shiftTotalsView struct {
	Show        bool
	Err         string // figures could not be read — said, never shown as zeros
	SignIns     int
	SignOuts    int
	VehiclesOut int
	VehiclesIn  int
	OnSite      int
	Since       string // "06:00", or "18:00 yesterday"
	Until       string
	Times       string // the configured or default change times, "06:00 and 18:00"
	Configured  bool
	Unresolved  int
}

// shiftTotalsFor builds the panel for this post. A failure is carried in Err
// and logged; it never fails the page it sits on.
func (s *Server) shiftTotalsFor(r *http.Request) shiftTotalsView {
	if dispatchHost(r.Host) {
		return shiftTotalsView{}
	}
	now := time.UnixMilli(s.clockReading().WallMS)
	starts, configured := s.Cfg.ShiftStartMinutes()
	v := shiftTotalsView{Show: true, Configured: configured}
	for i, m := range starts {
		if i > 0 {
			if i == len(starts)-1 {
				v.Times += " and "
			} else {
				v.Times += ", "
			}
		}
		v.Times += fmt.Sprintf("%02d:%02d", m/60, m%60)
	}
	t, err := domain.BuildShiftTotals(s.Store, s.Cfg.SiteID, now, time.Local, starts)
	if err != nil {
		log.Printf("shift totals: %v", err)
		v.Err = refusalText(err)
		return v
	}
	v.SignIns, v.SignOuts, v.VehiclesOut, v.VehiclesIn, v.OnSite = t.SignIns, t.SignOuts, t.VehiclesOut, t.VehiclesIn, t.OnSite
	v.Unresolved = t.Unresolved
	start, end := time.UnixMilli(t.StartMS).In(time.Local), time.UnixMilli(t.EndMS).In(time.Local)
	v.Since = start.Format("15:04") + dayWord(start, now)
	v.Until = end.Format("15:04") + dayWord(end, now)
	return v
}

// dayWord is " yesterday" / " tomorrow" when t is not on now's local date.
func dayWord(t, now time.Time) string {
	y, m, d := now.In(time.Local).Date()
	today := time.Date(y, m, d, 12, 0, 0, 0, time.Local)
	ty, tm, td := t.Date()
	switch time.Date(ty, tm, td, 12, 0, 0, 0, time.Local).Sub(today).Hours() / 24 {
	case -1:
		return " yesterday"
	case 1:
		return " tomorrow"
	}
	return ""
}
