// popup.js — gửi link thủ công hoặc quét link của tab hiện tại.
const msg = document.getElementById("msg");

function say(text) {
  msg.textContent = text;
}

async function sendLines() {
  const lines = document
    .getElementById("url")
    .value.split("\n")
    .map((s) => s.trim())
    .filter((s) => /^https?:\/\//i.test(s));
  if (!lines.length) return say("Chưa có URL hợp lệ.");
  let ok = 0;
  for (const u of lines) {
    const r = await chrome.runtime.sendMessage({ type: "send", url: u });
    if (r?.ok) ok++;
  }
  say(`Đã gửi ${ok}/${lines.length} link.`);
}

document.getElementById("send").addEventListener("click", sendLines);

document.getElementById("page").addEventListener("click", async () => {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tab?.id) return say("Không có tab.");
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
    const r = await chrome.runtime.sendMessage({ type: "send", url: u });
    if (r?.ok) ok++;
  }
  say(`Trang này: ${ok}/${urls.length} link đã gửi.`);
});

document.getElementById("health").addEventListener("click", async () => {
  const r = await chrome.runtime.sendMessage({ type: "ping" });
  say(r?.ok ? "Daemon đang chạy." : `Không kết nối được: ${r?.error || r?.status}`);
});
