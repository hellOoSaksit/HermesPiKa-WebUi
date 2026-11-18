# SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
#
# One-command installer for hpk on Windows (PowerShell 5.1 or newer):
#   irm https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest/download/install.ps1 | iex
#
# hpk runs on Windows in Docker (Hermes Agent and hpk together, one container,
# solo mode), so no program is installed on Windows itself. The script
# downloads the release's compose.release.yaml, checks its SHA-256 against
# checksums.txt (and stops if anything is off), writes your settings next to
# it, starts the container and opens HPK Console with a one-time link. You
# need Docker Desktop.
#
# Optional environment:
#   HPK_VERSION       use this version (e.g. 0.1.0) instead of the latest
#   HPK_RELEASE_BASE  download from here instead of GitHub (testing / mirrors)
#   HPK_DIR           where the compose file and settings go (default: %USERPROFILE%\hpk)
#   HPK_PROJECTS      the folder holding your projects (default: %USERPROFILE%\Documents);
#                     the Console sees it as /projects
#   HPK_NO_START=1    set up only; do not start the container
#
# Written as a function with throw/return so that piping it into iex never
# closes the caller's PowerShell window.

function Install-Hpk {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'   # progress bars make Invoke-WebRequest very slow on 5.1
    $repo = 'hellOoSaksit/HermesPiKa-WebUi'
    $compose = 'compose.release.yaml'
    $tmp = $null
    try {
        # Windows PowerShell 5.1 does not offer TLS 1.2 by default; GitHub needs it.
        try { [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12 } catch { }

        $tmp = Join-Path ([IO.Path]::GetTempPath()) ('hpk-install-' + [Guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Path $tmp | Out-Null

        $ver = $env:HPK_VERSION
        if (-not $ver) {
            try {
                $rel = Invoke-RestMethod -UseBasicParsing -Uri "https://api.github.com/repos/$repo/releases/latest"
                $ver = [string]$rel.tag_name
            } catch {
                throw "could not download the latest version info from GitHub ($($_.Exception.Message)); check your internet connection or set HPK_VERSION"
            }
            if (-not $ver) { throw 'could not read the latest version from GitHub; set HPK_VERSION (for example 0.1.0)' }
        }
        $ver = $ver.TrimStart('v')
        if ($ver -notmatch '^[0-9A-Za-z.+_-]+$') { throw "odd version '$ver'" }

        $base = $env:HPK_RELEASE_BASE
        if (-not $base) { $base = "https://github.com/$repo/releases/download/v$ver" }
        $base = $base.TrimEnd('/')

        Write-Host "Setting up hpk $ver in Docker..."
        foreach ($f in @($compose, 'checksums.txt')) {
            $url = "$base/$f"
            try {
                Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile (Join-Path $tmp $f)
            } catch {
                throw "could not download $url ($($_.Exception.Message)); check your internet connection, then run this again"
            }
        }

        # Verify. Fail closed: no line, a malformed line or a different hash all stop here.
        $expected = $null
        foreach ($line in (Get-Content -LiteralPath (Join-Path $tmp 'checksums.txt'))) {
            $parts = $line.Trim() -split '\s+', 2
            if ($parts.Count -eq 2 -and ($parts[1] -eq $compose -or $parts[1] -eq "*$compose")) { $expected = $parts[0]; break }
        }
        if (-not $expected) { throw "checksum for $compose is not listed in checksums.txt; refusing to install" }
        if ($expected -notmatch '^[0-9a-fA-F]{64}$') { throw 'checksums.txt is malformed; refusing to install' }
        $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $tmp $compose)).Hash
        if ($actual.ToLower() -ne $expected.ToLower()) {
            throw "checksum mismatch for $compose (expected $($expected.ToLower()), got $($actual.ToLower())); refusing to install"
        }
        Write-Host 'Checksum OK.'

        $dir = $env:HPK_DIR
        if (-not $dir) { $dir = Join-Path $env:USERPROFILE 'hpk' }
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
        Copy-Item -LiteralPath (Join-Path $tmp $compose) -Destination (Join-Path $dir $compose) -Force

        # Settings go in .env next to the compose file. An existing one is kept,
        # so running this again only updates the compose file (and the image).
        $envFile = Join-Path $dir '.env'
        if (-not (Test-Path -LiteralPath $envFile)) {
            $projects = $env:HPK_PROJECTS
            if (-not $projects) { $projects = Join-Path $env:USERPROFILE 'Documents' }
            if (-not (Test-Path -LiteralPath $projects -PathType Container)) { throw "the projects folder '$projects' does not exist; set HPK_PROJECTS" }
            $lines = @("HPK_PROJECTS=$($projects.Replace('\', '/'))")
            [IO.File]::WriteAllLines($envFile, [string[]]$lines)   # UTF-8 without a BOM, which docker compose needs
            Write-Host "Settings written to $envFile"
        }

        if ($env:HPK_NO_START) {
            Write-Host "Done. Start it any time in $dir with: docker compose -f $compose up -d"
            return $true
        }
        if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
            throw 'Docker is not installed. Install Docker Desktop (free for personal use and small businesses): https://www.docker.com/products/docker-desktop/ , start it, then run this again'
        }
        # PowerShell 5.1 turns a native command's redirected stderr into errors,
        # which 'Stop' would make fatal; docker's own exit codes decide here.
        $ErrorActionPreference = 'Continue'
        & docker info *> $null
        if ($LASTEXITCODE -ne 0) { throw 'Docker is installed but not running. Start Docker Desktop, wait until it says it is running, then run this again' }

        Push-Location $dir
        try {
            Write-Host 'Downloading and starting the container (the first time takes a few minutes)...'
            & docker compose -f $compose up -d | Out-Host
            if ($LASTEXITCODE -ne 0) { throw "docker compose up failed (exit code $LASTEXITCODE); see the messages above" }

            # A one-time link (valid 10 minutes) signs this browser in for 30 days.
            $link = $null
            for ($i = 0; $i -lt 40 -and -not $link; $i++) {
                $t = & docker compose -f $compose exec -T hermes cat /opt/data/hpk/console-link 2>$null
                if ($LASTEXITCODE -eq 0 -and "$t" -match 'http://127\.0\.0\.1:\d+/k/[0-9a-f]+') { $link = $Matches[0] } else { Start-Sleep -Seconds 3 }
            }
        } finally { Pop-Location }

        # Start menu shortcut "HPK Console": starts the container if needed and
        # opens the Console with a fresh one-time link, so nobody types a command.
        # A plain -Command (no hidden window, no execution-policy bypass).
        $open = "Set-Location -LiteralPath '$($dir.Replace("'", "''"))'; Write-Host 'Opening HPK Console...'; " +
            "docker compose -f $compose up -d; " +
            "for (`$i = 0; `$i -lt 40; `$i++) { `$l = (docker compose -f $compose exec -T hermes cat /opt/data/hpk/console-link) -join ''; " +
            "if (`$l -match 'http://127\.0\.0\.1:\d+/k/[0-9a-f]+') { Start-Process `$Matches[0]; exit }; Start-Sleep -Seconds 3 }; " +
            "Write-Host 'HPK Console did not start. Is Docker Desktop running?'; Read-Host 'Enter'"
        try {
            $lnk = Join-Path ([Environment]::GetFolderPath('Programs')) 'HPK Console.lnk'
            $sc = (New-Object -ComObject WScript.Shell).CreateShortcut($lnk)
            $sc.TargetPath = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
            $sc.Arguments = '-NoProfile -Command "' + $open.Replace('"', '\"') + '"'
            $sc.WorkingDirectory = $dir
            $sc.Description = 'Open HPK Console'
            $sc.Save()
            Write-Host 'Added "HPK Console" to the Start menu.'
        } catch { Write-Host "Could not add a Start menu shortcut ($($_.Exception.Message))." }

        $url = 'http://127.0.0.1:7878'
        Write-Host ''
        Write-Host "HPK Console is running at $url"
        Write-Host 'Next time, open it from the Start menu: "HPK Console".'
        if ($link) { $url = $link } else { Write-Host 'The Console is still starting; run that command in a minute.' }
        Write-Host "Your projects are in /projects inside the Console (your $((Get-Content -LiteralPath $envFile | Where-Object { $_ -like 'HPK_PROJECTS=*' }) -replace '^HPK_PROJECTS=', '') folder)."
        Write-Host "Add a model account: cd `"$dir`"; docker compose -f $compose exec -u hermes hermes hermes auth add <provider>"
        try { Start-Process $url } catch { }
        return $true
    } catch {
        [Console]::Error.WriteLine("hpk install: $($_.Exception.Message)")
        return $false
    } finally {
        if ($tmp -and (Test-Path -LiteralPath $tmp)) { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue }
    }
}

$hpkOk = Install-Hpk
# Run as a file (powershell -File) -> give a real exit code. Piped into iex there
# is no script path, so just return and leave the caller's shell open.
if (-not $hpkOk -and $PSCommandPath) { exit 1 }
