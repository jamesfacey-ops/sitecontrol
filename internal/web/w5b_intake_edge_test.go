// w5b_intake_edge_test.go — W5b AT THE INTAKE EDGE, which is the ONE handler
// phone is mandatory at:
//
//	W5b-1     the refusal is ABOVE THE AUTHORING LINE — no event minted, no
//	          projection row written, asserted by reading the LOG and the
//	          `person`/`company` tables, never by inspecting the handler
//	W5b-2(a)  the refusal preserves everything typed, asserted as `value="…"`
//	          on the form CONTROL — never strings.Contains over the body
//	W5b-2(b)  no err.Error() string appears in the rendered refusal for the
//	          internal-error arm
//	W5b-0     the AUTHORING path stores the phone byte-faithfully
//	W5b-4     the literal-`handleIntake` reading: no intake POST can enrol a
//	          person whose phone ends up absent
//
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31.
//
// ⚠ D-52 BOUNDS THIS WHOLE FILE. "Phone is enrolment-mandatory" is true of
// INTAKE and FALSE OF THE SYSTEM: `person_created` is authored from two
// doors, and James ruled the gate's add-on-demand door EXEMPT. Every
// mandatoriness assertion here is therefore made OF `handleIntake` AND OF
// `handleIntake` ONLY. The matching assertion that the gate door SUCCEEDS
// without a phone is in internal/domain/w5b_created_half_test.go
// (TestW5bGateAddOnDemandCreatesAPersonWithNoPhone) — a build that satisfies
// this file by putting the check in `createPerson` passes every test here and
// breaks how most persons enter the roster.
//
// ⚠ THIS FILE NAMES NO UNBUILT GO SYMBOL. `intakeForm` gains a Phone field at
// this build; nothing below selects it. Fixtures are url.Values posted over
// real HTTP and assertions read rendered HTML, so the file compiles and runs
// against `88dd6a6` and every pre-chunk failure is a produced RUNTIME red.
//
// SEAMS bound here, once; disputes go to the gate:
//
//   - SEAM W5b-S-FORM: the intake form's phone control is the one whose
//     `name` attribute is `phone` — the house convention is that a form field
//     name IS the payload/column spelling (`last_name`, `first_name`,
//     `company_id`, `new_company` at intake.html:159/:163/:180/:196), and
//     W5-X4(b) fixes the column spelling as `phone`. The control's `id` is
//     NOT pinned: w5bControlValue matches on `name` and accepts `id` as a
//     fallback, which is a deliberately WIDER matcher than W5a's id-only one.
//   - SEAM W5b-S-REFUSAL: a refused intake answers 4xx and re-renders the
//     form. 409 is the measured house value (`refuse` → renderRoster with
//     StatusConflict, intake.go:416–418) but NO criterion states a number, so
//     4xx is asserted and 409 is not pinned — a pinned number would red a
//     builder who chose 400 for a reason no document forbids.
package web

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"

	"path/filepath"
)

// --- machinery -----------------------------------------------------------

// w5bWebServer builds a post with a login password and NO seeded people.
func w5bWebServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "w5b-web.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SiteID: "site-a", DisplayName: "site-a", ListenAddr: "127.0.0.1:0",
		NodeEpoch: 1, DBPath: st.Path, SyncIntervalSeconds: 10,
		LoginPassword: testLoginPassword,
	}
	return New(cfg, st), st
}

// w5bIntakeServer is a post with one company on file, so intake has something
// to attach to — a person with no company is refused for a DIFFERENT reason,
// which would make every assertion below pass for the wrong cause.
func w5bIntakeServer(t *testing.T) (*Server, *store.Store, string) {
	t.Helper()
	srv, st := w5bWebServer(t)
	co, err := domain.CreateCompany(st, "site-a", 1, "Kakivik Asset Management", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	return srv, st, co.CompanyID
}

// w5bForm builds an intake POST body. `phone` is written under SEAM
// W5b-S-FORM's key; pass w5bNoPhone to omit the key entirely, which is a
// different wire fact from an empty box.
func w5bForm(last, first, companyID, newCompany, tradeID, newTrade, phone string) url.Values {
	v := url.Values{
		"last_name": {last}, "first_name": {first},
		"company_id": {companyID}, "new_company": {newCompany},
		// ⟦LOCK — §87, 2026-09-03⟧ LOCK-TIME instrument re-pointed; the property is KEPT.
		// ⚠ THESE ARE FORM KEYS — plain map strings, so no compiler could see them go stale
		// against intake.go:470-471, which reads `trade_id` / `new_trade`. Same root as the
		// intakeForm2 defect (§4.9): the helper was posting fields the page does not render,
		// so w5bPreservationFaults reported them as MISSING CONTROLS and the control below
		// counted 3 faults where the affirmation-only plant must produce exactly 1.
		// ⟦was: "\t\t\"workgroup_id\": {tradeID}, \"new_workgroup\": {newTrade},"⟧
		"trade_id": {tradeID}, "new_trade": {newTrade},
	}
	if phone != w5bNoPhone {
		v.Set("phone", phone)
	}
	return v
}

// w5bNoPhone is the sentinel for "the key is absent from the POST body". A
// real phone can never equal it.
const w5bNoPhone = "\x00absent\x00"

var w5bInputRe = regexp.MustCompile(`(?is)<input\b[^>]*>`)
var w5bAttrRe = regexp.MustCompile(`(?is)([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*"([^"]*)"`)

// w5bControlValue returns the `value="…"` of the form control whose `name`
// attribute is `field` (falling back to a matching `id`), and whether such a
// control exists at all.
//
// ⚠ THE ASSERTION SHAPE IS NOT A FREE CHOICE (W5-X2, made standing after
// W4-F1): a refusal-preservation assertion is made on the form CONTROL, never
// with strings.Contains over the response body. W4's gate found a
// `strings.Contains(body, wall)` assertion that passed on the refusal
// SENTENCE quoting the value back, so a mutant that discarded the officer's
// typing survived it. TestW5bIntakeRefusalPreservesTypedPhoneInItsFormControl
// demonstrates that failure rather than restating it.
func w5bControlValue(body, field string) (value string, found bool) {
	for _, tag := range w5bInputRe.FindAllString(body, -1) {
		attrs := map[string]string{}
		for _, m := range w5bAttrRe.FindAllStringSubmatch(tag, -1) {
			attrs[strings.ToLower(m[1])] = m[2]
		}
		if attrs["name"] == field || attrs["id"] == field {
			return attrs["value"], true
		}
	}
	return "", false
}

// ⚠ WIDENED POST-LOCK — A-3, reports/W5-POSTLOCK-AMENDMENTS.md.
//
// Everything from here to w5bAssertPreserved exists because W5b-2(a)'s
// original filter was a HAND-KEPT THREE-FIELD LIST matched by `value="…"`,
// while the criterion's subject is *"every field the officer entered"*. The
// form carries EIGHT operator-settable controls, and the one the list could
// never reach — `same_name`, a checkbox, preserved by `checked` and not by
// `value=` — is the one that shipped broken: W5b's phone refusal is the first
// refusal in this handler to sit BELOW the duplicate check, so a refusal
// could fire with the officer's affirmation already given, and the page it
// re-rendered had no affirmation control on it at all. That is (m5b-2)'s own
// registered shape — a typed field discarded on re-render — live in the tree
// that booked (m5b-2) as killed.
//
// The remedy is the one W5-X6's standing line prescribes one level up: the
// asserted field set is DERIVED FROM THE RENDERED PAGE, never from a list
// kept beside it. A list has to be remembered when a control is added; a
// derivation cannot be forgotten.
//
// NOTHING BELOW REPLACES OR RELAXES THE THREE `value="…"` CHECKS ABOVE — they
// stay exactly as locked, with their own sentences, and the sweep is added
// beside them. W5-X2's rule is unchanged and reinforced: preservation is read
// off the CONTROL (a text box by `value="…"`, a checkbox by `checked`, a
// picker by `selected`), never by strings.Contains over the body.

// w5bIntakeFormRe isolates the intake form itself. Scoping matters: a second
// form on this page carrying a control of the same `name` would let a sweep
// read preservation off the wrong one.
var w5bIntakeFormRe = regexp.MustCompile(`(?is)<form\b[^>]*action\s*=\s*"/roster/intake"[^>]*>(.*?)</form\s*>`)

// w5bCtrlTagRe matches every control-bearing tag inside that form, plus the
// `</select>` that ends an option group.
//
// ⚠ `<textarea>` is deliberately ABSENT: its value is its element content,
// not an attribute, and this form has none today. If one is ever added, the
// sweep reds with "no control named …" rather than passing blind — which is
// the correct direction, because it demands the parser be taught instead of
// silently exempting the new control. That is the whole lesson of A-3.
var w5bCtrlTagRe = regexp.MustCompile(`(?is)<(?:input|select|option)\b[^>]*>|</select\s*>`)

// w5bAnyAttrRe matches attributes WITH or WITHOUT a value, because `checked`
// and `selected` — the two attributes that carry a checkbox's and a picker's
// state — are bare. w5bAttrRe above cannot see either, which is precisely
// why the original filter could not have reached `same_name` even with the
// name on its list.
var w5bAnyAttrRe = regexp.MustCompile(`(?is)([a-zA-Z_:][-a-zA-Z0-9_:.]*)(?:\s*=\s*"([^"]*)")?`)

// w5bTagAttrs parses one tag's attributes. A present-but-valueless attribute
// maps to "" and is distinguished from an absent one by the map lookup's
// second return, which is how `checked` is read.
func w5bTagAttrs(tag string) map[string]string {
	inner := tag
	if i := strings.IndexAny(inner, " \t\r\n"); i >= 0 {
		inner = inner[i:]
	} else {
		inner = ""
	}
	attrs := map[string]string{}
	for _, m := range w5bAnyAttrRe.FindAllStringSubmatch(inner, -1) {
		attrs[strings.ToLower(m[1])] = m[2]
	}
	return attrs
}

// w5bRenderedForm returns what a BROWSER WOULD POST from the intake form on
// this page — every successful control, in the state the page rendered it.
// This is the subject W5b-2(a) actually names: not three fields, but whatever
// the form has.
//
// It is fail-closed by construction. A parser that matched nothing returns an
// empty set, and every field the officer typed is then reported MISSING —
// there is no arrangement in which a broken enumeration passes quietly, which
// is the failure mode the hand-kept list had.
func w5bRenderedForm(t *testing.T, body string) url.Values {
	t.Helper()
	forms := w5bIntakeFormRe.FindAllStringSubmatch(body, -1)
	if len(forms) == 0 {
		t.Fatalf("W5b-2(a): the page carries no <form … action=\"/roster/intake\"> at all, so there is nowhere for ANY of the officer's typing to be preserved. Body (first 400 bytes): %s", w5bClip(body))
	}
	if len(forms) > 1 {
		t.Fatalf("W5b-2(a): the page carries %d intake forms; the sweep cannot say which one the officer was working in", len(forms))
	}

	got := url.Values{}
	var sel string
	for _, tag := range w5bCtrlTagRe.FindAllString(forms[0][1], -1) {
		low := strings.ToLower(tag)
		attrs := w5bTagAttrs(tag)
		switch {
		case strings.HasPrefix(low, "</select"):
			sel = ""
		case strings.HasPrefix(low, "<select"):
			sel = attrs["name"]
		case strings.HasPrefix(low, "<option"):
			// A picker preserves by `selected` on the option, which is why
			// company_id and workgroup_id were invisible to a value="…"
			// filter too. This page always marks one option selected.
			if sel == "" {
				continue
			}
			if _, ok := attrs["selected"]; ok {
				got.Set(sel, html.UnescapeString(attrs["value"]))
			}
		default: // <input>
			name := attrs["name"]
			if name == "" {
				continue
			}
			switch strings.ToLower(attrs["type"]) {
			case "checkbox", "radio":
				// A checkbox is a successful control ONLY when checked, and
				// it submits its `value` (or "on" when it has none). This is
				// the branch A-3 lived behind.
				if _, ok := attrs["checked"]; ok {
					v := html.UnescapeString(attrs["value"])
					if v == "" {
						v = "on"
					}
					got.Set(name, v)
				}
			case "submit", "button", "image", "reset":
				// Not successful controls unless they are the one clicked.
			default:
				got.Set(name, html.UnescapeString(attrs["value"]))
			}
		}
	}
	return got
}

// w5bPreservationFaults is the comparison: for EVERY key the officer's
// browser sent, the re-rendered form must send the same thing back. One
// sentence per fault, naming the control.
func w5bPreservationFaults(submitted, rendered url.Values) []string {
	var keys []string
	for k := range submitted {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var faults []string
	for _, k := range keys {
		want := submitted.Get(k)
		got, ok := rendered[k]
		if !ok {
			faults = append(faults, fmt.Sprintf(
				"the officer submitted %s=%q and the refusal page carries NO control that would submit it back. A text box preserves by value=\"…\", a picker by `selected` on its option, a checkbox by `checked` — this control does none of them, because it is not on the page. The officer's answer is gone and the form they were handed cannot carry it",
				k, want))
			continue
		}
		if got[0] != want {
			faults = append(faults, fmt.Sprintf(
				"the officer submitted %s=%q and the refusal page re-renders it as %q. Everything the officer entered comes back INTO ITS FORM CONTROL: a refusal that costs them their typing trains them to work around the form",
				k, want, got[0]))
		}
	}
	return faults
}

// w5bAssertPreserved is the whole-form sweep. `submitted` must be what the
// officer's browser actually POSTed.
func w5bAssertPreserved(t *testing.T, label string, submitted url.Values, body string) {
	t.Helper()
	if len(submitted) == 0 {
		t.Fatalf("W5b-2(a) NON-DEGENERACY, %s: the sweep was handed an empty submission and would pass having checked nothing", label)
	}
	for _, f := range w5bPreservationFaults(submitted, w5bRenderedForm(t, body)) {
		t.Errorf("W5b-2(a) (m5b-2), %s: %s", label, f)
	}
}

// w5bStreamLen is the number of events on this post's own stream. Author
// appends only at watermark+1, so the watermark IS the length.
func w5bStreamLen(t *testing.T, srv *Server) int64 {
	t.Helper()
	wm, err := srv.Store.Watermark(srv.Cfg.SiteID, srv.Cfg.NodeEpoch)
	if err != nil {
		t.Fatalf("watermark: %v", err)
	}
	return wm
}

type w5bState struct {
	Events    int64
	Persons   int
	Companies int
	Trades    int
	CoNames   []string
}

func w5bSnapshot(t *testing.T, srv *Server) w5bState {
	t.Helper()
	ps, err := domain.Persons(srv.Store)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := domain.Companies(srv.Store)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := domain.Trades(srv.Store)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range cs {
		names = append(names, c.Name)
	}
	return w5bState{Events: w5bStreamLen(t, srv), Persons: len(ps), Companies: len(cs), Trades: len(ws), CoNames: names}
}

func w5bRefused(code int) bool { return code >= 400 && code < 500 }

// w5bClip keeps a failure message readable: a whole roster page in a test log
// buries the line that matters.
func w5bClip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 400 {
		return s[:400] + " …"
	}
	return s
}

// =======================================================================
// W5b-1 — THE REFUSAL IS ABOVE THE AUTHORING LINE
// =======================================================================

// TestW5bPhonelessIntakeIsRefusedAboveTheAuthoringLine — W5b-1.
//
// EXPECTED RED (pre-chunk, genuine, unaided): there is no phone field on the
// intake form at all — intake.go:29–33 says so in terms ("There is no
// phone-number field on this form, and that is not an oversight"), and
// templates/intake.html:26 and :248 tell the operator the same. A phoneless
// POST therefore ENROLS today: the test reds on the 303 redirect and on the
// person and company rows that appeared.
//
// THE FIXTURE IS THE STANDING RULE'S OWN WORKED EXAMPLE, and that is why it
// asks for a NEW COMPANY. intake.go:421–424: "EVERYTHING IS VALIDATED BEFORE
// ANYTHING IS AUTHORED. A refusal must leave no trace: half an intake — a
// company row minted for a person who was then refused — is a duplicate
// company nobody asked for, on the table the muster groups by." A phone check
// placed BELOW the authoring line leaves exactly that company row. A test
// that only asserted the status code could not see it.
//
// Kills (m5b-1)'s first arm — "refusal below the authoring line (event minted
// then refused)". The plant is real and produced: a phone check inserted
// after `CreateCompanyRecordedBy` in handleIntake still answers 4xx, and this
// test still reds — on the company row and the log.
func TestW5bPhonelessIntakeIsRefusedAboveTheAuthoringLine(t *testing.T) {
	srv, _, _ := w5bIntakeServer(t)
	const newCo = "Nanuq Drilling Services"

	before := w5bSnapshot(t, srv)
	rec := postForm(t, srv, "/roster/intake",
		w5bForm("Ferguson", "Mary", newRef, newCo, "", "", w5bNoPhone))

	if !w5bRefused(rec.Code) {
		t.Errorf("W5b-1: an enrolment with no phone returned %d, want a 4xx refusal (SEAM W5b-S-REFUSAL). Phone is enrolment-mandatory AT THIS HANDLER — [[D-52]]: mandatory at intake, the surface where the roster is built in advance", rec.Code)
	}

	after := w5bSnapshot(t, srv)
	if after.Events != before.Events {
		t.Errorf("W5b-1 (m5b-1): the refused enrolment appended %d event(s) — the log went %d → %d. The refusal is BELOW the authoring line: an envelope that exists has already been minted, hashed and made replicable, and a refusal after that point cannot un-say it", after.Events-before.Events, before.Events, after.Events)
	}
	if after.Companies != before.Companies {
		t.Errorf("W5b-1 (m5b-1): the refused enrolment minted %d company row(s) — %v became %v. This is the standing rule's own worked example: \"half an intake — a company row minted for a person who was then refused — is a duplicate company nobody asked for, on the table the muster groups by\" (intake.go:421–424)", after.Companies-before.Companies, before.CoNames, after.CoNames)
	}
	if after.Persons != before.Persons {
		t.Errorf("W5b-1 (m5b-1): the refused enrolment wrote %d person row(s)", after.Persons-before.Persons)
	}
	if after.Trades != before.Trades {
		t.Errorf("W5b-1 (m5b-1): the refused enrolment minted %d workgroup row(s)", after.Trades-before.Trades)
	}

	// --- NON-DEGENERACY, and it is not optional (standing rule 5). "Nothing
	// was authored" is trivially true of a handler that authors nothing ever,
	// or of a POST that never reached the route. The SAME SHAPE of form, WITH
	// a phone, must enrol and must mint the company it asked for.
	//
	// It uses a different person and a different new company deliberately: on
	// the PRE-CHUNK tree the refused POST above ENROLS (there is no phone box
	// yet), so re-posting the same name would be refused by the duplicate
	// check and this control would report a build failure that is really the
	// pre-chunk state.
	mid := w5bSnapshot(t, srv)
	rec = postForm(t, srv, "/roster/intake",
		w5bForm("Iqaluk", "Sam", newRef, "Tuktu Logistics", "", "", "01463 555 0134"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("W5b-1 NON-DEGENERACY: an enrolment of the same shape WITH a phone returned %d, want 303. Either intake refuses for a reason unrelated to phone — in which case the refusal above proves nothing — or the build refuses every enrolment. Body (first 400 bytes): %s", rec.Code, w5bClip(rec.Body.String()))
	}
	got := w5bSnapshot(t, srv)
	if got.Persons != mid.Persons+1 || got.Companies != mid.Companies+1 {
		t.Errorf("W5b-1 NON-DEGENERACY: the accepted enrolment wrote %d person and %d company row(s), want 1 and 1 — the fixture cannot show that the refusal above WITHHELD anything", got.Persons-mid.Persons, got.Companies-mid.Companies)
	}
}

// =======================================================================
// W5b-2(a) — the refusal preserves everything typed, on the CONTROL
// =======================================================================

// TestW5bIntakeRefusalPreservesTypedPhoneInItsFormControl — W5b-2(a).
//
// EXPECTED RED (pre-chunk, genuine): the intake form has no phone control at
// all, so the CONTROL block below fails first with "the refusal page has no
// control named \"phone\"". After the build it is the planted
// field-discarding re-render that carries the tooth.
//
// ⚠ ACCOUNTING, per §5's rule and W5a's own note: W5a-7 and W5b-2 assert the
// same property on the same form, and ONE produced red may not be booked for
// two criteria. W5a's plant discards a NAME PART. **This criterion's plant
// discards THE PHONE, and nothing else** — the form is re-rendered with every
// other field intact, which is what a re-render that dropped only the new
// field looks like and is the only plant W5b-2(a) may book.
//
// Kills (m5b-2).
func TestW5bIntakeRefusalPreservesTypedPhoneInItsFormControl(t *testing.T) {
	srv, _, companyID := w5bIntakeServer(t)
	const typedPhone = "(907) 555-0134"
	const typedLast = "Ferguson"
	const typedNewWG = "Night shift"

	// --- CONTROL FIRST — the planted phone-discarding re-render. This is not
	// a synthetic string: it is the REAL renderRoster path with a form whose
	// PHONE is empty and whose every other field is intact, which is
	// byte-for-byte what a re-render that dropped only the new field
	// produces. If the assertion cannot tell it from the real refusal, the
	// assertion proves nothing.
	rec := httptest.NewRecorder()
	srv.renderRoster(rec, httptest.NewRequest("GET", "/roster", nil), http.StatusConflict,
		intakeForm{Last: typedLast, First: "Mary", CompanyID: companyID, NewTrade: typedNewWG},
		"planted: a refusal that discarded the phone the officer typed", "", false)
	discarded := rec.Body.String()
	v, found := w5bControlValue(discarded, "phone")
	if !found {
		t.Fatalf("W5b-2(a) / CONTROL: the refusal page has NO control named \"phone\" (SEAM W5b-S-FORM) — there is nowhere for the officer's number to be preserved, so W5b-2(a) cannot be met.\nThis IS the pre-chunk state: intake.go:29–33 records that the form deliberately has no phone box, and templates/intake.html:248 tells the operator so. W5b's third build product is that box")
	}
	if v != "" {
		t.Fatalf("CONTROL FAILED: the phone-discarding re-render put %q in the phone control; the plant is not a plant", v)
	}
	// The OTHER controls must still carry their values in the plant, or the
	// plant is "a refusal that discarded everything" — W5a-7's plant, not
	// this one, and booking it here would double-count one red across two
	// criteria (§5's accounting rule).
	if got, ok := w5bControlValue(discarded, "last_name"); !ok || got != typedLast {
		t.Fatalf("CONTROL FAILED: the phone-only plant also discarded last_name (got %q, present=%v) — that is W5a-7's plant. W5b-2(a)'s plant must discard THE PHONE and nothing else", got, ok)
	}
	// And the W4-F1 demonstration, made once rather than asserted: a
	// body-contains assertion CANNOT tell the plant from the real refusal
	// when the refusal sentence quotes the value back, which is why W5-X2
	// forbids that shape.
	if strings.Contains(discarded, typedPhone) {
		t.Fatalf("CONTROL: the discarding re-render already contains %q — the fixture cannot demonstrate W5-X2's point", typedPhone)
	}

	// --- THE REAL REFUSAL. The fault is a half-entered name, NOT the phone:
	// the phone is typed and valid, so the only thing that can lose it is the
	// re-render. (A refusal caused BY the missing phone has an empty phone
	// box and cannot pose this case.)
	halfName := w5bForm("Ferguson", "", companyID, "", newRef, typedNewWG, typedPhone)
	rec2 := postForm(t, srv, "/roster/intake", halfName)
	if !w5bRefused(rec2.Code) {
		t.Fatalf("fixture defect: a half-entered name returned %d, want a 4xx refusal — requiredness at the intake edge is intake.go:426–429's existing rule and this fixture depends on it", rec2.Code)
	}
	body := rec2.Body.String()
	for _, c := range []struct{ field, want string }{
		{"phone", typedPhone},
		{"last_name", "Ferguson"},
		// ⟦LOCK — §87, 2026-09-03⟧ LOCK-TIME instrument re-pointed; the property is KEPT.
		// W5b-2(a) asserts the officer's typing re-renders into its own control. The CONTROL
		// NAME moved with the entity rename (intake.go:470-471 reads `new_trade`); the
		// promise — nothing typed is lost at a refusal — is untouched.
		// ⟦was: "\t\t{\"new_workgroup\", typedNewWG},"⟧
		{"new_trade", typedNewWG},
	} {
		got, ok := w5bControlValue(body, c.field)
		if !ok {
			t.Errorf("W5b-2(a) (m5b-2): the refusal page has no control named %q — the officer's typing has nowhere to be re-rendered into", c.field)
			continue
		}
		if got != c.want {
			t.Errorf("W5b-2(a) (m5b-2): after the refusal the %s control carries value=%q, want %q. Everything the officer entered re-renders INTO ITS FORM CONTROL: retyping a phone number at a window with a bus outside is how a second, wrong record gets made", c.field, got, c.want)
		}
	}
	// AND THE WHOLE FORM, derived from the page rather than listed here
	// (A-3). The three named fields above stay; this catches the other five,
	// including the two <select>s, which a value="…" filter cannot see.
	w5bAssertPreserved(t, "the half-entered-name refusal", halfName, body)

	// --- AND THE OTHER DIRECTION: the phone-triggered refusal must preserve
	// everything else. Both refusals are asserted because a build can pass
	// one and fail the other — the new refusal arm is a new call site.
	phoneRefused := w5bForm("Ferguson", "Mary", newRef, "Nanuq Drilling Services", "", "", "")
	rec3 := postForm(t, srv, "/roster/intake", phoneRefused)
	if !w5bRefused(rec3.Code) {
		t.Fatalf("W5b-2(a): an enrolment with an EMPTY phone box returned %d, want a 4xx refusal", rec3.Code)
	}
	body = rec3.Body.String()
	for _, c := range []struct{ field, want string }{
		{"last_name", "Ferguson"},
		{"first_name", "Mary"},
		{"new_company", "Nanuq Drilling Services"},
	} {
		got, ok := w5bControlValue(body, c.field)
		if !ok {
			t.Errorf("W5b-2(a) (m5b-2): the phone refusal's page has no control named %q", c.field)
			continue
		}
		if got != c.want {
			t.Errorf("W5b-2(a) (m5b-2): the phone refusal discarded %s — value=%q, want %q. A refusal that costs the officer their typing trains them to work around the form", c.field, got, c.want)
		}
	}
	w5bAssertPreserved(t, "the phone refusal", phoneRefused, body)
}

// TestW5bPhoneRefusalPreservesTheDuplicateAffirmation — W5b-2(a), WIDENED
// POST-LOCK (A-3). This is the criterion applied to the control its original
// filter could not reach, and to the journey that reaches it.
//
// ⚠ THE JOURNEY, which is the whole point. W5b's phone refusal is the FIRST
// refusal in handleIntake to sit BELOW the duplicate check. So an officer can
// now be refused for the duplicate, ANSWER IT — "this is a different person
// with the same name" — and be refused again for the phone. Before the fix
// that second refusal re-rendered a page with no `same_name` control on it at
// all, so the browser's next POST could not carry the answer, and the officer
// was told a second time that the person "is already on the roster … do not
// enrol them again". At forty below with people waiting, the plausible
// response to being told that twice is to believe it — and the outcome the
// duplicate refusal exists to make deliberate (a real second person with a
// shared name left off the roster) becomes the path of least resistance.
//
// ⚠ ACCOUNTING (§5's rule, and the same care W5a-7/W5b-2 needed): this test's
// plant discards THE AFFIRMATION AND NOTHING ELSE. W5a-7's discards a name
// part; W5b-2(a)'s discards the phone. Three distinct plants, three distinct
// reds, no red booked twice.
//
// Kills (m5b-2) on the control the three-field filter could not see.
func TestW5bPhoneRefusalPreservesTheDuplicateAffirmation(t *testing.T) {
	srv, _, companyID := w5bIntakeServer(t)
	const typedPhone = "(907) 555-0134"
	// An ampersand, so the sweep is exercised against HTML-escaped output:
	// a comparison that forgot to unescape would red here rather than pass.
	const typedNewWG = "Night shift & relief"

	// Somebody of this name is already on the roster, enrolled through the
	// real door so the row is authored exactly as intake authors one.
	seed := postForm(t, srv, "/roster/intake",
		w5bForm("Ferguson", "Mary", companyID, "", "", "", "(907) 555-0100"))
	if seed.Code != http.StatusSeeOther {
		t.Fatalf("fixture defect: seeding the first Ferguson returned %d, want 303. Body: %s", seed.Code, w5bClip(seed.Body.String()))
	}

	// --- CONTROL FIRST — the affirmation-discarding re-render, produced
	// through the REAL renderRoster path, not as a synthetic string.
	//
	// ⚠ WHAT THIS PLANT IS. It is the page a refusal produces when it does
	// NOT carry the officer's affirmation forward: their typing intact, and
	// no `same_name` control anywhere on it. That is the delivered defect's
	// observable exactly — `same_name` appears in ONE place in
	// templates/intake.html, inside `{{if .OfferSameName}}`, so a refusal
	// that renders with OfferSameName false emits no affirmation control at
	// all and the browser's next POST cannot carry the tick.
	//
	// ⚠ AND WHY IT IS BUILT THIS WAY RATHER THAN BY PASSING SameName:true.
	// The fix is `OfferSameName: offerSameName || form.SameName`, so passing
	// a ticked form now RENDERS the control — the plant would no longer be a
	// plant, and a control that stops planting once the bug is fixed proves
	// nothing thereafter. The plant is therefore stated as the OBSERVABLE
	// (typing preserved, affirmation control absent), which stays a plant
	// however the handler is written. The produced red against the ORIGINAL
	// defect — the fix reverted in the handler, this test watched go red — is
	// recorded in reports/W5-POSTLOCK-AMENDMENTS.md `A-3`.
	rec := httptest.NewRecorder()
	srv.renderRoster(rec, httptest.NewRequest("GET", "/roster", nil), http.StatusConflict,
		intakeForm{Last: "Ferguson", First: "Mary", Phone: "", CompanyID: companyID,
			TradeID: newRef, NewTrade: typedNewWG},
		"planted: a refusal that discarded the officer's duplicate-name affirmation", "", false)
	discarded := rec.Body.String()
	if v, found := w5bControlValue(discarded, "same_name"); found {
		t.Fatalf("CONTROL FAILED: the plant renders a same_name control (value=%q), so it is not a plant — it does not pose the case where the officer's affirmation has nowhere to go", v)
	}

	affirmed := w5bForm("Ferguson", "Mary", companyID, "", newRef, typedNewWG, "")
	affirmed.Set("same_name", "1")

	faults := w5bPreservationFaults(affirmed, w5bRenderedForm(t, discarded))
	if len(faults) == 0 {
		t.Fatalf("CONTROL FAILED: the sweep cannot tell an affirmation-discarding re-render from a faithful one, so every green it reports below is worth nothing. This is the exact blindness A-3 recorded: W5b-2(a)'s original filter was three field names matched by `value=\"…\"`, and a checkbox preserves by `checked`")
	}
	sawSameName := false
	for _, f := range faults {
		if strings.Contains(f, "same_name") {
			sawSameName = true
		}
	}
	if !sawSameName {
		t.Fatalf("CONTROL FAILED: the sweep flagged %d fault(s) on the plant but none of them names same_name — it is red for some other reason and does not reach the control A-3 is about: %v", len(faults), faults)
	}
	// The plant must discard THE AFFIRMATION AND NOTHING ELSE, or the red
	// booked here is W5a-7's or W5b-2(a)'s and not this one.
	if len(faults) != 1 {
		t.Fatalf("CONTROL FAILED: the affirmation-only plant produced %d faults, want exactly 1 — a plant that also drops other fields is a different criterion's plant and booking it here would double-count one red: %v", len(faults), faults)
	}

	// --- THE REAL JOURNEY, three rounds, each submitting EXACTLY the form
	// the previous page rendered — which is what a browser does and what a
	// hand-built url.Values quietly does not.
	round1 := w5bForm("Ferguson", "Mary", companyID, "", newRef, typedNewWG, "")
	r1 := postForm(t, srv, "/roster/intake", round1)
	if !w5bRefused(r1.Code) {
		t.Fatalf("fixture defect: a second person of an existing name returned %d, want a 4xx duplicate refusal — intake.go's duplicate check is what this journey rides on", r1.Code)
	}
	b1 := r1.Body.String()
	if _, ok := w5bRenderedForm(t, b1)["same_name"]; ok {
		t.Fatalf("fixture defect: the duplicate refusal already renders same_name as CHECKED before the officer has ticked anything — the journey below would prove nothing")
	}
	if v, found := w5bControlValue(b1, "same_name"); !found {
		t.Fatalf("fixture defect: the duplicate refusal offers no \"different person, same name\" control (value=%q), so there is no affirmation for the phone refusal to lose", v)
	}
	w5bAssertPreserved(t, "round 1, the duplicate refusal", round1, b1)

	// ROUND 2 — the officer TICKS THE BOX and resubmits the form as rendered.
	round2 := w5bRenderedForm(t, b1)
	round2.Set("same_name", "1")
	r2 := postForm(t, srv, "/roster/intake", round2)
	if !w5bRefused(r2.Code) {
		t.Fatalf("fixture defect: the affirmed enrolment with an empty phone box returned %d, want the 4xx PHONE refusal — this journey exists because W5b's refusal now sits below the duplicate check", r2.Code)
	}
	b2 := r2.Body.String()
	if !strings.Contains(b2, "A phone number is needed") {
		t.Fatalf("fixture defect: round 2 was refused for something other than the phone, so the journey does not reach A-3's case. Body: %s", w5bClip(b2))
	}
	// THE ASSERTION A-3 IS ABOUT. Every control, the affirmation included.
	w5bAssertPreserved(t, "round 2, the phone refusal that follows an affirmation", round2, b2)

	// ROUND 3 — the officer types the number and submits the form THE PAGE
	// GAVE THEM. If the affirmation survived, this enrols. If it did not,
	// they are told again that the person is already on the roster.
	round3 := w5bRenderedForm(t, b2)
	round3.Set("phone", typedPhone)
	r3 := postForm(t, srv, "/roster/intake", round3)
	if r3.Code != http.StatusSeeOther {
		t.Fatalf("W5b-2(a) (m5b-2), A-3: the officer answered the duplicate question in round 2, supplied the phone in round 3, and round 3 returned %d instead of the enrolment. Their affirmation did not survive the phone refusal's re-render, so they are refused for a question they have already answered — and the page they are shown says the person \"is already on the roster … do not enrol them again\". Body: %s", r3.Code, w5bClip(r3.Body.String()))
	}
	// And the enrolment is the SECOND Ferguson, not an overwrite of the first.
	persons, err := domain.Persons(srv.Store)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, p := range persons {
		if strings.EqualFold(strings.TrimSpace(p.Name), "Ferguson, Mary") {
			n++
		}
	}
	if n != 2 {
		t.Errorf("A-3 NON-DEGENERACY: after the affirmed enrolment the roster carries %d people named \"Ferguson, Mary\", want 2 — the journey is only meaningful if the affirmation actually enrolled the second person", n)
	}
}

// =======================================================================
// W5b-2(b) — no raw error string on the internal-error arm
// =======================================================================

// w5bRawErrorMarkers are tokens that appear in this store's error text and in
// no piece of authored copy on the roster page.
var w5bRawErrorMarkers = []string{"sql:", "sqlite", "domain:", "store:", ".go:", "database is closed"}

// TestW5bIntakeInternalErrorRendersNoRawErrorString — W5b-2(b).
//
// ⟦NO PRE-CHUNK RED — ALREADY TRUE, and marked as such at rev 7⟧.
// `handleIntake`'s only internal-error arm is `s.gateFail(w, …, err)`
// (intake.go:567–570 and four sibling calls), and `gateFail` is
// `log.Printf(…); http.Error(w, "database error", 500)` (gate.go:347–350) —
// it LOGS the error and renders a fixed string. Every hand-written
// `refuse(...)` in intake.go is authored copy. **W5b's surface is F-3-clean
// by construction, so its clean pass is not evidence.** The ledger row is the
// PLANTED RAW-ERROR RENDER — gateFail's fixed string replaced by
// `err.Error()` — which this assertion must catch. Register (m5b-2b).
//
// The test earns its keep by being NON-VACUOUS: it obtains the ACTUAL error
// text the same failing operation produces and asserts that exact string is
// absent from the body. A marker list alone could drift; the real text cannot.
func TestW5bIntakeInternalErrorRendersNoRawErrorString(t *testing.T) {
	srv, st, companyID := w5bIntakeServer(t)

	// The session is established BEFORE the store is broken — otherwise the
	// POST is refused by requireLogin and never reaches the internal-error
	// arm at all, and the test would pass having exercised nothing.
	cookie := loginCookie(t, srv, testLoginPassword)

	if err := st.Close(); err != nil {
		t.Fatalf("fixture defect: closing the store: %v", err)
	}

	// Rule 6 — the arrangement, checked rather than assumed: the same read
	// handleIntake makes must now FAIL, and its message must be distinctive
	// enough for the assertion below to be able to fail.
	_, readErr := domain.Companies(st)
	if readErr == nil {
		t.Fatalf("fixture defect: domain.Companies still succeeds against a closed store, so handleIntake's internal-error arm is unreachable and this test would pass having exercised nothing")
	}
	raw := readErr.Error()
	if len(raw) < 8 {
		t.Fatalf("fixture defect: the error text %q is too short to be a meaningful needle", raw)
	}

	req := httptest.NewRequest("POST", "/roster/intake",
		strings.NewReader(w5bForm("Ferguson", "Mary", companyID, "", "", "", "01463 555 0134").Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("fixture defect: the POST returned %d, want 500 — this test asserts about the INTERNAL-ERROR arm and never reached it. Body: %s", rec.Code, w5bClip(rec.Body.String()))
	}
	body := rec.Body.String()

	if strings.Contains(body, raw) {
		t.Errorf("W5b-2(b) (m5b-2b): the rendered 500 contains the RAW ERROR STRING %q. An operator is shown an internal message they cannot act on, and the page leaks the store's internals to whoever is standing at the window. gateFail LOGS the error and renders a fixed string (gate.go:347–350) — the log is where the detail belongs", raw)
	}
	for _, marker := range w5bRawErrorMarkers {
		if strings.Contains(strings.ToLower(body), marker) {
			t.Errorf("W5b-2(b) (m5b-2b): the rendered 500 contains %q, a token that appears in this store's error text and in no authored copy. Body: %q", marker, body)
		}
	}
}

// =======================================================================
// W5b-0 — byte fidelity at the AUTHORING path
// =======================================================================

// w5bAuthoringFidelityValues are (m5b-6)'s named shapes: parens/spaces/dash,
// an international prefix, an extension with letters, and a long value.
// NONE carries leading or trailing whitespace — see the report's seam note:
// no document rules what an edge-trimmed phone should be, and inventing that
// rule is not a test-writer's call.
var w5bAuthoringFidelityValues = []string{
	"(907) 555-0134",
	"+1 907-555-0134",
	"01463 555 0134 ext 22",
	"+44 (0)7700 900 123",
}

// TestW5bIntakeStoresThePhoneByteFaithfully — W5b-0's byte-fidelity half at
// the door an officer actually types into.
//
// The plan writes "`Phone` nullable, VERBATIM" (`:2037`). Normalisation is by
// far the likeliest builder reflex on a phone field, and the AUTHORING edge
// is where the reflex lives — a `strings.Map` that keeps digits, a "+1"
// prepended, a `TrimSpace` that grows into a `ReplaceAll`.
//
// EXPECTED RED (pre-chunk, genuine, unaided): there is no phone box, so every
// enrolment stores no phone and each value reds with "absent".
//
// Kills (m5b-6) on the authoring path.
func TestW5bIntakeStoresThePhoneByteFaithfully(t *testing.T) {
	srv, _, companyID := w5bIntakeServer(t)

	for i, want := range w5bAuthoringFidelityValues {
		last := fmt.Sprintf("Byte%d", i)
		rec := postForm(t, srv, "/roster/intake", w5bForm(last, "Fidelity", companyID, "", "", "", want))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("W5b-0: enrolling with phone %q returned %d, want 303. A refusal here means the edge is VALIDATING the shape of a phone number, which no criterion asks for and which would refuse real numbers at a real window. Body: %s", want, rec.Code, w5bClip(rec.Body.String()))
		}
		id := w5bEnrolledID(t, rec)
		p, err := domain.PersonByID(srv.Store, id)
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			t.Fatalf("W5b-0: the enrolment redirected to %s but no such person exists", id)
		}
		if p.Phone == nil {
			t.Errorf("W5b-0 (m5b-5): the person enrolled with phone %q carries no phone at all — intake collected it and discarded it, which is exactly what templates/intake.html:248 promises it does NOT do", want)
			continue
		}
		if *p.Phone != want {
			t.Errorf("W5b-0 (m5b-6): the officer typed %q and the roster stored %q. Phone is stored VERBATIM: no digit-stripping, no space/dash/paren removal, no country code added, no truncation, no case-fold (plan `:2037`)", want, *p.Phone)
		}
	}
}

// w5bEnrolledID pulls the person id out of the post-enrolment redirect
// (`/roster?enrolled=<id>`, intake.go:576).
func w5bEnrolledID(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parsing the redirect %q: %v", loc, err)
	}
	id := u.Query().Get("enrolled")
	if id == "" {
		t.Fatalf("the enrolment redirected to %q, which names no enrolled person — the fixture cannot read back what was stored", loc)
	}
	return id
}

// =======================================================================
// W5b-4 — the LITERAL `handleIntake` reading
// =======================================================================

// TestW5bIntakeNeverEnrolsAPersonWithoutAPhone — W5b-4 read literally.
//
// ⚠ WHY THIS TEST HAS THIS SHAPE, AND WHY IT IS THE ONLY SITE OF W5b-4's
// EDGE HALF. W5b-4's body says "`handleIntake` refuses the clear (phone is
// mandatory there)". `handleIntake` HAS NO CLEAR OPERATION — it is a creation
// handler and `Clear` is a field of `PersonAmendment` only [MEASURED]. Both
// readings were produced and the choice referred to the gate.
//
// **RULED 2026-08-31: the literal `handleIntake` reading STANDS and the
// amendment-edge reading is STRUCK**, on W5-X2's own precedent that a
// sub-chunk does not retrofit the shipped person amendment surface. So the
// only thing "clear the phone" can mean at a creation handler is "come out
// the other side with no phone", and NO form shape may achieve that. The
// ruling, the measurement and the OWED retrofit it leaves behind are recorded
// at §6.1 of reports/window-build/W5b-tests.md and guarded by
// internal/domain/w5b_amendment_edge_test.go.
//
// Every hostile shape either REFUSES (with nothing authored) or ENROLS WITH A
// NON-EMPTY PHONE. "Enrolled with phone absent or empty" is the one outcome
// forbidden — and a person whose phone composed to "" rather than absent
// would also violate W3-3's absent/empty distinction.
//
// EXPECTED RED (pre-chunk, genuine, unaided): every shape enrols phoneless
// today, because there is no phone box.
func TestW5bIntakeNeverEnrolsAPersonWithoutAPhone(t *testing.T) {
	srv, _, companyID := w5bIntakeServer(t)

	shapes := []struct {
		label string
		build func(last string) url.Values
	}{
		{"the phone key absent from the POST body entirely", func(last string) url.Values {
			return w5bForm(last, "Mary", companyID, "", "", "", w5bNoPhone)
		}},
		{"an empty phone box", func(last string) url.Values {
			return w5bForm(last, "Mary", companyID, "", "", "", "")
		}},
		{"a whitespace-only phone box", func(last string) url.Values {
			return w5bForm(last, "Mary", companyID, "", "", "", "   ")
		}},
		{"an explicit clear=phone alongside an empty box", func(last string) url.Values {
			v := w5bForm(last, "Mary", companyID, "", "", "", "")
			v.Set("clear", "phone")
			return v
		}},
		{"an explicit clear=phone alongside a typed number", func(last string) url.Values {
			v := w5bForm(last, "Mary", companyID, "", "", "", "01463 555 0134")
			v.Set("clear", "phone")
			return v
		}},
		{"the phone key sent twice, the first empty", func(last string) url.Values {
			v := w5bForm(last, "Mary", companyID, "", "", "", "")
			v["phone"] = []string{"", "01463 555 0134"}
			return v
		}},
	}

	for i, s := range shapes {
		last := fmt.Sprintf("Clear%d", i)
		before := w5bSnapshot(t, srv)
		rec := postForm(t, srv, "/roster/intake", s.build(last))

		if w5bRefused(rec.Code) {
			after := w5bSnapshot(t, srv)
			if after.Events != before.Events || after.Persons != before.Persons || after.Companies != before.Companies {
				t.Errorf("W5b-4/W5b-1 (m5b-1), %s: the refusal left a trace — log %d→%d, persons %d→%d, companies %d→%d",
					s.label, before.Events, after.Events, before.Persons, after.Persons, before.Companies, after.Companies)
			}
			continue
		}
		if rec.Code != http.StatusSeeOther {
			t.Errorf("W5b-4, %s: returned %d, which is neither a 4xx refusal nor a 303 enrolment. Body: %s", s.label, rec.Code, w5bClip(rec.Body.String()))
			continue
		}
		// It enrolled. Then the phone must be there.
		p, err := domain.PersonByID(srv.Store, w5bEnrolledID(t, rec))
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			t.Fatalf("W5b-4, %s: the enrolment redirected to a person that does not exist", s.label)
		}
		switch {
		case p.Phone == nil:
			t.Errorf("W5b-4, %s: intake ENROLLED a person with NO phone. Phone is mandatory AT THIS HANDLER — the surface where the roster is built in advance ([[D-52]]). The gate's add-on-demand door is the exempt one, and it is not this door", s.label)
		case strings.TrimSpace(*p.Phone) == "":
			t.Errorf("W5b-4, %s: intake enrolled a person whose phone composed to %q — an empty-string phone is not the same fact as an absent one (W3-3) and it is worse than either: it reads as recorded", s.label, *p.Phone)
		}
	}
}
