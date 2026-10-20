package main

// Windows installer already checks exclusive access to its owned executable.
func acquireRuntimeUse(executable string) (func(), error) { return func() {}, nil }
