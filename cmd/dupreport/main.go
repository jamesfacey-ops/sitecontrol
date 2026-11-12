// dupreport — the standing backstop for duplicate person IDENTITIES among
// OPEN visits.
//
// THE BUG THIS BACKSTOPS: internal/domain/refdata.go's createPerson (reached
// from checkin.go's resolvePersonSlot — an officer picking "add new" at the
// gate) mints a fresh person_id with NO match check against the existing
// roster. Vehicles already get this check at creation (vehicle.go's
// VehiclesFoldingTo, wired at internal/web/vehicle_amend.go:229); persons do
// not. So one human can end up holding two person_ids, each with its own
// open visit — an OVER-COUNT on the muster (a departed person still reads as
// on-site under their other id) and a way to HIDE a real stranding (the
// person who needs finding is "on site" under the id nobody is looking for).
//
// A fix at the picker (stop it happening) and a fix at the muster (fold
// already-linked identities when displaying) are being built elsewhere. This
// tool is neither: it is the REPORT that finds the condition wherever it
// ALREADY EXISTS, across every open visit this dump set can see, run any
// time an officer or a standing check wants a second opinion.
//
// ⛔ IT REPORTS; IT NEVER MUTATES. §213(1b) rules that identity merges are
// adjudicated by an officer, never automatic — this tool has no write path
// at all, only reads a saved dump and prints. Person.SameAs (the existing
// merge pointer, P-3 option 1a) is read here to recognise identities an
// officer has ALREADY linked — those are not re-reported — but nothing here
// ever asserts a same_as edge itself.
//
// ⚠ IDENTITY COMPOSITION LIVES IN internal/domain, NOT HERE (W3-14): a
// person's current name and merge pointer are amendable facts, folded from a
// side table this package is forbidden to know exists — no identifier and no
// string literal naming it may appear outside internal/domain
// (internal/store's TestW3ConsumerScan, empty allowlist, by design). This
// file gets composed identity from domain.ComposedPersonsFromDump — the same
// composition Persons(st) performs for a live store, offered as a dump-file
// entry point for exactly the kind of consumer this tool is (D-56: a saved
// dump, never a live *store.Store). Reach for that function, never for the
// side table's own shape, if this file ever needs another amendable fact.
//
// INPUT, matching cmd/dumpcompare's convention exactly (D-56: no network
// path, files only, produced on each post with):
//
//	sitectl -config /opt/sitectl/site.toml -dump > /tmp/dump-<x>.json
//
// Unlike dumpcompare — whose job is inherently pairwise (do A and B agree?)
// — this tool's job is a single-set question (does DUPLICATION exist in
// what's known?), so it takes ONE OR MORE dump files and UNIONS them
// (deduplicated by row id) rather than requiring exactly two. Ice Road and
// Pad Site Control replicate `visit` and `person` to each other, so a dump
// from either post alone is normally a complete picture; an operator who
// distrusts replication currency can pass both posts' files and this tool
// still produces one merged report. A real cross-post BYTE divergence in a
// replicated projection is dumpcompare's question, not this tool's — if the
// two files disagree on a row's content, whichever file is listed first
// on the command line wins that row, silently; run dumpcompare first if
// that possibility matters.
//
// MATCHING RULE, and why company/trade never SUPPRESS a name match: the
// primary (only gating) signal is the person's NAME, fold-compared the same
// way vehicle.go's vehicleFoldKey folds a vehicle number (casefold, strip
// punctuation) — see textFoldKey below. Company and trade, where recorded on
// the visit, are printed alongside every finding as CORROBORATING CONTEXT
// (do the members agree, disagree, or is it simply not recorded on one
// side) — but a company/trade MISMATCH never removes a name match from the
// report. The reasoning is safety-directional, not a stylistic choice: a
// contractor can genuinely be recorded under two different companies across
// two check-ins (clerical slip, employer change mid-contract), and this tool
// exists to catch strandings — silently dropping a real duplicate because a
// company field disagreed is exactly the false-negative failure mode a
// muster backstop must not have. An extra review line costs an officer a
// glance; a suppressed real duplicate costs a life-safety miss.
//
// DENOMINATOR: every run states how many open visits it examined, how many
// distinct person_ids and canonical (post-merge) identities that resolved
// to, how many name groups it formed, and how many of those groups already
// resolve to ONE canonical identity via an existing same_as link (excluded,
// not a new finding) versus TWO OR MORE (reported). A run that finds nothing
// prints the same counts a run that finds something does — "0 open visits
// examined" and "840 open visits examined, 0 duplicates" are printed
// differently on purpose, so a degenerate empty input can never read the
// same as a real clean pass (the dumpcompare project's own §219 lesson:
// "two empty dumps agree" must never be silently indistinguishable from an
// instrument that actually ran).
//
// Exit codes: 0 = the examined visit set produced no likely-duplicate
// groups; 1 = at least one likely-duplicate group was found (see the
// DUPLICATE blocks above the verdict line); 2 = usage, file, or parse error
// — the check never ran. As with dumpcompare, that distinction is the point:
// 1 means "this tool ran and found something to review", 2 means "nobody
// should trust this run's silence".
//
// A SECOND, SEPARATE BLOCK — THE NAME-FOLD CENSUS — is printed between the
// denominator and the verdict (see nameFoldCensus). It counts, over the
// whole ROSTER rather than the open visits, the pairs the gate's own asking
// rule (internal/web/person_match.go) would join, broken down by which fold
// did the joining, so the class this system's two entry conventions
// manufacture ("Last, First" at intake against "First Last" at the gate)
// is sized from the first day it can occur. It is a count, never a finding:
// it does not touch the verdict line or the exit code.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/web"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: dupreport <dump-file> [dump-file...]")
		fmt.Fprintln(stderr, "  each argument is a path to a saved projection dump, produced ON")
		fmt.Fprintln(stderr, "  its own post with:")
		fmt.Fprintln(stderr, "    sitectl -config /opt/sitectl/site.toml -dump > dump-<x>.json")
		fmt.Fprintln(stderr, "  one file is normally enough (visit/person replicate); pass more than")
		fmt.Fprintln(stderr, "  one to union several posts' views into one report.")
		return 2
	}

	visits := map[string]domain.Visit{}
	persons := map[string]domain.Person{} // COMPOSED already — see below, never the raw base rows

	for _, path := range args {
		body, err := loadFile(path)
		if err != nil {
			fmt.Fprintf(stderr, "dupreport: %s: %v\n", path, err)
			return 2
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err != nil {
			fmt.Fprintf(stderr, "dupreport: %s: not a projection dump (%v); first bytes: %.60s\n", path, err, body)
			return 2
		}
		// A real dump always carries these two keys (possibly with empty
		// arrays) — their outright ABSENCE means this file is not a dump at
		// all (wrong file, truncated capture, an HTML error page saved by
		// mistake), and that must fail loud rather than silently compose an
		// empty, falsely-clean report. An empty ARRAY under a present key is
		// a legitimate "nothing here right now" and is not an error — that
		// case is what the denominator line exists to make visible instead.
		vraw, vok := raw["visit"]
		_, pok := raw["person"]
		if !vok || !pok {
			fmt.Fprintf(stderr, "dupreport: %s: not a projection dump (missing \"visit\" or \"person\" section)\n", path)
			return 2
		}
		var fileVisits []domain.Visit
		if err := json.Unmarshal(vraw, &fileVisits); err != nil {
			fmt.Fprintf(stderr, "dupreport: %s: decoding \"visit\": %v\n", path, err)
			return 2
		}
		for _, v := range fileVisits {
			if _, seen := visits[v.VisitID]; !seen {
				visits[v.VisitID] = v
			}
		}

		// Composed identity (name, same_as) comes from the domain package's
		// own dump-file entry point to the composed read path — this tool
		// never decodes the amendment side table itself (W3-14: no package
		// outside internal/domain may name it, in an identifier or a string
		// literal). One file composes to one []Person; a second file's
		// composed result is unioned below, not the raw rows merged first —
		// each post's own dump already reflects everything THAT post has
		// folded, replicated in or authored locally.
		filePersons, err := domain.ComposedPersonsFromDump(body)
		if err != nil {
			fmt.Fprintf(stderr, "dupreport: %s: %v\n", path, err)
			return 2
		}
		for _, p := range filePersons {
			if _, seen := persons[p.PersonID]; !seen {
				persons[p.PersonID] = p
			}
		}
	}

	// alias: every composed person's effective same_as, fed straight into
	// domain.AliasMap — the SAME hop-capped Resolve this project's live
	// muster/merge code uses (internal/domain/merge.go), so a duplicate an
	// officer has already linked (via whatever path sets same_as) resolves
	// to one canonical id here exactly as it would there.
	alias := domain.AliasMap{}
	for id, p := range persons {
		if p.SameAs != nil {
			alias[id] = *p.SameAs
		}
	}

	var open []member
	for _, v := range visits {
		if v.IsOpen != 1 {
			continue
		}
		name := v.PersonID // last-resort fallback: never empty, a real id folds to nothing else
		if p, ok := persons[v.PersonID]; ok && p.Name != "" {
			name = p.Name
		} else if v.PersonSnapshotName != nil && *v.PersonSnapshotName != "" {
			name = *v.PersonSnapshotName
		}
		m := member{
			visitID:     v.VisitID,
			siteID:      v.SiteID,
			personID:    v.PersonID,
			canonicalID: alias.Resolve(v.PersonID),
			name:        name,
		}
		if v.CompanySnapshotName != nil {
			m.company = strings.TrimSpace(*v.CompanySnapshotName)
		}
		if v.TradeSnapshotName != nil {
			m.trade = strings.TrimSpace(*v.TradeSnapshotName)
		}
		open = append(open, m)
	}
	sort.Slice(open, func(i, j int) bool { return open[i].visitID < open[j].visitID }) // deterministic report order

	// Bucket by folded name. This is the ONLY grouping key (see the file
	// comment on why company/trade never suppress a match).
	buckets := map[string][]member{}
	var bucketOrder []string
	for _, m := range open {
		key := textFoldKey(m.name)
		if _, ok := buckets[key]; !ok {
			bucketOrder = append(bucketOrder, key)
		}
		buckets[key] = append(buckets[key], m)
	}
	sort.Strings(bucketOrder)

	rawIDs := map[string]bool{}
	canonicalIDs := map[string]bool{}
	for _, m := range open {
		rawIDs[m.personID] = true
		canonicalIDs[m.canonicalID] = true
	}

	var findingGroups int
	var cleanGroups int
	for _, key := range bucketOrder {
		members := buckets[key]
		distinct := map[string]bool{}
		for _, m := range members {
			distinct[m.canonicalID] = true
		}
		if len(distinct) < 2 {
			cleanGroups++
			continue
		}
		findingGroups++
		fmt.Fprintf(stdout, "DUPLICATE? %q: %d open visits across %d distinct identities\n", members[0].name, len(members), len(distinct))
		fmt.Fprintln(stdout, "  "+companyTradeNote(members))
		for _, m := range members {
			canon := ""
			if m.canonicalID != m.personID {
				canon = fmt.Sprintf(" (already linked -> %s)", m.canonicalID)
			}
			fmt.Fprintf(stdout, "  visit %s: person %s%s, site %s, name %q, company=%s, trade=%s\n",
				m.visitID, m.personID, canon, m.siteID, m.name, orNone(m.company), orNone(m.trade))
		}
	}

	alreadyLinkedRaw := len(rawIDs) - len(canonicalIDs)
	if alreadyLinkedRaw > 0 {
		fmt.Fprintf(stdout, "excluded: %d person_ids already resolve to another via an existing same_as link (not re-reported)\n", alreadyLinkedRaw)
	}

	fmt.Fprintf(stdout, "examined: %d open visits, %d distinct person_ids, %d distinct identities after existing merges, %d name groups (%d clean, %d flagged)\n",
		len(open), len(rawIDs), len(canonicalIDs), len(bucketOrder), cleanGroups, findingGroups)

	// The census: a count over the roster, printed BEFORE the verdict so the
	// verdict stays the last line, and contributing nothing to it.
	nameFoldCensus(persons, alias, stdout)

	if findingGroups == 0 {
		fmt.Fprintf(stdout, "CLEAN: no likely duplicate identities among %d open visits\n", len(open))
		return 0
	}
	fmt.Fprintf(stdout, "LIKELY DUPLICATES: %d groups found (see DUPLICATE? blocks above) — officer review required, §213(1b): this tool never merges\n", findingGroups)
	return 1
}

func orNone(s string) string {
	if s == "" {
		return "(none recorded)"
	}
	return s
}

// member is one open visit, resolved to the facts this report matches and
// prints on. canonicalID is personID after walking any existing same_as
// link (alias.Resolve) — the two differ exactly when an officer has already
// linked this identity to another. company/trade are the VISIT's own
// snapshot ("as recorded" at that check-in), never the composed person —
// see the file comment on why.
type member struct {
	visitID, siteID, personID, canonicalID, name string
	company, trade                               string // "" = not recorded on this visit
}

// companyTradeNote summarises whether the members of one finding group
// AGREE, DISAGREE, or leave company/trade unrecorded — corroborating
// context only, per the file comment; it never changes membership.
func companyTradeNote(members []member) string {
	note := func(get func(member) string, label string) string {
		vals := map[string]bool{}
		recorded := 0
		for _, m := range members {
			v := get(m)
			if v == "" {
				continue
			}
			recorded++
			vals[textFoldKey(v)] = true
		}
		switch {
		case recorded == 0:
			return label + ": not recorded on any member"
		case recorded < len(members):
			return fmt.Sprintf("%s: recorded on %d/%d members%s", label, recorded, len(members), agreementSuffix(vals))
		default:
			return label + agreementSuffix(vals)
		}
	}
	return note(func(m member) string { return m.company }, "company") + "; " +
		note(func(m member) string { return m.trade }, "trade")
}

func agreementSuffix(vals map[string]bool) string {
	if len(vals) <= 1 {
		return ": agrees"
	}
	return ": DISAGREES"
}

// The census classes, in print order. The comma swap is first because it is
// the class the census exists to size; "exact" is the strict key's own
// territory (two records that differ by case and whitespace only were minted
// past it); "other" is every remaining fold of the asking rule.
const (
	censusCommaSwap = "comma-swap"
	censusExact     = "exact"
	censusOther     = "other-folds"
)

// nameFoldCensus is the SECOND question this tool answers, and a different
// kind of question from the first: not "does a duplicate identity sit among
// the OPEN VISITS right now" (the muster backstop in run, which sets the exit
// code) but "how often has the ROSTER been given two records for one human by
// a spelling the gate's own asking rule joins" — counted over every composed
// person whatever their visit state, and broken down by WHICH FOLD did the
// joining. It prints a count and the pairs. It never changes the verdict
// line or the exit code: a measuring instrument, not a guard.
//
// WHY IT EXISTS, AND WHY HERE. internal/web/person_match.go's fold 1 joins
// "Last, First" (intake's spelling) against "First Last" (the gate's), a
// variant this system's two entry conventions MANUFACTURE. The legacy data
// could not size that class — one entry form, so its reversed-order rule
// added zero groups over the plain one — so the only way to know whether the
// fold earns its keep is to count the class in the new system from its first
// day, and this is the tool that already reads a saved dump for exactly this
// kind of question. The rule is CALLED (web.ReportNameMatch), never copied:
// a second spelling of it would drift, and the census would then describe a
// rule the gate does not run. The price of calling it: this files-only tool
// now links internal/web (and, through it, net/http and the web templates)
// for one pure function — see loadFile's comment.
//
// MERGES FIRST. A pair an officer has ALREADY linked (both ids resolve to
// one canonical through the same composed same_as map the backstop uses) is
// counted apart and not listed — a duplicate the system caught and a human
// closed, not an open one. The store-bound resolver the live muster and
// merge paths use cannot be called here (D-56: a saved dump, never a live
// store); this tool's AliasMap is built from the same composed effective
// same_as, without that resolver's cycle collapse — a wire-formed cycle,
// refused at every authoring edge, is the one shape on which the two could
// disagree.
//
// WHAT IT CANNOT SEE — the line that matters most when the count is zero: a
// duplicate the offer PREVENTED, because the officer picked the existing
// person when the rule offered them, left NO second record, and a count over
// records cannot see it. A zero here means "no second record exists". It
// does not distinguish "the class does not occur" from "the check is
// working" — those are opposite conclusions, and only the gate's own offer
// path (which this tool does not read) could tell them apart.
//
// PAIRS, NOT GROUPS: three spellings of one person are three pairs. The
// comparison is n² over the roster, calling the rule once per pair — the
// cost is stated in reports/witness/DUPREPORT-COMMA-SWAP.md so a slow run on
// a large roster is understood rather than debugged.
func nameFoldCensus(persons map[string]domain.Person, alias domain.AliasMap, stdout io.Writer) {
	type entry struct{ id, site, name string }
	var roster []entry
	unnamed := 0
	for id, p := range persons {
		if strings.TrimSpace(p.Name) == "" {
			unnamed++
			continue
		}
		roster = append(roster, entry{id: id, site: p.SiteID, name: p.Name})
	}
	sort.Slice(roster, func(i, j int) bool { return roster[i].id < roster[j].id }) // deterministic pair order
	identities := map[string]bool{}
	for _, e := range roster {
		identities[alias.Resolve(e.id)] = true
	}

	type pair struct {
		a, b  entry
		shape web.NameMatchShape
	}
	unmerged := map[string][]pair{}
	merged := map[string]int{}
	pairs, reads, priceShape := 0, 0, 0
	for i := 0; i < len(roster); i++ {
		for j := i + 1; j < len(roster); j++ {
			pairs++
			r := web.ReportNameMatch(roster[i].name, roster[j].name)
			if !r.Reads {
				continue
			}
			reads++
			class := censusOther
			switch {
			case r.Exact:
				class = censusExact
			case r.CommaSwap:
				class = censusCommaSwap
			}
			if alias.Resolve(roster[i].id) == alias.Resolve(roster[j].id) {
				merged[class]++
				continue
			}
			unmerged[class] = append(unmerged[class], pair{roster[i], roster[j], r.Shape})
			if r.Shape == web.ShapeOneLetterMiddle {
				priceShape++
			}
		}
	}

	fmt.Fprintln(stdout, "--- name-fold census: roster pairs the gate's asking rule joins (internal/web/person_match.go) — a COUNT, not a finding; the verdict below does not include it ---")
	fmt.Fprintf(stdout, "census: %d named persons (%d unnamed skipped), %d identities after existing merges, %d pairs compared, %d pairs read alike\n",
		len(roster), unnamed, len(identities), pairs, reads)
	for _, c := range []struct{ class, label string }{
		{censusCommaSwap, `comma-swap ("Last, First" against "First Last"; the swap did the joining — the class only this count can size)`},
		{censusExact, "exact (case and whitespace only)"},
		{censusOther, "other folds (spacing, middle name or initial, punctuation, suffix)"},
	} {
		fmt.Fprintf(stdout, "  %s: %d unmerged, %d already merged by an officer\n", c.label, len(unmerged[c.class]), merged[c.class])
		for _, p := range unmerged[c.class] {
			shape := string(p.shape)
			if shape == "" { // an exact match has no loose shape; say what joined it rather than print a blank
				shape = "case and whitespace"
			}
			fmt.Fprintf(stdout, "    %s %q (%s) / %s %q (%s) [%s]\n", p.a.id, p.a.name, p.a.site, p.b.id, p.b.name, p.b.site, shape)
		}
	}
	fmt.Fprintf(stdout, "  price-shape pairs among the unmerged above (a one-letter middle word dropped: \"Pat O Brien\"/\"Pat Brien\" lives here and so does \"Mary J Ferguson\"/\"Mary Ferguson\" — the rule cannot tell them apart, a glance can): %d\n", priceShape)
	fmt.Fprintln(stdout, "  cannot see: a duplicate the offer PREVENTED left no second record — a zero above means \"no second record exists\", not \"the class does not occur\"")
}

// textFoldKey is a case/punctuation-insensitive comparison key: Unicode-
// normalise (NFC), casefold, keep only letters and digits. Deliberately the
// same technique as internal/domain/vehicle.go's vehicleFoldKey (that
// function's own doc comment: "the SEARCH-AND-PROMPT form... for the
// duplicate prompt at creation") — this tool is that same idea, applied to
// person names instead of vehicle numbers, and to visits instead of a
// creation-time prompt. It is a comparison-only form: nothing here is ever
// written back anywhere.
func textFoldKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(norm.NFC.String(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// loadFile is cmd/dumpcompare's load(), verbatim in convention: refuse a
// URL rather than fetch it (D-56 removed the dump route; this tool has no
// network code path — since the name-fold census it LINKS net/http
// transitively through internal/web, whose one pure function it calls, and
// calls nothing in net/http itself), read the file, and hand back raw bytes
// for the caller to decode — kept as raw bytes rather than a decoded map
// here because this tool's shape check (which keys must be PRESENT, not
// just which keys the two sides share) is stricter than dumpcompare's and
// wants the json.RawMessage step itself.
func loadFile(arg string) ([]byte, error) {
	if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
		return nil, fmt.Errorf("dump URLs no longer exist (D-56: the /debug/projection-dump route was removed). "+
			"Produce the file on the post instead: sitectl -config /opt/sitectl/site.toml -dump > dump.json; got %q", arg)
	}
	return os.ReadFile(arg)
}
