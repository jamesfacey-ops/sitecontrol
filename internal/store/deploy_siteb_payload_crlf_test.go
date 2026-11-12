// deploy_siteb_payload_crlf_test.go — the site-b deploy scripts must not ship a
// carriage return into the container's shell.
//
// THE PROPERTY THIS TESTS IS THE VALUE, NOT THE LINE. deploy/wipe-replay-siteb.ps1
// and deploy/replay-epoch-unchanged-siteb.ps1 both build their remote payload as a
// single-quoted here-string, normalise CRLF to LF, base64-encode the result, and
// transmit `pct exec <vmid> -- sh -c 'echo <b64> | base64 -d | sh'`. The base64 hop
// exists for a DIFFERENT defect (PowerShell re-tokenises a native-command argument
// that contains a double quote and silently drops every quote — see each script's own
// comment). The NORMALISE hop exists for this one, and it is independently
// load-bearing:
//
//	.gitattributes carries `*.ps1 text eol=crlf`, so these files check out with CRLF
//	terminators and the here-string body genuinely contains CR. MEASURED 2026-09-08 at
//	integration/witness-2026-09-05 @ 5a20b070, on a fresh worktree: the raw byte span
//	between the @' and '@ delimiters holds 18 CR in replay-epoch-unchanged-siteb.ps1
//	and 20 in wipe-replay-siteb.ps1; the here-string VALUE PowerShell actually builds
//	holds 16 and 18, two fewer each, because the line break after @' and the one before
//	'@ are delimiters and not content. 16 and 18 are what this witness reports, and the
//	two counts reconciling is itself a check that the reconstruction is faithful.
//	Base64 is faithful too — it carries those CRs to the container intact. The payload's
//	FIRST line is `set -e`; with a CR it becomes `set` with the argument "-e\r", which
//	is VALID SYNTAX that fails only at RUNTIME. That is the documented dc.ps1 /
//	sever.ps1 mode.
//
// WHY `bash -n` CANNOT STAND IN FOR THIS. A control run (another lane, 2026-09-08) fed
// `bash -n` the UN-normalised CRLF form of both payloads and BOTH PARSED, exit 0. A
// syntax check proves well-formedness and is structurally incapable of detecting a
// trailing CR that misbehaves only when the shell executes it.
//
// WHY THIS IS NOT A GREP FOR THE NORMALISE LINE. A test matching that line's literal
// text would be a PROXY: it would pass on a line that is present but broken, and fail
// on a correct rewrite. Instead this reconstructs the payload the script would actually
// transmit — resolving the here-string and the -replace chain that feeds
// [Convert]::ToBase64String — and asserts no CR survives into it.
//
// HOW `-replace` IS MODELLED, AND WHAT THAT MODEL STILL CANNOT FOLLOW.
// crlfTransmittedPayload models PowerShell's `-replace` as the REGEX operator it actually
// is. Each operand is FIRST resolved as the double-quoted PowerShell string it really is,
// by crlfExpandDQ — backtick escapes AND $Var interpolation — because PowerShell substitutes
// its own variables before .NET ever sees the string. Only what survives that is treated as
// regex: the pattern is compiled with Go's regexp (RE2) and the replacement is translated
// from .NET substitution syntax ($&, $1, ${name}, $$) into Go's Expand syntax by
// crlfTranslateReplacement, which ERRORS on any substitution it does not model rather than
// guessing. It did NOT always do either, and each missing piece was a live false-PASS:
//
//	THE FALSE GREEN THAT PROMPTED THIS. MEASURED 2026-09-08 in a private worktree off
//	fix/siteb-dialect-comment @ 6b4e475, against a real CRLF checkout of
//	wipe-replay-siteb.ps1 (66 CR on disk, 18 in the here-string value): with the normalise
//	line rewritten to `-replace "`r`n", "$&"`, the literal model reconstructed a payload
//	holding 0 CR and ⟦was: TestSitebDeployPayloadCarriesNoCarriageReturn — not in the public repository⟧ printed `--- PASS`, the
//	package `ok`. Real PowerShell substitutes the whole match back and leaves EVERY CR in
//	place, so the witness certified a script that ships raw CR to the container. Under the
//	regex model the same mutant reds, because ${0} restores the CRLF the pattern matched.
//
//	THE FALSE RED. `-replace "\r", ""` is CORRECT PowerShell — run in powershell.exe
//	against a CRLF here-string it produces output byte-identical to the line it replaces,
//	0 CR out. The literal model unescaped "\r" to backslash-then-r, found no such
//	substring, and reported all 18 CR as surviving. Compiled as a regex, `\r` matches CR
//	and the rewrite is accepted — which is what "test the value, not the line" requires,
//	and why refusing every regex metacharacter was rejected as the fix: it would have
//	turned this witness back into a proxy for one specific dialect.
//
//	THE SECOND FALSE GREEN, ONE DOOR OVER — found in review, closed here. The revision
//	above still parsed only DOUBLE-quoted operands and still expanded only backtick
//	escapes, so it never ran PowerShell's OWN $Var interpolation and handed every surviving
//	$name to .NET semantics that PowerShell does not deliver in that quoting. MEASURED
//	2026-09-09 in powershell.exe 5.1, input a<CR><LF>b, checked byte by byte:
//
//		-replace "`r(`n)", "$1"   -> "ab"           CR=0 LF=0  THE TERMINATOR IS DELETED
//		-replace "`r(`n)", '$1'   -> "a<LF>b"       CR=0 LF=1  single quotes: .NET semantics
//		-replace "`r`n",   "$_"   -> "ab"           CR=0 LF=0  $_ interpolates to EMPTY
//		-replace "`r`n",   "`$_"  -> "aa<CR><LF>bb" escaped: .NET, the ENTIRE INPUT
//		-replace "`r`n",   "$&"   -> "a<CR><LF>b"   & is not an identifier byte, so $ is kept
//
//	Planted into wipe-replay-siteb.ps1 as -replace "`r(`n)", "$1", the previous revision
//	printed `--- PASS` / `ok` while the real payload collapses to ONE LINE. Arm (d) could
//	not save it, because the MODEL kept the LF that reality removes and the two line counts
//	agreed. Writing a group reference in double quotes rather than single is the most common
//	-replace mistake there is, so this was never an exotic case. crlfExpandDQ now resolves
//	BOTH operands as PowerShell strings before either is treated as regex.
//
// THE PLAUSIBILITY ASSERTION (arm (d)), AND WHY IT IS AN INVARIANT AND NOT A PROXY.
// The $& mutant did not merely hide CR. It reconstructed the entire 807-byte payload as a
// SINGLE LINE holding 0 CR *and* 0 LF, beginning `set -e$&systemctl stop sitectl$&...`
// (MEASURED, same worktree). Nothing checked that the reconstruction was still a shell
// script, so a self-evidently impossible payload satisfied the no-CR assertion. Arm (d)
// closes that whole class INDEPENDENTLY of the dialect question, by asserting the one
// property no correct CRLF normalisation can violate: rewriting line TERMINATORS cannot
// change the NUMBER of lines, so the payload must hold exactly as many lines as the
// here-string body on disk. That is an invariant of the transformation itself, not a guess
// about the payload's content: it holds for `-replace "`r`n", "`n"` (MEASURED: 17 body
// lines, 17 payload lines), for `-replace "\r", ""`, and for any future rewrite that is
// genuinely a line-ending normalisation. It reds if a step ADDS or REMOVES lines — which is
// no longer a normalisation, and is owed a human read rather than a silent green.
//
// WHAT THIS MODEL STILL CANNOT FOLLOW. It does NOT claim to err only red:
//
//	FAIL-CLOSED — errors, never a silent green. A pattern RE2 rejects: .NET backreferences
//	(\1), lookaround ((?=, (?<=), named groups ((?<n>...)). An operand that interpolates a
//	variable this witness never saw assigned, or $( ... ), or a scoped $env:X — reported,
//	never guessed as empty, because an interpolated value is inserted VERBATIM and could
//	itself carry a CR. A replacement still holding an unmodelled .NET substitution once
//	interpolation is done: $+, $` and $', and $_ — the last reachable ONLY as a
//	backtick-escaped `$_, which in that form really does mean the ENTIRE INPUT (MEASURED
//	above); a bare $_ never gets that far. A normalise written with SINGLE-quoted operands is
//	not matched by crlfReplace at all, so the base64 variable is never assigned and the
//	ToBase64String lookup errors; single quotes also suppress backtick expansion in
//	PowerShell, making such a line a live no-op defect in its own right.
//
//	ERRS RED — over-reports CR — wherever RE2 matches LESS than .NET. PowerShell's
//	`-replace` is CASE-INSENSITIVE by default while Go's regexp is case-sensitive; \d \w \s
//	are Unicode-aware in .NET and ASCII-only in Go; .NET's `$` also matches before a final
//	newline. In each the script strips more than the model believes, so the red is a
//	candidate artefact to read by hand, not a missed defect.
//
//	ERRS GREEN — the residue, and it is NOT proven empty. A construct RE2 matches MORE
//	broadly than .NET would let the model strip CR that the script actually leaves behind.
//	POSIX classes ([[:alpha:]]) are one: Go honours them, .NET reads them as an ordinary
//	character set. No such construct appears in either script today. The OTHER errs-green
//	route that used to sit here — a $name read with .NET semantics where PowerShell resolves
//	it first — is CLOSED by crlfExpandDQ. Arm (d) is a second net under the part of this
//	class that CHANGES THE LINE COUNT, but not under corruption that preserves it: replacing
//	each terminator with LF-plus-a-character keeps every LF, drops every CR, and passes both
//	arms. The earlier claim that this model "can only err red" is not restated here: it was
//	not true then, it is not provable now, and it has since been falsified twice.
//
// wipe-replay-siteb.ps1 is demo-walkthrough step 6.7, the DESTRUCTIVE stage-2 script.
package store_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The subset of PowerShell these two scripts use between the here-string and the
// base64 call. Deliberately tiny: anything outside it is reported as a structural
// change for a human to read, never silently tolerated.
var (
	crlfHereString = regexp.MustCompile(`(?s)\$(\w+)\s*=\s*@'\r?\n(.*?)\r?\n'@`)
	crlfReplace    = regexp.MustCompile(`(?m)^\s*\$(\w+)\s*=\s*\$(\w+)\s+-replace\s+"([^"]*)"\s*,\s*"([^"]*)"\s*\r?$`)
	crlfToBase64   = regexp.MustCompile(`\$(\w+)\s*=\s*\[Convert\]::ToBase64String\(\[System\.Text\.Encoding\]::UTF8\.GetBytes\(\$(\w+)\)\)`)
)

// crlfExpandDQ resolves a DOUBLE-QUOTED PowerShell operand to the string value PowerShell
// would actually hand to .NET. It does two things in ONE left-to-right pass, and the single
// pass is the point: backtick escapes (backtick, not backslash — `r is CR, `n is LF) AND
// PowerShell's own $Var / ${Var} interpolation, so a backtick-escaped `$ is correctly NOT
// treated as an interpolation.
//
// WHY IT EXISTS. Expanding only the backtick escapes left a live false-PASS: PowerShell
// resolves its own variables BEFORE .NET ever sees the string, so a group reference written
// in DOUBLE quotes is an undefined VARIABLE, not a substitution. See the file header for the
// powershell.exe measurements.
//
// AN UNRESOLVABLE NAME IS AN ERROR, NOT THE EMPTY STRING. This is a deliberate divergence
// from deploy_four_payload_crlf_test.go's pshExpandDQ, which expands an undefined variable
// to "" because that is what PowerShell does. That file can afford fidelity: it evaluates
// the whole script, so its environment is nearly complete and a name missing from it really
// is undefined. THIS file's environment holds only the here-string and the results of the
// -replace chain, so a name it cannot see is far more likely to be one the script genuinely
// defines. Guessing "" there is a false-GREEN route, because an interpolated value is
// inserted VERBATIM (MEASURED 2026-09-09, powershell.exe: with $cr holding a CR,
// "foo" -replace "foo", "$cr" yields CR=1): a variable whose real value carries a CR, used in
// a pattern spanning no line terminator, would put a CR in the payload the model never sees
// while the line count — and therefore arm (d) — stays unchanged. Erring RED closes that.
func crlfExpandDQ(s string, env map[string]string) (string, error) {
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
			return "", fmt.Errorf("operand %q contains a subexpression $( ... ), which this witness does not model", s)
		}
		var name string
		if s[i+1] == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				return "", fmt.Errorf("operand %q has an unterminated ${ ... }", s)
			}
			name = s[i+2 : i+2+end]
			i = i + 2 + end
		} else {
			j := i + 1
			for j < len(s) && crlfIsIdentByte(s[j]) {
				j++
			}
			if j == i+1 {
				// $ followed by nothing identifier-like is a literal $, e.g. the .NET
				// substitution $& — PowerShell leaves it alone and .NET receives it.
				b.WriteByte(c)
				continue
			}
			if j < len(s) && s[j] == ':' {
				return "", fmt.Errorf("operand %q reads the scoped variable $%s:..., whose value this witness cannot know", s, s[i+1:j])
			}
			name = s[i+1 : j]
			i = j - 1
		}
		val, ok := env[name]
		if !ok {
			if crlfAllDigits(name) {
				return "", fmt.Errorf("operand %q writes $%s inside DOUBLE quotes. PowerShell interpolates that as a "+
					"VARIABLE before .NET ever sees the string, and an undefined one expands to the EMPTY string — so "+
					"this DELETES each match instead of substituting capture group %s, collapsing the payload onto one "+
					"line. Write it in SINGLE quotes to mean the group. This is the most common -replace mistake there is", s, name, name)
			}
			return "", fmt.Errorf("operand %q interpolates $%s, which this witness never saw assigned. An interpolated "+
				"value is inserted verbatim and could itself carry a CR, so this is reported rather than guessed as empty", s, name)
		}
		b.WriteString(val)
	}
	return b.String(), nil
}

// crlfAllDigits reports whether name is a non-empty run of ASCII digits — i.e. whether it
// looks like a .NET capture-group reference written in the quoting where PowerShell eats it.
func crlfAllDigits(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		if name[i] < '0' || name[i] > '9' {
			return false
		}
	}
	return true
}

// crlfIsIdentByte reports whether c may appear in a .NET substitution group name.
func crlfIsIdentByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// crlfTranslateReplacement rewrites a .NET substitution string into Go's Expand syntax.
// Anything it does not recognise is an ERROR, never a guess: an unmodelled substitution
// that silently expanded to nothing would understate what survives into the payload, which
// is precisely the false-green this model was widened to close.
func crlfTranslateReplacement(rep string) (string, error) {
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
		case crlfIsIdentByte(rep[i]):
			j := i
			for j < len(rep) && crlfIsIdentByte(rep[j]) {
				j++
			}
			if name := rep[i:j]; name == "_" {
				return "", fmt.Errorf("replacement %q still holds $_ after interpolation, which only a "+
					"BACKTICK-ESCAPED `$_ can do — a bare $_ is interpolated away first. In that escaped form it IS "+
					"the .NET substitution for the ENTIRE INPUT string (MEASURED in powershell.exe: a<CR><LF>b with "+
					"that replacement yields aa<CR><LF>bb). Go would read it as a capture group named _, find none, "+
					"and expand it to nothing — understating what survives. Re-derive this payload by hand", rep)
			}
			b.WriteString("${" + rep[i:j] + "}")
			i = j - 1
		default:
			return "", fmt.Errorf("replacement %q uses the .NET substitution $%c, which this witness does not model", rep, rep[i])
		}
	}
	return b.String(), nil
}

// crlfApplyReplace performs one -replace. The pattern is compiled as a REGEX, because
// PowerShell's -replace IS a regex operator; modelling it as a literal replace both
// false-FAILED a correct rewrite such as -replace "\r", "" and false-PASSED the
// substitution mutant -replace "`r`n", "$&". See the header for both measurements.
func crlfApplyReplace(input, pat, rep string) (string, error) {
	re, err := regexp.Compile(pat)
	if err != nil {
		return "", fmt.Errorf("-replace pattern %q does not compile under Go's regexp (RE2): %v — "+
			".NET backreferences, lookaround and (?<name>...) are outside this witness's dialect; "+
			"re-derive the payload by hand rather than trusting a green", pat, err)
	}
	if !strings.Contains(rep, "$") {
		return re.ReplaceAllLiteralString(input, rep), nil
	}
	goRep, err := crlfTranslateReplacement(rep)
	if err != nil {
		return "", err
	}
	return re.ReplaceAllString(input, goRep), nil
}

// crlfCountLines counts the lines in s the way arm (d) compares them: terminators are
// counted, so a body and a payload that differ ONLY in which terminator they use agree.
func crlfCountLines(s string) int { return strings.Count(s, "\n") + 1 }

// crlfTransmittedPayload reconstructs the bytes the container's shell would execute:
// the here-string as it sits on disk, pushed through the -replace chain, resolved at
// the [Convert]::ToBase64String call site. It returns the payload, the name of the
// base64 variable, and an error describing any structure it could not follow.
//
// An UNRESOLVABLE variable is an ERROR, never an empty payload — that is the direction
// that would otherwise turn a deleted normalise step into a vacuous pass.
func crlfTransmittedPayload(src string) (payload string, b64Var string, err error) {
	env := map[string]string{}
	hs := crlfHereString.FindStringSubmatch(src)
	if hs == nil {
		return "", "", fmt.Errorf("no single-quoted here-string assignment ($X = @'...'@) found — the payload construction has changed shape and this witness can no longer follow it")
	}
	env[hs[1]] = hs[2]

	for _, m := range crlfReplace.FindAllStringSubmatch(src, -1) {
		dst, srcVar := m[1], m[2]
		cur, ok := env[srcVar]
		if !ok {
			return "", "", fmt.Errorf("assignment $%s = $%s -replace ... reads $%s, which this witness never saw assigned", dst, srcVar, srcVar)
		}
		// BOTH operands are resolved as PowerShell double-quoted strings FIRST. Skipping
		// this is what let a double-quoted group reference read as a .NET substitution.
		pat, perr := crlfExpandDQ(m[3], env)
		if perr != nil {
			return "", "", fmt.Errorf("assignment $%s = $%s -replace ...: pattern: %v", dst, srcVar, perr)
		}
		rep, rerr := crlfExpandDQ(m[4], env)
		if rerr != nil {
			return "", "", fmt.Errorf("assignment $%s = $%s -replace ...: replacement: %v", dst, srcVar, rerr)
		}
		next, aerr := crlfApplyReplace(cur, pat, rep)
		if aerr != nil {
			return "", "", fmt.Errorf("assignment $%s = $%s -replace %q, %q: %v", dst, srcVar, m[3], m[4], aerr)
		}
		env[dst] = next
	}

	b64 := crlfToBase64.FindStringSubmatch(src)
	if b64 == nil {
		return "", "", fmt.Errorf("no [Convert]::ToBase64String([System.Text.Encoding]::UTF8.GetBytes($X)) call found — the payload is no longer base64-encoded, so BOTH the quote-dropping fix and the CRLF fix need re-deriving by hand")
	}
	val, ok := env[b64[2]]
	if !ok {
		return "", "", fmt.Errorf("ToBase64String encodes $%s, which is never assigned from the here-string by any -replace this witness can follow — if the normalise step was DELETED, this is that deletion", b64[2])
	}
	return val, b64[1], nil
}

var crlfSitebScripts = []string{
	filepath.Join("deploy", "wipe-replay-siteb.ps1"),
	filepath.Join("deploy", "replay-epoch-unchanged-siteb.ps1"),
}

func crlfPayloadLine(payload string, n int) string {
	lines := strings.Split(payload, "\n")
	if n-1 < len(lines) {
		return lines[n-1]
	}
	return ""
}

// TestSitebDeployCRLFRiskModelStillHolds pins the fact that JUSTIFIES the normalise
// step. If *.ps1 stops checking out as CRLF, the witness above goes quiet for a reason
// that has nothing to do with the scripts being correct.
func TestSitebDeployCRLFRiskModelStillHolds(t *testing.T) {
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
			"the site-b deploy scripts normalise CRLF before base64: it is what puts CR in the here-string in " +
			"the first place. If it was removed deliberately the normalise step may now be dead weight; if it " +
			"was removed by accident, checkouts have silently diverged. Either way a human must decide — do not " +
			"delete the normalise step on the strength of a green suite.")
	}
}

// TestSitebCRLFWitnessCanProduceARed is the CONTROL. A zero from a method that cannot
// produce a one is not a zero. Arm 1 proves the reconstruction DETECTS a missing
// normalise step; arm 2 proves it accepts a CORRECT REWRITE (so it tests the value, not
// the literal line); arm 3 proves a DELETED step errors rather than resolving to "".
func TestSitebCRLFWitnessCanProduceARed(t *testing.T) {
	const body = "set -e\r\nsystemctl stop sitectl\r\necho done\r\n"
	tail := "$b64 = [Convert]::ToBase64String([System.Text.Encoding]::UTF8.GetBytes($%s))\n" +
		"$sshArgs = @(\"-i\", $k, \"root@h\", \"pct exec 503 -- sh -c 'echo $b64 | base64 -d | sh'\")\n"

	t.Run("missing normalise step is detected", func(t *testing.T) {
		src := "$remoteScript = @'\r\n" + body + "'@\r\n" + fmt.Sprintf(tail, "remoteScript")
		payload, _, err := crlfTransmittedPayload(src)
		if err != nil {
			t.Fatalf("control could not reconstruct its own fixture: %v", err)
		}
		if !strings.Contains(payload, "\r") {
			t.Fatal("CONTROL FAILED: an un-normalised CRLF payload reconstructed with NO carriage return. " +
				"The witness above is incapable of ever going red and its green means nothing.")
		}
	})

	t.Run("a correct rewrite is accepted", func(t *testing.T) {
		// Deliberately NOT the literal line the scripts use: this strips a bare CR. A
		// proxy test that grepped for the exact -replace text would false-red here.
		src := "$remoteScript = @'\r\n" + body + "'@\r\n" +
			"$norm = $remoteScript -replace \"`r\", \"\"\n" + fmt.Sprintf(tail, "norm")
		payload, _, err := crlfTransmittedPayload(src)
		if err != nil {
			t.Fatalf("control could not reconstruct its own fixture: %v", err)
		}
		if strings.Contains(payload, "\r") {
			t.Fatal("CONTROL FAILED: a CORRECT but differently-written normalise step was reported as leaving " +
				"a CR. The witness is behaving as a proxy for one specific line, not as a test of the payload.")
		}
	})

	t.Run("a deleted normalise step does not resolve silently", func(t *testing.T) {
		// The dangerous mutant: the -replace line is deleted but the base64 call still
		// names the normalised variable. This must ERROR, not yield an empty payload.
		src := "$remoteScript = @'\r\n" + body + "'@\r\n" + fmt.Sprintf(tail, "remoteScriptNormalized")
		if _, _, err := crlfTransmittedPayload(src); err == nil {
			t.Fatal("CONTROL FAILED: an unresolvable payload variable produced no error. A deleted normalise " +
				"step would reconstruct as the empty string and PASS the no-CR assertion — a live false-green.")
		}
	})

	t.Run("a $& substitution is not mistaken for two literal characters", func(t *testing.T) {
		// THE MUTANT THIS LANE EXISTS FOR. $& is the WHOLE MATCH, so real PowerShell puts
		// the CRLF straight back and every CR survives. A literal-replace model reconstructs
		// 0 CR here and the witness above prints ok on a script that ships raw CR.
		src := "$remoteScript = @'\r\n" + body + "'@\r\n" +
			"$norm = $remoteScript -replace \"`r`n\", \"$&\"\n" + fmt.Sprintf(tail, "norm")
		payload, _, err := crlfTransmittedPayload(src)
		if err != nil {
			t.Fatalf("control could not reconstruct its own fixture: %v", err)
		}
		if !strings.Contains(payload, "\r") {
			t.Fatal("CONTROL FAILED: -replace \"`r`n\", \"$&\" reconstructed with NO carriage return. $& substitutes " +
				"the whole match, so real PowerShell leaves every CR in place. A model that reads it as two " +
				"ordinary characters certifies a script that ships raw CR to the container — the live false-PASS.")
		}
	})

	t.Run("a regex-dialect normalise is accepted", func(t *testing.T) {
		// -replace "\r", "" is CORRECT PowerShell, measured byte-identical to the line it
		// replaces. A literal model unescapes "\r" to backslash-then-r, matches nothing, and
		// reds a working script.
		src := "$remoteScript = @'\r\n" + body + "'@\r\n" +
			"$norm = $remoteScript -replace \"\\r\", \"\"\n" + fmt.Sprintf(tail, "norm")
		payload, _, err := crlfTransmittedPayload(src)
		if err != nil {
			t.Fatalf("control could not reconstruct its own fixture: %v", err)
		}
		if strings.Contains(payload, "\r") {
			t.Fatal("CONTROL FAILED: the regex normalise -replace \"\\r\", \"\" was reported as leaving a CR. The " +
				"pattern is a REGEX, not a literal — this is the false RED the widened model exists to remove.")
		}
	})

	t.Run("an unmodelled substitution errors rather than guessing", func(t *testing.T) {
		// `$_ survives interpolation as a literal $_, and in THAT form it is the .NET
		// substitution for the ENTIRE INPUT string (MEASURED: -replace "`r`n", "`$_" on
		// a<CR><LF>b yields aa<CR><LF>bb). A bare $_ never reaches the translator at all.
		src := "$remoteScript = @'\r\n" + body + "'@\r\n" +
			"$norm = $remoteScript -replace \"`r`n\", \"`$_\"\n" + fmt.Sprintf(tail, "norm")
		if _, _, err := crlfTransmittedPayload(src); err == nil {
			t.Fatal("CONTROL FAILED: the .NET substitution $_ — the ENTIRE INPUT string — produced no error. Go " +
				"reads $_ as a capture group named _, finds none, and expands it to nothing, understating what " +
				"survives into the payload. An unmodelled substitution must fail closed, not guess.")
		}
	})

	t.Run("a line-collapsing replacement is caught by the plausibility invariant", func(t *testing.T) {
		// The shape the $& mutant produced under the OLD model: every terminator replaced by
		// an ordinary character, leaving one long line holding 0 CR. It PASSES the no-CR arm,
		// so only arm (d) can catch it. This control proves arm (d)'s comparison can differ.
		src := "$remoteScript = @'\r\n" + body + "'@\r\n" +
			"$norm = $remoteScript -replace \"`r`n\", \"X\"\n" + fmt.Sprintf(tail, "norm")
		payload, _, err := crlfTransmittedPayload(src)
		if err != nil {
			t.Fatalf("control could not reconstruct its own fixture: %v", err)
		}
		if strings.Contains(payload, "\r") {
			t.Fatal("control fixture is not the one intended: it still holds a CR, so arm (a) would catch it and " +
				"this control would prove nothing about arm (d)")
		}
		hs := crlfHereString.FindStringSubmatch(src)
		if hs == nil {
			t.Fatal("control could not re-find its own here-string")
		}
		if crlfCountLines(hs[2]) == crlfCountLines(payload) {
			t.Fatal("CONTROL FAILED: a payload whose line terminators were ALL replaced by an ordinary character " +
				"kept its line count. Arm (d) in the witness above therefore cannot detect the single-line " +
				"collapse, and its green means nothing.")
		}
	})

	t.Run("a double-quoted group reference is not read as a .NET substitution", func(t *testing.T) {
		// THE REVIEW FINDING. PowerShell interpolates $1 as an undefined VARIABLE before .NET
		// sees the string, so this DELETES every terminator (MEASURED in powershell.exe:
		// "a<CR><LF>b" -replace "`r(`n)", "$1" yields "ab", 0 CR and 0 LF). A model that reads
		// it as capture group 1 reconstructs "a<LF>b", keeps the LF reality removes, and BOTH
		// arm (a) and arm (d) go quiet — the payload really does collapse to one line.
		src := "$remoteScript = @'\r\n" + body + "'@\r\n" +
			"$norm = $remoteScript -replace \"`r(`n)\", \"$1\"\n" + fmt.Sprintf(tail, "norm")
		payload, _, err := crlfTransmittedPayload(src)
		if err == nil {
			t.Fatalf("CONTROL FAILED: a DOUBLE-quoted $1 was accepted, reconstructing %d line(s) with %d CR. "+
				"PowerShell interpolates it to the empty string and deletes every terminator, so the real payload "+
				"is one long line. Reading it as a .NET capture group is the false-PASS this control holds shut.",
				crlfCountLines(payload), strings.Count(payload, "\r"))
		}
	})

	t.Run("a backtick-escaped group reference IS a .NET substitution", func(t *testing.T) {
		// The other half of the same fact, and the reason this is not solved by banning $:
		// `$1 survives interpolation as a literal $1, so .NET does substitute capture group 1
		// (MEASURED: -replace "`r(`n)", "`$1" on a<CR><LF>b yields "a<LF>b"). The model must
		// FOLLOW this rewrite, not refuse it — it is correct PowerShell.
		src := "$remoteScript = @'\r\n" + body + "'@\r\n" +
			"$norm = $remoteScript -replace \"`r(`n)\", \"`$1\"\n" + fmt.Sprintf(tail, "norm")
		payload, _, err := crlfTransmittedPayload(src)
		if err != nil {
			t.Fatalf("control could not reconstruct its own fixture: %v", err)
		}
		if strings.Contains(payload, "\r") {
			t.Fatal("CONTROL FAILED: `$1, with the group capturing the LF, left a CR behind. The backtick escapes " +
				"the dollar, so .NET receives the substitution and this rewrite is CORRECT PowerShell.")
		}
		// Compare against the here-string body AS CAPTURED, the way arm (d) does: the
		// closing delimiter consumes the body's final terminator, so counting from the
		// `body` constant itself overstates by one line.
		hs := crlfHereString.FindStringSubmatch(src)
		if hs == nil {
			t.Fatal("control could not re-find its own here-string")
		}
		if want, got := crlfCountLines(hs[2]), crlfCountLines(payload); want != got {
			t.Fatalf("CONTROL FAILED: `$1 changed the line count (%d -> %d). It substitutes the captured LF back, "+
				"so it must preserve every line.", want, got)
		}
	})

	t.Run("an operand interpolating an unknown variable is reported, not guessed", func(t *testing.T) {
		// An interpolated value is inserted VERBATIM and could carry a CR, so a name this
		// witness never saw assigned is reported. Guessing "" would hide exactly that CR while
		// leaving the line count — and so arm (d) — untouched.
		src := "$remoteScript = @'\r\n" + body + "'@\r\n" +
			"$norm = $remoteScript -replace \"`r`n\", \"$lineEnding\"\n" + fmt.Sprintf(tail, "norm")
		if _, _, err := crlfTransmittedPayload(src); err == nil {
			t.Fatal("CONTROL FAILED: an operand interpolating $lineEnding, which is never assigned, was accepted. " +
				"Its real value could carry a CR, so guessing the empty string for it is a false-green route.")
		}
	})
}
