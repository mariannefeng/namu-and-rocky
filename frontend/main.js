const feed = document.getElementById("feed");
const loadEl = document.getElementById("load");

let nextCursor = null;
let loading = false;

function isPortrait(el) {
  return el.classList.contains("loaded") && !el.classList.contains("landscape");
}

function isLandscape(el) {
  return el.classList.contains("landscape");
}

/** Find loaded portraits that sit alone in a row (next item is landscape or end). */
function getLonePortraitIndices(imgs) {
  const lone = [];
  let col = 0;
  for (let i = 0; i < imgs.length; i++) {
    const el = imgs[i];
    const landscape = isLandscape(el);
    const next = imgs[i + 1];
    const nextIsLandscape = next ? isLandscape(next) : true;
    if (landscape) {
      col = 0;
    } else {
      if (isPortrait(el) && col === 0 && nextIsLandscape) lone.push(i);
      col = (col + 1) % 2;
    }
  }
  return lone;
}

function updatePortraitSpans() {
  let imgs = [...feed.querySelectorAll("img")];

  // Pair lone portraits: move the second to sit next to the first so one row is filled
  for (;;) {
    const lone = getLonePortraitIndices(imgs);
    if (lone.length < 2) break;
    const first = imgs[lone[0]];
    const second = imgs[lone[1]];
    feed.insertBefore(second, first.nextSibling);
    imgs = [...feed.querySelectorAll("img")];
  }

  imgs.forEach((el) => el.classList.remove("span-full"));
  const portraits = imgs.filter(isPortrait);
  if (portraits.length % 2 === 1)
    portraits[portraits.length - 1].classList.add("span-full");
}

function appendImages(urls) {
  for (const url of urls) {
    const img = document.createElement("img");
    img.loading = "lazy";
    img.alt = "";
    img.onload = () => {
      if (img.naturalWidth >= img.naturalHeight) img.classList.add("landscape");
      img.classList.add("loaded");
      updatePortraitSpans();
    };
    img.src = url;
    feed.appendChild(img);
  }
}

const API_BASE = import.meta.env.VITE_API_URL ?? "";

async function fetchPage(cursor) {
  const params = new URLSearchParams({ limit: "4" });
  if (cursor) params.set("cursor", cursor);
  const r = await fetch(`${API_BASE}/feed?${params}`);
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
