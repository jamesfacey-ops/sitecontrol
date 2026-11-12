// ownership.go — the D-87 choke point (JAMES-DECISIONS.md D-87, 2026-09-05:
// "read it all but modify only the individual site's data"; D-89: the rule
// covers PRESENCE data — visits, check-ins, check-outs, arrivals — and
// person / company / trade / vehicle records stay SHARED).
//
// ONE database per post holding every site's rows; every row carries the tag
// of the site that entered it; a site READS everything and MODIFIES only
// rows carrying its own tag. The tag already exists (every event carries its
// authoring site_id, every parent projection table a site_id / origin_site
// column); what was missing is anything reading it as a permission. D-87
// rules the shape of the fix: NOT a check scattered across write paths —
// "one choke point that refuses any write whose tag is not this site, plus
// a witness that watches the refusal fire — so a second write path added
// later fails the test rather than slipping past."
//
// WHERE IT SITS, AND WHY NOWHERE ELSE. Author (log.go) is the sole producer
// of own-authored events — insertAndFold's INSERT is the only event_log
// write in the module, *sql.DB is unexported, and insertAndFold is reached
// from exactly two places: Author and applyBatch. The check lives in Author,
// AFTER Validate and BEFORE insertAndFold, inside the authoring transaction.
// ⛔ It must never move into insertAndFold, applyBatch, or any fold: those
// paths also carry PEER events and this post's own replayed history (the
// self-history rejoin), and a refusal there would make two posts hold
// different state from the same bytes — the fold never refuses (W3-2,
// person_amend.go), and a rejoin must replay what was once accepted. Rule 4
// constrains what THIS post AUTHORS, never what it ACCEPTS.
//
// WHAT THE FUNNEL KNOWS, AND HOW. Author receives (siteID, payload) and
// cannot tell, from bytes, which row a payload modifies — nor whether a
// foreign id inside it is a TARGET or a REFERENCE (an arrival's trip_ref, a
// dispatch's occupants, an ack's trip_id all legitimately name another
// site's rows). store imports no sibling package, so that knowledge is
// DECLARED by the package that owns the event type and handed in here as an
// OwnerResolver, keyed by the payload's `type` — the same seam projections
// use (RegisterProjection). A type with no resolver is not gated: it mints
// its own row, or it is shared reference data under D-89, or it is open and
// not yet ruled. The closed set of gated types is domain's to declare
// (domain/ownership.go) and its census test's to police; this file supplies
// the mechanism and the refusal.
//
// G13 (James, 2026-09-08): FAILS CLOSED, not just on the foreign — on the
// UNCLASSIFIED. Before G13 "a type with no resolver is not gated" covered
// two different situations with one silent pass: a type someone actually
// classified as ungated (sharedReference/ownMinted/unruledOpen), and a type
// NOBODY classified at all — a new authored type landed on a branch that
// never touched this file. Only the census (a test) caught the second case,
// and the census had already leaked six times (D87-CENSUS-CLOSE.md); two
// orientation types once merged ungated and untested. RegisterAcknowledged
// (below) is domain's way of handing over the REST of its closed set — not
// just the gated four — so the funnel itself, not a test someone has to
// remember to run, refuses anything absent from the whole table.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
)

// OwnerResolver answers, for ONE event type, "which existing row does this
// payload modify, and whose tag does that row carry?" It runs inside the
// authoring transaction so the row it reads is the row the fold would touch.
//
//   - target: the id of the row the payload names (for the refusal sentence).
//   - ownerSiteID: the tag on that row; "" when no such row is known at this
//     post, which is refused too — a row this post cannot show is its own is
//     not its own. Own-site targets always exist by construction (this post's
//     own folds ran synchronously when it authored them).
//   - err: a read failure, surfaced as such — never as a refusal.
type OwnerResolver func(tx *sql.Tx, payload json.RawMessage) (target, ownerSiteID string, err error)

// OwnershipRule is the one sentence every D-87 refusal carries, pinned so a
// surface (and a test) can name the SPECIFIC refusal rather than a status.
// It is the officer-visible text, so it carries no decision id: the rule is
// [[D-87]], cited here and at each gate, never on the page. ⟦was: "a site
// modifies only its own site's data (D-87)" — FIX5 (R7 post-deploy backlog)⟧
const OwnershipRule = "a site modifies only its own site's data"

// OwnershipError is the typed pre-append refusal. Nothing was written.
// Match it with errors.As — every edge wraps it with %w, and text matching
// on a wrapped error is exactly the trap a lost %w hides.
type OwnershipError struct {
	EventType    string // the payload's `type`
	Target       string // the id the payload names
	TargetSiteID string // the tag on that row; "" = no such row is known here
	SiteID       string // the post that tried to author
}

func (e *OwnershipError) Error() string {
	if e.TargetSiteID == "" {
		return fmt.Sprintf("store: author: refused — %s names %s, but no such record is known at this post, so it cannot be shown to be %s's; %s",
			e.EventType, e.Target, e.SiteID, OwnershipRule)
	}
	return fmt.Sprintf("store: author: refused — %s would modify %s's record %s; this post is %s, and %s",
		e.EventType, e.TargetSiteID, e.Target, e.SiteID, OwnershipRule)
}

// RegisterOwnership declares eventType as GATED: every Author of a payload
// of that type is first put to r, and refused with *OwnershipError unless
// the row it names carries the authoring post's own tag. Startup-time only,
// like RegisterProjection: registration is not safe concurrently with
// Author. A second registration for the same type is refused — two
// resolvers for one type would mean two opinions about which row it
// modifies. So is registering a type already declared RegisterAcknowledged
// (the reverse of that guard, added G13 coverage close 2026-09-08): two
// opinions about whether a type needs a check AT ALL is the same defect one
// level up, and it is what keeps owners and acknowledged PROVABLY disjoint
// — refuseForeignRow's owners-then-acknowledged check order therefore
// cannot matter for any reachable state; a type is in exactly one of the
// two maps, or neither.
func (s *Store) RegisterOwnership(eventType string, r OwnerResolver) error {
	if eventType == "" || r == nil {
		return fmt.Errorf("store: ownership registration needs an event type and a resolver (got %q, nil=%v)", eventType, r == nil)
	}
	if _, dup := s.owners[eventType]; dup {
		return fmt.Errorf("store: ownership for %q already registered", eventType)
	}
	if s.acknowledged[eventType] {
		return fmt.Errorf("store: %q is already registered as an acknowledged (ungated) type", eventType)
	}
	if s.owners == nil {
		s.owners = make(map[string]OwnerResolver)
	}
	s.owners[eventType] = r
	return nil
}

// OwnershipGatedTypes lists the registered (gated) event types, sorted — the
// census's view of the closed set, so a declared presence type with no
// resolver, or a resolver for a type nobody declared, is a loud mismatch
// rather than a silent gap.
func (s *Store) OwnershipGatedTypes() []string {
	out := make([]string, 0, len(s.owners))
	for typ := range s.owners {
		out = append(out, typ)
	}
	sort.Strings(out)
	return out
}

// RegisterAcknowledged declares eventType as CLASSIFIED but NOT gated: the
// package that owns the payload has put it in one of shared reference,
// mints-own-row, or unruled-open, and there is nothing for a resolver to
// check. Startup-time only, like RegisterOwnership. Once ANYTHING has been
// registered — here or via RegisterOwnership — refuseForeignRow fails
// closed on any authored type in NEITHER map (G13): acknowledging a type is
// what tells the funnel "this one was considered", as opposed to "nobody
// has said what this is yet".
func (s *Store) RegisterAcknowledged(eventType string) error {
	if eventType == "" {
		return fmt.Errorf("store: acknowledged-type registration needs an event type")
	}
	if _, gated := s.owners[eventType]; gated {
		return fmt.Errorf("store: %q is already registered as a gated (ownership) type", eventType)
	}
	if s.acknowledged == nil {
		s.acknowledged = make(map[string]bool)
	}
	if s.acknowledged[eventType] {
		return fmt.Errorf("store: %q is already acknowledged", eventType)
	}
	s.acknowledged[eventType] = true
	return nil
}

// UnregisteredTypeError is G13's fail-closed refusal: an authored type
// absent from BOTH the gated set and the acknowledged set. Nothing was
// written — refuseForeignRow runs pre-append, same as *OwnershipError.
// Deliberately a DIFFERENT type from *OwnershipError: this names an absence
// of classification, not a foreign tag, so a caller (and a test) can tell
// "nobody has said what this type is" from "you don't own that row".
type UnregisteredTypeError struct {
	EventType string
	SiteID    string
}

func (e *UnregisteredTypeError) Error() string {
	return fmt.Sprintf("store: author: refused — %q is not a registered authored type at this post "+
		"(neither gated nor acknowledged as ungated); %s cannot author it until it is classified (G13)",
		e.EventType, e.SiteID)
}

// refuseForeignRow is the check itself, called from Author and from nowhere
// else. The payload has already passed Validate (valid JSON).
//
// G13: if NOTHING has ever been registered (neither owners nor
// acknowledged) the mechanism has not been wired up at all — most of this
// package's own tests, and every caller that never reached
// domain.Register — and every payload passes untouched, exactly as before
// G13. The instant anything IS registered, the funnel is live and fails
// CLOSED: a `type` present in owners is checked against its row's tag as
// always; a `type` present in acknowledged passes (it was classified as
// not needing a check); a `type` in NEITHER — absent from domain's whole
// authoredEventOwnership table, not merely ungated — is refused, loudly,
// naming the type. A payload with no `type` at all is not this mechanism's
// concern (Validate has already accepted it as valid JSON; nothing here
// widens what Validate requires).
func (s *Store) refuseForeignRow(tx *sql.Tx, siteID string, payload json.RawMessage) error {
	if len(s.owners) == 0 && len(s.acknowledged) == 0 {
		return nil
	}
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(payload, &head); err != nil || head.Type == "" {
		return nil
	}
	if resolve, gated := s.owners[head.Type]; gated {
		target, owner, err := resolve(tx, payload)
		if err != nil {
			return fmt.Errorf("store: author: ownership of %s: %w", head.Type, err)
		}
		if owner != siteID {
			return &OwnershipError{EventType: head.Type, Target: target, TargetSiteID: owner, SiteID: siteID}
		}
		return nil
	}
	if s.acknowledged[head.Type] {
		return nil
	}
	return &UnregisteredTypeError{EventType: head.Type, SiteID: siteID}
}
