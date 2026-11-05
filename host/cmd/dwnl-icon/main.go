// Command dwnl-icon writes the Windows icon resource linked into Dwnl.exe.
// Run "go generate ./cmd/dwnl-app" after the logo changes.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"dwnl/host/internal/winicon"
)

func main() {
	master := flag.String("master", "", "square PNG logo")
	output := flag.String("o", "", "output .syso file")
	flag.Parse()
	if *master == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "usage: dwnl-icon -master logo.png -o rsrc_windows_amd64.syso")
		os.Exit(2)
	}
	logo, err := os.ReadFile(*master)
	if err == nil {
		var data []byte
		if data, err = winicon.Syso(bytes.NewReader(logo)); err == nil {
			err = os.WriteFile(*output, data, 0o644)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "dwnl-icon:", err)
		os.Exit(1)
	}
}
