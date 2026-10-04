# Install Yggdrasil Core on Windows and, optionally, join a network (#40).
#
#   irm https://github.com/yeixio/yggdrasil-core/releases/latest/download/install.ps1 | iex
#   & ([scriptblock]::Create((irm https://github.com/yeixio/yggdrasil-core/releases/latest/download/install.ps1))) `
#     join -Server 192.168.1.10:7332 -Token ygj_… -Fingerprint sha256:…
#
# Installs the release's headless archive in %LOCALAPPDATA%\Programs\Yggdrasil,
# checked against SHA256SUMS.txt, adds it to your PATH, and starts it now and
# at each sign-in with a scheduled task. An installed Yggdrasil that is
# running is left as it is.
#
# Environment: TOSKAR_VERSION, TOSKAR_RELEASE_URL, TOSKAR_URL, as for
# install.sh. The YGGDRASIL_ names from before the rename work too.
[CmdletBinding()]
param(
    [Parameter(Position = 0)][ValidateSet('install', 'join')][string]$Command = 'install',
    [string]$Server,
    [string]$Token,
    [string]$Fingerprint,
    [string]$Name
)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$Repo = 'yeixio/yggdrasil-core'
function Get-Setting([string]$Name) {
    $v = [Environment]::GetEnvironmentVariable("TOSKAR_$Name")
    if ($v) { return $v }
    return [Environment]::GetEnvironmentVariable("YGGDRASIL_$Name")
}
$ApiSetting = Get-Setting 'URL'
$ReleaseUrl = Get-Setting 'RELEASE_URL'
$Version = Get-Setting 'VERSION'
$Api = if ($ApiSetting) { $ApiSetting.TrimEnd('/') } else { 'http://127.0.0.1:7331' }
if ($ReleaseUrl) {
    $Base = $ReleaseUrl.TrimEnd('/')
} elseif ($Version) {
    $Base = "https://github.com/$Repo/releases/download/v$($Version.TrimStart('v'))"
} else {
    $Base = "https://github.com/$Repo/releases/latest/download"
}
$Prefix = Join-Path $env:LOCALAPPDATA 'Programs\Yggdrasil'

function Fail([string]$Message) {
    Write-Host "Install failed: $Message" -ForegroundColor Red
    exit 1
}

function Test-Healthy {
    try {
        Invoke-RestMethod -Uri "$Api/api/v1/health" -TimeoutSec 2 | Out-Null
        return $true
    } catch {
        return $false
    }
}

if ($Command -eq 'join' -and (-not $Server -or -not $Token -or -not $Fingerprint)) {
    Write-Host 'usage: install.ps1 join -Server <host:port> -Token <ygj_…> -Fingerprint <sha256:…>'
    exit 2
}

$Yggctl = Join-Path $Prefix 'toskarctl.exe'
$installed = (Get-Command toskarctl.exe, yggctl.exe -ErrorAction SilentlyContinue | Select-Object -First 1)
if ($installed -and (Test-Healthy)) {
    Write-Host '✓ Yggdrasil Core is already installed and running'
    $Yggctl = $installed.Source
} else {
    if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64') {
        Fail "Yggdrasil is built for 64-bit Intel and AMD Windows, not $($env:PROCESSOR_ARCHITECTURE)"
    }
    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("yggdrasil-" + [guid]::NewGuid())
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        try {
            Invoke-WebRequest -Uri "$Base/SHA256SUMS.txt" -OutFile "$tmp\SHA256SUMS.txt" -UseBasicParsing
        } catch {
            Fail "couldn't download the release list from $Base"
        }
        $line = Get-Content "$tmp\SHA256SUMS.txt" | Where-Object { $_ -match '  (yggdrasil-[0-9]\S*-windows-amd64-headless\.tar\.gz)$' } | Select-Object -First 1
        if (-not $line) { Fail 'the release has no file for Windows amd64' }
        $want, $name = $line -split '\s+', 2
        Write-Host "Downloading $name..."
        try {
            Invoke-WebRequest -Uri "$Base/$name" -OutFile "$tmp\$name" -UseBasicParsing
        } catch {
            Fail "couldn't download $Base/$name"
        }
        $got = (Get-FileHash -Algorithm SHA256 "$tmp\$name").Hash.ToLowerInvariant()
        if ($got -ne $want.ToLowerInvariant()) {
            Fail "$name doesn't match its checksum (expected $want, got $got); nothing was installed"
        }
        # tar ships with Windows 10 1803 and later.
        tar -xzf "$tmp\$name" -C $tmp
        if ($LASTEXITCODE -ne 0) { Fail "couldn't unpack $name" }
        $src = Get-ChildItem -Path $tmp -Directory -Filter 'yggdrasil-*-headless' | Select-Object -First 1
        if (-not $src) { Fail "the archive didn't have the expected folder" }

        # Stop a copy that is running before replacing its files.
        Stop-ScheduledTask -TaskName 'Yggdrasil' -ErrorAction SilentlyContinue
        Get-Process toskar, yggdrasil-daemon -ErrorAction SilentlyContinue | Stop-Process -Force
        if (Test-Path $Prefix) { Remove-Item -Recurse -Force $Prefix }
        New-Item -ItemType Directory -Path (Split-Path $Prefix) -Force | Out-Null
        Move-Item $src.FullName $Prefix
        # The names from before the rename, for scheduled tasks and MCP settings
        # that run them by path (#237). Hard links need no admin rights. An older
        # release (TOSKAR_VERSION) has only the old names.
        foreach ($pair in @(@('toskar.exe', 'yggdrasil-daemon.exe'), @('toskarctl.exe', 'yggctl.exe'))) {
            $new = Join-Path $Prefix $pair[0]
            $old = Join-Path $Prefix $pair[1]
            if ((Test-Path $new) -and -not (Test-Path $old)) {
                New-Item -ItemType HardLink -Path $old -Target $new | Out-Null
            }
        }
    } finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not ($userPath -split ';' | Where-Object { $_ -eq $Prefix })) {
        [Environment]::SetEnvironmentVariable('Path', ((@($userPath, $Prefix) | Where-Object { $_ }) -join ';'), 'User')
    }
    $env:Path = "$env:Path;$Prefix"

    $daemon = Join-Path $Prefix 'toskar.exe'
    if (-not (Test-Path $daemon)) { $daemon = Join-Path $Prefix 'yggdrasil-daemon.exe' }
    if (-not (Test-Path $Yggctl)) { $Yggctl = Join-Path $Prefix 'yggctl.exe' }
    $action = New-ScheduledTaskAction -Execute $daemon -WorkingDirectory $Prefix
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User "$env:USERDOMAIN\$env:USERNAME"
    $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
        -ExecutionTimeLimit ([TimeSpan]::Zero) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)
    Register-ScheduledTask -TaskName 'Yggdrasil' -Action $action -Trigger $trigger -Settings $settings `
        -Description 'Yggdrasil Core' -Force | Out-Null
    Start-ScheduledTask -TaskName 'Yggdrasil'
    Write-Host '✓ Yggdrasil Core installed'

    Write-Host 'Waiting for Yggdrasil to start...'
    $deadline = (Get-Date).AddMinutes(1)
    while (-not (Test-Healthy)) {
        if ((Get-Date) -gt $deadline) {
            Fail "Yggdrasil didn't start within a minute; see the logs in $env:LOCALAPPDATA\Toskar\logs (or Yggdrasil\logs for an install from before the rename) and try again"
        }
        Start-Sleep -Seconds 1
    }
    Write-Host '✓ Yggdrasil Core is running'
}

if ($Command -eq 'join') {
    # Both names, in case an older release that reads only the old one was installed.
    $env:TOSKAR_URL = $Api
    $env:YGGDRASIL_URL = $Api
    $joinArgs = @('join', '--server', $Server, '--token', $Token, '--fingerprint', $Fingerprint)
    if ($Name) { $joinArgs += @('--name', $Name) }
    & $Yggctl @joinArgs
    exit $LASTEXITCODE
}
Write-Host "Open $Api or run $([System.IO.Path]::GetFileNameWithoutExtension($Yggctl)) to use it."
