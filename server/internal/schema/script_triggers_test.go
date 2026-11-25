package schema_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A migrated trigger arrives off and in the layout's trigger list; the
// review lists it with what keeps it from running, enabling one of an event
// File4Base runs moves it into the slot a client runs, and a batch with one
// trigger that cannot run changes nothing at all (#26).
func TestScriptTriggers_ReviewEnableAndDisable(t *testing.T) {
	svc, ctx := transferTestService(t)
	if svc == nil {
		return
	}
	stamp := uniqueTestStamp()
	tbl, err := svc.CreateTable(ctx, "Trigger Review", fmt.Sprintf("trigger_review_%d", stamp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteTable(context.Background(), tbl.ID) })
	occurrences, err := svc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	var occID string
	for _, o := range occurrences {
		if o.BaseTableID == tbl.ID {
			occID = o.ID
		}
	}
	require.NotEmpty(t, occID)

	scriptName := fmt.Sprintf("Greet %d", stamp)
	script, err := svc.CreateScript(ctx, scriptName, tbl.Name, nil, true, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteScript(context.Background(), script.ID) })

	ids := map[string]string{}
	for _, k := range []string{"enter", "exit", "load", "object", "missing", "param"} {
		ids[k] = fmt.Sprintf("trg-%s-%d", k, stamp)
	}
	quarantined := func(id, event string, extra map[string]interface{}) map[string]interface{} {
		m := map[string]interface{}{"id": id, "event": event, "type": "perform_script",
			"script_name": scriptName, "enabled": false, "quarantined_by_migration": true}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	def := map[string]interface{}{
		"objects": []interface{}{map[string]interface{}{"id": "obj-1", "type": "field",
			"field_binding": map[string]interface{}{"field_name": "status"}}},
		"script_triggers": []interface{}{
			quarantined(ids["enter"], "OnLayoutEnter", map[string]interface{}{"parameter": `"hello"`}),
			quarantined(ids["exit"], "OnLayoutExit", nil),
			quarantined(ids["load"], "OnRecordLoad", nil),
			quarantined(ids["object"], "OnObjectSave", map[string]interface{}{"object_id": "obj-1"}),
			quarantined(ids["missing"], "OnLayoutExit", map[string]interface{}{"script_name": "No Such Script"}),
			quarantined(ids["param"], "OnLayoutExit", map[string]interface{}{"parameter_problem": `reads "Gone::field", which the imported tables do not have`}),
		},
	}
	raw, err := json.Marshal(def)
	require.NoError(t, err)
	layout, err := svc.CreateLayout(ctx, fmt.Sprintf("Trigger Review %d", stamp), occID, raw)
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteLayout(context.Background(), layout.ID) })

	listed := func() map[string]schema.ScriptTrigger {
		all, err := svc.ListScriptTriggers(ctx)
		require.NoError(t, err)
		out := map[string]schema.ScriptTrigger{}
		for _, tr := range all {
			if tr.LayoutID == layout.ID {
				out[tr.ID] = tr
			}
		}
		return out
	}
	got := listed()
	require.Len(t, got, 6)
	enter := got[ids["enter"]]
	assert.False(t, enter.Enabled)
	assert.True(t, enter.QuarantinedByMigration)
	assert.True(t, enter.CanEnable)
	assert.Equal(t, script.ID, enter.ScriptID, "bound to the script by its name")
	assert.Empty(t, enter.Problems)
	codes := func(tr schema.ScriptTrigger) []string {
		var out []string
		for _, p := range tr.Problems {
			out = append(out, p.Code)
			assert.NotEmpty(t, p.Message)
			assert.NotEmpty(t, p.Remedy)
		}
		return out
	}
	assert.Equal(t, []string{schema.TriggerProblemUnsupportedEvent}, codes(got[ids["load"]]))
	assert.Equal(t, "object", got[ids["object"]].Scope)
	assert.Equal(t, "obj-1", got[ids["object"]].ObjectID)
	assert.Equal(t, "status", got[ids["object"]].ObjectLabel)
	assert.Equal(t, []string{schema.TriggerProblemUnsupportedEvent}, codes(got[ids["object"]]))
	assert.Equal(t, []string{schema.TriggerProblemUnresolvedScript}, codes(got[ids["missing"]]))
	assert.Equal(t, []string{schema.TriggerProblemParameter}, codes(got[ids["param"]]))

	// One trigger that cannot run refuses the whole batch.
	_, err = svc.SetScriptTriggersEnabled(ctx, []string{ids["enter"], ids["load"]}, true)
	require.ErrorIs(t, err, schema.ErrScriptTriggerCannotEnable)
	_, err = svc.SetScriptTriggersEnabled(ctx, []string{ids["enter"], "no-such-trigger"}, true)
	require.ErrorIs(t, err, schema.ErrScriptTriggerNotFound)
	stored, err := svc.GetLayout(ctx, layout.ID)
	require.NoError(t, err)
	assert.NotContains(t, string(stored.Definition), "on_layout_enter", "nothing was changed")
	assert.False(t, listed()[ids["enter"]].Enabled)

	before := stored.UpdatedAt
	update, err := svc.SetScriptTriggersEnabled(ctx, []string{ids["enter"], ids["exit"]}, true)
	require.NoError(t, err)
	assert.Equal(t, 2, update.Updated)
	assert.Equal(t, []string{layout.ID}, update.Layouts)

	stored, err = svc.GetLayout(ctx, layout.ID)
	require.NoError(t, err)
	assert.True(t, stored.UpdatedAt.After(before) || stored.UpdatedAt.Equal(before), "the layout's version moves on")
	var after map[string]interface{}
	require.NoError(t, json.Unmarshal(stored.Definition, &after))
	slot, ok := after["on_layout_enter"].(map[string]interface{})
	require.True(t, ok, "an enabled OnLayoutEnter trigger is in the slot a client runs")
	assert.Equal(t, script.ID, slot["script_id"])
	assert.Equal(t, true, slot["enabled"])
	assert.Equal(t, `"hello"`, slot["parameter"])
	assert.NotContains(t, slot, "quarantined_by_migration", "reviewed")
	assert.Len(t, after["script_triggers"], 4)

	got = listed()
	assert.True(t, got[ids["enter"]].Enabled)
	assert.False(t, got[ids["enter"]].QuarantinedByMigration)
	assert.Contains(t, codes(got[ids["param"]]), schema.TriggerProblemSlotTaken, "OnLayoutExit is taken now")

	// Disabling moves it back into the list, under the same id.
	_, err = svc.SetScriptTriggersEnabled(ctx, []string{ids["enter"]}, false)
	require.NoError(t, err)
	stored, err = svc.GetLayout(ctx, layout.ID)
	require.NoError(t, err)
	after = nil
	require.NoError(t, json.Unmarshal(stored.Definition, &after))
	assert.NotContains(t, after, "on_layout_enter")
	got = listed()
	assert.False(t, got[ids["enter"]].Enabled)
	assert.False(t, got[ids["enter"]].QuarantinedByMigration)
	assert.True(t, got[ids["enter"]].CanEnable)
}
