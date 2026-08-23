# ogamed (fork Outgrow1827) — Changelog

Log of changes in our fork. History below `v13-fix.3` is inherited from `ronnie32/ogame.mod`'s `fix-ogamed-v13-discovery` + `fix/artifact-inventory-endpoint` branches (the base this fork now builds on); the `[Unreleased]` section at the top lists this fork's own changes, generated from the real commit history and diff content (`git log` / `git diff`).

## [Unreleased] — Outgrow1827 fork

### Added

- **Gameforge proof-of-work login challenge solver** (`pkg/gameforge/gameforge.go`) — Gameforge added a `pow-captcha.gameforge.com` sha256 challenge type alongside the existing visual-drop challenge. `solvePowChallenge` fetches the challenge (`GET /api/challenge/{id}`), `solveSha256Pow` brute-forces a nonce per sub-challenge whose hash matches the target prefix, then `POST`s the solutions + timing/instrumentation metrics back. The login flow now retries once, transparently, after a `CHALLENGE_REQUIRED` 409 response is resolved this way (separate from the pre-existing visual-drop-challenge handling, which still returns immediately without retrying). Note: `ronnie32`'s base branch independently added its own in-process pow-captcha solver (`pkg/gameforge/powcaptcha.go`) — both exist after this merge; not yet reconciled into one.
- **Manual mode** (`cmd/ogamed/main.go`, `cmd/ogamed/handlers.go`) — new `--manual-mode-timeout` flag (default 30s idle timeout) and `POST /bot/toggle-manual-mode` endpoint. While active, the proxied game page (`GET`/`POST /game/index.php`) is served through a separate prioritized transaction (`tx`) instead of the bot's normal automated queue, so TBot's own workers naturally block/queue behind a live human session on the same underlying `bot` object until it's released or times out. The injected in-page button (top-right corner, red/green) reflects live state and lets a human toggle it directly from the browser. Verified working live on 2026-08-09 (toggle on/off, correct reflection in the proxied page, bot resumes normally afterward).
- **`--hide-account-info-in-logs` flag** — redacts player name/universe from `ogamed`'s own debug/info log lines.
- **Human-in-the-loop Telegram captcha solver fallback** (`--telegram-solver-bot-token`/`--telegram-solver-chat-id`) — when both the `solver.obot.de` API key (`TbotSolver`, `cmd/ogamed/tbot-solver.go`) and Telegram solver credentials are configured, the auto-solver is tried first and falls back to the Telegram human solver on failure; either can also be used alone.
- **Device fingerprint fully configurable via CLI flags/env vars** (`cmd/ogamed/main.go`) — `device-system`, `device-browser`, `device-memory`, `device-concurrency`, `device-color`, `device-width`, `device-height`, `device-timezone`, `device-lang`, `device-user-agent` were previously hardcoded in `main.go`; now read from flags (`OGAMED_DEVICE*` env vars) and passed through to the device builder.
- **Stale fingerprint auto-rebuild** (`pkg/device/device.go`, `matchesConfiguredParams`) — a persisted device fingerprint that no longer matches the currently-configured device params (e.g. `DeviceConf` changed in `instance_settings.json`) is now detected and discarded, forcing a fresh fingerprint instead of silently keeping stale values forever.
- **Device fingerprint blackbox format bumped to v12** — new `ParseBlackboxV12`, one extra `LastValue` field appended to the encrypted payload array (`ConstantVersion` 11 → 12).
- **`cmd/importcookies`** (new standalone CLI tool) — imports a Netscape-format `cookies.txt` file (e.g. exported via the Cookie-Editor browser extension) into `ogamed`'s persistent cookie jar for a given device, letting `ogamed` reuse an already-established browser session instead of performing its own login handshake.
- **Lifeform bonuses extraction** (`cmd/ogamed/LfBonuses.go`, `cmd/ogamed/extracts.go`, new) — parses the lifeform bonus panel DOM (ship stat bonuses: structural integrity/shield/weapon/speed/cargo/fuel; cost & time reduction bonuses) into a typed `LfBonuses` struct, exposed via `GET /bot/lfbonuses` and `GET /bot/planets/:planetID/lifeform-bonuses`.
- **New TBot-specific routes**: `GET /bot/alliance-class`, `GET /bot/planets/:planetID/artifacts`, `GET /bot/expedition-messages`, `POST /bot/planets/:planetID/get-system-available-discovery`, `GET /bot/planets/:planetID/get-available-discoveries`. Consolidated into `cmd/ogamed` rather than a separate `cmd/ogamed-tbot` binary (`ronnie32`'s approach) — see 2026-08-20 architecture decision below.
- **`make build-ogamed`** target added to the `Makefile` (outputs `bin/ogamed.exe`, embeds version from `git describe`).

### Fixed

- **`BuyOfferOfTheDay` for the v13.0.0 JSON-wrapped trader panel** (`pkg/extractor/v12_0_0/extracts.go`, `extractOfferOfTheDayFromJSON`) — the importexport panel now returns a JSON envelope (`content.trader` holds the HTML fragment) instead of raw HTML, and the trade token comes from the envelope's `newAjaxToken` field instead of an embedded `var token` script variable that no longer exists in the new schema.
- **`PlasmaTerraformer` copy-paste bug** (`pkg/extractor/v9/extracts.go`) — was reading HTML element id `lifeformTech11112` instead of `lifeformTech11212`.
- **Login request payload schema** (`pkg/gameforge/gameforge.go`, `postSessionsReq`) — field renamed `Identity`→`Email`, locale format `en_GB`→`en-GB`, `AutoGameAccountCreation` removed — matching what Gameforge's login endpoint now actually expects.
- Removed a committed `.env` file (should never have been tracked in the first place — real secrets/credentials risk).

### Documentation

- **Guide for building `ogamed` from source** in the `README.md` — explains why this fork must be built from source rather than downloading upstream's release binary (this fork's `cmd/ogamed` has extra TBot-specific routes upstream lacks: alliance class, lifeform bonuses, artifacts, manual mode, log privacy, etc.), then a step-by-step walkthrough aimed at readers unfamiliar with Go: installing Go (including a `winget` option on Windows), getting the source, the exact build command with each flag explained, verifying the result, the `make build-ogamed` target, cross-compiling for Windows from Linux/macOS, and the deploy note about copying `ogamed.exe` alongside `TBot.exe`'s publish output manually (TBot's own build no longer bundles it automatically).
- Documented the new HTTP routes in the README's route list (`espionage-report` GETs, `combat-report` GETs, `expedition-messages`, `attacks`, `galaxy-infos`, `get-research`).

### Chore

- `.gitignore`: reviewed for dead entries — dropped `*.har`/`console-export-*.log`/`erros.txt` (unused, no matching tooling references anything by those names), kept `/bin` and the `ogamed.exe` build-artifact entries.
- Removed references to folders that no longer exist in this fork.

### Architecture (2026-08-20)

- **Consolidated on a single `cmd/ogamed` binary instead of replicating `ronnie32`'s `cmd/ogamed-tbot` split.** `ronnie32`'s base branch forked a second, near-duplicate `main.go` (`cmd/ogamed-tbot/`) to isolate TBot-specific routes from a "vanilla" `cmd/ogamed`. Go doesn't allow one `main` package to import another, so that split is the only way to keep two binaries without a bigger refactor into a shared library package — but nothing in this fork's build/release pipeline actually consumes the vanilla `cmd/ogamed` separately, so the split has no real benefit here and would only add a second `main.go` to keep in sync. This fork's own pre-existing approach (extend `cmd/ogamed` in place) was kept, and `ronnie32`'s unique fixes (AutoDiscovery availability endpoint, artifact inventory endpoint, pow-captcha solver) were folded into it instead.

## v13-fix.3 (ronnie32 base)

### OGame v13 compatibility

#### Lifeform bonuses
- Refactored v13 lifeform bonus parsing into a dedicated `v13_0_0` extractor (`lfBonuses.go`, `trader.go`, `movement.go`).
- The extractor is now selected automatically from the server version (`serverData.xml` -> `getExtractorFor`): `>= 13.0.0` -> `v13_0_0`, else `v12_0_0`, and so on down the extractor chain.
- The v13 page reworked bonus categories (`data-toggable="ships"` instead of `categoryShips`) - ships/researches are now keyed directly by their ID.

#### Fleet recall (fleetsave / sleep mode)
- v13 changed the recall mechanism: the recall button now uses `data-recall-action` (`action=recallFleet&fleetId=...&asJson=1`) plus a CSRF token from the page script.
- `cancelFleet` now issues the correct v13 request (legacy v12 path preserved).
- `CancelFleetHandler` now returns a real error (500) when the recall fails, instead of masking it as a success.

#### Buy Offer of the Day
- v13 moved Import/Export to `component=trader&action=importexport` (JSON envelope `content.trader`).
- Uses the v13 actions `importExportTrade` / `importExportTakeItem` (instead of `trade` / `takeItem`).
- Trade token taken from the response envelope (`newAjaxToken`).

#### Hostile fleet / attack detection (Defender)
- v13 loads the event list via `component=eventlist&action=catchEvents` (JSON envelope `content.eventlist`).
- `getAttacks` now parses the v13 event list - the Defender detects hostile fleets again.

#### Fleet return time (Expeditions timer)
- v13 renders the return countdown as `getElementById("timerNext_..."), N` instead of `getElementByIdWithCache("timerNext_...", N)`.
- `BackIn` is now extracted correctly - the Expedition worker waits for the fleet return instead of re-checking slots every ~2 minutes.
