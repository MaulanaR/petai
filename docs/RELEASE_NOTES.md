## PetAI v0.1.0 — rilis pertama 🫧

Desktop pet lucu bergaya doodle 3D (three.js) yang tinggal di layar Windows, bisa diklik, diseret,
diajak ngobrol, dan mengomentari apa yang sedang kamu kerjakan — memakai AI milikmu sendiri (BYOK).

![Chat & komentar aktivitas](https://github.com/MaulanaR/petai/blob/main/docs/demo-chat.gif?raw=true)

### Unduh

| File | Untuk |
|---|---|
| `PetAI-v0.1.0-windows-amd64-setup.exe` | Installer (shortcut Start Menu, uninstaller, memasang WebView2 bila belum ada) |
| `PetAI-v0.1.0-windows-amd64-portable.exe` | Versi portable — langsung jalan tanpa instal |
| `SHA256SUMS.txt` | Checksum untuk verifikasi |

Windows 10/11 64-bit. Aplikasi belum ditandatangani (unsigned): jika SmartScreen muncul, pilih
**More info → Run anyway**.

### Isi rilis
- Overlay transparan tanpa jendela: pet berkeliaran di atas taskbar, area kosong tembus klik, fokus keyboard tidak dicuri.
- 3 karakter (Blob Jeli, Kucing, Anak Ayam) · 3 mode gerak (diam, jalan + nangkring di jendela, melayang bebas).
- Klik, klik ganda (chat), seret, elus, klik kanan (menu), ikon tray, `Ctrl+Alt+P`.
- AI BYOK: Anthropic (Claude) atau OpenAI / endpoint kompatibel OpenAI (OpenRouter, Ollama, LM Studio, dll). Key di Windows Credential Manager.
- Komentar & saran sesuai aplikasi/jendela yang aktif (opt-in), screenshot ke model vision (opt-in terpisah), memori kebiasaan.
- Gerakan baru dibuat AI dalam DSL JSON, divalidasi, disimpan lokal, dan dipakai ulang tanpa generate ulang.
- Privasi: default tidak mengamati apa pun; sensor judul jendela; daftar blokir (password manager, perbankan, chat, incognito…); sembunyi otomatis saat fullscreen.

Panduan lengkap: lihat [README](https://github.com/MaulanaR/petai#readme).
