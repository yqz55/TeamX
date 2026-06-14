package collector

import (
	"teamx/internal/proto"
)

// Collector gathers system information for reporting to the server.
// It is a zero-config struct; all platform-specific logic lives in
// the corresponding _linux / _windows files.
type Collector struct{}

// CollectHardware gathers CPU, memory, disk, network, BIOS, and motherboard
// information into a single HardwareInfo message. Optional fields (BIOS,
// motherboard) are nil when the underlying data source is unavailable.
func (c *Collector) CollectHardware() *proto.HardwareInfo {
	return &proto.HardwareInfo{
		Cpu:         collectCPU(),
		Memory:      collectMemory(),
		Disks:       collectDisks(),
		Nets:        collectNetwork(),
		Bios:        collectBIOS(),
		Motherboard: collectMotherboard(),
	}
}
