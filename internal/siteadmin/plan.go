package siteadmin

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
)

// Plan is the season input (`sitectl admin season -plan site-plan.toml`):
// every post, every road between posts, every ungated pad. It holds NO
// secret — keys and login passwords are generated, never typed — so it can
// be kept, mailed and reviewed like any other planning document.
type Plan struct {
	SyncIntervalSeconds int    `toml:"sync_interval_seconds"`
	MusterMinGroupSize  *int   `toml:"muster_min_group_size"`
	ThemeCookieDomain   string `toml:"theme_cookie_domain"`
	// Entrance is the post written as entrance_site_id in every file.
	// Optional; unset leaves every file neutral (config.EntranceSiteID).
	Entrance string `toml:"entrance"`

	Posts   []PlanPost    `toml:"post"`
	Routes  []PlanRoute   `toml:"route"`
	Ungated []PlanUngated `toml:"ungated"`

	// FormerPeers is not a plan key: `site add` copies the live mesh's
	// retired posts into the newcomer's file, so it names them and pulls
	// their records like every other post (FIX16 H, Q2).
	FormerPeers []config.FormerPeer `toml:"-"`
}

// PlanPost is one post. The fields are the config keys of the same name.
type PlanPost struct {
	SiteID             string           `toml:"site_id"`
	DisplayName        string           `toml:"display_name"`
	LongName           string           `toml:"long_name"`
	BaseURL            string           `toml:"base_url"`
	ListenAddr         string           `toml:"listen_addr"`
	DBPath             string           `toml:"db_path"` // default "<site_id>.db"
	SiteType           string           `toml:"site_type"`
	Orientation        *bool            `toml:"orientation"`
	MusterMinGroupSize *int             `toml:"muster_min_group_size"`
	RoadWork           *config.RoadWork `toml:"road_work"`
}

// PlanRoute is a road between two gated posts. It is BOTH directions —
// each post gets a controlled [[destinations]] entry for the other — unless
// OneWay says A dispatches to B only. Writing a road once is what makes an
// asymmetric pair impossible to produce by accident (design Q9).
type PlanRoute struct {
	A             string `toml:"a"`
	B             string `toml:"b"`
	TravelMinutes int64  `toml:"travel_minutes"`
	OneWay        bool   `toml:"one_way"`
}

// PlanUngated is an uncontrolled destination (no gate, no officer — the
// Conneco pad shape): only the post At can dispatch there.
type PlanUngated struct {
	At            string `toml:"at"`
	ID            string `toml:"id"`
	DisplayName   string `toml:"display_name"`
	TravelMinutes int64  `toml:"travel_minutes"`
}

// LoadPlan reads a plan file. Unknown keys refuse, as in config.Load: a typo
// must not silently drop a road.
func LoadPlan(path string) (*Plan, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Plan
	meta, err := toml.Decode(string(raw), &p)
	if err != nil {
		return nil, fmt.Errorf("plan %s: %w", path, err)
	}
	if u := meta.Undecoded(); len(u) > 0 {
		return nil, fmt.Errorf("plan %s: unknown key %q (typo?)", path, u[0].String())
	}
	return &p, nil
}

// Secrets is what a season generates per post. Held in memory only until
// written into that post's own files.
type Secrets struct {
	PeerKey       string
	LoginPassword string
}

// Validate is every refusal that can be stated about the plan itself,
// before anything is generated. All problems are returned, not the first.
func (p *Plan) Validate() []string {
	var probs []string
	add := func(f string, a ...any) { probs = append(probs, fmt.Sprintf(f, a...)) }
	if len(p.Posts) == 0 {
		add("the plan lists no [[post]]")
	}
	if p.SyncIntervalSeconds < 0 {
		add("sync_interval_seconds must be positive (omit it for 10)")
	}
	ids, names, urls, listens := map[string]bool{}, map[string]string{}, map[string]string{}, map[string]string{}
	for i, post := range p.Posts {
		where := fmt.Sprintf("post[%d] (%s)", i, post.SiteID)
		switch {
		case post.SiteID == "":
			add("post[%d]: site_id is required", i)
			continue
		case !safeFileID(post.SiteID):
			add("%s: site_id must be a plain name (letters, digits, '-', '_'; no spaces or slashes)", where)
		case post.SiteID == config.RoadWorkChoice:
			add("%s: site_id %q is reserved for the working-on-the-road choice", where, post.SiteID)
		case ids[post.SiteID]:
			add("%s: duplicate site_id", where)
		}
		ids[post.SiteID] = true
		dn := strings.TrimSpace(post.DisplayName)
		if dn == "" {
			add("%s: display_name is required (it is what every other post calls this one)", where)
		} else if other, dup := names[strings.ToLower(dn)]; dup {
			add("%s: display_name %q is also %s's — two posts with one name cannot be told apart on any screen", where, dn, other)
		} else {
			names[strings.ToLower(dn)] = post.SiteID
		}
		if u, err := url.Parse(post.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
			add("%s: base_url %q must be http(s)://host:port with no path — the address every OTHER post uses to reach this one over the private link", where, post.BaseURL)
		} else if other, dup := urls[strings.TrimSuffix(post.BaseURL, "/")]; dup {
			add("%s: base_url is also %s's", where, other)
		} else {
			urls[strings.TrimSuffix(post.BaseURL, "/")] = post.SiteID
		}
		if _, _, err := net.SplitHostPort(post.ListenAddr); err != nil {
			add("%s: listen_addr %q must be host:port (this laptop's address on the private link)", where, post.ListenAddr)
		} else if other, dup := listens[post.ListenAddr]; dup {
			add("%s: listen_addr is also %s's", where, other)
		} else {
			listens[post.ListenAddr] = post.SiteID
		}
		if post.SiteType == "" {
			add("%s: site_type is required: \"text_message\" or \"camp\"", where)
		} else if _, err := domain.ParseSiteType(post.SiteType); err != nil {
			add("%s: site_type: %v", where, err)
		}
		if post.Orientation == nil {
			add("%s: orientation is required: true at a work site that gives its own orientation, false at a road post", where)
		}
		if post.RoadWork != nil && post.RoadWork.LimitMinutes < 0 {
			add("%s: road_work.limit_minutes must be positive (omit it for 720)", where)
		}
	}
	if p.Entrance != "" && !ids[p.Entrance] {
		add("entrance %q is not a post in this plan", p.Entrance)
	}
	pairs := map[[2]string]bool{}
	for i, r := range p.Routes {
		where := fmt.Sprintf("route[%d] (%s-%s)", i, r.A, r.B)
		switch {
		case !ids[r.A] || !ids[r.B]:
			add("%s: both ends must be posts in this plan (a road to a place with no gate is an [[ungated]] entry)", where)
			continue
		case r.A == r.B:
			add("%s: a road needs two different posts", where)
			continue
		case r.TravelMinutes < 0:
			add("%s: travel_minutes must be positive (omit it for no prefill)", where)
		}
		k := [2]string{r.A, r.B}
		if r.B < r.A {
			k = [2]string{r.B, r.A}
		}
		if pairs[k] {
			add("%s: this road is already listed — write each road once (it covers both directions unless one_way = true)", where)
		}
		pairs[k] = true
	}
	ug := map[[2]string]bool{}
	for i, u := range p.Ungated {
		where := fmt.Sprintf("ungated[%d] (%s at %s)", i, u.ID, u.At)
		switch {
		case !ids[u.At]:
			add("%s: at must be a post in this plan", where)
		case u.ID == "" || !safeFileID(u.ID):
			add("%s: id is required and must be a plain name", where)
		case ids[u.ID]:
			add("%s: %q is a post with a gate — write it as a [[route]], not an ungated pad (an ungated destination never times a run)", where, u.ID)
		case u.ID == config.RoadWorkChoice:
			add("%s: id %q is reserved for the working-on-the-road choice", where, u.ID)
		case ug[[2]string{u.At, u.ID}]:
			add("%s: listed twice for the same post", where)
		case u.TravelMinutes < 0:
			add("%s: travel_minutes must be positive", where)
		}
		ug[[2]string{u.At, u.ID}] = true
	}
	return probs
}

// destinationsFor is one post's [[destinations]], in plan order.
func (p *Plan) destinationsFor(id string) []config.Destination {
	byID := p.postByID()
	var out []config.Destination
	for _, r := range p.Routes {
		switch {
		case r.A == id:
			out = append(out, config.Destination{ID: r.B, DisplayName: byID[r.B].DisplayName, Controlled: true, TravelMinutes: r.TravelMinutes})
		case r.B == id && !r.OneWay:
			out = append(out, config.Destination{ID: r.A, DisplayName: byID[r.A].DisplayName, Controlled: true, TravelMinutes: r.TravelMinutes})
		}
	}
	for _, u := range p.Ungated {
		if u.At == id {
			out = append(out, config.Destination{ID: u.ID, DisplayName: u.DisplayName, Controlled: false, TravelMinutes: u.TravelMinutes})
		}
	}
	return out
}

func (p *Plan) postByID() map[string]PlanPost {
	m := make(map[string]PlanPost, len(p.Posts))
	for _, post := range p.Posts {
		m[post.SiteID] = post
	}
	return m
}

// PeerEntry renders the [[peers]] block another post holds for `post`.
// key is post's own peer_key.
func PeerEntry(post PlanPost, key string) config.Peer {
	pe := config.Peer{SiteID: post.SiteID, DisplayName: strings.TrimSpace(post.DisplayName), LongName: strings.TrimSpace(post.LongName), BaseURL: strings.TrimSuffix(post.BaseURL, "/"), Key: key}
	if post.RoadWork != nil && strings.TrimSpace(post.RoadWork.DisplayName) != "" {
		pe.RoadWorkLabel = strings.TrimSpace(post.RoadWork.DisplayName)
	}
	// FIX17b (sc-stress F7): the peer's site type travels with its block, so
	// NDB's missed sign-outs report can judge every peer's visits without IT
	// adding the line by hand. ⟦was: never written — "not judged" for every
	// peer of a season-built mesh⟧
	pe.SiteType = domain.SiteType(strings.TrimSpace(post.SiteType))
	return pe
}

// RenderPost writes one post's complete site.toml. secrets holds every
// post's generated secrets (peers' keys are their peer_keys).
func (p *Plan) RenderPost(post PlanPost, secrets map[string]Secrets) string {
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f, a...) }
	w("# SiteControl post config — generated by `sitectl admin season` (FIX16 H).\n")
	w("# ⚠ This file holds this post's login password and EVERY post's peer key.\n")
	w("# Keep it in C:\\ProgramData\\SiteControl\\ (or the Linux service folder) only. Never email it.\n")
	w("site_id      = %s\n", tomlStr(post.SiteID))
	w("display_name = %s\n", tomlStr(strings.TrimSpace(post.DisplayName)))
	if strings.TrimSpace(post.LongName) != "" {
		w("long_name    = %s\n", tomlStr(strings.TrimSpace(post.LongName)))
	}
	w("listen_addr  = %s\n", tomlStr(post.ListenAddr))
	db := post.DBPath
	if db == "" {
		db = post.SiteID + ".db"
	}
	w("db_path      = %s\n", tomlStr(db))
	w("node_epoch   = 1   # a REPLACEMENT laptop for this post must run `sitectl admin swap plan`, which raises this\n")
	w("peer_key     = %s\n", tomlStr(secrets[post.SiteID].PeerKey))
	w("login_password = %s\n", tomlStr(secrets[post.SiteID].LoginPassword))
	w("dev_routes   = false\n")
	w("orientation  = %t\n", *post.Orientation)
	w("site_type    = %s\n", tomlStr(post.SiteType))
	mm := 5
	if p.MusterMinGroupSize != nil {
		mm = *p.MusterMinGroupSize
	}
	if post.MusterMinGroupSize != nil {
		mm = *post.MusterMinGroupSize
	}
	w("muster_min_group_size = %d\n", mm)
	si := p.SyncIntervalSeconds
	if si == 0 {
		si = 10
	}
	w("sync_interval_seconds = %d\n", si)
	if p.ThemeCookieDomain != "" {
		w("theme_cookie_domain = %s\n", tomlStr(p.ThemeCookieDomain))
	}
	if p.Entrance != "" {
		w("entrance_site_id = %s\n", tomlStr(p.Entrance))
	}
	dests := p.destinationsFor(post.SiteID)
	others := 0
	for _, o := range p.Posts {
		if o.SiteID != post.SiteID {
			others++
		}
	}
	if len(dests) == 0 && others > 0 {
		w("destinations = []   # this post dispatches nowhere\n")
	}
	if post.RoadWork != nil {
		w("\n[road_work]\n")
		if post.RoadWork.LimitMinutes > 0 {
			w("limit_minutes = %d\n", post.RoadWork.LimitMinutes)
		}
		if strings.TrimSpace(post.RoadWork.DisplayName) != "" {
			w("display_name  = %s\n", tomlStr(strings.TrimSpace(post.RoadWork.DisplayName)))
		}
	}
	for _, o := range p.Posts {
		if o.SiteID == post.SiteID {
			continue
		}
		b.WriteString("\n")
		b.WriteString(RenderPeerBlock(PeerEntry(o, secrets[o.SiteID].PeerKey)))
	}
	for _, d := range dests {
		b.WriteString("\n")
		b.WriteString(RenderDestinationBlock(d))
	}
	for _, f := range p.FormerPeers {
		b.WriteString("\n")
		b.WriteString(RenderFormerPeerBlock(f))
	}
	return b.String()
}

// RenderPeerBlock is one [[peers]] table.
func RenderPeerBlock(pe config.Peer) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[[peers]]\nsite_id  = %s\n", tomlStr(pe.SiteID))
	if pe.DisplayName != "" {
		fmt.Fprintf(&b, "display_name = %s\n", tomlStr(pe.DisplayName))
	}
	if pe.LongName != "" {
		fmt.Fprintf(&b, "long_name    = %s\n", tomlStr(pe.LongName))
	}
	if pe.RoadWorkLabel != "" {
		fmt.Fprintf(&b, "road_work_label = %s\n", tomlStr(pe.RoadWorkLabel))
	}
	if pe.SiteType != "" {
		fmt.Fprintf(&b, "site_type    = %s\n", tomlStr(string(pe.SiteType)))
	}
	fmt.Fprintf(&b, "base_url = %s\nkey      = %s   # %s\n", tomlStr(pe.BaseURL), tomlStr(pe.Key), Fingerprint(pe.Key))
	return b.String()
}

// RenderDestinationBlock is one [[destinations]] table.
func RenderDestinationBlock(d config.Destination) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[[destinations]]\nid           = %s\n", tomlStr(d.ID))
	if d.DisplayName != "" {
		fmt.Fprintf(&b, "display_name = %s\n", tomlStr(d.DisplayName))
	}
	fmt.Fprintf(&b, "controlled   = %t\n", d.Controlled)
	if d.TravelMinutes > 0 {
		fmt.Fprintf(&b, "travel_minutes = %d\n", d.TravelMinutes)
	}
	return b.String()
}

// RenderFormerPeerBlock is one [[former_peers]] table.
func RenderFormerPeerBlock(f config.FormerPeer) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[[former_peers]]   # retired: its records stay, it is no longer pulled from or dispatched to\nsite_id      = %s\n", tomlStr(f.SiteID))
	if f.DisplayName != "" {
		fmt.Fprintf(&b, "display_name = %s\n", tomlStr(f.DisplayName))
	}
	if f.LongName != "" {
		fmt.Fprintf(&b, "long_name    = %s\n", tomlStr(f.LongName))
	}
	return b.String()
}

// tomlStr is a TOML basic string. strconv.Quote is not one: it emits \x
// escapes TOML does not have.
func tomlStr(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// CrossCheck is the "peers match up" rule over a whole set of posts'
// configs, keyed by site_id: every post lists every other post and nobody
// else; the key a post holds for X is X's own peer_key; the base_url every
// post holds for X agrees; and a controlled destination's far end is a post
// in the set. It is run on a freshly generated season and is also
// `sitectl admin check` over deployed files. Problems never quote a key.
func CrossCheck(cfgs map[string]*config.Config) []string {
	var probs []string
	add := func(f string, a ...any) { probs = append(probs, fmt.Sprintf(f, a...)) }
	ids := make([]string, 0, len(cfgs))
	for id := range cfgs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	urlOf := map[string]string{}
	keyOwner := map[string]string{}
	for _, id := range ids {
		c := cfgs[id]
		if c.SiteID != id {
			add("%s: the file says site_id %q", id, c.SiteID)
		}
		if other, dup := keyOwner[c.PeerKey]; dup {
			add("%s and %s accept the SAME peer key (%s) — every post needs its own", other, id, Fingerprint(c.PeerKey))
		}
		keyOwner[c.PeerKey] = id
	}
	for _, id := range ids {
		c := cfgs[id]
		listed := map[string]bool{}
		for _, pe := range c.Peers {
			listed[pe.SiteID] = true
			o, ok := cfgs[pe.SiteID]
			if !ok {
				add("%s lists %s as a peer, but %s is not in this set", id, pe.SiteID, pe.SiteID)
				continue
			}
			if pe.Key != o.PeerKey {
				add("%s holds %s for %s, but %s accepts %s — %s cannot read %s", id, Fingerprint(pe.Key), pe.SiteID, pe.SiteID, Fingerprint(o.PeerKey), id, pe.SiteID)
			}
			if prev, seen := urlOf[pe.SiteID]; seen && prev != strings.TrimSuffix(pe.BaseURL, "/") {
				add("posts disagree on %s's base_url (%s vs %s)", pe.SiteID, prev, pe.BaseURL)
			}
			urlOf[pe.SiteID] = strings.TrimSuffix(pe.BaseURL, "/")
		}
		for _, other := range ids {
			if other != id && !listed[other] {
				add("%s does not list %s as a peer — every post must reach every other directly (no post relays another's records)", id, other)
			}
		}
		for _, d := range c.Destinations {
			if d.Controlled {
				if _, ok := cfgs[d.ID]; !ok {
					add("%s dispatches to %s as a gated post, but %s is not in this set", id, d.ID, d.ID)
				}
			}
		}
	}
	return probs
}

// SeasonResult is what Season reports: no secrets, only fingerprints.
type SeasonResult struct {
	Lines []string
}

// Season generates every post's config into outDir/<site_id>/site.toml plus
// passwords.txt and IT-CHECKLIST.txt. It refuses, writing nothing, on any
// plan problem, on any rendered file config.Parse refuses, and on any
// CrossCheck problem, and when outDir already holds anything.
func Season(p *Plan, outDir string) (*SeasonResult, error) {
	if probs := p.Validate(); len(probs) > 0 {
		return nil, refusal(probs)
	}
	if ok, err := dirEmptyOrAbsent(outDir); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("REFUSED: %s is not empty — write a season into a new, empty folder so no old key-bearing file is mixed in", outDir)
	}
	secrets := map[string]Secrets{}
	for _, post := range p.Posts {
		secrets[post.SiteID] = Secrets{PeerKey: NewKey(), LoginPassword: NewPassword()}
	}
	texts := map[string]string{}
	cfgs := map[string]*config.Config{}
	var probs []string
	for _, post := range p.Posts {
		t := p.RenderPost(post, secrets)
		c, err := config.Parse([]byte(t), post.SiteID+"/site.toml")
		if err != nil {
			probs = append(probs, "the generated file would not start: "+err.Error())
			continue
		}
		texts[post.SiteID], cfgs[post.SiteID] = t, c
	}
	if len(probs) == 0 {
		probs = CrossCheck(cfgs)
	}
	if len(probs) > 0 {
		return nil, refusal(probs)
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return nil, err
	}
	res := &SeasonResult{}
	var pw, it strings.Builder
	pw.WriteString("SiteControl login passwords — one per post. For the officer handover sheet; then delete this file.\n\n")
	it.WriteString(itChecklistHeader)
	for _, post := range p.Posts {
		dir := filepath.Join(outDir, post.SiteID)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		if err := WriteSecretFile(filepath.Join(dir, "site.toml"), []byte(texts[post.SiteID])); err != nil {
			return nil, err
		}
		fmt.Fprintf(&pw, "%-24s %s\n", post.DisplayName+" ("+post.SiteID+")", secrets[post.SiteID].LoginPassword)
		res.Lines = append(res.Lines, fmt.Sprintf("  %-12s %-28s accepts %s, %d peer(s), %d destination(s)",
			post.SiteID, filepath.Join(post.SiteID, "site.toml"), Fingerprint(secrets[post.SiteID].PeerKey), len(cfgs[post.SiteID].Peers), len(cfgs[post.SiteID].DestinationList())))
		writeITPost(&it, p, post)
	}
	if err := WriteSecretFile(filepath.Join(outDir, "passwords.txt"), []byte(pw.String())); err != nil {
		return nil, err
	}
	if err := WriteSecretFile(filepath.Join(outDir, "IT-CHECKLIST.txt"), []byte(it.String())); err != nil {
		return nil, err
	}
	res.Lines = append(res.Lines, "  cross-check: every post lists every other, every key held matches the key accepted, every base_url agrees",
		"  wrote passwords.txt (login passwords, 0600) and IT-CHECKLIST.txt (no secrets)")
	return res, nil
}

const itChecklistHeader = `SiteControl — IT checklist (no secrets in this file)

For EVERY post below:
  1. Copy that post's site.toml ONLY to C:\ProgramData\SiteControl\site.toml on that post's laptop,
     over the private link (admin share / RDP) or a USB stick wiped afterwards. Never email, Teams or OneDrive.
  2. Install the service (install-service.ps1), then start it.
  3. Firewall: allow inbound TCP on the listen port, ONLY from the other posts' addresses listed.
  4. When every post is running: sitectl admin swap status -config site.toml  (reads READY when synced).
Afterwards delete this whole output folder: it is the only other copy of every key.

`

func writeITPost(b *strings.Builder, p *Plan, post PlanPost) {
	fmt.Fprintf(b, "%s (%s)\n  listens on %s; others reach it at %s\n  allow inbound from:", post.DisplayName, post.SiteID, post.ListenAddr, post.BaseURL)
	for _, o := range p.Posts {
		if o.SiteID == post.SiteID {
			continue
		}
		host := o.ListenAddr
		if h, _, err := net.SplitHostPort(o.ListenAddr); err == nil {
			host = h
		}
		fmt.Fprintf(b, " %s (%s)", host, o.SiteID)
	}
	b.WriteString("\n\n")
}

func refusal(probs []string) error {
	return fmt.Errorf("REFUSED — nothing was written:\n  - %s", strings.Join(probs, "\n  - "))
}
