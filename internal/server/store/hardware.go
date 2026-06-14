package store

import (
	"database/sql"
	"fmt"
	"time"

	"teamx/internal/proto"
)

// ---- Hardware report persistence --------------------------------------------

// SaveHardwareReport persists a hardware report and its sub-entities in a
// single transaction. Duplicate report_id (replayed message) is silently
// ignored via INSERT OR IGNORE.
func (s *sqliteStore) SaveHardwareReport(clientID string, report *proto.ReportRequest) error {
	hw, ok := report.Type.(*proto.ReportRequest_Hardware)
	if !ok || hw.Hardware == nil {
		return fmt.Errorf("store: report %s has no HardwareInfo payload", report.GetReportId())
	}
	info := hw.Hardware
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback()

	// 1. Main hardware_reports row.
	const mainSQL = `
INSERT OR IGNORE INTO hardware_reports
    (report_id, client_id, created_at, cpu_model, cpu_cores, cpu_threads, cpu_arch,
     mem_total_bytes, mem_avail_bytes, mem_used_bytes)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	result, err := tx.Exec(mainSQL,
		report.GetReportId(), clientID, now,
		info.GetCpu().GetModel(), info.GetCpu().GetCores(), info.GetCpu().GetThreads(),
		info.GetCpu().GetArchitecture(),
		int64(info.GetMemory().GetTotalBytes()), int64(info.GetMemory().GetAvailableBytes()),
		int64(info.GetMemory().GetUsedBytes()),
	)
	if err != nil {
		return fmt.Errorf("store: insert hardware_reports: %w", err)
	}

	// If the row already existed (duplicate report_id), skip sub-tables.
	n, _ := result.RowsAffected()
	if n == 0 {
		return tx.Commit() // nothing to do — duplicate report
	}

	// 2. Disks.
	if disks := info.GetDisks(); len(disks) > 0 {
		const diskSQL = `
INSERT INTO hardware_disks (report_id, device, mount_point, fs_type,
    total_bytes, used_bytes, free_bytes)
VALUES (?, ?, ?, ?, ?, ?, ?)`
		for _, d := range disks {
			if _, err := tx.Exec(diskSQL,
				report.GetReportId(), d.GetDevice(), d.GetMountPoint(), d.GetFsType(),
				int64(d.GetTotalBytes()), int64(d.GetUsedBytes()), int64(d.GetFreeBytes()),
			); err != nil {
				return fmt.Errorf("store: insert disk: %w", err)
			}
		}
	}

	// 3. Network interfaces.
	if nets := info.GetNets(); len(nets) > 0 {
		const netSQL = `
INSERT INTO hardware_nets (report_id, name, mac_addr, ip_addrs, is_loopback)
VALUES (?, ?, ?, ?, ?)`
		for _, net := range nets {
			if _, err := tx.Exec(netSQL,
				report.GetReportId(), net.GetName(), net.GetMacAddr(),
				marshalJSONArray(net.GetIpAddrs()), net.GetIsLoopback(),
			); err != nil {
				return fmt.Errorf("store: insert net: %w", err)
			}
		}
	}

	// 4. BIOS (optional).
	if bios := info.GetBios(); bios != nil {
		const biosSQL = `
INSERT INTO hardware_bios (report_id, vendor, version, release_date)
VALUES (?, ?, ?, ?)`
		if _, err := tx.Exec(biosSQL,
			report.GetReportId(), bios.GetVendor(), bios.GetVersion(), bios.GetReleaseDate(),
		); err != nil {
			return fmt.Errorf("store: insert bios: %w", err)
		}
	}

	// 5. Motherboard (optional).
	if mb := info.GetMotherboard(); mb != nil {
		const mbSQL = `
INSERT INTO hardware_motherboard (report_id, manufacturer, product, serial)
VALUES (?, ?, ?, ?)`
		if _, err := tx.Exec(mbSQL,
			report.GetReportId(), mb.GetManufacturer(), mb.GetProduct(), mb.GetSerial(),
		); err != nil {
			return fmt.Errorf("store: insert motherboard: %w", err)
		}
	}

	return tx.Commit()
}

// GetLatestHardware returns the most recent hardware report for a client as a
// reconstituted proto.HardwareInfo, or nil if none exists.
func (s *sqliteStore) GetLatestHardware(clientID string) (*proto.HardwareInfo, error) {
	const mainSQL = `
SELECT report_id, cpu_model, cpu_cores, cpu_threads, cpu_arch,
       mem_total_bytes, mem_avail_bytes, mem_used_bytes
FROM hardware_reports
WHERE client_id = ?
ORDER BY created_at DESC
LIMIT 1`

	var (
		reportID                    string
		cores, threads              int32
		totalMem, availMem, usedMem int64
		cpuModel, cpuArch           string
	)
	err := s.db.QueryRow(mainSQL, clientID).Scan(
		&reportID, &cpuModel, &cores, &threads, &cpuArch,
		&totalMem, &availMem, &usedMem,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get latest hardware: %w", err)
	}

	info := &proto.HardwareInfo{
		Cpu: &proto.CPUInfo{
			Model:        cpuModel,
			Cores:        cores,
			Threads:      threads,
			Architecture: cpuArch,
		},
		Memory: &proto.MemoryInfo{
			TotalBytes:     uint64(totalMem),
			AvailableBytes: uint64(availMem),
			UsedBytes:      uint64(usedMem),
		},
	}

	// Sub-tables.
	info.Disks = s.loadDisks(reportID)
	info.Nets = s.loadNets(reportID)
	info.Bios = s.loadBIOS(reportID)
	info.Motherboard = s.loadMotherboard(reportID)

	return info, nil
}

// ListHardwareReports returns hardware snapshots for a client within a time
// range (since/until are RFC3339 strings; empty means unbounded).
func (s *sqliteStore) ListHardwareReports(clientID, since, until string, limit int) ([]*HardwareSnapshot, error) {
	if limit <= 0 {
		limit = 100
	}

	// Helper to avoid repeating the same query logic.
	query := func(where string, args []any) (*sql.Rows, error) {
		const base = `
SELECT report_id, created_at, cpu_model, cpu_cores, cpu_threads, cpu_arch,
       mem_total_bytes, mem_avail_bytes, mem_used_bytes
FROM hardware_reports
WHERE client_id = ? %s
ORDER BY created_at DESC LIMIT ?`
		allArgs := append([]any{clientID}, args...)
		allArgs = append(allArgs, limit)
		return s.db.Query(fmt.Sprintf(base, where), allArgs...)
	}

	var rows *sql.Rows
	var err error

	switch {
	case since != "" && until != "":
		rows, err = query("AND created_at >= ? AND created_at <= ?", []any{since, until})
	case since != "":
		rows, err = query("AND created_at >= ?", []any{since})
	case until != "":
		rows, err = query("AND created_at <= ?", []any{until})
	default:
		rows, err = query("", nil)
	}
	if err != nil {
		return nil, fmt.Errorf("store: list hardware reports: %w", err)
	}
	defer rows.Close()

	// Collect main rows first, then close the result set before loading
	// sub-tables.  This avoids a deadlock with SetMaxOpenConns(1): sub-queries
	// need a connection but the parent Rows still holds it.
	type mainRow struct {
		reportID, createdAt                   string
		cores, threads                        int32
		totalMem, availMem, usedMem           int64
		cpuModel, cpuArch                     string
	}
	var mainRows []mainRow
	for rows.Next() {
		var r mainRow
		if err := rows.Scan(&r.reportID, &r.createdAt, &r.cpuModel, &r.cores, &r.threads, &r.cpuArch,
			&r.totalMem, &r.availMem, &r.usedMem); err != nil {
			rows.Close()
			return nil, fmt.Errorf("store: scan hardware: %w", err)
		}
		mainRows = append(mainRows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate hardware: %w", err)
	}
	rows.Close() // release the connection before sub-queries

	// Now load sub-tables for each row.
	snapshots := make([]*HardwareSnapshot, 0, len(mainRows))
	for _, r := range mainRows {
		snapshots = append(snapshots, &HardwareSnapshot{
			ReportID:  r.reportID,
			CreatedAt: r.createdAt,
			Info: &proto.HardwareInfo{
				Cpu: &proto.CPUInfo{
					Model: r.cpuModel, Cores: r.cores, Threads: r.threads, Architecture: r.cpuArch,
				},
				Memory: &proto.MemoryInfo{
					TotalBytes: uint64(r.totalMem), AvailableBytes: uint64(r.availMem), UsedBytes: uint64(r.usedMem),
				},
				Disks:       s.loadDisks(r.reportID),
				Nets:        s.loadNets(r.reportID),
				Bios:        s.loadBIOS(r.reportID),
				Motherboard: s.loadMotherboard(r.reportID),
			},
		})
	}
	return snapshots, nil
}

// ---- sub-table loaders ------------------------------------------------------

func (s *sqliteStore) loadDisks(reportID string) []*proto.DiskInfo {
	rows, err := s.db.Query(`
SELECT device, mount_point, fs_type, total_bytes, used_bytes, free_bytes
FROM hardware_disks WHERE report_id = ?`, reportID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var disks []*proto.DiskInfo
	for rows.Next() {
		d := &proto.DiskInfo{}
		var total, used, free int64
		if err := rows.Scan(&d.Device, &d.MountPoint, &d.FsType, &total, &used, &free); err != nil {
			continue
		}
		d.TotalBytes = uint64(total)
		d.UsedBytes = uint64(used)
		d.FreeBytes = uint64(free)
		disks = append(disks, d)
	}
	return disks
}

func (s *sqliteStore) loadNets(reportID string) []*proto.NetInfo {
	rows, err := s.db.Query(`
SELECT name, mac_addr, ip_addrs, is_loopback
FROM hardware_nets WHERE report_id = ?`, reportID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var nets []*proto.NetInfo
	for rows.Next() {
		n := &proto.NetInfo{}
		var ipJSON string
		if err := rows.Scan(&n.Name, &n.MacAddr, &ipJSON, &n.IsLoopback); err != nil {
			continue
		}
		n.IpAddrs = parseJSONStringArray(ipJSON)
		nets = append(nets, n)
	}
	return nets
}

func (s *sqliteStore) loadBIOS(reportID string) *proto.BIOSInfo {
	const query = `SELECT vendor, version, release_date FROM hardware_bios WHERE report_id = ?`
	b := &proto.BIOSInfo{}
	err := s.db.QueryRow(query, reportID).Scan(&b.Vendor, &b.Version, &b.ReleaseDate)
	if err != nil {
		return nil
	}
	return b
}

func (s *sqliteStore) loadMotherboard(reportID string) *proto.MotherboardInfo {
	const query = `SELECT manufacturer, product, serial FROM hardware_motherboard WHERE report_id = ?`
	m := &proto.MotherboardInfo{}
	err := s.db.QueryRow(query, reportID).Scan(&m.Manufacturer, &m.Product, &m.Serial)
	if err != nil {
		return nil
	}
	return m
}

// parseJSONStringArray is a minimal JSON array-of-strings parser for the
// compact output produced by marshalJSONArray. It avoids importing
// encoding/json for simple cases.
func parseJSONStringArray(s string) []string {
	if s == "" || s == "[]" {
		return nil
	}
	// Strip brackets.
	inner := s[1 : len(s)-1]
	if inner == "" {
		return nil
	}
	parts := splitJSON(inner)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		// Unescape basic JSON escapes.
		p = unescapeJSON(p)
		out = append(out, p)
	}
	return out
}

// splitJSON splits "a","b" into ["a", "b"].
func splitJSON(s string) []string {
	var parts []string
	start := 0
	inStr := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '"' && (i == 0 || s[i-1] != '\\') {
			inStr = !inStr
			if !inStr {
				parts = append(parts, s[start:i+1])
			} else {
				start = i
			}
		}
	}
	return parts
}

// unescapeJSON strips surrounding quotes and handles \" and \\.
func unescapeJSON(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			out = append(out, s[i])
		} else {
			out = append(out, s[i])
		}
	}
	return string(out)
}
