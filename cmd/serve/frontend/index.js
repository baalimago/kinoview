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
var sessionID = "";
var sessionStartTime = null;
const subtitleState = {
  byItemID: {},
  loading: false,
  selectedByItemID: {},
};

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
  loadSubtitleResources(id);
  setSubtitleStatus(`Loading subtitles for ${media[id]?.Name || id}...`);
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

function subtitleLabel(resource) {
	if (resource.label) {
		return resource.label;
	}
	const parts = [];
	if (resource.language) {
		parts.push(resource.language);
	}
	if (resource.source) {
		parts.push(resource.source);
	}
	if (resource.id) {
		parts.push(resource.id);
	}
	return parts.join(" - ") || "Subtitle";
}

function subtitleResourceURL(subtitleID) {
	return `/gallery/subtitles/resource/${subtitleID}`;
}

function setSubtitleStatus(message, kind = "") {
	const status = document.getElementById("subtitleStatus");
	if (!status) {
		return;
	}
	status.innerText = message || "";
	status.classList.remove("success", "error");
	if (kind) {
		status.classList.add(kind);
	}
}

function setSubtitleLoading(isLoading) {
	subtitleState.loading = isLoading;
	const importBtn = document.getElementById("subtitleImportBtn");
	const defaultBtn = document.getElementById("subtitleDefaultBtn");
	const selector = document.getElementById("debugSubsSelector");
	if (importBtn) {
		importBtn.disabled = isLoading || !mostRecentID;
	}
	if (defaultBtn) {
		defaultBtn.disabled = isLoading || !mostRecentID;
	}
	if (selector) {
		selector.disabled = isLoading;
	}
}

function currentSubtitleResources(itemID) {
	return subtitleState.byItemID[itemID] || [];
}

function findSubtitleResource(itemID, subtitleID) {
	return currentSubtitleResources(itemID).find(resource => resource.id === subtitleID);
}

function resetSubtitleSelectorsForResources() {
	const subMenu = document.getElementById("subsMenu");
	const debugSubs = document.getElementById("debugSubsSelector");

	if (subMenu) {
		subMenu.innerHTML = "";
		const offBtn = createDropdownItem("Off", () => {
			selectSubtitle("off");
			updateActiveItem(subMenu, offBtn);
		}, true);
		subMenu.appendChild(offBtn);
	}

	if (debugSubs) {
		debugSubs.length = 0;
		const optOff = document.createElement("option");
		optOff.value = "";
		optOff.innerText = "Select subtitles";
		debugSubs.append(optOff);
	}
}

function appendSubtitleOption(label, value, onSelect, isActive = false) {
	const subMenu = document.getElementById("subsMenu");
	const debugSubs = document.getElementById("debugSubsSelector");

	if (subMenu) {
		const btn = createDropdownItem(label, () => {
			onSelect();
			updateActiveItem(subMenu, btn);
		}, isActive);
		subMenu.appendChild(btn);
	}

	if (debugSubs) {
		const opt = document.createElement("option");
		opt.value = value;
		opt.innerText = label;
		debugSubs.append(opt);
	}
}

function populateSubtitleResources(itemID, resources, defaultSubtitleID) {
	resetSubtitleSelectorsForResources();

	resources.forEach(resource => {
		const subtitleID = resource.id;
		const label = subtitleLabel(resource);
		appendSubtitleOption(label, subtitleID, () => {
			selectSubtitle(subtitleID);
		}, subtitleID === defaultSubtitleID);
	});

	if (defaultSubtitleID) {
		subtitleState.selectedByItemID[itemID] = defaultSubtitleID;
		selectSubtitle(defaultSubtitleID);
		const debugSubs = document.getElementById("debugSubsSelector");
		if (debugSubs) {
			debugSubs.value = defaultSubtitleID;
		}
		setSubtitleStatus("Loaded default subtitle.", "success");
		return;
	}

	if (resources.length > 0) {
		setSubtitleStatus("Loaded available subtitle resources.", "success");
	} else {
		setSubtitleStatus("No subtitles available for this item yet.");
	}
}

function loadSubtitleResources(itemID) {
	if (!itemID) {
		resetSubtitleSelectorsForResources();
		setSubtitleStatus("");
		return;
	}

	setSubtitleLoading(true);
	Promise.all([
		fetch(`/gallery/subtitles/item/${itemID}`).then(resp => {
			if (!resp.ok) throw new Error(`subtitle list status ${resp.status}`);
			return resp.json();
		}),
		fetch(`/gallery/subtitles/item/${itemID}/default`).then(resp => {
			if (resp.status === 404) return null;
			if (!resp.ok) throw new Error(`subtitle default status ${resp.status}`);
			return resp.json();
		}),
	])
		.then(([resources, defaultPayload]) => {
			subtitleState.byItemID[itemID] = resources || [];
			const defaultSubtitleID = defaultPayload && defaultPayload.binding ? defaultPayload.binding.defaultSubtitleID : "";
			if ((resources || []).length > 0) {
				populateSubtitleResources(itemID, resources, defaultSubtitleID);
				return;
			}

			setSubtitleStatus("No subtitle resources found, importing embedded subtitles...");
			return importEmbeddedSubtitle(itemID)
				.then(result => {
					subtitleState.byItemID[itemID] = [result.resource];
					populateSubtitleResources(itemID, [result.resource], result.resource.id);
					setSubtitleStatus(result.alreadyExists ? "Reused embedded subtitle resource." : "Imported embedded subtitles.", "success");
				})
				.catch(err => {
					console.error(`Failed to import embedded subtitles for ${itemID}: ${err}`);
					resetSubtitleSelectorsForResources();
					setSubtitleStatus("Failed to import embedded subtitles.", "error");
				});
		})
		.catch(err => {
			console.error(`Failed to load subtitle resources for ${itemID}: ${err}`);
			resetSubtitleSelectorsForResources();
			setSubtitleStatus("Failed to load subtitles.", "error");
		})
		.finally(() => {
			setSubtitleLoading(false);
		});
}

function importEmbeddedSubtitle(itemID) {
	return fetch(`/gallery/subtitles/item/${itemID}/import`, {
		method: "POST",
	}).then(resp => {
		if (!resp.ok) {
			throw new Error(`subtitle import status ${resp.status}`);
		}
		return resp.json();
	});
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
    subtitleState.selectedByItemID[mostRecentID] = "";
    if (debugSubs) debugSubs.value = "";
    setSubtitleStatus("Subtitles disabled.");
  } else {
    const subtitleResource = findSubtitleResource(mostRecentID, id);
    if (subtitleResource) {
      console.log(`Attempting to set subtitle resource: ${subtitleResourceURL(id)}`);
      track.src = subtitleResourceURL(id);
      subtitleState.selectedByItemID[mostRecentID] = id;
      setSubtitleStatus(`Selected subtitle: ${subtitleLabel(subtitleResource)}`, "success");
    } else {
      console.log(`Attempting to set legacy subtitle stream: /gallery/streams/${mostRecentID}/stream/${id}`);
      track.src = `/gallery/streams/${mostRecentID}/stream/${id}`;
      subtitleState.selectedByItemID[mostRecentID] = id;
      setSubtitleStatus("Selected legacy stream subtitle.");
    }
    if (debugSubs) debugSubs.value = id;
  }
}

function saveDefaultSubtitle() {
	if (!mostRecentID) {
		setSubtitleStatus("Select a video before saving a default subtitle.", "error");
		return;
	}
	const subtitleID = subtitleState.selectedByItemID[mostRecentID];
	if (!subtitleID) {
		setSubtitleStatus("Select a subtitle resource before saving default.", "error");
		return;
	}

	setSubtitleLoading(true);
	setSubtitleStatus("Saving default subtitle...");
	fetch(`/gallery/subtitles/item/${mostRecentID}/default`, {
		method: "POST",
		headers: {
			"Content-Type": "application/json",
		},
		body: JSON.stringify({ subtitleID }),
	})
		.then(resp => {
			if (!resp.ok) {
				throw new Error(`save default subtitle status ${resp.status}`);
			}
			return resp.json();
		})
		.then(() => {
			setSubtitleStatus("Saved default subtitle.", "success");
		})
		.catch(err => {
			console.error(`Failed to save default subtitle for ${mostRecentID}: ${err}`);
			setSubtitleStatus("Failed to save default subtitle.", "error");
		})
		.finally(() => {
			setSubtitleLoading(false);
		});
}

function manualImportSubtitles() {
	if (!mostRecentID) {
		setSubtitleStatus("Select a video before importing subtitles.", "error");
		return;
	}

	setSubtitleLoading(true);
	setSubtitleStatus("Importing embedded subtitles...");
	importEmbeddedSubtitle(mostRecentID)
		.then(result => {
			const current = currentSubtitleResources(mostRecentID);
			const withoutExisting = current.filter(resource => resource.id !== result.resource.id);
			subtitleState.byItemID[mostRecentID] = [...withoutExisting, result.resource];
			populateSubtitleResources(mostRecentID, subtitleState.byItemID[mostRecentID], result.resource.id);
			setSubtitleStatus(result.alreadyExists ? "Subtitle resource already existed and was reused." : "Embedded subtitles imported successfully.", "success");
		})
		.catch(err => {
			console.error(`Manual subtitle import failed for ${mostRecentID}: ${err}`);
			setSubtitleStatus("Failed to import embedded subtitles.", "error");
		})
		.finally(() => {
			setSubtitleLoading(false);
		});
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
            setTimeout(() => {
              const subSel = document.getElementById("debugSubsSelector");
              if (subSel) {
                subSel.value = rec.subtitleID;
              }
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
