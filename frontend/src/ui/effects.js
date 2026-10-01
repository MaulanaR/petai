// Little doodle effects (hearts, sparkles, zzz…) floating around the pet, as DOM sprites.

const GLYPHS = {
  hearts: ['❤', '#ff4d6d', 3],
  sparkles: ['✦', '#ffc93c', 4],
  sweat: ['💧', '#5ab0ff', 1],
  zzz: ['z', '#6c63ff', 3],
  question: ['?', '#2b2540', 1],
  exclaim: ['!', '#ff5d3d', 1],
};

export class Effects {
  constructor(container) {
    this.layer = document.createElement('div');
    this.layer.className = 'fx-layer';
    container.appendChild(this.layer);
  }

  spawn(type, x, y) {
    const g = GLYPHS[type];
    if (!g) return;
    const [ch, color, n] = g;
    for (let i = 0; i < n; i++) {
      const el = document.createElement('div');
      el.className = `fx fx-${type}`;
      el.textContent = ch;
      el.style.color = color;
      const dx = (Math.random() - 0.5) * 70 + (type === 'zzz' ? 18 + i * 10 : 0);
      el.style.left = x + dx + 'px';
      el.style.top = y - (type === 'zzz' ? i * 14 : 0) + 'px';
      el.style.animationDelay = (i * (type === 'zzz' ? 0.45 : 0.12)).toFixed(2) + 's';
      el.style.fontSize = (type === 'zzz' ? 16 + i * 5 : 18 + Math.random() * 8) + 'px';
      this.layer.appendChild(el);
      setTimeout(() => el.remove(), 2600 + i * 450);
    }
  }
}
