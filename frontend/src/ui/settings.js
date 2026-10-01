import { api, call, on } from '../bridge.js';
import { BrowserOpenURL } from '../../wailsjs/runtime/runtime';
import { CHARACTER_LIST } from '../characters/index.js';

const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

const TABS = [
  ['pet', '🐾 Karakter'],
  ['move', '🚶 Gerak'],
  ['ai', '🧠 AI'],
  ['voice', '🎙️ Voice'],
  ['apps', '🚀 Aplikasi'],
  ['privacy', '🔒 Privasi'],
  ['memory', '📒 Memori'],
  ['anims', '🎞️ Animasi'],
  ['general', '⚙️ Umum'],
];

const ANTHROPIC_MODELS = ['claude-opus-5-5', 'claude-sonnet-5-5', 'claude-haiku-4-5', 'claude-fable-5-1'];
const KIND_ICON = { program: '🖥️', website: '🌐', path: '📁' };
const ACCEPTS_LABEL = { docx: 'dokumen Word', txt: 'teks', none: '' };
const EMPTY_APP = { id: '', name: '', aliases: [], kind: 'program', target: '', args: '', accepts: 'none', clipboard: false };

export class Settings {
  constructor(container, { getConfig, onSaved, onPlay, onOpenChange, getBootstrap, speaker }) {
    this.getConfig = getConfig;
    this.speaker = speaker;
    this.voice = { ready: false, supported: false, suggest: [] };
    this.presets = null;
    this.docsDefault = '';
    this.editApp = null; // index being edited, -1 = new
    on('mic:level', (v) => {
      const bar = this.el.querySelector('.mic-meter i');
      if (bar) bar.style.width = Math.round(Math.min(1, v.level * 1.4) * 100) + '%';
      if (v.speaking && bar) bar.classList.add('talk');
    });
    this.onSaved = onSaved;
    this.onPlay = onPlay;
    this.onOpenChange = onOpenChange;
    this.getBootstrap = getBootstrap;
    this.el = document.createElement('div');
    this.el.className = 'settings hidden';
    container.appendChild(this.el);
    this.tab = 'pet';
    this.visible = false;
    this.saveTimer = null;
    this.models = { anthropic: ANTHROPIC_MODELS.slice(), openai: [] };
    this.keys = { anthropic: '', openai: '' };
    this.monitors = 1;
    this.version = '';
    this.drag = null;
    this.offset = { x: 0, y: 0 };
  }

  get open() { return this.visible; }

  async show(tab) {
    if (tab) this.tab = tab;
    const [b] = await call(api.GetBootstrap);
    if (b) {
      this.keys = b.keys || this.keys;
      this.monitors = b.monitors || 1;
      this.version = b.version;
      if (b.voice) this.voice = b.voice;
    }
    this.cfg = structuredClone(this.getConfig());
    this.visible = true;
    this.el.classList.remove('hidden');
    this.render();
    this.onOpenChange(true);
  }

  hide() {
    if (!this.visible) return;
    this.flush();
    this.visible = false;
    this.el.classList.add('hidden');
    this.onOpenChange(false);
  }

  rect() {
    if (!this.visible) return null;
    const r = this.el.getBoundingClientRect();
    return { x: r.left - 4, y: r.top - 4, w: r.width + 8, h: r.height + 8 };
  }

  /** Called when config changes elsewhere (menu, drag anchor…). */
  external(cfg) {
    if (!this.visible || this.saveTimer) return;
    this.cfg = structuredClone(cfg);
    if (!this.el.contains(document.activeElement)) this.render();
  }

  /** Voice capability changed (probe finished, model changed…). */
  setVoiceInfo(info) {
    this.voice = info || { ready: false };
    if (!this.visible) return;
    const card = this.el.querySelector('.status-card');
    if (card) card.outerHTML = this.voiceCard();
    const line = this.el.querySelector('.voice-line');
    if (line) line.outerHTML = this.voiceLine();
    const tog = this.el.querySelector('[data-k="ai.voice.enabled"]');
    if (tog) { tog.disabled = !this.voice.supported; tog.closest('label').classList.toggle('disabled', !this.voice.supported); }
    this.bindVoiceCard();
  }

  voiceCard() {
    const v = this.voice || {};
    const m = `<b>${esc(v.model || '(belum ada model)')}</b>`;
    const retest = '<button class="btn ghost" data-act="voice-recheck">Tes ulang</button>';
    if (!this.cfg.ai.enabled || !this.keys[this.cfg.ai.provider] || !v.model) {
      return `<div class="status-card"><div>ℹ️ Atur AI dulu (tab 🧠 AI: aktifkan, API key, model). Setelah itu aku otomatis mengetes apakah modelnya bisa mendengar suara.</div></div>`;
    }
    if (v.checking) {
      return `<div class="status-card wait"><div>⏳ Mengetes apakah ${m} bisa mendengar suara…</div></div>`;
    }
    if (!v.checkedAt) {
      return `<div class="status-card wait"><div>⏳ ${m} belum dites — sebentar lagi dites otomatis.</div>${retest}</div>`;
    }
    if (v.supported) {
      return `<div class="status-card ok"><div>✅ ${m} bisa mendengar suara. <b>Klik 2× pet</b> (atau Ctrl+Alt+V) untuk ngobrol pakai suara.</div>${retest}</div>`;
    }
    const chips = (v.suggest || []).length
      ? `<div class="chips"><small>Model yang menerima audio di endpoint ini:</small>${v.suggest.map((s) => `<button class="chip" data-vmodel="${esc(s)}">${esc(s)}</button>`).join('')}</div>`
      : '';
    return `<div class="status-card bad"><div>❌ ${m} tidak menerima audio${v.error && !/menerima audio/.test(v.error) ? ` <small>(${esc(v.error)})</small>` : ''}.
      Klik 2× pet tetap membuka <b>chat teks</b>. Pilih model yang mendukung audio di bawah untuk mengaktifkan mode suara.</div>${chips}${retest}</div>`;
  }

  voiceLine() {
    const v = this.voice || {};
    let s = '🎙️ Mode suara: ';
    if (v.checking) s += 'mengetes model…';
    else if (!v.checkedAt) s += 'belum dites';
    else if (v.supported) s += `✅ ${esc(v.model)} bisa mendengar`;
    else s += `❌ ${esc(v.model)} tidak menerima audio (klik 2× = chat teks)`;
    return `<p class="note voice-line">${s} · <a href="#" class="go-voice">atur</a></p>`;
  }

  bindVoiceCard() {
    this.el.querySelectorAll('[data-act="voice-recheck"]').forEach((b) => (b.onclick = () => this.action('voice-recheck', b)));
    this.el.querySelectorAll('[data-vmodel]').forEach((b) => (b.onclick = () => {
      this.change((c) => { c.ai.voice.model = b.dataset.vmodel; }, true);
      this.flush();
    }));
    this.el.querySelectorAll('.go-voice').forEach((a) => (a.onclick = (e) => { e.preventDefault(); this.flush(); this.tab = 'voice'; this.render(); }));
  }

  // ---------- persistence ----------

  change(mut, rerender = false) {
    mut(this.cfg);
    clearTimeout(this.saveTimer);
    this.saveTimer = setTimeout(() => this.flush(), 350);
    if (rerender) this.render();
  }

  async flush() {
    if (!this.saveTimer) return;
    clearTimeout(this.saveTimer);
    this.saveTimer = null;
    const [saved, err] = await call(api.SaveConfig, this.cfg);
    if (err) return this.toast('Gagal menyimpan: ' + err, true);
    this.cfg = structuredClone(saved);
    this.onSaved(saved);
  }

  toast(msg, bad) {
    const t = this.el.querySelector('.st-toast');
    if (!t) return;
    t.textContent = msg;
    t.className = 'st-toast show' + (bad ? ' bad' : '');
    clearTimeout(this.toastTimer);
    this.toastTimer = setTimeout(() => { t.className = 'st-toast'; }, 2600);
  }

  // ---------- rendering ----------

  render() {
    const tabs = TABS.map(([id, label]) => `<button class="st-tab ${id === this.tab ? 'on' : ''}" data-tab="${id}">${label}</button>`).join('');
    this.el.innerHTML = `
      <div class="st-head">
        <div class="st-title">PetAI <span>v${esc(this.version)} · oleh <a href="#" class="credit-link" data-url="https://maulanar.my.id">Maulana Rahman</a></span></div>
        <button class="st-close" title="Tutup">×</button>
      </div>
      <div class="st-body">
        <nav class="st-tabs">${tabs}</nav>
        <section class="st-page">${this.page()}</section>
      </div>
      <div class="st-toast"></div>`;
    this.el.style.transform = `translate(${this.offset.x}px, ${this.offset.y}px)`;
    this.el.querySelector('.st-close').onclick = () => this.hide();
    this.el.querySelectorAll('.st-tab').forEach((b) => (b.onclick = () => { this.flush(); this.tab = b.dataset.tab; this.render(); }));
    const head = this.el.querySelector('.st-head');
    head.onmousedown = (e) => {
      if (e.target.closest('button')) return;
      this.drag = { x: e.clientX - this.offset.x, y: e.clientY - this.offset.y };
      const mv = (ev) => { this.offset = { x: ev.clientX - this.drag.x, y: ev.clientY - this.drag.y }; this.el.style.transform = `translate(${this.offset.x}px, ${this.offset.y}px)`; };
      const up = () => { window.removeEventListener('mousemove', mv); window.removeEventListener('mouseup', up); };
      window.addEventListener('mousemove', mv);
      window.addEventListener('mouseup', up);
    };
    this.bind();
  }

  page() {
    const c = this.cfg;
    switch (this.tab) {
      case 'pet':
        return `
          <h3>Pilih karakter</h3>
          <div class="cards">${CHARACTER_LIST.map((m) => `
            <button class="card ${c.pet.character === m.id ? 'on' : ''}" data-char="${m.id}">
              <div class="card-emoji">${m.emoji}</div><div>${esc(m.labelId)}</div>
            </button>`).join('')}
          </div>
          <label>Nama pet <input data-k="pet.name" value="${esc(c.pet.name)}" maxlength="32"></label>
          <div class="row">
            <label>Warna <input type="color" data-k="pet.color" value="${esc(c.pet.color || this.charColor(c.pet.character))}"></label>
            <button class="btn ghost" data-act="color-auto">Warna bawaan</button>
          </div>
          <label>Kepribadian <textarea data-k="pet.personality" rows="3" maxlength="400">${esc(c.pet.personality)}</textarea></label>
          <div class="row">
            <label>Bahasa
              <select data-k="pet.language">
                <option value="id" ${c.pet.language === 'id' ? 'selected' : ''}>Bahasa Indonesia</option>
                <option value="en" ${c.pet.language === 'en' ? 'selected' : ''}>English</option>
              </select>
            </label>
            <label>Ukuran <input type="range" min="0.5" max="2" step="0.05" data-k="pet.scale" data-num value="${c.pet.scale}"></label>
          </div>`;
      case 'move':
        return `
          <h3>Mode gerak</h3>
          <div class="cards">
            ${[['stay', '📍', 'Diam di tempat', 'Tetap di satu posisi, hanya animasi di tempat. Seret untuk pindah.'],
               ['ground', '🚶', 'Jalan-jalan', 'Jalan di atas taskbar, bisa lompat & nangkring di atas jendela aktif.'],
               ['free', '🎈', 'Melayang bebas', 'Terbang ke mana saja di layar tanpa gravitasi.']].map(([id, e, l, d]) => `
              <button class="card wide ${c.movement.mode === id ? 'on' : ''}" data-mode="${id}">
                <div class="card-emoji">${e}</div><div><b>${l}</b><small>${d}</small></div>
              </button>`).join('')}
          </div>
          <label>Kecepatan <input type="range" min="0.25" max="3" step="0.05" data-k="movement.speed" data-num value="${c.movement.speed}"></label>
          <label>Seberapa aktif berkeliaran <input type="range" min="0" max="1" step="0.05" data-k="movement.activity" data-num value="${c.movement.activity}"></label>
          <button class="btn ghost" data-act="reset-pos">Kembalikan posisi pet</button>`;
      case 'ai': {
        const p = c.ai.provider;
        const pc = p === 'openai' ? c.ai.openai : c.ai.anthropic;
        const models = this.models[p] || [];
        return `
          <label class="switch"><input type="checkbox" data-k="ai.enabled" ${c.ai.enabled ? 'checked' : ''}><span></span> Aktifkan AI (komentar, saran & chat)</label>
          <label>Provider
            <select data-k="ai.provider">
              <option value="anthropic" ${p === 'anthropic' ? 'selected' : ''}>Anthropic (Claude)</option>
              <option value="openai" ${p === 'openai' ? 'selected' : ''}>OpenAI / kompatibel OpenAI</option>
            </select>
          </label>
          <label>API key (BYOK) — disimpan di Windows Credential Manager
            <div class="row">
              <input type="password" class="key-input" placeholder="${this.keys[p] ? 'tersimpan: ' + esc(this.keys[p]) : (p === 'openai' ? 'sk-…' : 'sk-ant-…')}" autocomplete="off">
              <button class="btn" data-act="save-key">Simpan</button>
              ${this.keys[p] ? '<button class="btn ghost" data-act="clear-key">Hapus</button>' : ''}
            </div>
          </label>
          <label>Model
            <input list="model-list" data-k="ai.${p}.model" value="${esc(pc.model)}" placeholder="${p === 'openai' ? 'pilih setelah Tes koneksi' : 'claude-opus-5-5'}">
            <datalist id="model-list">${models.map((m) => `<option value="${esc(m)}">`).join('')}</datalist>
          </label>
          <details ${pc.baseURL ? 'open' : ''}><summary>Lanjutan</summary>
            <label>Base URL ${p === 'openai' ? '(OpenRouter, Ollama, LM Studio…)' : '(proxy, opsional)'}
              <input data-k="ai.${p}.baseURL" value="${esc(pc.baseURL)}" placeholder="${p === 'openai' ? 'https://api.openai.com/v1' : 'https://api.anthropic.com'}">
            </label>
          </details>
          <div class="row"><button class="btn" data-act="test">Tes koneksi</button><span class="test-out"></span></div>
          ${this.voiceLine()}
          <label>Seberapa cerewet: maks <b>${c.ai.maxCallsPerHour}</b> komentar otomatis / jam <small>(0 = hanya saat diajak ngobrol)</small>
            <input type="range" min="0" max="60" step="1" data-k="ai.maxCallsPerHour" data-num value="${c.ai.maxCallsPerHour}">
          </label>
          <p class="note">Biaya API ditanggung akunmu sendiri. Model kecil (mis. claude-haiku-4-5) lebih hemat; Opus paling pintar.</p>`;
      }
      case 'voice': {
        const v = c.ai.voice;
        const p = c.ai.provider;
        const voices = this.speaker ? this.speaker.listVoices() : [];
        return `
          ${this.voiceCard()}
          <label>Model untuk suara <small>(kosong = sama dengan model chat: ${esc((p === 'openai' ? c.ai.openai : c.ai.anthropic).model || '-')})</small>
            <input list="voice-model-list" data-k="ai.voice.model" value="${esc(v.model)}" placeholder="sama dengan model chat">
            <datalist id="voice-model-list">${[...new Set([...(this.voice.suggest || []), ...(this.models[p] || [])])].map((m) => `<option value="${esc(m)}">`).join('')}</datalist>
          </label>
          <label class="switch ${this.voice.supported ? '' : 'disabled'}"><input type="checkbox" data-k="ai.voice.enabled" ${v.enabled ? 'checked' : ''} ${this.voice.supported ? '' : 'disabled'}><span></span>
            Klik 2× pet = ngobrol pakai suara</label>
          <h3>Mikrofon</h3>
          <div class="row"><button class="btn ghost" data-act="mic-test">🎤 Tes mikrofon</button><div class="mic-meter"><i></i></div><span class="mic-out"></span></div>
          <label>Jeda diam sebelum dikirim: <b>${v.silenceMs}</b> ms
            <input type="range" min="500" max="3000" step="100" data-k="ai.voice.silenceMs" data-num value="${v.silenceMs}"></label>
          <label class="switch"><input type="checkbox" data-k="ai.voice.continuous" ${v.continuous ? 'checked' : ''}><span></span> Percakapan bersambung (setelah menjawab, aku dengerin lagi)</label>
          <h3>Suara pet</h3>
          <label>Suara
            <select data-k="ai.voice.ttsVoice">
              <option value="">Otomatis (Andika untuk Bahasa Indonesia)</option>
              ${voices.map((x) => `<option value="${esc(x.name)}" ${v.ttsVoice === x.name ? 'selected' : ''}>${esc(x.name)} — ${esc(x.lang)}</option>`).join('')}
            </select>
          </label>
          <div class="row">
            <label>Nada <input type="range" min="0.5" max="2" step="0.05" data-k="ai.voice.ttsPitch" data-num value="${v.ttsPitch}"></label>
            <label>Kecepatan <input type="range" min="0.5" max="2" step="0.05" data-k="ai.voice.ttsRate" data-num value="${v.ttsRate}"></label>
          </div>
          <button class="btn ghost" data-act="tts-try">🔊 Coba suara</button>
          <label class="switch"><input type="checkbox" data-k="ai.voice.speakAuto" ${v.speakAuto ? 'checked' : ''}><span></span> Bacakan juga komentar otomatis</label>
          <p class="note">Mikrofon hanya aktif selama mode suara (titik merah di bubble). Rekaman tidak disimpan — hanya dikirim ke provider AI-mu. Selesai: ✖, Esc, klik pet, atau bilang "udah ya".</p>`;
      }
      case 'apps': {
        const apps = c.launcher.apps || [];
        const ed = this.editApp === null ? null : (this.editApp >= 0 ? apps[this.editApp] : null) || EMPTY_APP;
        return `
          <p class="note">Pet hanya bisa membuka aplikasi di daftar ini — mis. <i>"buka Word, catat notulensi meeting hari ini"</i> atau <i>"cari resep rendang di Google"</i>.</p>
          <div class="app-list">${apps.length ? apps.map((a, i) => `
            <div class="app-row">
              <span class="app-ico">${KIND_ICON[a.kind] || '🖥️'}</span>
              <div class="app-main"><b>${esc(a.name)}</b>${a.accepts !== 'none' ? ` <span class="badge builtin">${ACCEPTS_LABEL[a.accepts]}</span>` : ''}${a.clipboard ? ' <span class="badge ai">clipboard</span>' : ''}
                <small>${esc(a.target)}${a.aliases && a.aliases.length ? ' · alias: ' + esc(a.aliases.join(', ')) : ''}</small></div>
              <button class="icon" data-app-test="${i}" title="Coba buka">▶</button>
              <button class="icon" data-app-edit="${i}" title="Ubah">✎</button>
              <button class="icon" data-app-del="${i}" title="Hapus">🗑</button>
            </div>`).join('') : '<p class="note">Belum ada aplikasi. Tambah dari pilihan cepat di bawah.</p>'}
          </div>
          <h3>Tambah cepat</h3>
          <div class="chips preset-chips"><small>Memuat…</small></div>
          <div class="row"><button class="btn ghost" data-act="app-startmenu">📋 Pilih dari Start Menu</button><button class="btn ghost" data-act="app-new">➕ Tambah manual</button></div>
          <div class="lnk-list"></div>
          ${ed ? `
          <div class="app-form">
            <h3>${this.editApp >= 0 ? 'Ubah aplikasi' : 'Aplikasi baru'}</h3>
            <label>Nama <input class="af-name" value="${esc(ed.name)}" maxlength="40" placeholder="Microsoft Word"></label>
            <label>Jenis
              <select class="af-kind">
                <option value="program" ${ed.kind === 'program' ? 'selected' : ''}>Program (.exe / shortcut)</option>
                <option value="website" ${ed.kind === 'website' ? 'selected' : ''}>Website (boleh pakai {query})</option>
                <option value="path" ${ed.kind === 'path' ? 'selected' : ''}>File / folder</option>
              </select>
            </label>
            <label>Target
              <div class="row"><input class="af-target" value="${esc(ed.target)}" placeholder="C:\\…\\app.exe  atau  https://www.google.com/search?q={query}"><button class="btn ghost" data-act="app-browse">Browse…</button></div>
            </label>
            <label>Argumen tambahan <small>(opsional)</small> <input class="af-args" value="${esc(ed.args)}"></label>
            <label>Nama panggilan lain <small>(pisahkan koma)</small> <input class="af-aliases" value="${esc((ed.aliases || []).join(', '))}" placeholder="word, ms word"></label>
            <label>Dokumen yang bisa disiapkan pet
              <select class="af-accepts">
                <option value="none" ${ed.accepts === 'none' ? 'selected' : ''}>Tidak ada (cuma buka)</option>
                <option value="docx" ${ed.accepts === 'docx' ? 'selected' : ''}>Dokumen Word (.docx)</option>
                <option value="txt" ${ed.accepts === 'txt' ? 'selected' : ''}>Teks (.txt)</option>
              </select>
            </label>
            <label class="switch"><input type="checkbox" class="af-clip" ${ed.clipboard ? 'checked' : ''}><span></span> Salin template ke clipboard (untuk web, mis. Google Docs)</label>
            <div class="row"><button class="btn" data-act="app-save">Simpan</button><button class="btn ghost" data-act="app-try">▶ Coba</button><button class="btn ghost" data-act="app-cancel">Batal</button></div>
          </div>` : ''}
          <label class="switch"><input type="checkbox" data-k="launcher.confirm" ${c.launcher.confirm ? 'checked' : ''}><span></span> Tanya dulu sebelum membuka aplikasi</label>
          <label>Folder dokumen buatan pet <input data-k="launcher.docsFolder" value="${esc(c.launcher.docsFolder)}" placeholder="${esc(this.docsDefault || 'Documents\\PetAI')}"></label>`;
      }
      case 'privacy':
        return `
          <label class="switch"><input type="checkbox" data-k="privacy.watchActivity" ${c.privacy.watchActivity ? 'checked' : ''}><span></span>
            <b>Izinkan pet melihat aktivitas PC</b></label>
          <p class="note">Nama aplikasi & judul jendela aktif (judul disensor: email, nomor panjang, token). Dipakai untuk komentar, saran, dan belajar kebiasaanmu. Tidak ada keylogger, tidak membaca clipboard.</p>
          <label class="switch ${c.privacy.watchActivity ? '' : 'disabled'}"><input type="checkbox" data-k="privacy.screenshots" ${c.privacy.screenshots ? 'checked' : ''} ${c.privacy.watchActivity ? '' : 'disabled'}><span></span>
            Izinkan screenshot berkala ke AI (vision)</label>
          <p class="note">Gambar diperkecil & hanya dikirim ke provider AI-mu, tidak disimpan ke disk. Pet memakai kacamata 👓 sesaat sebelum melihat.</p>
          <label>Interval screenshot minimal (menit) <input type="number" min="1" max="240" data-k="privacy.screenshotIntervalMin" data-num value="${c.privacy.screenshotIntervalMin}" ${c.ScreenshotsEffective ? '' : ''}></label>
          <label>Daftar blokir (satu per baris — app/judul yang cocok tidak pernah dicatat/dikirim)
            <textarea data-k="privacy.blocklist" data-lines rows="5">${esc((c.privacy.blocklist || []).join('\n'))}</textarea></label>
          <label>Simpan log aktivitas selama (hari) <input type="number" min="1" max="365" data-k="privacy.retentionDays" data-num value="${c.privacy.retentionDays}"></label>
          <label class="switch"><input type="checkbox" data-k="privacy.excludeFromCapture" ${c.privacy.excludeFromCapture ? 'checked' : ''}><span></span>
            Sembunyikan pet dari screenshot & screen share</label>
          <div class="danger">
            <button class="btn bad" data-act="wipe-all" data-confirm="Yakin? Klik lagi untuk menghapus">Hapus semua data pribadi</button>
            <small>Log aktivitas, memori & riwayat chat. Pengaturan, key & animasi tetap.</small>
          </div>`;
      case 'memory':
        return `
          <h3>Yang aku ingat tentangmu</h3>
          <div class="mem-list"><p class="note">Memuat…</p></div>
          <div class="row">
            <select class="mem-kind"><option value="habit">kebiasaan</option><option value="preference">preferensi</option><option value="fact">fakta</option><option value="goal">tujuan</option></select>
            <input class="mem-text" placeholder="Tambah memori manual…" maxlength="300">
            <button class="btn" data-act="mem-add">Tambah</button>
          </div>
          <button class="btn bad" data-act="mem-wipe" data-confirm="Yakin? Klik lagi">Lupakan semua</button>
          <h3>Ringkasan 7 hari</h3>
          <div class="stats"><p class="note">${c.privacy.watchActivity ? 'Memuat…' : 'Pengamatan aktivitas nonaktif (lihat tab Privasi).'}</p></div>`;
      case 'anims':
        return `
          <p class="note">Gerakan buatan AI disimpan lokal & dipakai ulang tanpa generate ulang.</p>
          <div class="anim-list"><p class="note">Memuat…</p></div>
          <details><summary>Import animasi (JSON)</summary>
            <textarea class="anim-import" rows="5" placeholder='{"name":"my_dance", ...}'></textarea>
            <button class="btn" data-act="anim-import">Import</button>
          </details>`;
      case 'general':
        return `
          <label class="switch"><input type="checkbox" data-k="general.autostart" ${c.general.autostart ? 'checked' : ''}><span></span> Jalankan saat Windows mulai</label>
          <label class="switch"><input type="checkbox" data-k="general.respectFullscreen" ${c.general.respectFullscreen ? 'checked' : ''}><span></span> Sembunyi saat game/presentasi/fullscreen</label>
          <div class="row">
            <label>FPS
              <select data-k="general.fps" data-num>${[15, 30, 60].map((f) => `<option value="${f}" ${c.general.fps === f ? 'selected' : ''}>${f}</option>`).join('')}</select>
            </label>
            <label>Monitor
              <select data-k="general.monitor" data-num>${Array.from({ length: Math.max(1, this.monitors) }, (_, i) => `<option value="${i}" ${c.general.monitor === i ? 'selected' : ''}>Monitor ${i + 1}</option>`).join('')}</select>
            </label>
          </div>
          <label class="switch"><input type="checkbox" data-k="general.debug" ${c.general.debug ? 'checked' : ''}><span></span> Log debug</label>
          <p class="note">Pintasan: <b>Ctrl+Alt+P</b> buka chat. Klik kanan pet untuk menu cepat. Ikon tray untuk tampilkan/sembunyikan.</p>
          <button class="btn ghost" data-act="open-data">Buka folder data</button>
          <div class="credit">
            <div class="credit-name">🫧 PetAI dibuat dengan ❤️ oleh <b>Maulana Rahman</b></div>
            <a href="#" class="credit-link" data-url="https://maulanar.my.id">maulanar.my.id</a> ·
            <a href="#" class="credit-link" data-url="https://github.com/MaulanaR/petai">github.com/MaulanaR/petai</a>
          </div>`;
    }
    return '';
  }

  charColor(id) {
    return (CHARACTER_LIST.find((m) => m.id === id) || CHARACTER_LIST[0]).color;
  }

  bind() {
    const root = this.el;
    root.querySelectorAll('[data-k]').forEach((input) => {
      const ev = input.type === 'checkbox' || input.tagName === 'SELECT' || input.type === 'color' ? 'change' : 'input';
      input.addEventListener(ev, () => {
        const path = input.dataset.k.split('.');
        let v = input.type === 'checkbox' ? input.checked : input.value;
        if (input.dataset.num !== undefined) v = Number(v);
        if (input.dataset.lines !== undefined) v = String(v).split('\n').map((s) => s.trim()).filter(Boolean);
        const rerender = ['ai.provider', 'privacy.watchActivity', 'ai.maxCallsPerHour'].includes(input.dataset.k);
        if (input.dataset.k === 'ai.voice.silenceMs') {
          const b = input.closest('label').querySelector('b');
          if (b) b.textContent = v;
        }
        this.change((c) => {
          let o = c;
          for (let i = 0; i < path.length - 1; i++) o = o[path[i]];
          o[path[path.length - 1]] = v;
          if (input.dataset.k === 'privacy.watchActivity' && !v) c.privacy.screenshots = false;
        }, rerender && ev === 'change');
        if (input.dataset.k === 'ai.maxCallsPerHour') {
          const b = input.closest('label').querySelector('b');
          if (b) b.textContent = v;
        }
      });
    });
    root.querySelectorAll('[data-char]').forEach((b) => (b.onclick = () => this.change((c) => { c.pet.character = b.dataset.char; }, true)));
    root.querySelectorAll('[data-mode]').forEach((b) => (b.onclick = () => this.change((c) => { c.movement.mode = b.dataset.mode; c.movement.anchorX = -1; c.movement.anchorY = -1; }, true)));
    root.querySelectorAll('[data-act]').forEach((b) => (b.onclick = () => this.action(b.dataset.act, b)));
    this.el.querySelectorAll('.credit-link').forEach((a) => (a.onclick = (e) => { e.preventDefault(); BrowserOpenURL(a.dataset.url); }));
    if (this.tab === 'memory') this.loadMemories();
    if (this.tab === 'anims') this.loadAnims();
    if (this.tab === 'apps') this.bindApps();
    this.bindVoiceCard();
  }

  // ---------- apps tab ----------

  async bindApps() {
    const root = this.el;
    root.querySelectorAll('[data-app-test]').forEach((b) => (b.onclick = async () => {
      const app = this.cfg.launcher.apps[Number(b.dataset.appTest)];
      const [msg] = await call(api.TestLaunch, app);
      this.toast(msg ? 'Gagal: ' + msg : `Membuka ${app.name}…`, !!msg);
    }));
    root.querySelectorAll('[data-app-edit]').forEach((b) => (b.onclick = () => { this.editApp = Number(b.dataset.appEdit); this.render(); }));
    root.querySelectorAll('[data-app-del]').forEach((b) => (b.onclick = () => {
      const i = Number(b.dataset.appDel);
      this.editApp = null;
      this.change((c) => { c.launcher.apps.splice(i, 1); }, true);
    }));
    if (!this.docsDefault) {
      const [d] = await call(api.DefaultDocsFolder);
      this.docsDefault = d || '';
      const inp = root.querySelector('[data-k="launcher.docsFolder"]');
      if (inp && d) inp.placeholder = d;
    }
    if (!this.presets) {
      const [p] = await call(api.GetAppPresets);
      this.presets = p || [];
    }
    const box = root.querySelector('.preset-chips');
    if (!box) return;
    const have = new Set((this.cfg.launcher.apps || []).map((a) => a.id));
    box.innerHTML = this.presets.map((p, i) => `<button class="chip" data-preset="${i}" ${have.has(p.id) ? 'disabled' : ''}>${KIND_ICON[p.kind] || ''} ${esc(p.name)}${have.has(p.id) ? ' ✓' : ''}</button>`).join('')
      || '<small>Tidak ada preset.</small>';
    box.querySelectorAll('[data-preset]').forEach((b) => (b.onclick = () => {
      const p = this.presets[Number(b.dataset.preset)];
      this.change((c) => { c.launcher.apps = [...(c.launcher.apps || []), structuredClone(p)]; }, true);
      this.flush();
      this.toast(`${p.name} ditambahkan`);
    }));
  }

  readAppForm() {
    const q = (s) => this.el.querySelector(s);
    return {
      id: this.editApp >= 0 ? this.cfg.launcher.apps[this.editApp].id : '',
      name: q('.af-name').value.trim(),
      kind: q('.af-kind').value,
      target: q('.af-target').value.trim(),
      args: q('.af-args').value.trim(),
      aliases: q('.af-aliases').value.split(',').map((s) => s.trim()).filter(Boolean),
      accepts: q('.af-accepts').value,
      clipboard: q('.af-clip').checked,
    };
  }

  async showStartMenu() {
    const box = this.el.querySelector('.lnk-list');
    if (!box) return;
    box.innerHTML = '<p class="note">Memuat…</p>';
    const [list] = await call(api.ListStartMenuApps);
    const items = list || [];
    box.innerHTML = `<input class="lnk-filter" placeholder="Cari aplikasi…"><div class="lnk-items"></div>`;
    const draw = (f) => {
      const want = f.toLowerCase();
      box.querySelector('.lnk-items').innerHTML = items.filter((x) => x.name.toLowerCase().includes(want)).slice(0, 60)
        .map((x) => `<button class="lnk" data-lnk="${esc(x.path)}" data-name="${esc(x.name)}">${esc(x.name)}</button>`).join('') || '<small>Tidak ketemu.</small>';
      box.querySelectorAll('[data-lnk]').forEach((b) => (b.onclick = () => {
        const name = b.dataset.name;
        const accepts = /\bword\b/i.test(name) ? 'docx' : /notepad/i.test(name) ? 'txt' : 'none';
        this.change((c) => { c.launcher.apps = [...(c.launcher.apps || []), { ...EMPTY_APP, name, target: b.dataset.lnk, accepts }]; }, true);
        this.flush();
        this.toast(`${name} ditambahkan`);
      }));
    };
    draw('');
    const fi = box.querySelector('.lnk-filter');
    fi.oninput = () => draw(fi.value);
    fi.focus();
  }

  confirmOnce(btn) {
    if (!btn.dataset.confirm) return true;
    if (btn.dataset.armed) return true;
    btn.dataset.armed = '1';
    const orig = btn.textContent;
    btn.textContent = btn.dataset.confirm;
    setTimeout(() => { if (btn.isConnected) { btn.textContent = orig; delete btn.dataset.armed; } }, 3000);
    return false;
  }

  async action(act, btn) {
    const c = this.cfg;
    const p = c.ai.provider;
    switch (act) {
      case 'color-auto':
        return this.change((cfg) => { cfg.pet.color = ''; }, true);
      case 'reset-pos':
        this.change((cfg) => { cfg.movement.anchorX = -1; cfg.movement.anchorY = -1; });
        this.onPlay('__reset_pos');
        return this.toast('Posisi dikembalikan');
      case 'save-key': {
        const v = this.el.querySelector('.key-input').value.trim();
        if (!v) return this.toast('Isi API key dulu', true);
        const [masked, err] = await call(api.SetAPIKey, p, v);
        if (err) return this.toast('Gagal: ' + err, true);
        this.keys[p] = masked;
        this.render();
        return this.toast('API key tersimpan 🔐');
      }
      case 'clear-key': {
        const [, err] = await call(api.SetAPIKey, p, '');
        if (err) return this.toast('Gagal: ' + err, true);
        this.keys[p] = '';
        this.render();
        return this.toast('API key dihapus');
      }
      case 'test': {
        await this.flush();
        const out = this.el.querySelector('.test-out');
        out.textContent = 'Mengetes…';
        const pc = p === 'openai' ? this.cfg.ai.openai : this.cfg.ai.anthropic;
        const [res, err] = await call(api.TestProvider, p, pc.baseURL || '');
        if (err || !res.ok) { out.textContent = '❌ ' + (err || res.error); out.className = 'test-out bad'; return; }
        this.models[p] = res.models;
        out.textContent = `✅ Terhubung — ${res.models.length} model`;
        out.className = 'test-out ok';
        if (!this.voice.checking && !this.voice.checkedAt) call(api.RecheckVoice).then(([info]) => info && this.setVoiceInfo(info));
        const dl = this.el.querySelector('#model-list');
        if (dl) dl.innerHTML = res.models.map((m) => `<option value="${esc(m)}">`).join('');
        if (p === 'openai' && !this.cfg.ai.openai.model && res.models.length) {
          this.change((cfg) => { cfg.ai.openai.model = res.models[0]; }, true);
        }
        return;
      }
      case 'wipe-all': {
        if (!this.confirmOnce(btn)) return;
        const [, err] = await call(api.WipeAllData);
        return this.toast(err ? 'Gagal: ' + err : 'Data pribadi dihapus', !!err);
      }
      case 'mem-add': {
        const kind = this.el.querySelector('.mem-kind').value;
        const text = this.el.querySelector('.mem-text').value.trim();
        if (!text) return;
        const [, err] = await call(api.AddMemory, kind, text);
        if (err) return this.toast(err, true);
        return this.loadMemories();
      }
      case 'mem-wipe': {
        if (!this.confirmOnce(btn)) return;
        await call(api.WipeMemories);
        return this.loadMemories();
      }
      case 'anim-import': {
        const txt = this.el.querySelector('.anim-import').value;
        const [m, err] = await call(api.ImportAnimation, txt);
        if (err) return this.toast('Ditolak: ' + err, true);
        this.toast(`Animasi "${m.name}" diimport`);
        return this.loadAnims();
      }
      case 'open-data':
        return call(api.OpenDataFolder);
      case 'voice-recheck': {
        await this.flush();
        this.voice = { ...this.voice, checking: true };
        this.setVoiceInfo(this.voice);
        const [info, err] = await call(api.RecheckVoice);
        if (err) return this.toast('Gagal: ' + err, true);
        return this.setVoiceInfo(info);
      }
      case 'mic-test': {
        const out = this.el.querySelector('.mic-out');
        const bar = this.el.querySelector('.mic-meter i');
        if (bar) bar.classList.remove('talk');
        out.textContent = 'Coba bicara…';
        btn.disabled = true;
        const [r, err] = await call(api.TestMic, 4);
        btn.disabled = false;
        if (!out.isConnected) return;
        if (bar) bar.style.width = '0%';
        if (err || r.error) { out.textContent = '❌ ' + (err || r.error); return; }
        out.textContent = r.speech ? '✅ Suaramu terdengar jelas' : r.peak > 0.02 ? '⚠️ Terdengar pelan — dekatkan mikrofon' : '⚠️ Tidak ada suara masuk — cek mikrofon default Windows';
        return;
      }
      case 'tts-try': {
        if (!this.speaker || !this.speaker.available()) return this.toast('Text-to-speech tidak tersedia', true);
        const v = this.cfg.ai.voice;
        const name = this.cfg.pet.name || 'Mochi';
        const text = this.cfg.pet.language === 'en' ? `Hi! I'm ${name}. This is my voice.` : `Halo! Aku ${name}. Begini suaraku, lucu kan?`;
        return this.speaker.speak(text, { lang: this.cfg.pet.language, voice: v.ttsVoice, pitch: v.ttsPitch, rate: v.ttsRate });
      }
      case 'app-new':
        this.editApp = -1;
        return this.render();
      case 'app-cancel':
        this.editApp = null;
        return this.render();
      case 'app-startmenu':
        return this.showStartMenu();
      case 'app-browse': {
        const [path] = await call(api.PickProgram);
        if (!path) return;
        const t = this.el.querySelector('.af-target');
        t.value = path;
        const n = this.el.querySelector('.af-name');
        if (n && !n.value) n.value = path.split(/[\\/]/).pop().replace(/\.(exe|lnk)$/i, '');
        return;
      }
      case 'app-try': {
        const app = this.readAppForm();
        if (!app.target) return this.toast('Isi target dulu', true);
        const [msg] = await call(api.TestLaunch, { ...app, name: app.name || 'tes' });
        return this.toast(msg ? 'Gagal: ' + msg : 'Membuka…', !!msg);
      }
      case 'app-save': {
        const app = this.readAppForm();
        if (!app.name || !app.target) return this.toast('Nama & target wajib diisi', true);
        if (app.kind === 'website' && !/^https?:\/\//i.test(app.target)) return this.toast('Website harus diawali http:// atau https://', true);
        const i = this.editApp;
        this.editApp = null;
        this.change((cfg) => {
          const list = [...(cfg.launcher.apps || [])];
          if (i >= 0) list[i] = app; else list.push(app);
          cfg.launcher.apps = list;
        }, true);
        await this.flush();
        return this.toast(`${app.name} disimpan`);
      }
    }
  }

  async loadMemories() {
    const list = this.el.querySelector('.mem-list');
    const [mems, err] = await call(api.ListMemories);
    if (!list) return;
    if (err) { list.innerHTML = `<p class="note bad">${esc(err)}</p>`; return; }
    const kinds = { habit: 'kebiasaan', preference: 'preferensi', fact: 'fakta', goal: 'tujuan' };
    list.innerHTML = mems.length ? mems.map((m) => `
      <div class="mem" data-id="${m.id}">
        <span class="badge ${esc(m.kind)}">${esc(kinds[m.kind] || m.kind)}</span>
        <span class="mem-content" contenteditable="true" spellcheck="false">${esc(m.content)}</span>
        <small>${m.source === 'ai' ? '🤖' : '✍️'}</small>
        <button class="icon" data-del="${m.id}" title="Hapus">🗑</button>
      </div>`).join('') : '<p class="note">Belum ada memori. Ngobrol atau izinkan pengamatan agar aku belajar kebiasaanmu.</p>';
    list.querySelectorAll('[data-del]').forEach((b) => (b.onclick = async () => { await call(api.DeleteMemory, Number(b.dataset.del)); this.loadMemories(); }));
    list.querySelectorAll('.mem').forEach((row) => {
      const content = row.querySelector('.mem-content');
      const kind = row.querySelector('.badge').className.split(' ')[1];
      content.addEventListener('blur', async () => {
        const txt = content.textContent.trim();
        if (txt) await call(api.UpdateMemory, Number(row.dataset.id), kind, txt);
      });
      content.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); content.blur(); } });
    });
    if (this.cfg.privacy.watchActivity) {
      const [st] = await call(api.ActivitySummary);
      const box = this.el.querySelector('.stats');
      if (box && st) {
        box.innerHTML = st.topApps && st.topApps.length ? `
          <p>Biasanya mulai: <b>${esc(st.typicalStart || '-')}</b> · hari aktif: ${st.activeDays}</p>
          ${st.topApps.map((a) => `<div class="bar"><span>${esc(a.app)}</span><i style="width:${Math.min(100, (a.minutes / st.topApps[0].minutes) * 100)}%"></i><em>${Math.round(a.minutes)} mnt</em></div>`).join('')}`
          : '<p class="note">Belum ada data.</p>';
      }
    }
  }

  async loadAnims() {
    const list = this.el.querySelector('.anim-list');
    const [anims, err] = await call(api.ListAllAnimations);
    if (!list) return;
    if (err) { list.innerHTML = `<p class="note bad">${esc(err)}</p>`; return; }
    list.innerHTML = anims.map((a) => `
      <div class="anim">
        <button class="icon" data-play="${esc(a.name)}" title="Mainkan">▶</button>
        <div><b>${esc(a.name)}</b> <span class="badge ${a.builtin ? 'builtin' : 'ai'}">${a.builtin ? 'bawaan' : esc(a.target)}</span><small>${esc(a.description)}</small></div>
        ${a.builtin ? '' : `<button class="icon" data-export="${esc(a.target)}|${esc(a.name)}" title="Salin JSON">⧉</button>
        <button class="icon" data-delanim="${esc(a.target)}|${esc(a.name)}" title="Hapus">🗑</button>`}
      </div>`).join('');
    list.querySelectorAll('[data-play]').forEach((b) => (b.onclick = () => this.onPlay(b.dataset.play)));
    list.querySelectorAll('[data-export]').forEach((b) => (b.onclick = async () => {
      const [t, n] = b.dataset.export.split('|');
      const [json, err2] = await call(api.ExportAnimation, t, n);
      if (err2) return this.toast(err2, true);
      try { await navigator.clipboard.writeText(json); this.toast('JSON disalin ke clipboard'); } catch { this.toast('Gagal menyalin', true); }
    }));
    list.querySelectorAll('[data-delanim]').forEach((b) => (b.onclick = async () => {
      const [t, n] = b.dataset.delanim.split('|');
      const [, err2] = await call(api.DeleteAnimation, t, n);
      if (err2) return this.toast(err2, true);
      this.loadAnims();
    }));
  }
}
