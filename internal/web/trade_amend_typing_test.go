// trade_amend_typing_test.go — fix, worktree fix/trade-typing-witness.
//
// DEFECT (trade.go:169-170, buildTradePage): every call re-derives
// typed.Fields from the package-level tradeAmendFields template —
//
//	typed.Fields = make([]tradeAmendField, len(tradeAmendFields))
//	copy(typed.Fields, tradeAmendFields)
//
// — unconditionally. handleTradeAmend (trade.go:268-281) fills typed.Fields
// with the officer's own typing BEFORE calling AmendTrade, so that on a
// refusal the re-render carries what was typed (the comment at trade.go:104
// says exactly this: "Value carries the officer's typing back onto the
// control after a refusal, so a validation error does not make them
// re-enter everything"). But every refusal path re-renders through
// buildTradePage (renderTradeRefusal, and the two direct calls in
// handleTradeAmend's InputError/InternalError arms), and buildTradePage
// throws the caller's typed.Fields away and rebuilds it from the blank
// template — so the officer's typing never survives a single refusal.
//
// This is officer-facing on the /trade admin screen (D-66/D-71: the GATE
// itself never refuses; this is not the gate), operated at -65F: an officer
// refused mid-correction has to retype the whole amendment, which is time
// at an open door.
//
// ⚠ STATUS-ONLY IS BLIND HERE (measured at 4 sites in this codebase already):
// both a preserved-typing render and a wiped one answer the SAME 409, so
// this test never asserts on rec.Code alone — it asserts the typed VALUE is
// present in the specific <input value="…"> the template emits for that
// field (trade.html:94), never a whole-body Contains, and it round-trips an
// apostrophe through w5aEscape (w5a_read_paths_test.go) because html/template
// escapes it on render.
package web

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// tradeFieldInputHTML is the exact <input> trade.html:94 renders for one
// amend-form field, so the assertion is scoped to the control that field
// owns rather than a bare substring match anywhere on the page.
func tradeFieldInputHTML(key, value string) string {
	return fmt.Sprintf(`<input type="text" id="trade-f-%s" name="%s" autocomplete="off" value="%s">`, key, key, w5aEscape(value))
}

// TestTradeAmendRefusalPreservesOfficerTyping kills trade.go:169-170: a
// trade-amend POST that AmendTrade refuses on the FACT domain (Arm 6, "a
// record cannot be merged with itself" — trade_amend.go, same_as == the
// trade's own id) must re-render the form with every field the officer
// typed still on it, not just the one the refusal happened to be about.
//
// The name carries an apostrophe deliberately, to also confirm the
// assertion isn't fooled by html/template escaping the value on render.
func TestTradeAmendRefusalPreservesOfficerTyping(t *testing.T) {
	srv, st := newVehicleAmendTestServer(t)
	id := createTradeViaRoute(t, srv, "Alpha Fabrication")
	_ = st

	typedName := "O'Brien's Crew"
	typedPhoneDay := "555-0199"
	rec := postForm(t, srv, "/trade/amend", url.Values{
		"trade_id":  {id},
		"name":      {typedName},
		"phone_day": {typedPhoneDay},
		// same_as naming the trade's OWN id is Arm 6's trivial self-loop —
		// refused with a *TradeAmendRefusal wholly independent of the name
		// and phone_day fields above, so a live refusal exists alongside
		// live, unrelated typed values in the same request.
		"same_as": {id},
	})
	if rec.Code != 409 {
		t.Fatalf("POST /trade/amend (self-merge) = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	const wantNotice = "a record cannot be merged with itself"
	if !strings.Contains(body, wantNotice) {
		t.Fatalf("409 body does not carry the expected refusal reason %q — the fixture no longer reaches the intended refusal arm:\n%s", wantNotice, body)
	}

	if want := tradeFieldInputHTML("name", typedName); !strings.Contains(body, want) {
		t.Errorf("trade.go:169-170: officer's typed name lost on refusal re-render.\nwant control: %s\ngot body:     %s", want, body)
	}
	if want := tradeFieldInputHTML("phone_day", typedPhoneDay); !strings.Contains(body, want) {
		t.Errorf("trade.go:169-170: officer's typed phone_day lost on refusal re-render.\nwant control: %s\ngot body:     %s", want, body)
	}
	if want := tradeFieldInputHTML("same_as", id); !strings.Contains(body, want) {
		t.Errorf("trade.go:169-170: officer's typed same_as lost on refusal re-render.\nwant control: %s\ngot body:     %s", want, body)
	}
}
