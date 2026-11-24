package web

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// The report pages and /history/signouts must classify a peer's visits with
// the SAME site types (own + typed peers, Config.KnownSiteTypes). Before the
// fix reportTable passed only the post's own type, so on the dispatch post
// every peer visit was "unclassified" and the missed count was understated.
func TestReportsClassifyPeerVisitsWithKnownSiteTypes(t *testing.T) {
	w := fix16fSeed(t)
	srv := w.server("site-c") // dispatch post; site-a is a typed text-message peer
	hist := fix7Do(t, srv, "GET", fix7DispatchHost, "/history/signouts", nil).Body.String()
	m := regexp.MustCompile(`(\d+) missed sign-outs`).FindStringSubmatch(hist)
	if m == nil || m[1] == "0" {
		t.Fatalf("history page shows no missed sign-outs to compare against:\n%s", hist)
	}
	for _, name := range []string{"visits", "signout-failures"} {
		_, _, summary, err := srv.reportTable(name)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(summary, "\n")
		want := fmt.Sprintf("missed sign-outs at text-message sites (R-4) %s;", m[1])
		if !strings.Contains(joined, want) {
			t.Errorf("%s report: want %q (== /history/signouts total), got:\n%s", name, want, joined)
		}
		if !strings.Contains(joined, "unclassified, site type not known at this post 0") {
			t.Errorf("%s report: typed peer visits are unclassified:\n%s", name, joined)
		}
	}
}
