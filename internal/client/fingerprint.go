package client

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// cacheDir is the client config directory relative to the user's home.
const cacheDir = ".teamx"

// cacheFileName is the file storing the stable device fingerprint.
const cacheFileName = "device_id"

// GenerateDeviceID returns a stable, hardware-derived device fingerprint.
//
// Strategy:
//  1. If ~/.teamx/device_id exists, return its content (cached).
//  2. Otherwise, collect hardware sources (DMI UUID, machine-id, primary MAC, disk serial),
//     filter out empty/invalid values, join with "|", SHA-256 hash, cache to disk.
//  3. If no hardware sources are available, fall back to a hostname+kernel derived hash
//     and log a warning.
//
// The result is always a 64-character lowercase hex string.
func GenerateDeviceID() string {
	// 1. Try cached value.
	cachePath := deviceIDCachePath()
	if id, err := os.ReadFile(cachePath); err == nil && len(id) == 64 {
		return string(id)
	}

	// 2. Collect hardware sources.
	sources := collectHardwareSources()

	// Filter out empty and obviously-invalid values.
	var valid []string
	for _, s := range sources {
		s = strings.TrimSpace(s)
		if s == "" || isInvalidDeviceSource(s) {
			continue
		}
		valid = append(valid, s)
	}

	var id string
	if len(valid) == 0 {
		// Fallback: hostname + kernel version. Not stable across reinstalls,
		// but better than a random UUID.
		fallback := hostnameKernelFallback()
		id = sha256Hex(fallback)
		logWarn("device_id: no valid hardware sources, using fallback (hostname+kernel)")
	} else {
		seed := strings.Join(valid, "|")
		id = sha256Hex(seed)
	}

	// 3. Cache to disk.
	dir := filepath.Dir(cachePath)
	if err := os.MkdirAll(dir, 0700); err == nil {
		_ = os.WriteFile(cachePath, []byte(id), 0600)
	}

	return id
}

// deviceIDCachePath returns the full path to the cached device_id file.
func deviceIDCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, cacheDir, cacheFileName)
}

// isInvalidDeviceSource returns true for values that are clearly not real hardware IDs.
func isInvalidDeviceSource(s string) bool {
	u := strings.ToUpper(s)
	// All zeros, all Fs, or known filler strings.
	if isAll(u, '0') || isAll(u, 'F') {
		return true
	}
	invalid := []string{
		"NOT SPECIFIED",
		"TO BE FILLED BY O.E.M.",
		"TO BE FILLED BY OEM",
		"NONE",
		"UNKNOWN",
		"DEFAULT STRING",
		"00000000-0000-0000-0000-000000000000",
	}
	for _, v := range invalid {
		if u == v {
			return true
		}
	}
	return false
}

func isAll(s string, ch byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != ch {
			return false
		}
	}
	return len(s) > 0
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}

func logWarn(msg string) {
	// Use fmt.Fprintf to stderr so it is visible before logging is set up,
	// but also try the standard log package.
	fmtMsg := "[warn] " + msg
	os.Stderr.WriteString(fmtMsg + "\n")
}
