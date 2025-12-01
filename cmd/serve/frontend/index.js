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
    const options = document.getElementById("debugMediaSelector")
    items = data.items
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
  })
  .catch(err => {
    console.error('Error fetching gallery:');
    console.error(err)
  });


var mostRecentID = "";
function selectMedia(id) {
  const video = document.getElementById("screen");
  // Thank the gods for js's excellent singlethreaded scheduler
  mostRecentID = id;
  video.src = `/gallery/video/${id}`;
  video.style.display = "initial"
  loadSubtitles(id);
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
    "timeOfDay": new Date().toISOString(),
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

function loadSubtitles(id) {
  fetch(`/gallery/subs/${id}`)
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

      // Check if streams is array, sometimes it might be null if find returned empty
      if (data.streams) {
          for (const i of data.streams) {
            // Audio
            if (i.codec_type === 'audio') {
                hasAudio = true;
                const lang = i.tags && i.tags.language ? i.tags.language : `Track ${i.index}`;
                const title = i.tags && i.tags.title ? `${i.tags.title} (${lang})` : lang;
                
                const isDefault = i.disposition && i.disposition.default;
                if (audioMenu) {
                    const btn = createDropdownItem(title, () => {
                        selectAudio(id, i.index);
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
           const btn = createDropdownItem("Default Audio", () => {}, true);
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

function selectAudio(vidId, streamIndex) {
    console.log(`Selected audio stream: ${streamIndex}`);
    // Audio switching requires backend support (re-transcoding or HLS). 
    // This is the frontend hook for it.
    // For now, we communicate via log.
    console.info(`Audio switching to stream ${streamIndex} requested.`);
}

function selectSubtitle(id) {
  const track = document.getElementById("subs");
  const debugSubs = document.getElementById("debugSubsSelector");
  
  if (id === 'off' || id === "") {
      console.log("Disabling subtitles");
      track.src = "";
      track.removeAttribute("src");
      if(debugSubs) debugSubs.value = "";
  } else {
      console.log(`Attempting to set subs to: /gallery/subs/${mostRecentID}/${id}`)
      track.src = `/gallery/subs/${mostRecentID}/${id}`;
      // Sync debug selector keying off numeric stream index usually
      if(debugSubs) debugSubs.value = id;
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

setTimeout(() => {
  const screen = document.getElementById("screen")
  screen.addEventListener("timeupdate", function () {
    const item = loadPersistedMediaItem(mostRecentID);
    item.playedFor = this.currentTime
    item.viewedAt = new Date().toISOString()

    const persistedMedia = getPersistedMedia()
    persistedMedia[mostRecentID] = item
    localStorage.setItem("media", JSON.stringify(persistedMedia));
  });


  screen.addEventListener("loadeddata", function () {
    const item = loadPersistedMediaItem(mostRecentID)
    const playedForSec = item.playedFor
    if (playedForSec) {
      console.log(`Setting played for to: ${playedForSec}`)
      screen.currentTime = playedForSec
    }
  });
}, 10)
