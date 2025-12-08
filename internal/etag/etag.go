// Package etag can create the etag value for the given data.
package etag

import (
	"crypto/sha256"
	"fmt"
)

// Of returns the etag for the given data.
func Of(data []byte) string {
	hash := sha256.Sum256(data)
	return fmt.Sprintf(`%x`, hash[:16])
}
