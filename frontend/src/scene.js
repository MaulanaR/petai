import * as THREE from 'three';

// A small WebGL canvas that follows the pet around the transparent overlay. Rendering only a
// pet-sized square (instead of the whole monitor) keeps GPU usage tiny.

const VIEW_W = 2.6; // world units across the canvas
const FEET = 0.78; // feet sit at 78% of the canvas height

export class PetView {
  constructor(container) {
    this.canvas = document.createElement('canvas');
    this.canvas.className = 'pet-canvas';
    container.appendChild(this.canvas);
    this.renderer = new THREE.WebGLRenderer({ canvas: this.canvas, alpha: true, antialias: true, premultipliedAlpha: true });
    this.renderer.setClearColor(0x000000, 0);
    this.renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
    this.renderer.outputColorSpace = THREE.SRGBColorSpace;
    this.scene = new THREE.Scene();
    const top = VIEW_W * FEET;
    this.cam = new THREE.OrthographicCamera(-VIEW_W / 2, VIEW_W / 2, top, top - VIEW_W, 0.1, 50);
    this.cam.position.set(0, 0, 10);
    this.scene.add(new THREE.HemisphereLight(0xffffff, 0xb9a7d6, 1.6));
    const sun = new THREE.DirectionalLight(0xffffff, 1.9);
    sun.position.set(-2, 4, 5);
    this.scene.add(sun);
    this.tilt = new THREE.Group();
    this.tilt.rotation.x = 0.14;
    this.scene.add(this.tilt);
    this.char = null;
    this.unitPx = 110;
    this.size = 0;
    this.feet = { x: 0, y: 0 };
    this.box = new THREE.Box3();
    this.v = new THREE.Vector3();
    this.wobbleT = 0;
    this.setUnit(110);
  }

  setCharacter(char) {
    if (this.char) {
      this.tilt.remove(this.char.facing);
      this.char.dispose();
    }
    this.char = char;
    this.tilt.add(char.facing);
  }

  /** px per world unit (pet ≈ 1 unit tall). */
  setUnit(px) {
    this.unitPx = px;
    this.size = Math.round(px * VIEW_W);
    this.renderer.setSize(this.size, this.size, false);
    this.canvas.style.width = this.size + 'px';
    this.canvas.style.height = this.size + 'px';
  }

  place(x, y) {
    this.feet = { x, y };
    const left = Math.round(x - this.size / 2);
    const top = Math.round(y - this.size * FEET);
    this.canvas.style.transform = `translate3d(${left}px, ${top}px, 0)`;
  }

  setVisible(v) {
    this.canvas.style.opacity = v ? '1' : '0';
  }

  /** Bounding box of the pet in overlay CSS px. */
  petRect(pad = 6) {
    if (!this.char) return null;
    this.box.setFromObject(this.char.facing);
    if (this.box.isEmpty()) return null;
    const s = this.unitPx;
    const x0 = this.feet.x + this.box.min.x * s;
    const x1 = this.feet.x + this.box.max.x * s;
    const y0 = this.feet.y - this.box.max.y * s;
    const y1 = this.feet.y - this.box.min.y * s;
    return { x: x0 - pad, y: y0 - pad, w: x1 - x0 + pad * 2, h: y1 - y0 + pad * 2 };
  }

  /** Screen position (overlay CSS px) of a point in pet space (units, relative to feet). */
  toScreen(ux, uy) {
    return { x: this.feet.x + ux * this.unitPx, y: this.feet.y - uy * this.unitPx };
  }

  wobble(dt) {
    // Doodle "line boil": re-jitter outline thickness ~8 times a second.
    this.wobbleT += dt;
    if (this.wobbleT < 0.125 || !this.char) return;
    this.wobbleT = 0;
    for (const o of this.char.outlines) {
      const w = o.userData.outline * (0.8 + Math.random() * 0.45);
      o.scale.setScalar(1 + w);
    }
  }

  render() {
    this.renderer.render(this.scene, this.cam);
  }
}
