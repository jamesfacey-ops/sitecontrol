package web

import "strings"

// padLabel names an UNCONTROLLED pad by id: the configured display name when
// siteName knows one, else the id made readable ("conneco-pad" -> "Conneco
// Pad"). ⟦fix4 H, 2026-10-05 (L03-07 obs)⟧ a replicated trip can name a pad
// this post does not configure, and SiteName's fallback (config.SiteLabel)
// returns such an id unchanged, so posts without the pad printed
// "conneco-pad". A pad is never a post, so a humanised id cannot be mistaken
// for one; post ids keep SiteName's own fallback.
func padLabel(siteName func(string) string, id string) string {
	if n := siteName(id); n != id {
		return n
	}
	words := strings.FieldsFunc(id, func(r rune) bool { return r == '-' || r == '_' || r == ' ' })
	if len(words) == 0 {
		return id
	}
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// destinationAssertedProse rewrites the canonical destination rendering the
// domain keeps for the agreement oracle ("controlled ice-pad, 90 min",
// "uncontrolled <pad>") so a person reads the site by its display name.
// James, 2026-10-04: no screen shows a raw site id. The canonical string
// itself stays unchanged in the domain (renderDestinationAmend): the oracle
// compares it. Only the /trips/detail ledger cell is translated, via the
// `assertedprose` template func. [MEASURED 2026-10-04: no tool under
// sc-test/tools or cmd/ parses that cell.] Any other value passes through.
func destinationAssertedProse(siteName func(string) string, v string) string {
	for _, kind := range []string{"controlled ", "uncontrolled "} {
		if !strings.HasPrefix(v, kind) {
			continue
		}
		rest := v[len(kind):]
		id, tail := rest, ""
		if i := strings.Index(rest, ", "); i >= 0 {
			id, tail = rest[:i], rest[i:]
		}
		if kind == "uncontrolled " {
			return kind + padLabel(siteName, id) + tail
		}
		return kind + siteName(id) + tail
	}
	return v
}
