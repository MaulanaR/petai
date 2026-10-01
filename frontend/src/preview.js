// Dev-only preview: renders every character side by side and plays built-in animations,
// without the Wails runtime. Open with `npm run dev` → /preview.html.
import * as THREE from 'three';
import { createCharacter, CHARACTER_LIST } from './characters/index.js';
import { AnimPlayer } from './anim/player.js';

const files = import.meta.glob('../../internal/anim/builtin/*.json', { eager: true, import: 'default' });
const specs = Object.values(files);
const names = specs.map((s) => s.name).sort();

const grid = document.getElementById('grid');
const btns = document.getElementById('btns');
const pets = [];
const expressions = ['neutral', 'happy', 'sleepy', 'surprised', 'angry', 'love', 'closed'];

for (const m of CHARACTER_LIST) {
  const cell = document.createElement('div');
  cell.className = 'cell';
  cell.innerHTML = `<span>${m.emoji} ${m.labelId}</span>`;
  grid.appendChild(cell);
  const unit = 120;
  const size = unit * 2.6;
  const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true });
  renderer.setPixelRatio(window.devicePixelRatio);
  renderer.setSize(size, size);
  renderer.outputColorSpace = THREE.SRGBColorSpace;
  cell.appendChild(renderer.domElement);
  const scene = new THREE.Scene();
  const top = 2.6 * 0.78;
  const cam = new THREE.OrthographicCamera(-1.3, 1.3, top, top - 2.6, 0.1, 50);
  cam.position.z = 10;
  scene.add(new THREE.HemisphereLight(0xffffff, 0xb9a7d6, 1.6));
  const sun = new THREE.DirectionalLight(0xffffff, 1.9);
  sun.position.set(-2, 4, 5);
  scene.add(sun);
  const tilt = new THREE.Group();
  tilt.rotation.x = 0.14;
  scene.add(tilt);
  const ch = createCharacter(m.id, '');
  tilt.add(ch.facing);
  const player = new AnimPlayer(ch, { onExpression: (e, mo) => ch.face.setExpression(e, mo) });
  player.load(specs);
  player.play('idle');
  pets.push({ ch, player, renderer, scene, cam });
}

function playAll(name) {
  for (const p of pets) p.player.play(name, { restart: true, onDone: () => p.player.play('idle') });
  document.querySelectorAll('#btns button').forEach((b) => b.classList.toggle('on', b.textContent === name));
}
for (const n of names) {
  const b = document.createElement('button');
  b.textContent = n;
  b.onclick = () => playAll(n);
  btns.appendChild(b);
}
const sep = document.createElement('b');
sep.textContent = ' | ekspresi:';
btns.appendChild(sep);
for (const e of expressions) {
  const b = document.createElement('button');
  b.textContent = e;
  b.onclick = () => pets.forEach((p) => p.ch.face.setExpression(e, e === 'surprised' ? 'o' : e === 'happy' ? 'open' : 'smile'));
  btns.appendChild(b);
}
window.__preview = { playAll, pets };

let last = performance.now();
function loop(now) {
  requestAnimationFrame(loop);
  const dt = Math.min(0.1, (now - last) / 1000);
  last = now;
  for (const p of pets) {
    p.player.update(dt);
    p.ch.face.update(dt);
    p.renderer.render(p.scene, p.cam);
  }
}
requestAnimationFrame(loop);
