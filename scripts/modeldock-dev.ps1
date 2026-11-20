param(
  [ValidateSet('start', 'stop', 'restart', 'status')]
  [string]$Action = 'status',
  [int]$Port = 8787,
  [string]$ModelRoot = "$env:USERPROFILE\ModelDock",
  [string]$StateDir = "$PSScriptRoot\..\.modeldock-dev"
)

$ErrorActionPreference = 'Stop'
$RootDir = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$StateDir = [System.IO.Path]::GetFullPath($StateDir)
$ModelRoot = [System.IO.Path]::GetFullPath($ModelRoot)
$PidFile = Join-Path $StateDir 'modeldock.pid'
$LogFile = Join-Path $StateDir 'modeldock.log'
$ErrorLogFile = Join-Path $StateDir 'modeldock-error.log'
$Binary = Join-Path $StateDir 'modeldock.exe'

function Get-ModelDockProcess {
  if (-not (Test-Path -LiteralPath $PidFile)) { return $null }
  $pidValue = (Get-Content -LiteralPath $PidFile -Raw).Trim()
  if ($pidValue -notmatch '^\d+$') { return $null }
  return Get-Process -Id ([int]$pidValue) -ErrorAction SilentlyContinue
}

function Test-Health {
  try {
    $response = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$Port/api/health" -TimeoutSec 2
    return $response.StatusCode -eq 200
  } catch { return $false }
}

function Stop-ModelDock {
  $process = Get-ModelDockProcess
  if ($process) {
    Stop-Process -Id $process.Id -Force
    Write-Output "ModelDock stopped (PID $($process.Id))"
  } else {
    Write-Output 'ModelDock is not running'
  }
  Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
}

function Start-ModelDock {
  $existing = Get-ModelDockProcess
  if ($existing) {
    Write-Output "ModelDock is already running (PID $($existing.Id), port $Port)"
    return
  }
  New-Item -ItemType Directory -Force -Path $StateDir, $ModelRoot | Out-Null
  $go = Get-Command go -ErrorAction SilentlyContinue
  if (-not $go) { throw 'Go is not available in PATH. Install Go or add its bin directory to PATH.' }
  $goVersion = (& $go.Source version 2>$null)
  if ($goVersion -notmatch 'go(\d+)\.(\d+)' -or [int]$Matches[1] -lt 1 -or ([int]$Matches[1] -eq 1 -and [int]$Matches[2] -lt 24)) {
    throw "Go 1.24 or newer is required. Found: $goVersion"
  }
  Push-Location $RootDir
  try { & $go.Source build -o $Binary ./cmd/modeldock } finally { Pop-Location }
  $process = Start-Process -FilePath $Binary -ArgumentList @('serve', '--addr', "127.0.0.1:$Port", '--root', $ModelRoot) -WorkingDirectory $RootDir -RedirectStandardOutput $LogFile -RedirectStandardError $ErrorLogFile -PassThru
  Set-Content -LiteralPath $PidFile -Value $process.Id -NoNewline
  for ($i = 0; $i -lt 50 -and -not (Test-Health); $i++) { Start-Sleep -Milliseconds 100 }
  if (-not (Test-Health)) { throw "ModelDock did not become healthy. See $LogFile and $ErrorLogFile" }
  Write-Output "ModelDock started: http://127.0.0.1:$Port"
}

switch ($Action) {
  'start' { Start-ModelDock }
  'stop' { Stop-ModelDock }
  'restart' { Stop-ModelDock; Start-ModelDock }
  'status' {
    $process = Get-ModelDockProcess
    if ($process -and (Test-Health)) { Write-Output "ModelDock is running (PID $($process.Id), port $Port)"; Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$Port/api/health" | Select-Object -ExpandProperty Content }
    else { Write-Output 'ModelDock is not running'; exit 1 }
  }
}
