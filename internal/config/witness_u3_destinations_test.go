// witness_u3_destinations_test.go — U3, then D-77 (2026-09-05): the empty-
// `[[destinations]]` fallback in DestinationList (config.go, DestinationList)
// under a topology wider than two sites.
//
// James, D-72 (2026-09-04): the deployment will be FIVE sites; everyone sees
// everyone; only the two ice-road posts change each other's data. The
// fallback predates D-72 and reads as obviously correct under two posts,
// because "every peer" and "the one real destination" were the same set —
// under five sites they are not. A camp site whose config simply omitted
// [[destinations]] got one CONTROLLED destination synthesised per configured
// peer, which arms an in-transit timer and an overdue alert for every other
// site, including the ones this box has no road to at all.
//
// The U3 pass witnessed that shape as it stood. D-77 (2026-09-05) then ruled
// the fix: sites are expandable by configuration alone, so the fallback may
// not be narrowed to any named pair (that hardcodes site identity) — instead
// `destinations` becomes MANDATORY when there is more than one peer, refused
// at Load, at the same fail-loudly point as every other peer/destination
// disagreement. This file now witnesses THAT refusal (the census's finding:
// accepting paths get reached, refusing paths do not) and pins its boundary:
// zero or one peer keeps the derived list; two or more must write it down.
//
// Site ids below are arbitrary placeholders. No real site name belongs in
// source (D-77's standing test), and none is asserted here.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fourPeersNoDestinations is the D-72 camp-site shape as a careless
// provisioner would write it: replicates with four sites, says nothing about
// which of them it can drive to.
const fourPeersNoDestinations = `
site_id               = "site-camp"
listen_addr           = "127.0.0.1:0"
node_epoch            = 1
db_path               = "test.db"
sync_interval_seconds = 10
login_password        = "x"
orientation           = false
site_type             = "text_message"

[[peers]]
site_id  = "site-a"
base_url = "http://127.0.0.1:0"
key      = "k"

[[peers]]
site_id  = "site-b"
base_url = "http://127.0.0.1:0"
key      = "k"

[[peers]]
site_id  = "site-d"
base_url = "http://127.0.0.1:0"
key      = "k"

[[peers]]
site_id  = "site-e"
base_url = "http://127.0.0.1:0"
key      = "k"
`

func loadD77(t *testing.T, body string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "site.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

// The refusal itself. Asserted on the text an administrator reads on the
// box, not merely on err != nil: the message must name the peer count, the
// missing block, and what to write — a refusal that only says "invalid"
// sends somebody hunting.
func TestMoreThanOnePeerWithoutDestinationsIsRefusedAtLoad(t *testing.T) {
	c, err := loadD77(t, fourPeersNoDestinations)
	if err == nil {
		t.Fatalf("a 4-peer config with no [[destinations]] LOADED, DestinationList()=%+v — D-77: with more than one peer the "+
			"dispatch list must be explicit; the per-peer fallback would arm an in-transit deadline toward all four peers",
			c.DestinationList())
	}
	if c != nil {
		t.Fatalf("Load returned a non-nil *Config alongside its error: %+v", c)
	}
	for _, want := range []string{
		"4 peers are configured",
		"no [[destinations]] block",
		"more than one peer",
		"D-77",
		"controlled = true",
		"controlled = false",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal text lacks %q\n  got: %v", want, err)
		}
	}
}

// Positive control for the same topology: the SAME four peers, with the
// dispatch list written down, load — and the list is exactly what was
// written, not a synthesis. This is the D-72 camp site done right: it
// replicates with four sites and can drive to one gate and one pad.
func TestMoreThanOnePeerWithExplicitDestinationsLoadsUnsynthesised(t *testing.T) {
	c, err := loadD77(t, fourPeersNoDestinations+`
[[destinations]]
id           = "site-a"
display_name = "Gate A"
controlled   = true

[[destinations]]
id           = "pad-north"
controlled   = false
`)
	if err != nil {
		t.Fatalf("4 peers + explicit destinations must Load: %v", err)
	}
	got := c.DestinationList()
	if len(got) != 2 || got[0].ID != "site-a" || !got[0].Controlled || got[0].DisplayName != "Gate A" ||
		got[1].ID != "pad-north" || got[1].Controlled || got[1].DisplayName != "pad-north" {
		t.Fatalf("DestinationList() = %+v, want exactly [site-a controlled \"Gate A\", pad-north uncontrolled] — nothing synthesised from the other three peers", got)
	}
	if ids := c.ControlledDestinationIDs(); len(ids) != 1 || ids[0] != "site-a" {
		t.Fatalf("ControlledDestinationIDs() = %v, want [site-a] only — site-b/site-d/site-e are peers, not roads", ids)
	}
}

// The boundary, pinned as a table so the number lives in one place: the
// derived list survives at zero and one peer (every pre-destinations config
// and every 1-peer fixture in the tree), and the refusal begins at two —
// not at "five", not at any named pair. Nothing here assumes a site count.
func TestDestinationsFallbackBoundaryIsExactlyOnePeer(t *testing.T) {
	head := `
site_id               = "site-x"
listen_addr           = "127.0.0.1:0"
node_epoch            = 1
db_path               = "test.db"
sync_interval_seconds = 10
login_password        = "x"
orientation           = false
site_type             = "text_message"
`
	peer := func(i int) string {
		return fmt.Sprintf("\n[[peers]]\nsite_id  = \"peer-%d\"\nbase_url = \"http://127.0.0.1:0\"\nkey      = \"k\"\n", i)
	}
	for _, tc := range []struct {
		peers    int
		wantLoad bool
	}{
		{0, true},
		{1, true},
		{2, false},
		{3, false},
	} {
		body := head
		for i := 0; i < tc.peers; i++ {
			body += peer(i)
		}
		c, err := loadD77(t, body)
		switch {
		case tc.wantLoad && err != nil:
			t.Errorf("%d peer(s), no [[destinations]]: want Load, got refusal: %v", tc.peers, err)
		case tc.wantLoad && err == nil && len(c.DestinationList()) != tc.peers:
			t.Errorf("%d peer(s), no [[destinations]]: DestinationList() = %+v, want one derived controlled entry per peer", tc.peers, c.DestinationList())
		case !tc.wantLoad && err == nil:
			t.Errorf("%d peers, no [[destinations]]: LOADED with DestinationList() = %+v, want the D-77 refusal", tc.peers, c.DestinationList())
		}
	}
}
