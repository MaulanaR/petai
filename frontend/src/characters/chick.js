import * as THREE from 'three';
import { toon, part, slot, buildFace, buildGlasses, shade } from './common.js';

export const meta = { id: 'chick', label: 'Chick', labelId: 'Anak Ayam', color: '#ffd84d', emoji: '🐥' };

export function build(color) {
  const main = toon(color);
  const dark = toon(shade(color, 0.85));
  const orange = toon(0xffa53d);
  const root = new THREE.Group();
  root.name = 'slot_root';

  const body = slot('body', root, [0, 0.44, 0]);
  const ball = part(new THREE.SphereGeometry(0.45, 40, 28), main);
  ball.scale.set(1, 0.97, 0.95);
  body.add(ball);
  const fluff = part(new THREE.SphereGeometry(0.2, 20, 14), toon(shade(color, 1.12)), { outline: 0 });
  fluff.scale.set(1.2, 0.9, 0.45);
  fluff.position.set(0, -0.17, 0.33);
  body.add(fluff);

  const head = slot('head', root, [0, 0.55, 0]);
  const look = new THREE.Group();
  look.name = 'look';
  head.add(look);
  const face = buildFace(look, {
    eyeX: 0.16, eyeY: 0.08, eyeZ: 0.39, eyeSize: 0.06,
    mouthY: -0.05, mouthZ: 0.43, mouthSize: 0.07, beak: true,
    cheekX: 0.27, cheekY: -0.02, cheekZ: 0.355, cheekTurn: 0.6,
  });
  const acc = slot('accessory', look, [0, 0.33, 0]);
  for (const [x, rz, h] of [[-0.05, 0.5, 0.13], [0, 0, 0.17], [0.05, -0.5, 0.13]]) {
    const f = part(new THREE.ConeGeometry(0.035, h, 8), dark, { outline: 0.15 });
    f.position.set(x, h / 2, 0);
    f.rotation.z = rz;
    acc.add(f);
  }
  const glasses = buildGlasses(look, { y: 0.08, z: 0.43, spread: 0.16, size: 0.08 });

  for (const [n, x, rz] of [['armL', 0.42, 0.25], ['armR', -0.42, -0.25]]) {
    const arm = slot(n, root, [x, 0.52, 0]);
    arm.rotation.z = rz;
    const wing = part(new THREE.SphereGeometry(0.16, 20, 14), dark, { outline: 0.08 });
    wing.scale.set(0.42, 1, 0.75);
    wing.position.set(0, -0.13, 0);
    arm.add(wing);
  }
  for (const [n, x] of [['legL', 0.14], ['legR', -0.14]]) {
    const leg = slot(n, root, [x, 0.1, 0.04]);
    const shin = part(new THREE.CylinderGeometry(0.022, 0.022, 0.1, 8), orange, { outline: 0.2 });
    shin.position.y = -0.03;
    leg.add(shin);
    for (const ry of [-0.5, 0, 0.5]) {
      const toe = part(new THREE.SphereGeometry(0.03, 10, 8), orange, { outline: 0.15 });
      toe.scale.set(0.8, 0.55, 1.9);
      toe.position.set(Math.sin(ry) * 0.045, -0.08, Math.cos(ry) * 0.045);
      toe.rotation.y = ry;
      leg.add(toe);
    }
  }
  const tail = slot('tail', root, [0, 0.38, -0.42]);
  for (const [x, rz] of [[-0.04, 0.4], [0.04, -0.4]]) {
    const t = part(new THREE.ConeGeometry(0.06, 0.16, 8), dark, { outline: 0.12 });
    t.rotation.x = -1.0;
    t.rotation.z = rz;
    t.position.set(x, 0.04, -0.03);
    tail.add(t);
  }

  return { root, face, glasses, height: 1.0, headY: 0.92, faceY: 0.6, lookTurn: 0.15 };
}
