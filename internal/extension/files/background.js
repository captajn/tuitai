// background.js — cầu nối giữa trình duyệt và `tuitai daemon`.
//
// Cài đặt: chrome://extensions → Developer mode → Load unpacked → chọn thư mục này.
// Trước đó chạy:  tuitai daemon --port 5555 [--token <chuỗi>]
// Nếu đặt token, nhập cùng token ở trang Options của extension.

const DEFAULTS = { port: 5555, token: "" };

async function settings() {
  const s = await chrome.storage.sync.get(DEFAULTS);
  return { port: s.port || DEFAULTS.port, token: s.token || "" };
}

/** Gửi một URL sang daemon; trả về {ok, id, dest, error}. */
async function send(url, name) {
  const { port, token } = await settings();
  const q = new URLSearchParams({ url });
  if (name) q.set("name", name);
  const headers = {};
  if (token) headers["X-Tuidl-Token"] = token;
  try {
    const resp = await fetch(`http://127.0.0.1:${port}/add?${q.toString()}`, {
      method: "POST",
      headers,
    });
    const data = await resp.json().catch(() => ({}));
    if (!resp.ok) return { ok: false, error: data.error || `HTTP ${resp.status}` };
    return { ok: true, id: data.id, dest: data.dest };
  } catch (e) {
    return { ok: false, error: String(e) };
  }
}

function notify(title, message) {
  // Không cần quyền notifications: dùng badge + console.
  console.log(`[Tui Download] ${title}: ${message}`);
  chrome.action.setBadgeText({ text: "✓" });
  setTimeout(() => chrome.action.setBadgeText({ text: "" }), 2000);
}

chrome.runtime.onInstalled.addListener(() => {
  chrome.contextMenus.create({
    id: "tuitai-link",
    title: "Tải bằng Tui Download",
    contexts: ["link"],
  });
  chrome.contextMenus.create({
    id: "tuitai-media",
    title: "Tải media đang phát bằng Tui Download",
    contexts: ["video", "audio"],
  });
  chrome.contextMenus.create({
    id: "tuitai-page",
    title: "Tải các file trên trang này",
    contexts: ["page"],
  });
});

chrome.contextMenus.onClicked.addListener(async (info, tab) => {
  if (info.menuItemId === "tuitai-link" && info.linkUrl) {
    const r = await send(info.linkUrl);
    notify("Link", r.ok ? r.dest : r.error);
    return;
  }
  if (info.menuItemId === "tuitai-media" && info.srcUrl) {
    const r = await send(info.srcUrl);
    notify("Media", r.ok ? r.dest : r.error);
    return;
  }
  if (info.menuItemId === "tuitai-page" && tab?.id) {
    // Quét trang để lấy link file rồi gửi lần lượt (giới hạn 50).
    const results = await chrome.scripting.executeScript({
      target: { tabId: tab.id },
      func: () => {
        const out = [];
        const re = /\.(mp4|mkv|webm|mov|avi|mp3|m4a|flac|zip|rar|7z|pdf|iso|exe|msi|apk|dmg)$/i;
        for (const el of document.querySelectorAll("a[href],video[src],audio[src],source[src]")) {
          const u = el.href || el.src;
          if (u && re.test(u)) out.push(u);
        }
        return [...new Set(out)].slice(0, 50);
      },
    });
    const urls = results?.[0]?.result || [];
    let ok = 0;
    for (const u of urls) {
      const r = await send(u);
      if (r.ok) ok++;
    }
    notify("Cả trang", `${ok}/${urls.length} link đã gửi`);
  }
});

chrome.runtime.onMessage.addListener((msg, _sender, reply) => {
  if (msg?.type === "send") {
    send(msg.url, msg.name).then(reply);
    return true; // trả lời bất đồng bộ
  }
  if (msg?.type === "ping") {
    settings().then(async ({ port, token }) => {
      const headers = token ? { "X-Tuidl-Token": token } : {};
      try {
        const r = await fetch(`http://127.0.0.1:${port}/health`, { headers });
        reply({ ok: r.ok, status: r.status });
      } catch (e) {
        reply({ ok: false, error: String(e) });
      }
    });
    return true;
  }
  return false;
});
