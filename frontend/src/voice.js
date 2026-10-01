// Text-to-speech for the pet (Windows voices via the Web Speech API) with simple lip-sync.

const EMOJI = /[\p{Extended_Pictographic}\u{FE0F}\u{200D}\u{20E3}]/gu;

/** Makes AI text pleasant to speak: no emoji, markdown or URLs. */
export function speakable(text) {
  return String(text || '')
    .replace(EMOJI, '')
    .replace(/https?:\/\/\S+/g, '')
    .replace(/[*_#`>~|]/g, '')
    .replace(/\s+/g, ' ')
    .trim();
}

export class Speaker {
  constructor({ onMouth }) {
    this.onMouth = onMouth; // (open:boolean) — lip-sync callback
    this.synth = window.speechSynthesis || null;
    this.voices = [];
    this.speaking = false;
    this.mouthTimer = null;
    if (this.synth) {
      const load = () => { this.voices = this.synth.getVoices() || []; };
      load();
      this.synth.addEventListener?.('voiceschanged', load);
    }
  }

  available() { return !!this.synth; }

  listVoices() {
    return this.voices.map((v) => ({ name: v.name, lang: v.lang, local: v.localService }));
  }

  pickVoice(lang, preferred) {
    if (preferred) {
      const v = this.voices.find((x) => x.name === preferred);
      if (v) return v;
    }
    const want = lang === 'en' ? 'en' : 'id';
    return this.voices.find((v) => v.lang.toLowerCase().startsWith(want) && /andika|gadis|ardi/i.test(v.name))
      || this.voices.find((v) => v.lang.toLowerCase().startsWith(want))
      || null;
  }

  /** Speaks text; resolves when finished or cancelled. */
  speak(text, { lang = 'id', voice = '', pitch = 1.3, rate = 1.05 } = {}) {
    const say = speakable(text);
    if (!this.synth || !say) return Promise.resolve();
    this.cancel();
    return new Promise((resolve) => {
      const u = new SpeechSynthesisUtterance(say);
      const v = this.pickVoice(lang, voice);
      if (v) u.voice = v;
      u.lang = v ? v.lang : lang === 'en' ? 'en-US' : 'id-ID';
      u.pitch = pitch;
      u.rate = rate;
      const done = () => {
        this.speaking = false;
        this.stopMouth();
        resolve();
      };
      u.onstart = () => { this.speaking = true; this.startMouth(); };
      u.onboundary = () => this.onMouth(true);
      u.onend = done;
      u.onerror = done;
      this.synth.speak(u);
      // Safety: some engines never fire onend.
      setTimeout(() => { if (this.speaking) done(); }, 1500 + say.length * 120);
    });
  }

  startMouth() {
    this.stopMouth();
    let open = false;
    this.mouthTimer = setInterval(() => {
      open = !open && Math.random() > 0.15;
      this.onMouth(open);
    }, 110);
  }

  stopMouth() {
    clearInterval(this.mouthTimer);
    this.mouthTimer = null;
    this.onMouth(false);
  }

  cancel() {
    if (this.synth && (this.synth.speaking || this.synth.pending)) this.synth.cancel();
    this.speaking = false;
    this.stopMouth();
  }
}
