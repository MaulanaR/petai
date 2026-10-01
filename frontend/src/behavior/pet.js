// Pet behavior: a small state machine + movement for the three modes.
//   stay   – stays at an anchor, only in-place animations (drag moves the anchor)
//   ground – gravity; walks on the work-area floor (top of taskbar) and can hop onto the
//            top edge of the foreground window
//   free   – floats anywhere on the monitor, no gravity
// Positions are the pet's feet (bottom-centre) in overlay CSS px.

const GRAVITY = 2600;
const rand = (a, b) => a + Math.random() * (b - a);
const pick = (arr) => arr[Math.floor(Math.random() * arr.length)];

export class PetBehavior {
  constructor({ player, setFacing, onStateChange }) {
    this.player = player;
    this.setFacing = setFacing; // (dir -1|0|1, lean)
    this.onStateChange = onStateChange || (() => {});
    this.mode = 'ground';
    this.speed = 1;
    this.activity = 0.5;
    this.anchor = null;
    this.pos = { x: 300, y: 300 };
    this.vel = { x: 0, y: 0 };
    this.bounds = { width: 1280, height: 720, floorY: 680, left: 0, right: 1280, top: 0 };
    this.platform = null; // {x, y, w}
    this.onPlatform = false;
    this.state = 'idle';
    this.timer = 1;
    this.target = null;
    this.away = false;
    this.talkUntil = 0;
    this.jump = null;
    this.unitPx = 110;
    this.heightUnits = 1;
    this.time = 0;
    this.dir = 0;
    this.placed = false; // true once the pet has a real on-screen position
  }

  // ---------- configuration ----------

  setBounds(b) {
    this.bounds = b;
    this.clampToBounds();
    if (this.mode === 'stay' && this.anchor) this.pos = { ...this.anchor };
  }

  setSize(unitPx, heightUnits) {
    this.unitPx = unitPx;
    this.heightUnits = heightUnits;
  }

  get heightPx() { return this.unitPx * this.heightUnits; }

  setMode(mode, { speed = 1, activity = 0.5, anchor = null } = {}) {
    const changed = mode !== this.mode;
    this.mode = mode;
    this.speed = speed;
    this.activity = activity;
    if (mode === 'stay') {
      if (anchor && anchor.x >= 0 && anchor.y >= 0) this.anchor = { ...anchor };
      else if (changed || !this.anchor) this.anchor = this.placed ? { ...this.pos } : this.defaultAnchor();
      this.pos = { ...this.anchor };
      this.clampToBounds();
      this.anchor = { ...this.pos };
    }
    if (changed && !['dragged', 'sleep'].includes(this.state)) {
      if (mode === 'ground' && !this.supported()) this.enter('falling');
      else this.enter('idle');
    }
  }

  defaultAnchor() {
    return { x: this.bounds.right - 140, y: this.bounds.floorY };
  }

  setPlatform(rect) {
    const b = this.bounds;
    let p = null;
    if (rect && rect.w > 160 && rect.y > b.top + 90 && rect.y < b.floorY - 80) {
      p = { x: Math.max(rect.x + 12, b.left), y: rect.y, w: Math.min(rect.w - 24, b.right - rect.x - 12) };
    }
    const prev = this.platform;
    this.platform = p;
    if (this.mode !== 'ground' || !this.onPlatform) return;
    // Perched: follow the window if it moved under us, otherwise fall.
    if (p && prev) {
      const nx = this.pos.x + (p.x - prev.x);
      if (nx >= p.x && nx <= p.x + p.w) {
        this.pos.x = nx;
        this.pos.y = p.y;
        return;
      }
    }
    this.onPlatform = false;
    if (this.state !== 'dragged') this.enter('falling');
  }

  // ---------- helpers ----------

  clampToBounds() {
    const b = this.bounds;
    const half = this.unitPx * 0.6;
    this.pos.x = Math.max(b.left + half, Math.min(b.right - half, this.pos.x));
    this.pos.y = Math.max(b.top + this.heightPx + 10, Math.min(b.floorY, this.pos.y));
  }

  supported() {
    if (Math.abs(this.pos.y - this.bounds.floorY) < 2) return true;
    const p = this.platform;
    return !!(p && Math.abs(this.pos.y - p.y) < 2 && this.pos.x >= p.x && this.pos.x <= p.x + p.w);
  }

  surfaceRange() {
    if (this.onPlatform && this.platform) return [this.platform.x + 20, this.platform.x + this.platform.w - 20];
    const half = this.unitPx * 0.7;
    return [this.bounds.left + half, this.bounds.right - half];
  }

  play(name, opts) {
    if (!this.player.play(name, opts)) this.player.play('idle', opts);
  }

  enter(state, data = {}) {
    this.state = state;
    this.onStateChange(state);
    const act = 0.4 + this.activity * 1.2; // higher activity → shorter idles
    switch (state) {
      case 'idle':
        this.timer = rand(2.5, 7) / act;
        this.dir = 0;
        this.play(this.mode === 'free' ? 'float' : 'idle');
        break;
      case 'walk':
        this.target = data.target;
        this.play(this.mode === 'free' ? 'float' : 'walk');
        break;
      case 'sit':
        this.timer = rand(5, 12);
        this.dir = 0;
        this.play(this.mode === 'free' ? 'float' : 'sit');
        break;
      case 'sleep':
        this.dir = 0;
        this.play('sleep', { fade: 0.6 });
        break;
      case 'react':
        this.dir = 0;
        this.play(data.anim, { restart: true, onDone: () => { if (this.state === 'react') this.afterReact(); } });
        this.timer = 6; // safety
        break;
      case 'dragged':
        this.dir = 0;
        this.play('dangle', { fade: 0.15 });
        break;
      case 'falling':
        this.onPlatform = false;
        this.play('fall', { fade: 0.15 });
        break;
      case 'land':
        this.play('land', { restart: true, fade: 0.08, onDone: () => { if (this.state === 'land') this.enter('idle'); } });
        this.timer = 2;
        break;
      case 'hop':
        this.jump = data;
        this.play('jump', { restart: true, fade: 0.1 });
        break;
      case 'talk':
        this.dir = 0;
        if (data.anim) this.play(data.anim, { restart: true, onDone: () => this.play(this.mode === 'free' ? 'float' : 'idle') });
        else this.play(this.mode === 'free' ? 'float' : 'idle');
        break;
    }
  }

  afterReact() {
    if (this.away) return this.enter('sleep');
    if (this.mode === 'ground' && !this.supported()) return this.enter('falling');
    this.enter('idle');
  }

  // ---------- external events ----------

  setAway(away) {
    this.away = away;
    if (away && !['dragged', 'falling', 'hop'].includes(this.state)) this.enter('sleep');
    if (!away && this.state === 'sleep') this.react('surprised');
  }

  react(anim) {
    if (['dragged', 'falling', 'hop'].includes(this.state)) return;
    if (!this.player.has(anim)) return;
    this.enter('react', { anim });
  }

  talk(seconds, anim) {
    this.talkUntil = this.time + seconds;
    if (['dragged', 'falling', 'hop'].includes(this.state)) return;
    this.enter('talk', { anim: anim && this.player.has(anim) ? anim : null });
  }

  startDrag() {
    this.enter('dragged');
    this.onPlatform = false;
    this.vel = { x: 0, y: 0 };
  }

  dragTo(x, y) {
    this.pos.x = x;
    this.pos.y = y;
    const b = this.bounds;
    this.pos.x = Math.max(b.left, Math.min(b.right, this.pos.x));
    this.pos.y = Math.max(b.top + this.heightPx, Math.min(b.floorY + this.heightPx * 0.3, this.pos.y));
  }

  endDrag(vx, vy) {
    if (this.mode === 'stay') {
      this.clampToBounds();
      this.anchor = { ...this.pos };
      this.enter('react', { anim: 'land' });
      return { anchor: this.anchor };
    }
    if (this.mode === 'free') {
      this.clampToBounds();
      this.vel = { x: vx * 0.3, y: vy * 0.3 };
      this.enter('idle');
      return {};
    }
    this.vel = { x: Math.max(-900, Math.min(900, vx)), y: Math.max(-900, Math.min(600, vy)) };
    this.enter('falling');
    return {};
  }

  // ---------- update ----------

  update(dt) {
    this.time += dt;
    const talking = this.time < this.talkUntil;
    if (this.state === 'talk' && !talking) this.enter('idle');

    switch (this.state) {
      case 'dragged':
        break;
      case 'falling':
        this.updateFalling(dt);
        break;
      case 'hop':
        this.updateHop(dt);
        break;
      case 'walk':
        this.mode === 'free' ? this.updateFly(dt) : this.updateWalk(dt);
        break;
      case 'idle':
      case 'sit':
        if (this.away) { this.enter('sleep'); break; }
        if (this.mode === 'free') this.drift(dt);
        this.timer -= dt;
        if (this.timer <= 0 && !talking) this.decide();
        break;
      case 'react':
        if (this.mode === 'free') this.drift(dt);
        this.timer -= dt;
        if (this.timer <= 0) this.afterReact();
        break;
      case 'land':
        this.timer -= dt;
        if (this.timer <= 0) this.enter('idle');
        break;
      case 'sleep':
        if (this.mode === 'free') this.drift(dt * 0.3);
        break;
    }
    if (this.mode === 'stay' && this.anchor && this.state !== 'dragged') {
      this.pos.x += (this.anchor.x - this.pos.x) * Math.min(1, dt * 8);
      this.pos.y += (this.anchor.y - this.pos.y) * Math.min(1, dt * 8);
    }
    const lean = this.state === 'dragged' ? 0 : this.dir * 0.08;
    this.setFacing(this.dir, lean);
  }

  decide() {
    if (this.mode === 'stay') {
      const r = Math.random();
      if (r < 0.35) this.enter('react', { anim: 'look_around' });
      else if (r < 0.55) this.enter('sit');
      else this.enter('idle');
      return;
    }
    if (this.mode === 'free') {
      const b = this.bounds;
      const m = this.unitPx;
      this.enter('walk', {
        target: { x: rand(b.left + m, b.right - m), y: rand(b.top + this.heightPx + 40, b.floorY - 20) },
      });
      return;
    }
    // ground
    const r = Math.random();
    const p = this.platform;
    if (p && !this.onPlatform && r < 0.12 && Math.abs(p.y - this.pos.y) < 700) {
      const tx = rand(p.x + 30, p.x + p.w - 30);
      this.enter('hop', { from: { ...this.pos }, to: { x: tx, y: p.y }, t: 0, dur: 0.9, platform: true });
      return;
    }
    if (this.onPlatform && r < 0.08) {
      // hop back down to the floor
      const tx = this.pos.x + rand(-160, 160);
      this.enter('hop', { from: { ...this.pos }, to: { x: tx, y: this.bounds.floorY }, t: 0, dur: 0.9, platform: false });
      return;
    }
    if (r < 0.62) {
      const [lo, hi] = this.surfaceRange();
      let tx = this.pos.x + rand(-420, 420);
      tx = Math.max(lo, Math.min(hi, tx));
      if (Math.abs(tx - this.pos.x) > 30) return this.enter('walk', { target: { x: tx, y: this.pos.y } });
    }
    if (r < 0.8) return this.enter('sit');
    this.enter('react', { anim: pick(['look_around', 'look_around', 'jump', 'wave']) });
  }

  updateWalk(dt) {
    const dx = this.target.x - this.pos.x;
    const step = 75 * this.speed * dt;
    this.dir = Math.sign(dx);
    if (Math.abs(dx) <= step) {
      this.pos.x = this.target.x;
      this.enter('idle');
      return;
    }
    this.pos.x += step * this.dir;
    if (this.onPlatform && this.platform) {
      const p = this.platform;
      if (this.pos.x < p.x || this.pos.x > p.x + p.w) { this.enter('falling'); }
    }
  }

  updateFly(dt) {
    const dx = this.target.x - this.pos.x;
    const dy = this.target.y - this.pos.y;
    const dist = Math.hypot(dx, dy);
    const maxV = 85 * this.speed;
    if (dist < 6) { this.enter('idle'); return; }
    const desired = { x: (dx / dist) * Math.min(maxV, dist * 1.5), y: (dy / dist) * Math.min(maxV, dist * 1.5) };
    this.vel.x += (desired.x - this.vel.x) * Math.min(1, dt * 2.5);
    this.vel.y += (desired.y - this.vel.y) * Math.min(1, dt * 2.5);
    this.pos.x += this.vel.x * dt;
    this.pos.y += this.vel.y * dt;
    this.dir = Math.abs(this.vel.x) > 10 ? Math.sign(this.vel.x) : 0;
    this.clampToBounds();
  }

  drift(dt) {
    this.vel.x *= Math.max(0, 1 - dt * 2);
    this.vel.y *= Math.max(0, 1 - dt * 2);
    this.pos.x += this.vel.x * dt;
    this.pos.y += this.vel.y * dt + Math.sin(this.time * 1.3) * 4 * dt;
    this.clampToBounds();
  }

  updateFalling(dt) {
    if (this.mode !== 'ground') { this.enter('idle'); return; }
    const prevY = this.pos.y;
    this.vel.y += GRAVITY * dt;
    this.pos.x += this.vel.x * dt;
    this.pos.y += this.vel.y * dt;
    this.vel.x *= Math.max(0, 1 - dt * 0.6);
    const b = this.bounds;
    const half = this.unitPx * 0.6;
    if (this.pos.x < b.left + half) { this.pos.x = b.left + half; this.vel.x = Math.abs(this.vel.x) * 0.5; }
    if (this.pos.x > b.right - half) { this.pos.x = b.right - half; this.vel.x = -Math.abs(this.vel.x) * 0.5; }
    const p = this.platform;
    if (this.vel.y > 0 && p && prevY <= p.y && this.pos.y >= p.y && this.pos.x >= p.x && this.pos.x <= p.x + p.w) {
      this.pos.y = p.y;
      this.onPlatform = true;
      this.vel = { x: 0, y: 0 };
      this.enter('land');
      return;
    }
    if (this.pos.y >= b.floorY) {
      this.pos.y = b.floorY;
      this.onPlatform = false;
      this.vel = { x: 0, y: 0 };
      this.enter('land');
    }
    if (this.pos.y < b.top + this.heightPx) { this.pos.y = b.top + this.heightPx; this.vel.y = Math.abs(this.vel.y) * 0.3; }
  }

  updateHop(dt) {
    const j = this.jump;
    j.t += dt / j.dur;
    const t = Math.min(1, j.t);
    const peak = Math.max(90, (j.from.y - j.to.y) + 90);
    this.pos.x = j.from.x + (j.to.x - j.from.x) * t;
    this.pos.y = j.from.y + (j.to.y - j.from.y) * t - Math.sin(Math.PI * t) * peak;
    this.dir = Math.sign(j.to.x - j.from.x);
    if (t >= 1) {
      // Landing target might have moved/vanished; verify support.
      this.pos.y = j.to.y;
      this.onPlatform = j.platform && !!this.platform && Math.abs(this.platform.y - j.to.y) < 4;
      if (j.platform && !this.onPlatform) { this.enter('falling'); return; }
      this.enter('land');
    }
  }
}
