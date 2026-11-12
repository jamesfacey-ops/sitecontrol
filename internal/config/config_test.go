// config_test.go — destination-list validation (configurable destinations,
// James 2026-08-20). The controlled/uncontrolled split decides which site can
// close a trip's expectation, so a destination list that disagrees with the
// peer list must fail loudly AT STARTUP (Load), never surface at dispatch
// time. First test file for this package; the pre-existing validate() rules
// get no retroactive coverage here — only the new destination rules and their
// interaction with peers.
package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConfig writes a minimal valid config plus extra TOML and Loads it.
func loadWith(t *testing.T, extra string) (*Config, error) {
	t.Helper()
	base := `
site_id               = "site-a"
listen_addr           = "127.0.0.1:0"
node_epoch            = 1
db_path               = "test.db"
sync_interval_seconds = 10
login_password        = "x"
orientation           = false
site_type             = "text_message"

[[peers]]
site_id  = "site-b"
base_url = "http://127.0.0.1:0"
key      = "k"
`
	path := filepath.Join(t.TempDir(), "site.toml")
	if err := os.WriteFile(path, []byte(base+extra), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func TestControlledDestinationMustBeAPeer(t *testing.T) {
	_, err := loadWith(t, `
[[destinations]]
id         = "site-quaqua"
controlled = true
`)
	if err == nil {
		t.Fatal("controlled destination that is not a configured peer must fail Load — nobody could ever close its expectations")
	}
}

func TestUncontrolledDestinationMustNotBeAPeer(t *testing.T) {
	_, err := loadWith(t, `
[[destinations]]
id         = "site-b"
controlled = false
`)
	if err == nil {
		t.Fatal("uncontrolled destination that IS a configured peer must fail Load — it would silently strip the timer from runs to a gated site")
	}
}

func TestDuplicateDestinationIDRefused(t *testing.T) {
	_, err := loadWith(t, `
[[destinations]]
id         = "conneco"
controlled = false

[[destinations]]
id         = "conneco"
controlled = false
`)
	if err == nil {
		t.Fatal("duplicate destination ids must fail Load")
	}
}

func TestOwnSiteAsDestinationRefused(t *testing.T) {
	_, err := loadWith(t, `
[[destinations]]
id         = "site-a"
controlled = false
`)
	if err == nil {
		t.Fatal("a site as its own destination must fail Load")
	}
}

func TestValidMixedDestinationListLoads(t *testing.T) {
	c, err := loadWith(t, `
[[destinations]]
id           = "site-b"
display_name = "Pad Site Control"
controlled   = true

[[destinations]]
id           = "conneco"
display_name = "Conneco Pad"
controlled   = false
`)
	if err != nil {
		t.Fatalf("valid mixed destination list refused: %v", err)
	}
	list := c.DestinationList()
	if len(list) != 2 || !list[0].Controlled || list[1].Controlled {
		t.Fatalf("DestinationList = %+v, want [site-b controlled, conneco uncontrolled]", list)
	}
	if got := c.ControlledDestinationIDs(); len(got) != 1 || got[0] != "site-b" {
		t.Fatalf("ControlledDestinationIDs = %v, want [site-b]", got)
	}
	if got := c.UncontrolledDestinationIDs(); len(got) != 1 || got[0] != "conneco" {
		t.Fatalf("UncontrolledDestinationIDs = %v, want [conneco]", got)
	}
}

// No [[destinations]] at all: every existing deploy config and test fixture.
// The list derives one CONTROLLED destination per peer — the exact
// pre-destinations behaviour of the dispatch form's peer-built <select>.
func TestNoDestinationsDerivesControlledPeers(t *testing.T) {
	c, err := loadWith(t, "")
	if err != nil {
		t.Fatal(err)
	}
	list := c.DestinationList()
	if len(list) != 1 || list[0].ID != "site-b" || !list[0].Controlled {
		t.Fatalf("derived DestinationList = %+v, want [site-b controlled]", list)
	}
	if got := c.UncontrolledDestinationIDs(); got == nil || len(got) != 0 {
		t.Fatalf("UncontrolledDestinationIDs = %#v, want non-nil empty (membership always enforced)", got)
	}
}

func TestDisplayNameDefaultsToID(t *testing.T) {
	c, err := loadWith(t, `
[[destinations]]
id         = "conneco"
controlled = false
`)
	if err != nil {
		t.Fatal(err)
	}
	if list := c.DestinationList(); list[0].DisplayName != "conneco" {
		t.Fatalf("DisplayName = %q, want defaulted to id", list[0].DisplayName)
	}
}

// The four shipped deploy configs must always pass Load — a typo'd
// destination block failing at first boot on a site box is exactly the
// failure this test moves to CI time. Skipped if the deploy tree is absent
// (e.g. a stripped archive).
func TestShippedDeployConfigsLoad(t *testing.T) {
	for _, f := range []string{
		"../../deploy/site-a.toml",
		"../../deploy/site-b.toml",
		"../../deploy/site-a.proxmox.toml",
		"../../deploy/site-b.proxmox.toml",
	} {
		if _, err := os.Stat(f); err != nil {
			t.Skipf("deploy tree not present: %v", err)
		}
		c, err := Load(f)
		if err != nil {
			t.Fatalf("%s does not Load: %v", f, err)
		}
		var uncontrolled int
		for _, d := range c.DestinationList() {
			if !d.Controlled {
				uncontrolled++
			}
		}
		if uncontrolled == 0 {
			t.Fatalf("%s has no uncontrolled destination — the Conneco pad button is the point of the destination list", f)
		}
	}
}
