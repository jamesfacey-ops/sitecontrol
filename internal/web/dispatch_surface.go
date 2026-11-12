// dispatch_surface.go — the Dispatch Analytics surface is READ-ONLY, in the
// server and not only in the markup (FIX7, James 2026-10-05: "The dispatch
// is not supposed to be able to edit, only read, so they should only have
// the information displayed, not have access to the operator console").
//
// THE BUG THIS CLOSES. Dispatch reaches a post's /analytics through its own
// hostname (the demo's sc-analytics.jnkfacey.com is reverse-proxied to the
// Ice Road post's port, the SAME process as that post's gate). /analytics
// mounted the top bar's alert strip, whose rows carry the "Acknowledge
// (logged)" form, and the count line and page nav linked into the operator
// console. An acknowledgment from Dispatch silenced the overdue alarm at the
// gates. Two layers now:
//
//  1. RENDERING (analytics.go, banner.html, analytics*.html): the analytics
//     pages show each alert's information — vehicle, direction, OVERDUE /
//     VERIFY BY RADIO, times — with no ack form, and link only between
//     analytics pages. That holds on every host: the analytics pages are the
//     dispatch view wherever they are opened.
//  2. ENFORCEMENT (here): a request that arrives on the dispatch hostname
//     cannot write and cannot open the console. Every non-GET except log in
//     / log out is refused 403 before any handler runs, so nothing is
//     recorded; every GET outside the analytics pages is sent to /analytics.
//
// WHICH HOST IS DISPATCH — no new config. The first DNS label of the
// request's Host is "analytics" or "dispatch", or ends in "-analytics" /
// "-dispatch" (sc-analytics.jnkfacey.com). Caddy's reverse_proxy passes the
// client's Host through unchanged, and the Cloudflare tunnel routes on it,
// so the post sees the name the dispatcher typed. Gate hostnames
// (sc-iceroad, sc-pad, …) and loopback peer traffic never match.
package web

import (
	"bytes"
	"log"
	"net"
	"net/http"
	"strings"

	"sitecontrol/internal/config"
)

// dispatchHost reports whether host (a Request.Host, port allowed) is the
// read-only dispatch surface. See the file comment for the rule.
func dispatchHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	label, _, _ := strings.Cut(strings.ToLower(host), ".")
	for _, name := range []string{"analytics", "dispatch"} {
		if label == name || strings.HasSuffix(label, "-"+name) {
			return true
		}
	}
	return false
}

// dispatchReadable is what a dispatch-host GET may open: the two analytics
// pages, the dispatch reports (FIX16 E, dispatch_reports.go), the records
// search (FIX9: GET-only, renders no console link on this host — search.go)
// and its CSV download (FIX16, search_csv.go), the two sitectl reports and
// their downloads (FIX16, reports.go), the muster for a moment, its CSV
// download and a person's last sighting (FIX16a: GET-only, no console link on
// this host — muster_at.go, last_seen.go), the three history reports and
// their downloads (FIX16f, history.go), the theme switch, and the login page
// (a log-in records nothing). Every one is a GET-only read; a POST to any of
// them is still refused 403 below.
func dispatchReadable(path string) bool {
	return path == "/analytics" || strings.HasPrefix(path, "/analytics/site/") ||
		path == "/analytics/reports" || strings.HasPrefix(path, "/analytics/reports/") || // FIX16 E
		path == "/search" || path == "/search.csv" || strings.HasPrefix(path, "/reports/") ||
		path == "/muster/at" || path == "/muster/at.csv" || path == "/last-seen" ||
		strings.HasPrefix(path, "/history/") ||
		path == "/theme" || path == "/login"
}

// dispatchGuard wraps the served surface (Root). Off the dispatch host it
// is a pass-through.
func (s *Server) dispatchGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !dispatchHost(r.Host) {
			next.ServeHTTP(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			if dispatchReadable(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			http.Redirect(w, r, "/analytics", http.StatusSeeOther)
		case http.MethodPost:
			if r.URL.Path == "/login" || r.URL.Path == "/logout" {
				next.ServeHTTP(w, r)
				return
			}
			s.dispatchRefuse(w, r)
		default:
			s.dispatchRefuse(w, r)
		}
	})
}

type dispatchRefusedData struct {
	Cfg    *config.Config
	SiteID string
	// AlertsReadOnly: the strip, if any page ever mounts it from here,
	// renders its items with no link into the console (banner.html). This
	// page carries no Banner.
	AlertsReadOnly bool
}

// dispatchRefuse answers a write attempted on the dispatch host: 403, a
// plain page, and nothing recorded (the wrapped handler never runs).
func (s *Server) dispatchRefuse(w http.ResponseWriter, r *http.Request) {
	log.Printf("dispatch surface: refused %s %s from %s (read-only host %q)", r.Method, r.URL.Path, r.RemoteAddr, r.Host)
	var buf bytes.Buffer
	data := dispatchRefusedData{Cfg: s.Cfg, SiteID: s.Cfg.SiteID, AlertsReadOnly: true}
	h := w.Header()
	if err := s.templates().ExecuteTemplate(&buf, "dispatch_refused.html", data); err != nil {
		log.Printf("dispatch refused render: %v", err)
		h.Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("Not recorded — Dispatch analytics is read-only.\n"))
		return
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write(buf.Bytes())
}

// ⟦was: readOnlyAlert / templateReadOnlyAlert (FIX7) — the strip's one
// alert row with its ack form left out, for the dispatch view. FIX13 (James,
// 2026-10-05): the strip is a compact list of vehicle ids and people aboard
// on every host, with no forms anywhere and, under AlertsReadOnly, no links
// into the console (banner.html alert_strip).⟧
