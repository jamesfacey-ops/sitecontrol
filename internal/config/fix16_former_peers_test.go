// fix16_former_peers_test.go — FIX16 H: [[former_peers]] names a RETIRED
// post (sitectl admin peer remove) so its records keep their name; Parse is
// Load on bytes in hand, with the same refusals.
package config

import (
	"strings"
	"testing"
)

const fix16Base = `
site_id               = "ice-road"
display_name          = "2P Ice Road Entrance"
listen_addr           = "127.0.0.1:0"
node_epoch            = 1
db_path               = "test.db"
sync_interval_seconds = 10
login_password        = "x"
site_type             = "text_message"
orientation           = false

[[peers]]
site_id  = "ice-pad"
display_name = "Ice Pad"
base_url = "http://127.0.0.1:8082"
key      = "k"
`

func TestFix16FormerPeersKeepARetiredPostsName(t *testing.T) {
	c, err := Parse([]byte(fix16Base+`
[[former_peers]]
site_id = "ndb"
display_name = "NDB"
long_name = "North Dock Base"
`), "site.toml")
	if err != nil {
		t.Fatal(err)
	}
	if got := c.SiteName("ndb"); got != "NDB" {
		t.Errorf("SiteName(ndb) = %q, want NDB", got)
	}
	if got := c.SiteLongName("ndb"); got != "North Dock Base" {
		t.Errorf("SiteLongName(ndb) = %q", got)
	}
	// A former peer is never pulled from or dispatched to.
	if len(c.Peers) != 1 || len(c.DestinationList()) != 1 || c.DestinationList()[0].ID != "ice-pad" {
		t.Errorf("a former peer leaked into peers/destinations: %+v / %+v", c.Peers, c.DestinationList())
	}
	// Without the table the raw id is the fallback, as before.
	c2, _ := Parse([]byte(fix16Base), "site.toml")
	if got := c2.SiteName("ndb"); got != SiteLabel("ndb") {
		t.Errorf("no former_peers: SiteName = %q", got)
	}
}

func TestFix16FormerPeersRefusals(t *testing.T) {
	for _, c := range []struct{ name, extra, want string }{
		{"still a peer", "[[former_peers]]\nsite_id = \"ice-pad\"\n", "still a configured peer"},
		{"itself", "[[former_peers]]\nsite_id = \"ice-road\"\n", "cannot list itself"},
		{"twice", "[[former_peers]]\nsite_id = \"ndb\"\n[[former_peers]]\nsite_id = \"ndb\"\n", "duplicate former peer"},
		{"no id", "[[former_peers]]\ndisplay_name = \"X\"\n", "site_id is required"},
		{"typo", "[[former_peers]]\nsite_id = \"ndb\"\ndisplayname = \"X\"\n", "unknown key"},
	} {
		_, err := Parse([]byte(fix16Base+c.extra), "site.toml")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want %q, got %v", c.name, c.want, err)
		}
	}
}

func TestFix16ParseIsLoadOnBytes(t *testing.T) {
	// The weather refusal and the unknown-key refusal run in Parse too.
	if _, err := Parse([]byte(fix16Base+"\n[[weather.phase]]\nphase = 2\n"), "x.toml"); err == nil || !strings.Contains(err.Error(), "weather") {
		t.Errorf("weather: %v", err)
	}
	if _, err := Parse([]byte("site_idd = \"x\"\n"), "x.toml"); err == nil || !strings.Contains(err.Error(), "x.toml") {
		t.Errorf("the refusal must name the file: %v", err)
	}
}
