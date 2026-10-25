# Đối chiếu tính năng

Cột "Tui Download (macOS)" mô tả nhóm tính năng phổ biến của các trình quản lý
tải xuống desktop trên macOS (tải đa kết nối, hàng đợi, hẹn giờ, bắt link từ
Safari/Chrome). Cột "tuitai" là trạng thái thực tế của mã nguồn này.

| Tính năng | Tui Download (macOS) | tuitai (Go) | Ghi chú |
|-----------|----------------------|------------|---------|
| Tải đa kết nối (multi-thread) | Có | ✅ Có (1–32) | `internal/segments` + `Range` |
| Cấp phát động giữa các luồng | Có | ✅ Có | Luồng xong sớm "steal" nửa đuôi segment chậm |
| Resume sau khi tắt app | Có | ✅ Có | `<file>.tuitai` + ETag/Last-Modified |
| Pause/Resume | Có | ✅ Có | `tuitai pause/resume`, phím `p/r` trong TUI |
| Hàng đợi + giới hạn đồng thời | Có | ✅ Có | `internal/queue` |
| Ưu tiên job | Có | ✅ Có | `--priority` |
| Giới hạn tốc độ | Có | ✅ Có (toàn cục + theo job) | `--set rate=2M` |
| Checksum sau khi tải | Một phần | ✅ Có | `--checksum sha256:…` |
| Mirror/đa nguồn | Không rõ | ✅ Có | `--mirror` |
| Bắt link từ trình duyệt | Có (extension) | ✅ Extension MV3 + daemon | `browser-extension/` |
| Theo dõi clipboard | Có | ✅ `daemon --watch-clipboard` | `cmd/tuitai/clipboard.go` |
| Hẹn giờ tải | Có | ✅ Khung giờ (`schedule_window`) | `internal/schedule` |
| Phân loại file theo thư mục | Có | ✅ `organize_by_type` + `category_dir` | `internal/category` |
| FTP/FTPS | Không rõ | 🟡 FTP tối giản | `internal/ftp` (RETR/REST/PASV) |
| Proxy / SOCKS | Có | 🟡 Proxy HTTP | `--set proxy=http://…` |
| Tải video HLS/DASH | Có | 🟡 Nhận diện + tải từng segment; **chưa ghép** | `internal/grab` (`Playlist`) |
| Giao diện đồ hoạ | Có (AppKit) | ❌ Không (CLI/TUI) | Cố ý: ưu tiên chạy mọi nơi |
| Đa nền tảng | macOS | ✅ Windows/Linux/macOS | Cùng một mã nguồn |
| Ký số / notarize | Có | ❌ Chưa | Cần chứng chỉ |

## Điểm khác biệt có chủ đích

1. **Không có GUI**: `tuitai` nhắm vào terminal và tự động hoá (script, server),
   giống triết lý của các công cụ như `aria2`/`curl` nhưng có hàng đợi và TUI.
2. **Không can thiệp hệ thống**: không cài driver mạng, không hook trình duyệt,
   không sửa registry — chỉ là một file thực thi.
3. **Trạng thái resume là văn bản**: file `.tuitai` đọc được bằng mắt, dễ debug.

## Việc cần làm tiếp (nếu muốn tiệm cận bản gốc)

- [x] Scheduler khung giờ (`config --set schedule=01:00-07:00`).
- [ ] `--at HH:MM` cho từng job.
- [x] Phân loại theo phần mở rộng như IDM.
- [x] Extension mẫu (Chrome MV3) gọi `daemon /add`.
- [ ] Ghép segment HLS thành file hoàn chỉnh (ffmpeg).
- [ ] Tải HLS (m3u8) ghép segment.
- [ ] Đồ thị tốc độ trong TUI (sparkline).
- [ ] Giới hạn tốc độ theo lịch (ban ngày/ban đêm).
