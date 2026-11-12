<#
.SYNOPSIS
    Removes the sitectl Windows service installed by install-service.ps1.

.DESCRIPTION
    Stops and deletes the service and removes the installed exe directory.
    By default the data directory (config + sqlite DB) is LEFT IN PLACE --
    pass -RemoveData to delete it too. Idempotent: safe to run when the
    service does not exist (e.g. already uninstalled).

    Must run elevated (Run as Administrator).

.PARAMETER ServiceName
    Must match the name used at install time. Default: SiteControl

.PARAMETER InstallDir
    Where the exe was installed. Default: C:\Program Files\SiteControl

.PARAMETER DataDir
    Where the config and sqlite DB live. Default: C:\ProgramData\SiteControl
    Only removed if -RemoveData is passed.

.PARAMETER RemoveData
    Also delete DataDir (config + DB). Irreversible -- omit to keep the data
    for a later reinstall.

.EXAMPLE
    .\uninstall-service.ps1
.EXAMPLE
    .\uninstall-service.ps1 -RemoveData
#>
[CmdletBinding()]
param(
    [string]$ServiceName = 'SiteControl',
    [string]$InstallDir = 'C:\Program Files\SiteControl',
    [string]$DataDir = 'C:\ProgramData\SiteControl',
    [switch]$RemoveData
)

$ErrorActionPreference = 'Stop'

function Assert-Admin {
    $id = [Security.Principal.WindowsIdentity]::GetCurrent()
    $p = [Security.Principal.WindowsPrincipal]::new($id)
    if (-not $p.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw "uninstall-service.ps1 must run elevated (Run as Administrator)."
    }
}

Assert-Admin

$svc = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($svc) {
    if ($svc.Status -ne 'Stopped') {
        Write-Host "Stopping service '$ServiceName' ..."
        Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
        try {
            $svc.WaitForStatus('Stopped', (New-TimeSpan -Seconds 30))
        } catch {
            Write-Warning "Service did not confirm Stopped within 30s; deleting registration anyway."
        }
    }
    Write-Host "Deleting service '$ServiceName' ..."
    & sc.exe delete $ServiceName | Out-Null
    if ($LASTEXITCODE -ne 0) {
        Write-Warning "sc.exe delete returned exit $LASTEXITCODE"
    }
} else {
    Write-Host "No service named '$ServiceName' found -- nothing to stop/delete."
}

if (Test-Path -LiteralPath $InstallDir) {
    Write-Host "Removing install directory $InstallDir ..."
    Remove-Item -LiteralPath $InstallDir -Recurse -Force
} else {
    Write-Host "Install directory $InstallDir not present -- nothing to remove."
}

if ($RemoveData) {
    if (Test-Path -LiteralPath $DataDir) {
        Write-Host "Removing data directory $DataDir (config + DB) -- -RemoveData was passed."
        Remove-Item -LiteralPath $DataDir -Recurse -Force
    } else {
        Write-Host "Data directory $DataDir not present -- nothing to remove."
    }
} else {
    if (Test-Path -LiteralPath $DataDir) {
        Write-Host "Data directory $DataDir left in place (pass -RemoveData to delete it)."
    }
}

Write-Host "Done."
