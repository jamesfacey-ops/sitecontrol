// lostsignal.go — FIX15 (James, 2026-10-06: "i need a lost signal
// notification on the sites to notify on the dispatch page if signal is
// dropped"). One red LOST SIGNAL line in the top bar's compact strip, on
// every page of the dispatch host and of every post, naming each site the
// serving post has lost contact with; and the Signal tile on /analytics.
//
// WHAT "LOST" MEANS — no new number. A peer is lost when this post has
// completed no sync with it for longer than the SAME age ceiling the STALE
// verdict applies (syncStaleMultiple × sync_interval_seconds: 30 s at the
// deployed 10 s), counted from the last successful exchange, or from this
// post's start when there has been none since (peerView.Lost). It clears by
// itself: the first completed pull resets the contact age, and the next page
// load — the analytics pages reload every 60 s — renders no line.
//
// WHAT IS NOT LOST: a peer whose last pull was ANSWERED with a refusal of
// this post's key or identity (repl.ErrorKindKeyRejected / ...Identity-
// Rejected, 401/403). The link is up; that is the key fault, worded as one
// by the sync verdict (keyRejectedVerdict) and the chip, never as lost
// signal. A refused POKE alone never ages the contact (pulls still land), so
// it cannot reach the ceiling either.
//
// WHERE IT RENDERS. The top bar emits a marker (lostsignalslot) inside the
// stuck alert area; this wrapper replaces it per request, because the
// wording depends on the HOST (Dispatch or a post) and no page handler needs
// to know it — the same at-the-writer shape as homelink.go and
// dispatch_identity.go. It sits OUTSIDE dispatchIdentity, so that rewrite's
// "this post" substitutions never touch it. The line is NOT inside the
// strip's role="alert" region: it is a standing condition re-rendered on
// every 60 s reload, and an assertive region would re-announce it each
// time. Each item carries its own spoken name instead.
package web

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"sitecontrol/internal/humantime"
)

// lostSignalMarker is what lostsignalslot renders. An HTML comment, so a
// response that never passes through lostSignalStrip (a handler test, a
// non-HTML route) carries nothing visible and the empty slot stays :empty.
const lostSignalMarker = "<!--sc:lost-signal-->"

// lostSignalSlot is the template func behind {{lostsignalslot}}.
func lostSignalSlot() template.HTML { return template.HTML(lostSignalMarker) }

// lostSignal is THE rule (file comment). interval is the configured sync
// interval; without one there is no ceiling and nothing is called lost
// (buildPeerView words that case itself).
func lostSignal(contactAgo *float64, uptime time.Duration, pullKind string, interval time.Duration) bool {
	if pullKind != "" || interval <= 0 {
		return false
	}
	ceiling := (syncStaleMultiple * interval).Seconds()
	if contactAgo == nil {
		return uptime.Seconds() > ceiling
	}
	return *contactAgo > ceiling
}

// signalSiteView is one site as the strip and the Signal tile show it.
type signalSiteView struct {
	SiteID string
	Name   string
	Own    bool
	Lost   bool
	OK     bool // the peer's ONE Healthy verdict
	// Fault: neither OK nor lost — a key fault, a stalled log, a failed
	// attempt still inside the ceiling. FaultText says which, briefly.
	Fault     string
	Heard     string // "last heard 14:32 (8 minutes ago)"
	HeardAt   string // "14:32" — the phone strip's short form
	When      string // "last heard 14:32" — the post line's wide form
	Spoken    string // "NPF, lost signal, last heard 8 minutes ago"
	NeverSeen bool
}

// lostSignalView is the strip line's whole data.
type lostSignalView struct {
	Sites    []signalSiteView // the LOST peers only, config order
	All      bool             // every configured peer is lost
	Dispatch bool
	Serving  string // the serving post's display name
	// Rule is the threshold in words, for the line's title and the tile.
	Rule string
}

// signalRule states the ceiling the lost judgement used, naming the post
// that judged it (serving): on the dispatch host that is the post behind it.
func signalRule(v syncStatusView, serving string) string {
	if v.StaleMultiple <= 0 || v.StaleAfter <= 0 {
		return "A site is LOST when " + serving + " has completed no sync with it for longer than the ceiling the sync status uses to call a site stale."
	}
	return fmt.Sprintf("A site is LOST when %s has completed no sync with it for more than %d× the sync interval (%s), i.e. %s — the same ceiling the sync status uses to call a site stale. A site that answers but refuses the key is a key fault, shown on the sync status, not lost signal.",
		serving, v.StaleMultiple, v.StaleAfter/time.Duration(v.StaleMultiple), v.StaleAfter)
}

// buildSignalSites renders every configured peer's contact state, in config
// order, as of now. Pure: the strip and the tile share it, so they can never
// disagree about a site.
func buildSignalSites(v syncStatusView, name func(string) string, serving string, now time.Time) []signalSiteView {
	out := make([]signalSiteView, 0, len(v.Peers))
	for _, p := range v.Peers {
		sv := signalSiteView{SiteID: p.SiteID, Name: name(p.SiteID), Lost: p.Lost, OK: p.Healthy && !p.Lost, NeverSeen: p.ContactAgo == nil}
		if p.ContactAgo != nil {
			ago := time.Duration(*p.ContactAgo * float64(time.Second))
			sv.HeardAt = clockText(now, now.Add(-ago))
			sv.When = "last heard " + sv.HeardAt
			sv.Heard = sv.When + " (" + humantime.Ago(ago) + ")"
			sv.Spoken = sv.Name + ", lost signal, last heard " + humantime.Ago(ago)
		} else {
			sv.HeardAt = "none since " + clockText(now, now.Add(-v.Uptime))
			sv.When = "not heard since " + clockText(now, now.Add(-v.Uptime))
			sv.Heard = "not heard since " + serving + " started at " + clockText(now, now.Add(-v.Uptime)) + " (" + humantime.Ago(v.Uptime) + ")"
			sv.Spoken = sv.Name + ", lost signal, not heard since " + serving + " started " + humantime.Ago(v.Uptime)
		}
		if !sv.Lost && !sv.OK {
			switch {
			case p.KeyRejected:
				sv.Fault = "key mismatch — see the sync status"
			case p.CannotRead:
				sv.Fault = "cannot read this post — see the sync status"
			default:
				sv.Fault = "not current — see the sync status"
			}
		}
		out = append(out, sv)
	}
	return out
}

// clockText is an instant on this post's local clock: "14:32" today, with
// the date in front on any other day.
func clockText(now, at time.Time) string {
	now, at = now.In(time.Local), at.In(time.Local)
	if now.Format("2006-01-02") != at.Format("2006-01-02") {
		return at.Format("Jan 2 15:04")
	}
	return at.Format("15:04")
}

// buildLostSignal is the strip line's view, or ok=false when there is no
// line to draw: sync status unavailable (the chip already says "cannot
// tell"), no peers, or every peer in contact.
func buildLostSignal(v syncStatusView, name func(string) string, serving string, now time.Time, dispatch bool) (lostSignalView, bool) {
	if !v.Available || len(v.Peers) == 0 {
		return lostSignalView{}, false
	}
	lv := lostSignalView{Dispatch: dispatch, Serving: serving, Rule: signalRule(v, serving)}
	for _, s := range buildSignalSites(v, name, serving, now) {
		if s.Lost {
			lv.Sites = append(lv.Sites, s)
		}
	}
	if len(lv.Sites) == 0 {
		return lostSignalView{}, false
	}
	lv.All = len(lv.Sites) == len(v.Peers)
	return lv, true
}

// lostSignalStrip wraps the served surface (Root): it fills the top bar's
// slot with this request's LOST SIGNAL line, or with nothing.
func (s *Server) lostSignalStrip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&lostSignalWriter{ResponseWriter: w, s: s, dispatch: dispatchHost(r.Host)}, r)
	})
}

type lostSignalWriter struct {
	http.ResponseWriter
	s        *Server
	dispatch bool
}

var lostSignalMarkerBytes = []byte(lostSignalMarker)

func (lw *lostSignalWriter) Write(p []byte) (int, error) {
	i := bytes.Index(p, lostSignalMarkerBytes)
	if i < 0 {
		return lw.ResponseWriter.Write(p)
	}
	out := make([]byte, 0, len(p)+512)
	out = append(out, p[:i]...)
	out = append(out, lw.s.renderLostSignal(lw.dispatch)...)
	out = append(out, p[i+len(lostSignalMarkerBytes):]...)
	if _, err := lw.ResponseWriter.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}

// renderLostSignal renders the line for this post's sync state now.
func (s *Server) renderLostSignal(dispatch bool) []byte {
	if s.Cfg == nil {
		return nil
	}
	now := time.Now()
	if s.Store != nil {
		now = time.UnixMilli(s.clockReading().WallMS)
	}
	lv, ok := buildLostSignal(s.buildSyncSection(), s.Cfg.SiteName, s.Cfg.PostName(), now, dispatch)
	if !ok {
		return nil
	}
	var buf bytes.Buffer
	if err := s.templates().ExecuteTemplate(&buf, "lost_signal", lv); err != nil {
		// Never silently nothing: a line that failed to render still says
		// the one thing it exists to say.
		log.Printf("lost signal render: %v", err)
		return []byte(`<div class="notice notice-fault banner-notice alert-line lost-signal" data-lost-signal="error"><strong>LOST SIGNAL</strong> — contact with one or more sites is lost; see the sync status.</div>`)
	}
	return buf.Bytes()
}
