// Package repl is the replication sync core (chunk C3a): the pull server
// ("give me site S, epoch E, from seq N"), the puller client, the poke-then-
// pull fast path, the ingest identity rule, the per-stream sync status, and
// the §9.3 anchor. It CONSUMES the C2 store's watermark store and contiguity
// rule (chunk-plan AR-5) and never redefines them: every delivered envelope
// goes through store.Apply, so duplicates are no-ops, gaps stall loudly, and
// the watermark advances only on contiguous application.
//
// The wire protocol is documented in design/sync-protocol-v1.md (in-repo,
// design §13.3). This file holds the exchange shapes.
package repl

import (
	"sitecontrol/internal/store"
)

// HeaderFrom carries the requester's claimed site_id on every exchange. C3a
// uses it to know whom to pull after a poke; C3b's self-history rejoin
// exception (§8.4) keys on it in refuseForeignSite (server.go).
const HeaderFrom = "X-SiteControl-From"

// Echo is the NTP-style offset-echo triple carried on EVERY sync exchange
// (design §8.2): t1 = requester's local send stamp (echoed back verbatim),
// t2 = responder's local receive stamp, t3 = responder's local send stamp,
// all unix nanoseconds of the respective LOCAL wall clock. They exist so the
// deferred clock-offset gauge (§8.9) needs no protocol revision. They are
// display/diagnostic data only: they participate in no ordering or deadline
// decision, are displayed nowhere, and are stored nowhere beyond the exchange
// — the puller decodes them into the response struct and drops them with it.
type Echo struct {
	T1 int64 `json:"t1"`
	T2 int64 `json:"t2"`
	T3 int64 `json:"t3"`
}

// StreamHead announces one stream of the author's log: an epoch and its head.
// Head IS the author's own watermark for that stream (carry-forward row 3:
// "Watermark serves as that head") — safe because Apply/Author insert only at
// watermark+1, so the log holds a contiguous prefix per stream, always.
type StreamHead struct {
	NodeEpoch int64 `json:"node_epoch"`
	Head      int64 `json:"head"`
}

// StreamsResponse answers GET /sync/streams?site=S: every stream the server
// holds FOR ITS OWN SITE, ascending by epoch. A higher epoch than the
// consumer has seen is the §8.4 "newly announced higher epoch" — the consumer
// opens a fresh watermark at zero for it.
type StreamsResponse struct {
	SiteID  string       `json:"site_id"`
	Streams []StreamHead `json:"streams"`
	Echo
}

// PullResponse answers GET /sync/pull?site=S&epoch=E&from=N&limit=L: one page
// of envelopes plus the stream's current head, so the client knows whether
// this pull has drained (client watermark >= head) or another page is needed.
type PullResponse struct {
	SiteID    string           `json:"site_id"`
	NodeEpoch int64            `json:"node_epoch"`
	Head      int64            `json:"head"`
	Events    []store.Envelope `json:"events"`

	// PrevHash is the seam echo (C3a addendum 3): the author's envelope hash
	// (store.Envelope.Hash) at seq from−1, set whenever from ≥ 2 and the
	// author holds that row; absent on a from-1 pull, which has no seam. The
	// client compares it against the row IT already holds at that seq before
	// applying anything from an advancing page; a mismatch is the silent
	// mixed-prefix splice (a same-epoch rebuild grown past the consumer's
	// watermark) and is refused as prefix_diverged with the watermark
	// untouched. Additive wire field — no schema bump, no envelope change.
	PrevHash string `json:"prev_hash,omitempty"`
	Echo
}

// PokeResponse answers POST /sync/poke. The poke carries no records — records
// are never pushed (§8.9): a pushed out-of-order record could not apply
// against the contiguity watermark. The poked site pulls from its own
// watermark instead.
type PokeResponse struct {
	SiteID string `json:"site_id"`
	Echo
}
