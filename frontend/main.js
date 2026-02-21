const feed = document.getElementById("feed");
const loadEl = document.getElementById("load");

let loading = false;

const API_BASE = import.meta.env.VITE_API_URL ?? "";

function appendImages(urls) {
  for (const url of urls) {
    const img = document.createElement("img");
    img.src = url;
    img.loading = "lazy";
    img.alt = "";
    feed.appendChild(img);
  }
}

async function fetchPage() {
  const params = new URLSearchParams({ limit: "4" });
  const r = await fetch(`${API_BASE}/feed?${params}`);
  if (!r.ok) throw new Error("feed failed");
  return r.json();
}

async function loadMore() {
  if (loading) return;
  loading = true;
  loadEl.hidden = false;
  try {
    const data = await fetchPage();
    appendImages(data.urls);
    loadEl.textContent = "Scroll for more";
  } catch (e) {
    loadEl.textContent = "Load failed";
  } finally {
    loading = false;
    loadEl.hidden = false;
  }
}

function onScroll() {
  const { scrollTop, scrollHeight, clientHeight } = document.documentElement;
  if (scrollTop + clientHeight >= scrollHeight - 100) loadMore();
}

window.addEventListener("scroll", onScroll, { passive: true });
loadMore();
