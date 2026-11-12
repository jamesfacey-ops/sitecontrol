// demo_explicit_none_test.go — DEMO-FIVE-SITES: a post with peers must be able
// to say "I have no travel destinations" in so many words, and must never
// acquire destinations from its peer list by omission.
package config

import "testing"

// The D-72 shape for NPF / NDB / STP: four peers, no travel. `destinations = []`
// is the explicit statement. Before the fix this was refused (len==0 read as
// "omitted") so the only way to load such a post was to invent a destination.
func TestExplicitEmptyDestinationsLoadsAsNoTravelAndNeverDerives(t *testing.T) {
	c, err := loadD77(t, "destinations = []\n"+fourPeersNoDestinations)
	if err != nil {
		t.Fatalf("4 peers + explicit `destinations = []` must Load: %v", err)
	}
	if got := c.DestinationList(); len(got) != 0 {
		t.Fatalf("DestinationList() = %+v, want empty — nothing may be derived from the 4 peers", got)
	}
	if ids := c.ControlledDestinationIDs(); len(ids) != 0 {
		t.Fatalf("ControlledDestinationIDs() = %v, want none", ids)
	}
	if c.VehicleModeEnabled() {
		t.Fatal("VehicleModeEnabled() true at a post with no destinations")
	}
}

// The same declaration at ONE peer: absent derives (legacy, pinned by
// TestDestinationsFallbackBoundaryIsExactlyOnePeer); declared-empty must not.
func TestExplicitEmptyDestinationsAtOnePeerDoesNotDerive(t *testing.T) {
	body := `
site_id               = "site-x"
listen_addr           = "127.0.0.1:0"
node_epoch            = 1
db_path               = "test.db"
sync_interval_seconds = 10
login_password        = "x"
orientation           = false
site_type             = "text_message"
destinations          = []

[[peers]]
site_id  = "site-a"
base_url = "http://127.0.0.1:0"
key      = "k"
`
	c, err := loadD77(t, body)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.DestinationList(); len(got) != 0 {
		t.Fatalf("1 peer + `destinations = []` derived %+v, want none", got)
	}
}

func TestLoginHintDefaultsOffAndLoadsWhenSet(t *testing.T) {
	c, err := loadD77(t, "destinations = []\n"+fourPeersNoDestinations)
	if err != nil {
		t.Fatal(err)
	}
	if c.LoginHint != "" {
		t.Fatalf("LoginHint = %q, want empty when unset", c.LoginHint)
	}
	c, err = loadD77(t, "login_hint = \"password is demo\"\ndestinations = []\n"+fourPeersNoDestinations)
	if err != nil || c.LoginHint != "password is demo" {
		t.Fatalf("login_hint did not load: c=%+v err=%v", c, err)
	}
}
