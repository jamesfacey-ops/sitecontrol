package siteadmin

import (
	"fmt"
	"strings"

	"sitecontrol/internal/config"
)

// BlankRebuildNote is design Q2, kept in its own file so the ruling lives
// in one function (lead, 2026-10-06: "keep each one isolated").
//
// James, 2026-10-06 ruled that every site has the full database: a laptop
// rebuilt BLANK after a post was retired must get that post's records back.
// Since FIX16 H every live post serves the stream of each post in its
// [[former_peers]] from the copy it holds (repl: refuseForeignSite /
// drainFormer), so a blank rebuild pulls a retired post's history from any
// live post exactly as it pulls a live one's — the earlier refusal ("restore
// from backup or accept the loss") is withdrawn. Restore-from-backup stays
// available (-from-backup). This only says what will happen.
func BlankRebuildNote(cfg *config.Config, blank bool) string {
	if !blank || len(cfg.FormerPeers) == 0 {
		return ""
	}
	var names []string
	for _, f := range cfg.FormerPeers {
		names = append(names, fmt.Sprintf("%s (%s)", cfg.SiteName(f.SiteID), f.SiteID))
	}
	return fmt.Sprintf("blank rebuild: the retired post(s) %s will be pulled back from the live posts along with everything else; `swap status` shows them under each stream", strings.Join(names, ", "))
}
