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
// ES5 on purpose (var/function, no arrow fns, no template literals): the baseline
// target is webOS TV 4.x, i.e. Chromium 53. Same reason the CSS ships -webkit-
// prefixes and avoids `inset`.
;(function() {
  var MAX_INTRO_MS = 5200;   // hard cap; the performance itself runs ~3s
  var pageStart = performance.now();
  var overlay = document.getElementById('intro-overlay');
  var stage = document.getElementById('intro-stage');
  var logo = overlay ? overlay.querySelector('.intro-logo') : null;
  var dismissed = false;
  var loadsDone = 0;
  var performanceDone = false;
  var timers = [];

  function at(ms, fn) { timers.push(setTimeout(fn, ms)); }
  function rand(a, b) { return a + Math.random() * (b - a); }
  function pick(arr) { return arr[Math.floor(Math.random() * arr.length)]; }

  var reducedMotion = false;
  try {
    reducedMotion = !!(window.matchMedia &&
      window.matchMedia('(prefers-reduced-motion: reduce)').matches);
  } catch (e) {}

  /* ══════════════════════════════════════════════════════════════════
     CAST — one entry per species.
     A species supplies: a `build` returning its DOM, the coat palettes to
     choose from, its physical range, and a `voice` that *schedules* its
     call on an AudioContext at an absolute time. Adding a dog means adding
     a `dog:` entry here; the director below is species-agnostic.
     ══════════════════════════════════════════════════════════════════ */
  var CAST = {
    cat: {
      build: buildCat,
      voice: voiceCat,
      scale: [0.85, 1.15],
      walkMs: [1250, 1700],
      stepMs: [290, 380],       // one leg cycle
      swayMs: [1300, 1900],     // idle tail
      vocalDelayMs: [200, 340], // settle → open mouth
      vocalMs: 620,
      // tailTip normally matches the fur so the tip just continues the tail;
      // only the breeds that really have a marked tip differ (tuxedo, siamese).
      palettes: [
        { name: 'ginger',  fur: '#e8913c', furDark: '#c2762e', belly: '#f7c58a', tailTip: '#e8913c', innerEar: '#c96a72', nose: '#d98a94', eye: '#2f4a2c' },
        { name: 'grey',    fur: '#8d97a4', furDark: '#6f7885', belly: '#c3cad4', tailTip: '#8d97a4', innerEar: '#b0757c', nose: '#c98c93', eye: '#3f6b3a' },
        { name: 'cream',   fur: '#e6d3b3', furDark: '#c8b291', belly: '#f6ecd9', tailTip: '#e6d3b3', innerEar: '#cf8f95', nose: '#dfa0a6', eye: '#4a6ea8' },
        // Dark coats are deliberately lifted well clear of the overlay
        // background (#000 → #0f172a); at true black-cat values the body and
        // tail disappear and only the belly and tail tip read.
        { name: 'tuxedo',  fur: '#4b5464', furDark: '#3a4250', belly: '#f2f4f7', tailTip: '#f2f4f7', innerEar: '#a5666d', nose: '#c98c93', eye: '#c8a63e' },
        { name: 'char',    fur: '#5d6880', furDark: '#485266', belly: '#8b94a8', tailTip: '#5d6880', innerEar: '#9d626a', nose: '#b47f86', eye: '#d8b23f' },
        { name: 'siamese', fur: '#d8c6ab', furDark: '#5c4a3d', belly: '#efe4d1', tailTip: '#5c4a3d', innerEar: '#5c4a3d', nose: '#7d6152', eye: '#4d8fc4' }
      ]
    }
  };

  /* ── Cat DOM ─────────────────────────────────────────────────────── */
  function buildCat(coat) {
    var cat = el('div', 'cat');
    // Far-side legs first so the near pair paints on top of them.
    cat.appendChild(el('div', 'cat-leg cat-leg-far cat-leg-bl'));
    cat.appendChild(el('div', 'cat-leg cat-leg-far cat-leg-fl'));
    var tail = el('div', 'cat-tail');
    tail.appendChild(el('div', 'cat-tail-tip'));
    cat.appendChild(tail);
    cat.appendChild(el('div', 'cat-body'));
    cat.appendChild(el('div', 'cat-belly'));
    cat.appendChild(el('div', 'cat-leg cat-leg-br'));
    cat.appendChild(el('div', 'cat-leg cat-leg-fr'));

    var head = el('div', 'cat-head');
    var earL = el('div', 'cat-ear cat-ear-l');
    earL.appendChild(el('div', 'cat-ear-inner'));
    var earR = el('div', 'cat-ear cat-ear-r');
    earR.appendChild(el('div', 'cat-ear-inner'));
    head.appendChild(earL);
    head.appendChild(earR);
    head.appendChild(el('div', 'cat-cheek'));
    head.appendChild(el('div', 'cat-eye cat-eye-l'));
    head.appendChild(el('div', 'cat-eye cat-eye-r'));
    head.appendChild(el('div', 'cat-whiskers'));
    head.appendChild(el('div', 'cat-nose'));
    head.appendChild(el('div', 'cat-mouth'));
    cat.appendChild(head);

    // Coat travels as custom properties so one palette drives every part.
    setVar(cat, '--fur', coat.fur);
    setVar(cat, '--fur-dark', coat.furDark);
    setVar(cat, '--belly', coat.belly);
    setVar(cat, '--tail-tip', coat.tailTip);
    setVar(cat, '--inner-ear', coat.innerEar);
    setVar(cat, '--nose', coat.nose);
    setVar(cat, '--eye', coat.eye);
    return cat;
  }

  // Schedule the cat's call. Returns when the audio ends.
  function voiceCat(ctx, when) {
    var dur = 0.42 + Math.random() * 0.18;
    var f0  = 620 + Math.random() * 150;
    var end = renderMeow(ctx, when, dur, f0, 0.50 + Math.random() * 0.25);
    if (Math.random() < 0.30) {
      end = renderMeow(ctx, end + rand(0.06, 0.20),
        dur * rand(0.55, 0.80), f0 * rand(0.82, 1.02), rand(0.28, 0.42));
    }
    return end;
  }

  function el(tag, cls) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    return n;
  }
  function setVar(node, name, value) {
    if (node.style.setProperty) node.style.setProperty(name, value);
  }
  function setAnim(node, prop, value) {
    node.style[prop] = value;
    node.style['webkit' + prop.charAt(0).toUpperCase() + prop.slice(1)] = value;
  }
  function setTransform(node, value) {
    node.style.webkitTransform = value;
    node.style.transform = value;
  }

  /* ── Casting ──────────────────────────────────────────────────────
     Returns the actors for this run. Today: exactly one cat. The shape is
     already a list so extra actors only need extra entries (each gets its
     own lane, mark and stagger). */
  function castList() {
    return [{ species: 'cat', lane: 0, stagger: 0, vocal: true }];
  }

  /* ── The performance ─────────────────────────────────────────────── */
  function runIntro() {
    if (!overlay) return;

    // Background fade happens regardless of whether the cast shows up.
    at(400, function() { overlay.classList.add('bg-reveal'); });

    if (!stage || reducedMotion) return logoOnly();

    var audio = openAudio();
    var actors = castList();
    var lastBeat = 0;

    for (var i = 0; i < actors.length; i++) {
      var beat = stageActor(actors[i], audio);
      if (beat > lastBeat) lastBeat = beat;
    }

    // Logo blooms once the cast has spoken, then we hand over to the app.
    at(lastBeat + 120, function() { if (logo) logo.classList.add('reveal'); });
    at(lastBeat + 780, function() {
      performanceDone = true;
      maybeDismiss();
    });
  }

  function logoOnly() {
    if (logo) logo.classList.add('reveal');
    playVoiceOnly();
    at(700, function() { performanceDone = true; maybeDismiss(); });
  }

  // Returns the timestamp (ms from intro start) at which this actor finishes.
  function stageActor(spec, audio) {
    var def = CAST[spec.species];
    if (!def) return 0;

    var coat = pick(def.palettes);
    var scale = rand(def.scale[0], def.scale[1]);
    var walkMs = rand(def.walkMs[0], def.walkMs[1]);
    var stepMs = rand(def.stepMs[0], def.stepMs[1]);
    var fromRight = Math.random() < 0.5;
    var lowPerf = document.body.classList.contains('low-perf');

    var actor = el('div', 'actor');
    var walk = el('div', 'actor-walk');
    var inner = el('div', 'actor-inner');
    inner.appendChild(def.build(coat));
    walk.appendChild(inner);
    actor.appendChild(walk);

    // Vertical placement: lane 0 is the front. Extra lanes sit further back
    // and are drawn smaller, so a future crowd has depth.
    var lane = spec.lane || 0;
    actor.style.bottom = (12 + lane * 7) + '%';

    // The actor is parked at its mark; the entrance animation supplies the
    // off-screen offset. Marks are spread around centre so a group does not
    // stack on one spot.
    var vw = Math.max(320, window.innerWidth || 960);

    // The cat is drawn at 160px; scale it to the viewport or it vanishes on a
    // TV. Capped so a 4K panel does not give us a billboard cat.
    var fit = Math.min(2.4, Math.max(1, vw / 640));
    var depth = scale * fit * (1 - lane * 0.12);
    var markX = vw * (fromRight ? rand(0.54, 0.66) : rand(0.34, 0.46));
    actor.style.left = Math.round(markX) + 'px';
    // Mirroring for a right-hand entrance also reverses the walk-in direction,
    // so the cat always faces the way it is travelling.
    setTransform(actor, 'scale(' + depth.toFixed(3) + ')' +
      (fromRight ? ' scaleX(-1)' : ''));
    stage.appendChild(actor);

    var t0 = spec.stagger || 0;

    at(t0, function() {
      actor.classList.add('staged');
      setAnim(walk, 'animationDuration', walkMs + 'ms');
      actor.classList.add('entering');
      if (!lowPerf) {
        actor.classList.add('walking');
        // Timing set inline rather than via calc(var()) — see style.css.
        var legs = actor.getElementsByClassName('cat-leg');
        for (var i = 0; i < legs.length; i++) {
          setAnim(legs[i], 'animationDuration', stepMs + 'ms');
          // Diagonal pairs: offset half a cycle via a negative delay.
          var far = legs[i].className.indexOf('cat-leg-far') !== -1;
          var front = legs[i].className.indexOf('-f') !== -1;
          if (far === front) setAnim(legs[i], 'animationDelay', (-stepMs / 2) + 'ms');
        }
        setAnim(inner, 'animationDuration', (stepMs / 2) + 'ms');
        var head = actor.getElementsByClassName('cat-head')[0];
        if (head) setAnim(head, 'animationDuration', stepMs + 'ms');
        var tw = actor.getElementsByClassName('cat-tail')[0];
        if (tw) setAnim(tw, 'animationDuration', (stepMs * 2) + 'ms');
      }
    });

    // Arrival: stop the feet, start an idle tail, settle.
    var arriveAt = t0 + walkMs;
    at(arriveAt, function() {
      actor.classList.remove('walking');
      actor.classList.add('idle');
      var ti = actor.getElementsByClassName('cat-tail')[0];
      if (ti && !lowPerf) {
        setAnim(ti, 'animationDuration', rand(def.swayMs[0], def.swayMs[1]) + 'ms');
      }
      scheduleBlinks(actor, lowPerf);
    });

    if (!spec.vocal) return arriveAt + 300;

    // The call. The audio is *queued* against the AudioContext clock at an
    // absolute time rather than fired from this timer, so a janky TV cannot
    // drift the sound away from the mouth opening.
    var vocalDelay = rand(def.vocalDelayMs[0], def.vocalDelayMs[1]);
    var vocalAt = arriveAt + vocalDelay;
    if (audio && audio.ctx) {
      queueVoice(audio, def, vocalAt);
    }
    at(vocalAt, function() { actor.classList.add('vocalizing'); });
    at(vocalAt + def.vocalMs, function() { actor.classList.remove('vocalizing'); });

    return vocalAt + def.vocalMs;
  }

  function scheduleBlinks(actor, lowPerf) {
    if (lowPerf) return;
    var cat = actor.getElementsByClassName('cat')[0];
    if (!cat) return;
    var n = Math.random() < 0.5 ? 1 : 2;
    for (var i = 0; i < n; i++) {
      (function(delay) {
        at(delay, function() {
          cat.classList.add('blink');
          // 130ms from *now* — the inner delay is relative to the outer firing,
          // so reusing `delay` here would hold the eyes shut for delay+130ms.
          at(130, function() { cat.classList.remove('blink'); });
        });
      })(rand(150, 900) + i * rand(400, 700));
    }
  }

  /* ── Audio: open the context early so calls can be queued ahead ───── */
  function openAudio() {
    try {
      var Ctor = window.AudioContext || window.webkitAudioContext;
      if (!Ctor) return null;
      var ctx = new Ctor();
      var bag = { ctx: ctx, origin: ctx.currentTime, ready: ctx.state !== 'suspended' };
      if (!bag.ready && ctx.resume) {
        ctx.resume().then(function() {
          bag.ready = true;
          // Re-anchor: currentTime only advances once running.
          bag.origin = ctx.currentTime;
          flushPending(bag);
        })['catch'](function() {});
      }
      bag.pending = [];
      return bag;
    } catch (e) {
      return null;
    }
  }

  function queueVoice(bag, def, whenMs) {
    if (!bag.ready) {
      // Context still suspended — remember it and schedule on resume.
      bag.pending.push({ def: def, whenMs: whenMs, queuedAt: performance.now() });
      return;
    }
    var lead = Math.max(0, whenMs - (performance.now() - pageStart)) / 1000;
    var end = def.voice(bag.ctx, bag.ctx.currentTime + lead);
    closeWhenDone(bag, end);
  }

  // The context resumed after a call was already due. Keep whatever lead time
  // is left; if the moment has passed, fire immediately rather than drop it.
  function flushPending(bag) {
    if (!bag.pending) return;
    while (bag.pending.length) {
      var p = bag.pending.shift();
      var lead = Math.max(0, p.whenMs - (performance.now() - pageStart)) / 1000;
      closeWhenDone(bag, p.def.voice(bag.ctx, bag.ctx.currentTime + lead));
    }
  }

  function closeWhenDone(bag, endTime) {
    var ms = Math.ceil((endTime - bag.ctx.currentTime + 0.25) * 1000);
    setTimeout(function() { try { bag.ctx.close(); } catch (e) {} }, Math.max(0, ms));
  }

  // Fallback when there is no stage (reduced motion): just make the sound.
  function playVoiceOnly() {
    var bag = openAudio();
    if (!bag) return;
    var def = CAST.cat;
    if (bag.ready) {
      closeWhenDone(bag, def.voice(bag.ctx, bag.ctx.currentTime + 0.05));
    } else {
      bag.pending.push({ def: def, whenMs: 0, queuedAt: performance.now() });
    }
  }

  // Render one meow starting at t0; returns the time it finishes.
  //
  // A meow is the spoken word "m-e-ow": an articulatory gesture, not a tone.
  // Grounded in feline bioacoustics (Meowsic / Nicastro / Sedova et al.):
  //   • the mouth traces  [m] nasal → [ɛ/æ] open → [ɑ] → [ɔ/o→u] rounded close
  //   • that mouth open→close gesture (muffled→bright→muffled) is what the ear
  //     decodes as "M-E-OW"; three formants (F1/F2/F3) carve each vowel out of a
  //     harmonic-rich glottal source.
  // Tuned for "cute" rather than "gruff": young/female register (higher f0), a
  // short call, formants scaled up for a small vocal tract, a sweet sine voice
  // blended under the fundamental, and no subharmonic FM (that reads as hoarse).
  function renderMeow(ctx, t0, dur, f0, amp) {
      var tEnd = t0 + dur;

      // ── Glottal source: sawtooth for harmonics, plus a sine for sweetness ──
      // The pitch arc: a quick lift then a gentle fall — a bright "hello!", not a wail.
      // The fall is shallow (down to 0.82×) so it stays perky instead of mournful.
      function pitchArc(param) {
        param.setValueAtTime(f0 * 0.94, t0);
        param.linearRampToValueAtTime(f0 * 1.16, t0 + dur * 0.16);
        param.exponentialRampToValueAtTime(Math.max(60, f0 * 0.82), tEnd);
      }

      var osc = ctx.createOscillator();
      osc.type = 'sawtooth';
      pitchArc(osc.frequency);

      // A pure sine on the fundamental, blended in under the formants. This is what
      // takes the edge off — it thickens the tone without adding harsh upper harmonics.
      var pure = ctx.createOscillator();
      pure.type = 'sine';
      pitchArc(pure.frequency);
      var pureGain = ctx.createGain();
      pureGain.gain.value = 0.42;

      // Vibrato — the living wobble (~±2%), on both voices so they stay locked.
      var vib = ctx.createOscillator();
      vib.type = 'sine';
      vib.frequency.value = 7 + Math.random() * 3;   // 7–10 Hz
      var vibGain = ctx.createGain();
      vibGain.gain.value = f0 * 0.02;
      vib.connect(vibGain);
      vibGain.connect(osc.frequency);
      vibGain.connect(pure.frequency);

      // ── Formant bank: three parallel band-passes tracing the vowel path ──
      //   frac:  0.00 nasal[m]   0.20 open[ɛ/æ]   0.55 [ɑ]   1.00 round-close[o→u]
      // Scaled ~1.2× above an adult-male tract: a shorter tract means higher
      // resonances, which is precisely what the ear hears as "small animal".
      var kf     = [0.00, 0.20, 0.55, 1.00];
      var tracks = [
        [ 500, 1050,  900,  470],   // F1  (mouth height)
        [1150, 2150, 1600, 1000],   // F2  (front↔back, rounding)
        [3000, 3400, 3150, 2900]    // F3  (timbre)
      ];
      var fGain = [1.0, 0.45, 0.14];  // F3 kept low — it's what made it sound gruff
      var fQ    = [7, 10, 11];

      var formantSum = ctx.createGain();
      formantSum.gain.value = 1;
      pure.connect(pureGain);
      pureGain.connect(formantSum);

      for (var i = 0; i < tracks.length; i++) {
        var bp = ctx.createBiquadFilter();
        bp.type = 'bandpass';
        bp.Q.value = fQ[i];
        bp.frequency.setValueAtTime(tracks[i][0], t0);
        for (var k = 1; k < kf.length; k++) {
          bp.frequency.linearRampToValueAtTime(tracks[i][k], t0 + dur * kf[k]);
        }
        var g = ctx.createGain();
        g.gain.value = fGain[i];
        osc.connect(bp);
        bp.connect(g);
        g.connect(formantSum);
      }

      // ── Mouth-openness low-pass: the muffled→bright→muffled "M…OW…" arc ──
      var mouth = ctx.createBiquadFilter();
      mouth.type = 'lowpass';
      mouth.Q.value = 0.7;
      // Ceiling kept at ~4.6 kHz (was 6.5 k) — above that it starts to buzz.
      mouth.frequency.setValueAtTime(550, t0);                             // closed / nasal [m]
      mouth.frequency.exponentialRampToValueAtTime(4600, t0 + dur * 0.20); // mouth wide open
      mouth.frequency.exponentialRampToValueAtTime(2900, t0 + dur * 0.55);
      mouth.frequency.exponentialRampToValueAtTime(780, tEnd);             // lips round & close
      formantSum.connect(mouth);

      // ── Amplitude: quiet nasal onset → swell on the open vowel → decay on close ──
      var env = ctx.createGain();
      env.gain.setValueAtTime(0.0001, t0);
      env.gain.linearRampToValueAtTime(amp * 0.22, t0 + dur * 0.10);  // [m] hum, eased in
      env.gain.linearRampToValueAtTime(amp, t0 + dur * 0.24);         // open peak
      env.gain.linearRampToValueAtTime(amp * 0.78, t0 + dur * 0.55);
      // Hold real level through the rounding close — a single long exponential to
      // near-zero collapses in the first few ms and swallows the whole "…ow".
      env.gain.linearRampToValueAtTime(amp * 0.42, t0 + dur * 0.80);
      env.gain.exponentialRampToValueAtTime(0.0001, tEnd);
      mouth.connect(env);
      env.connect(ctx.destination);

      // ── Breath noise, gated by mouth openness (turbulent air) ──
      // Barely there: audible breath is a large chesty animal, and reads as hoarse.
      var nLen = Math.ceil(ctx.sampleRate * (dur + 0.1));
      var nBuf = ctx.createBuffer(1, nLen, ctx.sampleRate);
      var nDat = nBuf.getChannelData(0);
      for (var ni = 0; ni < nLen; ni++) nDat[ni] = Math.random() * 2 - 1;
      var nSrc = ctx.createBufferSource();
      nSrc.buffer = nBuf;
      var nBp = ctx.createBiquadFilter();
      nBp.type = 'bandpass';
      nBp.Q.value = 1.5;
      nBp.frequency.setValueAtTime(1100, t0);
      nBp.frequency.exponentialRampToValueAtTime(3000, t0 + dur * 0.20);
      nBp.frequency.exponentialRampToValueAtTime(900, tEnd);
      var nGain = ctx.createGain();
      nGain.gain.setValueAtTime(0, t0);
      nGain.gain.linearRampToValueAtTime(amp * 0.02, t0 + dur * 0.20);
      nGain.gain.exponentialRampToValueAtTime(0.0001, tEnd);
      nSrc.connect(nBp);
      nBp.connect(nGain);
      nGain.connect(ctx.destination);

      // ── Start / stop every voice ──
      var tStop = tEnd + 0.05;
      osc.start(t0);  osc.stop(tStop);
      pure.start(t0); pure.stop(tStop);
      vib.start(t0);  vib.stop(tStop);
      nSrc.start(t0); nSrc.stop(tStop);

      return tStop;
  }

  function dismissIntro() {
    if (dismissed || !overlay) return;
    dismissed = true;
    for (var i = 0; i < timers.length; i++) clearTimeout(timers[i]);
    timers.length = 0;
    overlay.classList.add('dismiss');
    setTimeout(function() {
      if (overlay.parentNode) overlay.parentNode.removeChild(overlay);
    }, 550);
  }

  // Hand over only once the cat has had its say *and* the app is ready — then
  // whichever of the two finishes last decides when we go.
  function maybeDismiss() {
    if (dismissed || !performanceDone || loadsDone < 3) return;
    var elapsed = performance.now() - pageStart;
    var remaining = Math.max(0, Math.min(MAX_INTRO_MS - elapsed, 350));
    setTimeout(dismissIntro, remaining);
  }

  window.__introMarkLoaded = function() {
    loadsDone++;
    maybeDismiss();
  };

  window.__introMarkFailed = function() {
    window.__introMarkLoaded();
  };

  // Let anyone who has seen it enough skip straight through — includes the TV
  // remote, whose OK/Back arrive as ordinary keydowns.
  function skip() { dismissIntro(); }
  document.addEventListener('keydown', skip, false);
  document.addEventListener('click', skip, false);

  // Safety net: never hold the app hostage to the animation.
  setTimeout(function() {
    if (!dismissed) dismissIntro();
  }, MAX_INTRO_MS + 500);

  runIntro();
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

// prettyMediaName returns a human-readable display name from an item.
// Prefers classified metadata over raw filenames:
//   Movies → "Movie Title"
//   Shows  → "Show Name · S1·E5" or "Show Name · S1·E5 – Episode Title"
//   Fallback → cleaned-up filename (dots/underscores → spaces, extension stripped)
function prettyMediaName(it) {
  if (!it) return '';
  var md = (it.Metadata && typeof it.Metadata === 'object') ? it.Metadata : null;

  // 1. Classified name (movie title or episode title from metadata)
  var classifiedName = '';
  if (md && md.name && typeof md.name === 'string') classifiedName = md.name.trim();

  // 2. Show name from metadata (set during classification or show extraction)
  var showName = '';
  if (md && md.showName && typeof md.showName === 'string') showName = md.showName.trim();
  if (!showName) {
    // Try Metadata.title as a fallback
    if (md && md.title && typeof md.title === 'string') showName = md.title.trim();
  }

  // 3. Season and episode numbers
  var season = (md && md.season) ? md.season : 0;
  var episode = (md && md.episode) ? md.episode : 0;

  // Show episode path: "Show Name · S1·E5" or "Show Name · S1·E5 – Episode Title"
  if (showName && season && episode) {
    var result = showName + ' \u00B7 S' + season + '\u00B7E' + episode;
    if (classifiedName && classifiedName !== showName) {
      result += ' \u2013 ' + classifiedName;
    }
    return result;
  }

  // Movie path: just the classified name
  if (classifiedName) return classifiedName;

  // Fallback: clean up the raw filename
  var name = it.Name || '';
  name = name.replace(/\.[^.]+$/, '');       // strip extension
  name = name.replace(/[._-]/g, ' ').replace(/\s+/g, ' ').trim();
  return name || (it.Name || '');
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

// populateMediaDropdown indexes video items into the in-memory `media` map and
// syncs the persisted store. (Historically it also populated a <select>; that
// crude control was replaced by the search-driven UI.)
function populateMediaDropdown(items) {
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

// selectMedia loads a media item into the custom player. The heavy lifting
// (resume, transcode-aware seeking, autoplay) lives in the Player module below;
// this thin wrapper keeps the many existing call sites working.
function selectMedia(id) {
  mostRecentID = id;
  loadStreams(id);
  if (window.Player) {
    window.Player.load(id);
  }
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
  const btn = document.getElementById("recommendBtn");
  if (!inp.value.trim()) {
    status.innerText = "Tell the concierge what you feel like watching first.";
    return;
  }
  const req = JSON.stringify({ request: inp.value, context: constuctClientContext() });
  console.info("Sending:", req)
  status.innerText = "Consulting the concierge… (this may take a moment)";
  if (btn) { btn.classList.add("loading"); btn.querySelector("span").innerText = "Thinking…"; }
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
        status.innerText = "No recommendation found — try rephrasing.";
        return;
      }
      status.innerText = "▶ Now playing: " + prettyMediaName(item);
      selectMedia(item.ID);
    })
    .catch(err => {
      console.error("recommend error:");
      console.error(err)
      status.innerText = "Error — check kinoview server logs, or console logs";
    })
    .finally(() => {
      if (btn) { btn.classList.remove("loading"); btn.querySelector("span").innerText = "Recommend"; }
    });
}

function loadStreams(id) {
  fetch(`/gallery/streams/${id}`)
    .then(response => response.json())
    .then(data => {
      console.log(`Attempting to load streams for: ${id}`)

      const subMenu = document.getElementById("subsMenu");
      const audioMenu = document.getElementById("audioMenu");

      if (subMenu) subMenu.innerHTML = '';
      if (audioMenu) audioMenu.innerHTML = '';

      // Add "Off" option for subtitles
      if (subMenu) {
        const offBtn = createDropdownItem("Off", () => {
          selectSubtitle('off');
          updateActiveItem(subMenu, offBtn);
        }, true);
        subMenu.appendChild(offBtn);
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

  document.querySelectorAll('.popover').forEach(m => {
    if (m.id !== menuId) m.classList.add('hidden');
  });

  menu.classList.toggle('hidden');
}

// Close menus when clicking outside
document.addEventListener('click', (e) => {
  if (!e.target.closest('.ctrl-menu')) {
    document.querySelectorAll('.popover').forEach(m => m.classList.add('hidden'));
  }
  if (!e.target.closest('.header-search')) {
    const sr = document.getElementById('searchResults');
    if (sr) sr.classList.add('hidden');
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

  if (id === 'off' || id === "") {
    console.log("Disabling subtitles");
    track.src = "";
    track.removeAttribute("src");
    if (track.track) track.track.mode = "disabled";
  } else {
    console.log(`Attempting to set subs to: /gallery/streams/${mostRecentID}/stream/${id}`)
    track.src = `/gallery/streams/${mostRecentID}/stream/${id}`;
    if (track.track) track.track.mode = "showing";
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
            // Wait small delay for subs stream options to populate if needed
            setTimeout(() => {
              selectSubtitle(rec.subtitleID);
            }, 500);
          }
        };

        const title = document.createElement("strong");
        title.innerText = prettyMediaName(rec);

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

// ─────────────────────────────────────────────────────────────────────────
// Custom Video Player
//
// Wraps the <video> with a bespoke control bar so playback and seeking
// behave the same in fullscreen as they do inline. The server handles
// on-the-fly MKV→MP4 transcoding transparently.
// ─────────────────────────────────────────────────────────────────────────
(function () {
  const el = document.getElementById("player");
  const video = document.getElementById("screen");
  if (!el || !video) return;

  const controls = document.getElementById("controls");
  const bigPlay = document.getElementById("bigPlay");
  const playBtn = document.getElementById("playBtn");
  const back10 = document.getElementById("back10");
  const fwd10 = document.getElementById("fwd10");
  const skipIntroBtn = document.getElementById("skipIntro");
  const scrubber = document.getElementById("scrubber");
  const scrubFill = document.getElementById("scrubFill");
  const scrubBuffered = document.getElementById("scrubBuffered");
  const scrubThumb = document.getElementById("scrubThumb");
  const hoverTime = document.getElementById("scrubHoverTime");
  const timeLabel = document.getElementById("timeLabel");
  const timeCur = timeLabel.querySelector("span:first-child");
  const timeTot = timeLabel.querySelector("span:last-child");
  const muteBtn = document.getElementById("muteBtn");
  const volSlider = document.getElementById("volSlider");
  const fsBtn = document.getElementById("fsBtn");
  const titleEl = document.getElementById("playerTitle");
  const hero = document.getElementById("heroSection");
  const subsTrack = document.getElementById("subs");

  const SKIP_INTRO_SEC = 85; // typical TV intro length
  const NUDGE_SEC = 10;

  const state = {
    id: "",
    duration: 0,      // best-known total seconds (0 = unknown)
    wasPlaying: true,
    resumeAt: 0,      // pending native resume applied on loadedmetadata
    dragging: false,
  };

  function itemDurationSec(id) {
    const it = media[id];
    if (it && it.Metadata && typeof it.Metadata === "object" && it.Metadata.duration_min) {
      const s = parseFloat(it.Metadata.duration_min) * 60;
      if (isFinite(s) && s > 0) return s;
    }
    return 0;
  }

  function total() {
    if (state.duration > 0) return state.duration;
    if (isFinite(video.duration) && video.duration > 0) return video.duration;
    return 0;
  }

  function displayTime() {
    return video.currentTime || 0;
  }

  function fmt(sec) {
    if (!isFinite(sec) || sec < 0) sec = 0;
    sec = Math.floor(sec);
    const h = Math.floor(sec / 3600);
    const m = Math.floor((sec % 3600) / 60);
    const s = sec % 60;
    const pad = (n) => (n < 10 ? "0" + n : "" + n);
    return h > 0 ? h + ":" + pad(m) + ":" + pad(s) : m + ":" + pad(s);
  }

  function getResume(id, dur) {
    const it = loadPersistedMediaItem(id);
    const pf = it && it.playedFor;
    if (pf && pf > 10 && (dur <= 0 || pf < dur - 15)) return pf;
    return 0;
  }

  function resetSubtitles() {
    if (subsTrack) {
      subsTrack.src = "";
      subsTrack.removeAttribute("src");
      if (subsTrack.track) subsTrack.track.mode = "disabled";
    }
  }

  function load(id) {
    if (!id) return;
    state.id = id;
    mostRecentID = id;
    const it = media[id] || {};
    state.duration = itemDurationSec(id);
    const resume = getResume(id, state.duration);

    titleEl.textContent = prettyMediaName(it);
    if (hero) hero.classList.add("hidden");
    el.setAttribute("data-state", "active");
    el.classList.add("buffering");
    state.wasPlaying = true;
    state.resumeAt = resume;

    resetSubtitles();
    video.src = "/gallery/video/" + id;
    video.load();
    updateProgress();
  }

  // Seek to an absolute position (seconds from the start of the media).
  function seekTo(target, keepPlaying) {
    const tot = total();
    let t = Math.max(0, target);
    if (tot > 0) t = Math.min(t, tot - 0.5);
    try { video.currentTime = t; } catch (e) { /* not seekable yet */ }
  }

  function nudge(delta) {
    if (!state.id) return;
    seekTo(displayTime() + delta);
  }

  function togglePlay() {
    if (!state.id) return;
    if (video.paused) video.play().catch(() => {});
    else video.pause();
  }

  function updateProgress() {
    const tot = total();
    const dt = displayTime();
    if (tot > 0) {
      const frac = Math.max(0, Math.min(1, dt / tot));
      scrubFill.style.width = (frac * 100) + "%";
      scrubThumb.style.left = (frac * 100) + "%";
    } else {
      scrubFill.style.width = "0%";
      scrubThumb.style.left = "0%";
    }
    if (video.buffered && video.buffered.length && tot > 0) {
      const be = video.buffered.end(video.buffered.length - 1);
      scrubBuffered.style.width = Math.min(100, (be / tot) * 100) + "%";
    } else {
      scrubBuffered.style.width = "0%";
    }
    timeCur.textContent = fmt(dt);
    timeTot.textContent = tot > 0 ? fmt(tot) : "--:--";
  }

  // ── UI auto-hide ──
  let hideTimer = null;
  function showUI() {
    el.classList.add("show-ui");
    clearTimeout(hideTimer);
    if (!video.paused) {
      hideTimer = setTimeout(() => {
        if (!video.paused && !menuOpen()) el.classList.remove("show-ui");
      }, 2800);
    }
  }
  function menuOpen() {
    return !!el.querySelector(".popover:not(.hidden)");
  }
  el.addEventListener("pointermove", showUI);
  el.addEventListener("pointerleave", () => {
    if (!video.paused && !menuOpen()) el.classList.remove("show-ui");
  });

  // ── Persist progress ──
  let lastPersist = 0;
  function persist() {
    if (!state.id) return;
    if (video.seeking || video.readyState < 2) return;
    const now = Date.now();
    if (now - lastPersist < 900) return;
    lastPersist = now;
    const item = loadPersistedMediaItem(state.id);
    item.playedFor = video.currentTime;
    item.viewedAt = new Date().toISOString();
    const pm = getPersistedMedia();
    pm[state.id] = item;
    localStorage.setItem("media", JSON.stringify(pm));
  }

  // ── Video events ──
  video.addEventListener("loadedmetadata", () => {
    if (isFinite(video.duration) && video.duration > 0) {
      state.duration = video.duration;
    }
    if (state.resumeAt > 0) {
      try { video.currentTime = state.resumeAt; } catch (e) {}
      state.resumeAt = 0;
    }
    updateProgress();
  });
  video.addEventListener("canplay", () => {
    el.classList.remove("buffering");
    if (state.wasPlaying) video.play().catch(() => {});
  });
  video.addEventListener("play", () => { el.classList.add("playing"); showUI(); });
  video.addEventListener("pause", () => { el.classList.remove("playing"); showUI(); });
  video.addEventListener("waiting", () => el.classList.add("buffering"));
  video.addEventListener("playing", () => { el.classList.remove("buffering"); el.classList.add("playing"); });
  video.addEventListener("timeupdate", () => { if (!state.dragging) updateProgress(); persist(); });
  video.addEventListener("progress", updateProgress);
  video.addEventListener("volumechange", () => {
    el.classList.toggle("muted", video.muted || video.volume === 0);
    volSlider.value = video.muted ? 0 : video.volume;
  });
  video.addEventListener("ended", () => { el.classList.remove("playing"); showUI(); });

  // ── Button wiring ──
  bigPlay.addEventListener("click", togglePlay);
  playBtn.addEventListener("click", togglePlay);
  video.addEventListener("click", togglePlay);
  back10.addEventListener("click", () => nudge(-NUDGE_SEC));
  fwd10.addEventListener("click", () => nudge(NUDGE_SEC));
  skipIntroBtn.addEventListener("click", () => nudge(SKIP_INTRO_SEC));
  muteBtn.addEventListener("click", () => { video.muted = !video.muted; });
  volSlider.addEventListener("input", () => { video.volume = parseFloat(volSlider.value); video.muted = video.volume === 0; });

  fsBtn.addEventListener("click", () => {
    if (document.fullscreenElement) {
      document.exitFullscreen();
    } else if (el.requestFullscreen) {
      el.requestFullscreen().catch(() => {});
    } else if (video.webkitEnterFullscreen) {
      video.webkitEnterFullscreen(); // iOS Safari
    }
  });

  // ── Scrubber (pointer drag) ──
  function fractionFromEvent(e) {
    const rect = scrubber.getBoundingClientRect();
    return Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
  }
  scrubber.addEventListener("pointermove", (e) => {
    const tot = total();
    if (tot <= 0) return;
    const frac = fractionFromEvent(e);
    hoverTime.style.left = (frac * 100) + "%";
    hoverTime.textContent = fmt(frac * tot);
    if (state.dragging) {
      scrubFill.style.width = (frac * 100) + "%";
      scrubThumb.style.left = (frac * 100) + "%";
      timeCur.textContent = fmt(frac * tot);
    }
  });
  scrubber.addEventListener("pointerdown", (e) => {
    if (total() <= 0) return;
    state.dragging = true;
    scrubber.classList.add("dragging");
    scrubber.setPointerCapture(e.pointerId);
  });
  scrubber.addEventListener("pointerup", (e) => {
    if (!state.dragging) return;
    state.dragging = false;
    scrubber.classList.remove("dragging");
    const tot = total();
    if (tot > 0) seekTo(fractionFromEvent(e) * tot);
  });

  // ── Keyboard shortcuts ──
  document.addEventListener("keydown", (e) => {
    if (!state.id) return;
    const tag = (e.target.tagName || "").toLowerCase();
    if (tag === "input" || tag === "textarea" || e.target.isContentEditable) return;
    switch (e.key) {
      case " ":
      case "k": e.preventDefault(); togglePlay(); showUI(); break;
      case "ArrowLeft": e.preventDefault(); nudge(-NUDGE_SEC); showUI(); break;
      case "ArrowRight": e.preventDefault(); nudge(NUDGE_SEC); showUI(); break;
      case "f": el.requestFullscreen ? (document.fullscreenElement ? document.exitFullscreen() : el.requestFullscreen()) : null; break;
      case "m": video.muted = !video.muted; break;
    }
  });

  document.addEventListener("fullscreenchange", showUI);

  window.Player = { load, seekTo };
})();
