//go:build linux

package client

import (
	"os"
	"strings"
)

// collectHardwareSources gathers all available hardware identifiers on Linux.
func collectHardwareSources() []string {
	return []string{
		readFile("/sys/class/dmi/id/product_uuid"),
		readFile("/etc/machine-id"),
		primaryMAC(),
		systemDiskSerial(),
	}
}

// primaryMAC returns the MAC address of the first non-loopback, non-virtual interface.
func primaryMAC() string {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return ""
	}

	// Prefer the interface that has a "device" symlink (physical NIC).
	var fallback string
	for _, e := range entries {
		name := e.Name()
		if name == "lo" || strings.HasPrefix(name, "docker") ||
			strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "virbr") ||
			strings.HasPrefix(name, "br-") || strings.HasPrefix(name, "wg") {
			continue
		}

		addr := readFile("/sys/class/net/" + name + "/address")
		if addr == "" {
			continue
		}

		// Check if this is a physical device (has a device symlink).
		if _, err := os.Stat("/sys/class/net/" + name + "/device"); err == nil {
			return addr
		}
		if fallback == "" {
			fallback = addr
		}
	}
	return fallback
}

// systemDiskSerial returns the serial of the first non-removable block device.
func systemDiskSerial() string {
	// Try common system disk names.
	candidates := []string{"sda", "vda", "nvme0n1", "hda"}
	for _, dev := range candidates {
		serial := readFile("/sys/block/" + dev + "/device/serial")
		if serial != "" {
			return serial
		}
	}
	return ""
}

// hostnameKernelFallback returns "hostname|kernel" as a last-resort fallback.
func hostnameKernelFallback() string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	kernel := readFile("/proc/version")
	if kernel == "" {
		kernel = "unknown"
	}
	return host + "|" + kernel
}

// readFile reads the first line of a file and trims whitespace.
func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
