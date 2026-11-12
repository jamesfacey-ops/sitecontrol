// heldout.go — who was held out at THIS site's gate for lack of its
// orientation and has not been entered since: the READER half of
// d91-arrival-respec.md §3.4's edge ("a trip_arrival_not_entered row at this
// site for this person with no later visit at this site").
//
// THE GAP THIS CLOSES (2026-09-09). D-98 wrote the not-entered fact down and
// nothing read it back: trip_arrival_not_entered had ZERO production readers
// (MEASURED at f7afc3a, reports/witness/ORIENTATION-ENFORCEMENT.md), and the
// only signal an officer ever got was the one-shot NOT ENTERED notice on the
// gate page after the redirect (web/notentered.go), gone on navigation. Since
// the gate doors enforce the rule too (2dd6b1b), every refusal at the busiest
// door creates the question "who is waiting on an orientation?" and no
// surface could answer it. The failure direction was SAFE — nobody was
// over-counted — but "safe and invisible" is how a person ends up standing
// outside a gate that nobody remembers refusing.
//
// ⚠ D-98 said this fact "is not a state and is never rendered", and D-10
// still says the orientation flag goes on no muster and no printed sheet.
// This reader honours the second and narrows the first: it is rendered on
// exactly one screen, /orientation — the one screen D-10 already permits to
// show the flag, registered only where an orientation exists (web.go) — and
// on no muster, no printed sheet, no other page. What it lists is not a new
// category of person: it is the officer's own D-3 observations ("got off,
// not entered") that no later entry has yet superseded. It changes no fold,
// no schema, no event type and nothing about what any door does.
//
// ⛔ NOT BUILT HERE, deliberately: entry-on-completion (§3.4's other half — a
// completion that itself mints the visit). That is schema-bearing (a
// visit-minting fold path off a three-key event) and is gated to James. The
// next action this list points at is the one that exists today: record the
// completion, then check them in again at the gate — the same instruction
// the NOT ENTERED notice gives.
package domain

import (
	"sort"

	"sitecontrol/internal/store"
)

// HeldOut is one person held out at this site and not entered since.
type HeldOut struct {
	// PersonID is the MERGE-RESOLVED record ([[D-59]]: one human, one row;
	// composedOccupantRefs' rule) — the surviving record, which is the one
	// the marking surface lists and the one a tick would name.
	PersonID string
	// Name is that record's current roster name, falling back to the id —
	// a row never vanishes because a roster reference has not replicated.
	Name string
	// ArrivalID / Key: the hold-out arrival — the LATEST one when the same
	// person was held out more than once with no entry between. Key is its
	// source key at this site, which is what "since" is measured against
	// (same-stream (node_epoch, seq) is true authoring order, §8.3) and what
	// a surface hands to ResolveOccurredAt for the time it happened.
	ArrivalID string
	Key       store.Key
}

// HeldOutAt lists, oldest first, the people held out at siteID's gate for
// lack of its orientation who have not been entered at siteID since.
//
// "Entered since" is sightedSince — the SAME predicate OutAtUncontrolled,
// NotTracked and InTransitFrom use — so a fourth reader cannot disagree with
// the other three about what counts as a recorded return: an OPEN visit at
// this site right now (rule 3), or any visit created by a trip authored on
// this site's stream AFTER the hold-out (rule 2 — the ordinary second
// check-in, which authors a fresh degenerate trip here). A person entered
// here by an arrival that carries no orderable trip reference (an
// uncorrelated unsolicited arrival) and who has since LEFT is the case this
// cannot see and keeps listing: the asymmetry that errs toward listing, same
// as the other three, stated rather than hidden.
//
// Scope: rows whose arrival was authored AT siteID. A peer's hold-outs
// replicate into this store like every other row and are the peer's
// business (the flag is per-site, D-88). At an orientation=false post
// nothing ever writes a row for this site's arrivals (buildArrivalVisits
// never consults OrientationHeld there), so this returns nothing and costs
// one empty read — the ice road is untouched.
//
// Cost: the not-entered table as a whole (it holds only the un-oriented
// occupants of arrivals at orientation posts — small by construction and
// empty at a road post), then point lookups per distinct arrival and per
// person; never the season's visit or arrival tables.
func HeldOutAt(st *store.Store, siteID string) ([]HeldOut, error) {
	rows, err := dumpSection[ArrivalNotEntered](st, "trip_arrival_not_entered")
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	arrivals := map[string]*Arrival{}
	arrivalOf := func(id string) (*Arrival, error) {
		if a, ok := arrivals[id]; ok {
			return a, nil
		}
		a, err := oneRow[Arrival](st, "trip_arrival", store.Eq{Column: "arrival_id", Value: id})
		if err != nil {
			return nil, err
		}
		arrivals[id] = a
		return a, nil
	}
	// ⟦[[D-59]]⟧ merge-RESOLVED at the read, loaded once (a whole-table
	// derivation), exactly as the other three off-site readers do.
	merge, err := LoadPersonMergeMap(st)
	if err != nil {
		return nil, err
	}
	latest := map[string]HeldOut{} // representative person id → latest outstanding hold-out
	for _, r := range rows {
		a, err := arrivalOf(r.ArrivalID)
		if err != nil {
			return nil, err
		}
		if a == nil || a.SiteID != siteID {
			// A row whose arrival has not reached this store yet, or a
			// peer's hold-out: not this post's to answer for.
			continue
		}
		// The RAW id is what was held out and what any later entry of the
		// same record would be keyed on; the predicate runs on it, as the
		// other three readers run theirs on the raw occupant ids.
		back, err := sightedSince(st, r.PersonID, siteID, a.NodeEpoch, a.Seq)
		if err != nil {
			return nil, err
		}
		if back {
			continue
		}
		// SchemaVersion 12 (R-16): a SECOND, independent way off this list,
		// checked directly rather than through sightedSince's
		// visit-trip-origin heuristic. That heuristic was built for LOCAL
		// entries (a degenerate check-in's trip_id has OriginSite == siteID,
		// so it satisfies sightedSince's rule 2 forever, open or closed) and
		// never anticipated entry-on-completion's visit, whose trip_id is
		// the CROSS-SITE arrival that held the person out — its OriginSite
		// is the OTHER site, so rule 2 never matches it, and once that
		// visit closes, rule 3 (an OPEN visit here) stops matching too.
		// Without this check a person entry-on-completion entered would
		// REAPPEAR here the moment they left — the exact "append-only pile"
		// defect R-11 exists to refuse.
		//
		// ⟦D1 fix, WINDOW-AUDIT.md⟧ resolved through the merge BEFORE this
		// check: a duplicate merged (D-59) into a canonical record that
		// already holds this site's orientation must never be held out on
		// the strength of its own raw id — the record that matters is the
		// surviving one, and completions are recorded under it. Both ids are
		// checked: a raw hold-out that predates a merge is still real, and a
		// same-record completion recorded before the merge replicated here
		// is still real too.
		rep := merge.Resolve(r.PersonID)
		// ⟦CASE 3 fix, D87-D92-COHERENCE.md⟧ THIS USED TO BE "does rep/raw
		// hold this site's orientation" — D-88's unconditional guarantee
		// ("once a person holds a site's orientation they can never be held
		// out by it again") read as "a past not-entered row against them
		// carries no live meaning any more". That reads the FLAG as proof
		// of the ENTRY, and findEntryCandidate can falsify exactly that:
		// under incomplete self-knowledge (a post whose own not-entered row
		// was not yet visible to it when it authored a completion, e.g. a
		// re-image whose boot Rejoin could not recover its own history),
		// the completion lands with NO arrival_ref/trip_ref/visit at all —
		// and once the row later replicates in, orientation is real but no
		// entry ever was. Left unconditional, a person can end up ORIENTED,
		// UN-ENTERED, and dropped from the only list an officer with a bus
		// stopped at the gate can consult — worse than a stale row, which
		// is at least visible.
		//
		// The provable question is narrower and needs no orientation lookup
		// at all: does a visit exist at THIS site, for this person (raw or
		// merge-resolved), MINTED AFTER this hold-out's own arrival key?
		// D-88 gates every entry here on holding orientation first, so such
		// a visit's existence already implies the orientation was real —
		// the converse (oriented therefore entered) is exactly what case 3
		// falsifies, so it is never assumed. This still answers what the
		// old check existed for: entry-on-completion's visit carries the
		// arrival's own trip_ref (a cross-site or nil reference
		// sightedSince's heuristic cannot use), but it is always minted at
		// THIS site with a key later than the arrival it paid — a same-site
		// key comparison, exactly what keyLess already sorts this list by.
		// Absence of proof lists rather than excludes — the same asymmetry
		// this file's own doc comment states for sightedSince: a stale row
		// is visible noise, a wrong exclusion is the invisible person.
		key := store.Key{SiteID: a.SiteID, NodeEpoch: a.NodeEpoch, Seq: a.Seq}
		ids := []string{r.PersonID}
		if rep != r.PersonID {
			ids = append(ids, rep)
		}
		entered := false
		for _, id := range ids {
			vs, err := dumpSectionWhere[Visit](st, "visit",
				store.Eq{Column: "site_id", Value: siteID},
				store.Eq{Column: "person_id", Value: id})
			if err != nil {
				return nil, err
			}
			for _, v := range vs {
				if keyLess(key, store.Key{SiteID: siteID, NodeEpoch: v.NodeEpoch, Seq: v.Seq}) {
					entered = true
					break
				}
			}
			if entered {
				break
			}
		}
		if entered {
			continue
		}
		if prev, ok := latest[rep]; ok && !keyLess(prev.Key, key) {
			continue // an earlier hold-out of someone already listed at a later one
		}
		latest[rep] = HeldOut{PersonID: rep, ArrivalID: a.ArrivalID, Key: key}
	}
	out := make([]HeldOut, 0, len(latest))
	for rep, h := range latest {
		p, err := PersonByID(st, rep)
		if err != nil {
			return nil, err
		}
		h.Name = rep
		if p != nil && p.Name != "" {
			h.Name = p.Name
		}
		out = append(out, h)
	}
	// Oldest outstanding first by the arrival's source key — the person who
	// has been waiting longest reads first — never rowid, never wall-time
	// (row 10; the pair-read boundary).
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key != out[j].Key {
			return keyLess(out[i].Key, out[j].Key)
		}
		return out[i].PersonID < out[j].PersonID
	})
	return out, nil
}

// anyoneStillHeldOutFromTrip (SchemaVersion 12, R-16) answers whether any
// CURRENTLY held-out person at siteID was held out by an arrival of tripID —
// the live check tripauthor.go's "trip already arrived" refusal needs so its
// "check them in again" epilogue does not survive an entry-on-completion
// that already resolved everyone this trip held out. Reuses HeldOutAt
// rather than a second query shape, so the two can never disagree about who
// is still waiting.
func anyoneStillHeldOutFromTrip(st *store.Store, siteID, tripID string) (bool, error) {
	held, err := HeldOutAt(st, siteID)
	if err != nil {
		return false, err
	}
	for _, h := range held {
		a, err := oneRow[Arrival](st, "trip_arrival", store.Eq{Column: "arrival_id", Value: h.ArrivalID})
		if err != nil {
			return false, err
		}
		if a != nil && a.TripRef != nil && *a.TripRef == tripID {
			return true, nil
		}
	}
	return false, nil
}

// keyLess orders two source keys of ONE site's streams: epoch, then seq.
// Only ever called on keys sharing a SiteID (both are this site's arrivals).
func keyLess(a, b store.Key) bool {
	if a.NodeEpoch != b.NodeEpoch {
		return a.NodeEpoch < b.NodeEpoch
	}
	return a.Seq < b.Seq
}
