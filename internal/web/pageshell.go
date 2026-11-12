// pageshell.go — the top-bar data every officer page carries (W4, taking
// over W8's "slim top bar" item, team-lead 2026-10-03).
//
// _topbar.html invents nothing: each slot renders only if the page's data
// HAS the field (the `has` guard). The amendment screens (/trade,
// /person/amend, /company/amend, /vehicle/amend, /time/correct,
// /trips/amend) never populated Post or Banner, so they rendered a bare
// strip — no "Recording as / Log out", and, worst, NO OVERDUE ALERT while an
// officer sat on a correction form. Embedding pageShell gives each of them
// the same slots the gate and the board already carry, filled from the same
// sources (banner(), the post name). ⟦was: "Post, Phase or Banner … the same
// three slots … buildPhaseView()" — the weather-phase chip went with weather
// phases, James 2026-10-04⟧ Every one of these routes is behind requireLogin, so the
// Post chip's "this rendered for a signed-in session" meaning still holds.
package web

import (
	"sitecontrol/internal/deadline"
)

type pageShell struct {
	Post      string
	Banner    []deadline.Alert
	BannerErr string
}

// shell fills the top bar's data for a page.
func (s *Server) shell() pageShell {
	sh := pageShell{Post: s.Cfg.PostName()}
	sh.Banner, sh.BannerErr = s.banner()
	return sh
}
