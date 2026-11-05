package winicon

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func square(size int, fill func(x, y int) color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetNRGBA(x, y, fill(x, y))
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		panic(err)
	}
	return buffer.Bytes()
}

type entry struct{ id, offset uint32 }

func entries(section []byte, at uint32) []entry {
	count := uint32(binary.LittleEndian.Uint16(section[at+12:])) + uint32(binary.LittleEndian.Uint16(section[at+14:]))
	out := make([]entry, count)
	for i := range out {
		base := at + 16 + 8*uint32(i)
		out[i] = entry{binary.LittleEndian.Uint32(section[base:]), binary.LittleEndian.Uint32(section[base+4:])}
	}
	return out
}

// resources reads type → id → bytes, resolving each data entry through its
// relocation the way the linker does for a section placed at address 0.
func resources(t *testing.T, data []byte) map[uint32]map[uint32][]byte {
	t.Helper()
	file, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if file.Machine != pe.IMAGE_FILE_MACHINE_AMD64 || len(file.Sections) != 1 || len(file.Symbols) != 1 {
		t.Fatalf("unexpected object layout: machine %#x, %d sections, %d symbols", file.Machine, len(file.Sections), len(file.Symbols))
	}
	rsrc := file.Section(".rsrc")
	if rsrc == nil {
		t.Fatal("missing .rsrc section")
	}
	section, err := rsrc.Data()
	if err != nil {
		t.Fatal(err)
	}
	relocated := map[uint32]bool{}
	for _, r := range rsrc.Relocs {
		if r.Type != 0x0003 || r.SymbolTableIndex != 0 {
			t.Fatalf("relocation %+v is not ADDR32NB against .rsrc", r)
		}
		relocated[r.VirtualAddress] = true
	}
	out := map[uint32]map[uint32][]byte{}
	for _, kind := range entries(section, 0) {
		out[kind.id] = map[uint32][]byte{}
		for _, id := range entries(section, kind.offset&^0x80000000) {
			languages := entries(section, id.offset&^0x80000000)
			if len(languages) != 1 || languages[0].offset&0x80000000 != 0 {
				t.Fatal("expected one language with a data entry")
			}
			at := languages[0].offset
			if !relocated[at] {
				t.Fatalf("data entry at %d has no relocation", at)
			}
			start, size := binary.LittleEndian.Uint32(section[at:]), binary.LittleEndian.Uint32(section[at+4:])
			out[kind.id][id.id] = section[start : start+size]
		}
	}
	if len(relocated) != len(Sizes)+1 {
		t.Fatalf("%d relocations for %d resources", len(relocated), len(Sizes)+1)
	}
	return out
}

func TestSysoHoldsOneGroupNamingEveryFrame(t *testing.T) {
	data, err := Syso(bytes.NewReader(square(512, func(x, y int) color.NRGBA { return color.NRGBA{0x10, 0x60, 0xf0, 0xff} })))
	if err != nil {
		t.Fatal(err)
	}
	found := resources(t, data)
	groups := found[rtGroupIcon]
	if len(groups) != 1 || groups[GroupID] == nil {
		t.Fatalf("expected only icon group %d, got %v", GroupID, len(groups))
	}
	directory := groups[GroupID]
	if binary.LittleEndian.Uint16(directory[2:]) != 1 || int(binary.LittleEndian.Uint16(directory[4:])) != len(Sizes) {
		t.Fatal("group header does not list every size")
	}
	for i, size := range Sizes {
		item := directory[6+14*i:]
		id := uint32(binary.LittleEndian.Uint16(item[12:]))
		frame := found[rtIcon][id]
		if frame == nil || binary.LittleEndian.Uint32(item[8:]) != uint32(len(frame)) {
			t.Fatalf("size %d: group entry does not match its frame", size)
		}
		width := int(item[0])
		if width == 0 {
			width = 256
		}
		if width != size || binary.LittleEndian.Uint16(item[6:]) != 32 {
			t.Fatalf("size %d listed as %d pixels", size, width)
		}
		if size >= 256 {
			decoded, err := png.Decode(bytes.NewReader(frame))
			if err != nil || decoded.Bounds().Dx() != 256 {
				t.Fatalf("256 frame is not a 256 pixel PNG: %v", err)
			}
			continue
		}
		if binary.LittleEndian.Uint32(frame[0:]) != 40 || int(binary.LittleEndian.Uint32(frame[4:])) != size || int(binary.LittleEndian.Uint32(frame[8:])) != size*2 {
			t.Fatalf("size %d frame is not a doubled-height DIB", size)
		}
		// The top-left pixel is the last row of the bottom-up bitmap: opaque blue.
		top := frame[40+(size-1)*size*4:]
		if top[0] != 0xf0 || top[1] != 0x60 || top[2] != 0x10 || top[3] != 0xff {
			t.Fatalf("size %d pixel stored as %v, want BGRA of the logo color", size, top[:4])
		}
	}
}

func TestResizeKeepsTransparencyAndColor(t *testing.T) {
	// Left half opaque red, right half clear: the middle column blends alpha
	// without darkening the red.
	source := image.NewNRGBA(image.Rect(0, 0, 300, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 150; x++ {
			source.SetNRGBA(x, y, color.NRGBA{0xff, 0, 0, 0xff})
		}
	}
	out := Resize(source, 3)
	left, middle, right := out.NRGBAAt(0, 1), out.NRGBAAt(1, 1), out.NRGBAAt(2, 1)
	if left != (color.NRGBA{0xff, 0, 0, 0xff}) || right.A != 0 {
		t.Fatalf("edges: %v %v", left, right)
	}
	if middle.R != 0xff || middle.A < 120 || middle.A > 135 {
		t.Fatalf("half-covered pixel: %v", middle)
	}
}

func TestSysoRejectsNonSquareMasters(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 300, 200))
	var buffer bytes.Buffer
	_ = png.Encode(&buffer, img)
	if _, err := Syso(&buffer); err == nil {
		t.Fatal("accepted a non-square master")
	}
}
