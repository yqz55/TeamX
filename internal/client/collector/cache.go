package collector

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"google.golang.org/protobuf/proto"
	pb "teamx/internal/proto"
)

// ReportCache tracks the last reported hardware snapshot to avoid sending
// duplicate data when nothing has changed.
type ReportCache struct {
	mu       sync.Mutex
	lastHash string
}

// IsChanged returns true when info differs from the last cached snapshot (or
// when no snapshot has been cached yet). On the first call with a zero cache
// it always returns true so the initial report is always sent.
func (c *ReportCache) IsChanged(info *pb.HardwareInfo) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.lastHash == "" {
		return true
	}

	hash := hashHardware(info)
	return hash != c.lastHash
}

// MarkSent stores the hash of info so subsequent IsChanged calls return false
// until the hardware data actually changes.
func (c *ReportCache) MarkSent(info *pb.HardwareInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastHash = hashHardware(info)
}

// hashHardware serializes info via proto.Marshal and returns its SHA-256 hex
// digest. A marshal error produces a unique "error-" prefix so it never
// accidentally matches a valid cached hash.
func hashHardware(info *pb.HardwareInfo) string {
	b, err := proto.Marshal(info)
	if err != nil {
		return "error-" + hex.EncodeToString(make([]byte, 32)) // all-zeros, never matches real data
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
