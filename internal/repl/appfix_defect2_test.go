// appfix_defect2_test.go — T5 (design/app-fixes-v1.md §3.3): the shipped
// descendant of APPFIX-PROBES.md probe 2, Defect 2's fail-before.
//
// THE DEFECT: handleStreams appends this server's OWN config-seeded epoch to
// the announced stream list for ANY requested site, so a streams query for a
// FOREIGN site's log carries a phantom (server's-own-epoch, head 0) stream.
// Measured pre-fix answer: [{NodeEpoch:10 Head:0}] where the store holds
// NOTHING for the requested site.
//
// INTENDED KILLER MUTANTS (measured by AF3, recipes in APPFIX-PROBES.md):
//   - G1: drop the `site == e.cfg.SiteID` conjunct from the §3.1 guard — KILLED here.
//   - G2: invert the guard to `site != e.cfg.SiteID` — KILLED here.
//   - G4: keep the guard but re-add the phantom in another form — KILLED here.
//
// (G3 — dropping the epoch bound — is accepted near-equivalent, AF3-2 wontfix;
// no assertion here targets it.)
//
// The quirk test (rejoin_test.go's ...QuirkStaysInertUnderTheEpochBound) kills
// NONE of these — it guards the nonzero-head ingest vector instead (§3.2,
// measured). This test alone carries the announce guard.
package repl

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestStreamsResponseForAForeignSiteNeverCarriesTheServersOwnEpoch(t *testing.T) {
	sites := newMesh(t, 1)
	a := sites[0]
	a.cfg.NodeEpoch = 10 // a holds NOTHING for site-b at any epoch

	// The self-history shape: an authenticated requester asks for ITS OWN
	// stream (X-SiteControl-From == requested site), exactly what a
	// re-imaged site-b's Rejoin sends — and what the provisioning-side
	// detector curls.
	req, err := http.NewRequest(http.MethodGet, a.srv.URL+"/sync/streams?site=site-b", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer demo-key-site-a")
	req.Header.Set(HeaderFrom, "site-b")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /sync/streams?site=site-b: status %d, want 200 — the self-history query must be answerable", resp.StatusCode)
	}
	var sr StreamsResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		t.Fatal(err)
	}
	for _, sh := range sr.Streams {
		if sh.NodeEpoch == 10 {
			t.Fatalf("streams response for site-b's log contains the SERVER's own epoch 10 (head=%d): %+v — "+
				"a foreign-site query must announce only epochs the store actually holds for that site", sh.Head, sr.Streams)
		}
	}
}
