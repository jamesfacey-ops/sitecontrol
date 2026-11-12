// gate_row_seed_test.go — a gate row that ARRIVED RESOLVED keeps its person
// (the row-seeding hunk in gate.html's wireRow, landed 4f21d05).
//
// THE DEFECT THIS PINS lived in the tree from 2026-08-21 until 2026-09-09:
// every scripted check-in of somebody ALREADY ON THE ROSTER minted a
// duplicate of them — reproduced in a real browser, roster 6 → 8
// (reports/witness/GATE-ADOPT-CONFIRM.md). The row's hidden kind/value named
// the right person, but the commit re-resolved every row through the row's
// OWN resolver, whose captured pick started empty, so it answered 'new' and
// the server minted the person it had been handed. The fix seeds each row's
// resolver with the person its hidden id names — looked up BY ID, only
// while the visible text still reads as that person's roster name
// (resolvePick's own rule). The reviewer MEASURED zero test hits on that
// hunk (reports/witness/REVIEW-ADOPT-CONFIRM.md §6): deleting it would fail
// nothing. This file is the tripwire it lacked.
//
// WHAT A GO TEST CAN AND CANNOT SEE HERE — read before trusting the green.
// The seed is client-side script and this module carries no JavaScript
// runtime (gate.html, resolvePick's header: a decision, not an omission), so
// nothing here can press Enter or commit a form through the script. What
// CAN be pinned, and is, is the SHAPE of the served script — the pattern
// gate_adopt_confirm_test.go set. The assertions are keyed to the seeding
// CALL and its GUARD CLAUSES, never to whitespace or to the comment above
// the hunk: a rename of the locals passes (they are captured, not spelled);
// a reflow passes; deleting the seed fails; keeping the seed but dropping
// the name-agreement guard fails.
//
// WHAT IT CATCHES: the hunk deleted or moved out of wireRow (so a row the
// server decorated, or one addFromOpenBox wrote, is no longer seeded); the
// seed no longer gated on kind === 'existing'; the lookup switched from the
// hidden id to something else; the adopt no longer conditioned on the
// visible text agreeing with the roster name (which would let an edited row
// check in the previously-resolved person under a corrected name — the
// exact hazard resolvePick's header names).
//
// WHAT IT WOULD MISS: a semantic break that keeps the text — personByID
// rewritten to match on name, adopt() turned into a no-op, the seed run
// against another row's input, the submit handler consulting a different
// resolver, a runtime error earlier in wireRow that never reaches the seed,
// or the server writing kind=existing with the wrong id. Those need a
// browser; the witness harness is not in this tree.
package web

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// gateScriptFunction slices one top-level function out of the served gate
// script: from its `function NAME(` to the next top-level `function ` at the
// same indentation, or to the end of the script. It is locality only — the
// property is asserted on what the slice CONTAINS, not on where its edges
// fall.
func gateScriptFunction(t *testing.T, page, name string) string {
	t.Helper()
	start := strings.Index(page, "function "+name+"(")
	if start < 0 {
		t.Fatalf("the served gate script carries no `function %s(` — the row wiring is gone or renamed; this tripwire cannot see the seed", name)
	}
	rest := page[start:]
	end := len(rest)
	if i := strings.Index(rest[1:], "\n    function "); i >= 0 {
		end = i + 1
	}
	if i := strings.Index(rest, "</script>"); i >= 0 && i < end {
		end = i
	}
	return rest[:end]
}

var (
	// The row's hidden pair, read by the class names the server renders and
	// rowPersonSlot reads back — the contract, not a local name.
	rowHiddenKind  = regexp.MustCompile(`querySelector\('\.p-kind'\)`)
	rowHiddenValue = regexp.MustCompile(`(\w+)\s*=\s*\w+\.querySelector\('\.p-value'\)`)
	// The seed runs only for a row the server or the ask already resolved.
	seedGatedOnExisting = regexp.MustCompile(`\.value\s*===\s*'existing'`)
)

func TestGateRowSeedsItsResolverWithThePersonItArrivedWith(t *testing.T) {
	srv, _, _ := newGateTestServer(t)
	code, page := getGate(t, srv)
	if code != http.StatusOK {
		t.Fatalf("GET /gate = %d", code)
	}
	body := gateScriptFunction(t, page, "wireRow")

	// 1. The seed reads the row's hidden kind and value.
	if !rowHiddenKind.MatchString(body) {
		t.Errorf("wireRow never reads the row's hidden .p-kind — the seed that keeps an already-resolved person is gone; a scripted check-in of a roster person mints a duplicate of them")
	}
	vm := rowHiddenValue.FindStringSubmatch(body)
	if vm == nil {
		t.Fatalf("wireRow never binds the row's hidden .p-value — the seed that keeps an already-resolved person is gone; a scripted check-in of a roster person mints a duplicate of them")
	}
	valueField := vm[1]

	// 2. …only for a row that arrived resolved.
	if !seedGatedOnExisting.MatchString(body) {
		t.Errorf("wireRow's seed is not gated on the hidden kind reading 'existing' — a row typed as a new person must not be seeded with anybody")
	}

	// 3. …looked up BY ID from that hidden value, never by name.
	byID := regexp.MustCompile(`(\w+)\s*=\s*personByID\(\s*` + regexp.QuoteMeta(valueField) + `\.value\s*\)`)
	km := byID.FindStringSubmatch(body)
	if km == nil {
		t.Fatalf("wireRow never looks the row's person up BY ID from %s.value (personByID) — the seed is gone or resolves by something other than the id the row was handed", valueField)
	}
	known := km[1]

	// 4. …and adopted into the row's resolver ONLY while the visible text
	//    still reads as that person's roster name — resolvePick's rule. The
	//    adopt call must sit in the same `if` as the name comparison.
	adopt := regexp.MustCompile(`\.adopt\(\s*` + regexp.QuoteMeta(known) + `\s*\)`)
	locs := adopt.FindAllStringIndex(body, -1)
	if locs == nil {
		t.Fatalf("wireRow never adopts %s into the row's resolver — the person the row arrived with is looked up and then discarded; the commit still re-resolves the row to 'new'", known)
	}
	guarded := false
	for _, loc := range locs {
		k := strings.LastIndex(body[:loc[0]], "if (")
		if k < 0 {
			continue
		}
		cond := body[k:loc[0]]
		if strings.Contains(cond, ";") {
			continue // a different statement, not this adopt's guard
		}
		if strings.Contains(cond, known+".name.toLowerCase()") && strings.Contains(cond, ".value.trim().toLowerCase()") {
			guarded = true
		}
	}
	if !guarded {
		t.Errorf("wireRow adopts %s without the name-agreement guard (`%s.name.toLowerCase() === <input>.value.trim().toLowerCase()`) — an edited row would check in the previously-resolved person under the corrected name, which resolvePick's header forbids", known, known)
	}
}
