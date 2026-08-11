# ogamed (fork Outgrow1827) — Changelog

Log of changes in our fork. Generated from the real commit history and diff content (`git log` / `git diff`).

## [Unreleased]

### Added

- **Gameforge proof-of-work login challenge solver** (`pkg/gameforge/gameforge.go`) — Gameforge added a `pow-captcha.gameforge.com` sha256 challenge type alongside the existing visual-drop challenge. `solvePowChallenge` fetches the challenge (`GET /api/challenge/{id}`), `solveSha256Pow` brute-forces a nonce per sub-challenge whose hash matches the target prefix, then `POST`s the solutions + timing/instrumentation metrics back. The login flow now retries once, transparently, after a `CHALLENGE_REQUIRED` 409 response is resolved this way (separate from the pre-existing visual-drop-challenge handling, which still returns immediately without retrying).
- **Manual mode** (`cmd/ogamed/main.go`, `cmd/ogamed/handlers.go`) — new `--manual-mode-timeout` flag (default 30s idle timeout) and `POST /bot/toggle-manual-mode` endpoint. While active, the proxied game page (`GET`/`POST /game/index.php`) is served through a separate prioritized transaction (`tx`) instead of the bot's normal automated queue, so TBot's own workers naturally block/queue behind a live human session on the same underlying `bot` object until it's released or times out. The injected in-page button (top-right corner, red/green) reflects live state and lets a human toggle it directly from the browser. Verified working live on 2026-08-09 (toggle on/off, correct reflection in the proxied page, bot resumes normally afterward).
- **`--hide-account-info-in-logs` flag** — redacts player name/universe from `ogamed`'s own debug/info log lines.
- **Human-in-the-loop Telegram captcha solver fallback** (`--telegram-solver-bot-token`/`--telegram-solver-chat-id`) — when both the `solver.obot.de` API key (`TbotSolver`, `cmd/ogamed/tbot-solver.go`) and Telegram solver credentials are configured, the auto-solver is tried first and falls back to the Telegram human solver on failure; either can also be used alone.
- **Device fingerprint fully configurable via CLI flags/env vars** (`cmd/ogamed/main.go`) — `device-system`, `device-browser`, `device-memory`, `device-concurrency`, `device-color`, `device-width`, `device-height`, `device-timezone`, `device-lang`, `device-user-agent` were previously hardcoded in `main.go`; now read from flags (`OGAMED_DEVICE*` env vars) and passed through to the device builder.
- **Stale fingerprint auto-rebuild** (`pkg/device/device.go`, `matchesConfiguredParams`) — a persisted device fingerprint that no longer matches the currently-configured device params (e.g. `DeviceConf` changed in `instance_settings.json`) is now detected and discarded, forcing a fresh fingerprint instead of silently keeping stale values forever.
- **Device fingerprint blackbox format bumped to v12** — new `ParseBlackboxV12`, one extra `LastValue` field appended to the encrypted payload array (`ConstantVersion` 11 → 12).
- **`cmd/importcookies`** (new standalone CLI tool) — imports a Netscape-format `cookies.txt` file (e.g. exported via the Cookie-Editor browser extension) into `ogamed`'s persistent cookie jar for a given device, letting `ogamed` reuse an already-established browser session instead of performing its own login handshake.
- **Lifeform bonuses extraction** (`cmd/ogamed/LfBonuses.go`, `cmd/ogamed/extracts.go`, new) — parses the lifeform bonus panel DOM (ship stat bonuses: structural integrity/shield/weapon/speed/cargo/fuel; cost & time reduction bonuses) into a typed `LfBonuses` struct, exposed via `GET /bot/lfbonuses` and `GET /bot/planets/:planetID/lifeform-bonuses`.
- **New TBot-specific routes**: `GET /bot/alliance-class`, `GET /bot/planets/:planetID/artifacts`, `GET /bot/expedition-messages`, `POST /bot/planets/:planetID/get-system-available-discovery`, `GET /bot/planets/:planetID/get-available-discoveries`.
- **`make build-ogamed`** target added to the `Makefile` (outputs `bin/ogamed.exe`, embeds version from `git describe`).

### Fixed

- **`BuyOfferOfTheDay` for the v13.0.0 JSON-wrapped trader panel** (`pkg/extractor/v12_0_0/extracts.go`, `extractOfferOfTheDayFromJSON`) — the importexport panel now returns a JSON envelope (`content.trader` holds the HTML fragment) instead of raw HTML, and the trade token comes from the envelope's `newAjaxToken` field instead of an embedded `var token` script variable that no longer exists in the new schema.
- **`PlasmaTerraformer` copy-paste bug** (`pkg/extractor/v9/extracts.go`) — was reading HTML element id `lifeformTech11112` instead of `lifeformTech11212`.
- **Login request payload schema** (`pkg/gameforge/gameforge.go`, `postSessionsReq`) — field renamed `Identity`→`Email`, locale format `en_GB`→`en-GB`, `AutoGameAccountCreation` removed — matching what Gameforge's login endpoint now actually expects.
- Removed a committed `.env` file (should never have been tracked in the first place — real secrets/credentials risk).

### Documentation

- **Guide for building `ogamed` from source** in the `README.md` — explains why this fork must be built from source rather than downloading upstream's release binary (this fork's `cmd/ogamed` has extra TBot-specific routes upstream lacks: alliance class, lifeform bonuses, artifacts, manual mode, log privacy, etc.), then a step-by-step walkthrough aimed at readers unfamiliar with Go: installing Go (including a `winget` option on Windows), getting the source, the exact build command with each flag explained, verifying the result, the `make build-ogamed` target, cross-compiling for Windows from Linux/macOS, and the `TBot/ogamed.exe` deploy gotcha (TBot's own build re-copies its bundled copy into every publish output, so that copy needs updating too when developing both repos side by side).
- Documented the new HTTP routes in the README's route list (`espionage-report` GETs, `combat-report` GETs, `expedition-messages`, `attacks`, `galaxy-infos`, `get-research`).

### Chore

- `.gitignore`: added `*.har`, `console-export-*.log`, `erros.txt`, `/bin` safety-net entries (matches the same cleanup done on the TBot side); local/private files consolidated outside the repo.
- Removed references to folders that no longer exist in this fork.
