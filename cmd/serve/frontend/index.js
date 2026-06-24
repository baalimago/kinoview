const media = {}

const ogConsoleLog = console.log
const ogConsoleError = console.error

console.log = postInfo
console.error = postErr

function postLogMsg(level, data) {
  if (typeof data === 'object') {
    data = JSON.stringify(data, null, 2);
  }

  fetch('/gallery/log', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      "level": level,
      "message": data,
    }),
  })
    .then(response => {
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      return response.text();
    })
    .catch(error => {
      ogConsoleError("Error posting log:", error);
    });
}

function postErr(data) {
  postLogMsg("error", data)
  ogConsoleError(data)
}

function postInfo(data) {
  postLogMsg("info", data)
  ogConsoleLog(data)
}

function getPersistedMedia() {
  try {
    let media = localStorage.getItem("media");
    if (!media) {
      return {};
    }
    return JSON.parse(media);
  } catch (err) {
    console.error("failed to load media from localStorage", err)
  }
  return {}
}

function loadPersistedMediaItem(vID) {
  const media = getPersistedMedia();
  const item = media[vID];
  if (!item) {
    console.error(`media item with id: ${vID} not found in media store`)
    return {}
  }
  return item
}

function videoNameWithProgress(vID, vidName) {
  let name = vidName;
  const storedItem = loadPersistedMediaItem(vID)
  if (!storedItem) {
    return name
  }
  const playTime = storedItem.playedFor;
  if (playTime) {
    const asMin = (playTime / 60).toFixed(3);
    name += ` - ${asMin} min`;
  }
  return name;
}

fetch('/gallery?start=0&am=1000&mime=video')
  .then(response => response.json())
  .then(data => {
    populateMediaDropdown(data.items)
    // Handle deep-link play from shows page
    autoPlayFromQuery();
  })
  .catch(err => {
    console.error('Error fetching gallery:');
    console.error(err)
  });

let searchDebounceTimer = null;
const SEARCH_DEBOUNCE_MS = 250;
const MAX_SEARCH_RESULTS = 5;

// Auto-play an episode when navigated from shows page via ?play=ID
function autoPlayFromQuery() {
  const params = new URLSearchParams(window.location.search);
  const playID = params.get('play');
  if (playID && media[playID]) {
    selectMedia(playID);
    // Clean URL without reload
    const url = new URL(window.location);
    url.searchParams.delete('play');
    window.history.replaceState({}, '', url);
  }
}

function searchMedia() {
  clearTimeout(searchDebounceTimer);
  searchDebounceTimer = setTimeout(() => {
    const query = document.getElementById("searchInput").value.trim();
    let url = '/gallery?start=0&am=1000&mime=video';
    if (query) {
      url += '&search=' + encodeURIComponent(query);
    }
    fetch(url)
      .then(response => response.json())
      .then(data => {
        populateMediaDropdown(data.items);
        populateSearchResults(data.items, query);
      })
      .catch(err => {
        console.error('Error searching media:');
        console.error(err);
      });
  }, SEARCH_DEBOUNCE_MS);
}

function populateSearchResults(items, query) {
  const resultsDiv = document.getElementById("searchResults");
  resultsDiv.innerHTML = '';

  if (!query || items.length === 0) {
    resultsDiv.classList.add('hidden');
    return;
  }

  const topItems = items.slice(0, MAX_SEARCH_RESULTS);
  for (const it of topItems) {
    const row = document.createElement('div');
    row.className = 'search-result-item';

    const nameSpan = document.createElement('span');
    nameSpan.className = 'result-name';
    nameSpan.textContent = videoNameWithProgress(it.ID, it.Name);

    const pathSpan = document.createElement('span');
    pathSpan.className = 'result-path';
    pathSpan.textContent = it.Path || '';

    row.appendChild(nameSpan);
    row.appendChild(pathSpan);

    row.addEventListener('click', () => {
      selectMedia(it.ID);
      document.getElementById("searchInput").value = it.Name;
      document.getElementById("searchResults").classList.add('hidden');
    });

    resultsDiv.appendChild(row);
  }

  if (items.length > MAX_SEARCH_RESULTS) {
    const more = document.createElement('div');
    more.className = 'search-results-empty';
    more.textContent = `... and ${items.length - MAX_SEARCH_RESULTS} more (refine search)`;
    resultsDiv.appendChild(more);
  }

  resultsDiv.classList.remove('hidden');
}

function populateMediaDropdown(items) {
    const options = document.getElementById("debugMediaSelector")
    options.innerHTML = '<option value="">Select video</option>';
    items.sort((a, b) => a.Name.localeCompare(b.Name))
    const persistedMedia = getPersistedMedia()
    for (const i of items) {
      if (!i.MIMEType.includes("video")) {
        continue
      }
      media[i.ID] = i
      const storageItem = loadPersistedMediaItem(i.ID);
      storageItem.name = i.Name
      persistedMedia[i.ID] = storageItem
      const opt = document.createElement("option")

      opt.value = i.ID
      opt.innerText = videoNameWithProgress(i.ID, i.Name)
      options.append(opt)
    }
    localStorage.setItem("media", JSON.stringify(persistedMedia))
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
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function (c) {
    const r = (Math.random() * 16) | 0;
    const v = c === 'x' ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

function selectMedia(id) {
  const video = document.getElementById("screen");
  // Thank the gods for js's excellent singlethreaded scheduler
  mostRecentID = id;
  video.src = `/gallery/video/${id}`;
  video.style.display = "initial"
  loadStreams(id);
  // Hide hero placeholder
  const hero = document.getElementById('heroSection');
  if (hero) hero.classList.add('hidden');
}

function constuctClientContext() {
  const viewingHistory = []
  const persistedMedia = getPersistedMedia()
  Object.values(persistedMedia).forEach(
    i => {
      if (i.viewedAt) {
        const playedForFloat = i.playedFor
        i.playedFor = `${playedForFloat} seconds`
        viewingHistory.push(i)
      }
    }
  )
  return {
    "viewingHistory": viewingHistory,
  }
}

function requestRecommendation() {
  const inp = document.getElementById("recommendInput");
  const status = document.getElementById("recommendationStatus");
  const req = JSON.stringify({ request: inp.value, context: constuctClientContext() });
  console.info("Sending:", req)
  status.innerText = "Requesting... (this may take a moment)";
  fetch("/gallery/recommend", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: req,
  })
    .then(r => {
      if (!r.ok) throw new Error("status " + r.status);
      return r.json();
    })
    .then(item => {
      if (!item || !item.ID) {
        status.innerText = "No recommendation";
        return;
      }
      status.innerText = "Recommended: " + (item.Name || item.ID);
      const sel = document.getElementById("debugMediaSelector");
      sel.value = item.ID;
      selectMedia(item.ID);
    })
    .catch(err => {
      console.error("recommend error:");
      console.error(err)
      status.innerText = "Error - Check kinoview server logs, or console logs";
    });
}

function loadStreams(id) {
  fetch(`/gallery/streams/${id}`)
    .then(response => response.json())
    .then(data => {
      console.log(`Attempting to load streams for: ${id}`)

      const subMenu = document.getElementById("subsMenu");
      const audioMenu = document.getElementById("audioMenu");
      const debugSubs = document.getElementById("debugSubsSelector");

      if (subMenu) subMenu.innerHTML = '';
      if (audioMenu) audioMenu.innerHTML = '';
      if (debugSubs) debugSubs.length = 0;

      // Add "Off" option for subtitles
      if (subMenu) {
        const offBtn = createDropdownItem("Off", () => {
          selectSubtitle('off');
          updateActiveItem(subMenu, offBtn);
        }, true);
        subMenu.appendChild(offBtn);
      }

      if (debugSubs) {
        const optOff = document.createElement("option");
        optOff.value = "";
        optOff.innerText = "Select subtitles";
        debugSubs.append(optOff);
      }

      let hasAudio = false;
      let audioTrackIndex = 0;

      // Check if streams is array, sometimes it might be null if find returned empty
      if (data.streams) {
        for (const i of data.streams) {
          // Audio
          if (i.codec_type === 'audio') {
            hasAudio = true;
            const currentAudioTrackIndex = audioTrackIndex;
            audioTrackIndex++;
            const lang = i.tags && i.tags.language ? i.tags.language : `Track ${i.index}`;
            const title = i.tags && i.tags.title ? `${i.tags.title} (${lang})` : lang;

            const isDefault = i.disposition && i.disposition.default;
            if (audioMenu) {
              const btn = createDropdownItem(title, () => {
                selectAudio(currentAudioTrackIndex);
                updateActiveItem(audioMenu, btn);
              }, isDefault);
              audioMenu.appendChild(btn);
            }
          }

          // Subtitles
          if (i.codec_type === 'subtitle') {
            // Relaxed check: include even if no language tag
            const lang = i.tags && i.tags.language ? i.tags.language : `Track ${i.index}`;
            const title = i.tags && i.tags.title ? `${i.tags.title} (${lang})` : lang;

            if (subMenu) {
              const btn = createDropdownItem(title, () => {
                selectSubtitle(i.index);
                updateActiveItem(subMenu, btn);
              });
              subMenu.appendChild(btn);
            }

            if (debugSubs) {
              const opt = document.createElement("option");
              opt.value = i.index;
              opt.innerText = title;
              debugSubs.append(opt);
            }
          }
        }
      }

      if (!hasAudio && audioMenu) {
        const btn = createDropdownItem("Default Audio", () => { }, true);
        audioMenu.appendChild(btn);
      }
    })
}

function toggleMenu(menuId) {
  const menu = document.getElementById(menuId);
  if (!menu) return;

  document.querySelectorAll('.dropdown-menu').forEach(m => {
    if (m.id !== menuId) m.classList.add('hidden');
  });

  menu.classList.toggle('hidden');
}

// Close menus when clicking outside
document.addEventListener('click', (e) => {
  if (!e.target.closest('.dropdown-group')) {
    document.querySelectorAll('.dropdown-menu').forEach(m => m.classList.add('hidden'));
  }
  if (!e.target.closest('.search-wrapper')) {
    document.getElementById('searchResults').classList.add('hidden');
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
  container.querySelectorAll('.dropdown-item').forEach(item => item.classList.remove('active'));
  activeItem.classList.add('active');
  container.classList.add('hidden');
}

function selectAudio(index) {
  const video = document.getElementById("screen");
  if (video.audioTracks) {
    for (let i = 0; i < video.audioTracks.length; i++) {
      video.audioTracks[i].enabled = (i === index);
    }
  }
  console.log(`Selected audio stream: ${index}`);
}

function selectSubtitle(id) {
  const track = document.getElementById("subs");
  const debugSubs = document.getElementById("debugSubsSelector");

  if (id === 'off' || id === "") {
    console.log("Disabling subtitles");
    track.src = "";
    track.removeAttribute("src");
    if (debugSubs) debugSubs.value = "";
  } else {
    console.log(`Attempting to set subs to: /gallery/streams/${mostRecentID}/stream/${id}`)
    track.src = `/gallery/streams/${mostRecentID}/stream/${id}`;
    // Sync debug selector keying off numeric stream index usually
    if (debugSubs) debugSubs.value = id;
  }
}

// Integrate events.js
(function () {
  const script = document.createElement("script");
  script.src = "events.js";
  script.async = true;
  document.head.appendChild(script);

  loadSuggestions();
})();

function loadSuggestions() {
  fetch("/gallery/suggestions")
    .then(response => {
      if (!response.ok) throw new Error("status " + response.status);
      return response.json();
    })
    .then(suggestions => {
      if (!suggestions || suggestions.length === 0) return;

      const container = document.getElementById("butler-suggestions");
      const list = document.getElementById("suggestions-list");
      container.style.display = "block";
      list.innerHTML = ""; // clear

      suggestions.forEach(rec => {
        // rec includes Item fields (Name, MIMEType, etc) + Motivation + SubtitleID
        const itemDiv = document.createElement("div");
        itemDiv.className = "suggestion-item";

        itemDiv.onclick = () => {
          selectMedia(rec.ID);
          if (rec.subtitleID) {
            // Wait small delay for subs to load/options to populate if needed
            setTimeout(() => {
              const subSel = document.getElementById("debugSubsSelector");
              subSel.value = rec.subtitleID;
              selectSubtitle(rec.subtitleID);
            }, 500);
          }
        };

        const title = document.createElement("strong");
        title.innerText = rec.Name;

        const motivation = document.createElement("p");
        motivation.innerText = rec.motivation;

        itemDiv.appendChild(title);
        itemDiv.appendChild(motivation);

        list.appendChild(itemDiv);
      });
    })
    .catch(err => {
      console.error("Failed to load suggestions:", err);
    });
}

// ── Sidebar Shows Browser ──
(function () {
  const sidebar = document.getElementById('sidebarBody');
  if (!sidebar) return;

  var sidebarShows = [];
  var activeShowIdx = -1;       // which show is expanded (-1 = none)
  var activeSeasonIdx = {};     // show index → season index (-1 = none selected)

  function fetchShows() {
    sidebar.innerHTML = '<div class="sidebar-loading">Loading…</div>';
    fetch('/gallery/shows')
      .then(function (r) {
        if (!r.ok) throw new Error('HTTP ' + r.status);
        return r.json();
      })
      .then(function (data) {
        sidebarShows = data.shows || [];
        activeSeasonIdx = {};
        for (var i = 0; i < sidebarShows.length; i++) activeSeasonIdx[i] = -1;
        render();
      })
      .catch(function (err) {
        console.error('Sidebar: failed to fetch shows:', err);
        sidebar.innerHTML = '<div class="sidebar-empty">Unavailable</div>';
      });
  }

  function episodeDisplayName(ep) {
    if (ep.Metadata && typeof ep.Metadata === 'object' && ep.Metadata.name) {
      var mn = ep.Metadata.name;
      if (!/[Ss]\d{1,2}[Ee]\d{1,3}/.test(mn) && !/\d{1,2}x\d{1,3}/i.test(mn)) return mn;
    }
    var raw = ep.Name || '';
    raw = raw.replace(/\.[^.]+$/, '');
    raw = raw.replace(/[._-]/g, ' ').replace(/\s+/g, ' ').trim();
    return raw || ep.Name;
  }

  function episodeWatched(epID, epMeta) {
    var m = getPersistedMedia();
    var item = m[epID];
    if (!item || !item.playedFor || item.playedFor < 5) return { status: 'none' };
    // Determine total duration in seconds from metadata
    var totalSec = 0;
    if (epMeta && typeof epMeta === 'object' && epMeta.duration_min) {
      totalSec = parseFloat(epMeta.duration_min) * 60;
    }
    // Consider watched if ≥90% of duration has been played, or if no duration metadata and played > 5 min
    if (totalSec > 0 && item.playedFor >= totalSec * 0.9) return { status: 'watched', playedFor: item.playedFor };
    if (totalSec === 0 && item.playedFor > 300) return { status: 'watched', playedFor: item.playedFor };
    return { status: 'progress', playedFor: item.playedFor };
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
    sidebar.innerHTML = '';
    if (sidebarShows.length === 0) {
      sidebar.innerHTML = '<div class="sidebar-empty">No shows detected</div>';
      return;
    }
    for (var si = 0; si < sidebarShows.length; si++) {
      var show = sidebarShows[si];
      if (activeSeasonIdx[si] === undefined) activeSeasonIdx[si] = -1;
      var isOpen = (si === activeShowIdx);
      var hasEpisodes = isOpen && activeSeasonIdx[si] >= 0;

      var div = document.createElement('div');
      div.className = 'sidebar-show' + (isOpen ? ' open' : '');

      // Show header
      var hdr = document.createElement('div');
      hdr.className = 'sidebar-show-header';
      var epCount = 0;
      for (var sc = 0; sc < show.seasons.length; sc++) epCount += show.seasons[sc].episodes.length;
      hdr.innerHTML = '<span>' + esc(show.name) + '</span>' +
        '<span style="font-size:0.7rem;color:var(--text-secondary);margin-left:auto;margin-right:6px">' + epCount + '</span>' +
        '<svg class="sidebar-show-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"></polyline></svg>';
      hdr.onclick = function (idx) {
        return function () {
          if (activeShowIdx === idx) { activeShowIdx = -1; }
          else { activeShowIdx = idx; }
          render();
        };
      }(si);
      div.appendChild(hdr);

      if (isOpen) {
        var body = document.createElement('div');
        body.className = 'sidebar-show-body';

        // Season pills
        var seasonRow = document.createElement('div');
        seasonRow.className = 'sidebar-seasons';
        for (var ssi = 0; ssi < show.seasons.length; ssi++) {
          var ssn = show.seasons[ssi];
          var pill = document.createElement('button');
          pill.className = 'sidebar-season-pill';
          if (ssi === activeSeasonIdx[si]) pill.classList.add('active');
          pill.textContent = 'S' + ssn.season + ' (' + ssn.episodes.length + ')';
          pill.onclick = (function (sIdx, ssIdx) {
            return function (e) { e.stopPropagation(); selectSeason(sIdx, ssIdx); };
          })(si, ssi);
          seasonRow.appendChild(pill);
        }
        body.appendChild(seasonRow);

        // Episodes (only if a season is selected)
        if (hasEpisodes) {
          var epContainer = document.createElement('div');
          epContainer.className = 'sidebar-episodes';
          var activeSeas = show.seasons[activeSeasonIdx[si]];
          if (activeSeas) {
            for (var ei = 0; ei < activeSeas.episodes.length; ei++) {
              var ep = activeSeas.episodes[ei];
              var epRow = document.createElement('div');
              epRow.className = 'sidebar-ep';
              if (ep.ID === mostRecentID) epRow.classList.add('playing');

              var num = document.createElement('span');
              num.className = 'sidebar-ep-num';
              num.textContent = ep.episode;
              epRow.appendChild(num);

              var name = document.createElement('span');
              name.className = 'sidebar-ep-name';
              name.textContent = episodeDisplayName(ep);
              epRow.appendChild(name);

              var ws = episodeWatched(ep.ID, ep.Metadata);
              if (ws.status === 'watched') {
                var dot = document.createElement('span');
                dot.className = 'sidebar-ep-watched';
                epRow.appendChild(dot);
                epRow.style.opacity = '0.7';
              } else if (ws.status === 'progress') {
                var pct = 0;
                if (ep.Metadata && typeof ep.Metadata === 'object' && ep.Metadata.duration_min) {
                  var totalSec = parseFloat(ep.Metadata.duration_min) * 60;
                  if (totalSec > 0) pct = Math.min(100, Math.round((ws.playedFor / totalSec) * 100));
                }
                var prog = document.createElement('span');
                prog.className = 'sidebar-ep-progress-text';
                prog.textContent = Math.round(ws.playedFor / 60) + 'm';
                epRow.appendChild(prog);
                // Thin progress bar
                var bar = document.createElement('span');
                bar.className = 'sidebar-ep-progress-bar';
                bar.innerHTML = '<span style="width:' + pct + '%"></span>';
                epRow.appendChild(bar);
              }

              epRow.onclick = (function (epID) {
                return function () { selectMedia(epID); };
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
  }

  function esc(s) {
    var d = document.createElement('div');
    d.textContent = s;
    return d.innerHTML;
  }

  // Refresh watch dots periodically
  setInterval(function () {
    if (sidebarShows.length > 0) render();
  }, 30000);

  fetchShows();
})();

// Defer video sync setup safely after sidebar is alive
if (typeof setTimeout === 'function') {
  setTimeout(function () {
    var screen = document.getElementById("screen");
    if (!screen) return;
    screen.addEventListener("timeupdate", function () {
      var item = loadPersistedMediaItem(mostRecentID);
      item.playedFor = this.currentTime;
      item.viewedAt = new Date().toISOString();
      var persistedMedia = getPersistedMedia();
      persistedMedia[mostRecentID] = item;
      localStorage.setItem("media", JSON.stringify(persistedMedia));
    });
    screen.addEventListener("loadeddata", function () {
      var item = loadPersistedMediaItem(mostRecentID);
      var playedForSec = item.playedFor;
      if (playedForSec) {
        ogConsoleLog("Setting played for to: " + playedForSec);
        screen.currentTime = playedForSec;
      }
    });
  }, 10);
}
