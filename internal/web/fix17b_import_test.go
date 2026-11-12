package web

// FIX17b (sc-stress F14/F16): a roster import commits in small batches, each
// its own short transaction, and lets the post's other requests through
// between them; a commit that stops midway is safe to run again — the rows
// that went in come back "already on the roster", never as rows to tick.

import (
	"database/sql"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

func fix17bImportCSV(from, n int) string {
	var b strings.Builder
	b.WriteString("last name,first name,phone,company\n")
	for i := from; i < from+n; i++ {
		fmt.Fprintf(&b, "Batchrow%04d,Pat,555-1%04d,Acme Rigging\n", i, i)
	}
	return b.String()
}

func TestFix17bImportCommitsInBatchesOfItsOwnTransactions(t *testing.T) {
	srv, st, cookie := importWorld(t, false)
	var commits atomic.Int64
	st.OnAuthored = func() { commits.Add(1) } // once per committed transaction that authored
	csvText := fix17bImportCSV(0, 60)
	token, raw := importFormOf(t, importUpload(t, srv, cookie, csvText).Body.String())
	rec := importCommit(t, srv, cookie, token, raw)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "60 person(s) added") {
		t.Fatalf("commit = %d:\n%s", rec.Code, rec.Body.String())
	}
	// 60 rows (and their one new company) in batches of 25: three commits,
	// not sixty-one — a flush per batch, not per row.
	if got, want := commits.Load(), int64((60+importBatchRows-1)/importBatchRows); got != want {
		t.Fatalf("the import committed %d transactions, want %d (batches of %d)", got, want, importBatchRows)
	}
	// F16: the same file again — every row is ALREADY on the roster (same
	// name, phone and company) and none is offered for ticking.
	body := importUpload(t, srv, cookie, csvText).Body.String()
	v := importVerdicts(body)
	for line := 2; line <= 61; line++ {
		if v[fmt.Sprint(line)] != importAlready {
			t.Fatalf("re-import row %d = %q, want %q (all: %v)", line, v[fmt.Sprint(line)], importAlready, v)
		}
	}
	if strings.Contains(body, `name="affirm"`) {
		t.Fatal("a row already on the roster is offered as \"different person — add them\"")
	}
	if !strings.Contains(html.UnescapeString(body), "<strong>60</strong> already on the roster with the same name, phone and company") {
		t.Fatal("the totals do not say how many rows are already on the roster")
	}
}

func TestFix17bImportAlreadyNeedsNamePhoneAndCompany(t *testing.T) {
	srv, st, cookie := importWorld(t, false)
	acme, err := domain.CreateCompany(st, "site-a", 1, "Acme Rigging", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	phone := "555-0200"
	if _, err := domain.CreatePersonWithDetails(st, "site-a", 1, "Ferris, Jo", &acme.CompanyID, nil, nil, domain.PersonDetails{Phone: &phone}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	csvText := "last,first,phone,company\n" +
		"Ferris,Jo,(555) 0200,acme rigging\n" + // 2: the same record — already
		"Ferris,Jo,555-0299,Acme Rigging\n" + // 3: another phone — only possibly the same: asked
		"Ferris,Jo,555-0200,Other Co\n" // 4: another company — asked
	v := importVerdicts(importUpload(t, srv, cookie, csvText).Body.String())
	if v["2"] != importAlready || v["3"] != importDupInFile && v["3"] != importDuplicate || v["4"] == importAlready {
		t.Fatalf("verdicts %v: want row 2 already, rows 3 and 4 never already", v)
	}
}

// fix17bFaultProjection fails the fold of any person_created whose name
// contains "Boom" while armed: the store refuses that event, as a full disk
// or a broken row would, partway through an import. perCommit > 0 makes the
// first event of every transaction hold the post's one connection that long
// — the cost a slow disk's flush puts on every commit, whatever it carries.
func fix17bFaultProjection(armed *atomic.Bool, perCommit time.Duration) store.Projection {
	var mu sync.Mutex
	var last *sql.Tx
	return store.Projection{
		Name: "zz_fix17b_fault", Table: "zz_fix17b_fault", OrderBy: "n",
		Schema: `CREATE TABLE IF NOT EXISTS zz_fix17b_fault (n INTEGER PRIMARY KEY)`,
		Apply: func(tx *sql.Tx, env store.Envelope) error {
			mu.Lock()
			first := tx != last
			last = tx
			mu.Unlock()
			if perCommit > 0 && first {
				time.Sleep(perCommit)
			}
			if armed != nil && armed.Load() && strings.Contains(string(env.Payload), "Boom") {
				return fmt.Errorf("database or disk is full (13)")
			}
			return nil
		},
	}
}

func TestFix17bImportStoppedMidwayIsSafeToRunAgain(t *testing.T) {
	srv, st, cookie := importWorld(t, false)
	var armed atomic.Bool
	armed.Store(true)
	if err := st.RegisterProjection(fix17bFaultProjection(&armed, 0)); err != nil {
		t.Fatal(err)
	}
	persons0, _ := domain.Persons(st)
	// Row 30 (file line 31) fails: batch one (lines 2-26) lands, batch two
	// (lines 27-51) rolls back whole.
	csvText := strings.Replace(fix17bImportCSV(0, 60), "Batchrow0029,Pat", "Batchrow0029,Boom", 1)
	token, raw := importFormOf(t, importUpload(t, srv, cookie, csvText).Body.String())
	rec := importCommit(t, srv, cookie, token, raw)
	body := html.UnescapeString(rec.Body.String())
	for _, want := range []string{"STOPPED", "this post's disk is full", "call IT",
		"25 row(s) before file row 27 were added; row 27 and every row after it were NOT added"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the stopped commit does not say %q:\n%s", want, body)
		}
	}
	persons1, _ := domain.Persons(st)
	if len(persons1) != len(persons0)+25 {
		t.Fatalf("persons %d -> %d, want +25 (the first batch only)", len(persons0), len(persons1))
	}
	// Run it again once the fault is gone: the 25 that went in are named
	// already on the roster, the other 35 are new, and nobody doubles.
	armed.Store(false)
	page := importUpload(t, srv, cookie, csvText).Body.String()
	v := importVerdicts(page)
	for line := 2; line <= 61; line++ {
		want := importNew
		if line <= 26 {
			want = importAlready
		}
		if v[fmt.Sprint(line)] != want {
			t.Fatalf("re-run row %d = %q, want %q", line, v[fmt.Sprint(line)], want)
		}
	}
	token2, raw2 := importFormOf(t, page)
	if rec := importCommit(t, srv, cookie, token2, raw2); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "35 person(s) added") {
		t.Fatalf("re-run commit = %d:\n%s", rec.Code, rec.Body.String())
	}
	persons2, _ := domain.Persons(st)
	if len(persons2) != len(persons0)+60 {
		t.Fatalf("persons %d -> %d, want +60 and no duplicate", len(persons0), len(persons2))
	}
}

// TestFix17bGateStaysResponsiveDuringAnImport: every transaction this post
// commits holds the store's one connection for 30 ms (standing in for the
// disk flush a slow post pays per commit — MEASURED 80 ms on the staging
// VM, 300 ms under load). A 300-row import runs through Root, as in
// production, while an officer loads /gate again and again: every gate load
// must finish within 300 ms. Before FIX17b each row was its own commit, back
// to back, and the gate's every read queued behind one.
func TestFix17bGateStaysResponsiveDuringAnImport(t *testing.T) {
	srv, st, cookie := importWorld(t, false)
	if err := st.RegisterProjection(fix17bFaultProjection(nil, 30*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	root := srv.Root()
	csvText := fix17bImportCSV(0, 300)
	token, raw := importFormOf(t, importUpload(t, srv, cookie, csvText).Body.String())

	getGate := func() (int, time.Duration) {
		req := httptest.NewRequest("GET", "/gate", nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		t0 := time.Now()
		root.ServeHTTP(rec, req)
		return rec.Code, time.Since(t0)
	}
	getGate() // warm the templates and the page's reads

	done := make(chan struct{})
	var commitCode int
	var commitBody string
	go func() {
		defer close(done)
		form := strings.NewReader(url.Values{"token": {token}, "csv": {raw}}.Encode())
		req := httptest.NewRequest("POST", "/roster/import/commit", form)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		root.ServeHTTP(rec, req)
		commitCode, commitBody = rec.Code, rec.Body.String()
	}()
	var mu sync.Mutex
	var worst time.Duration
	loads := 0
	time.Sleep(20 * time.Millisecond) // the commit is under way
loop:
	for {
		select {
		case <-done:
			break loop
		default:
		}
		code, took := getGate()
		if code != http.StatusOK {
			t.Fatalf("/gate during the import = %d", code)
		}
		mu.Lock()
		loads++
		if took > worst {
			worst = took
		}
		mu.Unlock()
		time.Sleep(15 * time.Millisecond)
	}
	if commitCode != http.StatusOK || !strings.Contains(commitBody, "300 person(s) added") {
		t.Fatalf("commit = %d:\n%s", commitCode, commitBody)
	}
	if loads < 5 {
		t.Fatalf("only %d gate loads ran during the import — the test measured nothing", loads)
	}
	if worst > 300*time.Millisecond {
		t.Fatalf("a gate load took %v during the import (%d loads) — the bound is 300 ms", worst, loads)
	}
	t.Logf("%d gate loads during a 300-row import, slowest %v", loads, worst)
}
