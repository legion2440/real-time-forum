param(
  [string]$Image = "forum:local",
  [string]$Container = "forum-local",
  [int]$HostPort = 8080
)

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$projectRoot = Split-Path -Parent $scriptDir

function Remove-ContainerIfExists {
  param([string]$Name)

  $oldEap = $ErrorActionPreference
  $ErrorActionPreference = "SilentlyContinue"

  # try remove container if it exists (ignore errors if not)
  docker rm -f $Name 2>$null | Out-Null
  $global:LASTEXITCODE = 0

  $ErrorActionPreference = $oldEap
}

$started = $false

try {
  Set-Location $projectRoot

  Remove-ContainerIfExists -Name $Container

  Write-Host "==> docker build -t $Image ."
  docker build -t $Image .
  if ($LASTEXITCODE -ne 0) { throw "docker build failed" }

  Write-Host "==> docker run --rm -d --name $Container -p $HostPort`:8080 $Image"
  $cid = (docker run --rm -d --name $Container -p "${HostPort}:8080" $Image).Trim()
  if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($cid)) { throw "docker run failed" }
  $started = $true

  Write-Host "==> docker ps (container row)"
  docker ps --filter "name=$Container" --format "table {{.ID}}`t{{.Image}}`t{{.Status}}`t{{.Ports}}`t{{.Names}}"
  if ($LASTEXITCODE -ne 0) { throw "docker ps failed" }

  $url = "http://127.0.0.1:$HostPort/"
  $ok = $false
  for ($i = 0; $i -lt 20; $i++) {
    try {
      $res = Invoke-WebRequest -UseBasicParsing -Uri $url -TimeoutSec 2
      if ($res.StatusCode -eq 200) {
        $ok = $true
        break
      }
    } catch {
      # wait for app startup
    }
    Start-Sleep -Milliseconds 500
  }

  if (-not $ok) {
    throw "HTTP check failed (expected 200 from $url)"
  }

  Write-Host "==> HTTP check passed: 200 $url"
}
finally {
  if ($started) {
    Write-Host "==> Stopping container $Container"
    docker stop $Container *> $null
    $global:LASTEXITCODE = 0
  }
}

