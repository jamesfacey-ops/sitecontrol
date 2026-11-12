// w1_census_shape_test.go — W1-10's by-construction half: "the signature
// change non-backward-compatible by construction (§6 step 1)". The census
// itself is the compiler (`go build ./...` PLUS `go vet ./...` — vet
// type-checks _test.go, the half that catches muster_rebuild_test.go, which
// constructs zero Envelope literals yet drives the real domain authors) and
// is recorded in the ledger with before/after counts diffed against §6's
// inventory; THIS test pins what no compiler run can: that the new authoring
// signatures are shapes old callers cannot silently compile against.
// (WINDOW-BUILD-PLAN-v1 §3 W1 + §6 step 1, LOCKED 2026-08-27.)
//
// KILLER MUTANT: m11 — the backward-compatible rebase shim: a variadic tail,
// or a defaulted options pointer, or an unchanged signature fed through a
// grown params struct field (zero forced non-test caller edits against §6's
// 64-site inventory is a RED ledger row, not a pass).
//
// EXPECTED RED (pre-build): every signature below still has its measured v9
// shape at HEAD 89ecee6, so every row fails with "signature unchanged". That
// produced red is this file's ledger RED row.
//
// TEST-WRITER SEAM, stated for the gate: for the two struct-input authors
// (GateCheckIn / Dispatch / DispatchLocalCheckIn) a field ADDED to
// GateCheckInInput/DispatchInput would compile for every old caller with a
// zero-valued pair — exactly m11. This test therefore requires the signature
// TEXT of every authoring function to change. If the builder believes a
// changed-params-struct mechanism satisfies §6 step 1's "forced to find
// every caller" rule some other way, that dispute goes to the gate with
// evidence (§5 step 3); the test is written to the strict reading.
package store

import (
	"archive/tar"
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ⟦LOCK — §67, 2026-09-03⟧ --- THE SHRINK TOOTH, DERIVED NOT PINNED ---------
//
// ⛔ WHY IT EXISTS. [PRODUCED 2026-09-03, S64 R3] this inventory was reduced
// from 20 rows to ONE and the test passed clean. There was no floor, no count
// and no derivation — nothing noticed that nineteen censused authoring
// functions had been dropped. §64(b) warned that *"shrinking an inventory is
// exactly how a census goes quietly toothless"*; ⚠ THAT WAS WRITTEN AS A
// CAUTION WHEN IT NEEDED TO BE A MECHANISM ⟦§67 R3⟧.
//
// ⚠ THE COUNT IS DERIVED, NEVER PINNED. A pinned number is the same hand-kept
// list one level up: it would go stale the first time the surface changed, and
// nothing would say which row was missing. The property asserted instead is:
//
//	EVERY EXPORTED FUNCTION THAT AUTHORED AT THE v9 BASELINE IS CENSUSED,
//	unless it is exempted below WITH A STATED REASON.
//
// The v9 baseline is `89ecee6` — the commit this file's own header names as
// where the signatures were measured. It is read out of git, so the floor
// cannot be hand-shrunk. ⚠ Shelling out to git is this suite's own precedent:
// `internal/domain/w8_deletions_test.go:153` runs `git ls-files` for the same
// kind of reason.

// w1V9CensusExemptions names v9 authoring functions that are deliberately NOT
// in the inventory, each with its reason. ⟦LOCK — §67, 2026-09-03⟧
//
// ⚠ ASSERTED, NOT SUPPRESSED — and it has its OWN tooth: an exemption naming a
// function that is NOT in the derived v9 authoring surface is a failure, so
// this list cannot quietly grow to cover today's functions.
var w1V9CensusExemptions = map[string]string{
	"CreateAlias": "DELETED at W8 by P-3 §5 / [[D-47]] — internal/domain/alias.go no longer exists, so there " +
		"is no successor and no shape for W1-10 to require a change to. Removal authorised at §64(b).",
	"CreateAliasRecordedBy": "DELETED at W8 by P-3 §5 / [[D-47]] — the attributed half of CreateAlias, same reasoning.",
	"DeclarePhase": "DELETED with weather phases — James, 2026-10-04: \"the phase conditions can be removed they will " +
		"not effect site control … go ahead and delete the phase conditions\". internal/domain/weather.go no longer " +
		"exists, so there is no successor and no shape for W1-10 to require a change to.",
	"AcknowledgePhaseReview": "DELETED with weather phases — James, 2026-10-04 (same ruling): the phase-scoped ack " +
		"of a weather VERIFY row has no successor; deadline.Acknowledge / AcknowledgeKind remain censused.",
	"SeedFixture": "⚠ UNRULED GAP, ROUTED AT S67 R3a — NOT a decision that it is out of scope. SeedFixture " +
		"authored at v9 and was NEVER in §6's inventory, and [MEASURED] its signature is UNCHANGED from v9 " +
		"(*store.Store,string,int64), so censusing it would RED as a live W1-10 violation. Whether a seed/demo " +
		"fixture is inside §6 step 1's \"force every caller through the type checker\" rule is a criterion " +
		"question, not a bookkeeping one. ⚠ THIS ENTRY MUST NOT BE LEFT STANDING WITHOUT A RULING.",
}

// w1V9AuthoringSurface derives, from the v9 commit, every EXPORTED function
// that transitively authors. ⟦LOCK — §67, 2026-09-03⟧
//
// One `git archive` call, read through archive/tar in-process: no shell, no
// temp tree, no per-file subprocess.
func w1V9AuthoringSurface(t *testing.T, root string) map[string]string {
	t.Helper()
	cmd := exec.Command("git", "archive", "--format=tar", w1V9Baseline,
		"internal/domain", "internal/deadline", "internal/store")
	cmd.Dir = root
	var out, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errBuf
	if err := cmd.Run(); err != nil {
		t.Fatalf("SHRINK TOOTH INSTRUMENT BROKEN: reading the v9 baseline %s out of git: %v\n%s\n"+
			"⚠ The floor for this census is DERIVED from that commit. If it cannot be read, the inventory is "+
			"unguarded and this test must not report a pass — a census whose floor is unreadable is exactly "+
			"the toothless state §67 R3 exists to prevent.", w1V9Baseline, err, errBuf.String())
	}
	// name -> file, for every function declared in the v9 tree; plus the call graph.
	declFile := map[string]string{}
	callees := map[string][]string{}
	local := map[string]bool{}
	tr := tar.NewReader(&out)
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		if h.Typeflag != tar.TypeReg || !strings.HasSuffix(h.Name, ".go") || strings.HasSuffix(h.Name, "_test.go") {
			continue
		}
		var src bytes.Buffer
		if _, err := src.ReadFrom(tr); err != nil {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), h.Name, src.Bytes(), 0)
		if err != nil {
			continue
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			declFile[fn.Name.Name] = h.Name
			local[fn.Name.Name] = true
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				c, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch x := c.Fun.(type) {
				case *ast.Ident:
					callees[fn.Name.Name] = append(callees[fn.Name.Name], x.Name)
				case *ast.SelectorExpr:
					callees[fn.Name.Name] = append(callees[fn.Name.Name], x.Sel.Name)
				}
				return true
			})
		}
	}
	var reaches func(string, int) bool
	reaches = func(name string, depth int) bool {
		if depth > 8 {
			return false
		}
		for _, c := range callees[name] {
			if c == "Author" {
				return true
			}
			if local[c] && c != name && reaches(c, depth+1) {
				return true
			}
		}
		return false
	}
	surface := map[string]string{}
	for name, file := range declFile {
		if name == "" || name[0] < 'A' || name[0] > 'Z' {
			continue
		}
		if reaches(name, 0) {
			surface[name] = file
		}
	}
	if len(surface) == 0 {
		t.Fatalf("SHRINK TOOTH INSTRUMENT BROKEN: the v9 baseline %s yielded ZERO authoring functions. A floor "+
			"of zero would make the coverage assertion vacuously true, which is the failure this tooth exists "+
			"to prevent", w1V9Baseline)
	}
	return surface
}

// w1V9Baseline is the commit this file's header names as where the v9
// signatures were measured. ⟦LOCK — §67, 2026-09-03⟧
const w1V9Baseline = "89ecee6"

// The measured v9 signatures (HEAD 89ecee6), per-name-expanded parameter type
// lists. These are the shapes that MUST NOT survive W1.
var w1V9AuthoringSignatures = []struct {
	file string // relative to module root
	fn   string // the name TODAY — what the source scan looks for
	v9   string // the measured v9 parameter type list
	// ⟦LOCK — §67, 2026-09-03⟧ v9fn is the name this function had AT THE v9
	// BASELINE, when it has since been RENAMED. "" means the name is unchanged.
	//
	// ⚠ IT EXISTS SO A RENAME IS MACHINE-LEGIBLE, NOT JUST A COMMENT. The
	// coverage tooth below checks the inventory against the v9 authoring
	// surface DERIVED FROM `89ecee6`; a row re-pointed to a new name would
	// otherwise leave its v9 name looking uncensused, and the tooth would
	// demand a row that already exists under another name.
	v9fn string
}{
	{"internal/store/log.go", "Author", "string,int64,json.RawMessage", ""},
	// ⟦LOCK — §64, 2026-09-03⟧ ⭐ A CENSUS REDUCTION, AND IT MUST READ AS ONE.
	//
	// ⟦was: "\t{\"internal/domain/alias.go\", \"CreateAlias\", \"*store.Store,string,int64,string,string\"},"⟧
	// ⟦was: "\t{\"internal/domain/alias.go\", \"CreateAliasRecordedBy\", \"*store.Store,string,int64,string,string,*string\"},"⟧
	//
	// WHAT WAS CENSUSED, so the record survives the removal:
	//
	//	CreateAlias            (*store.Store, string, int64, string, string)
	//	CreateAliasRecordedBy  (*store.Store, string, int64, string, string, *string)
	//
	// both declared in `internal/domain/alias.go`, both part of §6's 64-site
	// authoring inventory, and both subject to W1-10's requirement that their
	// v9 shape must not survive.
	//
	// WHY THEY ARE GONE: ⚠ THE FILE WAS DELETED, NOT RENAMED. P-3 §5's
	// deletion schedule removes the whole `ref_alias` mechanism at W8, and
	// [[D-47]] is the ruling behind it; `internal/domain/alias.go` no longer
	// exists. These two authoring functions therefore have no successor
	// anywhere — there is no shape for W1-10 to require a change to, because
	// there is nothing left to call.
	//
	// ⚠ THIS IS THE DELETION LANDING, NOT COVERAGE QUIETLY DROPPED — the
	// distinction §60 draws, and the reason the two facts above are written
	// out rather than left to `git log`. A reader a window from now must be
	// able to tell "two rows removed because the mechanism was deleted on
	// schedule" from "two rows removed because they were failing".
	//
	// ⚠ AND THE SHRINKING IS THE RISK, NOT THE FIX: an inventory is what gives
	// this census its subject, so removing rows is exactly how it would go
	// quietly toothless. [PRODUCED 2026-09-03] after this reduction the census
	// was shown to STILL RED on a planted v9-signature authoring function, and
	// STILL RED on a censused function that disappears — see
	// reports/window-build/S64-STORE-REDS.md. The reduction is 22 rows -> 20,
	// and the remaining 20 are unaffected.
	{"internal/domain/checkin.go", "CheckOutVisit", "*store.Store,string,int64,string,*string", ""},
	{"internal/domain/checkin.go", "CheckOutVisits", "*store.Store,string,int64,[]string,*string", ""},
	{"internal/domain/checkin.go", "GateCheckIn", "*store.Store,string,int64,GateCheckInInput", ""},
	{"internal/domain/checkin.go", "ResumeCheckIn", "*store.Store,string,int64,string,*string", ""},
	// ⟦was: "\t{\"internal/domain/weather.go\", \"DeclarePhase\", \"*store.Store,string,int64,int64,string,*string,*string\", \"\"},"⟧
	// A CENSUS REDUCTION, read as one (the §64 protocol above): weather phases
	// were removed by James's ruling of 2026-10-04 and weather.go deleted, so
	// DeclarePhase has no successor. Moved to w1V9CensusExemptions WITH ITS
	// REASON, so the shrink tooth still knows it existed at v9.
	{"internal/domain/tripauthor.go", "Dispatch", "*store.Store,string,int64,DispatchInput", ""},
	{"internal/domain/tripauthor.go", "DispatchLocalCheckIn", "*store.Store,string,int64,DispatchInput", ""},
	{"internal/domain/tripauthor.go", "RecordExpectedArrival", "*store.Store,string,int64,string,*string", ""},
	{"internal/domain/tripauthor.go", "RecordExpectedArrivalExcluding", "*store.Store,string,int64,string,[]string,*string", ""},
	{"internal/domain/tripauthor.go", "RecordUnsolicitedArrival", "*store.Store,string,int64,string,string,[]string,*string", ""},
	{"internal/domain/tripauthor.go", "CancelTrip", "*store.Store,string,int64,string,*string", ""},
	{"internal/domain/refdata.go", "CreateCompany", "*store.Store,string,int64,string", ""},
	{"internal/domain/refdata.go", "CreateCompanyRecordedBy", "*store.Store,string,int64,string,*string", ""},
	// ⟦LOCK — §67, 2026-09-03⟧ ⭐ RE-POINTED, NOT REMOVED ⟦§67 R2⟧.
	// ⟦was: "\t{\"internal/domain/refdata.go\", \"CreateWorkgroup\", \"*store.Store,string,int64,string,string\"},"⟧
	// ⟦was: "\t{\"internal/domain/refdata.go\", \"CreateWorkgroupRecordedBy\", \"*store.Store,string,int64,string,string,*string\"},"⟧
	//
	// ⚠ A RENAME IS NOT A DELETION. W8 renamed the workgroup surface to trade;
	// both functions are still declared, still exported, and still author
	// (`refdata.go:553,559`). Removing the rows would have SILENTLY DROPPED TWO
	// LIVE AUTHORING FUNCTIONS from the census — and with no shrink tooth in
	// place at the time, nothing anywhere would have caught it.
	//
	// The v9 SIGNATURE STRING is carried across unchanged, because it is the
	// shape W1-10 forbids surviving and that shape belongs to the function, not
	// to its name.
	{"internal/domain/refdata.go", "CreateTrade", "*store.Store,string,int64,string,string", "CreateWorkgroup"},
	{"internal/domain/refdata.go", "CreateTradeRecordedBy", "*store.Store,string,int64,string,string,*string", "CreateWorkgroupRecordedBy"},
	{"internal/domain/refdata.go", "CreatePerson", "*store.Store,string,int64,string,*string,*string", ""},
	{"internal/domain/refdata.go", "CreatePersonRecordedBy", "*store.Store,string,int64,string,*string,*string,*string", ""},
	{"internal/deadline/ack.go", "Acknowledge", "*store.Store,string,int64,string,*string", ""},
	// ⟦was: "\t{\"internal/deadline/ack.go\", \"AcknowledgePhaseReview\", \"*store.Store,string,int64,string,int64,*string\", \"\"},"⟧
	// Same reduction, same ruling: exempted with its reason above.
}

func TestW1AuthoringSignaturesChangedNonBackwardCompatibly(t *testing.T) {
	root := w1ModuleRoot(t)

	// --- ⟦LOCK — §67, 2026-09-03⟧ THE SHRINK TOOTH, FIRST -----------------
	//
	// ⚠ IT RUNS BEFORE THE SIGNATURE ASSERTIONS DELIBERATELY. Those assertions
	// are only as good as the inventory they iterate; an inventory that has
	// quietly lost rows reports a clean pass over whatever is left, which is
	// precisely how this census could be reduced from 20 rows to one and stay
	// green ⟦§67 R3⟧.
	surface := w1V9AuthoringSurface(t, root)
	censused := map[string]bool{}
	for _, row := range w1V9AuthoringSignatures {
		name := row.fn
		if row.v9fn != "" {
			name = row.v9fn // the rename's v9 identity is what the floor knows
		}
		censused[name] = true
	}
	var uncovered []string
	for name, file := range surface {
		if censused[name] {
			continue
		}
		if _, exempt := w1V9CensusExemptions[name]; exempt {
			continue
		}
		uncovered = append(uncovered, name+" ("+file+")")
	}
	if len(uncovered) != 0 {
		sort.Strings(uncovered)
		t.Errorf("W1-10 SHRINK TOOTH ⟦§67 R3⟧: %d authoring function(s) present at the v9 baseline %s are NOT "+
			"censused and NOT exempted:\n  %s\n"+
			"⚠ §6's inventory is what gives this census its subject. A row dropped from it removes an authoring "+
			"function from W1-10's reach SILENTLY — the count is derived from the v9 commit precisely so that "+
			"cannot happen. Either census the function, or add it to w1V9CensusExemptions WITH ITS REASON.\n"+
			"⚠ Do NOT satisfy this by deleting rows.",
			len(uncovered), w1V9Baseline, strings.Join(uncovered, "\n  "))
	}
	// The exemption list's OWN tooth: it may only name v9 authoring functions.
	for name, reason := range w1V9CensusExemptions {
		if _, real := surface[name]; !real {
			t.Errorf("W1-10 ⟦§67 R3⟧: w1V9CensusExemptions names %q (%s), which is NOT an authoring function at "+
				"the v9 baseline %s. An exemption list that can name anything is not an exemption list — it is "+
				"a way to silence this tooth.", name, reason, w1V9Baseline)
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("W1-10 ⟦§67 R3⟧: the exemption for %q states no reason. Dropping an authoring function from "+
				"the census is a deliberate act and must read as one.", name)
		}
	}
	t.Logf("W1-10 SHRINK TOOTH [MEASURED]: v9 baseline %s declares %d exported authoring function(s); "+
		"inventory censuses %d; %d exempted with stated reasons",
		w1V9Baseline, len(surface), len(w1V9AuthoringSignatures), len(w1V9CensusExemptions))
	byFile := map[string]map[string]bool{}
	for _, row := range w1V9AuthoringSignatures {
		if byFile[row.file] == nil {
			byFile[row.file] = map[string]bool{}
		}
		byFile[row.file][row.fn] = true
	}
	parsed := map[string]map[string]w1FuncSig{}
	for file, names := range byFile {
		parsed[file] = w1ParseFuncSigs(t, filepath.Join(root, filepath.FromSlash(file)), names)
	}
	for _, row := range w1V9AuthoringSignatures {
		sig, ok := parsed[row.file][row.fn]
		if !ok || !sig.Found {
			// A renamed/moved authoring function is a census question, not a
			// silent pass: §6's inventory names these functions; a rename
			// must be routed to the gate and this table amended in the same
			// commit.
			t.Errorf("%s: authoring signature %s not found — §6's census inventory names it; a rename/move must be adjudicated, not scanned around", row.file, row.fn)
			continue
		}
		if sig.ParamTypes == row.v9 {
			t.Errorf("%s.%s still has its v9 signature (%s) — the rebase must force every caller through the type checker (§6 step 1, mutant m11)", row.file, row.fn, row.v9)
		}
		if sig.Variadic {
			t.Errorf("%s.%s is variadic — a variadic tail is the named m11 shim (§6 step 1: never a variadic tail)", row.file, row.fn)
		}
		if sig.PointerOptionParam {
			t.Errorf("%s.%s takes a pointer options/params/config parameter — the defaulted-options-pointer m11 shim (§6 step 1: never an options pointer with a nil default)", row.file, row.fn)
		}
	}

	// Store.Author additionally: no pointer parameter of ANY kind (its v9
	// shape had none; any pointer added here is a nil-defaultable seam), and
	// the pair means MORE parameters, not fewer.
	authorSig := parsed["internal/store/log.go"]["Author"]
	if authorSig.Found {
		if authorSig.PointerParams != 0 {
			t.Errorf("Store.Author takes %d pointer parameter(s) — a nil-defaultable parameter is a census hole (§6 step 1)", authorSig.PointerParams)
		}
	}
}

// Channel control (standing rule): the shim-detection observations must be
// shown able to fire before their clean pass counts.
func TestW1CensusShapeControlDetectsShimShapes(t *testing.T) {
	variadic := []byte(`package planted

func Author(siteID string, nodeEpoch int64, payload []byte, pair ...int64) {}
`)
	sigs := w1ParseFuncSigsFromSource(t, "variadic.go", variadic, map[string]bool{"Author": true})
	if !sigs["Author"].Variadic {
		t.Fatal("census control: variadic tail not detected")
	}
	optPtr := []byte(`package planted

func Dispatch(st *Store, siteID string, nodeEpoch int64, in DispatchInput, opts *AuthorOptions) {}
`)
	sigs = w1ParseFuncSigsFromSource(t, "optptr.go", optPtr, map[string]bool{"Dispatch": true})
	if !sigs["Dispatch"].PointerOptionParam {
		t.Fatal("census control: defaulted-options-pointer shape not detected")
	}
	unchanged := []byte(`package planted

func CancelTrip(st *store.Store, siteID string, nodeEpoch int64, tripID string, recordedBy *string) error { return nil }
`)
	sigs = w1ParseFuncSigsFromSource(t, "unchanged.go", unchanged, map[string]bool{"CancelTrip": true})
	if sigs["CancelTrip"].ParamTypes != "*store.Store,string,int64,string,*string" {
		t.Fatalf("census control: signature rendering drifted (%q) — the v9 table above would compare against garbage", sigs["CancelTrip"].ParamTypes)
	}
}
