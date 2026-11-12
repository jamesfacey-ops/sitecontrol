// trade_amend_typing_arms_test.go — tester-go pass over fix/trade-typing-witness
// (0bc1a7f), the second and third refusal arms plus the by-Key contract.
//
// The builder's witness (trade_amend_typing_test.go) drives ONE of the three
// error arms in handleTradeAmend: the *TradeAmendRefusal arm, via Arm 6's
// self-merge. The fix lives in buildTradePage, which all three arms re-render
// through — so the fix is common, but a fix verified on one arm and shipped
// for three is the shape this project keeps finding, and the other two arms
// get their own witnesses here. Each was watched RED against the base tree's
// trade.go (087711c) before being trusted GREEN; the commands and output
// lines are in reports/witness/TESTER-trade-typing.md.
//
// ⚠ STATUS-ONLY IS BLIND HERE too: every arm answers the same code whether
// the typing survived or not, so each test asserts (a) the arm's OWN notice
// element, proving the fixture reached the arm it claims to, and (b) the
// typed VALUE inside the exact <input> trade.html:94 renders for that field.
// Each also asserts an UNTOUCHED field renders value="" — a restore that
// smears one value across the form would satisfy a presence-only check.
//
// The fourth test pins the contract the fix's own comment states — "captured
// by Key … never a switch" — by handing buildTradePage a typed slice in a
// different order from the template, with stale labels and a stray key. The
// route-level witnesses cannot tell a by-Key restore from a positional one,
// because handleTradeAmend always hands over the template's own order; this
// test can, and it is what makes that mutant killable.
package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// tradeNoticeHTML is the warn notice trade.html:38/:41 renders for .Error
// and .InputError, with the text escaped as html/template escapes it.
func tradeNoticeHTML(text string) string {
	return `<div class="notice notice-warn">` + w5aEscape(text) + `</div>`
}

// tradeInternalErrorNoticeHTML is the fixed sentence trade.html:44 renders
// when .InternalError is set — the internal-error arm's own signal.
const tradeInternalErrorNoticeHTML = `<div class="notice notice-warn">This post could not complete that — <strong>nothing was recorded</strong>. Try again, and if it repeats, radio it through.</div>`

// requireTradeFieldInput asserts the exact <input> for one amend-form field
// carries `value`, or that it is empty when value is "".
func requireTradeFieldInput(t *testing.T, body, key, value, arm string) {
	t.Helper()
	want := tradeFieldInputHTML(key, value)
	if !strings.Contains(body, want) {
		if value == "" {
			t.Errorf("%s: untouched field %q did not render empty — a value was put on a control the officer never typed into.\nwant control: %s", arm, key, want)
			return
		}
		t.Errorf("%s: officer's typed %s lost on refusal re-render.\nwant control: %s", arm, key, want)
	}
}

// TestTradeAmendInputErrorPreservesOfficerTyping — the *TradeAmendInputError
// arm (handleTradeAmend's second errors.As branch): the officer named a
// trade id that is not on this post. AmendTrade refuses it BEFORE reading the
// typed fields, so the name and night-handset typed alongside must come back
// on the form, and the untouched day-handset must come back empty.
func TestTradeAmendInputErrorPreservesOfficerTyping(t *testing.T) {
	srv, _ := newVehicleAmendTestServer(t)
	_ = createTradeViaRoute(t, srv, "Alpha Fabrication")

	const bogusID = "site-a-trade-0000000000000000"
	typedName := "O'Brien's Crew"
	typedNight := "555-0142"
	rec := postForm(t, srv, "/trade/amend", url.Values{
		"trade_id":    {bogusID},
		"name":        {typedName},
		"phone_night": {typedNight},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("POST /trade/amend (unknown trade id) = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// The arm's own notice: TradeAmendInputError.Reason with handleTradeAmend's
	// suffix, rendered through .InputError — not .Error, not the internal one.
	wantNotice := tradeNoticeHTML(fmt.Sprintf("%q is not a trade on this post; nothing was recorded", bogusID))
	if !strings.Contains(body, wantNotice) {
		t.Fatalf("409 body does not carry the InputError arm's notice %q — the fixture no longer reaches the intended arm:\n%s", wantNotice, body)
	}

	const arm = "InputError arm"
	requireTradeFieldInput(t, body, "name", typedName, arm)
	requireTradeFieldInput(t, body, "phone_night", typedNight, arm)
	requireTradeFieldInput(t, body, "phone_day", "", arm)
	requireTradeFieldInput(t, body, "same_as", "", arm)
}

// TestTradeAmendInternalErrorPreservesOfficerTyping — the internal-error
// fallback (handleTradeAmend's log-and-500 arm). With the trade table
// dropped, AmendTrade's TradeByID read fails with a non-refusal error, the
// handler answers 500 with the internal-error notice, and — because the
// restore in buildTradePage runs before its own Trades() read fails — the
// officer's typing must still be on the form.
func TestTradeAmendInternalErrorPreservesOfficerTyping(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)
	id := createTradeViaRoute(t, srv, "Alpha Fabrication")
	witnessY6DropTable(t, st, "trade")
	logBuf := w5aCaptureWebLog(t)

	typedName := "O'Brien's Crew"
	typedShared := "555-0177"
	rec := postForm(t, srv, "/trade/amend", url.Values{
		"trade_id":     {id},
		"name":         {typedName},
		"phone_shared": {typedShared},
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("POST /trade/amend (trade table dropped) = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if !strings.Contains(body, tradeInternalErrorNoticeHTML) {
		t.Fatalf("500 body does not carry the internal-error notice — the fixture no longer reaches the intended arm:\n%s", body)
	}
	if !strings.Contains(logBuf.String(), "trade amend:") {
		t.Errorf("internal-error arm: 500 answered but the log never carries \"trade amend:\" (log has: %q)", logBuf.String())
	}

	const arm = "internal-error arm"
	requireTradeFieldInput(t, body, "name", typedName, arm)
	requireTradeFieldInput(t, body, "phone_shared", typedShared, arm)
	requireTradeFieldInput(t, body, "phone_day", "", arm)
	requireTradeFieldInput(t, body, "same_as", "", arm)
}

// TestBuildTradePageRestoresTypingByKeyNotByPosition pins the contract the
// fix states for itself: the officer's values are carried across by Key onto
// a fresh copy of the template, whose order and labels are the template's.
//
// The incoming slice is deliberately NOT the template's shape: reversed
// order, stale labels, one key the template does not have. A positional
// restore puts same_as's value on the name control; a restore that keeps the
// caller's slice keeps the stale labels and the stray control; a restore
// that drops nothing renders six controls. Each of those is a different
// regression at the same site, and each fails a different line below.
func TestBuildTradePageRestoresTypingByKeyNotByPosition(t *testing.T) {
	srv, _ := newVehicleAmendTestServer(t)

	typedName := "O'Brien's Crew"
	typedSameAs := "site-a-trade-feedfacefeedface"
	in := tradePage{Fields: []tradeAmendField{
		{Key: "same_as", Label: "stale label", Value: typedSameAs},
		{Key: "name", Label: "stale label", Value: typedName},
		{Key: "not_a_trade_field", Label: "stray", Value: "must not render"},
	}}
	got := srv.buildTradePage(in, "")
	if got.InternalError {
		t.Fatalf("fixture defect: buildTradePage took its internal-error branch on a healthy store")
	}

	if len(got.Fields) != len(tradeAmendFields) {
		t.Fatalf("rendered field set has %d controls, want the template's %d — a stray key must be dropped, never rendered: %+v", len(got.Fields), len(tradeAmendFields), got.Fields)
	}
	want := map[string]string{"name": typedName, "same_as": typedSameAs}
	// (C1) iterate the SLICE in its own order, never the map.
	for i, f := range got.Fields {
		tpl := tradeAmendFields[i]
		if f.Key != tpl.Key || f.Label != tpl.Label {
			t.Errorf("field %d: Key/Label = %q/%q, want the template's %q/%q — the fresh copy must keep the template's order and labels, not the caller's", i, f.Key, f.Label, tpl.Key, tpl.Label)
		}
		if f.Value != want[f.Key] {
			t.Errorf("field %q: Value = %q, want %q — the restore is by Key; a positional restore would put %q's value here", f.Key, f.Value, want[f.Key], in.Fields[i%len(in.Fields)].Key)
		}
	}
}
