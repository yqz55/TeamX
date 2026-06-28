// dump_db is a quick SQLite inspection tool for Phase 3 verification.
// Usage: go run tools/dump_db.go
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	dbPath := "teamx.db"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// ---- terminals ----
	fmt.Println("=== terminals ===")
	rows, err := db.Query(`SELECT session_id, device_id, hostname, os, os_version, client_version,
		online, blocked, last_heartbeat, last_seen_at, first_seen_at
		FROM terminals ORDER BY last_seen_at DESC`)
	if err != nil {
		log.Fatal(err)
	}
	count := 0
	for rows.Next() {
		var sid, did, host, osName, osVer, cliVer, lhb, lastSeen, firstSeen string
		var online, blocked int
		rows.Scan(&sid, &did, &host, &osName, &osVer, &cliVer, &online, &blocked, &lhb, &lastSeen, &firstSeen)
		fmt.Printf("  sid=%s did=%s host=%s os=%s ver=%s online=%d blocked=%d\n",
			safeP(sid, 8), safeP(did, 16), host, osName, cliVer, online, blocked)
		fmt.Printf("    first=%s last=%s hb=%s\n",
			safeP(firstSeen, 19), safeP(lastSeen, 19), safeP(lhb, 19))
		count++
	}
	rows.Close()
	fmt.Printf("  → %d terminal(s)\n", count)

	// ---- hardware_reports ----
	fmt.Println("\n=== hardware_reports ===")
	rows2, err := db.Query(`SELECT report_id, device_id, session_id, cpu_model, cpu_cores, cpu_threads,
		cpu_arch, mem_total_bytes/1048576, mem_avail_bytes/1048576, created_at
		FROM hardware_reports ORDER BY created_at DESC LIMIT 10`)
	if err != nil {
		log.Fatal(err)
	}
	count2 := 0
	for rows2.Next() {
		var rid, did, sid, model, arch, created string
		var cores, threads int
		var totalMB, availMB int64
		rows2.Scan(&rid, &did, &sid, &model, &cores, &threads, &arch, &totalMB, &availMB, &created)
		fmt.Printf("  rid=%s did=%s sid=%s cpu=%s cores=%d/%d arch=%s mem=%dMB/%dMB created=%s\n",
			safeP(rid, 8), safeP(did, 16), safeP(sid, 8), model, cores, threads, arch, totalMB, availMB, safeP(created, 19))
		count2++
	}
	rows2.Close()
	fmt.Printf("  → %d hardware report(s)\n", count2)

	// ---- hardware_disks ----
	fmt.Println("\n=== hardware_disks ===")
	rows3, err := db.Query(`SELECT report_id, device, mount_point, fs_type,
		total_bytes/1073741824, used_bytes/1073741824
		FROM hardware_disks LIMIT 10`)
	if err != nil {
		log.Fatal(err)
	}
	count3 := 0
	for rows3.Next() {
		var rid, dev, mp, fs string
		var totalGB, usedGB int64
		rows3.Scan(&rid, &dev, &mp, &fs, &totalGB, &usedGB)
		fmt.Printf("  rid=%s dev=%s mount=%s fs=%s total=%dGB used=%dGB\n",
			safeP(rid, 8), dev, mp, fs, totalGB, usedGB)
		count3++
	}
	rows3.Close()
	fmt.Printf("  → %d disk(s)\n", count3)

	// ---- hardware_nets ----
	fmt.Println("\n=== hardware_nets ===")
	rows4, err := db.Query(`SELECT report_id, name, mac_addr, is_loopback
		FROM hardware_nets LIMIT 10`)
	if err != nil {
		log.Fatal(err)
	}
	count4 := 0
	for rows4.Next() {
		var rid, name, mac string
		var lo bool
		rows4.Scan(&rid, &name, &mac, &lo)
		fmt.Printf("  rid=%s name=%s mac=%s lo=%v\n", safeP(rid, 8), name, mac, lo)
		count4++
	}
	rows4.Close()
	fmt.Printf("  → %d network interface(s)\n", count4)

	// ---- hardware_bios ----
	fmt.Println("\n=== hardware_bios ===")
	rows5, err := db.Query(`SELECT report_id, vendor, version, release_date
		FROM hardware_bios LIMIT 5`)
	if err != nil {
		log.Fatal(err)
	}
	count5 := 0
	for rows5.Next() {
		var rid, vendor, ver, date string
		rows5.Scan(&rid, &vendor, &ver, &date)
		fmt.Printf("  rid=%s vendor=%s ver=%s date=%s\n", safeP(rid, 8), vendor, ver, date)
		count5++
	}
	rows5.Close()
	fmt.Printf("  → %d BIOS record(s)\n", count5)

	// ---- hardware_motherboard ----
	fmt.Println("\n=== hardware_motherboard ===")
	rows6, err := db.Query(`SELECT report_id, manufacturer, product, serial
		FROM hardware_motherboard LIMIT 5`)
	if err != nil {
		log.Fatal(err)
	}
	count6 := 0
	for rows6.Next() {
		var rid, manu, prod, serial string
		rows6.Scan(&rid, &manu, &prod, &serial)
		fmt.Printf("  rid=%s mfr=%s prod=%s serial=%s\n", safeP(rid, 8), manu, prod, serial)
		count6++
	}
	rows6.Close()
	fmt.Printf("  → %d motherboard record(s)\n", count6)

	fmt.Println("\n✅ dump_db complete.")
}

func safeP(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}
