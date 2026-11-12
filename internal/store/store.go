// Package store owns the SQLite database: open, self-create schema on first
// run, the append-only event log, the watermark store and its contiguity rule
// (chunk-plan AR-5: C2 owns these; C3a consumes them and never redefines them),
// and projection registration/dumping.
//
// ALL database access goes through Store methods (C1 review M2): the *sql.DB
// is unexported, so the single-writer-connection decision stays a store-internal
// default instead of an implicit contract at every call site. Fold functions
// get a *sql.Tx scoped inside the apply transaction — that is the one
// sanctioned seam for other packages to touch tables.
//
// Driver mandate (design §6): modernc.org/sqlite ONLY. mattn/go-sqlite3 or any
// cgo driver is prohibited without re-review — cgo kills the CGO_ENABLED=0
// cross-compile that the whole one-file deployment story rests on.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"

	_ "modernc.org/sqlite"
)

// SchemaVersion is the in-season frozen schema version (design §8.5). It is
// the single source for the envelope's schema_version stamp (C1 review M1),
// and Open refuses a database stored under any other version.
//
// Bumped 1 → 2 at C2: the event log, watermark, trip and visit tables landed.
// Bumped 2 → 3 at C6: trip/visit grew their real fold shapes (trip: source
// key + verbatim vehicle_entered; visit: split point-in-time snapshot columns
// + is_open) — a projection SHAPE change, which must bump this version
// because Projection.Schema DDL rides outside this stored-version gate and
// IF NOT EXISTS cannot heal a changed shape (C2 review row 7).
// Bumped 3 → 4 at C7: the alert_acknowledged event and its alert_ack fold
// landed. A new REPLICATED event type changes what a fold over the log
// means — a v3 binary sharing a mesh with this one would silently compute a
// dump missing every acknowledgment, which is precisely the divergence the
// §8.5 freeze exists to refuse loudly. (C7's deadline_capture table is
// site-local/non-dumped and alone would NOT have forced a bump.)
// Bumped 4 → 5 at C8: the visit_checked_out event and its visit_checkout
// fold landed (design §8.1: "check-out... replicate as ordinary own-site log
// events" — a new REPLICATED event type, same reasoning as the 3→4 bump).
// Bumped 5 → 6 in the F25 fix: event_log grew a row_hash column (see
// insertAndFold in log.go), verified against every row's recomputed
// Envelope.Hash() by IntegrityCheck on every Open. F25 (reports/test-
// durability.md, reports/fix-integrity.md): a single-byte flip confined to
// event_log.payload's content — the exact shape a bit-rot fault produces —
// changes no length prefix and touches no indexed column, so PRAGMA
// integrity_check (verified, empirically: reports/fix-integrity.md) does
// NOT catch it; Open, RegisterProjection and DumpProjections all reported
// success over the corrupted row. row_hash is a second, physically separate
// copy of a digest over the row's own content, so a single localized fault
// can flip the content or the hash but not both consistently — this is what
// finally makes payload corruption structurally detectable, not just
// structural corruption. A projection-shape change would ride outside this
// gate (as documented above); a core event_log column addition does not,
// hence the bump.
// Bumped 6 → 7 for configurable destinations: trip grew a
// destination_uncontrolled column and the trip_dispatched payload the
// matching optional field (a run to a pad this company does not control —
// recorded, occupants checked out, but structurally outside boards, the
// deadline scan and correlation, exactly like the §10 degenerate trip). A
// projection SHAPE change (same reasoning as 2→3), and a v6 binary folding
// the new payload would silently drop where the truck went — the §8.5
// freeze refuses that loudly instead.
// Bumped 7 → 8 for weather phases (design/weather-phases-v1.md): (a) a new
// REPLICATED event type, weather_phase_declared, and its weather_phase
// registered projection — a v7 binary sharing a mesh would silently compute
// a dump missing every declaration (same reasoning as 3→4); (b) the
// trip_dispatched payload and trip table gained phase_at_dispatch and
// phase_override_reason (a projection SHAPE change, same reasoning as 2→3);
// (c) the alert_acknowledged payload and alert_ack table gained the optional
// review_phase scope (phase-review acks). C7's deadline_capture also gained
// x_receipt_ms, but that table is site-local/non-dumped and alone would not
// have forced a bump.
// Bumped 8 → 9 for the alert-honesty rulings (F38/F39, 2026-08-20): the
// alert_acknowledged payload and the replicated alert_ack table gained
// acked_at_board — the board-time of the declaration episode a phase-scoped
// ack answered, which is what lets an acked weather VERIFY row re-arm when
// a phase RETURNS after an intervening lower one (a projection SHAPE change
// plus a payload change, same reasoning as the 7→8 bump: a v8 binary would
// fold rows missing the episode and compute different suppression over the
// same log). The F39 deadline_red_fired table is site-local/non-dumped and
// alone would NOT have forced a bump.
// Bumped 9 → 10 in the schema-10 window (W1, items 6 + 13): the envelope —
// and therefore the §8.2 wire shape, changed here for the first time ever —
// gained the replicated time basis (AuthoredAt{wall_ms, max_error_us,
// state}) plus the OccurredAt/TimeSource provenance pair, all five values
// inside Envelope.Hash() and stored as five event_log columns with NO CHECK
// constraint (received bytes are stored exactly as sent; TP §4.1 rule 1).
// The site-local event_time index was DELETED in the same change — the time
// now travels with the record, so exactly one record of an instant exists
// and it survives a dead box (schema-10 §5B.7). A v9 binary folding v10
// envelopes (or vice versa) would disagree on every row_hash, which is
// precisely the divergence the §8.5 freeze refuses loudly. The v10 log
// starts empty (D-29): no migration exists or is wanted.
// NOT bumped for the W5 window's own sub-chunks, deliberately. W5 lands
// projection SHAPE changes — cargo_note on trip (item 3, W5e); name parts +
// position, phone and pad_oriented on person (W5a/W5b/W5c) — and every one of
// them RIDES the 9 → 10 bump above. The schema-10 window carries exactly ONE
// increment, paid by W1 at 49abf65.
// The store's shape rule (internal/domain/refdata.go:100–105, restated at
// :158–159 and :192–196: "IF NOT EXISTS cannot heal a changed shape") is a
// rule about crossing a DEPLOYMENT boundary, not about editing a file, and
// this window crosses none: the v10 log starts empty (D-29), §7 step 2 of
// design/WINDOW-BUILD-PLAN-v1.md DELETES every database at the cutover, and
// one hash-verified binary ships to both posts — so no stored database and no
// peer ever meets a differently-shaped binary inside the window. The plan's §8
// bump-gate table prices these very columns as "window-priced" (row 10), and
// W3 and W4 have already landed new REPLICATED event types here under this
// same one bump (rows 3 and 8). A within-window bump would also falsify
// design/WINDOW-CHECKLIST-v1.md:61 — the PRINTED operator checkbox worked at
// the cutover, which read "GET / shows schema_version 10 at both" (re-pinned
// to 11 with the 10 → 11 increment below).
// ⚠️ The residual footgun, accepted knowingly: a DEVELOPER holding a mid-window
// database across builds gets a clean Open and "no such column" at first use.
// Delete the file — the window does not bump between its sub-chunks.
// ⚠️ THE NEXT WINDOW MUST BUMP. Across a real deployment boundary a projection
// shape change is exactly as dangerous as the 7→8 and 8→9 paragraphs above say.
// Ruled by reports/W5-SCHEMAVERSION-ADJUDICATION.md (2026-09-01), which STRIKES
// the requirement — in W5-X5 as locked, and in post-lock amendment §1 which
// propagated it — that each W5 sub-chunk bump. §1's MEASUREMENTS were correct;
// its REMEDY was not.
// Bumped 10 → 11 (2026-09-05) — THE NEXT WINDOW, exactly once, for three
// rulings that all want the same increment and were batched into ONE so the
// camp never pays three both-posts-down windows ([[D-92]]'s orchestration
// rule; free until the first real arrival, [[D-55]]): (1) [[D-92]] — every
// person_amended carries `seen`, the keys its author had already seen
// standing, so composition is causal first and key-ordered only between
// genuinely concurrent edits ([[D-28]] Part B REVERSED — re-ruled, never
// silently edited); the `person_amendment` side table gains the `seen`
// column and the wire gains the `seen` key. (2) [[D-88]]/[[D-91]] —
// `pad_oriented` is STRUCK from person_created, person_amended, `person` and
// `person_amendment` (one flag on the ENROLLING site's row cannot hold a
// per-site orientation; D-88 reverses D-10/D-21.1); the per-site
// `orientation` projection, `orientation_completed`/`orientation_retracted`
// ([[D-95]]) and [[D-98]]'s `not_entered` land in LATER STAGES of this same
// increment — no redeploy happens between stages, because a boundary inside
// the window is exactly how the schema-10 registry skew (13 → 21 with zero
// bumps) accrued. (3) The cutover from the schema-10 demo pair. The v11 log
// starts empty, as the v10 log did ([[D-29]]): no migration exists or is
// wanted, and a schema-10 database or peer envelope is REFUSED (below, and
// log.go's ApplyBatch gate) rather than adopted.
// Databases stored under an older version refuse to open with an explicit
// error; delete the file — the schema self-creates on next start.
//
// Bumped 11 → 12 (2026-09-11, R-16 — James: "Batch them now while it's
// free") — TWO changes James picked from SCHEMA-INVENTORY.md, batched into
// ONE increment for the same reason 10→11 was: no live site exists yet, so
// today the cost is re-seeding two test environments; after the binaries
// move to the work PCs the identical change costs real gate records. (A
// third candidate, the unknown-vehicle schema path, is HELD — a working
// non-schema fix shipped instead and the premise James ruled on changed;
// see R-16's "ITEM 1 IS HELD".) (1) Entry-on-completion (d91-arrival-
// respec.md §3.4, D-91/D-98's arrival split, SCHEMA-11 step 7):
// `orientation_completed` grows by three `omitempty` keys —
// `arrival_ref`/`trip_ref`/`visit` — following [[D-92]]'s `seen` wire-bump
// precedent exactly. `orientation_completion` gains the matching
// `arrival_ref`/`trip_ref` columns (mirroring the payload, house style);
// the visit itself is not duplicated onto that row because it materialises
// as an ordinary row in `visit`, exactly as trip_arrival's own visits do.
// A completion found with a held-out `trip_arrival_not_entered` row at
// this site (and no visit already open here) carries the entry; the bulk
// backfill door (thirty people from last season, no held-out row) carries
// none and enters nobody — same fold, same discipline as trip_arrival's
// own visit-mint (D-91/D-98 §3.4). Retracting a completion that entered
// someone is refused while that visit stays open (§3.5): the completion
// and the visit it produced cannot be allowed to disagree about whether
// the person may be on site. (2) Import grandfathering marker (R-8-
// REVISED's ~1,927 orientation-less legacy visits, capacity only per
// James's own scoping — "build the FIELD, not the importer," the importer
// itself stays deferred): `visit` gains `predates_orientation_rule`, a
// nullable marker so a future import's historical rows can say "this
// predates the orientation rule" rather than reading as a live D-91/R-9
// violation. [[D-10]]'s value domain (only TRUE and ABSENT are ever
// authored, never FALSE) is reused here on purpose — a CHECK enforces it.
// No live authoring edge sets this today; `buildArrivalVisits` never
// writes it, so the column stays NULL on every visit this build can mint.
//
// Bumped 12 → 13 (2026-09-11, R-17 — James, shown that the shipped
// no-schema fix covered only some entry points: "there are no doors only
// vehicles"; on the recommendation to take the schema path: "ah well then
// go with your recomondation"). A CLEAN second bump, not a rewrite of the
// merged 12 bump above: the 11→12 text is left exactly as it reads (R-16
// held this item explicitly, "ITEM 1 IS HELD", and closed the book on that
// increment) — R-17 is its OWN ruling, reached after 12 had already merged,
// and belongs to its own paragraph the same way D-92's `seen` bump got its
// own paragraph rather than an edit to D-36's. A second bump costs nothing
// extra here: the wipe is paid at deploy, and neither test environment
// (laptop bench, demo pair) has redeployed since 12 landed — one deploy
// after this bump is still one wipe, R-16's whole argument.
//
// Unidentified vehicle: `trip` gains ONE new column,
// `vehicle_unidentified`, alongside the UNCHANGED `vehicle_id` (still `TEXT
// NOT NULL` — W7-12(b) untouched, see that test's own comment for why this
// was measured against it and found NOT to require weakening it). An
// earlier pass at this exact ruling made `vehicle_id` itself nullable
// (NULL = unidentified) and was abandoned after being measured: ~90 test
// files construct a `VehicleID:` literal, `TripDispatched`/`Trip` are both
// touched by nearly every fixture in the tree, and the distinction a
// nullable column buys is one an ADDITIVE flag buys for free, at zero
// fixture cost, with the LOCKED NOT-NULL guard left standing. `vehicle_id`
// stays `""` for an unidentified movement — already the value
// addValueKeys/vehicleNumberSnapshot treat as "no key, no record" (the
// existing branch a non-degenerate trip carrying neither correlation key
// falls into), which is exactly the correlation posture an unidentified
// movement needs: it cannot be known to be the same truck as any other
// trip's, so it must never share a correlation key with one. The pair is
// made STRUCTURALLY consistent, not merely conventionally so, by a CHECK
// constraint below rather than trusted to application code alone.
//
// Bumped 13 → 14 (2026-09-13, R-18 — James, shown that one movement never
// got R-17's treatment: "Fix it — same bump."). A CLEAN third bump, not a
// rewrite of the merged 13 bump above: R-17's own paragraph stands exactly
// as it reads, the same discipline the 12→13 bump gave the 11→12 text
// above it. R-17 was stated GENERALLY ("there are no doors only vehicles",
// "every movement … arrival and departure") but its build's coverage list
// was the four NAMED doors an assessment had measured — `dispatchTrip`'s
// two departure/local-check-in callers and the two gate HTTP surfaces —
// and `POST /trips/arrive-unsolicited` (an ARRIVAL a site records for a
// vehicle it was never told about, §9.2) was never on that list, so it was
// never put to him. R-18 records that the ruling always covered it and
// closes the gap in R-17's own increment family rather than opening a new
// ruling for what is the same fact about the same kind of thing.
//
// `trip_arrival` gains ONE new column, `vehicle_unidentified` (INTEGER
// 0/1), on the SAME representation R-17 chose for `trip`: additive, not a
// change to `vehicle_id`'s existing nullability. `trip_arrival.vehicle_id`
// is already `TEXT` (nullable) — unlike `trip.vehicle_id`'s locked
// `NOT NULL` — because an EXPECTED arrival (`trip_ref` set) never carries
// one at all (the vehicle is already known via the trip; RecordExpected-
// ArrivalExcluding's own TripArrival{} never sets VehicleID/VehicleEntered/
// OriginSite). That existing NULL is unrelated to this ruling and stays
// exactly what it always meant — "an arrival that names no vehicle because
// it did not need to." A derivable-at-read alternative for THIS case (a
// NULL/NULL/NULL signature on an unsolicited arrival, argued to be
// impossible today) was considered and rejected: it is exactly the
// convention-over-CHECK shape R-17 already rejected for `trip`'s own
// `vehicle_id`/`""` question, for the identical reason — a convention that
// "cannot occur today" is not a guarantee about tomorrow, a different
// build, or a replicated row from one.
//
// The CHECK constraint is ONE-DIRECTIONAL, exactly as `trip`'s is, and for
// the measured reason `trip`'s own comment gives: `trip_arrival.vehicle_id
// IS NULL` with the flag `0` is the ORDINARY, overwhelmingly common shape
// of every EXPECTED arrival ever authored — a symmetric CHECK forbidding
// that pairing would fail the fold on every one of them, the identical
// "the fold is a bare INSERT, it never validates against current state"
// hazard (F4 §3.2) that broke a first pass at `trip`'s own CHECK. What
// must be impossible, and is, is the dangerous direction: a real,
// populated `vehicle_id` simultaneously claimed unidentified.
//
// Bumped 14 → 15 (2026-09-13, R-20 + R-23 — James, shown two posts holding
// IDENTICAL raw logs and DIFFERENT musters: "Make the presence folds
// order-independent"; and, on the rejoin path accepting a peer's copy of
// our own history: "Restore them, but mark them as restored"). A CLEAN
// fourth bump, not a rewrite of the merged 14 bump above — the same
// discipline every bump since 11→12 has kept. One bump, one wipe: no live
// site exists, both test environments hold seeded data, and the cost is
// paid once at the next deploy (R-16).
//
// `visit` gains TWO columns, `node_epoch` and `seq` — the MINTING
// ENVELOPE's own key, exactly as `trip` and `trip_arrival` carry theirs
// (chunk-carry-forward row 10: a projection that must be ordered carries
// its source key). Both NOT NULL: every visit this binary mints is minted
// by exactly one envelope and there is no other way to get one.
//
// WHY (R-20, MEASURED through the real repl.Engine, D87-D92-COHERENCE.md):
// `visit.is_open` was the ONE derived value mutated in place by predicate
// UPDATEs inside the folds (dispatch closed the occupant's open visit,
// an arrival closed the prior one before opening the new, a check-out
// closed by id). Those UPDATEs are order-DEPENDENT, and their determinism
// premise — carry-forward ROW 28, "every visit row at a site is only ever
// mutated by that same site's own stream, which is contiguous" — is TRUE
// OF A STREAM AND FALSE OF A SITE: D-87's ownership unit is the site,
// D-92's ordering unit is the stream (site_id, node_epoch), and a wipe
// gives one site two streams with no order between them except what
// delivery supplies. A post re-imaged mid-shift whose boot Rejoin cannot
// reach its peer authors on epoch 2, then the §8.6 self-history retry
// lands its OWN epoch-1 tail afterwards: the epoch-1 dispatch closed the
// epoch-2 visit by predicate. Author: person OFF site. Peer and a from-
// zero replay: ON site. Same log, different muster, under-report
// direction, at the authoritative post. Row 28 is FALSIFIED for the
// SECOND time (F8 falsified it for peers; the fix constrained DELIVERY —
// epoch-major — and re-marked the premise "true by construction"; a
// premise repaired by constraining delivery is guarded only by whoever
// remembers the constraint, and the self-history retry did not).
//
// THE FIX IS THE HOUSE SHAPE, raw + derived (orientation.go's header; the
// same audit measured orientation order-independent across epochs):
// every fold on `visit` is now a bare INSERT (or, for check-out, a bare
// INSERT into `visit_checkout`), and `is_open` is RE-DERIVED per
// (site_id, person_id) in the visit projection's Finalize as a pure set-
// function of the raw tables, ordered by the sanctioned key
// (domain/visit_open.go states the one rule). The raw columns are made
// immutable BY TRIGGER below; `is_open` is the single column any UPDATE
// may touch, so "derived, never predicate-folded" is structural rather
// than a comment somebody has to remember.
//
// R-23 rides the same key: a row under THIS post's own site tag whose
// node_epoch is BELOW the running config epoch cannot have been observed
// by this incarnation of the post — the only door for such a row is the
// §8.4 self-history exception (repl/engine.go), i.e. the peer's copy of
// our history. The muster and the gate roster mark those rows RESTORED
// (domain.Visit.RestoredFromPeer); nothing new replicates, and recovery
// still restores a whole shift. Second-hand presence is never presented
// as first-hand — the muster never claims more than it knows.
//
// Bumped 15 → 16 (2026-10-04, fix round 2, ruling R3 — James: "an ack on an
// amber VERIFY row should not silence the other end's red overdue; ack per
// alert kind"). The alert_acknowledged payload gains the optional
// `alert_kind` ("overdue_live" | "overdue_unresolved") and the replicated
// alert_ack table gains the nullable `alert_kind` column (deadline/ack.go).
// The kind is the row the officer pressed on at THEIR post, which depends
// on that post's link state at the press and so cannot be re-derived from
// the event set — it has to be carried. A binary without the column would
// fold the same log to a different alert_ack dump, which is exactly what
// the §8.5 freeze refuses; hence a bump, and the house rule: one bump, one
// wipe. The sole bump of fix round 2 (team-lead, 2026-10-04); the lanes are
// reseeded after merge and the demo meets it only at the gated fork→demo
// merge.
// NOT bumped for the removal of weather phases (James, 2026-10-04: "the phase
// conditions can be removed they will not effect site control"), which rides
// the same unreleased v16: v16 has never been deployed and the fork→demo
// deploy wipes. What a v16 log already holding phase events does on read —
// SKIPPED, never refused, so no stored log can stop a post opening:
// `weather_phase_declared` has no fold (inert on replay and ingest) and is
// absent from the G13 closed set (refused at authoring); an
// `alert_acknowledged` carrying `review_phase` is skipped by the ack fold, so
// it can never be read as a plain ack and silence a red; `phase_at_dispatch`
// and `phase_override_reason` on a trip payload are ignored keys. The
// weather_phase table and the trip/trip_amendment/alert_ack phase columns
// are gone from the DDL; a developer database holding the old shape still
// opens (CREATE TABLE IF NOT EXISTS keeps it; every INSERT names its
// columns), and the old columns simply stay NULL.
const SchemaVersion = 16

// The full self-created schema. The event log is append-only BY TRIGGER, not
// just by policy — UPDATE and DELETE abort at the SQL layer (§8.1: no
// updates-in-place, no deletes, no tombstones).
//
// trip and visit exist from this first schema because their nullable
// recorded_by columns are unbackfillable later (design §10); C6 extended
// their shapes to the real fold shapes (SchemaVersion 2 → 3) and their
// folds live in internal/domain (trip.go); recorded_by population is
// asserted at C11. vehicle_entered is the VERBATIM entered text — the
// normalised compare form (domain.NormalizeVehicleID) is deliberately
// never stored anywhere (chunk-plan C6 M8). trip.node_epoch/seq is the
// event's source key: "oldest open trip" orders by it, never rowid or
// wall-time (C2 review row 10). C6's other trip-family tables
// (trip_occupant/trip_arrival/trip_cancel/trip_state/arrival_state) ride
// domain Projection.Schema, not this const.
const schema = `
CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS event_log (
    site_id        TEXT    NOT NULL,
    node_epoch     INTEGER NOT NULL CHECK (node_epoch >= 1),
    seq            INTEGER NOT NULL CHECK (seq >= 1),
    schema_version INTEGER NOT NULL,
    origin_auth    TEXT    NOT NULL,
    -- The replicated time basis (SchemaVersion 10, schema-10 §5B / TP §4.1):
    -- the AuthoredAt triple plus the OccurredAt/TimeSource pair. All five
    -- values sit inside Envelope.Hash() and therefore inside row_hash, so
    -- they carry NO CHECK constraint — an inbound envelope's bytes must be
    -- stored exactly as received or IntegrityCheck fires on untampered
    -- records; unknown values fail closed at READ, never at write. They
    -- order nothing: no index covers them, no projection column copies
    -- them, and no fold/watermark/comparator may consult them (§5B.5).
    authored_at_wall_ms      INTEGER NOT NULL,
    authored_at_max_error_us INTEGER NOT NULL,
    authored_at_state        TEXT    NOT NULL,
    occurred_at              INTEGER NOT NULL,
    time_source              TEXT    NOT NULL,
    payload        TEXT    NOT NULL,
    -- row_hash: Envelope.Hash() at the moment this row was written (F25 fix,
    -- SchemaVersion 6). A physically separate copy of a digest over the same
    -- row, so a corruption confined to payload (or any other column here)
    -- disagrees with it; verified by IntegrityCheck on every Open. See the
    -- SchemaVersion doc comment above for why this needed a version bump.
    row_hash       TEXT    NOT NULL,
    PRIMARY KEY (site_id, node_epoch, seq)
) STRICT;

CREATE TRIGGER IF NOT EXISTS event_log_append_only_update
BEFORE UPDATE ON event_log
BEGIN SELECT RAISE(ABORT, 'event_log is append-only'); END;

CREATE TRIGGER IF NOT EXISTS event_log_append_only_delete
BEFORE DELETE ON event_log
BEGIN SELECT RAISE(ABORT, 'event_log is append-only'); END;

CREATE TABLE IF NOT EXISTS watermark (
    site_id     TEXT    NOT NULL,
    node_epoch  INTEGER NOT NULL,
    applied_seq INTEGER NOT NULL,
    PRIMARY KEY (site_id, node_epoch)
) STRICT;

CREATE TABLE IF NOT EXISTS trip (
    trip_id          TEXT PRIMARY KEY,
    origin_site      TEXT    NOT NULL,
    node_epoch       INTEGER NOT NULL,
    seq              INTEGER NOT NULL,
    -- ⟦W7, item 9 + item 8⟧ THE TWO VEHICLE COLUMNS, and they are DIFFERENT
    -- FACTS with opposite nullability — which is the pair that must be read
    -- together, because a build that relaxes both reads as "item 8 made the
    -- VEHICLE optional" when what item 8 makes optional is the NUMBER.
    --
    -- vehicle_id: which vehicle RECORD this run named. NOT NULL —
    -- PLAN-SEC-A-schema.md:723, "every dispatch names an entity". There is
    -- deliberately NO foreign key to the vehicle table (analytics-v1.md:932-933:
    -- there are none anywhere in this schema and none is added): a post may
    -- legitimately hold a trip whose vehicle record has not reached it yet,
    -- and the fold must land it rather than refuse it (W7-6).
    --
    -- vehicle_entered: RELAXED from NOT NULL to nullable by item 8. It is no
    -- longer the identity of the truck — it is the frozen SNAPSHOT of the
    -- number as it stood at dispatch, the role visit.person_snapshot_name
    -- plays for people in item 5, and it is honestly NULL when the vehicle
    -- had no number. Without the snapshot, amending a vehicle's number would
    -- silently rewrite what every past trip appears to have been.
    --
    -- ⚠ SQLite has no ALTER COLUMN, so this relaxation cannot be healed on a
    -- stored database by IF NOT EXISTS the way a NEW table can. That is why
    -- W7 carries its own '9'-edge refusal instance (W7-X5) rather than
    -- closing the row by ruling: SchemaVersion stayed 10 through that window
    -- (11 since the D-92 increment) and a stale store is refused at Open,
    -- loudly, instead of failing later at INSERT.
    vehicle_id       TEXT    NOT NULL,
    -- vehicle_unidentified (SchemaVersion 13, R-17 — James: "there are no
    -- doors only vehicles"): the explicit, decided fact that no vehicle
    -- could be named for this movement at all — a fact about the MOVEMENT,
    -- distinct from vehicle_entered IS NULL (a KNOWN, role-only vehicle
    -- record with no number: vehicle_id is still real and non-empty there).
    -- Decided ONCE, at the shared authoring core (dispatchTrip, tripauthor.go)
    -- both the departure door and the check-in door's own degenerate-trip
    -- entry point call through — never a per-caller convention. (Naming the
    -- check-in door's entry point literally here trips
    -- TestOnlyTheCheckInPathAuthorsALocalCheckInDispatch's raw source scan,
    -- which is right to fire on a real CALLER outside tripauthor.go/
    -- checkin.go and is deliberately not made comment-aware — see that
    -- test's own file for the false-positive class this avoids instead.)
    --
    -- ⚠ THE CHECK BELOW IS THE WHOLE POINT OF ADDING A SEPARATE COLUMN
    -- RATHER THAN TRUSTING vehicle_id="" ALONE. A flag that CAN disagree
    -- with the field it describes (a populated vehicle_id claimed
    -- UNIDENTIFIED at the same time) is worse than the nullable vehicle_id
    -- an earlier pass at this ruling rejected — at least a nullable column
    -- cannot ALSO hold a value. Application code (dispatchTrip) already
    -- refuses that contradiction before ever authoring, but "refused by the
    -- caller" and "impossible in storage" are different guarantees, and a
    -- replicated row from a future, different build should not be able to
    -- defeat the first without also defeating the second.
    --
    -- ⚠ ONE-DIRECTIONAL, DELIBERATELY — MEASURED, not the symmetric pair a
    -- first pass at this CHECK wrote. A symmetric CHECK (also forbidding
    -- vehicle_unidentified=0 with vehicle_id='') broke the fold on real,
    -- pre-existing tests the moment this column existed:
    -- TestWitnessY2LegacyTripWithNoVehicleIDFallsBackToTheEnteredSnapshot and
    -- two W9 soup tests author a raw trip_dispatched with an empty
    -- vehicle_id directly (bypassing dispatchTrip entirely) to test FOLD
    -- robustness against historical/malformed data that predates this
    -- ruling — and applyTripDispatched's own file header states the
    -- invariant a symmetric CHECK would have violated: "the fold is a bare
    -- INSERT, it never validates against current state" (F4 §3.2), because
    -- by fold time the event is ALREADY durably logged and hashed —
    -- refusing to fold it (which is what a failing CHECK does, identically
    -- to a Go-level refusal) is not available as an option, it corrupts the
    -- projection for an event that already happened. An empty vehicle_id
    -- with the flag false is the OLD (pre-R-17) state this column adds
    -- nothing dangerous to: it renders exactly as it always did, and
    -- dispatchTrip's own refusal already makes it unreachable through any
    -- SANCTIONED door (see its "vehicle id is required" branch) — the
    -- residual is historical/synthetic data the fold must keep tolerating
    -- regardless of this column. What must be impossible, and IS made
    -- impossible here, is the dangerous direction: a real, populated
    -- vehicle_id simultaneously claimed unidentified.
    vehicle_unidentified INTEGER NOT NULL DEFAULT 0
        CHECK (NOT (vehicle_unidentified = 1 AND vehicle_id <> '')),
    vehicle_entered  TEXT,
    driver_person_id TEXT    NOT NULL,
    destination_site TEXT,
    -- Uncontrolled destination (SchemaVersion 7): a configured pad with no
    -- gate — recorded for "who is out there", never an expectation. Mutually
    -- exclusive with destination_site; every structural exclusion keyed on
    -- destination_site IS NULL (degenerate trips, §10) covers these rows.
    destination_uncontrolled TEXT,
    deadline_minutes INTEGER,
    -- ⟦was: phase_at_dispatch INTEGER, phase_override_reason TEXT (weather
    -- phases, SchemaVersion 8) — both struck with weather phases, James
    -- 2026-10-04, inside the unreleased v16 (see SchemaVersion's note).⟧
    -- Cargo note (item 3, W5e): SchemaVersion stayed 10 — this column rode
    -- W1's 9 → 10 bump (W5-SCHEMAVERSION-ADJUDICATION.md); 11 since the D-92
    -- increment. What the officer chose to write
    -- about what is on the truck, stored VERBATIM — no trim, no case change,
    -- no truncation, exactly as vehicle_entered is. NULL means the officer
    -- wrote nothing, which is all it means: absent is not a "not hauling"
    -- flag and no code path may read what the note says (D-21.3 declined a
    -- manifest system). Departures record it; unsolicited arrivals do not,
    -- so there is deliberately no counterpart column on trip_arrival.
    cargo_note       TEXT,
    recorded_by      TEXT
) STRICT;

CREATE TABLE IF NOT EXISTS visit (
    visit_id                TEXT PRIMARY KEY,
    site_id                 TEXT    NOT NULL,
    person_id               TEXT    NOT NULL,
    trip_id                 TEXT,
    company_snapshot_id     TEXT,
    company_snapshot_name   TEXT,
    -- W8 (item 10): the trade snapshot. RENAMED with the entity itself
    -- (D-26 part 2: "Not a re-purpose") — the columns record WHICH trade was
    -- used at check-in and stay nullable for the reason their company
    -- siblings are.
    trade_snapshot_id       TEXT,
    trade_snapshot_name     TEXT,
    -- W6 (item 5): the person's display name as the roster composed it AT
    -- CHECK-IN, frozen onto the visit so a historical sheet stays true when
    -- that name later changes. Third instance of the snapshot pattern on
    -- this table, and NULLABLE for the same reason its two siblings are:
    -- the snapshot is ABSENT for a person the roster cannot name (a peer's
    -- person_created with an empty name folds unchecked, and that person
    -- still checks in). No DEFAULT: a default '' would make the
    -- empty-is-ABSENT render rule the resting state of every row a broken
    -- populate wrote. ⚠ Name parts are deliberately NOT here — D-60 freezes
    -- the display name and nothing else.
    person_snapshot_name    TEXT,
    -- node_epoch / seq (SchemaVersion 15, R-20): the key of the ENVELOPE
    -- that minted this visit — a trip_arrival, or an orientation_completed
    -- carrying an entry (mintVisitFromArrivalVisit, trip.go, the ONE mint).
    -- Never a payload field: the key is the envelope's, and the site is
    -- site_id above, so a row structurally cannot claim another stream's
    -- position. It is what makes is_open DERIVABLE: "the open visit is the
    -- GREATEST-keyed visit of (site, person), unless a dispatch at that
    -- site with a greater key or a check-out for it exists" is a pure
    -- function of table contents under any delivery order, including a
    -- site's own earlier epoch landing after its later one (R-20).
    -- It is also what R-23 reads: node_epoch below the running config
    -- epoch, under our own site tag, means RESTORED FROM THE PEER.
    node_epoch              INTEGER NOT NULL,
    seq                     INTEGER NOT NULL,
    -- is_open is the ONE DERIVED column on this table (SchemaVersion 15,
    -- R-20). It is written by domain/visit_open.go's Finalize and by
    -- nothing else; the trigger below makes every OTHER column immutable
    -- after INSERT, so a fold cannot regress to mutating presence by
    -- predicate. Its INSERT value is 1 on purpose: if a derivation were
    -- ever skipped, a missed close OVER-reports (a name left on the
    -- muster is the safe error) where a missed open would hide a live
    -- person.
    is_open                 INTEGER NOT NULL CHECK (is_open IN (0, 1)),
    recorded_by             TEXT,
    -- Import grandfathering marker (SchemaVersion 12, R-8-REVISED/R-16):
    -- CAPACITY ONLY — the importer itself is deferred (James, 2026-09-09).
    -- [[D-10]]'s value domain, reused deliberately: only TRUE (1) or ABSENT
    -- (NULL) is ever authored, never FALSE — the CHECK enforces it, the way
    -- the derived orientation table's own value domain does. A future
    -- import's historical visit rows set this to say "this predates the
    -- orientation rule; its lack of an orientation record is not a live
    -- D-91/R-9 violation." NO code path in this build ever writes 1 —
    -- buildArrivalVisits never sets ArrivalVisit.PredatesOrientationRule,
    -- so every visit this binary mints carries NULL here, exactly as
    -- before this column existed.
    predates_orientation_rule INTEGER CHECK (predates_orientation_rule IS NULL OR predates_orientation_rule = 1)
) STRICT;

-- F30 fix: idx_visit_site_open covers the hottest query EXPLAIN QUERY PLAN
-- showed as a full scan of the largest table: the per-occupant fold
-- checkout/close ("UPDATE visit ... WHERE site_id=? AND person_id=? AND
-- is_open=1", run on every trip event). It equally serves the muster's
-- open-visits read ("WHERE site_id=? AND is_open=?", prefix match), which
-- bounds that page by on-site headcount instead of season row count.
-- Indexes are not part of the dumped/replicated shape (DumpProjections reads
-- rows, never plans), so no SchemaVersion bump: IF NOT EXISTS applies it to
-- an existing database on next Open.
--
-- Deliberately the ONLY index added (reports/fix-perf.md): an index on
-- trip(node_epoch, seq, trip_id) for the recompute's ORDER BY was tried,
-- changed the plan (temp-sort removed), and measured neutral-to-slightly-
-- NEGATIVE on the apply path at n≤6.5k pairs — index-walk row lookups cost
-- what the sort saved. An index that buys no measured time only taxes write
-- throughput on the already-contended apply path (F29), so it was removed.
-- The same measurement rejected any index on trip_state: that table is
-- wholesale-rewritten by the recompute on every trip event, so index
-- maintenance there taxes the deliberate fold directly.
CREATE INDEX IF NOT EXISTS idx_visit_site_open ON visit (site_id, is_open, person_id);

-- SchemaVersion 15 (R-20): the derivation's own read — every visit of one
-- (site, person), greatest key first (domain/visit_open.go). Without it the
-- per-batch re-derive scans the site's whole visit history per touched
-- person, and a season replay goes quadratic through this table the way
-- F28 found trip_state's did. Indexes are not dumped/replicated shape.
CREATE INDEX IF NOT EXISTS idx_visit_site_person_key ON visit (site_id, person_id, node_epoch, seq);

-- FIX17 (perf, 2026-10-06): the per-trip visit read (domain.VisitsByTrip,
-- "WHERE trip_id=?"). The muster's not-accounted-for section calls it once
-- per arrived trip, and a season holds a trip per gate check-in, so without
-- it the muster scanned the whole visit table ~14k times per page (8.7 s at
-- season scale). Same footing as the two above: not dumped/replicated shape,
-- no SchemaVersion bump, IF NOT EXISTS applies it on the next Open.
CREATE INDEX IF NOT EXISTS idx_visit_trip ON visit (trip_id);
-- …and the per-arrival visit read (the visits one arrival minted, "WHERE
-- site_id=? AND node_epoch=? AND seq=?": domain.aboardOnTripArrivals, once
-- per arrived run on the same section). idx_visit_site_person_key above
-- only narrows it to the site, so each call scanned the site's season.
CREATE INDEX IF NOT EXISTS idx_visit_key ON visit (site_id, node_epoch, seq);
-- …and every visit of one person at any site ("WHERE person_id=?":
-- domain.sightedSince, once per not-aboard candidate on that section and on
-- the in-transit and held-out reads), which no index above can serve.
CREATE INDEX IF NOT EXISTS idx_visit_person ON visit (person_id);

-- SchemaVersion 15 (R-20): visit is RAW + one derived column, BY TRIGGER.
-- Every column except is_open is immutable once inserted, and no row is
-- ever deleted — the same append-only-by-trigger stance event_log takes,
-- narrowed to leave the single derived column writable. A fold that tried
-- to bring back a predicate UPDATE on anything else aborts at the SQL
-- layer; the W6 "no UPDATE visit ever rewrites a snapshot" property stops
-- being a census of statements and becomes a constraint.
CREATE TRIGGER IF NOT EXISTS visit_raw_immutable
BEFORE UPDATE OF visit_id, site_id, person_id, trip_id,
                 company_snapshot_id, company_snapshot_name,
                 trade_snapshot_id, trade_snapshot_name,
                 person_snapshot_name, node_epoch, seq,
                 recorded_by, predates_orientation_rule ON visit
BEGIN
    SELECT RAISE(ABORT, 'visit: raw columns are immutable; only is_open is derived (SchemaVersion 15, R-20)');
END;
CREATE TRIGGER IF NOT EXISTS visit_no_delete
BEFORE DELETE ON visit
BEGIN
    SELECT RAISE(ABORT, 'visit: rows are never deleted (SchemaVersion 15, R-20)');
END;
`

type Store struct {
	db   *sql.DB
	Path string

	// SiteNamer, when set, renders a site id as the officer reads it. The
	// domain's refusal sentences name sites ("on site at ..."), and domain
	// has no config; the web layer installs this so those sentences use
	// display names. Nil means the plain SiteLabel fallback. It is a
	// per-Store field, not a package global, so two servers in one process
	// (tests) never see each other's names. Display only.
	SiteNamer func(id string) string

	// tx, when non-nil, makes this Store a Transact VIEW (transact.go): every
	// Author, LocalTx and projection read runs on this one transaction
	// instead of the pool, and the outermost Transact commits or discards
	// them together. Nil on every Store Open returns — the ordinary,
	// one-event-per-commit behaviour is unchanged.
	tx *sql.Tx

	// holder records which goroutine currently holds the single connection
	// through a tracked acquisition (reentrancy_guard.go) — the fact the
	// guard's timeout branch needs to tell reentrancy from contention. A
	// pointer, shared unchanged by Transact's shallow view copy, so a view
	// and the pool store judge the same record. Nil on a Store built
	// without Open (tests): every acquisition then goes untracked and the
	// guard reads a timeout as contention.
	holder *connHolder

	// Clock is the audit clock used to stamp this site's OWN authored events
	// with their Envelope time basis (AuthoredAt + rule W's OccurredAt copy),
	// read exactly once per Author call. Defaulted to SystemClock by Open;
	// injectable so
	// tests can present a fixed instant or a deliberately hostile one (a box
	// whose clock has never been disciplined is otherwise untestable without
	// a real dead RTC). Nil is treated as SystemClock, so a Store built
	// without going through Open still stamps.
	//
	// This is NOT the deadline clock and must never be used as one: §9.3
	// arithmetic runs on the monotonic clock, and this reading is only ever
	// compared against readings from the same box. See clock.go.
	Clock Clock

	// projections, sorted by Name: folds run and dumps emit in this one
	// deterministic order. Registration happens at startup, before any
	// Apply/Author traffic; it is not safe for concurrent use.
	projections []Projection

	// owners is the D-87 choke point's registry (ownership.go): event type →
	// the resolver that names the row a payload of that type MODIFIES and
	// the tag on it. Consulted by Author only — never by Apply, never by a
	// fold (W3-2). Registered at startup alongside the projections.
	owners map[string]OwnerResolver

	// acknowledged is G13's other half of the closed set (ownership.go): an
	// event type classified elsewhere as NOT gated — shared reference data,
	// a type that mints its own row, or a type ruled open and not yet
	// gated — with nothing for a resolver to check. Registered at startup
	// alongside owners. A type in NEITHER map, once anything has been
	// registered at all, is refused (G13: fail closed on the unclassified,
	// not just the foreign).
	acknowledged map[string]bool

	// OnAuthored, if set, runs ONCE after every COMMITTED transaction that
	// appended at least one event to this post's own stream through Author:
	// once per standalone Author (its own transaction, log.go), once per
	// Transact commit however many events fn authored inside it
	// (transact.go) — never for a rolled-back Transact, never for peer
	// events delivered through ApplyBatch, never for LocalTx. It runs on the
	// authoring goroutine AFTER the commit has returned and the connection
	// has been released, so the write is already durable and the hook can
	// neither fail it nor deadlock on the store; it must not block.
	//
	// This is the §8.9 poke fast path's one seam (W7 / Y-17, 2026-10-03):
	// every door authors through this package — alone or inside a Transact
	// — and nothing in the product ever called repl.Engine.Author, so the
	// documented "poke peers after each write" was dead code and replication
	// ran on the 10 s tick alone (3-17 s to the first peer, sc-test L12).
	// cmd/sitectl/main.go sets it to the engine's PokePeers. Nil (the
	// default, and every test that does not set it) means no hook.
	OnAuthored func()

	// authored is set on a Transact VIEW by Author (the s.tx != nil arm) and
	// read by Transact after fn returns, so the outermost commit knows
	// whether to fire OnAuthored. Never set on the pool Store itself: the
	// standalone Author arm fires the hook directly after its own commit.
	authored bool

	// memo is the derived-read reuse table (memo.go), shared unchanged by
	// Transact's shallow view copy — though a view never consults it. Nil on
	// a Store built without Open: Memo then builds every time.
	memo *memoTable

	// reads counts the statements that take the store's one connection on a
	// read path (Reads). Shared by Transact views; nil on a Store built
	// without Open.
	reads *atomic.Int64

	// pool is the read connection pool (readpool.go, FIX17c): reads outside
	// a write run there and never wait for the writer. Nil on a Store built
	// without Open and for an in-memory database — every read then takes
	// the writer, as before.
	pool *readPool

	// latest is set on the copy Latest returns: reads ignore the calling
	// goroutine's bound view.
	latest bool

	// BeforeCommit, if set, runs inside every writer commit with the
	// transaction's writes complete and not yet visible to any reader —
	// where a commit's fsync stall sits. A TEST SEAM ONLY (the FIX17c stall
	// tests hold a commit there): nil everywhere in production.
	BeforeCommit func()
}

// fireOnAuthored runs the OnAuthored hook if one is set. Callers arrange,
// via a defer registered BEFORE begin(), that it runs after release() — the
// connection must be free before anything downstream of a commit runs.
func (s *Store) fireOnAuthored() {
	if s.OnAuthored != nil {
		s.OnAuthored()
	}
}

// readDSN is the read pool's connection string (readpool.go): the same file,
// query_only so a pool connection can never write, the same busy_timeout for
// the brief lock a WAL reader can meet while a checkpoint resets the log,
// and a 256 KiB page cache — the OS page cache already holds the file, and
// MEASURED on the seeded season (npf, 6 clients x 288 page loads) the
// default 2 MB per connection left a post settling at 145 MB RSS against
// 116 MB before FIX17c; at 256 KiB it settles at 117 MB, pages no slower.
func readDSN(path string) string {
	return fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=query_only(1)&_pragma=cache_size(-256)", filepath.ToSlash(path))
}

// Open opens (creating if absent) the SQLite database at path and ensures the
// schema exists. The file self-creates on first run — this is the §6 measured
// deployment claim: one binary plus one config, nothing else.
func Open(path string) (*Store, error) {
	// modernc.org/sqlite registers as driver name "sqlite".
	// _pragma DSN options are driver-specific to modernc; WAL + busy_timeout
	// are the sane defaults for a single-process web app writer.
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", filepath.ToSlash(path))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	// One writer connection: SQLite serialises writers anyway; >1 conn under
	// WAL invites SQLITE_BUSY instead of queueing. Correct for this load
	// (~3 events/min peak). Reads have their own pool beside it since FIX17c
	// (readpool.go) — the store-internal change this comment always said it
	// would be, which is exactly why db is unexported.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: create schema in %s: %w", path, err)
	}
	// Intra-node analogue of the §8.5 inter-node schema refusal: a binary must
	// never proceed over a database of another schema version. Refusal is the
	// whole behaviour — no migration here; schema is frozen in-season anyway.
	//
	// F26: read the marker BEFORE seeding it, so the seed (which only ever
	// runs "ON CONFLICT DO NOTHING") cannot paper over a marker that is
	// missing from a database that is not actually fresh. A genuinely new
	// file has no marker AND no rows in any of its own tables — that combination
	// is seeded as current, same as before. A file with a missing marker but
	// existing event/watermark/trip/visit rows did not get that way through
	// this binary (Open always seeds the row before any write is possible):
	// it predates the versioning scheme, or the row was removed by external
	// surgery (a botched manual recovery, a `.recover` pass). Either way it is
	// exactly the "silently trust a stale/incompatible shape" case §8.5's
	// refusal exists to prevent, so it is refused loudly rather than adopted
	// silently as version-current.
	var stored string
	err = db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&stored)
	switch {
	case err == nil:
		if stored != fmt.Sprint(SchemaVersion) {
			db.Close()
			return nil, fmt.Errorf("store: %s has schema_version %s but this binary requires %d — refusing to open", path, stored, SchemaVersion)
		}
	case errors.Is(err, sql.ErrNoRows):
		preexisting, dataErr := hasAnyData(db)
		if dataErr != nil {
			db.Close()
			return nil, fmt.Errorf("store: checking %s for pre-existing data: %w", path, dataErr)
		}
		if preexisting {
			db.Close()
			return nil, fmt.Errorf("store: %s has data but no schema_version marker — this database predates versioning or has been tampered with, not merely a different version; refusing to open", path)
		}
		if _, err := db.Exec(
			`INSERT INTO meta (key, value) VALUES ('schema_version', ?)`, fmt.Sprint(SchemaVersion)); err != nil {
			db.Close()
			return nil, fmt.Errorf("store: seed schema_version: %w", err)
		}
	default:
		db.Close()
		return nil, fmt.Errorf("store: read schema_version from %s: %w", path, err)
	}

	// SchemaVersion 10 note: the site-local event_time index that used to be
	// created here — OUTSIDE the version gate, as its own Exec — is GONE:
	// table, append-only triggers, writer, and this very exemption (item 6).
	// The authoring time now rides the Envelope itself, inside the hash and
	// inside the version-gated `schema` const above, so exactly one record
	// of an instant exists and it replicates with the event.

	pool, err := openReadPool(path, db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("store: open read pool on %s: %w", path, err)
	}
	s := &Store{db: db, Path: path, Clock: SystemClock, holder: &connHolder{}, memo: newMemoTable(), reads: new(atomic.Int64), pool: pool}

	// F25: a corrupted mid-file page or a corrupted content-only column
	// (e.g. event_log.payload) is otherwise openable and silently wrong —
	// nothing else in Open reads every page or re-verifies every row's
	// content. Measured at real season-scale (a 126MB synthetic database,
	// above the ~54-115MB projected range in reports/test-scale.md): the
	// combined IntegrityCheck (PRAGMA integrity_check + the row_hash pass)
	// cost 372ms (78ms + 236ms; see reports/fix-integrity.md). That is
	// trivial next to "opened once at the start of a shift", so it runs
	// unconditionally here rather than living behind a flag or a separate
	// maintenance command — a corrupt store must never open silently and
	// serve wrong rows, and Open is the one place every caller already
	// passes through.
	if err := s.IntegrityCheck(); err != nil {
		pool.close()
		db.Close()
		return nil, fmt.Errorf("store: %s failed integrity check on open: %w", path, err)
	}

	// Projection registration lives at the composition root
	// (domain.Register, called from cmd/sitectl/main.go), NOT here - C2
	// review L1 / chunk-carry-forward row 6/8: RegisterProjection rejects
	// duplicate names, so a real C6 trip/visit fold could never register
	// alongside a built-in dump-only stub of the same name; and store cannot
	// import internal/domain to register the real thing itself (domain
	// imports store for Envelope/Projection, so the reverse import would
	// cycle). Open therefore registers NOTHING. Callers must call
	// domain.Register(st) (or their own RegisterProjection calls, e.g. in
	// tests) before any Author/Apply traffic.
	return s, nil
}

// hasAnyData reports whether any of store's own core tables already hold at
// least one row. Used only by Open's F26 marker-seed path (above) to tell a
// genuinely fresh, empty database (no schema_version row yet — seed it) from
// an existing database whose schema_version row is simply missing (refuse:
// see Open). event_log is the authoritative record of every event ever
// authored; watermark, trip and visit are populated the same transaction as
// event_log for any real activity, so any one of the four having a row means
// this file has been used before.
func hasAnyData(db *sql.DB) (bool, error) {
	for _, table := range []string{"event_log", "watermark", "trip", "visit"} {
		var exists int
		// table is always one of the four fixed names above, never external
		// input, so this Sprintf is safe (same pattern as store's other
		// fixed-identifier queries in projection.go).
		if err := db.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s LIMIT 1)`, table)).Scan(&exists); err != nil {
			return false, fmt.Errorf("check %s: %w", table, err)
		}
		if exists == 1 {
			return true, nil
		}
	}
	return false, nil
}

// IntegrityCheck runs two checks over the whole database and returns nil
// only if both pass:
//
//  1. PRAGMA integrity_check — SQLite's own full page/btree/index scan.
//     Catches header corruption, page-linkage corruption, and corruption of
//     any INDEXED column (a TEXT PRIMARY KEY included — it rides an
//     implicit unique index).
//  2. A row_hash re-verification pass over every event_log row (below).
//     event_log.payload — the actual JSON event content, the thing a
//     corrupted muster would actually be built from — is NOT part of any
//     index, so (1) alone provably does not see corruption confined to it:
//     verified directly by flipping one byte inside a stored payload's TEXT
//     content (outside any length prefix) and confirming PRAGMA
//     integrity_check alone reports "ok" while Open goes on to serve the
//     wrong bytes back (reports/fix-integrity.md). (2) is what actually
//     closes that gap: row_hash is Envelope.Hash() computed once at write
//     time (log.go insertAndFold) and stored as a second, physically
//     separate copy of a digest over the row — a single localized fault can
//     flip the content or the hash, not both consistently.
//
// Both run on every Open (see Open's doc comment for the cost measurement
// this is based on: 372ms combined — 78ms PRAGMA + 236ms row_hash pass — on
// a 126MB synthetic database, above the projected season-scale range).
func (s *Store) IntegrityCheck() error {
	if err := s.pragmaIntegrityCheck(); err != nil {
		return err
	}
	return s.verifyRowHashes()
}

func (s *Store) pragmaIntegrityCheck() error {
	rows, err := s.db.Query(`PRAGMA integrity_check`)
	if err != nil {
		return fmt.Errorf("store: integrity check: %w", err)
	}
	defer rows.Close()
	var problems []string
	for rows.Next() {
		var msg string
		if err := rows.Scan(&msg); err != nil {
			return fmt.Errorf("store: integrity check scan: %w", err)
		}
		if msg != "ok" {
			problems = append(problems, msg)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: integrity check: %w", err)
	}
	if len(problems) > 0 {
		return fmt.Errorf("store: integrity check found %d problem(s), e.g. %q", len(problems), problems[0])
	}
	return nil
}

// verifyRowHashes re-derives Envelope.Hash() for every event_log row from
// that row's own stored columns and compares it against row_hash, the
// digest stored alongside it at write time. See IntegrityCheck's doc
// comment for why this runs in addition to PRAGMA integrity_check rather
// than instead of it. Refuses loudly on the FIRST mismatch, naming the
// exact (site, epoch, seq) key — same style as GapError/SchemaVersionError.
func (s *Store) verifyRowHashes() error {
	rows, err := s.db.Query(
		`SELECT site_id, node_epoch, seq, schema_version, origin_auth,
		        authored_at_wall_ms, authored_at_max_error_us, authored_at_state,
		        occurred_at, time_source, payload, row_hash FROM event_log`)
	if err != nil {
		return fmt.Errorf("store: row hash check: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var env Envelope
		var payload, wantHash, authState string
		if err := rows.Scan(&env.SiteID, &env.NodeEpoch, &env.Seq, &env.SchemaVersion, &env.OriginAuth,
			&env.AuthoredAt.WallMS, &env.AuthoredAt.MaxErrorUS, &authState,
			&env.OccurredAt, &env.TimeSource, &payload, &wantHash); err != nil {
			return fmt.Errorf("store: row hash check scan: %w", err)
		}
		env.AuthoredAt.State = ClockState(authState)
		env.Payload = json.RawMessage(payload)
		if got := env.Hash(); got != wantHash {
			return fmt.Errorf("store: row hash check: %v is corrupted — stored row_hash %s, recomputed %s from its current content", env.Key(), wantHash, got)
		}
	}
	return rows.Err()
}

// StoredSchemaVersion reads schema_version back out of the database — the
// status page shows it so "the DB self-created and is readable" is a fact on
// screen, not an assumption.
func (s *Store) StoredSchemaVersion() (string, error) {
	var v string
	err := s.conn().QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&v)
	if err != nil {
		return "", fmt.Errorf("store: read schema_version: %w", err)
	}
	return v, nil
}

func (s *Store) Close() error {
	perr := s.pool.close()
	if err := s.db.Close(); err != nil {
		return err
	}
	return perr
}

// diskFullMarkers are the texts a full disk reaches this package as:
// SQLite's own (SQLITE_FULL, "database or disk is full (13)" through the
// modernc driver), the POSIX errno text, and Windows' ERROR_DISK_FULL /
// ERROR_HANDLE_DISK_FULL wording.
var diskFullMarkers = []string{
	"database or disk is full", "SQLITE_FULL", "no space left on device",
	"not enough space on the disk", "disk is full",
}

// IsDiskFull reports whether err is a write refused because the disk this
// post's database lives on is full (FIX17b, sc-stress F10). It reads the
// error's text, the one form every layer (driver, OS, wrapping) preserves.
func IsDiskFull(err error) bool {
	return err != nil && TextIsDiskFull(err.Error())
}

// TextIsDiskFull is IsDiskFull over an error's text, for callers that hold
// only the string (the web layer's refusal mapping).
func TextIsDiskFull(s string) bool {
	for _, m := range diskFullMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}
