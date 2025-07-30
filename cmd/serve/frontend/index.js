const media = {}

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
      opt.innerText = i.Name
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

function loadSubtitles(id) {
  fetch(`/gallery/subs/${id}`)
    .then(response => response.json())
    .then(data => {
      const options = document.getElementById("debugSubsSelector")
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
  track.src = `/gallery/subs/${mostRecentID}/${id}`;
}

setTimeout(() => {
  const screen = document.getElementById("screen")
  screen.addEventListener("timeupdate", function () {
    localStorage.setItem(
      "video_play_duration_" + mostRecentID,
      this.currentTime
    );
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

