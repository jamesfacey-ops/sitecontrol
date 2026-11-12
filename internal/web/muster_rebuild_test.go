// muster_rebuild_test.go — the D-16a regression: when a post's database is
// DESTROYED and the box re-imaged, does its on-site muster come back from
// its peer?
//
// WHY THIS TEST EXISTS AND MUST KEEP EXISTING. reports/JAMES-DECISIONS.md
// D-16a prices a schema-change rebuild at 2-4 operator-hours, because the
// documented recovery procedure has a human re-recording ~150 residents
// from a printed muster. That price is only correct if presence does NOT
// come back on its own. The events that open and close a visit REPLICATE
// (§8.1), so it might; a rebuilt post might equally replay only its peer's
// own stream and never its own prior-epoch history. Nobody knew which.
// This test is the answer, and it has to keep being the answer: if the
// manual re-entry step ever becomes load-bearing (or ever stops being
// necessary) that must break a build, not be discovered during an
// evacuation.
//
// THE TEARDOWN IS THE REAL ONE, not an invented one. deploy/wipe-replay-siteb.ps1
// is the §8.6 procedure: stop the binary, `rm -f site-b.db*` (the glob
// matters — -wal/-shm ride along), bump node_epoch in the TOML (§8.2: the
// re-image SEEDS the epoch, it is never inferred), start again. The binary's
// boot sequence then calls repl.Engine.Rejoin synchronously before serving.
// wipeAndReimage below performs exactly those steps against a real store,
// real config and a real engine.
//
// SCOPE — WHICH REBUILD THIS MEASURES, AND WHICH IT DOES NOT. Everything
// here is a SAME-SCHEMA-VERSION rebuild: the box is wiped and re-imaged
// with the SAME binary, so last season's envelopes are still ingestible.
// That is the only rebuild whose recovery can be measured, because the
// store refuses a foreign schema_version on BOTH sides — store.Open
// refuses to open a database whose meta.schema_version is not this
// binary's (store.go), and ApplyBatch refuses an envelope whose
// SchemaVersion is not this binary's before it is ever written (log.go,
// SchemaVersionError). So a rebuild that BUMPS the schema version cannot
// replay the prior season's log at all, and NOTHING in this file should be
// read as covering it: D-16a's schema-change case stays manual until the
// store grows a migration path, and a green run here is not evidence
// otherwise.
//
// WHAT IS ASSERTED. The RENDERED sheet at /muster on the rebuilt post: the
// on-site tally (data-tally-count="on-site"), the company/trade
// grouping (data-company-group / data-trade-group) and every person's
// name inside the on-site roster section. Row counts in a table would not
// catch a fold that repopulated `visit` but lost the person names it needs
// to render, and the sheet is the artefact an officer carries outside.
package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"sitecontrol/internal/config"
	"sitecontrol/internal/domain"
	"sitecontrol/internal/repl"
	"sitecontrol/internal/store"
)

// --- harness ----------------------------------------------------------

// latePeerHandler lets both httptest servers exist before either engine
// does — peer base_urls are config, and config precedes the engine that
// serves them. Same shape as internal/repl's own mesh builder; duplicated
// rather than imported because that one is unexported in another package.
type latePeerHandler struct {
	mu sync.Mutex
	h  http.Handler
}

func (l *latePeerHandler) set(h http.Handler) { l.mu.Lock(); l.h = h; l.mu.Unlock() }
func (l *latePeerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	h := l.h
	l.mu.Unlock()
	if h == nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	h.ServeHTTP(w, r)
}

// post is one running site: config, store, replication engine and the web
// server that renders its muster.
type post struct {
	cfg *config.Config
	st  *store.Store
	eng *repl.Engine
	web *Server
	// lh is the post's public listener. A re-image replaces the process
	// behind the SAME address, so the rebuilt engine is swapped in here —
	// without that, the peer keeps talking to the destroyed instance and
	// every "did it come back?" assertion passes vacuously because nothing
	// could reach it either way.
	lh *latePeerHandler
}

func (p *post) epoch() int64 { return p.cfg.NodeEpoch }

// openPost opens (or re-opens) a store at cfg.DBPath, registers the domain
// projections, and builds the engine + web server pair. Deliberately the
// same call sequence cmd/sitectl/main.go performs at boot, so a rebuild in
// this test exercises the same wiring a re-imaged box does.
func openPost(t *testing.T, cfg *config.Config, lh *latePeerHandler) *post {
	t.Helper()
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open %s: %v", cfg.DBPath, err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Register(st); err != nil {
		t.Fatalf("register domain projections: %v", err)
	}
	eng := repl.New(cfg, st)
	t.Cleanup(eng.Shutdown)
	w := New(cfg, st)
	w.Sync = eng.SyncHandler()
	lh.set(eng.SyncHandler())
	return &post{cfg: cfg, st: st, eng: eng, web: w, lh: lh}
}

// twoPosts wires Ice Road (site-a) and Pad Site Control (site-b) as mutual
// peers over real HTTP, mirroring deploy/site-a.toml + deploy/site-b.toml.
func twoPosts(t *testing.T) (*post, *post, string) {
	t.Helper()
	dir := t.TempDir()
	lhA, lhB := &latePeerHandler{}, &latePeerHandler{}
	srvA := httptest.NewServer(lhA)
	srvB := httptest.NewServer(lhB)
	t.Cleanup(srvA.Close)
	t.Cleanup(srvB.Close)

	cfgA := &config.Config{
		SiteID: "site-a", DisplayName: "Ice Road", ListenAddr: ":0", NodeEpoch: 1,
		PeerKey: "key-site-a", SyncIntervalSeconds: 3600,
		DBPath: filepath.Join(dir, "site-a.db"),
		Peers:  []config.Peer{{SiteID: "site-b", BaseURL: srvB.URL, Key: "key-site-b"}},
		Destinations: []config.Destination{
			{ID: "site-b", DisplayName: "Pad Site Control", Controlled: true},
		},
	}
	cfgB := &config.Config{
		SiteID: "site-b", DisplayName: "Pad Site Control", ListenAddr: ":0", NodeEpoch: 1,
		PeerKey: "key-site-b", SyncIntervalSeconds: 3600,
		DBPath: filepath.Join(dir, "site-b.db"),
		Peers:  []config.Peer{{SiteID: "site-a", BaseURL: srvA.URL, Key: "key-site-a"}},
		Destinations: []config.Destination{
			{ID: "site-a", DisplayName: "Ice Road", Controlled: true},
		},
	}
	a := openPost(t, cfgA, lhA)
	b := openPost(t, cfgB, lhB)
	return a, b, dir
}

// settle runs replication both ways until neither store changes — the
// local stand-in for "leave the link up for a sync interval".
func settle(t *testing.T, a, b *post) {
	t.Helper()
	for i := 0; i < 6; i++ {
		beforeA, beforeB := mustDumpOf(t, a.st), mustDumpOf(t, b.st)
		a.eng.PullAll(context.Background())
		b.eng.PullAll(context.Background())
		if string(mustDumpOf(t, a.st)) == string(beforeA) && string(mustDumpOf(t, b.st)) == string(beforeB) {
			// A fixed point reached because NOTHING COULD TALK is not
			// convergence, and it is exactly how this harness first lied:
			// the rebuilt post came up behind a listener the peer was no
			// longer wired to, so "the dumps stopped changing" meant "the
			// link is dead". Both engines must be healthy at the fixed point.
			requireLinkUp(t, a)
			requireLinkUp(t, b)
			return
		}
	}
	t.Fatal("replication never reached a fixed point in 6 rounds")
}

// requireLinkUp refuses a silent-link convergence — see settle.
func requireLinkUp(t *testing.T, p *post) {
	t.Helper()
	for _, ps := range p.eng.Snapshot().Peers {
		if ps.LastError != "" {
			t.Fatalf("%s cannot reach peer %s: %s — any convergence claim here is vacuous",
				p.cfg.SiteID, ps.SiteID, ps.LastError)
		}
	}
}

func mustDumpOf(t *testing.T, st *store.Store) []byte {
	t.Helper()
	b, err := st.DumpProjections()
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	return b
}

// wipeAndReimage IS deploy/wipe-replay-siteb.ps1, in-process: stop, delete
// the database and its -wal/-shm siblings, bump node_epoch (§8.2 re-image
// seeding, never inferred), start again, and run the §8.6 rejoin
// synchronously before any traffic is served. The returned post is a
// genuinely blank box wearing the same site_id.
func wipeAndReimage(t *testing.T, p *post) *post {
	t.Helper()
	return wipeAndReimageRecoveringWith(t, p, func(e *repl.Engine) { e.Rejoin(context.Background()) })
}

// wipeAndReimageRecoveringWith is wipeAndReimage with the BOOT RECOVERY STEP
// as a parameter. Production has exactly one caller of this seam
// (cmd/sitectl/main.go calls Rejoin), and wipeAndReimage above hard-codes
// it. The parameter exists for one reason: to let
// TestNothingOnThePadsRosterSurvivesAPeerLogOnlyReplay run the SAME wipe
// with the peer-log-only recovery a naive rebuild would perform, so the
// mechanism behind the answer is pinned by a test rather than only visible
// to whoever thinks to mutate one.
func wipeAndReimageRecoveringWith(t *testing.T, p *post, recover func(*repl.Engine)) *post {
	t.Helper()
	p.eng.Shutdown()
	if err := p.st.Close(); err != nil {
		t.Fatalf("stop sitectl (close store): %v", err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(p.cfg.DBPath + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatalf("rm -f %s%s: %v", p.cfg.DBPath, suffix, err)
		}
	}
	if _, err := os.Stat(p.cfg.DBPath); !os.IsNotExist(err) {
		t.Fatalf("precondition: %s still exists after the wipe (%v) — the teardown did not happen", p.cfg.DBPath, err)
	}
	reimaged := *p.cfg
	reimaged.NodeEpoch = p.cfg.NodeEpoch + 1 // the TOML edit the re-image performs
	reimaged.Peers = append([]config.Peer(nil), p.cfg.Peers...)
	reimaged.Destinations = append([]config.Destination(nil), p.cfg.Destinations...)

	fresh := openPost(t, &reimaged, p.lh) // same listener, new process behind it
	recover(fresh.eng)                    // boot-time, before serving traffic
	return fresh
}

// --- reading the rendered sheet ---------------------------------------

var (
	tallyRe   = regexp.MustCompile(`data-tally-count="([a-z-]+)"[^>]*>\s*([0-9]+)\s*<`)
	companyRe = regexp.MustCompile(`data-company-group="([^"]*)"`)
	// ⟦LOCK — §85, 2026-09-03⟧ LOCK-TIME instrument re-pointed; THE PROPERTY IS KEPT.
	// ⚠ THIS SELECTOR IS THE TEST'S EYE, NOT ITS SUBJECT. muster.html:315 renders
	// `data-trade-group`, and muster.html:55 names that attribute set deliberately — the
	// PAGE was renamed and this scanner was not, so it matched nothing and returned [].
	// ⟦was: "\twgRe      = regexp.MustCompile(`data-workgroup-group=\"([^\"]*)\"`)"⟧
	wgRe = regexp.MustCompile(`data-trade-group="([^"]*)"`)
	// ⟦SchemaVersion 15, R-23 — 2026-09-13⟧ INSTRUMENT RE-POINTED; THE PROPERTY
	// IS KEPT (the same shape as wgRe's §85 re-point above). A name recovered
	// from the peer's copy after a rebuild now carries an inline RESTORED tag
	// inside its <li> (muster.html; James: "Restore them, but mark them as
	// restored"), so the bare `<li>NAME</li>` eye went blind to exactly the
	// names these five tests exist to see recovered — the on-site tally still
	// read 5 while roster() read []. The tag is OPTIONAL here and the NAME is
	// still what is captured: recovery is asserted exactly as before. The
	// marking itself is asserted in muster_rebuild_restored_test.go beside
	// this file, per this file's tracking rule (app-fixes-v1 §5).
	// ⟦was: "nameLiRe = regexp.MustCompile(`<li>([^<]+)</li>`)"⟧
	nameLiRe = regexp.MustCompile(`<li>([^<]+?)(?:\s*<span class="restored-tag"[^>]*>RESTORED</span>)?</li>`)
)

// sheet is one rendered /muster page, with the inlined stylesheet stripped
// first (F19: a class name in the stylesheet is not a rendered element).
type sheet struct {
	body string
}

func renderMuster(t *testing.T, p *post) sheet {
	t.Helper()
	rec := httptest.NewRecorder()
	p.web.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/muster", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /muster on %s: status %d\n%s", p.cfg.SiteID, rec.Code, rec.Body.String())
	}
	return sheet{body: styleRe.ReplaceAllString(rec.Body.String(), "")}
}

func (s sheet) tally(t *testing.T, which string) int {
	t.Helper()
	for _, m := range tallyRe.FindAllStringSubmatch(s.body, -1) {
		if m[1] == which {
			var n int
			fmt.Sscanf(m[2], "%d", &n)
			return n
		}
	}
	t.Fatalf("the sheet carries no data-tally-count=%q at all:\n%s", which, s.body)
	return -1
}

// section returns the markup of one data-muster-section, so a word from the
// tally block or another section can never stand in for this one's content.
func (s sheet) section(t *testing.T, name string) string {
	t.Helper()
	at := strings.Index(s.body, `data-muster-section="`+name+`"`)
	if at < 0 {
		t.Fatalf("the sheet has no %q section at all:\n%s", name, s.body)
	}
	end := strings.Index(s.body[at:], "</section>")
	if end < 0 {
		t.Fatalf("the %q section is never closed", name)
	}
	return s.body[at : at+end]
}

// roster is the on-site section's names, companies and trades — what an
// officer actually reads aloud.
func (s sheet) roster(t *testing.T) (names, companies, trades []string) {
	t.Helper()
	sec := s.section(t, "on-site")
	for _, m := range nameLiRe.FindAllStringSubmatch(sec, -1) {
		names = append(names, strings.TrimSpace(m[1]))
	}
	for _, m := range companyRe.FindAllStringSubmatch(sec, -1) {
		companies = append(companies, m[1])
	}
	for _, m := range wgRe.FindAllStringSubmatch(sec, -1) {
		trades = append(trades, m[1])
	}
	return
}

func sortedEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	strSort(g)
	strSort(w)
	for i := range g {
		if g[i] != w[i] {
			return false
		}
	}
	return true
}

func strSort(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// --- the camp ---------------------------------------------------------

type camp struct {
	a, b *post
	// arrivedFromA: dispatched from Ice Road on TRK-100 and ARRIVED at the
	// Pad — James's case, "they just change from in transit to on site".
	arrivedFromA []domain.Person
	// localAtB: walked in at the Pad's own gate. Included deliberately —
	// these may recover differently from the trip-borne crew, and the
	// difference IS the finding.
	localAtB []domain.Person
	// stillRolling: dispatched from Ice Road on TRK-200 and NOT yet
	// arrived when the Pad is destroyed (D-17's in-transit population).
	stillRolling []domain.Person
	rollingTrip  domain.Trip
}

func person(t *testing.T, st *store.Store, site string, epoch int64, name, companyID, tradeID string) domain.Person {
	t.Helper()
	p, err := domain.CreatePerson(st, site, epoch, name, &companyID, &tradeID, 0, "default")
	if err != nil {
		t.Fatalf("create person %s: %v", name, err)
	}
	return p
}

func ids(ps []domain.Person) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.PersonID)
	}
	return out
}

func names(ps []domain.Person) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

// checkInAt puts everyone on site at siteID through the degenerate
// local-check-in trip plus its arrival — the same pair GateCheckIn authors.
func checkInAt(t *testing.T, p *post, vehicle string, crew []domain.Person) {
	t.Helper()
	all := ids(crew)
	trip, err := domain.DispatchLocalCheckIn(p.st, p.cfg.SiteID, p.epoch(), domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: vehicle, DriverPersonID: all[0], OccupantPersonIDs: all[1:],"⟧
		VehicleID: w7SeedVehicleID(t, p.st, p.cfg.SiteID, vehicle), DriverPersonID: all[0], OccupantPersonIDs: all[1:],
	}, 0, "default")
	if err != nil {
		t.Fatalf("check-in dispatch at %s: %v", p.cfg.SiteID, err)
	}
	if _, err := domain.RecordExpectedArrival(p.st, p.cfg.SiteID, p.epoch(), trip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("check-in arrival at %s: %v", p.cfg.SiteID, err)
	}
}

// dispatchTo authors one controlled run from p to dest. Everyone aboard is
// checked OUT of p in the same fold — they are on the road, on nobody's
// on-site list.
func dispatchTo(t *testing.T, p *post, dest, vehicle string, crew []domain.Person) domain.Trip {
	t.Helper()
	mins := int64(90)
	all := ids(crew)
	trip, err := domain.Dispatch(p.st, p.cfg.SiteID, p.epoch(), domain.DispatchInput{
		// ⟦LOCK — §47 re-seed, 2026-09-03⟧ ⟦was: "VehicleEntered: vehicle, DriverPersonID: all[0], OccupantPersonIDs: all[1:],"⟧
		VehicleID: w7SeedVehicleID(t, p.st, p.cfg.SiteID, vehicle), DriverPersonID: all[0], OccupantPersonIDs: all[1:],
		DestinationSite: &dest, DeadlineMinutes: &mins,
	}, 0, "default")
	if err != nil {
		t.Fatalf("dispatch %s from %s: %v", vehicle, p.cfg.SiteID, err)
	}
	return trip
}

// newCamp builds the populated two-post camp described in camp's fields.
// Reference data is authored at BOTH posts on purpose: companies/persons
// minted at Ice Road and companies/persons minted at the Pad recover by
// different routes (the peer's own log vs. this post's own prior-epoch
// history), and conflating them would hide exactly the partial answer this
// test exists to report.
func newCamp(t *testing.T) *camp {
	a, b, _ := twoPosts(t)

	// Reference data authored at Ice Road.
	drilling, err := domain.CreateCompany(a.st, "site-a", 1, "Northslope Drilling", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "rigCrew, err := domain.CreateTrade(a.st, "site-a", 1, drilling.CompanyID, "Rig Crew", 0, "default")"⟧
	rigCrew, err := domain.CreateTrade(a.st, "site-a", 1, "Rig Crew", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// Reference data authored at the Pad itself.
	catering, err := domain.CreateCompany(b.st, "site-b", 1, "Camp Services", 0, "default")
	if err != nil {
		t.Fatal(err)
	}
	// ⟦LOCK — W8 alias-deletion repair, 2026-09-03⟧ DR-W8-2 arity: the company argument is dropped — a trade is a site-wide open set, not scoped to a company. ⟦was: "kitchen, err := domain.CreateTrade(b.st, "site-b", 1, catering.CompanyID, "Kitchen", 0, "default")"⟧
	kitchen, err := domain.CreateTrade(b.st, "site-b", 1, "Kitchen", 0, "default")
	if err != nil {
		t.Fatal(err)
	}

	c := &camp{a: a, b: b}
	for _, n := range []string{"Nadeau, Pierre", "Ohara, Wendy", "Petrov, Sasha"} {
		c.arrivedFromA = append(c.arrivedFromA, person(t, a.st, "site-a", 1, n, drilling.CompanyID, rigCrew.TradeID))
	}
	for _, n := range []string{"Quinn, Aidan", "Rasmussen, Iver"} {
		c.stillRolling = append(c.stillRolling, person(t, a.st, "site-a", 1, n, drilling.CompanyID, rigCrew.TradeID))
	}
	for _, n := range []string{"Silva, Marcos", "Tran, Linh"} {
		c.localAtB = append(c.localAtB, person(t, b.st, "site-b", 1, n, catering.CompanyID, kitchen.TradeID))
	}

	// The Pad's own gate: two people walk in at the Pad and never travel.
	checkInAt(t, b, "gate walk-in", c.localAtB)

	// Ice Road checks in everyone who is about to travel, then sends the
	// first truck to the Pad. Dispatch closes their Ice Road visits.
	checkInAt(t, a, "TRK-14", append(append([]domain.Person{}, c.arrivedFromA...), c.stillRolling...))
	rolled := dispatchTo(t, a, "site-b", "TRK-100", c.arrivedFromA)

	// The Pad has to KNOW about the trip before it can record its arrival —
	// that is the replicated event this whole question turns on.
	b.eng.PullAll(context.Background())
	if _, err := domain.RecordExpectedArrival(b.st, "site-b", 1, rolled.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("arrival of TRK-100 at the Pad: %v", err)
	}

	// The second truck leaves and is still on the road when the Pad dies.
	c.rollingTrip = dispatchTo(t, a, "site-b", "TRK-200", c.stillRolling)

	settle(t, a, b)
	return c
}

// --- THE REGRESSION ---------------------------------------------------

// TestPadMusterRecoversFromItsPeerAfterTheDatabaseIsDestroyedAndRebuilt is
// the D-16a question in one place: destroy the Pad's database, re-image it
// per §8.6, and read the sheet it renders afterwards.
//
// MEASURED ANSWER, asserted below rather than assumed: presence recovers in
// full. Both the crew that arrived on a trip dispatched from Ice Road and
// the crew that walked in at the Pad's own gate are back on the rendered
// roster with the same total, because the arrival events that opened their
// visits were authored on the PAD's own stream, Ice Road holds a replicated
// copy of that stream, and Rejoin (§8.6) pulls this post's own prior-epoch
// history back FIRST before any peer log.
//
// WHAT THAT MEANS FOR D-16a: the 2-4 operator-hour re-entry of ~150
// residents from a printed muster is NOT required for presence, as long as
// a peer is reachable and holds the wiped post's stream. If this test ever
// goes red, that manual step became load-bearing again and D-16a's price is
// real — which is the one thing nobody should discover during an evacuation.
func TestPadMusterRecoversFromItsPeerAfterTheDatabaseIsDestroyedAndRebuilt(t *testing.T) {
	c := newCamp(t)

	onSite := append(append([]string{}, names(c.arrivedFromA)...), names(c.localAtB)...)

	// --- before the wipe: the two posts agree, and the Pad's sheet reads 5 ---
	if got, want := string(mustDumpOf(t, c.b.st)), string(mustDumpOf(t, c.a.st)); got != want {
		t.Fatalf("precondition: the two posts do not agree before the wipe")
	}
	before := renderMuster(t, c.b)
	if got := before.tally(t, "on-site"); got != len(onSite) {
		t.Fatalf("precondition: the Pad's sheet reads %d on site, want %d", got, len(onSite))
	}
	gotNames, gotCos, gotWgs := before.roster(t)
	if !sortedEqual(gotNames, onSite) {
		t.Fatalf("precondition: the Pad's roster names %v, want %v", gotNames, onSite)
	}
	if !sortedEqual(gotCos, []string{"Camp Services", "Northslope Drilling"}) {
		t.Fatalf("precondition: the Pad's roster groups companies %v", gotCos)
	}
	if !sortedEqual(gotWgs, []string{"Kitchen", "Rig Crew"}) {
		t.Fatalf("precondition: the Pad's roster groups trades %v", gotWgs)
	}
	// Ice Road owns the people it put on the road (D-6), and the Pad shows
	// none of them on its own sheet — before OR after the rebuild.
	if got := renderMuster(t, c.a).tally(t, "in-transit"); got != len(c.stillRolling) {
		t.Fatalf("precondition: Ice Road reads %d in transit, want %d", got, len(c.stillRolling))
	}

	// --- the rebuild: deploy/wipe-replay-siteb.ps1, in-process ---
	c.b = wipeAndReimage(t, c.b)
	if c.b.epoch() != 2 {
		t.Fatalf("the re-image did not seed a new node_epoch: epoch=%d", c.b.epoch())
	}

	// --- the answer ---
	after := renderMuster(t, c.b)
	if got := after.tally(t, "on-site"); got != len(onSite) {
		t.Fatalf("PRESENCE DID NOT RECOVER: the rebuilt Pad's sheet reads %d on site, want %d.\n"+
			"If this is the new truth, D-16a's manual re-entry of the printed muster is load-bearing "+
			"and this test must be rewritten to assert the loss, not deleted.", got, len(onSite))
	}
	gotNames, gotCos, gotWgs = after.roster(t)
	if !sortedEqual(gotNames, onSite) {
		t.Fatalf("PRESENCE PARTIALLY RECOVERED: the rebuilt Pad's roster names %v, want %v.\n"+
			"Trip-borne arrivals were %v; gate walk-ins at the Pad were %v — which set is missing is the finding.",
			gotNames, onSite, names(c.arrivedFromA), names(c.localAtB))
	}
	if !sortedEqual(gotCos, []string{"Camp Services", "Northslope Drilling"}) {
		t.Fatalf("the rebuilt Pad's roster groups companies %v — presence came back but its grouping did not", gotCos)
	}
	if !sortedEqual(gotWgs, []string{"Kitchen", "Rig Crew"}) {
		t.Fatalf("the rebuilt Pad's roster groups trades %v — presence came back but its grouping did not", gotWgs)
	}

	// The rebuilt post agrees with its peer again, not merely "looks right".
	settle(t, c.a, c.b)
	if got, want := string(mustDumpOf(t, c.b.st)), string(mustDumpOf(t, c.a.st)); got != want {
		t.Fatal("the rebuilt Pad and Ice Road do not agree after the rebuild")
	}
}

// TestRebuiltPadRecoversReferenceDataFromBothItsOwnStreamAndItsPeers is the
// separate question: reference data (companies, trades, people) is not
// presence and could have a different answer, because half of it was
// authored at the peer and half at the post that was destroyed. Both halves
// are asserted by ORIGIN, so a partial recovery names which half was lost
// instead of reading as one undifferentiated count.
func TestRebuiltPadRecoversReferenceDataFromBothItsOwnStreamAndItsPeers(t *testing.T) {
	c := newCamp(t)
	c.b = wipeAndReimage(t, c.b)

	persons, err := domain.Persons(c.b.st)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range persons {
		got = append(got, p.Name)
	}
	authoredAtIceRoad := append(append([]string{}, names(c.arrivedFromA)...), names(c.stillRolling)...)
	authoredAtThePad := names(c.localAtB)
	want := append(append([]string{}, authoredAtIceRoad...), authoredAtThePad...)
	if !sortedEqual(got, want) {
		t.Fatalf("reference data did not fully recover: the rebuilt Pad knows %v, want %v.\n"+
			"Authored at Ice Road (recovered via the peer's own log): %v\n"+
			"Authored at the Pad (recovered only via §8.6 self-history): %v",
			got, want, authoredAtIceRoad, authoredAtThePad)
	}

	companies, err := domain.Companies(c.b.st)
	if err != nil {
		t.Fatal(err)
	}
	var cos []string
	for _, x := range companies {
		cos = append(cos, x.Name)
	}
	if !sortedEqual(cos, []string{"Camp Services", "Northslope Drilling"}) {
		t.Fatalf("companies after the rebuild: %v — 'Northslope Drilling' was authored at Ice Road, "+
			"'Camp Services' at the Pad itself; a missing one names which recovery route failed", cos)
	}
	trades, err := domain.Trades(c.b.st)
	if err != nil {
		t.Fatal(err)
	}
	var wgs []string
	for _, x := range trades {
		wgs = append(wgs, x.Name)
	}
	if !sortedEqual(wgs, []string{"Kitchen", "Rig Crew"}) {
		t.Fatalf("trades after the rebuild: %v", wgs)
	}
}

// TestACrewOnTheRoadWhenThePadIsDestroyedStillArrivesOntoItsRebuiltMuster is
// the most fragile population (D-17): people dispatched from Ice Road who
// have NOT arrived at the moment the Pad's database is destroyed. They hold
// no visit row anywhere by definition, so nothing about them can be
// recovered from a visit fold — the only thing that can save them is the
// replicated TRIP, and the rebuilt Pad needs it to accept their arrival at
// all.
//
// Note what is deliberately NOT asserted: that the rebuilt Pad shows them
// in transit on its own sheet. Under D-6 the ORIGIN owns an in-transit
// crew, so they are on Ice Road's sheet and on nobody else's, rebuild or
// no rebuild — asserting otherwise here would be asserting a bug.
func TestACrewOnTheRoadWhenThePadIsDestroyedStillArrivesOntoItsRebuiltMuster(t *testing.T) {
	c := newCamp(t)
	settled := len(c.arrivedFromA) + len(c.localAtB)

	c.b = wipeAndReimage(t, c.b)

	// The rebuilt Pad does not claim them: they are not here.
	after := renderMuster(t, c.b)
	if got := after.tally(t, "in-transit"); got != 0 {
		t.Fatalf("the rebuilt Pad claims %d people in transit; under D-6 the ORIGIN owns them", got)
	}
	if got := after.tally(t, "on-site"); got != settled {
		t.Fatalf("the rebuilt Pad reads %d on site, want %d — a crew still on the road must not be counted as arrived", got, settled)
	}
	// Ice Road, never wiped, still names them.
	if got := renderMuster(t, c.a).tally(t, "in-transit"); got != len(c.stillRolling) {
		t.Fatalf("Ice Road reads %d in transit after its peer was rebuilt, want %d", got, len(c.stillRolling))
	}

	// The truck now rolls in. The rebuilt Pad must still hold the trip —
	// recovered from its peer's log — to be able to record the arrival, and
	// it authors that arrival at its NEW epoch.
	if _, err := domain.RecordExpectedArrival(c.b.st, "site-b", c.b.epoch(), c.rollingTrip.TripID, nil, 0, "default"); err != nil {
		t.Fatalf("the rebuilt Pad could not record the arrival of a truck that was on the road when it died: %v\n"+
			"That means the crew on TRK-200 cannot be checked in through the ordinary inbound path at all.", err)
	}
	final := renderMuster(t, c.b)
	if got := final.tally(t, "on-site"); got != settled+len(c.stillRolling) {
		t.Fatalf("after TRK-200 arrives the rebuilt Pad reads %d on site, want %d", got, settled+len(c.stillRolling))
	}
	gotNames, _, _ := final.roster(t)
	wantNames := append(append(append([]string{}, names(c.arrivedFromA)...), names(c.localAtB)...), names(c.stillRolling)...)
	if !sortedEqual(gotNames, wantNames) {
		t.Fatalf("after TRK-200 arrives the rebuilt Pad's roster names %v, want %v", gotNames, wantNames)
	}

	// And Ice Road learns they got there: the in-transit section empties.
	settle(t, c.a, c.b)
	if got := renderMuster(t, c.a).tally(t, "in-transit"); got != 0 {
		t.Fatalf("Ice Road still reads %d in transit after the rebuilt Pad recorded their arrival", got)
	}
}

// TestNothingOnThePadsRosterSurvivesAPeerLogOnlyReplay pins the MECHANISM
// behind the answer above, so it cannot quietly stop being true.
//
// The alternative hypothesis was that a rebuilt post replays only its
// peer's own stream. Measured here: under that recovery the Pad's sheet
// reads ZERO on site — not "the walk-ins are missing but the trip crew is
// fine". EVERY on-site person at the Pad is lost, including the crew Ice
// Road dispatched, because a visit is opened by the ARRIVAL event and the
// arrival is authored by the post the truck arrives AT (§8.1). The trip
// replicates from Ice Road; the arrival does not exist anywhere except on
// the Pad's own stream and the peers' replicated copies of it.
//
// So the whole recovery rests on ONE thing: repl.Engine.Rejoin's §8.6
// self-history step, which pulls this post's own prior-epoch log back from
// a peer before touching any peer's own log. Reference data splits the same
// way and this test asserts that split explicitly: people minted at Ice
// Road come back through the ordinary peer log, people minted at the Pad do
// not come back at all without self-history.
//
// If this test ever goes GREEN in the sense of "some presence survived a
// peer-log-only replay", the fold moved and the sibling test above is
// measuring something else than it says.
func TestNothingOnThePadsRosterSurvivesAPeerLogOnlyReplay(t *testing.T) {
	c := newCamp(t)
	c.b = wipeAndReimageRecoveringWith(t, c.b, func(e *repl.Engine) {
		e.PullAll(context.Background()) // peer logs only: no §8.6 self-history
	})

	after := renderMuster(t, c.b)
	if got := after.tally(t, "on-site"); got != 0 {
		t.Fatalf("a peer-log-only replay left %d people on the Pad's roster, want 0.\n"+
			"That would mean presence no longer depends solely on §8.6 self-history recovery — "+
			"re-derive which populations survive before trusting the sibling rebuild test.", got)
	}

	// Reference data: the peer-authored half comes back, the self-authored
	// half does not. Naming both halves is the point — "reference data
	// recovers" and "reference data does not" are both wrong answers.
	persons, err := domain.Persons(c.b.st)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range persons {
		got = append(got, p.Name)
	}
	fromIceRoad := append(append([]string{}, names(c.arrivedFromA)...), names(c.stillRolling)...)
	if !sortedEqual(got, fromIceRoad) {
		t.Fatalf("after a peer-log-only replay the Pad knows %v; want exactly the Ice-Road-authored people %v "+
			"and none of the Pad-authored ones %v", got, fromIceRoad, names(c.localAtB))
	}
}

// TestAReImageThatForgetsTheEpochBumpRecoversNobody is the operational
// hazard sitting next to the good news above, asserted so it cannot be
// forgotten: the recovery is CONDITIONAL on the re-image seeding a HIGHER
// node_epoch (§8.2 — seeded in the TOML, never inferred), which is step 2
// of deploy/wipe-replay-siteb.ps1.
//
// MEASURED: a rebuild that comes back on the SAME epoch recovers ZERO
// people. Its own prior-epoch history is refused on ingest with
// identity_rejected (§8.4 — the self-history exception will not accept
// records at or above the node's own current epoch; see repl's
// TestSelfHistoryIngestRejectsRecordsAtOrAboveOurOwnCurrentEpoch for the
// mechanism), so the sheet renders empty. This test does not re-prove that
// mechanism — it pins its CONSEQUENCE ON THE MUSTER, which is the thing an
// operator is looking at: skip one sed line in the re-image and the camp
// silently comes back with nobody on it, while replication reports itself
// as running.
func TestAReImageThatForgetsTheEpochBumpRecoversNobody(t *testing.T) {
	c := newCamp(t)
	sameEpoch := *c.b.cfg // the TOML edit that did NOT happen

	c.b.eng.Shutdown()
	if err := c.b.st.Close(); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(c.b.cfg.DBPath + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	botched := openPost(t, &sameEpoch, c.b.lh)
	botched.eng.Rejoin(context.Background())

	if got := renderMuster(t, botched).tally(t, "on-site"); got != 0 {
		t.Fatalf("a same-epoch rebuild recovered %d people onto the roster, want 0.\n"+
			"If self-history ingest now accepts same-epoch records, the epoch bump stopped being "+
			"load-bearing and deploy/wipe-replay-siteb.ps1's step 2 needs re-reading, not this test deleting.", got)
	}
	// And it says so rather than looking healthy: the refused stream is
	// surfaced, which is the only thing standing between this and a silent
	// empty camp.
	var rejected bool
	for _, ss := range botched.eng.Snapshot().Streams {
		if ss.SiteID == "site-b" && ss.State != repl.StateOK {
			rejected = true
		}
	}
	if !rejected {
		t.Fatal("a same-epoch rebuild recovered nobody AND reported every stream healthy — " +
			"an empty camp with a green sync status is the worst possible combination")
	}
}
