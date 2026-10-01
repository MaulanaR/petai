# PetAI 🫧

Desktop pet lucu bergaya doodle (three.js) yang berkeliaran di layar Windows, bisa diklik, diseret,
diajak ngobrol, dan terhubung ke AI milikmu sendiri (BYOK: Anthropic atau OpenAI/kompatibel).

- **Tanpa jendela aplikasi** — overlay transparan always-on-top yang tembus klik di area kosong;
  akses lewat ikon tray. Fokus keyboard tidak pernah dicuri.
- **3 karakter**: Blob Jeli, Kucing, Anak Ayam (warna, nama, kepribadian, ukuran bisa diatur).
- **3 mode gerak**: 📍 Diam di tempat · 🚶 Jalan di atas taskbar (bisa lompat & nangkring di atas
  jendela aktif) · 🎈 Melayang bebas.
- **Interaksi**: klik (reaksi), klik ganda (chat), seret (diangkat & dijatuhkan), klik kanan (menu),
  "elus" dengan menggosok kursor di atas pet.
- **AI (opsional, BYOK)**: komentar, saran, chat, memori kebiasaan. Key disimpan di Windows
  Credential Manager, bukan di file.
- **Animasi buatan AI**: AI menulis gerakan dalam DSL JSON (data, bukan kode) → divalidasi →
  disimpan lokal di `%APPDATA%\PetAI\animations` → dipakai ulang tanpa generate ulang.
- **Privasi opt-in**: izin melihat aktivitas (nama app + judul jendela, disensor) default OFF;
  screenshot ke vision model adalah toggle terpisah (default OFF, tidak pernah disimpan ke disk);
  daftar blokir (password manager, perbankan, incognito…); retensi log; tombol hapus semua data.
  Sembunyi otomatis saat game/presentasi/fullscreen.

## Menjalankan

Prasyarat: Windows 10/11, Go 1.22+, Node 18+, WebView2 Runtime (bawaan Windows 11).

```bash
powershell -ExecutionPolicy Bypass -File scripts/build.ps1
```

Hasil: `build\bin\petai.exe`. Jalankan, lalu klik kanan ikon tray → **Pengaturan → AI** untuk
memasukkan API key. Pintasan **Ctrl+Alt+P** membuka chat.

Mode pengembangan (hot reload):

```bash
powershell -ExecutionPolicy Bypass -File scripts/dev.ps1
```

> Skrip memakai `go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0` karena Wails CLI v2.9.1
> gagal membuat bindings di Go 1.26+. Alternatif: `go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0`.

Preview karakter & animasi di browser biasa (tanpa Wails): `npm --prefix frontend run dev` lalu buka
`http://localhost:5173/preview.html`.

Installer NSIS: `scripts/build.ps1 -Installer` (butuh `makensis` di PATH).

## Cek koneksi AI

Tes key/model/base URL tanpa membuka pet (key dibaca dari Credential Manager, tidak pernah dicetak):

```bash
go run ./cmd/petai-check -provider openai -model <model> -base <url> -chat "halo!"
```

Menampilkan: daftar model, balasan chat, dan komentar pet tentang jendela yang sedang aktif
(`-fg-app code.exe -fg-title "..."` untuk simulasi). `-save` menulis pengaturan yang lolos tes ke config app.

## Struktur

```
main.go, app.go            Wails app + method yang dipanggil frontend
internal/overlay           Win32: overlay transparan, click-through dinamis, fokus, DPI
internal/watcher           Jendela aktif, idle, DND/fullscreen, screenshot, sensor & blocklist
internal/brain             Kapan pet bicara, prompt, structured output, animasi & memori
internal/ai                Provider Anthropic (anthropic-sdk-go) & OpenAI (openai-go v3)
internal/anim              DSL animasi, validasi, library lokal + animasi bawaan (builtin/*.json)
internal/store             SQLite: aktivitas, memori, chat
internal/config, secrets   config.json & Windows Credential Manager
internal/tray, sys         Ikon tray, autostart, hotkey
internal/debugapi          API QA lokal (hanya bila PETAI_DEBUG_ADDR diset)
frontend/src               three.js: karakter, player DSL, behavior, bubble, menu, pengaturan
qa/                        Kontrak uji + harness QA independen
```

## Data

`%APPDATA%\PetAI\` → `config.json`, `petai.db`, `animations\`, `logs\petai.log`.
Override dengan env `PETAI_DATA_DIR`. Lihat `qa/CONTRACT.md` untuk env uji lainnya
(`PETAI_DEBUG_ADDR`, `PETAI_FAST`, base URL & key override).

## Model AI

Default Anthropic `claude-opus-5-5` (effort `low` untuk obrolan, `medium` untuk membuat animasi &
merangkum memori). Pilih `claude-sonnet-5-5` / `claude-haiku-4-5` di Pengaturan untuk lebih hemat.
OpenAI: pilih model setelah **Tes koneksi**; base URL bisa diarahkan ke OpenRouter/Ollama/LM Studio.
Server-side refusal fallback aktif otomatis untuk model Claude yang mendukungnya.
