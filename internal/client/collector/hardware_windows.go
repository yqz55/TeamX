//go:build windows

package collector

import (
	"runtime"

	"teamx/internal/proto"
)

// ---- CPU -------------------------------------------------------------------

func collectCPU() *proto.CPUInfo {
	return &proto.CPUInfo{Architecture: runtime.GOARCH}
}

// ---- Memory ----------------------------------------------------------------

func collectMemory() *proto.MemoryInfo {
	return &proto.MemoryInfo{}
}

// ---- Disks -----------------------------------------------------------------

func collectDisks() []*proto.DiskInfo {
	return nil
}

// ---- Network ---------------------------------------------------------------

func collectNetwork() []*proto.NetInfo {
	return nil
}

// ---- BIOS ------------------------------------------------------------------

func collectBIOS() *proto.BIOSInfo {
	return nil
}

// ---- Motherboard -----------------------------------------------------------

func collectMotherboard() *proto.MotherboardInfo {
	return nil
}
