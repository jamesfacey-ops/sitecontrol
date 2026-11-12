# SiteControl sync protocol v1

**Authored in chunk C3a** (design §13.3: this document contains exactly the listed contents —
envelope by reference, watermark rule, idempotency key, version rule, rejoin rule, origin
fields, offset echoes — plus the regression section C3b owns). **C3b amended §5 (rejoin) and
§6 (regression); every section is now closed.** The demo script in `prototype-design-v3.md`
§13.3 is this protocol's test harness — there is no separate test campaign.

Transport is HTTP between site boxes, three endpoints, all requiring the target site's demo
`peer_key` as `Authorization: Bearer <key>` and carrying the requester's site id in
`X-SiteControl-From` (design §8.4 sanctions demo keys; rotation is production-doc material):

| Endpoint | Ask | Answer |
|---|---|---|
| `GET /sync/streams?site=S` | "what streams do you hold for site S?" | `{site_id, streams:[{node_epoch, head}], t1,t2,t3}` |
| `GET /sync/pull?site=S&epoch=E&from=N&limit=L` | "give me site S, epoch E, from seq N, at most L" | `{site_id, node_epoch, head, events:[envelope…], t1,t2,t3}` |
| `POST /sync/poke` | "I authored something — pull me" | `{site_id, t1,t2,t3}` |

Wire shapes live in `internal/repl/wire.go`; server in `internal/repl/server.go`; puller in
`internal/repl/engine.go`.

## 1. Envelope — by reference

The envelope is defined ONCE, in design §8.2 and in code at `internal/store/envelope.go`
(`store.Envelope`). This document deliberately does not restate its fields — a second copy
would drift. Events travel on the wire as the JSON serialisation of that one struct, inside
the pull response's `events` array. The offset echoes (§7) are **exchange** fields, not
envelope fields, and never appear inside an envelope.

## 2. Watermark rule

The consumer keeps one watermark per **(site_id, node_epoch)** stream — C2's watermark store;
this protocol consumes it and does not redefine it (chunk-plan AR-5).

- The watermark advances **only on contiguous application** (seq == watermark+1). A delivered
  seq beyond watermark+1 is a **loud stall** (`store.GapError`): nothing applies, the
  watermark does not move, the stream's status turns `gap_stalled`. Never a silent skip.
  Because the author's own log holds a contiguous prefix per stream, always (Apply/Author
  insert only at watermark+1), an honest author can never serve a gapped page — a gap on the
  pull path means corruption or a bug, which is exactly why the stall is loud.
- A stream never heard from opens at watermark 0; the first pull asks `from=1`.
- **A newly announced higher node_epoch is a new stream and opens a fresh watermark at
  zero** (§8.4). Announcement is the `streams` list: the author lists every epoch it holds
  for its own site (plus its current config-seeded epoch, even when empty at head 0).
- **The author's current head is reported via `Watermark`** — the author's own watermark for
  the stream IS the head, valid because of the contiguous-prefix property above. Every pull
  page carries `head` (read after the page, so a racing append can only under-report drain,
  never fake it). A pull of a stream has **drained** when the consumer's watermark reaches
  the `head` of the page it just applied; a pull of an author has drained when every
  announced stream has. Catch-up is paginated (`limit`; the server caps pages at 1000
  regardless), and pagination is invisible to every rule here: each page applies
  per-envelope, so a partly-bad page keeps its earlier contiguous progress.
- **The seam echo (splice guard).** A pull page carries `prev_hash` — the author's envelope
  hash at `from−1` — whenever `from ≥ 2` and the author holds that row. The hash
  (`store.Envelope.Hash`, computed on demand, stored nowhere, no envelope field added) is
  reimplementable from this line: lowercase-hex SHA-256 over
  `site_id NUL node_epoch NUL seq NUL schema_version NUL origin_auth NUL payload-bytes`, the
  integers in decimal, NUL = 0x00, the payload being the exact JSON bytes that travel in the
  envelope — well-defined on both ends because replication copies payload bytes verbatim. Before applying anything from an **advancing** page (one
  containing seq watermark+1; the gate is the served events, never the reported head, which a
  splicing server could understate), the consumer compares `prev_hash` against the row it
  already holds at that seq. Mismatch → terminal state **`prefix_diverged`**: watermark
  untouched, nothing applied. This is the only runtime defence against the same-epoch-rebuild
  splice — a node wiped without its config epoch bump re-authors from seq 1, and once its new
  history passes a consumer's watermark the identity triple, the contiguity check, and the
  §8.9 regression detector all pass. An advancing page without a seam echo is a protocol
  violation (an author holds every row below its head). A `from=1` pull has no seam and
  carries no echo. Known residual: an equal-head rebuild that never authors past the
  consumer's watermark serves no advancing page and stays invisible to this guard — it also
  never spreads; dump-compare/ops territory, with the full chain-hash in `/sync/streams` as
  the production successor (schema bump; not built in the prototype).
- Client-side consequence (not itself a wire rule): the §9.3 anchor — the last B-local
  instant B held the author's head — is recorded only when a pull drains to the author's
  current head, and intermediate catch-up batches never touch it; see
  `internal/repl/engine.go` (`peerState`) for the two-slot rule and its §9.3 rationale.
  **Anchor currency is drain-relative**: `Anchor()` is valid only for the events the LATEST
  drain delivered — every rotation silently invalidates it for older uncaptured events, and
  no receipt time exists in the store to detect that after the fact. A consumer needing G at
  receipt (C7's remaining-at-receipt) must capture it synchronously at apply time via the
  engine's `OnApplied` hook, which fires per applied envelope with the pre-rotation anchor —
  never by polling `Anchor()` later.

## 3. Idempotency key

The idempotency, ordering, and watermark key is the **full triple (site_id, node_epoch,
seq)**, epoch-major (§8.3). A delivered envelope whose seq is at or below the stream's
watermark is a duplicate: it is acknowledged as a no-op — no log write, no projection
mutation. Re-delivering an entire stream is byte-identically nothing. Keying on
(site_id, seq) alone would dedup a re-imaged site's fresh epoch as "already applied" and
silently drop a season; the epoch is in the key precisely so that cannot happen.

The key's limit, and why the seam echo (§2) exists beside it: the key proves a delivered seq
was **already applied**, not that the author still holds the **same rows** below it. Two
histories under one unbumped (site_id, node_epoch) can agree on every sequence number and
share no events — idempotency alone would splice them at the watermark. The `prev_hash`
comparison is what upgrades "same key" to "same prefix" before any advancing application.

## 4. Version rule

`schema_version` is frozen in-season (§8.5). The store refuses to apply an envelope bearing
any other version — a typed `store.SchemaVersionError`, raised **before anything is
written**; the log never holds a foreign-version event. On the pull path that refusal is
surfaced, never smoothed over: the stream's status becomes `schema_mismatch` (a visible
refusal in the §8.9 surface), and its watermark stalls.

**There is deliberately NO local queue table, and a future reader must not "fix" its
absence.** §8.5's queue obligation is already satisfied structurally in a pull transport
(C2 architecture review ruling): the refused records sit durably in the **author's own
log**, and the consumer's stalled watermark marks exactly where to resume; after the
consumer's own re-image brings its schema forward, the next routine pull drains them
through the identical §8.6 catch-up path. A push transport would have needed a real queue;
pull makes it virtual. Two obligations bind the implementation:

- **The catch is per-stream.** One mismatched (site_id, node_epoch) stalls only its own
  watermark and status; the pull cycle continues for the author's other streams and for
  every other peer.
- **The surfaced state is per-stream** in the §8.9 UI (C10 renders `schema_mismatch` from
  the per-stream status this engine produces).

A mixed-version epoch is impossible by design: a version bump always rides a re-image,
which is a new node_epoch.

## 5. Rejoin rule — *built in C3b, on C3a's routine catch-up path*

A blank (re-imaged or replaced) node rejoins by **replaying logs from seq 0 through the
identical routine catch-up path** above — one code path, exercised all season (§8.6): C3b
adds no second, parallel recovery implementation. Concretely, `internal/repl/engine.go`
factors the "ask a peer's `/sync/streams`, then page every announced epoch through
`pullStream`" routine into one function, `drainSite(ctx, ps, site)`. `pullPeer` — the
function `Run()`'s ticker calls every sync interval, unchanged since C3a — calls it as
`drainSite(ctx, ps, ps.cfg.SiteID)` for a peer's own log. `Rejoin(ctx)` calls the *same*
function as `drainSite(ctx, ps, e.cfg.SiteID)` — the only difference is which site_id is
asked for.

Order: the reborn node first re-pulls **its own prior-epoch stream** from any authenticated
peer — the single sanctioned relay exception, self-history only (§8.4) — then every peer's
log; it then resumes authoring under the **config-seeded node_epoch+1**, a fresh stream that
cannot collide with its own history by construction and that peers accept as new (fresh
watermark at zero, §2 above). `Rejoin` is synchronous and is meant to run once, before
`Run()` starts the routine timer and before any `Author` call — that ordering is what makes
"own-history-first" *observed*, not merely eventual. It is also safe to call unconditionally
(including on a node that was never wiped): if no peer holds anything under our own site_id
the self-history step pulls zero streams and is a no-op, and every delivered record is
idempotent through the ordinary `Apply` rule regardless. Never ship DB files between live
peers. Full-season log retention is the stated decision, so seq 0 is always reachable.

**Ingest side of the exception** (client, `pullStream`): one added arm on the existing
identity check, not a relaxed check. The original condition
(`env.SiteID == ps.cfg.SiteID && env.OriginAuth == ps.cfg.SiteID`) is untouched; a second arm
accepts an envelope only when the stream being asked for is our own
(`site == e.cfg.SiteID`) **and** the envelope itself bears our own site_id and origin_auth. A
third site's records delivered under cover of a self-history pull match neither arm and are
rejected as `identity_rejected`, exactly as any other foreign-identity record would be —
proven against a rigged, non-conforming peer by
`TestSelfHistoryIngestRejectsRecordsBearingAThirdSitesIdentity`.

**Serving side of the exception** (server, `refuseForeignSite`): the mirror rule in §7 below
("no transitive relay") now also serves `site` when it equals the *requester's own claimed
identity* (`X-SiteControl-From`) — a node asking for exactly the stream it claims to be,
still gated by the ordinary demo-key authentication. This is not a relaxation of
authentication during recovery: a request for a third site's stream is refused exactly as
before, regardless of the requester's claimed identity — `site` must equal either the
server's own site_id or the requester's own claim, never anything else. Both directions of
the boundary are tested: `TestPullServerServesSelfHistoryOnlyNotAnyThirdSiteClaimedAsSelf`
(a legitimate self-history ask succeeds; the same requester claiming to be site-b while
asking for site-c's stream is still 403) and the client-side test above (even a
non-conforming peer server cannot get a third site's records applied by returning them in
answer to a self-history pull).

## 6. Regression — *built in C3b*

"Peer log regressed" (§8.9): an author answering **below a previously seen seq within the
same node_epoch**. A genuine re-image announces a HIGHER epoch instead (§8.6), so a
same-epoch regression is never legitimate: it is its own distinct surfaced status,
`peer_log_regressed`, never folded into `ok`/`gap_stalled`/etc.

Mechanism: the engine remembers the highest head it has ever seen per stream
(`map[StreamKey]int64`, keyed by (site_id, node_epoch) exactly like the watermark store).
Every reported head is compared against that memory before the per-pull state is recorded; a
lower head within the same key flips the state. Because the key includes node_epoch, a real
re-image's fresh, low-numbered stream is a *different* key and can never be mistaken for a
regression of the old one — verified directly by
`TestEpochBumpToAFreshLowHeadDoesNotFlipRegressed`. The flip never touches the watermark:
regression is a surfaced signal about the peer, not a rollback — nothing here can move an
applied watermark backwards, which only the idempotent `Apply` loop advances, and only
forward. Rendering `peer_log_regressed` in the sync-status UI is C10's work, over this
engine's existing per-stream status surface (`StreamStatus.State`, unchanged since C3a).

## 7. Origin fields

Every envelope carries `origin_auth` — the origin identity the record claims (§8.2). The
**ingest identity rule** (§8.4): a consumer accepts a record only when **both** its
`site_id` and its `origin_auth` match the authenticated peer identity delivering it, and its
`node_epoch` matches the stream being pulled. Anything else is rejected without application
and surfaces as `identity_rejected` on that stream. The pull server enforces the mirror
rule: it serves **only the streams of its own site_id** (403 otherwise) — the prototype mesh
has **no transitive relay**; every site pulls every author directly. The one sanctioned
exception (a rejoining node ingesting records bearing its OWN site_id from any authenticated
peer) is C3b's, per §5. Cross-site effects are counter-events only: a site reacts to a
peer's fact by authoring its own event, never by writing into the peer's stream. Demo keys
authenticate peers (§8.4); `X-SiteControl-From` carries the requester's claimed identity,
which C3a uses to route poke-then-pull and C3b will use to key the self-history exception.

## 8. Offset echoes

Every sync **exchange** (streams, pull, and poke alike — not every record) carries NTP-style
timestamp echoes: the requester stamps `t1` (its local send instant, unix nanoseconds); the
responder echoes `t1` and adds `t2` (its local receive) and `t3` (its local send). They
exist so the deferred clock-offset gauge (§8.9) needs no protocol revision when built. They
are display/diagnostic data only: they participate in **no ordering, deadline, or conflict
decision**, are displayed nowhere in the prototype, and are stored nowhere beyond the
exchange — the puller decodes them into the response struct and drops them with it.
