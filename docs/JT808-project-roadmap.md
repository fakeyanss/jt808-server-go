# JT808 Project Progress and TODO Roadmap

Last reconciled: 2026-11-01  
Code baseline: branch `main`, commit `6a79797` (`feat: JT808 P0/P1 protocol stack`).
The earlier baseline commit `ca724ae` still exists on `origin/p0-region-route-typed`;
the branch name `codex/jt808-p0` recorded before did not exist and has been corrected.
Protocol message-by-message matrix: [JT808-2019-coverage.md](JT808-2019-coverage.md).  
Industry HTTP API notes: [industry-api.md](industry-api.md).

This file tracks project-level work, implementation depth, dependencies, and
acceptance criteria. The coverage matrix remains the detailed Appendix B
message index. “Done” means present in the code and verified at the indicated
level; a codec or acknowledgment by itself does not mean the device-side
business flow is complete.

## Project status

The project has a working JT808 server and a TCP/UDP client simulator. Core
registration, authentication, heartbeat, location, acknowledgments, parameter
flows, message fragmentation/retransmission, alarms, and several industry
messages have executable paths. The simulator is used in localhost integration
tests, including media upload, media catalog search, and 0x8800 replay.

The implementation is not yet a complete commercial JT808 server. The largest
active gaps are durable persistence (all caches are bounded and in-memory only),
message-body encryption (`0x8A00/0x0A00` RSA), JT/T 1078 RTP media streaming,
and header-level 2011 version detection (2011 is only inferred from the `0x0100`
body length, not from the message header). The coverage matrix enumerates
implemented, partial, and missing message IDs; use the prioritized tasks below
to plan work.

## Completed work

| Area | Status | What is in place |
|---|---|---|
| JT808 framing and sessions | Done | Message headers, BCD/GBK fields, escaping/checksum, TCP/UDP sessions, acknowledgments, keepalive and retransmission handling. |
| Client simulator and protocol tests | Done | Registration/authentication, reports, downlink handling, synthetic industry messages, malformed-frame and fragmentation scenarios; localhost client/server tests are part of `go test ./...`. |
| Location and alarms | Done for current scope | Location reports/cache with TLV extended items, location query response flow, alarm history in bounded memory, and manual alarm confirmation flow. Durable history remains open. |
| Parameters and terminal properties | Done for current scope | Set/query/specified-query/property flows with serial-correlated response handling. |
| Driver identity and CAN | Done for current scope | Typed `0x0702`, `0x0705`, driver query command, in-memory latest-report access and simulator reports. |
| Recorder message envelope | Done | `0x8700/0x8701/0x0700` typed GB/T 19056-2021 data block codecs for `0x31/0x33/0x41/0x56/0x57/0x58` commands and their responses; simulator returns command-specific typed blocks. |
| Region/route message envelope | Done | Typed circle/rectangle/polygon/route structures with attribute-controlled fields; set/delete/query paths backed by a per-device simulator catalog; structured JSON HTTP input and decoded query responses. |
| Multimedia base flow | Done | Event/upload/capture/search messages and bounded upload cache; `0x0802` typed 35-byte records searched against cached uploads; `0x9205/0x1205` resource lists backed by the same catalog with channel/type/time filters. |
| Media command codecs | Done | `0x8803/04/05` have typed layouts, validation and HTTP downlink. The simulator executes them: `0x8803` uploads catalog entries matching type/channel/event/time, `0x8804` toggles a recording flag and emits sample audio frames, `0x8805` replays the requested media item. |
| Media retransmission | Done | Server detects missing `0x0801` fragments and requests retransmission with `0x8800` (media ID + missing packet IDs); first-fragment loss falls back to `0x8003`. Simulator replays cached frames; server reassembles exactly once. `0x8003` remains for non-media transport fragmentation. |
| Protocol status documentation | Done as index | `JT808-2019-coverage.md` maps Appendix B message IDs to implementation states and notes. Amendment 1 conformance has not been reviewed. |

## Prioritized TODO

### P0 — Complete the protocol flows already exposed by the API

#### P0.1 — Typed region and route records; simulator catalog and queries

**Status:** Done. Typed circle/rectangle/polygon/route models live in
`internal/protocol/model/region_route_types.go` and `msg_8600.go`; `0x0608`
responses are typed in `msg_0608.go`. Set/delete/query paths are backed by the
per-device simulator catalog in `internal/storage/region_catalog.go`, with
structured JSON HTTP input (`POST /device/:phone/regions/set|delete|query`).

**Work:**

- Add typed circle, rectangle, polygon, route, and route-point structures,
  including attribute-controlled time/speed fields, 2019 night-speed fields,
  names, polygon vertices, and route segment constraints.
- Parse and encode set messages and variable-length `0x0608` records with
  strict count, length, attribute, ID, coordinate and GBK-name validation.
- Back simulator behavior with per-device maps. Apply replace/append/update
  semantics, deletion-by-ID/delete-all semantics, and query-by-type/ID.
- Replace or supplement the HTTP `bodyHex` API with structured JSON input and
  return decoded query records. Preserve a raw escape hatch only if needed for
  forward compatibility.
- Keep set/delete and query test cases at codec level and exercise set → query,
  update, append, delete-one, delete-all, and malformed record cases through the
  client/server simulator.

**Acceptance:** Set a circle, rectangle, polygon, and route through an actual
server downlink; query each back from the simulator and compare typed fields;
all malformed lengths/counts are rejected; full suite passes.

**Dependencies:** None. Confirm each conditional field against the repository's
JT/T 808-2019 PDF and note any 2011/2013 compatibility behavior explicitly.

#### P0.2 — GB/T 19056 record command and data block decoding

**Status:** Done. GB/T 19056-2021 data blocks for `0x31`/`0x33`/`0x41`/`0x56`/`0x57`/`0x58`
commands and their responses are typed-encoded/decoded. The simulator returns
command-specific typed response blocks. Unsupported commands fall back to raw
data passthrough.

**Work:**

- Identify the required GB/T 19056 edition and obtain/record the applicable
  normative data command table, record block layouts, checksums, and field
  encodings before implementing decoders.
- Model supported command codes and their typed request/response data blocks;
  validate fixed/variable lengths, BCD dates, checksums and reserved values.
- Preserve raw blocks for unsupported commands and vendor extensions, while
  reporting the command and parse status separately.
- Make simulator responses vary by requested supported command, rather than
  returning the same sample block for every command.
- Test known vectors from the standard and end-to-end `0x8700 → 0x0700`, plus
  `0x8701 → 0x0001` and the expected device result.

**Acceptance:** Every implemented command has a standard-backed typed codec,
at least one known vector, negative validation cases, and simulator behavior
that matches the command. Do not infer field layouts from the JT808 wrapper.

**Dependency:** Confirm and add the applicable GB/T 19056 document/version.

#### P0.3 — Implement stored media upload commands in the simulator

**Status:** Done. `processMsg8803` filters the simulator media catalog by
type/channel/event/time range and uploads matching `0x0801` records;
`processMsg8804` toggles the recording flag and emits sample audio frames on
start; `processMsg8805` replays the catalog item matching the requested media
ID. All paths are covered by client/server integration tests.

**Work:**

- Give the simulator a bounded per-device media catalog populated by generated
  and explicitly seeded samples.
- For `0x8803`, select by type/channel/event/time and apply the protocol delete
  behavior; return the generic acknowledgment and upload matching `0x0801`
  records.
- For `0x8805`, locate the media ID, honor the delete flag and upload that one
  item or report the standard failure case.
- For `0x8804`, simulate start/stop recording and saved versus real-time upload
  semantics with deterministic test media. Model only behavior represented by
  the command fields; do not claim real microphone capture.
- Add full TCP client/server tests that verify acknowledgments, actual uploads,
  catalog searches afterward, deletion behavior, and missing-item results.

**Acceptance:** `0x8803` and `0x8805` cause observable matching media uploads;
`0x8804` changes simulator recording state and reports outcomes; `0x0802`
search reflects the resulting catalog.

#### P0.4 — Server-side missing media packet detection and `0x8800` requests

**Status:** Done. The server tracks in-progress `0x0801` fragments by device and
original serial, extracts the `MultiMediaID` from the first fragment, and on idle
timeout sends `0x8800` listing the missing packet IDs. The simulator replays the
cached frames; the server reassembles the media exactly once and releases state.
If the first fragment (which carries the media ID) is missing, the server falls
back to `0x8003` for that recovery only; non-media messages keep using `0x8003`
exclusively.

**Work:**

- Define the media packet/fragment identity and timeout semantics from JT/T 808
  and applicable JT/T 1078 rules; distinguish media packet IDs from JT808
  transport fragment IDs.
- Retain bounded receive state keyed by device/media ID and track expected,
  received, duplicate and missing packet IDs.
- At completion or timeout, send `0x8800` with exact missing packet IDs, retry
  within a bounded policy, and finalize or expire the media record.
- Ensure `0x8003` remains transport-level fragmentation recovery and has no
  accidental interaction with media-level retransmission.
- Test dropped, duplicated, reordered and expired media packets over the
  simulator, including successful finalization after retransmission.

**Acceptance:** Dropping a media packet leads the server to request its media
packet ID using `0x8800`; the simulator replays it; the server reconstructs
the media exactly once and releases bounded state after completion/timeout.

**Dependency:** Verify media packet framing/IDs in the applicable standard
before choosing a receive-state design.

### P1 — Commercial protocol coverage and data correctness

#### P1.1 — Finish multimedia resource and location support

**Status:** Done. `0x0200` decodes the 28-byte base body plus TLV extended
(附加信息) items. `0x9205/0x1205` are backed by the same in-memory media catalog
as `0x0802` search, with channel/media-type/time-range filters and serial
correlation. HTTP `/device/:phone/media/resources` endpoint available.

**Work:**

- Decode `0x0200` TLV extended items (ID + 2-byte length + data) after the
  28-byte base body; encode them back on serialization.
- Refactor `0x1205` to carry a typed list of `DeviceMedia` items (28 bytes
  each) instead of a single hard-coded record.
- Back `processMsg9205` with `IndustryCache.MediaUploads`, filtering by logic
  channel, media type (0=all/1=audio/2=video/3=video-or-av), and BCD time
  range. Zero time boundaries mean no constraint.
- Store the latest `0x1205` response per device and expose it via
  `IndustryCache.MediaResourceList`.

**Acceptance:** `0x9205` queries return the same media items that `0x0802`
search can retrieve; filters for channel, media type, and time range are
verified end-to-end; empty catalog returns zero items; answer serial matches
the query serial.

#### P1.2 — Implement high-value missing message families

**Status:** All six sub-groups implemented end-to-end (typed models,
processor, simulator storage, HTTP endpoints, e2e tests).

**Recommended order:**

1. **Done** Text issue `0x8300` and event set/report `0x8301/0x0301`.
2. **Done** Vehicle control `0x8500/0x0500` with explicit simulator-side safe test effects.
3. **Done** Electronic waybill `0x0701` and batch positioning `0x0704`.
4. **Done** Information question/menu/service `0x8302/0x0302`, `0x8303/0x0303`,
   `0x8304`.
5. **Done** Phone callback/phonebook `0x8400/0x8401` and terminal upgrade
   `0x8108/0x0108` where target deployments require them.
6. **Done** Transparent transmission `0x8900/0x0900` and compressed report
   `0x0901` (with zlib decompression) for integrations that need them.

For each selected family, add typed models, bounds/encoding validation,
processor/API behavior, simulator response or report behavior, and full flow
tests. Prioritize by target industry and deployment contract; do not treat every
Appendix B message as mandatory for every product.

#### P1.3 — Review JT/T 808-2019 Amendment 1 and version compatibility

**Status:** Not reviewed. The coverage document notes that the amendment text
is not in the repository.

**Work:** Obtain the official amendment, list affected clauses/messages, update
models and golden vectors, then document support by protocol version.

**Acceptance:** A reviewed clause-to-code checklist and protocol vectors cover
each amendment change in scope.

### P2 — Persistence and production operation

#### P2.1 — Add a persistence abstraction and durable storage

**Status:** Not done. Device/session data and recent locations, alarms, regions,
driver/CAN/recorder reports, media reports and uploads are held in memory;
some caches are bounded and disappear on restart.

**Work:** Define repository interfaces, retention/TTL policies and migrations;
implement durable storage for devices as needed, location history, alarm
history/confirmation, region/route catalog, industry reports and media metadata.
Store large media payloads separately from metadata (filesystem/object storage)
and enforce size/retention limits. Add startup recovery and cleanup.

**Acceptance:** Restart does not lose configured durable records; retention and
size policies are tested; storage failures surface through logs/API behavior;
memory stays bounded under sustained load.

#### P2.2 — Operational readiness

**Status:** Not comprehensively assessed.

**Work:** Review authentication/authorization on HTTP control endpoints,
configuration/secrets, rate and payload limits, structured metrics/logging,
graceful shutdown, reconnect behavior, session/device concurrency, and
deployment guidance. Add load and fault-injection tests after agreed targets
are defined.

**Acceptance:** Document operational assumptions and limits; exercise shutdown,
reconnect, concurrent devices, oversized reports and storage/network failures.

## Deferred / deployment-dependent

- RSA key exchange and encrypted bodies (`0x8A00/0x0A00`): defer until target
  terminals require them and key-management requirements are agreed.
- Vendor-reserved IDs and vendor-specific extensions: implement through an
  explicit registry only when a device integration requires a defined format.
- Durable alarm persistence was explicitly deferred earlier for a unified
  persistence phase; it remains tracked under P2.1.
- `JT/T 1078` media streaming is a separate protocol layer. This roadmap covers
  JT808 media metadata/control and retransmission; it does not claim a complete
  JT1078 RTP/media-stream implementation.

## How to maintain this record

- Update the status, code baseline and acceptance notes when a task lands.
- Keep message-ID state and implementation notes in
  [JT808-2019-coverage.md](JT808-2019-coverage.md); keep project tasks/dependencies
  here. Both documents should agree on partial versus complete behavior.
- A feature is complete only when codecs, processor/business behavior,
  simulator behavior where applicable, and tests/documentation are all updated.
- Record protocol edition/source when an implementation depends on external
  standards; do not mark a standards-dependent parser complete without its
  normative field definitions.
