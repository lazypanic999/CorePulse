$ErrorActionPreference = 'Stop'
& wsl.exe -d Ubuntu -- minikube -p open5gs-lab stop
if ($LASTEXITCODE -ne 0) { throw 'Lab stop failed; session kept alive for diagnosis.' }
$pidPath = Join-Path $PSScriptRoot '..\reports\lab-session.pid'
if (Test-Path -LiteralPath $pidPath) {
    $savedId = [int](Get-Content -LiteralPath $pidPath)
    $existing = Get-CimInstance Win32_Process -Filter "ProcessId=$savedId" -ErrorAction SilentlyContinue
    if ($existing.Name -eq 'wsl.exe' -and $existing.CommandLine -like '*scripts/run-lab.sh*') {
        Get-CimInstance Win32_Process -Filter "ParentProcessId=$savedId" | Where-Object { $_.Name -eq 'wsl.exe' -and $_.CommandLine -eq $existing.CommandLine } | ForEach-Object { Stop-Process -Id $_.ProcessId -ErrorAction SilentlyContinue }
        Stop-Process -Id $savedId -ErrorAction SilentlyContinue
    }
}
Write-Output 'Lab stopped.'
