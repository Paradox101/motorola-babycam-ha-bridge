param(
    [switch]$SelfTest
)

$ErrorActionPreference = 'Stop'

function Test-TrackedPaths {
    param([string[]]$Paths)

    $violations = [System.Collections.Generic.List[string]]::new()
    foreach ($path in $Paths) {
        $normalized = $path.Replace('\', '/')
        $name = [System.IO.Path]::GetFileName($normalized)
        $extension = [System.IO.Path]::GetExtension($normalized).ToLowerInvariant()

        if ($extension -in @('.pcap', '.pcapng', '.har', '.apk', '.xapk', '.so', '.zip', '.jar')) {
            $violations.Add("forbidden tracked artifact: $normalized")
        }
        if ($name -notmatch '\.example\.json$' -and
            $name -match '(?i)^(creds.*|.*session.*|.*credentials.*)\.json$') {
            $violations.Add("forbidden tracked credential/session file: $normalized")
        }
    }
    return $violations
}

function Get-ProductionResearchReferences {
    $roots = @('cmd', 'internal', 'homeassistant', 'deploy')
    $files = & git grep -Il 'research/' -- $roots 'Dockerfile' 2>$null
    # git grep exits 1 when it matches nothing, which is the good case here.
    # Capture it immediately: leaving $LASTEXITCODE at 1 makes pwsh exit 1 for
    # the whole script even though the policy passed.
    $grepExit = $LASTEXITCODE
    if ($grepExit -notin @(0, 1)) {
        throw 'git grep failed while checking production references'
    }
    $executableFiles = @($files | Where-Object {
        $name = [System.IO.Path]::GetFileName($_)
        $extension = [System.IO.Path]::GetExtension($_).ToLowerInvariant()
        $name -eq 'Dockerfile' -or $extension -in @('.go', '.sh', '.yaml', '.yml')
    })
    return @($executableFiles | ForEach-Object { "production file references research/: $_" })
}

if ($SelfTest) {
    $bad = @(Test-TrackedPaths @(
        'captures/camera.pcapng',
        'runtime/creds.json',
        'research/vendor/tool.jar'
    ))
    if ($bad.Count -ne 3) {
        throw "self-test expected 3 violations, got $($bad.Count)"
    }
    $clean = @(Test-TrackedPaths @(
        'internal/magic/testdata/relay_open.bin',
        'deploy/go2rtc/creds.example.json',
        'research/docs/REPORT.md'
    ))
    if ($clean.Count -ne 0) {
        throw "self-test rejected allowed fixtures: $($clean -join '; ')"
    }
    Write-Output 'repository policy self-test passed'
    exit 0
}

$tracked = @(& git ls-files --cached --others --exclude-standard)
if ($LASTEXITCODE -ne 0) {
    throw 'git ls-files failed'
}

$violations = [System.Collections.Generic.List[string]]::new()
foreach ($violation in @(Test-TrackedPaths $tracked)) {
    $violations.Add($violation)
}
foreach ($violation in @(Get-ProductionResearchReferences)) {
    $violations.Add($violation)
}

if ($violations.Count -gt 0) {
    $violations | ForEach-Object { Write-Error $_ }
    exit 1
}

Write-Output "repository policy passed ($($tracked.Count) repository files checked)"
# Explicit: without it the script inherits the exit code of the last native
# command it happened to run, and a clean tree reports failure.
exit 0
