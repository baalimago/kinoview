// ── The splash harness (Node only, not shipped) ───────────────────────────
//
// Drives troupe.js — the fullscreen splash and its blocking audience control —
// headless: a DOM stub, a recording XHR, a resolved play endpoint and a fake
// engine mount. The repo has no JS test framework; this is a plain Node
// script:
//
//   node cmd/serve/frontend_test/splash.test.js
//
// The exits are safety-critical: while the splash is live the gallery beneath
// it is unreachable, so a regression in the thumb or the skip button traps the
// audience behind a black wall.

'use strict';

var path = require('path');
var assert = require('assert');

var SPLASH = path.join(__dirname, '..', 'frontend', 'troupe.js');
var PLAY_ID = 'story_20260820T161500Z';

// makeEl is the one element factory: a class list, children, parent links and
// recorded listeners, so a click can be fired like the browser would.
function makeEl(tag) {
  var el = {
    tagName: tag,
    className: '',
    textContent: '',
    placeholder: '',
    value: '',
    type: '',
    title: '',
    maxLength: 0,
    children: [],
    parentNode: null,
    listeners: {},
    classList: {
      // className is the storage: troupe.js sets some classes directly and
      // adds others through the list, and both must read back the same.
      add: function(name) {
        var parts = el.className ? el.className.split(/\s+/) : [];
        if (parts.indexOf(name) < 0) parts.push(name);
        el.className = parts.join(' ').trim();
      },
      contains: function(name) {
        return el.className.split(/\s+/).indexOf(name) >= 0;
      }
    },
    appendChild: function(child) { child.parentNode = el; el.children.push(child); return child; },
    removeChild: function(child) {
      var at = el.children.indexOf(child);
      if (at >= 0) el.children.splice(at, 1);
      child.parentNode = null;
    },
    addEventListener: function(type, fn) { (el.listeners[type] = el.listeners[type] || []).push(fn); },
    fire: function(type) {
      var event = { stopPropagation: function() {}, preventDefault: function() {} };
      (el.listeners[type] || []).forEach(function(fn) { fn(event); });
    }
  };
  return el;
}

// boot loads troupe.js against fresh globals and returns the handles the
// assertions need: the stage, the notes posted, the mounted engine call and
// the document listeners.
function boot(options) {
  options = options || {};
  var stage = makeEl('div');
  var sent = [];
  var mount = null;
  var documentListeners = {};

  global.document = {
    getElementById: function(id) { return id === 'troupe' ? stage : null; },
    createElement: makeEl,
    addEventListener: function(type, fn) {
      (documentListeners[type] = documentListeners[type] || []).push(fn);
    }
  };
  global.window = global;
  global.fetch = function() {
    if (options.emptyStage) {
      return Promise.resolve({ ok: false, status: 404, json: function() { return {}; } });
    }
    var play = { play: { id: PLAY_ID }, assets: {} };
    return Promise.resolve({ ok: true, status: 200, json: function() { return play; } });
  };
  global.XMLHttpRequest = function() {
    this.open = function() {};
    this.setRequestHeader = function() {};
    this.send = function(body) { sent.push(JSON.parse(body)); };
  };
  global.TroupeEngine = {
    mount: function(el, play, opts) {
      mount = { el: el, play: play, opts: opts, destroyed: false };
      return {
        destroy: function() { mount.destroyed = true; },
        time: function() { return 1234; }
      };
    }
  };

  delete require.cache[SPLASH];
  require(SPLASH);
  return {
    stage: stage,
    sent: sent,
    mounted: function() { return mount; },
    documentListeners: documentListeners
  };
}

// flush lets the resolved-play promise chain (mountPlay) run.
function flush() {
  return new Promise(function(resolve) { setTimeout(resolve, 0); });
}

// controlIn finds the first direct child of the stage whose class list
// contains the name.
function controlIn(stage, name) {
  for (var i = 0; i < stage.children.length; i++) {
    if (stage.children[i].classList.contains(name)) return stage.children[i];
  }
  return null;
}

// controlByClass finds the first descendant of root whose class list contains
// the name — the feedback control's fields and thumbs.
function controlByClass(root, name) {
  for (var i = 0; i < root.children.length; i++) {
    if (root.children[i].classList.contains(name)) return root.children[i];
    var nested = controlByClass(root.children[i], name);
    if (nested) return nested;
  }
  return null;
}

async function testEmptyStageStaysHidden() {
  var splash = boot({ emptyStage: true });
  await flush();
  assert.strictEqual(splash.stage.classList.contains('live'), false,
    'an empty stage must not paint the splash');
  assert.strictEqual(splash.mounted(), null, 'no play, no mount');
}

async function testSkipDuringPlayRecordsDismissal() {
  var splash = boot();
  await flush();
  assert.strictEqual(splash.stage.classList.contains('live'), true, 'the play mounts live');

  var skip = controlIn(splash.stage, 'troupe-skip');
  assert.ok(skip, 'the splash carries a skip button');
  skip.fire('click');

  assert.deepStrictEqual(splash.sent, [{ playId: PLAY_ID, type: 'dismissal', data: { atMs: 1234 } }],
    'skipping a running play records where it stopped');
  assert.strictEqual(splash.mounted().destroyed, true, 'the engine is destroyed on skip');
  assert.strictEqual(splash.stage.classList.contains('dismiss'), true, 'the splash fades out');
}

async function testFeedbackSendsKeepAndChange() {
  var splash = boot();
  await flush();
  splash.mounted().opts.onEnd(9600);
  assert.deepStrictEqual(splash.sent, [{ playId: PLAY_ID, type: 'completion', data: { durationMs: 9600 } }],
    'the completion note is posted when the play ends');

  var control = controlIn(splash.stage, 'troupe-feedback');
  assert.ok(control, 'the blocking control appears after the play');
  controlByClass(control, 'keep').value = ' the cat walking away , , the bird size ';
  controlByClass(control, 'change').value = 'the pier';
  controlByClass(control, 'troupe-feedback-note').value = 'keep it';
  controlByClass(control, 'up').fire('click');

  var rating = splash.sent[splash.sent.length - 1];
  assert.strictEqual(rating.type, 'rating');
  assert.strictEqual(rating.data.rating, 1);
  assert.strictEqual(rating.data.comment, 'keep it');
  assert.deepStrictEqual(rating.data.keep, ['the cat walking away', 'the bird size'],
    'the keep list is trimmed and blank entries are dropped');
  assert.deepStrictEqual(rating.data.change, ['the pier']);
  assert.strictEqual(splash.stage.classList.contains('dismiss'), true, 'rating leaves the splash');
}

async function testFeedbackIsBlockingUntilSkip() {
  var splash = boot();
  await flush();
  splash.mounted().opts.onEnd(9600);
  var before = splash.sent.length;

  splash.documentListeners.click.forEach(function(fn) { fn({}); });
  assert.strictEqual(splash.stage.classList.contains('dismiss'), false,
    'a stray click must not dismiss the live feedback control');
  assert.strictEqual(splash.sent.length, before, 'a blocked click posts nothing');

  controlIn(splash.stage, 'troupe-skip').fire('click');
  assert.strictEqual(splash.stage.classList.contains('dismiss'), true,
    'the skip button leaves the feedback control');
  assert.strictEqual(splash.sent.length, before, 'skipping the feedback posts no note');
}

async function testDirectionIsCapped() {
  var splash = boot();
  await flush();
  splash.mounted().opts.onEnd(9600);
  var control = controlIn(splash.stage, 'troupe-feedback');
  var many = [];
  for (var i = 0; i < 9; i++) many.push('element ' + i);
  controlByClass(control, 'keep').value = many.join(',');
  controlByClass(control, 'down').fire('click');

  var rating = splash.sent[splash.sent.length - 1];
  assert.strictEqual(rating.data.rating, -1);
  assert.strictEqual(rating.data.keep.length, 5, 'the client caps the list like the note contract');
}

(async function run() {
  await testEmptyStageStaysHidden();
  await testSkipDuringPlayRecordsDismissal();
  await testFeedbackSendsKeepAndChange();
  await testFeedbackIsBlockingUntilSkip();
  await testDirectionIsCapped();
  console.log('splash.test.js: all assertions passed');
  process.exit(0);
})().catch(function(err) {
  console.error(err);
  process.exit(1);
});