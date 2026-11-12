package siteadmin

// FIX16 H: the swap and retire flows against REAL replication — real
// repl engines over real stores, served on httptest listeners with the same
// /sync/ and /debug/sync-status handlers a post serves. Nothing is faked
// about what a peer answers.

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/repl"
	"sitecontrol/internal/store"
)

type lateHandler struct {
	mu sync.Mutex
	h  http.Handler
}

func (l *lateHandler) set(h http.Handler) { l.mu.Lock(); l.h = h; l.mu.Unlock() }
func (l *lateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	h := l.h
	l.mu.Unlock()
	if h == nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	h.ServeHTTP(w, r)
}

type post struct {
	cfg *config.Config
	st  *store.Store
	eng *repl.Engine
	lh  *lateHandler
	srv *httptest.Server
}

func (p *post) serve() {
	mux := http.NewServeMux()
	mux.Handle("/sync/", p.eng.SyncHandler())
	mux.Handle("GET /debug/sync-status", p.eng.StatusHandler())
	p.lh.set(mux)
}

// mesh builds full-mesh posts with ids given; every post's key is "k-<id>".
func mesh(t *testing.T, ids ...string) map[string]*post {
	t.Helper()
	ps := map[string]*post{}
	for _, id := range ids {
		lh := &lateHandler{}
		ps[id] = &post{lh: lh, srv: httptest.NewServer(lh)}
		t.Cleanup(ps[id].srv.Close)
	}
	for _, id := range ids {
		cfg := &config.Config{SiteID: id, DisplayName: strings.ToUpper(id), ListenAddr: "127.0.0.1:0", NodeEpoch: 1,
			PeerKey: "k-" + id, SyncIntervalSeconds: 10, DBPath: filepath.Join(t.TempDir(), id+".db")}
		for _, o := range ids {
			if o != id {
				cfg.Peers = append(cfg.Peers, config.Peer{SiteID: o, DisplayName: strings.ToUpper(o), BaseURL: ps[o].srv.URL, Key: "k-" + o})
			}
		}
		ps[id].cfg = cfg
		ps[id].st = openDomainStore(t, cfg.DBPath)
		ps[id].eng = repl.New(cfg, ps[id].st)
		ps[id].serve()
	}
	return ps
}

func openDomainStore(t *testing.T, path string) *store.Store {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatal(err)
	}
	return st
}

// freeAddr is a loopback host:port nothing listens on.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a := l.Addr().String()
	l.Close()
	return a
}

// writeSiteFile renders cfg as a site.toml a post would start from.
func writeSiteFile(t *testing.T, dir string, cfg *config.Config, listen string) string {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "site_id = %s\ndisplay_name = %s\nlisten_addr = %s\ndb_path = %s\nnode_epoch = %d   # kept comment\npeer_key = %s\nlogin_password = \"pw-test\"\norientation = false\nsite_type = \"camp\"\nsync_interval_seconds = 10\n",
		tomlStr(cfg.SiteID), tomlStr(cfg.DisplayName), tomlStr(listen), tomlStr(cfg.SiteID+".db"), cfg.NodeEpoch, tomlStr(cfg.PeerKey))
	if len(cfg.Peers) > 1 {
		b.WriteString("destinations = []\n")
	}
	for _, p := range cfg.Peers {
		b.WriteString("\n" + RenderPeerBlock(p))
	}
	path := filepath.Join(dir, "site.toml")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatalf("test fixture does not load: %v", err)
	}
	return path
}

func company(t *testing.T, st *store.Store, site string, epoch int64, name string) {
	t.Helper()
	if _, err := domain.CreateCompanyRecordedBy(st, site, epoch, name, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
}

// TestFix16SwapPlanRaisesTheEpochAboveEveryPeerAnswer: X authored in
// epochs 1 and 2 and both peers hold them; a replacement laptop starting
// from X's file (node_epoch 2) must come up at 3, never reuse 2.
func TestFix16SwapPlanRaisesTheEpochAboveEveryPeerAnswer(t *testing.T) {
	ctx := context.Background()
	m := mesh(t, "x", "a", "b")
	company(t, m["x"].st, "x", 1, "Acme")
	company(t, m["x"].st, "x", 1, "Bolt")
	company(t, m["x"].st, "x", 2, "Crane")
	m["a"].eng.PullAll(ctx)
	m["b"].eng.PullAll(ctx)

	dir := t.TempDir()
	xc := *m["x"].cfg
	xc.NodeEpoch = 2
	path := writeSiteFile(t, dir, &xc, freeAddr(t))
	lines, err := SwapPlan(ctx, SwapOptions{CfgPath: path, Now: time.Now()})
	if err != nil {
		t.Fatalf("swap plan refused: %v\n%s", err, strings.Join(lines, "\n"))
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.NodeEpoch != 3 {
		t.Fatalf("node_epoch = %d, want 3 (above epoch 2 held by both peers)", c.NodeEpoch)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "node_epoch = 3   # kept comment") {
		t.Errorf("the edit did not keep the line's comment:\n%s", raw)
	}
	baks, _ := filepath.Glob(path + ".bak-*")
	if len(baks) != 1 {
		t.Errorf("want exactly one backup of the old file, got %v", baks)
	}
	out := strings.Join(lines, "\n")
	if !strings.Contains(out, "A") || !strings.Contains(out, "highest epoch 2") {
		t.Errorf("output does not report each peer's answer:\n%s", out)
	}
	for _, k := range []string{"k-x", "k-a", "k-b"} {
		if strings.Contains(out, k) {
			t.Errorf("output printed the key %s:\n%s", k, out)
		}
	}
}

func TestFix16SwapPlanRefusesWhileAnyoneAnswersAsThisPost(t *testing.T) {
	ctx := context.Background()
	m := mesh(t, "x", "a")
	dir := t.TempDir()
	// The old laptop (or this laptop's own service) still answers at the
	// post's address.
	running := strings.TrimPrefix(m["x"].srv.URL, "http://")
	path := writeSiteFile(t, dir, m["x"].cfg, running)
	before, _ := os.ReadFile(path)
	_, err := SwapPlan(ctx, SwapOptions{CfgPath: path, Now: time.Now()})
	if err == nil || !strings.Contains(err.Error(), "already answers") {
		t.Fatalf("want a refusal naming the running author, got %v", err)
	}
	// -old-url names a different address for the old laptop.
	path2 := writeSiteFile(t, t.TempDir(), m["x"].cfg, freeAddr(t))
	if _, err := SwapPlan(ctx, SwapOptions{CfgPath: path2, OldURL: m["x"].srv.URL, Now: time.Now()}); err == nil || !strings.Contains(err.Error(), "old laptop still answers") {
		t.Fatalf("want a refusal naming the old laptop, got %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("a refused swap plan changed the file")
	}
}

func TestFix16SwapPlanRefusesAnUnreachablePeerUnlessOverriddenAboveEveryAnswer(t *testing.T) {
	ctx := context.Background()
	m := mesh(t, "x", "a", "b")
	company(t, m["x"].st, "x", 1, "Acme")
	m["a"].eng.PullAll(ctx)
	m["b"].srv.Close() // b is gone

	path := writeSiteFile(t, t.TempDir(), m["x"].cfg, freeAddr(t))
	_, err := SwapPlan(ctx, SwapOptions{CfgPath: path, Now: time.Now()})
	if err == nil || !strings.Contains(err.Error(), "did not answer") || !strings.Contains(err.Error(), "B") {
		t.Fatalf("want a refusal naming B as unreachable, got %v", err)
	}
	if c, _ := config.Load(path); c.NodeEpoch != 1 {
		t.Fatalf("refused plan changed node_epoch to %d", c.NodeEpoch)
	}
	if _, err := SwapPlan(ctx, SwapOptions{CfgPath: path, UnreachableOK: true, ForceEpoch: 1, Now: time.Now()}); err == nil {
		t.Fatal("-epoch 1 is not above the answers received and must be refused")
	}
	if _, err := SwapPlan(ctx, SwapOptions{CfgPath: path, UnreachableOK: true, ForceEpoch: 7, Now: time.Now()}); err != nil {
		t.Fatalf("-epoch 7 -unreachable-ok refused: %v", err)
	}
	if c, _ := config.Load(path); c.NodeEpoch != 7 {
		t.Fatalf("node_epoch = %d, want the operator's 7", c.NodeEpoch)
	}
}

// TestFix16SwapStatusIsReadyOnlyAfterTheRealRejoin: a blank replacement for
// X, under its raised epoch, is NOT READY before Rejoin and READY after it
// has pulled its own history back from the peers.
func TestFix16SwapStatusIsReadyOnlyAfterTheRealRejoin(t *testing.T) {
	ctx := context.Background()
	m := mesh(t, "x", "a", "b")
	company(t, m["x"].st, "x", 1, "Acme")
	company(t, m["a"].st, "a", 1, "Bolt")
	for _, id := range []string{"a", "b"} {
		m[id].eng.PullAll(ctx)
	}
	// The old laptop dies; a blank one takes its place at epoch 2.
	nc := *m["x"].cfg
	nc.NodeEpoch = 2
	nc.DBPath = filepath.Join(t.TempDir(), "x-new.db")
	st := openDomainStore(t, nc.DBPath)
	eng := repl.New(&nc, st)
	m["x"].eng = eng
	m["x"].st = st
	m["x"].serve()

	snap := eng.Snapshot()
	if ready, why := Readiness(&nc, &snap); ready {
		t.Fatal("a blank laptop that has pulled nothing reads READY")
	} else if len(why) == 0 {
		t.Fatal("NOT READY with no reason given")
	}
	eng.Rejoin(ctx)
	snap = eng.Snapshot()
	ready, why := Readiness(&nc, &snap)
	if !ready {
		t.Fatalf("after the rejoin pulled everything back: NOT READY:\n%s", strings.Join(why, "\n"))
	}
	if w, _ := st.Watermark("x", 1); w != 1 {
		t.Fatalf("own epoch-1 history not recovered: watermark %d", w)
	}
}

func TestFix16ReadinessNamesEveryBlockerInWords(t *testing.T) {
	cfg := &config.Config{SiteID: "x", Peers: []config.Peer{{SiteID: "a", DisplayName: "Ice Pad"}, {SiteID: "b", DisplayName: "NPF"}}}
	zero := 1.0
	ok := func() *repl.Snapshot {
		return &repl.Snapshot{SiteID: "x",
			Peers: []repl.PeerSnapshot{{SiteID: "a", LastContactAgoSec: &zero}, {SiteID: "b", LastContactAgoSec: &zero}},
			Streams: []repl.StreamStatus{
				{StreamKey: repl.StreamKey{SiteID: "a", NodeEpoch: 1}, Watermark: 3, Head: 3, State: repl.StateOK},
				{StreamKey: repl.StreamKey{SiteID: "b", NodeEpoch: 1}, Watermark: 0, Head: 0, State: repl.StateOK},
			}}
	}
	if r, why := Readiness(cfg, ok()); !r {
		t.Fatalf("baseline must be READY: %v", why)
	}
	cases := []struct {
		name string
		mut  func(s *repl.Snapshot)
		want string
	}{
		{"self history pending", func(s *repl.Snapshot) { s.Peers[0].SelfHistoryPending = true }, "Ice Pad has not yet handed back"},
		{"left behind", func(s *repl.Snapshot) { s.Streams[0].State = repl.StateLeftBehind }, "MORE of this post's own records"},
		{"diverged", func(s *repl.Snapshot) { s.Streams[1].State = repl.StatePrefixDiverged }, "DIVERGED"},
		{"catching up", func(s *repl.Snapshot) { s.Streams[0].Watermark = 1 }, "still catching up on Ice Pad's records"},
		{"never answered", func(s *repl.Snapshot) { s.Peers[1].LastContactAgoSec = nil }, "NPF has not answered"},
		{"key refused", func(s *repl.Snapshot) { s.Peers[1].InboundKeyRefused = true }, "matching keys"},
		{"no stream", func(s *repl.Snapshot) { s.Streams = s.Streams[:1] }, "no records from NPF"},
		{"wrong post", func(s *repl.Snapshot) { s.SiteID = "y" }, `answers as "y"`},
		{"unreachable", func(s *repl.Snapshot) { s.Peers[0].LastError, s.Peers[0].ErrorKind = "dial", repl.ErrorKindUnreachable }, "Ice Pad: cannot be reached"},
	}
	for _, c := range cases {
		s := ok()
		c.mut(s)
		r, why := Readiness(cfg, s)
		if r {
			t.Errorf("%s: READY", c.name)
			continue
		}
		if !strings.Contains(strings.Join(why, "\n"), c.want) {
			t.Errorf("%s: blockers %q do not say %q", c.name, why, c.want)
		}
	}
}

// TestFix16RetireRefusesUntilClearAndFullyReplicated walks the retire flow
// end to end on real stores: someone on site refuses; checking them out
// leaves a record the peers have not pulled, which refuses; after the pull
// it passes, and every remaining post's own re-check and file edit pass.
func TestFix16RetireRefusesUntilClearAndFullyReplicated(t *testing.T) {
	ctx := context.Background()
	m := mesh(t, "x", "a", "b")
	xs := m["x"].st
	p, err := domain.CreatePerson(xs, "x", 1, "Diaz, Ana", nil, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	num := "TRK 1"
	v, err := domain.CreateVehicle(xs, "x", 1, &num, nil, 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := domain.GateCheckIn(xs, "x", 1, domain.GateCheckInInput{VehicleID: v.VehicleID, Driver: domain.PersonSlot{ExistingID: p.PersonID}}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	m["a"].eng.PullAll(ctx)
	m["b"].eng.PullAll(ctx)

	dir := t.TempDir()
	cfgPath := writeSiteFile(t, dir, m["x"].cfg, freeAddr(t))
	opts := RetireOptions{CfgPath: cfgPath, DBOverride: m["x"].cfg.DBPath, OutDir: filepath.Join(dir, "out")}
	_, err = Retire(ctx, opts)
	if err == nil || !strings.Contains(err.Error(), "1 person(s) are still on site at X") {
		t.Fatalf("want the on-site refusal, got %v", err)
	}
	if _, err := os.Stat(opts.OutDir); !os.IsNotExist(err) {
		t.Fatal("a refused retire wrote output")
	}
	open, _ := domain.OpenVisitsAt(xs, "x")
	if err := domain.CheckOutVisit(xs, "x", 1, open[0].VisitID, nil, 0, "default"); err != nil {
		t.Fatal(err)
	}
	_, err = Retire(ctx, opts)
	if err == nil || !strings.Contains(err.Error(), "A holds") {
		t.Fatalf("want a refusal because the peers lack the checkout, got %v", err)
	}
	m["a"].eng.PullAll(ctx)
	m["b"].eng.PullAll(ctx)
	lines, err := Retire(ctx, opts)
	if err != nil {
		t.Fatalf("clear and replicated, yet refused: %v", err)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "may retire") {
		t.Errorf("output: %v", lines)
	}
	frag, err := LoadLeave(filepath.Join(opts.OutDir, "leave-x.toml"))
	if err != nil {
		t.Fatal(err)
	}
	head, _ := xs.Watermark("x", 1)
	if len(frag.FinalHeads) != 1 || frag.FinalHeads[0].Head != head {
		t.Fatalf("final heads %+v, want epoch 1 head %d", frag.FinalHeads, head)
	}
	// At A: its own re-check passes, and a fragment claiming more than A
	// holds is refused.
	if probs := PeerRemoveCheck(m["a"].st, m["a"].cfg, frag); len(probs) > 0 {
		t.Fatalf("A's re-check refused: %v", probs)
	}
	more := *frag
	more.FinalHeads = []FinalHead{{Epoch: 1, Head: head + 5}}
	if probs := PeerRemoveCheck(m["a"].st, m["a"].cfg, &more); len(probs) == 0 || !strings.Contains(probs[0], "must catch up") {
		t.Fatalf("want a catch-up refusal, got %v", probs)
	}
	// And a running retiring post is refused before anything is read.
	running := writeSiteFile(t, t.TempDir(), m["x"].cfg, strings.TrimPrefix(m["x"].srv.URL, "http://"))
	if _, err := Retire(ctx, RetireOptions{CfgPath: running, DBOverride: m["x"].cfg.DBPath, OutDir: filepath.Join(t.TempDir(), "o")}); err == nil || !strings.Contains(err.Error(), "still answers") {
		t.Fatalf("want the still-running refusal, got %v", err)
	}
}

func TestFix16RetireRefusesAnOpenRunToThePost(t *testing.T) {
	m := mesh(t, "x", "a")
	as := m["a"].st
	p, _ := domain.CreatePerson(as, "a", 1, "Evans, Bo", nil, nil, 0, "default")
	num := "TRK 2"
	v, _ := domain.CreateVehicle(as, "a", 1, &num, nil, 0, "default")
	dest := "x"
	deadline := int64(60)
	if _, err := domain.Dispatch(as, "a", 1, domain.DispatchInput{VehicleID: v.VehicleID, DriverPersonID: p.PersonID, DestinationSite: &dest, DeadlineMinutes: &deadline, KnownDestinationSites: []string{"x"}}, 0, "default"); err != nil {
		t.Fatal(err)
	}
	w, err := OpenWorkAt(as, "x")
	if err != nil {
		t.Fatal(err)
	}
	if w.RunsTo != 1 {
		t.Fatalf("open runs to X = %d, want 1", w.RunsTo)
	}
	if s := strings.Join(w.Sentences("X"), "\n"); !strings.Contains(s, "run(s) to X are still open") {
		t.Fatalf("sentence: %s", s)
	}
}

// TestFix16SwapPlanFromBackupClearsTheBackupsEpochsAndRestoresAtTheServicePath:
// a backup holding epoch 3 (above anything the peers hold) sets the new
// epoch to 4, and the restore is handed the database path the SERVICE uses
// (beside site.toml), not this shell's working directory.
func TestFix16SwapPlanFromBackupClearsTheBackupsEpochsAndRestoresAtTheServicePath(t *testing.T) {
	ctx := context.Background()
	m := mesh(t, "x", "a")
	company(t, m["x"].st, "x", 1, "Acme")
	m["a"].eng.PullAll(ctx)
	bdir := t.TempDir()
	bst := openDomainStore(t, filepath.Join(bdir, "x-backup.db"))
	company(t, bst, "x", 3, "Late")
	bst.Close()

	dir := t.TempDir()
	path := writeSiteFile(t, dir, m["x"].cfg, freeAddr(t))
	var got *config.Config
	var gotPath string
	r := RestorerFunc(func(c *config.Config, p string, out io.Writer, _ time.Time) error {
		got, gotPath = c, p
		fmt.Fprintln(out, "restore: ok")
		return nil
	})
	lines, err := SwapPlan(ctx, SwapOptions{CfgPath: path, FromBackup: filepath.Join(bdir, "x-backup.db"), Restorer: r, Now: time.Now()})
	if err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(lines, "\n"))
	}
	if got == nil || got.NodeEpoch != 4 || got.DBPath != filepath.Join(dir, "x.db") || gotPath != filepath.Join(bdir, "x-backup.db") {
		t.Fatalf("restore got epoch %v path %q from %q", got, got.DBPath, gotPath)
	}
	if c, _ := config.Load(path); c.NodeEpoch != 4 {
		t.Fatalf("file node_epoch %d, want 4", c.NodeEpoch)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "the backup holds epochs [3]") {
		t.Errorf("output does not report the backup's epochs:\n%s", strings.Join(lines, "\n"))
	}
	// The stand-in restorer refuses rather than pretending.
	if _, err := SwapPlan(ctx, SwapOptions{CfgPath: path, FromBackup: filepath.Join(bdir, "x-backup.db"), Now: time.Now()}); err == nil {
		t.Fatal("no restorer wired, yet the restore reported success")
	}
}

// TestFix16SwapPlanOnABlankLaptopAfterARetirementSaysTheRecordsComeBack (Q2).
func TestFix16SwapPlanOnABlankLaptopAfterARetirementSaysTheRecordsComeBack(t *testing.T) {
	m := mesh(t, "x", "a")
	cfg := *m["x"].cfg
	cfg.FormerPeers = []config.FormerPeer{{SiteID: "ndb", DisplayName: "NDB"}}
	path := writeSiteFile(t, t.TempDir(), &cfg, freeAddr(t))
	raw, _ := os.ReadFile(path)
	os.WriteFile(path, append(raw, []byte("\n"+RenderFormerPeerBlock(cfg.FormerPeers[0]))...), 0o600)
	lines, err := SwapPlan(context.Background(), SwapOptions{CfgPath: path, Now: time.Now()})
	if err != nil {
		t.Fatalf("a blank rebuild after a retirement is no longer refused: %v", err)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "NDB (ndb) will be pulled back") {
		t.Errorf("output:\n%s", strings.Join(lines, "\n"))
	}
}
