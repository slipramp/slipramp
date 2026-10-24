package inbox

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

const occurrenceVersionPrefix = "slipramp-inbox-problem-occurrence:v1"

func problemOccurrenceID(kind Kind, resourceID string, occurredAt time.Time) string {
	canonical := occurrenceVersionPrefix + "\x00" + string(kind) + "\x00" + resourceID + "\x00" +
		occurredAt.UTC().Format(time.RFC3339Nano)
	digest := sha256.Sum256([]byte(canonical))

	return hex.EncodeToString(digest[:])
}
