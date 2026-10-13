// Package native implements bounded Chrome Native Messaging frames.
package native

import (
	"dwnl/host/internal/contracts"
	"encoding/binary"
	"errors"
	"io"
	"unicode/utf8"
)

var ErrFrameTooLarge = errors.New("native frame too large")

func ReadFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	size := binary.LittleEndian.Uint32(header[:])
	if size > contracts.MaxMessageBytes {
		return nil, ErrFrameTooLarge
	}
	if size == 0 {
		return nil, errors.New("empty native frame")
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	if !utf8.Valid(body) {
		return nil, errors.New("invalid UTF-8 native frame")
	}
	return body, nil
}
func WriteFrame(w io.Writer, b []byte) error {
	if len(b) > contracts.MaxMessageBytes {
		return ErrFrameTooLarge
	}
	if len(b) == 0 || !utf8.Valid(b) {
		return errors.New("invalid native frame")
	}
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], uint32(len(b)))
	for _, part := range [][]byte{header[:], b} {
		for len(part) > 0 {
			n, err := w.Write(part)
			if err != nil {
				return err
			}
			if n <= 0 || n > len(part) {
				return io.ErrShortWrite
			}
			part = part[n:]
		}
	}
	return nil
}
