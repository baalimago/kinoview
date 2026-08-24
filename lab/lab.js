// ── The Troupe lab driver ─────────────────────────────────────────────────
// Dev-only simulator: loads a fixture resolved play from ./fixtures and
// drives TroupeEngine.mount with an explicit clock (auto:false), so the play
// can be played, paused and scrubbed by hand. No framework, ES5.
(function() {
  'use strict';

  var engine = window.TroupeEngine;
  var stage = document.getElementById('troupe');
  var fixtureSel = document.getElementById('fixture');
  var playBtn = document.getElementById('play');
  var scrub = document.getElementById('scrub');
  var timeLabel = document.getElementById('time');
  var status = document.getElementById('status');

  var FIXTURES = ['story_20260820T161500Z.resolved.json', 'garden_20260821T090000Z.resolved.json'];

  var handle = null;
  var audio = null;
  var state = { t: 0 };
  var playing = false;
  var rafId = null;
  var lastTs = 0;

  function setStatus(msg) { status.textContent = msg || ''; }

  function teardown() {
    if (rafId !== null) { cancelAnimationFrame(rafId); rafId = null; }
    playing = false;
    if (handle) { handle.destroy(); handle = null; }
    if (audio) {
      try { audio.close(); } catch (e) {}
      audio = null;
    }
  }

  function format(ms) { return Math.round(ms) + ' ms'; }

  function refreshScrub() {
    scrub.max = String(handle ? handle.duration : 0);
    scrub.value = String(Math.min(state.t, handle ? handle.duration : 0));
    scrub.disabled = !handle;
    timeLabel.textContent = format(state.t) + ' / ' + format(handle ? handle.duration : 0);
  }

  // The engine's step is monotonic (backward scrubs need a remount), so a
  // seek below the current time rebuilds the play first. Audio reschedules
  // from the new position; the old context is closed.
  function seek(t) {
    if (!handle) return;
    if (t < state.t) {
      var play = currentPlay;
      teardown();
      start(play, t);
      return;
    }
    state.t = t;
    handle.step(t);
    refreshScrub();
  }

  var currentPlay = null;

  function start(play, at) {
    teardown();
    currentPlay = play;
    state = { t: at || 0 };
    var Ctor = window.AudioContext || window.webkitAudioContext;
    audio = Ctor ? new Ctor() : null;
    handle = engine.mount(stage, play, {
      size: { w: stage.clientWidth || 640, h: stage.clientHeight || 360 },
      auto: false,
      clock: function() { return state.t; },
      audio: audio
    });
    playBtn.disabled = false;
    playBtn.textContent = 'Play';
    refreshScrub();
  }

  function loadFixture(name) {
    var xhr = new XMLHttpRequest();
    xhr.open('GET', 'fixtures/' + name, true);
    xhr.onload = function() {
      if (xhr.status !== 200) { setStatus('failed to load ' + name); return; }
      try {
        start(JSON.parse(xhr.responseText));
        setStatus('');
      } catch (e) {
        setStatus('parse error: ' + e.message);
      }
    };
    xhr.onerror = function() { setStatus('network error'); };
    xhr.send();
  }

  function frame(ts) {
    rafId = requestAnimationFrame(frame);
    if (!playing || !handle) return;
    if (lastTs === 0) lastTs = ts;
    var dt = ts - lastTs;
    lastTs = ts;
    var t = state.t + dt;
    if (t >= handle.duration) {
      state.t = handle.duration;
      handle.step(state.t);
      playing = false;
      playBtn.textContent = 'Play';
      lastTs = 0;
    } else {
      state.t = t;
      handle.step(t);
    }
    refreshScrub();
  }

  // ── Controls ────────────────────────────────────────────────────────────
  for (var i = 0; i < FIXTURES.length; i++) {
    var opt = document.createElement('option');
    opt.value = FIXTURES[i];
    opt.textContent = FIXTURES[i];
    fixtureSel.appendChild(opt);
  }

  fixtureSel.addEventListener('change', function() {
    if (fixtureSel.value) loadFixture(fixtureSel.value);
  }, false);

  playBtn.addEventListener('click', function() {
    if (!handle) return;
    playing = !playing;
    playBtn.textContent = playing ? 'Pause' : 'Play';
    lastTs = 0;
    if (playing) rafId = requestAnimationFrame(frame);
  }, false);

  scrub.addEventListener('input', function() {
    if (!handle) return;
    var t = parseInt(scrub.value, 10);
    if (playing) { playing = false; playBtn.textContent = 'Play'; }
    seek(t);
  }, false);

  window.addEventListener('beforeunload', teardown, false);

  // Start with the first fixture.
  loadFixture(FIXTURES[0]);
})();
