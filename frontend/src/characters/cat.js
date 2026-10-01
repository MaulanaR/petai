import * as THREE from 'three';
import { toon, part, slot, buildFace, buildGlasses, limb, shade, inkMaterial } from './common.js';

export const meta = { id: 'cat', label: 'Cat', labelId: 'Kucing', color: '#ffb26b', emoji: '🐱' };

export function build(color) {
  const main = toon(color);
  const light = toon(0xfff7ec);
  const pink = toon(0xff9fb5);
  const dark = toon(shade(color, 0.82));
  const root = new THREE.Group();
  root.name = 'slot_root';

  const body = slot('body', root, [0, 0.32, 0]);
  const torso = part(new THREE.SphereGeometry(0.31, 32, 24), main);
  torso.scale.set(1.02, 0.98, 0.92);
  body.add(torso);
  const belly = part(new THREE.SphereGeometry(0.2, 24, 16), light, { outline: 0 });
  belly.scale.set(1, 1.05, 0.5);
  belly.position.set(0, -0.03, 0.2);
  body.add(belly);

  const acc = slot('accessory', root, [0, 0.55, 0.02]);
  const collar = part(new THREE.TorusGeometry(0.2, 0.03, 8, 28), toon(0xff5d73), { outline: 0.12 });
  collar.rotation.x = Math.PI / 2 - 0.15;
  const bell = part(new THREE.SphereGeometry(0.045, 12, 10), toon(0xffd84d), { outline: 0.12 });
  bell.position.set(0, -0.05, 0.21);
  acc.add(collar, bell);

  const head = slot('head', root, [0, 0.74, 0.02]);
  const look = new THREE.Group();
  look.name = 'look';
  head.add(look);
  const skull = part(new THREE.SphereGeometry(0.33, 36, 26), main);
  skull.scale.set(1.14, 0.94, 0.95);
  look.add(skull);
  const muzzle = part(new THREE.SphereGeometry(0.11, 20, 14), light, { outline: 0.05 });
  muzzle.scale.set(1.35, 0.8, 0.7);
  muzzle.position.set(0, -0.1, 0.25);
  look.add(muzzle);
  const nose = new THREE.Mesh(new THREE.SphereGeometry(0.026, 12, 10), pink);
  nose.scale.set(1.3, 0.9, 0.8);
  nose.position.set(0, -0.055, 0.325);
  look.add(nose);
  for (const sx of [-1, 1]) {
    for (const dy of [-0.012, 0.03]) {
      const w = new THREE.Mesh(new THREE.CapsuleGeometry(0.005, 0.13, 3, 6), inkMaterial());
      w.rotation.z = Math.PI / 2 + sx * (dy > 0 ? 0.12 : -0.1);
      w.position.set(sx * 0.27, -0.09 + dy, 0.24);
      look.add(w);
    }
  }
  const face = buildFace(look, {
    eyeX: 0.135, eyeY: 0.05, eyeZ: 0.3, eyeSize: 0.056,
    mouthY: -0.145, mouthZ: 0.31, mouthSize: 0.035,
    cheekX: 0.225, cheekY: -0.06, cheekZ: 0.268, cheekTurn: 0.6,
  });
  for (const [n, x, rz] of [['earL', 0.2, -0.32], ['earR', -0.2, 0.32]]) {
    const ear = slot(n, look, [x, 0.25, -0.02]);
    ear.rotation.z = rz;
    const outer = part(new THREE.ConeGeometry(0.11, 0.2, 4), main, { outline: 0.1 });
    outer.position.y = 0.08;
    outer.rotation.y = Math.PI / 4;
    outer.scale.z = 0.55;
    const inner = new THREE.Mesh(new THREE.ConeGeometry(0.06, 0.12, 4), pink);
    inner.position.set(0, 0.06, 0.035);
    inner.rotation.y = Math.PI / 4;
    inner.scale.z = 0.3;
    ear.add(outer, inner);
  }
  const glasses = buildGlasses(look, { y: 0.05, z: 0.335, spread: 0.135, size: 0.075 });

  for (const [n, x] of [['armL', 0.16], ['armR', -0.16]]) {
    const arm = slot(n, root, [x, 0.3, 0.13]);
    arm.add(limb(main, 0.07, 0.13, { outline: 0.09 }));
    const paw = part(new THREE.SphereGeometry(0.075, 14, 10), light, { outline: 0.09 });
    paw.scale.set(1.05, 0.7, 1.15);
    paw.position.set(0, -0.26, 0.02);
    arm.add(paw);
  }
  for (const [n, x] of [['legL', 0.23], ['legR', -0.23]]) {
    const leg = slot(n, root, [x, 0.1, -0.04]);
    const foot = part(new THREE.SphereGeometry(0.09, 16, 12), dark, { outline: 0.1 });
    foot.scale.set(1.1, 0.65, 1.35);
    foot.position.set(0, -0.06, 0.03);
    leg.add(foot);
  }

  const tail = slot('tail', root, [0.05, 0.18, -0.26]);
  const curve = new THREE.CatmullRomCurve3([
    new THREE.Vector3(0, 0, 0), new THREE.Vector3(0.08, 0.1, -0.12),
    new THREE.Vector3(0.18, 0.3, -0.12), new THREE.Vector3(0.14, 0.48, -0.02),
  ]);
  tail.add(part(new THREE.TubeGeometry(curve, 24, 0.045, 10, false), main, { outline: 0.12 }));
  const tip = part(new THREE.SphereGeometry(0.05, 12, 10), dark, { outline: 0.12 });
  tip.position.copy(curve.getPoint(1));
  tail.add(tip);

  return { root, face, glasses, height: 1.1, headY: 1.05, faceY: 0.76, lookTurn: 1 };
}
