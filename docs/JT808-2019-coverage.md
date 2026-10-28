# JT/T 808-2019 Message Coverage

This matrix compares the implementation in `internal/protocol/model` and
`internal/protocol/msg_processor.go` with the message ID table in Appendix B,
Table B.1 of `docs/协议文档/JTT 808-2019.pdf`. Status is based on executable
code, not the older feature summary in README.

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
| `0x8108` | Terminal upgrade package | Missing | No model or processor path. |
| `0x0108` | Terminal upgrade result notification | Missing | No model or processor path. |
| `0x0200` | Location information report | Partial | Decodes and caches latest geo data; alarm-bit events are kept in a bounded in-memory history and can be confirmed after terminal acknowledgment. Durable storage and extended location items remain open. |
| `0x8201` | Location information query | Implemented | Sends a query to the terminal; HTTP `/device/:phone/location` waits for the serial-correlated response. |
| `0x0201` | Location information query response | Implemented | Encodes/decodes the answer serial and base location report; delivers responses to pending HTTP queries. |
| `0x8202` | Temporary location tracking control | Implemented | Client simulator changes its location report interval for the requested validity period and restores the configured interval on stop/expiry; replies with `0x0001`. |
| `0x8301` | Event setting | Missing | No model or processor path. |
| `0x0301` | Event report | Missing | No model or processor path. |
| `0x8302` | Question issue | Missing | No model or processor path. |
| `0x0302` | Question response | Missing | No model or processor path. |
| `0x8303` | Information menu setting | Missing | No model or processor path. |
| `0x0303` | Information menu response/cancel | Missing | No model or processor path. |
| `0x8304` | Information service | Missing | No model or processor path. |
| `0x8004` | Query server time response | Implemented | Decodes the server timestamp; the client simulator accepts the response. |
| `0x8400` | Call callback | Missing | No model or processor path. |
| `0x8401` | Set phonebook | Missing | No model or processor path. |
| `0x8500` | Vehicle control | Missing | No model or processor path. |
| `0x0500` | Vehicle control response | Missing | No model or processor path. |
| `0x8600` | Set circular area | Missing | No model or processor path. |
| `0x8601` | Delete circular area | Partial | Encodes/decodes the standard ID list and supports deletion of all areas; HTTP downlink plus simulator acknowledgement. |
| `0x8602` | Set rectangular area | Missing | No model or processor path. |
| `0x8603` | Delete rectangular area | Partial | Shared delete-ID message model, HTTP downlink, and simulator acknowledgement. |
| `0x8604` | Set polygonal area | Missing | No model or processor path. |
| `0x8605` | Delete polygonal area | Partial | Shared delete-ID message model, HTTP downlink, and simulator acknowledgement. |
| `0x8606` | Set route | Missing | No model or processor path. |
| `0x8607` | Delete route | Partial | Shared delete-ID message model, HTTP downlink, and simulator acknowledgement. |
| `0x8700` | Driving recorder data collection command | Missing | No model or processor path. |
| `0x0700` | Driving recorder data upload | Missing | No model or processor path. |
| `0x8701` | Driving recorder parameter download | Missing | No model or processor path. |
| `0x8203` | Manual alarm confirmation | Partial | HTTP command serializes alarm serial/type; matching in-memory alarm events are marked confirmed after terminal `0x0001`; durable storage remains open. |
| `0x8204` | Server-initiated link detection request | Implemented | Empty-body request is decoded by the client simulator and receives `0x0001`. |
| `0x8300` | Text information issue | Missing | No model or processor path. |
| `0x0702` | Driver identity information report | Missing | No model or processor path. |
| `0x8702` | Driver identity information request | Missing | No model or processor path. |
| `0x0704` | Batch positioning data upload | Missing | No model or processor path. |
| `0x0705` | CAN bus data upload | Missing | No model or processor path. |
| `0x0800` | Multimedia event information upload | Model only | Model is marked TODO and is not registered in the processor. |
| `0x0801` | Multimedia data upload | Model only | Model exists; no processor path, storage pipeline, or `0x8800` response flow. |
| `0x8800` | Multimedia upload response | Model only | Model exists; no processor path. |
| `0x8801` | Camera immediate capture command | Missing | No model or processor path. |
| `0x0805` | Camera capture response | Missing | No model or processor path. |
| `0x8802` | Search stored multimedia data | Missing | No model or processor path. |
| `0x0802` | Stored multimedia search response | Missing | No model or processor path. |
| `0x8608` | Query region/route data | Missing | No model or processor path. |
| `0x0608` | Region/route data response | Missing | No model or processor path. |
| `0x0701` | Electronic waybill upload | Missing | No model or processor path. |
| `0x8803` | Stored multimedia upload command | Missing | No model or processor path. |
| `0x8804` | Start audio recording command | Missing | No model or processor path. |
| `0x8805` | Single stored multimedia upload command | Missing | No model or processor path. |
| `0x8900` | Data uplink transparent transmission | Missing | No model or processor path. |
| `0x0900` | Data downlink transparent transmission | Missing | No model or processor path. |
| `0x0901` | Compressed data report | Missing | No model or processor path. |
| `0x8A00` | Platform RSA public key | Missing | RSA public-key exchange and encrypted bodies are unsupported. |
| `0x0A00` | Terminal RSA public key | Missing | RSA public-key exchange and encrypted bodies are unsupported. |
| `0x8F00`–`0x8FFF` | Platform-reserved messages | Reserved/custom | No reserved-ID extension handler. |
| `0x0F00`–`0x0FFF` | Terminal-reserved messages | Reserved/custom | No reserved-ID extension handler. |
| `0xE000`–`0xEFFF` | Vendor-defined uplink messages | Reserved/custom | No vendor registry or custom handler mechanism. |
| `0xF000`–`0xFFFF` | Vendor-defined downlink messages | Reserved/custom | No vendor registry or custom handler mechanism. |

## Repository extensions outside Appendix B.1

| ID | Message | Status | Implementation note |
|---|---|---|---|
| `0x9205` | Query terminal audio/video resource list | Partial | Processor returns a hard-coded one-item result and echoes the requested time range. |
| `0x1205` | Terminal audio/video resource list | Partial | Model exists; list generation is not backed by a media catalog. |

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
