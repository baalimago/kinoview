const media = {}

fetch('/gallery')
  .then(response => response.json())
  .then(data => {
    const options = document.getElementById("debugMediaSelector")
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


function selectMedia(id) {
  const screen = document.getElementById("screen");
  screen.src = `/gallery/${id}`;
}
