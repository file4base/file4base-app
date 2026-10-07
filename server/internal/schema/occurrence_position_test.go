package schema

import "testing"

// Table occurrences used to be created at the sys_table_occurrences column
// default (100, 100), so every card on the relationships graph sat on top of
// the previous one and a database with several tables looked as if it had one.
func TestOccurrencePositionNeverRepeats(t *testing.T) {
	seen := map[[2]float64]int{}
	for i := 0; i < 20; i++ {
		x, y := occurrencePosition(i)
		key := [2]float64{x, y}
		if prev, dup := seen[key]; dup {
			t.Fatalf("occurrence %d lands on occurrence %d at (%v, %v)", i, prev, x, y)
		}
		seen[key] = i
	}
}

func TestOccurrencePositionStartsAtTheOrigin(t *testing.T) {
	x, y := occurrencePosition(0)
	if x != 100 || y != 100 {
		t.Fatalf("first occurrence = (%v, %v), want (100, 100)", x, y)
	}
}

func TestOccurrencePositionWrapsToANewRow(t *testing.T) {
	_, firstRowY := occurrencePosition(0)
	_, fifthY := occurrencePosition(4)
	if fifthY <= firstRowY {
		t.Fatalf("the fifth occurrence stays on the first row: y=%v (first row y=%v)", fifthY, firstRowY)
	}

	fourthX, fourthY := occurrencePosition(3)
	fifthX, _ := occurrencePosition(4)
	if fifthX >= fourthX {
		t.Fatalf("the fifth occurrence did not go back to the left: x=%v (fourth x=%v)", fifthX, fourthX)
	}
	if fourthY != firstRowY {
		t.Fatalf("the fourth occurrence left the first row: y=%v", fourthY)
	}
}

// The cards the client draws are 260 wide, so two occurrences in the same row
// must be further apart than that or they overlap.
func TestOccurrencePositionLeavesRoomForTheCard(t *testing.T) {
	const cardWidth = 260.0
	x0, _ := occurrencePosition(0)
	x1, _ := occurrencePosition(1)
	if x1-x0 <= cardWidth {
		t.Fatalf("columns are %v apart, which does not clear a %v-wide card", x1-x0, cardWidth)
	}
}
