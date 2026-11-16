# Installs a pitwall release on Windows; see "Install" in README.md.

function Get-PitwallAsset([string]$Arch) {
    switch ($Arch) {
        'AMD64' { 'pitwall_windows_amd64.zip' }
        'ARM64' { 'pitwall_windows_arm64.zip' }
        default { throw "pitwall: no release for $Arch" }
    }
}

# Writes a shortcut at $Path that opens a pitwall window from $Exe, with the
# icon built into it.
function New-PitwallShortcut([string]$Exe, [string]$Path) {
    $s = (New-Object -ComObject WScript.Shell).CreateShortcut($Path)
    $s.TargetPath = $Exe
    $s.IconLocation = "$Exe,0"
    $s.WorkingDirectory = $env:USERPROFILE
    $s.Description = 'Terminal multiplexer for coding agents'
    $s.Save()
}

# Verifies the environment before anything downloads: the cmdlets the
# install relies on, an arch a release exists for, a writable install dir,
# a writable user PATH, and that github.com answers. Every failure is
# reported, then the script stops before touching anything.
function Test-PitwallPreflight([string]$Dir) {
    $bad = @()
    foreach ($cmd in 'Invoke-WebRequest', 'Expand-Archive', 'Get-FileHash') {
        if (Get-Command $cmd -ErrorAction SilentlyContinue) {
            Write-Host "[preflight] $cmd ok"
        } else {
            $bad += "$cmd is missing (needs Windows PowerShell 5+)"
        }
    }
    try {
        Get-PitwallAsset $env:PROCESSOR_ARCHITECTURE | Out-Null
        Write-Host "[preflight] arch $env:PROCESSOR_ARCHITECTURE ok"
    } catch {
        $bad += $_.Exception.Message
    }
    try {
        New-Item -ItemType Directory -Force -Path $Dir | Out-Null
        $probe = Join-Path $Dir ('.write-' + [guid]::NewGuid())
        [IO.File]::WriteAllText($probe, 'x')
        Remove-Item -Force $probe
        Write-Host "[preflight] install dir $Dir writable"
    } catch {
        $bad += "cannot write to the install dir ${Dir}: $($_.Exception.Message)"
    }
    try {
        [Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path', 'User'), 'User')
        Write-Host '[preflight] user PATH writable'
    } catch {
        # why: the binary still lands; PATH is the convenience around it.
        Write-Host "[preflight] user PATH not writable ($($_.Exception.Message)); the install works, you add the dir yourself"
    }
    try {
        Invoke-WebRequest -UseBasicParsing -Method Head -Uri 'https://github.com' -TimeoutSec 10 | Out-Null
        Write-Host '[preflight] github.com reachable'
    } catch {
        $bad += "github.com is not reachable: $($_.Exception.Message)"
    }
    if ($bad.Count) {
        foreach ($b in $bad) { Write-Host "[preflight] FAIL: $b" }
        throw "pitwall: $($bad.Count) preflight check(s) failed; nothing installed"
    }
}

function Install-Pitwall {
    # why: iex runs this in the caller's session, so preferences stay local here.
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'Continue'
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $repo = 'kamenkadmitry/pitwall-win'
    $arch = $env:PROCESSOR_ARCHITEW6432
    if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
    $name = Get-PitwallAsset $arch
    $version = $env:PITWALL_VERSION
    if ($version) {
        $base = "https://github.com/$repo/releases/download/$version"
    } else {
        $base = "https://github.com/$repo/releases/latest/download"
        $version = 'latest'
    }
    $dir = $env:PITWALL_INSTALL_DIR
    if (-not $dir) { $dir = Join-Path $env:LOCALAPPDATA 'pitwall\bin' }

    try {
        Write-Progress -Activity 'Installing pitwall' -Status 'Checking the environment' -PercentComplete 5
        Test-PitwallPreflight $dir

        $tmp = Join-Path ([IO.Path]::GetTempPath()) ('pitwall-' + [guid]::NewGuid())
        New-Item -ItemType Directory -Path $tmp | Out-Null
        try {
            Write-Progress -Activity 'Installing pitwall' -Status "Downloading $name ($version)" -PercentComplete 30
            Write-Host "Downloading $name ($version)"
            $zip = Join-Path $tmp $name
            Invoke-WebRequest -UseBasicParsing -Uri "$base/$name" -OutFile $zip
            Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')
            Write-Progress -Activity 'Installing pitwall' -Status 'Verifying the checksum' -PercentComplete 60
            $want = $null
            foreach ($line in Get-Content (Join-Path $tmp 'checksums.txt')) {
                $f = -split $line
                if ($f.Count -ge 2 -and ($f[1] -eq $name -or $f[1] -eq "*$name")) { $want = $f[0] }
            }
            $got = (Get-FileHash -Algorithm SHA256 -Path $zip).Hash
            if (-not $want -or $want -ne $got) { throw "pitwall: $name does not match checksums.txt; nothing installed" }
            Write-Progress -Activity 'Installing pitwall' -Status "Installing to $dir" -PercentComplete 80
            Expand-Archive -Path $zip -DestinationPath (Join-Path $tmp 'x')
            New-Item -ItemType Directory -Force -Path $dir | Out-Null
            $exe = Join-Path $dir 'pitwall.exe'
            # why: Windows refuses to overwrite a running exe but lets it be renamed.
            # An older one a daemon still runs keeps its name; pitwall deletes them all later.
            if (Test-Path $exe) {
                $old = "$exe.old"
                for ($i = 1; (Test-Path $old) -and $i -lt 10; $i++) {
                    Remove-Item -Force $old -ErrorAction SilentlyContinue
                    if (Test-Path $old) { $old = "$exe.old$i" }
                }
                Move-Item -Force $exe $old
            }
            Copy-Item (Join-Path $tmp 'x\pitwall.exe') $exe
        } finally {
            Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
        }

        Write-Progress -Activity 'Installing pitwall' -Status 'Updating PATH and the Start menu' -PercentComplete 95
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        if (($userPath -split ';') -notcontains $dir) {
            $newPath = if ($userPath) { "$userPath;$dir" } else { $dir }
            [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
            $env:Path = "$env:Path;$dir"
            Write-Host "Added $dir to your user PATH; new terminals pick it up."
        }
        New-PitwallShortcut $exe (Join-Path ([Environment]::GetFolderPath('Programs')) 'pitwall.lnk')
        Write-Host "Installed $(& $exe --version) in $dir, with pitwall in the Start menu."
        Write-Host 'Next: pitwall hooks install'
        Write-Host 'Then run pitwall and trust Codex hooks once with /hooks.'
    } finally {
        Write-Progress -Activity 'Installing pitwall' -Completed
    }
}

# why: the install runs last, so a download cut short runs nothing.
if ($env:PITWALL_GET_TEST -ne '1') { Install-Pitwall }
