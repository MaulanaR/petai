## PetAI v0.3.0 — ngobrol pakai suara 🎙️ & membuka aplikasi 🚀

![Mode suara](https://github.com/MaulanaR/petai/blob/main/docs/voice-mode.png?raw=true)

### Baru

**🎙️ Ngobrol pakai suara** — klik 2× pet (atau `Ctrl+Alt+V`), bicara, dan pet menjawab dengan suara
(Microsoft Andika untuk Bahasa Indonesia) sambil mulutnya bergerak. Percakapan bersambung sampai kamu
menekan ✖ / `Esc`, mengklik pet, bilang "udah ya", atau diam 30 detik. Klik pet saat ia bicara untuk memotong.

- **Hanya aktif bila model AI-mu bisa mendengar audio.** Setiap kali provider / base URL / model / API key
  berubah, PetAI mengetes model dengan rekaman bawaan "satu, dua, tiga" (bukan suaramu). Bila model tidak
  menerima audio, klik 2× tetap membuka **chat teks** seperti biasa, dan tab 🎙️ Voice menampilkan alasannya
  beserta model yang mendukung audio (bila endpoint menyebutkannya).
- **Model untuk suara** bisa dibedakan dari model chat (mis. chat pakai model pintar, suara pakai model yang
  menerima `input_audio`).
- Mikrofon hanya aktif selama mode suara (titik merah), rekaman tidak disimpan ke disk.

**🚀 Pet bisa membuka aplikasi** — daftarkan aplikasi di *Pengaturan → 🚀 Aplikasi* (preset Notepad, Word,
Google Docs, Google Search, YouTube, Kalkulator, pilih dari Start Menu, atau tambah manual), lalu suruh lewat
chat atau suara:

| Kamu bilang | Hasil |
|---|---|
| "Tolong buka Word, catat notulensi meeting hari ini" | `.docx` template notulensi (Waktu & Tempat, Peserta, Agenda, Pembahasan, Keputusan, Action Items) terbuka di Word |
| "Buka Google Docs, catat notulensi" | `docs.new` + template di clipboard → tinggal Ctrl+V |
| "Cari resep rendang di Google" | Tab pencarian Google |

Pet hanya bisa membuka aplikasi yang ada di daftarmu; path & argumen tidak pernah berasal dari AI.
Opsional: *Tanya dulu sebelum membuka aplikasi* (✔/✖ di bubble).

**Lainnya**
- Tab Pengaturan baru: 🎙️ Voice (status model, tes ulang, tes mikrofon, suara/nada/kecepatan TTS, jeda diam)
  dan 🚀 Aplikasi. Status mode suara juga tampil di tab 🧠 AI.
- Selama ngobrol pakai suara, komentar otomatis dan aktivitas acak ditahan dulu.
- Menu klik kanan & tray: *Ngobrol (suara)* dan *Ajak ngobrol (ketik)*.
- `petai-check -voice` untuk mengetes kemampuan audio model dari command line.

### Unduh

| File | Untuk |
|---|---|
| `PetAI-v0.3.0-windows-amd64-setup.exe` | Installer (shortcut Start Menu, uninstaller, memasang WebView2 bila perlu) |
| `PetAI-v0.3.0-windows-amd64-portable.exe` | Portable — langsung jalan tanpa instal |
| `SHA256SUMS.txt` | Checksum |

Windows 10/11 64-bit. Belum ditandatangani: jika SmartScreen muncul, pilih **More info → Run anyway**.
Pengaturan, API key, memori, dan animasi dari versi sebelumnya tetap terpakai.

Panduan lengkap: [README](https://github.com/MaulanaR/petai#readme).

---
Dibuat oleh **Maulana Rahman** — [maulanar.my.id](https://maulanar.my.id)
