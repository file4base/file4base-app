package data_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A field of an occurrence two relationships away from a layout's shows the
// record FileMaker shows: the first of all those reached, the one an unstored
// calculation reading that field across the same path reads.
func TestFirstRelatedAlong(t *testing.T) {
	for _, sorted := range []bool{false, true} {
		t.Run(fmt.Sprintf("sorted=%t", sorted), func(t *testing.T) {
			ctx, a, _ := twoSolutions(t, fmt.Sprintf("xfirst%t", sorted), fmt.Sprintf("xfirst%t_unused", sorted))
			groups, groupsOcc, groupFields := a.table(t, ctx, "groups", [2]string{"code", "TEXT"})
			_, membersOcc, memberFields := a.table(t, ctx, "members", [2]string{"code", "TEXT"}, [2]string{"key", "TEXT"})
			_, itemsOcc, itemFields := a.table(t, ctx, "items", [2]string{"key", "TEXT"}, [2]string{"label", "TEXT"},
				[2]string{"rank", "NUMBER"})
			a.relate(t, ctx, schema.CreateRelationshipInput{Name: "groups_members",
				LeftOccurrenceID: groupsOcc, RightOccurrenceID: membersOcc,
				Predicates: []schema.RelationshipPredicate{{LeftColumnID: groupFields["code"], Operator: "=", RightColumnID: memberFields["code"]}}})
			var sort *string
			if sorted {
				s := "rank"
				sort = &s
			}
			a.relate(t, ctx, schema.CreateRelationshipInput{Name: "members_items", SortRelated: sort,
				LeftOccurrenceID: membersOcc, RightOccurrenceID: itemsOcc,
				Predicates: []schema.RelationshipPredicate{{LeftColumnID: memberFields["key"], Operator: "=", RightColumnID: itemFields["key"]}}})
			a.unstored(t, ctx, groups, "first", "items::label", "Text")
			svc := data.NewService(a.driver)
			insert := func(table string, record map[string]interface{}) string {
				row, err := svc.InsertRow(ctx, table, record)
				require.NoError(t, err)
				return fmt.Sprint(row["id"])
			}
			insert("members", map[string]interface{}{"code": "g", "key": "ma"})
			insert("members", map[string]interface{}{"code": "g", "key": "mb"})
			insert("members", map[string]interface{}{"code": "h", "key": "mc"})
			for i, item := range []struct {
				key  string
				rank int
			}{{"mb", 4}, {"ma", 3}, {"mb", 2}, {"ma", 2}, {"mc", 1}} {
				insert("items", map[string]interface{}{"key": item.key, "label": fmt.Sprintf("i%d", i+1), "rank": item.rank})
			}
			g := insert("groups", map[string]interface{}{"code": "g"})
			h := insert("groups", map[string]interface{}{"code": "h"})
			lonely := insert("groups", map[string]interface{}{"code": "nobody"})

			got, err := svc.FirstRelatedAlong(ctx, "groups", "groups", []string{"members", "items"}, []string{g, h, lonely, "missing"})
			require.NoError(t, err)
			require.Len(t, got, 4)
			row, err := svc.GetRow(ctx, "groups", g)
			require.NoError(t, err)
			require.NotNil(t, got[g])
			assert.Equal(t, row["first"], got[g]["label"], "the record the calculation reads")
			want := "i1"
			if sorted {
				want = "i3"
			}
			assert.Equal(t, want, got[g]["label"])
			require.NotNil(t, got[h])
			assert.Equal(t, "i5", got[h]["label"])
			assert.Nil(t, got[lonely])
			assert.Nil(t, got["missing"])

			// The record given is a whole record, which is what is edited.
			_, err = svc.UpdateRow(ctx, "items", fmt.Sprint(got[h]["id"]), map[string]interface{}{"label": "edited"})
			require.NoError(t, err)
			again, err := svc.FirstRelatedAlong(ctx, "groups", "", []string{"members", "items"}, []string{h})
			require.NoError(t, err)
			assert.Equal(t, "edited", again[h]["label"])

			// The path is followed as given, never searched for.
			_, err = svc.FirstRelatedAlong(ctx, "groups", "groups", []string{"items"}, []string{g})
			assert.True(t, errors.Is(err, data.ErrRelationshipNotFound), "%v", err)
		})
	}
}
