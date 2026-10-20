package ports

import "runtime"

// ExecutableFile is used only for host-owned roles, never browser input.
func ExecutableFile(role ExecutableRole) string {
	if runtime.GOOS == "windows" {
		return string(role) + ".exe"
	}
	return string(role)
}
