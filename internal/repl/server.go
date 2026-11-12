// server.go — the pull server: the serving half of the replication engine.
// Serves this site's OWN log to authenticated peers. Demo-key auth per §8.4.
package repl

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"sitecontrol/internal/store"
)

// serverMaxPage caps a single pull page regardless of what the client asks
// for, so season-scale catch-up can never ship as one response (C2 review M1).
const serverMaxPage = 1000

// defaultPageLimit is used when a client omits limit, and is the Engine's
// default client-side page size.
const defaultPageLimit = 200

// SyncHandler returns the pull server: /sync/streams, /sync/pull, /sync/poke.
// Mount it at the site's root mux; patterns carry the full /sync/ prefix.
func (e *Engine) SyncHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sync/streams", e.requireKey(e.handleStreams))
	mux.HandleFunc("GET /sync/pull", e.requireKey(e.handlePull))
	mux.HandleFunc("POST /sync/poke", e.requireKey(e.handlePoke))
	return mux
}

// requireKey is the demo-key gate (§8.4: demo keys acceptable; rotation is
// production-doc material): peers must present THIS site's peer_key.
func (e *Engine) requireKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ok := r.Header.Get("Authorization") == "Bearer "+e.cfg.PeerKey
		// D26 (sc-test L12-Y31b): the refusal is the ONLY place this post can
		// learn that a peer cannot read it — see Engine.NoteInbound.
		e.NoteInbound(r.Header.Get(HeaderFrom), ok)
		if !ok {
			http.Error(w, "sync: bad or missing peer key", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// echoIn stamps t2 (local receive) and echoes the requester's t1. t3 is
// stamped by writeJSON just before the response is serialised.
func echoIn(r *http.Request) Echo {
	t2 := time.Now().UnixNano()
	t1, _ := strconv.ParseInt(r.URL.Query().Get("t1"), 10, 64)
	return Echo{T1: t1, T2: t2}
}

func writeJSON(w http.ResponseWriter, v interface{ stampT3() }) {
	v.stampT3()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (e *Echo) stampT3() { e.T3 = time.Now().UnixNano() }

// refuseForeignSite is the no-transitive-relay rule (§8.4): this server
// serves only the streams it AUTHORS. C3b's single sanctioned exception
// (§8.4/§8.6): a rejoining node may ask for records bearing its OWN
// site_id, from any authenticated peer — so `site` is also served when it
// equals the REQUESTER's own claimed identity (X-SiteControl-From), i.e. a
// node asking for exactly the stream it claims to be. This is not "relax
// auth during recovery": a request for a THIRD site's stream is refused
// exactly as before, regardless of the requester's claimed identity —
// `site` must equal either us or the requester's own claim, never anything
// else (see TestPullServerServesSelfHistoryOnlyNotAnyThirdSiteClaimedAsSelf).
func (e *Engine) refuseForeignSite(w http.ResponseWriter, r *http.Request, site string) bool {
	if site == e.cfg.SiteID {
		return false
	}
	if from := r.Header.Get(HeaderFrom); site != "" && site == from {
		return false
	}
	// FIX16 H (James, 2026-10-06: "every site has the full database"): a
	// RETIRED post's stream, held here in full, is served to any
	// authenticated peer, so a laptop rebuilt blank after the retirement
	// gets it back. Only a site this post's own config lists in
	// [[former_peers]] — never a live peer's stream (that is served by its
	// author alone, as before), never an arbitrary id.
	if e.isFormer(site) {
		return false
	}
	http.Error(w, fmt.Sprintf("sync: this site serves only its own stream %q, not %q (no transitive relay except a peer's own self-history, §8.4)", e.cfg.SiteID, site), http.StatusForbidden)
	return true
}

func (e *Engine) handleStreams(w http.ResponseWriter, r *http.Request) {
	resp := StreamsResponse{Echo: echoIn(r), SiteID: e.cfg.SiteID}
	site := r.URL.Query().Get("site")
	if e.refuseForeignSite(w, r, site) {
		return
	}
	epochs, err := e.st.Epochs(site)
	if err != nil {
		http.Error(w, "sync: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// The current config-seeded epoch is announced even before its first
	// event: a head-0 stream is a valid (empty) stream, and announcing it is
	// what lets a peer confirm it holds this site's head when nothing has
	// been authored yet.
	if site == e.cfg.SiteID && (len(epochs) == 0 || epochs[len(epochs)-1] < e.cfg.NodeEpoch) {
		epochs = append(epochs, e.cfg.NodeEpoch)
	}
	resp.Streams = make([]StreamHead, 0, len(epochs))
	for _, ep := range epochs {
		head, err := e.st.Watermark(site, ep) // head IS the watermark (row 3)
		if err != nil {
			http.Error(w, "sync: "+err.Error(), http.StatusInternalServerError)
			return
		}
		resp.Streams = append(resp.Streams, StreamHead{NodeEpoch: ep, Head: head})
	}
	writeJSON(w, &resp)
}

func (e *Engine) handlePull(w http.ResponseWriter, r *http.Request) {
	resp := PullResponse{Echo: echoIn(r), SiteID: e.cfg.SiteID}
	q := r.URL.Query()
	site := q.Get("site")
	if e.refuseForeignSite(w, r, site) {
		return
	}
	epoch, err1 := strconv.ParseInt(q.Get("epoch"), 10, 64)
	from, err2 := strconv.ParseInt(q.Get("from"), 10, 64)
	if err1 != nil || err2 != nil || epoch < 1 || from < 1 {
		http.Error(w, "sync: epoch and from must be positive integers", http.StatusBadRequest)
		return
	}
	limit, err := strconv.ParseInt(q.Get("limit"), 10, 64)
	if err != nil || limit < 1 {
		limit = defaultPageLimit
	}
	if limit > serverMaxPage {
		limit = serverMaxPage
	}
	// Page and head come from ONE snapshot (store.EventsAndHead, W7
	// 2026-10-03). They used to be two reads, page then head — which can
	// never yield a false "drained", but CAN straddle an Author commit and
	// hand the client an empty page beside a head one past its watermark,
	// which the client rightly refuses as "served page advanced nothing"
	// (engine.go pullStream: a server claiming a head it serves nothing of
	// is a protocol violation, not something to spin on) and logs as a SYNC
	// ERROR that clears on the next pull. With the §8.9 poke live, peers
	// pull during the author's own burst and the straddle stopped being
	// rare (ndb: 4 in 1 260 events, sc-test L12 Y-30). One snapshot keeps
	// both properties: head > wm implies the page carries wm+1, and a
	// drained verdict still means the client holds the head as of this read.
	events, head, err := e.st.EventsAndHead(site, epoch, from, limit)
	if err != nil {
		http.Error(w, "sync: "+err.Error(), http.StatusInternalServerError)
		return
	}
	resp.NodeEpoch, resp.Head, resp.Events = epoch, head, events
	if resp.Events == nil {
		resp.Events = []store.Envelope{} // [] on the wire, never null
	}
	// The seam echo: attest our envelope at from−1 so the client can prove
	// the two histories share a prefix, not merely a sequence number, before
	// applying anything (see PullResponse.PrevHash). from=1 has no seam; a
	// row we don't hold stays unattested and the client decides loudness.
	if from >= 2 {
		prev, err := e.st.Events(site, epoch, from-1, 1)
		if err != nil {
			http.Error(w, "sync: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if len(prev) == 1 && prev[0].Seq == from-1 {
			resp.PrevHash = prev[0].Hash()
		}
	}
	writeJSON(w, &resp)
}

func (e *Engine) handlePoke(w http.ResponseWriter, r *http.Request) {
	resp := PokeResponse{Echo: echoIn(r), SiteID: e.cfg.SiteID}
	from := r.Header.Get(HeaderFrom)
	if _, ok := e.peers[from]; !ok {
		http.Error(w, fmt.Sprintf("sync: poke from unknown peer %q", from), http.StatusForbidden)
		return
	}
	// Poke-then-pull (§8.9): the poke carries nothing; we pull the poker from
	// our own watermark, asynchronously and coalesced.
	e.requestPull(from)
	writeJSON(w, &resp)
}
