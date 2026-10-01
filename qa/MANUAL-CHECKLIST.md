# PetAI - Manual Acceptance Checklist

These are the checks a script cannot judge: how the pet looks and feels, real keys, the real installer.
Tick each box. Note anything that feels off, even when the box technically passes. The IDs map to
`qa/ACCEPTANCE.md`.

Two ways to run the app for these checks:

* **Real use** (M-18 and M-20): start PetAI normally and enter your own keys in Settings.
* **Safe sandbox** (everything else): uses the mock AI, fake keys and a temporary data folder, so it costs
  nothing and leaves your real settings alone:
  ```
  powershell -ExecutionPolicy Bypass -File qa\scripts\Start-ManualSession.ps1            # normal answers
  powershell -ExecutionPolicy Bypass -File qa\scripts\Start-ManualSession.ps1 -Scenario xss
  ```
  The window that opens prints the commands for switching mock answers (e.g. `long_speech`, `new_anim`, `http401`)
  and for triggering a comment immediately. Press Enter there to stop everything.

Screenshots from the automated run (`qa/artifacts/run-*/`) help with M-02, M-03, M-09 and M-10.

---

## Look and feel

- [ ] **M-01 three.js scene, smooth motion** (AC-03). The pet is a small 3D/toon figure, not a flat image. Idle
  breathing, blinking and walking look smooth: no stutter, no flicker, no black square around the pet.
- [ ] **M-02 Cute doodle style** (AC-11). Rounded shapes, outline, friendly eyes and mouth: "doodle lucu".
  Check at Windows display scaling 100% and 150% (Settings > Display). Edges stay crisp and the pet is not tiny or blurry.
- [ ] **M-03 Three distinct characters** (AC-31). In Settings > Karakter, switch blob > cat > chick. Each is
  recognisable at a glance (cat: ears and tail; chick: beak and small wings; blob: round). All three are equally cute,
  and the colour and name settings apply.
- [ ] **M-05 Notices the cursor** (AC-18). Move the mouse near the pet: eyes or head follow it, even though you can still
  click the desktop next to it.

## Desktop presence

- [ ] **M-04 No window to manage; tray works** (AC-20, AC-27, AC-29). Start PetAI while typing in Notepad: you can keep typing, because the pet never takes the focus. No taskbar button, and the pet does not appear in Alt-Tab or
  Win-Tab. The tray icon exists. Its menu has Tampilkan/Sembunyikan, Chat, Settings, Pause pengamatan and Keluar, and each one
  works. **Keluar** really exits: no `petai.exe` left in Task Manager.
- [ ] Click-through feels natural: desktop icons, browser tabs and the taskbar under or next to the pet can be clicked
  normally. Typing in another app is never interrupted when the pet moves or speaks.

## Interaction

- [ ] **M-06 Click** (AC-12). A single click gives a visible, cute reaction: bounce, surprised face or similar. Sometimes
  a short line. Rapid clicking does not break it.
- [ ] **M-07 Petting** (AC-17). Wiggle the mouse quickly over the pet: hearts or a happy face appear.
- [ ] **M-08 Drag and drop** (AC-15). Drag the pet around: it dangles from the cursor. Drop it:
  `ground` falls down and lands on the taskbar; `free` stays floating there; `stay` stays exactly where dropped and
  stays there after a restart. Dragging it off-screen brings it back fully on-screen.
- [ ] **M-09 Right-click menu** (AC-16). The menu appears next to the pet, fully on-screen even at screen edges, and
  shows Chat, Tidur, Mode gerak, Settings, Sembunyikan 1 jam, Keluar. Each item does what it says. "Sembunyikan 1 jam" hides
  the pet, and it comes back. Escape or a click elsewhere closes the menu.
- [ ] **M-10 Chat** (AC-14). Double-click the pet: a text box appears and already has focus. Type a sentence and press Enter.
  The reply appears in a readable bubble (typewriter effect OK) that does not cover the pet's face, wraps long text and
  disappears on its own. Escape closes the box, and keyboard focus returns to the app you were using.

## AI comments and suggestions

- [ ] **M-11 Spontaneous comments and suggestions** (AC-60, AC-61). With "izinkan lihat aktivitas" ON, work normally for
  about 30 minutes (real build, real key) or use the sandbox with FAST timing. The pet comments now and then. The comments
  relate to what you are doing, are in Indonesian (`pet.language=id`), are short and friendly, and are not annoying.
  A suggestion (e.g. take a break after a long focus) looks different from small talk.
- [ ] **M-12 Weird AI text is shown safely** (AC-65). Sandbox `-Scenario xss`: the bubble shows the HTML tags literally as
  text. Nothing becomes bold, no image box appears, no script runs. `-Scenario long_speech`: the long text is wrapped or
  scrollable and fully readable, and the bubble stays on-screen.

## Movement and idle life

- [ ] **M-13 Ground mode** (AC-34, AC-37). The pet walks along the top of the taskbar, sometimes jumps onto the top edge of
  the active window, and falls when that window moves, is minimised or closed. It never walks inside other windows.
- [ ] **M-14 Settings panel** (AC-36). Open Settings (tray or right-click). The sections Karakter, Gerak, AI, Privasi,
  Memory, Animasi and Umum exist, are readable and are not cut off. Changes apply immediately and are still there after a restart.
  The panel can be closed, and focus returns to your app.
- [ ] **M-15 Sleeps when you are away** (AC-38). Leave the PC idle past the configured time: the pet goes to sleep (zzz).
  Move the mouse: it wakes up.

## Privacy and memory

- [ ] **M-16 Privacy panel** (AC-50, AC-55, AC-58, AC-73). On a fresh install, "izinkan lihat aktivitas" and "screenshot"
  are both OFF, each with a plain-language explanation. You can edit the blocklist. The memory list shows what the pet
  remembers, single items can be edited and deleted, "lupakan semua" works, and "hapus semua data" really empties activity
  and memories (check again after a restart).
- [ ] **M-17 Quiet when it should be** (AC-64, AC-58). Play a fullscreen video or game, or start a PowerPoint slideshow:
  the pet hides or sleeps and says nothing. Afterwards it comes back. Tray "Pause pengamatan" stops watching until you
  resume it.
- [ ] **M-19 Screenshot opt-in** (AC-56, AC-28). Enable screenshots: before each capture the pet shows the "glasses"
  indicator for about 3 seconds. Take a Windows screenshot (Win+Shift+S) or share your screen in a meeting with the default
  setting: the pet is NOT in the image. No image files appear in `%APPDATA%\PetAI`.

## Real BYOK (your own keys; costs a few cents)

- [ ] **M-18 Anthropic and OpenAI with your own keys** (AC-40, AC-41, AC-43, AC-45, AC-48).
  1. Settings > AI: choose Anthropic and paste your key. The field shows it masked. "Test koneksi" reports success, and the model
     dropdown lists real models (default claude-opus-5-5). Chat with the pet.
  2. Switch to OpenAI, paste your key, pick a model from the dropdown, test, and chat.
  3. Run `cmdkey /list | findstr /i petai`: entries for `PetAI` anthropic and openai exist.
  4. Search `%APPDATA%\PetAI` for your key: `findstr /s /m /c:"<first 12 chars of key>" "%APPDATA%\PetAI\*"` finds **nothing**.
  5. Enter a wrong key: a clear, friendly error appears (not a crash, not silence) and the pet keeps living.
  6. Optional: an OpenAI-compatible server (Ollama `http://localhost:11434/v1`, LM Studio, OpenRouter) via the base URL field works.

## Animations made by the AI

- [ ] **M-21 Animation library** (AC-80, AC-88). Ask in chat "coba bikin gerakan baru: muter-muter senang". A new movement
  plays. Settings > Animasi lists it, and you can preview, delete, export and import it. Ask for the same movement again: it plays
  immediately, with no waiting for the AI.

## Install and performance

- [ ] **M-20 Installer** (AC-05). On a clean Windows machine (or Windows Sandbox), run `build\bin\*installer*.exe`.
  It installs without a developer toolchain, adds a Start-menu entry, the pet appears on first start, and nothing autostarts
  unless enabled. Uninstall removes the program. Decide whether user data in `%APPDATA%\PetAI` should be kept or offered for
  removal, and check that the behaviour matches.
- [ ] **M-22 Light on the PC** (AC-91). Task Manager, Details tab, with petai.exe and its msedgewebview2.exe children: while
  idle, CPU stays under about 3% and GPU stays low. Fans do not spin up. After 2+ hours memory has not grown steadily, and on
  a laptop battery use is acceptable.
- [ ] **M-24 Multi-monitor and scaling.** With two monitors and/or 125% to 150% scaling, the pet is drawn and clickable at the
  same spot (no offset between the picture and the click area). "monitor" in Settings > Umum moves it to the chosen screen.

---

Sign-off: ______________________   date: ____________   build/commit: ____________
