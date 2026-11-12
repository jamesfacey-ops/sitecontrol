// deploy_four_payload_crlf_test.go — the four remaining deploy scripts must not ship a
// carriage return into the shell that executes their payload.
//
// SCOPE. deploy/sever.ps1, deploy/restore.ps1, deploy/demo/dc.ps1 (BOTH of its `-On`
// branches) and deploy/demo/push-harness.ps1. The two site-b scripts are covered
// separately by deploy_siteb_payload_crlf_test.go, which this file deliberately does not
// duplicate and does not share code with (a shared helper package was tried on this repo
// and reverted: it breaks the module-shape stop). ⟦was: "the 9-package stop" — the stop is
// TEN packages since G15 (2026-09-09) landed cmd/dupreport deliberately. ⛔ Written as a
// NUMERAL, this survived a whole-word grep for "nine" twice: once in the G15 site sweep and
// once in the review of it. The stop is named here instead of counted, so it cannot go
// stale again. The revert itself stands — it bought nothing and cost a package.⟧
//
// THE PROPERTY THIS TESTS IS THE VALUE, NOT THE LINE. Each of these scripts builds a
// multi-line payload as a here-string and hands it to `ssh`, which hands it to a POSIX
// shell on pve1. `.gitattributes` carries `*.ps1 text eol=crlf`, so the file checks out
// with CRLF terminators and the here-string body GENUINELY CONTAINS CR — the defect is
// introduced AT CHECKOUT, not at commit. Each script therefore carries a normalise step
// (`-replace "`r`n", "`n"`) before the payload crosses to sh. With a CR the payload's
// `set -e` becomes `set` with the argument "-e\r" — VALID SYNTAX that fails only at
// RUNTIME, which is why `bash -n` returns 0 on the broken form. sever.ps1's own comment
// records the live consequence: a HALF sever, one direction of the partition still open,
// MEASURED 2026-09-05.
//
// WHY THIS IS NOT A GREP FOR THE NORMALISE LINE. A test matching that line's text is a
// PROXY, and the proxy is demonstrably worthless here: two PRESENT-BUT-NO-OP rewrites
// reproduce the full defect while leaving the line visibly in place —
//
//	$remote = $remote -replace '`r`n', "`n"     # SINGLE quotes: PowerShell does not expand
//	                                            # backtick escapes, so the pattern is the
//	                                            # four literal characters ` r ` n and
//	                                            # matches nothing. 36 CR survive.
//	$remoteScript = $remoteScript -replace "`r`n", "`r`n"   # CRLF -> CRLF. 3 CR survive.
//
// Both are reproduced as live mutants in reports/witness/CRLF-FOUR-WITNESSES.md. A grep
// passes on clean and mutant alike. So instead this file EVALUATES the payload: it
// resolves the here-strings, the concatenations and the -replace chain, reconstructs the
// exact argument strings handed to `& ssh`, and asserts no CR survives into any of them.
//
// WHAT THE MODEL SUPPORTS, AND WHAT IT WOULD MISJUDGE — read this before trusting a green.
// pshEval implements a deliberately tiny subset of PowerShell: single- and double-quoted
// here-strings, single-line string literals, integer literals, array literals, `+`
// concatenation, `-replace`, `$Var` / `${Var}` interpolation, backtick escapes, `if` /
// `elseif` / `else` arms (enumerated, so dc.ps1's two `-On` branches are BOTH checked) and
// `try` / `finally`. Anything outside that subset is an ERROR, never a silent pass.
//
//	REGEX DIALECT. PowerShell's `-replace` is REGEX-based, not a literal replace. This
//	model compiles the pattern with Go's regexp (RE2) and translates .NET substitution
//	syntax ($1, ${name}, $&, $$) in the replacement. It therefore ACCEPTS a correct
//	rewrite such as `-replace "\r", ""`, which strips a bare CR and is not the literal
//	line the scripts use today. (The site-b witness models -replace as a LITERAL replace
//	and goes red on that rewrite — a false-FAIL, noted by its reviewer as non-blocking
//	because it errs red. This model does not inherit that limitation.)
//	IT WOULD MISJUDGE, all in the red-erring direction: .NET backreferences (\1) and
//	lookaround ((?=, (?<=) — RE2 rejects them, so the model errors out rather than
//	guessing; .NET named groups (?<n>...) — same; and \d \w \s, which are Unicode-aware
//	in .NET but ASCII in Go, so the model matches a SMALLER set and can only OVER-count
//	CR. Matching is modelled CASE-SENSITIVELY while PowerShell's -replace is case-
//	INSENSITIVE: again the model replaces no more than PowerShell does, so it can only
//	over-count. CR and LF have no case, so this is documentation, not a live difference.
//	Every one of these errs toward a false FAIL (noise), never a false PASS (danger).
//
//	THE DIRECTION RULE, because "errs red" is a property of each gap and not of the model:
//	a pattern RE2 matched MORE BROADLY than .NET would remove MORE CR than PowerShell does
//	and err GREEN — the dangerous direction. Every gap enumerated above happens to go the
//	other way (Go matches a smaller set), which is the only reason they are all noise; that
//	is a finding about those constructs, not a guarantee about the next one. MEASURED in a
//	real checkout 2026-09-09: all four payload normalisers are the same literal spelling,
//	`-replace "`r`n", "`n"` (sever.ps1:28, restore.ps1:31, dc.ps1:113, push-harness.ps1:86)
//	— no character class, no backreference, no case-dependent construct — so no broader-
//	match residue is live today. The only other -replace in scope, push-harness.ps1:110's
//	`-replace '^REMOTE_SHA256=', ''`, is off the payload path and its `^` anchor means the
//	same in both dialects without a multiline flag. If a pattern ever changes, re-apply this
//	rule rather than scanning the list above for a matching entry.
//
//	OPERAND QUOTING DECIDES WHO INTERPRETS THE `$` — BUT ONLY WHEN THE `$` IS BARE. The two
//	-replace operands are NOT one dialect. In a DOUBLE-quoted operand PowerShell interpolates
//	`$…` FIRST, so an UNESCAPED `"$_"` is an undefined variable and reaches .NET as the EMPTY
//	STRING. In a SINGLE-quoted operand nothing is interpolated, so .NET receives `$_` intact
//	and applies its own substitution rule: `$_` is THE ENTIRE INPUT STRING. Same four
//	characters, opposite meanings.
//
//	DOUBLE QUOTES ARE NOT PROOF THE `$` WAS CONSUMED. A BACKTICK-ESCAPED dollar inside a
//	double-quoted operand — `"`$_"` — survives PowerShell's pass as a literal `$_` and
//	reaches .NET LIVE, so it behaves exactly like the single-quoted spelling. The quoting
//	selects the interpreter only for a bare `$`; a backtick overrides it. MEASURED in
//	powershell.exe 2026-09-09 on input "AA`r`nBB" (one CR):
//
//		"AA`r`nBB" -replace "`r`n", "$_"    ->  AABB               0 CR  (PowerShell ate it)
//		"AA`r`nBB" -replace "`r`n", '$_'    ->  AA·AA CRLF BB·BB   1 CR  (.NET expanded it)
//		"AA`r`nBB" -replace "`r`n", "`$_"   ->  AA·AA CRLF BB·BB   1 CR  (.NET expanded it,
//		                                                             from a DOUBLE-quoted operand)
//		"AA`r`nBB" -replace "`r`n", "`${_}" ->  AA${_}BB           0 CR  (.NET emits an unknown
//		                                                             group name literally)
//
//	MODELLING ONLY THE DOUBLE-QUOTED SIDE PRODUCED TWO LIVE FALSE PASSES, both closed by one
//	change on 2026-09-09 and both now pinned by controls below.
//	Before the fix the single-quoted `$_` was scanned as the group name "_", resolved to
//	"" and DELETED the match. So `-replace "`r", '$_'` — which reads like "strip the CR" —
//	modelled as CR-removal while real PowerShell substitutes a CR-BEARING copy of the whole
//	payload at every CR. MEASURED against the real deploy/sever.ps1 payload block in
//	powershell.exe: the ssh argument carried 9 CR and grew 301 -> 1141 bytes, and the
//	witness printed `ok`. That mutant is now control "PRESENT-BUT-WORSE-THAN-NO-OP" below.
//	Note it evades the anti-vacuity check (c) by design: the pattern is a BARE CR, so the
//	LF line structure the here-string is matched on survives intact. A CRLF-pattern variant
//	of the same defect fragments the body and trips (c) instead — with a message blaming a
//	broken payload chain, which is the wrong diagnosis. Check (a) now names it correctly.
//
//	THE SECOND FALSE PASS was the backtick-escaped double-quoted spelling `"`$_"` above. It
//	shares the root cause — both spellings meet in pshTranslateReplacement, one via
//	pshExpandSQ and one via pshExpandDQ — so the same change closed it AS A SIDE EFFECT,
//	unremarked and unpinned, while this header still said a double-quoted operand always
//	reaches .NET with the `$` consumed. MEASURED with this model: 0 CR at e812ce3 (a live
//	false PASS) and 1 CR at 6b10b89, against 1 CR in real powershell.exe. A hole closed by
//	accident, undocumented and unpinned, is one edit away from being a hole again — in
//	particular a "simplification" that skipped .NET substitution for double-quoted operands
//	would keep every arm below green and silently reopen it. It now has its own control:
//	"a BACKTICK-ESCAPED `$_ in a DOUBLE-quoted operand still reaches .NET live".
//
//	WHAT THE FIXED MODEL STILL CANNOT FOLLOW, and which way each errs — all MEASURED
//	2026-09-09 in powershell.exe, none inherited:
//	  * `$``  (text before match) and `$'` (text after match) in a single-quoted
//	    replacement: the model ERRORS OUT. Both can carry CR, so this matters — but an
//	    error is a RED. Errs FALSE-FAIL (noisy, safe).
//	  * `$+` (last captured group) in a single-quoted replacement: the model ERRORS OUT.
//	    Measured: it substituted a CR-bearing match. Errs FALSE-FAIL (noisy, safe).
//	  * `$1` / `$x` / `${nope}` naming a group that DOES NOT EXIST: .NET emits the token
//	    LITERALLY (measured: `$1` -> the two characters $1); Go's Expand emits "". The model
//	    therefore under-produces, but only ever by CR-FREE literal text such as "$1", so it
//	    cannot hide a CR. NEUTRAL for this property.
//	  * `$12` where group 12 does not exist: .NET does NOT fall back to group 1 — it emits
//	    the literal `$12` (measured, CR-free). Go emits "". NEUTRAL, same reason.
//	  * A pattern-side `$` anchor: .NET's `$` also matches before a trailing \n, Go's does
//	    not. Go matches a SMALLER set, so it removes fewer CR. Errs FALSE-FAIL.
//	  * Case: `-replace` is case-INSENSITIVE (measured: 'aa' matched "AA"); the model is
//	    case-sensitive, so again it replaces no more than PowerShell does. Errs FALSE-FAIL.
//	    CR and LF have no case, so this is documentation rather than a live difference.
//	Only `$_` handling was ever able to err FALSE-PASS. It did so in TWO spellings — the
//	single-quoted `'$_'` and the backtick-escaped double-quoted `"`$_"` — and both are now
//	closed and pinned by controls below. Both were found by a reviewer reading the fix, not
//	by the suite: nothing here would have gone red for either one.
//
// WHAT WOULD MAKE THIS VACUOUS, AND WHAT CATCHES IT. If a normalise step were deleted and
// the ssh call renamed to a variable that is never assigned, a naive model would resolve
// the payload to "" and pass. ⟦was: TestFourDeployPayloadsCarryNoCarriageReturn — not in the public repository⟧'s check (c)
// requires every here-string's evaluated value to actually REACH an ssh argument in some
// arm, so a broken chain is a red rather than an empty pass. Check (d) requires the
// here-strings to contain CR on disk at all, so a non-CRLF checkout is reported instead of
// passing for free.
package store_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// values and environment
// ---------------------------------------------------------------------------

// pshVal is a PowerShell value this model can carry: a scalar string, or an array of
// strings (for the $sshArgs splat that sever.ps1 and restore.ps1 use).
type pshVal struct {
	s     string
	arr   []string
	isArr bool
}

func pshScalar(s string) pshVal { return pshVal{s: s} }

// pshEnv is the variable table. A variable this model could not evaluate is simply
// ABSENT: PowerShell expands an undefined variable to the empty string inside a
// double-quoted string, so absence is faithful. Absence is made safe by check (c) —
// a payload that stops reaching ssh is a red, not a quiet empty pass.
type pshEnv map[string]pshVal

// ---------------------------------------------------------------------------
// statement splitting — here-strings are consumed atomically
// ---------------------------------------------------------------------------

// pshStmt is one PowerShell statement. A here-string assignment is ONE statement even
// though it spans many physical lines; the body must never be scanned for block
// delimiters or operators, because it is sh source, not PowerShell.
type pshStmt struct {
	text    string      // full statement text, CRs preserved
	head    string      // first physical line, right-trimmed of CR
	line    int         // 1-based line number of head
	arms    map[int]int // blockID -> arm index this statement lives under
	inCatch bool        // inside a catch body: not executed on the normal path
}

var pshHereOpen = regexp.MustCompile(`@['"]\s*$`)

func pshSplitStatements(src string) []pshStmt {
	lines := strings.Split(src, "\n")
	var out []pshStmt
	for i := 0; i < len(lines); i++ {
		head := strings.TrimRight(lines[i], "\r")
		if !pshHereOpen.MatchString(head) {
			out = append(out, pshStmt{text: lines[i], head: head, line: i + 1})
			continue
		}
		// A here-string terminator (`'@` or `"@`) must sit at the START of its line.
		term := "'@"
		if strings.HasSuffix(head, `@"`) {
			term = `"@`
		}
		body := []string{lines[i]}
		j := i + 1
		for ; j < len(lines); j++ {
			body = append(body, lines[j])
			if strings.HasPrefix(strings.TrimRight(lines[j], "\r"), term) {
				break
			}
		}
		out = append(out, pshStmt{text: strings.Join(body, "\n"), head: head, line: i + 1})
		i = j
	}
	return out
}

// ---------------------------------------------------------------------------
// block structure — if/elseif/else arms are ENUMERATED, so both dc.ps1 branches run
// ---------------------------------------------------------------------------

var (
	pshIfOpen     = regexp.MustCompile(`^if\s*\(.*\)\s*\{\s*$`)
	pshElseIfOpen = regexp.MustCompile(`^\}\s*elseif\s*\(.*\)\s*\{\s*$`)
	pshElseOpen   = regexp.MustCompile(`^\}\s*else\s*\{\s*$`)
	pshTryOpen    = regexp.MustCompile(`^try\s*\{\s*$`)
	pshCatchOpen  = regexp.MustCompile(`^\}\s*catch\b.*\{\s*$`)
	pshFinallyOp  = regexp.MustCompile(`^\}\s*finally\s*\{\s*$`)
	pshBlockClose = regexp.MustCompile(`^\}\s*$`)
	pshAnyOpen    = regexp.MustCompile(`\{\s*$`)
)

type pshFrame struct {
	id      int  // -1 for try/catch/finally (sequential, not alternative)
	arm     int  // current arm index within an if-block
	inCatch bool // currently inside a catch body
}

// pshAnnotate walks the statements and records, for each, which if-arm it lives under.
// A one-armed `if` gets TWO arms (taken / not taken), because its body is conditional.
// It returns the arm count per if-block. An unrecognised block opener is an error: this
// model must never evaluate a loop body once and call the result the payload.
func pshAnnotate(stmts []pshStmt) ([]int, error) {
	var arms []int
	var stack []pshFrame
	for i := range stmts {
		h := stmts[i].head
		switch {
		case pshIfOpen.MatchString(h):
			arms = append(arms, 2) // arm 0 = taken; the extra arm covers "not taken"
			stack = append(stack, pshFrame{id: len(arms) - 1})
			continue
		case pshElseIfOpen.MatchString(h), pshElseOpen.MatchString(h):
			if len(stack) == 0 || stack[len(stack)-1].id < 0 {
				return nil, fmt.Errorf("line %d: %q closes an if-arm but no if-block is open", stmts[i].line, h)
			}
			f := &stack[len(stack)-1]
			f.arm++
			if f.arm+1 > arms[f.id] {
				arms[f.id] = f.arm + 1
			}
			continue
		case pshTryOpen.MatchString(h):
			stack = append(stack, pshFrame{id: -1})
			continue
		case pshCatchOpen.MatchString(h):
			if len(stack) == 0 || stack[len(stack)-1].id != -1 {
				return nil, fmt.Errorf("line %d: catch without try", stmts[i].line)
			}
			stack[len(stack)-1].inCatch = true
			continue
		case pshFinallyOp.MatchString(h):
			if len(stack) == 0 || stack[len(stack)-1].id != -1 {
				return nil, fmt.Errorf("line %d: finally without try", stmts[i].line)
			}
			stack[len(stack)-1].inCatch = false
			continue
		case pshBlockClose.MatchString(h):
			if len(stack) == 0 {
				return nil, fmt.Errorf("line %d: closing brace with no open block", stmts[i].line)
			}
			stack = stack[:len(stack)-1]
			continue
		case pshAnyOpen.MatchString(h) && !strings.HasPrefix(h, " ") && !strings.HasPrefix(h, "\t") && !strings.HasPrefix(h, "#"):
			return nil, fmt.Errorf("line %d: unmodelled block opener %q — this witness enumerates if/elseif/else and "+
				"executes try/finally, and refuses to guess at anything else (a loop body evaluated once is not the payload)",
				stmts[i].line, h)
		}
		sel := map[int]int{}
		inCatch := false
		for _, f := range stack {
			if f.id >= 0 {
				sel[f.id] = f.arm
			}
			if f.inCatch {
				inCatch = true
			}
		}
		stmts[i].arms = sel
		stmts[i].inCatch = inCatch
	}
	if len(stack) != 0 {
		return nil, fmt.Errorf("%d block(s) left open at end of file", len(stack))
	}
	return arms, nil
}

// pshCombos enumerates every arm selection. Capped, so a script that grows many branches
// reports the growth instead of quietly taking minutes.
func pshCombos(arms []int) ([]map[int]int, error) {
	total := 1
	for _, n := range arms {
		total *= n
	}
	if total > 32 {
		return nil, fmt.Errorf("%d if-arm combinations (>32) — the script's branching outgrew this witness", total)
	}
	out := []map[int]int{{}}
	for b, n := range arms {
		var next []map[int]int
		for _, base := range out {
			for a := 0; a < n; a++ {
				m := map[int]int{}
				for k, v := range base {
					m[k] = v
				}
				m[b] = a
				next = append(next, m)
			}
		}
		out = next
	}
	return out, nil
}

func pshSelected(st pshStmt, combo map[int]int) bool {
	if st.inCatch {
		return false
	}
	for b, a := range st.arms {
		if combo[b] != a {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// string expansion
// ---------------------------------------------------------------------------

func pshIsIdentByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// pshExpandDQ expands a DOUBLE-QUOTED body: backtick escapes (backtick, not backslash —
// `r is CR, `n is LF) and $Var / ${Var} interpolation, in one left-to-right pass, so a
// backtick-escaped dollar is correctly NOT expanded. An undefined variable expands to the
// empty string, which is what PowerShell does.
func pshExpandDQ(s string, env pshEnv) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '`' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'r':
				b.WriteByte('\r')
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '0':
				b.WriteByte(0)
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		if c != '$' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		if s[i+1] == '(' {
			return "", fmt.Errorf("subexpression $( ... ) inside a double-quoted string is not modelled by this witness")
		}
		if s[i+1] == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				return "", fmt.Errorf("unterminated ${ ... } in %q", s)
			}
			name := s[i+2 : i+2+end]
			b.WriteString(env[name].s)
			i = i + 2 + end
			continue
		}
		j := i + 1
		for j < len(s) && pshIsIdentByte(s[j]) {
			j++
		}
		if j == i+1 {
			b.WriteByte(c)
			continue
		}
		name := s[i+1 : j]
		if j < len(s) && s[j] == ':' { // $env:NAME and friends: not modelled, expands empty
			k := j + 1
			for k < len(s) && pshIsIdentByte(s[k]) {
				k++
			}
			i = k - 1
			continue
		}
		b.WriteString(env[name].s)
		i = j - 1
	}
	return b.String(), nil
}

// pshExpandSQ resolves a SINGLE-quoted single-line literal: no interpolation, no backtick
// escapes; only a doubled quote stands for one quote.
func pshExpandSQ(s string) string { return strings.ReplaceAll(s, "''", "'") }

// ---------------------------------------------------------------------------
// -replace, modelled as the REGEX operator it actually is
// ---------------------------------------------------------------------------

// pshTranslateReplacement rewrites a .NET substitution string into Go's Expand syntax.
// Anything it does not recognise is an error, never a guess.
//
// `input` is the string being searched. It is needed because .NET's `$_` substitution means
// THE ENTIRE INPUT STRING, and Go's Expand has no equivalent token to translate it into.
func pshTranslateReplacement(rep, input string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(rep); i++ {
		if rep[i] != '$' {
			b.WriteByte(rep[i])
			continue
		}
		if i+1 >= len(rep) {
			return "", fmt.Errorf("replacement %q ends with a bare $", rep)
		}
		i++
		switch {
		case rep[i] == '$':
			b.WriteString("$$")
		case rep[i] == '&':
			b.WriteString("${0}")
		case rep[i] == '{':
			end := strings.IndexByte(rep[i:], '}')
			if end < 0 {
				return "", fmt.Errorf("replacement %q has an unterminated ${ ... }", rep)
			}
			b.WriteString("${" + rep[i+1:i+end] + "}")
			i += end
		case rep[i] == '_':
			// .NET substitution `$_` is THE ENTIRE INPUT STRING — not a group, not empty.
			// This case MUST precede the pshIsIdentByte arm below, because '_' is an
			// identifier byte and that arm would otherwise scan it as the group name "_"
			// and resolve it to "" (no such group), silently DELETING the match instead of
			// replacing it with a CR-bearing copy of the payload.
			//
			// MEASURED in powershell.exe 2026-09-09, input "AA`r`nBB" (one CR):
			//   "AA`r`nBB" -replace "`r",   '$_'    -> AA AA CR LF BB LF BB   (CR survives)
			//   "AA`r`nBB" -replace "`r`n", '$_abc' -> AA AA CR LF BB abc BB  ($_ is a TWO-
			//                                          character token; "abc" stays literal)
			//   "AA`r`nBB" -replace "`r`n", '$$_'   -> AA $ _ BB              ($$ escapes it)
			// `$_` is consumed here as exactly two characters, matching that measurement.
			//
			// Go's Expand has no equivalent token, so the input is spliced in literally with
			// every '$' doubled, which is how Expand encodes a literal '$'.
			b.WriteString(strings.ReplaceAll(input, "$", "$$"))
		case pshIsIdentByte(rep[i]):
			j := i
			for j < len(rep) && pshIsIdentByte(rep[j]) {
				j++
			}
			b.WriteString("${" + rep[i:j] + "}")
			i = j - 1
		default:
			return "", fmt.Errorf("replacement %q uses the .NET substitution $%c, which this witness does not model", rep, rep[i])
		}
	}
	return b.String(), nil
}

// pshApplyReplace performs one -replace. The pattern is compiled as a REGEX, because
// PowerShell's -replace is a regex operator; modelling it as a literal replace would
// false-FAIL a correct rewrite such as -replace "\r", "".
func pshApplyReplace(input, pat, rep string) (string, error) {
	re, err := regexp.Compile(pat)
	if err != nil {
		return "", fmt.Errorf("-replace pattern %q does not compile under Go's regexp (RE2): %v — "+
			".NET backreferences, lookaround and (?<name>...) are outside this witness's dialect; "+
			"re-derive the payload by hand rather than trusting a green", pat, err)
	}
	if !strings.Contains(rep, "$") {
		return re.ReplaceAllLiteralString(input, rep), nil
	}
	goRep, err := pshTranslateReplacement(rep, input)
	if err != nil {
		return "", err
	}
	return re.ReplaceAllString(input, goRep), nil
}

// ---------------------------------------------------------------------------
// expression parser
// ---------------------------------------------------------------------------

type pshParser struct {
	s   string
	i   int
	env pshEnv
}

func (p *pshParser) ws() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t' || p.s[p.i] == '\r' || p.s[p.i] == '\n') {
		p.i++
	}
}

func (p *pshParser) peek(lit string) bool {
	p.ws()
	return strings.HasPrefix(p.s[p.i:], lit)
}

// literal reads a quoted string and reports whether it was double-quoted.
func (p *pshParser) literal() (raw string, dq bool, ok bool) {
	p.ws()
	if p.i >= len(p.s) {
		return "", false, false
	}
	q := p.s[p.i]
	if q != '"' && q != '\'' {
		return "", false, false
	}
	for j := p.i + 1; j < len(p.s); j++ {
		if q == '"' && p.s[j] == '`' { // backtick escapes the next byte
			j++
			continue
		}
		if p.s[j] != q {
			continue
		}
		if j+1 < len(p.s) && p.s[j+1] == q { // doubled quote is an escaped quote
			j++
			continue
		}
		raw = p.s[p.i+1 : j]
		p.i = j + 1
		return raw, q == '"', true
	}
	return "", false, false
}

func (p *pshParser) primary() (pshVal, error) {
	p.ws()
	if p.i >= len(p.s) {
		return pshVal{}, fmt.Errorf("unexpected end of expression")
	}
	// array literal @( ... )
	if strings.HasPrefix(p.s[p.i:], "@(") {
		p.i += 2
		var items []string
		for {
			p.ws()
			if p.peek(")") {
				p.i++
				break
			}
			v, err := p.expr()
			if err != nil {
				return pshVal{}, err
			}
			if v.isArr {
				items = append(items, v.arr...)
			} else {
				items = append(items, v.s)
			}
			p.ws()
			if p.peek(",") {
				p.i++
				continue
			}
			if p.peek(")") {
				p.i++
				break
			}
			return pshVal{}, fmt.Errorf("array literal: expected , or ) at %q", p.s[p.i:])
		}
		return pshVal{arr: items, isArr: true}, nil
	}
	if p.peek("(") {
		p.i++
		v, err := p.expr()
		if err != nil {
			return pshVal{}, err
		}
		p.ws()
		if !p.peek(")") {
			return pshVal{}, fmt.Errorf("expected ) at %q", p.s[p.i:])
		}
		p.i++
		return v, nil
	}
	// here-strings
	if strings.HasPrefix(p.s[p.i:], "@'") || strings.HasPrefix(p.s[p.i:], `@"`) {
		dq := p.s[p.i+1] == '"'
		term := "\n'@"
		if dq {
			term = "\n\"@"
		}
		rest := p.s[p.i+2:]
		nl := strings.IndexByte(rest, '\n')
		if nl < 0 {
			return pshVal{}, fmt.Errorf("here-string opener is not at end of line")
		}
		body := rest[nl+1:]
		end := strings.Index(body, term)
		if end < 0 {
			// The terminator may be the very first thing (empty here-string).
			if strings.HasPrefix(body, term[1:]) {
				p.i = len(p.s)
				return pshScalar(""), nil
			}
			return pshVal{}, fmt.Errorf("unterminated here-string")
		}
		// PowerShell drops the line break immediately before the terminator, and the one
		// immediately after the opener. Both are delimiters, not content.
		raw := strings.TrimSuffix(body[:end], "\r")
		p.i = p.i + 2 + nl + 1 + end + len(term)
		if !dq {
			return pshScalar(raw), nil
		}
		v, err := pshExpandDQ(raw, p.env)
		if err != nil {
			return pshVal{}, err
		}
		return pshScalar(v), nil
	}
	if raw, dq, ok := p.literal(); ok {
		if !dq {
			return pshScalar(pshExpandSQ(raw)), nil
		}
		v, err := pshExpandDQ(raw, p.env)
		if err != nil {
			return pshVal{}, err
		}
		return pshScalar(v), nil
	}
	if p.s[p.i] == '$' {
		j := p.i + 1
		for j < len(p.s) && pshIsIdentByte(p.s[j]) {
			j++
		}
		if j == p.i+1 {
			return pshVal{}, fmt.Errorf("bare $ at %q", p.s[p.i:])
		}
		name := p.s[p.i+1 : j]
		p.i = j
		if j < len(p.s) && (p.s[j] == '.' || p.s[j] == ':' || p.s[j] == '[') {
			return pshVal{}, fmt.Errorf("member access / scoped variable on $%s is not modelled", name)
		}
		return p.env[name], nil
	}
	// integer literal
	j := p.i
	for j < len(p.s) && p.s[j] >= '0' && p.s[j] <= '9' {
		j++
	}
	if j > p.i {
		n, _ := strconv.Atoi(p.s[p.i:j])
		p.i = j
		return pshScalar(strconv.Itoa(n)), nil
	}
	return pshVal{}, fmt.Errorf("unmodelled expression at %q", p.s[p.i:])
}

// additive parses `+` concatenation, which binds tighter than -replace in PowerShell.
func (p *pshParser) additive() (pshVal, error) {
	v, err := p.primary()
	if err != nil {
		return pshVal{}, err
	}
	for {
		p.ws()
		if !p.peek("+") {
			return v, nil
		}
		p.i++
		r, err := p.primary()
		if err != nil {
			return pshVal{}, err
		}
		if v.isArr || r.isArr {
			return pshVal{}, fmt.Errorf("array concatenation is not modelled")
		}
		v = pshScalar(v.s + r.s)
	}
}

func (p *pshParser) expr() (pshVal, error) {
	v, err := p.additive()
	if err != nil {
		return pshVal{}, err
	}
	for {
		p.ws()
		if !strings.HasPrefix(strings.ToLower(p.s[p.i:]), "-replace") {
			return v, nil
		}
		p.i += len("-replace")
		patRaw, patDQ, ok := p.literal()
		if !ok {
			return pshVal{}, fmt.Errorf("-replace pattern is not a string literal at %q — this witness will not guess it", p.s[p.i:])
		}
		p.ws()
		if !p.peek(",") {
			return pshVal{}, fmt.Errorf("-replace: expected , after the pattern")
		}
		p.i++
		repRaw, repDQ, ok := p.literal()
		if !ok {
			return pshVal{}, fmt.Errorf("-replace replacement is not a string literal at %q", p.s[p.i:])
		}
		pat, rep := pshExpandSQ(patRaw), pshExpandSQ(repRaw)
		if patDQ {
			if pat, err = pshExpandDQ(patRaw, p.env); err != nil {
				return pshVal{}, err
			}
		}
		if repDQ {
			if rep, err = pshExpandDQ(repRaw, p.env); err != nil {
				return pshVal{}, err
			}
		}
		if v.isArr {
			return pshVal{}, fmt.Errorf("-replace over an array is not modelled")
		}
		s, err := pshApplyReplace(v.s, pat, rep)
		if err != nil {
			return pshVal{}, err
		}
		v = pshScalar(s)
	}
}

// ---------------------------------------------------------------------------
// evaluation of one arm
// ---------------------------------------------------------------------------

var (
	pshAssignRe = regexp.MustCompile(`(?s)^\s*\$(\w+)\s*=\s*(.+)$`)
	pshSSHRe    = regexp.MustCompile(`&\s*ssh\b`)
)

// pshArmResult is what one arm of one script transmits.
type pshArmResult struct {
	sshArgs   []string          // every argument string handed to `& ssh`, resolved
	hereVals  map[string]string // here-string variable -> its evaluated value
	sshCalls  int
	errs      []string
	comboDesc string
}

// pshEvalArm walks the selected statements in file order, building the environment, and
// resolves every `& ssh` argument list it meets.
func pshEvalArm(stmts []pshStmt, combo map[int]int) pshArmResult {
	res := pshArmResult{hereVals: map[string]string{}, comboDesc: fmt.Sprint(combo)}
	env := pshEnv{}
	for _, st := range stmts {
		if !pshSelected(st, combo) {
			continue
		}
		if m := pshAssignRe.FindStringSubmatch(st.text); m != nil {
			name, rhs := m[1], m[2]
			if pshSSHRe.MatchString(rhs) {
				// e.g. $RemoteOutput = & ssh ... — the ssh call is handled below.
			} else {
				p := &pshParser{s: rhs, env: env}
				v, err := p.expr()
				if err == nil {
					p.ws()
					if p.i < len(p.s) {
						err = fmt.Errorf("trailing text %q", strings.TrimSpace(p.s[p.i:]))
					}
				}
				if err != nil {
					// An unevaluable assignment is NOT fatal on its own: these scripts
					// assign paths and hashes this model has no business knowing. It
					// becomes fatal via check (c) if it was carrying the payload.
					delete(env, name)
				} else {
					env[name] = v
					if strings.HasPrefix(strings.TrimSpace(rhs), "@'") || strings.HasPrefix(strings.TrimSpace(rhs), `@"`) {
						res.hereVals[fmt.Sprintf("%s@L%d", name, st.line)] = v.s
					}
				}
			}
		}
		loc := pshSSHRe.FindStringIndex(st.head)
		if loc == nil {
			continue
		}
		res.sshCalls++
		args, err := pshSSHArgs(st.head[loc[1]:], env)
		if err != nil {
			res.errs = append(res.errs, fmt.Sprintf("line %d: %v", st.line, err))
			continue
		}
		res.sshArgs = append(res.sshArgs, args...)
	}
	return res
}

// pshSSHArgs tokenises an `& ssh` argument list and resolves each token. `@Var` splats an
// array; a bare token still interpolates variables, as PowerShell does.
func pshSSHArgs(rest string, env pshEnv) ([]string, error) {
	var out []string
	p := &pshParser{s: rest, env: env}
	for {
		p.ws()
		if p.i >= len(p.s) {
			return out, nil
		}
		if p.s[p.i] == '@' && p.i+1 < len(p.s) && pshIsIdentByte(p.s[p.i+1]) {
			j := p.i + 1
			for j < len(p.s) && pshIsIdentByte(p.s[j]) {
				j++
			}
			name := p.s[p.i+1 : j]
			p.i = j
			v, ok := env[name]
			if !ok {
				return nil, fmt.Errorf("splat @%s names a variable this witness never resolved — "+
					"the transmitted argument list cannot be reconstructed, so a green here would be meaningless", name)
			}
			if v.isArr {
				out = append(out, v.arr...)
			} else {
				out = append(out, v.s)
			}
			continue
		}
		if raw, dq, ok := p.literal(); ok {
			if !dq {
				out = append(out, pshExpandSQ(raw))
				continue
			}
			s, err := pshExpandDQ(raw, env)
			if err != nil {
				return nil, err
			}
			out = append(out, s)
			continue
		}
		j := p.i
		for j < len(p.s) && p.s[j] != ' ' && p.s[j] != '\t' {
			j++
		}
		tok := p.s[p.i:j]
		p.i = j
		s, err := pshExpandDQ(tok, env)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
}

// ---------------------------------------------------------------------------
// the witness
// ---------------------------------------------------------------------------

var pshCRLFScripts = []string{
	filepath.Join("deploy", "sever.ps1"),
	filepath.Join("deploy", "restore.ps1"),
	filepath.Join("deploy", "demo", "dc.ps1"),
	filepath.Join("deploy", "demo", "push-harness.ps1"),
}

func pshLineOf(s string, idx int) (int, string) {
	line := 1 + strings.Count(s[:idx], "\n")
	lines := strings.Split(s, "\n")
	if line-1 < len(lines) {
		return line, lines[line-1]
	}
	return line, ""
}

// TestFourDeployCRLFRiskModelStillHolds pins the fact that JUSTIFIES the normalise steps.
// If *.ps1 stops checking out as CRLF, the witness above goes quiet for a reason that has
// nothing to do with the scripts being correct.
func TestFourDeployCRLFRiskModelStillHolds(t *testing.T) {
	root := w10DcModuleRoot(t)
	b, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatalf("reading .gitattributes: %v", err)
	}
	found := false
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) >= 2 && f[0] == "*.ps1" {
			for _, a := range f[1:] {
				if a == "eol=crlf" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf(".gitattributes no longer declares `*.ps1 ... eol=crlf`. That attribute is the whole reason " +
			"sever.ps1, restore.ps1, dc.ps1 and push-harness.ps1 normalise CRLF before the payload crosses to sh: " +
			"it is what puts CR in the here-string in the first place. If it was removed deliberately the normalise " +
			"steps may now be dead weight; if it was removed by accident, checkouts have silently diverged. Either " +
			"way a human must decide — do not delete a normalise step on the strength of a green suite.")
	}
}

// TestFourCRLFWitnessCanProduceARed is the CONTROL. A zero from a method that cannot
// produce a one is not a zero. Each arm below is a defect the witness above MUST catch, or
// a correct form it MUST accept, run against a synthetic script so the real files are
// never touched.
func TestFourCRLFWitnessCanProduceARed(t *testing.T) {
	// A miniature of the sever.ps1 / restore.ps1 shape: double-quoted here-string with
	// interpolation, a normalise step, then a splatted ssh argument array.
	build := func(normalise string) string {
		return "$PveHost = \"10.0.0.32\"\r\n" +
			"$SshKey = \"k\"\r\n" +
			"$RouterVmid = 501\r\n" +
			"$SiteASubnet = \"10.90.1.0/24\"\r\n" +
			"$remoteScript = @\"\r\niptables -C FORWARD -s $SiteASubnet -j DROP || \\\r\n  iptables -I FORWARD -j DROP\r\n\"@\r\n" +
			normalise +
			"$sshArgs = @(\"-i\", $SshKey, \"root@$PveHost\", \"pct exec $RouterVmid -- sh -c '$remoteScript'\")\r\n" +
			"& ssh @sshArgs\r\n"
	}
	crIn := func(t *testing.T, src string) int {
		t.Helper()
		stmts := pshSplitStatements(src)
		arms, err := pshAnnotate(stmts)
		if err != nil {
			t.Fatalf("control could not read its own fixture: %v", err)
		}
		combos, err := pshCombos(arms)
		if err != nil {
			t.Fatalf("control: %v", err)
		}
		n := 0
		for _, c := range combos {
			res := pshEvalArm(stmts, c)
			if len(res.errs) != 0 {
				t.Fatalf("control fixture did not resolve: %v", res.errs)
			}
			if res.sshCalls == 0 {
				t.Fatal("control fixture produced no ssh call")
			}
			for _, a := range res.sshArgs {
				n += strings.Count(a, "\r")
			}
		}
		return n
	}

	t.Run("a missing normalise step is detected", func(t *testing.T) {
		if n := crIn(t, build("")); n == 0 {
			t.Fatal("CONTROL FAILED: an un-normalised CRLF payload reconstructed with NO carriage return. " +
				"The witness above is incapable of ever going red and its green means nothing.")
		}
	})

	t.Run("the real normalise step is accepted", func(t *testing.T) {
		if n := crIn(t, build("$remoteScript = $remoteScript -replace \"`r`n\", \"`n\"\r\n")); n != 0 {
			t.Fatalf("CONTROL FAILED: the normalise line the scripts actually use left %d CR in the model", n)
		}
	})

	t.Run("a correct REWRITE is accepted — the regex dialect", func(t *testing.T) {
		// Not the literal line the scripts use. `\r` is a REGEX escape, and PowerShell's
		// -replace is regex-based, so this correctly strips a bare CR. A model that treated
		// -replace as a literal replace would false-FAIL here.
		if n := crIn(t, build("$remoteScript = $remoteScript -replace \"\\r\", \"\"\r\n")); n != 0 {
			t.Fatalf("CONTROL FAILED: a CORRECT regex rewrite (-replace \"\\r\", \"\") was reported as leaving %d CR. "+
				"The witness is behaving as a proxy for one specific line, not as a test of the payload.", n)
		}
	})

	t.Run("PRESENT-BUT-NO-OP: single-quoted pattern", func(t *testing.T) {
		// PowerShell does not expand backtick escapes inside single quotes, so the pattern
		// is the four literal characters ` r ` n and matches nothing. The line is VISIBLY
		// PRESENT; a grep for it passes. The payload still carries every CR.
		if n := crIn(t, build("$remoteScript = $remoteScript -replace '`r`n', \"`n\"\r\n")); n == 0 {
			t.Fatal("CONTROL FAILED: a single-quoted -replace pattern (which expands no backtick escape and " +
				"therefore matches nothing) was scored as normalising the payload. This is the present-but-no-op " +
				"mutant the whole witness exists to catch.")
		}
	})

	t.Run("PRESENT-BUT-WORSE-THAN-NO-OP: single-quoted $_ over a bare CR", func(t *testing.T) {
		// The mutant that was a live FALSE PASS until 2026-09-09. In a SINGLE-quoted
		// replacement .NET receives `$_` intact, and `$_` is THE ENTIRE INPUT STRING — so
		// this line does not strip the CR, it substitutes a CR-bearing copy of the whole
		// payload at every CR. MEASURED in powershell.exe against the real sever.ps1
		// payload block: 9 CR in the ssh argument, 301 -> 1141 bytes.
		//
		// It is the bare-CR pattern, not the CRLF one, that makes this dangerous: the LF
		// line structure survives, so the anti-vacuity check (c) stays satisfied and only
		// check (a) can catch it. A model that scans `$_` as a group name resolves it to ""
		// and reports a perfectly clean payload.
		if n := crIn(t, build("$remoteScript = $remoteScript -replace \"`r\", '$_'\r\n")); n == 0 {
			t.Fatal("CONTROL FAILED: a single-quoted '$_' replacement was scored as REMOVING carriage returns. " +
				".NET expands $_ to the entire input string, so this line MULTIPLIES them. The model is " +
				"treating $_ as an undefined capture group — the false-PASS this control exists to pin.")
		}
	})

	t.Run("PRESENT-BUT-WORSE-THAN-NO-OP: single-quoted $_ over CRLF", func(t *testing.T) {
		// Same defect, CRLF pattern. Real PowerShell multiplies the CR here too. Before the
		// fix this one did go red, but on check (c) with a message blaming a broken payload
		// chain — the wrong diagnosis for an intact chain and a mis-modelled operator.
		if n := crIn(t, build("$remoteScript = $remoteScript -replace \"`r`n\", '$_'\r\n")); n == 0 {
			t.Fatal("CONTROL FAILED: a single-quoted '$_' replacement over CRLF was scored as normalising the payload.")
		}
	})

	t.Run("the operand-quoting asymmetry is respected: double-quoted \"$_\" is NOT $_", func(t *testing.T) {
		// The other half of the asymmetry, and the reason the fix belongs in the .NET
		// substitution stage rather than in pshExpandSQ. PowerShell interpolates `"$_"`
		// itself — it is an undefined variable outside a pipeline — so .NET receives an
		// EMPTY replacement and every CRLF is deleted. MEASURED: 0 CR, output "AABB".
		// A model that applied .NET's $_ rule to both operands would report CR here and
		// red a script that real PowerShell leaves CR-free: a false FAIL.
		//
		// READ THIS WITH THE ARM BELOW. It is the BARE `$` that PowerShell consumes, not the
		// double quotes. Do not generalise this arm into "a double-quoted operand is always
		// safe" — the backtick-escaped spelling is not, and the next arm exists to stop that
		// generalisation being turned into code.
		if n := crIn(t, build("$remoteScript = $remoteScript -replace \"`r`n\", \"$_\"\r\n")); n != 0 {
			t.Fatalf("CONTROL FAILED: a DOUBLE-quoted \"$_\" replacement left %d CR in the model. PowerShell "+
				"interpolates it to the empty string before .NET ever sees it, so no CR can survive. The model "+
				"is applying .NET substitution semantics to an operand PowerShell already consumed.", n)
		}
	})

	t.Run("a BACKTICK-ESCAPED `$_ in a DOUBLE-quoted operand still reaches .NET live", func(t *testing.T) {
		// THE SECOND FALSE-PASS FORM — closed by the $_ fix as a side effect, and unpinned
		// until this arm. A backtick stops PowerShell interpolating, so a DOUBLE-quoted
		// "`$_" survives its pass as a literal $_ and .NET applies the whole-input rule.
		// This line therefore MULTIPLIES carriage returns exactly as '$_' does, while
		// reading like a normalise step and sitting inside the quoting style the arm above
		// certifies as safe.
		//
		// MEASURED in powershell.exe 2026-09-09, input "AA`r`nBB" (one CR):
		//   -replace "`r`n", "$_"   ->  AABB                0 CR  (interpolated away)
		//   -replace "`r`n", "`$_"  ->  AAAA CRLF BBBB      1 CR  (escaped: .NET expanded it)
		//   -replace "`r",   "`$_"  ->  AAAA CRLF BB LF BB  1 CR
		// MEASURED against this model: 0 CR at e812ce3 — a LIVE FALSE PASS — and 1 CR at
		// 6b10b89.
		//
		// The BARE-CR pattern is used deliberately, for the same reason as the single-quoted
		// arm above: it leaves the LF line structure intact, so the anti-vacuity check (c)
		// stays satisfied and ONLY check (a) can catch it. A regression that stopped running
		// .NET substitution on double-quoted operands — the obvious "simplification" to make
		// after reading the arm above — would leave every other arm in this file green.
		if n := crIn(t, build("$remoteScript = $remoteScript -replace \"`r\", \"`$_\"\r\n")); n == 0 {
			t.Fatal("CONTROL FAILED: a backtick-escaped \"`$_\" in a DOUBLE-quoted replacement was scored as " +
				"REMOVING carriage returns. The backtick stops PowerShell interpolating, so .NET receives a live " +
				"$_ — the entire input string — and this line multiplies CR instead of stripping it. The model is " +
				"treating the quoting style alone as proof that the $ was already consumed.")
		}
	})

	t.Run("$$ still escapes a literal dollar", func(t *testing.T) {
		// Guards the new $_ arm against over-reach. MEASURED: '$$_' -> the two literal
		// characters $_ , no substitution, 0 CR.
		if n := crIn(t, build("$remoteScript = $remoteScript -replace \"`r`n\", '$$_'\r\n")); n != 0 {
			t.Fatalf("CONTROL FAILED: '$$_' left %d CR. $$ is an escaped literal dollar, so the trailing _ is "+
				"literal too and nothing is substituted — the $_ arm has swallowed an escaped dollar.", n)
		}
	})

	t.Run("PRESENT-BUT-NO-OP: CRLF replaced by CRLF", func(t *testing.T) {
		if n := crIn(t, build("$remoteScript = $remoteScript -replace \"`r`n\", \"`r`n\"\r\n")); n == 0 {
			t.Fatal("CONTROL FAILED: a CRLF -> CRLF replacement was scored as normalising the payload.")
		}
	})

	t.Run("a renamed, never-assigned payload variable does not resolve silently", func(t *testing.T) {
		// The dangerous mutant: the normalise line is deleted and the transmitted string
		// names a variable that was never assigned. PowerShell would send an empty payload;
		// a naive model would report zero CR and PASS. Check (c) must see the here-string
		// failing to reach ssh.
		src := "$remoteScript = @'\r\nset -e\r\necho hi\r\n'@\r\n" +
			"$sshArgs = @(\"-i\", \"k\", \"root@h\", \"pct exec 501 -- sh -c '$remoteScriptNormalized'\")\r\n" +
			"& ssh @sshArgs\r\n"
		stmts := pshSplitStatements(src)
		arms, err := pshAnnotate(stmts)
		if err != nil {
			t.Fatalf("control could not read its own fixture: %v", err)
		}
		combos, _ := pshCombos(arms)
		res := pshEvalArm(stmts, combos[0])
		joined := strings.ReplaceAll(strings.Join(res.sshArgs, "\x00"), "\r\n", "\n")
		for _, v := range res.hereVals {
			if strings.Contains(joined, strings.ReplaceAll(v, "\r\n", "\n")) {
				t.Fatal("CONTROL FAILED: a here-string that never reaches ssh was scored as reaching it. Check (c) " +
					"cannot detect a deleted normalise step that renamed the transmitted variable, and that mutant " +
					"would reconstruct as \"\" and PASS the no-CR assertion — a live false-green.")
			}
		}
		if strings.Count(joined, "\r") != 0 {
			t.Fatal("control fixture unexpectedly carried CR; the check above proved nothing")
		}
	})

	t.Run("both if-arms are enumerated", func(t *testing.T) {
		// dc.ps1's shape: a common stage plus a branch-selected place. Only the SECOND arm
		// is left un-normalised. A model that evaluated one arm would miss it.
		src := "$stage = @'\r\nset -e\r\necho staged\r\n'@\r\n" +
			"if ($On -eq 'post') {\r\n" +
			"$place = @'\r\necho post\r\n'@\r\n" +
			"} else {\r\n" +
			"$place = @'\r\necho lab\r\n'@\r\n" +
			"}\r\n" +
			"$stage = ($stage + \"`n\" + $place)\r\n" +
			"& ssh -i $k \"root@$h\" \"$stage\"\r\n"
		stmts := pshSplitStatements(src)
		arms, err := pshAnnotate(stmts)
		if err != nil {
			t.Fatalf("control could not read its own fixture: %v", err)
		}
		combos, err := pshCombos(arms)
		if err != nil {
			t.Fatalf("control: %v", err)
		}
		seen := map[string]bool{}
		for _, c := range combos {
			res := pshEvalArm(stmts, c)
			for _, a := range res.sshArgs {
				if strings.Contains(a, "echo post") {
					seen["post"] = true
				}
				if strings.Contains(a, "echo lab") {
					seen["lab"] = true
				}
			}
		}
		if !seen["post"] || !seen["lab"] {
			t.Fatalf("CONTROL FAILED: arm enumeration reached post=%v lab=%v — dc.ps1 has TWO payload branches and "+
				"a witness that evaluates one of them has ASSUMED the other.", seen["post"], seen["lab"])
		}
	})
}
