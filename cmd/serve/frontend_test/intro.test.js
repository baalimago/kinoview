// ── Intro player harness (Node only, not shipped) ────────────────────────
//
// Runs the intro splash player headless in Node with a DOM/AudioContext stub,
// so the phase-8 bird's geometry (perch height, species scale) and its chirp
// scheduling can be asserted at the JS seams the browser would otherwise only
// show on a TV. The repo has no JS test framework; this is a plain Node
// script — run it with:
//
//   node cmd/serve/frontend_test/intro.test.js
//
// The player is ES5 for webOS; the harness stubs just enough of the DOM for
// playStory to run end to end: real timers fire the beats, the recording
// AudioContext captures the chirp schedule, and the stage element's children
// carry the actors' geometry.
//
// Why these assertions are safe headless: they inspect DATA the player sets
// (style.bottom, the scale transform, the scheduled oscillator frequencies,
// the gait class, the staged class), never painted pixels. Motion itself stays
// unverified, as documented in the agent notebook — CSS keyframes are not
// executed here.
//
// Phase 1: the harness grew from one fixture to three. Each fixture gets its
// own stage/DOM/AudioContext via runStory, because playStory is a singleton —
// the player refuses a second story once started. The guard fixture
// reproduces the production shape (actors with beats but no `enter` beat) and
// asserts the player stages them at their cast marks; the exit-first fixture
// covers an actor whose only beat is `exit`.
//
// Phase 4: the audience feedback control. The harness records the document's
// click/keydown listeners, bubbles synthetic events through the element tree
// (the control's delegated stopPropagation is what keeps a thumb tap or a
// focus click from reaching the document's dismiss listener), records the
// feedback POSTs, and records every timer delay plus the timers themselves,
// so a test can prove the control's blocking behaviour — no dismissal path
// removes the overlay while a note is unsent — and that the block adds no
// timers to the splash schedule.

'use strict';

var fs = require('fs');
var path = require('path');
var assert = require('assert');

var INTRO = path.join(__dirname, '..', 'frontend', 'intro.js');

// A fixed random stream makes the chirp deterministic: rand(2, 4) with 0.5
// lands on 3, so the bird's bursts resolve to exactly three notes.
var stubMath = Object.create(Math);
stubMath.random = function() { return 0.5; };

// ── DOM stub ─────────────────────────────────────────────────────────────
// One element factory for every node the player touches: a classList, a
// plain-object style (setProperty included), children, and the few read-only
// fields playStory needs (offsetWidth, firstChild, querySelector).
function makeEl() {
  var classes = [];
  var el = {
    className: '',
    style: {
      setProperty: function(name, value) { this[name] = value; }
    },
    children: [],
    firstChild: null,
    parentNode: null,
    offsetWidth: 640,
    listeners: {},
    appendChild: function(c) {
      this.children.push(c);
      c.parentNode = this;
      if (!this.firstChild) this.firstChild = c;
      return c;
    },
    removeChild: function(c) {
      var i = this.children.indexOf(c);
      if (i >= 0) this.children.splice(i, 1);
      if (this.firstChild === c) this.firstChild = this.children[0] || null;
      return c;
    },
    addEventListener: function(type, fn) {
      (this.listeners[type] = this.listeners[type] || []).push(fn);
    },
    classList: {
      add: function(c) { if (classes.indexOf(c) < 0) classes.push(c); },
      remove: function(c) { var i = classes.indexOf(c); if (i >= 0) classes.splice(i, 1); },
      contains: function(c) { return classes.indexOf(c) >= 0; }
    },
    querySelector: function() { return null; },
    getElementsByClassName: function() { return []; }
  };
  return el;
}

// ── Recording AudioContext ───────────────────────────────────────────────
// Every createOscillator returns an object whose frequency parameter records
// its first value (the note's start), and the object itself is retained so
// the test can separate the sawtooth notes from the sine/pure layers.
function makeAudioContext(rec) {
  function param(recordInto) {
    var p = {
      value: 0,
      setValueAtTime: function(v) { if (recordInto) recordInto.push(v); },
      linearRampToValueAtTime: function(v) { if (recordInto) recordInto.push(v); },
      exponentialRampToValueAtTime: function(v) { if (recordInto) recordInto.push(v); }
    };
    return p;
  }
  function osc() {
    var o = { type: '', freq: [], connect: function() {}, start: function() {}, stop: function() {} };
    o.frequency = param(o.freq);
    rec.oscs.push(o);
    return o;
  }
  return {
    state: 'running',
    currentTime: 0,
    sampleRate: 44100,
    destination: {},
    createOscillator: osc,
    createGain: function() { return { gain: param(), connect: function() {} }; },
    createBiquadFilter: function() {
      return { type: '', Q: param(), frequency: param(), connect: function() {} };
    },
    createBuffer: function() { return { getChannelData: function() { return new Float32Array(0); } }; },
    createBufferSource: function() {
      return { buffer: null, connect: function() {}, start: function() {}, stop: function() {} };
    },
    resume: function() { return { then: function(cb) { cb(); } }; },
    close: function() {}
  };
}

// The XHR hands the fixture story over synchronously, so playStory has run
// by the time the script returns. POSTs (the feedback control) are recorded
// in `posts`; with failPosts a POST throws instead — a dead network, which
// the player must swallow silently.
function makeXHR(storyJSON, posts, failPosts) {
  return function() {
    var xhr = {
      readyState: 4,
      status: 200,
      responseText: storyJSON,
      onreadystatechange: null,
      method: '',
      url: '',
      open: function(method, url) { xhr.method = method; xhr.url = url; },
      setRequestHeader: function() {},
      send: function(body) {
        if (xhr.method === 'POST' && posts) {
          if (failPosts) throw new Error('network down');
          posts.push({ url: xhr.url, body: body });
        }
        if (xhr.onreadystatechange) xhr.onreadystatechange();
      }
    };
    return xhr;
  };
}

// The server is unreachable: the story fetch fails, so the player must fall
// back to its local story.
function makeFailingXHR() {
  return function() {
    return {
      readyState: 4,
      status: 500,
      responseText: '',
      onreadystatechange: null,
      open: function() {},
      setRequestHeader: function() {},
      send: function() { if (this.onreadystatechange) this.onreadystatechange(); }
    };
  };
}

// The story arrives LATE — after the old 320 ms fallback budget would have
// fired. The player must wait for it and never play the placeholder instead.
// The test calls deliver() when the story finally arrives.
function makeLateXHR(storyJSON, ref) {
  return function() {
    var xhr = {
      readyState: 0,
      status: 200,
      responseText: storyJSON,
      onreadystatechange: null,
      open: function() {},
      setRequestHeader: function() {},
      send: function() {},
      deliver: function() {
        xhr.readyState = 4;
        if (xhr.onreadystatechange) xhr.onreadystatechange();
      }
    };
    ref.xhr = xhr;
    return xhr;
  };
}

// ── Harness: one fresh stage + player per fixture ────────────────────────
// playStory is a singleton, so every fixture must run against its own
// document/window stubs. The story lands synchronously through the XHR stub,
// so by the time runStory returns the whole build path — cast loop, staging,
// beat scheduling — has executed and no timer has fired yet.
//
// opts: lowPerf (body carries the low-perf class), reducedMotion (matchMedia
// agrees to reduce), failFetch (the story request fails → local fallback),
// failPosts (the feedback POST throws → must be swallowed silently),
// lateStory (the story arrives after the fallback budget — the player must
// wait for it), logoPending (the logo image is still loading at the reveal).
function runStory(storyJSON, opts) {
  opts = opts || {};
  var stageEl = makeEl();
  var overlayEl = makeEl();
  var bodyEl = makeEl();
  if (opts.lowPerf) bodyEl.classList.add('low-perf');
  bodyEl.appendChild(overlayEl);

  // The logo is a real <img> in the page; the reveal waits for its load.
  // Loaded by default so the fixtures that do not care about the logo see
  // the reveal exactly when they always did.
  var logoEl = makeEl();
  logoEl.complete = true;
  logoEl.naturalWidth = 640;
  if (opts.logoPending) { logoEl.complete = false; logoEl.naturalWidth = 0; }
  overlayEl.querySelector = function(sel) {
    return sel === '.intro-logo' ? logoEl : null;
  };

  var docListeners = {};
  var posts = [];
  var timerDelays = [];
  var timerCalls = [];
  var documentStub = {
    body: bodyEl,
    getElementById: function(id) {
      if (id === 'intro-stage') return stageEl;
      if (id === 'intro-overlay') return overlayEl;
      return makeEl();
    },
    createElement: function() { return makeEl(); },
    addEventListener: function(type, fn) {
      (docListeners[type] = docListeners[type] || []).push(fn);
    }
  };

  var rec = { oscs: [] };
  var windowStub = {
    innerWidth: 640,
    performance: { now: function() { return Date.now(); } },
    matchMedia: function() { return { matches: !!opts.reducedMotion }; },
    AudioContext: function() { return makeAudioContext(rec); },
    addEventListener: function() {}
  };

  var src = fs.readFileSync(INTRO, 'utf8');
  var run = new Function(
    'window', 'document', 'navigator', 'XMLHttpRequest', 'setTimeout', 'clearTimeout',
    'performance', 'Math', 'Date', 'console',
    src
  );

  // Record every timer delay so a test can prove the splash schedule is
  // undisturbed: the hard cap must stay the longest timer. The calls are
  // kept so a test can fire a specific timer (the hard cap, the logo
  // backstop) on demand instead of waiting out real seconds.
  function wrapSetTimeout(fn, delay) {
    timerDelays.push(delay);
    var id = setTimeout(fn, delay);
    timerCalls.push({ fn: fn, delay: delay, id: id });
    return id;
  }
  function wrapClearTimeout(id) { return clearTimeout(id); }

  var lateRef = {};
  var xhrFactory = opts.failFetch
    ? makeFailingXHR()
    : opts.lateStory
      ? makeLateXHR(storyJSON, lateRef)
      : makeXHR(storyJSON, posts, opts.failPosts);

  run(windowStub, documentStub, { sendBeacon: function() {} }, xhrFactory,
    wrapSetTimeout, wrapClearTimeout, { now: function() { return Date.now(); } },
    stubMath, Date, console);

  return {
    stageEl: stageEl,
    overlay: overlayEl,
    body: bodyEl,
    logo: logoEl,
    posts: posts,
    timerDelays: timerDelays,
    // The document's click listener (dismissIntro) is registered separately
    // from the element tree; this invokes it the way a browser would when a
    // click bubbles all the way to the document.
    docClick: function(ev) {
      var ls = docListeners.click || [];
      for (var i = 0; i < ls.length; i++) ls[i](ev);
    },
    // Invoke the most recently registered timer with this delay — the hard
    // cap, the logo backstop, the story backstop — without waiting it out.
    fireTimer: function(delay) {
      for (var i = timerCalls.length - 1; i >= 0; i--) {
        if (timerCalls[i].delay === delay) { timerCalls[i].fn(); return true; }
      }
      return false;
    },
    // Hand the late story over, as the network finally would.
    deliverStory: function() {
      if (lateRef.xhr) lateRef.xhr.deliver();
    },
    // Complete the app's three data loads; __introMarkLoaded is the player's
    // own hook, so windowStub has it after run().
    markLoaded: function(n) {
      var i, total = n || 3;
      for (i = 0; i < total; i++) windowStub.__introMarkLoaded();
    },
    rec: rec,
    findActor: function(cls) {
      for (var i = 0; i < stageEl.children.length; i++) {
        if (stageEl.children[i].className.indexOf(cls) !== -1) return stageEl.children[i];
      }
      return null;
    },
    findActors: function(cls) {
      var out = [];
      for (var i = 0; i < stageEl.children.length; i++) {
        if (stageEl.children[i].className.indexOf(cls) !== -1) out.push(stageEl.children[i]);
      }
      return out;
    }
  };
}

// First child of `el` whose class carries `cls`.
function findByClass(el, cls) {
  for (var i = 0; i < el.children.length; i++) {
    if (el.children[i].className.indexOf(cls) !== -1) return el.children[i];
  }
  return null;
}

function findControl(run) {
  return findByClass(run.overlay, 'intro-feedback');
}

// Simulate a browser event bubbling from `target` up through the control
// `root`. The control's delegated listeners stop the bubble; the returned
// event records whether they did — a stopped event never reaches the
// document's dismiss listeners.
function fireEvent(type, target, root) {
  var ev = { target: target, stopped: false };
  ev.stopPropagation = function() { ev.stopped = true; };
  var node = target;
  while (node && !ev.stopped) {
    var ls = node.listeners && node.listeners[type];
    if (ls) {
      for (var i = 0; i < ls.length && !ev.stopped; i++) ls[i](ev);
    }
    node = node.parentNode;
  }
  return ev;
}

// ── Fixture 1: the bird visits, hops, and chirps ─────────────────────────
// Keys are the wire format (Go marshals lowercase). The walkTo at t=800 with
// ms=600 ends at t=1400, so a check at t≈1200 catches the bird mid-hop. The
// cat (ina) is a zero-beat cast member — the phase-1 guard: it must stay
// hidden, never forced on stage.
var STORY = JSON.stringify({
  title: 'Pip Calls By',
  durationMs: 2500,
  scene: { backdrop: 'night' },
  cast: [
    { id: 'pip', character: 'bird', coat: 'chaffinch', lane: 0, x: 0.3, scale: 1 },
    { id: 'ina', character: 'cat', lane: 0, x: 0.7, scale: 1 }
  ],
  beats: [
    { t: 0, actor: 'pip', action: 'enter', from: 'left', ms: 300 },
    { t: 400, actor: 'pip', action: 'vocalize' },
    { t: 800, actor: 'pip', action: 'walkTo', x: 0.6, ms: 600 }
  ]
});

// ── Fixture 2: the production shape that broke the splash ────────────────
// The guards (freija, ina) only sit, stareoff, nap and blink — never `enter`.
// Pre-fix the player hid them off-stage for the whole show. Post-fix they are
// anchored at their cast marks the moment the build runs. Expected lefts,
// derived from markPx: stage 640px; cat depth = base * fitScale * (1 - lane
// * 0.12) → freija (lane 1) 0.88, half 70.4, x=0.15 → 25.6 → '26px'; ina
// (lane 2) 0.76, half 60.8, x=0.85 → 483.2 → '483px'.
var GUARDS = JSON.stringify({
  title: 'The Night Watch',
  durationMs: 2500,
  scene: { backdrop: 'night' },
  cast: [
    { id: 'mouse1', character: 'mouse', coat: 'field', lane: 0, x: 0.5, scale: 1 },
    { id: 'freija', character: 'cat', coat: 'char', lane: 1, x: 0.15, scale: 1 },
    { id: 'ina', character: 'cat', coat: 'grey', lane: 2, x: 0.85, scale: 1 }
  ],
  beats: [
    { t: 0, actor: 'mouse1', action: 'enter', from: 'left', ms: 300 },
    { t: 400, actor: 'freija', action: 'sit', ms: 800 },
    { t: 500, actor: 'ina', action: 'nap', ms: 1000 },
    { t: 900, actor: 'freija', action: 'stareoff', ms: 600 },
    { t: 1300, actor: 'ina', action: 'blink' },
    { t: 1600, actor: 'mouse1', action: 'vocalize' }
  ]
});

// ── Fixture 3: an actor whose only beat is `exit` ────────────────────────
// Never entered, but it has a beat — the player must stage it at its cast
// mark so the exit is visible, then take it off again. Bird depth 0.30, half
// 9.6, x=0.5 → markPx 310.4 → '310px'; exit to the right lands at
// 640 + (64+60)*0.30 = 677.2 → '677px'. The dragon is an unknown character:
// makeActor must return null and the staging pass must skip it, no panic.
var EXIT_FIRST = JSON.stringify({
  title: 'Walk-On, Walk-Off',
  durationMs: 2000,
  scene: { backdrop: 'theatre' },
  cast: [
    { id: 'ina', character: 'cat', coat: 'tuxedo', lane: 0, x: 0.4, scale: 1 },
    { id: 'pip', character: 'bird', coat: 'chaffinch', lane: 0, x: 0.5, scale: 1 },
    { id: 'drake', character: 'dragon', lane: 0, x: 0.5, scale: 1 }
  ],
  beats: [
    { t: 0, actor: 'ina', action: 'enter', from: 'left', ms: 300 },
    { t: 100, actor: 'drake', action: 'sit', ms: 200 },
    { t: 300, actor: 'pip', action: 'exit', from: 'right', ms: 400 }
  ]
});

// ── Fixture 4: prototype-name cast ids ───────────────────────────────────
// A cast id may be validator-legal yet collide with Object.prototype
// (constructor, hasOwnProperty, ...). Pre-fix (review 3, R3-01) the staging
// pass used plain object maps: id `constructor` resolved the inherited
// member and was silently left hidden, and id `hasOwnProperty` shadowed the
// guard method, so the build threw and the show silently never played.
// Post-fix both are staged at their cast marks and the story still plays.
// Cat depth (lane 0) = 1.0, half 80px; x=0.25 → markPx 80 → '80px'; x=0.75
// → 400 → '400px'. Pip enters via its own beat, proving beats still
// schedule after the staging pass.
var PROTOTYPE = JSON.stringify({
  title: 'Name Collisions',
  durationMs: 2000,
  scene: { backdrop: 'theatre' },
  cast: [
    { id: 'pip', character: 'bird', coat: 'chaffinch', lane: 0, x: 0.5, scale: 1 },
    { id: 'constructor', character: 'cat', coat: 'grey', lane: 0, x: 0.25, scale: 1 },
    { id: 'hasOwnProperty', character: 'cat', coat: 'tuxedo', lane: 0, x: 0.75, scale: 1 }
  ],
  beats: [
    { t: 0, actor: 'pip', action: 'enter', from: 'left', ms: 300 },
    { t: 400, actor: 'constructor', action: 'sit', ms: 800 },
    { t: 500, actor: 'hasOwnProperty', action: 'nap', ms: 1000 }
  ]
});

// ── Fixture 5: the audience feedback control ────────────────────────────
// A real story (has an id, matching the server's story id pattern) with a
// short duration: one enter beat at t=0 gives lastBeat 0, so storyEnd =
// max(0 + 800, 600) = 800ms — the control appears at 800ms and the
// assertions settle just after.
var FEEDBACK = JSON.stringify({
  id: 'stry_abc12345',
  title: 'A Note for the Director',
  durationMs: 600,
  scene: { backdrop: 'night' },
  cast: [
    { id: 'pip', character: 'bird', coat: 'chaffinch', lane: 0, x: 0.5, scale: 1 }
  ],
  beats: [
    { t: 0, actor: 'pip', action: 'enter', from: 'left', ms: 300 }
  ]
});

var failures = [];

function check(name, fn) {
  try {
    fn();
    console.log('ok   - ' + name);
  } catch (e) {
    failures.push(name + ': ' + e.message);
    console.error('FAIL - ' + name + ': ' + e.message);
  }
}

// Twelve fixtures, each finishing on its own timer (the reduced-motion
// fixture finishes synchronously); the last one reports.
var pending = 12;
function finish() {
  pending--;
  if (pending > 0) return;
  if (failures.length) {
    console.error('\n' + failures.length + ' assertion(s) failed');
    process.exit(1);
  }
  console.log('\nall intro player assertions passed');
  process.exit(0);
}

// ── Fixture 1 assertions ─────────────────────────────────────────────────
var birdRun = runStory(STORY);

// The beats fire on real timers; the walkTo ends at t=1400, so 1250ms is
// mid-hop and every assertion is settled.
setTimeout(function() {
  var bird = birdRun.findActor('actor--bird');
  var cat = birdRun.findActor('actor--cat');

  check('bird actor built and staged', function() {
    assert.ok(bird, 'no actor--bird element on stage');
  });

  check('bird renders at perch height (lane 0 = 37%)', function() {
    assert.strictEqual(bird.style.bottom, '37%', 'bird bottom = ' + bird.style.bottom);
  });

  check('bird scale follows its species base (0.30 < mouse 0.44)', function() {
    assert.ok(bird.style.webkitTransform.indexOf('scale(0.300)') !== -1,
      'bird transform = ' + bird.style.webkitTransform);
  });

  check('cat keeps the ground line and its own base (1.00)', function() {
    assert.strictEqual(cat.style.bottom, '11%', 'cat bottom = ' + cat.style.bottom);
    assert.ok(cat.style.webkitTransform.indexOf('scale(1.000)') !== -1,
      'cat transform = ' + cat.style.webkitTransform);
  });

  check('bird walkTo is a hop, never a leg-walk', function() {
    assert.ok(bird.classList.contains('hopping'), 'bird should be hopping mid-walk');
    assert.ok(!bird.classList.contains('walking'), 'bird must never get the walking class');
  });

  check('zero-beat cast member stays hidden', function() {
    assert.ok(cat, 'no actor--cat element on stage');
    assert.ok(!cat.classList.contains('staged'),
      'a cast member with no beats must not be forced on stage');
  });

  check('chirp schedules three notes with a rising middle', function() {
    var notes = [];
    for (var i = 0; i < birdRun.rec.oscs.length; i++) {
      if (birdRun.rec.oscs[i].type === 'sawtooth') notes.push(birdRun.rec.oscs[i].freq[0]);
    }
    assert.strictEqual(notes.length, 3, 'sawtooth notes = ' + JSON.stringify(notes));
    assert.ok(notes[1] > notes[0] && notes[0] > notes[2],
      'want middle note above the first and the last below it, got ' + JSON.stringify(notes));
  });

  finish();
}, 1250);

// ── Fixture 2 assertions ─────────────────────────────────────────────────
var guardsRun = runStory(GUARDS);

// The story landed synchronously: the build ran, so the guards are already
// staged — no beat timer has fired yet (the t=0 mouse enter included).
check('never-entered guards are staged at their cast marks before any beat fires', function() {
  var cats = guardsRun.findActors('actor--cat');
  assert.strictEqual(cats.length, 2, 'want the two guard cats on stage, got ' + cats.length);
  var lefts = [];
  for (var i = 0; i < cats.length; i++) {
    assert.ok(cats[i].classList.contains('staged'),
      'guard ' + i + ' must carry the staged class');
    lefts.push(cats[i].style.left);
  }
  lefts.sort();
  assert.deepStrictEqual(lefts, ['26px', '483px'],
    'guards must sit at their cast marks, got ' + JSON.stringify(lefts));
});

// Mid-show: the mouse entered via its own beat and the guards are still on
// stage, doing their beatwork (freija sits at t=400, ina naps at t=500).
setTimeout(function() {
  var mouse = guardsRun.findActor('actor--mouse');
  var cats = guardsRun.findActors('actor--cat');

  check('entering actor still enters via its own beat', function() {
    assert.ok(mouse, 'no actor--mouse element on stage');
    assert.ok(mouse.classList.contains('staged'), 'mouse must be staged by its enter beat');
  });

  check('guards stay staged through their beats', function() {
    assert.strictEqual(cats.length, 2, 'want the two guard cats, got ' + cats.length);
    for (var i = 0; i < cats.length; i++) {
      assert.ok(cats[i].classList.contains('staged'),
        'guard ' + i + ' must stay staged while it sits and naps');
    }
  });

  finish();
}, 600);

// ── Fixture 3 assertions ─────────────────────────────────────────────────
var exitRun = runStory(EXIT_FIRST);

// The exit-first bird is staged at its cast mark from the build; its exit
// fires at t=300 and completes at t=700, so 100ms catches it visible and
// 900ms catches it gone.
setTimeout(function() {
  var pip = exitRun.findActor('actor--bird');

  check('exit-first actor is staged at its cast mark', function() {
    assert.ok(pip, 'no actor--bird element on stage');
    assert.ok(pip.classList.contains('staged'),
      'an actor whose first beat is exit must still be staged');
    assert.strictEqual(pip.style.left, '310px',
      'exit-first actor must stand at its cast mark, left = ' + pip.style.left);
  });
}, 100);

setTimeout(function() {
  var pip = exitRun.findActor('actor--bird');

  check('exit-first actor leaves the stage after its exit', function() {
    assert.ok(pip, 'no actor--bird element on stage');
    assert.ok(!pip.classList.contains('staged'),
      'the staged class must be removed once the exit beat completes');
    assert.strictEqual(pip.style.left, '677px',
      'exit must glide the actor off-stage, left = ' + pip.style.left);
  });

  check('unknown cast member is skipped, never staged', function() {
    assert.strictEqual(exitRun.findActor('actor--dragon'), null,
      'makeActor must return null for an unknown character — no element, no crash');
  });

  finish();
}, 900);

// ── Fixture 4 assertions: prototype-name cast ids ───────────────────────
var protoRun = runStory(PROTOTYPE);

// The story landed synchronously: the build ran. If the staging pass threw
// (pre-fix), nothing on stage carries the staged class — the show silently
// never plays.
check('prototype-name cast ids are staged at their cast marks without throwing', function() {
  var cats = protoRun.findActors('actor--cat');
  assert.strictEqual(cats.length, 2,
    'want the two prototype-name cats, got ' + cats.length);
  var lefts = [];
  for (var i = 0; i < cats.length; i++) {
    assert.ok(cats[i].classList.contains('staged'),
      'prototype-name cat ' + i + ' must carry the staged class');
    lefts.push(cats[i].style.left);
  }
  lefts.sort();
  assert.deepStrictEqual(lefts, ['400px', '80px'],
    'prototype-name cats must sit at their cast marks, got ' + JSON.stringify(lefts));
});

// Mid-show: the story is still alive — pip entered via its own beat, so
// beats were scheduled after the staging pass, and the prototype-name actors
// stayed staged through their beats.
setTimeout(function() {
  var pip = protoRun.findActor('actor--bird');
  var cats = protoRun.findActors('actor--cat');

  check('the story still plays with prototype-name cast ids', function() {
    assert.ok(pip, 'no actor--bird element on stage');
    assert.ok(pip.classList.contains('staged'),
      'pip must be staged by its enter beat — beats must still be scheduled');
    assert.strictEqual(cats.length, 2, 'want the two prototype-name cats, got ' + cats.length);
    for (var i = 0; i < cats.length; i++) {
      assert.ok(cats[i].classList.contains('staged'),
        'prototype-name cat ' + i + ' must stay staged through its beat');
    }
  });

  finish();
}, 700);

// ── Fixture 5 assertions: the feedback control ──────────────────────────
var upRun = runStory(FEEDBACK);

// The hard cap is scheduled at load, before any story timer; the blocking
// control must not add timers to the splash schedule.
check('the blocking control adds no timers; the hard cap stays the longest', function() {
  var maxDelay = Math.max.apply(null, upRun.timerDelays);
  assert.strictEqual(maxDelay, 13500,
    'the hard cap (MAX_INTRO_MS + 500) must stay the longest timer, got ' +
    JSON.stringify(upRun.timerDelays));
});

setTimeout(function() {
  var control = findControl(upRun);

  check('feedback control appears at the logo reveal for a real story', function() {
    assert.ok(control, 'no .intro-feedback inside the overlay');
    assert.ok(findByClass(control, 'intro-feedback-note'),
      'control must carry the note input');
    assert.ok(findByClass(control, 'up'), 'control must carry the thumbs-up button');
    assert.ok(findByClass(control, 'down'), 'control must carry the thumbs-down button');
  });

  var note = control ? findByClass(control, 'intro-feedback-note') : null;
  var up = control ? findByClass(control, 'up') : null;

  check('clicking into the note does not dismiss the intro', function() {
    var ev = fireEvent('click', note, control);
    assert.ok(ev.stopped, 'the control must stop the click before the document sees it');
    assert.ok(!upRun.overlay.classList.contains('dismiss'), 'overlay must not dismiss');
    assert.strictEqual(upRun.posts.length, 0, 'focusing the note must not submit');
  });

  check('typing in the note does not dismiss the intro', function() {
    var ev = fireEvent('keydown', note, control);
    assert.ok(ev.stopped, 'the control must stop keydowns before the document sees them');
    assert.ok(!upRun.overlay.classList.contains('dismiss'), 'overlay must not dismiss');
  });

  check('thumbs up posts the note verbatim and hides the control', function() {
    note.value = 'more dog';
    var ev = fireEvent('click', up, control);
    assert.ok(ev.stopped, 'a thumb tap must not reach the document dismiss listener');
    assert.strictEqual(upRun.posts.length, 1,
      'want exactly one POST, got ' + upRun.posts.length);
    assert.strictEqual(upRun.posts[0].url, '/gallery/intro/feedback',
      'post url = ' + upRun.posts[0].url);
    assert.deepStrictEqual(JSON.parse(upRun.posts[0].body),
      { storyId: 'stry_abc12345', rating: 1, comment: 'more dog' },
      'body = ' + upRun.posts[0].body);
    assert.ok(control.classList.contains('sent'), 'control must hide after submit');
    assert.ok(!upRun.overlay.classList.contains('dismiss'), 'the tap itself must not dismiss');
  });

  finish();
}, 850);

// ── Fixture 5 assertions: thumbs down, and the low-perf path ─────────────
// The same story on a low-perf body: the control still appears (low-perf
// only trims animation, never the build), and the down thumb posts -1.
var downRun = runStory(FEEDBACK, { lowPerf: true });

setTimeout(function() {
  var control = findControl(downRun);

  check('feedback control still appears under low-perf', function() {
    assert.ok(control, 'low-perf must not suppress the control');
  });

  var note = control ? findByClass(control, 'intro-feedback-note') : null;
  var down = control ? findByClass(control, 'down') : null;
  if (note) note.value = '';

  check('thumbs down posts rating -1 with an empty comment', function() {
    var ev = fireEvent('click', down, control);
    assert.ok(ev.stopped, 'a thumb tap must not reach the document dismiss listener');
    assert.strictEqual(downRun.posts.length, 1,
      'want exactly one POST, got ' + downRun.posts.length);
    assert.deepStrictEqual(JSON.parse(downRun.posts[0].body),
      { storyId: 'stry_abc12345', rating: -1, comment: '' },
      'body = ' + downRun.posts[0].body);
  });

  finish();
}, 850);

// ── Fixture 6 assertions: the ignored control BLOCKS dismissal ─────────
// No tap, no keystroke: while the control is live the overlay must not
// dismiss — not by the schedule, not by the hard cap, not by an outside
// click. The only exit is a thumb; once tapped, the handover completes.
var ignoredRun = runStory(FEEDBACK);

setTimeout(function() {
  var control = findControl(ignoredRun);

  check('an ignored control still sits in the overlay at the reveal', function() {
    assert.ok(control, 'no .intro-feedback inside the overlay');
  });

  // Outside click is blocked while the control is live.
  ignoredRun.docClick({ target: ignoredRun.stageEl });
  check('outside click cannot dismiss while the control is live', function() {
    assert.ok(!ignoredRun.overlay.classList.contains('dismiss'),
      'the overlay must stay while a note is unsent');
    assert.strictEqual(ignoredRun.posts.length, 0,
      'ignoring the control must not submit');
  });

  // The hard cap is blocked too: its dismissal attempt is deferred.
  ignoredRun.fireTimer(13500);
  check('the hard cap cannot dismiss while the control is live', function() {
    assert.ok(!ignoredRun.overlay.classList.contains('dismiss'),
      'the hard cap must not take the overlay away mid-feedback');
  });

  // A thumb is the exit: it posts and releases the block.
  var down = control ? findByClass(control, 'down') : null;
  fireEvent('click', down, control);
  check('a thumb releases the block and posts', function() {
    assert.ok(control.classList.contains('sent'), 'control must hide after submit');
    assert.strictEqual(ignoredRun.posts.length, 1,
      'want exactly one POST, got ' + ignoredRun.posts.length);
  });
}, 850);

setTimeout(function() {
  // performanceDone landed at storyEnd+700 (1500 ms); once the app data is
  // in and the block is released, the handover completes on schedule.
  ignoredRun.markLoaded(3);
}, 1600);

setTimeout(function() {
  check('the overlay hands over once the block is released', function() {
    assert.ok(ignoredRun.overlay.classList.contains('dismiss'),
      'the overlay must dismiss after the audience has spoken');
  });
  finish();
}, 2150);

// ── Fixture 10: the story arrives late — the fallback must not win ──────
// A slow TV can take longer than the old 320 ms budget to fetch the story.
// The player must wait for the real production, never play the placeholder
// in its stead. deliverStory() lands the response after the old budget.
var lateRun = runStory(FEEDBACK, { lateStory: true });

check('the player does not start a placeholder while the story loads', function() {
  assert.strictEqual(lateRun.stageEl.children.length, 0,
    'the stage must stay empty until the real story arrives');
});

setTimeout(function() {
  check('the fallback never wins the fetch race', function() {
    assert.strictEqual(lateRun.stageEl.children.length, 0,
      'past the old 320 ms budget, the local fallback must not have played');
  });

  lateRun.deliverStory();

  check('the real story plays once it arrives', function() {
    assert.ok(lateRun.findActor('actor--bird'),
      'the prepared story must play, not the placeholder cat');
  });

  finish();
}, 500);

// ── Fixture 11: a still-loading logo delays the reveal ──────────────────
// The reveal must never paint an empty frame: when the image is still on
// the wire at storyEnd, the reveal waits for the load event, so a slow TV
// gets the full logo.
var logoRun = runStory(FEEDBACK, { logoPending: true });

setTimeout(function() {
  check('a still-loading logo delays the reveal', function() {
    assert.ok(!logoRun.logo.classList.contains('reveal'),
      'the logo must not reveal while the image is still loading');
  });

  fireEvent('load', logoRun.logo, logoRun.logo);

  check('the logo reveals once the image loads', function() {
    assert.ok(logoRun.logo.classList.contains('reveal'),
      'the load event must trigger the reveal');
  });

  finish();
}, 850);

// ── Fixture 12: the logo backstop — a dead image never blocks forever ──
// The backstop is LOGO_BACKSTOP_MS after the reveal moment (storyEnd 800 +
// 1500 = 2300), not after page start.
var logoDeadRun = runStory(FEEDBACK, { logoPending: true });

setTimeout(function() {
  check('the logo backstop reveals even if the image never loads', function() {
    assert.ok(logoDeadRun.logo.classList.contains('reveal'),
      'the backstop must reveal the logo after LOGO_BACKSTOP_MS');
  });
  finish();
}, 2400);

// ── Fixture 7 assertions: the local fallback story ───────────────────────
// The server is down (500), so the player falls back to the local cat,
// which has no id — the control must never appear for it. The fallback
// story ends at 3550ms; the assertion waits just past that.
var fallbackRun = runStory(null, { failFetch: true });

setTimeout(function() {
  check('no feedback control for the local fallback story', function() {
    assert.strictEqual(findControl(fallbackRun), null,
      'the id-less fallback must never build the control');
  });
  finish();
}, 3650);

// ── Fixture 8 assertions: reduced motion ─────────────────────────────────
// The player takes the logoOnly path: no story, no storyEnd, no reveal
// moment — and no feedback control.
var reducedRun = runStory(FEEDBACK, { reducedMotion: true });

check('no feedback control under reduced motion', function() {
  assert.strictEqual(findControl(reducedRun), null,
    'logoOnly must never build the control');
});
finish();

// ── Fixture 9 assertions: the POST fails ─────────────────────────────────
// A dead network must be silent: the control hides, the splash plays on,
// and nothing escapes the player's try/catch.
var failRun = runStory(FEEDBACK, { failPosts: true });

setTimeout(function() {
  var control = findControl(failRun);
  var up = control ? findByClass(control, 'up') : null;

  check('a failed feedback POST is silent and the control still hides', function() {
    var ev = fireEvent('click', up, control);
    assert.ok(ev.stopped, 'a thumb tap must not reach the document dismiss listener');
    assert.strictEqual(failRun.posts.length, 0,
      'the throwing POST must not be recorded');
    assert.ok(control.classList.contains('sent'),
      'control must hide even when the POST fails');
    assert.ok(!failRun.overlay.classList.contains('dismiss'),
      'the splash must be unaffected by a failed POST');
  });

  finish();
}, 850);
