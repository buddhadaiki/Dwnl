package windows

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dwnl/host/internal/ports"
	"dwnl/host/internal/testutil"
	win "golang.org/x/sys/windows"
)

// A restored dependency must still run only from the private root. A same-name
// executable in PATH must never satisfy the temporarily missing private role.
func TestMissingPrivateExecutableNeverUsesPathFallback(t *testing.T) {
	for _, role := range []ports.ExecutableRole{ports.YTDLP, ports.Deno, ports.FFmpeg, ports.FFprobe} {
		t.Run(string(role), func(t *testing.T) {
			bin := fixtureBin(t)
			fallback := testutil.TempDir(t)
			name := string(role) + ".exe"
			privatePath, fallbackPath := filepath.Join(bin, name), filepath.Join(fallback, name)
			if err := os.Rename(privatePath, fallbackPath); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", fallback)
			runner, err := NewRunner(bin)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var output strings.Builder
			emit := func(line ports.ProcessLine) {
				if line.Stream == ports.Stdout {
					output.WriteString(line.Text)
				}
			}
			spec := ports.ExecSpec{Executable: role, Args: fixtureArgs("echo", "private executable"), Dir: bin}
			result, err := runner.Run(ctx, spec, emit)
			if err == nil || result.ExitCode != -1 || result.Canceled || output.Len() != 0 {
				t.Fatalf("missing private role used a fallback: result=%+v err=%v output=%q", result, err, output.String())
			}
			if err := os.Rename(fallbackPath, privatePath); err != nil {
				t.Fatal(err)
			}
			result, err = runner.Run(ctx, spec, emit)
			if err != nil || result.ExitCode != 0 || result.Canceled || output.String() != `["private executable"]` {
				t.Fatalf("restored private role unavailable: result=%+v err=%v output=%q", result, err, output.String())
			}
		})
	}
}

func TestDiagnosticRunnerRejectsUnsafeExistingDependencies(t *testing.T) {
	for _, kind := range []string{"root junction", "dependency junction", "dependency directory"} {
		t.Run(kind, func(t *testing.T) {
			root := testutil.TempDir(t)
			bin, outside := filepath.Join(root, "bin"), filepath.Join(root, "outside")
			if err := os.Mkdir(outside, 0700); err != nil {
				t.Fatal(err)
			}
			if kind == "root junction" {
				makeDependencyJunction(t, bin, outside)
			} else {
				if err := os.Mkdir(bin, 0700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(bin, "ffprobe.exe")
				if kind == "dependency junction" {
					makeDependencyJunction(t, path, outside)
				} else if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := NewRunner(bin); err == nil {
				t.Fatalf("unsafe %s accepted for diagnostic startup", kind)
			}
		})
	}
}

// A real mount-point reparse fixture uses only owned temporary directories and
// requires neither a shell nor administrator privileges.
func makeDependencyJunction(t *testing.T, path, target string) {
	t.Helper()
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	p, _ := win.UTF16PtrFromString(path)
	h, err := win.CreateFile(p, win.GENERIC_WRITE, 0, nil, win.OPEN_EXISTING, win.FILE_FLAG_BACKUP_SEMANTICS|win.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer win.CloseHandle(h)
	substitute, _ := win.UTF16FromString(`\??\` + target)
	printName, _ := win.UTF16FromString(target)
	data := make([]byte, 16+len(substitute)*2+len(printName)*2)
	binary.LittleEndian.PutUint32(data, win.IO_REPARSE_TAG_MOUNT_POINT)
	binary.LittleEndian.PutUint16(data[4:], uint16(len(data)-8))
	binary.LittleEndian.PutUint16(data[10:], uint16((len(substitute)-1)*2))
	binary.LittleEndian.PutUint16(data[12:], uint16(len(substitute)*2))
	binary.LittleEndian.PutUint16(data[14:], uint16((len(printName)-1)*2))
	for i, value := range append(substitute, printName...) {
		binary.LittleEndian.PutUint16(data[16+i*2:], value)
	}
	var returned uint32
	err = testutil.RetryDirectoryNotEmpty(func() error {
		return win.DeviceIoControl(h, win.FSCTL_SET_REPARSE_POINT, &data[0], uint32(len(data)), nil, 0, &returned, nil)
	})
	if err != nil {
		if errors.Is(err, win.ERROR_ACCESS_DENIED) {
			t.Fatal("real reparse fixture requires normal-user execution: ", err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(path); err != nil {
			t.Error(err)
		}
	})
}
