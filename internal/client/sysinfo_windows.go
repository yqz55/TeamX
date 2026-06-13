//go:build windows

package client

// collectOSVersion on Windows reads OS version from the registry. Full
// implementation is deferred to Phase 10 (cross-platform adaptation).
func (s *SysInfo) collectOSVersion() {
	s.OSVersion = "windows"
	s.KernelVersion = "windows"
}
