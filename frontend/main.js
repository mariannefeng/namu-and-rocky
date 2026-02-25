const feed = document.getElementById("feed");
const loadEl = document.getElementById("load");

let loading = false;

const API_BASE = import.meta.env.VITE_API_URL ?? "";

const posthogKey = import.meta.env.VITE_POSTHOG_API_KEY ?? "";
if (posthogKey && typeof window.posthog !== "undefined") {
  window.posthog.init(posthogKey, {
    api_host: "https://us.i.posthog.com",
    defaults: "2026-01-30",
  });
}

function appendImages(urls) {
  for (const url of urls) {
    const img = document.createElement("img");
    img.src = url;
    img.loading = "lazy";
    img.alt = "";
    feed.appendChild(img);
  }
}
function getClientKey() {
  let key = localStorage.getItem("namu-and-rocky-key");
  if (!key) {
    key = crypto.randomUUID();
    localStorage.setItem("namu-and-rocky-key", key);
  }
  return key;
}

async function fetchPage() {
  const params = new URLSearchParams({ limit: "10", key: getClientKey() });
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

const HAS_VOTED_KEY = "namu-rocky-has-voted";
const BANNER_HIDDEN_KEY = "namu-rocky-banner-hidden";

function setHasVoted() {
  localStorage.setItem(HAS_VOTED_KEY, "1");
  document.getElementById("vote-bar").classList.add("has-voted");
  document.getElementById("vote-question").classList.add("has-voted");
}

function hideBanner() {
  localStorage.setItem(BANNER_HIDDEN_KEY, "1");
  document.getElementById("vote-bar").classList.add("banner-hidden");
  document.body.classList.add("banner-hidden");
}

// Show "See the consensus" and persist banner visibility on load
if (localStorage.getItem(HAS_VOTED_KEY)) {
  document.getElementById("vote-bar").classList.add("has-voted");
  if (localStorage.getItem(BANNER_HIDDEN_KEY)) {
    hideBanner();
  }
}

async function sendVote(namuIsTuxedo) {
  try {
    const r = await fetch(`${API_BASE}/vote`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        key: getClientKey(),
        namu_is_tuxedo: namuIsTuxedo,
      }),
    });
    if (!r.ok) throw new Error("vote failed");
    setHasVoted();
  } catch (e) {
    console.error("Vote failed", e);
  }
}

function renderConsensusChart(namuTuxedo, namuNotTuxedo, container) {
  const total = namuTuxedo + namuNotTuxedo;
  const max = Math.max(namuTuxedo, namuNotTuxedo, 1);
  container.innerHTML = "";
  const rows = [
    {
      label: "Namu is the tuxedo",
      count: namuTuxedo,
      fillClass: "namu-tuxedo",
    },
    {
      label: "Rocky is the tuxedo",
      count: namuNotTuxedo,
      fillClass: "namu-not-tuxedo",
    },
  ];
  for (const row of rows) {
    const pct = total ? Math.round((row.count / total) * 100) : 0;
    const widthPct = max ? (row.count / max) * 100 : 0;
    const wrap = document.createElement("div");
    wrap.className = "bar-wrap";
    wrap.innerHTML = `
      <div class="bar-label"><span>${row.label}</span><span>${row.count} vote${row.count !== 1 ? "s" : ""} (${pct}%)</span></div>
      <div class="bar-track"><div class="bar-fill ${row.fillClass}" style="width:${widthPct}%"></div></div>
    `;
    container.appendChild(wrap);
  }
}

async function showConsensus() {
  const voteBar = document.getElementById("vote-bar");
  const resultsEl = document.getElementById("consensus-results");
  const chartEl = document.getElementById("consensus-chart");
  try {
    const r = await fetch(`${API_BASE}/consensus`);
    if (!r.ok) throw new Error("consensus failed");
    const data = await r.json();
    renderConsensusChart(
      data.namu_is_tuxedo ?? 0,
      data.namu_is_not_tuxedo ?? 0,
      chartEl,
    );
    resultsEl.classList.add("visible");
    voteBar.classList.add("consensus-visible");
  } catch (e) {
    console.error("Consensus failed", e);
  }
}

document
  .getElementById("btn-namu")
  .addEventListener("click", () => sendVote(true));
document
  .getElementById("btn-rocky")
  .addEventListener("click", () => sendVote(false));
document
  .getElementById("btn-consensus")
  .addEventListener("click", showConsensus);
document.getElementById("bar-dismiss").addEventListener("click", hideBanner);
