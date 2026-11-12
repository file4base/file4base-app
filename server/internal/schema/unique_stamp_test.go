package schema_test

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

// uniqueTestStamp is the suffix the integration tests give the tables they
// create, so that two of them never collide on one database.
//
// These tests used to take `time.Now().UnixNano() % 1000000`. On a host whose
// clock has microsecond resolution — macOS, among others — every nanosecond
// reading is a whole multiple of 1000, so that expression has only a thousand
// possible values. Nothing drops the tables afterwards either, so a developer's
// database fills up with them and the suite starts failing on
// "table already exists" for reasons that have nothing to do with the code
// under test.
//
// The stamp is the full nanosecond reading with random bits mixed in, which
// makes a repeat a practical impossibility and keeps the name sorted roughly
// by when it was made.
func uniqueTestStamp() int64 {
	var noise [2]byte
	if _, err := rand.Read(noise[:]); err != nil {
		return time.Now().UnixNano()
	}
	return time.Now().UnixNano() + int64(binary.BigEndian.Uint16(noise[:]))
}
