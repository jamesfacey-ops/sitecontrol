package siteadmin

// FIX16 H: season generation, the cross-check, and the join/leave file
// edits — every output re-read through config.Load, every output scanned
// for the secrets it must never print.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sitecontrol/internal/config"
)

const fix16Plan = `
sync_interval_seconds = 10
entrance = "ice-road"

[[post]]
site_id = "ice-road"
display_name = "2P Ice Road Entrance"
base_url = "http://100.64.0.1:8081"
listen_addr = "100.64.0.1:8081"
site_type = "text_message"
orientation = false
road_work = { display_name = "Working on the ice road" }

[[post]]
site_id = "ice-pad"
display_name = "Ice Pad"
base_url = "http://100.64.0.2:8081"
listen_addr = "100.64.0.2:8081"
site_type = "camp"
orientation = true

[[post]]
site_id = "sid"
display_name = "STP"
long_name = "Salt Water Treatment Plant"
base_url = "http://100.64.0.3:8081"
listen_addr = "100.64.0.3:8081"
site_type = "camp"
orientation = true

[[route]]
a = "ice-road"
b = "ice-pad"
travel_minutes = 65

[[ungated]]
at = "ice-pad"
id = "conneco"
display_name = "Conneco pad"
`

func loadPlanText(t *testing.T, text string) *Plan {
	t.Helper()
	p := filepath.Join(t.TempDir(), "plan.toml")
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	pl, err := LoadPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	return pl
}

func loadSeason(t *testing.T, dir string, ids ...string) map[string]*config.Config {
	t.Helper()
	out := map[string]*config.Config{}
	for _, id := range ids {
		c, err := config.Load(filepath.Join(dir, id, "site.toml"))
		if err != nil {
			t.Fatalf("%s does not load: %v", id, err)
		}
		out[id] = c
	}
	return out
}

// secretsIn collects every key and login password in a set of configs.
func secretsIn(cfgs map[string]*config.Config) []string {
	var s []string
	for _, c := range cfgs {
		s = append(s, c.PeerKey, c.LoginPassword)
	}
	return s
}

func TestFix16SeasonGeneratesAMatchingMeshAndPrintsNoSecret(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "season")
	res, err := Season(loadPlanText(t, fix16Plan), dir)
	if err != nil {
		t.Fatal(err)
	}
	cfgs := loadSeason(t, dir, "ice-road", "ice-pad", "sid")
	if probs := CrossCheck(cfgs); len(probs) > 0 {
		t.Fatalf("generated season does not match up: %v", probs)
	}
	road, pad, stp := cfgs["ice-road"], cfgs["ice-pad"], cfgs["sid"]
	// Roads both ways, the ungated pad only at its post, explicit none elsewhere.
	if d := pad.DestinationList(); len(d) != 2 || d[0].ID != "ice-road" || !d[0].Controlled || d[0].TravelMinutes != 65 || d[1].ID != "conneco" || d[1].Controlled {
		t.Errorf("ice-pad destinations = %+v", d)
	}
	if d := road.DestinationList(); len(d) != 1 || d[0].ID != "ice-pad" || d[0].TravelMinutes != 65 {
		t.Errorf("ice-road destinations = %+v", d)
	}
	if d := stp.DestinationList(); len(d) != 0 {
		t.Errorf("sid must dispatch nowhere, got %+v", d)
	}
	if !road.RoadWorkEnabled() || road.RoadWorkLabel() != "Working on the ice road" {
		t.Error("ice-road lost its [road_work]")
	}
	if stp.SiteName("ice-road") != "2P Ice Road Entrance" || road.SiteLongName("sid") != "Salt Water Treatment Plant" {
		t.Error("peer display/long names not carried")
	}
	if pad.EntranceSiteID != "ice-road" || !pad.HasOrientation() || road.HasOrientation() {
		t.Error("entrance / orientation not carried")
	}
	for _, c := range cfgs {
		if c.NodeEpoch != 1 || c.ListenAddr == "" || len(c.Peers) != 2 {
			t.Errorf("%s: epoch %d listen %q peers %d", c.SiteID, c.NodeEpoch, c.ListenAddr, len(c.Peers))
		}
		if len(c.PeerKey) < 40 || len(c.LoginPassword) < 16 {
			t.Errorf("%s: weak generated secret", c.SiteID)
		}
	}
	// No secret in the printed lines or the IT checklist; every secret-bearing file is 0600.
	printed := strings.Join(res.Lines, "\n")
	it, _ := os.ReadFile(filepath.Join(dir, "IT-CHECKLIST.txt"))
	for _, s := range secretsIn(cfgs) {
		if strings.Contains(printed, s) || strings.Contains(string(it), s) {
			t.Fatal("a key or password appears in the printed output or the IT checklist")
		}
	}
	pw, _ := os.ReadFile(filepath.Join(dir, "passwords.txt"))
	for _, c := range cfgs {
		if !strings.Contains(string(pw), c.LoginPassword) || strings.Contains(string(pw), c.PeerKey) {
			t.Errorf("passwords.txt must hold %s's login password and no peer key", c.SiteID)
		}
	}
	for _, f := range []string{"ice-road/site.toml", "passwords.txt", "IT-CHECKLIST.txt"} {
		if fi, err := os.Stat(filepath.Join(dir, f)); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v, want 0600", f, fi.Mode().Perm())
		}
	}
	if !strings.Contains(printed, Fingerprint(pad.PeerKey)) {
		t.Error("the output must name each key by its fingerprint")
	}
	// Two seasons never share a key.
	dir2 := filepath.Join(t.TempDir(), "s2")
	if _, err := Season(loadPlanText(t, fix16Plan), dir2); err != nil {
		t.Fatal(err)
	}
	if c2 := loadSeason(t, dir2, "ice-pad")["ice-pad"]; c2.PeerKey == pad.PeerKey {
		t.Fatal("two seasons generated the same key")
	}
}

func TestFix16SeasonRefusalsWriteNothing(t *testing.T) {
	cases := []struct{ name, edit, want string }{
		{"duplicate id", strings.Replace(fix16Plan, `site_id = "sid"`, `site_id = "ice-pad"`, 1), "duplicate site_id"},
		{"duplicate name", strings.Replace(fix16Plan, `display_name = "STP"`, `display_name = "ice pad"`, 1), "two posts with one name"},
		{"route to nowhere", fix16Plan + "\n[[route]]\na = \"sid\"\nb = \"npf\"\n", "both ends must be posts"},
		{"road twice", fix16Plan + "\n[[route]]\na = \"ice-pad\"\nb = \"ice-road\"\n", "already listed"},
		{"ungated is a post", fix16Plan + "\n[[ungated]]\nat = \"sid\"\nid = \"ice-pad\"\n", "write it as a [[route]]"},
		{"no orientation", strings.Replace(fix16Plan, "orientation = false\n", "", 1), "orientation is required"},
		{"bad site type", strings.Replace(fix16Plan, `site_type = "text_message"`, `site_type = "pad"`, 1), "site_type"},
		{"base_url with path", strings.Replace(fix16Plan, `"http://100.64.0.3:8081"`, `"http://100.64.0.3:8081/x"`, 1), "base_url"},
		{"same base_url", strings.Replace(fix16Plan, `base_url = "http://100.64.0.3:8081"`, `base_url = "http://100.64.0.2:8081"`, 1), "base_url is also"},
		{"reserved id", strings.Replace(fix16Plan, `site_id = "sid"`, `site_id = "road-work"`, 1), "reserved"},
		{"unknown entrance", strings.Replace(fix16Plan, `entrance = "ice-road"`, `entrance = "gate"`, 1), "entrance"},
	}
	for _, c := range cases {
		dir := filepath.Join(t.TempDir(), "out")
		_, err := Season(loadPlanText(t, c.edit), dir)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want a refusal containing %q, got %v", c.name, c.want, err)
		}
		if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
			t.Errorf("%s: a refused season wrote %s", c.name, dir)
		}
	}
	// A non-empty output folder refuses (no old key file mixed in).
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "old.toml"), []byte("x"), 0o600)
	if _, err := Season(loadPlanText(t, fix16Plan), dir); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Errorf("want the not-empty refusal, got %v", err)
	}
	// A typo in the plan refuses rather than dropping the key.
	p := filepath.Join(t.TempDir(), "plan.toml")
	os.WriteFile(p, []byte(strings.Replace(fix16Plan, "travel_minutes = 65", "travel_minute = 65", 1)), 0o600)
	if _, err := LoadPlan(p); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("want unknown-key refusal, got %v", err)
	}
}

func TestFix16CrossCheckCatchesAWrongKeyAddressAndPeerSet(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "season")
	if _, err := Season(loadPlanText(t, fix16Plan), dir); err != nil {
		t.Fatal(err)
	}
	fresh := func() map[string]*config.Config { return loadSeason(t, dir, "ice-road", "ice-pad", "sid") }
	c := fresh()
	c["sid"].Peers[0].Key = "wrong"
	probs := strings.Join(CrossCheck(c), "\n")
	if !strings.Contains(probs, "sid cannot read ice-road") || strings.Contains(probs, "wrong") {
		t.Errorf("wrong key: %s", probs)
	}
	c = fresh()
	c["sid"].Peers[1].BaseURL = "http://10.9.9.9:8081"
	if probs := strings.Join(CrossCheck(c), "\n"); !strings.Contains(probs, "disagree on") {
		t.Errorf("wrong address: %s", probs)
	}
	c = fresh()
	c["sid"].Peers = c["sid"].Peers[:1]
	if probs := strings.Join(CrossCheck(c), "\n"); !strings.Contains(probs, "sid does not list") {
		t.Errorf("missing peer: %s", probs)
	}
	c = fresh()
	c["sid"].PeerKey = c["ice-pad"].PeerKey
	if probs := strings.Join(CrossCheck(c), "\n"); !strings.Contains(probs, "SAME peer key") {
		t.Errorf("shared key: %s", probs)
	}
}

// demoStyle is a hand-annotated file in the demo's shape: one peer, no
// destinations key (derived), comments everywhere.
const demoStyle = `# A hand-kept post file — every comment must survive an edit.
site_id      = "ice-road"
display_name = "2P Ice Road Entrance"
listen_addr  = "127.0.0.1:8081"   # the proxy fronts this
db_path      = "ice-road.db"
node_epoch   = 1
peer_key     = "key-ice-road"
login_password = "pw"
orientation  = false
site_type    = "text_message"
sync_interval_seconds = 10

# The pad, our only road.
[[peers]]
site_id  = "ice-pad"
display_name = "Ice Pad"
base_url = "http://127.0.0.1:8082"
key      = "key-ice-pad"

[road_work]
display_name = "Working on the ice road"
`

func joinFor(id string, dests ...JoinDest) *JoinFragment {
	k := "key-" + id
	return &JoinFragment{Kind: "join", SiteID: id, KeyFingerprint: Fingerprint(k),
		Peer: config.Peer{SiteID: id, DisplayName: strings.ToUpper(id), BaseURL: "http://127.0.0.1:8090", Key: k}, Destinations: dests}
}

func TestFix16PeerApplyKeepsCommentsAndMakesDerivedDestinationsExplicit(t *testing.T) {
	out, notes, err := ApplyJoin(demoStyle, joinFor("npf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, demoStyle[:strings.Index(demoStyle, "[road_work]")]) {
		t.Errorf("the edit changed the existing text:\n%s", out)
	}
	c, err := config.Parse([]byte(out), "edited")
	if err != nil {
		t.Fatal(err)
	}
	// Before: one peer, derived controlled destination to ice-pad. After:
	// that destination is written out, and npf is NOT a destination.
	d := c.DestinationList()
	if len(d) != 1 || d[0].ID != "ice-pad" || !d[0].Controlled {
		t.Fatalf("destinations after join = %+v, want only the old ice-pad", d)
	}
	if len(c.Peers) != 2 || c.Peers[1].SiteID != "npf" || c.Peers[1].Key != "key-npf" {
		t.Fatalf("peers = %+v", c.Peers)
	}
	if !c.RoadWorkEnabled() {
		t.Error("[road_work] lost")
	}
	if !strings.Contains(strings.Join(notes, "\n"), "explicitly") {
		t.Errorf("notes do not say the derived destination was written out: %v", notes)
	}
	for _, n := range notes {
		if strings.Contains(n, "key-npf") {
			t.Fatal("a note printed the key")
		}
	}
	// A post with explicit `destinations = []` gaining a destination drops that line.
	none := strings.Replace(demoStyle, "sync_interval_seconds = 10\n", "sync_interval_seconds = 10\ndestinations = []\n", 1)
	out, _, err = ApplyJoin(none, joinFor("npf", JoinDest{At: "ice-road", ID: "npf", DisplayName: "NPF", Controlled: true, TravelMinutes: 30}))
	if err != nil {
		t.Fatal(err)
	}
	c, _ = config.Parse([]byte(out), "edited")
	if d := c.DestinationList(); len(d) != 1 || d[0].ID != "npf" || d[0].TravelMinutes != 30 {
		t.Fatalf("destinations = %+v", d)
	}
	// A post with explicit none and no destination gained keeps none.
	out, _, err = ApplyJoin(none, joinFor("npf"))
	if err != nil {
		t.Fatal(err)
	}
	if c, _ = config.Parse([]byte(out), "edited"); len(c.DestinationList()) != 0 {
		t.Fatalf("destinations appeared from nowhere: %+v", c.DestinationList())
	}
}

func TestFix16PeerApplyRefusals(t *testing.T) {
	if _, _, err := ApplyJoin(demoStyle, joinFor("ice-pad")); err == nil || !strings.Contains(err.Error(), "already a peer") {
		t.Errorf("already a peer: %v", err)
	}
	if _, _, err := ApplyJoin(demoStyle, joinFor("ice-road")); err == nil || !strings.Contains(err.Error(), "THIS post") {
		t.Errorf("self: %v", err)
	}
	retired := demoStyle + "\n[[former_peers]]\nsite_id = \"npf\"\ndisplay_name = \"NPF\"\n"
	if _, _, err := ApplyJoin(retired, joinFor("npf")); err == nil || !strings.Contains(err.Error(), "NEW site_id") {
		t.Errorf("returning retired id: %v", err)
	}
	// A damaged fragment (key not matching its fingerprint) is refused on load.
	f := joinFor("npf")
	f.Peer.Key = "tampered"
	p := filepath.Join(t.TempDir(), "join-npf.toml")
	os.WriteFile(p, []byte(renderJoin(*f)), 0o600)
	if _, err := LoadJoin(p); err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Errorf("tampered fragment: %v", err)
	}
	os.WriteFile(p, []byte(renderJoin(*joinFor("npf"))), 0o600)
	if _, err := LoadJoin(p); err != nil {
		t.Errorf("good fragment refused: %v", err)
	}
}

func TestFix16ApplyLeaveRemovesThePostAndKeepsItsName(t *testing.T) {
	two := demoStyle + "\n[[peers]]\nsite_id = \"npf\"\ndisplay_name = \"NPF\"\nlong_name = \"North Production Facility\"\nbase_url = \"http://127.0.0.1:8083\"\nkey = \"key-npf\"\n\n[[destinations]]\nid = \"ice-pad\"\ncontrolled = true\n\n[[destinations]]\nid = \"npf\"\ncontrolled = true\ntravel_minutes = 20\n"
	if _, err := config.Parse([]byte(two), "fixture"); err != nil {
		t.Fatal(err)
	}
	out, _, err := ApplyLeave(two, &LeaveFragment{Kind: "leave", SiteID: "npf"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Parse([]byte(out), "edited")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Peers) != 1 || c.Peers[0].SiteID != "ice-pad" {
		t.Fatalf("peers = %+v", c.Peers)
	}
	if d := c.DestinationList(); len(d) != 1 || d[0].ID != "ice-pad" {
		t.Fatalf("destinations = %+v", d)
	}
	if c.SiteName("npf") != "NPF" || c.SiteLongName("npf") != "North Production Facility" {
		t.Errorf("retired post lost its name: %q / %q", c.SiteName("npf"), c.SiteLongName("npf"))
	}
	if strings.Contains(out, "key-npf") {
		t.Error("the retired post's key is still in the file")
	}
	// Removing the only destination writes an explicit none (never derived).
	out, notes, err := ApplyLeave(demoStyle, &LeaveFragment{Kind: "leave", SiteID: "ice-pad", DisplayName: "Ice Pad"})
	if err != nil {
		t.Fatal(err)
	}
	c, _ = config.Parse([]byte(out), "edited")
	if len(c.Peers) != 0 || len(c.DestinationList()) != 0 || !strings.Contains(strings.Join(notes, " "), "destinations = []") {
		t.Fatalf("peers %v dests %v notes %v", c.Peers, c.DestinationList(), notes)
	}
	// Refusals.
	if _, _, err := ApplyLeave(demoStyle, &LeaveFragment{SiteID: "npf"}); err == nil || !strings.Contains(err.Error(), "not a peer") {
		t.Errorf("not a peer: %v", err)
	}
	if _, _, err := ApplyLeave(out, &LeaveFragment{SiteID: "ice-pad"}); err == nil || !strings.Contains(err.Error(), "already retired") {
		t.Errorf("twice: %v", err)
	}
	ent := strings.Replace(demoStyle, "sync_interval_seconds = 10\n", "sync_interval_seconds = 10\nentrance_site_id = \"ice-pad\"\n", 1)
	if _, _, err := ApplyLeave(ent, &LeaveFragment{SiteID: "ice-pad"}); err == nil || !strings.Contains(err.Error(), "entrance") {
		t.Errorf("entrance: %v", err)
	}
}

// TestFix16AddSiteJoinsTheMeshAndEveryFileStillMatches: season of three,
// add a fourth from ONE live file, apply the join everywhere — the four
// files cross-check clean and the new post's road is in both files.
func TestFix16AddSiteJoinsTheMeshAndEveryFileStillMatches(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "season")
	if _, err := Season(loadPlanText(t, fix16Plan), dir); err != nil {
		t.Fatal(err)
	}
	live, _ := os.ReadFile(filepath.Join(dir, "ice-pad", "site.toml"))
	add := loadPlanText(t, `
[[post]]
site_id = "npf"
display_name = "NPF"
base_url = "http://100.64.0.4:8081"
listen_addr = "100.64.0.4:8081"
site_type = "camp"
orientation = true
[[route]]
a = "npf"
b = "sid"
travel_minutes = 25
`)
	out := filepath.Join(t.TempDir(), "add")
	// The live post's own address must be given, and must be its own.
	if _, err := AddSite(live, "http://100.64.0.1:8081", add, out); err == nil || !strings.Contains(err.Error(), "ice-road's address") {
		t.Fatalf("want the wrong -self-url refusal, got %v", err)
	}
	res, err := AddSite(live, "http://100.64.0.2:8081", add, out)
	if err != nil {
		t.Fatal(err)
	}
	f, err := LoadJoin(filepath.Join(out, "join-npf.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ice-road", "ice-pad", "sid"} {
		p := filepath.Join(dir, id, "site.toml")
		raw, _ := os.ReadFile(p)
		text, _, err := ApplyJoin(string(raw), f)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		os.WriteFile(p, []byte(text), 0o600)
	}
	os.MkdirAll(filepath.Join(dir, "npf"), 0o700)
	nb, _ := os.ReadFile(filepath.Join(out, "npf", "site.toml"))
	os.WriteFile(filepath.Join(dir, "npf", "site.toml"), nb, 0o600)
	cfgs := loadSeason(t, dir, "ice-road", "ice-pad", "sid", "npf")
	if probs := CrossCheck(cfgs); len(probs) > 0 {
		t.Fatalf("after the add the mesh does not match: %v", probs)
	}
	if d := cfgs["sid"].DestinationList(); len(d) != 1 || d[0].ID != "npf" || d[0].TravelMinutes != 25 {
		t.Errorf("sid destinations %+v", d)
	}
	if d := cfgs["npf"].DestinationList(); len(d) != 1 || d[0].ID != "sid" {
		t.Errorf("npf destinations %+v", d)
	}
	if cfgs["npf"].EntranceSiteID != "ice-road" || cfgs["npf"].SiteName("ice-road") != "2P Ice Road Entrance" {
		t.Error("the new post did not inherit the entrance / peer names")
	}
	printed := strings.Join(res.Lines, "\n")
	for _, s := range secretsIn(cfgs) {
		if strings.Contains(printed, s) {
			t.Fatal("site add printed a secret")
		}
	}
	// An id already in the mesh is refused (that is a swap, not an add).
	again := loadPlanText(t, "[[post]]\nsite_id = \"sid\"\ndisplay_name = \"STP2\"\nbase_url = \"http://100.64.0.9:8081\"\nlisten_addr = \"100.64.0.9:8081\"\nsite_type = \"camp\"\norientation = true\n")
	if _, err := AddSite(live, "http://100.64.0.2:8081", again, filepath.Join(t.TempDir(), "o")); err == nil || !strings.Contains(err.Error(), "sitectl admin swap") {
		t.Errorf("adding an existing post: %v", err)
	}
}

// Q2 (James, 2026-10-06): a blank rebuild is no longer refused when a post
// has been retired — the live posts serve its records (repl's
// TestFix16ABlankRebuildGetsARetiredPostsRecordsFromALivePost) — it is
// only told so.
func TestFix16AddSiteCarriesRetiredPostsIntoTheNewcomer(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "season")
	if _, err := Season(loadPlanText(t, fix16Plan), dir); err != nil {
		t.Fatal(err)
	}
	live, _ := os.ReadFile(filepath.Join(dir, "ice-pad", "site.toml"))
	live = append(live, []byte("\n"+RenderFormerPeerBlock(config.FormerPeer{SiteID: "ndb", DisplayName: "NDB"}))...)
	add := loadPlanText(t, "[[post]]\nsite_id = \"npf\"\ndisplay_name = \"NPF\"\nbase_url = \"http://100.64.0.4:8081\"\nlisten_addr = \"100.64.0.4:8081\"\nsite_type = \"camp\"\norientation = true\n")
	out := filepath.Join(t.TempDir(), "add")
	if _, err := AddSite(live, "http://100.64.0.2:8081", add, out); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(filepath.Join(out, "npf", "site.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.SiteName("ndb") != "NDB" || len(c.FormerPeers) != 1 {
		t.Fatalf("the newcomer does not know the retired post: %+v", c.FormerPeers)
	}
}

func TestFix16BlankRebuildNoteQ2(t *testing.T) {
	cfg := &config.Config{SiteID: "x", FormerPeers: []config.FormerPeer{{SiteID: "ndb", DisplayName: "NDB"}}}
	if n := BlankRebuildNote(cfg, true); !strings.Contains(n, "NDB (ndb) will be pulled back from the live posts") {
		t.Errorf("note = %q", n)
	}
	if n := BlankRebuildNote(cfg, false); n != "" {
		t.Errorf("a rebuild from a database says nothing: %q", n)
	}
	if n := BlankRebuildNote(&config.Config{SiteID: "x"}, true); n != "" {
		t.Errorf("no retired posts says nothing: %q", n)
	}
}

func TestFix16KeysAndPasswords(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		k, p := NewKey(), NewPassword()
		if seen[k] || seen[p] {
			t.Fatal("repeat")
		}
		seen[k], seen[p] = true, true
		if !regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`).MatchString(k) {
			t.Fatalf("key shape %q", k)
		}
		if !regexp.MustCompile(`^[a-z2-9]{4}(-[a-z2-9]{4}){3}$`).MatchString(p) || strings.ContainsAny(p, "01lio") {
			t.Fatalf("password shape %q", p)
		}
	}
	if f := Fingerprint("abc"); f != "fp:ba7816bf" {
		t.Fatalf("fingerprint = %s", f)
	}
}
