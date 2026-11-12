// w9c_seams_test.go — W9c (item 17, F6's alert-ack retraction): shared
// fixture machinery for the WEB-side W9c criteria (W9c-6, W9c-9, W9c-10).
// NO ASSERTIONS LIVE HERE.
//
// ⚠ The design decision behind this file is stated in full in
// internal/deadline/w9c_seams_test.go and is the same one: W9c names no
// unbuilt Go symbol, so every W9c test COMPILES on the pre-chunk tree and
// every pre-chunk failure is a produced RUNTIME red. Acks and retractions
// that must state their own log keys are hand-built envelopes carrying F6
// §2's frozen wire JSON, applied through the real store.Apply; everything
// else goes through the real HTTP surface via srv.Handler().
//
// ⚠ `store.EachEvent` — the seam W9c-6 and W9c-9 name for their event
// counts — does not exist on the delivered tree [MEASURED]. w9cLogCount
// reads `SELECT count(*) FROM event_log`, which is the total-log count
// those criteria mean and cannot miss a stream a per-(site, epoch) walk
// would.
package web

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// --- the frozen wire spellings, as literals (F6 §2) ---

const (
	w9cEvtAckRetracted = "alert_ack_retracted"
	w9cEvtAck          = "alert_acknowledged"

	w9cKeyType         = "type"
	w9cKeyAckSiteID    = "ack_site_id"
	w9cKeyAckNodeEpoch = "ack_node_epoch"
	w9cKeyAckSeq       = "ack_seq"
	w9cKeyRecordedBy   = "recorded_by"
	w9cKeyTripID       = "trip_id"

	// the un-ack authoring surface (F6 §3.4) and its two form fields
	w9cUnackPath  = "/alerts/unack"
	w9cFormTripID = "trip_id"

	w9cPadTripID = "w9c-pad-no-such-trip"

	// the two posts, STATED — never inherited from web_test.go's default.
	w9cBoardSite = "site-b" // the post whose store every page below is fetched at
	w9cPeerSite  = "site-a" // the peer, and the origin of every inbound trip
)

// w9cKey is one log key — F6 §3.3's "FULL THREE-COLUMN key".
type w9cKey struct {
	Site  string
	Epoch int64
	Seq   int64
}

func (k w9cKey) String() string {
	return fmt.Sprintf("(%s, epoch %d, seq %d)", k.Site, k.Epoch, k.Seq)
}

func w9cEnvelope(k w9cKey, wallMS int64, payload map[string]any) store.Envelope {
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("w9c: marshalling a fixture payload: %v", err))
	}
	return store.Envelope{
		SiteID:        k.Site,
		NodeEpoch:     k.Epoch,
		Seq:           k.Seq,
		SchemaVersion: store.SchemaVersion,
		OriginAuth:    k.Site,
		AuthoredAt:    store.AuthoredAt{WallMS: wallMS, MaxErrorUS: 42, State: store.ClockSynced},
		OccurredAt:    wallMS,
		TimeSource:    "default",
		Payload:       raw,
	}
}

func w9cApply(t *testing.T, st *store.Store, env store.Envelope) store.Envelope {
	t.Helper()
	applied, err := st.Apply(env)
	if err != nil {
		t.Fatalf("w9c: apply %v: %v", w9cKey{env.SiteID, env.NodeEpoch, env.Seq}, err)
	}
	if !applied {
		t.Fatalf("w9c: apply %v reported a duplicate — the fixture's stream numbering is wrong", w9cKey{env.SiteID, env.NodeEpoch, env.Seq})
	}
	return env
}

func w9cNextSeq(t *testing.T, st *store.Store, site string, epoch int64) int64 {
	t.Helper()
	wm, err := st.Watermark(site, epoch)
	if err != nil {
		t.Fatalf("w9c: watermark(%s, %d): %v", site, epoch, err)
	}
	return wm + 1
}

// w9cPadTo advances (site, epoch) so the NEXT event lands at exactly `want`,
// failing loudly if the stream is already past it — a fixture that quietly
// gave up on its stated seq would be a fixture whose "stated literally" was
// a comment. Padding uses `alert_acknowledged` for a trip that does not
// exist, so no new replicated event-type literal enters the tree.
func w9cPadTo(t *testing.T, st *store.Store, site string, epoch, want int64) {
	t.Helper()
	next := w9cNextSeq(t, st, site, epoch)
	if next > want {
		t.Fatalf("w9c fixture: %s epoch %d is already at seq %d — cannot place the next event at the stated seq %d", site, epoch, next-1, want)
	}
	for ; next < want; next++ {
		w9cApply(t, st, w9cEnvelope(w9cKey{site, epoch, next}, 1_700_000_000_000+next,
			map[string]any{w9cKeyType: w9cEvtAck, w9cKeyTripID: w9cPadTripID}))
	}
}

// w9cAckAt places a plain ack at the STATED key, padding the stream to it.
func w9cAckAt(t *testing.T, st *store.Store, at w9cKey, tripID, recordedBy string, wallMS int64) w9cKey {
	t.Helper()
	w9cPadTo(t, st, at.Site, at.Epoch, at.Seq)
	p := map[string]any{w9cKeyType: w9cEvtAck, w9cKeyTripID: tripID}
	if recordedBy != "" {
		p[w9cKeyRecordedBy] = recordedBy
	}
	w9cApply(t, st, w9cEnvelope(at, wallMS, p))
	return at
}

// w9cRetractAt authors one F6 §2 retraction naming `target`. `at.Seq == 0`
// means the next free seq on that stream.
func w9cRetractAt(t *testing.T, st *store.Store, at, target w9cKey, recordedBy string, wallMS int64) w9cKey {
	t.Helper()
	if at.Seq == 0 {
		at.Seq = w9cNextSeq(t, st, at.Site, at.Epoch)
	}
	p := map[string]any{
		w9cKeyType:         w9cEvtAckRetracted,
		w9cKeyAckSiteID:    target.Site,
		w9cKeyAckNodeEpoch: target.Epoch,
		w9cKeyAckSeq:       target.Seq,
	}
	if recordedBy != "" {
		p[w9cKeyRecordedBy] = recordedBy
	}
	w9cApply(t, st, w9cEnvelope(at, wallMS, p))
	return at
}

// --- raw log reads (never an HTTP dump — D-56) ---

func w9cLogCount(t *testing.T, st *store.Store) int {
	t.Helper()
	db, err := sql.Open("sqlite", st.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM event_log`).Scan(&n); err != nil {
		t.Fatalf("w9c: counting event_log: %v", err)
	}
	return n
}

// w9cRetractionTargets is every retraction in the log, as the target key it
// names, in (site_id, node_epoch, seq) order of the RETRACTIONS.
func w9cRetractionTargets(t *testing.T, st *store.Store) []w9cKey {
	t.Helper()
	db, err := sql.Open("sqlite", st.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT payload FROM event_log ORDER BY site_id, node_epoch, seq`)
	if err != nil {
		t.Fatalf("w9c: reading event_log: %v", err)
	}
	defer rows.Close()
	var out []w9cKey
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var p map[string]any
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			continue
		}
		if s, _ := p[w9cKeyType].(string); s != w9cEvtAckRetracted {
			continue
		}
		site, _ := p[w9cKeyAckSiteID].(string)
		ep, _ := p[w9cKeyAckNodeEpoch].(float64)
		sq, _ := p[w9cKeyAckSeq].(float64)
		out = append(out, w9cKey{site, int64(ep), int64(sq)})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// w9cSortKeys puts a key set in a stable order for a literal comparison.
func w9cSortKeys(ks []w9cKey) []w9cKey {
	out := append([]w9cKey{}, ks...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Site != out[j].Site {
			return out[i].Site < out[j].Site
		}
		if out[i].Epoch != out[j].Epoch {
			return out[i].Epoch < out[j].Epoch
		}
		return out[i].Seq < out[j].Seq
	})
	return out
}

// --- the HTTP surface ---

// w9cUnack posts the un-ack form. ⟦was: a `phase *int64` argument submitted
// as `review_phase`, selecting the phase scope — weather phases removed,
// James 2026-10-04; a review_phase is now refused (fix3_no_weather_test.go)⟧
func w9cUnack(t *testing.T, srv *Server, tripID string) *httptest.ResponseRecorder {
	t.Helper()
	return postForm(t, srv, w9cUnackPath, url.Values{w9cFormTripID: {tripID}})
}

// w9cFollow303 follows a See Other, returning the rendered body. It fails
// with the response it actually got, so "the route does not exist yet" and
// "the handler refused" read differently in the log.
func w9cFollow303(t *testing.T, srv *Server, rec *httptest.ResponseRecorder, what string) string {
	t.Helper()
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("%s: POST %s = %d, want 303 See Other; body:\n%.400s", what, w9cUnackPath, rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatalf("%s: the 303 carried no Location header", what)
	}
	out := httptest.NewRecorder()
	srv.Handler().ServeHTTP(out, httptest.NewRequest("GET", loc, nil))
	if out.Code != http.StatusOK {
		t.Fatalf("%s: GET %s (the 303 target) = %d", what, loc, out.Code)
	}
	return out.Body.String()
}

// --- rendered-row extraction ---
//
// A per-trip REGION is one rendered block that mentions the trip. Two block
// shapes carry a trip on the operator console: the inbound board's
// <article class="board-row"> … </article> and the alert surface's
// <div class="notice … banner-notice"> … </div>. Regions are closed at their
// own end tag, NOT at the next opener, so nothing outside a row (the top
// bar, the post chip, page prose) can drift into the last region and make an
// absence assertion unfalsifiable.
//
// Scoping to a region is what lets ⟨c⟩'s never-acked CONTROL be asserted to
// carry NO repudiation clause on a page that necessarily carries one for the
// un-acked trip — and it reaches BOTH surfaces, so an attribution rendered on
// only one of them is still found.

type w9cBlockShape struct{ open, close string }

var w9cBlockShapes = []w9cBlockShape{
	{open: "<article", close: "</article>"},
	{open: `<div class="notice`, close: "</div>"},
}

// w9cRegionsMentioning returns every rendered block that mentions `needle`.
func w9cRegionsMentioning(page, needle string) []string {
	type span struct{ lo, hi int }
	var spans []span
	for _, sh := range w9cBlockShapes {
		for i := 0; ; {
			j := strings.Index(page[i:], sh.open)
			if j < 0 {
				break
			}
			lo := i + j
			k := strings.Index(page[lo:], sh.close)
			hi := len(page)
			if k >= 0 {
				hi = lo + k + len(sh.close)
			}
			spans = append(spans, span{lo, hi})
			i = lo + len(sh.open)
		}
	}
	sort.Slice(spans, func(a, b int) bool { return spans[a].lo < spans[b].lo })
	var out []string
	for _, sp := range spans {
		block := page[sp.lo:sp.hi]
		// FIX13 (James, 2026-10-05): the sticky strip's compact lines are
		// `.notice` blocks too, but each lists EVERY vehicle of its colour —
		// not a row — so they would merge trips here. ⟦was: no exclusion;
		// the strip carried one alert row⟧
		if strings.Contains(block, `data-alert-line=`) {
			continue
		}
		if strings.Contains(block, needle) {
			out = append(out, block)
		}
	}
	return out
}

// w9cRowText is every rendered block mentioning the trip, joined — the
// surface a per-row assertion is made against.
func w9cRowText(t *testing.T, page, label, needle string) string {
	t.Helper()
	regions := w9cRegionsMentioning(page, needle)
	if len(regions) == 0 {
		t.Fatalf("w9c: no rendered row mentions %s (%q) — the fixture's own row is missing from the page", label, needle)
	}
	return strings.Join(regions, "\n")
}

// w9cAckViaHTTP acknowledges through the REAL POST /alerts/ack, so the ack
// carries the production post attribution (s.postAttribution(), D-5), and
// returns the log key it took on the board post's stream.
func w9cAckViaHTTP(t *testing.T, srv *Server, st *store.Store, tripID string) w9cKey {
	t.Helper()
	want := w9cNextSeq(t, st, w9cBoardSite, 1)
	rec := postForm(t, srv, "/alerts/ack", url.Values{w9cFormTripID: {tripID}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("w9c: POST /alerts/ack for %s = %d; body:\n%.300s", tripID, rec.Code, rec.Body.String())
	}
	if got := w9cNextSeq(t, st, w9cBoardSite, 1); got != want+1 {
		t.Fatalf("w9c: the ack authored %d events, want exactly 1", got-want)
	}
	return w9cKey{w9cBoardSite, 1, want}
}
