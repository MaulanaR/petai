import './style.css';
import { api, on, call } from './bridge.js';
import { createCharacter } from './characters/index.js';
import { AnimPlayer } from './anim/player.js';
import { PetView } from './scene.js';
import { PetBehavior } from './behavior/pet.js';
import { Bubble } from './ui/bubble.js';
import { Menu } from './ui/menu.js';
import { Effects } from './ui/effects.js';
import { Settings } from './ui/settings.js';
import { setLang, t, line } from './i18n.js';

const BASE_UNIT = 110; // px per world unit at scale 1
const MOOD_FACE = {
  neutral: ['neutral', 'smile'], happy: ['happy', 'smile'], excited: ['happy', 'open'],
  curious: ['neutral', 'o'], sleepy: ['sleepy', 'neutral'], concerned: ['neutral', 'frown'],
  sad: ['sleepy', 'frown'], love: ['love', 'smile'],
};
const MOOD_ANIM = { happy: 'happy_bounce', excited: 'jump', curious: 'look_around', love: 'happy_bounce', sleepy: 'sit' };

const root = document.getElementById('app');
window.addEventListener('error', (e) => api.LogFrontend(`${e.message} @ ${e.filename}:${e.lineno}`));
window.addEventListener('unhandledrejection', (e) => api.LogFrontend('unhandled: ' + (e.reason?.stack || e.reason)));
const S = {
  cfg: null,
  specs: [],
  monitor: null,
  char: null,
  player: null,
  cursor: { x: -9999, y: -9999 },
  fps: 30,
  down: null,
  dragging: false,
  dragVel: { x: 0, y: 0 },
  lastDrag: null,
  clicks: [],
  clickTimer: null,
  petting: { dist: 0, since: 0 },
  lastRegions: '',
  lastRegionSend: 0,
  lastReport: 0,
  focus: false,
  facingY: 0,
  hidden: false,
};

const view = new PetView(root);
const fx = new Effects(root);
const bubble = new Bubble(root, { onSend: sendChat, onClose: () => updateFocus(), t });
const menu = new Menu(root, t);
const settings = new Settings(root, {
  getConfig: () => S.cfg,
  onSaved: (c) => applyConfig(c),
  onPlay: (name) => {
    if (name === '__reset_pos') return resetPosition();
    pet.react(name);
  },
  onOpenChange: () => updateFocus(),
});

const pet = new PetBehavior({
  player: { play: () => false, has: () => false },
  setFacing: (dir, lean) => {
    S.targetFacing = dir * 0.55;
    S.lean = lean;
  },
  onStateChange: () => {},
});

// ---------------- character ----------------

function buildCharacter() {
  const c = S.cfg.pet;
  if (S.player) S.player.dispose();
  S.char = createCharacter(c.character, c.color);
  view.setCharacter(S.char);
  S.player = new AnimPlayer(S.char, {
    onExpression: (eyes, mouth) => S.char.face.setExpression(eyes, mouth),
    onEffect: (type) => {
      const head = headPoint();
      fx.spawn(type, head.x, head.y);
    },
  });
  S.player.load(S.specs);
  pet.player = S.player;
  pet.setSize(view.unitPx, S.char.height);
  const st = pet.state;
  pet.enter(st === 'dragged' || st === 'falling' || st === 'sleep' ? st : 'idle');
}

async function reloadSpecs(character) {
  const [specs] = await call(api.GetAnimations, character);
  if (specs) S.specs = specs;
}

function headPoint() {
  return view.toScreen(0, (S.char ? S.char.height : 1) * 1.02);
}

// ---------------- config / bounds ----------------

function bounds() {
  // The overlay window covers exactly the monitor's work area, so the viewport is the world.
  const w = window.innerWidth;
  const h = window.innerHeight;
  return { width: w, height: h, left: 0, right: w, top: 0, floorY: h };
}

async function applyConfig(c, first = false) {
  const prev = S.cfg;
  S.cfg = c;
  setLang(c.pet.language);
  S.fps = c.general.fps || 30;
  const charChanged = !prev || prev.pet.character !== c.pet.character || prev.pet.color !== c.pet.color;
  const unit = Math.round(BASE_UNIT * (c.pet.scale || 1));
  if (unit !== view.unitPx || first) view.setUnit(unit);
  if (charChanged) {
    if (prev && prev.pet.character !== c.pet.character) await reloadSpecs(c.pet.character);
    buildCharacter();
  }
  pet.setSize(view.unitPx, S.char.height);
  const mv = c.movement;
  pet.setMode(mv.mode, { speed: mv.speed, activity: mv.activity, anchor: mv.anchorX >= 0 ? { x: mv.anchorX, y: mv.anchorY } : null });
  // Persist where "stay" anchored the pet so it survives restarts.
  if (mv.mode === 'stay' && mv.anchorX < 0 && pet.placed && pet.anchor) saveAnchor(pet.anchor);
  settings.external(c);
}

function applyBounds() {
  pet.setBounds(bounds());
}

function resetPosition() {
  const b = bounds();
  pet.vel = { x: 0, y: 0 };
  if (pet.mode === 'stay') {
    pet.anchor = pet.defaultAnchor();
    pet.pos = { ...pet.anchor };
  } else if (pet.mode === 'free') {
    pet.pos = { x: b.right - 220, y: b.floorY - 160 };
  } else {
    pet.pos = { x: b.right - 220, y: b.top + 200 };
    pet.enter('falling');
  }
}

// ---------------- AI actions ----------------

function setMood(mood) {
  const f = MOOD_FACE[mood];
  if (f && S.char) S.char.face.setExpression(f[0], f[1]);
}

function handleAction({ action }) {
  if (!action) return;
  const anim = action.animation || (action.speech ? MOOD_ANIM[action.mood] : '');
  if (action.speech || action.suggestion) {
    bubble.say(action.speech, action.suggestion);
    const secs = Math.min(14, 4 + ((action.speech || '').length + (action.suggestion || '').length) / 14);
    pet.talk(secs, anim);
  } else if (anim) {
    pet.react(anim);
  }
  setTimeout(() => setMood(action.mood), 60);
}

async function sendChat(text) {
  const [, err] = await call(api.Chat, text);
  if (err) {
    bubble.setThinking(false);
    bubble.say(/API key/i.test(err) ? t('noKey') : t('aiError') + err, '', 8);
  }
}

function openChat() {
  menu.close();
  bubble.openChat();
  updateFocus();
}

function updateFocus() {
  const want = bubble.chatOpen || settings.open;
  if (want !== S.focus) {
    S.focus = want;
    api.SetFocusable(want);
  }
  if (bubble.chatOpen) setTimeout(() => bubble.input.focus(), 50);
}

// ---------------- input ----------------

function petRect() {
  return view.petRect(4);
}

function inRect(r, x, y) {
  return r && x >= r.x && x <= r.x + r.w && y >= r.y && y <= r.y + r.h;
}

window.addEventListener('mousedown', (e) => {
  if (e.button !== 0 || !inRect(petRect(), e.clientX, e.clientY)) return;
  e.preventDefault();
  S.down = { x: e.clientX, y: e.clientY, t: performance.now(), ox: pet.pos.x - e.clientX, oy: pet.pos.y - e.clientY };
  api.SetForceInteractive(true);
});

window.addEventListener('mousemove', (e) => {
  S.cursor = { x: e.clientX, y: e.clientY };
  if (S.down) {
    const d = Math.hypot(e.clientX - S.down.x, e.clientY - S.down.y);
    if (!S.dragging && d > 5) {
      S.dragging = true;
      menu.close();
      pet.startDrag();
      S.lastDrag = { x: e.clientX, y: e.clientY, t: performance.now() };
    }
    if (S.dragging) {
      const now = performance.now();
      const dt = Math.max(1, now - S.lastDrag.t) / 1000;
      S.dragVel = { x: (e.clientX - S.lastDrag.x) / dt * 0.5 + S.dragVel.x * 0.5, y: (e.clientY - S.lastDrag.y) / dt * 0.5 + S.dragVel.y * 0.5 };
      S.lastDrag = { x: e.clientX, y: e.clientY, t: now };
      // hang below the cursor
      pet.dragTo(e.clientX, e.clientY + pet.heightPx * 0.55);
    }
    return;
  }
  // petting: rub the cursor over the pet
  if (e.buttons === 0 && inRect(petRect(), e.clientX, e.clientY)) {
    const now = performance.now();
    if (now - S.petting.since > 2000) S.petting = { dist: 0, since: now };
    S.petting.dist += Math.abs(e.movementX) + Math.abs(e.movementY);
    if (S.petting.dist > 900) {
      S.petting = { dist: 0, since: now + 4000 };
      S.char.face.setExpression('love', 'smile');
      const h = headPoint();
      fx.spawn('hearts', h.x, h.y);
      pet.react('happy_bounce');
      if (Math.random() < 0.6) bubble.say(line('petLines'), '', 2.5);
    }
  }
});

window.addEventListener('mouseup', (e) => {
  if (e.button !== 0 || !S.down) return;
  const wasDrag = S.dragging;
  S.down = null;
  S.dragging = false;
  api.SetForceInteractive(false);
  if (wasDrag) {
    const res = pet.endDrag(S.dragVel.x, S.dragVel.y);
    S.dragVel = { x: 0, y: 0 };
    if (Math.random() < 0.3) bubble.say(line('dropLines'), '', 2);
    if (res.anchor) saveAnchor(res.anchor);
    return;
  }
  // click / double click
  clearTimeout(S.clickTimer);
  const now = performance.now();
  S.clicks = S.clicks.filter((t0) => now - t0 < 2500);
  S.clicks.push(now);
  if (S.clicks.length >= 2 && now - S.clicks[S.clicks.length - 2] < 320) {
    S.clicks = [];
    openChat();
    return;
  }
  S.clickTimer = setTimeout(() => {
    pet.react(['happy_bounce', 'surprised', 'jump', 'wave'][Math.floor(Math.random() * 4)]);
    if (Math.random() < 0.4 && !bubble.open) bubble.say(line('clickLines'), '', 2.2);
    api.PetClicked(S.clicks.length);
  }, 330);
});

window.addEventListener('contextmenu', (e) => {
  e.preventDefault();
  if (!inRect(petRect(), e.clientX, e.clientY)) return;
  openMenu(e.clientX + 8, e.clientY);
});

window.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') {
    if (settings.open) settings.hide();
    menu.close();
  }
});

function openMenu(x, y) {
  const c = S.cfg;
  const setMode = (mode) => saveConfig((cfg) => { cfg.movement.mode = mode; cfg.movement.anchorX = -1; cfg.movement.anchorY = -1; });
  const setChar = (id) => saveConfig((cfg) => { cfg.pet.character = id; });
  menu.open(x, y, [
    { label: t('menuChat'), onClick: openChat },
    { label: pet.state === 'sleep' ? t('menuWake') : t('menuSleep'), onClick: () => (pet.state === 'sleep' ? pet.setAway(false) : pet.enter('sleep')) },
    { sep: true },
    { label: t('menuMode'), row: [
      { label: '📍 ' + t('modeStay'), checked: c.movement.mode === 'stay', onClick: () => setMode('stay') },
      { label: '🚶 ' + t('modeGround'), checked: c.movement.mode === 'ground', onClick: () => setMode('ground') },
      { label: '🎈 ' + t('modeFree'), checked: c.movement.mode === 'free', onClick: () => setMode('free') },
    ] },
    { label: t('menuChar'), row: [
      { label: '🫧', title: 'Blob', checked: c.pet.character === 'blob', onClick: () => setChar('blob') },
      { label: '🐱', title: 'Kucing', checked: c.pet.character === 'cat', onClick: () => setChar('cat') },
      { label: '🐥', title: 'Anak ayam', checked: c.pet.character === 'chick', onClick: () => setChar('chick') },
    ] },
    { sep: true },
    { label: t('menuSettings'), onClick: () => settings.show() },
    { label: t('menuHide'), onClick: () => api.HideFor(60) },
    { label: t('menuQuit'), onClick: () => api.Quit() },
  ]);
}

async function saveConfig(mut) {
  const next = structuredClone(S.cfg);
  mut(next);
  const [saved] = await call(api.SaveConfig, next);
  if (saved) applyConfig(saved);
}

function saveAnchor(a) {
  saveConfig((cfg) => { cfg.movement.anchorX = Math.round(a.x); cfg.movement.anchorY = Math.round(a.y); });
}

// ---------------- Go events ----------------

on('monitor', (m) => { S.monitor = m; applyBounds(); });
on('cursor', (x, y) => {
  S.cursor = { x, y };
  menu.trackCursor(x, y, petRect());
});
on('pet:action', handleAction);
on('pet:anim-added', (spec) => {
  if (spec.target === 'generic' || spec.target === S.cfg.pet.character) {
    S.specs = S.specs.filter((s) => s.name !== spec.name).concat(spec);
    S.player.load([spec]);
  }
});
on('pet:play', (name) => pet.react(name));
on('ai:thinking', (onOff) => { if (bubble.chatOpen) bubble.setThinking(onOff); });
on('ai:error', (msg) => {
  if (!bubble.chatOpen) return;
  bubble.setThinking(false);
  bubble.say(/API key/i.test(msg) ? t('noKey') : t('aiError') + msg, '', 8);
});
on('pet:watching', (onOff) => {
  if (S.char && S.char.glasses) S.char.glasses.visible = onOff;
});
on('fg:window', (rect) => pet.setPlatform(rect));
on('user:presence', ({ away }) => {
  pet.setAway(away);
  if (!away && Math.random() < 0.5) bubble.say(line('wakeLines'), '', 3);
});
on('config:changed', (c) => applyConfig(c));
on('ui:open', (what) => {
  if (what === 'settings') return settings.show();
  if (what === 'menu') {
    const r = petRect();
    return r && openMenu(r.x + r.w, r.y);
  }
  openChat();
});
on('memory:changed', () => { if (settings.open && settings.tab === 'memory') settings.loadMemories(); });
window.addEventListener('resize', () => {
  applyBounds();
  S.lastReport = 0; // push the new devicePixelRatio to Go right away
});

// ---------------- loop ----------------

let last = performance.now();
function frame(now) {
  requestAnimationFrame(frame);
  const dt = (now - last) / 1000;
  const fps = pet.state === 'sleep' ? Math.min(S.fps, 15) : S.fps;
  if (dt < (1 / fps) * 0.92) return;
  last = now;
  const d = Math.min(dt, 0.1);

  pet.update(d);
  S.player.update(d);
  const face = S.char.face;
  // look toward the cursor when it is near
  const head = headPoint();
  const dx = S.cursor.x - head.x, dy = S.cursor.y - head.y;
  const near = Math.hypot(dx, dy) < 600 && pet.state !== 'sleep';
  face.lookAt(near ? dx / 280 : 0, near ? -dy / 280 : 0);
  face.update(d);
  const look = S.char.slots.head && S.char.slots.head.getObjectByName('look');
  if (look) {
    const turn = S.char.lookTurn ?? 1; // face-on-body characters barely turn
    const ty = near ? Math.max(-0.45, Math.min(0.45, dx / 700)) * turn : 0;
    const tx = near ? Math.max(-0.25, Math.min(0.25, dy / 900)) * turn : 0;
    look.rotation.y += (ty - look.rotation.y) * Math.min(1, d * 5);
    look.rotation.x += (tx - look.rotation.x) * Math.min(1, d * 5);
  }
  S.facingY += ((S.targetFacing || 0) - S.facingY) * Math.min(1, d * 6);
  S.char.facing.rotation.y = S.facingY;
  S.char.facing.rotation.z = -(S.lean || 0);

  view.place(pet.pos.x, pet.pos.y);
  view.wobble(d);
  view.render();
  bubble.place(head.x, head.y, pet.pos.y, bounds().width);

  // hit regions → Go (throttled)
  if (now - S.lastRegionSend > 60) {
    const regions = [petRect(), bubble.rect(), menu.rect(), settings.rect()].filter(Boolean)
      .map((r) => ({ x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.w), h: Math.round(r.h) }));
    const key = JSON.stringify(regions);
    if (key !== S.lastRegions) {
      S.lastRegions = key;
      api.SetHitRegions(regions);
    }
    S.lastRegionSend = now;
  }
  if (now - S.lastReport > 250) {
    S.lastReport = now;
    const r = petRect() || { x: 0, y: 0, w: 0, h: 0 };
    api.ReportPetState({
      x: r.x, y: r.y, w: r.w, h: r.h, character: S.char.id, mode: pet.mode,
      state: pet.state, animation: S.player.name, visible: true,
      viewW: window.innerWidth, viewH: window.innerHeight,
      scrollY: (document.scrollingElement || document.documentElement).scrollTop + (window.visualViewport ? window.visualViewport.offsetTop : 0),
      dpr: window.devicePixelRatio,
    });
  }
}

// ---------------- boot ----------------

async function boot() {
  const [b, err] = await call(api.GetBootstrap);
  if (err || !b) {
    console.error('bootstrap failed', err);
    return;
  }
  S.specs = b.animations || [];
  if (b.monitor && b.monitor.sizeCss && b.monitor.sizeCss.w) S.monitor = b.monitor;
  const [m] = await call(api.GetMonitor);
  if (m && m.sizeCss && m.sizeCss.w) S.monitor = m;
  applyBounds();
  await applyConfig(b.config, true);
  applyBounds();
  // entrance: drop in (ground), appear (free), or sit at the anchor (stay)
  const bd = bounds();
  if (pet.mode === 'ground') {
    pet.pos = { x: bd.right - 260 - Math.random() * 300, y: bd.top + 220 };
    pet.enter('falling');
  } else if (pet.mode === 'free') {
    pet.pos = { x: bd.right - 260, y: bd.floorY - 180 };
    pet.enter('react', { anim: 'wave' });
  } else {
    pet.enter('react', { anim: 'wave' });
  }
  pet.placed = true;
  if (b.initError) bubble.say('⚠️ ' + b.initError, '', 10);
  requestAnimationFrame(frame);
}

boot();
