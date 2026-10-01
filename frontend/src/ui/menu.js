// Right-click context menu next to the pet.

export class Menu {
  constructor(container, t) {
    this.t = t;
    this.el = document.createElement('div');
    this.el.className = 'menu hidden';
    container.appendChild(this.el);
    this.visible = false;
    this.leftAt = 0;
  }

  /** items: [{label, onClick, checked?, sep?, sub?:[...] }] */
  open(x, y, items) {
    this.el.innerHTML = '';
    for (const it of items) {
      if (it.sep) {
        const hr = document.createElement('div');
        hr.className = 'menu-sep';
        this.el.appendChild(hr);
        continue;
      }
      if (it.row) {
        const row = document.createElement('div');
        row.className = 'menu-row';
        const lab = document.createElement('div');
        lab.className = 'menu-row-label';
        lab.textContent = it.label;
        row.appendChild(lab);
        const chips = document.createElement('div');
        chips.className = 'menu-chips';
        for (const c of it.row) {
          const b = document.createElement('button');
          b.className = 'chip' + (c.checked ? ' on' : '');
          b.textContent = c.label;
          b.title = c.title || '';
          b.addEventListener('click', () => { this.close(); c.onClick(); });
          chips.appendChild(b);
        }
        row.appendChild(chips);
        this.el.appendChild(row);
        continue;
      }
      const b = document.createElement('button');
      b.className = 'menu-item';
      b.textContent = it.label;
      b.addEventListener('click', () => { this.close(); it.onClick(); });
      this.el.appendChild(b);
    }
    this.el.classList.remove('hidden');
    this.visible = true;
    this.leftAt = 0;
    const r = this.el.getBoundingClientRect();
    const nx = Math.max(8, Math.min(window.innerWidth - r.width - 8, x));
    const ny = Math.max(8, Math.min(window.innerHeight - r.height - 8, y));
    this.el.style.transform = `translate3d(${nx}px, ${ny}px, 0)`;
  }

  close() {
    this.visible = false;
    this.el.classList.add('hidden');
  }

  rect() {
    if (!this.visible) return null;
    const r = this.el.getBoundingClientRect();
    return { x: r.left - 6, y: r.top - 6, w: r.width + 12, h: r.height + 12 };
  }

  /** Auto-close when the cursor stays away from the menu and pet for a while. */
  trackCursor(x, y, petRect) {
    if (!this.visible) return;
    const inside = (r) => r && x >= r.x && x <= r.x + r.w && y >= r.y && y <= r.y + r.h;
    if (inside(this.rect()) || inside(petRect)) { this.leftAt = 0; return; }
    if (!this.leftAt) this.leftAt = performance.now();
    else if (performance.now() - this.leftAt > 1400) this.close();
  }
}
