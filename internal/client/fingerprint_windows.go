//go:build windows

package client

import (
	"os"
	"strings"
)

// collectHardwareSources gathers all available hardware identifiers on Windows.
// Full WMI implementation deferred to Phase 10; this stub provides a fallback.
func collectHardwareSources() []string {
	// Phase 10 will add:
	//   - WMI Win32_ComputerSystemProduct.UUID
	//   - WMI Win32_NetworkAdapter (primary MAC)
	//   - WMI Win32_DiskDrive.SerialNumber
	return nil
}

// hostnameKernelFallback returns "hostname|kernel" as a last-resort fallback.
func hostnameKernelFallback() string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	// Use os version info as a kernel-equivalent.
	return host + "|windows"
}

// readFile reads the first line of a file and trims whitespace.
// Kept for symmetry; Windows stubs use WMI instead.
func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
