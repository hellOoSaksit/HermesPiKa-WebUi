# SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
#
# One-command installer for hpk (Windows PowerShell 5.1 or newer):
#   irm https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest/download/install.ps1 | iex
#
# It downloads the release archive for this PC, checks its SHA-256 against
# checksums.txt (and stops if anything is off), then runs `hpk self-install`.
#
# Optional environment:
#   HPK_VERSION       install this version (e.g. 0.1.0) instead of the latest
#   HPK_RELEASE_BASE  download from here instead of GitHub (testing / mirrors)
#   HPK_DATA_DIR      where hpk keeps its files (default: %LOCALAPPDATA%\hpk)
#   HPK_NO_START=1    install only; do not start hpk afterwards
#
# Written as a function with throw/return so that piping it into iex never
# closes the caller's PowerShell window.

function Install-Hpk {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'   # progress bars make Invoke-WebRequest very slow on 5.1
    $repo = 'hellOoSaksit/HermesPiKa-WebUi'
    $tmp = $null
    try {
        # Windows PowerShell 5.1 does not offer TLS 1.2 by default; GitHub needs it.
        try { [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12 } catch { }

        $cpu = $env:PROCESSOR_ARCHITEW6432
        if (-not $cpu) { $cpu = $env:PROCESSOR_ARCHITECTURE }
        switch ($cpu) {
            'AMD64' { $arch = 'amd64' }
            'ARM64' { $arch = 'arm64' }
            default { throw "unsupported CPU '$cpu': hpk is built for amd64 and arm64" }
        }

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
        $archive = "hpk_${ver}_windows_$arch.zip"

        Write-Host "Installing hpk $ver (windows/$arch)..."
        foreach ($f in @($archive, 'checksums.txt')) {
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
            if ($parts.Count -eq 2 -and ($parts[1] -eq $archive -or $parts[1] -eq "*$archive")) { $expected = $parts[0]; break }
        }
        if (-not $expected) { throw "checksum for $archive is not listed in checksums.txt; refusing to install" }
        if ($expected -notmatch '^[0-9a-fA-F]{64}$') { throw 'checksums.txt is malformed; refusing to install' }
        $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $tmp $archive)).Hash
        if ($actual.ToLower() -ne $expected.ToLower()) {
            throw "checksum mismatch for $archive (expected $($expected.ToLower()), got $($actual.ToLower())); refusing to install"
        }
        Write-Host 'Checksum OK.'

        $x = Join-Path $tmp 'x'
        Expand-Archive -LiteralPath (Join-Path $tmp $archive) -DestinationPath $x -Force
        $exe = Join-Path $x 'hpk.exe'
        if (-not (Test-Path -LiteralPath $exe)) { throw 'the archive does not contain hpk.exe' }

        & $exe self-install | Out-Host
        if ($LASTEXITCODE -ne 0) { throw "hpk self-install failed (exit code $LASTEXITCODE)" }

        $data = $env:HPK_DATA_DIR
        if (-not $data) { $data = Join-Path $env:LOCALAPPDATA 'hpk' }
        $target = Join-Path (Join-Path $data 'bin') 'hpk.exe'

        if ($env:HPK_NO_START) {
            Write-Host "Done. Start it any time with: $target"
        } elseif (Test-Path -LiteralPath $target) {
            Write-Host 'Done. Starting hpk (open a new terminal later and just type: hpk).'
            Start-Process -FilePath $target
        } else {
            Write-Host 'Done. Open a new terminal and type: hpk'
        }
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
