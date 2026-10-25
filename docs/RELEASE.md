# Chính sách phát hành

Repo này **không tự phát hành**. Không có trigger theo tag, không có publish tự động.

## Quy trình

1. Kiểm thử local: `go test ./...`, `go build ./cmd/tuitai`, chạy `tuitai gui`.
2. Vào **Actions → release → Run workflow**:
   - `version`: để mặc định `v0.0.0-dev` nếu chỉ muốn build thử.
   - `publish`: **false** → chỉ tạo artifact (file tải trong tab Artifacts), **không** tạo Release.
3. Khi đã chạy ổn và được duyệt: chạy lại với `publish: true` + version thật (v.d. `v0.0.1`).

## Ghi chú

- Tag chỉ được tạo bởi bước Release (khi `publish: true`), không tạo tay.
- `build-windows` chỉ upload artifact, không bao giờ tạo Release.
- `guard` chặn commit binary/file tạm > 6MB.
- Bản build local: `go build -o tuitai.exe ./cmd/tuitai` (không sinh tag, không đẩy gì lên).
