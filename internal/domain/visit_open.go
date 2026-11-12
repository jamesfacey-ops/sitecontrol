// visit_open.go — SchemaVersion 15, R-20 (James, 2026-09-13: "Make the
// presence folds order-independent"). THE derivation of visit.is_open,
// stated once, as a pure set-function of the raw tables.
//
// WHAT WAS WRONG. visit.is_open used to be mutated in place by three
// predicate UPDATEs inside the folds: dispatch closed every open visit of
// each occupant at the origin site; an arrival (or an entry-on-completion)
// closed the person's prior open visit before opening the new one; a
// check-out closed by visit id. Each was correct WITHIN ONE STREAM, and the
// premise written beside them — carry-forward ROW 28, "every visit row at
// a site is only ever mutated by that same site's own stream, which is
// contiguous" — is a claim about a STREAM (site_id, node_epoch) that was
// being relied on as a claim about a SITE. D-87's ownership unit is the
// site; D-92's ordering unit is the stream; a re-imaged post owns rows on
// two streams, and nothing orders them but delivery. MEASURED through the
// real repl.Engine (reports/witness/D87-D92-COHERENCE.md; the shipped
// witness is repl's TestR20EngineLateSelfHistoryAfterAuthoringAgreesWithThePeer):
// site-b epoch 1 checks Pat in and out → site-b is wiped → boots epoch 2,
// Rejoin cannot reach the peer, main.go authors anyway → Pat is checked in
// on epoch 2 → the link returns and pullPeer's self-history retry applies
// site-b's OWN epoch-1 stream AFTER epoch 2 was folded → the epoch-1
// dispatch closed the epoch-2 visit by predicate. Author: Pat OFF site.
// Peer and a from-zero replay: ON site. Identical raw logs, different
// muster, under-report direction, at the authoritative post.
//
// ⚠ ROW 28 IS FALSIFIED FOR THE SECOND TIME. F8 falsified it for PEERS;
// that fix constrained DELIVERY (epoch-major order in repl/engine.go's
// drainSite) and re-marked the premise "true by construction". A premise
// repaired by constraining delivery is guarded only by whoever remembers
// the constraint — the later self-history retry did not, because its
// ordering note weighed only cross-site interleaving. This file removes
// the premise instead of re-guarding it: the folds no longer depend on
// order at all. Epoch-major delivery stays (it is still the right order to
// deliver in, and other readers benefit) but nothing here rests on it.
//
// THE RULE. For one (site_id, person_id), over the raw tables only —
// `visit` (INSERT-only, each row carrying its minting envelope's
// node_epoch/seq), `trip` + `trip_occupant` (a dispatch at origin_site =
// site_id naming the person), `visit_checkout` (a check-out naming the
// visit under the same site tag):
//
//	the OPEN visit, if any, is the visit with the GREATEST key
//	(node_epoch, seq; visit_id as a total tiebreak), and it is open iff
//	  • no dispatch of that person from that site carries a GREATER key, and
//	  • no check-out for it exists.
//	Every other visit of the pair is closed.
//
// That is exactly what the in-order predicate folds computed when delivery
// happened to be in order — a later arrival supersedes the earlier visit,
// a later dispatch closes it, a check-out closes it by id — restated so it
// holds for ANY order the same rows arrive in. "Greater" is the sanctioned
// key order (store.Key.Less) within the one owning site; both sides of
// every comparison carry site_id = site by construction, so the SiteID limb
// never decides, and no clock is read anywhere in this file (D-90/D-92).
// Two visits minted by ONE envelope for ONE person cannot be authored
// (buildArrivalVisits mints one per occupant), so the visit_id tiebreak
// exists to make the function total, not to choose between real cases.
//
// WHERE IT RUNS. In the `visit` projection's Finalize — once per applied
// batch, after every fold, on the same transaction (store.Projection's
// F28 seam; orientation.go's finalizeOrientation is the sibling). It
// re-derives every (site, person) pair the batch's presence events name,
// so is_open after a commit is f(raw tables) regardless of the order the
// batch's envelopes — or any earlier batch's — arrived in. Adding rows can
// only close (a newer visit, a later dispatch, a check-out); nothing
// re-opens, and the derivation is idempotent.
//
// ⚠ THE FOLD NEVER REFUSES (W3-2) and never calls Author (row 9). D-87 is
// preserved at the derivation exactly as it was at the predicates: a
// dispatch counts only at ITS origin site, a check-out only under ITS own
// site tag, so a foreign site's rows are inert against this site's visits.
//
// ⚠ NOT A READER OF ANY DERIVED TABLE. trip_state, orientation and the
// alert tables are never consulted here — the derivation is a function of
// raw rows only, which is what lets it run in Finalize order beside them
// without a dependency in either direction.
//
// ⚠ THE RAW COLUMNS ARE IMMUTABLE BY TRIGGER (store.go, visit_raw_immutable):
// is_open is the only column an UPDATE may touch, so a fold cannot regress
// to mutating presence by predicate without aborting at the SQL layer.
package domain

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"sitecontrol/internal/store"
)

// visitOpenFinalize collects every (site, person) pair whose open state
// the batch could have changed and re-derives each — the visit
// projection's Finalize. Pairs are gathered into a map and SORTED before
// iteration (C1: force ordering; never hope for incidental map order), as
// finalizeOrientation does — not that it changes the result, which is
// per-pair and commutative, but a determinism proof should not have to
// argue that.
func visitOpenFinalize(tx *sql.Tx, applied []store.Envelope) error {
	type pair struct{ site, person string }
	touched := map[pair]bool{}
	for _, env := range applied {
		var head struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(env.Payload, &head); err != nil {
			continue // not an object payload; no presence event can be one
		}
		switch head.Type {
		case "trip_dispatched":
			var p TripDispatched
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				return fmt.Errorf("domain: visit derive: %v: %w", env.Key(), err)
			}
			// The driver is ALWAYS an occupant (§7) — the fold enforces it,
			// and the derivation names the same set the fold wrote.
			touched[pair{env.SiteID, p.DriverPersonID}] = true
			for _, id := range p.OccupantPersonIDs {
				touched[pair{env.SiteID, id}] = true
			}
		case "trip_arrival":
			var p TripArrival
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				return fmt.Errorf("domain: visit derive: %v: %w", env.Key(), err)
			}
			for _, v := range p.Visits {
				touched[pair{env.SiteID, v.PersonID}] = true
			}
		case evtOrientationCompleted:
			var p OrientationCompleted
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				return fmt.Errorf("domain: visit derive: %v: %w", env.Key(), err)
			}
			if p.Visit != nil {
				// The fold FORCES the minted visit's person to the
				// COMPLETING person (orientation.go's R-9 fold guard: a
				// payload's own visit.person_id is never trusted), so the
				// pair the derivation must revisit is (site, p.PersonID) —
				// the row the fold actually wrote, never the payload's claim.
				touched[pair{env.SiteID, p.PersonID}] = true
			}
		case "visit_checked_out":
			var p VisitCheckedOut
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				return fmt.Errorf("domain: visit derive: %v: %w", env.Key(), err)
			}
			// A check-out names a VISIT, not a person: the pair comes from
			// the visit row. A check-out that lands before its visit
			// (dangling) touches nothing now; the visit's own envelope
			// names the pair when it arrives, and the derivation then finds
			// the check-out row already in place. Same dangling tolerance
			// orientation's retraction-before-completion has.
			var site, person string
			err := tx.QueryRow(`SELECT site_id, person_id FROM visit WHERE visit_id = ?`, p.VisitID).Scan(&site, &person)
			switch {
			case err == nil:
				touched[pair{site, person}] = true
			case errors.Is(err, sql.ErrNoRows):
			default:
				return fmt.Errorf("domain: visit derive: %v: %w", env.Key(), err)
			}
		}
	}
	if len(touched) == 0 {
		return nil
	}
	pairs := make([]pair, 0, len(touched))
	for p := range touched {
		pairs = append(pairs, p)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].site != pairs[j].site {
			return pairs[i].site < pairs[j].site
		}
		return pairs[i].person < pairs[j].person
	})
	for _, p := range pairs {
		if err := rederiveVisitOpen(tx, p.site, p.person); err != nil {
			return err
		}
	}
	return nil
}

// rederiveVisitOpen is THE derivation, stated once (file header: THE RULE).
// Idempotent; a pure function of `visit`, `trip`+`trip_occupant` and
// `visit_checkout` for the one pair.
func rederiveVisitOpen(tx *sql.Tx, siteID, personID string) error {
	var (
		topID           string
		topEpoch, topSq int64
	)
	err := tx.QueryRow(
		`SELECT visit_id, node_epoch, seq FROM visit
		  WHERE site_id = ? AND person_id = ?
		  ORDER BY node_epoch DESC, seq DESC, visit_id DESC LIMIT 1`, siteID, personID).Scan(&topID, &topEpoch, &topSq)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// No visit of this pair: a dispatch or a check-out alone opens
		// nothing, and there is nothing to close.
		return nil
	case err != nil:
		return fmt.Errorf("domain: visit derive (%s, %s): top visit: %w", siteID, personID, err)
	}
	open := true

	// A dispatch of this person FROM this site with a greater key closes
	// it — the dispatch-time check-out of §10, as a comparison of keys
	// rather than an in-place predicate.
	var dEpoch, dSeq int64
	err = tx.QueryRow(
		`SELECT t.node_epoch, t.seq FROM trip_occupant o
		   JOIN trip t ON t.trip_id = o.trip_id
		  WHERE o.person_id = ? AND t.origin_site = ?
		  ORDER BY t.node_epoch DESC, t.seq DESC LIMIT 1`, personID, siteID).Scan(&dEpoch, &dSeq)
	switch {
	case err == nil:
		visitKey := store.Key{SiteID: siteID, NodeEpoch: topEpoch, Seq: topSq}
		dispatchKey := store.Key{SiteID: siteID, NodeEpoch: dEpoch, Seq: dSeq}
		if visitKey.Less(dispatchKey) {
			open = false
		}
	case errors.Is(err, sql.ErrNoRows):
	default:
		return fmt.Errorf("domain: visit derive (%s, %s): latest dispatch: %w", siteID, personID, err)
	}

	// A check-out naming it, under the same site tag (D-87: a foreign
	// site's check-out is inert here exactly as the old predicate's
	// `AND site_id = ?` made it), closes it whatever its key — a check-out
	// is authored against a visit that already exists, so it is causally
	// after the visit by construction and needs no key comparison.
	if open {
		var one int
		err = tx.QueryRow(`SELECT 1 FROM visit_checkout WHERE visit_id = ? AND site_id = ?`, topID, siteID).Scan(&one)
		switch {
		case err == nil:
			open = false
		case errors.Is(err, sql.ErrNoRows):
		default:
			return fmt.Errorf("domain: visit derive (%s, %s): check-out: %w", siteID, personID, err)
		}
	}

	// Every visit of the pair other than the greatest is superseded and
	// closed; the greatest takes the computed value. Both statements are
	// no-ops when the table already says so — the only UPDATEs on `visit`
	// in the tree, and both touch is_open alone (the trigger forbids more).
	if _, err := tx.Exec(
		`UPDATE visit SET is_open = 0 WHERE site_id = ? AND person_id = ? AND is_open = 1 AND visit_id <> ?`,
		siteID, personID, topID,
	); err != nil {
		return fmt.Errorf("domain: visit derive (%s, %s): close superseded: %w", siteID, personID, err)
	}
	want := 0
	if open {
		want = 1
	}
	if _, err := tx.Exec(
		`UPDATE visit SET is_open = ? WHERE visit_id = ? AND is_open <> ?`,
		want, topID, want,
	); err != nil {
		return fmt.Errorf("domain: visit derive (%s, %s): set %s: %w", siteID, personID, topID, err)
	}
	return nil
}
