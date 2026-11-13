param(
    [string]$GatewayUrl = "",
    [string]$ApiKey = $env:OMNI_API_KEY,
    [string]$RepoRoot = "",
    [switch]$SkipLocalTests,
    [switch]$TriggerCycle,
    [int]$TimeoutSec = 8
)

# Enable modern TLS in PowerShell 5.1 / 7
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12 -bor [Net.SecurityProtocolType]::Tls13
} catch {
    # Tls13 might not be supported on older .NET Framework runtimes, ignore
}

Write-Host "================================================================" -ForegroundColor Cyan
Write-Host "       OmniRoute PDCA Autonomous Health & Telemetry Check       " -ForegroundColor Cyan
Write-Host "================================================================" -ForegroundColor Cyan

# -----------------------------------------------------------------------------
# 1. Resolve Repository Root
# -----------------------------------------------------------------------------
if ([string]::IsNullOrWhiteSpace($RepoRoot)) {
    $searchDir = if ($PSScriptRoot) { $PSScriptRoot } else { (Get-Location).Path }
    $current = $searchDir
    while ($current) {
        if (Test-Path (Join-Path $current "go.mod")) {
            $RepoRoot = $current
            break
        }
        $parent = Split-Path -Path $current -Parent
        if (-not $parent -or $parent -eq $current) {
            break
        }
        $current = $parent
    }
    if ([string]::IsNullOrWhiteSpace($RepoRoot)) {
        $RepoRoot = (Get-Location).Path
    }
}

Write-Host "[*] Repository Root: $RepoRoot" -ForegroundColor Gray

# -----------------------------------------------------------------------------
# 2. Local Static & Unit Verification
# -----------------------------------------------------------------------------
$vetPassed = $null
$testPassed = $null

if (-not $SkipLocalTests) {
    if (-not (Test-Path (Join-Path $RepoRoot "go.mod"))) {
        Write-Warning "Could not find 'go.mod' in $RepoRoot. Skipping local go checks."
    } else {
        Write-Host "`n[1/3] Running Local Go Verifications..." -ForegroundColor Yellow
        Push-Location $RepoRoot
        try {
            Write-Host "  -> Running: go vet ./..." -ForegroundColor Gray
            & go vet ./...
            if ($LASTEXITCODE -eq 0) {
                Write-Host "  [+] 'go vet ./...' passed successfully." -ForegroundColor Green
                $vetPassed = $true
            } else {
                Write-Host "  [-] 'go vet ./...' failed with exit code $LASTEXITCODE." -ForegroundColor Red
                $vetPassed = $false
            }

            Write-Host "  -> Running: go test -count=1 ./gateway -run TestPDCA" -ForegroundColor Gray
            & go test -count=1 ./gateway -run TestPDCA
            if ($LASTEXITCODE -eq 0) {
                Write-Host "  [+] 'go test ... TestPDCA' passed successfully." -ForegroundColor Green
                $testPassed = $true
            } else {
                Write-Host "  [-] 'go test ... TestPDCA' failed with exit code $LASTEXITCODE." -ForegroundColor Red
                $testPassed = $false
            }
        } finally {
            Pop-Location
        }
    }
} else {
    Write-Host "`n[1/3] Local tests skipped (-SkipLocalTests)." -ForegroundColor DarkGray
}

# -----------------------------------------------------------------------------
# 3. Live Endpoint Health & PDCA Telemetry Check
# -----------------------------------------------------------------------------
Write-Host "`n[2/3] Probing Live Gateway Endpoint..." -ForegroundColor Yellow

# Resolve Gateway Base URL
$candidateUrls = @()
$explicitUrlProvided = $false

if (-not [string]::IsNullOrWhiteSpace($GatewayUrl)) {
    $candidateUrls += $GatewayUrl.TrimEnd('/')
    $explicitUrlProvided = $true
} elseif (-not [string]::IsNullOrWhiteSpace($env:GATEWAY_URL)) {
    $candidateUrls += $env:GATEWAY_URL.TrimEnd('/')
    $explicitUrlProvided = $true
} else {
    $candidateUrls += "https://omni.bucloud.vip"
    $candidateUrls += "http://localhost:8080"
    $candidateUrls += "http://127.0.0.1:8787"
}

$headers = @{}
if (-not [string]::IsNullOrWhiteSpace($ApiKey)) {
    $headers["Authorization"] = "Bearer $ApiKey"
}

$activeBaseUrl = $null
$healthzData = $null

foreach ($cand in $candidateUrls) {
    $healthUrl = "$cand/healthz"
    try {
        Write-Host "  -> Probing $healthUrl (Timeout: ${TimeoutSec}s)..." -ForegroundColor Gray
        $resp = Invoke-RestMethod -Uri $healthUrl -Method Get -Headers $headers -TimeoutSec $TimeoutSec -ErrorAction Stop
        $activeBaseUrl = $cand
        $healthzData = $resp
        Write-Host "  [+] Connected to live gateway at: $activeBaseUrl" -ForegroundColor Green
        break
    } catch {
        if ($explicitUrlProvided) {
            Write-Warning "Failed connecting to ${healthUrl}: $($_.Exception.Message)"
        } else {
            Write-Host "     Unable to reach $cand, trying next fallback..." -ForegroundColor DarkGray
        }
    }
}

if ($healthzData) {
    Write-Host "      Status:              $($healthzData.status)" -ForegroundColor White
    if ($healthzData.version) {
        Write-Host "      Version:             $($healthzData.version)" -ForegroundColor White
    }
    if ($healthzData.connected_accounts -ne $null) {
        Write-Host "      Connected Accounts:  $($healthzData.connected_accounts)" -ForegroundColor White
    }
    if ($healthzData.routing_strategy) {
        Write-Host "      Routing Strategy:    $($healthzData.routing_strategy)" -ForegroundColor White
    }
}

# -----------------------------------------------------------------------------
# 4. Check PDCA Telemetry (/api/pdca/status)
# -----------------------------------------------------------------------------
Write-Host "`n[3/3] Inspecting PDCA Watchdog Telemetry..." -ForegroundColor Yellow

$pdcaStatus = $null
$pdcaEndpointReachable = $false

if ($activeBaseUrl) {
    $pdcaUrl = "$activeBaseUrl/api/pdca/status"
    if ($TriggerCycle) {
        $pdcaUrl += "?run=true"
        Write-Host "  [*] -TriggerCycle specified: requesting immediate PDCA cycle run." -ForegroundColor Magenta
    }

    try {
        Write-Host "  -> Querying $pdcaUrl..." -ForegroundColor Gray
        $pdcaStatus = Invoke-RestMethod -Uri $pdcaUrl -Method Get -Headers $headers -TimeoutSec $TimeoutSec -ErrorAction Stop
        $pdcaEndpointReachable = $true
    } catch {
        $ex = $_.Exception
        $statusCode = $null
        if ($ex.Response -and $ex.Response.StatusCode) {
            $statusCode = [int]$ex.Response.StatusCode
        }

        if ($statusCode -eq 404) {
            Write-Warning "  [!] Endpoint /api/pdca/status returned HTTP 404 Not Found.`n      The remote gateway at $activeBaseUrl may be running an older release prior to PDCA engine integration."
        } else {
            Write-Warning "  [!] Failed querying /api/pdca/status: $($ex.Message)"
        }
    }
} else {
    Write-Warning "  [!] Skipping PDCA status check because no live gateway endpoint was reachable."
}

$pdcaStateStr = "UNREACHABLE"
$crossProviderVerified = $false
$alertCount = 0

if ($pdcaStatus) {
    $pdcaStateStr = if ($pdcaStatus.status) { [string]$pdcaStatus.status } else { "UNKNOWN" }
    $crossProviderVerified = [bool]$pdcaStatus.cross_provider_isolation_verified
    $alertCount = if ($pdcaStatus.alerts) { @($pdcaStatus.alerts).Count } else { 0 }

    Write-Host "  [+] PDCA Engine Status:                $pdcaStateStr" -ForegroundColor $(if ($pdcaStateStr -eq "healthy") { "Green" } elseif ($pdcaStateStr -eq "healed") { "Cyan" } else { "Yellow" })
    Write-Host "      Watchdog Running:                  $($pdcaStatus.watchdog_running)" -ForegroundColor White
    Write-Host "      Watchdog Interval:                 $($pdcaStatus.watchdog_interval_sec)s" -ForegroundColor White
    Write-Host "      Total Cycles Completed:            $($pdcaStatus.total_cycles)" -ForegroundColor White
    Write-Host "      Total Anomalies Detected:          $($pdcaStatus.total_anomalies)" -ForegroundColor White
    Write-Host "      Total Self-Healing Actions:        $($pdcaStatus.total_actions)" -ForegroundColor White
    Write-Host "      Cross-Provider Isolation Verified: $crossProviderVerified" -ForegroundColor $(if ($crossProviderVerified) { "Green" } else { "Red" })

    if ($alertCount -gt 0) {
        Write-Host "`n  [!] Active PDCA Alerts ($alertCount):" -ForegroundColor Red
        foreach ($alt in $pdcaStatus.alerts) {
            $sevColor = if ($alt.severity -eq "critical") { "Red" } else { "Yellow" }
            Write-Host "    - [$($alt.severity.ToUpper())] Category: $($alt.category) | Provider: $($alt.provider) | Account: $($alt.account)" -ForegroundColor $sevColor
            Write-Host "      Summary:     $($alt.summary)" -ForegroundColor White
            Write-Host "      Remediation: $($alt.recommended_remediation)" -ForegroundColor Gray
        }
    } else {
        Write-Host "  [+] Active Alerts:                     0 (all clear)" -ForegroundColor Green
    }

    if ($pdcaStatus.recent_actions -and @($pdcaStatus.recent_actions).Count -gt 0) {
        Write-Host "`n  [*] Recent Self-Healing Actions:" -ForegroundColor Cyan
        foreach ($act in @($pdcaStatus.recent_actions)[0..([Math]::Min(2, @($pdcaStatus.recent_actions).Count - 1))]) {
            Write-Host "    * [$($act.type)] Target: $($act.target) on $($act.account) ($($act.detail))" -ForegroundColor Gray
        }
    }
}

# -----------------------------------------------------------------------------
# 5. PDCA Health Scorecard & Exit Evaluation
# -----------------------------------------------------------------------------
Write-Host "`n================================================================" -ForegroundColor Cyan
Write-Host "                   [PDCA Health Scorecard]                      " -ForegroundColor Cyan
Write-Host "================================================================" -ForegroundColor Cyan

# Local Vet
if ($vetPassed -eq $true) {
    Write-Host "  Local Static Check (go vet):      PASSED" -ForegroundColor Green
} elseif ($vetPassed -eq $false) {
    Write-Host "  Local Static Check (go vet):      FAILED" -ForegroundColor Red
} else {
    Write-Host "  Local Static Check (go vet):      SKIPPED" -ForegroundColor DarkGray
}

# Local Tests
if ($testPassed -eq $true) {
    Write-Host "  Local Unit Tests (TestPDCA):      PASSED" -ForegroundColor Green
} elseif ($testPassed -eq $false) {
    Write-Host "  Local Unit Tests (TestPDCA):      FAILED" -ForegroundColor Red
} else {
    Write-Host "  Local Unit Tests (TestPDCA):      SKIPPED" -ForegroundColor DarkGray
}

# Live Gateway
if ($activeBaseUrl) {
    Write-Host "  Live Gateway Endpoint:            ONLINE ($activeBaseUrl)" -ForegroundColor Green
} else {
    Write-Host "  Live Gateway Endpoint:            OFFLINE / UNREACHABLE" -ForegroundColor Yellow
}

# PDCA Telemetry
if ($pdcaEndpointReachable) {
    Write-Host "  PDCA Engine Telemetry:            REACHABLE (Status: $pdcaStateStr)" -ForegroundColor $(if ($pdcaStateStr -eq "healthy") { "Green" } else { "Cyan" })
    Write-Host "  Cross-Provider Isolation:         $(if ($crossProviderVerified) { 'VERIFIED' } else { 'FAILED' })" -ForegroundColor $(if ($crossProviderVerified) { "Green" } else { "Red" })
    Write-Host "  Active Alerts:                    $alertCount" -ForegroundColor $(if ($alertCount -eq 0) { "Green" } else { "Yellow" })
} else {
    Write-Host "  PDCA Engine Telemetry:            NOT AVAILABLE" -ForegroundColor DarkGray
}

Write-Host "================================================================" -ForegroundColor Cyan

# Exit code: fail if local static check or unit tests explicitly failed
if ($vetPassed -eq $false -or $testPassed -eq $false) {
    Write-Host "[!] Local PDCA verification failed. Please check compiler or unit test errors." -ForegroundColor Red
    exit 1
}

Write-Host "[+] PDCA health diagnostic completed successfully." -ForegroundColor Green
exit 0
