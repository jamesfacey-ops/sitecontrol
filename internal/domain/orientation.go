// orientation.go — SCHEMA-11 step 4: per-site orientation (D-88, D-95,
// D-91, D-98; reports/witness/STEP4-ORIENTATION.md). The two replicated
// events `orientation_completed` / `orientation_retracted`, their two RAW
// INSERT-only tables, the DERIVED `orientation(person_id, site_id)`
// projection, the read API the arrival split (step 7) consumes, and the
// authoring edges with their pre-append refusals.
//
// THE RULE (D-88 + amendment, D-91): every site except the ice road has its
// own orientation; a person takes it ONCE, AT THAT SITE, FOREVER; until it
// is completed that site does not enter them at all. Being in the system is
// not being oriented — one flag per (person, site).
//
// ⚠ THE SITE IS THE ENVELOPE'S, NEVER THE PAYLOAD'S. Neither event names a
// site. D-88 rule 4 (taken AT that site) is expressed as OWNERSHIP, exactly
// as visits and arrivals express theirs: the orienting post authors the
// event, so `site_id` on the envelope IS the site whose orientation it is.
// A payload that could name another site would be a payload that could
// assert another site's orientation — the D-87 violation made
// representable on the wire. It is not representable.
//
// ⚠ TWO RAW TABLES AND ONE DERIVED, not one table folded INSERT/DELETE, and
// the reason is order-independence ACROSS STREAMS (projection.go's header;
// the W1 scramble tests). A site re-imaged mid-season authors on a new
// node_epoch, so a completion on epoch 1 and its retraction on epoch 2 are
// two streams a peer may apply in either order; a single-table fold
// diverges under that permutation. The house pattern for exactly this is
// raw + derived (trip/trip_cancel → trip_state; alert_ack +
// alert_ack_retraction → EffectiveAcks). `orientation` is recomputed per
// (person, site) from the two raw tables inside the same transaction — a
// pure function of table contents, so identical at every post under any
// delivery order and dangling-tolerant (a retraction delivered before its
// completion leaves no row, and still none when the completion lands with
// the lower key — which is correct).
//
// ⚠ THE DERIVATION ORDERS BY THE SANCTIONED KEY, `store.Key.Less`, within
// the one owning site. No clock is read anywhere in this file (D-90/D-92).
//
// ⚠ THE FOLD NEVER REFUSES (W3-2). Every refusal below is at an authoring
// edge, pre-append; a peer's wire event folds whatever it says. A wire
// `orientation_retracted` from site X can only ever act on X's OWN
// (person, X) row, because the derivation keys on env.SiteID — so a
// cross-site retraction is INERT at the fold and REFUSED at the edge (the
// edge queries this post's own row and finds none). That is D-87 by
// construction; the `store.Author` choke point (step 8) adds nothing here
// but belt-and-braces.
//
// ⚠ VALUE DOMAIN, carried from D-10: only `true` and ABSENT are ever
// authored. There is no `false`, no expiry, no date arithmetic. A
// retraction is not `false` — it is a second recorded assertion that the
// first was a mis-tick; the derived row's absence afterwards is
// indistinguishable from never-oriented, which is the point.
//
// ⚠ NOT ON THE MUSTER OR ANY PRINTED SHEET (D-10, unreversed): nothing in
// this file renders, and no muster reader consults these tables.
//
// ⚠ NEITHER EVENT IS A §4.4 CARRIER. Both are membership-shaped — no value
// slot, nothing for Outcome to resolve — the same ground on which
// `arrival_ref_retracted` and `alert_ack_retracted` are exempt.
// `orientation_retracted` ends in `_retracted`, so the W9b-4 widened
// suffix classification enumerates it on any log that holds one; this
// step's own coverage witness exempts it with that written reason.
//
// ⚠ STEP 7 (D-91/D-98's arrival split, SCHEMA-11 step 5) READS
// `OrientationHeld` and nothing else. Entry-on-completion — d91 §3.4's
// optional `arrival_ref`/`trip_ref`/`visit` on the completion — landed
// SchemaVersion 12 (R-16): `findEntryCandidate` + `buildOrientationCompletedPayload`
// below, consumed by both `CompleteOrientation` and `CompleteOrientations`;
// its fold is `mintVisitFromArrivalVisit` (trip.go), the same statements
// trip_arrival's own fold uses. The step-4 payload set from the paragraph
// above is FROZEN AS OF THAT BUMP; it does not grow again without a new
// version.
package domain

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"sitecontrol/internal/store"
)

// --- the frozen wire (B-2 literal set) -----------------------------------

const (
	evtOrientationCompleted = "orientation_completed"
	evtOrientationRetracted = "orientation_retracted"
)

// OrientationCompleted is the wire payload. Step 4 froze it at exactly
// three keys — `type`, `person_id`, `recorded_by` — and NO site of any kind
// (see the file header; the orienting post's identity is the envelope's).
//
// SchemaVersion 12 (R-16, following [[D-92]]'s `seen` wire-bump precedent
// exactly): it grows by three `omitempty` keys — `arrival_ref`, `trip_ref`,
// `visit` — d91-arrival-respec.md §3.4, "entry after orientation IS the
// entry." All three are AUTHORING-TIME DERIVED (findEntryCandidate, below),
// never officer input: the edge looks for a `trip_arrival_not_entered` row
// at THIS site for this person with no visit already open here; if one
// exists, the completion carries the entry, snapshotted by the same
// buildArrivalVisits code path arrivals themselves use, for this one id.
// The bulk backfill door (D-95's marking control — thirty people from last
// season, no held-out row) finds no candidate and carries none, entering
// nobody — the same fold, unconditionally, for both doors.
type OrientationCompleted struct {
	Type       string  `json:"type"` // "orientation_completed"
	PersonID   string  `json:"person_id"`
	RecordedBy *string `json:"recorded_by,omitempty"` // post attribution (D-5)
	// ArrivalRef / TripRef / Visit: see the type comment above. ArrivalRef
	// names the trip_arrival that held this person out; TripRef is that
	// arrival's own trip_ref, carried through unchanged (may be nil — an
	// unsolicited arrival can hold someone out too); Visit is the minted
	// snapshot the fold materialises as an ordinary `visit` row, exactly as
	// trip_arrival's own visits[] entries are. All three are present
	// together or all three absent — never a partial triple (enforced at
	// the edge, not the wire; the fold does not validate, W3-2).
	ArrivalRef *string       `json:"arrival_ref,omitempty"`
	TripRef    *string       `json:"trip_ref,omitempty"`
	Visit      *ArrivalVisit `json:"visit,omitempty"`
}

// OrientationRetracted is the wire payload of D-95's mis-tick withdrawal —
// the same three keys. It names the PERSON; the (person, site) pair is the
// row, and the site is the authoring envelope's, so a retraction cannot
// name another site's row.
type OrientationRetracted struct {
	Type       string  `json:"type"` // "orientation_retracted"
	PersonID   string  `json:"person_id"`
	RecordedBy *string `json:"recorded_by,omitempty"`
}

// --- projection row shapes (tags = column names; dumpSection decodes) ---

// OrientationCompletionRow / OrientationRetractionRow are the raw rows: PK
// the envelope's OWN key, one row per event, never rewritten.
type OrientationCompletionRow struct {
	SiteID     string  `json:"site_id"`
	NodeEpoch  int64   `json:"node_epoch"`
	Seq        int64   `json:"seq"`
	PersonID   string  `json:"person_id"`
	RecordedBy *string `json:"recorded_by"`
	// ArrivalRef / TripRef (SchemaVersion 12): mirror the payload's own
	// fields (house style — a raw row mirrors what it was authored with).
	// The visit itself is not mirrored here; it materialises as its own
	// row in `visit`, exactly as trip_arrival's visits[] do.
	ArrivalRef *string `json:"arrival_ref"`
	TripRef    *string `json:"trip_ref"`
}

type OrientationRetractionRow struct {
	SiteID     string  `json:"site_id"`
	NodeEpoch  int64   `json:"node_epoch"`
	Seq        int64   `json:"seq"`
	PersonID   string  `json:"person_id"`
	RecordedBy *string `json:"recorded_by"`
}

// OrientationRow is the DERIVED row: "‹person› holds ‹site›'s orientation".
// Its existence IS the answer; NodeEpoch/Seq/RecordedBy are the completing
// event's, carried so a surface can say "recorded by ‹post›" and so the
// derivation is auditable against orientation_completion.
type OrientationRow struct {
	PersonID   string  `json:"person_id"`
	SiteID     string  `json:"site_id"`
	NodeEpoch  int64   `json:"node_epoch"`
	Seq        int64   `json:"seq"`
	RecordedBy *string `json:"recorded_by"`
}

// --- the projections -----------------------------------------------------

// orientationCompletionProjection is the raw completion table: STRICT,
// INSERT-only by trigger, PK the envelope's own key, dump-participating and
// therefore compared by dumpcompare by default (§67 inversion). NEW table —
// rides the 10 → 11 bump paid at the head of this train (ruling/schema-11
// stage 1a), which is why there is no bump here.
//
// `recorded_by` is `omitempty` on the wire, so the column is NULLABLE: a
// NOT NULL there would refuse a legal event at fold time, and the fold is
// never allowed to refuse.
func orientationCompletionProjection() store.Projection {
	return store.Projection{
		Name:    "orientation_completion",
		Table:   "orientation_completion",
		OrderBy: "site_id, node_epoch, seq",
		Schema: `CREATE TABLE IF NOT EXISTS orientation_completion (
			site_id     TEXT    NOT NULL,
			node_epoch  INTEGER NOT NULL,
			seq         INTEGER NOT NULL,
			person_id   TEXT    NOT NULL,
			recorded_by TEXT,
			-- Entry-on-completion (SchemaVersion 12, R-16): see
			-- OrientationCompleted's own field comment. NULL on every row a
			-- backfill/bulk-marking completion writes, and on every row
			-- authored before this bump.
			arrival_ref TEXT,
			trip_ref    TEXT,
			PRIMARY KEY (site_id, node_epoch, seq)
		) STRICT;
		-- The one read that is not a full scan: the per-(person, site)
		-- derivation below. Indexes are not dumped/replicated shape.
		CREATE INDEX IF NOT EXISTS idx_orientation_completion_pair ON orientation_completion (person_id, site_id);
		CREATE TRIGGER IF NOT EXISTS orientation_completion_insert_only_update
			BEFORE UPDATE ON orientation_completion BEGIN
			SELECT RAISE(ABORT, 'orientation_completion is INSERT-only: a recorded completion is never rewritten in place — withdraw it with orientation_retracted (D-95)');
		END;
		CREATE TRIGGER IF NOT EXISTS orientation_completion_insert_only_delete
			BEFORE DELETE ON orientation_completion BEGIN
			SELECT RAISE(ABORT, 'orientation_completion is INSERT-only: a deletable completion row is a silent un-orientation nobody recorded');
		END;`,
		Apply: applyOrientationCompleted,
	}
}

// orientationRetractionProjection is the raw retraction table, the same
// shape and the same discipline as its sibling.
func orientationRetractionProjection() store.Projection {
	return store.Projection{
		Name:    "orientation_retraction",
		Table:   "orientation_retraction",
		OrderBy: "site_id, node_epoch, seq",
		Schema: `CREATE TABLE IF NOT EXISTS orientation_retraction (
			site_id     TEXT    NOT NULL,
			node_epoch  INTEGER NOT NULL,
			seq         INTEGER NOT NULL,
			person_id   TEXT    NOT NULL,
			recorded_by TEXT,
			PRIMARY KEY (site_id, node_epoch, seq)
		) STRICT;
		CREATE INDEX IF NOT EXISTS idx_orientation_retraction_pair ON orientation_retraction (person_id, site_id);
		CREATE TRIGGER IF NOT EXISTS orientation_retraction_insert_only_update
			BEFORE UPDATE ON orientation_retraction BEGIN
			SELECT RAISE(ABORT, 'orientation_retraction is INSERT-only: a withdrawal is a recorded assertion, never rewritten in place');
		END;
		CREATE TRIGGER IF NOT EXISTS orientation_retraction_insert_only_delete
			BEFORE DELETE ON orientation_retraction BEGIN
			SELECT RAISE(ABORT, 'orientation_retraction is INSERT-only: a deletable retraction row is a silent re-orientation nobody recorded');
		END;`,
		Apply: applyOrientationRetracted,
	}
}

// orientationProjection is the DERIVED table — the projection the criteria
// name: `orientation(person_id, site_id)`. It has no per-event Apply; it is
// recomputed in Finalize, once per applied batch, for exactly the (person,
// site) pairs the batch touched, AFTER both raw folds have written their
// rows (Finalize runs after every envelope's Apply — log.go). No trigger:
// a derived row is rewritten or removed by derivation, never by hand.
func orientationProjection() store.Projection {
	return store.Projection{
		Name:    "orientation",
		Table:   "orientation",
		OrderBy: "person_id, site_id",
		Schema: `CREATE TABLE IF NOT EXISTS orientation (
			person_id   TEXT    NOT NULL,
			site_id     TEXT    NOT NULL,
			node_epoch  INTEGER NOT NULL,
			seq         INTEGER NOT NULL,
			recorded_by TEXT,
			PRIMARY KEY (person_id, site_id)
		) STRICT;`,
		Finalize: finalizeOrientation,
	}
}

// applyOrientationCompleted is the fold: a BARE INSERT, plus — SchemaVersion
// 12, entry-on-completion — one visit mint when the payload carries one.
// Still never refuses (W3-2): whatever the edge decided to author, the fold
// materialises unconditionally.
func applyOrientationCompleted(tx *sql.Tx, env store.Envelope) error {
	var p OrientationCompleted
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return fmt.Errorf("domain: orientation fold: %w", err)
	}
	if p.Type != evtOrientationCompleted {
		return nil
	}
	if _, err := tx.Exec(
		`INSERT INTO orientation_completion (site_id, node_epoch, seq, person_id, recorded_by, arrival_ref, trip_ref)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		env.SiteID, env.NodeEpoch, env.Seq, p.PersonID, p.RecordedBy, p.ArrivalRef, p.TripRef,
	); err != nil {
		return fmt.Errorf("domain: orientation fold: insert completion %v: %w", env.Key(), err)
	}
	if p.Visit != nil {
		// R-9 FOLD GUARD (RA1-OWNMINTED-RERUN.md's latent hole, closed
		// 2026-09-13): entry-on-completion's visit is, by definition, the
		// COMPLETING PERSON'S OWN entry — never trust a payload's own
		// visit.person_id, which the real edge (buildOrientationCompletedPayload)
		// always sets to match anyway. A caller that bypasses the edge and
		// authors via store.Author directly (the only PLANNED one being the
		// legacy-visit importer, ~1,927 orientation-less rows) could carry
		// a visit naming a DIFFERENT, un-oriented person — own-site, so not
		// a D-87 breach, but a real R-9 hole: a completion for one person
		// would open a visit, with no orientation check at all, for
		// somebody else entirely. The fold never refuses (W3-2), so the
		// closure is a correction, not a rejection: the minted visit's
		// person is FORCED to the completion's own PersonID. Closed here,
		// once, rather than left as a rule every future store.Author
		// caller must remember to honour on its own.
		v := *p.Visit
		v.PersonID = p.PersonID
		// ⟦SchemaVersion 15, R-20⟧ the whole envelope: the visit row carries
		// its minting key (trip.go's mint) — the fold's env, never a payload.
		if err := mintVisitFromArrivalVisit(tx, env, p.TripRef, v, p.RecordedBy); err != nil {
			return fmt.Errorf("domain: orientation fold: entry-on-completion %v: %w", env.Key(), err)
		}
	}
	return nil
}

// applyOrientationRetracted is the fold: a BARE INSERT, dangling-tolerant.
func applyOrientationRetracted(tx *sql.Tx, env store.Envelope) error {
	var p OrientationRetracted
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return fmt.Errorf("domain: orientation fold: %w", err)
	}
	if p.Type != evtOrientationRetracted {
		return nil
	}
	if _, err := tx.Exec(
		`INSERT INTO orientation_retraction (site_id, node_epoch, seq, person_id, recorded_by)
		 VALUES (?, ?, ?, ?, ?)`,
		env.SiteID, env.NodeEpoch, env.Seq, p.PersonID, p.RecordedBy,
	); err != nil {
		return fmt.Errorf("domain: orientation fold: insert retraction %v: %w", env.Key(), err)
	}
	return nil
}

// finalizeOrientation recomputes the derived row for every (person, site)
// pair the batch's orientation events touched. The pair set is collected
// into a map and then SORTED before iteration ((C1): force ordering; never
// hope for incidental map order), so the sequence of writes is the same at
// every post — not that it changes the result, which is per-pair and
// commutative, but a determinism proof should not have to argue that.
func finalizeOrientation(tx *sql.Tx, applied []store.Envelope) error {
	type pair struct{ person, site string }
	touched := map[pair]bool{}
	for _, env := range applied {
		var head struct {
			Type     string `json:"type"`
			PersonID string `json:"person_id"`
		}
		if err := json.Unmarshal(env.Payload, &head); err != nil {
			continue // not an object payload; no orientation event can be one
		}
		if head.Type != evtOrientationCompleted && head.Type != evtOrientationRetracted {
			continue
		}
		touched[pair{head.PersonID, env.SiteID}] = true
	}
	if len(touched) == 0 {
		return nil
	}
	pairs := make([]pair, 0, len(touched))
	for p := range touched {
		pairs = append(pairs, p)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].person != pairs[j].person {
			return pairs[i].person < pairs[j].person
		}
		return pairs[i].site < pairs[j].site
	})
	for _, p := range pairs {
		if err := rederiveOrientation(tx, p.person, p.site); err != nil {
			return err
		}
	}
	return nil
}

// rederiveOrientation is THE derivation, stated once: for (person, site),
// the row exists iff a completion exists and no retraction carries a
// GREATER key. "Greater" is `store.Key.Less` — the one comparator over
// keys — applied within the one owning site (both raw rows carry
// site_id = site by construction, so the SiteID limb never decides).
func rederiveOrientation(tx *sql.Tx, personID, siteID string) error {
	var (
		cEpoch, cSeq int64
		cBy          sql.NullString
		hasC         bool
	)
	err := tx.QueryRow(
		`SELECT node_epoch, seq, recorded_by FROM orientation_completion
		  WHERE person_id = ? AND site_id = ?
		  ORDER BY node_epoch DESC, seq DESC LIMIT 1`, personID, siteID).Scan(&cEpoch, &cSeq, &cBy)
	switch {
	case err == nil:
		hasC = true
	case errors.Is(err, sql.ErrNoRows):
	default:
		return fmt.Errorf("domain: orientation derive (%s, %s): completion: %w", personID, siteID, err)
	}
	var (
		rEpoch, rSeq int64
		hasR         bool
	)
	err = tx.QueryRow(
		`SELECT node_epoch, seq FROM orientation_retraction
		  WHERE person_id = ? AND site_id = ?
		  ORDER BY node_epoch DESC, seq DESC LIMIT 1`, personID, siteID).Scan(&rEpoch, &rSeq)
	switch {
	case err == nil:
		hasR = true
	case errors.Is(err, sql.ErrNoRows):
	default:
		return fmt.Errorf("domain: orientation derive (%s, %s): retraction: %w", personID, siteID, err)
	}
	held := hasC && (!hasR ||
		store.Key{SiteID: siteID, NodeEpoch: rEpoch, Seq: rSeq}.Less(store.Key{SiteID: siteID, NodeEpoch: cEpoch, Seq: cSeq}))
	if held {
		var by any
		if cBy.Valid {
			by = cBy.String
		}
		if _, err := tx.Exec(
			`INSERT INTO orientation (person_id, site_id, node_epoch, seq, recorded_by)
			 VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(person_id, site_id) DO UPDATE SET
			   node_epoch = excluded.node_epoch, seq = excluded.seq, recorded_by = excluded.recorded_by`,
			personID, siteID, cEpoch, cSeq, by,
		); err != nil {
			return fmt.Errorf("domain: orientation derive (%s, %s): upsert: %w", personID, siteID, err)
		}
		return nil
	}
	if _, err := tx.Exec(`DELETE FROM orientation WHERE person_id = ? AND site_id = ?`, personID, siteID); err != nil {
		return fmt.Errorf("domain: orientation derive (%s, %s): delete: %w", personID, siteID, err)
	}
	return nil
}

// --- the read API (D-87: every site reads all of it) --------------------

// OrientationHeld answers "does ‹person› hold ‹site›'s orientation?" — nil
// when not. This is the read the arrival split (step 7) makes at the
// destination for each ticked occupant, and the read the marking surface
// makes before it offers a checkbox. A point lookup on the derived table.
func OrientationHeld(st *store.Store, personID, siteID string) (*OrientationRow, error) {
	rows, err := dumpSectionWhere[OrientationRow](st, "orientation",
		store.Eq{Column: "person_id", Value: personID},
		store.Eq{Column: "site_id", Value: siteID})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// ⟦R4, James, 2026-10-04: merge carries orientation — the canonical record
// inherits the orientation of the merged-away record, visibly and
// reversibly on unmerge⟧
//
// A person who completed this site's orientation under record A, where A was
// later merged (same_as) into B, held out under B because only B's own row
// was read. The register is NOT rewritten to fix that: nothing is authored
// for B. Instead every "does this person hold it?" read resolves through the
// merge snapshot (LoadPersonMergeMap, derived per read) and accepts a row
// held by ANY record in the canonical's merge class. That is what makes it
// reversible: clear A's same_as and the next read no longer sees B as
// oriented, with no retraction to author or undo. CarriedFrom is what makes
// it visible: the surfaces that say "holds" say whose record it came from.

// OrientationHolding is one canonical person's standing orientation at a
// site, resolved through the merge. CarriedFrom is "" when the canonical
// record holds it itself, else the merged-away person id whose row it is.
type OrientationHolding struct {
	Row         OrientationRow
	CarriedFrom string
}

// OrientationHeldThroughMerge answers OrientationHeld for personID's
// CANONICAL record, accepting a row held by any record merged into it. The
// canonical's own row wins; otherwise the lowest merged-away id holding one
// (deterministic, so two surfaces never name different sources). nil when
// none holds it.
func OrientationHeldThroughMerge(st *store.Store, merge AliasMap, personID, siteID string) (*OrientationHolding, error) {
	canonical := merge.Resolve(personID)
	own, err := OrientationHeld(st, canonical, siteID)
	if err != nil {
		return nil, err
	}
	if own != nil {
		return &OrientationHolding{Row: *own}, nil
	}
	var members []string
	for raw := range merge {
		if raw != canonical && merge.Resolve(raw) == canonical {
			members = append(members, raw)
		}
	}
	sort.Strings(members)
	for _, id := range members {
		row, err := OrientationHeld(st, id, siteID)
		if err != nil {
			return nil, err
		}
		if row != nil {
			return &OrientationHolding{Row: *row, CarriedFrom: id}, nil
		}
	}
	return nil, nil
}

// OrientationHoldersAt is the whole-register form of
// OrientationHeldThroughMerge: canonical person id → holding, for one site.
// Look a person up by merge.Resolve(id) (the map is keyed canonical-only).
func OrientationHoldersAt(st *store.Store, merge AliasMap, siteID string) (map[string]OrientationHolding, error) {
	rows, err := OrientationsAt(st, siteID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]OrientationHolding, len(rows))
	// rows arrive in person_id order, so "lowest merged-away id" falls out
	// of keeping the first carried row seen.
	for _, r := range rows {
		c := merge.Resolve(r.PersonID)
		if r.PersonID == c {
			out[c] = OrientationHolding{Row: r}
			continue
		}
		if _, seen := out[c]; !seen {
			out[c] = OrientationHolding{Row: r, CarriedFrom: r.PersonID}
		}
	}
	return out, nil
}

// OrientationsAt lists everyone who holds one site's orientation, in
// person_id order.
func OrientationsAt(st *store.Store, siteID string) ([]OrientationRow, error) {
	return dumpSectionWhere[OrientationRow](st, "orientation", store.Eq{Column: "site_id", Value: siteID})
}

// OrientationsOf lists every site whose orientation one person holds —
// the "read everything" half of D-87, which is what lets an origin see
// whether a traveller holds the destination's orientation, should a later
// ruling want that shown (D-10's "no warning at dispatch" stands today).
func OrientationsOf(st *store.Store, personID string) ([]OrientationRow, error) {
	return dumpSectionWhere[OrientationRow](st, "orientation", store.Eq{Column: "person_id", Value: personID})
}

// --- the authoring edges --------------------------------------------------

// OrientationRefusalCode names WHICH pre-append refusal fired. Typed rather
// than a message so the officer-facing surface composes its own copy — a
// refusal rendered as a raw err.Error() puts a Go package prefix in front
// of the person who has to act on it (the house rule, unref.go).
type OrientationRefusalCode int

const (
	// OrientationPersonRequired: no person named.
	OrientationPersonRequired OrientationRefusalCode = iota + 1
	// OrientationPersonUnknown: this post holds no record of that person.
	// Orientation is not intake (D-18): nobody is enrolled from this edge.
	OrientationPersonUnknown
	// OrientationAlreadyHeld: the person already holds this site's
	// orientation. Once per person per site, forever (D-88) — a second
	// completion is refused rather than silently accepted, because a silent
	// no-op hides a double-marking mistake at a bulk screen.
	OrientationAlreadyHeld
	// OrientationNotHeld: nothing to retract — this post holds no orientation
	// row for that person. This is also every cross-site case: the edge
	// only ever looks at (person, THIS site).
	OrientationNotHeld
	// OrientationEnteredCannotRetract (SchemaVersion 12, d91-arrival-respec
	// §3.5): the person holds an OPEN visit at this site right now. Whether
	// that visit came from entry-on-completion or an ordinary oriented
	// arrival, retracting the orientation underneath someone standing on
	// site would let the orientation record and the visit disagree about
	// whether they may be here — refused rather than allowed to diverge.
	OrientationEnteredCannotRetract
)

// OrientationRefusal is one pre-append refusal. NOTHING is authored on any
// of them.
type OrientationRefusal struct {
	Code       OrientationRefusalCode
	PersonID   string
	PersonName string // composed display name when the person is known
	SiteID     string // the site whose orientation was in question (this post)
	HeldBy     string // AlreadyHeld: the post that recorded the standing completion, if attributed
}

func (e *OrientationRefusal) Error() string {
	who := e.PersonName
	if who == "" {
		who = e.PersonID
	}
	switch e.Code {
	case OrientationPersonRequired:
		return "domain: orientation: no person was named; nothing was recorded"
	case OrientationPersonUnknown:
		return fmt.Sprintf("domain: orientation: person %s is not known at this site; nothing was recorded", e.PersonID)
	case OrientationAlreadyHeld:
		by := e.HeldBy
		if by == "" {
			by = "an unattributed post"
		}
		return fmt.Sprintf("domain: orientation: %s already holds %s's orientation (recorded by %s); nothing was recorded", who, SiteLabel(e.SiteID), by)
	case OrientationNotHeld:
		return fmt.Sprintf("domain: orientation: %s does not hold %s's orientation; there is nothing to retract", who, SiteLabel(e.SiteID))
	case OrientationEnteredCannotRetract:
		return fmt.Sprintf("domain: orientation: %s's orientation record entered them on site; a person on site cannot become un-oriented; nothing was retracted", who)
	}
	return fmt.Sprintf("domain: orientation: %s refused", who)
}

// entryCandidate is what a held-out arrival offers a completion to carry as
// its entry (SchemaVersion 12, d91-arrival-respec.md §3.4).
type entryCandidate struct {
	ArrivalID string
	TripRef   *string
}

// findEntryCandidate looks for personID's most recent
// trip_arrival_not_entered row belonging to THIS site, with no visit
// already open here — d91 §3.4: "the edge looks for a
// trip_arrival_not_entered row at this site for this person with no later
// visit at this site." nil means backfill: completing carries no entry
// (D-95's bulk marking control finds nobody held out because nobody was
// ever aboard an arrival this site's door processed).
//
// Reads ONLY through the store's sanctioned projection API (dump.go's
// house rule: no raw-SQL join, Store.DB stays unexported) — the join
// happens in Go across two small, per-person-bounded result sets, never a
// full-table scan.
func findEntryCandidate(st *store.Store, siteID, personID string) (*entryCandidate, error) {
	openHere, err := dumpSectionWhere[Visit](st, "visit",
		store.Eq{Column: "site_id", Value: siteID},
		store.Eq{Column: "person_id", Value: personID},
		store.Eq{Column: "is_open", Value: 1})
	if err != nil {
		return nil, fmt.Errorf("domain: orientation: entry candidate: open visit: %w", err)
	}
	if len(openHere) > 0 {
		return nil, nil // already on site here some other way; nothing to enter
	}
	// ⟦D2 fix, WINDOW-AUDIT.md⟧ d91-arrival-respec.md §3.4's actual rule is
	// "no LATER visit at this site", not "no OPEN visit here" — the two
	// diverge the moment a completion-carried visit is later CHECKED OUT and
	// its completion RETRACTED (§3.5 permits retracting once the visit is
	// closed): the not_entered row itself is never deleted, so a bare
	// "nothing open right now" reading treats a row that has ALREADY
	// produced one entry as still owed and re-arms it on the next
	// completion — a desk action, weeks later, that mints a fresh visit with
	// no vehicle and no gate action (D2). A visit's trip_id records which
	// arrival it was entered FOR; a not-entered row whose own trip_ref
	// already owns a visit here — open or long since closed — has been paid
	// once and must never be treated as still outstanding.
	allHere, err := dumpSectionWhere[Visit](st, "visit",
		store.Eq{Column: "site_id", Value: siteID},
		store.Eq{Column: "person_id", Value: personID})
	if err != nil {
		return nil, fmt.Errorf("domain: orientation: entry candidate: visit history: %w", err)
	}
	consumedTrip := map[string]bool{}
	for _, v := range allHere {
		if v.TripID != nil {
			consumedTrip[*v.TripID] = true
		}
	}
	// ⟦D1 extension, R-22⟧ a not_entered row is keyed on whatever RAW id a
	// bus manifest named at arrival time. James (R-22,
	// design/REQUIREMENTS-FROM-JAMES-2026-09-09.md): the held-out person is
	// physically in the guard shack and the vehicle is STOPPED until their
	// orientation is complete — the completion the officer records is
	// necessarily against whichever id /orientation offers, and that page
	// filters out any record merged (D-59) into another (web/orientation.go,
	// `SameAs != nil → continue`), so a duplicate that was held out under
	// its OWN raw id and later merged into personID can only ever be
	// completed under personID. Without resolving the merge here, that
	// completion clears the held-out row (OrientationHeld already resolves
	// it — the D1 fix above) while THIS function still looks only at
	// personID's own not_entered rows and finds none: the row silently
	// disappears from the one screen an officer must trust, and nobody was
	// entered — the vehicle would be waved through with the seat still
	// empty. So: every raw id the merge map resolves TO personID is checked
	// alongside personID itself.
	merge, err := LoadPersonMergeMap(st)
	if err != nil {
		return nil, fmt.Errorf("domain: orientation: entry candidate: merge: %w", err)
	}
	candidateIDs := []string{personID}
	for raw := range merge {
		if raw != personID && merge.Resolve(raw) == personID {
			candidateIDs = append(candidateIDs, raw)
		}
	}
	var best *Arrival
	for _, id := range candidateIDs {
		notEntered, err := dumpSectionWhere[ArrivalNotEntered](st, "trip_arrival_not_entered",
			store.Eq{Column: "person_id", Value: id})
		if err != nil {
			return nil, fmt.Errorf("domain: orientation: entry candidate: not-entered rows: %w", err)
		}
		for _, row := range notEntered {
			arr, err := oneRow[Arrival](st, "trip_arrival", store.Eq{Column: "arrival_id", Value: row.ArrivalID})
			if err != nil {
				return nil, fmt.Errorf("domain: orientation: entry candidate: arrival %s: %w", row.ArrivalID, err)
			}
			if arr == nil || arr.SiteID != siteID {
				continue // held out at a DIFFERENT site's arrival — not this edge's to enter (D-87)
			}
			if arr.TripRef != nil && consumedTrip[*arr.TripRef] {
				continue // already produced a visit here — paid once, never re-armed
			}
			if best == nil || (store.Key{SiteID: best.SiteID, NodeEpoch: best.NodeEpoch, Seq: best.Seq}).
				Less(store.Key{SiteID: arr.SiteID, NodeEpoch: arr.NodeEpoch, Seq: arr.Seq}) {
				best = arr
			}
		}
	}
	if best == nil {
		return nil, nil
	}
	return &entryCandidate{ArrivalID: best.ArrivalID, TripRef: best.TripRef}, nil
}

// buildOrientationCompletedPayload composes the payload for personID's
// completion at siteID, finding and attaching an entry when one is owed
// (SchemaVersion 12) — the ONE place both the single and bulk edges derive
// it, so they cannot drift into two different entry rules.
func buildOrientationCompletedPayload(st *store.Store, siteID, personID string, recordedBy *string) (OrientationCompleted, error) {
	p := OrientationCompleted{Type: evtOrientationCompleted, PersonID: personID, RecordedBy: recordedBy}
	cand, err := findEntryCandidate(st, siteID, personID)
	if err != nil {
		return OrientationCompleted{}, err
	}
	if cand == nil {
		return p, nil
	}
	visits, _, err := buildArrivalVisits(st, siteID, []string{personID}, false)
	if err != nil {
		return OrientationCompleted{}, fmt.Errorf("domain: orientation: entry snapshot: %w", err)
	}
	if len(visits) != 1 {
		// buildArrivalVisits with hasOrientation=false never holds anyone
		// out, so this cannot happen; refuse loudly rather than author a
		// completion silently missing the entry it found.
		return OrientationCompleted{}, fmt.Errorf("domain: orientation: entry snapshot: expected 1 visit, got %d", len(visits))
	}
	v := visits[0]
	p.ArrivalRef = &cand.ArrivalID
	p.TripRef = cand.TripRef
	p.Visit = &v
	return p, nil
}

// CompleteOrientation appends ONE `orientation_completed` for personID at
// THIS post (siteID) — the orienting site, D-88 rule 4. Refusals are
// pre-append and typed; the returned envelope is the recorded key.
func CompleteOrientation(st *store.Store, siteID string, nodeEpoch int64, personID string, recordedBy *string, occurredAtMS int64, timeSource string) (store.Envelope, error) {
	if _, err := checkOrientationCompletable(st, siteID, personID); err != nil {
		return store.Envelope{}, err
	}
	p, err := buildOrientationCompletedPayload(st, siteID, personID, recordedBy)
	if err != nil {
		return store.Envelope{}, err
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: orientation: %w", err)
	}
	env, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: orientation: %w", err)
	}
	return env, nil
}

// CompleteOrientations is the bulk marking control's edge: VALIDATE ALL,
// THEN AUTHOR PER PERSON. Every id is checked before the first event is
// appended, so a refusal anywhere in the submission means NOTHING was
// recorded for any of it — the officer re-ticks a clean form rather than
// working out which of twenty-six landed. A duplicate id within one
// submission is refused as AlreadyHeld too (the second tick of the same
// name is the same mistake). One event per person (D-3: each affirmed
// individually), in the order given.
func CompleteOrientations(st *store.Store, siteID string, nodeEpoch int64, personIDs []string, recordedBy *string, occurredAtMS int64, timeSource string) ([]store.Envelope, error) {
	seen := map[string]bool{}
	names := make([]string, 0, len(personIDs))
	for _, id := range personIDs {
		name, err := checkOrientationCompletable(st, siteID, id)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, &OrientationRefusal{Code: OrientationAlreadyHeld, PersonID: id, PersonName: name, SiteID: siteID, HeldBy: "this submission"}
		}
		seen[id] = true
		names = append(names, name)
	}
	out := make([]store.Envelope, 0, len(personIDs))
	for i, id := range personIDs {
		p, err := buildOrientationCompletedPayload(st, siteID, id, recordedBy)
		if err != nil {
			return out, err
		}
		payload, err := json.Marshal(p)
		if err != nil {
			return out, fmt.Errorf("domain: orientation: %w", err)
		}
		env, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource)
		if err != nil {
			// A store failure mid-batch: what landed, landed (each is its own
			// committed event). Say how far it got, by name.
			return out, fmt.Errorf("domain: orientation: recorded %d of %d; failed at %s: %w", i, len(personIDs), names[i], err)
		}
		out = append(out, env)
	}
	return out, nil
}

// CompleteOrientationsSkippingHeld authors THIS site's orientation for
// exactly these person ids, de-duplicated and re-checked live
// (OrientationHeld) immediately before authoring: an id that already holds
// this post's orientation — ticked in error, or granted by a concurrent
// submission in the same instant — is skipped rather than sent to
// CompleteOrientations, which refuses a redundant completion as a mistake
// (OrientationAlreadyHeld). Skipping it here is correct, not a swallowed
// error: whatever door reads OrientationHeld next sees them oriented
// either way. Always default-captured (occurredAtMS 0, "default"),
// matching /orientation/complete's own EXEMPT convention — the completion
// is the act at the desk (or the gate tick standing in for one), never a
// typed instant.
//
// THE ONE authoring path for "ticking an orientation at the gate" — shared
// by web/gate.go's completeOrientationsFor (the accumulate form's own
// cross-site-warning tick, /gate/resume, the legacy check-in body) and
// GateCheckIn's own D3 fix below, so the rule cannot drift into two
// different implementations depending on which door it was ticked from.
func CompleteOrientationsSkippingHeld(st *store.Store, siteID string, ids []string, recordedBy *string, nodeEpoch int64) error {
	if len(ids) == 0 {
		return nil
	}
	merge, err := LoadPersonMergeMap(st)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var toComplete []string
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		// ⟦R4⟧ through the merge: a canonical that inherits a merged-away
		// record's completion already holds it — nothing to author.
		held, err := OrientationHeldThroughMerge(st, merge, id, siteID)
		if err != nil {
			return err
		}
		if held != nil {
			continue // already holds it — nothing to author, nothing wrong
		}
		toComplete = append(toComplete, id)
	}
	if len(toComplete) == 0 {
		return nil
	}
	_, err = CompleteOrientations(st, siteID, nodeEpoch, toComplete, recordedBy, 0, "default")
	return err
}

// completeOrientationsBeforeArrival is GateCheckIn's own call into
// CompleteOrientationsSkippingHeld — named separately only so its call site
// (D3 fix, WINDOW-AUDIT.md) reads as what it is: the check-in door's own
// choke point, not a generic helper reused by accident.
func completeOrientationsBeforeArrival(st *store.Store, siteID string, nodeEpoch int64, ids []string, recordedBy *string) error {
	return CompleteOrientationsSkippingHeld(st, siteID, ids, recordedBy, nodeEpoch)
}

// checkOrientationCompletable is the shared pre-append check for the single
// and bulk edges: named, known, not already held here. Returns the
// composed display name for the refusal copy.
func checkOrientationCompletable(st *store.Store, siteID, personID string) (string, error) {
	if personID == "" {
		return "", &OrientationRefusal{Code: OrientationPersonRequired, SiteID: siteID}
	}
	p, err := PersonByID(st, personID)
	if err != nil {
		return "", fmt.Errorf("domain: orientation: %w", err)
	}
	if p == nil {
		return "", &OrientationRefusal{Code: OrientationPersonUnknown, PersonID: personID, SiteID: siteID}
	}
	held, err := OrientationHeld(st, personID, siteID)
	if err != nil {
		return "", fmt.Errorf("domain: orientation: %w", err)
	}
	if held != nil {
		by := ""
		if held.RecordedBy != nil {
			by = *held.RecordedBy
		}
		return "", &OrientationRefusal{Code: OrientationAlreadyHeld, PersonID: personID, PersonName: p.Name, SiteID: siteID, HeldBy: by}
	}
	return p.Name, nil
}

// RetractOrientation appends ONE `orientation_retracted` for personID at
// THIS post — D-95: a mis-tick, withdrawn by the site that recorded it.
// Own-site by construction: the only row this edge consults is
// (personID, siteID), so a person who holds another site's orientation
// and not this one is refused NotHeld, and that other site's row is never
// touched (D-87). The check is the `checkin.go:134` shape — a per-edge
// own-tag guard — until the `store.Author` choke point lands.
func RetractOrientation(st *store.Store, siteID string, nodeEpoch int64, personID string, recordedBy *string, occurredAtMS int64, timeSource string) (store.Envelope, error) {
	if personID == "" {
		return store.Envelope{}, &OrientationRefusal{Code: OrientationPersonRequired, SiteID: siteID}
	}
	p, err := PersonByID(st, personID)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: orientation: %w", err)
	}
	if p == nil {
		return store.Envelope{}, &OrientationRefusal{Code: OrientationPersonUnknown, PersonID: personID, SiteID: siteID}
	}
	held, err := OrientationHeld(st, personID, siteID)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: orientation: %w", err)
	}
	if held == nil {
		return store.Envelope{}, &OrientationRefusal{Code: OrientationNotHeld, PersonID: personID, PersonName: p.Name, SiteID: siteID}
	}
	// d91 §3.5 (SchemaVersion 12): a person this orientation entered — or
	// who simply arrived oriented and is still here — cannot be un-oriented
	// while an open visit says they are on site. This is deliberately not
	// scoped to "only if entry-on-completion produced this visit": ANY open
	// visit at this hasOrientation=true site implies the standing
	// orientation is why they are allowed to be here, so retracting it
	// underneath them is the same hazard regardless of which door entered
	// them.
	openHere, err := dumpSectionWhere[Visit](st, "visit",
		store.Eq{Column: "site_id", Value: siteID},
		store.Eq{Column: "person_id", Value: personID},
		store.Eq{Column: "is_open", Value: 1})
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: orientation: %w", err)
	}
	if len(openHere) > 0 {
		return store.Envelope{}, &OrientationRefusal{Code: OrientationEnteredCannotRetract, PersonID: personID, PersonName: p.Name, SiteID: siteID}
	}
	payload, err := json.Marshal(OrientationRetracted{Type: evtOrientationRetracted, PersonID: personID, RecordedBy: recordedBy})
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: orientation: %w", err)
	}
	env, err := st.Author(siteID, nodeEpoch, payload, occurredAtMS, timeSource)
	if err != nil {
		return store.Envelope{}, fmt.Errorf("domain: orientation: %w", err)
	}
	return env, nil
}
