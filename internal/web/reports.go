// reports.go — GET /reports/{name} and /reports/{name}.csv: the two
// `sitectl -report` reports (visits, signout-failures) as a page and as a
// download (FIX16 stream B, James-approved 2026-10-06).
//
// NOT A FORK. Both routes call domain.WriteReport — the call sitectl's
// -report makes — into a buffer and read the CSV back, so the page's table,
// the download and the shell's stdout are the same rows in the same order
// with the same columns. The denominator lines sitectl logs to stderr
// (visits examined, merge folding, coverage per stream, which site types
// this post knows) are printed above the table, since a browser has no
// stderr. The site type passed is this post's own, from config, exactly as
// sitectl passes it.
//
// READ-ONLY: GET only, nothing authored. Behind the login (the reports name
// people). Allowed on the dispatch host (dispatch_surface.go), where the
// page links only to the other read pages.
package web

import (
	"bytes"
	"encoding/csv"
	"log"
	"net/http"
	"strings"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
)

func (s *Server) registerReportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /reports/{name}", s.requireLogin(s.handleReport))
}

// reportTitles is each report's heading, in domain.ReportNames order.
var reportTitles = map[string]string{
	domain.ReportVisits:          "Visits report",
	domain.ReportSignoutFailures: "Sign-out failures report",
}

var reportBlurbs = map[string]string{
	domain.ReportVisits: "One row per visit: time on site and how each visit closed, every post's records as " +
		"replicated here. Times are UTC. The same rows as sitectl -report visits.",
	domain.ReportSignoutFailures: "One row per person: visits closed by the person's own return with no departure " +
		"recorded, at a text-message site (camp sites exempt). The same rows as sitectl -report signout-failures.",
}

type reportLink struct{ Name, Title, Href string }

type reportData struct {
	Cfg      *config.Config
	SiteID   string
	Dispatch bool

	Name    string
	Title   string
	Blurb   string
	CSVHref string
	Others  []reportLink
	Summary []string
	Header  []string
	Rows    [][]string
	Err     string
}

// reportTable runs the named report through domain.WriteReport and returns
// its header, rows and denominator lines.
func (s *Server) reportTable(name string) (header []string, rows [][]string, summary []string, err error) {
	var buf bytes.Buffer
	sum, err := domain.WriteReport(&buf, s.Store, name, domain.SiteTypes{s.Cfg.SiteID: s.Cfg.SiteType})
	if err != nil {
		return nil, nil, nil, err
	}
	recs, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		return nil, nil, nil, err
	}
	if len(recs) == 0 {
		return nil, nil, sum.Lines(), nil
	}
	return recs[0], recs[1:], sum.Lines(), nil
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	name, isCSV := strings.CutSuffix(r.PathValue("name"), ".csv")
	if !domain.IsReportName(name) {
		http.NotFound(w, r)
		return
	}
	header, rows, summary, err := s.reportTable(name)
	if isCSV {
		if err != nil {
			log.Printf("report %s: %v", name, err)
			http.Error(w, "the report could not be built — "+refusalText(err)+". No file was produced.", http.StatusInternalServerError)
			return
		}
		s.writeCSVDownload(w, "report-"+name, header, rows)
		return
	}
	data := reportData{Cfg: s.Cfg, SiteID: s.Cfg.SiteID, Dispatch: dispatchHost(r.Host),
		Name: name, Title: reportTitles[name], Blurb: reportBlurbs[name], CSVHref: "/reports/" + name + ".csv"}
	for _, n := range domain.ReportNames {
		if n != name {
			data.Others = append(data.Others, reportLink{Name: n, Title: reportTitles[n], Href: "/reports/" + n})
		}
	}
	if err != nil {
		// Loud, never a partial table.
		log.Printf("report %s: %v", name, err)
		data.Err = refusalText(err)
	} else {
		data.Header, data.Rows, data.Summary = header, rows, summary
	}
	s.renderPage(w, "report", "reports.html", data)
}
