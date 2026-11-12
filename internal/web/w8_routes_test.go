// w8_routes_test.go — W8-20(a-i): GATING, asserted with teeth over a DERIVED
// route set.
//
// Written from the LOCKED W8 criteria only. No built implementation was read.
//
// ⟦rev 2 — D-63; DR-W8-20. ⚠ Revision 1 of the draft asserted only the
// negative bound and said in terms that it "asserts nothing about the naming
// screen's gating". That left W8 with NO CHECK THAT WOULD FAIL IF AN
// AUTHORING ROUTE SHIPPED REACHABLE BY ANYONE — which is precisely the gap
// [[D-63]] found in W7's draft and called "the failure [[D-51]] legislated
// against." A SCOPE NOTE IS NOT A CRITERION.⟧
//
// ⚠ CITATION DISCIPLINE, kept: [[D-51]] is cited here for BINDING CONSEQUENCE
// 1 ONLY — a rule about HOW A GATING CRITERION MUST BE WRITTEN — exactly as
// D-63 cites it. NO W8 criterion cites D-46, D-51 or D-63 as authority for
// WHERE trade's screens sit, and nothing below depends on that.
//
// ⚠ THIS DOES NOT DECIDE THE PLACEMENT QUESTION. §10(a) Q2 asks whether the
// naming-and-numbers screen should be NARROWER than the ordinary officer
// login; A "NARROWER" RULING ADDS A GATE AND CANNOT REMOVE THIS ONE, so the
// criterion is correct under either answer and TIGHTENS, NEVER LOOSENS, when
// the answer arrives.
package web

import (
	"go/scanner"
	"go/token"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// w8Route is one discovered registration literal.
type w8Route struct {
	Method string
	Path   string
	File   string
	Line   int
	// Gated records whether the registration's handler expression mentions
	// the requireLogin wrapper — read from the SAME registration statement,
	// so a wrapper applied to a different route cannot vouch for this one.
	Gated bool
}

func (r w8Route) String() string { return r.Method + " " + r.Path }

// w8RegistrationsInTree is W8-20(a-i)'s DERIVATION, with THREE CORRECTIONS
// measured against the landed W2 walk ⟦LOCK — B-5 / FIL-F4 with FAL-F7
// merged, severity raised to BLOCKER⟧:
//
//	(1) "GET /` IS INCLUDED AS WELL AS "POST /`. [MEASURED —
//	    w2PostRegistrationsInDir admits ONLY literals with prefix "POST /`.]
//	(2) THE WALK IS RECURSIVE. [MEASURED — the same function globs
//	    filepath.Join(dir, "*.go"), NON-RECURSIVELY.]
//	(3) the probe set is the SET DIFFERENCE between this enumeration over the
//	    post-W8 tree and the same enumeration at f33c4c7 — so it is "the
//	    routes W8 introduces" AS A COMPUTED VALUE, NEVER A LIST SOMEBODY TYPED.
//
// ⚠⚠ WHY THIS IS THE BLOCKER AND NOT A TIDY-UP. A `GET /trade/new` registered
// without requireLogin — THE NAMING-AND-NUMBERS SCREEN ITSELF, RENDERING THE
// SITE'S TRADE LIST AND THE THREE POSITIONAL HANDSET NUMBERS — is invisible
// to W2's walk (never discovered, so reverse reconciliation never fires) AND
// invisible to this limb unless somebody listed it by hand. (m8-62) would then
// be applied to a listed route, red, and THE LEDGER WOULD BOOK A KILL WHILE
// THE SCREEN SHIPS OPEN.
//
// ⚠ THE RESIDUAL, STATED: a route registered by a NON-LITERAL expression is
// not discovered by any prefix scan. W8 asserts its own registrations are
// LITERALS, so the residual is bounded; a build that registers a W8 route by
// a computed string has left the instrument's reach and THAT IS A FINDING,
// NEVER A PASS.
func w8RegistrationsInTree(t *testing.T, root string) []w8Route {
	t.Helper()
	var out []w8Route
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out = append(out, w8RegistrationsInSource(t, path, src)...)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// w8RegistrationsInSource token-scans one file for `"GET /…"` and `"POST /…"`
// literals, and records whether `requireLogin` appears on the SAME LINE — the
// registration statement's own line, so a wrapper elsewhere cannot vouch for
// this route.
func w8RegistrationsInSource(t *testing.T, path string, src []byte) []w8Route {
	t.Helper()
	lines := strings.Split(string(src), "\n")
	fset := token.NewFileSet()
	file := fset.AddFile(path, fset.Base(), len(src))
	var sc scanner.Scanner
	sc.Init(file, src, nil, 0) // comments skipped: a comment narrating a route is not a registration
	var out []w8Route
	for {
		pos, tok, lit := sc.Scan()
		if tok == token.EOF {
			break
		}
		if tok != token.STRING {
			continue
		}
		var method string
		switch {
		case strings.HasPrefix(lit, `"GET /`):
			method = "GET"
		case strings.HasPrefix(lit, `"POST /`):
			method = "POST"
		default:
			continue
		}
		p := fset.Position(pos)
		route := strings.TrimSuffix(strings.TrimPrefix(lit, `"`+method+` `), `"`)
		gated := false
		if p.Line-1 >= 0 && p.Line-1 < len(lines) {
			gated = strings.Contains(lines[p.Line-1], "requireLogin")
		}
		out = append(out, w8Route{Method: method, Path: route, File: path, Line: p.Line, Gated: gated})
	}
	return out
}

// w8BaselineRoutes enumerates the SAME registration set at f33c4c7, from git,
// so the probe set below is a COMPUTED SET DIFFERENCE and not a list.
func w8BaselineRoutes(t *testing.T, root, rev string) map[string]bool {
	t.Helper()
	names := exec.Command("git", "ls-tree", "-r", "--name-only", rev, "--", "internal", "cmd")
	names.Dir = root
	out, err := names.Output()
	if err != nil {
		t.Fatalf("PRECONDITION (instrument): `git ls-tree %s` failed in %s: %v — W8-20(a-i)'s probe set is the SET "+
			"DIFFERENCE against the pre-chunk tree, and without the baseline it would degenerate into a "+
			"hand-kept list, which is exactly what B-5 raised to BLOCKER", rev, root, err)
	}
	set := map[string]bool{}
	var files int
	for _, name := range strings.Split(string(out), "\n") {
		name = strings.TrimSpace(name)
		if name == "" || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		show := exec.Command("git", "show", rev+":"+name)
		show.Dir = root
		src, err := show.Output()
		if err != nil {
			continue
		}
		files++
		for _, r := range w8RegistrationsInSource(t, name, src) {
			set[r.String()] = true
		}
	}
	if files == 0 || len(set) == 0 {
		t.Fatalf("PRECONDITION (instrument): the baseline enumeration at %s read %d file(s) and found %d route(s). "+
			"An empty baseline makes EVERY route look like one W8 introduced, which is a different test from the "+
			"one this claims to be", rev, files, len(set))
	}
	return set
}

// TestW8DerivedRouteSetIsGated is W8-20(a-i).
//
// "EVERY ROUTE W8 REGISTERS REFUSES AN UNAUTHENTICATED REQUEST AND ACCEPTS AN
// AUTHENTICATED ONE" — W5d-23(a)'s landed form, re-pointed at trade, over a
// route set that is DERIVED and not hand-kept.
//
// ⚠ PRE-CHUNK the set difference is EMPTY BY CONSTRUCTION, and that is
// reported as a `PRECONDITION:` rather than as a pass: an empty probe set
// makes every assertion below vacuous, and "an un-controlled green is not
// coverage".
func TestW8DerivedRouteSetIsGated(t *testing.T) {
	root := w8WebModuleRoot(t)
	const baselineRev = "f33c4c7"

	current := w8RegistrationsInTree(t, root)
	if len(current) == 0 {
		t.Fatal("PRECONDITION (instrument): the recursive GET+POST walk found NO registrations — it would report a clean pass over nothing")
	}
	// The walk's own liveness: it must have reached BOTH verbs and MORE THAN
	// ONE DIRECTORY. ⚠ Correction (2) is that the landed walk is
	// NON-RECURSIVE; a recursive walk that never leaves one directory has
	// silently reproduced it.
	verbs := map[string]bool{}
	dirs := map[string]bool{}
	for _, r := range current {
		verbs[r.Method] = true
		dirs[filepath.Dir(r.File)] = true
	}
	if !verbs["GET"] || !verbs["POST"] {
		t.Fatalf("PRECONDITION (instrument): the walk found verbs %v — correction (1) is that \"GET /` is included "+
			"as well as \"POST /`, and a walk that finds only one verb has not made it", verbs)
	}
	if len(dirs) < 2 {
		t.Fatalf("PRECONDITION (instrument): the walk found registrations in %d directory(ies) — correction (2) is "+
			"that the walk is RECURSIVE, and one that never leaves a directory has silently reproduced the landed "+
			"non-recursive glob", len(dirs))
	}

	baseline := w8BaselineRoutes(t, root, baselineRev)
	var diff []w8Route
	for _, r := range current {
		if !baseline[r.String()] {
			diff = append(diff, r)
		}
	}

	// ⚠⚠ THE SET DIFFERENCE IS CONTAMINATED, AND THE CONTAMINATION IS
	// REPORTED RATHER THAN ABSORBED. The criterion computes the probe set as
	// (post-W8 tree) minus (f33c4c7), and at the moment the criteria were
	// drafted f33c4c7 WAS the pre-chunk tree. It no longer is: this tree
	// carries W7's, W9a's, W9b's and W9c's in-flight builds, so the raw
	// difference contains THEIR routes as well as W8's.
	//
	// W8's own routes are separated by ENTITY NAME, and the scope is stated
	// rather than implied: a route whose path names `trade`. ⚠ THE RESIDUAL
	// IS NAMED — a W8 route that does not carry the entity in its path is
	// outside this partition and would be counted as another window's. That
	// is a FINDING for the gate, not a pass, and it is why the other half is
	// printed below instead of dropped.
	var introduced, otherWindows []w8Route
	for _, r := range diff {
		if strings.Contains(strings.ToLower(r.Path), "trade") {
			introduced = append(introduced, r)
			continue
		}
		otherWindows = append(otherWindows, r)
	}
	if len(otherWindows) != 0 {
		var names []string
		for _, r := range otherWindows {
			names = append(names, r.String())
		}
		t.Logf("W8-20(a-i) ROUTED, NOT ABSORBED: the raw set difference against %s carries %d route(s) that are "+
			"NOT W8's — %v. They are other windows' in-flight builds (W7/W9a/W9b/W9c), and this test makes no "+
			"assertion about them. ⚠ Recorded so the contamination of the baseline is visible: f33c4c7 was the "+
			"pre-chunk tree when the criteria were drafted and is no longer",
			baselineRev, len(otherWindows), names)
	}

	if len(introduced) == 0 {
		t.Fatalf("PRECONDITION / W8-20(a-i): the SET DIFFERENCE between this tree's %d registrations and the %d at "+
			"%s contains NO route naming `trade`, so W8 has registered no route yet and every gating assertion "+
			"below would be VACUOUS.\n"+
			"⚠ Reported as a PRECONDITION rather than as a pass: an un-controlled green is not coverage, and a "+
			"check that cannot fail can be ACTIVE COVER.\n"+
			"⚠ When W8's routes land, the assertion is W5d-23(a)'s: each REFUSES an unauthenticated request and "+
			"ACCEPTS an authenticated one. THE BLOCKER CASE IS A `GET /trade/new` WITHOUT requireLogin — the "+
			"naming-and-numbers screen itself, rendering the site's trade list and the three positional handset "+
			"numbers. It is invisible to W2's POST-only walk AND invisible to this limb unless the set is DERIVED.\n"+
			"⚠ The raw difference held %d route(s), all of them other windows' — see the ROUTED note above",
			len(current), len(baseline), baselineRev, len(diff))
	}

	// When they land: every introduced route is gated, and the gating is
	// PRODUCED.
	//
	// ⚠⚠ NEVER A STATUS CODE. [MEASURED — auth.go:191-197, in its own words]
	// "An unauthenticated request gets the login form SERVED DIRECTLY at the
	// URL it tried to reach, STATUS 200 — not a redirect — precisely so a
	// probe can assert on BODY CONTENT rather than status code… the C1
	// catch-all already makes every unmatched path answer 200, SO STATUS
	// ALONE PROVES NOTHING HERE EITHER." The landed form
	// (auth_test.go:75-110) asserts `action="/login"` is served, that
	// protected content did NOT leak, and that THE WATERMARK DID NOT MOVE.
	srv, st, _ := newGateTestServer(t)
	cookie := loginCookie(t, srv, testLoginPassword)
	for _, r := range introduced {
		if !r.Gated {
			t.Errorf("W8-20(a-i) ((m8-62)): %s (%s:%d) is registered WITHOUT the requireLogin wrapper. "+
				"⚠ A GET route without it is invisible to W2's walk (POST-only, so never discovered, never "+
				"classified, never reverse-reconciled) AND to a hand-kept gating list — so (m8-62) would be "+
				"applied to a LISTED route, red, and the ledger would book a kill WHILE THE SCREEN SHIPS OPEN",
				r, r.File, r.Line)
			continue
		}
		wmBefore, err := st.Watermark("site-a", 1)
		if err != nil {
			t.Fatal(err)
		}
		anon := httptest.NewRecorder()
		srv.Handler().ServeHTTP(anon, httptest.NewRequest(r.Method, r.Path, nil))
		body := anon.Body.String()
		if !strings.Contains(body, `action="/login"`) {
			t.Errorf("W8-20(a-i) ((m8-62)): an UNAUTHENTICATED %s did not serve the login form. ⚠ The assertion is "+
				"on BODY CONTENT, never a status code: auth.go serves the login form AT status 200 deliberately, "+
				"and the Go 1.22 catch-all makes every unmatched path answer 200 too", r)
		}
		if strings.Contains(body, "Recording as:") {
			t.Errorf("W8-20(a-i) ((m8-62)): protected page content leaked into an UNAUTHENTICATED %s response", r)
		}
		wmAfter, err := st.Watermark("site-a", 1)
		if err != nil {
			t.Fatal(err)
		}
		if wmAfter != wmBefore {
			t.Errorf("W8-20(a-i): an UNAUTHENTICATED %s AUTHORED AN EVENT (watermark %d -> %d)", r, wmBefore, wmAfter)
		}

		auth := httptest.NewRequest(r.Method, r.Path, nil)
		auth.AddCookie(cookie)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, auth)
		if strings.Contains(rec.Body.String(), `action="/login"`) {
			t.Errorf("W8-20(a-i): an AUTHENTICATED %s was still served the login form — the floor is "+
				"refuse-anonymous AND accept-authenticated, so a route gated shut is as much a defect as one "+
				"gated open", r)
		}
	}
}

// TestW8GatingScanSeesAnUngatedGET is W8-20(a-i)'s CONTROL — "and it is a GET,
// DELIBERATELY".
//
// "A GET route registered WITHOUT the requireLogin wrapper. ⚠ It PASSES EVERY
// LANDED GATING TEST IN internal/web TODAY and reds only under the widened
// form, WHICH IS WHAT MAKES IT A CONTROL RATHER THAN DECORATION."
//
// ⭐ PRODUCED: the plant is scanned on a real on-disk copy ⚠ [DOC] rule 20 —
// `go test -overlay` does NOT reach a source scan.
func TestW8GatingScanSeesAnUngatedGET(t *testing.T) {
	root := t.TempDir()
	// A NESTED directory, so the plant also exercises correction (2): the
	// landed walk's non-recursive glob would never open this file.
	p := filepath.Join(root, "internal", "web", "sub", "plant.go")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package sub\n\n" +
		"func register(mux Mux, s *Server) {\n" +
		"\tmux.HandleFunc(\"GET /trade/new\", s.handleTradeNew)\n" +
		"\tmux.HandleFunc(\"POST /trade/create\", s.requireLogin(s.handleTradeCreate))\n" +
		"}\n"
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	found := w8RegistrationsInTree(t, root)
	var sawUngatedGET, sawGatedPOST bool
	for _, r := range found {
		if r.Method == "GET" && r.Path == "/trade/new" && !r.Gated {
			sawUngatedGET = true
		}
		if r.Method == "POST" && r.Path == "/trade/create" && r.Gated {
			sawGatedPOST = true
		}
	}
	if !sawUngatedGET {
		t.Errorf("W8-20(a-i) CONTROL FAILED: a `GET /trade/new` registered WITHOUT requireLogin, in a NESTED "+
			"directory, was not reported as ungated. found=%v\n"+
			"⚠ This is THE control: such a route passes every landed gating test in internal/web today, is "+
			"invisible to W2's POST-only walk, and is the naming-and-numbers screen itself — the site's trade "+
			"list and the three positional handset numbers, rendered to anyone", found)
	}
	if !sawGatedPOST {
		t.Errorf("W8-20(a-i) CONTROL FAILED (the other direction): a route WRAPPED in requireLogin was not "+
			"recognised as gated, so the scan would report every route ungated and its verdict would be noise. found=%v", found)
	}
}
