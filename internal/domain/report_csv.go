// report_csv.go — the named CSV reports, one entry point for every caller.
//
// `sitectl -report <name>` (cmd/sitectl) and the web pages /reports/<name>
// and /reports/<name>.csv (web/reports.go, FIX16 stream B, James-approved
// 2026-10-06) both call WriteReport, so the shell and the browser can never
// produce different rows from the same store. Nothing here is new logic:
// it is the switch that used to live in sitectl's main, moved so both can
// reach it.
package domain

import (
	"fmt"
	"io"

	"sitecontrol/internal/store"
)

// Report names, as `sitectl -report` takes them and the web routes use them.
const (
	ReportVisits          = "visits"
	ReportSignoutFailures = "signout-failures"
)

// ReportNames lists every report, in the order they are offered.
var ReportNames = []string{ReportVisits, ReportSignoutFailures}

// IsReportName reports whether name is a known report.
func IsReportName(name string) bool {
	for _, n := range ReportNames {
		if n == name {
			return true
		}
	}
	return false
}

// WriteReport writes the named report as CSV to w and returns the
// denominator summary (VisitReportSummary.Lines) the caller shows beside it.
// types is the site types this post knows — its own, from config.
func WriteReport(w io.Writer, st *store.Store, name string, types SiteTypes) (VisitReportSummary, error) {
	if !IsReportName(name) {
		return VisitReportSummary{}, fmt.Errorf("unknown report %q (the reports are 'visits' and 'signout-failures')", name)
	}
	recs, sum, err := VisitReport(st, types)
	if err != nil {
		return VisitReportSummary{}, err
	}
	switch name {
	case ReportVisits:
		err = WriteVisitReportCSV(w, recs)
	case ReportSignoutFailures:
		err = WritePersonSignoutFailuresCSV(w, SignoutFailuresByPerson(recs))
	}
	return sum, err
}
