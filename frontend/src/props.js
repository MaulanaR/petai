import * as THREE from 'three';
import { toon, part, inkMaterial, INK } from './characters/common.js';

// Toon/doodle props used by activities. Units match the pet (pet ≈ 1 unit tall); every prop's
// origin is where it touches the ground (or its centre for balls).

const basic = (color, opts = {}) => new THREE.MeshBasicMaterial({ color, ...opts });

export function soccerBall(r = 0.17) {
  const g = new THREE.Group();
  g.add(part(new THREE.SphereGeometry(r, 24, 18), toon(0xffffff), { outline: 0.08 }));
  const ico = new THREE.IcosahedronGeometry(1, 0);
  const pos = ico.getAttribute('position');
  const seen = new Set();
  for (let i = 0; i < pos.count; i++) {
    const v = new THREE.Vector3().fromBufferAttribute(pos, i).normalize();
    const k = v.toArray().map((n) => n.toFixed(2)).join();
    if (seen.has(k)) continue;
    seen.add(k);
    const patch = new THREE.Mesh(new THREE.CircleGeometry(r * 0.34, 5), basic(0x2b2540));
    patch.position.copy(v.clone().multiplyScalar(r * 1.004));
    patch.lookAt(v.clone().multiplyScalar(r * 2));
    g.add(patch);
  }
  g.userData.radius = r;
  return g;
}

export function basketball(r = 0.16) {
  const g = new THREE.Group();
  g.add(part(new THREE.SphereGeometry(r, 24, 18), toon(0xf28c28), { outline: 0.08 }));
  const seam = basic(0x3a2418);
  for (const [rx, ry] of [[0, 0], [Math.PI / 2, 0], [0, Math.PI / 2]]) {
    const t = new THREE.Mesh(new THREE.TorusGeometry(r * 1.003, r * 0.05, 6, 40), seam);
    t.rotation.set(rx, ry, 0);
    g.add(t);
  }
  g.userData.radius = r;
  return g;
}

/** Basketball hoop. `dir` = side the shooter stands on (-1 left, +1 right). userData.rim = rim object. */
export function hoop(dir = -1) {
  const g = new THREE.Group();
  const turn = new THREE.Group();
  turn.rotation.y = dir * 0.75; // board faces the shooter and the viewer
  g.add(turn);
  const grey = toon(0x8a8fa3);
  const base = part(new THREE.BoxGeometry(0.55, 0.08, 0.4), toon(0x4b4f63), { outline: 0.05 });
  base.position.y = 0.04;
  const pole = part(new THREE.CylinderGeometry(0.035, 0.045, 2.3, 10), grey, { outline: 0.15 });
  pole.position.set(0, 1.15, -0.12);
  turn.add(base, pole);
  const board = part(new THREE.BoxGeometry(0.95, 0.62, 0.04), toon(0xffffff), { outline: 0.03 });
  board.position.set(0, 2.25, 0);
  turn.add(board);
  const red = basic(0xe5484d);
  for (const [w, h, x, y] of [[0.36, 0.025, 0, 2.38], [0.36, 0.025, 0, 2.12], [0.025, 0.26, -0.17, 2.25], [0.025, 0.26, 0.17, 2.25]]) {
    const b = new THREE.Mesh(new THREE.BoxGeometry(w, h, 0.01), red);
    b.position.set(x, y, 0.026);
    turn.add(b);
  }
  const rim = new THREE.Group();
  rim.position.set(0, 2.02, 0.26);
  const ring = part(new THREE.TorusGeometry(0.2, 0.022, 8, 28), toon(0xff6a1a), { outline: 0.2 });
  ring.rotation.x = Math.PI / 2;
  const net = new THREE.Mesh(new THREE.CylinderGeometry(0.2, 0.12, 0.3, 12, 3, true), basic(0xffffff, { wireframe: true }));
  net.position.y = -0.16;
  rim.add(ring, net);
  turn.add(rim);
  g.userData.rim = rim;
  g.userData.net = net;
  return g;
}

export function golfHole() {
  const g = new THREE.Group();
  const hole = new THREE.Mesh(new THREE.CircleGeometry(0.15, 24), basic(0x1d1a2b));
  hole.rotation.x = -Math.PI / 2;
  hole.position.y = 0.004;
  const lip = new THREE.Mesh(new THREE.RingGeometry(0.15, 0.2, 24), basic(0x6fcf74));
  lip.rotation.x = -Math.PI / 2;
  lip.position.y = 0.003;
  const pole = part(new THREE.CylinderGeometry(0.014, 0.014, 1.25, 8), toon(0xffffff), { outline: 0.25 });
  pole.position.y = 0.625;
  const flag = new THREE.Group();
  flag.position.y = 1.2;
  const shape = new THREE.Shape();
  shape.moveTo(0, 0);
  shape.lineTo(0.42, -0.12);
  shape.lineTo(0, -0.26);
  shape.closePath();
  const cloth = new THREE.Mesh(new THREE.ShapeGeometry(shape), basic(0xe5484d, { side: THREE.DoubleSide }));
  flag.add(cloth);
  g.add(lip, hole, pole, flag);
  g.userData.flag = flag;
  g.userData.update = (t) => { flag.rotation.y = Math.sin(t * 5) * 0.35; };
  return g;
}

export function golfBall() {
  const g = new THREE.Group();
  g.add(part(new THREE.SphereGeometry(0.05, 16, 12), toon(0xffffff), { outline: 0.18 }));
  g.userData.radius = 0.05;
  return g;
}

/** Golf club held in a hand; origin at the grip, pointing down. */
export function golfClub() {
  const g = new THREE.Group();
  const grip = part(new THREE.CylinderGeometry(0.02, 0.02, 0.12, 8), toon(0x2b2540), { outline: 0.2 });
  grip.position.y = -0.06;
  const shaft = part(new THREE.CylinderGeometry(0.011, 0.011, 0.55, 6), toon(0xc9ccd6), { outline: 0.3 });
  shaft.position.y = -0.38;
  const head = part(new THREE.BoxGeometry(0.13, 0.05, 0.06), toon(0x4b4f63), { outline: 0.12 });
  head.position.set(0.05, -0.66, 0);
  g.add(grip, shaft, head);
  return g;
}

/** Cute outhouse. userData.door = hinge group (rotate y to open). */
export function outhouse(dir = 1) {
  const g = new THREE.Group();
  const turn = new THREE.Group();
  turn.rotation.y = -dir * 0.32;
  g.add(turn);
  const wood = toon(0xd39a5c);
  const dark = toon(0x9a6436);
  const body = part(new THREE.BoxGeometry(1.0, 1.42, 0.8), wood, { outline: 0.03 });
  body.position.y = 0.71;
  turn.add(body);
  const roof = part(new THREE.BoxGeometry(1.22, 0.13, 1.0), dark, { outline: 0.04 });
  roof.position.set(0, 1.5, 0);
  roof.rotation.z = 0.12;
  turn.add(roof);
  const vent = part(new THREE.CylinderGeometry(0.06, 0.06, 0.28, 10), toon(0x8a8fa3), { outline: 0.15 });
  vent.position.set(0.28, 1.68, -0.1);
  turn.add(vent);
  const plank = basic(INK, { transparent: true, opacity: 0.35 });
  for (const x of [-0.42, 0.42]) {
    const l = new THREE.Mesh(new THREE.BoxGeometry(0.012, 1.3, 0.01), plank);
    l.position.set(x, 0.7, 0.402);
    turn.add(l);
  }
  const inside = new THREE.Mesh(new THREE.PlaneGeometry(0.62, 1.08), basic(0x241c2e));
  inside.position.set(0, 0.6, 0.401);
  turn.add(inside);
  const door = new THREE.Group();
  door.position.set(-0.31, 0.6, 0.405); // hinge on the left edge
  const panel = part(new THREE.BoxGeometry(0.62, 1.08, 0.04), toon(0xe8b071), { outline: 0.03 });
  panel.position.x = 0.31;
  door.add(panel);
  const moon = new THREE.Mesh(new THREE.CircleGeometry(0.1, 20), basic(0x241c2e));
  moon.position.set(0.31, 0.32, 0.022);
  const bite = new THREE.Mesh(new THREE.CircleGeometry(0.09, 20), basic(0xe8b071));
  bite.position.set(0.355, 0.34, 0.023);
  const knob = new THREE.Mesh(new THREE.SphereGeometry(0.035, 10, 8), toon(0xffd84d));
  knob.position.set(0.53, 0, 0.04);
  door.add(moon, bite, knob);
  turn.add(door);
  g.userData.door = door;
  g.userData.vent = vent;
  return g;
}

export function puff(color = 0x9be37a) {
  const m = new THREE.Mesh(new THREE.SphereGeometry(0.09, 12, 10), new THREE.MeshBasicMaterial({ color, transparent: true, opacity: 0.75 }));
  return m;
}

export function poofCloud() {
  const g = new THREE.Group();
  const mat = new THREE.MeshBasicMaterial({ color: 0xffffff, transparent: true, opacity: 0.9 });
  for (let i = 0; i < 7; i++) {
    const a = (i / 7) * Math.PI * 2;
    const s = new THREE.Mesh(new THREE.SphereGeometry(0.16 + Math.random() * 0.08, 12, 10), mat);
    s.position.set(Math.cos(a) * 0.32, 0.2 + Math.sin(a) * 0.2, 0.2);
    g.add(s);
  }
  g.userData.mat = mat;
  return g;
}

export function disposeTree(obj) {
  obj.traverse((o) => {
    if (o.geometry) o.geometry.dispose();
    if (o.material && o.material !== inkMaterial() && o.material.dispose && !o.material.isMeshToonMaterial) o.material.dispose();
  });
}
