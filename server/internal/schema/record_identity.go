package schema

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/file4base/file4base-app/server/internal/data"
)

// RecordIdentityKey is the reserved record key under which a data file
// (DatabaseDataBundle) carries a record's identity (#208): its record ID and
// its modification count, as a map with the keys "record_id" and "mod_id".
// A restore keeps them, and a migration gives the records FileMaker's. Field
// names start with a letter, so it can never name a field.
const RecordIdentityKey = "__record_identity"

// SetRecordIdentity puts a record's identity in a record of a data file. A
// recordID of 0 says the record ID is not known: the record is given a new
// one.
func SetRecordIdentity(record map[string]interface{}, recordID, modID int64) {
	identity := map[string]interface{}{"mod_id": modID}
	if recordID > 0 {
		identity["record_id"] = recordID
	}
	record[RecordIdentityKey] = identity
}

// recordIdentityFromRecord removes RecordIdentityKey from a record being
// restored and returns the identity it carried, a record ID of 0 when none.
func recordIdentityFromRecord(record map[string]interface{}) (data.Identity, error) {
	raw, ok := record[RecordIdentityKey]
	if !ok {
		return data.Identity{}, nil
	}
	delete(record, RecordIdentityKey)
	if raw == nil {
		return data.Identity{}, nil
	}
	var identity data.Identity
	fields, ok := raw.(map[string]interface{})
	if !ok {
		return identity, fmt.Errorf("%s must map record_id and mod_id to numbers", RecordIdentityKey)
	}
	for key, value := range fields {
		n, ok := wholeNumber(value)
		switch {
		case key != "record_id" && key != "mod_id":
			return identity, fmt.Errorf("%s has an unknown key %q", RecordIdentityKey, key)
		case !ok || n < 0 || (key == "record_id" && n == 0):
			return identity, fmt.Errorf("%s.%s must be a whole number above 0", RecordIdentityKey, key)
		case key == "record_id":
			identity.RecordID = n
		default:
			identity.ModID = n
		}
	}
	return identity, nil
}

// wholeNumber reads a number a data file holds, however MessagePack typed it.
func wholeNumber(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case uint8:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint64:
		if n > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	case uint:
		if uint64(n) > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	case float64:
		if n != math.Trunc(n) || n > math.MaxInt64 || n < math.MinInt64 {
			return 0, false
		}
		return int64(n), true
	case float32:
		return wholeNumber(float64(n))
	case string:
		parsed, err := strconv.ParseInt(n, 10, 64)
		return parsed, err == nil
	case []byte:
		return wholeNumber(string(n))
	}
	return 0, false
}

// restoredRecordIDs decides the record ID each record of a table being
// restored is written with: the one it carries, unless the table has given
// it already (to a record since deleted, or to another), or another record
// of the file carries it too; a new one otherwise. A record ID is never
// given twice, so a backup restored into the database it was taken from
// keeps none of the IDs its records still hold there. It returns the IDs, by
// record index, and how many records carried an ID they could not keep.
//
// The table's allocator is moved past the IDs kept before any record is
// written, so a record created meanwhile does not take one of them.
func (s *Service) restoredRecordIDs(ctx context.Context, tableName string, carried []data.Identity) ([]int64, int, error) {
	if has, err := data.HasCreationOrder(ctx, s.driver, tableName); err != nil || !has {
		return nil, 0, err // a table not numbered yet: the engine numbers its records
	}
	given, err := data.RecordIDsGiven(ctx, s.driver, tableName)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]int64, len(carried))
	kept := map[int64]bool{}
	var last int64
	renumbered, fresh := 0, 0
	for i, identity := range carried {
		switch {
		case identity.RecordID > given && !kept[identity.RecordID]:
			ids[i] = identity.RecordID
			kept[identity.RecordID] = true
			last = max(last, identity.RecordID)
		case identity.RecordID > 0:
			renumbered++
			fresh++
		default:
			fresh++
		}
	}
	if err := data.AdvanceRecordIDs(ctx, s.driver, tableName, last); err != nil {
		return nil, 0, err
	}
	reserved, err := data.ReserveRecordIDs(ctx, s.driver, tableName, fresh)
	if err != nil {
		return nil, 0, err
	}
	if len(reserved) < fresh {
		return nil, 0, fmt.Errorf("failed reserving the record IDs of %s", tableName)
	}
	for i := range ids {
		if ids[i] == 0 {
			ids[i], reserved = reserved[0], reserved[1:]
		}
	}
	return ids, renumbered, nil
}
