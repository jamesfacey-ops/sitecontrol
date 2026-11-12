package siteadmin

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"sitecontrol/internal/config"
)

// FIX17b (sc-stress F7): a season-built mesh writes each peer's site_type
// into the [[peers]] block every other post holds, so NDB's missed sign-outs
// report judges every peer's visits without IT adding the line by hand.
func TestFix17bSeasonPeerBlocksCarryTheSiteType(t *testing.T) {
	blk := RenderPeerBlock(PeerEntry(PlanPost{SiteID: "ice-pad", DisplayName: "Ice Pad", SiteType: "camp", BaseURL: "http://10.0.0.2:8080/"}, "k"))
	if !strings.Contains(blk, `site_type    = "camp"`) {
		t.Fatalf("the peer block carries no site_type:\n%s", blk)
	}
	var got struct{ Peers []config.Peer }
	if _, err := toml.Decode(blk, &got); err != nil || len(got.Peers) != 1 || got.Peers[0].SiteType != "camp" {
		t.Fatalf("the block does not read back as a camp peer: %+v %v", got, err)
	}
	if blk := RenderPeerBlock(PeerEntry(PlanPost{SiteID: "x", BaseURL: "http://h"}, "k")); strings.Contains(blk, "site_type") {
		t.Fatalf("a post with no site type writes one anyway:\n%s", blk)
	}
}
