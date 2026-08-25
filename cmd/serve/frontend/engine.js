// ── The Troupe Engine ────────────────────────────────────────────────────
//
// The fixed, human-owned interpreter of the frozen grammar (see
// internal/agents/troupe/STAGE.md). It consumes the Phase 1 resolved-play
// format — { "play": …, "assets": { models, voices, sounds, clips, gags } } —
// and renders it into a stage element (#troupe) as a 2D skeletal keyframe
// animation built from CSS divs: no canvas, no WebGL, no framework.
//
// ES5 on purpose (var/function, no arrow fns, no template literals): the
// baseline target is webOS TV 4.x, i.e. Chromium 53. Same reason the engine
// animates only transform/opacity and writes -webkit- prefixed transforms.
//
// Determinism: every scatter/recurse carries a seed (or inherits the play
// seed); all randomness — placement jitter, audio parameter picks — flows
// from seeded PRNGs, so a resolved play reproduces identically across runs.
//
// Frames: the model frame is CSS-natural (+Y down, clockwise-positive
// rotation). A bone is a joint plus a segment along +Y; a bone's children
// mount at the parent's far end plus their own (x, y) offset, so a bone with
// x:0,y:0 sits exactly at the parent's end. The stage frame is also
// CSS-natural: an instance's x/y are fractions of the stage width/height
// measured from the top-left corner; one model unit is stageH/10 px.
;(function(global) {
  'use strict';

  var DEG = Math.PI / 180;
  var PI2 = Math.PI * 2;

  function clamp(v, lo, hi) { return v < lo ? lo : (v > hi ? hi : v); }
  function lerp(a, b, f) { return a + (b - a) * f; }

  // The shared easing enum (grammar: keyframes and tweens).
  function ease(e, t) {
    if (e === 'ease-in') return t * t;
    if (e === 'ease-out') return t * (2 - t);
    if (e === 'ease-in-out') return t < 0.5 ? 2 * t * t : -1 + (4 - 2 * t) * t;
    return t; // linear
  }

  // Direction angle of a vector, measured from +Y clockwise (CSS-natural).
  function angleOf(dx, dy) { return Math.atan2(-dx, dy) / DEG; }
  // Rotate a point by deg using the CSS rotation matrix (+Y down).
  function rotX(x, y, deg) { var r = deg * DEG, c = Math.cos(r), s = Math.sin(r); return x * c - y * s; }
  function rotY(x, y, deg) { var r = deg * DEG, c = Math.cos(r), s = Math.sin(r); return x * s + y * c; }

  // ── Seeded PRNG ─────────────────────────────────────────────────────────
  // mulberry32. Every random draw in the engine flows from one of these.
  function makeRng(seed) {
    var a = seed >>> 0;
    return function() {
      a = (a + 0x6D2B79F5) >>> 0;
      var t = a;
      t = Math.imul(t ^ (t >>> 15), t | 1);
      t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
      return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
  }

  // Per-instance seed derivation: (parent seed, instance index) — N instances
  // are N unique but reproducible, never N clones.
  function childSeed(parentSeed, index) {
    var x = (parentSeed + Math.imul(index + 1, 0x9E3779B9)) >>> 0;
    x = Math.imul(x ^ (x >>> 16), 0x85EBCA6B) >>> 0;
    x = Math.imul(x ^ (x >>> 13), 0xC2B2AE35) >>> 0;
    return (x ^ (x >>> 16)) >>> 0;
  }

  function rRange(pair, rng) { return pair[0] + (pair[1] - pair[0]) * rng(); }

  // ── Tiny DOM helpers (transform/opacity only, per the webOS floor) ──────
  function setTransform(el, value) {
    el.style.webkitTransform = value;
    el.style.transform = value;
  }

  // ── Parsing ─────────────────────────────────────────────────────────────
  // The resolved play's envelopes carry their kind-specific content under
  // `spec` as inline JSON (the Go side preserves it as json.RawMessage), so
  // `spec` arrives as an object; a string is tolerated defensively.
  function parseSpec(env) {
    var s = env.spec;
    return (typeof s === 'string') ? JSON.parse(s) : s;
  }

  function buildBones(spec) {
    var bones = {};
    var list = spec.bones || [];
    for (var i = 0; i < list.length; i++) {
      var b = list[i];
      var bone = {
        id: b.id,
        parent: b.parent || null,
        x: b.x || 0,
        y: b.y || 0,
        rot: b.rot || 0,
        scale: b.scale === undefined ? 1 : b.scale,
        len: b.length || 0,
        children: [],
        ch: null
      };
      bones[bone.id] = bone;
    }
    for (var id in bones) {
      if (bones.hasOwnProperty(id)) {
        var p = bones[id].parent;
        if (p && bones[p]) bones[p].children.push(id);
      }
    }
    return bones;
  }

  // FK: joint positions and world rotations of every bone, in the skeleton's
  // root frame, from the current channel values (ch). A child's joint is the
  // parent's far end plus the child's (x, y) offset, rotated by the parent.
  function computeFK(bones) {
    var out = {};
    function walk(id, pjx, pjy, prot, plen) {
      var b = bones[id];
      var jx = pjx + rotX(b.ch.x, plen + b.ch.y, prot);
      var jy = pjy + rotY(b.ch.x, plen + b.ch.y, prot);
      var wrot = prot + b.ch.rot;
      out[id] = { x: jx, y: jy, rot: wrot, len: b.len };
      for (var i = 0; i < b.children.length; i++) walk(b.children[i], jx, jy, wrot, b.len);
    }
    for (var id in bones) {
      if (bones.hasOwnProperty(id) && !bones[id].parent) walk(id, 0, 0, 0, 0);
    }
    return out;
  }

  // ── Closed-form IK (constraints, never iteration) ───────────────────────
  // reach: the effector's far end lands on the target. The chain is the
  // effector bone plus its parent; both are solved analytically. hint picks
  // the elbow side (front/left = one configuration, back/right = the other).
  function solveReach(bones, effectorId, target, hint) {
    var eff = bones[effectorId];
    if (!eff) return;
    var par = eff.parent ? bones[eff.parent] : null;
    var fk = computeFK(bones);
    var J = fk[effectorId];
    var dir = angleOf(target.x - J.x, target.y - J.y);
    if (!par) {
      // A single bone: point it at the target.
      eff.ch.rot += dir - fk[effectorId].rot;
      return;
    }
    var A = fk[par.id];
    var a = Math.sqrt(eff.x * eff.x + (par.len + eff.y) * (par.len + eff.y));
    var b = eff.len;
    var dx = target.x - A.x;
    var dy = target.y - A.y;
    var d = Math.sqrt(dx * dx + dy * dy);
    var phi = angleOf(dx, dy);
    var side = (hint === 'front' || hint === 'left') ? 1 : -1;
    var alpha, beta;
    if (d < 1e-6) {
      // Degenerate target at the shoulder: fold the arm back on itself.
      alpha = 0;
      beta = Math.acos(clamp((a * a + b * b) / (2 * a * b), -1, 1)) / DEG;
    } else {
      // Math.acos returns radians; phi is in degrees — convert so the sums
      // below stay in one unit.
      alpha = Math.acos(clamp((a * a + d * d - b * b) / (2 * a * d), -1, 1)) / DEG;
      beta = Math.acos(clamp((a * a + b * b - d * d) / (2 * a * b), -1, 1)) / DEG;
    }
    var thetaAB = phi + side * alpha;
    var thetaBC = thetaAB + 180 + side * beta; // all in degrees
    var angle0 = angleOf(eff.x, par.len + eff.y);
    var wrotPar = thetaAB - angle0;
    par.ch.rot = wrotPar - A.rot;
    eff.ch.rot = thetaBC - wrotPar;
  }

  // look: the chain's root faces the target — its +Y points at the coordinate.
  function solveLook(bones, chainId, target) {
    var chain = bones[chainId];
    if (!chain) return;
    var fk = computeFK(bones);
    var c = fk[chainId];
    chain.ch.rot += angleOf(target.x - c.x, target.y - c.y) - c.rot;
  }

  // plant: the bone's joint stays on the coordinate while the rest moves.
  // The parent's rotation is solved so the parent's far end plus the bone's
  // own offset lands exactly on the target; a root bone translates instead.
  function solvePlant(bones, boneId, at) {
    var b = bones[boneId];
    if (!b) return;
    var par = b.parent ? bones[b.parent] : null;
    if (!par) { b.ch.x = at.x; b.ch.y = at.y; return; }
    var fk = computeFK(bones);
    var PJ = fk[par.id];
    var dir = angleOf(at.x - PJ.x, at.y - PJ.y);
    var angle0 = angleOf(b.x, par.len + b.y);
    par.ch.rot = dir - angle0 - PJ.rot;
  }

  // track: the chain's root continuously faces the target bone's joint.
  function solveTrack(bones, chainId, targetBoneId) {
    var chain = bones[chainId];
    if (!chain) return;
    var fk = computeFK(bones);
    var t = fk[targetBoneId];
    var c = fk[chainId];
    if (!t) return;
    chain.ch.rot += angleOf(t.x - c.x, t.y - c.y) - c.rot;
  }

  // ── Keyframes + oscillation ─────────────────────────────────────────────
  function sampleKeys(keys, t, easing) {
    var first = keys[0];
    var last = keys[keys.length - 1];
    if (t <= first.t) return first.v;
    if (t >= last.t) return last.v;
    for (var i = 1; i < keys.length; i++) {
      if (t <= keys[i].t) {
        var k0 = keys[i - 1];
        var k1 = keys[i];
        var span = k1.t - k0.t;
        var f = span === 0 ? 1 : (t - k0.t) / span;
        return lerp(k0.v, k1.v, ease(easing, f));
      }
    }
    return last.v;
  }

  function oscValue(o, t) {
    return o.amp * Math.sin(PI2 * o.freq * (t / 1000) + (o.phase || 0) * DEG);
  }

  // ── The stage ───────────────────────────────────────────────────────────
  // One mount builds the whole play: instances, expansion, DOM, audio.
  // Returns a handle { step, duration, audio, destroy }.
  function mount(el, resolvedPlay, opts) {
    opts = opts || {};
    // One stage per element: a re-mount on the same element — the bootstrap
    // self-mount racing the fetch-driven mount in index.js, or a re-mount
    // after a new generation — destroys the previous stage (rAF loop
    // stopped, DOM removed) instead of stacking a second one; separate
    // elements keep concurrent stages.
    var prev = stageFor(el);
    if (prev) prev.handle.destroy();
    var W = (opts.size && opts.size.w) || el.clientWidth || 640;
    var H = (opts.size && opts.size.h) || el.clientHeight || 360;
    var u = opts.pxPerUnit || H / 10; // px per model unit
    var clock = opts.clock || (function() {
      return (global.performance && performance.now) ? performance.now() : +new Date();
    });
    var playEnv = resolvedPlay.play;
    var playSpec = parseSpec(playEnv);
    var assets = resolvedPlay.assets || {};
    var playSeed = (playSpec.seed === undefined || playSpec.seed === null) ? 0 : playSpec.seed;

    // Asset tables: id@version → parsed spec, per kind.
    var table = {
      models: {}, voices: {}, sounds: {}, clips: {}, gags: {}
    };
    var kinds = ['models', 'voices', 'sounds', 'clips', 'gags'];
    for (var ki = 0; ki < kinds.length; ki++) {
      var kind = kinds[ki];
      var src = assets[kind] || {};
      for (var key in src) {
        if (src.hasOwnProperty(key)) table[kind][key] = parseSpec(src[key]);
      }
    }

    // Audio. The lab passes its own context (created inside a user gesture);
    // headless tests pass a recording stub; otherwise fall back to the
    // browser's context and tolerate its absence silently.
    var audio = opts.audio;
    if (!audio) {
      try {
        var Ctor = global.AudioContext || global.webkitAudioContext;
        if (Ctor) audio = new Ctor();
      } catch (e) { audio = null; }
    }
    var rng = makeRng(playSeed); // play-level draws: audio picks

    // ── Selector resolution ────────────────────────────────────────────────
    // A selector is `model:<id>@<version>` or a `/`-joined path of
    // `id#*`/`id#<index>` instance segments; a plain final segment names a
    // bone inside the matched instances. Paths match the generated suffix of
    // a node's path (the play instance id is not part of selector space).
    // Returns an array of { node, boneId }.
    function isSelector(name) {
      return name.indexOf('model:') === 0 || name.indexOf('#') >= 0;
    }

    function matchesPath(path, segments) {
      // path: "playId/generated/..." — match the generated suffix exactly.
      var parts = path.split('/');
      var off = parts.length - segments.length;
      if (off < 1) return false; // the play instance id is never matched
      for (var i = 0; i < segments.length; i++) {
        var want = segments[i];
        var got = parts[off + i];
        var hash = want.indexOf('#');
        if (hash < 0) return false; // instance segments always carry #
        if (want.slice(0, hash) !== got.slice(0, got.indexOf('#'))) return false;
        var wantIdx = want.slice(hash + 1);
        var gotIdx = got.slice(got.indexOf('#') + 1);
        if (wantIdx !== '*' && wantIdx !== gotIdx) return false;
      }
      return true;
    }

    function collectNodes(node, out) {
      out.push(node);
      for (var i = 0; i < node.children.length; i++) collectNodes(node.children[i], out);
    }

    function resolveSelector(pi, sel) {
      var out = [];
      if (sel.indexOf('model:') === 0) {
        var ref = sel.slice(6);
        var nodes = [];
        collectNodes(pi.node, nodes);
        for (var i = 0; i < nodes.length; i++) {
          if (nodes[i].modelRef === ref) out.push({ node: nodes[i], boneId: nodes[i].rootBoneId });
        }
        return out;
      }
      var segments = sel.split('/');
      var last = segments[segments.length - 1];
      var isBone = last.indexOf('#') < 0;
      var instSegs = isBone ? segments.slice(0, -1) : segments;
      var all = [];
      collectNodes(pi.node, all);
      for (var j = 0; j < all.length; j++) {
        var node = all[j];
        if (!matchesPath(node.path, instSegs)) continue;
        if (isBone) {
          if (node.bones[last]) out.push({ node: node, boneId: last });
        } else {
          out.push({ node: node, boneId: node.rootBoneId });
        }
      }
      return out;
    }

    // Resolve a bone or slot target to a list of { node, boneId | slotId }.
    // Exact ids stay in the clip's instance; selectors walk the expansion.
    function resolveTarget(pi, boneName, slotName) {
      var out = [];
      if (slotName !== undefined) {
        if (pi.slots[slotName]) out.push({ node: pi.node, slotId: slotName });
        return out;
      }
      if (boneName === undefined) return out;
      if (isSelector(boneName)) return resolveSelector(pi, boneName);
      if (pi.bones[boneName]) out.push({ node: pi.node, boneId: boneName });
      return out;
    }

    function writeChannel(pi, target, channel, value) {
      var node = target.node;
      if (target.slotId !== undefined) {
        var slot = node.slots[target.slotId];
        if (slot) applyChannel(slot.ch, channel, value);
        return;
      }
      var bone = node.bones[target.boneId];
      if (bone) applyChannel(bone.ch, channel, value);
    }

    function applyChannel(ch, channel, value) {
      if (channel === 'x') ch.x = value;
      else if (channel === 'y') ch.y = value;
      else if (channel === 'rotation') ch.rot = value;
      else if (channel === 'scaleX') ch.scaleX = value;
      else if (channel === 'scaleY') ch.scaleY = value;
      else if (channel === 'opacity') ch.opacity = value;
    }

    // ── Audio: the formant synth (rehomed from the old intro.js VOICES) ───
    // A voice spec maps 1:1 to the synth; every random draw uses the play
    // PRNG so the schedule is reproducible.
    function speak(ctx, t0, voice) {
      var n = Math.round(rRange(voice.bursts, rng));
      var f0 = rRange(voice.f0, rng);
      var amp = rRange(voice.amp, rng);
      var end = t0;
      for (var i = 0; i < n; i++) {
        var d = rRange(voice.dur, rng);
        var bp = (voice.burstPitch && voice.burstPitch[i]) || (1 - i * 0.05);
        var f = f0 * bp;
        var a = amp * (i === 0 ? 1 : 0.82);
        end = renderVoice(ctx, end, d, f, a, voice);
        if (i < n - 1) end += rRange(voice.gap, rng);
      }
      return end;
    }

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

      var pure = ctx.createOscillator();
      pure.type = 'sine';
      pitchArc(pure.frequency);
      var pureGain = ctx.createGain();
      pureGain.gain.value = v.pure;

      var vib = ctx.createOscillator();
      vib.type = 'sine';
      vib.frequency.value = rRange(v.vib, rng);
      var vibGain = ctx.createGain();
      vibGain.gain.value = f0 * v.vib[2];
      vib.connect(vibGain);
      vibGain.connect(osc.frequency);
      vibGain.connect(pure.frequency);

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

      var mouth = ctx.createBiquadFilter();
      mouth.type = 'lowpass';
      mouth.Q.value = 0.7;
      mouth.frequency.setValueAtTime(v.mouth[0], t0);
      mouth.frequency.exponentialRampToValueAtTime(v.mouth[1], t0 + dur * v.kf[1]);
      mouth.frequency.exponentialRampToValueAtTime(v.mouth[2], t0 + dur * v.kf[2]);
      mouth.frequency.exponentialRampToValueAtTime(v.mouth[3], tEnd);
      formantSum.connect(mouth);

      var env = ctx.createGain();
      env.gain.setValueAtTime(0.0001, t0);
      env.gain.linearRampToValueAtTime(amp * 0.22, t0 + dur * 0.10);
      env.gain.linearRampToValueAtTime(amp, t0 + dur * 0.24);
      env.gain.linearRampToValueAtTime(amp * v.decay, t0 + dur * 0.55);
      env.gain.linearRampToValueAtTime(amp * v.decay * 0.5, t0 + dur * 0.80);
      env.gain.exponentialRampToValueAtTime(0.0001, tEnd);
      mouth.connect(env);
      env.connect(ctx.destination);

      if (v.noise > 0) {
        var nLen = Math.ceil(ctx.sampleRate * (dur + 0.05));
        var nBuf = ctx.createBuffer(1, nLen, ctx.sampleRate);
        var nDat = nBuf.getChannelData(0);
        for (var ni = 0; ni < nLen; ni++) nDat[ni] = rng() * 2 - 1;
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
      osc.start(t0); osc.stop(tStop);
      pure.start(t0); pure.stop(tStop);
      vib.start(t0); vib.stop(tStop);
      return tStop;
    }

    // Environmental sound effects: the four closed synthesis types over Web
    // Audio. All parameter picks come from the play PRNG.
    function playSound(ctx, spec, t0) {
      var dur = rRange(spec.dur, rng);
      var amp = rRange(spec.amp, rng);
      var attack = (spec.env && spec.env.attack) || 0.01;
      var decay = (spec.env && spec.env.decay) || 0.25;
      var tEnd = t0 + dur + 0.05;
      function applyEnv(g) {
        g.gain.setValueAtTime(0.0001, t0);
        g.gain.linearRampToValueAtTime(amp, t0 + attack);
        g.gain.exponentialRampToValueAtTime(0.0001, t0 + attack + decay + dur * 0.5);
      }
      var type = spec.type || 'tone';
      if (type === 'noise') {
        var nLen = Math.ceil(ctx.sampleRate * (dur + 0.05));
        var nBuf = ctx.createBuffer(1, nLen, ctx.sampleRate);
        var nDat = nBuf.getChannelData(0);
        for (var ni = 0; ni < nLen; ni++) nDat[ni] = rng() * 2 - 1;
        var nSrc = ctx.createBufferSource();
        nSrc.buffer = nBuf;
        var bp = ctx.createBiquadFilter();
        bp.type = 'bandpass';
        bp.Q.value = 1.2;
        bp.frequency.setValueAtTime(spec.freq[0], t0);
        bp.frequency.linearRampToValueAtTime(spec.freq[1], t0 + dur);
        var ng = ctx.createGain();
        applyEnv(ng);
        nSrc.connect(bp);
        bp.connect(ng);
        ng.connect(ctx.destination);
        nSrc.start(t0);
        nSrc.stop(tEnd);
      } else if (type === 'tone') {
        var o = ctx.createOscillator();
        o.type = 'sine';
        o.frequency.value = rRange(spec.freq, rng);
        var g = ctx.createGain();
        applyEnv(g);
        o.connect(g);
        g.connect(ctx.destination);
        o.start(t0);
        o.stop(tEnd);
      } else if (type === 'sweep') {
        var sw = ctx.createOscillator();
        sw.type = 'sine';
        sw.frequency.setValueAtTime(spec.freq[0], t0);
        sw.frequency.linearRampToValueAtTime(spec.freq[1], t0 + dur);
        var sg = ctx.createGain();
        applyEnv(sg);
        sw.connect(sg);
        sg.connect(ctx.destination);
        sw.start(t0);
        sw.stop(tEnd);
      } else if (type === 'burst') {
        var b = ctx.createOscillator();
        b.type = 'sine';
        b.frequency.value = rRange(spec.freq, rng);
        var bg = ctx.createGain();
        bg.gain.setValueAtTime(0.0001, t0);
        bg.gain.linearRampToValueAtTime(amp, t0 + 0.005);
        bg.gain.exponentialRampToValueAtTime(0.0001, t0 + Math.min(dur, 0.12));
        b.connect(bg);
        bg.connect(ctx.destination);
        b.start(t0);
        b.stop(t0 + Math.min(dur, 0.12) + 0.05);
      }
    }

    function scheduleVoice(ctx, voiceSpec, atMs) {
      var nowMs = clock();
      var t0 = ctx.currentTime + Math.max(0, atMs - nowMs) / 1000;
      speak(ctx, t0, voiceSpec);
    }

    function scheduleSound(ctx, soundSpec, atMs) {
      var nowMs = clock();
      var t0 = ctx.currentTime + Math.max(0, atMs - nowMs) / 1000;
      playSound(ctx, soundSpec, t0);
    }

    // ── Node + instance construction ───────────────────────────────────────
    // A play instance's node record carries its own bones/slots plus the
    // expanded generated children. Generated nodes mount at a bone's far end
    // (attach/recurse) or at the model frame (scatter).
    function newSkeleton(pi, spec, path) {
      var node = { path: path, spec: spec, children: [] };
      var bones = buildBones(spec);
      var rootBoneId = null;
      for (var id in bones) {
        if (bones.hasOwnProperty(id) && !bones[id].parent) { rootBoneId = id; break; }
      }
      // Slots from the default skin: slot id → attachment id. Attachments are
      // drawn once per attachment; the skin picks which is visible.
      var skins = spec.skins || {};
      var def = skins['default'] || {};
      var slots = {};
      var attachments = spec.attachments || [];
      for (var i = 0; i < attachments.length; i++) {
        var att = attachments[i];
        var slotId = def[att.slot] === att.id ? att.slot : null;
        if (slotId !== null && !slots[slotId]) {
          slots[slotId] = { attachment: att, boneId: att.bone, ax: att.x || 0, ay: att.y || 0, arot: att.rot || 0, ch: null };
        }
      }
      node.bones = bones;
      node.rootBoneId = rootBoneId;
      node.slots = slots;
      node.boneEls = {};
      node.slotEls = {};
      return node;
    }

    function addNode(pi, parentNode, modelRef, path, seed) {
      var spec = table.models[modelRef];
      if (!spec) return null;
      var node = newSkeleton(pi, spec, path);
      node.modelRef = modelRef;
      node.mount = null; // set by the caller: { boneId, x, y, rot, scale }
      node.seed = seed;
      parentNode.children.push(node);
      return node;
    }

    // The deterministic expander. recurseDepth threads the L-system depth:
    // a recurse child re-expands the same model with depth-1, so recursion is
    // bounded by the authored depth, never by the call stack.
    function expandNode(pi, node, recurseDepth) {
      var spec = node.spec || pi.spec;
      var structure = spec.structure || [];
      for (var i = 0; i < structure.length; i++) {
        var v = structure[i];
        if (v.type === 'attach') {
          var child = addNode(pi, node, v.model, node.path + '/' + v.model.split('@')[0] + '#0', childSeed(node.seed, 0));
          if (child) {
            child.mount = { boneId: v.at, x: 0, y: 0, rot: v.rot || 0, scale: v.scale === undefined ? 1 : v.scale };
            expandNode(pi, child, undefined);
          }
        } else if (v.type === 'scatter') {
          expandScatter(pi, node, v);
        } else if (v.type === 'recurse') {
          var depth = recurseDepth === undefined ? v.depth : recurseDepth;
          expandRecurse(pi, node, v, depth);
        }
      }
    }

    function expandScatter(pi, node, v) {
      var count = v.count || 0;
      for (var i = 0; i < count; i++) {
        var seed = childSeed(v.seed === undefined ? node.seed : v.seed, i);
        var rngI = makeRng(seed);
        var pos = regionPoint(v.over, i, count, rngI, node);
        var js = v.jitter || {};
        var scale = 1 + (js.scale || 0) * (rngI() * 2 - 1);
        var rot = (js.rot || 0) * (rngI() * 2 - 1);
        var child = addNode(pi, node, v.model, node.path + '/' + v.model.split('@')[0] + '#' + i, seed);
        if (child) {
          child.mount = { boneId: null, x: pos.x, y: pos.y, rot: rot, scale: scale };
          expandNode(pi, child, undefined);
        }
      }
    }

    function expandRecurse(pi, node, v, depth) {
      if (depth < 0) return;
      var branch = v.branch || 0;
      var angle = v.angle || 0;
      var decay = v.decay === undefined ? 1 : v.decay;
      for (var i = 0; i < branch; i++) {
        var seed = childSeed(v.seed === undefined ? node.seed : v.seed, i);
        var rot = (i - (branch - 1) / 2) * angle;
        var childRef = depth <= 0 ? v.tip : v.model;
        var child = addNode(pi, node, childRef, node.path + '/' + childRef.split('@')[0] + '#' + i, seed);
        if (child) {
          child.mount = { boneId: v.at, x: 0, y: 0, rot: rot, scale: decay };
          expandNode(pi, child, depth - 1);
        }
      }
    }

    // Region placement: the closed `over` vocabulary for scatter. All draws
    // come from the per-instance PRNG, so placement is reproducible.
    // Rest FK: the bone tree's rest pose, before any animation channel is
    // applied. Used for `along` placement, which needs the containing model's
    // rest geometry at expansion time.
    function restFK(bones) {
      var out = {};
      function walk(id, pjx, pjy, prot, plen) {
        var b = bones[id];
        var jx = pjx + rotX(b.x, plen + b.y, prot);
        var jy = pjy + rotY(b.x, plen + b.y, prot);
        var wrot = prot + b.rot;
        out[id] = { x: jx, y: jy, rot: wrot, len: b.len };
        for (var i = 0; i < b.children.length; i++) walk(b.children[i], jx, jy, wrot, b.len);
      }
      for (var id in bones) {
        if (bones.hasOwnProperty(id) && !bones[id].parent) walk(id, 0, 0, 0, 0);
      }
      return out;
    }

    function regionPoint(region, i, count, rngI, node) {
      var t = count > 1 ? i / (count - 1) : 0;
      if (!region || region.type === 'band') {
        var w = (region && region.w) || 1;
        var h = (region && region.h) || 1;
        return { x: rngI() * w, y: rngI() * h };
      }
      if (region.type === 'disc') {
        var r = region.r || 1;
        var a = rngI() * PI2;
        var rad = r * Math.sqrt(rngI());
        return { x: Math.cos(a) * rad, y: Math.sin(a) * rad };
      }
      if (region.type === 'grid') {
        var cols = region.cols || 1;
        var cell = region.cell || 1;
        var cx = (i % cols) * cell + rngI() * cell;
        var cy = Math.floor(i / cols) * cell + rngI() * cell;
        return { x: cx, y: cy };
      }
      if (region.type === 'curve') {
        var pts = region.points || [];
        if (pts.length === 0) return { x: 0, y: 0 };
        var seg = Math.min(pts.length - 1, Math.floor(t * (pts.length - 1)));
        var f = t * (pts.length - 1) - seg;
        var p0 = pts[seg];
        var p1 = pts[Math.min(seg + 1, pts.length - 1)];
        return { x: lerp(p0[0], p1[0], f), y: lerp(p0[1], p1[1], f) };
      }
      if (region.type === 'along') {
        // Along the named bone of the containing model: joint → far end.
        var fk = restFK(node.bones);
        var bj = fk[region.bone];
        if (!bj) return { x: 0, y: 0 };
        return {
          x: bj.x + t * rotX(0, bj.len, bj.rot),
          y: bj.y + t * rotY(0, bj.len, bj.rot)
        };
      }
      return { x: 0, y: 0 };
    }

    // ── DOM construction ───────────────────────────────────────────────────
    function makeDiv() { return global.document.createElement('div'); }

    function buildAttachmentEl(att, u) {
      var el = makeDiv();
      el.style.position = 'absolute';
      el.style.left = '0px';
      el.style.top = '0px';
      el.style.background = att.shape.color || '#888';
      var sh = att.shape;
      if (sh.type === 'ellipse') {
        el.style.width = (sh.w || 0) * u + 'px';
        el.style.height = (sh.h || 0) * u + 'px';
        el.style.borderRadius = '50%';
      } else if (sh.type === 'rect') {
        el.style.width = (sh.w || 0) * u + 'px';
        el.style.height = (sh.h || 0) * u + 'px';
        if (sh.radius) el.style.borderRadius = sh.radius * u + 'px';
      } else if (sh.type === 'path') {
        var pts = sh.points || [];
        var minX = 0, minY = 0, maxX = 0, maxY = 0;
        for (var i = 0; i < pts.length; i++) {
          minX = Math.min(minX, pts[i][0]); maxX = Math.max(maxX, pts[i][0]);
          minY = Math.min(minY, pts[i][1]); maxY = Math.max(maxY, pts[i][1]);
        }
        var pw = maxX - minX, ph = maxY - minY;
        el.style.width = (pw || 1) * u + 'px';
        el.style.height = (ph || 1) * u + 'px';
        var poly = [];
        for (var k = 0; k < pts.length; k++) {
          poly.push(((pts[k][0] - minX) / (pw || 1)) * 100 + '% ' + ((pts[k][1] - minY) / (ph || 1)) * 100 + '%');
        }
        el.style.clipPath = 'polygon(' + poly.join(',') + ')';
        el.style.webkitClipPath = 'polygon(' + poly.join(',') + ')';
      }
      return el;
    }

    // Build the bone/attachment divs of one skeleton into `container`, which
    // is either the instance div (play instance) or a node root div.
    function buildSkeletonDOM(pi, node, container) {
      var bones = node.bones;
      var boneEls = {};
      for (var id in bones) {
        if (!bones.hasOwnProperty(id)) continue;
        var el = makeDiv();
        el.style.position = 'absolute';
        el.style.left = '0px';
        el.style.top = '0px';
        el.setAttribute('data-path', node.path);
        el.setAttribute('data-bone', id);
        container.appendChild(el);
        boneEls[id] = el;
      }
      // Attach each bone div under its parent bone div (the rig is the DOM
      // nesting); root bone divs nest under the container.
      for (var bid in bones) {
        if (!bones.hasOwnProperty(bid)) continue;
        var b = bones[bid];
        if (b.parent && boneEls[b.parent]) boneEls[b.parent].appendChild(boneEls[bid]);
      }
      var slotEls = {};
      // The slot records (built by newSkeleton from the default skin) carry
      // their attachment; draw one div per slot.
      for (var sid in node.slots) {
        if (!node.slots.hasOwnProperty(sid)) continue;
        var slotRec = node.slots[sid];
        var att = slotRec.attachment;
        var ael = buildAttachmentEl(att, u);
        ael.style.position = 'absolute';
        ael.style.left = '0px';
        ael.style.top = '0px';
        ael.setAttribute('data-path', node.path);
        ael.setAttribute('data-slot', sid);
        var boneEl = boneEls[att.bone] || container;
        boneEl.appendChild(ael);
        if (!slotEls[sid]) slotEls[sid] = ael;
      }
      node.boneEls = boneEls;
      node.slotEls = slotEls;
      // Per-frame channels: bones start from rest; slots from the attachment.
      resetChannels(pi, node);
    }

    function resetChannels(pi, node) {
      var bones = node.bones;
      for (var id in bones) {
        if (!bones.hasOwnProperty(id)) continue;
        var b = bones[id];
        b.ch = { x: b.x, y: b.y, rot: b.rot, scaleX: b.scale, scaleY: b.scale, opacity: 1 };
      }
      for (var sid in node.slots) {
        if (!node.slots.hasOwnProperty(sid)) continue;
        var s = node.slots[sid];
        s.ch = { rot: s.arot, scaleX: 1, scaleY: 1, opacity: 1 };
      }
    }

    function applySkeletonDOM(pi, node) {
      var bones = node.bones;
      for (var id in bones) {
        if (!bones.hasOwnProperty(id)) continue;
        var b = bones[id];
        var el = node.boneEls[id];
        if (!el) continue;
        var py = b.parent ? (bones[b.parent].len + b.ch.y) : b.ch.y;
        setTransform(el, 'translate(' + b.ch.x * u + 'px,' + py * u + 'px) rotate(' + b.ch.rot + 'deg) scale(' + b.ch.scaleX + ',' + b.ch.scaleY + ')');
        if (b.ch.opacity !== 1) el.style.opacity = b.ch.opacity;
        else if (el.style.opacity !== undefined) el.style.opacity = '';
      }
      for (var sid in node.slots) {
        if (!node.slots.hasOwnProperty(sid)) continue;
        var s = node.slots[sid];
        var sel = node.slotEls[sid];
        if (!sel) continue;
        setTransform(sel, 'translate(' + s.ax * u + 'px,' + s.ay * u + 'px) rotate(' + s.ch.rot + 'deg) scale(' + s.ch.scaleX + ',' + s.ch.scaleY + ')');
        if (s.ch.opacity !== 1) sel.style.opacity = s.ch.opacity;
        else if (sel.style.opacity !== undefined) sel.style.opacity = '';
      }
    }

    // ── The play instance ──────────────────────────────────────────────────
    var instances = [];
    var instancesById = {};
    var seq = 0; // activation sequence: later-started clips win conflicts

    function buildInstance(pi) {
      var env = table.models[pi.model];
      var spec = env || {};
      pi.spec = spec;
      pi.node = newSkeleton(pi, spec, pi.id);
      pi.node.spec = spec;
      pi.node.modelRef = pi.model;
      pi.node.children = [];
      pi.node.seed = childSeed(playSeed, instances.length);
      pi.bones = pi.node.bones;
      pi.slots = pi.node.slots;
      pi.activeClips = [];
      pi.gag = null;
      pi.tween = null;
      pi.el = makeDiv();
      pi.el.style.position = 'absolute';
      pi.el.style.left = '0px';
      pi.el.style.top = '0px';
      pi.el.setAttribute('data-instance', pi.id);
      stageEl.appendChild(pi.el);
      buildSkeletonDOM(pi, pi.node, pi.el);
      // Generated children: expand the structure verbs, build their DOM.
      expandNode(pi, pi.node, undefined);
      buildNodeDOM(pi, pi.node);
      // Resolved voice/sound: instance override → model default.
      pi.voiceRef = pi.voice || (spec.voice ? spec.voice : null);
      pi.soundRef = pi.sound || (spec.sound ? spec.sound : null);
    }

    function buildNodeDOM(pi, node) {
      for (var i = 0; i < node.children.length; i++) {
        var child = node.children[i];
        var mount = child.mount;
        var cEl = makeDiv();
        cEl.style.position = 'absolute';
        cEl.style.left = '0px';
        cEl.style.top = '0px';
        cEl.setAttribute('data-path', child.path);
        // Scatter children sit in the model frame (the instance div);
        // attach/recurse children sit at the mount bone's far end.
        var parentEl;
        if (mount.boneId) {
          parentEl = node.boneEls[mount.boneId] || pi.el;
          var boneLen = node.bones[mount.boneId] ? node.bones[mount.boneId].len : 0;
          setTransform(cEl, 'translate(' + mount.x * u + 'px,' + (mount.y + boneLen) * u + 'px) rotate(' + mount.rot + 'deg) scale(' + mount.scale + ',' + mount.scale + ')');
        } else {
          parentEl = pi.el;
          setTransform(cEl, 'translate(' + mount.x * u + 'px,' + mount.y * u + 'px) rotate(' + mount.rot + 'deg) scale(' + mount.scale + ',' + mount.scale + ')');
        }
        parentEl.appendChild(cEl);
        child.el = cEl;
        buildSkeletonDOM(pi, child, cEl);
        buildNodeDOM(pi, child);
      }
    }

    // ── Timeline sequencing ────────────────────────────────────────────────
    // Stable order: same-at entries run concurrently and apply in authored
    // order (later-authored wins on channel conflicts). The sort key carries
    // the authored index because old Blink's sort is not stable.
    var timeline = (playSpec.timeline || []).map(function(e, i) {
      return { at: e.at, idx: i, entry: e };
    }).sort(function(a, b) { return a.at - b.at || a.idx - b.idx; });
    var ptr = 0;
    var lastT = -1;

    function newClipPlay(spec, start, loop) {
      return { spec: spec, start: start, loop: !!loop, seq: seq++, firedEvents: {} };
    }

    function startGag(pi, gagSpec, now) {
      if (!gagSpec.clips.length) return;
      var clipSpec = table.clips[gagSpec.clips[0]];
      if (!clipSpec) return;
      pi.gag = {
        spec: gagSpec,
        clipIndex: 0,
        clipStart: now,
        clipPlay: newClipPlay(clipSpec, now, false)
      };
    }

    function evalGag(pi, now) {
      var g = pi.gag;
      if (!g) return;
      var clipSpec = table.clips[g.spec.clips[g.clipIndex]];
      while (clipSpec && now - g.clipStart >= clipSpec.duration) {
        var next = g.clipIndex + 1;
        if (next >= g.spec.clips.length) { pi.gag = null; return; }
        g.clipStart += clipSpec.duration;
        g.clipIndex = next;
        g.clipPlay = newClipPlay(table.clips[g.spec.clips[next]], g.clipStart, false);
        clipSpec = table.clips[g.spec.clips[next]];
      }
    }

    // The instance's pose at time t: the running tween evaluated there, or
    // the authored rest pose. A finished tween keeps its target — the pose
    // stays where the last tween left it.
    function poseAt(pi, t) {
      var x = pi.x || 0, y = pi.y || 0, rot = 0, scale = pi.scale || 1;
      var tw = pi.tween;
      if (tw) {
        var over = tw.spec.over || 1;
        var f = ease(tw.spec.easing || 'linear', clamp((t - tw.start) / over, 0, 1));
        var to = tw.to;
        if (to.x !== undefined) x = lerp(tw.from.x, to.x, f);
        if (to.y !== undefined) y = lerp(tw.from.y, to.y, f);
        if (to.rot !== undefined) rot = lerp(tw.from.rot, to.rot, f);
        if (to.scale !== undefined) scale = lerp(tw.from.scale, to.scale, f);
      }
      return { x: x, y: y, rot: rot, scale: scale };
    }

    function startTween(pi, tweenSpec, now) {
      var to = tweenSpec.to || {};
      // A tween moves the root from its current pose at the moment it starts
      // (D-19: beside/off resolve at tween start).
      var from = poseAt(pi, now);
      var target = {};
      if (to.beside) {
        var other = instancesById[to.beside];
        var op = other ? poseAt(other, now) : from;
        var side = to.side || 'left';
        if (side === 'left') { target.x = op.x - 0.15; target.y = op.y; }
        else if (side === 'right') { target.x = op.x + 0.15; target.y = op.y; }
        else if (side === 'front') { target.x = op.x; target.y = op.y + 0.15; }
        else { target.x = op.x; target.y = op.y - 0.15; }
      } else if (to.off) {
        target.x = to.off === 'left' ? -0.5 : 1.5;
        target.y = from.y;
      } else {
        if (to.x !== undefined) target.x = to.x;
        if (to.y !== undefined) target.y = to.y;
        if (to.rot !== undefined) target.rot = to.rot;
        if (to.scale !== undefined) target.scale = to.scale;
      }
      pi.tween = { spec: tweenSpec, start: now, from: from, to: target };
    }

    function fireTimeline(now) {
      while (ptr < timeline.length && timeline[ptr].at <= now) {
        var e = timeline[ptr++].entry;
        var inst = instancesById[e.on];
        if (!inst) continue;
        // Entries start at their authored `at`, not at the first step that
        // happens to land later: a timeline is an absolute clock.
        if (e.clip && table.clips[e.clip]) {
          inst.activeClips.push(newClipPlay(table.clips[e.clip], e.at, table.clips[e.clip].loop));
        } else if (e.gag && table.gags[e.gag]) {
          startGag(inst, table.gags[e.gag], e.at);
        } else if (e.tween) {
          startTween(inst, e.tween, e.at);
        }
      }
    }

    // ── Per-frame evaluation ───────────────────────────────────────────────
    function evalClip(pi, clipPlay, now) {
      var spec = clipPlay.spec;
      var elapsed = now - clipPlay.start;
      var local = clipPlay.loop ? elapsed % spec.duration : Math.min(elapsed, spec.duration);
      var i, k, targets;
      var kfs = spec.keyframes || [];
      for (i = 0; i < kfs.length; i++) {
        var kf = kfs[i];
        targets = resolveTarget(pi, kf.bone, kf.slot);
        var v = sampleKeys(kf.keys, local, kf.easing);
        for (k = 0; k < targets.length; k++) writeChannel(pi, targets[k], kf.channel, v);
      }
      var oscs = spec.oscillations || [];
      for (i = 0; i < oscs.length; i++) {
        var os = oscs[i];
        targets = resolveTarget(pi, os.bone, undefined);
        var ov = oscValue(os, local);
        for (k = 0; k < targets.length; k++) writeChannel(pi, targets[k], os.channel, ov);
      }
      var cons = spec.constraints || [];
      for (i = 0; i < cons.length; i++) applyConstraint(pi, cons[i]);
      var evs = spec.events || [];
      for (i = 0; i < evs.length; i++) {
        if (clipPlay.firedEvents[i]) continue;
        var ev = evs[i];
        if (ev.at > local) continue;
        clipPlay.firedEvents[i] = true;
        if (!audio) continue;
        var atMs = clipPlay.start + ev.at;
        if (ev.voice && pi.voiceRef && table.voices[pi.voiceRef]) {
          scheduleVoice(audio, table.voices[pi.voiceRef], atMs);
        } else if (ev.sound && table.sounds[ev.sound]) {
          scheduleSound(audio, table.sounds[ev.sound], atMs);
        }
      }
    }

    function applyConstraint(pi, c) {
      // The resolved play preserves the authored constraint targets: reach/
      // look/plant carry a {x, y} coordinate, track carries a bone id string.
      if (c.type === 'reach') {
        var coord = c.target && typeof c.target === 'object' ? c.target : { x: 0, y: 0 };
        var effTargets = resolveTarget(pi, c.effector, undefined);
        for (var i = 0; i < effTargets.length; i++) {
          solveReach(effTargets[i].node.bones, effTargets[i].boneId, coord, c.hint || 'front');
        }
      } else if (c.type === 'look') {
        var lookCoord = c.target && typeof c.target === 'object' ? c.target : { x: 0, y: 0 };
        var lookTargets = resolveTarget(pi, c.chain, undefined);
        for (var j = 0; j < lookTargets.length; j++) {
          solveLook(lookTargets[j].node.bones, lookTargets[j].boneId, lookCoord);
        }
      } else if (c.type === 'plant') {
        var plantTargets = resolveTarget(pi, c.bone, undefined);
        for (var k = 0; k < plantTargets.length; k++) {
          solvePlant(plantTargets[k].node.bones, plantTargets[k].boneId, c.at || { x: 0, y: 0 });
        }
      } else if (c.type === 'track') {
        var trackTargets = resolveTarget(pi, c.chain, undefined);
        for (var m = 0; m < trackTargets.length; m++) {
          solveTrack(trackTargets[m].node.bones, trackTargets[m].boneId, c.target);
        }
      }
    }

    function resetAllChannels(pi, node) {
      resetChannels(pi, node);
      for (var i = 0; i < node.children.length; i++) resetAllChannels(pi, node.children[i]);
    }

    function evalInstance(pi, now) {
      // Every frame starts from the rest pose across the whole expansion —
      // generated nodes included — and the active clips re-apply (constraints
      // with += never compound across frames). The gag advances before the
      // clips evaluate, so the clip current at `now` is the one that runs.
      resetAllChannels(pi, pi.node);
      if (pi.gag) evalGag(pi, now);
      var all = pi.activeClips.slice();
      if (pi.gag) all.push(pi.gag.clipPlay);
      all.sort(function(a, b) { return a.seq - b.seq; });
      for (var i = 0; i < all.length; i++) evalClip(pi, all[i], now);
      // Drop finished non-loop clips (the gag's current clip is managed by evalGag).
      var keep = [];
      for (var j = 0; j < pi.activeClips.length; j++) {
        var cp = pi.activeClips[j];
        if (cp.loop || now - cp.start < cp.spec.duration) keep.push(cp);
      }
      pi.activeClips = keep;
      applyTween(pi, now);
      applySkeletonDOM(pi, pi.node);
      for (var n = 0; n < pi.node.children.length; n++) applyNodeDOM(pi, pi.node.children[n]);
    }

    function applyNodeDOM(pi, node) {
      applySkeletonDOM(pi, node);
      for (var i = 0; i < node.children.length; i++) applyNodeDOM(pi, node.children[i]);
    }

    function applyTween(pi, now) {
      var pose = poseAt(pi, now);
      setTransform(pi.el, 'translate(' + pose.x * W + 'px,' + pose.y * H + 'px) rotate(' + pose.rot + 'deg) scale(' + pose.scale + ',' + pose.scale + ')');
    }

    function step(now) {
      if (now < lastT) now = lastT; // monotonic: backward scrubs need a remount
      lastT = now;
      fireTimeline(now);
      for (var i = 0; i < instances.length; i++) evalInstance(instances[i], now);
    }

    // ── Build ──────────────────────────────────────────────────────────────
    var stageEl = makeDiv();
    stageEl.setAttribute('data-stage', 'troupe');
    stageEl.style.position = 'absolute';
    stageEl.style.left = '0px';
    stageEl.style.top = '0px';
    stageEl.style.width = W + 'px';
    stageEl.style.height = H + 'px';
    el.appendChild(stageEl);

    var playInsts = playSpec.instances || [];
    for (var piIdx = 0; piIdx < playInsts.length; piIdx++) {
      var pi = playInsts[piIdx];
      var rec = {
        id: pi.id,
        model: pi.model,
        role: pi.role || 'actor',
        scale: pi.scale || 1,
        x: pi.x || 0,
        y: pi.y || 0,
        voice: pi.voice || null,
        sound: pi.sound || null,
        activeClips: [],
        gag: null,
        tween: null,
        spec: null,
        node: null,
        bones: null,
        slots: null,
        el: null,
        voiceRef: null,
        soundRef: null
      };
      instances.push(rec);
      instancesById[rec.id] = rec;
    }
    for (var bi = 0; bi < instances.length; bi++) buildInstance(instances[bi]);

    // Total duration estimate for the simulator: last timeline entry plus its
    // clip/gag/tween span (one loop cycle for looping clips).
    var duration = 0;
    for (var di = 0; di < timeline.length; di++) {
      var e = timeline[di].entry;
      var span = 0;
      if (e.clip && table.clips[e.clip]) span = table.clips[e.clip].duration;
      else if (e.gag && table.gags[e.gag]) {
        var gs = table.gags[e.gag];
        for (var gi = 0; gi < gs.clips.length; gi++) {
          if (table.clips[gs.clips[gi]]) span += table.clips[gs.clips[gi]].duration;
        }
      } else if (e.tween) span = e.tween.over;
      if (e.at + span > duration) duration = e.at + span;
    }

    // Self-driving loop (auto mode). The lab mounts with auto:false and calls
    // step() itself from its own clock. Production loops the play: once the
    // timeline's total duration elapses it restarts from t=0, so the splash
    // never freezes on a finished one-shot.
    var rafId = null;
    var playing = true;
    var loopEpoch = 0;
    function restartPlay() {
      ptr = 0;
      lastT = 0;
      for (var i = 0; i < instances.length; i++) {
        var pi = instances[i];
        pi.activeClips = [];
        pi.gag = null;
        pi.tween = null;
        resetAllChannels(pi, pi.node);
      }
    }
    function loop() {
      if (!playing) return;
      var now = clock();
      if (duration > 0 && now - loopEpoch >= duration) {
        loopEpoch = now;
        restartPlay();
      }
      step(now - loopEpoch);
      rafId = requestFrame(loop);
    }
    function requestFrame(fn) {
      if (global.requestAnimationFrame) return global.requestAnimationFrame(fn);
      return setTimeout(function() { fn(); }, 16);
    }
    if (opts.auto !== false) {
      if (global.document && global.document.hidden !== undefined) {
        // Pause when the tab is hidden — a TV is the primary use, but a
        // backgrounded tab should not keep burning a weak device.
        global.document.addEventListener('visibilitychange', function() {
          if (global.document.hidden) playing = false;
          else if (!playing) { playing = true; loop(); }
        }, false);
      }
      loop();
    }

    var handle = {
      step: step,
      duration: duration,
      audio: audio,
      destroy: function() {
        if (rafId !== null) {
          if (global.cancelAnimationFrame) global.cancelAnimationFrame(rafId);
          else clearTimeout(rafId);
        }
        playing = false;
        if (stageEl.parentNode) stageEl.parentNode.removeChild(stageEl);
        for (var si = 0; si < stages.length; si++) {
          if (stages[si].handle === handle) { stages.splice(si, 1); break; }
        }
      }
    };
    stages.push({ el: el, handle: handle });
    return handle;
  }

  // Live stages by element. Mount replaces the element's previous stage
  // instead of stacking a second rAF loop over one div; separate elements
  // keep concurrent stages (the engine harness mounts two plays side by
  // side to compare reproducibility). Destroy unregisters its own entry.
  var stages = [];

  function stageFor(el) {
    for (var i = 0; i < stages.length; i++) {
      if (stages[i].el === el) return stages[i];
    }
    return null;
  }

  var TroupeEngine = { mount: mount };

  // Self-mount: when #troupe exists and a resolved play is present, render
  // it. An empty stage is the signal to investigate — nothing renders, no
  // play means no play.
  function bootstrap() {
    var doc = global.document;
    if (!doc || !doc.getElementById) return;
    var el = doc.getElementById('troupe');
    if (!el) return;
    if (!global.TROUPE_PLAY) return;
    TroupeEngine.mount(el, global.TROUPE_PLAY);
  }
  if (global.document) {
    if (global.document.readyState === 'loading') {
      global.document.addEventListener('DOMContentLoaded', bootstrap, false);
    } else {
      bootstrap();
    }
  }

  global.TroupeEngine = TroupeEngine;
})(typeof window !== 'undefined' ? window : this);
