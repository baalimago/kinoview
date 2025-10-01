const media = {}

const ogConsoleLog = console.log
const ogConsoleError = console.error

console.log = postInfo
console.error = postErr

function postLogMsg(level, data) {
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
      return response.json();
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

function videoNameWithProgress(vID, vidName) {
  let name = vidName;
  const playTime = localStorage.getItem(
    "video_play_duration_" + vID
  );
  if (playTime) {
    const asSec = playTime.split(".")[0];
    const asMin = (asSec / 60).toFixed(3);
    name += ` - ${asMin} min`;
  }
  return name;
}

fetch('/gallery')
  .then(response => response.json())
  .then(data => {
    const options = document.getElementById("debugMediaSelector")
    data.sort((a, b) => a.Name.localeCompare(b.Name))
    for (const i of data) {
      if (!i.MIMEType.includes("video")) {
        continue
      }
      media[i.ID] = i
      const opt = document.createElement("option")

      opt.value = i.ID
      opt.innerText = videoNameWithProgress(i.ID, i.Name)
      options.append(opt)
    }
  })
  .catch(error => {
    console.error('Error fetching gallery:', error);
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

function requestRecommendation() {
  const inp = document.getElementById("recommendInput");
  const status = document.getElementById("recommendationStatus");
  const req = { Request: inp.value, Context: JSON.stringify(localStorage) };
  console.info("Sending:", req)
  status.innerText = "Requesting... (this may take a moment)";
  fetch("/gallery/recommend", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
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
      console.error("recommend error:", err);
      status.innerText = "Error";
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
    localStorage.setItem(
      mostRecentID + "_has_been_played_for_s",
      this.currentTime
    );
    localStorage.setItem(
      mostRecentID + "_was_played_last_at",
      new Date().toISOString()
    );
    localStorage.setItem(
      "last_played_ID",
      mostRecentID
    );
    console.log(`Updating time for: ${mostRecentID}, to time: ${this.currentTime}s`)
  });


  screen.addEventListener("loadeddata", function () {
    const playTime = localStorage.getItem(
      "video_play_duration_" + mostRecentID
    );
    if (playTime) {
      screen.currentTime = playTime
    }

  });
}, 10)

