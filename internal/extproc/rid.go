package extproc

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"time"
)

var ridFallbackCounter atomic.Uint64

// newBYGRID creates a BYG-owned correlation ID. A random UUID-shaped value
// avoids reusing either legacy endpoint's incompatible RID format; the prefix
// makes its provenance obvious in cross-system diagnostics.
// NewBYGRID creates a gateway-neutral correlation ID for a processing stream.
func NewBYGRID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err == nil {
		bytes[6] = (bytes[6] & 0x0f) | 0x40
		bytes[8] = (bytes[8] & 0x3f) | 0x80
		return fmt.Sprintf("RID-%s-%s-%s-%s-%s", hex.EncodeToString(bytes[0:4]), hex.EncodeToString(bytes[4:6]), hex.EncodeToString(bytes[6:8]), hex.EncodeToString(bytes[8:10]), hex.EncodeToString(bytes[10:16]))
	}
	// Extremely rare entropy failures must not leave a stream without a RID.
	return fmt.Sprintf("RID-fallback-%x-%x", time.Now().UnixNano(), ridFallbackCounter.Add(1))
}
