package siteadmin

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// The config files are hand-annotated (the demo's carry paragraphs of
// history), so every edit here is TEXTUAL: whole tables are appended or cut
// out and one top-level line may be replaced. Nothing is re-serialised, so
// every comment outside the touched block survives byte for byte. Every
// edited text is re-validated with config.Parse by the caller before it is
// written anywhere.

var headerRE = regexp.MustCompile(`^\s*\[\[?\s*([A-Za-z0-9_.]+)\s*\]\]?\s*(#.*)?$`)

// section is one run of lines. Section 0 is the top-level key/value part;
// every later section starts at a table header line.
type section struct {
	name  string // "" for the top level, else the header's table name
	array bool   // [[name]]
	lines []string
}

func splitSections(text string) []section {
	lines := strings.SplitAfter(text, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	secs := []section{{}}
	for _, ln := range lines {
		if m := headerRE.FindStringSubmatch(strings.TrimRight(ln, "\r\n")); m != nil && !inMultiline(secs) {
			secs = append(secs, section{name: m[1], array: strings.HasPrefix(strings.TrimSpace(ln), "[[")})
		}
		secs[len(secs)-1].lines = append(secs[len(secs)-1].lines, ln)
	}
	return secs
}

// inMultiline guards against a header-looking line inside a """ string: an
// odd count of """ so far in the current section means we are inside one.
func inMultiline(secs []section) bool {
	n := 0
	for _, l := range secs[len(secs)-1].lines {
		n += strings.Count(l, `"""`) + strings.Count(l, `'''`)
	}
	return n%2 == 1
}

func joinSections(secs []section) string {
	var b strings.Builder
	for _, s := range secs {
		for _, l := range s.lines {
			b.WriteString(l)
		}
	}
	return b.String()
}

// fieldOf decodes one array-of-tables section's body and returns a string
// field ("site_id" of a [[peers]] block, "id" of a [[destinations]] block).
func fieldOf(s section, key string) string {
	body := strings.Join(s.lines[1:], "")
	var m map[string]any
	if _, err := toml.Decode(body, &m); err != nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

// removeArrayEntries cuts every [[name]] section whose `key` equals value.
// It returns the new text and how many were cut. Trailing blank lines of a
// cut block go with it; a comment block that introduced the NEXT table is
// left alone (it belongs to the table after, as the reader sees it).
func removeArrayEntries(text, name, key, value string) (string, int) {
	secs := splitSections(text)
	out := secs[:0:0]
	n := 0
	for _, s := range secs {
		if s.array && s.name == name && fieldOf(s, key) == value {
			n++
			continue
		}
		out = append(out, s)
	}
	return joinSections(out), n
}

// appendBlocks adds table blocks at the end of the file.
func appendBlocks(text string, blocks ...string) string {
	if !strings.HasSuffix(text, "\n") && text != "" {
		text += "\n"
	}
	for _, bl := range blocks {
		text += "\n" + bl
	}
	return text
}

// setTopLevel replaces the top-level line `key = …` (in section 0) with
// `key = value`, keeping any trailing comment. It refuses when the key is
// absent or present more than once.
func setTopLevel(text, key, value string) (string, error) {
	secs := splitSections(text)
	re := regexp.MustCompile(`^(\s*` + regexp.QuoteMeta(key) + `\s*=\s*)([^#\r\n]*?)(\s*(#[^\r\n]*)?)(\r?\n?)$`)
	hits := 0
	for i, l := range secs[0].lines {
		if m := re.FindStringSubmatch(l); m != nil {
			hits++
			secs[0].lines[i] = m[1] + value + m[3] + m[5]
		}
	}
	if hits != 1 {
		return "", fmt.Errorf("expected exactly one top-level %q line, found %d", key, hits)
	}
	return joinSections(secs), nil
}

var emptyDestsRE = regexp.MustCompile(`^\s*destinations\s*=\s*\[\s*\]\s*(#.*)?\r?\n?$`)

// dropEmptyDestinations removes a top-level `destinations = []` line (it
// cannot coexist with a [[destinations]] table). Reports whether it was there.
func dropEmptyDestinations(text string) (string, bool) {
	secs := splitSections(text)
	keep := secs[0].lines[:0:0]
	found := false
	for _, l := range secs[0].lines {
		if emptyDestsRE.MatchString(l) {
			found = true
			continue
		}
		keep = append(keep, l)
	}
	secs[0].lines = keep
	return joinSections(secs), found
}

// addTopLevelLine inserts a top-level line at the end of section 0 (before
// the first table header), where a top-level key must live.
func addTopLevelLine(text, line string) string {
	secs := splitSections(text)
	top := secs[0].lines
	// Insert after the last non-blank line of the top level so the blank
	// line before the first table survives.
	i := len(top)
	for i > 0 && strings.TrimSpace(top[i-1]) == "" {
		i--
	}
	if i > 0 && !strings.HasSuffix(top[i-1], "\n") {
		top[i-1] += "\n"
	}
	nl := append([]string{}, top[:i]...)
	nl = append(nl, line+"\n")
	nl = append(nl, top[i:]...)
	secs[0].lines = nl
	return joinSections(secs)
}
