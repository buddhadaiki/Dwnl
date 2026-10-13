// Package contracts defines the version 1 control protocol. Internal plans are
// host-owned and are never decoded as browser payloads.
package contracts

import "encoding/json"

const ProtocolVersion = 1
const MaxMessageBytes = 524288
const MaxSafeInteger uint64 = 9007199254740991
const HostName = "com.dwnl.host"

type Operation string

const (
	OperationHandshake         Operation = "handshake"
	OperationAnalyze           Operation = "analyze"
	OperationListFormats       Operation = "listFormats"
	OperationChooseDestination Operation = "chooseDestination"
	OperationStart             Operation = "start"
	OperationStatus            Operation = "status"
	OperationReconcile         Operation = "reconcile"
	OperationCancel            Operation = "cancel"
)

type JobState string

const (
	StateAnalyzing   JobState = "analyzing"
	StateReady       JobState = "ready"
	StateDownloading JobState = "downloading"
	StateMuxing      JobState = "muxing"
	StateConverting  JobState = "converting"
	StateSaving      JobState = "saving"
	StateCompleted   JobState = "completed"
	StateCanceling   JobState = "canceling"
	StateCanceled    JobState = "canceled"
	StateFailed      JobState = "failed"
	StateInterrupted JobState = "interrupted"
)

type DownloadMode string

const (
	ModeVideo DownloadMode = "video"
	ModeMP3   DownloadMode = "mp3"
)

type ErrorCode string

const (
	InvalidMessage        ErrorCode = "INVALID_MESSAGE"
	MessageTooLarge       ErrorCode = "MESSAGE_TOO_LARGE"
	IncompatibleProtocol  ErrorCode = "INCOMPATIBLE_PROTOCOL"
	UnknownOperation      ErrorCode = "UNKNOWN_OPERATION"
	InvalidSelection      ErrorCode = "INVALID_SELECTION"
	SessionEnded          ErrorCode = "SESSION_ENDED"
	AnalysisExpired       ErrorCode = "ANALYSIS_EXPIRED"
	InvalidCursor         ErrorCode = "INVALID_CURSOR"
	Busy                  ErrorCode = "BUSY"
	RequestConflict       ErrorCode = "REQUEST_CONFLICT"
	UnknownJob            ErrorCode = "UNKNOWN_JOB"
	DependencyUnavailable ErrorCode = "DEPENDENCY_UNAVAILABLE"
	MP3Unavailable        ErrorCode = "MP3_UNAVAILABLE"
	NoAudio               ErrorCode = "NO_AUDIO"
	AccessRestricted      ErrorCode = "ACCESS_RESTRICTED"
	NetworkError          ErrorCode = "NETWORK_ERROR"
	NetworkPolicyBlocked  ErrorCode = "NETWORK_POLICY_BLOCKED"
	DestinationError      ErrorCode = "DESTINATION_ERROR"
	DiskFull              ErrorCode = "DISK_FULL"
	EngineError           ErrorCode = "ENGINE_ERROR"
	CancelFailed          ErrorCode = "CANCEL_FAILED"
	InvalidOutput         ErrorCode = "INVALID_OUTPUT"
	InternalError         ErrorCode = "INTERNAL_ERROR"
)

type ProtocolDecodeError struct {
	Code    ErrorCode
	Message string
}

func (e *ProtocolDecodeError) Error() string { return string(e.Code) + ": " + e.Message }

type ProtocolError struct {
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message"`
	Retryable bool      `json:"retryable"`
}

func (e *ProtocolError) Error() string { return string(e.Code) + ": " + e.Message }

type Dependency struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Available bool   `json:"available"`
}
type Capabilities struct {
	Video         bool `json:"video"`
	MP3           bool `json:"mp3"`
	NetworkAccess bool `json:"networkAccess"`
}
type Destination struct {
	DestinationID string `json:"destinationId"`
	Label         string `json:"label"`
}
type VideoChoice struct {
	OptionID      string  `json:"optionId"`
	Container     string  `json:"container"`
	Width         *uint64 `json:"width,omitempty"`
	Height        *uint64 `json:"height,omitempty"`
	VideoCodec    string  `json:"videoCodec"`
	AudioCodec    string  `json:"audioCodec,omitempty"`
	HasAudio      bool    `json:"hasAudio"`
	SizeBytes     *uint64 `json:"sizeBytes,omitempty"`
	SizeEstimated bool    `json:"sizeEstimated"`
}
type MediaItem struct {
	ItemID          string        `json:"itemId"`
	Title           string        `json:"title"`
	DurationSeconds *float64      `json:"durationSeconds,omitempty"`
	HasAudio        bool          `json:"hasAudio"`
	VideoChoices    []VideoChoice `json:"videoChoices"`
	FormatsCursor   string        `json:"formatsCursor,omitempty"`
	OmittedMetadata bool          `json:"omittedMetadata"`
}
type Analysis struct {
	AnalysisID      string      `json:"analysisId"`
	HostSessionID   string      `json:"hostSessionId"`
	SourceURL       string      `json:"sourceUrl"`
	ExpiresAt       string      `json:"expiresAt"`
	Items           []MediaItem `json:"items"`
	OmittedMetadata bool        `json:"omittedMetadata"`
}
type Progress struct {
	Stage            JobState `json:"stage"`
	CompletedBytes   *uint64  `json:"completedBytes,omitempty"`
	TotalBytes       *uint64  `json:"totalBytes,omitempty"`
	ProcessedSeconds *float64 `json:"processedSeconds,omitempty"`
	DurationSeconds  *float64 `json:"durationSeconds,omitempty"`
	Percent          *float64 `json:"percent,omitempty"`
}
type CompletedFile struct {
	Path           string `json:"path"`
	Format         string `json:"format"`
	SizeBytes      uint64 `json:"sizeBytes"`
	SHA256         string `json:"sha256"`
	VideoCodec     string `json:"videoCodec,omitempty"`
	AudioCodec     string `json:"audioCodec,omitempty"`
	MP3BitrateKbps int    `json:"mp3BitrateKbps,omitempty"`
}
type JobSnapshot struct {
	HostSessionID  string         `json:"hostSessionId"`
	JobID          string         `json:"jobId"`
	StartRequestID string         `json:"startRequestId"`
	Sequence       uint64         `json:"sequence"`
	State          JobState       `json:"state"`
	Progress       *Progress      `json:"progress,omitempty"`
	CompletedFile  *CompletedFile `json:"completedFile,omitempty"`
	Error          *ProtocolError `json:"error,omitempty"`
}
type HandshakePayload struct {
	ExtensionVersion string `json:"extensionVersion"`
}
type AnalyzePayload struct {
	URL string `json:"url"`
}
type ListFormatsPayload struct {
	AnalysisID string `json:"analysisId"`
	ItemID     string `json:"itemId"`
	Cursor     string `json:"cursor,omitempty"`
}
type ChooseDestinationPayload struct {
	UserGesture bool `json:"userGesture"`
}
type StartPayload struct {
	AnalysisID    string       `json:"analysisId"`
	ItemID        string       `json:"itemId"`
	Mode          DownloadMode `json:"mode"`
	VideoOptionID string       `json:"videoOptionId,omitempty"`
	BitrateKbps   int          `json:"bitrateKbps,omitempty"`
	DestinationID string       `json:"destinationId"`
}
type StatusPayload struct {
	JobID          string `json:"jobId,omitempty"`
	StartRequestID string `json:"startRequestId,omitempty"`
}
type ReconcilePayload struct {
	PreviousHostSessionID string `json:"previousHostSessionId"`
	JobID                 string `json:"jobId,omitempty"`
	StartRequestID        string `json:"startRequestId,omitempty"`
}
type CancelPayload struct {
	JobID string `json:"jobId"`
}
type HandshakeResult struct {
	HostVersion        string       `json:"hostVersion"`
	EngineVersion      string       `json:"engineVersion"`
	Dependencies       []Dependency `json:"dependencies"`
	Capabilities       Capabilities `json:"capabilities"`
	DefaultDestination Destination  `json:"defaultDestination"`
}
type ListFormatsResult struct {
	AnalysisID      string        `json:"analysisId"`
	ItemID          string        `json:"itemId"`
	Choices         []VideoChoice `json:"choices"`
	NextCursor      string        `json:"nextCursor,omitempty"`
	OmittedMetadata bool          `json:"omittedMetadata"`
}
type ChooseDestinationResult struct {
	DestinationID string `json:"destinationId"`
	Label         string `json:"label"`
	Canceled      bool   `json:"canceled"`
}
type StartResult struct {
	JobID          string   `json:"jobId"`
	StartRequestID string   `json:"startRequestId"`
	Sequence       uint64   `json:"sequence"`
	State          JobState `json:"state"`
}
type StatusResult struct {
	Snapshot *JobSnapshot `json:"snapshot"`
}
type ReconcileResult struct {
	PreviousHostSessionID string       `json:"previousHostSessionId"`
	Outcome               string       `json:"outcome"`
	Snapshot              *JobSnapshot `json:"snapshot"`
}
type CancelResult struct {
	Snapshot JobSnapshot `json:"snapshot"`
}
type Request struct {
	ProtocolVersion int             `json:"protocolVersion"`
	RequestID       string          `json:"requestId"`
	HostSessionID   string          `json:"hostSessionId,omitempty"`
	Operation       Operation       `json:"operation"`
	Payload         json.RawMessage `json:"payload"`
}
type Response struct {
	ProtocolVersion int             `json:"protocolVersion"`
	Kind            string          `json:"kind"`
	RequestID       string          `json:"requestId"`
	HostSessionID   string          `json:"hostSessionId"`
	Operation       Operation       `json:"operation"`
	Result          json.RawMessage `json:"result,omitempty"`
	Error           *ProtocolError  `json:"error,omitempty"`
}
type JobEvent struct {
	ProtocolVersion int            `json:"protocolVersion"`
	Kind            string         `json:"kind"`
	HostSessionID   string         `json:"hostSessionId"`
	JobID           string         `json:"jobId"`
	Sequence        uint64         `json:"sequence"`
	State           JobState       `json:"state"`
	Progress        *Progress      `json:"progress,omitempty"`
	CompletedFile   *CompletedFile `json:"completedFile,omitempty"`
	Error           *ProtocolError `json:"error,omitempty"`
}

// Message contains exactly one branch after successful DecodeMessage.
type Message struct {
	Response *Response
	Event    *JobEvent
}
type ValidationContext struct {
	HostSessionID string    `json:"hostSessionId"`
	Analysis      *Analysis `json:"analysis,omitempty"`
	Now           string    `json:"now,omitempty"`
}

// PayloadAs returns an operation's exported payload type after DecodeRequest.
func PayloadAs[T any](request Request) (T, error) {
	var payload T
	err := json.Unmarshal(request.Payload, &payload)
	return payload, err
}

// ResultAs returns an operation's exported result type after DecodeMessage.
func ResultAs[T any](response Response) (T, error) {
	var result T
	err := json.Unmarshal(response.Result, &result)
	return result, err
}

// DownloadPlan resolves selection and host destinations; this is never wire input.
type DownloadPlan struct {
	RequestID       string
	HostSessionID   string
	JobID           string
	Analysis        Analysis
	Item            MediaItem
	Mode            DownloadMode
	VideoChoice     *VideoChoice
	MP3BitrateKbps  int
	DestinationID   string
	DestinationPath string
	WorkDir         string
}
