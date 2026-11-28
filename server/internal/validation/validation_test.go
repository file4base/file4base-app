package validation

import (
	"context"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// A time field File4Base keeps as a timestamp holds a time of day.
func TestTimeOfDayAcceptsATimestampValue(t *testing.T) {
	c := &Checker{Table: "slots", Fields: map[string]Field{
		"starts": {Name: "starts", Type: dbal.FieldTypeTimestamp, Rules: &Rules{StrictTypeEnabled: true, StrictType: "Time of Day"}},
	}}
	for _, v := range []interface{}{time.Date(1970, 1, 1, 9, 30, 0, 0, time.UTC), "09:30", "09:30:15"} {
		if err := c.Check(context.Background(), "", map[string]interface{}{"starts": v}, nil, true); err != nil {
			t.Errorf("%v: %v", v, err)
		}
	}
	if err := c.Check(context.Background(), "", map[string]interface{}{"starts": "half past nine"}, nil, true); err == nil {
		t.Error("a text that is no time of day passes")
	}
}
