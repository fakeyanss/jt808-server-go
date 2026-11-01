# JT/T 808-2019 Message Coverage

This matrix compares the implementation in `internal/protocol/model` and
`internal/protocol/msg_processor.go` with the message ID table in Appendix B,
Table B.1 of `docs/协议文档/JTT 808-2019.pdf`. Status is based on executable
code, not the older feature summary in README.

For project-wide completed work, prioritized TODOs, dependencies, and
acceptance criteria, see [JT808-project-roadmap.md](JT808-project-roadmap.md).

The 2019 standard's Amendment 1 was published by the Ministry of Transport in
2021. Its official announcement is recorded at
<https://xxgk.mot.gov.cn/2020/jigou/kjs/202111/t20211111_3625608.html>. The
amendment document itself is not included in this repository, so this matrix
does not claim amendment-specific conformance; it must be updated when the
amendment text has been reviewed.

## Status definitions

- **Implemented**: message has a model and is connected to a processor path.
- **Partial**: a model or processor path exists, but business behavior is a
  stub, hard-coded, or has a documented TODO.
- **Model only**: a message model exists but is not connected to the processor.
- **Missing**: no message model or processor path was found.
- **Reserved/custom**: standard-reserved or vendor-defined ID range; no generic
  extension dispatch is implemented.

## Appendix B, Table B.1

| ID | Message | Status | Implementation note |
|---|---|---|---|
| `0x0001` | Terminal general acknowledgment | Implemented | Decodes acknowledgment fields and settles pending messages. |
| `0x8001` | Platform general acknowledgment | Implemented | Encodes responses and settles pending messages. |
| `0x0002` | Terminal heartbeat | Implemented | Refreshes device activity and receives `0x8001`. |
| `0x8003` | Platform retransmission request | Implemented | Server requests missing fragments after an idle interval, retries at most three times, and clears state on completion; the client simulator replays requested frames. |
| `0x0005` | Terminal retransmission request | Implemented | Oversized platform messages are split into indexed frames and cached; the terminal simulator can request selected frames, which the server replays byte-for-byte. |
| `0x0100` | Terminal registration | Implemented | Validates duplicate plate/phone and returns `0x8100`. |
| `0x8100` | Terminal registration response | Implemented | Returns authentication code; client simulator proceeds to authentication. |
| `0x0003` | Terminal logout | Implemented | Removes device state and cancels keepalive tracking. |
| `0x0004` | Query server time | Implemented | Responds with a BCD timestamp in `0x8004`. |
| `0x0102` | Terminal authentication | Implemented | Validates the generated auth code and updates device state. |
| `0x8103` | Set terminal parameters | Implemented | Decodes and caches provided parameter values. |
| `0x8104` | Query terminal parameters | Implemented | HTTP query sends a serial-correlated request and returns the terminal response; timeout is 10 seconds. |
| `0x0104` | Terminal parameters response | Implemented | Validates through normal decoding, caches parameters, and delivers to a waiter keyed by phone and answered platform serial. |
| `0x8105` | Terminal control | Partial | Model, GBK command parameters, HTTP downlink endpoint, and simulator-side generic acknowledgement are implemented. Command-specific effects remain terminal responsibilities. |
| `0x8106` | Query specified terminal parameters | Implemented | HTTP `/device/:phone/params?ids=...` sends selected IDs; the response is correlated by phone and platform serial. |
| `0x8107` | Query terminal properties | Implemented | HTTP `/device/:phone/properties` sends a query and waits up to 10 seconds for the terminal response. |
| `0x0107` | Terminal properties response | Implemented | Decodes 2011/2013/2019 field widths and delivers the response to the pending phone query. |
| `0x8108` | Terminal upgrade package | Implemented | Typed upgrade type, 5-byte maker ID, version string, and DWORD-prefixed upgrade data; cached per device; terminal acknowledges `0x0001`; HTTP `/device/:phone/terminal/upgrade` endpoint. |
| `0x0108` | Terminal upgrade result notification | Implemented | Decodes upgrade type and result code; cached per device; platform replies `0x8001`. |
| `0x0200` | Location information report | Implemented | Decodes the 28-byte base body plus TLV extended (附加信息) items; caches latest geo data; alarm-bit events are kept in a bounded in-memory history and can be confirmed after terminal acknowledgment. Durable storage remains open. |
| `0x8201` | Location information query | Implemented | Sends a query to the terminal; HTTP `/device/:phone/location` waits for the serial-correlated response. |
| `0x0201` | Location information query response | Implemented | Encodes/decodes the answer serial and base location report; delivers responses to pending HTTP queries. |
| `0x8202` | Temporary location tracking control | Implemented | Client simulator changes its location report interval for the requested validity period and restores the configured interval on stop/expiry; replies with `0x0001`. |
| `0x8301` | Event setting | Implemented | Typed set type and event item list with GBK content; simulator stores events per set type (delete/replace/append/modify); HTTP `/device/:phone/events/set` endpoint. |
| `0x0301` | Event report | Implemented | Decodes event ID; cached per device; platform replies `0x8001`. |
| `0x8302` | Question issue | Implemented | Typed flag, GBK question, and candidate answer list (DWORD ID + GBK content); cached per device; terminal acknowledges `0x0001`; HTTP `/device/:phone/question/issue` endpoint. |
| `0x0302` | Question response | Implemented | Decodes answer serial and chosen candidate ID; cached per device; platform replies `0x8001`. |
| `0x8303` | Information menu setting | Implemented | Typed set type and menu item list (DWORD ID + GBK content); simulator stores menu per set type (delete/replace/append/modify); HTTP `/device/:phone/info/menu/set` endpoint. |
| `0x0303` | Information menu response/cancel | Implemented | Decodes on-demand/cancel action flag and menu ID; cached per device; platform replies `0x8001`. |
| `0x8304` | Information service | Implemented | Typed info type, WORD length and GBK content; cached per device; terminal acknowledges `0x0001`; HTTP `/device/:phone/info/service` endpoint. |
| `0x8004` | Query server time response | Implemented | Decodes the server timestamp; the client simulator accepts the response. |
| `0x8400` | Call callback | Implemented | Typed flag and GBK phone number; cached per device; terminal acknowledges `0x0001`; HTTP `/device/:phone/call/callback` endpoint. |
| `0x8401` | Set phonebook | Implemented | Typed set type and contact list (flag, GBK phone, GBK name); simulator stores contacts per set type (delete/replace/append/modify) keyed by phone number; HTTP `/device/:phone/phonebook/set` endpoint. |
| `0x8500` | Vehicle control | Implemented | Typed control flags; simulator stores flags and echoes them in `0x0500`; HTTP `/device/:phone/vehicle/control` endpoint. |
| `0x0500` | Vehicle control response | Implemented | Decodes control flags; cached per device; platform replies `0x8001`. |
| `0x8600` | Set circular area | Implemented | Typed circle area items (attribute-controlled time/speed fields, GBK name) are decoded and applied to the per-device simulator catalog; structured JSON HTTP downlink. |
| `0x8601` | Delete circular area | Implemented | Encodes/decodes the standard ID list and supports deletion of all areas; applied to the simulator catalog; HTTP downlink plus simulator acknowledgement. |
| `0x8602` | Set rectangular area | Implemented | Typed rectangle area items decoded and stored in the simulator catalog; structured JSON HTTP downlink. |
| `0x8603` | Delete rectangular area | Implemented | Shared delete-ID message model, HTTP downlink, catalog deletion and simulator acknowledgement. |
| `0x8604` | Set polygonal area | Implemented | Typed polygon area items with vertex lists decoded and stored in the simulator catalog; structured JSON HTTP downlink. |
| `0x8605` | Delete polygonal area | Implemented | Shared delete-ID message model, HTTP downlink, catalog deletion and simulator acknowledgement. |
| `0x8606` | Set route | Implemented | Typed route items with route points and segment constraints decoded and stored in the simulator catalog; structured JSON HTTP downlink. |
| `0x8607` | Delete route | Implemented | Shared delete-ID message model, HTTP downlink, catalog deletion and simulator acknowledgement. |
| `0x8700` | Driving recorder data collection command | Implemented | Typed GB/T 19056 data block codec for commands `0x31`/`0x33`/`0x56`/`0x57`/`0x58`; HTTP downlink available. Simulator returns a typed `0x0700` response block. |
| `0x0700` | Driving recorder data upload | Implemented | Answer serial, command byte, and typed GB/T 19056 response blocks (`0xB1`/`0xB3`/`0xD6`/`0xD8`) are decoded; data is cached and a generic acknowledgment is sent. |
| `0x8701` | Driving recorder parameter download | Implemented | Typed `0x41` parameter-setting block (with 16-byte Salt) and `0xC1` response block codec; HTTP downlink available. Simulator sends `0x0001`. |
| `0x8203` | Manual alarm confirmation | Implemented | HTTP command serializes alarm serial/type; matching in-memory alarm events are marked confirmed after terminal `0x0001`. Durable alarm storage remains tracked under persistence work. |
| `0x8204` | Server-initiated link detection request | Implemented | Empty-body request is decoded by the client simulator and receives `0x0001`. |
| `0x8300` | Text information issue | Implemented | Typed flag, text type, and GBK text; simulator acknowledges with `0x0001`; HTTP `/device/:phone/text/issue` endpoint. |
| `0x0702` | Driver identity information report | Implemented | Decodes IC-card state, BCD time, read result, GBK name/issuer and fixed-width credentials; caches latest report and sends `0x8001`. |
| `0x8702` | Driver identity information request | Partial | Empty-body command; simulator returns a synthetic `0x0702`; HTTP downlink is available. |
| `0x0704` | Batch positioning data upload | Implemented | Decodes item count, data type, and per-item length-prefixed bodies; each body is parsed as a typed `0x0200` location. Cached per device; platform replies `0x8001`. |
| `0x0705` | CAN bus data upload | Implemented | Decodes the 5-byte BCD timestamp, CAN ID flags and fixed 8-byte data items; caches latest report and sends `0x8001`. |
| `0x0800` | Multimedia event information upload | Implemented | Decodes and caches the 8-byte event descriptor; sends `0x8001`. |
| `0x0801` | Multimedia data upload | Implemented | Decodes the media descriptor, fixed 28-byte base location body, and remaining media bytes; bounded in-memory cache, `0x8800` response, and server-side missing-fragment detection via `0x8800` retransmission. Extended location items remain open. |
| `0x8800` | Multimedia upload response | Implemented | Encodes/decodes media ID and requested packet IDs. Server detects missing `0x0801` fragments and sends `0x8800` with the missing packet IDs; the simulator replays cached frames and the server reassembles exactly once. |
| `0x8801` | Camera immediate capture command | Implemented | Decodes the 12-byte command; simulator returns `0x0805`. HTTP downlink available. |
| `0x0805` | Camera capture response | Implemented | Decodes result and media IDs, caches latest response, and receives platform `0x8001`. |
| `0x8802` | Search stored multimedia data | Implemented | Decodes search criteria and HTTP downlink; simulator filters its bounded in-memory `0x0801` catalog by type, channel, event, and capture time, then returns matching records. |
| `0x0802` | Stored multimedia search response | Implemented | Decodes/encodes each 35-byte result as media ID, media type, channel, event, and typed `0x0200` location fields; response is cached. |
| `0x8608` | Query region/route data | Implemented | Decodes query type and DWORD ID list; HTTP request waits for a matching `0x0608`; the simulator answers from its per-device region/route catalog (`internal/storage/region_catalog.go`). |
| `0x0608` | Region/route data response | Implemented | Query type/count envelope plus typed circle/rectangle/polygon/route records are decoded and encoded; responses are correlated to pending queries. |
| `0x0701` | Electronic waybill upload | Implemented | Decodes GBK waybill content; cached per device; platform replies `0x8001`. |
| `0x8803` | Stored multimedia upload command | Implemented | Typed 16-byte command with media type/channel/event, BCD range and delete flag; HTTP body is validated, and the simulator uploads matching stored media as `0x0801` records. |
| `0x8804` | Start audio recording command | Implemented | Typed 5-byte start/stop, duration, save and sample-rate fields; the simulator toggles its recording flag and emits sample audio `0x0801` frames on start. |
| `0x8805` | Single stored multimedia upload command | Implemented | Typed media ID and delete flag; HTTP body is validated and the simulator replays the catalog item matching the requested media ID. |
| `0x8900` | Data downlink transparent transmission | Implemented | Typed DWORD message type and raw payload bytes; cached per device; terminal acknowledges `0x0001`; HTTP `/device/:phone/transparent/down` endpoint. |
| `0x0900` | Data uplink transparent transmission | Implemented | Decodes DWORD message type and raw payload bytes; cached per device; platform replies `0x8001`. |
| `0x0901` | Compressed data report | Implemented | Decodes DWORD length and zlib-compressed body; the inflated payload is stored in `Decompressed` (nil on failure); cached per device; platform replies `0x8001`. |
| `0x8A00` | Platform RSA public key | Missing | RSA public-key exchange and encrypted bodies are unsupported. |
| `0x0A00` | Terminal RSA public key | Missing | RSA public-key exchange and encrypted bodies are unsupported. |
| `0x8F00`–`0x8FFF` | Platform-reserved messages | Reserved/custom | No reserved-ID extension handler. |
| `0x0F00`–`0x0FFF` | Terminal-reserved messages | Reserved/custom | No reserved-ID extension handler. |
| `0xE000`–`0xEFFF` | Vendor-defined uplink messages | Reserved/custom | No vendor registry or custom handler mechanism. |
| `0xF000`–`0xFFFF` | Vendor-defined downlink messages | Reserved/custom | No vendor registry or custom handler mechanism. |

## Repository extensions outside Appendix B.1

| ID | Message | Status | Implementation note |
|---|---|---|---|
| `0x9205` | Query terminal audio/video resource list | Implemented | Processor filters the in-memory media catalog by channel, media type, and time range; HTTP `/device/:phone/media/resources` endpoint available. |
| `0x1205` | Terminal audio/video resource list | Implemented | Typed list of `DeviceMedia` items (28 bytes each) generated from the media catalog; response cached per device. |

## Golden protocol vectors

`internal/protocol/packet_codec_test.go` contains exact frame-byte vectors for:

- A 2013-format heartbeat with an empty body.
- A versioned 2019-format heartbeat.
- A terminal general acknowledgment with a five-byte body.
- A fragmented location frame whose body contains both reserved bytes `0x7e`
  and `0x7d`, verifying escaping, checksum, and fragment-header encoding.

These vectors are independently assembled from the normative header/body
layouts and escape/checksum rules in clauses 4.3–4.4 and Appendix B. They are
not copied from a published full-frame example. The local 2019 PDF does not
include Amendment 1; amendment-specific vectors remain an open item pending
review of the amendment text.
