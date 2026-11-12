package domain

import (
	"strings"

	"sitecontrol/internal/store"
)

// SiteLabel renders a site id for a person to read. Ids key replicated data
// and URLs and are never changed; this is display only.
//
// James, 2026-10-03: "all site names need to be fully capitalized on the
// acronyms NPF, STP, NDB." An id that is a single short run of letters (npf,
// ndb, stp) is an acronym and renders upper-case; a hyphenated or longer
// id (ice-road, ice-pad) is a mixed-case name and is returned unchanged. This
// is the fallback for when config holds no display name for the site; a
// configured display name always wins where the caller has one.
func SiteLabel(id string) string {
	if len(id) == 0 || len(id) > 4 {
		return id
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') {
			return id
		}
	}
	return strings.ToUpper(id)
}

// siteNameAt renders a site id the way the post that owns st names it:
// st.SiteNamer when the web layer installed one, else SiteLabel.
// James, 2026-10-04: refusals name a post by its display name, not its id.
func siteNameAt(st *store.Store, id string) string {
	if st != nil && st.SiteNamer != nil {
		return st.SiteNamer(id)
	}
	return SiteLabel(id)
}
