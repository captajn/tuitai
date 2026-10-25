# Build

## Yêu cầu

- Go 1.25 trở lên (đã kiểm với 1.27.2).
- Không cần CGO, không cần mạng sau khi có toolchain.

## Lưu ý về `go.work`

Nếu thư mục cha có `go.work` trỏ tới module không tồn tại, mọi lệnh `go` sẽ lỗi
`cannot load module …`. Khi build trong repo này, đặt:

```powershell
$env:GOWORK = "off"
```

(hoặc chạy `go env -w GOWORK=off`, hoặc xoá `go.work` ở thư mục cha.)

## Build cho máy hiện tại

```powershell
cd tuitai
$env:GOWORK = "off"
go build -trimpath -ldflags "-s -w" -o tuitai.exe ./cmd/tuitai
```

## Build chéo

```powershell
$env:GOWORK = "off"; New-Item -ItemType Directory -Force dist | Out-Null

$env:GOOS="windows"; $env:GOARCH="amd64"; go build -trimpath -ldflags "-s -w" -o dist/tuitai-windows-amd64.exe ./cmd/tuitai
$env:GOOS="linux";   $env:GOARCH="amd64"; go build -trimpath -ldflags "-s -w" -o dist/tuitai-linux-amd64      ./cmd/tuitai
$env:GOOS="darwin";  $env:GOARCH="arm64"; go build -trimpath -ldflags "-s -w" -o dist/tuitai-darwin-arm64     ./cmd/tuitai
$env:GOOS="darwin";  $env:GOARCH="amd64"; go build -trimpath -ldflags "-s -w" -o dist/tuitai-darwin-amd64     ./cmd/tuitai
```

Kích thước tham khảo: 7–7,5 MB mỗi file (đã `-s -w`).

## Kiểm thử

```powershell
$env:GOWORK = "off"
go vet ./...
go test -timeout 120s ./...
pwsh tests/run-tests.ps1     # vet + test + build chéo + smoke test daemon
```

## Đóng gói (gợi ý)

| Nền tảng | Cách làm |
|----------|----------|
| Windows | zip file `.exe`; thêm `versioninfo` bằng `go-winres` nếu muốn metadata |
| macOS | `codesign --sign -` + `notarize` nếu phát hành rộng |
| Linux | đặt vào `/usr/local/bin`, thêm systemd unit nếu chạy `daemon` |

File cấu hình/queue được tạo tự động ở lần chạy đầu, không cần bước cài đặt.
