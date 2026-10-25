# tests/run-tests.ps1 — chạy toàn bộ kiểm tra cho tuitai trên Windows.
#
#   1. go vet + go test
#   2. build chéo windows/linux/darwin
#   3. smoke test: daemon + POST /add + GET /list với server fixture nội bộ
#
# Dùng: pwsh -File tests/run-tests.ps1 [-SkipSmoke]

param(
    [switch]$SkipSmoke
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    $env:GOWORK = 'off'
    $fail = 0

    Write-Host '== go vet ==' -ForegroundColor Cyan
    go vet ./...
    if ($LASTEXITCODE -ne 0) { $fail = 1 }

    Write-Host '== go test ==' -ForegroundColor Cyan
    go test -timeout 120s ./...
    if ($LASTEXITCODE -ne 0) { $fail = 1 }

    Write-Host '== build chéo ==' -ForegroundColor Cyan
    New-Item -ItemType Directory -Force dist | Out-Null
    $targets = @(
        @{ os = 'windows'; arch = 'amd64'; out = 'dist/tuitai-windows-amd64.exe' },
        @{ os = 'linux';   arch = 'amd64'; out = 'dist/tuitai-linux-amd64' },
        @{ os = 'darwin';  arch = 'arm64'; out = 'dist/tuitai-darwin-arm64' }
    )
    foreach ($t in $targets) {
        $env:GOOS = $t.os; $env:GOARCH = $t.arch
        go build -trimpath -ldflags '-s -w' -o $t.out ./cmd/tuitai
        if ($LASTEXITCODE -ne 0) {
            Write-Host "  build $($t.os)/$($t.arch) THẤT BẠI" -ForegroundColor Red
            $fail = 1
        } else {
            $size = [math]::Round((Get-Item $t.out).Length / 1MB, 2)
            Write-Host "  $($t.out) ($size MB)"
        }
    }
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'

    if (-not $SkipSmoke) {
        Write-Host '== smoke test daemon ==' -ForegroundColor Cyan
        $bin = 'dist/tuitai-windows-amd64.exe'
        if (-not (Test-Path $bin)) { $bin = 'tuitai.exe' }
        if (Test-Path $bin) {
            $port = 5599
            $proc = Start-Process -FilePath $bin -ArgumentList 'daemon', '--port', "$port" -PassThru -WindowStyle Hidden
            try {
                Start-Sleep -Seconds 2
                $health = Invoke-RestMethod -Uri "http://127.0.0.1:$port/health" -TimeoutSec 5
                if ($health.status -ne 'ok') { throw "health trả $($health.status)" }
                Write-Host "  /health ok (version $($health.version))"

                $resp = Invoke-RestMethod -Uri "http://127.0.0.1:$port/add?url=https%3A%2F%2Fspeed.cloudflare.com%2F__down%3Fbytes%3D100000" -Method Post -TimeoutSec 30
                if (-not $resp.id) { throw 'POST /add không trả id' }
                Write-Host "  /add ok (id $($resp.id))"

                $list = Invoke-RestMethod -Uri "http://127.0.0.1:$port/list" -TimeoutSec 10
                if ($list.jobs.Count -lt 1) { throw 'GET /list rỗng' }
                Write-Host "  /list ok ($($list.jobs.Count) job)"
            } catch {
                Write-Host "  smoke test THẤT BẠI: $_" -ForegroundColor Red
                $fail = 1
            } finally {
                Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
            }
        } else {
            Write-Host '  bỏ qua smoke: chưa có binary' -ForegroundColor Yellow
        }
    }

    if ($fail -eq 0) {
        Write-Host "`nTẤT CẢ ĐỀU ĐẠT" -ForegroundColor Green
        exit 0
    } else {
        Write-Host "`nCÓ LỖI" -ForegroundColor Red
        exit 1
    }
} finally {
    Pop-Location
}
