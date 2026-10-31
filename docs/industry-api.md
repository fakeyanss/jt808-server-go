# Industry protocol API and simulator

The HTTP API issues JT808 downlinks to an online device. Asynchronous commands
return `202 Accepted`; resulting terminal reports can be read from
`GET /device/:phone/reports/:kind`.

## Region and route management

- `POST /device/:phone/regions/set` accepts `messageId` (`0x8600`, `0x8602`,
  `0x8604`, or `0x8606`) and `bodyHex`. `bodyHex` is the standard JT808 message
  body, excluding the JT808 header. The server validates the setting attribute
  and nonzero item count, then sends the body unchanged.
- `POST /device/:phone/regions/delete` accepts `messageId` (`0x8601`, `0x8603`,
  `0x8605`, or `0x8607`) and an `ids` array. An empty array deletes all objects
  of that type; one message supports up to 125 IDs.
- `POST /device/:phone/regions/query` accepts `queryType` (1 circle, 2 rectangle,
  3 polygon, 4 route) and optional `ids`. It waits up to 10 seconds for `0x0608`.
  The response envelope is decoded; type-specific variable-length records are
  currently retained as raw bytes. The simulator currently returns no stored
  region/route records.

## Driver, CAN, and driving recorder

- `POST /device/:phone/driver/query` sends `0x8702`.
- `POST /device/:phone/recorder/command` accepts `messageId` (`0x8700` or
  `0x8701`), `command`, and optional `dataHex` (the GB/T 19056 data block).
- Read the latest reports with `GET /device/:phone/reports/driver`, `/can`, or
  `/recorder`.

The client simulator returns synthetic driver and recorder data to those
requests. It can send one sample driver, CAN, recorder, multimedia event, and
multimedia upload report after authentication with `go run ./test/client --industry-reports`.

## Multimedia

- `POST /device/:phone/media/capture` accepts the `0x8801` command fields and
  returns asynchronously; the simulator answers with `0x0805`.
- `POST /device/:phone/media/search` accepts the `0x8802` media type, channel,
  event, start time, and end time. The simulator filters its in-memory upload
  catalog and returns matching `0x0802` items with typed media metadata and
  location data. All-zero start/end times mean no time filter.
- `POST /device/:phone/media/command` sends the standard body in `bodyHex` for
  `0x8803`, `0x8804`, or `0x8805`; each body is decoded and validated against its
  typed command fields before sending.
- Read received reports with `/reports/media-event`, `/reports/media-capture`,
  `/reports/media-search`, and `/reports/media-uploads`.

The upload handler decodes the 8-byte media descriptor, 28-byte base location
report, and remaining media fragment bytes. The in-memory cache keeps at most
100 uploads per device, and ignores a single fragment larger than 1 MiB. Media
payloads and reports are not persisted yet. `0x8800` packet-ID retransmission
is implemented in the client simulator for fragmented uploads: it retains sent
frames and replays requested IDs. Server-side detection of missing media packets
and request generation still needs implementation. The `0x0802` result decoder
parses each standard 35-byte media record.
