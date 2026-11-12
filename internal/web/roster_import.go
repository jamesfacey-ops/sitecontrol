// roster_import.go — FIX16 H (c): bulk-load contractors from a spreadsheet
// (CSV) onto the roster. Design fork-docs/SITE-TOOL-DESIGN.md §3(c),
// lead-approved 2026-10-06.
//
// It is the intake form, many rows at a time — NOT a second way in:
//
//   - every row is judged by intake's own rules (both names, the 120-
//     character limit, a phone, a company; the shared person_match rule
//     for a possible duplicate; the retired-name key for a retired record),
//     and authored through intake's own domain calls, as ordinary
//     replicated events attributed to this post;
//   - a DRY RUN always comes first and authors nothing; the commit carries
//     a token over the file AND the roster it was judged against, so a
//     roster that changed in between (an enrolment here, a record arriving
//     from another post) is re-previewed, never committed blind;
//   - a possible duplicate is committed only when the officer ticks "this
//     is a different person" on that row (D-66's one affirmation); a
//     retired name is never committed (un-retire that record instead);
//   - re-importing the same file finds every row already on the roster, so
//     nothing doubles: a row whose name, phone and company all match one
//     live roster record is "already on the roster" and is never offered
//     for adding (FIX17b, sc-stress F14/F16) — that is the earlier import
//     of the same file, or a re-run after a commit that stopped midway;
//   - the commit authors in small batches, each its own short transaction,
//     and lets the post's other requests through between them (FIX17b,
//     sc-stress F14: one row per transaction paid one disk flush per row —
//     ~80 ms each on the staging VM's disk, 300 ms under load — and the
//     gate queued behind 500 of them for 108 s).
//
// The uploaded file is parsed in memory and never written to disk; no row's
// content is ever logged (names and phone numbers are personal data).
package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"sitecontrol/internal/config"
	"sitecontrol/internal/deadline"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

const (
	importMaxBytes = 1 << 20 // 1 MB: a season's roster is a few hundred rows
	importMaxRows  = 2000
)

// Row verdicts.
const (
	importNew       = "new"
	importDuplicate = "duplicate"   // person_match candidates: committed only if affirmed
	importDupInFile = "dup_in_file" // the same person earlier in this file
	importRetired   = "retired"     // matches a RETIRED record: never committed
	importAlready   = "already"     // the same name, phone AND company as a live record: never committed
	importInvalid   = "invalid"
)

type importCandidate struct {
	Name, Company, Site string
	Retired             bool
}

type importRow struct {
	Line                               int // spreadsheet row number; the header is row 1
	Last, First, Phone, Company, Trade string
	Name                               string // as it would be stored ("Last, First")
	Verdict                            string
	Reason                             string
	NewCompany, NewTrade               bool
	Candidates                         []importCandidate
	Affirmed                           bool

	companyID, tradeID string // resolved existing ids ("" = new / none)
}

// Committable reports whether a commit authors this row.
func (r importRow) Committable() bool {
	return r.Verdict == importNew || (r.Verdict == importDuplicate && r.Affirmed)
}

type importTotals struct {
	Rows, New, Duplicate, DupInFile, Retired, Already, Invalid, Committable int
}

type importData struct {
	SiteID    string
	Cfg       *config.Config
	Banner    []deadline.Alert
	BannerErr string
	Sync      syncStatusView

	Post             string
	HasOrientation   bool
	OrientsOnImport  bool
	Stage            string // "upload", "preview", "done"
	Error            string
	Rows             []importRow
	Totals           importTotals
	NewCompanies     []string
	NewTrades        []string
	Token            string
	CSV              string
	Committed        int
	CommitStoppedMsg string
}

func (s *Server) registerRosterImportRoutes(mux *http.ServeMux) {
	// All three are gated: the page exists to AUTHOR roster rows, and the
	// preview shows every name and phone in the file.
	mux.HandleFunc("GET /roster/import", s.requireLogin(s.handleImportPage))
	mux.HandleFunc("POST /roster/import/preview", s.requireLogin(s.handleImportPreview))
	mux.HandleFunc("POST /roster/import/commit", s.requireLogin(s.handleImportCommit))
}

func (s *Server) importBase() importData {
	d := importData{SiteID: s.Cfg.SiteID, Cfg: s.Cfg, Post: s.Cfg.PostName(), HasOrientation: s.Cfg.HasOrientation(),
		OrientsOnImport: importRecordsOrientation(s.Cfg), Stage: "upload", Sync: s.buildSyncSection()}
	d.Banner, d.BannerErr = s.banner()
	return d
}

func (s *Server) handleImportPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "roster import", "roster_import.html", s.importBase())
}

func (s *Server) importRefuse(w http.ResponseWriter, msg string) {
	d := s.importBase()
	d.Error = msg
	s.renderPageStatus(w, http.StatusConflict, "roster import", "roster_import.html", d)
}

// readUpload returns the uploaded file's bytes, in memory only.
func readUpload(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, importMaxBytes+64<<10)
	// maxMemory above the body cap: multipart never spills to a temp file.
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return nil, fmt.Errorf("that file is larger than %d KB — split it into smaller files", importMaxBytes>>10)
		}
		return nil, fmt.Errorf("the upload did not arrive intact — choose the file again")
	}
	defer r.MultipartForm.RemoveAll()
	f, _, err := r.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("choose a CSV file to check")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, importMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("the file could not be read")
	}
	if len(b) > importMaxBytes {
		return nil, fmt.Errorf("that file is larger than %d KB — split it into smaller files", importMaxBytes>>10)
	}
	return b, nil
}

func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	raw, err := readUpload(w, r)
	if err != nil {
		s.importRefuse(w, err.Error()+". Nothing was recorded.")
		return
	}
	d, err := s.importPreview(raw, nil)
	if err != nil {
		s.importRefuse(w, err.Error()+" Nothing was recorded.")
		return
	}
	s.renderPage(w, "roster import", "roster_import.html", d)
}

// importPreview parses and judges raw against the live roster.
func (s *Server) importPreview(raw []byte, affirmed map[int]bool) (importData, error) {
	d := s.importBase()
	rows, err := parseImportCSV(raw)
	if err != nil {
		return d, err
	}
	persons, err := domain.Persons(s.Store)
	if err != nil {
		return d, fmt.Errorf("the roster could not be read")
	}
	companies, err := domain.Companies(s.Store)
	if err != nil {
		return d, fmt.Errorf("the company list could not be read")
	}
	trades, err := domain.Trades(s.Store)
	if err != nil {
		return d, fmt.Errorf("the trade list could not be read")
	}
	d.NewCompanies, d.NewTrades = judgeImportRows(rows, persons, companies, trades, s.Cfg.SiteName)
	for i := range rows {
		rows[i].Affirmed = rows[i].Verdict == importDuplicate && affirmed[rows[i].Line]
	}
	d.Rows = rows
	d.Totals = importTotalsOf(rows)
	d.Token = importToken(raw, persons, companies, trades)
	d.CSV = string(raw)
	d.Stage = "preview"
	return d, nil
}

func (s *Server) handleImportCommit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2*importMaxBytes+64<<10)
	if err := r.ParseForm(); err != nil {
		s.importRefuse(w, "That form did not arrive intact. Nothing was recorded — check the file again.")
		return
	}
	raw := []byte(r.PostFormValue("csv"))
	affirmed := map[int]bool{}
	for _, v := range r.PostForm["affirm"] {
		if n, err := strconv.Atoi(v); err == nil {
			affirmed[n] = true
		}
	}
	// One import at a time (FIX17b): the token is checked against the
	// roster as it stands, so a second commit judged before this one landed
	// is re-judged after it, never interleaved with it.
	s.importMu.Lock()
	defer s.importMu.Unlock()
	d, err := s.importPreview(raw, affirmed)
	if err != nil {
		s.importRefuse(w, err.Error()+" Nothing was recorded.")
		return
	}
	if r.PostFormValue("token") != d.Token {
		d.Error = "The roster changed since this file was checked — somebody was enrolled here, or records arrived from another post. Nothing was recorded. The rows below have been checked again against the roster as it is now; read them and press Add again."
		s.renderPageStatus(w, http.StatusConflict, "roster import", "roster_import.html", d)
		return
	}
	n, stopped := s.commitImport(d.Rows)
	d.Stage, d.Committed, d.CommitStoppedMsg = "done", n, stopped
	s.renderPage(w, "roster import", "roster_import.html", d)
}

// importBatchRows is how many rows one commit transaction carries. The
// cost of a row is almost all the disk flush at its commit (MEASURED on the
// staging VM, FIX17b: 200 single-row commits 15.6 s, the same 200 in
// batches of 25 0.45 s; the CPU per row is ~0.1 ms), so a batch costs about
// one flush — short enough that a page waiting for the store's connection
// behind it waits one flush, not five hundred.
const importBatchRows = 25

// importYieldMax bounds how long a batch waits for the post's other
// requests to finish before it goes anyway, so a busy post still finishes
// its import (each batch then queues like any other write).
const importYieldMax = 2 * time.Second

// commitImport authors every committable row, in file order, through
// intake's own domain calls. A new company or trade is created once, on the
// first row that needs it.
//
// ⟦FIX17b, sc-stress F14⟧ IN BATCHES. importBatchRows rows go into one
// store.Transact: they all land or none of them does, and the next batch is
// a new transaction. Between batches the import waits (up to importYieldMax)
// until no other request is being served, so an officer's /gate that
// arrives mid-import waits for at most the one batch in hand, and its render
// is never re-done because a batch landed under it. ⟦was: one transaction
// per row, back to back — the post's one connection was handed straight
// from row to row, and a 500-row import held the gate for 108 s⟧
//
// It stops at the first failure and says how far it got: the batches
// already committed stay (each row is a complete, ordinary enrolment), the
// failing batch is rolled back whole, and importing the same file again
// shows every row that went in as "already on the roster" and adds only
// what is missing.
func (s *Server) commitImport(rows []importRow) (int, string) {
	recordedBy := s.postAttribution()
	type made struct{ name, id string }
	var newCompanies, newTrades []made // created by this commit, matched by the register's own name rule
	var todo []importRow
	for _, r := range rows {
		if r.Committable() {
			todo = append(todo, r)
		}
	}
	n := 0
	var took time.Duration // the last batch's commit, flush included
	for start := 0; start < len(todo); start += importBatchRows {
		end := start + importBatchRows
		if end > len(todo) {
			end = len(todo)
		}
		batch := todo[start:end]
		if start > 0 {
			s.yieldToRequests(took)
		}
		began := time.Now()
		keptCompanies, keptTrades := len(newCompanies), len(newTrades)
		var failed importRow
		var failedWhat string
		err := s.Store.Transact("roster import", func(st *store.Store) error {
			for _, r := range batch {
				failed = r
				companyID := r.companyID
				if companyID == "" {
					for _, m := range newCompanies {
						if domain.SameCompanyName(m.name, collapseSpaces(r.Company)) {
							companyID = m.id
							break
						}
					}
					if companyID == "" {
						c, err := domain.CreateCompanyRecordedBy(st, s.Cfg.SiteID, s.Cfg.NodeEpoch, collapseSpaces(r.Company), recordedBy, 0, "default")
						if err != nil {
							failedWhat = "company"
							return err
						}
						companyID = c.CompanyID
						newCompanies = append(newCompanies, made{c.Name, c.CompanyID})
					}
				}
				var tradeID *string
				switch {
				case r.tradeID != "":
					id := r.tradeID
					tradeID = &id
				case collapseSpaces(r.Trade) != "":
					id := ""
					for _, m := range newTrades {
						if domain.SameTradeName(m.name, collapseSpaces(r.Trade)) {
							id = m.id
							break
						}
					}
					if id == "" {
						t, err := domain.CreateTradeRecordedBy(st, s.Cfg.SiteID, s.Cfg.NodeEpoch, collapseSpaces(r.Trade), recordedBy, 0, "default")
						if err != nil {
							failedWhat = "trade"
							return err
						}
						id = t.TradeID
						newTrades = append(newTrades, made{t.Name, t.TradeID})
					}
					tradeID = &id
				}
				details := typedNameParts(r.Last, r.First)
				phone := r.Phone
				details.Phone = &phone
				p, err := domain.CreatePersonWithDetails(st, s.Cfg.SiteID, s.Cfg.NodeEpoch, r.Name, &companyID, tradeID, recordedBy, details, 0, "default")
				if err != nil {
					failedWhat = "person"
					return err
				}
				if importRecordsOrientation(s.Cfg) {
					if _, err := domain.CompleteOrientation(st, s.Cfg.SiteID, s.Cfg.NodeEpoch, p.PersonID, recordedBy, 0, "default"); err != nil {
						failedWhat = "orientation"
						return err
					}
				}
			}
			return nil
		})
		if err != nil {
			// The batch rolled back whole: the companies and trades it made
			// are gone with it.
			newCompanies, newTrades = newCompanies[:keptCompanies], newTrades[:keptTrades]
			if failedWhat == "" {
				failedWhat = "batch"
			}
			log.Printf("roster import: stopped at file row %d (%s): %T", failed.Line, failedWhat, err) // never the row's content
			return n, importStoppedMessage(batch[0].Line, failed.Line, failedWhat, n, err)
		}
		n += len(batch)
		took = time.Since(began)
	}
	return n, ""
}

// importStoppedMessage is the officer's account of a commit that stopped:
// which rows went in, which did not, and what to do.
func importStoppedMessage(firstLine, failedLine int, what string, added int, err error) string {
	why := fmt.Sprintf("the %s on file row %d could not be recorded", what, failedLine)
	if store.IsDiskFull(err) {
		why = "this post's disk is full, so nothing more can be recorded here — call IT"
	}
	return fmt.Sprintf("Stopped: %s. %d row(s) before file row %d were added; row %d and every row after it were NOT added. Importing the same file again shows the rows that went in as already on the roster and adds only what is missing.", why, added, firstLine, firstLine)
}

// yieldToRequests runs between two batches. It first rests for as long as
// the last batch took (at least 10 ms, at most 250 ms), so the import holds
// the store's connection at most about half the time however slow the
// disk's flush is, and a request that has just reached this post gets in
// ahead of the next batch; then it waits, up to importYieldMax, until this
// post is serving no request but the import's own, so the next batch never
// lands under a page part-way through its reads. Only Root() counts requests
// (countRequests); a Server driven through Handler() alone sees none.
func (s *Server) yieldToRequests(lastBatch time.Duration) {
	rest := lastBatch
	if rest < 10*time.Millisecond {
		rest = 10 * time.Millisecond
	}
	if rest > 250*time.Millisecond {
		rest = 250 * time.Millisecond
	}
	time.Sleep(rest)
	deadline := time.Now().Add(importYieldMax)
	for s.inflight.Load() > 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
}

// --- parsing ---

// importColumns maps each accepted header (lower case, spaces/underscores
// folded) to its field.
var importColumns = map[string]string{
	"last": "last", "lastname": "last", "surname": "last", "familyname": "last",
	"first": "first", "firstname": "first", "givenname": "first", "givennames": "first",
	"phone": "phone", "phonenumber": "phone", "mobile": "phone", "cell": "phone", "cellphone": "phone", "telephone": "phone",
	"company": "company", "employer": "company", "contractor": "company",
	"trade": "trade", "craft": "trade",
}

func headerKey(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	return strings.NewReplacer(" ", "", "_", "", "-", "", ".", "").Replace(h)
}

// parseImportCSV reads the file: UTF-8 (a BOM is dropped), comma or
// semicolon separated (the separator of the header line), a header row
// naming at least last, first, phone and company; trade is optional; other
// columns are ignored. Blank rows are skipped.
func parseImportCSV(raw []byte) ([]importRow, error) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("the file is not UTF-8 text — in Excel use Save As, \"CSV UTF-8 (Comma delimited)\".")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("the file is empty.")
	}
	firstLine := raw
	if i := bytes.IndexByte(raw, '\n'); i >= 0 {
		firstLine = raw[:i]
	}
	cr := csv.NewReader(bytes.NewReader(raw))
	if bytes.Count(firstLine, []byte(";")) > bytes.Count(firstLine, []byte(",")) {
		cr.Comma = ';'
	}
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("the first row could not be read as CSV.")
	}
	col := map[string]int{}
	for i, h := range header {
		if f, ok := importColumns[headerKey(h)]; ok {
			if _, dup := col[f]; !dup {
				col[f] = i
			}
		}
	}
	var missing []string
	for _, f := range []string{"last", "first", "phone", "company"} {
		if _, ok := col[f]; !ok {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("the first row must name the columns: last name, first name, phone, company (and optionally trade). Missing: %s.", strings.Join(missing, ", "))
	}
	get := func(rec []string, f string) string {
		i, ok := col[f]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}
	var rows []importRow
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				return nil, fmt.Errorf("row %d could not be read as CSV (check its quotes).", pe.StartLine)
			}
			return nil, fmt.Errorf("the file could not be read as CSV.")
		}
		// The row number the officer sees in the spreadsheet: the reader
		// skips blank lines silently, so it is read off the reader, never
		// counted.
		line, _ := cr.FieldPos(0)
		blank := true
		for _, c := range rec {
			if strings.TrimSpace(c) != "" {
				blank = false
				break
			}
		}
		if blank {
			continue
		}
		if len(rows) == importMaxRows {
			return nil, fmt.Errorf("the file has more than %d people — split it into smaller files.", importMaxRows)
		}
		rows = append(rows, importRow{Line: line, Last: get(rec, "last"), First: get(rec, "first"), Phone: get(rec, "phone"), Company: get(rec, "company"), Trade: get(rec, "trade")})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("the file has a header row but no people.")
	}
	return rows, nil
}

// --- judging ---

// judgeImportRows gives every row its verdict, by intake's rules, in file
// order. It returns the company and trade names the commit would create.
func judgeImportRows(rows []importRow, persons []domain.Person, companies []domain.Company, trades []domain.Trade, siteName func(string) string) (newCompanies, newTrades []string) {
	companyName := map[string]string{}
	for _, c := range companies {
		companyName[c.CompanyID] = c.Name
	}
	var accepted []foldedName // names of earlier rows that would be committed or are pending affirmation
	folded := foldNames(persons)
	for i := range rows {
		r := &rows[i]
		r.Name = composeName(r.Last, r.First)
		switch {
		case collapseSpaces(r.Last) == "" || collapseSpaces(r.First) == "":
			r.Verdict, r.Reason = importInvalid, "Both the last name and the first name are needed."
			continue
		case utf8.RuneCountInString(r.Name) > maxNewPersonNameRunes:
			r.Verdict, r.Reason = importInvalid, fmt.Sprintf("The name is %d characters long — longer than any real name (the limit is %d).", utf8.RuneCountInString(r.Name), maxNewPersonNameRunes)
			continue
		case collapseSpaces(r.Company) == "":
			r.Verdict, r.Reason = importInvalid, "A company is needed: a person with no company cannot be grouped on the muster."
			continue
		case collapseSpaces(r.Phone) == "":
			r.Verdict, r.Reason = importInvalid, "A phone number is needed: it is the only way to reach somebody who has not turned up for a muster."
			continue
		}
		r.NewCompany = true
		for _, c := range companies {
			if domain.SameCompanyName(c.Name, collapseSpaces(r.Company)) {
				r.companyID, r.NewCompany = c.CompanyID, false
				r.Company = c.Name
				break
			}
		}
		if collapseSpaces(r.Trade) != "" {
			r.NewTrade = true
			for _, t := range trades {
				if domain.SameTradeName(t.Name, collapseSpaces(r.Trade)) {
					r.tradeID, r.NewTrade = t.TradeID, false
					r.Trade = t.Name
					break
				}
			}
		}
		if p := domain.RetiredNamesake(persons, r.Name); p != nil {
			r.Verdict = importRetired
			r.Reason = fmt.Sprintf("%q is on the roster but has been RETIRED, so this row will not add a second record for the name. If they are back, un-retire that record on its amend page.", p.Name)
			continue
		}
		dupInFile := false
		typed := foldName(r.Name)
		for _, prev := range accepted {
			if prev.same(typed) {
				dupInFile = true
				break
			}
		}
		if dupInFile {
			r.Verdict, r.Reason = importDupInFile, "The same person is listed on an earlier row of this file. Remove one of the two rows (or, if they really are two people, add the second at the roster page)."
			continue
		}
		accepted = append(accepted, typed)
		if p := sameRosterRecord(persons, r.Name, r.Phone, r.companyID); p != nil {
			r.Verdict = importAlready
			r.Reason = fmt.Sprintf("Already on the roster as %q, with this phone and this company — added by an earlier import of this file, or enrolled before. It will not be added again.", p.Name)
			continue
		}
		if m := personMatchFolded(persons, folded, r.Name, r.Phone); len(m) > 0 {
			r.Verdict = importDuplicate
			r.Reason = "Possibly already on the roster. Tick \"different person\" only if this is somebody else with a similar name."
			for _, c := range m {
				ic := importCandidate{Name: c.Person.Name, Site: siteName(c.Person.SiteID), Retired: c.Person.IsRetired()}
				if c.Person.CompanyID != nil {
					ic.Company = companyName[*c.Person.CompanyID]
				}
				r.Candidates = append(r.Candidates, ic)
			}
			continue
		}
		r.Verdict = importNew
	}
	newCompanies = foldedNewNames(rows, domain.SameCompanyName, func(r importRow) (string, bool) { return collapseSpaces(r.Company), r.NewCompany })
	newTrades = foldedNewNames(rows, domain.SameTradeName, func(r importRow) (string, bool) { return collapseSpaces(r.Trade), r.NewTrade })
	return newCompanies, newTrades
}

// sameRosterRecord is the live roster record this row IS: the same name
// (case and spacing aside), the same phone (digits) and the same existing
// company. Nothing short of all three — a namesake with another phone or
// company is still person_match's "possibly already on the roster", asked
// about, never decided here. A retired record never matches: the retired-
// name rule above has already answered for it.
func sameRosterRecord(persons []domain.Person, name, phone, companyID string) *domain.Person {
	pk := phoneKey(phone)
	if pk == "" || companyID == "" {
		return nil
	}
	for i := range persons {
		p := &persons[i]
		if p.IsRetired() || !sameName(p.Name, name) || p.Phone == nil || phoneKey(*p.Phone) != pk ||
			p.CompanyID == nil || *p.CompanyID != companyID {
			continue
		}
		return p
	}
	return nil
}

// foldedNewNames lists each new name once (first spelling wins), among rows
// that could be committed.
func foldedNewNames(rows []importRow, same func(a, b string) bool, pick func(importRow) (string, bool)) []string {
	var out []string
next:
	for _, r := range rows {
		if r.Verdict != importNew && r.Verdict != importDuplicate {
			continue
		}
		name, isNew := pick(r)
		if !isNew || name == "" {
			continue
		}
		for _, o := range out {
			if same(o, name) {
				continue next
			}
		}
		out = append(out, name)
	}
	return out
}

func importTotalsOf(rows []importRow) importTotals {
	t := importTotals{Rows: len(rows)}
	for _, r := range rows {
		switch r.Verdict {
		case importNew:
			t.New++
		case importDuplicate:
			t.Duplicate++
		case importDupInFile:
			t.DupInFile++
		case importRetired:
			t.Retired++
		case importAlready:
			t.Already++
		case importInvalid:
			t.Invalid++
		}
		if r.Committable() {
			t.Committable++
		}
	}
	return t
}

// importToken binds a preview to the file and to the roster it was judged
// against: any person, company or trade added, renamed or retired since —
// here or arriving from another post — changes it, and the commit refuses.
func importToken(raw []byte, persons []domain.Person, companies []domain.Company, trades []domain.Trade) string {
	h := sha256.New()
	h.Write(raw)
	var lines []string
	for _, p := range persons {
		phone := ""
		if p.Phone != nil {
			phone = *p.Phone
		}
		lines = append(lines, fmt.Sprintf("p\x00%s\x00%s\x00%t\x00%s", p.PersonID, p.Name, p.IsRetired(), phone))
	}
	for _, c := range companies {
		lines = append(lines, "c\x00"+c.CompanyID+"\x00"+c.Name)
	}
	for _, t := range trades {
		lines = append(lines, "t\x00"+t.TradeID+"\x00"+t.Name)
	}
	sort.Strings(lines)
	for _, l := range lines {
		h.Write([]byte{0})
		h.Write([]byte(l))
	}
	return hex.EncodeToString(h.Sum(nil))
}
