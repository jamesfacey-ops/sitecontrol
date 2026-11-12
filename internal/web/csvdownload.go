// csvdownload.go — "Download CSV" for the read pages (FIX16 stream B,
// James-approved 2026-10-06): search results, the visits report and the
// sign-out-failures report.
//
// ONE WRITER for every download, so the rules below hold for all of them:
//
//   - The rows are the page's rows. Each download is served by the SAME
//     builder its page renders from (searchResults, reportTable); nothing
//     here derives a row.
//   - GET only (the routes are GET patterns; Go's mux answers HEAD with
//     them). Behind the login like the pages: an anonymous request gets
//     the login form, never a file. Phones are in full, as on the page.
//   - Content-Disposition: attachment, with a dated filename
//     (sitecontrol-<what>-YYYYMMDD-HHMM.csv by this post's clock).
//   - UTF-8 with a byte-order mark, so Excel opens names with accents
//     cleanly instead of guessing a code page.
//   - FORMULA INJECTION: a cell that begins with = + - @ (or a tab or
//     carriage return, which Excel also strips before evaluating) is
//     prefixed with a single quote, so a name typed at the gate as
//     "=HYPERLINK(…)" is shown as text, never run. ⟦FIX17b, sc-stress F13⟧
//     EXCEPT a plain number in a column this program computes (a duration
//     in minutes or seconds, a count — csvNumericColumn): "-20" there is a
//     number, not a formula, and quoting it turned a negative duration into
//     text in an otherwise numeric column. No typed value (a name, a phone)
//     ever lands in those columns, so the guard still covers everything an
//     officer or a CSV import can write.
//   - no-store: the file names people and carries phone numbers.
package web

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// csvBOM is UTF-8's byte-order mark.
const csvBOM = "\xef\xbb\xbf"

// csvSafe neutralises a spreadsheet formula at the start of a cell.
func csvSafe(cell string) string {
	if cell == "" {
		return cell
	}
	switch cell[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + cell
	}
	return cell
}

// csvPlainNumber is a whole or decimal number, optionally negative, and
// nothing else — no formula can be spelled in it.
var csvPlainNumber = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

// csvNumericColumn reports a column whose every cell is a number this
// program computed: durations (…_min, …_s) and the report's counts.
func csvNumericColumn(header string) bool {
	switch header {
	case "visits", "missed_signouts", "exempt_camp_returns", "unclassified_returns":
		return true
	}
	return strings.HasSuffix(header, "_min") || strings.HasSuffix(header, "_s")
}

// csvCell is csvSafe, with a plain number in a computed numeric column left
// as it is.
func csvCell(header, cell string) string {
	if csvNumericColumn(header) && csvPlainNumber.MatchString(cell) {
		return cell
	}
	return csvSafe(cell)
}

// csvFilename is sitecontrol-<what>-YYYYMMDD-HHMM.csv at the given instant,
// in this post's local time.
func csvFilename(what string, at time.Time) string {
	return fmt.Sprintf("sitecontrol-%s-%s.csv", what, at.In(time.Local).Format("20060102-1504"))
}

// writeCSVDownload sends header + rows as a CSV attachment. The body is
// built in full before anything is written, so a failure is a 500 and
// never half a file that reads as complete.
func (s *Server) writeCSVDownload(w http.ResponseWriter, what string, header []string, rows [][]string) {
	var buf bytes.Buffer
	buf.WriteString(csvBOM)
	cw := csv.NewWriter(&buf)
	cw.UseCRLF = true // what Excel writes and expects
	write := func(rec []string, cell func(i int, c string) string) error {
		out := make([]string, len(rec))
		for i, c := range rec {
			out[i] = cell(i, c)
		}
		return cw.Write(out)
	}
	err := write(header, func(_ int, c string) string { return csvSafe(c) })
	for _, r := range rows {
		if err != nil {
			break
		}
		err = write(r, func(i int, c string) string {
			if i < len(header) {
				return csvCell(header[i], c)
			}
			return csvSafe(c)
		})
	}
	if err == nil {
		cw.Flush()
		err = cw.Error()
	}
	if err != nil {
		log.Printf("csv %s: %v", what, err)
		http.Error(w, "the download could not be built — nothing was sent", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/csv; charset=utf-8")
	h.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`,
		csvFilename(what, time.UnixMilli(s.clockReading().WallMS))))
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(buf.Bytes())
}
