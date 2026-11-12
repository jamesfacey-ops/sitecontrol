// envelope.go holds THE definition of the replication envelope (design §8.2:
// "defined once, referenced everywhere"). No other file, package, or doc in
// this repo restates its shape; domain event types live inside Payload, and
// the C3a transport ships this struct as-is.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Envelope wraps every replicated event (design §8.2).
type Envelope struct {
	// SiteID is the authoring site. Exactly one site ever authors a given
	// stream; no site ever mints events in another site's namespace.
	SiteID string `json:"site_id"`

	// NodeEpoch increments at every re-image (design §8.2). It is part of the
	// ordering, idempotency, and watermark key — seeded into local config at
	// provisioning, never inferred.
	NodeEpoch int64 `json:"node_epoch"`

	// Seq is per-site monotonic and contiguous WITHIN a node_epoch. It orders
	// only together with NodeEpoch (§8.3, epoch-major).
	Seq int64 `json:"seq"`

	// SchemaVersion is the in-season frozen schema version (§8.5), stamped
	// from store.SchemaVersion at authoring — the store const is the single
	// source (C1 review M1: the DB refuses to open under any other version).
	SchemaVersion int64 `json:"schema_version"`

	// OriginAuth is the origin-identity this record claims (§8.4). For
	// own-authored events it is the authoring site's identity; C3a enforces
	// the ingest rule that it match the authenticated delivering peer.
	OriginAuth string `json:"origin_auth"`

	// AuthoredAt is the authoring post's own clock reading at the instant it
	// authored this event, and how far that clock could be trusted then
	// (schema-10 §5B, item 6). It is AUDIT EVIDENCE. It orders nothing —
	// ordering is (site_id, node_epoch, seq), always (§5B.5). Always
	// present, never a pointer.
	AuthoredAt AuthoredAt `json:"authored_at"`

	// OccurredAt is when the thing this event records actually HAPPENED, in
	// UTC ms (item 13, TP §4.1). On the default path it is a COPY of
	// AuthoredAt.WallMS from the same single clock reading (rule W); on the
	// entered path it is the officer's typed instant, stored byte-exactly.
	// It orders nothing (TP §4.4): no fold, replay selection, sync,
	// watermark, or comparator over events may read it.
	OccurredAt int64 `json:"occurred_at"`

	// TimeSource records WHO supplied OccurredAt: "default" (the officer
	// left the time field untouched — the machine stamped it) or "entered"
	// (the officer explicitly overrode it). It is NOT clock_state and never
	// substitutes for it (TP §4.2): AuthoredAt.State describes the MACHINE's
	// clock, TimeSource describes who supplied the value. Received bytes are
	// stored exactly as sent — an unknown value survives write and read
	// untouched and fails closed at READ (domain.ProvenanceClass), which is
	// what makes a third value addable post-bump without a schema outage.
	TimeSource string `json:"time_source"`

	// Payload is the event itself: a JSON object whose types internal/domain
	// owns (C5/C6). The store treats it as opaque, valid JSON.
	Payload json.RawMessage `json:"payload"`
}

// AuthoredAt is the replicated time basis (schema-10 §5B.3): the authoring
// post's own wall-clock reading and the kernel's own opinion of how far that
// reading could be trusted, taken together at authoring. The confidence
// lives in the same struct as the time on purpose — a timestamp whose
// confidence has to be looked up elsewhere will be read without it.
type AuthoredAt struct {
	// WallMS is Unix ms, from the AUTHORING post's clock.
	WallMS int64 `json:"wall_ms"`
	// MaxErrorUS is the kernel's own error bound in microseconds, or
	// MaxErrorUnknown (-1) when the kernel would not say.
	MaxErrorUS int64 `json:"max_error_us"`
	// State is clock.go's frozen three-state vocabulary:
	// synced | unsynced | unverified. Own-authored events always carry one
	// of the three (the authoring path normalises an unrecognised state to
	// unverified BEFORE hashing, §5B.4 rule 2); an inbound envelope's value
	// is stored exactly as received, because it sits inside Hash().
	State ClockState `json:"state"`
}

// Key is the ordering and idempotency key: the FULL triple, epoch-major
// (§8.3). Never (site_id, seq) alone — keying on the pair would dedup a
// re-imaged site's fresh low sequence numbers as already-applied and silently
// drop an entire season of records.
type Key struct {
	SiteID    string
	NodeEpoch int64
	Seq       int64
}

func (e Envelope) Key() Key { return Key{SiteID: e.SiteID, NodeEpoch: e.NodeEpoch, Seq: e.Seq} }

func (k Key) String() string {
	return fmt.Sprintf("(%s, epoch %d, seq %d)", k.SiteID, k.NodeEpoch, k.Seq)
}

// Validate checks the envelope's structural invariants. It says nothing about
// contiguity (the watermark's job) or origin identity (C3a's job).
func (e Envelope) Validate() error {
	switch {
	case e.SiteID == "":
		return fmt.Errorf("envelope: site_id is required")
	case e.NodeEpoch < 1:
		return fmt.Errorf("envelope %v: node_epoch must be >= 1", e.Key())
	case e.Seq < 1:
		return fmt.Errorf("envelope %v: seq must be >= 1 (watermarks open at 0)", e.Key())
	case e.OriginAuth == "":
		return fmt.Errorf("envelope %v: origin_auth is required", e.Key())
	case len(e.Payload) == 0 || !json.Valid(e.Payload):
		return fmt.Errorf("envelope %v: payload must be valid JSON", e.Key())
	}
	return nil
}

// Hash is a deterministic digest of the whole envelope. Computed on demand.
// SchemaVersion 10 CHANGED the §8.2 wire shape — the first change ever taken
// there (schema-10 §5B.3): the replicated time basis (AuthoredAt's triple)
// and the OccurredAt/TimeSource pair ride the envelope and ALL FIVE values
// are hashed, so altering a recorded time breaks a digest a second post also
// holds. Since the F25 fix (SchemaVersion 6) the digest is ALSO stored at
// rest, once per row, in
// event_log.row_hash (log.go insertAndFold) — a second, physically separate
// copy of this same digest that IntegrityCheck re-derives and compares on
// every Open, so localized at-rest corruption of a row's content disagrees
// with its own stored hash. That is a distinct use from the seam-echo
// comparison below; both call this same function because both want "does
// this envelope's content match what it's being compared against."
// Payload bytes are NOT copied byte-for-byte by replication in general —
// Go's JSON encoder rewrites RawMessage in flight (whitespace stripped,
// <, >, & escaped) — which is why Author canonicalises the payload to that
// same wire encoding at authoring time (F9): stored bytes ≡ wire bytes by
// construction, so any two nodes holding the same event hash it
// identically. Consumed by the C3a seam-echo check: the pull server
// attests its envelope at from−1 and the client compares against the row it
// already holds, refusing a stream whose history shares a seq but not a
// prefix (the same-epoch-rebuild splice).
func (e Envelope) Hash() string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d\x00%d\x00%d\x00%s\x00%d\x00%d\x00%s\x00%d\x00%s\x00",
		e.SiteID, e.NodeEpoch, e.Seq, e.SchemaVersion, e.OriginAuth,
		e.AuthoredAt.WallMS, e.AuthoredAt.MaxErrorUS, e.AuthoredAt.State,
		e.OccurredAt, e.TimeSource)
	h.Write(e.Payload)
	return hex.EncodeToString(h.Sum(nil))
}

// GapError reports a delivered seq beyond watermark+1. The watermark stalls
// LOUDLY (§8.4): it never advances across a gap and never silently skips —
// the caller gets the exact missing range in the error.
type GapError struct {
	Key       Key
	Watermark int64
}

func (e *GapError) Error() string {
	return fmt.Sprintf("store: gap in stream %s: delivered seq %d but watermark is %d (seqs %d..%d missing) — watermark stalled, event not applied",
		e.Key.SiteID, e.Key.Seq, e.Watermark, e.Watermark+1, e.Key.Seq-1)
}

// SchemaVersionError reports an envelope stamped with a schema version other
// than this binary's. The log never holds events of a foreign schema version;
// C3a catches this to implement the §8.5 queue + visible refusal.
type SchemaVersionError struct {
	Key  Key
	Got  int64
	Want int64
}

func (e *SchemaVersionError) Error() string {
	return fmt.Sprintf("store: event %v has schema_version %d but this binary is %d — refused, never best-effort applied (§8.5)",
		e.Key, e.Got, e.Want)
}

// Less is the fixed-order comparison: lexicographic (SiteID, NodeEpoch, Seq),
// epoch-major per the Key doc comment above (§8.3). This is the Go half of
// the ONE ordering spec (visible-failure-v1 §4.3 rule 1); the SQL half is
// the single canonical `ORDER BY` spelling in the amendment composition
// (internal/domain), and test T-A proves the two agree. No other comparator
// over Key may ever exist: keying that ignores the epoch "would silently
// drop an entire season of records" (the Key doc comment above), and a
// second comparator is the two-implementations drift surface T-A refuses.
func (a Key) Less(b Key) bool {
	if a.SiteID != b.SiteID {
		return a.SiteID < b.SiteID
	}
	if a.NodeEpoch != b.NodeEpoch {
		return a.NodeEpoch < b.NodeEpoch
	}
	return a.Seq < b.Seq
}
