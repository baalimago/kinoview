const media = {};

// ── The troupe stage ──────────────────────────────────────────────────────
// Phase 9 cutover: the troupe is the only splash path. The engine
// (engine.js) self-mounts into <div id="troupe"> when a resolved play is
// present; index.js fetches the newest submitted play from the API and hands
// it over. An empty stage (404) renders nothing — no seed, no fallback: an
// empty stage is the signal to investigate.
(function() {
  function mountPlay(play) {
    window.TROUPE_PLAY = play;
    var el = document.getElementById("troupe");
    if (el && window.TroupeEngine) window.TroupeEngine.mount(el, play);
  }

  fetch("/api/v1/troupe/play/resolved")
    .then(function(res) {
      if (!res.ok) throw new Error("no play: " + res.status);
      return res.json();
    })
    .then(mountPlay)
    .catch(function(err) {
      // No submitted play — the empty stage. The engine renders nothing.
      console.info("troupe: " + err.message);
    });
})();

// ── Lag detection ──
(function() {
  var samples = [];
  var MAX_SAMPLES = 20;
  var lastTime = performance.now();
  var threshold = 40; // ms — flag if avg frame > 40ms (below ~25fps)

  function measureFrame() {
    var now = performance.now();
    var delta = now - lastTime;
    lastTime = now;
    if (samples.length < MAX_SAMPLES) {
      samples.push(delta);
      requestAnimationFrame(measureFrame);
    } else {
      var sum = 0;
      for (var i = 0; i < samples.length; i++) sum += samples[i];
      var avg = sum / samples.length;
      if (avg > threshold) {
        document.body.classList.add("low-perf");
        console.info("low-perf mode: avg frame " + avg.toFixed(1) + "ms");
      }
    }
  }

  requestAnimationFrame(measureFrame);
})();

const ogConsoleLog = console.log;
const ogConsoleError = console.error;

console.log = postInfo;
console.error = postErr;

function postLogMsg(level, data) {
  if (typeof data === "object") {
    data = JSON.stringify(data, null, 2);
  }

  fetch("/gallery/log", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      level: level,
      message: data,
    }),
  })
    .then((response) => {
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      return response.text();
    })
    .catch((error) => {
      ogConsoleError("Error posting log:", error);
    });
}

function postErr(data) {
  postLogMsg("error", data);
  ogConsoleError(data);
}

function postInfo(data) {
  postLogMsg("info", data);
  ogConsoleLog(data);
}

function getPersistedMedia() {
  try {
    let media = localStorage.getItem("media");
    if (!media) {
      return {};
    }
    return JSON.parse(media);
  } catch (err) {
    console.error("failed to load media from localStorage", err);
  }
  return {};
}

function loadPersistedMediaItem(vID) {
  const media = getPersistedMedia();
  const item = media[vID];
  if (!item) {
    console.error(`media item with id: ${vID} not found in media store`);
    return {};
  }
  return item;
}

function videoNameWithProgress(vID, vidName) {
  let name = vidName;
  const storedItem = loadPersistedMediaItem(vID);
  if (!storedItem) {
    return name;
  }
  const playTime = storedItem.playedFor;
  if (playTime) {
    const asMin = (playTime / 60).toFixed(3);
    name += ` - ${asMin} min`;
  }
  return name;
}

// prettyMediaName returns a human-readable display name from an item.
// Prefers classified metadata over raw filenames:
//   Movies → "Movie Title"
//   Shows  → "Show Name · S1·E5" or "Show Name · S1·E5 – Episode Title"
//   Fallback → cleaned-up filename (dots/underscores → spaces, extension stripped)
function prettyMediaName(it) {
  if (!it) return "";
  var md = it.Metadata && typeof it.Metadata === "object" ? it.Metadata : null;

  // 1. Classified name (movie title or episode title from metadata)
  var classifiedName = "";
  if (md && md.name && typeof md.name === "string")
    classifiedName = md.name.trim();

  // 2. Show name from metadata (set during classification or show extraction)
  var showName = "";
  if (md && md.showName && typeof md.showName === "string")
    showName = md.showName.trim();
  if (!showName) {
    // Try Metadata.title as a fallback
    if (md && md.title && typeof md.title === "string")
      showName = md.title.trim();
  }

  // 3. Season and episode numbers
  var season = md && md.season ? md.season : 0;
  var episode = md && md.episode ? md.episode : 0;

  // Show episode path: "Show Name · S1·E5" or "Show Name · S1·E5 – Episode Title"
  if (showName && season && episode) {
    var result = showName + " \u00B7 S" + season + "\u00B7E" + episode;
    if (classifiedName && classifiedName !== showName) {
      result += " \u2013 " + classifiedName;
    }
    return result;
  }

  // Movie path: just the classified name
  if (classifiedName) return classifiedName;

  // Fallback: clean up the raw filename
  var name = it.Name || "";
  name = name.replace(/\.[^.]+$/, ""); // strip extension
  name = name.replace(/[._-]/g, " ").replace(/\s+/g, " ").trim();
  return name || it.Name || "";
}

fetch("/gallery?start=0&am=1000&mime=video")
  .then((response) => response.json())
  .then((data) => {
    populateMediaDropdown(data.items);
    // Handle deep-link play from shows page
    autoPlayFromQuery();
  })
  .catch((err) => {
    console.error("Error fetching gallery:");
    console.error(err);
  });

let searchDebounceTimer = null;
const SEARCH_DEBOUNCE_MS = 250;
const MAX_SEARCH_RESULTS = 5;

// Auto-play an episode when navigated from shows page via ?play=ID
function autoPlayFromQuery() {
  const params = new URLSearchParams(window.location.search);
  const playID = params.get("play");
  if (playID && media[playID]) {
    selectMedia(playID);
    // Clean URL without reload
    const url = new URL(window.location);
    url.searchParams.delete("play");
    window.history.replaceState({}, "", url);
  }
}

function searchMedia() {
  clearTimeout(searchDebounceTimer);
  searchDebounceTimer = setTimeout(() => {
    const query = document.getElementById("searchInput").value.trim();
    let url = "/gallery?start=0&am=1000&mime=video";
    if (query) {
      url += "&search=" + encodeURIComponent(query);
    }
    fetch(url)
      .then((response) => response.json())
      .then((data) => {
        populateMediaDropdown(data.items);
        populateSearchResults(data.items, query);
      })
      .catch((err) => {
        console.error("Error searching media:");
        console.error(err);
      });
  }, SEARCH_DEBOUNCE_MS);
}

function populateSearchResults(items, query) {
  const resultsDiv = document.getElementById("searchResults");
  resultsDiv.innerHTML = "";

  if (!query || items.length === 0) {
    resultsDiv.classList.add("hidden");
    return;
  }

  const topItems = items.slice(0, MAX_SEARCH_RESULTS);
  for (const it of topItems) {
    const row = document.createElement("div");
    row.className = "search-result-item";

    const nameSpan = document.createElement("span");
    nameSpan.className = "result-name";
    nameSpan.textContent = videoNameWithProgress(it.ID, it.Name);

    const pathSpan = document.createElement("span");
    pathSpan.className = "result-path";
    pathSpan.textContent = it.Path || "";

    row.appendChild(nameSpan);
    row.appendChild(pathSpan);

    row.addEventListener("click", () => {
      selectMedia(it.ID);
      document.getElementById("searchInput").value = it.Name;
      document.getElementById("searchResults").classList.add("hidden");
    });

    resultsDiv.appendChild(row);
  }

  if (items.length > MAX_SEARCH_RESULTS) {
    const more = document.createElement("div");
    more.className = "search-results-empty";
    more.textContent = `... and ${items.length - MAX_SEARCH_RESULTS} more (refine search)`;
    resultsDiv.appendChild(more);
  }

  resultsDiv.classList.remove("hidden");
}

// populateMediaDropdown indexes video items into the in-memory `media` map and
// syncs the persisted store. (Historically it also populated a <select>; that
// crude control was replaced by the search-driven UI.)
function populateMediaDropdown(items) {
  items.sort((a, b) => a.Name.localeCompare(b.Name));
  const persistedMedia = getPersistedMedia();
  for (const i of items) {
    if (!i.MIMEType.includes("video")) {
      continue;
    }
    media[i.ID] = i;
    const storageItem = loadPersistedMediaItem(i.ID);
    storageItem.name = i.Name;
    persistedMedia[i.ID] = storageItem;
  }
  localStorage.setItem("media", JSON.stringify(persistedMedia));
}

var mostRecentID = "";
var sessionID = "";
var sessionStartTime = null;

function getSessionID() {
  if (!sessionID) {
    sessionID = generateUUID();
  }
  return sessionID;
}

function getSessionStartTime() {
  if (!sessionStartTime) {
    sessionStartTime = new Date().toISOString();
  }
  return sessionStartTime;
}

function generateUUID() {
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, function(c) {
    const r = (Math.random() * 16) | 0;
    const v = c === "x" ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

// selectMedia loads a media item into the custom player. The heavy lifting
// (resume, transcode-aware seeking, autoplay) lives in the Player module below;
// this thin wrapper keeps the many existing call sites working.
function selectMedia(id) {
  mostRecentID = id;
  loadStreams(id);
  if (window.Player) {
    window.Player.load(id);
  }
}

function constuctClientContext() {
  const viewingHistory = [];
  const persistedMedia = getPersistedMedia();
  Object.values(persistedMedia).forEach((i) => {
    if (i.viewedAt) {
      const playedForFloat = i.playedFor;
      i.playedFor = `${playedForFloat} seconds`;
      viewingHistory.push(i);
    }
  });
  return {
    viewingHistory: viewingHistory,
  };
}

function requestRecommendation() {
  const inp = document.getElementById("recommendInput");
  const status = document.getElementById("recommendationStatus");
  const btn = document.getElementById("recommendBtn");
  if (!inp.value.trim()) {
    status.innerText = "Tell the concierge what you feel like watching first.";
    return;
  }
  const req = JSON.stringify({
    request: inp.value,
    context: constuctClientContext(),
  });
  console.info("Sending:", req);
  status.innerText = "Consulting the concierge… (this may take a moment)";
  if (btn) {
    btn.classList.add("loading");
    btn.querySelector("span").innerText = "Thinking…";
  }
  fetch("/gallery/recommend", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: req,
  })
    .then((r) => {
      if (!r.ok) throw new Error("status " + r.status);
      return r.json();
    })
    .then((item) => {
      if (!item || !item.ID) {
        status.innerText = "No recommendation found — try rephrasing.";
        return;
      }
      status.innerText = "▶ Now playing: " + prettyMediaName(item);
      selectMedia(item.ID);
    })
    .catch((err) => {
      console.error("recommend error:");
      console.error(err);
      status.innerText = "Error — check kinoview server logs, or console logs";
    })
    .finally(() => {
      if (btn) {
        btn.classList.remove("loading");
        btn.querySelector("span").innerText = "Recommend";
      }
    });
}

function loadStreams(id) {
  fetch(`/gallery/streams/${id}`)
    .then((response) => response.json())
    .then((data) => {
      console.log(`Attempting to load streams for: ${id}`);

      const subMenu = document.getElementById("subsMenu");
      const audioMenu = document.getElementById("audioMenu");

      if (subMenu) subMenu.innerHTML = "";
      if (audioMenu) audioMenu.innerHTML = "";

      // Add "Off" option for subtitles
      if (subMenu) {
        const offBtn = createDropdownItem(
          "Off",
          () => {
            selectSubtitle("off");
            updateActiveItem(subMenu, offBtn);
          },
          true,
        );
        subMenu.appendChild(offBtn);
      }

      let hasAudio = false;
      let audioTrackIndex = 0;

      // Check if streams is array, sometimes it might be null if find returned empty
      if (data.streams) {
        for (const i of data.streams) {
          // Audio
          if (i.codec_type === "audio") {
            hasAudio = true;
            const currentAudioTrackIndex = audioTrackIndex;
            audioTrackIndex++;
            const lang =
              i.tags && i.tags.language ? i.tags.language : `Track ${i.index}`;
            const title =
              i.tags && i.tags.title ? `${i.tags.title} (${lang})` : lang;

            const isDefault = i.disposition && i.disposition.default;
            if (audioMenu) {
              const btn = createDropdownItem(
                title,
                () => {
                  selectAudio(currentAudioTrackIndex);
                  updateActiveItem(audioMenu, btn);
                },
                isDefault,
              );
              audioMenu.appendChild(btn);
            }
          }

          // Subtitles
          if (i.codec_type === "subtitle") {
            // Relaxed check: include even if no language tag
            const lang =
              i.tags && i.tags.language ? i.tags.language : `Track ${i.index}`;
            const title =
              i.tags && i.tags.title ? `${i.tags.title} (${lang})` : lang;

            if (subMenu) {
              const btn = createDropdownItem(title, () => {
                selectSubtitle(i.index);
                updateActiveItem(subMenu, btn);
              });
              subMenu.appendChild(btn);
            }
          }
        }
      }

      if (!hasAudio && audioMenu) {
        const btn = createDropdownItem("Default Audio", () => { }, true);
        audioMenu.appendChild(btn);
      }
    });
}

function toggleMenu(menuId) {
  const menu = document.getElementById(menuId);
  if (!menu) return;

  document.querySelectorAll(".popover").forEach((m) => {
    if (m.id !== menuId) m.classList.add("hidden");
  });

  menu.classList.toggle("hidden");
}

// Close menus when clicking outside
document.addEventListener("click", (e) => {
  if (!e.target.closest(".ctrl-menu")) {
    document
      .querySelectorAll(".popover")
      .forEach((m) => m.classList.add("hidden"));
  }
  if (!e.target.closest(".header-search")) {
    const sr = document.getElementById("searchResults");
    if (sr) sr.classList.add("hidden");
  }
});

function createDropdownItem(text, onClick, isActive = false) {
  const btn = document.createElement("button");
  btn.className = "dropdown-item";
  if (isActive) btn.classList.add("active");
  btn.innerText = text;
  btn.onclick = onClick;
  return btn;
}

function updateActiveItem(container, activeItem) {
  container
    .querySelectorAll(".dropdown-item")
    .forEach((item) => item.classList.remove("active"));
  activeItem.classList.add("active");
  container.classList.add("hidden");
}

function selectAudio(index) {
  const video = document.getElementById("screen");
  if (video.audioTracks) {
    for (let i = 0; i < video.audioTracks.length; i++) {
      video.audioTracks[i].enabled = i === index;
    }
  }
  console.log(`Selected audio stream: ${index}`);
}

function selectSubtitle(id) {
  const track = document.getElementById("subs");

  if (id === "off" || id === "") {
    console.log("Disabling subtitles");
    track.src = "";
    track.removeAttribute("src");
    if (track.track) track.track.mode = "disabled";
  } else {
    console.log(
      `Attempting to set subs to: /gallery/streams/${mostRecentID}/stream/${id}`,
    );
    track.src = `/gallery/streams/${mostRecentID}/stream/${id}`;
    if (track.track) track.track.mode = "showing";
  }
}

// Integrate events.js
(function() {
  const script = document.createElement("script");
  script.src = "events.js";
  script.async = true;
  document.head.appendChild(script);

  loadSuggestions();
})();

function loadSuggestions() {
  fetch("/gallery/suggestions")
    .then(function(response) {
      if (!response.ok) throw new Error("status " + response.status);
      return response.json();
    })
    .then(function(payload) {
      renderSuggestionsFromPayload(payload);
    })
    .catch(function(err) {
      console.error("Failed to load suggestions:", err);
      // Show empty state on fetch failure.
      var container = document.getElementById("butler-suggestions");
      if (container) container.style.display = "block";
      var emptyEl = document.getElementById("suggestions-empty");
      if (emptyEl) emptyEl.style.display = "block";
    });
}

function formatAge(rfc3339) {
  try {
    var then = new Date(rfc3339);
    var now = new Date();
    var diffSec = Math.floor((now - then) / 1000);
    if (diffSec < 60) return "just now";
    if (diffSec < 3600) return Math.floor(diffSec / 60) + "m ago";
    if (diffSec < 86400) return Math.floor(diffSec / 3600) + "h ago";
    return Math.floor(diffSec / 86400) + "d ago";
  } catch (e) {
    return "";
  }
}

// handleSuggestionsEvent is called from events.js when the server pushes
// fresh suggestions through the websocket. It re-renders the shelf live.
function handleSuggestionsEvent(payload) {
  renderSuggestionsFromPayload(payload);
}

function suggestionKindLabel(kind) {
  switch (kind) {
    case "movie":
      return "Movie";
    case "episode":
      return "Episode";
    case "extras":
      return "Extras";
    default:
      return "Media";
  }
}

// buildSuggestionCard renders one suggestion as a rich card. The server
// attaches a resolved `view` (kind, title, series position, year, duration,
// description …) to every suggestion; older servers without it fall back to
// the legacy title + motivation layout.
function buildSuggestionCard(rec) {
  var card = document.createElement("div");
  card.className = "suggestion-item";
  card.onclick = function() {
    selectMedia(rec.ID);
    if (rec.subtitleID) {
      setTimeout(function() {
        selectSubtitle(rec.subtitleID);
      }, 500);
    }
  };

  var view = rec.view && typeof rec.view === "object" ? rec.view : null;
  if (!view || !view.title) {
    var legacyTitle = document.createElement("strong");
    legacyTitle.className = "suggestion-title";
    legacyTitle.innerText = prettyMediaName(rec);
    card.appendChild(legacyTitle);
    var legacyMot = document.createElement("p");
    legacyMot.className = "suggestion-motivation";
    legacyMot.innerText = rec.motivation || "";
    card.appendChild(legacyMot);
    return card;
  }

  // Badge row: kind pill + series position pill for episodes.
  var badgeRow = document.createElement("div");
  badgeRow.className = "suggestion-badge-row";
  var badge = document.createElement("span");
  badge.className = "suggestion-kind suggestion-kind-" + view.kind;
  badge.innerText = suggestionKindLabel(view.kind);
  badgeRow.appendChild(badge);
  if (view.kind === "episode" && view.season && view.episode) {
    var pos = document.createElement("span");
    pos.className = "suggestion-pos";
    pos.innerText = "S" + view.season + "\u00B7E" + view.episode;
    badgeRow.appendChild(pos);
  }
  card.appendChild(badgeRow);

  var title = document.createElement("strong");
  title.className = "suggestion-title";
  title.innerText = view.title;
  card.appendChild(title);

  if (view.episodeTitle) {
    var epTitle = document.createElement("div");
    epTitle.className = "suggestion-ep-title";
    epTitle.innerText = view.episodeTitle;
    card.appendChild(epTitle);
  }

  var metaParts = [];
  if (view.year) metaParts.push(String(view.year));
  if (view.durationMin) metaParts.push(view.durationMin + " min");
  if (view.language) metaParts.push(view.language);
  if (metaParts.length) {
    var meta = document.createElement("div");
    meta.className = "suggestion-meta";
    meta.innerText = metaParts.join(" \u00B7 ");
    card.appendChild(meta);
  }

  if (view.description) {
    var desc = document.createElement("p");
    desc.className = "suggestion-desc";
    desc.innerText = view.description;
    card.appendChild(desc);
  }

  if (rec.motivation) {
    var mot = document.createElement("p");
    mot.className = "suggestion-motivation";
    mot.innerText = rec.motivation;
    card.appendChild(mot);
  }

  return card;
}

function renderSuggestionsFromPayload(payload) {
  var suggestions = payload.suggestions || [];
  var state = payload.state || "empty";
  var generated = payload.generated || "";

  var container = document.getElementById("butler-suggestions");
  if (!container) return;
  var list = document.getElementById("suggestions-list");
  var computingEl = document.getElementById("suggestions-computing");
  var emptyEl = document.getElementById("suggestions-empty");
  var ageEl = document.getElementById("suggestions-age");

  // Hide all sub-views.
  if (list) list.style.display = "none";
  if (computingEl) computingEl.style.display = "none";
  if (emptyEl) emptyEl.style.display = "none";

  if (state === "available" && suggestions.length > 0) {
    container.style.display = "block";
    if (list) {
      list.style.display = "block";
      list.innerHTML = "";
      suggestions.forEach(function(rec) {
        list.appendChild(buildSuggestionCard(rec));
      });
    }
    if (ageEl) {
      ageEl.innerText = generated
        ? "Chosen " + formatAge(generated)
        : "Chosen for you";
    }
  } else if (state === "computing") {
    container.style.display = "block";
    if (computingEl) computingEl.style.display = "block";
    // Show previous suggestions alongside skeleton.
    if (suggestions.length > 0 && list) {
      list.style.display = "block";
      list.innerHTML = "";
      suggestions.forEach(function(rec) {
        list.appendChild(buildSuggestionCard(rec));
      });
    }
  } else {
    container.style.display = "block";
    if (emptyEl) emptyEl.style.display = "block";
  }
}

// ── Sidebar Shows Browser ──
(function() {
  const sidebar = document.getElementById("sidebarBody");
  if (!sidebar) return;

  var sidebarShows = [];
  var activeShowIdx = -1; // which show is expanded (-1 = none)
  var activeSeasonIdx = {}; // show index → season index (-1 = none selected)
  var initialRenderDone = false;
  var continueEpisodeCache = {}; // show index → {ep, reason, seasonIdx}

  // ── Continue / Position helpers ──

  function findContinueEpisode(show, showIdx) {
    // Use cache if already computed this render cycle
    if (continueEpisodeCache[showIdx] !== undefined)
      return continueEpisodeCache[showIdx];

    var m = getPersistedMedia();
    var bestProgress = null; // {ep, viewedAt, seasonIdx, epIdx}
    var bestWatched = null; // {ep, viewedAt, seasonIdx, epIdx}

    for (var si = 0; si < show.seasons.length; si++) {
      var season = show.seasons[si];
      for (var ei = 0; ei < season.episodes.length; ei++) {
        var ep = season.episodes[ei];
        var item = m[ep.ID];
        if (!item || !item.playedFor) continue;

        var totalSec = 0;
        if (
          ep.Metadata &&
          typeof ep.Metadata === "object" &&
          ep.Metadata.duration_min
        ) {
          totalSec = parseFloat(ep.Metadata.duration_min) * 60;
        }

        var isWatched = false;
        if (totalSec > 0 && item.playedFor >= totalSec * 0.9) isWatched = true;
        else if (totalSec === 0 && item.playedFor > 300) isWatched = true;

        if (item.playedFor >= 5 && !isWatched) {
          if (
            !bestProgress ||
            (item.viewedAt &&
              (!bestProgress.viewedAt || item.viewedAt > bestProgress.viewedAt))
          ) {
            bestProgress = {
              ep: ep,
              viewedAt: item.viewedAt || "",
              seasonIdx: si,
              epIdx: ei,
            };
          }
        }

        if (isWatched && item.viewedAt) {
          if (!bestWatched || item.viewedAt > bestWatched.viewedAt) {
            bestWatched = {
              ep: ep,
              viewedAt: item.viewedAt,
              seasonIdx: si,
              epIdx: ei,
            };
          }
        }
      }
    }

    // In-progress episode → continue
    if (bestProgress) {
      var result = {
        ep: bestProgress.ep,
        reason: "continue",
        seasonIdx: bestProgress.seasonIdx,
      };
      continueEpisodeCache[showIdx] = result;
      return result;
    }

    // Last watched → next sequential
    if (bestWatched) {
      var si = bestWatched.seasonIdx;
      var ei = bestWatched.epIdx;
      var season = show.seasons[si];
      if (ei + 1 < season.episodes.length) {
        var result = {
          ep: season.episodes[ei + 1],
          reason: "next",
          seasonIdx: si,
        };
        continueEpisodeCache[showIdx] = result;
        return result;
      } else if (si + 1 < show.seasons.length) {
        var nextSeason = show.seasons[si + 1];
        if (nextSeason.episodes.length > 0) {
          var result = {
            ep: nextSeason.episodes[0],
            reason: "next",
            seasonIdx: si + 1,
          };
          continueEpisodeCache[showIdx] = result;
          return result;
        }
      }
    }

    // Nothing watched → first episode
    if (show.seasons.length > 0 && show.seasons[0].episodes.length > 0) {
      var result = {
        ep: show.seasons[0].episodes[0],
        reason: "start",
        seasonIdx: 0,
      };
      continueEpisodeCache[showIdx] = result;
      return result;
    }

    continueEpisodeCache[showIdx] = null;
    return null;
  }

  function findCurrentShowIdx() {
    var m = getPersistedMedia();
    var bestIdx = -1;
    var bestTime = "";

    for (var si = 0; si < sidebarShows.length; si++) {
      var show = sidebarShows[si];
      for (var ssi = 0; ssi < show.seasons.length; ssi++) {
        var season = show.seasons[ssi];
        for (var ei = 0; ei < season.episodes.length; ei++) {
          var ep = season.episodes[ei];
          var item = m[ep.ID];
          if (item && item.viewedAt && item.viewedAt > bestTime) {
            bestTime = item.viewedAt;
            bestIdx = si;
          }
        }
      }
    }
    return bestIdx;
  }

  function positionLabel(ep) {
    return "S" + ep.season + "\u00B7E" + ep.episode;
  }

  function fetchShows() {
    sidebar.innerHTML = '<div class="sidebar-loading">Loading…</div>';
    fetch("/gallery/shows")
      .then(function(r) {
        if (!r.ok) throw new Error("HTTP " + r.status);
        return r.json();
      })
      .then(function(data) {
        sidebarShows = data.shows || [];
        activeSeasonIdx = {};
        for (var i = 0; i < sidebarShows.length; i++) activeSeasonIdx[i] = -1;
        render();

        // Auto-expand to current show on first load
        if (!initialRenderDone) {
          initialRenderDone = true;
          var curIdx = findCurrentShowIdx();
          if (curIdx >= 0) {
            activeShowIdx = curIdx;
            var cont = findContinueEpisode(sidebarShows[curIdx], curIdx);
            if (cont) activeSeasonIdx[curIdx] = cont.seasonIdx;
            render();
          }
        }
      })
      .catch(function(err) {
        console.error("Sidebar: failed to fetch shows:", err);
        sidebar.innerHTML = '<div class="sidebar-empty">Unavailable</div>';
      });
  }

  function episodeDisplayName(ep) {
    if (ep.Metadata && typeof ep.Metadata === "object" && ep.Metadata.name) {
      var mn = ep.Metadata.name;
      if (!/[Ss]\d{1,2}[Ee]\d{1,3}/.test(mn) && !/\d{1,2}x\d{1,3}/i.test(mn))
        return mn;
    }
    var raw = ep.Name || "";
    raw = raw.replace(/\.[^.]+$/, "");
    raw = raw.replace(/[._-]/g, " ").replace(/\s+/g, " ").trim();
    return raw || ep.Name;
  }

  function episodeWatched(epID, epMeta) {
    var m = getPersistedMedia();
    var item = m[epID];
    if (!item || !item.playedFor || item.playedFor < 5)
      return { status: "none" };
    // Determine total duration in seconds from metadata
    var totalSec = 0;
    if (epMeta && typeof epMeta === "object" && epMeta.duration_min) {
      totalSec = parseFloat(epMeta.duration_min) * 60;
    }
    // Consider watched if ≥90% of duration has been played, or if no duration metadata and played > 5 min
    if (totalSec > 0 && item.playedFor >= totalSec * 0.9)
      return { status: "watched", playedFor: item.playedFor };
    if (totalSec === 0 && item.playedFor > 300)
      return { status: "watched", playedFor: item.playedFor };
    return { status: "progress", playedFor: item.playedFor };
  }

  function selectSeason(si, ssi) {
    if (activeSeasonIdx[si] === ssi) {
      activeSeasonIdx[si] = -1; // deselect
    } else {
      activeSeasonIdx[si] = ssi;
    }
    render();
  }

  function render() {
    // Clear continue cache each render cycle
    continueEpisodeCache = {};

    sidebar.innerHTML = "";
    if (sidebarShows.length === 0) {
      sidebar.innerHTML = '<div class="sidebar-empty">No shows detected</div>';
      return;
    }
    for (var si = 0; si < sidebarShows.length; si++) {
      var show = sidebarShows[si];
      if (activeSeasonIdx[si] === undefined) activeSeasonIdx[si] = -1;
      var isOpen = si === activeShowIdx;
      var hasEpisodes = isOpen && activeSeasonIdx[si] >= 0;

      var div = document.createElement("div");
      div.className = "sidebar-show" + (isOpen ? " open" : "");
      var continueInfo = findContinueEpisode(show, si);

      // Show header
      var hdr = document.createElement("div");
      hdr.className = "sidebar-show-header";

      // Name with optional position badge
      var nameSpan = document.createElement("span");
      nameSpan.textContent = show.name;
      hdr.appendChild(nameSpan);

      // Position indicator + continue button (visible when collapsed too)
      if (continueInfo) {
        var posSpan = document.createElement("span");
        posSpan.className = "sidebar-show-position";
        posSpan.textContent = positionLabel(continueInfo.ep);
        hdr.appendChild(posSpan);

        var contBtn = document.createElement("button");
        contBtn.className = "sidebar-show-continue";
        contBtn.title =
          continueInfo.reason === "continue"
            ? "Continue watching"
            : "Play next";
        contBtn.innerHTML =
          '<svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><polygon points="5 3 19 12 5 21 5 3"></polygon></svg>';
        contBtn.onclick = (function(epID) {
          return function(e) {
            e.stopPropagation();
            selectMedia(epID);
          };
        })(continueInfo.ep.ID);
        hdr.appendChild(contBtn);
      }

      var epCount = 0;
      for (var sc = 0; sc < show.seasons.length; sc++)
        epCount += show.seasons[sc].episodes.length;
      var metaSpan = document.createElement("span");
      metaSpan.style.cssText =
        "font-size:0.7rem;color:var(--text-secondary);margin-left:auto;margin-right:6px";
      metaSpan.textContent = epCount;
      hdr.appendChild(metaSpan);

      var chevron = document.createElement("span");
      chevron.innerHTML =
        '<svg class="sidebar-show-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"></polyline></svg>';
      hdr.appendChild(chevron);
      hdr.onclick = (function(idx) {
        return function() {
          if (activeShowIdx === idx) {
            activeShowIdx = -1;
          } else {
            activeShowIdx = idx;
          }
          render();
        };
      })(si);
      div.appendChild(hdr);

      if (isOpen) {
        var body = document.createElement("div");
        body.className = "sidebar-show-body";

        // Season pills
        var seasonRow = document.createElement("div");
        seasonRow.className = "sidebar-seasons";
        for (var ssi = 0; ssi < show.seasons.length; ssi++) {
          var ssn = show.seasons[ssi];
          var pill = document.createElement("button");
          pill.className = "sidebar-season-pill";
          if (ssi === activeSeasonIdx[si]) pill.classList.add("active");
          pill.textContent =
            "S" + ssn.season + " (" + ssn.episodes.length + ")";
          pill.onclick = (function(sIdx, ssIdx) {
            return function(e) {
              e.stopPropagation();
              selectSeason(sIdx, ssIdx);
            };
          })(si, ssi);
          seasonRow.appendChild(pill);
        }
        body.appendChild(seasonRow);

        // Episodes (only if a season is selected)
        if (hasEpisodes) {
          var epContainer = document.createElement("div");
          epContainer.className = "sidebar-episodes";
          var activeSeas = show.seasons[activeSeasonIdx[si]];
          if (activeSeas) {
            for (var ei = 0; ei < activeSeas.episodes.length; ei++) {
              var ep = activeSeas.episodes[ei];
              var epRow = document.createElement("div");
              epRow.className = "sidebar-ep";
              if (continueInfo && ep.ID === continueInfo.ep.ID)
                epRow.classList.add("next-up");
              if (ep.ID === mostRecentID) epRow.classList.add("playing");

              var num = document.createElement("span");
              num.className = "sidebar-ep-num";
              num.textContent = ep.episode;
              epRow.appendChild(num);

              var name = document.createElement("span");
              name.className = "sidebar-ep-name";
              name.textContent = episodeDisplayName(ep);
              epRow.appendChild(name);

              var ws = episodeWatched(ep.ID, ep.Metadata);
              if (ws.status === "watched") {
                var dot = document.createElement("span");
                dot.className = "sidebar-ep-watched";
                epRow.appendChild(dot);
                epRow.style.opacity = "0.7";
              } else if (ws.status === "progress") {
                var pct = 0;
                if (
                  ep.Metadata &&
                  typeof ep.Metadata === "object" &&
                  ep.Metadata.duration_min
                ) {
                  var totalSec = parseFloat(ep.Metadata.duration_min) * 60;
                  if (totalSec > 0)
                    pct = Math.min(
                      100,
                      Math.round((ws.playedFor / totalSec) * 100),
                    );
                }
                var prog = document.createElement("span");
                prog.className = "sidebar-ep-progress-text";
                prog.textContent = Math.round(ws.playedFor / 60) + "m";
                epRow.appendChild(prog);
                // Thin progress bar
                var bar = document.createElement("span");
                bar.className = "sidebar-ep-progress-bar";
                bar.innerHTML = '<span style="width:' + pct + '%"></span>';
                epRow.appendChild(bar);
              }

              epRow.onclick = (function(epID) {
                return function() {
                  selectMedia(epID);
                };
              })(ep.ID);
              epContainer.appendChild(epRow);
            }
          }
          body.appendChild(epContainer);
        }
        div.appendChild(body);
      }
      sidebar.appendChild(div);
    }

    // Scroll to next-up episode if a show is expanded
    if (activeShowIdx >= 0) {
      var nextUp = sidebar.querySelector(".sidebar-ep.next-up");
      if (nextUp)
        nextUp.scrollIntoView({ block: "nearest", behavior: "smooth" });
    }
  }

  function esc(s) {
    var d = document.createElement("div");
    d.textContent = s;
    return d.innerHTML;
  }

  // Refresh watch dots periodically
  // Refresh watch dots periodically (but don't change expansion state)
  setInterval(function() {
    if (sidebarShows.length > 0) render();
  }, 30000);

  // ── End Sidebar Shows Browser ──

  fetchShows();
})();

// ─────────────────────────────────────────────────────────────────────────
// Custom Video Player
//
// Wraps the <video> with a bespoke control bar so playback and seeking
// behave the same in fullscreen as they do inline. The server handles
// on-the-fly MKV→MP4 transcoding transparently.
// ─────────────────────────────────────────────────────────────────────────
(function() {
  const el = document.getElementById("player");
  const video = document.getElementById("screen");
  if (!el || !video) return;

  const controls = document.getElementById("controls");
  const bigPlay = document.getElementById("bigPlay");
  const playBtn = document.getElementById("playBtn");
  const back10 = document.getElementById("back10");
  const fwd10 = document.getElementById("fwd10");
  const skipIntroBtn = document.getElementById("skipIntro");
  const scrubber = document.getElementById("scrubber");
  const scrubFill = document.getElementById("scrubFill");
  const scrubBuffered = document.getElementById("scrubBuffered");
  const scrubThumb = document.getElementById("scrubThumb");
  const hoverTime = document.getElementById("scrubHoverTime");
  const timeLabel = document.getElementById("timeLabel");
  const timeCur = timeLabel.querySelector("span:first-child");
  const timeTot = timeLabel.querySelector("span:last-child");
  const muteBtn = document.getElementById("muteBtn");
  const volSlider = document.getElementById("volSlider");
  const fsBtn = document.getElementById("fsBtn");
  const titleEl = document.getElementById("playerTitle");
  const hero = document.getElementById("heroSection");
  const subsTrack = document.getElementById("subs");

  const SKIP_INTRO_SEC = 85; // typical TV intro length
  const NUDGE_SEC = 10;

  const state = {
    id: "",
    duration: 0, // best-known total seconds (0 = unknown)
    wasPlaying: true,
    resumeAt: 0, // pending native resume applied on loadedmetadata
    dragging: false,
  };

  function itemDurationSec(id) {
    const it = media[id];
    if (
      it &&
      it.Metadata &&
      typeof it.Metadata === "object" &&
      it.Metadata.duration_min
    ) {
      const s = parseFloat(it.Metadata.duration_min) * 60;
      if (isFinite(s) && s > 0) return s;
    }
    return 0;
  }

  function total() {
    if (state.duration > 0) return state.duration;
    if (isFinite(video.duration) && video.duration > 0) return video.duration;
    return 0;
  }

  function displayTime() {
    return video.currentTime || 0;
  }

  function fmt(sec) {
    if (!isFinite(sec) || sec < 0) sec = 0;
    sec = Math.floor(sec);
    const h = Math.floor(sec / 3600);
    const m = Math.floor((sec % 3600) / 60);
    const s = sec % 60;
    const pad = (n) => (n < 10 ? "0" + n : "" + n);
    return h > 0 ? h + ":" + pad(m) + ":" + pad(s) : m + ":" + pad(s);
  }

  function getResume(id, dur) {
    const it = loadPersistedMediaItem(id);
    const pf = it && it.playedFor;
    if (pf && pf > 10 && (dur <= 0 || pf < dur - 15)) return pf;
    return 0;
  }

  function resetSubtitles() {
    if (subsTrack) {
      subsTrack.src = "";
      subsTrack.removeAttribute("src");
      if (subsTrack.track) subsTrack.track.mode = "disabled";
    }
  }

  function load(id) {
    if (!id) return;
    state.id = id;
    mostRecentID = id;
    const it = media[id] || {};
    state.duration = itemDurationSec(id);
    const resume = getResume(id, state.duration);

    titleEl.textContent = prettyMediaName(it);
    if (hero) hero.classList.add("hidden");
    el.setAttribute("data-state", "active");
    el.classList.add("buffering");
    state.wasPlaying = true;
    state.resumeAt = resume;

    resetSubtitles();
    video.src = "/gallery/video/" + id;
    video.load();
    updateProgress();
  }

  // Seek to an absolute position (seconds from the start of the media).
  function seekTo(target, keepPlaying) {
    const tot = total();
    let t = Math.max(0, target);
    if (tot > 0) t = Math.min(t, tot - 0.5);
    try {
      video.currentTime = t;
    } catch (e) {
      /* not seekable yet */
    }
  }

  function nudge(delta) {
    if (!state.id) return;
    seekTo(displayTime() + delta);
  }

  function togglePlay() {
    if (!state.id) return;
    if (video.paused) video.play().catch(() => { });
    else video.pause();
  }

  function updateProgress() {
    const tot = total();
    const dt = displayTime();
    if (tot > 0) {
      const frac = Math.max(0, Math.min(1, dt / tot));
      scrubFill.style.width = frac * 100 + "%";
      scrubThumb.style.left = frac * 100 + "%";
    } else {
      scrubFill.style.width = "0%";
      scrubThumb.style.left = "0%";
    }
    if (video.buffered && video.buffered.length && tot > 0) {
      const be = video.buffered.end(video.buffered.length - 1);
      scrubBuffered.style.width = Math.min(100, (be / tot) * 100) + "%";
    } else {
      scrubBuffered.style.width = "0%";
    }
    timeCur.textContent = fmt(dt);
    timeTot.textContent = tot > 0 ? fmt(tot) : "--:--";
  }

  // ── UI auto-hide ──
  let hideTimer = null;
  function showUI() {
    el.classList.add("show-ui");
    clearTimeout(hideTimer);
    if (!video.paused) {
      hideTimer = setTimeout(() => {
        if (!video.paused && !menuOpen()) el.classList.remove("show-ui");
      }, 2800);
    }
  }
  function menuOpen() {
    return !!el.querySelector(".popover:not(.hidden)");
  }
  el.addEventListener("pointermove", showUI);
  el.addEventListener("pointerleave", () => {
    if (!video.paused && !menuOpen()) el.classList.remove("show-ui");
  });

  // ── Persist progress ──
  let lastPersist = 0;
  function persist() {
    if (!state.id) return;
    if (video.seeking || video.readyState < 2) return;
    const now = Date.now();
    if (now - lastPersist < 900) return;
    lastPersist = now;
    const item = loadPersistedMediaItem(state.id);
    item.playedFor = video.currentTime;
    item.viewedAt = new Date().toISOString();
    const pm = getPersistedMedia();
    pm[state.id] = item;
    localStorage.setItem("media", JSON.stringify(pm));
  }

  // ── Video events ──
  video.addEventListener("loadedmetadata", () => {
    if (isFinite(video.duration) && video.duration > 0) {
      state.duration = video.duration;
    }
    if (state.resumeAt > 0) {
      try {
        video.currentTime = state.resumeAt;
      } catch (e) { }
      state.resumeAt = 0;
    }
    updateProgress();
  });
  video.addEventListener("canplay", () => {
    el.classList.remove("buffering");
    if (state.wasPlaying) video.play().catch(() => { });
  });
  video.addEventListener("play", () => {
    el.classList.add("playing");
    showUI();
  });
  video.addEventListener("pause", () => {
    el.classList.remove("playing");
    showUI();
  });
  video.addEventListener("waiting", () => el.classList.add("buffering"));
  video.addEventListener("playing", () => {
    el.classList.remove("buffering");
    el.classList.add("playing");
  });
  video.addEventListener("timeupdate", () => {
    if (!state.dragging) updateProgress();
    persist();
  });
  video.addEventListener("progress", updateProgress);
  video.addEventListener("volumechange", () => {
    el.classList.toggle("muted", video.muted || video.volume === 0);
    volSlider.value = video.muted ? 0 : video.volume;
  });
  video.addEventListener("ended", () => {
    el.classList.remove("playing");
    showUI();
  });

  // ── Button wiring ──
  bigPlay.addEventListener("click", togglePlay);
  playBtn.addEventListener("click", togglePlay);
  video.addEventListener("click", togglePlay);
  back10.addEventListener("click", () => nudge(-NUDGE_SEC));
  fwd10.addEventListener("click", () => nudge(NUDGE_SEC));
  skipIntroBtn.addEventListener("click", () => nudge(SKIP_INTRO_SEC));
  muteBtn.addEventListener("click", () => {
    video.muted = !video.muted;
  });
  volSlider.addEventListener("input", () => {
    video.volume = parseFloat(volSlider.value);
    video.muted = video.volume === 0;
  });

  fsBtn.addEventListener("click", () => {
    if (document.fullscreenElement) {
      document.exitFullscreen();
    } else if (el.requestFullscreen) {
      el.requestFullscreen().catch(() => { });
    } else if (video.webkitEnterFullscreen) {
      video.webkitEnterFullscreen(); // iOS Safari
    }
  });

  // ── Scrubber (pointer drag) ──
  function fractionFromEvent(e) {
    const rect = scrubber.getBoundingClientRect();
    return Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
  }
  scrubber.addEventListener("pointermove", (e) => {
    const tot = total();
    if (tot <= 0) return;
    const frac = fractionFromEvent(e);
    hoverTime.style.left = frac * 100 + "%";
    hoverTime.textContent = fmt(frac * tot);
    if (state.dragging) {
      scrubFill.style.width = frac * 100 + "%";
      scrubThumb.style.left = frac * 100 + "%";
      timeCur.textContent = fmt(frac * tot);
    }
  });
  scrubber.addEventListener("pointerdown", (e) => {
    if (total() <= 0) return;
    state.dragging = true;
    scrubber.classList.add("dragging");
    scrubber.setPointerCapture(e.pointerId);
  });
  scrubber.addEventListener("pointerup", (e) => {
    if (!state.dragging) return;
    state.dragging = false;
    scrubber.classList.remove("dragging");
    const tot = total();
    if (tot > 0) seekTo(fractionFromEvent(e) * tot);
  });

  // ── Keyboard shortcuts ──
  document.addEventListener("keydown", (e) => {
    if (!state.id) return;
    const tag = (e.target.tagName || "").toLowerCase();
    if (tag === "input" || tag === "textarea" || e.target.isContentEditable)
      return;
    switch (e.key) {
      case " ":
      case "k":
        e.preventDefault();
        togglePlay();
        showUI();
        break;
      case "ArrowLeft":
        e.preventDefault();
        nudge(-NUDGE_SEC);
        showUI();
        break;
      case "ArrowRight":
        e.preventDefault();
        nudge(NUDGE_SEC);
        showUI();
        break;
      case "f":
        el.requestFullscreen
          ? document.fullscreenElement
            ? document.exitFullscreen()
            : el.requestFullscreen()
          : null;
        break;
      case "m":
        video.muted = !video.muted;
        break;
    }
  });

  document.addEventListener("fullscreenchange", showUI);

  window.Player = { load, seekTo };
})();
