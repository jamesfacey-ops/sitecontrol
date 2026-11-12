// d87_choke_point_test.go — D-87's refusal witness (JAMES-DECISIONS.md
// D-87: "read it all but modify only the individual site's data"; D-89: the
// rule covers PRESENCE data — visits, check-ins, check-outs, arrivals — and
// reference data stays shared).
//
// PRODUCED RED FIRST. The v0 of this file named no symbol the build adds and
// asked only whether a post can modify another post's presence rows: on the
// unmodified tree (087711c) every one of the four gated payloads LANDED from
// camp-7 against camp-9's rows (watermark 0 → 4) and both unguarded edges
// accepted (reports/witness/D87-CHOKE-POINT.md §3 carries the output). The
// build made them refuse; this v1 names the typed refusal, its fields, and
// the positive controls that prove the refusal is about the TAG and not the
// bytes.
package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// d87Posts is two posts, deliberately NOT site-a/site-b (the hyphenated
// ids exercise the id parsing the tag must never rely on). camp-9 takes a
// gate check-in — one degenerate trip (origin_site camp-9), one arrival
// (site_id camp-9, trip_ref set) and one open visit (site_id camp-9) — and
// its whole stream is replicated into camp-7's post, so camp-7 HOLDS every
// one of those rows (rule 3: a site reads everything) and may modify none
// of them (rule 4).
type d87Posts struct {
	st7, st9  *store.Store
	tripID    string
	arrivalID string
	visitID   string
	personID  string
}

func d87TwoPosts(t *testing.T) d87Posts {
	t.Helper()
	open := func() *store.Store {
		st, err := store.Open(filepath.Join(t.TempDir(), "post.db"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { st.Close() })
		if err := Register(st); err != nil {
			t.Fatalf("register: %v", err)
		}
		return st
	}
	st7, st9 := open(), open()

	driver, _ := seedTwoPersons(t, st9, "camp-9")
	trip, arr, err := GateCheckIn(st9, "camp-9", 1, GateCheckInInput{
		VehicleID: w7SeedVehicleID(t, st9, "camp-9", "TRK-9"),
		Driver:    PersonSlot{ExistingID: driver.PersonID},
	}, 0, "default")
	if err != nil {
		t.Fatalf("fixture: check-in at camp-9: %v", err)
	}
	visits, err := OpenVisitsAt(st9, "camp-9")
	if err != nil {
		t.Fatal(err)
	}
	if len(visits) != 1 || visits[0].SiteID != "camp-9" {
		t.Fatalf("fixture: want exactly one open camp-9 visit, got %+v", visits)
	}

	// Replicate camp-9 → camp-7, contiguous prefix in order (what the sync
	// engine does; the fold never refuses a peer's event — W3-2).
	envs, err := st9.Events("camp-9", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st7.ApplyBatch(envs); err != nil {
		t.Fatalf("fixture: replicate camp-9 into camp-7: %v", err)
	}
	if a, err := rawArrivalByID(st7, arr.ArrivalID); err != nil || a == nil || a.SiteID != "camp-9" {
		t.Fatalf("fixture: camp-7 must HOLD camp-9's arrival after replication (rule 3): %+v %v", a, err)
	}
	return d87Posts{st7: st7, st9: st9, tripID: trip.TripID, arrivalID: arr.ArrivalID,
		visitID: visits[0].VisitID, personID: driver.PersonID}
}

type d87Gated struct {
	typ, target string
	payload     []byte
}

// d87GatedPayloads is the presence subset, one hand-built payload per type,
// each naming camp-9's row. Hand-built so the witness reaches store.Author
// DIRECTLY — the choke point, not any edge in front of it — which is the
// property D-87 demands: "a second write path added later fails the test
// rather than slipping past". Sorted by type so the order is forced (C1).
func d87GatedPayloads(t *testing.T, p d87Posts) []d87Gated {
	t.Helper()
	vehicle := "TRK-9 (corrected from camp-7)"
	out := []d87Gated{
		{"visit_checked_out", p.visitID, mustJSON(t, VisitCheckedOut{Type: "visit_checked_out", VisitID: p.visitID})},
		{"trip_cancelled", p.tripID, mustJSON(t, TripCancelled{Type: "trip_cancelled", TripID: p.tripID})},
		{evtArrivalAmended, p.arrivalID, mustJSON(t, ArrivalAmended{Type: evtArrivalAmended, ArrivalID: p.arrivalID, VehicleEntered: &vehicle})},
		{evtArrivalRefRetracted, p.arrivalID, mustJSON(t, ArrivalRefRetraction{Type: evtArrivalRefRetracted, ArrivalID: p.arrivalID})},
		// James, 2026-09-30: officer resolution of parked arrivals (Option
		// 1) widens the gated set from four to FIVE — a resolution modifies
		// the arrival's binding, the destination's own presence row, exactly
		// as the retraction does.
		{evtArrivalResolved, p.arrivalID, mustJSON(t, ArrivalResolution{Type: evtArrivalResolved, ArrivalID: p.arrivalID})},
	}
	sort.Slice(out, func(i, j int) bool { return out[i].typ < out[j].typ })
	for _, o := range out {
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(o.payload, &probe); err != nil || probe.Type != o.typ {
			t.Fatalf("fixture: payload for %s decodes to type %q (%v)", o.typ, probe.Type, err)
		}
	}
	return out
}

func d87Watermark(t *testing.T, st *store.Store, site string) int64 {
	t.Helper()
	wm, err := st.Watermark(site, 1)
	if err != nil {
		t.Fatal(err)
	}
	return wm
}

// TestD87ThePresenceSubsetCannotBeModifiedFromAnotherPost is the refusal
// witness at the FUNNEL: camp-7 hands store.Author a payload that modifies a
// camp-9-tagged presence row, for each of the four gated types. The refusal
// must be the typed *store.OwnershipError naming the event type, the row,
// camp-9 as its owner and camp-7 as the refused post; nothing may be
// appended. Then the POSITIVE CONTROL: the SAME BYTES, handed to camp-9's
// own post, land — so the refusal is about the tag, not the payload.
func TestD87ThePresenceSubsetCannotBeModifiedFromAnotherPost(t *testing.T) {
	p := d87TwoPosts(t)
	gated := d87GatedPayloads(t, p)
	if want := p.st7.OwnershipGatedTypes(); len(gated) != len(want) {
		t.Fatalf("PRECONDITION: the witness covers %d types, the funnel gates %d (%v) — cover them all", len(gated), len(want), want)
	}
	for _, g := range gated {
		t.Run(g.typ, func(t *testing.T) {
			before := d87Watermark(t, p.st7, "camp-7")
			_, err := p.st7.Author("camp-7", 1, g.payload, 0, "default")
			after := d87Watermark(t, p.st7, "camp-7")
			var own *store.OwnershipError
			if !errors.As(err, &own) {
				t.Fatalf("D-87 rule 4 NOT ENFORCED: camp-7 authored %s against camp-9's row — err=%v (%T), camp-7 watermark %d → %d "+
					"(want *store.OwnershipError and no append)", g.typ, err, err, before, after)
			}
			if own.EventType != g.typ || own.Target != g.target || own.TargetSiteID != "camp-9" || own.SiteID != "camp-7" {
				t.Errorf("the refusal names the wrong thing: %+v; want {EventType:%s Target:%s TargetSiteID:camp-9 SiteID:camp-7}", *own, g.typ, g.target)
			}
			if !strings.Contains(own.Error(), store.OwnershipRule) || !strings.Contains(own.Error(), "camp-9") {
				t.Errorf("the refusal sentence does not say why or whose: %q", own.Error())
			}
			if after != before {
				t.Errorf("a refusal is PRE-APPEND: camp-7's watermark moved %d → %d", before, after)
			}

			// POSITIVE CONTROL — same bytes, the owning post: accepted.
			ownBefore := d87Watermark(t, p.st9, "camp-9")
			if _, err := p.st9.Author("camp-9", 1, g.payload, 0, "default"); err != nil {
				t.Fatalf("CONTROL FAILED: the same %s payload, authored by camp-9 at camp-9's own post, was refused: %v "+
					"(the choke point is not gating the tag — it is gating the type)", g.typ, err)
			}
			if got := d87Watermark(t, p.st9, "camp-9"); got != ownBefore+1 {
				t.Errorf("CONTROL: camp-9's own %s did not append exactly one event (%d → %d)", g.typ, ownBefore, got)
			}
		})
	}
	if events, err := p.st7.Events("camp-7", 1, 1, 0); err != nil || len(events) != 0 {
		t.Errorf("camp-7's stream must still be EMPTY after four refusals; got %d event(s) (err=%v)", len(events), err)
	}
}

// TestD87TheTwoUnguardedEdgesSurfaceTheTypedRefusal asks the same question
// through the two presence edges that carried NO own-site check before
// D-87 (d86-ownership §3a #17, #18) — AmendArrival and RetractArrivalRef at
// camp-7 against camp-9's arrival — and holds the edges to WRAPPING the
// store's refusal with %w: errors.As must reach it through the edge's own
// prefix, or the web layer renders an internal fault where the officer
// needs "radio camp-9".
func TestD87TheTwoUnguardedEdgesSurfaceTheTypedRefusal(t *testing.T) {
	p := d87TwoPosts(t)
	vehicle := "TRK-9 (corrected from camp-7)"

	_, err := AmendArrival(p.st7, "camp-7", 1, ArrivalAmendment{ArrivalID: p.arrivalID, VehicleEntered: &vehicle}, 0, "default")
	var own *store.OwnershipError
	if !errors.As(err, &own) || own.TargetSiteID != "camp-9" || own.EventType != evtArrivalAmended {
		t.Errorf("AmendArrival at camp-7 against camp-9's arrival: err=%v (%T); want a wrapped *store.OwnershipError owned by camp-9", err, err)
	}
	_, err = RetractArrivalRef(p.st7, "camp-7", 1, p.arrivalID, nil, 0, "default")
	own = nil
	if !errors.As(err, &own) || own.TargetSiteID != "camp-9" || own.EventType != evtArrivalRefRetracted {
		t.Errorf("RetractArrivalRef at camp-7 against camp-9's arrival: err=%v (%T); want a wrapped *store.OwnershipError owned by camp-9", err, err)
	}
	if wm := d87Watermark(t, p.st7, "camp-7"); wm != 0 {
		t.Errorf("camp-7 appended %d event(s) while refusing; a refusal is PRE-APPEND", wm)
	}

	// POSITIVE CONTROL at the edges: the destination corrects and withdraws
	// its OWN arrival exactly as before D-87 — and each APPENDS: "returned no
	// error" alone is not a witness that anything was recorded.
	ownBefore := d87Watermark(t, p.st9, "camp-9")
	if _, err := AmendArrival(p.st9, "camp-9", 1, ArrivalAmendment{ArrivalID: p.arrivalID, VehicleEntered: &vehicle}, 0, "default"); err != nil {
		t.Errorf("CONTROL FAILED: camp-9 amending its own arrival was refused: %v", err)
	}
	if _, err := RetractArrivalRef(p.st9, "camp-9", 1, p.arrivalID, nil, 0, "default"); err != nil {
		t.Errorf("CONTROL FAILED: camp-9 withdrawing its own arrival's reference was refused: %v", err)
	}
	if got := d87Watermark(t, p.st9, "camp-9"); got != ownBefore+2 {
		t.Errorf("CONTROL: camp-9's two own-arrival edits appended %d event(s) (%d → %d), want exactly 2", got-ownBefore, ownBefore, got)
	}
}

// TestD87ARowThisPostCannotShowIsItsOwnIsRefusedToo: a gated payload naming
// a row this post does not hold at all is refused, as a foreign row is — a
// post that cannot show a row is its own may not modify it. Own-site
// targets always exist by construction (own folds run synchronously), so
// this arm never refuses a true own-site write.
func TestD87ARowThisPostCannotShowIsItsOwnIsRefusedToo(t *testing.T) {
	p := d87TwoPosts(t)
	unknown := mustJSON(t, VisitCheckedOut{Type: "visit_checked_out", VisitID: "camp-7-visit-0000000000000000"})
	_, err := p.st7.Author("camp-7", 1, unknown, 0, "default")
	var own *store.OwnershipError
	if !errors.As(err, &own) {
		t.Fatalf("a checkout naming a visit this post has never seen was not refused as unprovable: err=%v (%T)", err, err)
	}
	if own.TargetSiteID != "" || own.Target != "camp-7-visit-0000000000000000" {
		t.Errorf("the unknown-row refusal must carry an EMPTY owner and the named id: %+v", *own)
	}
	if !strings.Contains(own.Error(), "no such record is known at this post") {
		t.Errorf("the unknown-row sentence must say the row is unknown here, not that it is another site's: %q", own.Error())
	}
	if wm := d87Watermark(t, p.st7, "camp-7"); wm != 0 {
		t.Errorf("refused, yet camp-7 appended %d event(s)", wm)
	}
}

// TestD87SharedReferenceDataIsNotGated is the D-89 boundary, and it is a
// CONTROL against over-application: person / company / trade / vehicle
// records stay SHARED — camp-7 correcting a camp-9-enrolled person's phone
// must LAND (D-84 stands; the gate never refuses, D-66). It is green before
// and after the build; a choke point that refuses it has gated the wrong
// set.
func TestD87SharedReferenceDataIsNotGated(t *testing.T) {
	p := d87TwoPosts(t)
	phone := "555-0187"
	if _, err := AmendPerson(p.st7, "camp-7", 1, PersonAmendment{PersonID: p.personID, Phone: &phone}, 0, "default"); err != nil {
		t.Fatalf("D-89 VIOLATED: camp-7 amending camp-9's PERSON record (shared reference data) was refused: %v", err)
	}
	if wm := d87Watermark(t, p.st7, "camp-7"); wm != 1 {
		t.Errorf("the cross-site person amendment must have appended exactly one event at camp-7; watermark = %d", wm)
	}
	for _, typ := range []string{evtPersonAmended, evtCompanyAmended, evtTradeAmended, evtVehicleAmended} {
		if row := authoredEventOwnership[typ]; row.Class != sharedReference {
			t.Errorf("%s is classified %q; D-89 says shared reference", typ, row.Class)
		}
	}
	for _, typ := range p.st7.OwnershipGatedTypes() {
		switch typ {
		case evtPersonAmended, evtCompanyAmended, evtTradeAmended, evtVehicleAmended:
			t.Errorf("%s has an ownership resolver registered; D-89 says it is SHARED and must not be gated", typ)
		}
	}
}

// --- the census: every authored type is classified, and the presence
// --- subset is EXACTLY the gated set ---------------------------------------

var (
	d87TypeLiteralRe = regexp.MustCompile(`\bType:\s*"([a-z_]+)"`)
	d87TypeConstRe   = regexp.MustCompile(`\bType:\s*(evt[A-Za-z0-9_]+)\b`)
	d87ConstDefRe    = regexp.MustCompile(`\b(evt[A-Za-z0-9_]+)\s*=\s*"([a-z_]+)"`)

	// The wire alphabet arm (i) reads, as the RAW literal text, quotes
	// included — the same characters d87TypeLiteralRe matches, so the two
	// arms share one definition of "readable". A `Type: "…"` literal spelled
	// outside it is authored past (i) unseen (`Type: "probe_v2"` at f5af871,
	// TESTER-d87.md §1), and so is one whose UNQUOTED value is in the
	// alphabet but whose raw text is not (`Type: "probe\x5fnew"` PASSED at
	// fda21a5 — REVIEWER-d87b.md F4), so d87UnseenAuthoringForms flags both.
	d87WireTypeRe = regexp.MustCompile(`^"[a-z_]+"$`)

	// A payload hand-built as JSON text — a raw string, a map literal, an
	// interpreted string with its quotes escaped (`\"type\":`, the optional
	// backslash). Case-insensitive because the DECODER is: refuseForeignRow
	// reads the type through encoding/json's Unmarshal, which matches keys
	// case-insensitively, so `{"Type":…}` is the event as surely as
	// `{"type":…}` (PRODUCED at cd8fa91: a capitalised-key payload was
	// refused as visit_checked_out — D87-CENSUS-CLOSE.md §2).
	d87RawTypeKeyRe = regexp.MustCompile(`(?i)"type\\?"\s*:`)
)

// d87UnseenAuthoringForms lists file:line for every occurrence, in a body of
// source, of an authoring form d87AuthoredTypes cannot classify. One textual
// arm (the raw JSON key, which lives inside string literals and map
// literals) and four structural arms read from the parsed file — a regex
// cannot see a positional literal split over lines, an element whose type is
// elided inside a slice, or the left-hand side of a tuple assignment, and
// each of those was authored past the regexes GREEN at cd8fa91
// (D87-CENSUS-CLOSE.md §1). A file that does not parse is a file the census
// cannot read, and is reported as such rather than skipped.
func d87UnseenAuthoringForms(sources map[string]string) []string {
	files := make([]string, 0, len(sources))
	for f := range sources {
		files = append(files, f)
	}
	sort.Strings(files)
	fset := token.NewFileSet()
	parsed := make([]*ast.File, 0, len(files))
	var out []string
	for _, f := range files {
		af, err := parser.ParseFile(fset, f, sources[f], parser.SkipObjectResolution)
		if err != nil {
			out = append(out, fmt.Sprintf("%s: does not parse, so cannot be censused: %v", f, err))
			continue
		}
		parsed = append(parsed, af)
	}
	carriers, declared := d87WireTypeCarriers(parsed)
	for _, af := range parsed {
		f := fset.File(af.Pos()).Name()
		lines := map[int]bool{}
		for i, line := range strings.Split(sources[f], "\n") {
			if d87RawTypeKeyRe.MatchString(line) {
				lines[i+1] = true
			}
		}
		d87StructuralUnseenForms(fset, af, carriers, declared, lines)
		nums := make([]int, 0, len(lines))
		for n := range lines {
			nums = append(nums, n)
		}
		sort.Ints(nums)
		for _, n := range nums {
			out = append(out, fmt.Sprintf("%s:%d", f, n))
		}
	}
	return out
}

// d87WireKey is the JSON key a struct field marshals under — the json tag's
// name when it has one, the Go field name otherwise (encoding/json's rule) —
// or "" for a field json omits. Any spelling of "type" is the event's type
// at the funnel, because the decoder matches keys case-insensitively: an
// UNTAGGED `Type string` marshals as "Type" and is decoded as the type.
func d87WireKey(f *ast.Field) string {
	if len(f.Names) == 0 {
		return "" // embedded: its own fields are censused where it is declared
	}
	name := f.Names[0].Name
	if f.Tag == nil {
		return name
	}
	raw, err := strconv.Unquote(f.Tag.Value)
	if err != nil {
		return name
	}
	switch tagName := strings.Split(reflect.StructTag(raw).Get("json"), ",")[0]; tagName {
	case "":
		return name
	case "-":
		return ""
	default:
		return tagName
	}
}

func d87CarriesWireType(st *ast.StructType) bool {
	for _, f := range st.Fields.List {
		if strings.EqualFold(d87WireKey(f), "type") {
			return true
		}
	}
	return false
}

// d87TypeName is the bare name an expression names as a type — `X`,
// `pkg.X`, `(X)`, `*X`, `X[T]` — or "".
func d87TypeName(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.Ident:
			return x.Name
		case *ast.SelectorExpr:
			return x.Sel.Name
		case *ast.ParenExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		default:
			return ""
		}
	}
}

// d87WireTypeCarriers is every named type in the corpus whose values marshal
// with a `type` key — DERIVED from the type declarations themselves (top
// level and function-local alike), then closed over aliases and defined
// types declared over a carrier (`type VCO = VisitCheckedOut`, `type VCO
// VisitCheckedOut`) until nothing new is added. Never a hand-kept list: a
// hand-kept list of payload types is the defect this census exists to
// prevent (R11; REVIEWER-d87.md B1).
//
// declared is every OTHER named type's underlying, by name (`type Batch
// []VisitCheckedOut` → `[]VisitCheckedOut`), so a composite literal of a
// NAMED slice/array/map can find the type its elided elements take
// (d87ElidedTypes). A named container is never itself a carrier — its
// elements are — which is why the fixpoint alone could not see one:
// `zzBatch{{"probe_batch", "v", nil}}` PASSED the census at fda21a5
// (REVIEWER-d87b.md F1; D87-F1-CLOSE.md). Every declaration under a name is
// kept: names are bare, two local types may share one, and resolving through
// all of them errs towards flagging.
func d87WireTypeCarriers(files []*ast.File) (carriers map[string]bool, declared map[string][]ast.Expr) {
	carriers = map[string]bool{}
	declared = map[string][]ast.Expr{}
	var named []*ast.TypeSpec
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			if st, ok := ts.Type.(*ast.StructType); ok {
				if d87CarriesWireType(st) {
					carriers[ts.Name.Name] = true
				}
			} else {
				named = append(named, ts)
				declared[ts.Name.Name] = append(declared[ts.Name.Name], ts.Type)
			}
			return true
		})
	}
	for changed := true; changed; {
		changed = false
		for _, ts := range named {
			if !carriers[ts.Name.Name] && carriers[d87TypeName(ts.Type)] {
				carriers[ts.Name.Name] = true
				changed = true
			}
		}
	}
	return carriers, declared
}

// d87IsCarrier reports whether a composite literal of type typ marshals with
// a `type` key: a named carrier spelled bare or qualified, or an anonymous
// struct that carries one.
func d87IsCarrier(typ ast.Expr, carriers map[string]bool) bool {
	for typ != nil {
		switch x := typ.(type) {
		case *ast.ParenExpr:
			typ = x.X
		case *ast.StarExpr:
			typ = x.X
		case *ast.StructType:
			return d87CarriesWireType(x)
		default:
			return carriers[d87TypeName(typ)]
		}
	}
	return false
}

// d87ElidedTypes are the types an element of a composite literal of type typ
// may leave unspelled — Go permits `[]T{{…}}`, `[]*T{{…}}`, `map[K]T{k: {…}}`
// and `map[T]V{{…}: v}`, and exactly the same for a NAMED slice, array or
// map (`type Batch []T; Batch{{…}}`), which is resolved through declared for
// as many hops as it takes (`type Batches []Batch`, `type B2 Batch`) — and
// nil, nil for a struct, whose field values must spell their own type. The
// hop budget is the number of names, which no acyclic chain can exceed; a
// name declared more than once is tried under every declaration.
func d87ElidedTypes(typ ast.Expr, declared map[string][]ast.Expr) (key, val ast.Expr) {
	return d87ElidedTypesWithin(typ, declared, len(declared)+1)
}

func d87ElidedTypesWithin(typ ast.Expr, declared map[string][]ast.Expr, hops int) (key, val ast.Expr) {
	if hops <= 0 {
		return nil, nil
	}
	switch x := typ.(type) {
	case *ast.ParenExpr:
		return d87ElidedTypesWithin(x.X, declared, hops)
	case *ast.ArrayType:
		return nil, x.Elt
	case *ast.MapType:
		return x.Key, x.Value
	}
	for _, under := range declared[d87TypeName(typ)] {
		if k, v := d87ElidedTypesWithin(under, declared, hops-1); k != nil || v != nil {
			return k, v
		}
	}
	return nil, nil
}

// d87TypeValueReadable is exactly the two value shapes arm (i) reads behind
// a `Type:` key — a double-quoted string literal whose RAW text is in the
// wire alphabet, or an evt identifier ((i) resolves it, or is fatal when it
// cannot). Everything else — a plain identifier, a call, a qualified or
// parenthesised constant, a backtick literal, a literal that only unquotes
// into the alphabet — is authored past (i) unseen and must be flagged.
func d87TypeValueReadable(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return v.Kind == token.STRING && d87WireTypeRe.MatchString(v.Value)
	case *ast.Ident:
		return strings.HasPrefix(v.Name, "evt")
	}
	return false
}

// d87StructuralUnseenForms marks, in lines, every line of one parsed file
// that carries one of the four structural forms:
//
//   - a `.Type` selector on the LEFT of any assignment — `p.Type = x`,
//     `p.Type, p.ID = x, y`, `p.Type += x` — whatever the right-hand side;
//   - a keyed `Type:` value arm (i) does not read (d87TypeValueReadable);
//   - a struct field, in any struct anywhere, whose wire key is `type` under
//     a Go name other than Type — any Go type, any tag order, any case of
//     the tag's name;
//   - a composite literal of a wire-type carrier with an UNKEYED element —
//     spelled by name, qualified, as an anonymous struct, or elided inside a
//     slice/array/map element list, the container literal or NAMED. `go vet`
//     does not flag an unkeyed literal of a same-package type.
//
// Composite literals are descended by hand so an elided element knows the
// type its enclosing literal gives it; everything else is a plain walk.
func d87StructuralUnseenForms(fset *token.FileSet, file *ast.File, carriers map[string]bool, declared map[string][]ast.Expr, lines map[int]bool) {
	mark := func(p token.Pos) { lines[fset.Position(p).Line] = true }
	var walk func(n ast.Node, elided ast.Expr)
	walk = func(n ast.Node, elided ast.Expr) {
		if n == nil {
			return
		}
		ast.Inspect(n, func(x ast.Node) bool {
			switch v := x.(type) {
			case *ast.AssignStmt:
				for _, lhs := range v.Lhs {
					if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "Type" {
						mark(sel.Sel.Pos())
					}
				}
			case *ast.StructType:
				for _, f := range v.Fields.List {
					if len(f.Names) > 0 && f.Names[0].Name != "Type" && strings.EqualFold(d87WireKey(f), "type") {
						mark(f.Pos())
					}
				}
			case *ast.CompositeLit:
				typ := v.Type
				if typ == nil {
					typ = elided
				}
				carrier := d87IsCarrier(typ, carriers)
				keyT, valT := d87ElidedTypes(typ, declared)
				for _, e := range v.Elts {
					kv, keyed := e.(*ast.KeyValueExpr)
					if !keyed {
						if carrier {
							mark(v.Pos())
						}
						walk(e, valT)
						continue
					}
					if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Type" && !d87TypeValueReadable(kv.Value) {
						mark(k.Pos())
					}
					walk(kv.Key, keyT)
					walk(kv.Value, valT)
				}
				return false
			}
			return true
		})
	}
	walk(file, nil)
}

// d87AuthoredTypes extracts every event type a body of Go source AUTHORS —
// the `Type: "..."` and `Type: evtX` forms every payload struct literal in
// this module uses — resolving evt constants from their definitions in the
// same corpus. Returns the sorted set and the constants it could not
// resolve.
func d87AuthoredTypes(sources map[string]string) (types []string, unresolved []string) {
	consts := map[string]string{}
	for _, src := range sources {
		for _, m := range d87ConstDefRe.FindAllStringSubmatch(src, -1) {
			consts[m[1]] = m[2]
		}
	}
	set := map[string]bool{}
	files := make([]string, 0, len(sources))
	for f := range sources {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		src := sources[f]
		for _, m := range d87TypeLiteralRe.FindAllStringSubmatch(src, -1) {
			set[m[1]] = true
		}
		for _, m := range d87TypeConstRe.FindAllStringSubmatch(src, -1) {
			if v, ok := consts[m[1]]; ok {
				set[v] = true
			} else {
				unresolved = append(unresolved, f+": "+m[1])
			}
		}
	}
	for typ := range set {
		types = append(types, typ)
	}
	sort.Strings(types)
	sort.Strings(unresolved)
	return types, unresolved
}

func d87NonTestGoSources(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out[filepath.ToSlash(path)] = string(b)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	return out
}

// TestD87EveryAuthoredEventTypeIsClassifiedAndThePresenceSubsetIsExactlyTheGatedSet
// is the "second write path added later fails the test" half of D-87:
//
//	(i)   the set of event types the module's non-test source AUTHORS equals
//	      authoredEventOwnership's key set — EQUALITY, so a new type with no
//	      classification and a stale row for a type nobody authors both fail;
//	(ii)  the presence-classified subset equals what the funnel gates after
//	      Register — EQUALITY, so a declared presence type with no resolver
//	      and a resolver for a type not declared presence both fail;
//	(iii) every unruled row carries its citation, so "not gated" is on the
//	      record as considered, never as forgotten;
//	(iv)  every authoring form the extractor cannot read occurs ZERO times in
//	      non-test source, so (i) is a census of everything and not only of
//	      what it can see. The forms (d87UnseenAuthoringForms): a raw `"type":`
//	      JSON key in ANY case the decoder accepts, quotes escaped or not; a
//	      Type field assigned after construction from ANY right-hand side,
//	      alone or in a tuple; a keyed `Type:` whose value is not a
//	      wire-alphabet double-quoted literal or an evt identifier (a plain
//	      identifier, a call, a parenthesised or qualified constant, a
//	      backtick literal, a literal spelled outside [a-z_]+ in its RAW
//	      text — an escape such as `"probe\x5fnew"` included); a wire `type`
//	      key on a field not named Type (any Go type, any tag order, any case);
//	      and an UNKEYED composite literal of any struct that carries the wire
//	      `type` key — the struct set derived from the module's own type
//	      declarations (untagged `Type` fields, aliases and defined types over
//	      a carrier included), the literal spelled by name, qualified,
//	      anonymous, or elided inside a slice/array/map element list, the
//	      container spelled literally or as a NAMED type over a carrier
//	      (`type Batch []T; Batch{{…}}`, resolved through any number of
//	      named hops).
//	      Measured 0/0/0 at b3ccb06 (raw / assign-literal / ident); 0/0 at
//	      f5af871 (alphabet / tag); every remaining form was PLANTED and
//	      authored past the census GREEN at cd8fa91, then measured 0 in real
//	      source once flagged (D87-CENSUS-CLOSE.md §1); the named container
//	      and the escaped literal were PLANTED and passed at fda21a5, then
//	      measured 0 (D87-F1-CLOSE.md). A new one must become a wire-alphabet
//	      `Type:` literal, and so be classified, or extend d87AuthoredTypes to
//	      read it.
//
// CONTROL FIRST: the extractor is shown finding both authoring forms in a
// snippet, and the unseen-form scan finding every form it guards against in
// a snippet that also plants the shapes it must ignore, or (i) and (iv)
// could pass over nothing.
func TestD87EveryAuthoredEventTypeIsClassifiedAndThePresenceSubsetIsExactlyTheGatedSet(t *testing.T) {
	ctl, unresolved := d87AuthoredTypes(map[string]string{
		"a.go": "const evtProbeThing = \"probe_thing\"\nfunc f() { _ = X{Type: evtProbeThing}; _ = Y{Type: \"probe_other\"} }",
	})
	if !reflect.DeepEqual(ctl, []string{"probe_other", "probe_thing"}) || len(unresolved) != 0 {
		t.Fatalf("CONTROL FAILED: the extractor found %v (unresolved %v); want [probe_other probe_thing] — (i) cannot fire", ctl, unresolved)
	}
	// One line per element, so the line a form sits on is its index + 1. The
	// snippet must PARSE (the structural arms read the AST); it need not
	// type-check.
	control := strings.Join([]string{
		/*  1 */ "package p",
		/*  2 */ "import \"encoding/json\"",
		/*  3 */ "type K struct {",
		/*  4 */ "\tKind EventType `db:\"k\" json:\"type\"`", // FLAG tag: not named Type, a named Go type, json not the first key
		/*  5 */ "\tType string `json:\"type,omitempty\"`", // ignore: the Go name IS Type
		/*  6 */ "}",
		/*  7 */ "type P struct {",
		/*  8 */ "\tType string `json:\"type\"`",
		/*  9 */ "\tID   string",
		/* 10 */ "}",
		/* 11 */ "type PA = P",
		/* 12 */ "type PD P", // defined type over a carrier: the fixpoint's second constructor
		/* 13 */ "type B []PD", // a NAMED slice over a carrier, through the defined type (F1)
		/* 14 */ "type M map[string]P", // a NAMED map over a carrier (F1)
		/* 15 */ "type NC struct{ ID string }", // not a carrier
		/* 16 */ "type NCs []NC", // a named slice over a non-carrier: its literals are nobody's payload
		/* 17 */ "type U struct {",
		/* 18 */ "\tType string", // untagged: marshals as "Type", which the decoder reads as the type
		/* 19 */ "}",
		/* 20 */ "func f(typ string) {",
		/* 21 */ "\traw := json.RawMessage(`{\"Type\":\"raw_thing\"}`)", // FLAG raw: capitalised key
		/* 22 */ "\tvar p P",
		/* 23 */ "\tp.Type = evtLate", // FLAG assign: from a constant
		/* 24 */ "\tp.Type, p.ID = typ, \"x\"", // FLAG assign: tuple
		/* 25 */ "\tq := P{Type: typ}", // FLAG keyed: plain identifier
		/* 26 */ "\tr := P{Type: evtProbeThing}", // ignore: (i) reads it
		/* 27 */ "\ts := P{Type: \"odd_v2\"}", // FLAG keyed: outside the wire alphabet
		/* 28 */ "\tt := P{Type: (evtProbeThing)}", // FLAG keyed: parenthesised
		/* 29 */ "\tu := P{Type: \"plain_ok\"}", // ignore: (i) reads it
		/* 30 */ "\tv := P{\"pos_thing\", \"x\"}", // FLAG positional: by name
		/* 31 */ "\tw := []PA{{evtProbeThing, \"x\"}}", // FLAG positional: elided element of an ALIAS, constant first
		/* 32 */ "\tx := struct{ Type string `json:\"type\"` }{\"pos_anon\"}", // FLAG positional: anonymous struct
		/* 33 */ "\ty := U{\"pos_untagged\"}", // FLAG positional: untagged carrier
		/* 34 */ "\tz := P{}", // ignore: nothing set
		/* 35 */ "\t_ = PD{\"pos_defined\", \"x\"}", // FLAG positional: defined type
		/* 36 */ "\t_ = B{{\"pos_batch\", \"x\"}}", // FLAG positional: elided element of a NAMED slice (F1)
		/* 37 */ "\t_ = M{\"k\": {\"pos_map\", \"x\"}}", // FLAG positional: elided value of a NAMED map (F1)
		/* 38 */ "\t_ = NCs{{\"x\"}}", // ignore: a named slice over a non-carrier
		/* 39 */ "\t_ = P{Type: \"probe\\x5fnew\"}", // FLAG keyed: an escape inside the literal — unquotes into the alphabet, (i) reads raw text (F4)
		/* 40 */ "\t_ = []byte(\"{\\\"type\\\":\\\"esc_thing\\\"}\")", // FLAG raw: quotes escaped in an interpreted string
		/* 41 */ "\t_ = P{Type: `raw_quoted`}", // FLAG keyed: a backtick literal (i) cannot read
		/* 42 */ "\t_, _, _, _, _, _, _, _, _, _, _, _ = raw, p, q, r, s, t, u, v, w, x, y, z",
		/* 43 */ "}",
	}, "\n")
	wantControl := []string{"b.go:4", "b.go:21", "b.go:23", "b.go:24", "b.go:25", "b.go:27", "b.go:28",
		"b.go:30", "b.go:31", "b.go:32", "b.go:33", "b.go:35", "b.go:36", "b.go:37", "b.go:39", "b.go:40", "b.go:41"}
	if got := d87UnseenAuthoringForms(map[string]string{"b.go": control}); !reflect.DeepEqual(got, wantControl) {
		t.Fatalf("CONTROL FAILED: the unseen-form scan found %v in a snippet planting seventeen forms and six lines it must ignore; want %v — (iv) cannot fire", got, wantControl)
	}

	// The module root (this package runs with cwd = internal/domain), so the
	// census sees domain, deadline, repl, web, config, cmd — every package
	// that could call store.Author.
	sources := d87NonTestGoSources(t, filepath.Join("..", ".."))
	authored, unresolved := d87AuthoredTypes(sources)
	if len(unresolved) != 0 {
		t.Fatalf("authored types behind an evt constant the census could not resolve: %v", unresolved)
	}
	if len(authored) == 0 {
		t.Fatal("PRECONDITION (instrument): the census found no authored types at all")
	}
	if unseen := d87UnseenAuthoringForms(sources); len(unseen) != 0 {
		t.Errorf("(iv) payload-authoring forms the census cannot classify, in non-test source: %v\n"+
			"Build the payload as a struct literal with a `Type:` field (so (i) classifies it), or extend d87AuthoredTypes to read this form.", unseen)
	}
	declared := make([]string, 0, len(authoredEventOwnership))
	for typ := range authoredEventOwnership {
		declared = append(declared, typ)
	}
	sort.Strings(declared)
	if !reflect.DeepEqual(authored, declared) {
		t.Errorf("(i) the authored set and the classified set differ.\n authored (source): %v\n declared (ownership.go): %v\n"+
			"A new authored event type must be classified in authoredEventOwnership — presence (gated), shared reference, mints-own, or unruled with its citation.",
			authored, declared)
	}

	var presence []string
	for typ, row := range authoredEventOwnership {
		switch row.Class {
		case ownPresence:
			presence = append(presence, typ)
		case unruledOpen:
			if !strings.Contains(row.Why, "D-") && !strings.Contains(row.Why, "J2") && !strings.Contains(row.Why, "row ") {
				t.Errorf("(iii) %s is unruled with no citation: %q", typ, row.Why)
			}
		case sharedReference, ownMinted:
		default:
			t.Errorf("%s carries an unknown class %q", typ, row.Class)
		}
	}
	sort.Strings(presence)
	st := newDomainTestStore(t)
	gated := st.OwnershipGatedTypes()
	if !reflect.DeepEqual(presence, gated) {
		t.Errorf("(ii) the presence subset and the funnel's gated set differ.\n presence (ownership.go): %v\n gated (store, after Register): %v", presence, gated)
	}
	// James, 2026-09-30: officer resolution of parked arrivals (Option 1)
	// adds `arrival_resolved` to the D-89 arrivals class — it modifies the
	// arrival's binding, the destination's own presence row, exactly as the
	// retraction does, so it is gated on the same footing. The ruled set is
	// FIVE from that date.
	if want := []string{evtArrivalAmended, evtArrivalRefRetracted, evtArrivalResolved, "trip_cancelled", "visit_checked_out"}; !reflect.DeepEqual(gated, want) {
		t.Errorf("the gated set is %v; the ruled set (D-87 precedents + D-89 arrivals incl. Option 1's resolution, J2/rows 14/17 excluded) is %v — a change here is a ruling, not a refactor", gated, want)
	}
}
