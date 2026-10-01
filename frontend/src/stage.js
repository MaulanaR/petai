import * as THREE from 'three';

// A second, temporary WebGL canvas for activity props (balls, hoop, outhouse…). It only exists
// while an activity runs and covers just the activity area. It sits above the pet canvas, so
// props can hide the pet (e.g. inside the outhouse). Coordinates are overlay CSS px; world units
// match the pet's (unitPx per unit).

const TILT = 0.14;

export class PropStage {
  constructor(container) {
    this.container = container;
    this.canvas = null;
    this.active = false;
    this.items = new Set();
    this.t = 0;
  }

  begin(rect, unitPx) {
    this.end();
    this.rect = { x: Math.round(rect.x), y: Math.round(rect.y), w: Math.round(rect.w), h: Math.round(rect.h) };
    this.unit = unitPx;
    this.canvas = document.createElement('canvas');
    this.canvas.className = 'stage-canvas';
    this.canvas.style.transform = `translate3d(${this.rect.x}px, ${this.rect.y}px, 0)`;
    this.container.appendChild(this.canvas);
    this.renderer = new THREE.WebGLRenderer({ canvas: this.canvas, alpha: true, antialias: true });
    this.renderer.setClearColor(0x000000, 0);
    this.renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
    this.renderer.outputColorSpace = THREE.SRGBColorSpace;
    this.renderer.setSize(this.rect.w, this.rect.h, false);
    this.canvas.style.width = this.rect.w + 'px';
    this.canvas.style.height = this.rect.h + 'px';
    this.scene = new THREE.Scene();
    this.cam = new THREE.OrthographicCamera(0, this.rect.w / unitPx, this.rect.h / unitPx, 0, -50, 50);
    this.cam.position.z = 10;
    this.scene.add(new THREE.HemisphereLight(0xffffff, 0xb9a7d6, 1.6));
    const sun = new THREE.DirectionalLight(0xffffff, 1.9);
    sun.position.set(-2, 4, 5);
    this.scene.add(sun);
    this.active = true;
    this.t = 0;
  }

  /** Adds a prop whose ground point is at screen (sx, sy). Returns a handle. */
  add(obj, sx, sy) {
    const holder = new THREE.Group();
    holder.rotation.x = TILT;
    holder.add(obj);
    this.scene.add(holder);
    const h = {
      obj, holder, sx, sy,
      move: (x, y) => { h.sx = x; h.sy = y; this.place(h); },
    };
    this.place(h);
    this.items.add(h);
    return h;
  }

  place(h) {
    h.holder.position.set((h.sx - this.rect.x) / this.unit, (this.rect.y + this.rect.h - h.sy) / this.unit, 0);
  }

  remove(h) {
    if (!h || !this.items.has(h)) return;
    this.scene.remove(h.holder);
    this.items.delete(h);
  }

  /** Screen position (CSS px) of an object inside a prop (e.g. the hoop rim). */
  screenOf(object3d) {
    const v = new THREE.Vector3();
    object3d.getWorldPosition(v);
    return { x: this.rect.x + v.x * this.unit, y: this.rect.y + this.rect.h - v.y * this.unit };
  }

  update(dt) {
    if (!this.active) return;
    this.t += dt;
    for (const h of this.items) if (h.obj.userData.update) h.obj.userData.update(this.t);
    this.renderer.render(this.scene, this.cam);
  }

  end() {
    if (!this.canvas) return;
    this.scene.traverse((o) => { if (o.geometry) o.geometry.dispose(); });
    this.renderer.dispose();
    this.canvas.remove();
    this.canvas = null;
    this.items.clear();
    this.active = false;
  }
}
