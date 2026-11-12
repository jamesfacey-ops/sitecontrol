// analytics_corrected_test.go — FINDINGS-R4 D2 (AN-PANIC-1) at the HTTP
// surface: after one /time/correct-shaped correction, /analytics and the
// per-site detail must still render. Before the fix both panicked with the
// store's REENTRANCY verdict after a 10 s stall, which held the single
// connection long enough to stall /muster at the same post.
package web

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"sitecontrol/internal/domain"
	"sitecontrol/internal/store"
)

func TestAnalyticsPagesRenderAfterATimeCorrection(t *testing.T) {
	w := anRoadWorld(t)
	tripKey := store.Key{SiteID: w.trip.OriginSite, NodeEpoch: w.trip.NodeEpoch, Seq: w.trip.Seq}
	if _, err := domain.CorrectOccurredAt(w.st, "site-a", 1, domain.TimeCorrection{
		Target: tripKey, CorrectedOccurredAtMS: time.Now().Add(-time.Hour).UnixMilli(), RecordedBy: "officer",
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/analytics", "/analytics/site/site-a", "/analytics/site/site-b"} {
		code, body, err := func() (code int, body string, err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("panicked: %v", r)
				}
			}()
			code, body = anSiteGet(t, w.srv, "GET", path, true)
			return
		}()
		if err != nil {
			t.Fatalf("GET %s after a time correction: %v", path, err)
		}
		if code != http.StatusOK {
			t.Fatalf("GET %s after a time correction = %d:\n%s", path, code, body)
		}
	}
}
