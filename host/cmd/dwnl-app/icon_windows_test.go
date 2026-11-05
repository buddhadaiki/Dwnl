package main

import (
	"bytes"
	"os"
	"testing"

	"dwnl/host/internal/winicon"
	win "golang.org/x/sys/windows"
)

// The checked-in resource must be exactly what the generator makes from the
// logo, so a logo change cannot ship with a stale program icon.
func TestIconResourceMatchesTheLogo(t *testing.T) {
	logo, err := os.Open("../../../docs/branding/dwnl-logo.png")
	if err != nil {
		t.Fatal(err)
	}
	defer logo.Close()
	want, err := winicon.Syso(logo)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("rsrc_windows_amd64.syso")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("rsrc_windows_amd64.syso is missing or stale (%v); run go generate ./cmd/dwnl-app", err)
	}
}

// The test binary links the same .syso as Dwnl.exe. Windows must load the
// icon by the ID Wails asks for, at taskbar and Explorer sizes.
func TestWindowsLoadsTheProgramIcon(t *testing.T) {
	var module win.Handle
	if err := win.GetModuleHandleEx(0, nil, &module); err != nil {
		t.Fatal(err)
	}
	user32 := win.NewLazySystemDLL("user32.dll")
	loadImage, destroyIcon := user32.NewProc("LoadImageW"), user32.NewProc("DestroyIcon")
	const imageIcon = 1
	for _, size := range []uintptr{16, 24, 32, 48, 256} {
		icon, _, err := loadImage.Call(uintptr(module), winicon.GroupID, imageIcon, size, size, 0)
		if icon == 0 {
			t.Fatalf("Windows could not load the %d pixel program icon: %v", size, err)
		}
		destroyIcon.Call(icon)
	}
}
