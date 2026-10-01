# PetAI — Test Contract

Shared contract between the implementation and the independent QA harness in `qa/`.
The implementation MUST honor everything here; QA tests MUST only rely on what is here
(plus black-box observation of the running app).

## Process & window
- Executable: `build/bin/petai.exe` (`wails build`) or `wails dev`.
- One overlay window: class `wailsWindow`, title `PetAI Overlay`, owned by the petai process.
- Overlay covers the WORK AREA of the selected monitor (default primary; i.e. monitor minus taskbar,
  and 1px shorter than the monitor when the taskbar auto-hides), frameless, transparent. It is never
  exactly monitor-sized, otherwise Windows reports a fullscreen app (QUNS_BUSY) and hides the taskbar.
  `/debug/state.window.rect` reports this overlay rect.
- Launching must not steal keyboard focus: after startup the foreground window is the one that was
  in front before launch.
- Ex-styles: `WS_EX_TOPMOST`, `WS_EX_TOOLWINDOW` (no taskbar / alt-tab entry), `WS_EX_NOACTIVATE`
  (except while chat/settings input has focus), `WS_EX_LAYERED`.
  `WS_EX_TRANSPARENT` is set whenever the cursor is NOT over an interactive region (pet, bubble,
  menu, settings panel) → clicks pass through to windows underneath.
  If the implementation falls back to `SetWindowRgn`, `/debug/state` reports `"clickThrough":"region"`
  instead of `"exstyle"`.
- Single instance: launching a second copy exits and does not create a second overlay.
- Tray icon present while running (menu: Tampilkan/Sembunyikan, Chat, Settings, Pause pengamatan, Keluar).

## Data directory
Default `%APPDATA%\PetAI\`. Override with env `PETAI_DATA_DIR=<abs path>` (QA always uses an override).
```
<data>/config.json
<data>/petai.db                     (SQLite)
<data>/animations/index.json
<data>/animations/<target>/<slug>.json   target = generic|blob|cat|chick
<data>/logs/petai.log
```
API keys are NEVER written to `<data>` (stored in Windows Credential Manager, service `PetAI`,
user `anthropic` / `openai`). Env `PETAI_API_KEY_ANTHROPIC` / `PETAI_API_KEY_OPENAI` override
the keyring (testing only). Keys never appear in logs, `config.json`, `/debug/*` output, or the DB.

## config.json (camelCase keys)
```json
{
  "version": 1,
  "pet": { "character": "blob|cat|chick", "name": "Mochi", "color": "#8fd3ff",
           "personality": "string", "language": "id|en", "scale": 1.0 },
  "movement": { "mode": "stay|ground|free", "speed": 1.0, "activity": 0.5,
                "anchorX": -1, "anchorY": -1 },
  "ai": { "enabled": true, "provider": "anthropic|openai",
          "anthropic": { "model": "claude-opus-5-5", "baseURL": "" },
          "openai":    { "model": "", "baseURL": "" },
          "maxCallsPerHour": 12 },
  "privacy": { "watchActivity": false, "screenshots": false, "screenshotIntervalMin": 10,
               "blocklist": ["1password","bitwarden","keepass","inprivate","incognito","bank","bca","mandiri","bri","bni"],
               "retentionDays": 14, "excludeFromCapture": true },
  "general": { "autostart": false, "fps": 30, "monitor": 0, "respectFullscreen": true, "debug": false }
}
```
Defaults: `watchActivity=false`, `screenshots=false` (privacy opt-in). Unknown/missing keys → defaults.

Env overrides (testing): `PETAI_ANTHROPIC_BASE_URL`, `PETAI_OPENAI_BASE_URL` (override baseURL),
`PETAI_FAST=1` (all trigger thresholds/cooldowns divided by 60: minutes → seconds),
`PETAI_EXCLUDE_CAPTURE=0` (force-disable capture exclusion so screenshots can see the pet).

## Debug API
Only when env `PETAI_DEBUG_ADDR` is set (e.g. `127.0.0.1:47611`). Binds that address only. JSON.
- `GET  /debug/state` →
  ```json
  { "window": { "hwnd": 123, "exStyle": 0, "clickThrough": "exstyle|region", "interactive": false,
                "rect": {"x":0,"y":0,"w":1920,"h":1080}, "scale": 1.25 },
    "pet": { "x": 0, "y": 0, "w": 0, "h": 0, "character": "blob", "mode": "stay",
             "state": "idle", "animation": "idle", "visible": true },
    "ai": { "provider": "anthropic", "model": "claude-opus-5-5", "hasKey": true, "callsLastHour": 0 },
    "config": { ...config.json... } }
  ```
  `pet.x/y/w/h` = pet bounding box in physical screen pixels.
- `POST /debug/config` body = partial config JSON (deep-merged, saved, applied live) → full config.
- `POST /debug/trigger` `{"occasion":"greet|long_focus|app_switch|late_night|random_chatter|user_click|screenshot_insight|consolidate"}`
  → runs that occasion now, bypassing cooldown/rate limit → `{"ok":true,"action":{PetAction}|null,"error":""}`.
- `POST /debug/chat` `{"text":"..."}` → same path as the in-app chat → `{"ok":true,"action":{PetAction}}`.
- `GET  /debug/animations` → `[{"name","target","description","tags","builtin":bool}]`.
- `POST /debug/play` `{"name":"wave"}` → plays a library animation (no AI call).
- `GET  /debug/memories` → `[{"id","kind","content","source","confidence"}]`.
- `POST /debug/activity` `{"name":"football|basketball|golf|toilet"}` → starts that prop scene now.
  While it runs `/debug/state.pet.state` is `"activity:<name>"`; during the toilet scene `pet.visible` is false.
- `POST /debug/ui` `{"open":"settings|chat|menu"}` → opens that UI exactly like the tray / double-click /
  right-click would (chat & settings take keyboard focus; Esc closes them and returns focus).
- `/debug/state.pet.viewport` = `{w,h,scrollY,dpr}` (CSS px) for coordinate diagnostics.

## AI wire contract
Anthropic: `POST {base}/v1/messages` (header `x-api-key`), models via `GET {base}/v1/models`.
OpenAI: `POST {base}/v1/chat/completions` (header `Authorization: Bearer`), models via `GET {base}/v1/models`.
Structured output: Anthropic `output_config.format` = `{"type":"json_schema","schema":{...}}`;
OpenAI `response_format` = `{"type":"json_schema","json_schema":{"name":...,"strict":true,"schema":{...}}}`.
The model's reply (Anthropic text block / OpenAI `message.content`) is a JSON string.

The LAST user message's text starts with one task tag line:
- `[task:pet_action]` → reply = PetAction
- `[task:animation_spec]` → reply = AnimationSpec
- `[task:memory_ops]` → reply = `{"memory_ops":[...]}`

After the tag line comes a JSON context object. For `pet_action` it contains at least
`occasion`, `localTime`, and — only when `privacy.watchActivity=true` and the foreground app is
not blocklisted — `activity: {"app":"code.exe","title":"<redacted title>"}`.
Blocklisted app/title → no `activity` key at all. Titles are redacted (emails → `[email]`,
digit runs ≥6 → `[num]`). A screenshot image block (Anthropic `image` / OpenAI `image_url` data URL,
JPEG) is attached ONLY for `screenshot_insight` with `privacy.screenshots=true` AND
`privacy.watchActivity=true` (screenshots are a sub-toggle of the master watch permission) and a
non-blocklisted foreground app.

### PetAction
```json
{ "speech": "string ('' = silent)",
  "mood": "neutral|happy|excited|curious|sleepy|concerned|sad|love",
  "animation": "library animation name or ''",
  "new_animation_request": { "name": "snake_case", "description": "string" } | null,
  "memory_ops": [ { "op": "add|update|forget", "id": 0, "kind": "habit|preference|fact|goal", "content": "string" } ],
  "suggestion": "string ('' = none)",
  "activity": "''|football|basketball|golf|toilet" }
```
`activity` starts a scripted prop scene in the frontend (ball, hoop, golf flag, outhouse). Explicit
user requests ("joget", "salto", "main bola"…) must be fulfilled: a matching catalog animation, else a
`new_animation_request`, or the activity.
App behavior: if `animation` exists in library → play it, no extra AI call. If
`new_animation_request.name` already exists (same target or generic) → play cached, NO
`animation_spec` call. Otherwise exactly one `[task:animation_spec]` call → validate → save to
`<data>/animations/...` → play. Invalid spec → rejected, not saved, pet falls back to a built-in.

### AnimationSpec (DSL)
```json
{ "name": "happy_spin", "description": "string", "tags": ["happy"], "target": "generic|blob|cat|chick",
  "duration": 1.2, "loop": false,
  "tracks": [ { "slot": "root|body|head|eyeL|eyeR|mouth|earL|earR|armL|armR|legL|legR|tail|accessory",
                "prop": "position.x|position.y|position.z|rotation.x|rotation.y|rotation.z|scale|scale.x|scale.y|scale.z",
                "times": [0, 0.6, 1.2], "values": [0, 3.14, 6.28], "interp": "linear|smooth|step" } ],
  "expressions": [ { "t": 0, "eyes": "neutral|happy|sleepy|surprised|angry|love|closed|pain", "mouth": "neutral|smile|open|frown|o|wavy" } ],
  "effects": [ { "t": 0.5, "type": "hearts|sparkles|sweat|zzz|question|exclaim|notes" } ] }
```
Validation: duration 0 < d ≤ 10; times strictly increasing within [0,duration], same length as
values; `scale` values are `[x,y,z]` arrays, others numbers; ≤ 24 tracks, ≤ 64 keys/track;
clamps: rotation ±6.2832, position ±2, scale 0.3–2. Unknown slot/prop → reject spec.
Never executed as code.

Built-in animations (always present, `builtin:true`, target `generic`):
`idle, walk, float, sleep, jump, wave, happy_bounce, surprised, dangle, fall, land, sit, look_around,
kick, dribble, throw, golf_swing, mulas, relieved`.

## Clarifications (answers to QA Phase 1 gaps)
- Base URLs: both forms work. Anthropic: a trailing `/v1` is stripped (SDK adds it). OpenAI-compatible:
  if the base does not end in `/vN` the app appends `/v1` (`http://h:p/openai` → `/openai/v1/chat/completions`;
  `https://openrouter.ai/api/v1` used as-is).
- `ai.maxCallsPerHour = 0` → no automatic comments at all (chat and debug triggers still work); valid range 0–120.
- Only automatic occasions count toward the budget. Chat and `/debug/trigger` / `/debug/chat` are not counted.
  SDK-internal retries count as one call. Occasions that fail before reaching a provider (no key) are not counted.
- `PETAI_FAST=1` divides every duration by 60, including the rolling budget window (1 h → 60 s) and min gap (3 min → 3 s).
- `/debug/trigger` bypasses: rate limit, min gap, cooldowns, screenshot interval and DND. It does NOT bypass
  privacy toggles or the blocklist.
- Blocklist: case-insensitive; matched against the bare exe name (substring) and the window title
  (substring; entries of ≤3 chars match title only as a whole word). `activity.app` is the bare lower-case
  exe name (never a path).
- Redaction also replaces API-key-like tokens (`[secret]`), long hex ids (`[id]`), URL query strings
  (stripped) and `X:\Users\<name>` (`[user]`). Titles are truncated to 160 chars.
- New-animation dedup: the saved spec name is always the requested (slugged) name; concurrent requests
  for the same name generate once.
- Default `pet.language` is `id`. The frontend never receives raw keys (only masked hints).
- WebView2 profile lives in `<data>/webview`. "Hapus semua data" wipes activity, memories and chat
  (not config, keys or animations).

## Behavior expectations (black-box)
- Pet is visible, cute toon/doodle style, rendered with three.js; ≥3 characters selectable (blob, cat, chick).
- Movement modes: `stay` (pet stays at anchor, only in-place animation), `ground` (walks along the
  top of the taskbar / work-area bottom, gravity, can perch on top edge of foreground window),
  `free` (floats anywhere on the monitor, no gravity). Pet never leaves the monitor bounds.
- Pet moves on its own (idle wandering) without user interaction in `ground`/`free`.
- Click → local reaction; double-click → chat input; drag → pet follows cursor, drop → falls (ground) or stays (free/stay, and stay updates anchor);
  right-click → context menu.
- Clicking empty overlay area reaches the window underneath.
- With `respectFullscreen=true` and a fullscreen app/presentation in foreground → no AI comments, pet hidden or asleep.
- AI rate limit: ≤ `maxCallsPerHour` automatic (non-chat, non-debug) calls per rolling hour.
- Memory: `memory_ops` from AI persist in `petai.db` and appear in `/debug/memories`; settings UI can delete them.
- Activity log rows older than `retentionDays` are purged on startup.
