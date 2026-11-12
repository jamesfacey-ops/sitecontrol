// visit_report.go — the analytics MVP: per-visit time on site and HOW each
// visit closed, derived AT READ TIME from event_log. Owner's requirement
// (2026-09-09): "show how long they are on site and track the times that the
// individual has failed to sign themselves in or out."
//
// WHAT IT IS. One linear replay of the log in key order through
// store.EachEvent — THE one audit read over the log (PLAN-SEC-A Amendment
// 2; historical-muster-v1 §11.2) — carrying open[site][person] → visit
// exactly as the folds do, then the sanctioned per-key helpers for each
// instant's label (ProvenanceClass + DisplayTimeClass), ResolveOccurredAt
// for any instant a correction targets, and LoadPersonMergeMap for the
// person column. CSV out. ⟦was: one structural SQL query with json_each
// and a first-closer window over event_log, read through LocalTx from this
// package — MEASURED quadratic (500 visits 0.9 s, 2,000 visits 12.5 s, a
// season's 14k ≈ minutes) and the first raw log read outside internal/store,
// invisible to W1 guard 3b's reader allowlist; review REVIEW-ANALYTICS.md.⟧
//
// WHAT IT IS NOT. No event type is minted, no projection column is added,
// no side table, no job, no page, no SQL against event_log in this package.
// The `visit` projection discards which closer fired and holds no time
// (§5B.5 forbids a projection column ever holding one), so the log is the
// only honest source and a read over it is the whole mechanism. This is the
// "derive pass, measured first" that CONSOLIDATED-PLAN-v1 asks for before
// any stored visit-span index is considered; it deliberately does NOT use
// that name.
//
// THE CLOSERS, mirrored from the folds exactly — every one is an own-site
// event on the visit's own site, so the closer that fires is the FIRST of
// these after the opener in that site's stream order. Three fold paths,
// FOUR report reasons, because one fold path carries two different facts:
//
//   - visit_checked_out naming the visit_id       (checkin.go applyVisitCheckedOut)
//     → checked_out: a recorded departure.
//   - trip_dispatched naming the person as driver or occupant, closing every
//     open visit of that person at the origin  (trip.go applyTripDispatched)
//     → dispatched:  the trip has a destination (controlled or
//     uncontrolled) — a recorded departure (D-4: nobody
//     leaves except in a vehicle, to somewhere) — or is a
//     ROAD-WORK run (road_work.go; James, 2026-10-04, R5:
//     road work counts as a departure).
//     ⟦was: "the trip has a destination (controlled or
//     uncontrolled)" only⟧
//     → re_checkin:  the trip has NO destination and is not road work.
//     That is the DEGENERATE
//     LOCAL TRIP (design §10) — the dispatch half of an
//     ordinary gate CHECK-IN at this same site. Its fold
//     closes the person's still-open visit BEFORE the
//     arrival half opens the new one, so a person who never
//     signed out and walks back through the gate is closed
//     HERE, never by the arrival. A missed sign-out.
//   - the person's NEXT trip_arrival at the same site, which closes the
//     still-open visit before opening the new one (trip.go applyTripArrival)
//     → next_arrival: reached only when no same-site dispatch preceded
//     the arrival — an expected arrival of a trip from
//     elsewhere, or an unsolicited one. A missed sign-out.
//
// ⚠ MEASURED while building the fixtures (2026-09-09): the first draft
// counted only the arrival path as the missed sign-out and read a gate
// re-check-in as "dispatched" — indistinguishable from a real departure. The
// destination-less dispatch is the fingerprint the gate path actually
// leaves. A missed sign-out's close instant is when the record was closed,
// NOT a departure, so on_site_min is blank for both re_checkin and
// next_arrival and record_open_min carries the span as an upper bound;
// departure_recorded says which kind a row is in one column.
//
// ⚠ BOUNDARY OF THE RULE. "Destination-less = local check-in" holds for
// logs authored since app commit 4091581 (2026-08-21, D-4: the vehicle-less
// exit was removed and Dispatch refuses a departure that names nowhere).
// Rows older than that could carry destination-less departures, and this
// classification does not apply to them. In practice no live store predates
// it — SchemaVersion has been bumped since and Open refuses an older file —
// but a log replayed from an archive of that era must not be read with it.
//
// Matching is on the RAW person_id, as the folds match; merge resolution is
// applied only to the person column of the output, so the report closes
// visits exactly as the projections did and then folds duplicate identities
// for counting. The muster deliberately does not fold them (a safe
// over-count); analytics must, or per-person figures are wrong by the
// duplicate rate.
//
// CLOCKS. Both ends of a visit are stamped by ONE post's clock (opener and
// closer are own-site), so a duration is a same-clock subtraction — the case
// the cross-site clock distrust does not cover. Each end is still labelled
// from stored provenance: observed (default × synced), observed-clock-
// untrusted (default × unsynced/unverified), claimed (entered), claimed-
// corrected (an occurred_at_corrected stands — TP §7.4's substitution cell,
// a claim in both clock columns), untrusted (an ill pair or unknown
// time_source), unresolvable (the resolver failed closed: dangling or cyclic
// correction chain — the instant is left blank, never guessed). A visit's
// duration_label is the worst of its two ends — EXCEPT when the two ends sit
// in different node_epochs of the same site: a re-image bumps the epoch, so
// those are two incarnations of the post and possibly two clocks, and the
// same-clock justification does not reach across them. That duration is
// still computed but labelled cross-epoch, never observed.
//
// SITE TYPE AND THE ONE VERDICT (R-4/R-5, 2026-09-09). The owner tracks
// failures to sign out at his text-message sites and exempts camp sites,
// where "the persons staying in the camp may be on site for a month or more
// regularly". The site type lives in each site's own TOML (`site_type`,
// REQUIRED, no default — config.validate() refuses its absence exactly as it
// refuses a missing `orientation`; nothing in the event schema changed). It
// decides ONE thing here: whether a visit closed by the person's own return
// (re_checkin / next_arrival — the closure is derived first, from the log,
// type-blind) is reported as a FAILURE. Duration is still bucketed BY
// DURATION with the bound on the label for every site (PLAN-SEC-D §1.4: split
// on the observable, never label a person resident or visitor): a 35-day
// checked-out stay at a camp reads "over 30d" and nothing else.
//
// ⚠ WHAT A POST KNOWS. Config is local-only (design §7), so a post knows its
// OWN type and no peer's — the same structural fact that withdrew the
// peer-orientation read (schema-10 §5C.7). The report therefore takes the
// types it is told (SiteTypes) and, for a site it was not told about, emits
// the raw closure with NO verdict and names the site on the summary: a zero
// that came from not knowing is never printed as a zero. Running the report
// at that post gives that site's verdicts.
//
// ⚠ WHAT IS NOT DERIVED. A visit still OPEN at report time is not aged and
// not flagged, at any site type: ageing it needs a report-time clock read
// against an authoring stamp (a different clock, unless own-site and
// same-epoch) and a per-site "how long is too long" ruling — neither is
// taken here. So a text-message worker who never signs out is counted the
// day their NEXT visit closes the old one, not before; their last missed
// sign-out of a season is invisible until they return. And a failure to sign
// IN has no log fingerprint at all: the gate's dispatch picker offers only
// people with open visits, so the officer's only route for someone leaving
// who never signed in is to sign them in first — which the log records as an
// ordinary arrival (a back-entered one carries time_source "entered", but so
// does a correctly relayed text, so the label cannot separate them).
//
// §4.4 DISCIPLINE. Nothing here reads OccurredAt/TimeSource in a comparator
// or orders by them: the replay is ordered by the stream (site_id,
// node_epoch, seq) that EachEvent hands over, and the stored pair reaches
// ProvenanceClass as plain arguments. The W1-5 scan
// (TestW1NoComparatorReadsOccurredAtOrTimeSourceInStoreReplOrDomain) covers
// this file like any other.
package domain

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"sitecontrol/internal/store"
)

// VisitClosure names how a visit's record was closed.
type VisitClosure string

const (
	// ClosedByCheckout: an explicit visit_checked_out — a recorded departure.
	ClosedByCheckout VisitClosure = "checked_out"
	// ClosedByDispatch: a trip_dispatched WITH a destination carrying the
	// person away — a recorded departure.
	ClosedByDispatch VisitClosure = "dispatched"
	// ClosedByReCheckin: the destination-less dispatch half of the person's
	// next gate check-in at the same site closed a visit nobody had closed.
	// A MISSED SIGN-OUT — the shape the gate path produces.
	ClosedByReCheckin VisitClosure = "re_checkin"
	// ClosedByNextArrival: the person's next trip_arrival at the same site
	// closed a visit nobody had closed. A MISSED SIGN-OUT — the shape an
	// expected or unsolicited arrival from elsewhere produces.
	ClosedByNextArrival VisitClosure = "next_arrival"
	// VisitStillOpen: no closer has fired.
	VisitStillOpen VisitClosure = "open"
)

// DepartureRecorded reports whether this closure records the person LEAVING
// (a duration is a measurement) as opposed to closing a record nobody had
// closed (a duration would be a guess). Open visits report false.
func (c VisitClosure) DepartureRecorded() bool {
	return c == ClosedByCheckout || c == ClosedByDispatch
}

// SiteType is the per-site kind the owner named on 2026-09-09 (R-4/R-5),
// read from that site's own config key `site_type` (REQUIRED, no default).
// It is the vocabulary config.validate() checks at Load, through
// ParseSiteType, so the report never meets a third value.
type SiteType string

const (
	// SiteTypeTextMessage: people sign in and out of the site PER VISIT —
	// the owner's text-message sites, and any gate post that enters each
	// visit by hand. A visit closed by the person's own return is a missed
	// sign-out (R-4).
	SiteTypeTextMessage SiteType = "text_message"
	// SiteTypeCamp: people live on the site for weeks at a time. The same
	// shape is normal there and is never a failure (R-5).
	SiteTypeCamp SiteType = "camp"
)

// ParseSiteType is the one place the vocabulary is checked. The error names
// both legal values and what each means, because it is read by an
// administrator at a post that will not start.
func ParseSiteType(s string) (SiteType, error) {
	switch SiteType(s) {
	case SiteTypeTextMessage, SiteTypeCamp:
		return SiteType(s), nil
	}
	return "", fmt.Errorf("%q is not a site type — the two legal values are %q (people sign in and out of this site per visit; a visit closed by the person's own return, with no departure recorded, counts as a missed sign-out — R-4) and %q (people live on this site for weeks at a time; the same shape is normal there and is never a failure — R-5)", s, SiteTypeTextMessage, SiteTypeCamp)
}

// SiteTypes is what the caller knows: site_id → type. A running post knows
// exactly one entry — its own, from config (design §7: config is
// local-only); a test may supply several. A site absent from the map is
// UNTYPED here, which is not the same as exempt.
type SiteTypes map[string]SiteType

// FailureMissedSignout is the one failure this report derives: at a
// text-message site the person's own return closed a visit nobody had
// closed. The incumbent Access system's TimeOutUnknown flag is the same
// fact, hand-set.
const FailureMissedSignout = "missed_signout"

// returnVerdict is the R-4/R-5 rule, in one place, read by both the
// per-visit report and the per-person fold so the two cannot disagree.
type returnVerdict int

const (
	// verdictNone: the closer was a recorded departure, or the visit is open —
	// there is nothing to judge.
	verdictNone returnVerdict = iota
	// verdictMissedSignout: closed by the person's own return at a
	// text-message site. The failure.
	verdictMissedSignout
	// verdictExemptCamp: the same shape at a camp — normal (R-5), counted so
	// the exemption is visible, never a failure.
	verdictExemptCamp
	// verdictUnclassified: the same shape at a site whose type this post has
	// not been told. Not a failure and NOT exempt — unknown.
	verdictUnclassified
)

// classifyReturn applies the rule. The closure is judged first and the type
// only when it matters, so a checked-out or dispatched visit carries no
// verdict at any site type.
func classifyReturn(closedBy VisitClosure, typ SiteType) returnVerdict {
	if closedBy != ClosedByReCheckin && closedBy != ClosedByNextArrival {
		return verdictNone
	}
	switch typ {
	case SiteTypeTextMessage:
		return verdictMissedSignout
	case SiteTypeCamp:
		return verdictExemptCamp
	}
	return verdictUnclassified
}

// Time-label vocabulary, ranked worst-last so a visit's duration_label is the
// max over its two ends. cross-epoch sits outside the rank: it is not a
// property of either end but of the pair.
const (
	timeObserved          = "observed"
	timeObservedClockBad  = "observed-clock-untrusted"
	timeClaimed           = "claimed"
	timeClaimedCorrected  = "claimed-corrected"
	timeUntrusted         = "untrusted"
	timeUnresolvable      = "unresolvable"
	timeCrossEpoch        = "cross-epoch"
	timeLabelRankUnranked = 99
)

func timeLabelRank(l string) int {
	switch l {
	case timeObserved:
		return 0
	case timeObservedClockBad:
		return 1
	case timeClaimed:
		return 2
	case timeClaimedCorrected:
		return 3
	case timeUntrusted:
		return 4
	case timeUnresolvable:
		return 5
	}
	return timeLabelRankUnranked
}

// VisitRecord is one row of the report: one visit, as the log records it.
type VisitRecord struct {
	SiteID  string
	VisitID string
	// PersonID is the merge-resolved canonical id (LoadPersonMergeMap).
	PersonID string
	// VisitPersonID is the id on the visit as recorded — differs from
	// PersonID exactly when the visit was recorded under a duplicate identity
	// an officer has since linked with same_as.
	VisitPersonID string
	// PersonName is the visit's own name snapshot from the arrival payload
	// (what the gate recorded), "" when the roster could not name the person.
	PersonName string
	TripRef    string

	// SiteType is the visit's site's type as THIS post knows it — "" when it
	// was not told (a peer's site), in which case Failure is never set.
	SiteType SiteType
	// Failure is FailureMissedSignout when the closer was the person's own
	// return (re_checkin / next_arrival) at a text-message site; ""
	// otherwise — at a camp (exempt, R-5) and at an untyped site (unknown;
	// the summary names the site) alike. Read the two together.
	Failure string

	ArrivalKey   store.Key
	ArrivedAtMS  int64 // 0 with ArrivalLabel == unresolvable
	ArrivalLabel string

	ClosedBy   VisitClosure
	CloseKey   store.Key // zero value while open
	ClosedAtMS int64     // 0 while open or unresolvable
	CloseLabel string    // "" while open

	// OnSiteMinutes is close − arrival, set ONLY when the closer is a
	// recorded departure (checkout or dispatch) and both ends resolved.
	OnSiteMinutes *int64
	// RecordOpenMinutes is close − arrival for ANY closer that resolved: the
	// span the record stood open. For next_arrival it is an upper bound on
	// the stay, not a measurement of it.
	RecordOpenMinutes *int64
	// DurationLabel is the worse of ArrivalLabel and CloseLabel, or
	// cross-epoch when the two ends were authored under different
	// node_epochs; "" while open.
	DurationLabel string
	// DurationBucket buckets OnSiteMinutes with the bound on the label;
	// "" when OnSiteMinutes is nil.
	DurationBucket string

	// The two ends' stored time basis, captured off the envelopes during the
	// replay so the common (uncorrected) case classifies with no further
	// read. Unexported: consumers read the resolved fields above.
	arrival, close timeBasis
}

// timeBasis is one envelope's stored pair plus its authoring clock, carried
// as plain values from EachEvent to the classifier. Nothing compares them.
type timeBasis struct {
	atMS   int64
	source string
	wallMS int64
	state  store.ClockState
}

func basisOf(env store.Envelope) timeBasis {
	return timeBasis{atMS: env.OccurredAt, source: env.TimeSource, wallMS: env.AuthoredAt.WallMS, state: env.AuthoredAt.State}
}

// StreamMark is one replicated stream's watermark at report time, so a
// reader can see how far a peer's log had reached on this post before
// trusting figures for that peer's site (analytics-v1 §8.4's rule: refuse
// figures from a stalled peer — this report prints the coverage; it does not
// decide).
type StreamMark struct {
	SiteID     string
	NodeEpoch  int64
	AppliedSeq int64
}

// VisitReportSummary is the denominator: printed on every run so an empty
// input can never read like a clean one (cmd/dupreport's own rule).
type VisitReportSummary struct {
	Visits      int
	Open        int
	CheckedOut  int
	Dispatched  int
	ReCheckin   int // missed sign-out, gate shape
	NextArrival int // missed sign-out, arrival-from-elsewhere shape

	DistinctVisitPersonIDs int
	DistinctPersons        int // after merge resolution

	NoPersonID       int // visits whose payload carried no person_id
	NoNameSnapshot   int // visits whose payload carried no person_name
	UnresolvableEnds int
	CrossEpoch       int // closed visits whose two ends sit in different epochs

	// The R-4 verdicts over the ReCheckin+NextArrival rows, split by what
	// this post knows about each site. The three always sum to
	// ReCheckin+NextArrival, so a reader can see how much of the raw count
	// was actually judged.
	MissedSignouts      int // at text-message sites — the failures
	ExemptCampReturns   int // at camp sites — normal, R-5
	UnclassifiedReturns int // at sites whose type this post was not told

	// TypedSites is what the caller told the report; UntypedSites is every
	// stream site in the store it did not — sorted, so the line is stable.
	TypedSites   SiteTypes
	UntypedSites []string

	Streams []StreamMark
}

// Lines renders the summary for stderr.
func (s VisitReportSummary) Lines() []string {
	pct := func(n int) string {
		if s.Visits == 0 {
			return "n/a"
		}
		return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(s.Visits))
	}
	out := []string{
		fmt.Sprintf("visits examined: %d (open %d, checked_out %d, dispatched %d, re_checkin %d, next_arrival %d)",
			s.Visits, s.Open, s.CheckedOut, s.Dispatched, s.ReCheckin, s.NextArrival),
		fmt.Sprintf("returns that closed a visit nobody had signed out (re_checkin + next_arrival, every site): %d", s.ReCheckin+s.NextArrival),
		fmt.Sprintf("  of which: missed sign-outs at text-message sites (R-4) %d; exempt returns at camp sites (R-5) %d; unclassified, site type not known at this post %d",
			s.MissedSignouts, s.ExemptCampReturns, s.UnclassifiedReturns),
		fmt.Sprintf("persons: %d distinct ids on visits -> %d after merge resolution (%d folded)",
			s.DistinctVisitPersonIDs, s.DistinctPersons, s.DistinctVisitPersonIDs-s.DistinctPersons),
		fmt.Sprintf("unattributable: %d visits with no person_id (%s), %d with no name snapshot (%s)",
			s.NoPersonID, pct(s.NoPersonID), s.NoNameSnapshot, pct(s.NoNameSnapshot)),
		fmt.Sprintf("time ends unresolvable (left blank, never guessed): %d", s.UnresolvableEnds),
		fmt.Sprintf("durations spanning a re-image (two epochs, labelled cross-epoch): %d", s.CrossEpoch),
	}
	if len(s.Streams) == 0 {
		out = append(out, "coverage: no replicated streams in this store")
	}
	for _, m := range s.Streams {
		out = append(out, fmt.Sprintf("coverage: %s@%d applied through seq %d", m.SiteID, m.NodeEpoch, m.AppliedSeq))
	}
	if len(s.TypedSites) == 0 {
		out = append(out, "site types known at this post: none — no row carries a verdict")
	} else {
		ids := make([]string, 0, len(s.TypedSites))
		for id := range s.TypedSites {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		parts := make([]string, 0, len(ids))
		for _, id := range ids {
			parts = append(parts, id+"="+string(s.TypedSites[id]))
		}
		out = append(out, "site types known at this post: "+strings.Join(parts, ", "))
	}
	if len(s.UntypedSites) > 0 {
		out = append(out, fmt.Sprintf("site types NOT known at this post (rows emitted with no verdict; run the report at that post for its verdicts): %s", strings.Join(s.UntypedSites, ", ")))
	}
	return out
}

// visitReplay is the per-site open set the folds keep in the visit table,
// rebuilt in memory as the stream is walked: one open visit per person (an
// arrival closes the prior one before opening), addressable by person for
// the dispatch and arrival closers and by visit for the checkout closer.
type visitReplay struct {
	site     string
	byPerson map[string]*VisitRecord
	byVisit  map[string]*VisitRecord
}

func newVisitReplay(site string) *visitReplay {
	return &visitReplay{site: site, byPerson: map[string]*VisitRecord{}, byVisit: map[string]*VisitRecord{}}
}

func (r *visitReplay) open(rec *VisitRecord) {
	r.byPerson[rec.VisitPersonID] = rec
	r.byVisit[rec.VisitID] = rec
}

func (r *visitReplay) closeRec(rec *VisitRecord, env store.Envelope, how VisitClosure) {
	rec.ClosedBy = how
	rec.CloseKey = env.Key()
	rec.close = basisOf(env)
	delete(r.byPerson, rec.VisitPersonID)
	delete(r.byVisit, rec.VisitID)
}

// openVisit is the ONE spelling of "env minted visit v at env.SiteID" for
// both minting events (trip_arrival's visits[] and orientation_completed's
// entry-on-completion visit): visit_open.go's derivation makes a later
// visit of the same person at the same site supersede the earlier one, so
// the person's still-open visit closes ClosedByNextArrival and the new one
// opens, keyed by the minting envelope — exactly as the fold's rows read
// back.
func (r *visitReplay) openVisit(env store.Envelope, v ArrivalVisit, tripRef *string, records *[]*VisitRecord) {
	if prev := r.byPerson[v.PersonID]; prev != nil {
		r.closeRec(prev, env, ClosedByNextArrival)
	}
	rec := &VisitRecord{
		SiteID:        env.SiteID,
		VisitID:       v.VisitID,
		VisitPersonID: v.PersonID,
		ArrivalKey:    env.Key(),
		ClosedBy:      VisitStillOpen,
		arrival:       basisOf(env),
	}
	if v.PersonName != nil {
		rec.PersonName = *v.PersonName
	}
	if tripRef != nil {
		rec.TripRef = *tripRef
	}
	*records = append(*records, rec)
	r.open(rec)
}

// apply mirrors the folds' visit effects for one event of this site's
// stream — the two that mint a visit (trip_arrival, orientation_completed
// carrying an entry) and the two that close one (trip_dispatched,
// visit_checked_out). Payloads that landed decoded for the fold, so a
// decode failure here is corruption and fails closed rather than skipping
// the event.
func (r *visitReplay) apply(env store.Envelope, records *[]*VisitRecord) error {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(env.Payload, &head); err != nil {
		return fmt.Errorf("event %v: %w", env.Key(), err)
	}
	switch head.Type {
	case "trip_arrival":
		var p TripArrival
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return fmt.Errorf("event %v: %w", env.Key(), err)
		}
		for _, v := range p.Visits {
			r.openVisit(env, v, p.TripRef, records)
		}
	case evtOrientationCompleted:
		// ⟦H-8b / H-8e, 2026-09-30⟧ ENTRY-ON-COMPLETION MINTS A VISIT TOO.
		// applyOrientationCompleted (orientation.go, SchemaVersion 12 / R-16)
		// materialises the completion's carried `visit` through the very
		// same mintVisitFromArrivalVisit trip_arrival's fold uses — but this
		// replay only ever mirrored trip_arrival, so a person a completion
		// entered off the arrival that held them out stood on the muster
		// (the visit table) and on /gate, and was missing from this report
		// and from BuildAnalytics, which reuses it: the persons tile, the
		// site card and `-report visits` were each short by exactly that
		// visit at every post (L11 H-8b at ndb, H-8e at ice-pad). Pre-
		// existing since R-16 — MEASURED on 9a2fb30, before A3 — not a
		// regression of the single-transaction commits. Mirrored exactly as
		// the fold does it: the visit's person is FORCED to the completion's
		// own PersonID (the fold's R-9 guard), trip_id is the carried
		// TripRef, and the completion's own envelope is the arrival end —
		// the same instant VisitSignInTimes' W4 fix shows on the roster. A
		// bare completion (bulk marking / backfill) carries no visit and
		// touches nothing here.
		var p OrientationCompleted
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return fmt.Errorf("event %v: %w", env.Key(), err)
		}
		if p.Visit == nil {
			return nil
		}
		v := *p.Visit
		v.PersonID = p.PersonID
		r.openVisit(env, v, p.TripRef, records)
	case "trip_dispatched":
		var p TripDispatched
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return fmt.Errorf("event %v: %w", env.Key(), err)
		}
		how := ClosedByDispatch
		// James, 2026-10-04 (R5): "Road work counts as a departure." A
		// road-work run (road_work.go) is destination-less like the
		// degenerate local trip, but it is equipment leaving to work on the
		// road — the same arm visit_closed.go's closeOf already gives it.
		// ⟦was: destination-less ⇒ re_checkin, so every road-work crew was
		// a "missed sign-out" in -report visits / signout-failures and left
		// out of the analytics averages (FINDINGS-R4 D9)⟧
		if p.DestinationSite == nil && p.DestinationUncontrolled == nil && !p.RoadWork {
			how = ClosedByReCheckin // the degenerate local trip: a check-in's first half
		}
		// trip.go applyTripDispatched: the driver is always an occupant.
		for _, id := range append([]string{p.DriverPersonID}, p.OccupantPersonIDs...) {
			if prev := r.byPerson[id]; prev != nil {
				r.closeRec(prev, env, how)
			}
		}
	case "visit_checked_out":
		var p VisitCheckedOut
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return fmt.Errorf("event %v: %w", env.Key(), err)
		}
		// checkin.go applyVisitCheckedOut: by visit_id, this site, if open.
		if prev := r.byVisit[p.VisitID]; prev != nil {
			r.closeRec(prev, env, ClosedByCheckout)
		}
	}
	return nil
}

// VisitReport derives the report from this store's whole event_log — own
// site and every replicated peer stream alike. Read-only; it authors nothing.
// types is what the caller knows about site types (a post: its own, from
// config); nil is legal and means no row carries a verdict.
//
// Linear in the log: one EachEvent walk per stream (Watermarks is the
// census), a map lookup per event, then one classification per visit end.
// Only an end that some occurred_at_corrected targets pays for the resolver.
//
// FIX17 (perf): the replay is reused between writes (store.Memo) — at season
// scale it is ~40k events decoded per call, and the shift panel on every
// gate and status page called it. Keyed by the store's head AND by types (a
// post's own type map and a nil map give different verdicts). Each caller
// gets its own record slice and summary slices.
func VisitReport(st *store.Store, types SiteTypes) ([]VisitRecord, VisitReportSummary, error) {
	recs, sum, err := visitReportShared(st, types)
	if err != nil {
		return nil, VisitReportSummary{}, err
	}
	sum.TypedSites = types
	sum.UntypedSites = slices.Clone(sum.UntypedSites)
	sum.Streams = slices.Clone(sum.Streams)
	return slices.Clone(recs), sum, nil
}

// visitReportShared is VisitReport WITHOUT the caller's own copy: the
// memoised records themselves, which the caller must only read. FIX17b
// (memory): the analytics basis keeps the records it reads for as long as
// it is memoised, and a private copy of a season's records (~6 MB at
// staging scale) beside the memo's own was pure duplication.
func visitReportShared(st *store.Store, types SiteTypes) ([]VisitRecord, VisitReportSummary, error) {
	type built struct {
		recs []VisitRecord
		sum  VisitReportSummary
	}
	v, err := st.Memo("domain.VisitReport"+types.memoKey(), func() (any, error) {
		recs, sum, err := visitReport(st, types)
		return built{recs, sum}, err
	})
	if err != nil {
		return nil, VisitReportSummary{}, err
	}
	b := v.(built)
	return b.recs, b.sum, nil
}

// memoKey spells a site-type map for a memo key: sorted, so two maps with
// the same content share a key; "" for an empty or nil map.
func (t SiteTypes) memoKey() string {
	ids := make([]string, 0, len(t))
	for id := range t {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&b, "|%q=%q", id, string(t[id]))
	}
	return b.String()
}

func visitReport(st *store.Store, types SiteTypes) ([]VisitRecord, VisitReportSummary, error) {
	fail := func(err error) ([]VisitRecord, VisitReportSummary, error) {
		return nil, VisitReportSummary{}, fmt.Errorf("domain: visit report: %w", err)
	}
	// Streams arrive in (site_id, node_epoch) order, so a site's epochs are
	// walked consecutively and epoch-major (§8.3): the open set persists
	// across a re-image exactly as the visit table does. FIX17: the walk is
	// the log follower's (log_follow.go) — the same per-site replay, resumed
	// from where it last stopped instead of restarted at seq 1 — and the
	// records come back as this call's own copies.
	snap, err := followLog(st, true)
	if err != nil {
		return fail(err)
	}
	streams, records := snap.streams, snap.records
	var summary VisitReportSummary
	summary.TypedSites = types
	untyped := map[string]bool{}
	for _, m := range streams {
		summary.Streams = append(summary.Streams, StreamMark{SiteID: m.SiteID, NodeEpoch: m.NodeEpoch, AppliedSeq: m.AppliedSeq})
		if _, known := types[m.SiteID]; !known && !untyped[m.SiteID] {
			untyped[m.SiteID] = true
			summary.UntypedSites = append(summary.UntypedSites, m.SiteID)
		}
	}

	corrected, err := correctedInstants(st)
	if err != nil {
		return fail(err)
	}
	merge, err := LoadPersonMergeMap(st)
	if err != nil {
		return fail(err)
	}

	out := make([]VisitRecord, 0, len(records))
	rawIDs := map[string]bool{}
	canonIDs := map[string]bool{}
	for _, rec := range records {
		rec.PersonID = merge.Resolve(rec.VisitPersonID)
		if rec.VisitPersonID == "" {
			summary.NoPersonID++
		} else {
			rawIDs[rec.VisitPersonID] = true
			canonIDs[rec.PersonID] = true
		}
		if rec.PersonName == "" {
			summary.NoNameSnapshot++
		}

		arrivedMS, arrivalLabel, arrivedOK := reportInstant(rec.ArrivalKey, rec.arrival, corrected)
		rec.ArrivedAtMS, rec.ArrivalLabel = arrivedMS, arrivalLabel
		if !arrivedOK {
			summary.UnresolvableEnds++
		}

		if rec.ClosedBy != VisitStillOpen {
			closedMS, closeLabel, closedOK := reportInstant(rec.CloseKey, rec.close, corrected)
			rec.ClosedAtMS, rec.CloseLabel = closedMS, closeLabel
			if !closedOK {
				summary.UnresolvableEnds++
			}
			rec.DurationLabel = arrivalLabel
			if timeLabelRank(closeLabel) > timeLabelRank(arrivalLabel) {
				rec.DurationLabel = closeLabel
			}
			if rec.ArrivalKey.NodeEpoch != rec.CloseKey.NodeEpoch {
				// Two incarnations of the post, possibly two clocks: the
				// same-clock justification stops at the re-image.
				rec.DurationLabel = timeCrossEpoch
				summary.CrossEpoch++
			}
			if arrivedOK && closedOK {
				span := (closedMS - arrivedMS) / int64(time.Minute/time.Millisecond)
				rec.RecordOpenMinutes = &span
				if rec.ClosedBy.DepartureRecorded() {
					onSite := span
					rec.OnSiteMinutes = &onSite
					rec.DurationBucket = durationBucket(onSite)
				}
			}
		}

		switch rec.ClosedBy {
		case ClosedByCheckout:
			summary.CheckedOut++
		case ClosedByDispatch:
			summary.Dispatched++
		case ClosedByReCheckin:
			summary.ReCheckin++
		case ClosedByNextArrival:
			summary.NextArrival++
		default:
			summary.Open++
		}
		// The R-4/R-5 verdict — the closure above was derived type-blind;
		// only now does the site's type, as this post knows it, enter.
		rec.SiteType = types[rec.SiteID]
		switch classifyReturn(rec.ClosedBy, rec.SiteType) {
		case verdictMissedSignout:
			rec.Failure = FailureMissedSignout
			summary.MissedSignouts++
		case verdictExemptCamp:
			summary.ExemptCampReturns++
		case verdictUnclassified:
			summary.UnclassifiedReturns++
		}
		out = append(out, *rec)
	}
	sort.Strings(summary.UntypedSites)
	summary.Visits = len(out)
	summary.DistinctVisitPersonIDs = len(rawIDs)
	summary.DistinctPersons = len(canonIDs)
	return out, summary, nil
}

// correctedInstant is one corrected target's resolution, taken before any
// replay: ok=false means ResolveOccurredAt failed closed (the instant is
// left blank); corrected=false means the key is targeted but nothing is
// effective, so its envelope pair stands.
type correctedInstant struct {
	ms        int64
	corrected bool
	ok        bool
}

// correctedInstants resolves every event key that at least one
// occurred_at_corrected directly targets — read off the correction side
// table with the same decoder ResolveOccurredAt's chain walk uses, so the
// two cannot disagree about what counts as a target. A key outside this map
// has an empty chain by construction (a correction-of-correction still
// targets a correction that targets the original), so its envelope value
// stands and no resolver call is owed.
//
// ⟦D2 / AN-PANIC-1, 2026-10-04⟧ The resolutions are made HERE, up front,
// and reportInstant no longer takes the store at all. ⟦was: a set of
// targeted keys, with reportInstant calling ResolveOccurredAt per key⟧ —
// and analytics calls reportInstant inside st.EachEvent's callback, where
// the read cursor holds the store's single connection: the resolver's
// LocalTx re-entered it, so ONE time correction anywhere made /analytics
// and every /analytics/site/* panic with REENTRANCY after a 10 s stall,
// stalling /muster behind it (FINDINGS-R4 D2). With the store out of
// reportInstant's signature, no caller can reopen that path.
func correctedInstants(st *store.Store) (map[store.Key]correctedInstant, error) {
	rows, err := queryCorrections(st, occurredAtCorrectionSelect+` WHERE target_site_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	out := make(map[store.Key]correctedInstant, len(rows))
	for _, r := range rows {
		if !r.TargetOK {
			continue
		}
		if _, done := out[r.Target]; done {
			continue
		}
		rt, _, err := ResolveOccurredAt(st, r.Target)
		out[r.Target] = correctedInstant{ms: rt.InstantUTCms, corrected: rt.Corrected, ok: err == nil}
	}
	return out, nil
}

// reportInstant is the one place a report instant is produced. An
// uncorrected end classifies straight from the envelope basis captured in
// the replay (ProvenanceClass + DisplayTimeClass, the one-place helpers); a
// targeted end takes ResolveOccurredAt's effective value, preloaded by
// correctedInstants. It touches no store, so it is safe inside an
// EachEvent callback. ok=false means the resolver failed closed — the
// caller leaves the instant blank.
func reportInstant(k store.Key, b timeBasis, corrected map[store.Key]correctedInstant) (ms int64, label string, ok bool) {
	if c, targeted := corrected[k]; targeted {
		if !c.ok {
			return 0, timeUnresolvable, false
		}
		if c.corrected {
			// TP §7.4's substitution cell: a corrected instant is the
			// officer's typed claim, DisplayClaim under either clock column.
			return c.ms, timeClaimedCorrected, true
		}
		// Targeted but nothing effective (every correction cleared or
		// superseded to nothing): the envelope pair stands, as below.
	}
	p := ProvenanceClass(b.source, b.atMS, b.wallMS)
	switch DisplayTimeClass(p, b.state) {
	case DisplayObservation:
		return b.atMS, timeObserved, true
	case DisplayObservationClockUntrusted:
		return b.atMS, timeObservedClockBad, true
	case DisplayClaim:
		return b.atMS, timeClaimed, true
	default:
		return b.atMS, timeUntrusted, true
	}
}

// durationBucket puts the bound on the label (PLAN-SEC-D §1.4). Minutes in.
func durationBucket(min int64) string {
	const (
		hour = 60
		day  = 24 * hour
	)
	switch {
	case min < 0:
		return "negative"
	case min < 8*hour:
		return "under 8h"
	case min < day:
		return "8h to 24h"
	case min < 7*day:
		return "1d to 7d"
	case min < 30*day:
		return "7d to 30d"
	default:
		return "over 30d"
	}
}

// VisitReportColumns is the CSV header, in order. Fixed: a consumer that
// reads by position must not be broken by a silent reorder — which is why
// the two R-4/R-5 columns were APPENDED, never inserted.
var VisitReportColumns = []string{
	"site_id", "visit_id", "person_id", "visit_person_id", "person_name", "trip_ref",
	"arrival_key", "arrived_at_utc", "arrival_time_label",
	"closed_by", "departure_recorded", "close_key", "closed_at_utc", "close_time_label",
	"on_site_min", "record_open_min", "duration_label", "duration_bucket",
	"site_type", "failure",
}

// WriteVisitReportCSV writes the header and one row per record. Instants
// are RFC 3339 UTC to the second; an unresolved or open end is blank.
func WriteVisitReportCSV(w io.Writer, recs []VisitRecord) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(VisitReportColumns); err != nil {
		return err
	}
	for _, r := range recs {
		if err := cw.Write(r.csvRow()); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func (r VisitRecord) csvRow() []string {
	utc := func(ms int64, label string) string {
		if label == timeUnresolvable || label == "" {
			return ""
		}
		return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05Z")
	}
	mins := func(p *int64) string {
		if p == nil {
			return ""
		}
		return strconv.FormatInt(*p, 10)
	}
	closeKey, departure := "", ""
	if r.ClosedBy != VisitStillOpen {
		closeKey = keyString(r.CloseKey)
		departure = "no"
		if r.ClosedBy.DepartureRecorded() {
			departure = "yes"
		}
	}
	return []string{
		r.SiteID, r.VisitID, r.PersonID, r.VisitPersonID, r.PersonName, r.TripRef,
		keyString(r.ArrivalKey), utc(r.ArrivedAtMS, r.ArrivalLabel), r.ArrivalLabel,
		string(r.ClosedBy), departure, closeKey, utc(r.ClosedAtMS, r.CloseLabel), r.CloseLabel,
		mins(r.OnSiteMinutes), mins(r.RecordOpenMinutes), r.DurationLabel, r.DurationBucket,
		string(r.SiteType), r.Failure,
	}
}

// PersonSignoutFailures is one row of the per-person roll-up R-4 asks for
// ("track the times that the individual has failed to sign themselves
// out"): the visit report folded onto the merge-resolved person. The two
// could-not-judge columns sit beside the failure count on purpose — a row
// reading 0 failures and 4 unclassified returns is a person whose site this
// post does not know, not a clean record.
type PersonSignoutFailures struct {
	PersonID string
	// PersonName is the first name snapshot seen for this person in log
	// order — a merged duplicate's spelling may differ from the roster's.
	PersonName string
	Visits     int

	MissedSignouts int
	// MissedSignoutSites lists where the failures happened, sorted — R-4 is
	// per person ACROSS sites, and one post's store holds every peer's log.
	MissedSignoutSites  []string
	ExemptCampReturns   int
	UnclassifiedReturns int
}

// SignoutFailuresByPerson folds the visit report onto persons: most failures
// first, then person id, so the top of the file is the list the owner asked
// for. Visits with no person_id are dropped here (the visit summary's
// unattributable line still counts them): a failure nobody can be named for
// is not somebody's failure. Persons are keyed by the MERGE-RESOLVED id, so
// a human who holds two records is one row — and the count is wrong by the
// duplicate rate until the officer links them (R-10 is what keeps that rate
// down; this fold only honours the links that exist).
func SignoutFailuresByPerson(recs []VisitRecord) []PersonSignoutFailures {
	by := map[string]*PersonSignoutFailures{}
	sites := map[string]map[string]bool{}
	for _, r := range recs {
		if r.PersonID == "" {
			continue
		}
		p := by[r.PersonID]
		if p == nil {
			p = &PersonSignoutFailures{PersonID: r.PersonID}
			by[r.PersonID] = p
		}
		if p.PersonName == "" {
			p.PersonName = r.PersonName
		}
		p.Visits++
		switch classifyReturn(r.ClosedBy, r.SiteType) {
		case verdictMissedSignout:
			p.MissedSignouts++
			if sites[r.PersonID] == nil {
				sites[r.PersonID] = map[string]bool{}
			}
			sites[r.PersonID][r.SiteID] = true
		case verdictExemptCamp:
			p.ExemptCampReturns++
		case verdictUnclassified:
			p.UnclassifiedReturns++
		}
	}
	out := make([]PersonSignoutFailures, 0, len(by))
	for id, p := range by {
		for s := range sites[id] {
			p.MissedSignoutSites = append(p.MissedSignoutSites, s)
		}
		sort.Strings(p.MissedSignoutSites)
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MissedSignouts != out[j].MissedSignouts {
			return out[i].MissedSignouts > out[j].MissedSignouts
		}
		return out[i].PersonID < out[j].PersonID
	})
	return out
}

// PersonSignoutFailureColumns is the per-person CSV header, in order. Fixed,
// for the same reason VisitReportColumns is.
var PersonSignoutFailureColumns = []string{
	"person_id", "person_name", "visits",
	"missed_signouts", "missed_signout_sites", "exempt_camp_returns", "unclassified_returns",
}

// WritePersonSignoutFailuresCSV writes the header and one row per person.
// Sites are joined with a space inside the one cell (site ids carry none).
func WritePersonSignoutFailuresCSV(w io.Writer, rows []PersonSignoutFailures) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(PersonSignoutFailureColumns); err != nil {
		return err
	}
	for _, p := range rows {
		if err := cw.Write([]string{
			p.PersonID, p.PersonName, strconv.Itoa(p.Visits),
			strconv.Itoa(p.MissedSignouts), strings.Join(p.MissedSignoutSites, " "),
			strconv.Itoa(p.ExemptCampReturns), strconv.Itoa(p.UnclassifiedReturns),
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// keyString renders a log key as site:epoch:seq — compact, sortable within
// a stream, and copy-pasteable into an audit query.
func keyString(k store.Key) string {
	return k.SiteID + ":" + strconv.FormatInt(k.NodeEpoch, 10) + ":" + strconv.FormatInt(k.Seq, 10)
}
