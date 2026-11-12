<#
.SYNOPSIS
    Installs sitectl as a Windows service that starts automatically and
    restarts itself on failure.

.DESCRIPTION
    Fixes Windows error 1053 ("the service did not respond to the start or
    control request in a timely fashion"): that error is the classic symptom
    of a binary that, when launched by the Service Control Manager, never
    calls into the service API at all. sitectl now does (cmd/sitectl/
    service_windows.go, svc.IsWindowsService + svc.Run) -- this script is
    the other half, registering it correctly.

    - Copies the exe to C:\Program Files\SiteControl\sitectl.exe.
    - Creates C:\ProgramData\SiteControl\ for config + the sqlite DB, and
      locks its ACL down to Administrators + the service account only.
    - Registers the service to run as a low-privilege virtual account
      (NT SERVICE\<ServiceName> by default) with automatic start and
      restart-on-failure recovery actions.
    - Idempotent: re-running it stops/removes any existing service of the
      same name first, then recreates it, so it is safe to re-run after
      dropping in a new exe or a changed config.

    Must run elevated (Run as Administrator).

.PARAMETER SourceExe
    Path to the sitectl.exe to install. Defaults to sitectl.exe next to this
    script (i.e. deploy\windows\sitectl.exe), falling back to .\sitectl.exe
    in the current directory if that is not present.

.PARAMETER ConfigSource
    Optional path to a config.toml to install as the service's config. If
    given, it is copied to <DataDir>\config.toml (overwriting any existing
    one there). If omitted, <DataDir>\config.toml must already exist -- the
    script refuses to invent a config, since it carries the site's login
    password and peer key.

.PARAMETER InstallDir
    Where the exe is installed. Default: C:\Program Files\SiteControl

.PARAMETER DataDir
    Where the config and sqlite DB live. Default: C:\ProgramData\SiteControl
    IMPORTANT: db_path in the installed config.toml should be an ABSOLUTE
    path under DataDir (e.g. "C:\ProgramData\SiteControl\site.db"), not a
    bare relative filename -- a Windows service's working directory is NOT
    guaranteed to be the exe's directory (the SCM does not expose a
    "start in" setting the way a shortcut does), so a relative db_path
    would land wherever the SCM happens to default to.

.PARAMETER ServiceName
    Must match windowsServiceName in cmd/sitectl/service_windows.go
    ("SiteControl"). Only change both together.

.PARAMETER ServiceAccount
    The account the service runs as. Default: NT SERVICE\<ServiceName>, a
    per-service virtual account -- no password to manage or rotate, and it
    gets its own distinguishable identity for the data-folder ACL below
    (as opposed to the shared, harder-to-scope LocalService account).
    Pass "NT AUTHORITY\LocalService" to use LocalService instead.

.EXAMPLE
    .\install-service.ps1 -ConfigSource C:\temp\my-site.toml
#>
[CmdletBinding()]
param(
    [string]$SourceExe = $(Join-Path $PSScriptRoot 'sitectl.exe'),
    [string]$ConfigSource,
    [string]$InstallDir = 'C:\Program Files\SiteControl',
    [string]$DataDir = 'C:\ProgramData\SiteControl',
    [string]$ServiceName = 'SiteControl',
    [string]$ServiceAccount = "NT SERVICE\$ServiceName"
)

$ErrorActionPreference = 'Stop'

function Assert-Admin {
    $id = [Security.Principal.WindowsIdentity]::GetCurrent()
    $p = [Security.Principal.WindowsPrincipal]::new($id)
    if (-not $p.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw "install-service.ps1 must run elevated (Run as Administrator)."
    }
}

Assert-Admin

if (-not (Test-Path -LiteralPath $SourceExe)) {
    $fallback = Join-Path (Get-Location) 'sitectl.exe'
    if (Test-Path -LiteralPath $fallback) {
        $SourceExe = $fallback
    } else {
        throw "sitectl.exe not found at '$SourceExe' or '$fallback'. Pass -SourceExe explicitly."
    }
}
Write-Host "Source exe: $SourceExe"

# ---- Stop and remove any existing service of this name (idempotent re-run) ----
$existing = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($existing) {
    Write-Host "Existing service '$ServiceName' found (status: $($existing.Status)) -- removing before reinstall."
    if ($existing.Status -ne 'Stopped') {
        Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
        $existing.WaitForStatus('Stopped', (New-TimeSpan -Seconds 30))
    }
    & sc.exe delete $ServiceName | Out-Null
    Start-Sleep -Seconds 1
}

# ---- Install the exe ----
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$destExe = Join-Path $InstallDir 'sitectl.exe'
Copy-Item -LiteralPath $SourceExe -Destination $destExe -Force
Write-Host "Installed: $destExe"

# ---- Data directory + config ----
$dataDirExisted = Test-Path -LiteralPath $DataDir
New-Item -ItemType Directory -Force -Path $DataDir | Out-Null
$destConfig = Join-Path $DataDir 'config.toml'

if ($ConfigSource) {
    if (-not (Test-Path -LiteralPath $ConfigSource)) {
        throw "-ConfigSource '$ConfigSource' does not exist."
    }
    Copy-Item -LiteralPath $ConfigSource -Destination $destConfig -Force
    Write-Host "Installed config: $destConfig (from $ConfigSource)"
} elseif (-not (Test-Path -LiteralPath $destConfig)) {
    throw "No config at '$destConfig' and no -ConfigSource given. This script will not " +
          "invent a config (it would need a real site_id / login_password / peer_key). " +
          "Place a config.toml there first, or re-run with -ConfigSource."
} else {
    Write-Host "Using existing config: $destConfig"
}

# ---- Register the service ----
# The quotes around each path are ESCAPED (\" not a bare ") rather than
# passed through PowerShell's own native-argument quoting. MEASURED on a
# real Windows 11 Pro VM: PowerShell 5.1 mishandles a variable that already
# contains embedded double quotes when it is handed to a native exe as one
# token -- sc.exe silently received a mangled argument and dumped its USAGE
# text (exit 1639, "invalid command line"), with binPath built the "normal"
# way ("`"$destExe`" ..."). Pre-escaping as \" (so the value PowerShell hands
# to CreateProcess is one already-quoted argument whose internal quotes are
# backslash-escaped) is what sc.exe/CommandLineToArgvW actually expects and
# is what worked in testing (sc.exe qc then showed the exact intended path).
$binPath = "\`"$destExe\`" -config \`"$destConfig\`""
Write-Host "Registering service '$ServiceName' as $ServiceAccount ..."
& sc.exe create $ServiceName `
    binPath= $binPath `
    start= auto `
    obj= $ServiceAccount `
    DisplayName= 'SiteControl' | Out-Null
if ($LASTEXITCODE -ne 0) { throw "sc.exe create failed (exit $LASTEXITCODE)" }

& sc.exe description $ServiceName 'SiteControl gate-control post (sitectl). Installed by install-service.ps1.' | Out-Null

# Recovery: restart on the 1st, 2nd and every subsequent failure, 5s later,
# resetting the failure count after 24h of continuous uptime. A clean Stop
# (service_windows.go reports exit code 0) does not count as a failure; a
# crash or a runServer error (exit code 1) does.
& sc.exe failure $ServiceName reset= 86400 actions= 'restart/5000/restart/5000/restart/5000' | Out-Null
& sc.exe failureflag $ServiceName 1 | Out-Null   # apply recovery actions even for non-crash failures

# ---- Lock down the data directory ACL ----
# Break inheritance and grant only: Administrators (full control, for
# operators/support) and the service account (modify -- it writes the
# sqlite DB and its WAL/SHM siblings there). Everyone else loses access,
# including the inherited Users/Authenticated Users grants ProgramData
# normally carries -- the config holds the site login password and peer key.
Write-Host "Restricting ACL on $DataDir ..."
& icacls $DataDir /inheritance:r | Out-Null
& icacls $DataDir /grant:r 'BUILTIN\Administrators:(OI)(CI)F' | Out-Null
& icacls $DataDir /grant:r 'NT AUTHORITY\SYSTEM:(OI)(CI)F' | Out-Null
& icacls $DataDir /grant:r "${ServiceAccount}:(OI)(CI)M" | Out-Null
if ($LASTEXITCODE -ne 0) {
    Write-Warning "icacls grant for '$ServiceAccount' returned exit $LASTEXITCODE -- the account may not be resolvable until the service has started at least once. Re-run icacls manually after first start if needed:"
    Write-Warning "  icacls `"$DataDir`" /grant:r `"${ServiceAccount}:(OI)(CI)M`""
}

# ---- Start it ----
Write-Host "Starting service '$ServiceName' ..."
Start-Service -Name $ServiceName
(Get-Service -Name $ServiceName).WaitForStatus('Running', (New-TimeSpan -Seconds 30))
$svc = Get-Service -Name $ServiceName
Write-Host "Service '$ServiceName' status: $($svc.Status)"
if ($svc.Status -ne 'Running') {
    throw "Service did not reach Running within 30s (status: $($svc.Status))."
}

Write-Host ""
Write-Host "Done. Installed at $destExe, config at $destConfig, data dir $DataDir."
Write-Host "Uninstall with: deploy\windows\uninstall-service.ps1 [-RemoveData]"
