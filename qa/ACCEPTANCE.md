# PetAI - Acceptance Criteria (independent QA)

Source of truth: the user's original request (Indonesian, quoted per row) and the follow-up decisions
(watching = app + window title, plus a separate screenshot opt-in; at least 3 characters (blob, cat, chick);
movement setting free / stay / ground). The binding test interface is `qa/CONTRACT.md`. Automated checks use
only that contract plus black-box observation (Win32, screen pixels, the mock AI server's request log, files
in the data dir).

Severity: **blocker** = the user would reject the product; **major** = a clearly broken or missing expectation;
**minor** = polish, or a secondary part of an expectation.

Verification: `AUTO <script>` = `qa/scripts/<script>.ps1`, run by `Run-Acceptance.ps1`. `MANUAL M-xx` = step in
`qa/MANUAL-CHECKLIST.md`. Most rows have both: AUTO proves the mechanism, MANUAL covers how it looks and feels.

How to run everything (about 20 min, the harness takes over mouse and keyboard; only the pet and its own QA
windows receive input):

```
powershell -ExecutionPolicy Bypass -File qa\scripts\Run-Acceptance.ps1
```

The result goes to `qa/REPORT.md` (PASS / FAIL / WARN / SKIP / MANUAL / NOT RUN per ID). Useful switches:
`-Only Test-Static,Test-AI`, `-Skip Test-Performance`, `-NoInput` (no mouse or keyboard injection), `-AppExe <path>`.

Interpretation notes:
* The `/debug/ui` endpoint is used only to tell "input not detected" apart from "UI broken". The real-input checks stay authoritative.
* "tanpa harus pengguna membuat apliaksinya" is read as "without the user having to *open* an app window". The pet
  lives directly on the desktop: no normal window, no taskbar button, clicks pass through everywhere except the pet.
* "bisa iddle gitu" is read as "it lives and moves on its own while idle": autonomous wandering and idle animations,
  with no user action needed.
* "BYOK" means the user's own key, stored in the OS keyring (Windows Credential Manager) and never written to app files.

## A. Stack - "membuat sebuah aplikasi golang", "golang dengan wails sepertinya cocok, dengan FE vanila js saja karena hanya butuh threeJS", "untuk animasinya gunakan threeJS"

| ID | User expectation (source) | Pass condition | Verification | Severity |
|---|---|---|---|---|
| AC-01 | Go app built with Wails ("aplikasi golang", "golang dengan wails") | go.mod requires `github.com/wailsapp/wails/v2`, no v3; `wails.Run(` used; `wails.json` present | AUTO Test-Static | blocker |
| AC-02 | Plain vanilla JS frontend ("FE vanila js saja") | `frontend/package.json` runtime deps = only `three`; devDeps only `vite` (`@types/three` tolerated); no React/Vue/Svelte/Angular/Preact/Lit/jQuery/TypeScript etc.; no .ts/.tsx/.jsx/.vue/.svelte sources in `frontend/src` | AUTO Test-Static | major |
| AC-03 | Animations rendered with three.js ("gunakan threeJS agar bisa dinamis") | frontend imports `three` and creates a `WebGLRenderer`; the pet on screen is a three.js scene (toon/doodle material expected) | AUTO Test-Static + MANUAL M-01 | blocker |
| AC-04 | Healthy codebase | `go vet ./...` and `go test ./...` pass offline (GOPROXY=off); the plan's unit tests exist (validate, redact, engine, providers, memory) | AUTO Test-Static | major |
| AC-05 | Installable Windows app | `wails build -nsis` produces `build/bin/*installer*.exe`; installs, starts and uninstalls cleanly on a clean machine | AUTO Test-Static (artifact) + MANUAL M-20 | major |

## B. The pet and interaction - "PET / seperti doodle lucu yang dapat diklik secara interaktif"

| ID | User expectation (source) | Pass condition | Verification | Severity |
|---|---|---|---|---|
| AC-10 | A pet is visible on the desktop right after start ("PET ... doodle lucu") | Overlay visible, not cloaked; `/debug/state pet.visible=true`; at least 3% non-backdrop pixels inside the reported pet bbox on a real screenshot (pet is drawn where the app says it is) | AUTO Probe-Window, Capture-Screen | blocker |
| AC-11 | It looks like a cute doodle ("doodle lucu") | Toon/outlined, rounded, expressive eyes; reads as cute and friendly, not a debug primitive; crisp at 100% and 150% scaling | MANUAL M-02 | major |
| AC-12 | Clicking the pet does something ("dapat diklik secara interaktif") | Real left click on the pet changes `pet.state`/`pet.animation` within 1.2 s (local reaction, no AI needed) | AUTO Test-Interaction + MANUAL M-06 | major |
| AC-13 | Clicking the pet never disturbs the user's work | After clicking the pet, the previously active window is still the foreground window (WS_EX_NOACTIVATE) | AUTO Test-Interaction | major |
| AC-14 | Double-click opens chat; the pet answers ("diklik secara interaktif" + AI comments) | Double-click gives keyboard focus to a chat input; typed text + Enter reaches the AI (seen in the mock log); reply shown in a bubble; after closing, the overlay returns to NOACTIVATE (no focus stealing) | AUTO Test-Interaction + MANUAL M-10 | major |
| AC-15 | The pet can be picked up and moved | Real drag: pet follows the cursor; drop in `stay` -> stays and anchor is updated; `free` -> floats where dropped; `ground` -> falls back to the taskbar line | AUTO Test-Interaction + MANUAL M-08 | major |
| AC-16 | Right-click gives a menu | Right-click opens a menu next to the pet with Chat, Tidur, Mode gerak, Settings, Sembunyikan 1 jam, Keluar; every item works; Escape / click elsewhere closes it | AUTO Test-Interaction (pixel diff + screenshot) + MANUAL M-09 | major |
| AC-17 | Petting the pet (fast mouse wiggle) makes it happy | Wiggling the cursor over the pet shows a happy reaction (hearts or happy animation) | AUTO Test-Interaction (observational) + MANUAL M-07 | minor |
| AC-18 | The pet notices the cursor | Eyes or head follow the cursor while it is near the pet, even though the overlay is click-through | MANUAL M-05 | minor |
| AC-19 | Settings are reachable and give focus back | Settings panel (tray / menu / `POST /debug/ui {open:settings}`) takes keyboard focus; Esc closes it and focus plus WS_EX_NOACTIVATE return to the previous window | AUTO Test-Interaction + MANUAL M-14 | major |

## C. Lives on the desktop - "pet bisa berkeliana bebas pada tampilan pengguna, tanpa harus pengguna membuat apliaksinya"

| ID | User expectation (source) | Pass condition | Verification | Severity |
|---|---|---|---|---|
| AC-20 | No app window to manage ("tanpa harus ... aplikasinya") | Overlay has WS_EX_TOOLWINDOW and no WS_EX_APPWINDOW; no petai window would get a taskbar or Alt-Tab entry | AUTO Probe-Window + MANUAL M-04 | blocker |
| AC-21 | Only the pet is drawn; the rest of the screen stays visible | With a solid backdrop under the overlay, regions at least 300 px from the pet show the backdrop unchanged (at most 2% other pixels): transparent, never black or grey | AUTO Capture-Screen | blocker |
| AC-22 | The user can keep working through the overlay | Cursor away from the pet -> WS_EX_TRANSPARENT set (or region mode); `WindowFromPoint` at empty points never returns the overlay; a REAL click on an empty area reaches the window underneath; restored within 1.5 s after leaving the pet | AUTO Test-ClickThrough | blocker |
| AC-23 | The pet itself is clickable | Cursor over the pet -> overlay interactive within 1.5 s (`window.interactive=true` / TRANSPARENT cleared); `WindowFromPoint(pet)` = overlay; a real click on the pet does not fall through | AUTO Test-ClickThrough, Test-Interaction | blocker |
| AC-24 | Pet stays on top of other windows | WS_EX_TOPMOST set | AUTO Probe-Window | major |
| AC-25 | Pet can go anywhere on the screen, and the taskbar keeps working | Exactly one overlay, frameless (no caption or thick frame), rect = WORK AREA of the selected monitor (primary by default; 1 px shorter than the monitor when the taskbar auto-hides), within 2 px; never exactly monitor-sized (Windows must not report a fullscreen app); `/debug/state window.rect` matches | AUTO Probe-Window | major |
| AC-26 | Contract ex-styles at rest | LAYERED, NOACTIVATE, TOOLWINDOW, TOPMOST set; TRANSPARENT set while the cursor is away (exstyle mode) | AUTO Probe-Window | major |
| AC-27 | Reachable without a window: tray icon | Tray icon present with Tampilkan/Sembunyikan, Chat, Settings, Pause pengamatan, Keluar; all work; Keluar really exits (no process left) | MANUAL M-04 | major |
| AC-28 | The pet does not appear in the user's screen shares or screenshots by default | Default `excludeFromCapture=true` gives display affinity WDA_EXCLUDEFROMCAPTURE (0x11) | AUTO Probe-Window (launch A) + MANUAL M-19 | minor |
| AC-29 | Starting the pet never interrupts typing ("tanpa harus pengguna membuat apliaksinya") | After launch (and relaunch) the window that was in front before keeps the keyboard focus | AUTO Run-Acceptance (focus sentinel at every launch) | major |

## D. Idle life, characters, movement - "bisa iddle gitu", "berikan berbagai pilihan karakter, minimal 3", "bisa bebas melayang atau stay at one place"

| ID | User expectation (source) | Pass condition | Verification | Severity |
|---|---|---|---|---|
| AC-30 | Pet moves on its own while the user does nothing ("bisa iddle gitu", "berkeliaran bebas") | With no input at all: path of at least 80 px in 20 s in `free` and in `ground` | AUTO Test-Wander | blocker |
| AC-31 | At least 3 characters to choose from ("minimal 3") | `blob`, `cat`, `chick` each applied live via config (`pet.character` reported); each visible; silhouettes or colours clearly differ on screen; code modules exist | AUTO Test-Characters, Test-Static + MANUAL M-03 | blocker |
| AC-32 | Option to stay in one place ("stay at one place") | `stay`: bbox centre moves at most max(16 px, 15% of width) over 20 s, no input | AUTO Test-Wander | blocker |
| AC-33 | Option to float freely ("bebas melayang") | `free`: path at least 150 px in 20 s and uses vertical space (y-range at least 60 px, or on the floor less than 50% of the time): no gravity | AUTO Test-Wander | blocker |
| AC-34 | Walks on the taskbar (approved third mode) | `ground`: at least 80% of grounded samples have the pet bottom on the work-area bottom (taskbar top) or on a foreground window's top edge; walks at least 80 px in 20 s | AUTO Test-Wander | major |
| AC-35 | Never gets lost off-screen | In every mode, and after dragging past the screen edge, the bbox stays inside the monitor (2 px tolerance) | AUTO Test-Wander, Test-Interaction | major |
| AC-36 | Settings apply immediately and survive a restart | `POST /debug/config` mode is reflected in `pet.mode` at once; mode, character and name persist after restart | AUTO Test-Wander, Test-Persistence + MANUAL M-14 | major |
| AC-37 | Ground mode perches on windows | In `ground`, the pet sometimes sits on the top edge of the foreground window and falls when it moves or minimizes | AUTO Test-Wander (best effort) + MANUAL M-13 | minor |
| AC-38 | Pet sleeps when the user is away | After the idle threshold the pet sleeps (zzz) and wakes on activity | MANUAL M-15 | minor |

## E. AI with BYOK - "terhubung ke AI dengan BYOK, bisa antropic atau open ai"

| ID | User expectation (source) | Pass condition | Verification | Severity |
|---|---|---|---|---|
| AC-40 | Works with the user's own Anthropic key ("antropic") | `POST {base}/v1/messages`, header `x-api-key` = configured key (fingerprint match), `anthropic-version`, model = config (`claude-opus-5-5` default), `[task:pet_action]` + JSON context with occasion and localTime, reply parsed into a PetAction | AUTO Test-AI + MANUAL M-18 (real key) | blocker |
| AC-41 | Works with the user's own OpenAI key ("open ai") | `POST {base}/v1/chat/completions`, `Authorization: Bearer` = configured key, non-empty model, `response_format` json_schema strict, reply parsed | AUTO Test-AI + MANUAL M-18 (real key) | blocker |
| AC-42 | Switching provider in settings just works | Changing `ai.provider` live routes the next calls to the other provider, with no restart and no cross-sending | AUTO Test-AI | major |
| AC-43 | My key stays secret (BYOK) | Real keys go to Windows Credential Manager (service `PetAI`); fake env keys never appear in request bodies, `/debug/*` output, config.json, petai.db, logs, WebView2 profile, stdout or stderr; each key only goes to its own provider; nothing is written to the keyring when env keys are used | AUTO Test-AI, Test-Persistence, Test-Static, Run-Acceptance (cmdkey diff) + MANUAL M-18 | blocker |
| AC-44 | The user can talk to the pet ("komentar kepada usernya") | `/debug/chat` text reaches the AI; reply speech returned, for both providers | AUTO Test-AI | blocker |
| AC-45 | AI problems never break the pet | refusal, 401, 429, 500, 529/503, malformed JSON, wrong-shape JSON, dropped connection: app stays alive and responsive, no retry storm (at most 6 requests per trigger), raw model text not shown, the next call works again | AUTO Test-AI + MANUAL M-18 (bad key message) | major |
| AC-46 | A slow AI does not freeze the pet | During an 8 s AI call, `/debug/state` answers in at most 1.5 s every time | AUTO Test-AI | major |
| AC-47 | AI can be switched off | `ai.enabled=false`: zero network calls for trigger and chat; local animations still play | AUTO Test-AI | major |
| AC-48 | BYOK setup is usable | Settings: key field (masked), model dropdown from the Models API, "Test koneksi", custom base URL for OpenAI-compatible servers (OpenRouter, Ollama, LM Studio) | MANUAL M-18 | minor |
| AC-49 | Requests are valid for the real APIs | Every request uses structured output (Anthropic `output_config.format`, OpenAI strict json_schema) and passes the mock's real-API rules (required fields, strict-schema rules, header requirements); no 400 | AUTO Test-AI | major |

## F. Privacy and watching the PC - "ada settingannya apakah diizinkan si pet ini melihat interaksi pc yang sedang dilakukan"

| ID | User expectation (source) | Pass condition | Verification | Severity |
|---|---|---|---|---|
| AC-50 | Watching is a setting and OFF until I allow it ("apakah diizinkan") | Fresh data dir: `privacy.watchActivity=false` in config.json and `/debug/state` | AUTO Test-Privacy-Defaults + MANUAL M-16 | blocker |
| AC-51 | When not allowed, the pet sees nothing | watchActivity=false: no `activity` key, no image and no foreground title in ANY AI request (incl. screenshot_insight, app_switch, automatic calls); title not written to disk | AUTO Test-Privacy-Defaults, Test-AI | blocker |
| AC-52 | When allowed, the pet knows what I am doing | watchActivity=true: requests carry `activity {app, title}` of the real foreground window (exe name and title) | AUTO Test-AI | blocker |
| AC-53 | Sensitive bits in titles are hidden | Emails become `[email]`, digit runs of 6 or more become `[num]`; the raw values appear in no request body | AUTO Test-AI | major |
| AC-54 | Sensitive apps are never watched | Blocklisted foreground (title "bank", "BCA" in any case, "Mandiri", "InPrivate"; app KeePass.exe; a user-added word): no activity key, title text in no request and not stored on disk, no screenshot | AUTO Test-AI | blocker |
| AC-55 | Screenshots are a separate opt-in, OFF by default | Fresh install: `privacy.screenshots=false`; default config never sends an image | AUTO Test-Privacy-Defaults + MANUAL M-16 | blocker |
| AC-56 | Screenshots only when I enabled them | Image only for `screenshot_insight` with watchActivity=true AND screenshots=true (screenshots are a sub-toggle of the master permission) and a non-blocklisted foreground; JPEG, longest side at most 1280 px, one image; none for other occasions, none when only screenshots=true | AUTO Test-AI + MANUAL M-19 (glasses indicator) | blocker |
| AC-57 | Screenshots are not kept | No image files under the data dir after screenshot_insight | AUTO Test-AI | major |
| AC-58 | I stay in control of watching data | Tray "Pause pengamatan" stops watching; "hapus semua data" wipes activity and memories; activity older than retentionDays is purged on start | MANUAL M-16, M-17 | minor |

## G. Suggestions and comments - "pet ini bisa memberikan sugesti terkait apa yg sedang dilakukan", "memberikan semacam komentar kepada usernya"

| ID | User expectation (source) | Pass condition | Verification | Severity |
|---|---|---|---|---|
| AC-60 | The pet comments on its own ("memberikan semacam komentar") | With PETAI_FAST=1, watchActivity=true and app switching, at least 1 automatic AI comment in 55 s with no debug call | AUTO Test-Autonomy + MANUAL M-11 | blocker |
| AC-61 | Suggestions relate to what I am doing ("sugesti terkait apa yg sedang dilakukan") | long_focus request carries the current activity; returned `suggestion` parsed and shown distinctly in the bubble | AUTO Test-AI + MANUAL M-11 | major |
| AC-62 | All comment occasions work | greet, long_focus, app_switch, late_night, random_chatter, user_click each produce `[task:pet_action]` with the right occasion; consolidate produces `[task:memory_ops]` | AUTO Test-AI | major |
| AC-63 | Not chatty or costly (BYOK pays per call) | Automatic calls at most `maxCallsPerHour` per rolling hour (tested with 3) | AUTO Test-Autonomy | major |
| AC-64 | Silent during fullscreen (games, presentations) | respectFullscreen=true and a fullscreen app in front: 0 automatic AI calls; pet hidden or asleep in at least 80% of samples; visible again within 15 s after (positive control required) | AUTO Test-Fullscreen + MANUAL M-17 | major |
| AC-65 | AI text is shown as text | Speech or suggestion containing HTML or script is displayed literally, never rendered or executed (bubble must not use innerHTML with AI text) | AUTO Test-Static (sink scan) + MANUAL M-12 (mock scenario `xss`) | major |

## H. Habit memory - "dimasukan juga memory terkait kebiasaan usernya"

| ID | User expectation (source) | Pass condition | Verification | Severity |
|---|---|---|---|---|
| AC-70 | The pet remembers my habits | `memory_ops add` from the AI shows up in `/debug/memories` (kind habit); `petai.db` exists | AUTO Test-AI | blocker |
| AC-71 | Memory survives restarts | Memories are still in `/debug/memories` after an app restart | AUTO Test-Persistence | major |
| AC-72 | Memory is actually used | A stored habit appears in the next pet_action prompt | AUTO Test-AI | major |
| AC-73 | I can delete what it remembers | `memory_ops forget` removes it; settings UI can delete single items and "lupakan semua" | AUTO Test-AI + MANUAL M-16 | major |
| AC-74 | Daily consolidation | `consolidate` sends `[task:memory_ops]` and applies the returned ops | AUTO Test-AI | minor |

## I. AI-made animations, saved locally - "tiap gerakan/animasi disimpan dilokal agar jika ai menggunakan gerakan itu lagi tidak perlu generate ulang"

| ID | User expectation (source) | Pass condition | Verification | Severity |
|---|---|---|---|---|
| AC-80 | AI can invent a new movement and it is stored locally ("disimpan dilokal") | `new_animation_request qa_spin` gives exactly ONE `[task:animation_spec]` call; file `<data>/animations/<target>/qa_spin.json` passes contract validation; listed in `index.json` and `/debug/animations` (builtin:false); played | AUTO Test-AI | blocker |
| AC-81 | Reused without generating again ("tidak perlu generate ulang") | Requesting `qa_spin` again (trigger and chat, same session) and after an app restart gives 0 animation_spec calls | AUTO Test-AI, Test-Persistence | blocker |
| AC-82 | Broken AI animations are rejected | Each spec breaking one rule (unknown slot, unknown prop, non-increasing times, duration 50, length mismatch, scalar scale, 25 tracks, 65 keys, string values) plus a combined one: exactly 1 call, not saved, not listed, not played, app alive | AUTO Test-AI | blocker |
| AC-83 | Out-of-range values are clamped | A spec with rotation 100, position 30 and scale 5/0.01 is saved clamped (rotation ±6.2832, position ±2, scale 0.3-2) | AUTO Test-AI | minor |
| AC-84 | AI output is data, never code | No eval, new Function or string timers in the frontend; code strings, `__proto__` and `constructor` in specs rejected; a `../../` name never writes outside `animations/` | AUTO Test-Static, Test-AI | blocker |
| AC-85 | Built-in movements exist | The 13 contract built-ins exist as DSL JSON (target generic), pass the validator and are listed `builtin:true` | AUTO Test-Static, Test-AI | major |
| AC-86 | Library animations play without AI | `/debug/play wave` sets `pet.animation=wave` with no AI call | AUTO Test-AI | minor |
| AC-87 | Existing movements are never regenerated | `new_animation_request` naming a built-in (`wave`) gives 0 generation calls | AUTO Test-AI | major |
| AC-88 | I can manage the saved movements | Settings > Animasi: list, preview, delete, export and import work; a deleted animation is regenerated only when asked again | MANUAL M-21 | minor |

## J. Robustness and operations

| ID | User expectation (source) | Pass condition | Verification | Severity |
|---|---|---|---|---|
| AC-90 | Only one pet at a time | A second launch exits; still exactly one overlay; first instance healthy | AUTO Test-SingleInstance | major |
| AC-91 | Light on the PC while idling ("bisa iddle") | Idle (`stay`, AI off) CPU of petai and all WebView2 children at most 3% of the machine over 30 s; free mode at most 6% (warn); working set at most 600 MB (warn) | AUTO Test-Performance + MANUAL M-22 (GPU) | major |
| AC-92 | Does not install itself into autostart | `general.autostart=false` by default; running the app creates no HKCU Run or Startup entry | AUTO Test-Privacy-Defaults, Run-Acceptance (diff) | minor |
| AC-93 | Logs exist for support but hold no secrets | `<data>/logs/petai.log` exists, non-empty, no key material | AUTO Test-Persistence | minor |
| AC-94 | A broken config never bricks the pet | Start with a corrupt config.json falls back to defaults, the overlay appears, and config.json is valid afterwards | AUTO Test-Privacy-Defaults -CorruptConfig (launch D) | minor |
| AC-95 | Settings are stored and correct by default | config.json written in the data dir with the contract defaults (blocklist contains at least the contract list, retentionDays 14, excludeFromCapture, maxCallsPerHour 12, version 1, default model) and persisted changes | AUTO Test-Privacy-Defaults, Test-Persistence | major |
