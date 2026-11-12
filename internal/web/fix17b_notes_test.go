package web

// FIX17b: the sc-stress NOTEs — a full disk said plainly (F10), an
// enrolment that cannot land whole lands not at all (F10's half-landed
// intake), a computed negative number left a number in a CSV (F13), a
// retired post still offered by the history/report/search filters but by no
// dispatch or gate picker (F15), and the overdue history's footnote counted
// from the typed departure (F9).

import (
	"database/sql"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

func TestFix17bDiskFullIsSaidPlainly(t *testing.T) {
	for _, raw := range []string{
		"domain: check out: store: author commit: database or disk is full (13)",
		"store: transact gate check-in: write ice-pad.db-wal: no space left on device",
		"database or disk is full",
	} {
		if got := officerText(raw); got != diskFullSentence {
			t.Errorf("officerText(%q) = %q, want the disk-full sentence", raw, got)
		}
	}
	if got := dbFailBody(errors.New("sqlite: database or disk is full (13)")); got != diskFullSentence {
		t.Errorf("a page's disk-full read failure = %q", got)
	}
	if got := dbFailBody(errors.New("sql: connection is already closed")); got != "database error" {
		t.Errorf("an ordinary read failure = %q, want the unchanged \"database error\"", got)
	}
	if !strings.Contains(diskFullSentence, "DISK IS FULL") || !strings.Contains(diskFullSentence, "Call IT") {
		t.Errorf("the sentence must say the disk is full and to call IT: %q", diskFullSentence)
	}
	if isFault(diskFullSentence) || officerText(diskFullSentence) != diskFullSentence {
		t.Error("the disk-full sentence must pass through the refusal page unchanged")
	}
}

func TestFix17bIntakeLandsWholeOrNotAtAll(t *testing.T) {
	srv, st, _ := newOrientationServer(t, true)
	if err := st.RegisterProjection(store.Projection{
		Name: "zz_fix17b_full", Table: "zz_fix17b_full", OrderBy: "n",
		Schema: `CREATE TABLE IF NOT EXISTS zz_fix17b_full (n INTEGER PRIMARY KEY)`,
		Apply: func(tx *sql.Tx, env store.Envelope) error {
			if strings.Contains(string(env.Payload), `"orientation_completed"`) {
				return fmt.Errorf("database or disk is full (13)")
			}
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, srv, "/roster/intake", intakeForm2("Halfway", "Hana", "__new__", "Brand New Co", "", ""))
	if rec.Code == http.StatusSeeOther {
		t.Fatalf("an enrolment whose orientation could not be recorded was reported done")
	}
	if !strings.Contains(rec.Body.String(), "DISK IS FULL") && !strings.Contains(rec.Body.String(), "database or disk is full") {
		t.Errorf("the refusal does not say the disk is full: %s", rec.Body.String())
	}
	persons, _ := domain.Persons(st)
	for _, p := range persons {
		if p.Name == "Halfway, Hana" {
			t.Fatal("the person landed without this post's orientation (a half-landed enrolment)")
		}
	}
	companies, _ := domain.Companies(st)
	for _, c := range companies {
		if c.Name == "Brand New Co" {
			t.Fatal("the enrolment's new company landed without the person")
		}
	}
}

func TestFix17bCSVKeepsComputedNumbersAndGuardsTypedText(t *testing.T) {
	for _, c := range []struct{ header, cell, want string }{
		{"on_site_min", "-20", "-20"},     // a negative duration stays a number (F13)
		{"record_open_min", "125", "125"}, //
		{"late_min", "-3.5", "-3.5"},      //
		{"duration_s", "-1", "-1"},        //
		{"on_site_min", "-2+3", "'-2+3"},  // not a plain number: guarded
		{"on_site_min", "=1+1", "'=1+1"},  //
		{"person_name", "-20", "'-20"},    // typed columns: guarded as before
		{"Phone", "-907", "'-907"},        //
		{"Phone", "+1 907 555 0177", "'+1 907 555 0177"},
		{"visits", "12", "12"},
	} {
		if got := csvCell(c.header, c.cell); got != c.want {
			t.Errorf("csvCell(%q, %q) = %q, want %q", c.header, c.cell, got, c.want)
		}
	}
}

func TestFix17bRetiredPostIsInTheFiltersAndNotInThePickers(t *testing.T) {
	srv, _, _ := newW2Server(t, w2Base-6*60*w2Minute)
	srv.Cfg.FormerPeers = []config.FormerPeer{{SiteID: "site-old", DisplayName: "Old Pad"}}
	cookie := loginCookie(t, srv, testLoginPassword)
	page := func(path string) string {
		rec := w2Get(t, srv, path, cookie)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, rec.Code)
		}
		return html.UnescapeString(rec.Body.String())
	}
	for _, path := range []string{"/search", "/muster/at"} {
		if body := page(path); !strings.Contains(body, `value="site-old"`) || !strings.Contains(body, "Old Pad (retired)") {
			t.Errorf("%s: the retired post is not offered, marked retired, in the site filter", path)
		}
	}
	// A search filtered to the retired post is accepted, not "choose a post from the list".
	if body := page("/search?q=x&site=site-old"); strings.Contains(body, "choose a post from the list") {
		t.Error("a search filtered to the retired post is refused")
	}
	// The gate and the dispatch pickers never offer it.
	for _, path := range []string{"/gate", "/trips"} {
		if body := page(path); strings.Contains(body, "site-old") || strings.Contains(body, "Old Pad") {
			t.Errorf("%s offers the retired post", path)
		}
	}
	// Without a retired post the filter is as it was: no "(retired)" anywhere.
	srv.Cfg.FormerPeers = nil
	if body := page("/search"); strings.Contains(body, "(retired)") {
		t.Error("a mesh with no retired post shows a retired mark")
	}
}

func TestFix17bOverdueHistorySaysTheDueTimeCountsFromTheTypedDeparture(t *testing.T) {
	w := fix16fSeed(t)
	body := strings.Join(strings.Fields(html.UnescapeString(fix7Do(t, w.server("site-c"), "GET", fix7DispatchHost, "/history/overdue", nil).Body.String())), " ")
	if !strings.Contains(body, "the departure time the sending post's officer typed, when one was typed") {
		t.Fatalf("the footnote does not say the due time counts from the typed departure:\n%s", body)
	}
	if strings.Contains(body, "the due time is when the sending post recorded the dispatch plus the allowance") {
		t.Fatal("the stale footnote is still served")
	}
}
