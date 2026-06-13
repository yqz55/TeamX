//go:build linux

package client

import (
	"os"
	"strings"
)

// collectOSVersion reads OS version from /etc/os-release and kernel version
// from /proc/version (Linux only).
func (s *SysInfo) collectOSVersion() {
	s.OSVersion = readOSRelease()
	s.KernelVersion = readProcVersion()
}

// readOSRelease parses /etc/os-release for the PRETTY_NAME field.
func readOSRelease() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		// Fallback: try /usr/lib/os-release (some distros).
		data, err = os.ReadFile("/usr/lib/os-release")
		if err != nil {
			return "unknown"
		}
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			// Strip PRETTY_NAME= and surrounding quotes.
			v := strings.TrimPrefix(line, "PRETTY_NAME=")
			v = strings.Trim(v, `"`)
			return v
		}
	}
	return "unknown"
}

// readProcVersion reads the first line of /proc/version for kernel info.
func readProcVersion() string {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return "unknown"
	}
	line := strings.TrimSpace(string(data))
	// Typical format: "Linux version 6.1.0-xxx ..."
	// Return just the first 3 words, e.g. "Linux version 6.1.0-amd64"
	parts := strings.Fields(line)
	if len(parts) >= 3 {
		return strings.Join(parts[:3], " ")
	}
	return line
}
