// w8_carrier_test.go — W8 (item 10, the trade register): the CARRIER and
// EVENT-TYPE-VOCABULARY criteria. W8-15's registry-membership limb,
// W8-27(b)'s coverage EQUALITY, W8-14's per-class property, W8-5(b)'s
// two-part instrument, and W8-9(a-2)'s authoring scan.
//
// Written from the LOCKED W8 criteria only. No built implementation was read.
//
// ⚠⚠ W8 IS THE CHUNK WHERE THE COVERAGE PROPERTY BITES HARDEST, AND IT IS
// ARITHMETIC: the event-type vocabulary's change across W8 is NET ZERO —
// `workgroup_created` and `ref_alias_created` LEAVE, `trade_created` and
// `trade_amended` ARRIVE. A ⊇- OR COUNT-SHAPED INVENTORY ASSERTION PASSES
// STRAIGHT THROUGH THE SWAP, and the `ref_alias_created` half of it is what
// [[D-47]] forbids BY NAME: "A freeze list that carries it forward re-creates
// the retired mechanism as a frozen ghost."
package domain

import (
	"encoding/json"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// w8WireEnvelope hand-builds an envelope arriving from another post. ⚠ It
// names NO Go symbol of the unbuilt entity: the type is a WIRE STRING, which
// is the only medium in which trade exists before the build.
func w8WireEnvelope(t *testing.T, siteID string, epoch, seq, atMS int64, payload map[string]any) store.Envelope {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	env := store.Envelope{
		SiteID: siteID, NodeEpoch: epoch, Seq: seq,
		SchemaVersion: store.SchemaVersion, OriginAuth: siteID,
		OccurredAt: atMS, TimeSource: "default",
		Payload: json.RawMessage(raw),
	}
	env.AuthoredAt.WallMS = atMS
	env.AuthoredAt.MaxErrorUS = 42
	env.AuthoredAt.State = store.ClockSynced
	return env
}

// w8ApplyWire folds one wire envelope and REQUIRES it to land. The fold NEVER
// refuses (W3-2), and W8's own W8-5(b) turns on that: an event type nothing
// registers still lands in the log.
func w8ApplyWire(t *testing.T, st *store.Store, env store.Envelope) {
	t.Helper()
	applied, err := st.Apply(env)
	if err != nil {
		t.Fatalf("THE FOLD REFUSED %v: %v — the fold never refuses (W3-2), and W8's merge criteria rest on it", env.Key(), err)
	}
	if !applied {
		t.Fatalf("fixture defect: apply %v deduped — that seq is already in the log", env.Key())
	}
}

// TestW8TradeAmendedIsARegisteredCarrier is W8-15's REGISTRY-MEMBERSHIP limb
// — "the limb this criterion's own title already promises (registry honesty)
// and did not carry" ⟦LOCK — ENU-F3⟧.
//
// ⚠⚠ THIS IS A MEMBERSHIP ASSERTION, SO IT CAN DETECT AN ABSENCE FROM THE
// REGISTRY — which is EXACTLY WHAT W8-14's REGISTRY-DERIVED ENUMERATION
// CANNOT. A set derived from X cannot detect an absence from X; that is why
// (m8-54) was struck from W8-14's Kills: line and re-booked to W8-5(d)(ii).
//
// Landed precedent: w5d_outcome_test.go:86-87.
//
// ⭐ GENUINE PRE-CHUNK RED at runtime, naming no unbuilt Go symbol.
func TestW8TradeAmendedIsARegisteredCarrier(t *testing.T) {
	// PRECONDITION: the registry answers TRUE for a carrier that IS
	// registered, or the assertion below cannot distinguish "absent" from
	// "the function is broken".
	if !IsRegisteredCarrier("person_amended") {
		t.Fatalf("PRECONDITION (instrument): IsRegisteredCarrier(\"person_amended\") = false. The registry is not " +
			"answering, so its answer about trade means nothing")
	}
	if IsRegisteredCarrier("w8_not_a_carrier") {
		t.Fatalf("PRECONDITION (instrument): IsRegisteredCarrier answers TRUE for an invented type — it cannot " +
			"report an absence")
	}

	if !IsRegisteredCarrier("trade_amended") {
		t.Errorf("W8-15 ((m8-53), (m8-57)): IsRegisteredCarrier(%q) = false — trade_amended must join the §4.4 "+
			"registry with the three registration goods.\n"+
			"⚠ This is a MEMBERSHIP assertion, so it CAN detect an absence from the registry. W8-14's "+
			"registry-DERIVED enumeration cannot: a set derived from X cannot detect an absence from X, which is "+
			"why (m8-54) was struck from its Kills: line", "trade_amended")
	}
	// ⚠ AND THE DELETED CARRIERS' HALF. `ref_alias_created` is a NON-carrier
	// and was never registered [MEASURED — outcome.go:120-126 says so in its
	// own comment], so this arm is ALREADY TRUE and is NOT evidence — it is
	// stated so a gate does not book it as one.
	if IsRegisteredCarrier("ref_alias_created") {
		t.Errorf("W8-9: ref_alias_created is a REGISTERED CARRIER. D-47: it \"must not be carried into v10's " +
			"frozen event-type vocabulary\"")
	}
}

// TestW8CarrierRegistrationsAreWiredToADispatchClass is W8-14 limb (iii).
//
// ⚠⚠ THE PROPERTY IS STATED PER CLASS, DERIVED FROM THE REGISTRATION ITSELF —
// because "answered positively by BOTH switches" is FALSE ON THE SHIPPED
// CORRECT BUILD ⟦LOCK — C-5⟧. [MEASURED — outcome.go:572] `Competitors`
// REFUSES `evtOccurredAtCorrected` BY NAME, and occurred_at_corrected IS a
// registered carrier. So the drafted property REDS ON A CORRECT TREE.
//
// THE CLASS IS DERIVABLE FROM THE REGISTRATION, not from a list:
//
//	CorrectionTarget non-nil                      -> the CORRECTION class
//	TargetID / CompanyTargetID / TripTargetID /
//	ArrivalTargetID non-nil                       -> an AMENDMENT class
//	NEITHER                                       -> ⭐ A FAULT — the arm that
//	                                                 catches a class added
//	                                                 with no dispatch at all
//
// ⟦NO PRE-CHUNK RED — the registry is correctly wired today.⟧ Its evidence is
// the PLANT produced below.
//
// ⟦LOCK — R11 per-site amendment, 2026-09-06, reports/witness/R11-DISPATCHCLASS.md:
// THE CLASS IS DERIVED BY REFLECTION OVER carrierRegistration's OWN FUNC FIELDS —
// `CorrectionTarget` non-nil is the correction class; ANY func field whose name
// ends in `TargetID` non-nil is an amendment class. The four-name list this
// replaced was (m8-35)'s rot class INSIDE the check written to prevent it: W7's
// `VehicleTargetID` and W8's `TradeTargetID` landed at 19eafc4, the list went
// short, and a correctly wired registry read as "NEITHER" twice (W8-build.md
// §1.2; S104B disposition 3, STALE BASE). Limb (iii)'s NEITHER arm is unchanged
// and re-proved by the control below; the two once-invisible extractors now
// have their own controls, so a classifier that reverts to a list reds here.⟧
// ⟦was: "TargetID / CompanyTargetID / TripTargetID / ArrivalTargetID non-nil ->
// an AMENDMENT class" — read as a closed list by the classifier.⟧
func TestW8CarrierRegistrationsAreWiredToADispatchClass(t *testing.T) {
	// classOf reads the registration BY SHAPE, never by a list of field names:
	// every func-typed field named `*TargetID` is an amendment extractor and
	// `CorrectionTarget` is the correction extractor. A class added with a new
	// `<Kind>TargetID` is seen without this test learning its name.
	classOf := func(r carrierRegistration) string {
		v := reflect.ValueOf(r)
		var correction, amendment bool
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if f.Type.Kind() != reflect.Func || v.Field(i).IsNil() {
				continue
			}
			switch {
			case f.Name == "CorrectionTarget":
				correction = true
			case strings.HasSuffix(f.Name, "TargetID"):
				amendment = true
			}
		}
		switch {
		case correction && !amendment:
			return "correction"
		case amendment && !correction:
			return "amendment"
		case correction && amendment:
			return "BOTH"
		default:
			return "NEITHER"
		}
	}

	// --- CONTROL FIRST: a registration wired to NEITHER must be classed
	// NEITHER, or limb (iii) cannot fire.
	if got := classOf(carrierRegistration{Facts: []string{"nothing"}}); got != "NEITHER" {
		t.Fatalf("W8-14(iii) CONTROL FAILED: a registration wired to no dispatch class was classed %q, want "+
			"\"NEITHER\". The arm that catches a sixth class added with no dispatch at all cannot fire", got)
	}
	if got := classOf(carrierRegistration{TargetID: func(personAmendmentRow) string { return "" }}); got != "amendment" {
		t.Fatalf("W8-14 CONTROL FAILED: an amendment-shaped registration was classed %q", got)
	}
	// PRECONDITION (instrument): the reflection actually sees the shape it
	// classifies by. A rename of the extractor fields would otherwise class
	// every carrier NEITHER — loud, but for the wrong reason.
	var targetIDFields int
	var sawCorrectionTarget bool
	for rt, i := reflect.TypeOf(carrierRegistration{}), 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.Type.Kind() != reflect.Func {
			continue
		}
		if f.Name == "CorrectionTarget" {
			sawCorrectionTarget = true
		}
		if strings.HasSuffix(f.Name, "TargetID") {
			targetIDFields++
		}
	}
	if !sawCorrectionTarget || targetIDFields == 0 {
		t.Fatalf("PRECONDITION (instrument): carrierRegistration carries CorrectionTarget=%v and %d `*TargetID` func "+
			"fields — the classifier's shape is not on the struct, so its NEITHER verdicts mean nothing", sawCorrectionTarget, targetIDFields)
	}
	// The two extractors the replaced four-name list could not see. A
	// classifier that reverts to a list reds HERE, before the registry loop.
	if got := classOf(carrierRegistration{VehicleTargetID: func(vehicleAmendmentRow) string { return "" }}); got != "amendment" {
		t.Fatalf("W8-14 CONTROL FAILED: a VEHICLE amendment-shaped registration was classed %q — the classifier is reading a list, not the shape", got)
	}
	if got := classOf(carrierRegistration{TradeTargetID: func(tradeAmendmentRow) string { return "" }}); got != "amendment" {
		t.Fatalf("W8-14 CONTROL FAILED: a TRADE amendment-shaped registration was classed %q — the classifier is reading a list, not the shape", got)
	}
	if got := classOf(carrierRegistration{CorrectionTarget: func(occurredAtCorrectionRow) store.Key { return store.Key{} }}); got != "correction" {
		t.Fatalf("W8-14 CONTROL FAILED: a correction-shaped registration was classed %q", got)
	}

	if len(carrierRegistry) == 0 {
		t.Fatal("PRECONDITION (instrument): carrierRegistry is empty — the derivation below would report a clean pass over nothing")
	}
	var names []string
	for typ := range carrierRegistry {
		names = append(names, typ)
	}
	sort.Strings(names)
	for _, typ := range names {
		switch c := classOf(carrierRegistry[typ]); c {
		case "NEITHER", "BOTH":
			t.Errorf("W8-14(iii) ((m8-34), (m8-35)): carrier %q is wired to %s dispatch class. ⚠ The class is "+
				"DERIVED FROM THE REGISTRATION'S OWN FIELDS, never from a list — and this clause is what makes "+
				"the criterion survive DR-7's open question: no W8 criterion depends on carrierRegistration's "+
				"field count or on the number five", typ, c)
		}
	}
}

// TestW8AmendmentFamilyInventoryIsEqualityNotSuperset is W8-27(b) and
// W8-5(b-1) — the SAME instrument, stated once and cited from both.
//
// ⭐ THE COVERAGE PROPERTY IS EQUALITY ON THE FILTERED INVENTORY, NOT ⊇ (M12).
// The landed tooth is reflect.DeepEqual(inv, want) at w4_registry_test.go and
// w5d_outcome_test.go:172-176.
//
// ⭐ BOTH DEGENERACY LOCKS ⟦LOCK — DR-1 + M12⟧:
//
//	(i)  the coverage fixture MUST AUTHOR at least one real trade_amended, so
//	     the live-log enumeration actually observes it — an unauthored family
//	     type makes the assertion pass VACUOUSLY;
//	(ii) the assertion is EQUALITY, because under ⊇ an inventory that DROPS
//	     trade_amended still passes trivially — so (i) is NECESSARY AND NOT
//	     SUFFICIENT.
//
// ⚠ The trade_amended is authored AS A WIRE EVENT because the authoring edge
// is unbuilt. That is not a weakening: W8-5(b)'s own instrument is "the LIVE
// LOG's distinct event types", and a wire-folded event is in the live log.
//
// ⭐ GENUINE PRE-CHUNK RED: the inventory carries trade_amended and the
// registry does not.
func TestW8AmendmentFamilyInventoryIsEqualityNotSuperset(t *testing.T) {
	st := w3NewStore(t)

	// A real, locally-authored amendment, so the inventory is not a fixture
	// of wire events alone.
	co, err := CreateCompany(st, "site-a", 1, "Acme Ltd", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	p, err := CreatePerson(st, "site-a", 1, "Dana Driver", &co.CompanyID, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	newName := "Dana D Driver"
	if _, err := AmendPerson(st, "site-a", 1, PersonAmendment{PersonID: p.PersonID, Name: &newName}, 0, "default"); err != nil {
		t.Fatal(err)
	}

	// (c)(i) THE DEGENERACY LOCK: the fixture AUTHORS a real trade_amended.
	w8ApplyWire(t, st, w8WireEnvelope(t, "site-b", 1, 1, w3Base, map[string]any{
		"type": "trade_amended", "trade_id": "t-canon", "name": "Scaffolding Ltd",
	}))

	inv := w3AmendmentFamilyTypes(t, st, [][2]interface{}{{"site-a", int64(1)}, {"site-b", int64(1)}})

	// The degeneracy lock is asserted, not assumed: if the fixture did not
	// put trade_amended in the log, everything below passes vacuously.
	var sawTrade bool
	for _, typ := range inv {
		if typ == "trade_amended" {
			sawTrade = true
		}
	}
	if !sawTrade {
		t.Fatalf("fixture defect (DR-1's degeneracy closure): the live-log inventory %v does not contain "+
			"trade_amended, so the coverage assertion below would pass VACUOUSLY. The fixture must AUTHOR at "+
			"least one real trade_amended", inv)
	}

	// ⭐ EQUALITY, never ⊇ — because under ⊇ an inventory that DROPS
	// trade_amended still passes trivially.
	want := []string{"person_amended", "trade_amended"}
	if !w8StringSlicesEqual(inv, want) {
		t.Errorf("W8-27(b) ((m8-67)): the W3-12 filtered inventory of the live log is %v, want EXACTLY %v.\n"+
			"⚠ EQUALITY, NOT ⊇: the event-type vocabulary's change across W8 is NET ZERO — workgroup_created and "+
			"ref_alias_created LEAVE, trade_created and trade_amended ARRIVE — so a ⊇- or count-shaped assertion "+
			"passes straight through the swap, and the ref_alias_created half of it is what D-47 forbids by name",
			inv, want)
	}

	// W8-5(b-2) / W8-27(a): every type in the filtered inventory is a
	// REGISTERED CARRIER. ⚠ Note the residual rather than hiding it: the
	// `_amended`/`_corrected` filter WOULD NOT REACH `ref_alias_created`
	// either, which is exactly why W8-5(b) and W8-9(a) cannot share one
	// instrument even after the fix.
	for _, typ := range inv {
		if !IsRegisteredCarrier(typ) {
			t.Errorf("W8-5(b-2) / W8-27(a): %q is present in the live log's amendment-family inventory and is NOT "+
				"a registered carrier. Adding a carrier to one dispatch switch and missing it in the other lands "+
				"it in outcomeMiss, whose IsRegisteredCarrier check is TRUE, so it returns the SANCTIONED arm — "+
				"which is why a per-carrier POSITIVE assertion is required and \"no error\" is not one", typ)
		}
	}

	// CONTROL: a planted THIRD event type appended on the same stream must
	// break the EQUALITY. Produced here so the clean pass, when it comes,
	// counts.
	w8ApplyWire(t, st, w8WireEnvelope(t, "site-b", 1, 2, w3Base+1, map[string]any{
		"type": "w8_planted_amended", "trade_id": "t-canon",
	}))
	if after := w3AmendmentFamilyTypes(t, st, [][2]interface{}{{"site-a", int64(1)}, {"site-b", int64(1)}}); w8StringSlicesEqual(after, inv) {
		t.Errorf("W8-5(b-1) CONTROL FAILED: a planted THIRD amendment-family event type did not change the "+
			"filtered inventory (%v). The instrument cannot see a third carrier, so its equality means nothing", after)
	}
}

func w8StringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// W8-9(a-2) — THE AUTHORING SCAN, "the load-bearing one"
// ---------------------------------------------------------------------------

// TestW8NoDomainFunctionMarshalsADeletedEventType is W8-9(a-2).
//
// "No function in internal/domain marshals a payload whose `type` resolves to
// `ref_alias_created`."
//
// ⚠⚠ THIS IS THE LIMB THAT MAKES W8-9(a) FALSIFIABLE, and the criterion says
// why in terms: read as `carrierRegistry`, limb (a-1) IS ALREADY TRUE AT
// f33c4c7 AND TRUE WHATEVER W8 DOES — alias.go could survive untouched, the
// projection stay registered, applyRefAliasCreated keep folding, and the limb
// is green. ⚠ "Which half produces the red is the half that must not be
// traded away at build."
//
// ⚠ THIS FORM IS IMMUNE TO THE LITERAL MOVING, which the carrierRegistry form
// is not.
//
// ⭐ GENUINE PRE-CHUNK RED: alias.go marshals the literal today.
func TestW8NoDomainFunctionMarshalsADeletedEventType(t *testing.T) {
	deleted := []string{"ref_alias_created", "workgroup_created"}

	// --- CONTROL FIRST: a planted `CreateTradeAlias` that marshals the
	// literal must be detected. ⚠ The criterion names this control; without
	// it the clean pass is a green over a scan that may select nothing.
	planted := "package domain\n\n" +
		"func CreateTradeAlias() []byte {\n" +
		"\treturn []byte(`{\"type\":\"ref_alias_created\"}`)\n" +
		"}\n"
	if hits := w8EventTypeLiterals(t, "planted/alias.go", []byte(planted), deleted); len(hits) == 0 {
		t.Fatalf("W8-9(a-2) CONTROL FAILED: a planted CreateTradeAlias marshalling the literal was NOT detected. " +
			"A scan that cannot see its own plant proves nothing, and this is THE LOAD-BEARING LIMB")
	}
	if hits := w8EventTypeLiterals(t, "planted/clean.go", []byte("package domain\n\nvar ok = true\n"), deleted); len(hits) != 0 {
		t.Fatalf("W8-9(a-2) CONTROL: the scan flagged a clean file: %v", hits)
	}

	dir := filepath.Join(w8AppRoot(t), "internal", "domain")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	var scanned int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		found = append(found, w8EventTypeLiterals(t, e.Name(), src, deleted)...)
	}
	if scanned == 0 {
		t.Fatal("PRECONDITION (instrument): the authoring scan read NO production files — a clean pass over nothing")
	}
	if len(found) != 0 {
		sort.Strings(found)
		t.Fatalf("W8-9(a-2) ((m8-58)): production Go in internal/domain still carries the string spelling of a "+
			"DELETED event type:\n  %s\n"+
			"⚠ THIS IS THE LOAD-BEARING LIMB OF W8-9(a). Read as carrierRegistry, limb (a-1) is ALREADY TRUE at "+
			"f33c4c7 and true WHATEVER W8 DOES: alias.go could survive untouched, the projection stay registered, "+
			"applyRefAliasCreated keep folding, and the limb is green. Trading (a-2) away at build leaves the "+
			"criterion green and the defect shipped.\n"+
			"⚠ The vocabulary's arithmetic across W8 is NET ZERO, so an inventory assertion cannot see this — "+
			"D-47 forbids the ref_alias_created half BY NAME",
			strings.Join(found, "\n  "))
	}
}

// w8EventTypeLiterals tokenises Go SOURCE and reports every string literal
// whose value contains one of the deleted event-type spellings.
//
// ⚠ COMMENTS ARE SKIPPED: a comment narrating a deletion is not a live
// reference. ⚠ The scan is over LITERAL VALUES, not identifiers — an event
// type in this system "is a string literal compared inside an Apply body"
// [MEASURED — store.Projection carries Name/Table/OrderBy/Schema/Apply/
// Finalize AND NO EVENT-TYPE FIELD], so an identifier scan would see nothing.
func w8EventTypeLiterals(t *testing.T, path string, src []byte, deleted []string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file := fset.AddFile(path, fset.Base(), len(src))
	var sc scanner.Scanner
	sc.Init(file, src, nil, 0) // comments skipped
	var hits []string
	for {
		pos, tok, lit := sc.Scan()
		if tok == token.EOF {
			break
		}
		if tok != token.STRING {
			continue
		}
		val := lit
		if unq, err := strconv.Unquote(lit); err == nil {
			val = unq
		}
		for _, d := range deleted {
			if strings.Contains(val, d) {
				hits = append(hits, fset.Position(pos).String()+": "+d)
			}
		}
	}
	return hits
}
