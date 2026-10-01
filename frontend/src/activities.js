import { soccerBall, basketball, hoop, golfHole, golfBall, golfClub, outhouse, puff, poofCloud } from './props.js';

// Scripted mini-scenes with props. Each scene takes over the pet (behavior state "activity"),
// spawns props on the PropStage, plays animations/expressions/effects/lines and cleans up.
// Any user grab/click aborts the scene immediately.

const ease = {
  out: (p) => 1 - (1 - p) * (1 - p),
  in: (p) => p * p,
  inOut: (p) => (p < 0.5 ? 2 * p * p : 1 - Math.pow(-2 * p + 2, 2) / 2),
  back: (p) => { const c = 1.70158; return 1 + (c + 1) * Math.pow(p - 1, 3) + c * Math.pow(p - 1, 2); },
  bounce: (p) => {
    const n = 7.5625, d = 2.75;
    if (p < 1 / d) return n * p * p;
    if (p < 2 / d) return n * (p -= 1.5 / d) * p + 0.75;
    if (p < 2.5 / d) return n * (p -= 2.25 / d) * p + 0.9375;
    return n * (p -= 2.625 / d) * p + 0.984375;
  },
};
const lerp = (a, b, p) => a + (b - a) * p;

class Aborted extends Error {}

export const ACTIVITIES = ['football', 'basketball', 'golf', 'toilet'];

export class ActivityRunner {
  /**
   * deps: { pet, view, stage, bubble, fx, line, getChar, getPlayer, bounds, setPetHidden, onEnd }
   */
  constructor(deps) {
    this.d = deps;
    this.cur = null;
    this.tasks = [];
  }

  get running() { return !!this.cur; }
  get name() { return this.cur ? this.cur.name : ''; }

  start(name) {
    if (this.cur || !SCENES[name]) return false;
    const cur = { name, aborted: false, cleanups: [] };
    this.cur = cur;
    this.d.pet.enter('activity');
    SCENES[name](this)
      .then(() => this.walkHome())
      .catch((e) => { if (!(e instanceof Aborted)) console.error(e); })
      .finally(() => this.finish(cur));
    return true;
  }

  abort() {
    if (!this.cur) return;
    this.cur.aborted = true;
    for (const t of this.tasks) t.reject(new Aborted());
    this.tasks = [];
  }

  finish(cur) {
    this.tasks = []; // drop fire-and-forget tweens (door, puffs…) of the finished scene
    for (const f of cur.cleanups.reverse()) { try { f(); } catch (e) { console.error(e); } }
    this.d.stage.end();
    this.d.setPetHidden(false);
    if (this.cur === cur) this.cur = null;
    const pet = this.d.pet;
    if (pet.state === 'activity') pet.afterReact();
    this.d.onEnd && this.d.onEnd(cur.name, cur.aborted);
  }

  update(dt) {
    if (!this.tasks.length) return;
    const done = [];
    for (const t of this.tasks) {
      t.t += dt * 1000;
      const p = Math.min(1, t.t / t.dur);
      t.fn(p);
      if (p >= 1) done.push(t);
    }
    if (done.length) {
      this.tasks = this.tasks.filter((t) => !done.includes(t));
      for (const t of done) t.resolve();
    }
  }

  // ---------- helpers used by scenes ----------

  check() { if (!this.cur || this.cur.aborted) throw new Aborted(); }
  onCleanup(f) { this.cur.cleanups.push(f); }

  tween(ms, fn = () => {}) {
    this.check();
    return new Promise((resolve, reject) => this.tasks.push({ t: 0, dur: Math.max(1, ms), fn, resolve, reject }));
  }

  wait(ms) { return this.tween(ms); }

  get u() { return this.d.view.unitPx; }
  get pet() { return this.d.pet; }

  play(name, opts = {}) {
    this.check();
    return new Promise((resolve) => {
      const ok = this.d.getPlayer().play(name, { restart: true, fade: 0.15, ...opts, onDone: () => resolve() });
      if (!ok) resolve();
      // Safety net: another clip may replace this one before it finishes.
      setTimeout(resolve, 4000);
    });
  }

  loop(name) { this.check(); this.d.getPlayer().play(name, { restart: true, fade: 0.2 }); }

  face(eyes, mouth) { this.d.getChar().face.setExpression(eyes, mouth); }

  say(key, secs = 3) { if (!this.d.bubble.voiceOpen) this.d.bubble.say(this.d.line(key), '', secs); }

  fx(type, sx, sy) { this.d.fx.spawn(type, sx, sy); }

  head() { const p = this.pet.pos; return { x: p.x, y: p.y - this.pet.heightPx * 1.05 }; }

  async walkTo(x, speedUnits = 1.6) {
    const pet = this.pet;
    const from = pet.pos.x;
    const dist = Math.abs(x - from);
    if (dist < 2) return;
    pet.dir = Math.sign(x - from);
    this.loop('walk');
    await this.tween((dist / (speedUnits * this.u)) * 1000, (p) => { pet.pos.x = lerp(from, x, p); });
    pet.dir = 0;
  }

  /** Picks a side with room and opens a stage covering [x0, x0+dir*span] Ã— height above the floor. */
  setup(spanUnits, heightUnits) {
    const pet = this.pet;
    const b = this.d.bounds();
    const u = this.u;
    const x0 = pet.pos.x;
    const need = (spanUnits + 0.8) * u;
    const right = b.right - x0, left = x0 - b.left;
    let dir = right >= need ? 1 : left >= need ? -1 : right >= left ? 1 : -1;
    const room = (dir > 0 ? right : left) - 0.8 * u;
    const span = Math.max(1.4, Math.min(spanUnits, room / u));
    const y0 = pet.pos.y;
    const xa = Math.min(x0, x0 + dir * span * u) - 1.2 * u;
    const xb = Math.max(x0, x0 + dir * span * u) + 1.2 * u;
    const rect = { x: Math.max(0, xa), y: Math.max(0, y0 - heightUnits * u), w: 0, h: 0 };
    rect.w = Math.min(b.width, xb) - rect.x;
    rect.h = Math.min(b.height, y0 + 0.5 * u) - rect.y;
    this.d.stage.begin(rect, u);
    return { x0, y0, dir, span, u };
  }

  async popIn(h, ms = 420) {
    h.obj.scale.setScalar(0.001);
    this.poof(h.sx, h.sy);
    await this.tween(ms, (p) => h.obj.scale.setScalar(Math.max(0.001, ease.back(p))));
  }

  async popOut(h, ms = 320) {
    this.poof(h.sx, h.sy);
    await this.tween(ms, (p) => h.obj.scale.setScalar(Math.max(0.001, 1 - ease.in(p))));
    this.d.stage.remove(h);
  }

  poof(sx, sy) {
    const st = this.d.stage;
    const cloud = poofCloud();
    const h = st.add(cloud, sx, sy);
    const t0 = { p: 0 };
    this.tasks.push({
      t: 0, dur: 450, resolve: () => st.remove(h), reject: () => st.remove(h),
      fn: (p) => { t0.p = p; cloud.scale.setScalar(0.5 + p * 0.9); cloud.userData.mat.opacity = 0.9 * (1 - p); },
    });
  }

  /** In "stay" mode the pet walks back to its spot instead of sliding there. */
  async walkHome() {
    const pet = this.pet;
    if (pet.mode !== 'stay' || !pet.anchor) return;
    await this.walkTo(pet.anchor.x, 1.8);
  }

  /** Ground-level start: in free mode the pet first floats down to the floor. */
  async toFloor() {
    const pet = this.pet;
    const floor = this.d.bounds().floorY;
    if (pet.mode !== 'free' || pet.pos.y >= floor - 2) return;
    const y = pet.pos.y;
    this.loop('float');
    await this.tween(Math.min(1500, (floor - y) * 2.5), (p) => { pet.pos.y = lerp(y, floor, ease.inOut(p)); });
  }
}

// ---------------- scenes ----------------

const SCENES = {
  async football(r) {
    await r.toFloor();
    const { x0, y0, dir, span, u } = r.setup(4.6, 3.4);
    const pet = r.pet;
    const rad = 0.17 * u;
    let bx = x0 + dir * Math.min(1.5, span * 0.4) * u;
    const ball = r.d.stage.add(soccerBall(), bx, y0 - 3.2 * u);
    r.onCleanup(() => r.d.stage.remove(ball));
    const roll = (x) => { ball.obj.rotation.z = -(x - x0) / u / 0.17; };
    pet.dir = dir;
    await r.tween(900, (p) => ball.move(bx, lerp(y0 - 3.2 * u, y0 - rad, ease.bounce(p))));
    r.play('surprised');
    r.say('footballStart', 2.5);
    await r.wait(700);
    await r.walkTo(bx - dir * 0.42 * u, 2);
    const kicks = 2;
    for (let k = 0; k < kicks; k++) {
      pet.dir = dir;
      r.play('kick');
      await r.wait(320);
      const from = bx, to = bx + dir * Math.min(1.2, span * 0.25) * u;
      await r.tween(750, (p) => { bx = lerp(from, to, ease.out(p)); ball.move(bx, y0 - rad); roll(bx); });
      await r.walkTo(bx - dir * 0.42 * u, 2.2);
    }
    pet.dir = dir;
    r.play('kick');
    await r.wait(320);
    const sx = bx, tx = bx + dir * 3 * u;
    await r.tween(900, (p) => {
      const x = lerp(sx, tx, p);
      ball.move(x, y0 - rad - Math.sin(p * Math.PI) * 1.4 * u - p * 0.6 * u);
      roll(x);
    });
    r.d.stage.remove(ball);
    r.say('footballGoal', 3);
    r.fx('sparkles', r.head().x, r.head().y);
    pet.dir = 0;
    await r.play('happy_bounce');
  },

  async basketball(r) {
    await r.toFloor();
    const { x0, y0, dir, span, u } = r.setup(2.8, 3.2);
    const pet = r.pet;
    const rad = 0.16 * u;
    const hx = x0 + dir * Math.min(2.4, span) * u;
    const hp = r.d.stage.add(hoop(-dir), hx, y0);
    r.onCleanup(() => r.d.stage.remove(hp));
    await r.popIn(hp);
    const hand = () => ({ x: pet.pos.x + dir * 0.5 * u, y: pet.pos.y - 0.4 * u });
    const ball = r.d.stage.add(basketball(), hand().x, hand().y);
    r.onCleanup(() => r.d.stage.remove(ball));
    pet.dir = dir;
    r.say('basketballStart', 2.5);
    const dribble = async (n) => {
      r.loop('dribble');
      for (let i = 0; i < n; i++) {
        const h = hand();
        await r.tween(250, (p) => ball.move(h.x, lerp(h.y, y0 - rad, ease.in(p))));
        await r.tween(250, (p) => ball.move(h.x, lerp(y0 - rad, h.y, ease.out(p))));
      }
    };
    await dribble(3);
    const rim = r.d.stage.screenOf(hp.obj.userData.rim);
    for (let attempt = 0; attempt < 3; attempt++) {
      const make = attempt > 0 || Math.random() > 0.45;
      r.play('throw');
      const start = hand();
      await r.tween(380, (p) => ball.move(start.x, lerp(start.y, y0 - 1.25 * u, ease.out(p))));
      const s = { x: start.x, y: y0 - 1.25 * u };
      if (make) {
        const t = { x: rim.x, y: rim.y - 0.12 * u };
        await r.tween(800, (p) => ball.move(lerp(s.x, t.x, p), lerp(s.y, t.y, p) - Math.sin(p * Math.PI) * 1.0 * u));
        const net = hp.obj.userData.net;
        await r.tween(380, (p) => { ball.move(t.x, lerp(t.y, y0 - rad, ease.in(p))); net.scale.y = 1 + Math.sin(p * Math.PI) * 0.35; });
        r.fx('sparkles', rim.x, rim.y);
        r.say('basketballMake', 2.6);
        pet.dir = 0;
        await r.play('happy_bounce');
        break;
      }
      const front = { x: rim.x - dir * 0.24 * u, y: rim.y - 0.05 * u };
      await r.tween(780, (p) => ball.move(lerp(s.x, front.x, p), lerp(s.y, front.y, p) - Math.sin(p * Math.PI) * 1.0 * u));
      const land = { x: rim.x - dir * 1.1 * u, y: y0 - rad };
      await r.tween(650, (p) => ball.move(lerp(front.x, land.x, p), lerp(front.y, land.y, p) - Math.sin(p * Math.PI) * 0.6 * u));
      r.say('basketballMiss', 2.2);
      r.face('sleepy', 'frown');
      r.fx('sweat', r.head().x, r.head().y);
      const h = hand();
      await r.tween(500, (p) => ball.move(lerp(land.x, h.x, ease.inOut(p)), lerp(land.y, h.y, ease.inOut(p)) - Math.sin(p * Math.PI) * 0.3 * u));
      pet.dir = dir;
      await dribble(1);
    }
    await r.wait(300);
    r.d.stage.remove(ball);
    await r.popOut(hp);
  },

  async golf(r) {
    await r.toFloor();
    const { x0, y0, dir, span, u } = r.setup(4.2, 2.0);
    const pet = r.pet;
    // hold the club in the hand on the flag's side (pet's left arm is screen-right)
    const arm = r.d.getChar().slots[dir > 0 ? 'armL' : 'armR'];
    const club = golfClub();
    club.position.set(0, -0.15, 0.06);
    arm.add(club);
    r.onCleanup(() => arm.remove(club));
    const hx = x0 + dir * Math.min(3.6, span) * u;
    const hole = r.d.stage.add(golfHole(), hx, y0);
    r.onCleanup(() => r.d.stage.remove(hole));
    await r.popIn(hole);
    let bx = x0 + dir * 0.32 * u;
    const ballY = y0 - 0.05 * u;
    const ball = r.d.stage.add(golfBall(), bx, ballY);
    r.onCleanup(() => r.d.stage.remove(ball));
    pet.dir = dir;
    r.say('golfStart', 2.5);
    r.face('neutral', 'neutral');
    await r.wait(700);
    r.play('golf_swing');
    await r.wait(1090);
    const holeInOne = Math.random() < 0.3;
    const land = holeInOne ? hx - dir * 0.35 * u : hx - dir * (0.55 + Math.random() * 0.4) * u;
    const sx = bx;
    await r.tween(1000, (p) => { bx = lerp(sx, land, p); ball.move(bx, ballY - Math.sin(p * Math.PI) * 1.3 * u); });
    const rollTo = holeInOne ? hx : land + dir * 0.25 * u;
    await r.tween(500, (p) => { bx = lerp(land, rollTo, ease.out(p)); ball.move(bx, ballY); });
    if (!holeInOne) {
      r.say('golfClose', 2);
      await r.walkTo(bx - dir * 0.36 * u, 1.8);
      pet.dir = dir;
      r.play('golf_swing', { timeScale: 1.5 });
      await r.wait(720);
      const from = bx;
      await r.tween(700, (p) => { bx = lerp(from, hx, ease.out(p)); ball.move(bx, ballY); });
    }
    await r.tween(250, (p) => { ball.obj.scale.setScalar(Math.max(0.001, 1 - p)); ball.move(hx, ballY + p * 0.06 * u); });
    r.d.stage.remove(ball);
    r.fx('sparkles', hx, y0 - 1.0 * u);
    r.say(holeInOne ? 'golfHoleInOne' : 'golfIn', 3);
    pet.dir = 0;
    await r.play('happy_bounce');
    arm.remove(club);
    await r.popOut(hole);
  },

  async toilet(r) {
    await r.toFloor();
    const { x0, y0, dir, u } = r.setup(1.8, 2.3);
    const pet = r.pet;
    r.loop('mulas');
    r.say('toiletStart', 3);
    await r.wait(2600);
    const bx = x0 + dir * 1.4 * u;
    const booth = r.d.stage.add(outhouse(dir), bx, y0);
    r.onCleanup(() => r.d.stage.remove(booth));
    await r.popIn(booth, 480);
    r.say('toiletGo', 2);
    await r.walkTo(bx - dir * 0.62 * u, 2.4);
    r.face('pain', 'wavy');
    const door = booth.obj.userData.door;
    await r.tween(320, (p) => { door.rotation.y = -1.9 * ease.out(p); });
    await r.walkTo(bx, 2.4);
    r.d.setPetHidden(true);
    await r.tween(260, (p) => { door.rotation.y = -1.9 * (1 - p); });
    r.say('toiletInside', 2);
    const vent = booth.obj.userData.vent;
    for (let i = 0; i < 6; i++) {
      const v = r.d.stage.screenOf(vent);
      const pf = r.d.stage.add(puff(), v.x, v.y - 0.12 * u);
      const drift = (Math.random() - 0.5) * 0.3 * u;
      r.tasks.push({
        t: 0, dur: 1300, resolve: () => r.d.stage.remove(pf), reject: () => r.d.stage.remove(pf),
        fn: (p) => { pf.move(v.x + drift * p, v.y - 0.12 * u - p * 0.9 * u); pf.obj.material.opacity = 0.75 * (1 - p); pf.obj.scale.setScalar(1 + p); },
      });
      await r.tween(550, (p) => { booth.holder.rotation.z = Math.sin(p * Math.PI * 4) * 0.035; });
      if (i === 3) r.say('toiletPlung', 1.6);
    }
    booth.holder.rotation.z = 0;
    await r.tween(320, (p) => { door.rotation.y = -1.9 * ease.out(p); });
    r.d.setPetHidden(false);
    await r.walkTo(bx - dir * 1.0 * u, 1.8);
    pet.dir = 0;
    r.tween(260, (p) => { door.rotation.y = -1.9 * (1 - p); });
    r.say('toiletDone', 3);
    await r.play('relieved');
    await r.popOut(booth, 380);
  },
};
