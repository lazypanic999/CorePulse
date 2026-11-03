$ErrorActionPreference = 'Stop'
$systemDisk = Get-PSDrive -Name ($env:SystemDrive.TrimEnd(':'))
if ($systemDisk.Free -lt 3GB) { throw 'At least 3 GiB free on the Windows system drive is required before starting the WSL lab.' }
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$reports = Join-Path $repo 'reports'
New-Item -ItemType Directory -Force -Path $reports | Out-Null
$pidPath = Join-Path $reports 'lab-session.pid'
if (Test-Path -LiteralPath $pidPath) {
    $savedId = Get-Content -LiteralPath $pidPath
    $existing = Get-CimInstance Win32_Process -Filter "ProcessId=$([int]$savedId)" -ErrorAction SilentlyContinue
    if ($existing.Name -eq 'wsl.exe' -and $existing.CommandLine -like '*scripts/run-lab.sh*') {
        Write-Output "Lab session already running (PID $savedId)."
        exit 0
    }
}
$process = Start-Process wsl.exe -ArgumentList @('-d', 'Ubuntu', '--cd', ('"' + $repo + '"'), '--', 'bash', 'scripts/run-lab.sh') -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $reports 'lab-session.log') -RedirectStandardError (Join-Path $reports 'lab-session-error.log')
$process.Id | Set-Content -LiteralPath $pidPath
Write-Output "Lab session started (PID $($process.Id)); logs: $reports"
