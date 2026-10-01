// Speech bubble above the pet: typewriter speech, optional suggestion, thinking dots and a
// chat input. The bubble is part of the hit regions so it is clickable.

export class Bubble {
  constructor(container, { onSend, onClose, t }) {
    this.t = t;
    this.onSend = onSend;
    this.onClose = onClose;
    this.el = document.createElement('div');
    this.el.className = 'bubble hidden';
    this.el.innerHTML = `
      <button class="bubble-x" title="Tutup">×</button>
      <div class="bubble-user"></div>
      <div class="bubble-text"></div>
      <div class="bubble-tip"></div>
      <div class="bubble-dots"><span></span><span></span><span></span></div>
      <form class="bubble-form">
        <input class="bubble-input" maxlength="1000" autocomplete="off" />
        <button class="bubble-send" type="submit">➤</button>
      </form>`;
    container.appendChild(this.el);
    this.userEl = this.el.querySelector('.bubble-user');
    this.textEl = this.el.querySelector('.bubble-text');
    this.tipEl = this.el.querySelector('.bubble-tip');
    this.dotsEl = this.el.querySelector('.bubble-dots');
    this.form = this.el.querySelector('.bubble-form');
    this.input = this.el.querySelector('.bubble-input');
    this.el.querySelector('.bubble-x').addEventListener('click', () => this.close());
    this.form.addEventListener('submit', (e) => {
      e.preventDefault();
      const text = this.input.value.trim();
      if (!text) return;
      this.input.value = '';
      this.userEl.textContent = text;
      this.userEl.style.display = 'block';
      this.setThinking(true);
      this.textEl.textContent = '';
      this.tipEl.textContent = '';
      this.onSend(text);
    });
    this.input.addEventListener('keydown', (e) => { if (e.key === 'Escape') this.close(); });
    this.chatOpen = false;
    this.visible = false;
    this.hideAt = 0;
    this.typer = null;
    this.thinking = false;
  }

  get open() { return this.visible; }

  say(text, suggestion, seconds) {
    if (!text && !suggestion) return;
    this.show();
    this.setThinking(false);
    if (!this.chatOpen) this.userEl.style.display = 'none';
    this.type(text || '');
    this.tipEl.textContent = suggestion ? '💡 ' + suggestion : '';
    this.tipEl.style.display = suggestion ? 'block' : 'none';
    const dur = seconds ?? Math.min(14, 4 + ((text || '').length + (suggestion || '').length) / 14);
    this.hideAt = this.chatOpen ? 0 : performance.now() + dur * 1000;
  }

  type(text) {
    clearInterval(this.typer);
    this.textEl.textContent = '';
    const chars = [...text];
    let i = 0;
    this.typer = setInterval(() => {
      i += 2;
      this.textEl.textContent = chars.slice(0, i).join('');
      if (i >= chars.length) clearInterval(this.typer);
    }, 28);
  }

  setThinking(on) {
    this.thinking = on;
    this.dotsEl.style.display = on ? 'flex' : 'none';
    if (on) this.show();
  }

  openChat() {
    this.chatOpen = true;
    this.show();
    this.el.classList.add('chat');
    this.input.placeholder = this.t('chatPlaceholder');
    this.hideAt = 0;
    setTimeout(() => this.input.focus(), 30);
  }

  show() {
    this.visible = true;
    this.el.classList.remove('hidden');
  }

  close() {
    const wasChat = this.chatOpen;
    this.chatOpen = false;
    this.visible = false;
    this.el.classList.remove('chat');
    this.el.classList.add('hidden');
    this.userEl.style.display = 'none';
    clearInterval(this.typer);
    if (wasChat) this.onClose();
  }

  /** Positions the bubble above (or below) the head, clamped to the screen. */
  place(headX, headY, footY, screenW) {
    if (this.hideAt && performance.now() > this.hideAt && !this.thinking) this.close();
    if (!this.visible) return;
    const r = this.el.getBoundingClientRect();
    const w = r.width, h = r.height;
    let x = headX - w / 2;
    let y = headY - h - 14;
    let below = false;
    if (y < 8) { y = footY + 10; below = true; }
    x = Math.max(8, Math.min(screenW - w - 8, x));
    this.el.style.transform = `translate3d(${Math.round(x)}px, ${Math.round(y)}px, 0)`;
    this.el.classList.toggle('below', below);
    this.el.style.setProperty('--tail', `${Math.max(16, Math.min(w - 16, headX - x))}px`);
  }

  rect() {
    if (!this.visible) return null;
    const r = this.el.getBoundingClientRect();
    return { x: r.left - 4, y: r.top - 4, w: r.width + 8, h: r.height + 8 };
  }
}
