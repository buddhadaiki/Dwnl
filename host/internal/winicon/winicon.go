// Package winicon turns the Dwnl logo into a Windows icon resource that the Go
// linker embeds from a .syso file, so Explorer, the taskbar and the window
// show the logo instead of the generic program icon.
package winicon

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/draw"
	"image/png"
	"io"
	"sort"
)

// Sizes Windows asks for across shell views and display scales. 256 is stored
// as PNG, as Explorer expects for large icons; smaller sizes are plain DIBs.
var Sizes = []int{16, 20, 24, 32, 40, 48, 64, 256}

// GroupID is the icon group Wails v2 loads for its window (winc.AppIconID).
// Explorer uses the first group in the file, and this is the only one.
const GroupID = 3

const (
	rtIcon      = 3
	rtGroupIcon = 14
	langEnUS    = 0x0409
)

// Syso renders the square master image at every size and wraps the frames in
// an AMD64 COFF object holding RT_ICON and RT_GROUP_ICON resources.
func Syso(master io.Reader) ([]byte, error) {
	decoded, err := png.Decode(master)
	if err != nil {
		return nil, err
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != bounds.Dy() || bounds.Dx() < Sizes[len(Sizes)-1] {
		return nil, errors.New("the icon master must be a square PNG of at least 256 pixels")
	}
	source := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(source, source.Bounds(), decoded, bounds.Min, draw.Src)
	frames := make([][]byte, len(Sizes))
	for i, size := range Sizes {
		scaled := Resize(source, size)
		if size >= 256 {
			var buffer bytes.Buffer
			if err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buffer, scaled); err != nil {
				return nil, err
			}
			frames[i] = buffer.Bytes()
		} else {
			frames[i] = dib(scaled)
		}
	}
	return coff(frames), nil
}

// Resize averages every source pixel a target pixel covers, weighting partial
// coverage and alpha, so thin edges stay smooth at 16 pixels.
func Resize(source *image.NRGBA, size int) *image.NRGBA {
	weights := axisWeights(source.Bounds().Dx(), size)
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a, area float64
			for _, wy := range weights[y] {
				for _, wx := range weights[x] {
					w := wx.weight * wy.weight
					p := source.PixOffset(wx.index, wy.index)
					alpha := float64(source.Pix[p+3]) * w
					r += float64(source.Pix[p]) * alpha
					g += float64(source.Pix[p+1]) * alpha
					b += float64(source.Pix[p+2]) * alpha
					a += alpha
					area += w
				}
			}
			p := out.PixOffset(x, y)
			if a > 0 {
				out.Pix[p], out.Pix[p+1], out.Pix[p+2] = round(r/a), round(g/a), round(b/a)
				out.Pix[p+3] = round(a / area)
			}
		}
	}
	return out
}

type weight struct {
	index  int
	weight float64
}

func axisWeights(from, to int) [][]weight {
	scale := float64(from) / float64(to)
	axis := make([][]weight, to)
	for i := range axis {
		start, end := float64(i)*scale, float64(i+1)*scale
		for j := int(start); j < from && float64(j) < end; j++ {
			covered := min(end, float64(j+1)) - max(start, float64(j))
			if covered > 0 {
				axis[i] = append(axis[i], weight{j, covered})
			}
		}
	}
	return axis
}

func round(v float64) uint8 {
	if v >= 255 {
		return 255
	}
	if v <= 0 {
		return 0
	}
	return uint8(v + .5)
}

// dib stores a frame the way .ico files do: a BITMAPINFOHEADER with doubled
// height, bottom-up BGRA pixels, then a 1-bit mask marking clear pixels.
func dib(img *image.NRGBA) []byte {
	size := img.Bounds().Dx()
	maskRow := (size + 31) / 32 * 4
	out := make([]byte, 40, 40+size*size*4+maskRow*size)
	binary.LittleEndian.PutUint32(out[0:], 40)
	binary.LittleEndian.PutUint32(out[4:], uint32(size))
	binary.LittleEndian.PutUint32(out[8:], uint32(size*2))
	binary.LittleEndian.PutUint16(out[12:], 1)
	binary.LittleEndian.PutUint16(out[14:], 32)
	binary.LittleEndian.PutUint32(out[20:], uint32(size*size*4+maskRow*size))
	for y := size - 1; y >= 0; y-- {
		for x := 0; x < size; x++ {
			p := img.PixOffset(x, y)
			out = append(out, img.Pix[p+2], img.Pix[p+1], img.Pix[p], img.Pix[p+3])
		}
	}
	for y := size - 1; y >= 0; y-- {
		row := make([]byte, maskRow)
		for x := 0; x < size; x++ {
			if img.Pix[img.PixOffset(x, y)+3] == 0 {
				row[x/8] |= 0x80 >> (x % 8)
			}
		}
		out = append(out, row...)
	}
	return out
}

// group is the GRPICONDIR that names every frame by its RT_ICON ID.
func group(frames [][]byte) []byte {
	out := make([]byte, 6, 6+14*len(frames))
	binary.LittleEndian.PutUint16(out[2:], 1)
	binary.LittleEndian.PutUint16(out[4:], uint16(len(frames)))
	for i, frame := range frames {
		entry := make([]byte, 14)
		if Sizes[i] < 256 {
			entry[0], entry[1] = byte(Sizes[i]), byte(Sizes[i])
		}
		binary.LittleEndian.PutUint16(entry[4:], 1)
		binary.LittleEndian.PutUint16(entry[6:], 32)
		binary.LittleEndian.PutUint32(entry[8:], uint32(len(frame)))
		binary.LittleEndian.PutUint16(entry[12:], uint16(i+1))
		out = append(out, entry...)
	}
	return out
}

type resource struct {
	kind, id uint16
	data     []byte
}

// coff lays out a .rsrc section (directory tree, data entries, then data) in
// an object file. Each data entry holds a section offset that the linker turns
// into an image-relative address through an ADDR32NB relocation.
func coff(frames [][]byte) []byte {
	var resources []resource
	for i, frame := range frames {
		resources = append(resources, resource{rtIcon, uint16(i + 1), frame})
	}
	resources = append(resources, resource{rtGroupIcon, GroupID, group(frames)})
	sort.SliceStable(resources, func(i, j int) bool {
		if resources[i].kind != resources[j].kind {
			return resources[i].kind < resources[j].kind
		}
		return resources[i].id < resources[j].id
	})
	var kinds []uint16
	byKind := map[uint16][]int{}
	for i, r := range resources {
		if len(byKind[r.kind]) == 0 {
			kinds = append(kinds, r.kind)
		}
		byKind[r.kind] = append(byKind[r.kind], i)
	}

	directory := func(entries int) int { return 16 + 8*entries }
	// Offsets: root, one directory per type, one per (type, id), one language
	// directory per resource, then the data entries, then the aligned data.
	offset := directory(len(kinds))
	typeDir := map[uint16]int{}
	for _, kind := range kinds {
		typeDir[kind] = offset
		offset += directory(len(byKind[kind]))
	}
	langDir := make([]int, len(resources))
	for i := range resources {
		langDir[i] = offset
		offset += directory(1)
	}
	dataEntry := make([]int, len(resources))
	for i := range resources {
		dataEntry[i] = offset
		offset += 16
	}
	dataAt := make([]int, len(resources))
	for i, r := range resources {
		offset = (offset + 7) &^ 7
		dataAt[i] = offset
		offset += len(r.data)
	}
	section := make([]byte, (offset+7)&^7)
	writeDirectory := func(at int, entries [][2]uint32) {
		binary.LittleEndian.PutUint16(section[at+14:], uint16(len(entries)))
		for i, e := range entries {
			binary.LittleEndian.PutUint32(section[at+16+8*i:], e[0])
			binary.LittleEndian.PutUint32(section[at+20+8*i:], e[1])
		}
	}
	const subdirectory = 0x80000000
	var root [][2]uint32
	for _, kind := range kinds {
		root = append(root, [2]uint32{uint32(kind), subdirectory | uint32(typeDir[kind])})
		var ids [][2]uint32
		for _, i := range byKind[kind] {
			ids = append(ids, [2]uint32{uint32(resources[i].id), subdirectory | uint32(langDir[i])})
		}
		writeDirectory(typeDir[kind], ids)
	}
	writeDirectory(0, root)
	for i, r := range resources {
		writeDirectory(langDir[i], [][2]uint32{{langEnUS, uint32(dataEntry[i])}})
		binary.LittleEndian.PutUint32(section[dataEntry[i]:], uint32(dataAt[i]))
		binary.LittleEndian.PutUint32(section[dataEntry[i]+4:], uint32(len(r.data)))
		copy(section[dataAt[i]:], r.data)
	}

	const (
		fileHeader    = 20
		sectionHeader = 40
		relocation    = 10
		symbol        = 18
	)
	sectionAt := fileHeader + sectionHeader
	relocationsAt := sectionAt + len(section)
	symbolsAt := relocationsAt + relocation*len(resources)
	out := make([]byte, symbolsAt+symbol+4)
	binary.LittleEndian.PutUint16(out[0:], 0x8664) // IMAGE_FILE_MACHINE_AMD64
	binary.LittleEndian.PutUint16(out[2:], 1)
	binary.LittleEndian.PutUint32(out[8:], uint32(symbolsAt))
	binary.LittleEndian.PutUint32(out[12:], 1)
	binary.LittleEndian.PutUint16(out[18:], 0x0004) // IMAGE_FILE_LINE_NUMS_STRIPPED
	header := out[fileHeader:]
	copy(header[0:8], ".rsrc")
	binary.LittleEndian.PutUint32(header[16:], uint32(len(section)))
	binary.LittleEndian.PutUint32(header[20:], uint32(sectionAt))
	binary.LittleEndian.PutUint32(header[24:], uint32(relocationsAt))
	binary.LittleEndian.PutUint16(header[32:], uint16(len(resources)))
	binary.LittleEndian.PutUint32(header[36:], 0x40000040) // INITIALIZED_DATA | MEM_READ
	copy(out[sectionAt:], section)
	for i := range resources {
		at := relocationsAt + relocation*i
		binary.LittleEndian.PutUint32(out[at:], uint32(dataEntry[i]))
		binary.LittleEndian.PutUint32(out[at+4:], 0)      // the .rsrc section symbol
		binary.LittleEndian.PutUint16(out[at+8:], 0x0003) // IMAGE_REL_AMD64_ADDR32NB
	}
	sym := out[symbolsAt:]
	copy(sym[0:8], ".rsrc")
	binary.LittleEndian.PutUint16(sym[12:], 1) // section number
	sym[16] = 3                                // IMAGE_SYM_CLASS_STATIC
	binary.LittleEndian.PutUint32(out[symbolsAt+symbol:], 4)
	return out
}
