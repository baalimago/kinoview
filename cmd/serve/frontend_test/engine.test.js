// ── The Troupe Engine harness (Node only, not shipped) ───────────────────
//
// Drives engine.js headless in Node with a DOM/AudioContext stub, so the
// phase-2 subsystems — the bone-div rig, closed-form IK, keyframes +
// oscillation, the deterministic expander, selectors, clip/gag/play
// sequencing, tween resolution and the formant/effect audio — can be
// asserted at the JS seams. The repo has no JS test framework; this is a
// plain Node script:
//
//   node cmd/serve/frontend_test/engine.test.js
//
// The engine is ES5 for webOS; the harness stubs just enough of the DOM for
// mount/step to run end to end: real style objects, a recording AudioContext
// and a deterministic external clock (the engine runs in auto:false and the
// test steps the timeline explicitly). Assertions inspect DATA the engine
// sets (transforms, opacity, data-path attributes, scheduled oscillators),
// never painted pixels.
//
// Fixture resolved plays are hand-resolved in this phase (the phase-3
// resolver does not exist yet): the conformance story and a procedural
// garden under lab/fixtures/. Small subsystem plays are constructed inline.

'use strict';

var fs = require('fs');
var path = require('path');
var assert = require('assert');

var ENGINE = path.join(__dirname, '..', 'frontend', 'engine.js');
var FIXTURES = path.join(__dirname, '..', '..', '..', 'lab', 'fixtures');

// ── DOM stub ─────────────────────────────────────────────────────────────
// One element factory for every node the engine touches: a plain-object
// style (the engine writes transform/opacity/size/colour directly), children,
// parent links and data-* attributes.
function makeEl() {
  var el = {
    style: {},
    children: [],
    parentNode: null,
    attrs: {},
    clientWidth: 0,
    clientHeight: 0,
    setAttribute: function(k, v) { this.attrs[k] = String(v); },
    getAttribute: function(k) { return this.attrs[k] === undefined ? null : this.attrs[k]; },
    appendChild: function(c) {
      // A real DOM moves the node — remove it from its current parent first,
      // so re-parenting a bone under its parent's div never leaves a copy
      // behind in the container.
      if (c.parentNode) {
        var old = c.parentNode.children.indexOf(c);
        if (old >= 0) c.parentNode.children.splice(old, 1);
      }
      this.children.push(c); c.parentNode = this; return c;
    },
    removeChild: function(c) {
      var i = this.children.indexOf(c);
      if (i >= 0) this.children.splice(i, 1);
      c.parentNode = null;
      return c;
    }
  };
  return el;
}

function makeDocument() {
  return {
    readyState: 'complete',
    getElementById: function() { return null; },
    createElement: function() { return makeEl(); },
    addEventListener: function() {}
  };
}

// ── Recording AudioContext ───────────────────────────────────────────────
// Every node the synth creates is retained so the tests can assert the
// scheduled voice/sound structure and its determinism across runs.
function makeRecordingContext() {
  var rec = { oscs: [], sources: [], filters: [], gains: [], buffers: [] };
  function param() {
    return {
      value: 0,
      vals: [],
      setValueAtTime: function(v) { this.vals.push(v); },
      linearRampToValueAtTime: function(v) { this.vals.push(v); },
      exponentialRampToValueAtTime: function(v) { this.vals.push(v); }
    };
  }
  var ctx = {
    state: 'running',
    currentTime: 0,
    sampleRate: 44100,
    destination: {},
    createOscillator: function() {
      var o = { type: '', frequency: param(), connect: function() {}, start: function(t) { o.startT = t; }, stop: function(t) { o.stopT = t; } };
      rec.oscs.push(o);
      return o;
    },
    createGain: function() {
      var g = { gain: param(), connect: function() {} };
      rec.gains.push(g);
      return g;
    },
    createBiquadFilter: function() {
      var f = { type: '', Q: param(), frequency: param(), connect: function() {} };
      rec.filters.push(f);
      return f;
    },
    createBuffer: function(ch, len) {
      var b = { getChannelData: function() { return new Float32Array(len); } };
      rec.buffers.push(b);
      return b;
    },
    createBufferSource: function() {
      var s = { buffer: null, connect: function() {}, start: function(t) { s.startT = t; }, stop: function() {} };
      rec.sources.push(s);
      return s;
    },
    resume: function() { return { then: function(cb) { cb(); } }; },
    close: function() {}
  };
  return { ctx: ctx, rec: rec };
}

// A stable summary of the recorded audio — two runs must produce identical
// summaries (the play is deterministic).
function audioSummary(rec) {
  var out = [];
  for (var i = 0; i < rec.oscs.length; i++) {
    var o = rec.oscs[i];
    out.push('osc ' + o.type + ' @' + o.startT + ' f0=' + (o.frequency.vals[0] || 0));
  }
  for (var j = 0; j < rec.sources.length; j++) {
    out.push('src @' + rec.sources[j].startT + ' buf=' + (rec.sources[j].buffer ? 'y' : 'n'));
  }
  return out.join(';');
}

// ── Engine loading + mounting ────────────────────────────────────────────
function loadEngine() {
  var src = fs.readFileSync(ENGINE, 'utf8');
  var windowStub = {
    document: makeDocument(),
    performance: { now: function() { return 0; } },
    TROUPE_PLAY: undefined
  };
  var run = new Function('window', src);
  run(windowStub);
  return { engine: windowStub.TroupeEngine, window: windowStub };
}

// Mount a resolved play with a steppable clock. clockT is the test's notion
// of "now"; the engine reads it for audio lead computation.
function mount(engine, play, opts) {
  opts = opts || {};
  var mountEl = makeEl();
  mountEl.clientWidth = 640;
  mountEl.clientHeight = 360;
  var state = { t: 0 };
  var clock = function() { return state.t; };
  var audio = null;
  if (opts.audio) audio = opts.audio;
  var handle = engine.mount(mountEl, play, {
    size: { w: 640, h: 360 },
    auto: false,
    clock: clock,
    audio: audio
  });
  return { mountEl: mountEl, handle: handle, state: state };
}

function step(run, t) {
  run.state.t = t;
  run.handle.step(t);
}

// ── DOM query helpers ────────────────────────────────────────────────────
function walk(el, fn) {
  fn(el);
  for (var i = 0; i < el.children.length; i++) walk(el.children[i], fn);
}

function findAll(el, pred) {
  var out = [];
  walk(el, function(n) { if (pred(n)) out.push(n); });
  return out;
}

function findOne(el, pred) {
  var out = findAll(el, pred);
  assert.ok(out.length > 0, 'expected one matching element, found ' + out.length);
  return out[0];
}

function byPath(el, dataPath) {
  return findOne(el, function(n) { return n.getAttribute('data-path') === dataPath; });
}

function parseTransform(style) {
  var m = (style.transform || '').match(
    /translate\((-?[0-9.]+)px,(-?[0-9.]+)px\) rotate\((-?[0-9.]+)deg\) scale\((-?[0-9.]+),(-?[0-9.]+)\)/
  );
  if (!m) return { tx: 0, ty: 0, rot: 0, sx: 1, sy: 1 };
  return { tx: +m[1], ty: +m[2], rot: +m[3], sx: +m[4], sy: +m[5] };
}

// World point: compose the transform chain from the mount root down to the
// element, then map the local point (px) through it. The rig's DOM nesting
// is exactly the transform hierarchy, so this mirrors the engine's FK.
function worldPoint(mountEl, el, lx, ly) {
  var chain = [];
  var n = el;
  while (n && n !== mountEl) { chain.push(n); n = n.parentNode; }
  var a = 1, b = 0, c = 0, d = 1, e = 0, f = 0;
  for (var i = chain.length - 1; i >= 0; i--) {
    var t = parseTransform(chain[i].style);
    var r = t.rot * Math.PI / 180;
    var cos = Math.cos(r), sin = Math.sin(r);
    // M = M · T(tx,ty)·R·S(sx,sy)
    var nA = a * (t.sx * cos) + c * (t.sx * sin);
    var nB = b * (t.sx * cos) + d * (t.sx * sin);
    var nC = a * (-t.sy * sin) + c * (t.sy * cos);
    var nD = b * (-t.sy * sin) + d * (t.sy * cos);
    var nE = a * t.tx + c * t.ty + e;
    var nF = b * t.tx + d * t.ty + f;
    a = nA; b = nB; c = nC; d = nD; e = nE; f = nF;
  }
  return { x: a * lx + c * ly + e, y: b * lx + d * ly + f };
}

function serialize(el) {
  var tag = el.getAttribute('data-instance') || el.getAttribute('data-path') ||
    el.getAttribute('data-bone') || el.getAttribute('data-slot') || '';
  var s = tag + '|' + (el.style.transform || '') + '|' + (el.style.opacity || '') +
    '|' + (el.style.width || '') + '|' + (el.style.height || '');
  for (var i = 0; i < el.children.length; i++) s += '[' + serialize(el.children[i]) + ']';
  return s;
}

// Generated node containers: elements that carry a data-path and are not a
// bone or attachment div (those echo their node's path too).
function nodeEls(el, pathRe) {
  return findAll(el, function(n) {
    return pathRe.test(n.getAttribute('data-path') || '') &&
      !n.getAttribute('data-bone') && !n.getAttribute('data-slot');
  });
}

// World scale: the compounded scale at a point, extracted from the same
// transform chain worldPoint walks (the DOM nesting multiplies node scales).
function worldScale(mountEl, el) {
  var chain = [];
  var n = el;
  while (n && n !== mountEl) { chain.push(n); n = n.parentNode; }
  var a = 1, b = 0, c = 0, d = 1, e = 0, f = 0;
  for (var i = chain.length - 1; i >= 0; i--) {
    var t = parseTransform(chain[i].style);
    var r = t.rot * Math.PI / 180;
    var cos = Math.cos(r), sin = Math.sin(r);
    var nA = a * (t.sx * cos) + c * (t.sx * sin);
    var nB = b * (t.sx * cos) + d * (t.sx * sin);
    var nC = a * (-t.sy * sin) + c * (t.sy * cos);
    var nD = b * (-t.sy * sin) + d * (t.sy * cos);
    var nE = a * t.tx + c * t.ty + e;
    var nF = b * t.tx + d * t.ty + f;
    a = nA; b = nB; c = nC; d = nD; e = nE; f = nF;
  }
  return Math.sqrt(a * a + b * b);
}

function approx(actual, expected, eps, msg) {
  eps = eps === undefined ? 0.01 : eps;
  assert.ok(Math.abs(actual - expected) <= eps,
    (msg || 'approx') + ': expected ' + expected + ' ±' + eps + ', got ' + actual);
}

// ── Fixture resolved plays ───────────────────────────────────────────────
function readFixture(name) {
  return JSON.parse(fs.readFileSync(path.join(FIXTURES, name), 'utf8'));
}

// A one-instance play builder for the subsystem tests. The instance sits at
// stage (0.5, 0.5) with scale 1, so model-frame targets map 1:1 onto px via
// u = stageH/10 = 36 (plus the 320/180 stage offset).
function playWith(modelSpec, clipSpec, timeline, modelRef) {
  return {
    play: {
      kind: 'play', id: 'test_20260821T000000Z', status: 'submitted', author: 'test', provenance: 'test',
      spec: {
        instances: [{ id: 'a', model: modelRef || 'a@1', role: 'actor', scale: 1, x: 0.5, y: 0.5 }],
        timeline: timeline || [{ at: 0, on: 'a', clip: 'c@1' }]
      }
    },
    assets: {
      models: { 'a@1': { kind: 'model', id: 'a', version: 1, status: 'draft', author: 'test', provenance: 'test', spec: modelSpec } },
      voices: {},
      sounds: {},
      clips: { 'c@1': { kind: 'clip', id: 'c', version: 1, status: 'draft', author: 'test', provenance: 'test', spec: clipSpec } },
      gags: {}
    }
  };
}

function armModel() {
  return {
    bones: [
      { id: 'root', parent: null, x: 0, y: 0, rot: 0, length: 0 },
      { id: 'upper', parent: 'root', x: 0, y: 0, rot: 0, length: 3 },
      { id: 'hand', parent: 'upper', x: 0, y: 0, rot: 0, length: 2 }
    ]
  };
}

// ── Tests ────────────────────────────────────────────────────────────────
var tests = [];
function test(name, fn) { tests.push({ name: name, fn: fn }); }

test('mount builds the bone-div rig with rigid skinning', function() {
  var env = loadEngine();
  var run = mount(env.engine, readFixture('story_20260820T161500Z.resolved.json'));
  var mountEl = run.mountEl;
  var instances = findAll(mountEl, function(n) { return n.getAttribute('data-instance'); });
  assert.deepStrictEqual(instances.map(function(n) { return n.getAttribute('data-instance'); }).sort(),
    ['cat', 'dog', 'forest']);

  var cat = findOne(mountEl, function(n) { return n.getAttribute('data-instance') === 'cat'; });
  var catBones = findAll(cat, function(n) { return n.getAttribute('data-bone'); });
  assert.deepStrictEqual(catBones.map(function(n) { return n.getAttribute('data-bone'); }).sort(),
    ['backLeg', 'frontLeg', 'root', 'spine']);

  // The rig is the DOM nesting: spine under root, legs under spine. (findAll
  // includes the queried element itself, so the legs query excludes spine.)
  var root = findOne(cat, function(n) { return n.getAttribute('data-bone') === 'root'; });
  var spine = findOne(cat, function(n) { return n.getAttribute('data-bone') === 'spine'; });
  assert.strictEqual(spine.parentNode, root, 'spine must nest under root');
  var legs = findAll(spine, function(n) { return n.getAttribute('data-bone') && n !== spine; });
  assert.deepStrictEqual(legs.map(function(n) { return n.getAttribute('data-bone'); }).sort(),
    ['backLeg', 'frontLeg']);

  // Rigid skinning: the body attachment is a div child of the spine bone div.
  var body = findOne(cat, function(n) { return n.getAttribute('data-slot') === 'main'; });
  assert.strictEqual(body.parentNode, spine, 'the attachment must bind to exactly one bone');
  assert.strictEqual(body.style.background, '#c89b6a');
  assert.strictEqual(body.style.width, (3 * 36) + 'px');
  assert.strictEqual(body.style.borderRadius, '50%');
});

test('the deterministic expander scatters distinct, reproducible instances', function() {
  var env = loadEngine();
  var run = mount(env.engine, readFixture('garden_20260821T090000Z.resolved.json'));
  var trees = nodeEls(run.mountEl, /^garden\/tree#\d+$/);
  assert.strictEqual(trees.length, 3);
  for (var i = 0; i < 3; i++) byPath(run.mountEl, 'garden/tree#' + i);

  // Distinct: no two trees share a position.
  var positions = trees.map(function(n) { return parseTransform(n.style).tx + ',' + parseTransform(n.style).ty; });
  assert.strictEqual(new Set(positions).size, 3, 'scatter must not clone positions');

  // Reproducible: a second mount produces the identical layout.
  var run2 = mount(env.engine, readFixture('garden_20260821T090000Z.resolved.json'));
  assert.strictEqual(serialize(run.mountEl), serialize(run2.mountEl));
});

test('recurse honours depth/branch/decay and terminates', function() {
  var env = loadEngine();
  var run = mount(env.engine, readFixture('garden_20260821T090000Z.resolved.json'));
  // tree#0 attaches branch#0 (attach is singular); the branch recurses
  // depth 2, branch 2: 2 + 4 branch nodes, then 8 leaf tips.
  var branchCount = nodeEls(run.mountEl, /^garden\/tree#0\/branch#0\/(branch#\d+\/)*branch#\d+$/).length;
  assert.strictEqual(branchCount, 6, '2 level-1 + 4 level-2 branches under tree#0');

  var leafCount = nodeEls(run.mountEl, /^garden\/tree#0\/branch#0\/.*\/leaf#\d+$/).length;
  assert.strictEqual(leafCount, 8, '2^3 = 8 leaf tips under tree#0');

  // Scale compounds through the DOM nesting: a leaf is decay^3 of its tree
  // (the ratio removes the tree's own scatter-jitter scale).
  var leaf = byPath(run.mountEl, 'garden/tree#0/branch#0/branch#0/branch#0/leaf#0');
  var tree = byPath(run.mountEl, 'garden/tree#0');
  approx(worldScale(run.mountEl, leaf) / worldScale(run.mountEl, tree), Math.pow(0.7, 3), 0.001, 'leaf scale compounds decay over depth');
});

test('selectors animate generated content (model: and wildcard path)', function() {
  var env = loadEngine();
  var run = mount(env.engine, readFixture('garden_20260821T090000Z.resolved.json'));
  step(run, 250);
  // breeze@1: model:leaf@1 roots sway at 10·sin(2π·1·0.25) = 10°.
  var leaf = byPath(run.mountEl, 'garden/tree#0/branch#0/branch#0/branch#0/leaf#0');
  var leafRoot = findOne(leaf, function(n) { return n.getAttribute('data-bone') === 'root'; });
  approx(parseTransform(leafRoot.style).rot, 10, 0.01, 'model: selector leaf sway');

  // tree#* (wildcard path) targets every tree's root bone (the trunk).
  var tree = byPath(run.mountEl, 'garden/tree#2');
  var trunk = findOne(tree, function(n) { return n.getAttribute('data-bone') === 'trunk'; });
  approx(parseTransform(trunk.style).rot, 3 * Math.sin(Math.PI / 4), 0.01, 'wildcard tree sway');
});

test('reach lands the effector far end exactly on the target', function() {
  var env = loadEngine();
  var play = playWith(armModel(), {
    duration: 1000, loop: false,
    constraints: [{ type: 'reach', effector: 'hand', target: { x: 2, y: 4 }, hint: 'front' }]
  });
  var run = mount(env.engine, play);
  step(run, 100);
  var hand = findOne(run.mountEl, function(n) { return n.getAttribute('data-bone') === 'hand'; });
  var p = worldPoint(run.mountEl, hand, 0, 2 * 36); // far end, 2 units down its +Y
  approx(p.x, 320 + 2 * 36, 0.001, 'reach x');
  approx(p.y, 180 + 4 * 36, 0.001, 'reach y');
});

test('look faces the target and plant pins a joint to its coordinate', function() {
  var env = loadEngine();
  // look: a 2-unit neck points at (1, 0) → world rotation -90° (toward +X).
  var lookPlay = playWith({
    bones: [
      { id: 'root', parent: null, x: 0, y: 0, rot: 0, length: 0 },
      { id: 'neck', parent: 'root', x: 0, y: 0, rot: 0, length: 2 }
    ]
  }, {
    duration: 1000, loop: false,
    constraints: [{ type: 'look', chain: 'neck', target: { x: 1, y: 0 } }]
  });
  var lookRun = mount(env.engine, lookPlay);
  step(lookRun, 100);
  var neck = findOne(lookRun.mountEl, function(n) { return n.getAttribute('data-bone') === 'neck'; });
  approx(parseTransform(neck.style).rot, -90, 0.001, 'look chain faces +X');

  // plant: the foot's joint is pinned to (3, 0); the hip rotates to put it
  // there while the rest of the rig stays put.
  var plantPlay = playWith({
    bones: [
      { id: 'root', parent: null, x: 0, y: 0, rot: 0, length: 0 },
      { id: 'hip', parent: 'root', x: 0, y: 0, rot: 0, length: 3 },
      { id: 'foot', parent: 'hip', x: 0, y: 0, rot: 0, length: 1 }
    ]
  }, {
    duration: 1000, loop: false,
    constraints: [{ type: 'plant', bone: 'foot', at: { x: 3, y: 0 } }]
  });
  var plantRun = mount(env.engine, plantPlay);
  step(plantRun, 100);
  var foot = findOne(plantRun.mountEl, function(n) { return n.getAttribute('data-bone') === 'foot'; });
  var fp = worldPoint(plantRun.mountEl, foot, 0, 0);
  approx(fp.x, 320 + 3 * 36, 0.001, 'plant x');
  approx(fp.y, 180, 0.001, 'plant y');
});

test('track follows an animated bone', function() {
  var env = loadEngine();
  var play = playWith({
    bones: [
      { id: 'root', parent: null, x: 0, y: 0, rot: 0, length: 0 },
      { id: 'mover', parent: 'root', x: 0, y: 0, rot: 0, length: 2 },
      { id: 'target', parent: 'root', x: 0, y: 0, rot: 0, length: 1 }
    ]
  }, {
    duration: 2000, loop: false,
    keyframes: [
      { bone: 'target', channel: 'x', easing: 'linear', keys: [{ t: 0, v: 1 }, { t: 1000, v: 2 }] }
    ],
    constraints: [{ type: 'track', chain: 'mover', target: 'target' }]
  });
  var run = mount(env.engine, play);
  step(run, 500); // target joint at x = 1.5 → mover points at +X (rot -90°)
  var mover = findOne(run.mountEl, function(n) { return n.getAttribute('data-bone') === 'mover'; });
  approx(parseTransform(mover.style).rot, -90, 0.001, 'track chain faces the moving target');
});

test('keyframes and oscillation hit their channel values', function() {
  var env = loadEngine();
  var run = mount(env.engine, readFixture('story_20260820T161500Z.resolved.json'));
  var cat = findOne(run.mountEl, function(n) { return n.getAttribute('data-instance') === 'cat'; });
  var front = findOne(cat, function(n) { return n.getAttribute('data-bone') === 'frontLeg'; });
  var back = findOne(cat, function(n) { return n.getAttribute('data-bone') === 'backLeg'; });

  // walk@1 loops (1200 ms): frontLeg swings at 30·sin(2π·2·t/1000),
  // backLeg mirrors with phase 180°.
  step(run, 300);
  approx(parseTransform(front.style).rot, 30 * Math.sin(2 * Math.PI * 2 * 0.3), 0.001, 'frontLeg at 300');
  approx(parseTransform(back.style).rot, 30 * Math.sin(2 * Math.PI * 2 * 0.3 + Math.PI), 0.001, 'backLeg at 300');
  step(run, 1500); // 1500 % 1200 = 300: the loop wraps
  approx(parseTransform(front.style).rot, 30 * Math.sin(2 * Math.PI * 2 * 0.3), 0.001, 'frontLeg loops');
  step(run, 1800); // 1800 % 1200 = 600 (steps are monotonic, so 600 is not re-enterable)
  approx(parseTransform(front.style).rot, 30 * Math.sin(2 * Math.PI * 2 * 0.6), 0.001, 'frontLeg at 600');
});

test('play sequencing: concurrent same-at entries, gag order, tween resolution', function() {
  var env = loadEngine();
  var run = mount(env.engine, readFixture('story_20260820T161500Z.resolved.json'));
  var cat = findOne(run.mountEl, function(n) { return n.getAttribute('data-instance') === 'cat'; });

  // Same-at concurrency: walk (bones) and tween (root) both fire at 0.
  step(run, 1500);
  var instT = parseTransform(cat.style);
  approx(instT.tx, (0.1 + 0.4 * 0.5) * 640, 0.01, 'tween 1 mid (ease-in-out 0.5)');
  approx(instT.ty, 0, 0.01, 'cat stays on its stage row');
  var front = findOne(cat, function(n) { return n.getAttribute('data-bone') === 'frontLeg'; });
  approx(parseTransform(front.style).rot, 30 * Math.sin(2 * Math.PI * 2 * 0.3), 0.001, 'walk still swinging');

  // Tween 2 (beside dog, side left): dog is at x 0.9, so the target is
  // (0.75, 0); at f = 0.5 with ease-out the x is 0.5 + 0.25·0.75.
  step(run, 3450);
  approx(parseTransform(cat.style).tx, 0.6875 * 640, 0.01, 'beside tween mid');

  // The gag (doubletake) starts at 3900: blink (160 ms) then pounce.
  step(run, 3950);
  var body = findOne(cat, function(n) { return n.getAttribute('data-slot') === 'main'; });
  approx(parseFloat(body.style.opacity), 0.5, 0.001, 'blink opacity mid');

  step(run, 4160); // pounce local 100: look orients the spine at (1.5, 0)
  var spine = findOne(cat, function(n) { return n.getAttribute('data-bone') === 'spine'; });
  var spineJoint = { x: 0, y: 1 }; // root (0,0) + spine offset (0, 0+1)
  var want = Math.atan2(-(1.5 - spineJoint.x), 0 - spineJoint.y) * 180 / Math.PI;
  approx(parseTransform(spine.style).rot, want, 0.01, 'pounce look orients the spine');

  // After the gag the non-loop clips are gone: at 5000 the legs still swing
  // from walk@1 alone at local 200 (5000 % 1200).
  step(run, 5000);
  approx(parseTransform(front.style).rot, 30 * Math.sin(2 * Math.PI * 2 * 0.2), 0.001, 'walk resumed after gag');
});

test('off tween exits the stage', function() {
  var env = loadEngine();
  var run = mount(env.engine, readFixture('garden_20260821T090000Z.resolved.json'));
  var cat = findOne(run.mountEl, function(n) { return n.getAttribute('data-instance') === 'cat'; });
  step(run, 5400); // off right over 800 ms, ease-in: f 0.5 → 0.25
  approx(parseTransform(cat.style).tx, (0.5 + 1.0 * 0.25) * 640, 0.01, 'off tween mid');
  step(run, 5800);
  approx(parseTransform(cat.style).tx, 1.5 * 640, 0.01, 'off tween end off-stage');
});

test('clip events schedule the resolved voice and sound', function() {
  var env = loadEngine();
  var rec = makeRecordingContext();
  var run = mount(env.engine, readFixture('story_20260820T161500Z.resolved.json'), { audio: rec.ctx });
  step(run, 4000); // walk + tween at 0, tween 2 at 3000, gag at 3900
  step(run, 4060); // blink ends; pounce starts
  step(run, 4460); // pounce local 400 → voice: true (cat@1)
  var voiceOscs = rec.rec.oscs.filter(function(o) { return o.type === 'sawtooth'; });
  assert.ok(voiceOscs.length >= 1, 'the voice must schedule its sawtooth formant source');
  approx(voiceOscs[0].startT, 0, 0.001, 'voice scheduled at the event time');
  // The first scheduled frequency is f0·pitch[0]; divide the pitch curve out
  // so the underlying f0 draw is checked against the authored range.
  var catVoice = readFixture('story_20260820T161500Z.resolved.json').assets.voices['cat@1'].spec;
  var f0First = voiceOscs[0].frequency.vals[0] / catVoice.pitch[0];
  assert.ok(f0First >= 620 && f0First <= 770, 'cat voice f0 within its authored range');

  step(run, 4560); // pounce local 500 → sound: rustle@1 (noise)
  var noise = rec.rec.sources.filter(function(s) { return s.buffer; });
  assert.ok(noise.length >= 1, 'the rustle must schedule its noise buffer');
  approx(noise[noise.length - 1].startT, 0, 0.001, 'sound scheduled at the event time');

  // Determinism: a fresh run of the same play must schedule the identical
  // audio (same seeded draws, same event times).
  var rec2 = makeRecordingContext();
  var run2 = mount(env.engine, readFixture('story_20260820T161500Z.resolved.json'), { audio: rec2.ctx });
  step(run2, 4000);
  step(run2, 4060);
  step(run2, 4460);
  step(run2, 4560);
  assert.strictEqual(audioSummary(rec2.rec), audioSummary(rec.rec), 'audio must be deterministic');
});

test('the whole conformance play reproduces identically across mounts', function() {
  var env = loadEngine();
  var run1 = mount(env.engine, readFixture('story_20260820T161500Z.resolved.json'));
  var run2 = mount(env.engine, readFixture('story_20260820T161500Z.resolved.json'));
  step(run1, 0); step(run1, 1500); step(run1, 3450); step(run1, 3950); step(run1, 4160); step(run1, 4800);
  step(run2, 0); step(run2, 1500); step(run2, 3450); step(run2, 3950); step(run2, 4160); step(run2, 4800);
  assert.strictEqual(serialize(run1.mountEl), serialize(run2.mountEl));
});

test('the along region places instances along a bone', function() {
  var env = loadEngine();
  // The instance is a@1 (the model carrying the scatter); leaf@1 is supplied
  // below as the scattered model. Passing 'leaf@1' as the modelRef would make
  // the instance itself the leaf (no structure) and the scatter never run.
  var play = playWith({
    bones: [{ id: 'stem', parent: null, x: 0, y: 0, rot: 0, length: 5 }],
    structure: [{
      type: 'scatter', model: 'leaf@1', count: 3,
      over: { type: 'along', bone: 'stem' }, seed: 1
    }]
  }, { duration: 100, loop: false }, [{ at: 0, on: 'a', tween: { to: { x: 0.5 }, over: 10, easing: 'linear' } }]);
  // The play references leaf@1 from the scatter: supply it.
  play.assets.models['leaf@1'] = {
    kind: 'model', id: 'leaf', version: 1, status: 'draft', author: 'test', provenance: 'test',
    spec: {
      bones: [{ id: 'root', parent: null, x: 0, y: 0, rot: 0, length: 0 }],
      attachments: [{ id: 'blade', slot: 'main', bone: 'root', shape: { type: 'ellipse', w: 1, h: 1, color: '#3a7d44' } }],
      skins: { default: { main: 'blade' } }
    }
  };
  var run = mount(env.engine, play);
  step(run, 5);
  var leaves = nodeEls(run.mountEl, /^a\/leaf#\d+$/);
  assert.strictEqual(leaves.length, 3);
  approx(parseTransform(leaves[0].style).ty, 0, 0.001, 'along start');
  approx(parseTransform(leaves[1].style).ty, 2.5 * 36, 0.001, 'along mid');
  approx(parseTransform(leaves[2].style).ty, 5 * 36, 0.001, 'along end');
});

test('destroy removes the stage', function() {
  var env = loadEngine();
  var run = mount(env.engine, readFixture('story_20260820T161500Z.resolved.json'));
  run.handle.destroy();
  assert.strictEqual(run.mountEl.children.length, 0, 'destroy must remove the stage');
});

test('a second mount replaces the stage instead of stacking', function() {
  // One stage per element: a re-mount on the same element (the bootstrap
  // self-mount racing the fetch-driven mount in index.js, or a re-mount
  // after a new generation) must destroy the previous stage — its rAF loop
  // stopped and its DOM removed — never stack a second one. Separate
  // elements keep concurrent stages.
  var env = loadEngine();
  var el = makeEl();
  el.clientWidth = 640;
  el.clientHeight = 360;
  var state = { t: 0 };
  var opts = { size: { w: 640, h: 360 }, auto: false, clock: function() { return state.t; } };
  var play = readFixture('story_20260820T161500Z.resolved.json');
  var run1 = env.engine.mount(el, play, opts);
  assert.strictEqual(el.children.length, 1, 'first mount renders one stage');
  var run2 = env.engine.mount(el, play, opts);
  assert.strictEqual(el.children.length, 1, 'a second mount must replace, never stack');
  assert.strictEqual(el.children[0].getAttribute('data-stage'), 'troupe', 'the surviving stage is the new one');
  // The replaced handle is destroyed: stepping it must not resurrect a node.
  state.t = 100;
  run1.step(state.t);
  assert.strictEqual(el.children.length, 1, 'the replaced stage must not come back');
  // The live handle still drives the surviving stage.
  var before = el.children[0].children.length;
  state.t = 200;
  run2.step(state.t);
  assert.strictEqual(el.children[0].children.length, before, 'the live stage keeps rendering');
  // A different element keeps its own concurrent stage.
  var el2 = makeEl();
  el2.clientWidth = 640;
  el2.clientHeight = 360;
  env.engine.mount(el2, play, opts);
  assert.strictEqual(el.children.length, 1, 'mounting another element must not touch this stage');
  assert.strictEqual(el2.children.length, 1, 'the other element hosts its own stage');
});

test('bootstrap self-mounts a resolved play into #troupe', function() {
  // Phase 9 cutover: the engine bootstraps when TROUPE_PLAY is present and
  // #troupe exists — the production frontend sets TROUPE_PLAY from
  // /api/v1/troupe/play/resolved and the engine renders without any other
  // wiring. An absent play leaves the stage empty (no seed, no fallback).
  var troupeEl = makeEl();
  troupeEl.clientWidth = 640;
  troupeEl.clientHeight = 360;
  var src = fs.readFileSync(ENGINE, 'utf8');
  var windowStub = {
    document: {
      readyState: 'complete',
      getElementById: function(id) { return id === 'troupe' ? troupeEl : null; },
      createElement: function() { return makeEl(); },
      addEventListener: function() {}
    },
    performance: { now: function() { return 0; } },
    // The auto loop's rAF never fires, so the mount stays inert and the
    // process can exit.
    requestAnimationFrame: function() { return 0; },
    cancelAnimationFrame: function() {},
    TROUPE_PLAY: readFixture('story_20260820T161500Z.resolved.json')
  };
  new Function('window', src)(windowStub);
  assert.ok(windowStub.TroupeEngine, 'engine must expose TroupeEngine');
  var stage = findOne(troupeEl, function(n) { return n.getAttribute('data-stage') === 'troupe'; });
  assert.ok(stage, 'bootstrap must mount the stage');
  var instances = findAll(stage, function(n) { return n.getAttribute('data-instance'); });
  assert.deepStrictEqual(instances.map(function(n) { return n.getAttribute('data-instance'); }).sort(),
    ['cat', 'dog', 'forest']);
});

test('bootstrap leaves the stage empty without a play', function() {
  var troupeEl = makeEl();
  troupeEl.clientWidth = 640;
  troupeEl.clientHeight = 360;
  var src = fs.readFileSync(ENGINE, 'utf8');
  var windowStub = {
    document: {
      readyState: 'complete',
      getElementById: function(id) { return id === 'troupe' ? troupeEl : null; },
      createElement: function() { return makeEl(); },
      addEventListener: function() {}
    },
    performance: { now: function() { return 0; } },
    TROUPE_PLAY: undefined
  };
  new Function('window', src)(windowStub);
  assert.strictEqual(troupeEl.children.length, 0,
    'no play means no play — the empty stage renders nothing');
});

// ── Runner ───────────────────────────────────────────────────────────────
var failures = 0;
for (var i = 0; i < tests.length; i++) {
  try {
    tests[i].fn();
    console.log('ok - ' + tests[i].name);
  } catch (e) {
    failures++;
    console.error('FAIL - ' + tests[i].name);
    console.error('  ' + (e && e.message));
  }
}
console.log('\n' + (tests.length - failures) + '/' + tests.length + ' engine tests passed');
process.exit(failures ? 1 : 0);
