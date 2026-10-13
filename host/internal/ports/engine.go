// Package ports defines host-owned execution boundaries. These types are not
// part of the browser protocol and cannot grant executable or path authority.
package ports

import (
	"context"
	"dwnl/host/internal/contracts"
)

type Engine interface {
	Analyze(context.Context, string) (contracts.Analysis, error)
	Execute(context.Context, contracts.DownloadPlan, func(contracts.Progress)) (contracts.CompletedFile, error)
}

type ExecutableRole string

const (
	YTDLP   ExecutableRole = "yt-dlp"
	Deno    ExecutableRole = "deno"
	FFmpeg  ExecutableRole = "ffmpeg"
	FFprobe ExecutableRole = "ffprobe"
)

type ExecSpec struct {
	Executable ExecutableRole
	Args       []string
	Dir        string
}
type ProcessStream string

const (
	Stdout ProcessStream = "stdout"
	Stderr ProcessStream = "stderr"
)
const MaxProcessLineBytes = 65536

type ProcessLine struct {
	Stream ProcessStream
	Text   string
}
type ProcessResult struct {
	ExitCode int
	Canceled bool
}
type ProcessRunner interface {
	Run(context.Context, ExecSpec, func(ProcessLine)) (ProcessResult, error)
}
