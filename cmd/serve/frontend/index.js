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

fetch('/gallery')
  .then(response => response.json())
  .then(data => {
    const options = document.getElementById("debugMediaSelector")
    data.sort((a, b) => a.Name.localeCompare(b.Name))
    const persistedMedia = getPersistedMedia()
    for (const i of data) {
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
        console.log(i)
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
      console.log(`Attempting to load subs for: ${id}`)
      const options = document.getElementById("debugSubsSelector")
      options.length = 0;
      for (const i of data.streams) {
        if (!i.tags.language) {
          continue
        }
        const opt = document.createElement("option")
        opt.value = i.index
        opt.innerText = i.tags.language
        options.append(opt)
      }
    })

}

function selectSubtitle(id) {
  const track = document.getElementById("subs");
  console.log(`Attempting to set subs to: /gallery/subs/${mostRecentID}/${id}`)
  track.src = `/gallery/subs/${mostRecentID}/${id}`;
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

