// ── Intro splash: a short story, acted out ────────────────────────────────
//
// The story is DATA (see internal/model/story.go). The server's storyteller
// agent prepares it ahead of time; this file is only a player for it. If the
// fetch fails we compose a minimal one locally, so the splash never depends on
// the network.
//
// ES5 on purpose (var/function, no arrow fns, no template literals): the
// baseline target is webOS TV 4.x, i.e. Chromium 53. Same reason the CSS ships
// -webkit- prefixes, avoids `inset`, and animates only transform/opacity.
;(function() {
  var STORY_URL = '/gallery/intro/story';
  var SESSION_END_URL = '/gallery/intro/session-end';
  var FETCH_BUDGET_MS = 320;   // how long we'll wait for a story before improvising
  var MAX_INTRO_MS = 13000;    // hard cap; a story runs ~9.5s

  var pageStart = now();
  var overlay = document.getElementById('intro-overlay');
  var stage = document.getElementById('intro-stage');
  var titleEl = document.getElementById('intro-title');
  var backdropEl = document.getElementById('intro-backdrop');
  var logo = overlay ? overlay.querySelector('.intro-logo') : null;

  var dismissed = false;
  var loadsDone = 0;
  var performanceDone = false;
  var started = false;
  var timers = [];

  function now() {
    return (window.performance && performance.now) ? performance.now() : +new Date();
  }
  function at(ms, fn) { timers.push(setTimeout(fn, ms)); }
  function rand(a, b) { return a + Math.random() * (b - a); }
  function randInt(a, b) { return Math.floor(rand(a, b + 1)); }
  function pick(arr) { return arr[Math.floor(Math.random() * arr.length)]; }

  var reducedMotion = false;
  try {
    reducedMotion = !!(window.matchMedia &&
      window.matchMedia('(prefers-reduced-motion: reduce)').matches);
  } catch (e) {}

  function lowPerf() { return document.body.classList.contains('low-perf'); }

  var BACKDROPS = { night: 1, livingroom: 1, garden: 1, theatre: 1, sunset: 1 };

  /* ══════════════════════════════════════════════════════════════════════
     VOICES
     One formant synthesiser, parameterised per species. Built on the meow
     research: a smaller animal has a higher f0 AND higher formants, because
     its vocal tract is shorter. The vowel path is the same shape for all of
     them — mouth closed, open, then rounded shut — which is what makes a
     call read as a word ("meow", "woof") rather than a tone.
     ══════════════════════════════════════════════════════════════════════ */
  var VOICES = {
    cat: {
      dur: [0.42, 0.60], f0: [620, 770], amp: [0.50, 0.72],
      kf: [0.00, 0.20, 0.55, 1.00],
      tracks: [[500, 1050, 900, 470], [1150, 2150, 1600, 1000], [3000, 3400, 3150, 2900]],
      gains: [1.0, 0.45, 0.14], q: [7, 10, 11],
      mouth: [550, 4600, 2900, 780], pitch: [0.94, 1.16, 0.82],
      vib: [7, 10, 0.02], noise: 0.02, pure: 0.42,
      bursts: [1, 2], gap: [0.06, 0.20], decay: 0.80
    },
    // Bigger animal: lower f0, lower formants, shorter and noisier bursts, and
    // a hard attack. Two or three of them makes a "woof woof".
    dog: {
      dur: [0.15, 0.22], f0: [240, 340], amp: [0.55, 0.78],
      kf: [0.00, 0.16, 0.45, 1.00],
      tracks: [[320, 820, 660, 340], [900, 1550, 1250, 820], [2100, 2500, 2300, 2000]],
      gains: [1.0, 0.52, 0.22], q: [5, 7, 8],
      mouth: [420, 3600, 2200, 520], pitch: [1.00, 1.06, 0.62],
      vib: [5, 8, 0.010], noise: 0.10, pure: 0.24, decay: 0.45,
      bursts: [2, 3], gap: [0.09, 0.17]
    },
    // Tiny animal: very high and very short, almost a pure tone.
    mouse: {
      dur: [0.08, 0.13], f0: [1700, 2500], amp: [0.20, 0.34],
      kf: [0.00, 0.25, 0.60, 1.00],
      tracks: [[900, 1500, 1300, 900], [2600, 3600, 3200, 2600], [5000, 5600, 5200, 4800]],
      gains: [1.0, 0.30, 0.08], q: [9, 12, 13],
      mouth: [900, 6000, 4200, 1200], pitch: [0.95, 1.26, 0.90],
      vib: [10, 14, 0.03], noise: 0.01, pure: 0.55, decay: 0.55,
      bursts: [1, 3], gap: [0.05, 0.11]
    }
  };

  // Schedule a whole vocalisation (one or more bursts) at absolute audio time.
  // Returns the time it finishes.
  function speak(ctx, t0, voice) {
    var n = randInt(voice.bursts[0], voice.bursts[1]);
    var f0 = rand(voice.f0[0], voice.f0[1]);
    var amp = rand(voice.amp[0], voice.amp[1]);
    var end = t0;
    for (var i = 0; i < n; i++) {
      var d = rand(voice.dur[0], voice.dur[1]);
      // Later bursts drop slightly in pitch and level, like a real repeat.
      var f = f0 * (1 - i * 0.05);
      var a = amp * (i === 0 ? 1 : 0.82);
      end = renderVoice(ctx, end, d, f, a, voice);
      if (i < n - 1) end += rand(voice.gap[0], voice.gap[1]);
    }
    return end;
  }

  // Render one burst. Returns the time it ends.
  function renderVoice(ctx, t0, dur, f0, amp, v) {
    var tEnd = t0 + dur;

    function pitchArc(param) {
      param.setValueAtTime(f0 * v.pitch[0], t0);
      param.linearRampToValueAtTime(f0 * v.pitch[1], t0 + dur * 0.16);
      param.exponentialRampToValueAtTime(Math.max(60, f0 * v.pitch[2]), tEnd);
    }

    var osc = ctx.createOscillator();
    osc.type = 'sawtooth';
    pitchArc(osc.frequency);

    // A sine on the fundamental, mixed under the formants. This is what takes
    // the harsh edge off without flattening the timbre.
    var pure = ctx.createOscillator();
    pure.type = 'sine';
    pitchArc(pure.frequency);
    var pureGain = ctx.createGain();
    pureGain.gain.value = v.pure;

    var vib = ctx.createOscillator();
    vib.type = 'sine';
    vib.frequency.value = rand(v.vib[0], v.vib[1]);
    var vibGain = ctx.createGain();
    vibGain.gain.value = f0 * v.vib[2];
    vib.connect(vibGain);
    vibGain.connect(osc.frequency);
    vibGain.connect(pure.frequency);

    // Formant bank — three band-passes tracing the vowel path.
    var formantSum = ctx.createGain();
    formantSum.gain.value = 1;
    pure.connect(pureGain);
    pureGain.connect(formantSum);

    for (var i = 0; i < v.tracks.length; i++) {
      var bp = ctx.createBiquadFilter();
      bp.type = 'bandpass';
      bp.Q.value = v.q[i];
      bp.frequency.setValueAtTime(v.tracks[i][0], t0);
      for (var k = 1; k < v.kf.length; k++) {
        bp.frequency.linearRampToValueAtTime(v.tracks[i][k], t0 + dur * v.kf[k]);
      }
      var g = ctx.createGain();
      g.gain.value = v.gains[i];
      osc.connect(bp);
      bp.connect(g);
      g.connect(formantSum);
    }

    // Mouth openness: muffled → bright → muffled. The gesture, not the pitch,
    // is what the ear decodes as a word.
    var mouth = ctx.createBiquadFilter();
    mouth.type = 'lowpass';
    mouth.Q.value = 0.7;
    mouth.frequency.setValueAtTime(v.mouth[0], t0);
    mouth.frequency.exponentialRampToValueAtTime(v.mouth[1], t0 + dur * v.kf[1]);
    mouth.frequency.exponentialRampToValueAtTime(v.mouth[2], t0 + dur * v.kf[2]);
    mouth.frequency.exponentialRampToValueAtTime(v.mouth[3], tEnd);
    formantSum.connect(mouth);

    // Amplitude. The extra breakpoint before the tail matters: a single long
    // exponential to near-zero collapses immediately and swallows the ending.
    var env = ctx.createGain();
    env.gain.setValueAtTime(0.0001, t0);
    env.gain.linearRampToValueAtTime(amp * 0.22, t0 + dur * 0.10);
    env.gain.linearRampToValueAtTime(amp, t0 + dur * 0.24);
    env.gain.linearRampToValueAtTime(amp * v.decay, t0 + dur * 0.55);
    env.gain.linearRampToValueAtTime(amp * v.decay * 0.5, t0 + dur * 0.80);
    env.gain.exponentialRampToValueAtTime(0.0001, tEnd);
    mouth.connect(env);
    env.connect(ctx.destination);

    // Breath noise, gated by mouth openness.
    if (v.noise > 0) {
      var nLen = Math.ceil(ctx.sampleRate * (dur + 0.05));
      var nBuf = ctx.createBuffer(1, nLen, ctx.sampleRate);
      var nDat = nBuf.getChannelData(0);
      for (var ni = 0; ni < nLen; ni++) nDat[ni] = Math.random() * 2 - 1;
      var nSrc = ctx.createBufferSource();
      nSrc.buffer = nBuf;
      var nBp = ctx.createBiquadFilter();
      nBp.type = 'bandpass';
      nBp.Q.value = 1.5;
      nBp.frequency.setValueAtTime(v.mouth[0] * 1.6, t0);
      nBp.frequency.exponentialRampToValueAtTime(v.mouth[1] * 0.6, t0 + dur * v.kf[1]);
      nBp.frequency.exponentialRampToValueAtTime(v.mouth[3] * 1.2, tEnd);
      var nGain = ctx.createGain();
      nGain.gain.setValueAtTime(0, t0);
      nGain.gain.linearRampToValueAtTime(amp * v.noise, t0 + dur * v.kf[1]);
      nGain.gain.exponentialRampToValueAtTime(0.0001, tEnd);
      nSrc.connect(nBp);
      nBp.connect(nGain);
      nGain.connect(ctx.destination);
      nSrc.start(t0);
      nSrc.stop(tEnd + 0.05);
    }

    var tStop = tEnd + 0.05;
    osc.start(t0);  osc.stop(tStop);
    pure.start(t0); pure.stop(tStop);
    vib.start(t0);  vib.stop(tStop);
    return tStop;
  }

  /* ══════════════════════════════════════════════════════════════════════
     CHARACTERS
     Built from divs + border-radius: CSS transforms on SVG child elements
     are unreliable on the old Blink builds in webOS TVs. Coats travel as
     custom properties so one palette drives every part.
     ══════════════════════════════════════════════════════════════════════ */
  function el(tag, cls) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    return n;
  }
  function setVar(node, name, value) {
    if (node.style.setProperty && value) node.style.setProperty(name, value);
  }
  function setAnim(node, prop, value) {
    node.style[prop] = value;
    node.style['webkit' + prop.charAt(0).toUpperCase() + prop.slice(1)] = value;
  }
  function setTransform(node, value) {
    node.style.webkitTransform = value;
    node.style.transform = value;
  }
  function applyCoat(root, coat) {
    setVar(root, '--fur', coat.fur);
    setVar(root, '--fur-dark', coat.furDark);
    setVar(root, '--belly', coat.belly);
    setVar(root, '--tail-tip', coat.tailTip);
    setVar(root, '--inner-ear', coat.innerEar);
    setVar(root, '--nose', coat.nose);
    setVar(root, '--eye', coat.eye);
  }

  var CHARACTERS = {
    cat: {
      cls: 'cat', w: 160, h: 112, voice: VOICES.cat,
      // Dark coats are lifted clear of the overlay background; at true
      // black-cat values the body vanishes and only the belly reads.
      coats: {
        ginger:  { fur: '#e8913c', furDark: '#c2762e', belly: '#f7c58a', tailTip: '#e8913c', innerEar: '#c96a72', nose: '#d98a94', eye: '#2f4a2c' },
        grey:    { fur: '#8d97a4', furDark: '#6f7885', belly: '#c3cad4', tailTip: '#8d97a4', innerEar: '#b0757c', nose: '#c98c93', eye: '#3f6b3a' },
        cream:   { fur: '#e6d3b3', furDark: '#c8b291', belly: '#f6ecd9', tailTip: '#e6d3b3', innerEar: '#cf8f95', nose: '#dfa0a6', eye: '#4a6ea8' },
        tuxedo:  { fur: '#4b5464', furDark: '#3a4250', belly: '#f2f4f7', tailTip: '#f2f4f7', innerEar: '#a5666d', nose: '#c98c93', eye: '#c8a63e' },
        char:    { fur: '#5d6880', furDark: '#485266', belly: '#8b94a8', tailTip: '#5d6880', innerEar: '#9d626a', nose: '#b47f86', eye: '#d8b23f' },
        siamese: { fur: '#d8c6ab', furDark: '#5c4a3d', belly: '#efe4d1', tailTip: '#5c4a3d', innerEar: '#5c4a3d', nose: '#7d6152', eye: '#4d8fc4' }
      },
      build: buildCat
    },
    dog: {
      cls: 'dog', w: 184, h: 126, voice: VOICES.dog,
      coats: {
        tan:   { fur: '#d9a15e', furDark: '#b5813f', belly: '#f0d6a8', tailTip: '#d9a15e', innerEar: '#a8636a', nose: '#4a3b33', eye: '#3b2c22' },
        cocoa: { fur: '#9a6b4a', furDark: '#7a5238', belly: '#d7b391', tailTip: '#9a6b4a', innerEar: '#96565d', nose: '#3d2f28', eye: '#33261e' },
        cloud: { fur: '#e3e0d8', furDark: '#bdb9ae', belly: '#f6f5f0', tailTip: '#e3e0d8', innerEar: '#c98c93', nose: '#4a3b33', eye: '#3b2c22' },
        slate: { fur: '#7c8798', furDark: '#616b7a', belly: '#b6bfcc', tailTip: '#7c8798', innerEar: '#9d626a', nose: '#3d3a38', eye: '#2f2a25' }
      },
      build: buildDog
    },
    mouse: {
      cls: 'mouse', w: 84, h: 52, voice: VOICES.mouse,
      coats: {
        field: { fur: '#9c9188', furDark: '#7d746c', belly: '#ded8d0', tailTip: '#c2a8a4', innerEar: '#d3a3a8', nose: '#c98c93', eye: '#241f1c' },
        white: { fur: '#e8e4de', furDark: '#c4bfb8', belly: '#f7f5f2', tailTip: '#e0bfbb', innerEar: '#e2acb1', nose: '#d99aa1', eye: '#7a2020' }
      },
      build: buildMouse
    }
  };

  function buildCat(coat) {
    var c = el('div', 'cat');
    // Far-side legs first so the near pair paints on top.
    c.appendChild(el('div', 'cat-leg cat-leg-far cat-leg-bl'));
    c.appendChild(el('div', 'cat-leg cat-leg-far cat-leg-fl'));
    var tail = el('div', 'cat-tail');
    tail.appendChild(el('div', 'cat-tail-tip'));
    c.appendChild(tail);
    c.appendChild(el('div', 'cat-body'));
    c.appendChild(el('div', 'cat-belly'));
    c.appendChild(el('div', 'cat-leg cat-leg-br'));
    c.appendChild(el('div', 'cat-leg cat-leg-fr'));

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
    c.appendChild(head);
    applyCoat(c, coat);
    return c;
  }

  function buildDog(coat) {
    var d = el('div', 'dog');
    d.appendChild(el('div', 'dog-leg dog-leg-far dog-leg-bl'));
    d.appendChild(el('div', 'dog-leg dog-leg-far dog-leg-fl'));
    var tail = el('div', 'dog-tail');
    tail.appendChild(el('div', 'dog-tail-tip'));
    d.appendChild(tail);
    d.appendChild(el('div', 'dog-body'));
    d.appendChild(el('div', 'dog-belly'));
    d.appendChild(el('div', 'dog-leg dog-leg-br'));
    d.appendChild(el('div', 'dog-leg dog-leg-fr'));

    var head = el('div', 'dog-head');
    head.appendChild(el('div', 'dog-skull'));
    var ear = el('div', 'dog-ear');   // floppy, hangs down
    ear.appendChild(el('div', 'dog-ear-inner'));
    head.appendChild(ear);
    var snout = el('div', 'dog-snout');
    snout.appendChild(el('div', 'dog-nose'));
    snout.appendChild(el('div', 'dog-mouth'));
    head.appendChild(snout);
    head.appendChild(el('div', 'dog-eye dog-eye-l'));
    head.appendChild(el('div', 'dog-eye dog-eye-r'));
    head.appendChild(el('div', 'dog-brow'));
    d.appendChild(head);
    applyCoat(d, coat);
    return d;
  }

  function buildMouse(coat) {
    var m = el('div', 'mouse');
    var tail = el('div', 'mouse-tail');
    m.appendChild(tail);
    m.appendChild(el('div', 'mouse-body'));
    m.appendChild(el('div', 'mouse-leg mouse-leg-b'));
    m.appendChild(el('div', 'mouse-leg mouse-leg-f'));
    var head = el('div', 'mouse-head');
    head.appendChild(el('div', 'mouse-ear mouse-ear-b'));
    head.appendChild(el('div', 'mouse-ear mouse-ear-f'));
    head.appendChild(el('div', 'mouse-eye'));
    head.appendChild(el('div', 'mouse-snout'));
    head.appendChild(el('div', 'mouse-nose'));
    m.appendChild(head);
    applyCoat(m, coat);
    return m;
  }

  var PROPS = {
    yarn: { cls: 'prop-yarn', w: 34, h: 34, build: function() {
      var p = el('div', 'prop prop-yarn');
      p.appendChild(el('div', 'yarn-ball'));
      p.appendChild(el('div', 'yarn-strand'));
      return p;
    } },
    box: { cls: 'prop-box', w: 92, h: 62, build: function() {
      var p = el('div', 'prop prop-box');
      p.appendChild(el('div', 'box-back'));
      p.appendChild(el('div', 'box-front'));
      return p;
    } }
  };

  /* ══════════════════════════════════════════════════════════════════════
     SET PIECES & THE CELL GRID
     The set is a grid of addressable cells. The playwright puts a piece in a
     cell when dressing the stage, and a `setCell` beat can swap it mid-play.
     Pieces are built as DOM from a whitelist — never innerHTML, because the
     story that names them is LLM-authored.
     ══════════════════════════════════════════════════════════════════════ */
  var PIECES = {
    tree:   function() { return layered('piece piece-tree',   ['tree-trunk', 'tree-crown', 'tree-crown2']); },
    bush:   function() { return layered('piece piece-bush',   ['bush-a', 'bush-b']); },
    fence:  function() { return layered('piece piece-fence',  ['fence-rail', 'fence-post p1', 'fence-post p2', 'fence-post p3']); },
    cloud:  function() { return layered('piece piece-cloud',  ['cloud-a', 'cloud-b', 'cloud-c']); },
    moon:   function() { return layered('piece piece-moon',   ['moon-disc', 'moon-glow']); },
    sofa:   function() { return layered('piece piece-sofa',   ['sofa-back', 'sofa-seat', 'sofa-arm a1', 'sofa-arm a2']); },
    lamp:   function() { return layered('piece piece-lamp',   ['lamp-pole', 'lamp-shade', 'lamp-glow']); },
    plant:  function() { return layered('piece piece-plant',  ['plant-pot', 'plant-leaf l1', 'plant-leaf l2', 'plant-leaf l3']); },
    window: function() { return layered('piece piece-window', ['win-frame', 'win-pane', 'win-bar-v', 'win-bar-h', 'win-sill']); },
    rug:    function() { return layered('piece piece-rug',    ['rug-base', 'rug-inner']); }
  };

  function layered(rootCls, parts) {
    var root = el('div', rootCls);
    for (var i = 0; i < parts.length; i++) root.appendChild(el('div', parts[i]));
    return root;
  }

  // Rows are depth bands. Further back reads smaller and hazier — cheap
  // atmospheric perspective that stops the set competing with the cast.
  // Pieces are authored on an 80px grid and scaled up here. The scales are set
  // against the cast, not against each other: a mid-row tree must read as
  // TALLER than a dog, or the whole set looks like dollhouse furniture.
  // `dim` is applied as a brightness filter, NOT opacity: a translucent tree
  // lets the backdrop bleed through and stops reading as a solid object.
  var ROWS = {
    sky:  { bottom: 58, scale: 1.30, dim: 1.00 },
    far:  { bottom: 26, scale: 1.45, dim: 0.72 },
    mid:  { bottom: 15, scale: 1.90, dim: 0.88 },
    near: { bottom: 6,  scale: 2.30, dim: 1.00 }
  };

  // Build the cell grid and remember each cell so beats can address it.
  function buildCells(story, sc) {
    var cells = (story.scene && story.scene.cells) || [];
    for (var i = 0; i < cells.length; i++) {
      var spec = cells[i];
      var row = ROWS[spec.row];
      if (!row) continue;
      var holder = el('div', 'cell cell--' + spec.row);
      var col = Math.max(0, Math.min(5, spec.col || 0));
      // Columns are sixths; the piece is centred in its column.
      holder.style.left = (col * (100 / 6)) + '%';
      holder.style.width = (100 / 6) + '%';
      holder.style.bottom = row.bottom + '%';
      holder.style.zIndex = String(spec.row === 'near' ? 15 : 5);
      setTransform(holder, 'scale(' + (row.scale * fitScale()).toFixed(3) + ')');
      if (row.dim < 1) {
        var f = 'brightness(' + row.dim + ')';
        holder.style.webkitFilter = f;
        holder.style.filter = f;
      }
      stage.appendChild(holder);
      sc.cells[spec.id] = holder;
      fillCell(holder, spec.piece);
    }
  }

  // Replace a cell's contents. Empty piece clears it.
  function fillCell(holder, piece) {
    while (holder.firstChild) holder.removeChild(holder.firstChild);
    var make = piece && PIECES[piece];
    if (!make) return;
    var node = make();
    holder.appendChild(node);
    // Let it arrive rather than pop.
    node.classList.add('arriving');
  }

  /* ══════════════════════════════════════════════════════════════════════
     STAGE & MOVEMENT
     Each actor is four nested layers, one transform each, so they compose
     without fighting:
       .actor      left anchor + scale
       .actor-walk movement offset (transitioned)
       .actor-face facing (scaleX)
       .actor-inner walk bob (animation)
     The anchor is set to the DESTINATION immediately and only the offset is
     animated. If the engine refuses to animate, everyone is simply standing
     where they belong instead of stranded off-screen.
     ══════════════════════════════════════════════════════════════════════ */
  function stageWidth() {
    if (stage && stage.offsetWidth) return stage.offsetWidth;
    return Math.max(320, window.innerWidth || 960);
  }

  function fitScale() {
    // Characters are drawn at ~160px; scale to the viewport or they vanish on
    // a TV. Capped so a 4K panel does not give us a billboard cat.
    return Math.min(2.4, Math.max(1, stageWidth() / 640));
  }

  function makeActor(spec) {
    var def = CHARACTERS[spec.character];
    if (!def) return null;

    var a = {
      id: spec.id, spec: spec, def: def,
      el: el('div', 'actor actor--' + def.cls),
      walk: el('div', 'actor-walk'),
      face: el('div', 'actor-face'),
      inner: el('div', 'actor-inner'),
      px: 0, facing: 1, onStage: false
    };

    var coatNames = [];
    for (var k in def.coats) { if (def.coats.hasOwnProperty(k)) coatNames.push(k); }
    var coat = def.coats[spec.coat] || def.coats[pick(coatNames)];

    a.root = def.build(coat);
    a.inner.appendChild(a.root);
    a.face.appendChild(a.inner);
    a.walk.appendChild(a.face);
    a.el.appendChild(a.walk);

    a.el.style.width = def.w + 'px';
    a.el.style.height = def.h + 'px';

    var lane = spec.lane || 0;
    a.el.style.bottom = (11 + lane * 8) + '%';
    a.depth = (spec.scale || 1) * fitScale() * (1 - lane * 0.12);
    setTransform(a.el, 'scale(' + a.depth.toFixed(3) + ')');
    // Nearer lanes paint in front.
    a.el.style.zIndex = String(20 - lane);

    // Start hidden and off to the side; `enter` reveals it.
    anchor(a, -offStagePx(a));
    return a;
  }

  function offStagePx(a) { return (a.def.w + 60) * a.depth; }

  // Put an actor at an absolute x with no animation.
  function anchor(a, px) {
    a.px = px;
    a.el.style.left = Math.round(px) + 'px';
    setAnim(a.walk, 'transitionDuration', '0ms');
    setTransform(a.walk, 'translate3d(0,0,0)');
  }

  function setFacing(a, dir) {
    // dir: 1 = facing right (drawn orientation), -1 = facing left.
    if (a.facing === dir) return;
    a.facing = dir;
    setTransform(a.face, dir < 0 ? 'scaleX(-1)' : 'scaleX(1)');
  }

  // Glide to an absolute x over ms, walking the legs on the way.
  function glide(a, toPx, ms, ease) {
    var dx = a.px - toPx;
    if (Math.abs(dx) < 1) return;
    setFacing(a, dx > 0 ? -1 : 1);

    a.px = toPx;
    a.el.style.left = Math.round(toPx) + 'px';

    // Start displaced by dx, then transition the displacement away.
    setAnim(a.walk, 'transitionDuration', '0ms');
    setTransform(a.walk, 'translate3d(' + Math.round(dx) + 'px,0,0)');
    void a.walk.offsetWidth;   // commit the start pose before transitioning
    setAnim(a.walk, 'transitionProperty', 'transform');
    setAnim(a.walk, 'transitionTimingFunction', ease || 'linear');
    setAnim(a.walk, 'transitionDuration', ms + 'ms');
    setTransform(a.walk, 'translate3d(0,0,0)');

    startWalking(a, ms, Math.abs(dx));
  }

  function startWalking(a, ms, distancePx) {
    if (lowPerf()) return;
    // Step rate follows speed, so fast moves get quick little legs.
    var speed = distancePx / Math.max(1, ms);        // px per ms
    var stepMs = Math.max(140, Math.min(420, 150 / Math.max(0.08, speed)));
    a.el.classList.remove('resting');
    a.el.classList.add('walking');
    var legs = a.root.getElementsByClassName(a.def.cls + '-leg');
    for (var i = 0; i < legs.length; i++) {
      setAnim(legs[i], 'animationDuration', Math.round(stepMs) + 'ms');
      var far = legs[i].className.indexOf('-far') !== -1;
      var front = legs[i].className.indexOf('-f') !== -1;
      if (far === front) setAnim(legs[i], 'animationDelay', Math.round(-stepMs / 2) + 'ms');
    }
    setAnim(a.inner, 'animationDuration', Math.round(stepMs / 2) + 'ms');
    var head = a.root.getElementsByClassName(a.def.cls + '-head')[0];
    if (head) setAnim(head, 'animationDuration', Math.round(stepMs) + 'ms');
    var tail = a.root.getElementsByClassName(a.def.cls + '-tail')[0];
    if (tail) setAnim(tail, 'animationDuration', Math.round(stepMs * 2) + 'ms');

    at(ms, function() { stopWalking(a); });
  }

  function stopWalking(a) {
    a.el.classList.remove('walking');
    a.el.classList.add('resting');
  }

  /* ── Actions ─────────────────────────────────────────────────────────
     The closed vocabulary from internal/model/story.go. Unknown actions
     never reach here — the server drops them — but we still guard. */
  var ACTIONS = {
    enter: function(a, b, sc) {
      var side = b.From || (a.spec.x < 0.5 ? 'left' : 'right');
      var w = stageWidth();
      anchor(a, side === 'left' ? -offStagePx(a) : w + offStagePx(a));
      a.el.classList.add('staged');
      a.onStage = true;
      var ms = b.Ms || 1100;
      glide(a, markPx(a, b.X || a.spec.x), ms);
    },

    exit: function(a, b, sc) {
      var w = stageWidth();
      var side = b.From || (a.px > w / 2 ? 'right' : 'left');
      var ms = b.Ms || 700;
      glide(a, side === 'left' ? -offStagePx(a) : w + offStagePx(a), ms);
      at(ms, function() { a.el.classList.remove('staged'); a.onStage = false; });
    },

    walkTo: function(a, b, sc) {
      glide(a, markPx(a, b.X), b.Ms || 800);
    },

    vocalize: function(a, b, sc) {
      // Audio was queued up front (see queueVocalizations); this is the mouth.
      a.el.classList.add('vocalizing');
      at(b.Ms || 520, function() { a.el.classList.remove('vocalizing'); });
    },

    sit:     function(a, b) { hold(a, 'sitting', b.Ms || 1200); },
    nap:     function(a, b) { hold(a, 'napping', b.Ms || 1500); },
    stretch: function(a, b) { pulse(a, 'stretching', b.Ms || 700); },
    blink:   function(a, b) { pulse(a, 'blinking', 140); },

    // Face a target and freeze — a beat of tension.
    stareoff: function(a, b, sc) {
      var t = sc.actors[b.Target];
      if (t) setFacing(a, t.px > a.px ? 1 : -1);
      pulse(a, 'staring', b.Ms || 900);
    },

    // Step in close and touch noses.
    greet: function(a, b, sc) {
      var t = sc.actors[b.Target];
      if (!t) return;
      var gap = 42 * a.depth;
      var toward = t.px > a.px ? t.px - gap : t.px + gap;
      glide(a, toward, b.Ms || 480, 'ease-out');
      at(b.Ms || 480, function() { pulse(a, 'greeting', 520); });
    },

    // Crouch then spring at the target.
    pounce: function(a, b, sc) {
      var t = sc.actors[b.Target];
      if (t) setFacing(a, t.px > a.px ? 1 : -1);
      a.el.classList.add('pouncing');
      at(420, function() { a.el.classList.remove('pouncing'); });
      if (t) {
        var land = t.px + (t.px > a.px ? -30 : 30) * a.depth;
        at(120, function() { glide(a, land, 260, 'ease-in'); });
      }
    },

    // Run after the target, off toward wherever it went.
    chase: function(a, b, sc) {
      var t = sc.actors[b.Target];
      var ms = b.Ms || 800;
      var w = stageWidth();
      var toPx;
      if (t) {
        toPx = t.px + (t.px > a.px ? offStagePx(a) : -offStagePx(a)) * 0.6;
      } else {
        toPx = a.px > w / 2 ? w + offStagePx(a) : -offStagePx(a);
      }
      a.el.classList.add('running');
      glide(a, toPx, ms, 'ease-in');
      at(ms, function() { a.el.classList.remove('running'); });
    },

    // ── Scene actions: dress the set mid-play ──────────────────────────
    // These carry no actor; the player routes them separately.

    // Swat at a prop; the prop reacts.
    bat: function(a, b, sc) {
      var p = sc.props[b.Target];
      if (p) setFacing(a, p.px > a.px ? 1 : -1);
      pulse(a, 'batting', b.Ms || 380);
      if (p) {
        at(120, function() {
          p.el.classList.remove('jostled');
          void p.el.offsetWidth;
          p.el.classList.add('jostled');
        });
      }
    }
  };

  // Scene beats are dispatched separately because they act on the set, not on
  // a character, and so have no actor to look up.
  var SCENE_ACTIONS = {
    setCell: function(beat, sc) {
      var holder = sc.cells[beat.target];
      if (!holder) return;
      holder.classList.remove('swapping');
      void holder.offsetWidth;
      holder.classList.add('swapping');
      // Swap at the midpoint of the fade so the change is not seen happening.
      at(220, function() { fillCell(holder, beat.piece); });
    },
    setBackdrop: function(beat) {
      if (!backdropEl || !BACKDROPS[beat.piece]) return;
      backdropEl.classList.add('changing');
      at(320, function() {
        backdropEl.className = 'intro-backdrop lit changing backdrop--' + beat.piece;
        at(60, function() { backdropEl.classList.remove('changing'); });
      });
    }
  };

  function markPx(a, xFrac) {
    var w = stageWidth();
    var x = (typeof xFrac === 'number' && xFrac > 0) ? xFrac : 0.5;
    // Keep the body on screen at its scaled size.
    var half = (a.def.w * a.depth) / 2;
    return Math.max(half * 0.2, Math.min(w - half * 1.2, x * w - half));
  }

  function hold(a, cls, ms) {
    stopWalking(a);
    a.el.classList.add(cls);
    at(ms, function() { a.el.classList.remove(cls); });
  }
  function pulse(a, cls, ms) {
    a.el.classList.remove(cls);
    void a.el.offsetWidth;
    a.el.classList.add(cls);
    at(ms, function() { a.el.classList.remove(cls); });
  }

  /* ══════════════════════════════════════════════════════════════════════
     AUDIO — every vocalisation is queued on the AudioContext clock up front
     ══════════════════════════════════════════════════════════════════════ */
  function openAudio() {
    try {
      var Ctor = window.AudioContext || window.webkitAudioContext;
      if (!Ctor) return null;
      var ctx = new Ctor();
      var bag = { ctx: ctx, ready: ctx.state !== 'suspended', pending: null };
      if (!bag.ready && ctx.resume) {
        ctx.resume().then(function() {
          bag.ready = true;
          if (bag.pending) { queueVocalizations(bag, bag.pending); bag.pending = null; }
        })['catch'](function() {});
      }
      return bag;
    } catch (e) {
      return null;
    }
  }

  // Schedule every vocalize beat at an absolute audio time. Doing this up front
  // rather than from each setTimeout means a janky TV cannot drift the sound
  // away from the mouths.
  function queueVocalizations(bag, plan) {
    if (!bag) return;
    if (!bag.ready) { bag.pending = plan; return; }
    var last = bag.ctx.currentTime;
    var elapsed = now() - pageStart;
    for (var i = 0; i < plan.length; i++) {
      var lead = Math.max(0, plan[i].t - elapsed) / 1000;
      var end = speak(bag.ctx, bag.ctx.currentTime + lead, plan[i].voice);
      if (end > last) last = end;
    }
    var ms = Math.ceil((last - bag.ctx.currentTime + 0.3) * 1000);
    setTimeout(function() { try { bag.ctx.close(); } catch (e) {} }, Math.max(0, ms));
  }

  /* ══════════════════════════════════════════════════════════════════════
     PLAYER
     ══════════════════════════════════════════════════════════════════════ */
  function playStory(story) {
    if (started) return;
    started = true;

    at(400, function() { if (overlay) overlay.classList.add('bg-reveal'); });
    dressStage(story);

    if (!stage || reducedMotion) return logoOnly();

    var sc = { actors: {}, props: {}, cells: {} };

    // Dress the set before anything else paints.
    buildCells(story, sc);

    // Props first so characters paint in front of them.
    var props = story.props || [];
    for (var p = 0; p < props.length; p++) {
      var pd = PROPS[props[p].prop];
      if (!pd) continue;
      var pe = pd.build();
      var lane = props[p].lane || 0;
      var depth = fitScale() * (1 - lane * 0.12);
      pe.style.bottom = (11 + lane * 8) + '%';
      setTransform(pe, 'scale(' + depth.toFixed(3) + ')');
      pe.style.zIndex = String(10 - lane);
      var ppx = (props[p].x || 0.5) * stageWidth() - (pd.w * depth) / 2;
      pe.style.left = Math.round(ppx) + 'px';
      stage.appendChild(pe);
      sc.props[props[p].id] = { el: pe, px: ppx };
    }

    var cast = story.cast || [];
    for (var i = 0; i < cast.length; i++) {
      var a = makeActor(cast[i]);
      if (!a) continue;
      sc.actors[a.id] = a;
      stage.appendChild(a.el);
    }

    // Queue all audio before the first beat fires.
    var beats = story.beats || [];
    var plan = [];
    for (var v = 0; v < beats.length; v++) {
      if (beats[v].action !== 'vocalize') continue;
      var actor = sc.actors[beats[v].actor];
      if (actor) plan.push({ t: beats[v].t, voice: actor.def.voice });
    }
    queueVocalizations(openAudio(), plan);

    // Title card, once the scene has begun.
    if (titleEl && story.title) {
      titleEl.textContent = story.title;   // never innerHTML: LLM-authored text
      at(600, function() { titleEl.classList.add('show'); });
      at(3800, function() { titleEl.classList.remove('show'); });
    }

    // Schedule the beats.
    var lastBeat = 0;
    for (var b = 0; b < beats.length; b++) {
      (function(beat) {
        var scene = SCENE_ACTIONS[beat.action];
        if (scene) {
          at(beat.t, function() { try { scene(beat, sc); } catch (e) {} });
          if (beat.t > lastBeat) lastBeat = beat.t;
          return;
        }
        var fn = ACTIONS[beat.action];
        var actor = sc.actors[beat.actor];
        if (!fn || !actor) return;
        // Normalise the wire format (Go marshals lowercase keys).
        var nb = { X: beat.x, Ms: beat.ms, Target: beat.target, From: beat.from };
        at(beat.t, function() {
          try { fn(actor, nb, sc); } catch (e) {}
        });
        if (beat.t > lastBeat) lastBeat = beat.t;
      })(beats[b]);
    }

    var storyEnd = Math.max(lastBeat + 800, story.durationMs || 9500);
    at(storyEnd, function() { if (logo) logo.classList.add('reveal'); });
    at(storyEnd + 700, function() { performanceDone = true; maybeDismiss(); });
  }

  // Dress the set before anyone walks on. An unknown backdrop is already
  // normalised server-side; the guard here is for the local fallback story.
  function dressStage(story) {
    if (!backdropEl) return;
    var name = (story.scene && story.scene.backdrop) || 'night';
    if (!BACKDROPS[name]) name = 'night';
    backdropEl.className = 'intro-backdrop backdrop--' + name;
    at(120, function() { backdropEl.classList.add('lit'); });
  }

  function logoOnly() {
    if (logo) logo.classList.add('reveal');
    var bag = openAudio();
    if (bag) queueVocalizations(bag, [{ t: 120, voice: VOICES.cat }]);
    at(900, function() { performanceDone = true; maybeDismiss(); });
  }

  // Local last resort: the classic single cat. Only used if the server has no
  // story for us — the splash must not depend on the request succeeding.
  function localStory() {
    return {
      title: 'Ina Arrives',
      durationMs: 3200,
      cast: [{ id: 'ina', character: 'cat', lane: 0, x: 0.44, scale: 1 }],
      beats: [
        { t: 0, actor: 'ina', action: 'enter', from: pick(['left', 'right']), ms: 1200 },
        { t: 1600, actor: 'ina', action: 'vocalize' },
        { t: 2300, actor: 'ina', action: 'stretch' },
        { t: 2750, actor: 'ina', action: 'blink' }
      ]
    };
  }

  /* ── Fetch the prepared story, but never wait long for it ──────────── */
  function begin() {
    var settled = false;
    function go(story) {
      if (settled) return;
      settled = true;
      playStory(story);
    }
    // Whatever happens, the show starts within the budget.
    setTimeout(function() { go(localStory()); }, FETCH_BUDGET_MS);

    try {
      var xhr = new XMLHttpRequest();
      xhr.open('GET', STORY_URL, true);
      xhr.onreadystatechange = function() {
        if (xhr.readyState !== 4 || settled) return;
        if (xhr.status >= 200 && xhr.status < 300) {
          try {
            var s = JSON.parse(xhr.responseText);
            if (s && s.cast && s.cast.length && s.beats && s.beats.length) return go(s);
          } catch (e) {}
        }
        go(localStory());
      };
      xhr.send();
    } catch (e) {
      go(localStory());
    }
  }

  /* ── Session end: ask the server to prepare the next story ─────────── */
  var beaconSent = false;
  function signalSessionEnd() {
    if (beaconSent) return;
    beaconSent = true;
    try {
      if (navigator.sendBeacon) {
        navigator.sendBeacon(SESSION_END_URL, '');
      } else {
        var xhr = new XMLHttpRequest();
        xhr.open('POST', SESSION_END_URL, true);
        xhr.send('');
      }
    } catch (e) {}
  }
  // pagehide is the reliable one; visibilitychange catches TV app backgrounding.
  window.addEventListener('pagehide', signalSessionEnd, false);
  document.addEventListener('visibilitychange', function() {
    if (document.visibilityState === 'hidden') signalSessionEnd();
  }, false);

  /* ── Dismissal ─────────────────────────────────────────────────────── */
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

  // Hand over only once the story has played AND the app is ready.
  function maybeDismiss() {
    if (dismissed || !performanceDone || loadsDone < 3) return;
    var elapsed = now() - pageStart;
    var remaining = Math.max(0, Math.min(MAX_INTRO_MS - elapsed, 350));
    setTimeout(dismissIntro, remaining);
  }

  window.__introMarkLoaded = function() {
    loadsDone++;
    maybeDismiss();
  };
  window.__introMarkFailed = function() { window.__introMarkLoaded(); };

  // Anyone who has seen it enough can skip; covers the TV remote, whose
  // OK/Back arrive as ordinary keydowns.
  document.addEventListener('keydown', dismissIntro, false);
  document.addEventListener('click', dismissIntro, false);

  // Never hold the app hostage to the animation.
  setTimeout(function() { if (!dismissed) dismissIntro(); }, MAX_INTRO_MS + 500);

  begin();
})();
