// ── The troupe stage ──────────────────────────────────────────────────────
// The troupe is the only splash path. This file fetches the newest submitted
// play, mounts it fullscreen, and after the play runs once it shows a BLOCKING
// feedback control (thumbs + note + the keep/change direction pair, the
// audience-note contract in internal/agents/troupe/feedback.go). The gallery
// is unreachable until the audience leaves through a thumb or the skip button;
// a click/key or the skip button before the play ends skips it and records a
// dismissal note, and the skip button over the feedback control leaves without
// rating. An empty stage (404) leaves #troupe hidden — no black wall, no
// fallback.
(function() {
  var el = document.getElementById("troupe");
  var FEEDBACK_URL = "/api/v1/troupe/feedback";
  var NOTE_MAX = 240;
  // The direction caps mirror maxDirectionLen/maxDirectionEntries in
  // internal/agents/troupe/feedback.go: a refused note is never silently
  // dropped, so the client must not send one the server would refuse.
  var DIRECTION_MAX = 120;
  var DIRECTION_ENTRIES = 5;
  var handle = null;
  var playId = null;
  var feedbackPending = false;
  var dismissed = false;
  var listenersOn = false;

  // Fire-and-forget: feedback must never break the splash. Failure is silent
  // — the director reads whatever arrives.
  function postFeedback(type, data) {
    try {
      var xhr = new XMLHttpRequest();
      xhr.open("POST", FEEDBACK_URL, true);
      xhr.setRequestHeader("Content-Type", "application/json");
      xhr.send(JSON.stringify({ playId: playId, type: type, data: data }));
    } catch (e) {}
  }

  // Stop the engine and fade the splash out, then remove it so the gallery
  // underneath is fully interactive. Idempotent.
  function removeSplash() {
    if (!el || dismissed) return;
    dismissed = true;
    if (handle) { handle.destroy(); handle = null; }
    el.classList.add("dismiss");
    setTimeout(function() {
      if (el.parentNode) el.parentNode.removeChild(el);
    }, 600);
  }

  // Outside dismissal — click or any key. Blocked while the feedback control
  // is live: the audience leaves through a thumb, never a stray key.
  function dismiss() {
    if (dismissed || !el) return;
    if (feedbackPending) return;
    postFeedback("dismissal", { atMs: handle ? handle.time() : 0 });
    removeSplash();
  }

  // Only start listening once a play is mounted: before then #troupe is
  // hidden and clicks belong to the gallery, never the splash.
  function attachDismiss() {
    if (listenersOn) return;
    listenersOn = true;
    document.addEventListener("click", dismiss, false);
    document.addEventListener("keydown", dismiss, false);
  }

  // splitDirection turns the comma-separated keep/change field into the
  // server's list: trimmed, blank entries dropped, capped like the contract.
  function splitDirection(value) {
    var out = [];
    var parts = String(value || "").split(",");
    for (var i = 0; i < parts.length && out.length < DIRECTION_ENTRIES; i++) {
      var entry = parts[i].trim().slice(0, DIRECTION_MAX);
      if (entry) out.push(entry);
    }
    return out;
  }

  // skip leaves the splash: over the running play it is a dismissal (the
  // same note a click records), over live feedback it leaves without rating.
  function skip() {
    if (dismissed) return;
    if (feedbackPending) { removeSplash(); return; }
    dismiss();
  }

  function buildSkipButton() {
    var b = document.createElement("button");
    b.type = "button";
    b.className = "troupe-skip";
    b.title = "Skip";
    b.textContent = "Skip \u23ED";
    b.addEventListener("click", function(e) {
      if (e && e.stopPropagation) e.stopPropagation();
      if (e && e.preventDefault) e.preventDefault();
      skip();
    }, false);
    b.addEventListener("keydown", function(e) {
      if (e && e.stopPropagation) e.stopPropagation();
    }, false);
    el.appendChild(b);
  }

  function textInput(className, placeholder, maxLength) {
    var input = document.createElement("input");
    input.type = "text";
    input.className = className;
    input.placeholder = placeholder;
    input.maxLength = String(maxLength);
    return input;
  }

  function buildFeedbackControl() {
    var root = document.createElement("div");
    root.className = "troupe-feedback";
    var note = textInput("troupe-feedback-note", "a note for the director", NOTE_MAX);
    var keep = textInput("troupe-feedback-direction keep", "keep: what worked", DIRECTION_MAX);
    var change = textInput("troupe-feedback-direction change", "change: what didn't", DIRECTION_MAX);
    var up = document.createElement("button");
    up.type = "button";
    up.className = "troupe-feedback-thumb up";
    up.title = "Loved it";
    up.textContent = "\uD83D\uDC4D"; // thumbs up
    var down = document.createElement("button");
    down.type = "button";
    down.className = "troupe-feedback-thumb down";
    down.title = "Not for me";
    down.textContent = "\uD83D\uDC4E"; // thumbs down

    root.appendChild(note);
    root.appendChild(keep);
    root.appendChild(change);
    root.appendChild(up);
    root.appendChild(down);

    // One delegated listener per event type: any click or key inside the
    // control — focusing the note, tapping a thumb, pressing OK on a focused
    // thumb — is the control's business, never the document's dismissal.
    root.addEventListener("click", function(e) {
      if (e && e.stopPropagation) e.stopPropagation();
    }, false);
    root.addEventListener("keydown", function(e) {
      if (e && e.stopPropagation) e.stopPropagation();
    }, false);

    function send(rating) {
      postFeedback("rating", {
        rating: rating,
        comment: note.value || "",
        keep: splitDirection(keep.value),
        change: splitDirection(change.value)
      });
      feedbackPending = false;
      root.classList.add("sent");
      removeSplash();
    }

    up.addEventListener("click", function() { send(1); }, false);
    down.addEventListener("click", function() { send(-1); }, false);

    el.appendChild(root);
    // Cross a frame so the fade-in transition actually runs.
    setTimeout(function() { root.classList.add("show"); }, 0);
  }

  function mountPlay(play) {
    window.TROUPE_PLAY = play;
    playId = play.play && play.play.id ? play.play.id : null;
    if (el && window.TroupeEngine) {
      el.classList.add("live");
      attachDismiss();
      buildSkipButton();
      handle = window.TroupeEngine.mount(el, play, {
        auto: true,
        onEnd: function(durationMs) {
          postFeedback("completion", { durationMs: durationMs || 0 });
          feedbackPending = true;
          buildFeedbackControl();
        }
      });
    }
  }

  fetch("/api/v1/troupe/play/resolved")
    .then(function(res) {
      if (!res.ok) throw new Error("no play: " + res.status);
      return res.json();
    })
    .then(mountPlay)
    .catch(function(err) {
      // No submitted play — the empty stage. #troupe stays hidden.
      console.info("troupe: " + err.message);
    });
})();
