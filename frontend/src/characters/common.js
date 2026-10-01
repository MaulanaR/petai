import * as THREE from 'three';

// Shared toon "doodle" look: 3-step gradient shading + inverted-hull outlines that wobble
// slightly (like a hand-drawn line boiling) + cheeks and an expressive face.

export const INK = 0x2b2540;

let gradient;
function toonGradient() {
  if (gradient) return gradient;
  const data = new Uint8Array([90, 90, 90, 255, 175, 175, 175, 255, 255, 255, 255, 255]);
  gradient = new THREE.DataTexture(data, 3, 1, THREE.RGBAFormat);
  gradient.minFilter = THREE.NearestFilter;
  gradient.magFilter = THREE.NearestFilter;
  gradient.needsUpdate = true;
  return gradient;
}

export function toon(color) {
  return new THREE.MeshToonMaterial({ color, gradientMap: toonGradient() });
}

const inkMat = new THREE.MeshBasicMaterial({ color: INK });
const outlineMat = new THREE.MeshBasicMaterial({ color: INK, side: THREE.BackSide });

export function inkMaterial() { return inkMat; }

/** Adds a mesh with an inverted-hull outline; returns the mesh. */
export function part(geometry, material, { outline = 0.045, name } = {}) {
  const mesh = new THREE.Mesh(geometry, material);
  if (name) mesh.name = name;
  if (outline > 0) {
    const hull = new THREE.Mesh(geometry, outlineMat);
    hull.userData.outline = outline;
    hull.scale.setScalar(1 + outline);
    hull.renderOrder = -1;
    mesh.add(hull);
  }
  return mesh;
}

/** Collects outline hulls so the rig can wobble them for the doodle effect. */
export function collectOutlines(root) {
  const list = [];
  root.traverse((o) => { if (o.userData && o.userData.outline) list.push(o); });
  return list;
}

export function slot(name, parent, pos = [0, 0, 0]) {
  const g = new THREE.Group();
  g.name = 'slot_' + name;
  g.position.set(pos[0], pos[1], pos[2]);
  parent.add(g);
  return g;
}

/** Colour helpers */
export function shade(hex, f) {
  const c = new THREE.Color(hex);
  const hsl = {};
  c.getHSL(hsl);
  c.setHSL(hsl.h, hsl.s, Math.max(0, Math.min(1, hsl.l * f)));
  return c;
}

// ---------------- face ----------------

function arcGeometry(radius, tube, arc) {
  return new THREE.TorusGeometry(radius, tube, 8, 24, arc);
}

function heartShape(size) {
  const s = new THREE.Shape();
  const x = 0, y = 0;
  s.moveTo(x, y + size * 0.3);
  s.bezierCurveTo(x, y + size * 0.6, x - size * 0.6, y + size * 0.6, x - size * 0.6, y + size * 0.25);
  s.bezierCurveTo(x - size * 0.6, y - size * 0.05, x - size * 0.2, y - size * 0.3, x, y - size * 0.55);
  s.bezierCurveTo(x + size * 0.2, y - size * 0.3, x + size * 0.6, y - size * 0.05, x + size * 0.6, y + size * 0.25);
  s.bezierCurveTo(x + size * 0.6, y + size * 0.6, x, y + size * 0.6, x, y + size * 0.3);
  return new THREE.ShapeGeometry(s);
}

const white = new THREE.MeshBasicMaterial({ color: 0xffffff });
const red = new THREE.MeshBasicMaterial({ color: 0xff4d6d });
const pinkMat = new THREE.MeshBasicMaterial({ color: 0xff8fab, transparent: true, opacity: 0.55 });
const mouthDark = new THREE.MeshBasicMaterial({ color: 0x4a1f2e });
const tongue = new THREE.MeshBasicMaterial({ color: 0xff7a93 });

/**
 * Builds one eye inside an eye slot. Variants: dot (neutral/surprised/sleepy/angry), happy arc,
 * closed arc, heart (love). `blink` group is code-controlled.
 */
function buildEye(eyeSlot, size, side) {
  const blink = new THREE.Group();
  blink.name = 'blink';
  eyeSlot.add(blink);
  const pupil = new THREE.Group();
  pupil.name = 'pupil';
  blink.add(pupil);

  const dot = new THREE.Mesh(new THREE.SphereGeometry(size, 16, 12), inkMat);
  dot.scale.set(0.82, 1.08, 0.35);
  const shine = new THREE.Mesh(new THREE.CircleGeometry(size * 0.32, 12), white);
  shine.position.set(-size * 0.28, size * 0.35, size * 0.38);
  dot.add(shine);
  pupil.add(dot);

  const happy = new THREE.Mesh(arcGeometry(size * 0.75, size * 0.2, Math.PI), inkMat);
  happy.position.y = -size * 0.25;
  const closed = new THREE.Mesh(arcGeometry(size * 0.75, size * 0.18, Math.PI), inkMat);
  closed.rotation.z = Math.PI;
  closed.position.y = size * 0.2;
  const heart = new THREE.Mesh(heartShape(size * 2.1), red);
  heart.position.z = size * 0.2;

  // angry brow
  const brow = new THREE.Mesh(new THREE.CapsuleGeometry(size * 0.16, size * 1.2, 4, 8), inkMat);
  brow.rotation.z = Math.PI / 2 + 0.45 * side;
  brow.position.set(0, size * 1.35, 0.01);

  // "pain": squeezed > < eyes (tip points toward the nose)
  const pts = [[0.55, 0.6], [-0.5, 0], [0.55, -0.6]].map(([x, y]) => new THREE.Vector3(x * size * side, y * size, 0));
  const pain = new THREE.Mesh(new THREE.TubeGeometry(new THREE.CatmullRomCurve3(pts, false, 'catmullrom', 0.05), 16, size * 0.17, 6, false), inkMat);

  blink.add(happy, closed, heart, brow, pain);
  return { blink, pupil, dot, happy, closed, heart, brow, pain };
}

function buildMouth(mouthSlot, size, beak) {
  const variants = {};
  if (beak) {
    // Chick beak: two cones that open for open/o.
    const mat = toon(0xffa53d);
    const top = part(new THREE.ConeGeometry(size * 0.9, size * 1.4, 12), mat, { outline: 0.06 });
    top.rotation.x = Math.PI / 2;
    top.scale.set(1, 1, 0.5);
    const bot = part(new THREE.ConeGeometry(size * 0.75, size * 1.1, 12), mat, { outline: 0.06 });
    bot.rotation.x = Math.PI / 2;
    bot.scale.set(1, 1, 0.45);
    bot.position.y = -size * 0.25;
    mouthSlot.add(top, bot);
    variants.beak = { top, bot };
    return variants;
  }
  variants.smile = new THREE.Mesh(arcGeometry(size, size * 0.22, Math.PI), inkMat);
  variants.smile.rotation.z = Math.PI;
  variants.smile.position.y = size * 0.55;
  variants.frown = new THREE.Mesh(arcGeometry(size * 0.8, size * 0.22, Math.PI), inkMat);
  variants.frown.position.y = -size * 0.5;
  const open = new THREE.Group();
  const hole = new THREE.Mesh(new THREE.CircleGeometry(size, 20, Math.PI, Math.PI), mouthDark);
  hole.scale.set(1.1, 1.0, 1);
  const tg = new THREE.Mesh(new THREE.CircleGeometry(size * 0.5, 16, Math.PI, Math.PI), tongue);
  tg.position.set(0, -size * 0.45, 0.002);
  open.add(hole, tg);
  variants.open = open;
  variants.o = new THREE.Mesh(new THREE.RingGeometry(size * 0.3, size * 0.55, 20), inkMat);
  variants.neutral = new THREE.Mesh(new THREE.CapsuleGeometry(size * 0.16, size * 0.8, 4, 8), inkMat);
  variants.neutral.rotation.z = Math.PI / 2;
  // "wavy": queasy squiggle
  const wave = [];
  for (let i = 0; i <= 12; i++) {
    const x = -1 + (i / 12) * 2;
    wave.push(new THREE.Vector3(x * size * 1.05, Math.sin((i * Math.PI) / 3) * size * 0.22, 0));
  }
  variants.wavy = new THREE.Mesh(new THREE.TubeGeometry(new THREE.CatmullRomCurve3(wave), 32, size * 0.13, 6, false), inkMat);
  for (const v of Object.values(variants)) mouthSlot.add(v);
  return variants;
}

/**
 * Builds a face on `parent` (the head's look group) at the front surface.
 * opts: { eyeY, eyeX, eyeZ, eyeSize, mouthY, mouthZ, mouthSize, cheekX, cheekY, cheekZ, beak }
 * Returns a controller with setExpression / blink / look.
 */
export function buildFace(parent, opts) {
  // L/R are the pet's own left/right: pet's left is screen-right (+x) when facing the viewer.
  const eyeL = slot('eyeL', parent, [opts.eyeX, opts.eyeY, opts.eyeZ]);
  const eyeR = slot('eyeR', parent, [-opts.eyeX, opts.eyeY, opts.eyeZ]);
  const mouth = slot('mouth', parent, [0, opts.mouthY, opts.mouthZ]);
  const L = buildEye(eyeL, opts.eyeSize, 1);
  const R = buildEye(eyeR, opts.eyeSize, -1);
  const M = buildMouth(mouth, opts.mouthSize, opts.beak);

  for (const sx of [-1, 1]) {
    const cheek = new THREE.Mesh(new THREE.CircleGeometry(opts.eyeSize * 0.95, 16), pinkMat);
    cheek.scale.set(1.3, 0.75, 1);
    cheek.position.set(sx * opts.cheekX, opts.cheekY, opts.cheekZ);
    cheek.rotation.y = sx * (opts.cheekTurn ?? 0.35);
    parent.add(cheek);
  }

  let eyes = 'neutral';
  let mouthState = 'smile';
  let blinkT = 0;
  let nextBlink = 2 + Math.random() * 3;
  const lookTarget = new THREE.Vector2();
  const look = new THREE.Vector2();

  function applyEyes() {
    for (const E of [L, R]) {
      E.dot.visible = ['neutral', 'surprised', 'sleepy', 'angry'].includes(eyes);
      E.happy.visible = eyes === 'happy';
      E.closed.visible = eyes === 'closed';
      E.heart.visible = eyes === 'love';
      E.brow.visible = eyes === 'angry';
      E.pain.visible = eyes === 'pain';
      const s = eyes === 'surprised' ? 1.3 : 1;
      E.dot.scale.set(0.82 * s, (eyes === 'sleepy' ? 0.38 : 1.08) * s, 0.35);
      E.dot.position.y = eyes === 'sleepy' ? -opts.eyeSize * 0.3 : 0;
    }
  }

  function applyMouth() {
    if (M.beak) {
      const open = mouthState === 'open' || mouthState === 'o';
      M.beak.bot.position.y = -opts.mouthSize * (open ? 0.75 : 0.25);
      M.beak.bot.rotation.x = Math.PI / 2 + (open ? 0.35 : 0);
      return;
    }
    for (const [k, v] of Object.entries(M)) v.visible = k === mouthState;
  }

  applyEyes();
  applyMouth();

  return {
    setExpression(e, m) {
      if (e && e !== eyes) { eyes = e; applyEyes(); }
      if (m && m !== mouthState) { mouthState = m; applyMouth(); }
    },
    get eyes() { return eyes; },
    /** look: x,y in [-1,1] (cursor direction) */
    lookAt(x, y) { lookTarget.set(Math.max(-1, Math.min(1, x)), Math.max(-1, Math.min(1, y))); },
    update(dt) {
      // blink only with open dot eyes
      blinkT += dt;
      let sy = 1;
      if (blinkT > nextBlink) {
        const p = (blinkT - nextBlink) / 0.14;
        if (p >= 1) { blinkT = 0; nextBlink = 1.8 + Math.random() * 4.5; }
        else sy = Math.max(0.08, Math.abs(1 - 2 * p));
      }
      const canBlink = eyes === 'neutral' || eyes === 'angry' || eyes === 'surprised';
      L.blink.scale.y = R.blink.scale.y = canBlink ? sy : 1;
      look.lerp(lookTarget, Math.min(1, dt * 6));
      const px = look.x * opts.eyeSize * 0.35, py = look.y * opts.eyeSize * 0.3;
      L.pupil.position.set(px, py, 0);
      R.pupil.position.set(px, py, 0);
    },
    look,
  };
}

/** Small glasses shown while the pet "looks" at the screen (screenshot indicator). */
export function buildGlasses(parent, { y, z, spread, size }) {
  const g = new THREE.Group();
  const ringGeo = new THREE.TorusGeometry(size, size * 0.18, 8, 24);
  for (const sx of [-1, 1]) {
    const ring = new THREE.Mesh(ringGeo, inkMat);
    ring.position.set(sx * spread, 0, 0);
    const lens = new THREE.Mesh(new THREE.CircleGeometry(size, 20), new THREE.MeshBasicMaterial({ color: 0x9fe3ff, transparent: true, opacity: 0.35 }));
    lens.position.set(sx * spread, 0, -0.005);
    g.add(ring, lens);
  }
  const bridge = new THREE.Mesh(new THREE.CapsuleGeometry(size * 0.12, spread * 0.6, 4, 8), inkMat);
  bridge.rotation.z = Math.PI / 2;
  g.add(bridge);
  g.position.set(0, y, z);
  g.visible = false;
  parent.add(g);
  return g;
}

/** Returns a capsule-ish limb mesh pivoting at the group origin, pointing down. */
export function limb(mat, radius, length, { outline = 0.08 } = {}) {
  const m = part(new THREE.CapsuleGeometry(radius, length, 6, 12), mat, { outline });
  m.position.y = -length / 2;
  return m;
}
