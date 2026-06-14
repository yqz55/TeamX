//go:build linux

package collector

import (
	"log"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"teamx/internal/proto"
)

// ---- CPU -------------------------------------------------------------------

func collectCPU() *proto.CPUInfo {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		log.Printf("[collector] read /proc/cpuinfo: %v", err)
		return &proto.CPUInfo{Architecture: runtime.GOARCH}
	}

	var model string
	var cores, threads int32

	// /proc/cpuinfo repeats the same fields for every logical processor.
	// Take the first occurrence of each; they are identical across blocks.
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			// Empty line separates processor blocks — stop at the first gap
			// because we already have model/cores/threads from block 0.
			if model != "" && cores > 0 {
				break
			}
			continue
		}

		key, val, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)

		switch key {
		case "model name":
			if model == "" {
				model = val
			}
		case "cpu cores":
			if cores == 0 {
				if n, err := strconv.Atoi(val); err == nil {
					cores = int32(n)
				}
			}
		case "siblings":
			if threads == 0 {
				if n, err := strconv.Atoi(val); err == nil {
					threads = int32(n)
				}
			}
		}
	}

	return &proto.CPUInfo{
		Model:        model,
		Cores:        cores,
		Threads:      threads,
		Architecture: runtime.GOARCH,
	}
}

// ---- Memory ----------------------------------------------------------------

func collectMemory() *proto.MemoryInfo {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		log.Printf("[collector] read /proc/meminfo: %v", err)
		return &proto.MemoryInfo{}
	}

	var totalKB, availKB uint64

	for _, line := range strings.Split(string(data), "\n") {
		key, val, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)

		// Value is like " 8196024 kB" — trim and drop the " kB" suffix.
		val = strings.TrimSpace(val)
		val = strings.TrimSuffix(val, " kB")

		n, err := strconv.ParseUint(val, 10, 64)
		if err != nil {
			continue
		}

		switch key {
		case "MemTotal":
			totalKB = n
		case "MemAvailable":
			availKB = n
		}

		if totalKB > 0 && availKB > 0 {
			break
		}
	}

	total := totalKB * 1024
	avail := availKB * 1024
	used := total - avail // simplified: does not account for buffers/cache

	return &proto.MemoryInfo{
		TotalBytes:     total,
		AvailableBytes: avail,
		UsedBytes:      used,
	}
}

// ---- Disks -----------------------------------------------------------------

// realFSTypes lists filesystem types considered "real" storage.
// Virtual / kernel filesystems are excluded.
var realFSTypes = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true,
	"xfs": true,
	"btrfs": true,
	"zfs": true,
	"ntfs": true, "ntfs-3g": true,
	"vfat": true, "msdos": true, "fat": true, "exfat": true,
	"f2fs": true,
	"jfs": true,
	"reiserfs": true,
	"nfs": true, "nfs4": true,
	"cifs": true, "smbfs": true,
	"hfs": true, "hfsplus": true,
	"apfs": true,
}

func collectDisks() []*proto.DiskInfo {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		log.Printf("[collector] read /proc/mounts: %v", err)
		return nil
	}

	var disks []*proto.DiskInfo

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Format: device mountpoint fstype options dump pass
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		device := fields[0]
		mountpoint := fields[1]
		fstype := fields[2]

		if !realFSTypes[fstype] {
			continue
		}

		// Get capacity via statfs.
		var stat syscall.Statfs_t
		if err := syscall.Statfs(mountpoint, &stat); err != nil {
			log.Printf("[collector] statfs %s: %v", mountpoint, err)
			continue
		}

		//nolint:unconvert // Bsize is int64 on some arch, uint64 cast is safe.
		bsize := uint64(stat.Bsize)
		total := stat.Blocks * bsize
		free := stat.Bfree * bsize
		used := total - free

		disks = append(disks, &proto.DiskInfo{
			Device:     device,
			MountPoint: mountpoint,
			FsType:     fstype,
			TotalBytes: total,
			UsedBytes:  used,
			FreeBytes:  free,
		})
	}

	return disks
}

// ---- Network ---------------------------------------------------------------

func collectNetwork() []*proto.NetInfo {
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Printf("[collector] list interfaces: %v", err)
		return nil
	}

	var nets []*proto.NetInfo

	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		var ips []string
		for _, addr := range addrs {
			// addr.String() returns "192.168.1.1/24" — strip the mask.
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil {
				// Some addresses are just bare IPs.
				if ip = net.ParseIP(strings.TrimSuffix(addr.String(), "/32")); ip == nil {
					continue
				}
			}
			ips = append(ips, ip.String())
		}

		mac := iface.HardwareAddr.String()
		if mac == "" {
			mac = "00:00:00:00:00:00"
		}

		nets = append(nets, &proto.NetInfo{
			Name:       iface.Name,
			MacAddr:    mac,
			IpAddrs:    ips,
			IsLoopback: iface.Flags&net.FlagLoopback != 0,
		})
	}

	return nets
}

// ---- BIOS ------------------------------------------------------------------

func collectBIOS() *proto.BIOSInfo {
	vendor := readDMIField("bios_vendor")
	version := readDMIField("bios_version")
	date := readDMIField("bios_date")

	if vendor == "" && version == "" && date == "" {
		return nil // DMI not available (container, some VMs)
	}

	return &proto.BIOSInfo{
		Vendor:      vendor,
		Version:     version,
		ReleaseDate: date,
	}
}

// ---- Motherboard -----------------------------------------------------------

func collectMotherboard() *proto.MotherboardInfo {
	mfr := readDMIField("board_vendor")
	product := readDMIField("board_name")
	serial := readDMIField("board_serial")

	if mfr == "" && product == "" && serial == "" {
		return nil
	}

	return &proto.MotherboardInfo{
		Manufacturer: mfr,
		Product:      product,
		Serial:       serial,
	}
}

// ---- helpers ---------------------------------------------------------------

// readDMIField reads a single value from /sys/class/dmi/id/<name>.
// Returns "" if the file is missing or unreadable.
func readDMIField(name string) string {
	data, err := os.ReadFile("/sys/class/dmi/id/" + name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
