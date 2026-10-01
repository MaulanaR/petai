import * as THREE from 'three';
import { toon, part, slot, buildFace, buildGlasses, limb, shade } from './common.js';

export const meta = { id: 'blob', label: 'Blob', labelId: 'Blob Jeli', color: '#8fd3ff', emoji: '🫧' };

export function build(color) {
  const main = toon(color);
  const dark = toon(shade(color, 0.8));
  const root = new THREE.Group();
  root.name = 'slot_root';

  const body = slot('body', root, [0, 0.42, 0]);
  const jelly = part(new THREE.SphereGeometry(0.48, 40, 28), main);
  jelly.scale.set(1.06, 0.9, 0.95);
  body.add(jelly);
  const shine = new THREE.Mesh(new THREE.SphereGeometry(0.09, 16, 12), new THREE.MeshBasicMaterial({ color: 0xffffff, transparent: true, opacity: 0.85 }));
  shine.scale.set(1.3, 0.8, 0.4);
  shine.position.set(-0.24, 0.24, 0.36);
  shine.rotation.z = 0.5;
  body.add(shine);

  const head = slot('head', root, [0, 0.46, 0]);
  const look = new THREE.Group();
  look.name = 'look';
  head.add(look);
  const face = buildFace(look, {
    eyeX: 0.15, eyeY: 0.06, eyeZ: 0.445, eyeSize: 0.062,
    mouthY: -0.08, mouthZ: 0.468, mouthSize: 0.05,
    cheekX: 0.27, cheekY: -0.04, cheekZ: 0.405, cheekTurn: 0.55,
  });
  for (const [n, x] of [['earL', 0.24], ['earR', -0.24]]) {
    const ear = slot(n, look, [x, 0.33, -0.02]);
    const e = part(new THREE.SphereGeometry(0.1, 16, 12), main, { outline: 0.09 });
    e.scale.set(1, 1.15, 0.8);
    ear.add(e);
  }
  const acc = slot('accessory', look, [0, 0.37, 0]);
  const stalk = part(new THREE.CylinderGeometry(0.015, 0.02, 0.16, 8), dark, { outline: 0.25 });
  stalk.position.y = 0.08;
  stalk.rotation.z = -0.15;
  const ball = part(new THREE.SphereGeometry(0.045, 12, 10), toon(0xff8fab), { outline: 0.12 });
  ball.position.set(0.012, 0.17, 0);
  acc.add(stalk, ball);
  const glasses = buildGlasses(look, { y: 0.06, z: 0.47, spread: 0.15, size: 0.085 });

  for (const [n, x, rz] of [['armL', 0.47, 0.45], ['armR', -0.47, -0.45]]) {
    const arm = slot(n, root, [x, 0.42, 0.04]);
    arm.rotation.z = rz;
    arm.add(limb(main, 0.065, 0.1, { outline: 0.1 }));
  }
  for (const [n, x] of [['legL', 0.18], ['legR', -0.18]]) {
    const leg = slot(n, root, [x, 0.1, 0.06]);
    const foot = part(new THREE.SphereGeometry(0.09, 16, 12), dark, { outline: 0.1 });
    foot.scale.set(1.15, 0.62, 1.3);
    foot.position.set(0, -0.055, 0.02);
    leg.add(foot);
  }

  return { root, face, glasses, height: 1.0, headY: 0.85, faceY: 0.5, lookTurn: 0.15 };
}
