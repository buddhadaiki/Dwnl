package testutil

import "testing"

func TempDir(t testing.TB) string                         { t.Helper(); return t.TempDir() }
func RetryDirectoryNotEmpty(operation func() error) error { return operation() }
