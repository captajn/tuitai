// options.js — lưu cổng và token vào chrome.storage.sync.
const portEl = document.getElementById("port");
const tokenEl = document.getElementById("token");
const statusEl = document.getElementById("status");

chrome.storage.sync.get({ port: 5555, token: "" }).then((s) => {
  portEl.value = s.port;
  tokenEl.value = s.token;
});

document.getElementById("save").addEventListener("click", async () => {
  const port = Number(portEl.value) || 5555;
  const token = tokenEl.value.trim();
  await chrome.storage.sync.set({ port, token });
  statusEl.textContent = "Đã lưu.";
  setTimeout(() => (statusEl.textContent = ""), 1500);
});
