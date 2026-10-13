package native

import (
	"bytes"
	"dwnl/host/internal/contracts"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

type oneByteReader struct{ io.Reader }

func (r oneByteReader) Read(b []byte) (int, error) {
	if len(b) > 1 {
		b = b[:1]
	}
	return r.Reader.Read(b)
}
func TestFrameSplitAcrossReads(t *testing.T) {
	var input bytes.Buffer
	binary.Write(&input, binary.LittleEndian, uint32(14))
	input.WriteString(`{"hello":"é"}`)
	got, err := ReadFrame(oneByteReader{&input})
	if err != nil || string(got) != `{"hello":"é"}` {
		t.Fatalf("split frame = %q, %v", got, err)
	}
}
func TestFrameRejectsOversizeBeforeAllocatingBody(t *testing.T) {
	var input bytes.Buffer
	binary.Write(&input, binary.LittleEndian, uint32(contracts.MaxMessageBytes+1))
	if _, err := ReadFrame(&input); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("oversized header: %v", err)
	}
}
func TestFrameRejectsTruncatedAndInvalidUTF8(t *testing.T) {
	for _, body := range [][]byte{{2, 0, 0, 0, '{'}, {1, 0, 0, 0, 0xff}, {0, 0, 0, 0}} {
		if _, err := ReadFrame(bytes.NewReader(body)); err == nil {
			t.Fatalf("accepted bad frame %v", body)
		}
	}
}

type shortWriter struct{ bytes.Buffer }

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > 2 {
		p = p[:2]
	}
	return w.Buffer.Write(p)
}
func TestWriteFrameHandlesShortWrites(t *testing.T) {
	var w shortWriter
	if err := WriteFrame(&w, []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	body, err := ReadFrame(&w.Buffer)
	if err != nil || string(body) != `{"ok":true}` {
		t.Fatalf("roundtrip %s %v", body, err)
	}
}
