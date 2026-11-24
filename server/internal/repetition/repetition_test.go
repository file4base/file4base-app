package repetition

import (
	"testing"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// Writing some repetitions keeps the others, including those past the
// maximum that a lowered maximum hides.
func TestMergeKeepsRepetitionsPastTheMaximum(t *testing.T) {
	got, err := Merge(dbal.FieldTypeText, 2, `["b","c","d"]`, map[int]any{2: "B"})
	if err != nil || got != `["B","c","d"]` {
		t.Fatalf("merge: %v, %v", got, err)
	}
	got, err = Merge(dbal.FieldTypeText, 3, nil, map[int]any{3: "c"})
	if err != nil || got != `[null,"c"]` {
		t.Fatalf("merge into nothing: %v, %v", got, err)
	}
	if _, err := Merge(dbal.FieldTypeText, 2, `["b","c"]`, map[int]any{3: "x"}); err == nil {
		t.Fatal("a repetition past the maximum is not written")
	}
}
