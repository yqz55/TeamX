package store

import (
	"database/sql"
	"log"
)

// migrate ensures all required tables exist.
// Each CREATE TABLE uses IF NOT EXISTS, so it is safe to call on every startup.
func migrate(db *sql.DB) error {
	ddl := []string{

		// ---- Terminal --------------------------------------------------------

		`CREATE TABLE IF NOT EXISTS terminals (
			client_id       TEXT PRIMARY KEY,
			hostname        TEXT NOT NULL,
			os              TEXT NOT NULL,
			os_version      TEXT NOT NULL DEFAULT '',
			kernel_version  TEXT NOT NULL DEFAULT '',
			client_version  TEXT NOT NULL DEFAULT '',
			mac_addrs       TEXT NOT NULL DEFAULT '[]',
			ip_addrs        TEXT NOT NULL DEFAULT '[]',
			first_seen_at   TEXT NOT NULL,
			last_seen_at    TEXT NOT NULL,
			last_heartbeat  TEXT,
			online          INTEGER NOT NULL DEFAULT 0,
			blocked         INTEGER NOT NULL DEFAULT 0
		)`,

		// ---- Hardware --------------------------------------------------------

		`CREATE TABLE IF NOT EXISTS hardware_reports (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			report_id       TEXT NOT NULL UNIQUE,
			client_id       TEXT NOT NULL,
			created_at      TEXT NOT NULL,
			cpu_model       TEXT NOT NULL DEFAULT '',
			cpu_cores       INTEGER NOT NULL DEFAULT 0,
			cpu_threads     INTEGER NOT NULL DEFAULT 0,
			cpu_arch        TEXT NOT NULL DEFAULT '',
			mem_total_bytes INTEGER NOT NULL DEFAULT 0,
			mem_avail_bytes INTEGER NOT NULL DEFAULT 0,
			mem_used_bytes  INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (client_id) REFERENCES terminals(client_id)
		)`,

		`CREATE INDEX IF NOT EXISTS idx_hw_reports_client
			ON hardware_reports(client_id, created_at DESC)`,

		`CREATE TABLE IF NOT EXISTS hardware_disks (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			report_id       TEXT NOT NULL,
			device          TEXT NOT NULL DEFAULT '',
			mount_point     TEXT NOT NULL DEFAULT '',
			fs_type         TEXT NOT NULL DEFAULT '',
			total_bytes     INTEGER NOT NULL DEFAULT 0,
			used_bytes      INTEGER NOT NULL DEFAULT 0,
			free_bytes      INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (report_id) REFERENCES hardware_reports(report_id)
		)`,

		`CREATE INDEX IF NOT EXISTS idx_hw_disks_report
			ON hardware_disks(report_id)`,

		`CREATE TABLE IF NOT EXISTS hardware_nets (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			report_id       TEXT NOT NULL,
			name            TEXT NOT NULL DEFAULT '',
			mac_addr        TEXT NOT NULL DEFAULT '',
			ip_addrs        TEXT NOT NULL DEFAULT '[]',
			is_loopback     INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (report_id) REFERENCES hardware_reports(report_id)
		)`,

		`CREATE INDEX IF NOT EXISTS idx_hw_nets_report
			ON hardware_nets(report_id)`,

		`CREATE TABLE IF NOT EXISTS hardware_bios (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			report_id       TEXT NOT NULL UNIQUE,
			vendor          TEXT NOT NULL DEFAULT '',
			version         TEXT NOT NULL DEFAULT '',
			release_date    TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (report_id) REFERENCES hardware_reports(report_id)
		)`,

		`CREATE TABLE IF NOT EXISTS hardware_motherboard (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			report_id       TEXT NOT NULL UNIQUE,
			manufacturer    TEXT NOT NULL DEFAULT '',
			product         TEXT NOT NULL DEFAULT '',
			serial          TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (report_id) REFERENCES hardware_reports(report_id)
		)`,

		// ---- Software (Phase 6) ----------------------------------------------

		`CREATE TABLE IF NOT EXISTS software_items (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			report_id       TEXT NOT NULL,
			client_id       TEXT NOT NULL,
			name            TEXT NOT NULL DEFAULT '',
			version         TEXT NOT NULL DEFAULT '',
			publisher       TEXT NOT NULL DEFAULT '',
			install_date    TEXT NOT NULL DEFAULT '',
			created_at      TEXT NOT NULL,
			FOREIGN KEY (client_id) REFERENCES terminals(client_id)
		)`,

		`CREATE INDEX IF NOT EXISTS idx_sw_items_client
			ON software_items(client_id, name)`,

		// ---- Users (Phase 6) -------------------------------------------------

		`CREATE TABLE IF NOT EXISTS user_accounts (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			report_id       TEXT NOT NULL,
			client_id       TEXT NOT NULL,
			username        TEXT NOT NULL DEFAULT '',
			uid             TEXT NOT NULL DEFAULT '',
			group_name      TEXT NOT NULL DEFAULT '',
			home_dir        TEXT NOT NULL DEFAULT '',
			shell           TEXT NOT NULL DEFAULT '',
			is_admin        INTEGER NOT NULL DEFAULT 0,
			is_disabled     INTEGER NOT NULL DEFAULT 0,
			created_at      TEXT NOT NULL,
			FOREIGN KEY (client_id) REFERENCES terminals(client_id)
		)`,

		// ---- Processes (Phase 6) ---------------------------------------------

		`CREATE TABLE IF NOT EXISTS process_items (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			report_id       TEXT NOT NULL,
			client_id       TEXT NOT NULL,
			pid             INTEGER NOT NULL DEFAULT 0,
			ppid            INTEGER NOT NULL DEFAULT 0,
			name            TEXT NOT NULL DEFAULT '',
			username        TEXT NOT NULL DEFAULT '',
			cpu_percent     REAL NOT NULL DEFAULT 0.0,
			mem_bytes       INTEGER NOT NULL DEFAULT 0,
			status          TEXT NOT NULL DEFAULT '',
			cmdline         TEXT NOT NULL DEFAULT '',
			created_at      TEXT NOT NULL,
			FOREIGN KEY (client_id) REFERENCES terminals(client_id)
		)`,

		// ---- Peripherals (Phase 6) -------------------------------------------

		`CREATE TABLE IF NOT EXISTS peripheral_devices (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			report_id       TEXT NOT NULL,
			client_id       TEXT NOT NULL,
			device_type     TEXT NOT NULL DEFAULT '',
			name            TEXT NOT NULL DEFAULT '',
			vendor_id       TEXT NOT NULL DEFAULT '',
			product_id      TEXT NOT NULL DEFAULT '',
			serial          TEXT NOT NULL DEFAULT '',
			extra           TEXT NOT NULL DEFAULT '{}',
			created_at      TEXT NOT NULL,
			FOREIGN KEY (client_id) REFERENCES terminals(client_id)
		)`,

		// ---- Command logs (Phase 5) ------------------------------------------

		`CREATE TABLE IF NOT EXISTS command_logs (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			command_id      TEXT NOT NULL UNIQUE,
			client_id       TEXT NOT NULL,
			type            TEXT NOT NULL DEFAULT '',
			params          TEXT NOT NULL DEFAULT '{}',
			status          TEXT NOT NULL DEFAULT 'Pending',
			exit_code       INTEGER,
			stdout          TEXT NOT NULL DEFAULT '',
			stderr          TEXT NOT NULL DEFAULT '',
			error_message   TEXT NOT NULL DEFAULT '',
			created_at      TEXT NOT NULL,
			started_at      TEXT,
			finished_at     TEXT,
			FOREIGN KEY (client_id) REFERENCES terminals(client_id)
		)`,

		`CREATE INDEX IF NOT EXISTS idx_cmd_logs_client
			ON command_logs(client_id, created_at DESC)`,
	}

	for _, stmt := range ddl {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}

	log.Printf("[store] schema migrated (%d tables)", len(ddl))
	return nil
}
