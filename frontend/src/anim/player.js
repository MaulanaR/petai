import * as THREE from 'three';

// Converts the data-only animation DSL (see qa/CONTRACT.md) into THREE.AnimationClips bound to a
// character rig, and plays them with crossfades plus timed expressions/effects.
// Values in the DSL are relative to the rig's rest pose, so one clip works on every character.

const INTERP = {
  linear: THREE.InterpolateLinear,
  smooth: THREE.InterpolateSmooth,
  step: THREE.InterpolateDiscrete,
};

export class AnimPlayer {
  constructor(character, { onExpression, onEffect } = {}) {
    this.char = character;
    this.mixer = new THREE.AnimationMixer(character.root);
    this.specs = new Map();
    this.clips = new Map();
    this.rest = new Map();
    this.current = null; // { name, action, spec, prevTime }
    this.fading = []; // { action, until }
    this.onExpression = onExpression || (() => {});
    this.onEffect = onEffect || (() => {});
    this.onFinished = null;
    for (const [name, obj] of Object.entries(character.slots)) {
      this.rest.set(name, {
        position: obj.position.clone(),
        rotation: obj.rotation.clone(),
        scale: obj.scale.clone(),
      });
    }
    this.mixer.addEventListener('finished', (e) => {
      if (this.current && e.action === this.current.action && this.onFinished) {
        const cb = this.onFinished;
        this.onFinished = null;
        cb(this.current.name);
      }
    });
  }

  /** Registers (or replaces) specs. */
  load(specs) {
    for (const s of specs) {
      this.specs.set(s.name, s);
      this.clips.delete(s.name);
    }
  }

  has(name) { return this.specs.has(name); }

  clip(name) {
    if (this.clips.has(name)) return this.clips.get(name);
    const spec = this.specs.get(name);
    if (!spec) return null;
    const tracks = [];
    for (const t of spec.tracks) {
      const obj = this.char.slots[t.slot];
      const rest = this.rest.get(t.slot);
      if (!obj || !rest) continue; // slot not present on this character
      const times = t.times.slice();
      const interp = INTERP[t.interp] ?? THREE.InterpolateSmooth;
      const path = obj.name;
      if (t.prop === 'scale') {
        const values = [];
        for (const v of t.values) {
          const a = Array.isArray(v) ? v : [v, v, v];
          values.push(rest.scale.x * a[0], rest.scale.y * a[1], rest.scale.z * a[2]);
        }
        tracks.push(new THREE.VectorKeyframeTrack(`${path}.scale`, times, values, interp));
        continue;
      }
      const [prop, axis] = t.prop.split('.');
      const base = rest[prop][axis];
      const values = t.values.map((v) => {
        const n = Array.isArray(v) ? v[0] : v;
        return prop === 'scale' ? base * n : base + n;
      });
      tracks.push(new THREE.NumberKeyframeTrack(`${path}.${prop}[${axis}]`, times, values, interp));
    }
    const clip = new THREE.AnimationClip(spec.name, spec.duration, tracks);
    this.clips.set(name, clip);
    return clip;
  }

  /**
   * Plays an animation. opts: { fade=0.25, loop (default spec.loop), onDone }.
   * Non-looping clips call onDone when finished. Returns false if unknown.
   */
  play(name, opts = {}) {
    const spec = this.specs.get(name);
    const clip = this.clip(name);
    if (!spec || !clip) return false;
    if (this.current && this.current.name === name && !opts.restart) {
      if (opts.onDone) this.onFinished = opts.onDone;
      return true;
    }
    const loop = opts.loop ?? spec.loop;
    const action = this.mixer.clipAction(clip);
    action.reset();
    action.setLoop(loop ? THREE.LoopRepeat : THREE.LoopOnce, Infinity);
    action.clampWhenFinished = !loop;
    action.timeScale = opts.timeScale ?? 1;
    const fade = opts.fade ?? 0.25;
    if (this.current && this.current.action !== action) {
      action.crossFadeFrom(this.current.action, fade, false);
      this.fading.push({ action: this.current.action, until: this.mixer.time + fade + 0.05 });
      this.fading = this.fading.filter((f) => f.action !== action);
    } else {
      action.fadeIn(fade);
    }
    action.play();
    this.onFinished = opts.onDone || null;
    this.current = { name, action, spec, prevTime: 0 };
    // fire t=0 events immediately
    this.fireEvents(spec, -1, 0);
    return true;
  }

  get name() { return this.current ? this.current.name : ''; }

  fireEvents(spec, from, to) {
    for (const e of spec.expressions || []) {
      if (e.t > from && e.t <= to) this.onExpression(e.eyes, e.mouth);
      else if (from < 0 && e.t === 0) this.onExpression(e.eyes, e.mouth);
    }
    for (const e of spec.effects || []) {
      if (e.t > from && e.t <= to) this.onEffect(e.type);
      else if (from < 0 && e.t === 0) this.onEffect(e.type);
    }
  }

  update(dt) {
    this.mixer.update(dt);
    if (this.fading.length) {
      // Fully stop faded-out actions so untouched properties return to the rest pose.
      this.fading = this.fading.filter((f) => {
        if (this.mixer.time < f.until) return true;
        if (!this.current || f.action !== this.current.action) f.action.stop();
        return false;
      });
    }
    const c = this.current;
    if (!c) return;
    const t = c.action.time;
    if (c.prevTime >= 0) {
      if (t >= c.prevTime) this.fireEvents(c.spec, c.prevTime, t);
      else {
        // looped
        this.fireEvents(c.spec, c.prevTime, c.spec.duration);
        this.fireEvents(c.spec, -1, t);
      }
    }
    c.prevTime = t;
  }

  dispose() {
    this.mixer.stopAllAction();
    this.mixer.uncacheRoot(this.char.root);
  }
}
