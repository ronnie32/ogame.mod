# Discovery dispatch and reservation boundaries

This maintenance patch starts at the MIT-licensed Ronnie32/ogame.mod release
`e9f365f803e15f937916ef152462f231b8676512` and ports the previously reviewed
single-discovery safeguards from `32f18c61d800f2509645d5e4481378f0606cf25b`.
The original donor work includes `b7f9e90`, `33d070c` and `a0c8113` (redirect
classification, a fleet watermark, local galaxy token, and one-attempt dispatch).
Ronnie's auction, CAPTCHA, message APIs and native system-discovery API remain.
The original MIT notice, Copyright (c) 2018 Alain Gilbert, remains in LICENSE.

The dispatch requires a target position with SearchForLifeforms mission 18 and
literal boolean canSend=true, a nonempty page-scoped token, and a readable
pre-dispatch fleet snapshot with positive fleet IDs. It preserves the sending
origin option and never automatically replays its state-changing POST.

Discovery reads use a separate strict movement snapshot path; the general
fleet extractor and other callers keep their existing compatibility behavior.
Each snapshot needs an original HTTP 2xx response with no followed redirect,
a movement page identified by the existing extractor (including the source
shape body#ingamepage with currentPage=movement), exactly one nonnegative
numeric occupied-slot count, the same number of fleet-detail rows, and unique
explicit positive fleet IDs
that agree with the existing extractor. Mobile UI may have multiple detail
links per fleet row; every link must supply the same positive ID. Missing,
invalid or conflicting link IDs and duplicate IDs across rows are rejected.
Missing or inconsistent rows prevent the POST when detected before dispatch,
and leave its outcome unconfirmed when
detected after dispatch. The occupied-slot/detail count agrees in all nine
checked-in movement samples (v7 through v11.13.0, including mobile); no modern
live-server completeness guarantee is inferred from those historical samples.

An HTTP 200 response needs an explicit boolean success=true at the top level or
inside response. Explicit errors, non-success statuses, false/null/invalid
success values and contradictory outer/nested responses are rejected before
success is consumed. Busy code 100 remains an error. Legacy galaxy responses
also reject explicit failure envelopes before their mission data is consumed.

Only HTTP 302 redirects to the same scheme/host, /game/index.php, and the known
overview/fleetdispatch forms of page=ingame or page=componentOnly (or no page
and no component) may be reconciled. Redirect bodies do not prove dispatch.
External, login, unknown-page and other HTTP statuses return errors without
following the redirect or replaying the POST.

Reconciliation reads movement at most three times, with cancellable 300 ms gaps.
It requires exactly one positive fleet ID absent from the entire earlier ID
snapshot, explicit data-return-flight=false and mission 18 attributes, exact
origin/destination coordinate anchors, and explicit matching planet/moon figure
classes. An old ID, returning fleet, changed route, unrelated mission,
missing new fleet or multiple matching new fleets cannot prove this dispatch.
SendDiscoveryFleet2 uses the same watermark rather than returning an old fleet.

Historical movement samples contain empty data-return-flight attributes. Their
IDs may participate in a complete watermark, but empty, missing, numeric or
unknown direction values cannot prove a new outgoing fleet. Missing or unknown
celestial figure types also cannot prove a route. If a current server uses only
those ambiguous forms, redirect reconciliation is deliberately unavailable and
returns an unconfirmed outcome; the POST is never replayed. An explicit valid
JSON success remains usable by SendDiscoveryFleet after a complete watermark.
Historical no-fleet movement redirects to fleet1 are not accepted as complete
watermarks: discovery stops before its POST when that view cannot be verified.

These are synthetic localhost transport tests using the existing real
extractors. No new account captures or live game/session requests were used. The
protocol shapes are source-based assumptions, not live-server certification.
A lost response stays unconfirmed. Visibility delayed beyond the bounded reads
also stays unconfirmed. A concurrent manual discovery with exactly the same
route cannot be distinguished by an API without a server request identifier;
this patch does not claim general fleet dispatch idempotency or account-wide
serialization. The C# bridge must independently avoid POST retries.

## Bounded system-discovery API

`POST /bot/planets/:planetID/send-system-discovery-bounded` accepts form fields
`galaxy`, `system`, `maxFleets`, and `positions`. Every field must appear once.
The origin, galaxy and system must be positive. `maxFleets` must be 1 through
15. `positions` is a comma-separated list of unique planet positions 1 through
15, with a nonzero length no greater than `maxFleets`.

For example, `galaxy=1&system=2&maxFleets=2&positions=3,7` authorizes at most two
single-target discovery POSTs, in that order, only to planets [1:2:3] and [1:2:7].
Missing, malformed, duplicate, out-of-range or excessive reservations return
HTTP400 before any game request. Existing public Go signatures and interfaces
remain source compatible; the concrete OGame/Prioritize types add
`SendSystemDiscoveryFleetBounded(celestialID, galaxy, system, maxFleets,
positions, options...) (SystemDiscoveryResult, error)`.

The existing API response envelope wraps this result under `Result`:

```json
{"Status":"ok","Code":200,"Message":"","Result":{"coordinates":[{"Galaxy":1,"System":2,"Position":3,"Type":1}],"complete":false,"error":"discovery at [1:2:7] is incomplete: ..."}}
```

HTTP200 describes a valid request, including a partial result. `complete=true`
means every supplied position has a unique proven new outgoing fleet. The
`coordinates` array includes only proven launches, in reservation order; it is
empty, rather than null, when nothing is proven. `error` is omitted on complete
success. The Go error return covers invalid reservations; runtime failures are
reported in the result so earlier successful launches survive.

Each target refreshes its eligibility and local token, takes a complete strict
watermark, makes one launch POST, and requires full movement proof. An expanded
eligible set cannot add targets. Ineligibility, explicit rejection or uncertain
outcome stops the sequence, returning its earlier proof. Never replay the whole
request or the failed target. An uncertain launch may have consumed its reserved
slot even when absent from `coordinates`; the caller must reconcile its current
fleet/slot state before using that capacity again.

OGame's native system action has no known supported server-side count/target
limit. A preflight eligible count cannot close the eligibility-change race. The
bounded endpoint therefore performs individual one-attempt launches; this costs
more requests and does not claim native atomic bulk dispatch. It is serialized
as one ordinary Go task, and stops at the first failure.

The legacy `/send-system-discovery` route and `SendSystemDiscoveryFleet` method
remain for released callers. They keep the native bulk action and coordinate
result, with stricter errors, HTTP status checks and movement proof. They have
**no reservation guarantee** and must not be used by a caller enforcing a slot
budget. Their native redirect proof now uses all prior IDs and explicit movement
evidence; positive JSON coordinates must stay in the requested system and be
proven with the same watermark. This does not make the native action bounded.

The final ordinary fleet launch POST also makes one attempt after an HTTP error
or lost response. Its read-only `checkTarget` preflight remains retryable. The
caller still owns ambiguous-launch reconciliation; neither route offers a
server-side idempotency key or account-wide protection from concurrent manual
launches on the same route.
