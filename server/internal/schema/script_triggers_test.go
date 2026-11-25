package schema_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	// Written as an update: a created layout gets trigger ids of its own.
	layout, err := svc.CreateLayout(ctx, fmt.Sprintf("Trigger Review %d", stamp), occID, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteLayout(context.Background(), layout.ID) })
	_, err = svc.UpdateLayout(ctx, layout.ID, layout.Name, raw)
	require.NoError(t, err)

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

// A new layout's triggers get ids of their own; triggers that still share an
// id are reported and cannot be switched by it; a script of another file or
// a name two scripts share binds to nothing; and switching a trigger leaves
// every number of the definition as it was written (#26).
func TestScriptTriggers_IDsAmbiguityAndNumbers(t *testing.T) {
	svc, ctx := transferTestService(t)
	if svc == nil {
		return
	}
	stamp := uniqueTestStamp()
	tbl, err := svc.CreateTable(ctx, "Trigger Ids", fmt.Sprintf("trigger_ids_%d", stamp))
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
	greetName, twinName := fmt.Sprintf("Greet %d", stamp), fmt.Sprintf("Twin %d", stamp)
	for _, name := range []string{greetName, twinName, twinName} {
		sc, err := svc.CreateScript(ctx, name, tbl.Name, nil, true, nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = svc.DeleteScript(context.Background(), sc.ID) })
	}

	const big, fine = `12345678901234567890123`, `0.1000000000000000055511151231257827`
	definition := func(triggers string) json.RawMessage {
		return json.RawMessage(`{"objects":[],"precision":` + big + `,"ratio":` + fine + `,"script_triggers":[` + triggers + `]}`)
	}
	trigger := func(id, event, script, extra string) string {
		return fmt.Sprintf(`{"id":%q,"event":%q,"type":"perform_script","script_name":%q,"enabled":false,"quarantined_by_migration":true%s}`, id, event, script, extra)
	}
	triggers := trigger("t-greet", "OnLayoutEnter", greetName, "") + "," +
		trigger("t-twin", "OnLayoutExit", twinName, "") + "," +
		trigger("t-other", "OnLayoutExit", greetName, `,"file_reference":"Seed Bank"`)
	a, err := svc.CreateLayout(ctx, fmt.Sprintf("Ids A %d", stamp), occID, definition(triggers))
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteLayout(context.Background(), a.ID) })
	b, err := svc.CreateLayout(ctx, fmt.Sprintf("Ids B %d", stamp), occID, definition(triggers))
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteLayout(context.Background(), b.ID) })

	listed := func(layoutID string) map[string]schema.ScriptTrigger {
		all, err := svc.ListScriptTriggers(ctx)
		require.NoError(t, err)
		out := map[string]schema.ScriptTrigger{}
		for _, tr := range all {
			if tr.LayoutID == layoutID {
				out[tr.Event+"/"+tr.ScriptName] = tr
			}
		}
		return out
	}
	inA, inB := listed(a.ID), listed(b.ID)
	require.Len(t, inA, 3)
	for key, tr := range inA {
		assert.NotContains(t, []string{"t-greet", "t-twin", "t-other"}, tr.ID, "a created layout's triggers get new ids (%s)", key)
		assert.NotEqual(t, tr.ID, inB[key].ID, "a copy's triggers are not the original's (%s)", key)
	}
	codes := func(tr schema.ScriptTrigger) []string {
		var out []string
		for _, p := range tr.Problems {
			out = append(out, p.Code)
		}
		return out
	}
	twin := inA["OnLayoutExit/"+twinName]
	assert.Empty(t, twin.ScriptID)
	assert.Equal(t, []string{schema.TriggerProblemAmbiguousScript}, codes(twin))
	other := inA["OnLayoutExit/"+greetName]
	assert.Empty(t, other.ScriptID, "a script of another file is not this solution's script of the same name")
	assert.Equal(t, []string{schema.TriggerProblemExternalScript}, codes(other))
	greet := inA["OnLayoutEnter/"+greetName]
	assert.True(t, greet.CanEnable)

	// Switching a trigger writes every other value back as it was.
	_, err = svc.SetScriptTriggersEnabled(ctx, []string{greet.ID}, true)
	require.NoError(t, err)
	stored, err := svc.GetLayout(ctx, a.ID)
	require.NoError(t, err)
	assert.Contains(t, string(stored.Definition), `"precision":`+big)
	assert.Contains(t, string(stored.Definition), `"ratio":`+fine)

	// A definition written with another layout's trigger ids — as copies
	// made before were — makes those ids name two triggers.
	copied, err := svc.GetLayout(ctx, b.ID)
	require.NoError(t, err)
	var def map[string]interface{}
	require.NoError(t, json.Unmarshal(copied.Definition, &def))
	for _, raw := range def["script_triggers"].([]interface{}) {
		e := raw.(map[string]interface{})
		if e["script_name"] == twinName {
			e["id"] = twin.ID
		}
	}
	raw, err := json.Marshal(def)
	require.NoError(t, err)
	_, err = svc.UpdateLayout(ctx, b.ID, copied.Name, raw)
	require.NoError(t, err)
	assert.Contains(t, codes(listed(a.ID)["OnLayoutExit/"+twinName]), schema.TriggerProblemDuplicateID)
	_, err = svc.SetScriptTriggersEnabled(ctx, []string{twin.ID}, false)
	require.ErrorIs(t, err, schema.ErrScriptTriggerDuplicateID, "an id naming two triggers is refused, not guessed")
}

// Anyone who is neither an owner nor an administrator saves a layout with its
// triggers as they were: no stored trigger changed, none switched on (#26).
func TestGuardScriptTriggerEdits(t *testing.T) {
	stored := `{"objects":[],"on_layout_exit":{"type":"perform_script","script_id":"s9","script_name":"Bye"},` +
		`"script_triggers":[{"id":"m1","event":"OnLayoutEnter","type":"perform_script","script_id":"s1","script_name":"Greet","parameter":"1","enabled":false,"quarantined_by_migration":true}]}`
	cases := []struct {
		name, next string
		user       bool // allowed for a user
	}{
		{"unchanged", stored, true},
		{"another edit", strings.Replace(stored, `"objects":[]`, `"objects":[{"id":"o1"}]`, 1), true},
		{"a client dropping null values", strings.Replace(stored, `"script_name":"Bye"`, `"script_name":"Bye","parameter":null,"params":{}`, 1), true},
		{"a new script for OnLayoutExit", strings.Replace(stored, `"script_id":"s9","script_name":"Bye"`, `"script_id":"s2","script_name":"Other"`, 1), true},
		{"the migrated trigger moved into its slot", `{"objects":[],"on_layout_exit":{"type":"perform_script","script_id":"s9","script_name":"Bye"},` +
			`"on_layout_enter":{"id":"m1","event":"OnLayoutEnter","type":"perform_script","script_id":"s1","script_name":"Greet","parameter":"1","enabled":true}}`, false},
		{"the migrated trigger switched on in place", strings.Replace(stored, `"enabled":false`, `"enabled":true`, 1), false},
		{"its script and parameter set in the slot", strings.Replace(stored, `"objects":[]`, `"objects":[],"on_layout_enter":{"type":"perform_script","script_id":"s1","script_name":"Greet","parameter":"1"}`, 1), false},
		{"a quarantined trigger in a slot", strings.Replace(stored, `"script_name":"Bye"`, `"script_name":"Bye","quarantined_by_migration":true`, 1), false},
	}
	for _, c := range cases {
		_, err := schema.GuardScriptTriggerEdits(json.RawMessage(stored), json.RawMessage(c.next), false)
		if c.user {
			assert.NoError(t, err, c.name)
		} else {
			assert.ErrorIs(t, err, schema.ErrScriptTriggersAdminOnly, c.name)
		}
		_, err = schema.GuardScriptTriggerEdits(json.RawMessage(stored), json.RawMessage(c.next), true)
		assert.NoError(t, err, "an administrator may (%s)", c.name)
	}

	// A client that does not know the list keeps it.
	kept, err := schema.GuardScriptTriggerEdits(json.RawMessage(stored), json.RawMessage(`{"objects":[]}`), false)
	require.NoError(t, err)
	assert.Contains(t, string(kept), `"m1"`)
}
