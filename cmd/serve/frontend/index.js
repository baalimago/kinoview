const media = {}

// ── Lag detection ──
;(function() {
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
        document.body.classList.add('low-perf');
        console.info('low-perf mode: avg frame ' + avg.toFixed(1) + 'ms');
      }
    }
  }

  requestAnimationFrame(measureFrame);
})();

// ── Intro animation loader ──
;(function() {
  const MIN_INTRO_MS = 3000;
  const pageStart = performance.now();
  const overlay = document.getElementById('intro-overlay');
  const logo = overlay ? overlay.querySelector('.intro-logo') : null;
  let pending = 3; // shows, usage (gallery), suggestions
  let dismissed = false;

  // Phase 1: logo emerges on black — nearly immediate
  setTimeout(function() {
    if (logo) logo.classList.add('reveal');
    playIntroBassWhoosh();
  }, 50);

  // Phase 2: background fades from black to marine blue behind the logo
  setTimeout(function() {
    if (overlay) overlay.classList.add('bg-reveal');
  }, 400);

  function playIntroBassWhoosh() {
    try {
      var ctx = new (window.AudioContext || window.webkitAudioContext)();
      if (ctx.state === 'suspended') {
        ctx.resume().then(function() { scheduleBassWhoosh(ctx); });
      } else {
        scheduleBassWhoosh(ctx);
      }
    } catch(e) {
      // Silently fail if AudioContext unavailable
    }
  }

  function scheduleBassWhoosh(ctx) {
      var now = ctx.currentTime;

      // ── Randomised meow parameters ──
      var startFreq  = 500 + Math.random() * 500;   // 500–1000 Hz
      var endFreq    = 140 + Math.random() * 300;   // 140–440 Hz
      var dur        = 0.35 + Math.random() * 0.45;  // 0.35–0.80 s
      var peakGain   = 0.14 + Math.random() * 0.12;  // 0.14–0.26
      var vibRate    = 6 + Math.random() * 7;        // 6–13 Hz vibrato
      var vibDepth   = 18 + Math.random() * 22;      // frequency wobble depth (Hz)
      var doDouble   = Math.random() < 0.35;         // ~35% chance of double-meow

      // ── Vibrato LFO ──
      var lfo = ctx.createOscillator();
      lfo.type = 'sine';
      lfo.frequency.value = vibRate;
      var lfoDepth = ctx.createGain();
      lfoDepth.gain.value = vibDepth;
      lfo.connect(lfoDepth);
      lfo.start(now);
      lfo.stop(now + dur + 0.25);

      // ── Main meow oscillator ──
      var osc = ctx.createOscillator();
      osc.type = 'triangle';
      osc.frequency.setValueAtTime(startFreq, now);
      osc.frequency.exponentialRampToValueAtTime(endFreq, now + dur);
      lfoDepth.connect(osc.frequency);  // vibrato

      // Bandpass filter — tracks the voice, slightly ahead
      var bp = ctx.createBiquadFilter();
      bp.type = 'bandpass';
      bp.frequency.setValueAtTime(startFreq * 1.05, now);
      bp.frequency.exponentialRampToValueAtTime(endFreq * 0.95, now + dur);
      bp.Q.value = 2.5 + Math.random() * 3.0;

      // Gain envelope: quick attack → hold → quick release
      var env = ctx.createGain();
      env.gain.setValueAtTime(0.001, now);
      env.gain.linearRampToValueAtTime(peakGain, now + 0.04);
      env.gain.setValueAtTime(peakGain, now + dur * 0.5);
      env.gain.exponentialRampToValueAtTime(0.001, now + dur);

      osc.connect(bp);
      bp.connect(env);
      env.connect(ctx.destination);
      osc.start(now);
      osc.stop(now + dur + 0.1);

      // ── Optional double-meow ──
      if (doDouble) {
        var gap = 0.12 + Math.random() * 0.25;
        var dStart = now + gap;
        var dStartFreq = startFreq * (0.7 + Math.random() * 0.35);
        var dEndFreq   = endFreq * (0.65 + Math.random() * 0.4);
        var dDur       = dur * (0.5 + Math.random() * 0.4);
        var dPeak      = peakGain * (0.45 + Math.random() * 0.35);

        var dLfo = ctx.createOscillator();
        dLfo.type = 'sine';
        dLfo.frequency.value = vibRate * (0.8 + Math.random() * 0.5);
        var dLfoDepth = ctx.createGain();
        dLfoDepth.gain.value = vibDepth * (0.5 + Math.random() * 0.5);
        dLfo.connect(dLfoDepth);
        dLfo.start(dStart);
        dLfo.stop(dStart + dDur + 0.25);

        var dOsc = ctx.createOscillator();
        dOsc.type = 'triangle';
        dOsc.frequency.setValueAtTime(dStartFreq, dStart);
        dOsc.frequency.exponentialRampToValueAtTime(dEndFreq, dStart + dDur);
        dLfoDepth.connect(dOsc.frequency);

        var dBp = ctx.createBiquadFilter();
        dBp.type = 'bandpass';
        dBp.frequency.setValueAtTime(dStartFreq * 1.05, dStart);
        dBp.frequency.exponentialRampToValueAtTime(dEndFreq * 0.95, dStart + dDur);
        dBp.Q.value = 2.0 + Math.random() * 3.5;

        var dEnv = ctx.createGain();
        dEnv.gain.setValueAtTime(0.001, dStart);
        dEnv.gain.linearRampToValueAtTime(dPeak, dStart + 0.03);
        dEnv.gain.setValueAtTime(dPeak, dStart + dDur * 0.4);
        dEnv.gain.exponentialRampToValueAtTime(0.001, dStart + dDur);

        dOsc.connect(dBp);
        dBp.connect(dEnv);
        dEnv.connect(ctx.destination);
        dOsc.start(dStart);
        dOsc.stop(dStart + dDur + 0.1);
      }

      var maxEnd = now + dur + 0.15;
      if (doDouble) {
        var doubleEnd = now + 0.37 + dur * 0.9 + 0.15;
        if (doubleEnd > maxEnd) maxEnd = doubleEnd;
      }
      // Close context after sound finishes
      setTimeout(function() { ctx.close(); }, Math.ceil((maxEnd - now) * 1000) + 100);
  }

  window.__introMarkLoaded = function() {
    pending--;
    if (pending <= 0 && !dismissed) {
      var elapsed = performance.now() - pageStart;
      var remaining = Math.max(0, MIN_INTRO_MS - elapsed);
      setTimeout(dismissIntro, remaining);
    }
  };

  window.__introMarkFailed = function() {
    // Still count as done on failure so we don't hang
    window.__introMarkLoaded();
  };

  function dismissIntro() {
    if (dismissed || !overlay) return;
    dismissed = true;
    overlay.classList.add('dismiss');
    // Remove from DOM after transition
    setTimeout(function() {
      if (overlay.parentNode) overlay.parentNode.removeChild(overlay);
    }, 550);
  }
})();

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
    window.__introMarkLoaded();
  })
  .catch(err => {
    console.error('Error fetching gallery:');
    console.error(err)
    window.__introMarkFailed();
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
      if (!suggestions || suggestions.length === 0) {
        window.__introMarkLoaded();
        return;
      }

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
      window.__introMarkLoaded();
    })
    .catch(err => {
      console.error("Failed to load suggestions:", err);
      window.__introMarkFailed();
    });
}

// ── Sidebar Shows Browser ──
(function () {
  const sidebar = document.getElementById('sidebarBody');
  if (!sidebar) return;

  var sidebarShows = [];
  var activeShowIdx = -1;       // which show is expanded (-1 = none)
  var activeSeasonIdx = {};     // show index → season index (-1 = none selected)
  var initialRenderDone = false;
  var continueEpisodeCache = {}; // show index → {ep, reason, seasonIdx}

  // ── Continue / Position helpers ──

  function findContinueEpisode(show, showIdx) {
    // Use cache if already computed this render cycle
    if (continueEpisodeCache[showIdx] !== undefined) return continueEpisodeCache[showIdx];

    var m = getPersistedMedia();
    var bestProgress = null; // {ep, viewedAt, seasonIdx, epIdx}
    var bestWatched = null;  // {ep, viewedAt, seasonIdx, epIdx}

    for (var si = 0; si < show.seasons.length; si++) {
      var season = show.seasons[si];
      for (var ei = 0; ei < season.episodes.length; ei++) {
        var ep = season.episodes[ei];
        var item = m[ep.ID];
        if (!item || !item.playedFor) continue;

        var totalSec = 0;
        if (ep.Metadata && typeof ep.Metadata === 'object' && ep.Metadata.duration_min) {
          totalSec = parseFloat(ep.Metadata.duration_min) * 60;
        }

        var isWatched = false;
        if (totalSec > 0 && item.playedFor >= totalSec * 0.9) isWatched = true;
        else if (totalSec === 0 && item.playedFor > 300) isWatched = true;

        if (item.playedFor >= 5 && !isWatched) {
          if (!bestProgress || (item.viewedAt && (!bestProgress.viewedAt || item.viewedAt > bestProgress.viewedAt))) {
            bestProgress = {ep: ep, viewedAt: item.viewedAt || '', seasonIdx: si, epIdx: ei};
          }
        }

        if (isWatched && item.viewedAt) {
          if (!bestWatched || item.viewedAt > bestWatched.viewedAt) {
            bestWatched = {ep: ep, viewedAt: item.viewedAt, seasonIdx: si, epIdx: ei};
          }
        }
      }
    }

    // In-progress episode → continue
    if (bestProgress) {
      var result = {ep: bestProgress.ep, reason: 'continue', seasonIdx: bestProgress.seasonIdx};
      continueEpisodeCache[showIdx] = result;
      return result;
    }

    // Last watched → next sequential
    if (bestWatched) {
      var si = bestWatched.seasonIdx;
      var ei = bestWatched.epIdx;
      var season = show.seasons[si];
      if (ei + 1 < season.episodes.length) {
        var result = {ep: season.episodes[ei + 1], reason: 'next', seasonIdx: si};
        continueEpisodeCache[showIdx] = result;
        return result;
      } else if (si + 1 < show.seasons.length) {
        var nextSeason = show.seasons[si + 1];
        if (nextSeason.episodes.length > 0) {
          var result = {ep: nextSeason.episodes[0], reason: 'next', seasonIdx: si + 1};
          continueEpisodeCache[showIdx] = result;
          return result;
        }
      }
    }

    // Nothing watched → first episode
    if (show.seasons.length > 0 && show.seasons[0].episodes.length > 0) {
      var result = {ep: show.seasons[0].episodes[0], reason: 'start', seasonIdx: 0};
      continueEpisodeCache[showIdx] = result;
      return result;
    }

    continueEpisodeCache[showIdx] = null;
    return null;
  }

  function findCurrentShowIdx() {
    var m = getPersistedMedia();
    var bestIdx = -1;
    var bestTime = '';

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
    return 'S' + ep.season + '\u00B7E' + ep.episode;
  }

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
        window.__introMarkLoaded();
      })
      .catch(function (err) {
        console.error('Sidebar: failed to fetch shows:', err);
        sidebar.innerHTML = '<div class="sidebar-empty">Unavailable</div>';
        window.__introMarkFailed();
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
    // Clear continue cache each render cycle
    continueEpisodeCache = {};

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
      var continueInfo = findContinueEpisode(show, si);

      // Show header
      var hdr = document.createElement('div');
      hdr.className = 'sidebar-show-header';

      // Name with optional position badge
      var nameSpan = document.createElement('span');
      nameSpan.textContent = show.name;
      hdr.appendChild(nameSpan);

      // Position indicator + continue button (visible when collapsed too)
      if (continueInfo) {
        var posSpan = document.createElement('span');
        posSpan.className = 'sidebar-show-position';
        posSpan.textContent = positionLabel(continueInfo.ep);
        hdr.appendChild(posSpan);

        var contBtn = document.createElement('button');
        contBtn.className = 'sidebar-show-continue';
        contBtn.title = continueInfo.reason === 'continue' ? 'Continue watching' : 'Play next';
        contBtn.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><polygon points="5 3 19 12 5 21 5 3"></polygon></svg>';
        contBtn.onclick = (function(epID) { return function(e) { e.stopPropagation(); selectMedia(epID); }; })(continueInfo.ep.ID);
        hdr.appendChild(contBtn);
      }

      var epCount = 0;
      for (var sc = 0; sc < show.seasons.length; sc++) epCount += show.seasons[sc].episodes.length;
      var metaSpan = document.createElement('span');
      metaSpan.style.cssText = 'font-size:0.7rem;color:var(--text-secondary);margin-left:auto;margin-right:6px';
      metaSpan.textContent = epCount;
      hdr.appendChild(metaSpan);

      var chevron = document.createElement('span');
      chevron.innerHTML = '<svg class="sidebar-show-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"></polyline></svg>';
      hdr.appendChild(chevron);
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
              if (continueInfo && ep.ID === continueInfo.ep.ID) epRow.classList.add('next-up');
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

    // Scroll to next-up episode if a show is expanded
    if (activeShowIdx >= 0) {
      var nextUp = sidebar.querySelector('.sidebar-ep.next-up');
      if (nextUp) nextUp.scrollIntoView({block: 'nearest', behavior: 'smooth'});
    }
  }

  function esc(s) {
    var d = document.createElement('div');
    d.textContent = s;
    return d.innerHTML;
  }

  // Refresh watch dots periodically
  // Refresh watch dots periodically (but don't change expansion state)
  setInterval(function () {
    if (sidebarShows.length > 0) render();
  }, 30000);

  // ── End Sidebar Shows Browser ──

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
