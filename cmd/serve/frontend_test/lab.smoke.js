// Smoke: boot lab.js against a minimal browser stub and drive a few frames.
// Verifies the lab wiring (fixture load → mount → clock → play/pause) without
// a real browser. Dev-only, run by hand:
//   node cmd/serve/frontend_test/lab.smoke.js
'use strict';
var fs = require('fs');
var path = require('path');
var vm = require('vm');
var assert = require('assert');

var ROOT = path.join(__dirname, '..', '..', '..');
var ENGINE = path.join(ROOT, 'cmd', 'serve', 'frontend', 'engine.js');
var LAB = path.join(ROOT, 'lab', 'lab.js');
var FIXTURE = path.join(ROOT, 'lab', 'fixtures', 'story_20260820T161500Z.resolved.json');

function makeEl() {
  var el = {
    style: {}, children: [], parentNode: null, attrs: {},
    clientWidth: 640, clientHeight: 360,
    handlers: {},
    setAttribute: function(k, v) { this.attrs[k] = String(v); },
    getAttribute: function(k) { return this.attrs[k] === undefined ? null : this.attrs[k]; },
    appendChild: function(c) {
      if (c.parentNode) {
        var i = c.parentNode.children.indexOf(c);
        if (i >= 0) c.parentNode.children.splice(i, 1);
      }
      this.children.push(c); c.parentNode = this; return c;
    },
    removeChild: function(c) {
      var i = this.children.indexOf(c);
      if (i >= 0) this.children.splice(i, 1);
      c.parentNode = null;
      return c;
    },
    addEventListener: function(type, fn) {
      (this.handlers[type] = this.handlers[type] || []).push(fn);
    },
    fire: function(type) {
      var hs = this.handlers[type] || [];
      for (var i = 0; i < hs.length; i++) hs[i].call(this);
    }
  };
  return el;
}

var stageEl = makeEl();
var fixtureSel = makeEl(); fixtureSel.value = '';
var playBtn = makeEl(); playBtn.disabled = true; playBtn.textContent = '';
var scrub = makeEl(); scrub.min = '0'; scrub.max = '0'; scrub.value = '0'; scrub.disabled = true;
var timeLabel = makeEl(); timeLabel.textContent = '';
var status = makeEl(); status.textContent = '';

function byId(id) {
  if (id === 'troupe') return stageEl;
  if (id === 'fixture') return fixtureSel;
  if (id === 'play') return playBtn;
  if (id === 'scrub') return scrub;
  if (id === 'time') return timeLabel;
  if (id === 'status') return status;
  return null;
}

var rafQueue = [];
var ctx = {
  console: console,
  setTimeout: setTimeout,
  document: {
    getElementById: byId,
    createElement: function() { return makeEl(); },
    addEventListener: function() {}
  },
  requestAnimationFrame: function(fn) { rafQueue.push(fn); return rafQueue.length; },
  cancelAnimationFrame: function() {},
  AudioContext: null,
  XMLHttpRequest: function() {
    return {
      open: function() {},
      send: function() {
        var self = this;
        setTimeout(function() {
          self.status = 200;
          self.responseText = fs.readFileSync(FIXTURE, 'utf8');
          self.onload();
        }, 0);
      }
    };
  },
  addEventListener: function() {}
};
ctx.window = ctx;
vm.createContext(ctx);

vm.runInContext(fs.readFileSync(ENGINE, 'utf8'), ctx);
assert.ok(ctx.TroupeEngine, 'engine must expose TroupeEngine');
vm.runInContext(fs.readFileSync(LAB, 'utf8'), ctx);

setTimeout(function() {
  try {
    assert.strictEqual(playBtn.disabled, false, 'play must enable after load');
    assert.ok(stageEl.children.length > 0, 'the stage must render the play');
    var scrubMax = parseInt(scrub.max, 10);
    assert.ok(scrubMax > 0, 'scrub must know the play duration, got ' + scrubMax);

    // Play: run the rAF queue a few frames, then pause.
    playBtn.fire('click');
    assert.strictEqual(playBtn.textContent, 'Pause', 'play toggles to pause');
    var ts = 1000;
    for (var i = 0; i < 6; i++) {
      ts += 40;
      var frames = rafQueue.splice(0);
      frames.forEach(function(fn) { fn(ts); });
    }
    var tMid = parseInt(timeLabel.textContent, 10);
    assert.ok(tMid > 0, 'clock must advance, got ' + tMid);

    playBtn.fire('click');
    assert.strictEqual(playBtn.textContent, 'Play', 'pause toggles back');
    console.log('lab smoke: ok (stage rendered, clock advanced to ' + tMid + ' ms, max ' + scrubMax + ' ms)');
  } catch (e) {
    console.error('FAIL - ' + e.message);
    process.exit(1);
  }
}, 80);
