# ART SPEC — Tui Download

Đặc tả để copy vào ChatGPT và yêu cầu tạo bộ ảnh cho Tui Download. Giải thích bằng tiếng Việt, prompt bằng tiếng Anh. Mỗi lượt tạo một ảnh riêng; giữ cùng nhân vật và hình học logo trong cả bộ.

**Đích lưu:** `E:/Code/IDM/tuitai/assets/`. Các đường dẫn bên dưới tính từ thư mục `tuitai/`.

**Trạng thái:** tài liệu yêu cầu sản xuất ảnh; ảnh mới chưa được tạo, kiểm tra hoặc tích hợp vào ứng dụng.

## 1. Thiết kế đã chốt

- Sản phẩm: trình quản lý tải xuống Windows/macOS/Linux, tên **Tui Download**.
- Tính cách: nhanh, hữu ích, vui nhẹ, gần gũi; không mang phong cách game hoặc doanh nghiệp.
- Chất liệu: kính mờ, khối bo tròn, ánh sáng mềm từ trên trái, bóng nhẹ, chiều sâu vừa phải.
- Light: nền trắng, điểm nhấn xanh lá. Dark: trắng–đen–xám hoàn toàn, kể cả mascot và bóng đổ.
- Mascot: capybara mũm mĩm, mõm bè hình chữ nhật bo tròn, tai nhỏ tròn, mắt chấm, chân ngắn; đội mũ giao hàng không chữ, mang kiện hàng hình mũi tên tải xuống. Lông trắng ngà ở light, xám nhạt ở dark; không dùng lông nâu.
- Không chữ, chữ số, logo bên thứ ba, watermark hoặc giao diện có nhãn trong ảnh. Thanh tải hoàn tất được thể hiện bằng hình học, không ghi phần trăm.
- Không tím, xanh dương, cyan, teal, hồng, cam hoặc vàng trong ảnh. Trạng thái lỗi được nhận biết bằng tư thế, dây cáp đứt và hình khối.

### Bảng màu cho ảnh

| Vai trò | Light | Dark |
|---|---|---|
| Nền đặc | `#FFFFFF`, `#F4FBF6`, `#E9F7EE` | `#000000`, `#0A0A0A`, `#141414` |
| Mascot | trắng ngà, bóng xanh lá rất nhạt | `#B3B3B3`, sáng `#FFFFFF`, bóng xám |
| Mũ / kiện hàng | `#16A34A`, `#15803D` | `#FFFFFF`, `#B3B3B3` |
| Nét / chi tiết tối | `#0D2116` | `#000000`, `#7A7A7A` |
| Thanh tiến độ | `#16A34A` → `#4ADE80` | `#FFFFFF` → `#BDBDBD` |
| Viền kính | trắng | trắng, độ trong nhẹ |

Bảng này đối chiếu với theme trong `cmd/tuitai/gui_index.html`. CSS có màu đỏ/vàng cho thông báo light; không đưa các màu trạng thái đó vào bộ minh họa. Mã màu là đích phối màu, cần kiểm tra file xuất vì ảnh sinh có thể lệch màu.

### Tham chiếu logo

Đính kèm `assets/logo.svg` nếu công cụ nhận SVG; nếu không, chuyển SVG thành PNG trước khi đính kèm. Giữ cấu trúc **mũi tên dày hướng xuống + khay hứng bo tròn + ba chấm nhỏ**. SVG hiện có dùng teal/cyan: chỉ tham chiếu hình dáng, thay màu theo bảng trên. Không sửa SVG gốc trong bước viết spec này.

## 2. Danh mục bàn giao

Tạo light trước, duyệt mẫu rồi dùng đúng ảnh đã duyệt làm tham chiếu cho dark. Tên không có hậu tố là bản light để giữ tên file đang được tài liệu assets sử dụng.

| ID | File trong `assets/` | Kích thước cuối | Nền / chế độ | Vai trò |
|---|---|---|---|---|
| A01 | `app-icon-1024.png` | 1024×1024 | RGBA, ngoài ô icon trong suốt | Icon gốc light |
| A02 | `app-icon-dark.png` | 1024×1024 | RGBA, ngoài ô icon trong suốt | Icon gốc dark |
| A03 | `mark-mono.png` | 512×512 | RGBA, nét đen, nền trong suốt | Nguồn tray/favicon light |
| A04 | `mark-mono-dark.png` | 512×512 | RGBA, nét trắng, nền trong suốt | Nguồn tray/favicon dark |
| A05 | `hero-banner.png` | 2400×1200 | RGB, nền light đặc | Bìa / About light |
| A06 | `hero-banner-dark.png` | 2400×1200 | RGB, nền dark đặc | Bìa / About dark |
| A07 | `empty-state.png` | 1200×900 | RGBA, nền trong suốt | Danh sách trống light |
| A08 | `empty-state-dark.png` | 1200×900 | RGBA, nền trong suốt | Danh sách trống dark |
| A09 | `mascot-sheet.png` | 2000×1200 | RGB, nền `#F4FBF6` | Mẫu nhân vật light, ba tư thế |
| A10 | `mascot-sheet-dark.png` | 2000×1200 | RGB, nền `#141414` | Mẫu nhân vật dark, ba tư thế |
| A11 | `mascot-success.png` | 1200×900 | RGBA, nền trong suốt | Hoàn tất light |
| A12 | `mascot-success-dark.png` | 1200×900 | RGBA, nền trong suốt | Hoàn tất dark |
| A13 | `mascot-error.png` | 1200×900 | RGBA, nền trong suốt | Lỗi light |
| A14 | `mascot-error-dark.png` | 1200×900 | RGBA, nền trong suốt | Lỗi dark |
| A15 | `cover-social.png` | 1200×630 | RGB, nền light đặc | Ảnh chia sẻ |
| A16 | `cover-social-dark.png` | 1200×630 | RGB, nền dark đặc | Ảnh chia sẻ dark |

Không yêu cầu ChatGPT xuất SVG, ICO hoặc ICNS. PNG là đầu ra tạo ảnh; định dạng vector và icon hệ điều hành cần bước dựng/chuyển đổi riêng. Grain/noise là hiệu ứng do giao diện vẽ, không phải ảnh phải tạo trong bộ này.

## 3. Cách gửi yêu cầu

1. Dán Brand brief bên dưới, chọn đúng một Theme block rồi dán prompt của asset.
2. Tạo A09 trước để chốt mascot; tạo A01 với logo đính kèm để chốt icon. Đây là hai tham chiếu độc lập.
3. Đính kèm mascot đã duyệt khi tạo A05–A16. Khi tạo dark, đính kèm thêm bản light tương ứng và yêu cầu giữ bố cục.
4. Mỗi lượt chỉ xuất một file, không ghép nhiều asset thành một ảnh. Character sheet là ngoại lệ có đúng ba tư thế trong một file.
5. Lưu ảnh gốc trước hậu kỳ. Nếu đầu ra khác kích thước yêu cầu, hoàn thiện trên canvas đúng kích thước, giữ tỷ lệ nhân vật; không kéo méo, không cắt mất chi tiết.

### Brand brief — dán trước mọi prompt

```text
Create one finished image asset for Tui Download, a friendly cross-platform download manager. The mood is calm, helpful, cozy and gently cheeky. Use rounded shapes, restrained frosted-glass materials, soft depth and one consistent upper-left light source. Keep the silhouette clear at small sizes.

For mascot artwork, use one consistent chubby capybara courier: broad rounded rectangular muzzle, two small round ears, tiny black dot eyes, short legs, compact oval body, a plain courier cap without lettering, and a parcel shaped like a downward download arrow. Keep the same face, cap shape, anatomy and proportions as the attached approved character reference. The light version has ivory fur and leaf-green accessories. The dark version has light-grey fur and white/grey accessories, with absolutely no hue.

For icon artwork, follow the attached logo geometry: one chunky downward arrow entering a shallow rounded tray, with three small speed dots. Use the selected theme palette instead of the reference image colors. Do not add a mascot to the app icon.

No text, letters, numbers, watermark, signature or third-party branding. No purple, violet, blue, cyan, teal, pink, orange or yellow. No neon, harsh glow, busy scenery or photographic humans. Produce only the requested single asset, without a presentation frame or device frame.
```

### Theme L — light

```text
LIGHT THEME: white and leaf green only. Backgrounds, when requested, use #FFFFFF, #F4FBF6 and #E9F7EE. Accessories use #16A34A and #15803D. Use #4ADE80 sparingly for progress highlights. Dark details use #0D2116. The capybara fur is ivory with subtle pale-green shading, never brown. Frosted-glass edges are white. No other hue is allowed. If transparency is requested, leave the background genuinely transparent rather than painting it white.
```

### Theme D — dark

```text
DARK THEME: strictly achromatic black, white and grey; every visible pixel must have equal red, green and blue values after final export. Backgrounds, when requested, use #000000, #0A0A0A and #141414. Use light-grey fur, a white/grey cap, white download-arrow parcel and neutral grey shadows. Preserve the exact approved light asset's composition, character proportions and lighting direction. Make the silhouette readable on #000000 and #141414. No colored reflections or colored accents. If transparency is requested, use a genuinely transparent background.
```

## 4. Prompt riêng cho từng ảnh

### A01 / A02 — app icon

Dán Brand brief + Theme L cho A01 hoặc Theme D cho A02 + prompt:

```text
Create a square app icon, final canvas 1024 by 1024 pixels. Place a centered rounded-square tile inside a safe box from x=82 to x=942 and y=82 to y=942. Keep all shadows within the canvas. Outside the tile and its soft shadow, the background must be transparent.

Use a softly frosted tile with a thin white upper-edge highlight. For light, use a leaf-green tile and a solid white symbol. For dark, use a #141414 tile and a solid white symbol. Center the attached logo's chunky downward arrow and shallow rounded tray; keep three small speed dots in the lower area. Preserve the reference geometry and generous separation between arrow, tray and dots. Use strong contrast and restrained depth. Ensure the arrow and tray remain recognizable when reduced to 32 by 32 pixels. No character, text, border frame or extra symbols.
```

### A03 / A04 — dấu đơn sắc

Đính kèm logo; A03 dùng nét đen, A04 dùng nét trắng. Nếu cần vector chính xác, dựng lại từ hình học SVG ở bước hậu kỳ.

```text
Create one flat monochrome download mark on a genuinely transparent 512 by 512 pixel canvas. Use the attached logo's downward arrow and shallow rounded tray with three small speed dots. Keep the main geometry inside a centered 384 by 384 pixel area. Use thick rounded strokes and open negative space. For the light asset, use solid black; for the dark asset, use solid white. No tile, gradients, shadows, lighting, texture or text. The arrow and tray must remain distinct at 16 by 16 pixels; remove the tiny dots only in a separately prepared small-size export if they merge.
```

### A05 / A06 — hero banner

```text
Create wide artwork, final canvas 2400 by 1200 pixels. Keep the left 40 percent calm and free of subjects, cards or distinct details so a headline can be typeset later. Keep every main subject at least 120 pixels from the outer edge.

In the right 60 percent, arrange three softly frosted-glass download cards at gentle angles. Represent their contents only with abstract short lines, one nearly full progress bar, one one-third-full bar and one paused bar with two vertical pause strokes. Do not render text or numbers. Seat the approved capybara courier beside the front card, holding the download-arrow parcel and looking quietly pleased. Use the selected theme, a soft opaque background and very restrained background gradients. Keep the face and parcel fully visible, with no cropped ears, feet or cap.
```

### A07 / A08 — empty state

```text
Create one empty-list illustration on a genuinely transparent 1200 by 900 pixel canvas. Keep the entire subject group inside x=120 to x=1080 and y=90 to y=810. Center the approved capybara courier sitting beside an empty shallow glass download tray. The courier holds an empty cup and looks patiently hopeful, with a small amused smile. Keep the composition compact and easy to read at 240 by 180 pixels. Use the selected theme and subtle contact shadows fading into transparency. No scenery, background rectangle, written labels or extra characters.
```

### A09 / A10 — mascot sheet

```text
Create one character sheet, final canvas 2000 by 1200 pixels, with exactly three full-body poses of the same capybara courier in one horizontal row. Use an opaque #F4FBF6 background for light or an opaque #141414 background for dark. Divide the canvas into three equal visual columns without drawing panel borders. Keep each character inside its column with at least 80 pixels of inner padding and at least 120 pixels of top and bottom padding. Match head size, body size, cap shape and eye placement across poses.

Left: working, standing and carrying the download-arrow parcel with a determined but friendly expression.
Middle: completed, sitting comfortably with a cup and a small satisfied smile.
Right: interrupted, holding the two ends of a disconnected network cable, one eyebrow raised, gently puzzled, never distressed.

Use the selected theme. Keep all ears, feet, accessories and cable ends fully visible. No labels, numbers, captions, grid lines or additional poses.
```

### A11 / A12 — hoàn tất

```text
Create one completion sticker on a genuinely transparent 1200 by 900 pixel canvas. Keep the subject group inside x=120 to x=1080 and y=90 to y=810. Show the approved capybara courier smiling and raising one short paw beside a compact frosted-glass progress bar that is completely filled, with one clear checkmark. Add at most five small geometric sparkles in the selected theme palette. The face, checkmark and full bar must read at 240 by 180 pixels. Use soft shading and contact shadows fading into transparency. No text, digits, percent sign or background panel behind the whole illustration.
```

### A13 / A14 — lỗi tải

```text
Create one interrupted-download sticker on a genuinely transparent 1200 by 900 pixel canvas. Keep the subject group inside x=120 to x=1080 and y=90 to y=810. Show the approved capybara courier holding two disconnected network-cable ends, shrugging gently with one eyebrow raised. Add a small outlined triangular warning symbol with a simple exclamation stroke and dot; use only the selected theme colors. Keep the mood friendly and recoverable. Do not use red or amber, tears, injury or destructive imagery. The disconnected cable must read at 240 by 180 pixels. No text, numbers or full-image background rectangle.
```

### A15 / A16 — social cover

```text
Create social-share artwork, final canvas 1200 by 630 pixels, using the selected theme and an opaque background. Keep all important artwork inside x=60 to x=1140 and y=60 to y=570. Reserve x=60 to x=660 as a calm, low-detail area for a headline to be added later. On the right, place one frosted-glass download card with a fully filled progress bar and the approved capybara courier peeking from its right edge. Keep the mascot's full face and cap visible. Use strong separation between subjects and background. No text, numbers, platform logos, watermark or device frame.
```

## 5. Prompt sửa lỗi

### Sai bảng màu

```text
Keep the exact composition, character design and logo geometry. Re-render using only the selected theme palette from the brief. Remove every forbidden hue, including in shadows and reflections. For dark, make the entire image strictly achromatic. Do not add new objects or text.
```

### Sai mascot

```text
Use the attached approved character sheet as the identity reference. Match its muzzle, ears, eyes, body proportions and courier cap exactly. Keep the requested pose and composition, changing only the inconsistent character details. Use the selected theme palette.
```

### Nền chưa trong suốt

```text
Keep the artwork unchanged. Remove the full background and export a PNG with a real alpha channel. Preserve clean antialiased edges and soft contact shadows fading to zero alpha. Do not paint a checkerboard, white matte, black matte or colored backdrop into the image.
```

Nền một màu và hình bàn cờ vẽ trong ảnh đều không phải nền trong suốt. Nếu công cụ chưa xuất được alpha, giữ bản gốc trong thư mục làm việc riêng để tách nền trước khi đặt vào `assets/`.

## 6. Kiểm tra trước khi nhận ảnh

- [ ] Đúng tên và đủ 16 PNG trong danh mục; mỗi file là đúng một asset.
- [ ] Đọc kích thước pixel thực tế của file, khớp bảng; không chỉ dựa vào lời mô tả của công cụ.
- [ ] PNG có nền đặc dùng RGB; ảnh cần nền trong dùng RGBA và có pixel alpha bằng 0 ở vùng ngoài chủ thể.
- [ ] Ảnh dark đạt R=G=B với các pixel nhìn thấy; ảnh light không có điểm nhấn ngoài palette.
- [ ] Không chữ hoặc chữ số ngoài ý muốn; không watermark hay logo bên thứ ba.
- [ ] Logo giữ hình học tham chiếu; mascot giữ cùng mặt, tỷ lệ và mũ giữa các tư thế.
- [ ] Không cắt tai, mũ, bàn chân, dây cáp hoặc bóng đổ; vùng dành cho tiêu đề đủ trống.
- [ ] Thử PNG trong suốt trên nền trắng, `#F4FBF6`, đen và `#141414`; không viền nền cũ, không ô bàn cờ bị vẽ vào ảnh.
- [ ] Xem icon ở 16, 32, 48 và 256 px; minh họa ở 240×180 px; hero và cover ở nửa kích thước.
- [ ] Kiểm tra trực quan cả hai theme trước khi ghi nhận ảnh được duyệt.

## 7. Handoff và tích hợp

Giữ ảnh nguồn và bản đã duyệt riêng để có thể truy ngược chỉnh sửa. Khi bàn giao, kèm danh sách tên file, kích thước thực, chế độ RGB/RGBA và SHA-256 của file cuối. Chỉ đánh dấu hoàn tất sau khi kiểm tra các file thực tế.

Các đầu ra tiếp theo, thực hiện sau khi PNG được duyệt:

- Windows: tạo `assets/app.ico` với các cỡ 16, 24, 32, 48, 64, 128, 256 px và kiểm tra từng cỡ.
- macOS: tạo `assets/app.icns` bằng bộ iconset chuẩn của công cụ đóng gói; đối chiếu yêu cầu mask/canvas trước khi dùng icon có ô bo góc.
- Tray/favicon: xuất từ dấu đơn sắc đã duyệt; kiểm tra riêng dark/light ở 16 và 32 px.
- Tiêu đề trên hero/social: thêm bằng công cụ dàn trang với font thật nếu cần; không yêu cầu mô hình viết chữ vào ảnh nền.

**Lưu ý mã nguồn hiện tại:** `cmd/tuitai/gui_assets.go` chỉ nhúng `gui_index.html`. Mẫu `//go:embed assets/*.png` đặt ở thư mục đó sẽ không trỏ tới thư mục `assets/` tại gốc module; Go embed không nhận đường dẫn đi ngược `../`. Khi tích hợp, cần một package nhúng tại `assets/` hoặc bố trí ảnh bên trong package GUI rồi bổ sung tuyến phục vụ ảnh và chuyển asset theo theme. Chỉ chép PNG vào `assets/` chưa làm ảnh xuất hiện trong ứng dụng. Bước tích hợp không thuộc tài liệu sản xuất ảnh này.
