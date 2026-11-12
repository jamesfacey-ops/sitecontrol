// muster.go — the muster view and print (design §11, chunk-plan C9): open
// visits grouped Company then Trade, with the numeric MusterMinGroupSize
// fold and the live-vs-reprint asymmetry named in §8.8-iv.
//
// This is a READ-ONLY view over projections C5/C6 already write (Visit,
// Company, Trade, the alias map). It authors no new event type, adds no
// table, and needs no store.SchemaVersion bump — see the C9 build report.
//
// THE JET REGRESSION (named in the chunk brief, carried forward from the
// Access system this project replaces): Nz() made JET compare crew sizes AS
// TEXT, so "13" < "2" evaluated true and a 13-person crew was silently
// folded onto the shared summary sheet. Every size comparison in this file
// is a Go int comparison (len(...) < minGroupSize) — never a formatted
// string — so that failure mode cannot recur here. See muster_test.go's
// TestMusterFoldingIsNumericNotTextual for the executable guard.
//
// THE LIVE-VS-REPRINT ASYMMETRY (design §8.8-iv / §11): the live board
// resolves a visit's company/trade through the alias read path (§8.8-ii)
// so a later merge shows one crew instead of two; a reprint of an
// already-printed sheet must keep showing what was true when it was printed,
// never re-resolve. MusterLive and MusterReprint below encode this as a
// TYPE-LEVEL guarantee, not a runtime flag: MusterReprint's signature has no
// AliasMap parameter at all, so it is structurally incapable of consulting
// the alias table no matter when or how many times it is called. That is
// also what makes "reprint" free: a visit's snapshot fields never change
// after arrival (§8.8-i), so calling MusterReprint again later on the same
// visit rows reproduces byte-identical grouping — no separate "what did we
// print" record needs to exist anywhere.
package domain

import (
	"fmt"
	"sort"
	"strings"
)

// MusterMember is one open visit rendered as a muster-sheet line.
type MusterMember struct {
	VisitID     string
	PersonID    string
	PersonName  string
	CompanyName string // as resolved/labelled for THIS board (live or snapshot) — carried onto the flat Summary bucket so a folded crew is still auditable by company/trade
	TradeName   string
}

// NoTradeRecorded is THE spelling of the no-trade idea, and there is exactly
// one of it in this program.
//
// ⚠⚠ IT IS A SHARED EXPORTED CONST BECAUSE THE DIVERGENCE IT REPLACES WAS
// REAL AND HAD TWO SPELLINGS OF ONE IDEA — a parenthesised one on the muster
// board and a title-cased one on the pickers, so the same absence read
// differently on two screens an officer uses in the same minute. ⚠ A rename
// applied to two spellings produces two new ones, which is why W8 unifies
// rather than renames: W8-22(ii) asserts the SET of "no X recorded" spellings
// for this entity is exactly ONE, derived from production Go rather than
// listed.
//
// ⚠ RECORDED AND DELIBERATELY NOT FIXED: the COMPANY idea has the same
// divergence, one entity over — "(no company recorded)" here and "No company
// recorded" in the picker. It is not W8's and nothing here touches it.
const NoTradeRecorded = "(no trade recorded)"

// TradeGroup is one trade's crew within a company.
type TradeGroup struct {
	TradeID   string // "" = no trade on file for anyone in this bucket
	TradeName string
	Members   []MusterMember
}

// CompanyGroup is one company section of the muster (design §11: "grouped
// Company then Trade").
type CompanyGroup struct {
	CompanyID   string // "" = no company on file
	CompanyName string
	Trades      []TradeGroup
}

// MusterBoard is a fully built muster: normal Company→Trade groups plus
// ONE flat summary bucket for crews below MusterMinGroupSize (design §11
// speaks of "the summary group," singular — sub-threshold crews are not
// each given their own miniature section, they are pooled together so the
// printed sheet does not become a wall of one- and two-line groups).
type MusterBoard struct {
	Companies []CompanyGroup
	Summary   []MusterMember
	Total     int
}

// VisitIDs collects every visit id appearing anywhere on the board, in
// stable (grouped, then summary) order. The web layer uses this to freeze
// "this sheet" into a print URL (chunk-plan C9's browser-print, no-client-
// storage mechanism — see internal/web/muster.go).
func (b MusterBoard) VisitIDs() []string {
	var ids []string
	for _, c := range b.Companies {
		for _, w := range c.Trades {
			for _, m := range w.Members {
				ids = append(ids, m.VisitID)
			}
		}
	}
	for _, m := range b.Summary {
		ids = append(ids, m.VisitID)
	}
	return ids
}

// resolvedRef is what a resolver produces for one visit: the id/label pair
// actually used to group and label, for company and trade independently —
// plus, as of W6 (item 5), the PERSON label the sheet prints.
//
// ⟦W6/DR-W6-5, carrying SCHEMA10-ARBITER-VERDICT.md adjudication #7 (RULED,
// closed)⟧ The person label moved OUT of buildMuster's shared block and INTO
// this struct because the two boards must now answer DIFFERENTLY, and resolve
// is the only thing that differs between them:
//
//	MusterLive     live Person.Name -> raw PersonID              (NO snapshot arm)
//	MusterReprint  snapshot -> live Person.Name -> raw PersonID
//
// The sorts below key on MusterMember.PersonName, i.e. on this label, so a
// frozen name gives a frozen ORDER without a second mechanism — adjudication
// #7's third bullet, and the reason the render and the comparator may never
// read different strings.
//
// ⚠ ⟦W6 / build-critique F-5⟧ THE FIRST TWO PAIRS ARE PER BUCKET; personLabel
// IS PER VISIT, AND THE TWO ARE MIXED IN ONE STRUCT. buildMuster stores a whole
// resolvedRef on the bucket — `b = &bucket{ref: ref}` — taken from the FIRST
// visit that opens it, and then reads b.ref.companyID/.companyLabel/
// .tradeID/.tradeLabel to NAME the group. Those four are bucket-wide by
// construction, because the bucket key is built from the ids. personLabel is
// not: b.ref.personLabel holds ONE ARBITRARY MEMBER'S NAME — whoever arrived
// first — and it MEANS NOTHING. Nothing reads it today and nothing may: the
// per-member label is MusterMember.PersonName, set per visit in the same loop.
// Recorded here rather than in a report because this doc comment is what a
// reader consults before reaching for b.ref.personLabel to label a trade,
// which is one autocomplete away and would be wrong on every crew of more than one.
type resolvedRef struct {
	companyID, companyLabel string
	tradeID, tradeLabel     string
	// ⚠ PER VISIT, unlike the four above. See the warning in the doc comment:
	// bucket.ref.personLabel is an arbitrary member's name and is meaningless.
	personLabel string
}

// personLabel is the empty-as-ABSENT preference rule, shared by both
// closures so they cannot drift apart in the part they agree on.
//
// ⟦DR-W6-3⟧ A candidate that is the empty string is ABSENT and falls through,
// exactly as ⟦W5d-22(d)⟧ already rules for a cleared company name on this
// same board: "indexed as ABSENT rather than as a present blank … never to an
// empty heading during a roll call." NOTHING TRIMS — a whitespace-only name
// renders as written. That cost is real and it is accepted deliberately: a
// silent TrimSpace at a life-safety render, introduced under cover of a
// different change, would differ from the arm this file has always had and
// from the company rule beside it, and nobody would have decided it.
func personLabel(candidates ...string) string {
	for _, c := range candidates {
		if c != "" {
			return c
		}
	}
	return ""
}

// MusterLive computes the live muster board: each visit's company/trade
// is resolved before grouping, so linking two duplicate rows merges their
// crews into one group — reflecting what is true NOW. The display label for
// a resolved id prefers the CURRENT name from companies/trades (a
// canonical row may have been renamed since some visits were snapshotted);
// if the resolved id is not found there (e.g. a merge pointing at an id this
// store has not replicated yet), it falls back to the row's own snapshot
// name, and failing that to the raw id rather than showing a blank.
//
// ⟦W5d, DR-4⟧ THE TWO RESOLVERS ARE SEPARATE PARAMETERS, and that is the
// whole point of the signature rather than a style choice. COMPANY resolves
// through the composed `company_amended.same_as` snapshot
// (LoadCompanyMergeMap) as of W5d — W3-4 exempted this grouping only
// because its composed source did not exist yet, and W5d is the chunk that
// makes the exemption expire. ⟦W8⟧ TRADE now resolves through its OWN
// composed `trade_amended.same_as` snapshot (LoadTradeMergeMap); the shared
// legacy table both used to ride is deleted, on the schedule stated once at
// P-3 §5 and not restated here. The two must be able to DISAGREE; one shared
// parameter cannot express that, and a build passing the same map to both
// satisfies every structural assertion while being exactly the failure
// (which is why W5d-6's guard is behavioural).
//
// ⚠ MusterReprint below takes NEITHER map, by design. Do not "helpfully"
// give it one.
func MusterLive(visits []Visit, persons []Person, companies []Company, trades []Trade, companyMerge AliasMap, tradeAlias AliasMap, minGroupSize int) MusterBoard {
	personName := personNameIndex(persons)
	// ⟦W5d-22(d)⟧ A company whose name was cleared FROM THE WIRE composes
	// with an empty Name (the edge refuses that clear; the fold never
	// refuses). It is indexed as ABSENT rather than as a present blank, so
	// the preference rule falls through to the snapshot name and then to
	// the raw company id — never to an empty heading during a roll call.
	companyName := make(map[string]string, len(companies))
	for _, c := range companies {
		if c.Name == "" {
			continue
		}
		companyName[c.CompanyID] = c.Name
	}
	tradeName := make(map[string]string, len(trades))
	for _, w := range trades {
		tradeName[w.TradeID] = w.Name
	}

	resolve := func(v Visit) resolvedRef {
		var r resolvedRef
		// ⟦W6/DR-W6-5⟧ THE LIVE CLOSURE HAS NO SNAPSHOT ARM, and that is a
		// ruling, not an omission: 1. the live roster name, 2. the raw id.
		// An unreplicated or empty-named person keeps rendering a raw
		// PersonID here — self-evidently not a name — where a frozen name in
		// the same <li> as every live name would be marked as nothing at all.
		// This is the operator signal muster_test.go:397-466 already pins
		// ("want the raw id … fall back to the id, don't drop or blank the
		// row"), and W6 does not remove it. Adding a snapshot arm in ANY
		// position is (m6-4) arm B.
		r.personLabel = personLabel(personName[v.PersonID], v.PersonID)
		r.companyLabel = "(no company recorded)"
		if v.CompanySnapshotID != nil {
			r.companyID = companyMerge.Resolve(*v.CompanySnapshotID)
			if n, ok := companyName[r.companyID]; ok {
				r.companyLabel = n
			} else if v.CompanySnapshotName != nil {
				r.companyLabel = *v.CompanySnapshotName
			} else {
				r.companyLabel = r.companyID
			}
		}
		r.tradeLabel = NoTradeRecorded
		if v.TradeSnapshotID != nil {
			r.tradeID = tradeAlias.Resolve(*v.TradeSnapshotID)
			if n, ok := tradeName[r.tradeID]; ok {
				r.tradeLabel = n
			} else if v.TradeSnapshotName != nil {
				r.tradeLabel = *v.TradeSnapshotName
			} else {
				r.tradeLabel = r.tradeID
			}
		}
		return r
	}
	return buildMuster(visits, resolve, minGroupSize)
}

// MusterReprint computes a HISTORICAL reprint: grouped Company→Trade
// using ONLY each visit's point-in-time snapshot (design §8.8-i). There is
// deliberately no AliasMap parameter — this function cannot consult the
// alias table even if a future edit wanted it to, which is the concrete
// meaning of "a reprint must never re-resolve" (chunk-plan C9 not-done-if).
// Calling this again later, after aliases exist, on the SAME visit rows
// reproduces the identical grouping every time, because a visit's snapshot
// fields never change after arrival.
//
// ⚠ ⟦W6 / build-critique F-6⟧ EVERY CLAUSE ABOVE IS ABOUT THE GROUPING, AND THE
// NAMES DO NOT ALL BEHAVE LIKE IT. Since W6 (item 5) this function also emits a
// PERSON label, and its second arm is the LIVE roster name — see the three arms
// spelled out inside resolve below. So for a visit whose snapshot is empty or
// NULL (the roster could not name that person at check-in), the printed name is
// read LIVE and CAN CHANGE under a later amendment: that one line is NOT
// reproducible, deliberately, because "still show the id, never drop the person"
// outranks freezing a blank. The grouping is unaffected — it never consults the
// person label — so "reproduces the identical grouping" stays true as written.
// ⚠ The printed sheet SAYS SO: muster.html's reprint mode-detail carries the
// qualifier "and that one line can still change" (W6-9). Said here too because a
// reader asking "is a printed sheet name-reproducible?" reads THIS comment first,
// and before this note it was told nothing. ⚠ The narrowing is INHERITED, not
// introduced by W6: this header already read "ONLY the snapshot" while the
// function already took live `persons` for the person name.
func MusterReprint(visits []Visit, persons []Person, minGroupSize int) MusterBoard {
	personName := personNameIndex(persons)
	resolve := func(v Visit) resolvedRef {
		var r resolvedRef
		// ⟦W6/DR-W6-5, adjudication #7 verbatim⟧ THREE ARMS, in this order:
		//   1. the SNAPSHOT — the name as recorded at check-in, which is what
		//      makes this sheet reproduce itself under a later amendment;
		//   2. the LIVE roster name — the ruled MIDDLE ARM. It is reachable:
		//      a peer's person_created with an empty name folds unchecked
		//      (refdata.go: the fold NEVER refuses) and that person still
		//      checks in, so the snapshot can be empty/NULL. For that class
		//      the frozen board is NOT amendment-invariant, deliberately —
		//      "still show the id, never drop the person" outranks freezing a
		//      blank, and the printed sheet is made to say so;
		//   3. the raw PersonID — the last resort, which #7 says "must be
		//      preserved".
		// Dropping arm 1 is (m6-1); dropping arm 2 is (m6-1e).
		snapshot := ""
		if v.PersonSnapshotName != nil {
			snapshot = *v.PersonSnapshotName
		}
		r.personLabel = personLabel(snapshot, personName[v.PersonID], v.PersonID)
		r.companyLabel = "(no company recorded)"
		if v.CompanySnapshotID != nil {
			r.companyID = *v.CompanySnapshotID
			if v.CompanySnapshotName != nil {
				r.companyLabel = *v.CompanySnapshotName
			} else {
				r.companyLabel = r.companyID
			}
		}
		r.tradeLabel = NoTradeRecorded
		if v.TradeSnapshotID != nil {
			r.tradeID = *v.TradeSnapshotID
			if v.TradeSnapshotName != nil {
				r.tradeLabel = *v.TradeSnapshotName
			} else {
				r.tradeLabel = r.tradeID
			}
		}
		return r
	}
	return buildMuster(visits, resolve, minGroupSize)
}

func personNameIndex(persons []Person) map[string]string {
	m := make(map[string]string, len(persons))
	for _, p := range persons {
		m[p.PersonID] = p.Name
	}
	return m
}

// buildMuster is the shared grouping/threshold engine. Only OPEN visits
// (IsOpen == 1) are considered — §11 is a view over open visits. resolve is
// the ONLY thing that differs between MusterLive and MusterReprint above.
//
// minGroupSize folding is NUMERIC throughout (the JET regression guard, file
// comment): a trade bucket with fewer than minGroupSize open members is
// pooled into the flat Summary instead of getting its own listed group.
// minGroupSize <= 0 disables folding entirely (every group is int-compared
// against it, and no count is ever < 0).
func buildMuster(visits []Visit, resolve func(Visit) resolvedRef, minGroupSize int) MusterBoard {
	type bucket struct {
		ref     resolvedRef
		members []MusterMember
	}
	buckets := map[string]*bucket{}
	var order []string
	for _, v := range visits {
		if v.IsOpen != 1 {
			continue
		}
		ref := resolve(v)
		key := ref.companyID + "\x00" + ref.tradeID
		b, ok := buckets[key]
		if !ok {
			b = &bucket{ref: ref}
			buckets[key] = b
			order = append(order, key)
		}
		b.members = append(b.members, MusterMember{
			VisitID: v.VisitID, PersonID: v.PersonID, PersonName: ref.personLabel,
			CompanyName: ref.companyLabel, TradeName: ref.tradeLabel,
		})
	}

	var board MusterBoard
	companyIdx := map[string]int{}
	for _, key := range order {
		b := buckets[key]
		// ⟦§101, 2026-09-03⟧ THIS ONE IS DELIBERATELY LEFT NON-TOTAL, and the three
		// sorts below are not. ⚠ NOT AN OVERSIGHT AND NOT A STYLE DRIFT: §101
		// authorised a tiebreak only where a FAILURE COULD BE PRODUCED, and for this
		// comparator none can. Two members of ONE bucket share a companyID and a
		// tradeID by construction, and the grouped section renders ONLY
		// `<li>{{.PersonName}}</li>` (muster.html:317) — so [MEASURED] two members
		// with one name render byte-identically in either order. Their per-member
		// CompanyName/TradeName CAN differ (a rename between two check-ins) but reach
		// paper only via the pooled Summary, which is re-sorted totally below.
		// ⚠ THE REASON IS FALSIFIABLE AND WILL GO STALE LOUDLY: if muster.html ever
		// renders a per-member field OTHER than PersonName inside a trade group, this
		// order becomes observable and wants `m.VisitID` as a tiebreak like :385.
		// ⟦SchemaVersion 15, R-23⟧ IT NOW DOES: muster.html tags a member
		// RESTORED by its VisitID (web/muster.go's RestoredVisits), so two
		// same-named members of one bucket, one restored and one not, render
		// differently in either order. The comparator is made total on
		// VisitID exactly as the note above prescribed — the failure could
		// now be produced, so §101 authorises the tiebreak.
		sort.Slice(b.members, func(i, j int) bool {
			if b.members[i].PersonName != b.members[j].PersonName {
				return b.members[i].PersonName < b.members[j].PersonName
			}
			return b.members[i].VisitID < b.members[j].VisitID
		})
		board.Total += len(b.members)

		// THE NUMERIC COMPARISON (chunk-plan C9's named regression): len(...)
		// is an int; minGroupSize is an int. Formatting either to a string
		// before this comparison is exactly the bug that folded a 13-person
		// JET crew onto the summary sheet because "13" < "2" as text.
		if minGroupSize > 0 && len(b.members) < minGroupSize {
			board.Summary = append(board.Summary, b.members...)
			continue
		}

		ci, ok := companyIdx[b.ref.companyID]
		if !ok {
			ci = len(board.Companies)
			board.Companies = append(board.Companies, CompanyGroup{CompanyID: b.ref.companyID, CompanyName: b.ref.companyLabel})
			companyIdx[b.ref.companyID] = ci
		}
		board.Companies[ci].Trades = append(board.Companies[ci].Trades, TradeGroup{
			TradeID: b.ref.tradeID, TradeName: b.ref.tradeLabel, Members: b.members,
		})
	}

	// ⟦§101, 2026-09-03⟧ THE THREE COMPARATORS BELOW ARE TOTAL, and they were not.
	//
	// ⚠⚠ WHAT WAS WRONG, stated precisely because the obvious reading is wrong.
	// `sort.Slice` is deterministic for a given input and `visit_id` persists, so
	// the same roster ALWAYS printed the same order — a sheet never reordered
	// between two prints of itself, and no reprint was ever unfaithful. What was
	// wrong is that among EQUALLY-NAMED groups the order was settled by whichever
	// crypto/rand visit id (ids.go:22-27) happened to open the bucket first, so it
	// MOVED when an unrelated person checked in or out. Two "Kakivik" sections
	// swapped places on the sheet an officer reads aloud, for a reason printed
	// nowhere on it.
	//
	// ⚠ THE TIE IS DESIGNED FOR, NOT HYPOTHETICAL: ids.go makes duplicate reference
	// data with matching NAMES and different IDS the expected steady state ("two
	// sites minting a 'Kakivik' company … can never collide on ID even though the
	// name matches"), and MusterReprint consults no AliasMap BY DESIGN, so on a
	// printed sheet the duplicates are never merged away.
	//
	// Each of the three is pinned by a test that FAILS without its tiebreak —
	// muster_order_test.go, whose failure text names the two orders it saw. A
	// tiebreak nobody has watched matter is a line somebody deletes as noise.
	// The id is the row identity, never a second display rule: this settles WHICH
	// of two identically-labelled groups prints first, and changes nothing else.
	sort.Slice(board.Summary, func(i, j int) bool {
		if board.Summary[i].PersonName != board.Summary[j].PersonName {
			return board.Summary[i].PersonName < board.Summary[j].PersonName
		}
		return board.Summary[i].VisitID < board.Summary[j].VisitID
	})
	sort.Slice(board.Companies, func(i, j int) bool {
		if board.Companies[i].CompanyName != board.Companies[j].CompanyName {
			return board.Companies[i].CompanyName < board.Companies[j].CompanyName
		}
		return board.Companies[i].CompanyID < board.Companies[j].CompanyID
	})
	for i := range board.Companies {
		wgs := board.Companies[i].Trades
		sort.Slice(wgs, func(a, b int) bool {
			if wgs[a].TradeName != wgs[b].TradeName {
				return wgs[a].TradeName < wgs[b].TradeName
			}
			return wgs[a].TradeID < wgs[b].TradeID
		})
	}
	return board
}

// VisitsByID filters a Visits() dump down to exactly the given ids. This is
// how a printed sheet's specific roster — frozen into its print URL at the
// moment the live board was rendered, chunk-plan C9's no-client-storage
// reprint mechanism — is recovered for a later reprint, without any new
// persisted "print" event: the visit rows themselves are the historical
// record (design §8.8-i).
//
// Returns an error naming every id that was NOT found, rather than silently
// returning a shorter slice (verification finding: a muster is a safety
// artifact — a print link that quietly drops whoever it can't resolve is
// the single worst failure mode this feature could have). Today's only
// caller builds this id list from the live board's own current visits, so
// the error path is not reachable in normal operation; it exists for
// whatever caller comes next that hands this a stale or hand-edited list —
// "unreachable today" is a property of call sites, not of this function.
func VisitsByID(all []Visit, ids []string) ([]Visit, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	byID := make(map[string]Visit, len(all))
	for _, v := range all {
		byID[v.VisitID] = v
	}
	out := make([]Visit, 0, len(ids))
	var missing []string
	for _, id := range ids {
		if v, ok := byID[id]; ok {
			out = append(out, v)
		} else {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("domain: VisitsByID: %d visit id(s) not found: %s", len(missing), strings.Join(missing, ", "))
	}
	return out, nil
}
