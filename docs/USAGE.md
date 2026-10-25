### Khay hệ thống & tự khởi động

`ash
tuitai gui --no-open --tray     # chạy nền, icon ở khay (Windows)
tuitai autostart on             # tự chạy cùng Windows/macOS/Linux
tuitai autostart status
tuitai autostart off
`

Menu khay: **Mở Tui Tải** · **Tạm dừng tất cả** · **Chạy tiếp tất cả** · **Thoát**.
Nhấp đúp icon để mở giao diện. Khi job xong/tạch, app bắn balloon kèm icon (kiểu IDM).

- Windows: ghi HKCU\Software\Microsoft\Windows\CurrentVersion\Run (không cần quyền admin).
- macOS: ~/Library/LaunchAgents/com.captajn.tuitai.plist.
- Linux: ~/.config/autostart/tuitai.desktop.
- Tray hiện chỉ Windows (macOS/Linux chưa có tray thuần Go — vẫn chạy nền + thông báo hệ thống).

### Giao diện: glassmorphism + Light/Dark

` + "`tuitai gui`" + @' mở cửa sổ kiểu IDM với giao diện kính mờ (backdrop-blur), nền
gradient động, thẻ trong suốt và bóng đổ mềm.

- **Sáng/Tối**: nút ☀️/🌙 ở góc phải; lựa chọn lưu vào `localStorage`, mặc định
  theo `prefers-color-scheme` của hệ điều hành.
- Ép theme khi mở: `http://127.0.0.1:5566/?theme=dark` hoặc `?theme=light`.
- Chi tiết vui tính: trạng thái đổi thành *ĐANG CÀY / XONG NGON / TẠCH / NGHỈ MÁT*,
  mỗi dòng có một câu đùa, footer hiện câu "châm ngôn" ngẫu nhiên.
- Tìm kiếm ngay trong danh sách, lọc theo thư mục loại file, chọn nhiều dòng
  bằng Ctrl/⌘ + click.
- Tôn trọng `prefers-reduced-motion`: tắt animation nếu người dùng yêu cầu.
### `tuitai gui` — giao diện đồ hoạ kiểu IDM

Mở cửa sổ ứng dụng (Chrome/Edge `--app`). Giao diện gồm: thanh công cụ
(Thêm URL / Bắt đầu / Dừng / Tiếp tục / Xoá / Tuỳ chọn), cây thư mục bên trái
(Tất cả, Đang tải, Đã xong, Lỗi, Compressed, Documents, Programs…), bảng tải
xuống (tên file · kích thước · thanh tiến độ · trạng thái · tốc độ) và thanh
trạng thái dưới cùng (tổng tốc độ, số job đang tải).

```bash
tuitai gui                 # mở cửa sổ, cổng 5566
tuitai gui --port 8080     # đổi cổng
tuitai gui --no-open       # chỉ chạy server
```

GUI gọi REST API nội bộ (`/api/jobs`, `/api/add`, `/api/action`, `/api/config`),
nên có thể điều khiển bằng script nếu cần.
# Cách dùng chi tiết

## Mô hình dữ liệu

- Hàng đợi: `%AppData%\tuitai\queue.json` (Windows), `~/.config/tuitai/queue.json`
  (Linux), `~/Library/Application Support/tuitai/queue.json` (macOS).
- Cấu hình: `config.json` cùng thư mục.
- Trạng thái resume: `<file đích>.tuitai`; dữ liệu tạm: `<file đích>.part`.

## Lệnh

### `tuitai add <url...>`

| Tuỳ chọn | Ý nghĩa |
|----------|---------|
| `-o, --dir DIR` | thư mục lưu (mặc định `download_dir` trong cấu hình) |
| `-n, --name NAME` | đặt tên file (bỏ qua tên từ server) |
| `-t, --threads N` | số kết nối song song 1..32 |
| `--mirror URL` | mirror thêm (lặp lại được) |
| `--header "K: V"` | header bổ sung (lặp lại được) |
| `--referer URL` | header Referer |
| `--user`, `--pass` | Basic auth |
| `--checksum sha256:<hex>` | kiểm tra sau khi tải (md5 cũng được) |
| `--priority N` | số lớn chạy trước |
| `--no-run` | chỉ xếp hàng |
| `--json`, `--quiet` | xuất máy đọc / im lặng |

Ví dụ:

```bash
tuitai add -t 16 --header "Cookie: a=1" --checksum sha256:ab12… https://host/f.iso
```

### `tuitai run`

Chạy toàn bộ job ở trạng thái `queued`, `paused`, `failed`, `cancelled`.
Ctrl+C lần đầu sẽ huỷ và **lưu trạng thái**; chạy lại `tuitai run` để tiếp tục.

### `tuitai list` / `tuitai status [id]`

Bảng job gồm ID, trạng thái, tiến độ, kích thước, tên file, URL. `status` không
có ID thì in tổng quan theo trạng thái.

### `tuitai pause|resume|rm <id...>`

Đổi trạng thái trong `queue.json`. `rm` xoá cả file `.tuitai` đi kèm.

### `tuitai tui`

Phím: `↑/k`, `↓/j` chọn · `p` dừng · `r` tiếp tục · `d` xoá · `a` thêm URL ·
`q` thoát. TUI vẽ lại 250 ms/lần, không nhấp nháy.

### `tuitai daemon`

```bash
tuitai daemon --port 5555 --token mysecret
```

| Endpoint | Mô tả |
|----------|-------|
| `GET /health` | `{"status":"ok","version":"0.1.0"}` |
| `POST /add?url=…` | thêm job + chạy nền; trả `{"id":…,"dest":…}` |
| `POST /add` body JSON | `{"url":"…"}` |
| `GET /list` | danh sách job |

Thêm `--watch-clipboard` để tự thêm link khi bạn copy vào clipboard.

Chỉ bind `127.0.0.1`. `Origin`/`Referer` được kiểm: chỉ chấp nhận
`chrome-extension://`, `moz-extension://`, `127.0.0.1`, `localhost`. Nếu đặt
`--token`, mọi request phải kèm header `X-Tuidl-Token`.

### `tuitai grab <url trang>`

Rút link file/media từ một trang HTML rồi (mặc định) tải luôn.

| Tuỳ chọn | Ý nghĩa |
|----------|---------|
| `--ext mp4,m3u8` | chỉ lấy các phần mở rộng này |
| `--pages` | lấy cả link trang (không có phần mở rộng) |
| `--limit N` | giới hạn số link (mặc định 2000) |
| `--no-run` | chỉ liệt kê, không tải |
| `-o DIR`, `-t N` | thư mục lưu / số kết nối |

```bash
tuitai grab https://example.com/videos --ext mp4,m3u8 --no-run   # xem trước
tuitai grab https://example.com/videos --ext mp4                 # tải luôn
```

Link `.m3u8` được nhận diện là playlist; `grab` tải **từng segment** (chưa ghép
thành file hoàn chỉnh — cần ffmpeg).

### `tuitai import <file|->`

Đọc danh sách URL (mỗi dòng một URL, `#` là chú thích, có thể kèm tên file sau
khoảng trắng/tab):

```bash
tuitai import urls.txt --no-run
curl -s https://example.com/list.txt | tuitai import -
```

### `tuitai hls <m3u8>`

Tải playlist HLS: chọn variant đầu tiên, tải song song các segment (kèm
`EXT-X-MAP` nếu có), rồi ghép lại. Không có ffmpeg thì nối nhị phân (đúng với
MPEG-TS / fMP4); có ffmpeg thì remux sang MP4.

`ash
tuitai hls https://host/live/master.m3u8 -o D:\Video -t 6
tuitai config --set ffmpeg=C:\ffmpeg\bin\ffmpeg.exe
`

### `tuitai doctor`

In chẩn đoán: thư mục cấu hình, dung lượng trống, ffmpeg, clipboard, khung giờ.

### `tuitai list --csv <file>`

Xuất danh sách job ra CSV (id, state, bytes, total, url, dest, created_at).

### Hẹn giờ từng job

`ash
tuitai add https://host/big.iso --at 02:00        # 2h sáng gần nhất
tuitai add https://host/x.iso --at 2026-10-26T23:30:00+07:00
`
### `tuitai probe <url>`

In kích thước, khả năng `Range`, ETag, Last-Modified, tên file, MIME — dùng để
biết trước server có tải song song được không.

### `tuitai config --set k=v`

| Khoá | Ví dụ | Ý nghĩa |
|------|-------|---------|
| `dir` | `D:\Downloads` | thư mục mặc định |
| `threads` | `16` | số kết nối mỗi job |
| `max_concurrent` | `3` | số job đồng thời |
| `rate` | `2M`, `500k` | giới hạn tốc độ toàn cục (0 = không) |
| `per_job_rate` | `1M` | giới hạn mỗi job |
| `proxy` | `http://127.0.0.1:8080` | proxy HTTP |
| `retries` | `5` | số lần thử lại mỗi chunk |
| `resume` | `true` | tự resume khi chạy lại |
| `verify_tls` | `false` | tắt kiểm tra chứng chỉ (cẩn thận) |
| `timeout` | `30s` | hạn một request |
| `organize` | `true` | tự phân loại vào thư mục con |
| `category_dir` | `video=Phim` | đổi tên thư mục con của một loại |
| `schedule` | `01:00-07:00` | chỉ tải trong khung giờ (rỗng = mọi lúc) |
| `offpeak_rate` | `5M` | giới hạn áp ngoài khung giờ (0 = dùng `rate`) |
| `notify` | `true` | thông báo khi job xong |

## Mã thoát

| Mã | Ý nghĩa |
|----|---------|
| 0 | thành công |
| 1 | lỗi chung |
| 2 | dùng sai lệnh/tuỳ chọn |
| 3 | không tìm thấy job |
| 4 | xung đột trạng thái |
| 5 | lỗi I/O (thư mục, file) |
| 6 | lỗi mạng |

## Khắc phục sự cố

- **`rename … file is being used`**: file `.part` còn bị tiến trình khác giữ;
  `tuitai` đã đóng file trước khi đổi tên, lỗi này thường do antivirus quét.
- **Server không hỗ trợ Range**: `tuitai probe` báo `Hỗ trợ Range : false`, khi đó
  tải một luồng (vẫn resume được nếu server trả `Accept-Ranges` ở lần sau).
- **Resume không chạy**: ETag/Last-Modified đã đổi → cố ý tải lại từ đầu để tránh
  file lai. Xoá `.tuitai` nếu muốn bỏ qua kiểm tra.
