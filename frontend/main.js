const feed = document.getElementById("feed");
const loadEl = document.getElementById("load");

let nextCursor = null;
let loading = false;

function appendImages(urls) {
  for (const url of urls) {
    const img = document.createElement("img");
    img.src = url;
    img.loading = "lazy";
    img.alt = "";
    feed.appendChild(img);
  }
}

async function fetchPage(cursor) {
  const params = new URLSearchParams({ limit: "5" });
  if (cursor) params.set("cursor", cursor);
  const r = await fetch(`/feed?${params}`);
  if (!r.ok) throw new Error("feed failed");
  return r.json();
}

async function loadMore() {
  if (loading) return;
  loading = true;
  loadEl.hidden = false;
  try {
    const data = await fetchPage(nextCursor);
    appendImages(data.urls);
    nextCursor = data.nextCursor ?? null;
    if (!nextCursor) loadEl.textContent = "";
    else loadEl.textContent = "Scroll for more";
  } catch (e) {
    loadEl.textContent = "Load failed";
  } finally {
    loading = false;
    loadEl.hidden = !nextCursor;
  }
}

function onScroll() {
  if (!nextCursor) return;
  const { scrollTop, scrollHeight, clientHeight } = document.documentElement;
  if (scrollTop + clientHeight >= scrollHeight - 100) loadMore();
}

window.addEventListener("scroll", onScroll, { passive: true });
loadMore();
