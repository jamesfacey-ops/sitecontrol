# build.ps1 — cross-compile the static Linux binaries from the Windows laptop.
# This IS the D1 gate check (chunk-plan §5): CGO_ENABLED=0, pure-Go SQLite.
# Run from anywhere: paths are derived from the script location.
#
# ⟦gate B-6, 2026-09-06⟧ TWO binaries, not one. `cmd/dumpcompare` is the
# changeover sheet's §3 comparison step — the printed operator check that the
# two posts agree, run past the point of no return. Until this change the
# ONLY thing in the tree that built it was deploy/demo/push-harness.ps1, which
# placed it inside container 501 (the lab's router — not a post, not a gate
# PC), so the sheet's "carry both files to one machine" named no machine that
# exists on the night (W10-2(a)(ii), W10-2(d); D-85 EXECUTION). Building it
# here puts it in dist/ beside sitectl so the SAME hand-push that installs the
# binary at each post (c4-procedure §4 step 7; sheet §2 "push the new binary
# + config to both") carries it to /opt/sitectl/dumpcompare on BOTH posts.
# The sheet then names the machine: either post.
#
# The sha256 of each is printed for the sheet's provenance lines (§1 "one
# binary"; W10-2(d)(i) "which dumpcompare build ran").
$ErrorActionPreference = "Stop"
$repo = Split-Path -Parent $PSScriptRoot
$out  = Join-Path $repo "dist"
New-Item -ItemType Directory -Force $out | Out-Null

$env:CGO_ENABLED = "0"
$env:GOOS        = "linux"
$env:GOARCH      = "amd64"

$sw = [System.Diagnostics.Stopwatch]::StartNew()
# -trimpath for reproducibility; -ldflags "-s -w" strips symbol/DWARF tables (size).
go build -trimpath -ldflags "-s -w" -o (Join-Path $out "sitectl-linux-amd64") "$repo/cmd/sitectl"
if ($LASTEXITCODE -ne 0) { throw "go build sitectl failed" }
go build -trimpath -ldflags "-s -w" -o (Join-Path $out "dumpcompare-linux-amd64") "$repo/cmd/dumpcompare"
if ($LASTEXITCODE -ne 0) { throw "go build dumpcompare failed" }
$sw.Stop()

Remove-Item Env:CGO_ENABLED, Env:GOOS, Env:GOARCH

foreach ($name in @("sitectl-linux-amd64", "dumpcompare-linux-amd64")) {
    $bin = Get-Item (Join-Path $out $name)
    $sha = (Get-FileHash -Algorithm SHA256 $bin.FullName).Hash.ToLower()
    "binary : $($bin.FullName)"
    "size   : $([math]::Round($bin.Length / 1MB, 2)) MB ($($bin.Length) bytes)"
    "sha256 : $sha"
}
"time   : $([math]::Round($sw.Elapsed.TotalSeconds, 1)) s"
"install: push BOTH to each post beside the unit's binary - /opt/sitectl/sitectl and /opt/sitectl/dumpcompare (chmod +x); the sheet's compare step runs dumpcompare ON A POST."
