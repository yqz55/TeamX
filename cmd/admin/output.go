package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"teamx/internal/proto"
)

// ---- JSON helpers ------------------------------------------------------------

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "json encode error: %v\n", err)
	}
}

func printJSONError(err error) {
	printJSON(map[string]string{"error": err.Error()})
}

// ---- Terminal list (table) ---------------------------------------------------

func printTerminalList(terminals []*proto.TerminalSummary, total int32, jsonMode bool) {
	if jsonMode {
		printJSON(map[string]any{
			"terminals":   terminals,
			"total_count": total,
		})
		return
	}

	if len(terminals) == 0 {
		fmt.Println("No terminals found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SESSION ID\tDEVICE ID\tHOSTNAME\tOS\tVERSION\tSTATUS\tLAST HEARTBEAT")
	for _, t := range terminals {
		status := "OFFLINE"
		if t.Online {
			status = "ONLINE"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			truncate(t.SessionId, 36),
			truncate(t.DeviceId, 16),
			t.Hostname,
			t.Os,
			t.ClientVersion,
			status,
			na(t.LastHeartbeat),
		)
	}
	w.Flush()

	// Summary line.
	online := 0
	for _, t := range terminals {
		if t.Online {
			online++
		}
	}
	fmt.Printf("---\nTotal: %d terminals (%d online, %d offline)\n", total, online, total-int32(online))
}

// ---- Terminal detail ---------------------------------------------------------

func printTerminalDetail(resp *proto.GetTerminalResponse, jsonMode bool) {
	if jsonMode {
		printJSON(resp)
		return
	}

	s := resp.Summary
	fmt.Println("Summary:")
	fmt.Printf("  Session ID:   %s\n", s.SessionId)
	fmt.Printf("  Device ID:    %s\n", s.DeviceId)
	fmt.Printf("  Hostname:     %s\n", s.Hostname)
	fmt.Printf("  OS:           %s (%s)\n", s.Os, s.OsVersion)
	fmt.Printf("  Version:      %s\n", s.ClientVersion)
	status := "OFFLINE"
	if s.Online {
		status = "ONLINE"
	}
	fmt.Printf("  Status:       %s\n", status)
	fmt.Printf("  Last Seen:    %s\n", na(s.LastSeenAt))
	fmt.Printf("  First Seen:   %s\n", "") // not in summary proto

	if hw := resp.LatestHardware; hw != nil {
		fmt.Println()
		printHardwareBlock(hw)
	} else {
		fmt.Println("\nHardware: (no report yet)")
	}
}

func printHardwareBlock(hw *proto.HardwareInfo) {
	fmt.Println("Hardware:")

	cpu := hw.GetCpu()
	if cpu != nil {
		fmt.Printf("  CPU:          %s (%d cores / %d threads, %s)\n",
			cpu.Model, cpu.Cores, cpu.Threads, cpu.Architecture)
	}

	mem := hw.GetMemory()
	if mem != nil {
		fmt.Printf("  Memory:       %s / %s (%.0f%%)\n",
			formatBytes(mem.UsedBytes), formatBytes(mem.TotalBytes),
			memPercent(mem.UsedBytes, mem.TotalBytes))
	}

	if disks := hw.GetDisks(); len(disks) > 0 {
		fmt.Println("  Disks:")
		for _, d := range disks {
			fmt.Printf("    %-12s %-6s %s / %s (%.0f%%)\n",
				d.Device, d.FsType,
				formatBytes(d.UsedBytes), formatBytes(d.TotalBytes),
				memPercent(d.UsedBytes, d.TotalBytes))
		}
	}

	if nets := hw.GetNets(); len(nets) > 0 {
		fmt.Println("  Network:")
		for _, n := range nets {
			ip := "-"
			if len(n.IpAddrs) > 0 {
				ip = strings.Join(n.IpAddrs, ", ")
			}
			fmt.Printf("    %-12s %s  %s\n", n.Name, n.MacAddr, ip)
		}
	}

	if bios := hw.GetBios(); bios != nil {
		fmt.Printf("  BIOS:         %s v%s (%s)\n", bios.Vendor, bios.Version, na(bios.ReleaseDate))
	}

	if mb := hw.GetMotherboard(); mb != nil {
		fmt.Printf("  Motherboard:  %s %s / SN: %s\n", mb.Manufacturer, mb.Product, na(mb.Serial))
	}
}

// ---- Terminal history --------------------------------------------------------

func printTerminalHistory(resp *proto.GetTerminalHistoryResponse, jsonMode bool) {
	if jsonMode {
		printJSON(resp)
		return
	}

	if len(resp.Snapshots) == 0 {
		fmt.Println("No hardware snapshots found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "REPORT ID\tCREATED AT\tCPU\tMEMORY\tDISKS\tNETS")
	for _, s := range resp.Snapshots {
		hw := s.Info
		cpuStr := "-"
		memStr := "-"
		diskStr := fmt.Sprintf("%d", len(hw.GetDisks()))
		netStr := fmt.Sprintf("%d", len(hw.GetNets()))

		if cpu := hw.GetCpu(); cpu != nil {
			cpuStr = fmt.Sprintf("%s %dC/%dT", cpu.Model, cpu.Cores, cpu.Threads)
		}
		if mem := hw.GetMemory(); mem != nil {
			memStr = fmt.Sprintf("%s/%s", formatBytes(mem.UsedBytes), formatBytes(mem.TotalBytes))
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			truncate(s.ReportId, 36),
			s.CreatedAt,
			truncate(cpuStr, 25),
			memStr,
			diskStr,
			netStr,
		)
	}
	w.Flush()
	fmt.Printf("---\n%d snapshots for device %s\n", len(resp.Snapshots), resp.DeviceId)
}

// ---- Simple result (kick / block / unblock) ----------------------------------

func printResult(ok bool, message string, jsonMode bool) {
	if jsonMode {
		printJSON(map[string]any{
			"ok":      ok,
			"message": message,
		})
		return
	}

	if ok {
		fmt.Printf("✓ %s\n", message)
	} else {
		fmt.Printf("✗ %s\n", message)
	}
}

func printError(err error, jsonMode bool) {
	if jsonMode {
		printJSONError(err)
		return
	}
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
}

// ---- formatting helpers ------------------------------------------------------

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func na(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func memPercent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}
