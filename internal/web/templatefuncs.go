package web

import (
	"fmt"
	"html/template"
	"reflect"
	"strings"

	"sitecontrol/internal/config"
	"sitecontrol/internal/repl"
)

// templateFuncs are the two helpers the shared UI shell needs, and nothing
// else. Both exist to serve one rule from the UI brief: the persistent top
// bar renders ONLY the fields a given page's handler already populates. It
// must therefore be able to ask a page's own data value "do you carry a
// sync state?" without every handler growing a common view-model — and
// without the bar ever inventing a field, a placeholder, or a guess.
//
// html/template treats a missing struct field as an EXECUTION ERROR, so a
// shared partial cannot simply write {{.Sync}} and hope. `has` and `field`
// answer by reflection instead, which is why the bar can be shared across
// six page types whose data structs have nothing in common.
var templateFuncs = template.FuncMap{
	// sitelabel renders a site id as a person reads it (acronym ids upper-case).
	"sitelabel": config.SiteLabel,
	// assertedprose: the ledger's destination cell by display name (siteprose.go).
	"assertedprose": func(v string) string { return destinationAssertedProse(config.SiteLabel, v) },
	// padname: an uncontrolled pad's id as a person reads it (siteprose.go).
	"padname": func(id string) string { return padLabel(config.SiteLabel, id) },
	"has":     templateHasField,
	"field":   templateField,
	"dict":    templateDict,
	// alerttally (V-01) counts a banner slice for the sticky strip's
	// persistent count line. See templateAlertTally in web.go.
	"alerttally": templateAlertTally,
	// alertstrip (FIX13) splits a banner slice into the strip's compact red
	// and amber lines. See templateAlertStrip in web.go.
	"alertstrip": templateAlertStrip,
	// lostsignalslot (FIX15) marks where the top bar's LOST SIGNAL line goes;
	// lostSignalStrip (lostsignal.go) fills it per request, by host.
	"lostsignalslot": lostSignalSlot,
	// ⟦was: readonlyalert (FIX7) — the strip's one row without its ack
	// form, for the dispatch view. FIX13: the strip renders no rows and no
	// forms on any host, so nothing renders a read-only row any more.⟧
	// ⟦W8, [[D-71]] obligation 3 clause (2′)⟧ checkedattr renders the
	// `checked` attribute WITHOUT a template branch. See its comment below —
	// this is a structural change, not a cosmetic one.
	"checkedattr": templateCheckedAttr,
	// ⟦[[D-84]]⟧ three more attribute helpers of the SAME KIND, for the same
	// reason: the passenger row now carries two <select>s (whose options
	// need `selected`), a <details> (which needs `open` for a row that is
	// ASKED rather than SHOWN) and a confirm control (hidden on a row that
	// has no record to amend). Each is a per-row attribute expressed without
	// a `{{if}}` inside the row — see templateCheckedAttr's comment.
	"selectedattr": templateSelectedAttr,
	"openattr":     templateOpenAttr,
	"hiddenattr":   templateHiddenAttr,
}

// templateSelectedAttr renders " selected" or "" — an <option>'s pre-selection
// without a branch inside the row. Same contract as templateCheckedAttr: it
// takes no caller-supplied text, so nothing here can carry a value into the
// markup.
func templateSelectedAttr(on bool) template.HTMLAttr {
	if on {
		return " selected"
	}
	return ""
}

// templateOpenAttr renders " open" or "" for a <details> element.
func templateOpenAttr(on bool) template.HTMLAttr {
	if on {
		return " open"
	}
	return ""
}

// templateHiddenAttr renders " hidden" or "".
func templateHiddenAttr(on bool) template.HTMLAttr {
	if on {
		return " hidden"
	}
	return ""
}

// ⟦was: "⟦W8, [[D-71]] obligation 1 clause (2′)⟧"⟧ — the hazard this guards is
// D-71's obligation **3** (the positional-zip hazard), not obligation 1 (which is the
// five-site coverage requirement). Clause (2′) itself was cited correctly.
// templateCheckedAttr renders " checked" or "", so a row can express a
// per-row attribute WITHOUT a `{{if}}` inside the row.
//
// ⚠⚠ THIS IS NOT A TIDY-UP AND THE REASON IS OPERATOR-FACING. The gate's
// passenger rows are submitted as PARALLEL ARRAYS, and clause (2′) of
// [[D-71]] requires every row to emit the SAME SET of fields, always, empty
// when unused — because a row that emits a different set shifts every LATER
// row's fields by one and silently attaches one person's details to another
// person's row. A `{{if}}` anywhere inside the row body is the construct that
// makes a differing set possible, so the rule that keeps the property true is
// "no branch inside the row", not "no branch around this one field": a branch
// beside the field today is a branch around it after one careless edit.
// Expressing the driver flag's attribute through this helper takes the only
// branch out of the row, which makes the property structural rather than
// something a reviewer has to re-check every time the row grows a control.
//
// It renders the SAME BYTES the branch it replaces did.
//
// The return type is template.HTMLAttr because the value IS an attribute
// fragment; it takes no caller-supplied text, so nothing here can carry a
// value into the markup.
func templateCheckedAttr(on bool) template.HTMLAttr {
	if on {
		return " checked"
	}
	return ""
}

// templateField returns data's exported field `name`, or nil when data is
// not a struct (or pointer to one) or has no such exported field. nil is a
// falsy, ranged-over-as-empty, {{with}}-skipped value in a template, which
// is exactly the "slot absent" rendering the top bar wants.
func templateField(data any, name string) any {
	v := reflect.ValueOf(data)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	f := v.FieldByName(name)
	if !f.IsValid() || !f.CanInterface() {
		return nil
	}
	return f.Interface()
}

// templateHasField reports whether data carries an exported field `name`.
// Distinct from templateField on purpose: a page that HAS the field but has
// it empty (no officers configured, no alerts outstanding) must still render
// its slot — "nothing to report" and "this page cannot tell" are different
// statements, and the muster campaign was explicit that they must not
// collapse into each other.
func templateHasField(data any, name string) bool {
	v := reflect.ValueOf(data)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return false
	}
	f := v.FieldByName(name)
	return f.IsValid() && f.CanInterface()
}

// templateDict builds the {Title, Main, Data} argument the shared layout
// takes. Keys must be strings and must come in pairs; anything else is an
// error at render time rather than a silently half-built page.
func templateDict(pairs ...any) (map[string]any, error) {
	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("dict: odd number of arguments (%d)", len(pairs))
	}
	out := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		k, ok := pairs[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key %d is %T, want string", i, pairs[i])
		}
		out[k] = pairs[i+1]
	}
	return out, nil
}

// Degraded / DegradedCount collapse the C10 per-peer verdicts into the ONE
// fact the top bar has room for. They read peerView.Healthy, which
// syncstatus.go sets true in exactly one branch and leaves false everywhere
// else — so a new bad signal added later is degraded here by default rather
// than silently healthy (the §8.9 posture: never a false "ok").
func (v syncStatusView) DegradedCount() int {
	n := 0
	for _, p := range v.Peers {
		if !p.Healthy {
			n++
		}
	}
	return n
}

// LeftBehindCount counts THIS post's own-site streams currently alarming
// repl.StateLeftBehind (LEFT-BEHIND-DETECT.md, Tier 1): a peer holds more of
// our own stream than our local watermark does. Scoped to this one state,
// not "any non-ok own row" — a self-history-pending own row already has its
// own render path (peerView.SelfHistoryPending) and must not double up
// here. Own-site rows own no configured peer (byOwner[snap.SiteID] is
// never a key any peerView.Streams draws from — see buildSyncStatusView),
// so without this, a left-behind alarm could sit in the AllStreams table as
// a bad row while every peer's own Healthy verdict, and therefore the ONE
// overall verdict below, still read clean. That is exactly the "silent
// alerting system" failure this file's whole doc comment exists to forbid.
func (v syncStatusView) LeftBehindCount() int {
	n := 0
	for _, s := range v.AllStreams {
		if s.Own && s.State == repl.StateLeftBehind {
			n++
		}
	}
	return n
}

// KeyRejectedCount / CannotReadCount count the peers whose verdict is a D26
// key fact (peerView.KeyRejected / CannotRead, ruling R11). Both are also
// !Healthy, so Degraded needs no new term; these only let the chip NAME it.
func (v syncStatusView) KeyRejectedCount() int {
	n := 0
	for _, p := range v.Peers {
		if p.KeyRejected {
			n++
		}
	}
	return n
}

func (v syncStatusView) CannotReadCount() int {
	n := 0
	for _, p := range v.Peers {
		if p.CannotRead && !p.KeyRejected { // one fact per peer: KeyRejected's verdict wins
			n++
		}
	}
	return n
}

// NotReceivingCount counts the peers known NOT to be getting this post's
// records (peerView.NotReceiving) — the only ones of which a page may say
// so (FIX3 F6, L04-N1).
func (v syncStatusView) NotReceivingCount() int {
	n := 0
	for _, p := range v.Peers {
		if p.NotReceiving {
			n++
		}
	}
	return n
}

// KeyFaultAdvice is the second sentence of the status page's key-fault
// notice (sync_status.html): what that means for this post's records, and
// what clears it, claimed only as far as the peers' facts go (FIX3 F6).
// ⟦was, fixed text: "Records made here are not reaching those posts until
// the keys in the posts' config files agree." — false for a peer that
// still pulls this post (L04-N1) and for a one-way cut, where the keys
// already agree (L01-8).⟧
func (v syncStatusView) KeyFaultAdvice() string {
	keyed, cut := 0, 0 // NotReceiving peers by cause: a key or peer list, or the link
	for _, p := range v.Peers {
		if !p.NotReceiving {
			continue
		}
		if p.KeyRejected || p.InboundRefused {
			keyed++
		} else {
			cut++
		}
	}
	var out []string
	if keyed > 0 {
		out = append(out, fmt.Sprintf("Records made here are not reaching %d of them until the keys and peer lists in the posts' config files agree.", keyed))
	}
	if cut > 0 {
		out = append(out, fmt.Sprintf("Records made here are not reaching %d of them: their requests for this post's records are not getting through, though this post's own requests to them succeed — check the link in their direction (a one-way cut).", cut))
	}
	n := 0
	for _, p := range v.Peers {
		if p.KeyRejected && !p.NotReceiving {
			n++
		}
	}
	if n > 0 {
		out = append(out, fmt.Sprintf("%d refuse this post's key until the keys in the posts' config files agree; each peer below says what that stops.", n))
	} else {
		// fix4 H (R6-2.2a): the pointer is said once. ⟦was: the template
		// always appended "See each peer below.", after this clause too⟧
		out = append(out, "See each peer below.")
	}
	return strings.Join(out, " ")
}

// Degraded is true when any configured peer is not healthy, OR this post's
// own stream is flagged left-behind of a peer's copy (LeftBehindCount). A
// site with no peers configured and no left-behind alarm is NOT degraded —
// it has nothing to be degraded about, and the bar says "no peers
// configured" instead.
func (v syncStatusView) Degraded() bool { return v.DegradedCount() > 0 || v.LeftBehindCount() > 0 }

// Summary is the chip's ONE line — the same four cases the top bar has
// always drawn, in the same words. It exists so the muster sheet's printed
// sync block (muster.html, SYNC-HONESTY 2026-09-09) and the bar's chip are
// literally the same string and can never drift into two vocabularies:
// the bar is hidden on paper, and the sheet must say on paper exactly what
// the bar says on glass.
func (v syncStatusView) Summary() string {
	switch {
	case !v.Available:
		return "cannot tell"
	case v.LeftBehindCount() > 0:
		return fmt.Sprintf("LEFT BEHIND — a peer holds more than this post does of %d of its own stream(s); see /debug/sync-status", v.LeftBehindCount())
	case v.KeyRejectedCount() > 0 || v.CannotReadCount() > 0:
		// James, 2026-10-04 (ruling R11, D26): "peers reject this post's
		// key", not "N peer(s) healthy" — a post whose records no peer can
		// read used to read fully healthy here (sc-test L12-Y31b).
		out := fmt.Sprintf("%d of %d peer(s) degraded", v.DegradedCount(), len(v.Peers))
		if n := v.KeyRejectedCount(); n > 0 {
			out += fmt.Sprintf(" — peers reject this post's key (%d)", n)
		}
		if n := v.CannotReadCount(); n > 0 {
			out += fmt.Sprintf(" — peers cannot read this post's records (%d)", n)
		}
		return out
	case v.Degraded():
		return fmt.Sprintf("%d of %d peer(s) degraded", v.DegradedCount(), len(v.Peers))
	case len(v.Peers) > 0:
		return fmt.Sprintf("%d peer(s) healthy", len(v.Peers))
	default:
		return "no peers configured"
	}
}
