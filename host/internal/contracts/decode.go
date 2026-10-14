package contracts

import (
	"encoding/json"
	"math"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var timestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,3})?Z$`)
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var authorityPattern = regexp.MustCompile(`^[A-Za-z0-9.-]+(:443)?$`)

func validPercentEscapes(s string) bool {
	hex := func(b byte) bool { return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F' }
	for i := 0; i < len(s); i++ {
		if s[i] == '%' {
			if i+2 >= len(s) || !hex(s[i+1]) || !hex(s[i+2]) {
				return false
			}
			i += 2
		}
	}
	return true
}

type validator struct{ err error }

func (v *validator) fail(code ErrorCode, message string) {
	if v.err == nil {
		v.err = &ProtocolDecodeError{Code: code, Message: message}
	}
}
func (v *validator) object(value any, required, optional string) map[string]any {
	m, ok := value.(map[string]any)
	if !ok {
		v.fail(InvalidMessage, "Expected an object")
		return map[string]any{}
	}
	allowed := map[string]bool{}
	for _, key := range strings.Fields(required) {
		allowed[key] = true
		if _, ok := m[key]; !ok {
			v.fail(InvalidMessage, "Missing "+key)
		}
	}
	for _, key := range strings.Fields(optional) {
		allowed[key] = true
	}
	for key := range m {
		if !allowed[key] {
			v.fail(InvalidMessage, "Unknown field "+key)
		}
	}
	return m
}
func (v *validator) text(value any, max int) string {
	s, ok := value.(string)
	if !ok || strings.TrimFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == '\uFEFF' }) == "" || len(s) > max || strings.ContainsAny(s, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f\x7f") {
		v.fail(InvalidMessage, "Invalid text")
	}
	return s
}
func (v *validator) id(value any) string {
	s, ok := value.(string)
	if !ok || !idPattern.MatchString(s) {
		v.fail(InvalidMessage, "Invalid identifier")
	}
	return s
}
func (v *validator) boolean(value any) bool {
	b, ok := value.(bool)
	if !ok {
		v.fail(InvalidMessage, "Expected a boolean")
	}
	return b
}
func (v *validator) number(value any, min, max float64) float64 {
	n, ok := value.(float64)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < min || n > max {
		v.fail(InvalidMessage, "Invalid number")
	}
	return n
}
func (v *validator) integer(value any, min float64) float64 {
	n := v.number(value, min, float64(MaxSafeInteger))
	if n != math.Trunc(n) {
		v.fail(InvalidMessage, "Expected a safe integer")
	}
	return n
}
func (v *validator) positive(value any) float64 {
	n := v.number(value, 0, math.MaxFloat64)
	if n <= 0 {
		v.fail(InvalidMessage, "Expected a positive number")
	}
	return n
}
func (v *validator) enum(value any, choices string) string {
	s, ok := value.(string)
	if ok {
		for _, choice := range strings.Fields(choices) {
			if s == choice {
				return s
			}
		}
	}
	v.fail(InvalidMessage, "Unknown enum value")
	return s
}
func (v *validator) operation(value any) Operation {
	s, ok := value.(string)
	if !ok {
		v.fail(InvalidMessage, "Invalid operation")
		return ""
	}
	for _, operation := range []Operation{OperationHandshake, OperationAnalyze, OperationListFormats, OperationChooseDestination, OperationStart, OperationStatus, OperationReconcile, OperationCancel} {
		if s == string(operation) {
			return operation
		}
	}
	v.fail(UnknownOperation, "Unknown operation")
	return ""
}
func (v *validator) version(m map[string]any) {
	version, exists := m["protocolVersion"]
	if !exists {
		v.fail(InvalidMessage, "Missing protocolVersion")
		return
	}
	if n, ok := version.(float64); !ok || n != ProtocolVersion {
		v.fail(IncompatibleProtocol, "Unsupported protocol version")
	}
}
func (v *validator) array(value any, min int) []any {
	a, ok := value.([]any)
	if !ok || len(a) < min || len(a) > 100 {
		v.fail(InvalidMessage, "Invalid array page")
	}
	return a
}
func has(m map[string]any, key string) bool { _, ok := m[key]; return ok }
func (v *validator) optionalText(m map[string]any, key string, max int) {
	if has(m, key) {
		v.text(m[key], max)
	}
}
func (v *validator) optionalID(m map[string]any, key string) {
	if has(m, key) {
		v.id(m[key])
	}
}
func (v *validator) timestamp(value any) time.Time {
	s, ok := value.(string)
	if !ok || !timestampPattern.MatchString(s) {
		v.fail(InvalidMessage, "Invalid UTC timestamp")
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		v.fail(InvalidMessage, "Invalid UTC timestamp")
	}
	return t
}
func (v *validator) sourceURL(value any) {
	s := v.text(value, 4096)
	if !strings.HasPrefix(s, "https://") || strings.TrimSpace(s) != s || strings.ContainsAny(s, "\\ \t\n\r") || !validPercentEscapes(s) {
		v.fail(InvalidMessage, "Invalid HTTPS URL")
		return
	}
	authority := s[len("https://"):]
	if end := strings.IndexAny(authority, "/?#"); end >= 0 {
		authority = authority[:end]
	}
	if !authorityPattern.MatchString(authority) {
		v.fail(InvalidMessage, "Invalid HTTPS authority")
		return
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" || (u.Port() != "" && u.Port() != "443") {
		v.fail(InvalidMessage, "Invalid HTTPS URL")
		return
	}
	allowed := map[string]bool{"youtube.com": true, "www.youtube.com": true, "m.youtube.com": true, "music.youtube.com": true, "youtu.be": true, "instagram.com": true, "www.instagram.com": true, "tiktok.com": true, "www.tiktok.com": true, "vm.tiktok.com": true, "vt.tiktok.com": true, "x.com": true, "www.x.com": true, "twitter.com": true, "www.twitter.com": true}
	if !allowed[strings.ToLower(u.Hostname())] {
		v.fail(InvalidMessage, "Unsupported source host")
	}
}
func parse(data []byte) (map[string]any, *validator) {
	v := &validator{}
	if len(data) > MaxMessageBytes {
		v.fail(MessageTooLarge, "Message exceeds 512 KiB")
		return nil, v
	}
	if !utf8.Valid(data) {
		v.fail(InvalidMessage, "Message is not UTF-8")
		return nil, v
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		v.fail(InvalidMessage, "Malformed JSON")
		return nil, v
	}
	m, ok := value.(map[string]any)
	if !ok {
		v.fail(InvalidMessage, "Expected an object")
		return nil, v
	}
	return m, v
}

// decodeTyped normalizes accepted numeric spellings before typed unmarshalling.
// In particular JSON 1e0 has the same integer value in JavaScript and Go.
func decodeTyped(value any, destination any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return &ProtocolDecodeError{InvalidMessage, "Invalid JSON value"}
	}
	if err = json.Unmarshal(data, destination); err != nil {
		return &ProtocolDecodeError{InvalidMessage, "Invalid typed message"}
	}
	return nil
}

func DecodeRequest(data []byte) (Request, error) {
	m, v := parse(data)
	if v.err != nil {
		return Request{}, v.err
	}
	v.version(m)
	op := v.operation(m["operation"])
	if v.err != nil {
		return Request{}, v.err
	}
	v.object(m, "protocolVersion requestId operation payload", "hostSessionId")
	v.id(m["requestId"])
	if op == OperationHandshake {
		if has(m, "hostSessionId") {
			v.fail(InvalidMessage, "Handshake must omit hostSessionId")
		}
	} else {
		v.id(m["hostSessionId"])
	}
	v.payload(op, m["payload"])
	if v.err != nil {
		return Request{}, v.err
	}
	var request Request
	if err := decodeTyped(m, &request); err != nil {
		return Request{}, err
	}
	if op == OperationReconcile {
		payload, _ := PayloadAs[ReconcilePayload](request)
		if payload.PreviousHostSessionID == request.HostSessionID {
			return Request{}, &ProtocolDecodeError{SessionEnded, "Reconcile requires a previous session"}
		}
	}
	return request, nil
}
func (v *validator) payload(operation Operation, value any) {
	switch operation {
	case OperationHandshake:
		m := v.object(value, "extensionVersion", "")
		v.text(m["extensionVersion"], 512)
	case OperationAnalyze:
		m := v.object(value, "url", "")
		v.sourceURL(m["url"])
	case OperationListFormats:
		m := v.object(value, "analysisId itemId", "cursor")
		v.id(m["analysisId"])
		v.id(m["itemId"])
		v.optionalID(m, "cursor")
	case OperationChooseDestination:
		m := v.object(value, "userGesture", "")
		if !v.boolean(m["userGesture"]) {
			v.fail(InvalidMessage, "Destination selection requires a user gesture")
		}
	case OperationStart:
		m := v.object(value, "analysisId itemId mode destinationId", "videoOptionId bitrateKbps")
		v.id(m["analysisId"])
		v.id(m["itemId"])
		v.id(m["destinationId"])
		mode := v.enum(m["mode"], "video mp3")
		if mode == "video" {
			v.id(m["videoOptionId"])
			if has(m, "bitrateKbps") {
				v.fail(InvalidMessage, "Video mode forbids MP3 bitrate")
			}
		}
		if mode == "mp3" {
			v.bitrate(m["bitrateKbps"])
			if has(m, "videoOptionId") {
				v.fail(InvalidMessage, "MP3 mode forbids video selection")
			}
		}
	case OperationStatus:
		m := v.object(value, "", "jobId startRequestId")
		v.optionalID(m, "jobId")
		v.optionalID(m, "startRequestId")
		if has(m, "jobId") && has(m, "startRequestId") {
			v.fail(InvalidMessage, "Status identity is ambiguous")
		}
	case OperationReconcile:
		m := v.object(value, "previousHostSessionId", "jobId startRequestId")
		v.id(m["previousHostSessionId"])
		v.optionalID(m, "jobId")
		v.optionalID(m, "startRequestId")
		if has(m, "jobId") == has(m, "startRequestId") {
			v.fail(InvalidMessage, "Reconcile requires exactly one job identity")
		}
	case OperationCancel:
		m := v.object(value, "jobId", "")
		v.id(m["jobId"])
	}
}
func (v *validator) bitrate(value any) {
	n := v.integer(value, 0)
	if n != 128 && n != 192 && n != 320 {
		v.fail(InvalidMessage, "Unsupported MP3 bitrate")
	}
}

func DecodeMessage(data []byte) (Message, error) {
	m, v := parse(data)
	if v.err != nil {
		return Message{}, v.err
	}
	v.version(m)
	if v.err != nil {
		return Message{}, v.err
	}
	kind := v.enum(m["kind"], "response event")
	if kind == "response" {
		op := v.operation(m["operation"])
		if v.err != nil {
			return Message{}, v.err
		}
		v.object(m, "protocolVersion kind requestId hostSessionId operation", "result error")
		v.id(m["requestId"])
		session := v.id(m["hostSessionId"])
		if v.err != nil {
			return Message{}, v.err
		}
		if has(m, "result") == has(m, "error") {
			v.fail(InvalidMessage, "Response requires exactly one result or error")
		}
		if has(m, "error") {
			v.protocolError(m["error"])
		} else {
			v.result(op, m["result"], session, v.id(m["requestId"]))
		}
		if v.err != nil {
			return Message{}, v.err
		}
		var response Response
		if err := decodeTyped(m, &response); err != nil {
			return Message{}, err
		}
		return Message{Response: &response}, nil
	}
	if kind == "event" {
		v.object(m, "protocolVersion kind hostSessionId jobId sequence state", "progress completedFile error")
		v.id(m["hostSessionId"])
		v.id(m["jobId"])
		v.integer(m["sequence"], 0)
		v.stateBody(m)
		if v.err != nil {
			return Message{}, v.err
		}
		var event JobEvent
		if err := decodeTyped(m, &event); err != nil {
			return Message{}, err
		}
		return Message{Event: &event}, nil
	}
	return Message{}, v.err
}

func (v *validator) protocolError(value any) {
	m := v.object(value, "code message retryable", "")
	v.enum(m["code"], "INVALID_MESSAGE MESSAGE_TOO_LARGE INCOMPATIBLE_PROTOCOL UNKNOWN_OPERATION INVALID_SELECTION SESSION_ENDED ANALYSIS_EXPIRED INVALID_CURSOR BUSY REQUEST_CONFLICT UNKNOWN_JOB DEPENDENCY_UNAVAILABLE MP3_UNAVAILABLE NO_AUDIO ACCESS_RESTRICTED NETWORK_ERROR NETWORK_POLICY_BLOCKED DESTINATION_ERROR DISK_FULL ENGINE_ERROR CANCEL_FAILED INVALID_OUTPUT INTERNAL_ERROR")
	v.text(m["message"], 4096)
	v.boolean(m["retryable"])
}
func (v *validator) videoChoice(value any) string {
	m := v.object(value, "optionId container videoCodec hasAudio sizeEstimated", "width height audioCodec sizeBytes audioUnknown")
	id := v.id(m["optionId"])
	v.enum(m["container"], "mp4 webm mkv")
	v.text(m["videoCodec"], 512)
	v.optionalText(m, "audioCodec", 512)
	v.boolean(m["hasAudio"])
	if has(m, "audioUnknown") {
		v.boolean(m["audioUnknown"])
	}
	v.boolean(m["sizeEstimated"])
	if m["hasAudio"] == false && has(m, "audioCodec") {
		v.fail(InvalidMessage, "Audio codec requires source audio")
	}
	if m["sizeEstimated"] == true && !has(m, "sizeBytes") {
		v.fail(InvalidMessage, "Estimated size requires bytes")
	}
	for _, field := range []string{"width", "height"} {
		if has(m, field) {
			v.integer(m[field], 1)
		}
	}
	if has(m, "sizeBytes") {
		v.integer(m["sizeBytes"], 0)
	}
	return id
}
func (v *validator) videoChoices(value any) {
	seen := map[string]bool{}
	for _, choice := range v.array(value, 0) {
		id := v.videoChoice(choice)
		if seen[id] {
			v.fail(InvalidMessage, "Duplicate video option")
		}
		seen[id] = true
	}
}
func (v *validator) mediaItem(value any) string {
	m := v.object(value, "itemId title hasAudio videoChoices omittedMetadata", "durationSeconds formatsCursor audioUnknown")
	id := v.id(m["itemId"])
	v.text(m["title"], 4096)
	v.boolean(m["hasAudio"])
	if has(m, "audioUnknown") {
		v.boolean(m["audioUnknown"])
	}
	v.videoChoices(m["videoChoices"])
	v.boolean(m["omittedMetadata"])
	v.optionalID(m, "formatsCursor")
	if has(m, "durationSeconds") {
		v.positive(m["durationSeconds"])
	}
	return id
}
func (v *validator) analysis(value any, session string) {
	m := v.object(value, "analysisId hostSessionId sourceUrl expiresAt items omittedMetadata", "")
	v.id(m["analysisId"])
	if v.id(m["hostSessionId"]) != session {
		v.fail(InvalidMessage, "Analysis belongs to another session")
	}
	v.sourceURL(m["sourceUrl"])
	v.timestamp(m["expiresAt"])
	v.boolean(m["omittedMetadata"])
	seen := map[string]bool{}
	for _, item := range v.array(m["items"], 1) {
		id := v.mediaItem(item)
		if seen[id] {
			v.fail(InvalidMessage, "Duplicate media item")
		}
		seen[id] = true
	}
}
func (v *validator) destination(value any, canceled bool) {
	required := "destinationId label"
	if canceled {
		required += " canceled"
	}
	m := v.object(value, required, "")
	v.id(m["destinationId"])
	v.text(m["label"], 4096)
	if canceled {
		v.boolean(m["canceled"])
	}
}
func (v *validator) result(operation Operation, value any, session, requestID string) {
	switch operation {
	case OperationHandshake:
		m := v.object(value, "hostVersion engineVersion dependencies capabilities defaultDestination", "")
		v.text(m["hostVersion"], 512)
		v.text(m["engineVersion"], 512)
		seenDependencies := map[string]bool{}
		for _, entry := range v.array(m["dependencies"], 0) {
			d := v.object(entry, "name version available", "")
			name := v.enum(d["name"], "yt-dlp deno ffmpeg ffprobe")
			if seenDependencies[name] {
				v.fail(InvalidMessage, "Duplicate dependency")
			}
			seenDependencies[name] = true
			v.text(d["version"], 512)
			v.boolean(d["available"])
		}
		c := v.object(m["capabilities"], "video mp3 networkAccess", "")
		v.boolean(c["video"])
		v.boolean(c["mp3"])
		v.boolean(c["networkAccess"])
		v.destination(m["defaultDestination"], false)
	case OperationAnalyze:
		v.analysis(value, session)
	case OperationListFormats:
		m := v.object(value, "analysisId itemId choices omittedMetadata", "nextCursor")
		v.id(m["analysisId"])
		v.id(m["itemId"])
		v.videoChoices(m["choices"])
		v.optionalID(m, "nextCursor")
		v.boolean(m["omittedMetadata"])
	case OperationChooseDestination:
		v.destination(value, true)
	case OperationStart:
		m := v.object(value, "jobId startRequestId sequence state", "")
		v.id(m["jobId"])
		if v.id(m["startRequestId"]) != requestID {
			v.fail(InvalidMessage, "Start result belongs to another request")
		}
		v.integer(m["sequence"], 0)
		v.state(m["state"])
	case OperationStatus:
		m := v.object(value, "snapshot", "")
		if m["snapshot"] != nil {
			v.snapshot(m["snapshot"], session)
		}
	case OperationCancel:
		m := v.object(value, "snapshot", "")
		v.snapshot(m["snapshot"], session)
	case OperationReconcile:
		m := v.object(value, "previousHostSessionId outcome snapshot", "")
		previous := v.id(m["previousHostSessionId"])
		if previous == session {
			v.fail(InvalidMessage, "Reconcile result requires a previous session")
		}
		outcome := v.enum(m["outcome"], "confirmed unconfirmed notFound")
		if m["snapshot"] == nil {
			if outcome == "confirmed" {
				v.fail(InvalidMessage, "Confirmed reconciliation needs a terminal snapshot")
			}
			return
		}
		state := v.snapshot(m["snapshot"], previous)
		if outcome == "notFound" || (outcome == "unconfirmed" && state != "interrupted") || (outcome == "confirmed" && !terminal(state)) {
			v.fail(InvalidMessage, "Invalid reconciliation snapshot")
		}
	}
}
func (v *validator) state(value any) string {
	return v.enum(value, "analyzing ready downloading muxing converting saving completed canceling canceled failed interrupted")
}
func terminal(state string) bool {
	return state == "completed" || state == "canceled" || state == "failed" || state == "interrupted"
}
func (v *validator) snapshot(value any, session string) string {
	m := v.object(value, "hostSessionId jobId startRequestId sequence state", "progress completedFile error")
	if v.id(m["hostSessionId"]) != session {
		v.fail(InvalidMessage, "Snapshot belongs to another session")
	}
	v.id(m["jobId"])
	v.id(m["startRequestId"])
	v.integer(m["sequence"], 0)
	return v.stateBody(m)
}
func (v *validator) stateBody(m map[string]any) string {
	state := v.state(m["state"])
	if has(m, "progress") {
		stage := v.progress(m["progress"])
		if stage != state {
			v.fail(InvalidMessage, "Progress stage differs from job state")
		}
	}
	if has(m, "completedFile") {
		v.completedFile(m["completedFile"])
		if state != "completed" {
			v.fail(InvalidMessage, "Completed file requires completed state")
		}
	} else if state == "completed" {
		v.fail(InvalidMessage, "Completed state requires a verified file")
	}
	if has(m, "error") {
		v.protocolError(m["error"])
		if state != "failed" && state != "interrupted" {
			v.fail(InvalidMessage, "Error requires failed or interrupted state")
		}
	} else if state == "failed" {
		v.fail(InvalidMessage, "Failed state requires a typed error")
	}
	return state
}
func (v *validator) progress(value any) string {
	m := v.object(value, "stage", "completedBytes totalBytes processedSeconds durationSeconds percent")
	stage := v.enum(m["stage"], "downloading muxing converting saving")
	if has(m, "completedBytes") {
		v.integer(m["completedBytes"], 0)
	}
	if has(m, "totalBytes") {
		v.integer(m["totalBytes"], 1)
		if !has(m, "completedBytes") {
			v.fail(InvalidMessage, "Byte total requires numerator")
		} else if v.number(m["completedBytes"], 0, math.MaxFloat64) > v.number(m["totalBytes"], 1, math.MaxFloat64) {
			v.fail(InvalidMessage, "Byte progress exceeds total")
		}
	}
	if has(m, "processedSeconds") {
		v.number(m["processedSeconds"], 0, math.MaxFloat64)
	}
	if has(m, "durationSeconds") {
		v.positive(m["durationSeconds"])
		if !has(m, "processedSeconds") {
			v.fail(InvalidMessage, "Duration requires numerator")
		} else if v.number(m["processedSeconds"], 0, math.MaxFloat64) > v.positive(m["durationSeconds"]) {
			v.fail(InvalidMessage, "Duration progress exceeds total")
		}
	}
	if has(m, "percent") {
		v.number(m["percent"], 0, 100)
		if !(has(m, "completedBytes") && has(m, "totalBytes")) && !(has(m, "processedSeconds") && has(m, "durationSeconds")) {
			v.fail(InvalidMessage, "Percent requires known total")
		}
	}
	return stage
}
func (v *validator) completedFile(value any) {
	m := v.object(value, "path format sizeBytes sha256", "videoCodec audioCodec mp3BitrateKbps")
	v.text(m["path"], 4096)
	format := v.enum(m["format"], "mp4 webm mkv mp3")
	v.integer(m["sizeBytes"], 1)
	hash, ok := m["sha256"].(string)
	if !ok || !hashPattern.MatchString(hash) {
		v.fail(InvalidMessage, "Invalid SHA-256")
	}
	v.optionalText(m, "videoCodec", 512)
	v.optionalText(m, "audioCodec", 512)
	if format == "mp3" {
		if has(m, "videoCodec") {
			v.fail(InvalidMessage, "MP3 output forbids video codec")
		}
		if m["audioCodec"] != "mp3" {
			v.fail(InvalidMessage, "MP3 output requires mp3 audio codec")
		}
		v.bitrate(m["mp3BitrateKbps"])
	} else if has(m, "mp3BitrateKbps") {
		v.fail(InvalidMessage, "MP3 bitrate requires MP3 output")
	}
}

func ValidateRequestContext(request Request, ctx ValidationContext) error {
	if request.Operation == OperationHandshake {
		return nil
	}
	if request.HostSessionID != ctx.HostSessionID {
		return &ProtocolDecodeError{SessionEnded, "Request belongs to another host session"}
	}
	if request.Operation == OperationReconcile {
		payload, err := PayloadAs[ReconcilePayload](request)
		if err != nil {
			return &ProtocolDecodeError{InvalidMessage, "Malformed reconciliation payload"}
		}
		if payload.PreviousHostSessionID == ctx.HostSessionID {
			return &ProtocolDecodeError{SessionEnded, "Reconcile requires a previous host session"}
		}
		return nil
	}
	if request.Operation != OperationStart && request.Operation != OperationListFormats {
		return nil
	}
	payload, err := PayloadAs[StartPayload](request)
	if err != nil {
		return &ProtocolDecodeError{InvalidMessage, "Malformed start payload"}
	}
	analysis := ctx.Analysis
	if analysis == nil || payload.AnalysisID != analysis.AnalysisID {
		return &ProtocolDecodeError{InvalidSelection, "Analysis is not registered"}
	}
	if analysis.HostSessionID != ctx.HostSessionID {
		return &ProtocolDecodeError{SessionEnded, "Analysis belongs to another host session"}
	}
	v := &validator{}
	expires := v.timestamp(analysis.ExpiresAt)
	now := time.Now().UTC()
	if ctx.Now != "" {
		now = v.timestamp(ctx.Now)
	}
	if v.err != nil {
		return v.err
	}
	if !now.Before(expires) {
		return &ProtocolDecodeError{AnalysisExpired, "Analysis has expired"}
	}
	for _, item := range analysis.Items {
		if item.ItemID != payload.ItemID {
			continue
		}
		if request.Operation == OperationListFormats {
			return nil
		}
		if payload.Mode == ModeMP3 {
			if !item.HasAudio && !item.AudioUnknown {
				return &ProtocolDecodeError{NoAudio, "Item has no source audio"}
			}
			return nil
		}
		for _, choice := range item.VideoChoices {
			if choice.OptionID == payload.VideoOptionID {
				return nil
			}
		}
		return &ProtocolDecodeError{InvalidSelection, "Video option does not belong to the item"}
	}
	return &ProtocolDecodeError{InvalidSelection, "Item does not belong to the analysis"}
}
