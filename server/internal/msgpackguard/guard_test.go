package msgpackguard_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/msgpackguard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

func TestCheck_AcceptsWellFormedDocuments(t *testing.T) {
	doc := map[string]interface{}{
		"format":  "file4base_data",
		"created": time.Date(2026, 10, 6, 9, 0, 0, 123456000, time.UTC),
		"tables": map[string]interface{}{
			"items": []map[string]interface{}{
				{"id": "a", "n": int64(-5), "big": uint64(1 << 63), "f": 1.5, "f32": float32(2.5), "ok": true, "none": nil},
				{"id": "b", "payload": []byte{0, 255, 128, 92, 10}, "text": strings.Repeat("x", 70000)},
			},
		},
		"nested": []interface{}{[]interface{}{[]interface{}{map[string]interface{}{"deep": 1}}}},
		"wide":   make([]interface{}, 70000),
	}
	data, err := msgpack.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, msgpackguard.Check(data, msgpackguard.Limits{}))

	for _, v := range []interface{}{nil, true, 0, -1, 127, -33, 300, -300, 70000, 1 << 40, "", "abc", []byte{}, []interface{}{}, map[string]int{}} {
		b, err := msgpack.Marshal(v)
		require.NoError(t, err)
		assert.NoError(t, msgpackguard.Check(b, msgpackguard.Limits{}), "%#v", v)
	}
}

func TestCheck_RejectsOversizedDeclarations(t *testing.T) {
	cases := map[string][]byte{
		"array32 of 4 billion in 5 bytes":  {0xdd, 0xff, 0xff, 0xff, 0xff},
		"map32 of 4 billion pairs":         {0xdf, 0xff, 0xff, 0xff, 0xff, 0xc0, 0xc0},
		"array16 longer than the data":     {0xdc, 0x00, 0x10, 0xc0, 0xc0},
		"map with more pairs than bytes":   {0x83, 0xa1, 'a', 0xc0},
		"str32 of 4 GiB":                   {0xdb, 0xff, 0xff, 0xff, 0xff, 'a'},
		"bin32 of 4 GiB":                   {0xc6, 0xff, 0xff, 0xff, 0xff},
		"ext32 of 4 GiB":                   {0xc9, 0xff, 0xff, 0xff, 0xff, 0x01},
		"array32 nested in a valid map":    append([]byte{0x81, 0xa4, 'r', 'o', 'w', 's'}, 0xdd, 0x7f, 0xff, 0xff, 0xff),
		"truncated int64":                  {0xd3, 0x00, 0x01},
		"unused format byte":               {0xc1},
		"empty input":                      {},
	}
	for name, data := range cases {
		err := msgpackguard.Check(data, msgpackguard.Limits{})
		assert.ErrorIs(t, err, msgpackguard.ErrRejected, name)
	}
}

func TestCheck_EnforcesDepthAndEntryLimits(t *testing.T) {
	deep := append(bytes.Repeat([]byte{0x91}, 40), 0xc0) // 40 nested one-element arrays
	assert.ErrorIs(t, msgpackguard.Check(deep, msgpackguard.Limits{}), msgpackguard.ErrRejected)
	assert.NoError(t, msgpackguard.Check(deep, msgpackguard.Limits{MaxDepth: 64}))

	many, err := msgpack.Marshal(make([]interface{}, 1000))
	require.NoError(t, err)
	assert.ErrorIs(t, msgpackguard.Check(many, msgpackguard.Limits{MaxEntries: 999}), msgpackguard.ErrRejected)
	assert.NoError(t, msgpackguard.Check(many, msgpackguard.Limits{MaxEntries: 1000}))
}
