package data

import "time"

// SetMissingCreationOrderTTL sets how long a table found without its creation
// order column is believed not to have it, until restore is called.
func SetMissingCreationOrderTTL(ttl time.Duration) (restore func()) {
	old := missingCreationOrderTTL
	missingCreationOrderTTL = ttl
	return func() { missingCreationOrderTTL = old }
}
