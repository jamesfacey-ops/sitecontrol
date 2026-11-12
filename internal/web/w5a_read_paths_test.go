// w5a_read_paths_test.go — W5a's WEB read-path halves:
//
//	W5a-1  behaviour half: an awkward Name survives every read path
//	       byte-identical, and no read path RENDERS a derived part where Name
//	       belongs
//	W5a-3  every read path that answers "what is this person called" returns
//	       the composed Name
//	W5a-5  divergence renders by Name and produces no warning, no
//	       reconciliation, no repair and no log line
//
// design/WINDOW-BUILD-PLAN-v1.md `### W5 —`, criteria LOCKED 2026-08-31.
//
// ⟦NO PRE-CHUNK RED⟧ for all three of these halves. **A clean pass here is
// not evidence.** The ledger rows are:
//
//	(m5a-1b)  a read path RENDERING a derived part where Name belongs — the
//	          shape (m5a-1) CANNOT produce, because (m5a-1) writes the parts
//	          and leaves Name byte-identical (M9/TE-6)
//	(m5a-3)   a read path composing Name from the parts
//	(m5a-5)   a reconciliation pass, warning banner or repair that fires when
//	          parts disagree with Name
//
// EVERY fixture person's parts DISAGREE with their Name (lawful under D-48),
// which is what makes all three mutants killable: a page that rendered a part
// would render a visibly different string.
package web

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"sitecontrol/internal/domain"
)

// w5aAwkwardCases are the Name shapes W5a-1's behaviour half names —
// commas, multiple spaces, particles, and a single token — each paired with
// a part that DISAGREES with it.
var w5aAwkwardCases = []struct{ name, last, first string }{
	{"Van der Berg, Jan", "PART-LAST-1", "PART-FIRST-1"},
	{"O'Brien,  Mary Kate", "PART-LAST-2", "PART-FIRST-2"}, // two spaces after the comma
	{"Cher", "PART-LAST-3", "PART-FIRST-3"},                // a single token: nothing to split on
	{"de la Cruz-Smith, José María", "PART-LAST-4", "PART-FIRST-4"},
}

// w5aCaptureWebLog redirects the standard logger for the duration of the
// test; its own failure mode is produced before any silence is asserted.
func w5aCaptureWebLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOut, prevFlags, prevPrefix := log.Writer(), log.Flags(), log.Prefix()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
		log.SetPrefix(prevPrefix)
	})
	const marker = "w5a-web-log-channel-control"
	log.Printf("%s", marker)
	if !strings.Contains(buf.String(), marker) {
		t.Fatalf("CONTROL FAILED: the log capture channel did not record a line written straight through it — every \"no log line\" assertion below would be true by construction (standing rule 5)")
	}
	buf.Reset()
	return &buf
}

// w5aReconciliationWords are the tokens a reconciliation pass, warning banner
// or repair would use.
var w5aReconciliationWords = []string{
	"reconcil", "diverg", "mismatch", "disagree", "inconsist", "repair", "recompose",
}

func w5aFindReconciliationWord(s string) string {
	low := strings.ToLower(s)
	for _, w := range w5aReconciliationWords {
		if strings.Contains(low, w) {
			return w
		}
	}
	return ""
}

// w5aRowFor returns the `<tr data-person-id="ID" …>…</tr>` element for one
// person, failing if it is absent — a per-person warning marker would land
// inside exactly this element, and searching for it in an empty string is the
// false pass this project keeps finding.
func w5aRowFor(t *testing.T, body, personID string) string {
	t.Helper()
	marker := `data-person-id="` + personID + `"`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("fixture defect: no roster row for person %s, so nothing about how their divergence is rendered can be observed", personID)
	}
	start := strings.LastIndex(body[:i], "<tr")
	if start < 0 {
		t.Fatalf("fixture defect: no <tr before %s", marker)
	}
	rest := body[start:]
	j := strings.Index(rest, "</tr>")
	if j < 0 {
		t.Fatalf("fixture defect: no </tr> after %s", marker)
	}
	return rest[:j]
}

// TestW5aDivergentAwkwardNamesRenderByNameAndSilently — W5a-1's behaviour
// half, W5a-3's render half and W5a-5's render/silence half, on one fixture
// because they are one property of one page set.
func TestW5aDivergentAwkwardNamesRenderByNameAndSilently(t *testing.T) {
	buf := w5aCaptureWebLog(t)
	srv, st := w5aServer(t, "site-a", "")
	co, err := domain.CreateCompany(st, "site-a", 1, "Kakivik Asset Management", 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	ids := map[string]string{} // name -> person id
	var partValues []string
	for _, c := range w5aAwkwardCases {
		p := w5aPerson(t, st, "site-a", c.name, c.last, &co.CompanyID, nil)
		if _, err := domain.AmendPerson(st, "site-a", 1, domain.PersonAmendment{
			PersonID: p.PersonID, FirstName: &c.first,
		}, 0, "default"); err != nil {
			t.Fatal(err)
		}
		ids[c.name] = p.PersonID
		partValues = append(partValues, c.last, c.first)
	}

	// Rule 6 — the arrangement every assertion below depends on: each
	// person's parts really did land and really do DISAGREE with their Name.
	// Without the divergence a page rendering a part would be
	// indistinguishable from one rendering the Name.
	for _, c := range w5aAwkwardCases {
		p, err := domain.PersonByID(st, ids[c.name])
		if err != nil || p == nil {
			t.Fatalf("fixture defect: reading back %q: %v", c.name, err)
		}
		if p.LastName == nil || *p.LastName != c.last || p.FirstName == nil || *p.FirstName != c.first {
			t.Fatalf("fixture defect: %q read back with parts last=%v first=%v, want %q/%q", c.name, p.LastName, p.FirstName, c.last, c.first)
		}
		if strings.Contains(p.Name, c.last) || strings.Contains(p.Name, c.first) {
			t.Fatalf("fixture defect: %q contains its own part values, so a part-rendering page would look identical to a Name-rendering one", c.name)
		}
	}

	pages := map[string]string{
		"/roster": w5aGet(t, srv, "/roster"),
		"/gate":   w5aGetAuthed(t, srv, "/gate"),
		"/trips":  w5aGet(t, srv, "/trips"),
	}
	for path, body := range pages {
		if len(body) == 0 {
			t.Fatalf("fixture defect: %s rendered an empty body", path)
		}
		for _, c := range w5aAwkwardCases {
			// The gate page HTML-escapes an apostrophe as &#39;, so the
			// comparison is made on the escaped form the page actually
			// carries. What is asserted is that the WHOLE composed Name is
			// present, unbroken — not a prefix of it.
			if !strings.Contains(body, c.name) && !strings.Contains(body, w5aEscape(c.name)) {
				t.Errorf("W5a-1/W5a-3 (m5a-1b/m5a-3): %s does not carry the composed Name %q anywhere. Every read path answering \"what is this person called\" returns composed Name — byte-identical, commas, doubled spaces, particles and single tokens included", path, c.name)
			}
		}
		for _, v := range partValues {
			if strings.Contains(body, v) {
				t.Errorf("W5a-1 (m5a-1b) / W5a-3 (m5a-3): %s renders the derived part %q. A part may diverge from the displayed name (D-48), so a page showing one is showing a different string from the one on the muster sheet", path, v)
			}
		}
	}

	// W5a-5: no warning, no reconciliation, no repair — asserted inside each
	// person's own roster row, which is where a per-person marker would land.
	roster := pages["/roster"]
	for _, c := range w5aAwkwardCases {
		row := w5aRowFor(t, roster, ids[c.name])
		if w := w5aFindReconciliationWord(row); w != "" {
			t.Errorf("W5a-5 (m5a-5): the roster row for %q carries %q. D-48's text is a PERMISSION — \"nothing needs to detect the disagreement, warn about it, or repair it\"; the prohibition asserted here is the DERIVED ruling on top of it. Row: %s", c.name, w, row)
		}
	}

	// ...and no log line, across every render above.
	if w := w5aFindReconciliationWord(buf.String()); w != "" {
		t.Errorf("W5a-5 (m5a-5): rendering pages for people whose parts disagree with their Name logged %q. Log:\n%s", w, buf.String())
	}
	for _, v := range partValues {
		if strings.Contains(buf.String(), v) {
			t.Errorf("W5a-5 (m5a-5): a render logged the divergent part %q — silent means silent. Log:\n%s", v, buf.String())
		}
	}
}

// w5aEscape is html/template's escaping of the characters these fixture
// names actually contain.
func w5aEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;")
	return r.Replace(s)
}
