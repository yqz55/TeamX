package client

import (
	"net"
	"os"
	"runtime"
)

// SysInfo collects platform-specific identification data used in the
// Handshake.
type SysInfo struct {
	Hostname      string
	OS            string
	OSVersion     string
	KernelVersion string
	IPAddrs       []string
	MacAddrs      []string
}

// Collect gathers all available system information. Platform-specific fields
// (OSVersion, KernelVersion) are filled by _linux / _windows / _darwin files.
func Collect() SysInfo {
	info := SysInfo{
		OS: runtime.GOOS,
	}
	info.collectHostname()
	info.collectNetwork()
	info.collectOSVersion() // build-tag specific
	return info
}

func (s *SysInfo) collectHostname() {
	h, err := os.Hostname()
	if err != nil {
		s.Hostname = "unknown"
		return
	}
	s.Hostname = h
}

func (s *SysInfo) collectNetwork() {
	interfaces, err := net.Interfaces()
	if err != nil {
		return
	}
	for _, iface := range interfaces {
		// Skip loopback; include only up interfaces.
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		mac := iface.HardwareAddr.String()
		if mac != "" {
			s.MacAddrs = append(s.MacAddrs, mac)
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.IsGlobalUnicast() {
				s.IPAddrs = append(s.IPAddrs, ipnet.IP.String())
			}
		}
	}
}
