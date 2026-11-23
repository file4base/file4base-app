package migration

import (
	"fmt"
	"strings"
	"unicode/utf16"
)

// decodeHBAM7Text implements Unicode Technical Standard #6 after removing the
// FileMaker text XOR encoding. Unlike the earlier reader, it preserves tabs,
// whitespace, line endings, supplementary characters and typographic ligatures.
func decodeHBAM7Text(raw []byte) (string, error) {
	b := make([]byte, len(raw))
	for i, v := range raw {
		b[i] = v ^ 0x5a
	}
	static := [8]rune{0, 0x80, 0x100, 0x300, 0x2000, 0x2080, 0x2100, 0x3000}
	dynamic := [8]rune{0x80, 0xc0, 0x400, 0x600, 0x900, 0x3040, 0x30a0, 0xff00}
	window, unicodeMode := 0, false
	var out strings.Builder
	var high rune
	emit := func(r rune) error {
		if r >= 0xd800 && r <= 0xdbff {
			if high != 0 {
				return fmt.Errorf("unpaired surrogate")
			}
			high = r
			return nil
		}
		if high != 0 {
			if r < 0xdc00 || r > 0xdfff {
				return fmt.Errorf("unpaired surrogate")
			}
			r = utf16.DecodeRune(high, r)
			high = 0
		} else if r >= 0xdc00 && r <= 0xdfff {
			return fmt.Errorf("unpaired surrogate")
		}
		if r > 0x10ffff {
			return fmt.Errorf("invalid Unicode scalar")
		}
		out.WriteRune(r)
		return nil
	}
	offset := func(v byte) (rune, error) {
		switch {
		case v >= 1 && v < 0x68:
			return rune(v) * 128, nil
		case v >= 0x68 && v < 0xa8:
			return rune(v)*128 + 0xac00, nil
		case v >= 0xf9:
			return [...]rune{0xc0, 0x250, 0x370, 0x530, 0x3040, 0x30a0, 0xff60}[v-0xf9], nil
		}
		return 0, fmt.Errorf("reserved Unicode window")
	}
	for i := 0; i < len(b); {
		c := b[i]
		i++
		take := func(n int) ([]byte, error) {
			if n > len(b)-i {
				return nil, fmt.Errorf("truncated Unicode sequence")
			}
			p := b[i : i+n]
			i += n
			return p, nil
		}
		var value rune
		if unicodeMode {
			switch {
			case c >= 0xe0 && c <= 0xe7:
				window = int(c - 0xe0)
				unicodeMode = false
				continue
			case c >= 0xe8 && c <= 0xef:
				p, e := take(1)
				if e != nil {
					return "", e
				}
				v, e := offset(p[0])
				if e != nil {
					return "", e
				}
				window = int(c - 0xe8)
				dynamic[window] = v
				unicodeMode = false
				continue
			case c == 0xf1:
				p, e := take(2)
				if e != nil {
					return "", e
				}
				window = int(p[0] >> 5)
				dynamic[window] = 0x10000 + 128*(rune(p[0]&31)<<8+rune(p[1]))
				unicodeMode = false
				continue
			case c == 0xf0:
				p, e := take(2)
				if e != nil {
					return "", e
				}
				value = rune(p[0])<<8 | rune(p[1])
			case c == 0xf2:
				return "", fmt.Errorf("reserved Unicode command")
			default:
				p, e := take(1)
				if e != nil {
					return "", e
				}
				value = rune(c)<<8 | rune(p[0])
			}
		} else {
			switch {
			case c >= 1 && c <= 8:
				p, e := take(1)
				if e != nil {
					return "", e
				}
				w := int(c - 1)
				if p[0] < 128 {
					value = static[w] + rune(p[0])
				} else {
					value = dynamic[w] + rune(p[0]-128)
				}
			case c == 0x0b:
				p, e := take(2)
				if e != nil {
					return "", e
				}
				window = int(p[0] >> 5)
				dynamic[window] = 0x10000 + 128*(rune(p[0]&31)<<8+rune(p[1]))
				continue
			case c == 0x0e:
				p, e := take(2)
				if e != nil {
					return "", e
				}
				value = rune(p[0])<<8 | rune(p[1])
			case c == 0x0f:
				unicodeMode = true
				continue
			case c >= 0x10 && c <= 0x17:
				window = int(c - 0x10)
				continue
			case c >= 0x18 && c <= 0x1f:
				p, e := take(1)
				if e != nil {
					return "", e
				}
				v, e := offset(p[0])
				if e != nil {
					return "", e
				}
				window = int(c - 0x18)
				dynamic[window] = v
				continue
			case c == 0 || c == 9 || c == 10 || c == 13 || c >= 0x20 && c < 0x80:
				value = rune(c)
			case c >= 0x80:
				value = dynamic[window] + rune(c-128)
			default:
				return "", fmt.Errorf("reserved Unicode command")
			}
		}
		if e := emit(value); e != nil {
			return "", e
		}
	}
	if high != 0 {
		return "", fmt.Errorf("unpaired surrogate")
	}
	return out.String(), nil
}
