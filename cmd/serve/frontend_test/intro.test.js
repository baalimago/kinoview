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
// the gait class), never painted pixels. Motion itself stays unverified, as
// documented in the agent notebook — CSS keyframes are not executed here.

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
    offsetWidth: 640,
    appendChild: function(c) {
      this.children.push(c);
      if (!this.firstChild) this.firstChild = c;
      return c;
    },
    removeChild: function(c) {
      var i = this.children.indexOf(c);
      if (i >= 0) this.children.splice(i, 1);
      if (this.firstChild === c) this.firstChild = this.children[0] || null;
      return c;
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

var stageEl = makeEl();
var documentStub = {
  body: { classList: { contains: function() { return false; } } },
  // The player captures the stage element at load; hand it the same node the
  // assertions inspect.
  getElementById: function(id) { return id === 'intro-stage' ? stageEl : makeEl(); },
  createElement: function() { return makeEl(); },
  addEventListener: function() {}
};

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
// by the time the script returns.
function makeXHR(storyJSON) {
  return function() {
    return {
      open: function() {},
      send: function() { if (this.onreadystatechange) this.onreadystatechange(); },
      readyState: 4,
      status: 200,
      responseText: storyJSON,
      onreadystatechange: null
    };
  };
}

// ── Fixture: the bird visits, hops, and chirps ──────────────────────────
// Keys are the wire format (Go marshals lowercase). The walkTo at t=800 with
// ms=600 ends at t=1400, so a check at t≈1200 catches the bird mid-hop.
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

function findActor(cls) {
  for (var i = 0; i < stageEl.children.length; i++) {
    if (stageEl.children[i].className.indexOf(cls) !== -1) return stageEl.children[i];
  }
  return null;
}

var rec = { oscs: [] };
var windowStub = {
  innerWidth: 640,
  performance: { now: function() { return Date.now(); } },
  matchMedia: function() { return { matches: false }; },
  AudioContext: function() { return makeAudioContext(rec); },
  addEventListener: function() {}
};

var src = fs.readFileSync(INTRO, 'utf8');
var run = new Function(
  'window', 'document', 'navigator', 'XMLHttpRequest', 'setTimeout', 'clearTimeout',
  'performance', 'Math', 'Date', 'console',
  src
);

run(windowStub, documentStub, { sendBeacon: function() {} }, makeXHR(STORY),
  setTimeout, clearTimeout, { now: function() { return Date.now(); } },
  stubMath, Date, console);

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

// The beats fire on real timers; the walkTo ends at t=1400, so 1250ms is
// mid-hop and every assertion is settled.
setTimeout(function() {
  var bird = findActor('actor--bird');
  var cat = findActor('actor--cat');

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

  check('chirp schedules three notes with a rising middle', function() {
    var notes = [];
    for (var i = 0; i < rec.oscs.length; i++) {
      if (rec.oscs[i].type === 'sawtooth') notes.push(rec.oscs[i].freq[0]);
    }
    assert.strictEqual(notes.length, 3, 'sawtooth notes = ' + JSON.stringify(notes));
    assert.ok(notes[1] > notes[0] && notes[0] > notes[2],
      'want middle note above the first and the last below it, got ' + JSON.stringify(notes));
  });

  if (failures.length) {
    console.error('\n' + failures.length + ' assertion(s) failed');
    process.exit(1);
  }
  console.log('\nall intro player assertions passed');
  process.exit(0);
}, 1250);
