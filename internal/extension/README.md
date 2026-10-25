# Extension trình duyệt (Chrome/Edge/Brave)

Cầu nối giữa trình duyệt và `tuitai daemon`.

## Cài

1. Chạy daemon:  `tuitai daemon --port 5555`  (thêm `--token abc` nếu muốn bảo vệ)
2. Mở `chrome://extensions` → bật **Developer mode** → **Load unpacked** → chọn thư mục này.
3. (Tuỳ chọn) mở **Options** của extension để nhập cổng/token.

## Dùng

- Chuột phải vào link → **Tải bằng Tui Download**
- Chuột phải vào video/audio → **Tải media đang phát bằng Tui Download**
- Chuột phải giữa trang → **Tải các file trên trang này** (tối đa 50 link)
- Bấm icon extension → dán nhiều URL hoặc **Lấy link trang này**

> Firefox: dùng `browser_specific_settings` + `background.scripts` (MV2/MV3 khác Chrome);
> phần lõi `background.js` dùng API `chrome.*` tương thích nên chỉ cần đổi manifest.

## Bảo mật

Extension chỉ gọi `http://127.0.0.1` và gửi header `X-Tuidl-Token` nếu có đặt.
Daemon chỉ bind loopback và kiểm `Origin`/`Referer`.
