# Dwnl native protocol v1

Every frame contains one UTF-8 JSON object, at most 524288 bytes (512 KiB). Native framing uses a four byte little-endian length; framing belongs to T3. Extra properties are rejected at every nesting level. Null is accepted only where explicitly specified. IDs and cursors match `[A-Za-z0-9][A-Za-z0-9_-]{0,127}`. Numbers are finite; integer byte counts and sequences are at most 9007199254740991 so both languages represent them exactly. Sequences start at zero. Protocol version is the literal integer 1.

Strings are nonempty without control characters and must contain a character outside Unicode White_Space plus U+FEFF; titles, destination labels, final paths and error messages are at most 4096 UTF-8 bytes; codec/version strings at most 512. Timestamps use a real UTC calendar date with terminal Z and at most millisecond fractional precision. Dimensions are positive integers. Optional measured media duration is positive; optional choice size may be zero. Source URLs are HTTPS without credentials, custom ports, whitespace or backslashes, and exact official entry hosts. Per-request URL validation is an entry gate; T4 must separately enforce all engine requests, redirects, DNS and CDN restrictions before enabling real network use.

Requests have `protocolVersion`, `requestId`, `operation`, `payload`. `handshake` omits `hostSessionId`; all other requests require the current `hostSessionId`. Responses have `protocolVersion`, `kind: "response"`, `requestId`, `hostSessionId`, `operation`, and exactly one of `result` or `error`. Events have `protocolVersion`, `kind: "event"`, `hostSessionId`, `jobId`, `sequence`, `state`, and optional `progress`, `completedFile`, `error`. No media bytes, raw command strings, CLI flags, executable paths, cookies or arbitrary destination paths can be supplied in requests.

`extension/src/protocol.ts` and `host/internal/contracts/types.go` define the exact fields. Both decoders test `contracts/cases.json` and reject unknown nested fields. Operation results are validated independently using the repeated `operation` field.

| Operation | Payload | Result |
|---|---|---|
| handshake | `{extensionVersion}` | `{hostVersion,engineVersion,dependencies,capabilities,defaultDestination}` |
| analyze | `{url}` | `Analysis` |
| listFormats | `{analysisId,itemId,cursor?}` | `{analysisId,itemId,choices,nextCursor?,omittedMetadata}` |
| chooseDestination | `{userGesture:true}` | `{destinationId,label,canceled}`; cancel retains the current destination |
| start video | `{analysisId,itemId,mode:"video",videoOptionId,destinationId}` | `{jobId,startRequestId,sequence,state}` |
| start mp3 | `{analysisId,itemId,mode:"mp3",bitrateKbps,destinationId}`; 128, 192 or 320 only | Same as video |
| status | `{}`, `{jobId}` or `{startRequestId}` | `{snapshot:JobSnapshot|null}`; snapshot is authoritative |
| reconcile | `{previousHostSessionId,jobId}` or `{previousHostSessionId,startRequestId}` | `{previousHostSessionId,outcome,snapshot}`; read only, never restarts a task |
| cancel | `{jobId}` | `{snapshot:JobSnapshot}`; canceling is distinct from terminal canceled |

The envelope request ID identifies a cancel operation; `startRequestId` identifies the earlier start. `status` never accepts an old session. `reconcile` requires a previous session different from the current session and one job/start identity. Ownership of the previous record is checked by the host. Its snapshot is either terminal or null. `confirmed` requires a terminal snapshot; `unconfirmed` may carry only interrupted, and `notFound` requires null. The snapshot session must equal `previousHostSessionId`. The response envelope still uses the current session.

An `Analysis` carries session ID, canonical HTTPS `sourceUrl`, UTC RFC3339 `expiresAt`, items, and `omittedMetadata`. Each item has an opaque item ID, title, optional measured duration, audio availability, video choices, optional formats cursor and an omission indicator. Video choices carry opaque option IDs, real container, dimensions/codecs and optional measured or estimated size. Unknown dimensions remain absent. Cursors paginate choices; omission indicators never excuse truncating IDs or required fields. Array pages are limited to 100 entries.

Starting requires current session/analysis, unexpired analysis, item membership and selected option membership. MP3 additionally requires source audio and bitrate 128/192/320; 192 is the UI default. These checks are exposed through the optional decoder validation context and the Go `ValidateRequestContext` helper. The host owns destination approval and authoritative analysis lookup; a schema-valid ID alone grants no access.

Job states are `analyzing`, `ready`, `downloading`, `muxing`, `converting`, `saving`, `completed`, `canceling`, `canceled`, `failed`, `interrupted`. A completed snapshot/event requires a verified `completedFile`; other states forbid it. Failed requires a typed error; only failed/interrupted may carry an error. `Progress.stage` is downloading/muxing/converting/saving. Percent is permitted only with a known positive byte total or duration and its numerator; totals without numerators are rejected and percent stays within 0..100. Numerators alone are permitted for unknown totals. A progress stage must match the event/snapshot state.

`CompletedFile` reports a host-produced final path, actual format, nonzero byte size, lowercase SHA-256, optional codec fields and MP3 encoding bitrate. MP3 requires audioCodec `mp3` and its configured bitrate. This is an output summary, never browser authority to select a path. Physical validation/hash/reconciliation belong to the host/motor. No raw extraction URLs, full stderr or tokens belong in user-facing errors.

Errors are `{code,message,retryable}`. Codes: INVALID_MESSAGE, MESSAGE_TOO_LARGE, INCOMPATIBLE_PROTOCOL, UNKNOWN_OPERATION, INVALID_SELECTION, SESSION_ENDED, ANALYSIS_EXPIRED, INVALID_CURSOR, BUSY, REQUEST_CONFLICT, UNKNOWN_JOB, DEPENDENCY_UNAVAILABLE, MP3_UNAVAILABLE, NO_AUDIO, ACCESS_RESTRICTED, NETWORK_ERROR, NETWORK_POLICY_BLOCKED, DESTINATION_ERROR, DISK_FULL, ENGINE_ERROR, CANCEL_FAILED, INVALID_OUTPUT, INTERNAL_ERROR.

Go ports: `Engine.Analyze(context.Context,string) (contracts.Analysis,error)` and `Engine.Execute(context.Context,contracts.DownloadPlan,func(contracts.Progress)) (contracts.CompletedFile,error)`. Internal `DownloadPlan` resolves `DestinationPath`, `WorkDir`, current identities and selection. `ProcessRunner.Run(context.Context,ExecSpec,func(ProcessLine)) (ProcessResult,error)` accepts a closed executable role (`yt-dlp`, `deno`, `ffmpeg`, `ffprobe`), built arguments and working directory. It resolves private executable paths and owns termination. `ProcessLine` has stdout/stderr plus text (limit 65536 UTF-8 bytes); `ProcessResult` has exit code and canceled. These structures never appear as browser inputs.

Extension identity lives in `extension-identity.json`: public SPKI key, deterministic Chrome ID derived from the first 16 SHA-256 bytes mapped nibble a..p, host name and protocol version. No private signing key is stored. There is no registration or Chrome mutation in T1.

Example after handshake, analysis and destination approval:

```json
{"protocolVersion":1,"requestId":"req-start","hostSessionId":"session-a","operation":"start","payload":{"analysisId":"analysis-a","itemId":"item-a","mode":"mp3","bitrateKbps":192,"destinationId":"destination-a"}}
```

The host acknowledges the same start identity; a later event reports this stage's measured progress:

```json
{"protocolVersion":1,"kind":"response","requestId":"req-start","hostSessionId":"session-a","operation":"start","result":{"jobId":"job-a","startRequestId":"req-start","sequence":0,"state":"downloading"}}
{"protocolVersion":1,"kind":"event","hostSessionId":"session-a","jobId":"job-a","sequence":1,"state":"downloading","progress":{"stage":"downloading","completedBytes":500,"totalBytes":1000,"percent":50}}
```

Each line above is a separate frame. Unknown totals omit percent. An old cached active job needs status/reconcile confirmation before the UI treats it as current.

Native extractors may return unknown codecs or audio presence. `videoCodec: "unknown"` means that local ffprobe must confirm the codec after download. Optional `audioUnknown: true` on an item or choice distinguishes unknown audio from confirmed silence; `hasAudio` remains false until positive source evidence. MP3 can be attempted for unknown audio, but must return `NO_AUDIO` if local readback finds no audio stream. Video processing resolves the flag from the actual downloaded streams before muxing. This does not authorize a fallback format or invent source bitrate.
