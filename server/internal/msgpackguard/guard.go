// Package msgpackguard checks the structure of an untrusted MessagePack
// document before it is decoded.
//
// github.com/vmihailenco/msgpack/v5 (v5.4.1, the latest release) allocates
// slices and maps from the lengths a document declares, before decoding their
// elements, and its allocation limit is never applied. A few bytes declaring
// a huge collection can therefore exhaust memory. Check walks the encoded
// bytes without allocating and rejects documents whose declared sizes cannot
// be backed by the bytes that follow, that nest too deeply or that declare
// too many elements in total.
package msgpackguard

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrRejected is returned (wrapped) for any document that fails the check.
var ErrRejected = errors.New("invalid MessagePack document")

// Limits bounds a document. Zero values use the defaults.
type Limits struct {
	// MaxDepth is the deepest nesting of arrays and maps (default 32).
	MaxDepth int
	// MaxEntries is the total number of array elements plus map pairs
	// (default 5,000,000).
	MaxEntries int
}

const (
	defaultMaxDepth   = 32
	defaultMaxEntries = 5_000_000
)

// Check validates the first MessagePack value in data.
func Check(data []byte, lim Limits) error {
	if lim.MaxDepth <= 0 {
		lim.MaxDepth = defaultMaxDepth
	}
	if lim.MaxEntries <= 0 {
		lim.MaxEntries = defaultMaxEntries
	}
	c := checker{data: data, lim: lim}
	if err := c.value(0); err != nil {
		return fmt.Errorf("%w: %v", ErrRejected, err)
	}
	return nil
}

type checker struct {
	data    []byte
	pos     int
	entries int
	lim     Limits
}

func (c *checker) remaining() int { return len(c.data) - c.pos }

func (c *checker) need(n int) error {
	if n < 0 || n > c.remaining() {
		return fmt.Errorf("declared length %d at offset %d exceeds the %d bytes left", n, c.pos, c.remaining())
	}
	return nil
}

func (c *checker) skip(n int) error {
	if err := c.need(n); err != nil {
		return err
	}
	c.pos += n
	return nil
}

// length reads an unsigned big-endian length of size bytes.
func (c *checker) length(size int) (int, error) {
	if err := c.need(size); err != nil {
		return 0, err
	}
	b := c.data[c.pos : c.pos+size]
	c.pos += size
	var n uint64
	switch size {
	case 1:
		n = uint64(b[0])
	case 2:
		n = uint64(binary.BigEndian.Uint16(b))
	case 4:
		n = uint64(binary.BigEndian.Uint32(b))
	}
	if n > uint64(c.remaining()) {
		return 0, fmt.Errorf("declared length %d at offset %d exceeds the %d bytes left", n, c.pos, c.remaining())
	}
	return int(n), nil
}

// collection checks n elements (perEntry values each: 1 for arrays, 2 for maps).
func (c *checker) collection(n, perEntry, depth int) error {
	if depth >= c.lim.MaxDepth {
		return fmt.Errorf("nesting deeper than %d levels", c.lim.MaxDepth)
	}
	// Every value takes at least one byte.
	if n*perEntry > c.remaining() {
		return fmt.Errorf("collection of %d entries at offset %d cannot fit in the %d bytes left", n, c.pos, c.remaining())
	}
	c.entries += n
	if c.entries > c.lim.MaxEntries {
		return fmt.Errorf("more than %d collection entries", c.lim.MaxEntries)
	}
	for i := 0; i < n*perEntry; i++ {
		if err := c.value(depth + 1); err != nil {
			return err
		}
	}
	return nil
}

func (c *checker) value(depth int) error {
	if c.remaining() < 1 {
		return fmt.Errorf("unexpected end of data at offset %d", c.pos)
	}
	b := c.data[c.pos]
	c.pos++
	switch {
	case b <= 0x7f, b >= 0xe0: // positive / negative fixint
		return nil
	case b <= 0x8f: // fixmap
		return c.collection(int(b&0x0f), 2, depth)
	case b <= 0x9f: // fixarray
		return c.collection(int(b&0x0f), 1, depth)
	case b <= 0xbf: // fixstr
		return c.skip(int(b & 0x1f))
	}
	switch b {
	case 0xc0, 0xc2, 0xc3: // nil, false, true
		return nil
	case 0xc4, 0xd9: // bin8, str8
		n, err := c.length(1)
		if err != nil {
			return err
		}
		return c.skip(n)
	case 0xc5, 0xda: // bin16, str16
		n, err := c.length(2)
		if err != nil {
			return err
		}
		return c.skip(n)
	case 0xc6, 0xdb: // bin32, str32
		n, err := c.length(4)
		if err != nil {
			return err
		}
		return c.skip(n)
	case 0xc7, 0xc8, 0xc9: // ext8/16/32: length, type byte, data
		size := map[byte]int{0xc7: 1, 0xc8: 2, 0xc9: 4}[b]
		n, err := c.length(size)
		if err != nil {
			return err
		}
		return c.skip(1 + n)
	case 0xca, 0xce, 0xd2: // float32, uint32, int32
		return c.skip(4)
	case 0xcb, 0xcf, 0xd3: // float64, uint64, int64
		return c.skip(8)
	case 0xcc, 0xd0: // uint8, int8
		return c.skip(1)
	case 0xcd, 0xd1: // uint16, int16
		return c.skip(2)
	case 0xd4, 0xd5, 0xd6, 0xd7, 0xd8: // fixext 1/2/4/8/16 (+ type byte)
		return c.skip(1 + map[byte]int{0xd4: 1, 0xd5: 2, 0xd6: 4, 0xd7: 8, 0xd8: 16}[b])
	case 0xdc: // array16
		n, err := c.length(2)
		if err != nil {
			return err
		}
		return c.collection(n, 1, depth)
	case 0xdd: // array32
		n, err := c.length(4)
		if err != nil {
			return err
		}
		return c.collection(n, 1, depth)
	case 0xde: // map16
		n, err := c.length(2)
		if err != nil {
			return err
		}
		return c.collection(n, 2, depth)
	case 0xdf: // map32
		n, err := c.length(4)
		if err != nil {
			return err
		}
		return c.collection(n, 2, depth)
	}
	return fmt.Errorf("invalid format byte 0x%02x at offset %d", b, c.pos-1)
}
