package windows

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
)

// ErrFileInUse means another program holds the file open for writing.
var ErrFileInUse = errors.New("file is open for writing elsewhere")

var errContentChanged = errors.New("file content differs from its record")

// matchContent reads the whole file from its current position and compares
// its length and SHA-256 with the recorded ones.
func matchContent(file io.Reader, size uint64, sha256Hex string) error {
	hash := sha256.New()
	n, err := io.Copy(hash, file)
	if err != nil {
		return err
	}
	if uint64(n) != size || hex.EncodeToString(hash.Sum(nil)) != strings.ToLower(sha256Hex) {
		return errContentChanged
	}
	return nil
}
