// visit_elsewhere.go — M-6 / T-L08-C (James, 2026-10-03, ruling): a person
// recorded ON SITE at this post who is ALSO recorded on site at a peer.
//
// WHAT HAPPENS. An unsolicited arrival at the pad of somebody the record
// still holds on site at the ice road opens a second visit. Under D-87
// ("read everything, modify only your own site's data") the pad's fold
// closes only the pad's own earlier visit for that person; the ice road's
// open row is not the pad's to close, and the ice road's own derivation
// (visit_open.go) counts only its own dispatches and check-outs. Both rows
// are therefore OPEN, at both posts, identically — MEASURED on L08
// (T-L08-C) and L13: two open visits in both posts' dumps, the person on
// both ON SITE lists and both musters, the origin told nothing.
//
// THE RULING: a READ-TIME flag, no cross-site write. Each post's muster and
// roster say, beside the name, "also recorded on site at <peer> since
// HH:MM"; BOTH visits stay counted. Closing the origin's visit from the pad
// would be a cross-site write (D-87 forbids it); dropping the person from
// one sheet would be the under-report direction, which the muster never
// takes ("an extra name costs search time, a missing name is what this
// sheet exists to prevent"). What was wrong was not the count but the
// silence: one person, two evacuation sheets, and neither sheet said so.
//
// DETERMINISTIC ACROSS POSTS. A pure function of the replicated visit rows
// and the sign-in instants behind them (VisitSignInTimes — the same read the
// gate roster already shows), so two posts holding the same rows render the
// same flags, each from its own side: the ice road says "also at the pad
// since 08:26", the pad says "also at the ice road since 06:40".
//
// ⛔ NO CLOCK IS READ AND NOTHING IS ORDERED (D-90/D-92). The instant shown
// is the PEER visit's recorded sign-in time, for display beside the name.
// WHICH of the two records is the stale one is deliberately NOT decided
// here — that would be a cross-site wall-clock comparison — so the flag
// states the fact in both directions and lets the officer ask by radio.
//
// SCOPE. Reads OpenVisitsAt per peer site (the F30 indexed open-set read,
// bounded by each site's current headcount, never by the season), for every
// site this store holds a stream from (store.Watermarks). A site this post
// has never heard from cannot flag anything — and cannot be flagged — which
// is the honest limit of what this post knows.
package domain

import (
	"fmt"
	"sort"

	"sitecontrol/internal/store"
)

// OpenElsewhere is one OTHER site's open visit for a person who also has an
// open visit at the site being rendered.
type OpenElsewhere struct {
	SiteID  string // the other site
	VisitID string // its open visit
	// SinceMS is that visit's recorded sign-in instant (VisitSignInTimes),
	// UTC ms; SinceOK is false when this store cannot resolve it (no arrival
	// event behind the row yet, or an unresolvable correction chain) — the
	// renderer shows the absence, never a guess.
	SinceMS int64
	SinceOK bool
}

// AlsoOnSiteElsewhere maps each of open's visit ids (those open at siteID;
// others are ignored) to the open visits the SAME person holds at OTHER
// sites, in (SiteID, VisitID) order. A visit with no such twin is absent
// from the map. Read-only; authors nothing.
func AlsoOnSiteElsewhere(st *store.Store, siteID string, open []Visit) (map[string][]OpenElsewhere, error) {
	local := map[string][]string{} // person -> local open visit ids
	for _, v := range open {
		if v.SiteID != siteID || v.IsOpen != 1 {
			continue
		}
		local[v.PersonID] = append(local[v.PersonID], v.VisitID)
	}
	out := map[string][]OpenElsewhere{}
	if len(local) == 0 {
		return out, nil
	}
	streams, err := st.Watermarks()
	if err != nil {
		return nil, fmt.Errorf("domain: also on site elsewhere: %w", err)
	}
	sites := map[string]bool{}
	for _, m := range streams {
		if m.SiteID != siteID {
			sites[m.SiteID] = true
		}
	}
	peers := make([]string, 0, len(sites))
	for s := range sites {
		peers = append(peers, s)
	}
	sort.Strings(peers) // C1: force ordering; never hope for incidental map order
	for _, peer := range peers {
		theirs, err := OpenVisitsAt(st, peer)
		if err != nil {
			return nil, fmt.Errorf("domain: also on site elsewhere: %s: %w", peer, err)
		}
		var twins []Visit
		for _, pv := range theirs {
			if _, ok := local[pv.PersonID]; ok {
				twins = append(twins, pv)
			}
		}
		if len(twins) == 0 {
			continue
		}
		times, err := VisitSignInTimes(st, peer, twins)
		if err != nil {
			return nil, fmt.Errorf("domain: also on site elsewhere: %s: %w", peer, err)
		}
		for _, pv := range twins {
			ms, ok := times[pv.VisitID]
			for _, lid := range local[pv.PersonID] {
				out[lid] = append(out[lid], OpenElsewhere{SiteID: peer, VisitID: pv.VisitID, SinceMS: ms, SinceOK: ok})
			}
		}
	}
	for _, list := range out {
		sort.Slice(list, func(i, j int) bool {
			if list[i].SiteID != list[j].SiteID {
				return list[i].SiteID < list[j].SiteID
			}
			return list[i].VisitID < list[j].VisitID
		})
	}
	return out, nil
}
