// Speech bubble above the pet: typewriter speech, optional suggestion, thinking dots, a chat
// input, a voice-mode header (mic + level meter) and inline yes/no questions. The bubble is part
// of the hit regions so it is clickable.

export class Bubble {
  constructor(container, { onSend, onClose, onVoiceStop, onVoiceKeyboard, t }) {
    this.t = t;
    this.onSend = onSend;
    this.onClose = onClose;
    this.onVoiceStop = onVoiceStop || (() => {});
    this.onVoiceKeyboard = onVoiceKeyboard || (() => {});
    this.el = document.createElement('div');
    this.el.className = 'bubble hidden';
    this.el.innerHTML = `
      <button class="bubble-x" title="Tutup">×</button>
      <div class="bubble-voice">
        <span class="mic-dot" title="Mikrofon aktif"></span>
        <span class="mic-bars"><i></i><i></i><i></i><i></i><i></i></span>
        <span class="voice-status"></span>
        <button class="voice-btn voice-kb" title="Ketik saja">⌨</button>
        <button class="voice-btn voice-stop" title="Selesai ngobrol">✖</button>
      </div>
      <div class="bubble-user"></div>
      <div class="bubble-text"></div>
      <div class="bubble-tip"></div>
      <div class="bubble-ask"></div>
      <div class="bubble-dots"><span></span><span></span><span></span></div>
      <form class="bubble-form">
        <input class="bubble-input" maxlength="1000" autocomplete="off" />
        <button class="bubble-send" type="submit">➤</button>
      </form>`;
    container.appendChild(this.el);
    this.userEl = this.el.querySelector('.bubble-user');
    this.textEl = this.el.querySelector('.bubble-text');
    this.tipEl = this.el.querySelector('.bubble-tip');
    this.askEl = this.el.querySelector('.bubble-ask');
    this.dotsEl = this.el.querySelector('.bubble-dots');
    this.form = this.el.querySelector('.bubble-form');
    this.input = this.el.querySelector('.bubble-input');
    this.statusEl = this.el.querySelector('.voice-status');
    this.bars = [...this.el.querySelectorAll('.mic-bars i')];
    this.el.querySelector('.bubble-x').addEventListener('click', () => (this.voiceOpen ? this.onVoiceStop() : this.close()));
    this.el.querySelector('.voice-stop').addEventListener('click', () => this.onVoiceStop());
    this.el.querySelector('.voice-kb').addEventListener('click', () => this.onVoiceKeyboard());
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
      this.tipEl.style.display = 'none';
      this.onSend(text);
    });
    this.input.addEventListener('keydown', (e) => { if (e.key === 'Escape') this.close(); });
    this.chatOpen = false;
    this.voiceOpen = false;
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
    if (!this.chatOpen && !this.voiceOpen) this.userEl.style.display = 'none';
    this.type(text || '');
    const hasIcon = suggestion && /^\p{Extended_Pictographic}/u.test(suggestion);
    this.tipEl.textContent = suggestion ? (hasIcon ? suggestion : '💡 ' + suggestion) : '';
    this.tipEl.style.display = suggestion ? 'block' : 'none';
    const dur = seconds ?? Math.min(14, 4 + ((text || '').length + (suggestion || '').length) / 14);
    this.hideAt = this.chatOpen || this.voiceOpen ? 0 : performance.now() + dur * 1000;
  }

  /** Adds a short note under the current text (e.g. "📂 Membuka Word…"). */
  note(text, seconds = 6) {
    this.show();
    this.tipEl.textContent = text;
    this.tipEl.style.display = 'block';
    if (!this.chatOpen && !this.voiceOpen) this.hideAt = Math.max(this.hideAt, performance.now() + seconds * 1000);
  }

  /** Inline question with buttons: [{label, onClick}]. */
  ask(text, buttons) {
    this.show();
    this.askEl.innerHTML = '';
    const p = document.createElement('div');
    p.textContent = text;
    this.askEl.appendChild(p);
    const row = document.createElement('div');
    row.className = 'ask-row';
    for (const b of buttons) {
      const el = document.createElement('button');
      el.className = 'ask-btn';
      el.textContent = b.label;
      el.onclick = () => { this.askEl.style.display = 'none'; b.onClick(); };
      row.appendChild(el);
    }
    this.askEl.appendChild(row);
    this.askEl.style.display = 'block';
    this.hideAt = 0;
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
    if (this.voiceOpen) this.closeVoice(0);
    this.chatOpen = true;
    this.show();
    this.el.classList.add('chat');
    this.input.placeholder = this.t('chatPlaceholder');
    this.hideAt = 0;
    setTimeout(() => this.input.focus(), 30);
  }

  // ---------- voice mode ----------

  openVoice() {
    if (this.chatOpen) this.close();
    this.voiceOpen = true;
    this.show();
    this.el.classList.add('voice');
    this.userEl.style.display = 'none';
    this.textEl.textContent = '';
    this.tipEl.style.display = 'none';
    this.hideAt = 0;
    this.setVoicePhase('listening');
  }

  setVoicePhase(phase) {
    this.phase = phase;
    this.el.dataset.phase = phase;
    const s = { listening: this.t('voiceListening'), thinking: this.t('voiceThinking'), speaking: '' }[phase] ?? '';
    this.statusEl.textContent = s;
    this.setThinking(phase === 'thinking');
    if (phase === 'listening') this.setLevel(0);
  }

  setLevel(level) {
    const l = Math.max(0, Math.min(1, level));
    this.bars.forEach((b, i) => {
      const h = Math.max(0.15, Math.min(1, l * (1.6 - Math.abs(i - 2) * 0.35) + Math.random() * 0.08));
      b.style.transform = `scaleY(${h.toFixed(2)})`;
    });
  }

  showHeard(text) {
    if (!text) return;
    this.userEl.textContent = text;
    this.userEl.style.display = 'block';
  }

  closeVoice(keepSeconds = 4) {
    if (!this.voiceOpen) return;
    this.voiceOpen = false;
    this.el.classList.remove('voice');
    this.setThinking(false);
    this.hideAt = keepSeconds > 0 ? performance.now() + keepSeconds * 1000 : 1;
  }

  show() {
    this.visible = true;
    this.el.classList.remove('hidden');
  }

  close() {
    const wasChat = this.chatOpen;
    this.chatOpen = false;
    this.voiceOpen = false;
    this.visible = false;
    this.el.classList.remove('chat', 'voice');
    this.el.classList.add('hidden');
    this.userEl.style.display = 'none';
    this.askEl.style.display = 'none';
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
