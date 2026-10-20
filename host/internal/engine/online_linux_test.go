package engine

import (
	"dwnl/host/internal/ports"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxOnlineUsesPrivateDenoAndCleanEnvironment(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(t.TempDir(), "bin")
	args := append(onlineArgs(dir, bin), "--list-extractors")
	if !validateOnlineSpec(ports.ExecSpec{Executable: ports.YTDLP, Args: args, Dir: dir}) {
		t.Fatal("Linux invocation rejected")
	}
	if args[len(onlineStaticArgs)] != "deno:"+filepath.Join(bin, "deno") {
		t.Fatal("did not select private Linux deno")
	}
	t.Setenv("HTTP_PROXY", "http://private.invalid")
	t.Setenv("PYTHONPATH", "/untrusted")
	t.Setenv("LD_PRELOAD", "/untrusted.so")
	for _, value := range onlineEnvironment(dir, bin) {
		if strings.HasPrefix(value, "HTTP_PROXY=") || strings.HasPrefix(value, "PYTHONPATH=") || strings.HasPrefix(value, "LD_PRELOAD=") {
			t.Fatalf("inherited unsafe environment %q", value)
		}
	}
}
