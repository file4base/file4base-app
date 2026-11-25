package schema

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/google/uuid"
)

// Script triggers of a solution's layouts, and their review (#26).
//
// A layout definition holds its triggers in two places:
//
//   - on_layout_enter and on_layout_exit, the slots a client runs: an action
//     there runs in Browse mode when the layout is shown or left, unless it
//     carries "enabled": false;
//   - "script_triggers", a list of every other trigger: the layout's record
//     and mode triggers (OnRecordLoad, OnRecordCommit, …), those set on a
//     layout object (an entry with "object_id", or the FileMaker "object" it
//     was set on when that object was not migrated), those switched off and
//     those of events File4Base does not run. A client runs an entry of the
//     list only when it carries "enabled": true and its event is one it runs
//     (ListTriggerEventRuns, ObjectTriggerEventRuns), so a client that does
//     not know the list never runs anything from it.
//
// A migrated trigger always arrives in the list, switched off and marked
// quarantined_by_migration, so a client that does not know the enabled flag
// still never runs it. Enabling an OnLayoutEnter or OnLayoutExit trigger
// moves it into its slot, and disabling one moves it back into the list;
// every other trigger is switched where it is, in the list. A trigger
// File4Base could not run as it stands — no script, a parameter that does
// not read, an event it does not run, an object that is not on the layout —
// cannot be enabled, so "enabled" never says a trigger runs when it does not.
// One script runs per event of a layout, or of one of its objects.

// LayoutScriptTriggersKey is the layout definition key of the trigger list.
const LayoutScriptTriggersKey = "script_triggers"

// LayoutTriggerSlot is a layout definition key a client runs a trigger from.
type LayoutTriggerSlot struct {
	Key   string
	Event string
}

// LayoutTriggerSlots are the layout triggers File4Base runs.
var LayoutTriggerSlots = []LayoutTriggerSlot{
	{Key: "on_layout_enter", Event: "OnLayoutEnter"},
	{Key: "on_layout_exit", Event: "OnLayoutExit"},
}

// ListTriggerEvents are the layout events File4Base runs from the trigger
// list, in Browse mode: OnRecordLoad after a record becomes the current one,
// OnRecordCommit before a changed record is committed (a script result of
// False keeps it uncommitted), OnRecordRevert before it is reverted (False
// keeps the changes), OnModeEnter after the window enters a mode and
// OnModeExit before it leaves one (False stays in the mode).
var ListTriggerEvents = []string{"OnRecordLoad", "OnRecordCommit", "OnRecordRevert", "OnModeEnter", "OnModeExit"}

// ObjectTriggerEvents are the events of a field object's triggers File4Base
// runs: OnObjectEnter after the field becomes active, OnObjectModify after
// its content changes, OnObjectValidate before a changed value is validated
// and saved (False keeps it unsaved, in the active field), OnObjectSave after
// it is saved (False keeps the field active) and OnObjectExit before the
// field is left (False keeps it active).
var ObjectTriggerEvents = []string{"OnObjectEnter", "OnObjectExit", "OnObjectModify", "OnObjectValidate", "OnObjectSave"}

// findModeTriggerEvents are the events File4Base runs in Find mode as well,
// for a trigger FileMaker runs there.
var findModeTriggerEvents = []string{"OnObjectEnter", "OnObjectExit", "OnObjectModify", "OnModeEnter", "OnModeExit"}

// LayoutTriggerEventRuns reports whether File4Base runs a layout trigger of
// the given FileMaker event.
func LayoutTriggerEventRuns(event string) bool {
	return slotForEvent(event) != nil || ListTriggerEventRuns(event)
}

// ListTriggerEventRuns reports whether File4Base runs a layout trigger of
// the event from the trigger list, where it is switched in place.
func ListTriggerEventRuns(event string) bool {
	return containsFold(ListTriggerEvents, event)
}

// ObjectTriggerEventRuns reports whether File4Base runs a trigger of the
// event set on a field object.
func ObjectTriggerEventRuns(event string) bool {
	return containsFold(ObjectTriggerEvents, event)
}

// TriggerEventRuns reports whether File4Base runs a trigger of the event,
// set on a layout object or on the layout itself.
func TriggerEventRuns(event string, onObject bool) bool {
	if onObject {
		return ObjectTriggerEventRuns(event)
	}
	return LayoutTriggerEventRuns(event)
}

// TriggerEventRunsInFind reports whether File4Base runs triggers of the
// event in Find mode as well, for a trigger FileMaker runs there.
func TriggerEventRunsInFind(event string) bool {
	return containsFold(findModeTriggerEvents, event)
}

func containsFold(list []string, value string) bool {
	value = strings.TrimSpace(value)
	for _, v := range list {
		if strings.EqualFold(v, value) {
			return true
		}
	}
	return false
}

func slotForEvent(event string) *LayoutTriggerSlot {
	for i := range LayoutTriggerSlots {
		if strings.EqualFold(LayoutTriggerSlots[i].Event, strings.TrimSpace(event)) {
			return &LayoutTriggerSlots[i]
		}
	}
	return nil
}

// ScriptTrigger is one trigger of a layout or of one of its objects, as the
// review lists it.
type ScriptTrigger struct {
	ID         string `json:"id"`
	LayoutID   string `json:"layout_id"`
	LayoutName string `json:"layout_name"`
	// Scope is "layout" or "object".
	Scope       string `json:"scope"`
	ObjectID    string `json:"object_id,omitempty"`
	ObjectLabel string `json:"object_label,omitempty"`
	ObjectType  string `json:"object_type,omitempty"`
	Event       string `json:"event"`
	ScriptID    string `json:"script_id,omitempty"`
	ScriptName  string `json:"script_name,omitempty"`
	Parameter   string `json:"parameter,omitempty"`
	Enabled     bool   `json:"enabled"`
	// QuarantinedByMigration is set while a migrated trigger has not been
	// enabled since it was imported.
	QuarantinedByMigration bool                   `json:"quarantined_by_migration"`
	Problems               []ScriptTriggerProblem `json:"problems"`
	// CanEnable is true when the trigger is off and nothing keeps it from
	// running once enabled.
	CanEnable bool `json:"can_enable"`
}

// ScriptTriggerProblem says, in words a developer reads, why a trigger would
// not run as File4Base stores it, and what to do about it.
type ScriptTriggerProblem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Remedy  string `json:"remedy"`
}

// Problem codes.
const (
	TriggerProblemMissingScript    = "missing_script"
	TriggerProblemUnresolvedScript = "unresolved_script"
	TriggerProblemParameter        = "unbound_parameter"
	TriggerProblemFindOnly         = "find_mode_only"
	TriggerProblemUnsupportedEvent = "unsupported_event"
	TriggerProblemObjectMissing    = "object_missing"
	TriggerProblemSlotTaken        = "event_taken"
	TriggerProblemAmbiguousScript  = "ambiguous_script"
	TriggerProblemExternalScript   = "script_in_other_file"
	TriggerProblemDuplicateID      = "duplicate_id"
	TriggerProblemObjectType       = "unsupported_object"
	TriggerProblemNoMode           = "no_mode"
)

// triggerRunsInBrowse reports whether a trigger's FileMaker modes include
// Browse mode.
func triggerRunsInBrowse(modes []interface{}) bool {
	for _, m := range modes {
		if strings.EqualFold(asString(m), "browse") {
			return true
		}
	}
	return false
}

var (
	// ErrScriptTriggerNotFound is returned when an id names no trigger.
	ErrScriptTriggerNotFound = errors.New("script trigger not found")
	// ErrScriptTriggerCannotEnable is returned when a trigger to enable has
	// a problem that keeps it from running.
	ErrScriptTriggerCannotEnable = errors.New("script trigger cannot be enabled")
	// ErrScriptTriggerDuplicateID is returned when an id names more than
	// one trigger, so which one is meant cannot be told.
	ErrScriptTriggerDuplicateID = errors.New("script trigger id is not unique")
	// ErrScriptTriggersAdminOnly is returned when a layout update by someone
	// who is neither an owner nor an administrator would switch a trigger on
	// or change the stored triggers.
	ErrScriptTriggersAdminOnly = errors.New("only an owner or an administrator can enable or change script triggers")
)

// ScriptTriggerUpdate is what SetScriptTriggersEnabled changed.
type ScriptTriggerUpdate struct {
	Updated int      `json:"updated"`
	Layouts []string `json:"layouts"`
}

// located is a trigger found in a layout definition, with what it takes to
// change it there.
type located struct {
	trigger ScriptTrigger
	entry   map[string]interface{}
	slot    *LayoutTriggerSlot // the slot it is in; nil when in the list
	layout  *layoutDefinition
}

type layoutDefinition struct {
	id, name string
	def      map[string]interface{}
	changed  bool
}

// scriptNames finds a script by id, or by its name when exactly one script
// has it.
type scriptNames struct {
	byID   map[string]string
	byName map[string][]string
}

func (n scriptNames) resolve(id, name string) (string, string) {
	found, foundName, _ := n.lookup(id, name)
	return found, foundName
}

// lookup finds a script by id, then by name; matches is how many scripts
// have the name when the id finds none, so a name several scripts share
// is reported as ambiguous rather than bound to one of them.
func (n scriptNames) lookup(id, name string) (string, string, int) {
	if id != "" {
		if found, ok := n.byID[id]; ok {
			return id, found, 1
		}
	}
	if strings.TrimSpace(name) == "" {
		return "", "", 0
	}
	ids := n.byName[strings.ToLower(strings.TrimSpace(name))]
	if len(ids) == 1 {
		return ids[0], n.byID[ids[0]], 1
	}
	return "", "", len(ids)
}

func (s *Service) scriptNames(ctx context.Context, q queryer) (scriptNames, error) {
	n := scriptNames{byID: map[string]string{}, byName: map[string][]string{}}
	rows, err := q.QueryContext(ctx, `SELECT id, name FROM sys_scripts`)
	if err != nil {
		return n, fmt.Errorf("failed reading scripts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return n, err
		}
		n.byID[id] = name
		key := strings.ToLower(strings.TrimSpace(name))
		n.byName[key] = append(n.byName[key], id)
	}
	return n, rows.Err()
}

// queryer is a *sql.DB or a *sql.Tx.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// ListScriptTriggers lists every trigger of every layout, by layout name.
func (s *Service) ListScriptTriggers(ctx context.Context) ([]ScriptTrigger, error) {
	db := s.driver.DB()
	layouts, err := s.layoutDefinitions(ctx, db, false)
	if err != nil {
		return nil, err
	}
	scripts, err := s.scriptNames(ctx, db)
	if err != nil {
		return nil, err
	}
	out := make([]ScriptTrigger, 0)
	for _, t := range locateAll(layouts, scripts) {
		out = append(out, t.trigger)
	}
	return out, nil
}

// locateAll lists the triggers of every layout. Triggers sharing an id —
// a layout copied with them before copies got ids of their own — are each
// reported, and none of them can be switched by that id.
func locateAll(layouts []*layoutDefinition, scripts scriptNames) []*located {
	var all []*located
	count := map[string]int{}
	for _, l := range layouts {
		for _, t := range locateTriggers(l, scripts) {
			all = append(all, t)
			count[t.trigger.ID]++
		}
	}
	for _, t := range all {
		if count[t.trigger.ID] > 1 {
			t.trigger.Problems = append(t.trigger.Problems, ScriptTriggerProblem{
				Code:    TriggerProblemDuplicateID,
				Message: fmt.Sprintf("%d triggers share this trigger's id, so it cannot be told apart from the others.", count[t.trigger.ID]),
				Remedy:  "Duplicate the layout again and delete the old copy, or set the trigger again in Layout Setup.",
			})
			t.trigger.CanEnable = false
		}
	}
	return all
}

// SetScriptTriggersEnabled switches the given triggers on or off, all of them
// or none. A trigger that cannot run as it stands is refused, with every
// such trigger named. Each changed layout's updated_at moves on, and the
// layouts are written in one transaction.
func (s *Service) SetScriptTriggersEnabled(ctx context.Context, ids []string, enabled bool) (*ScriptTriggerUpdate, error) {
	tx, err := s.driver.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := tx

	layouts, err := s.layoutDefinitions(ctx, q, s.driver.Dialect().Engine() != dbal.EngineSQLite)
	if err != nil {
		return nil, err
	}
	scripts, err := s.scriptNames(ctx, q)
	if err != nil {
		return nil, err
	}
	byID := map[string]*located{}
	shared := map[string]bool{}
	for _, t := range locateAll(layouts, scripts) {
		if _, again := byID[t.trigger.ID]; again {
			shared[t.trigger.ID] = true
		}
		byID[t.trigger.ID] = t
	}

	var missing, ambiguous, refused []string
	wanted := make([]*located, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		t, ok := byID[id]
		if !ok {
			missing = append(missing, id)
			continue
		}
		if shared[id] {
			ambiguous = append(ambiguous, id)
			continue
		}
		wanted = append(wanted, t)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrScriptTriggerNotFound, strings.Join(missing, ", "))
	}
	if len(ambiguous) > 0 {
		return nil, fmt.Errorf("%w: %s names more than one trigger", ErrScriptTriggerDuplicateID, strings.Join(ambiguous, ", "))
	}

	if enabled {
		// Two triggers of one layout's event cannot both run.
		claimed := map[string]string{}
		for _, t := range wanted {
			if t.trigger.Enabled {
				continue
			}
			if !t.trigger.CanEnable {
				refused = append(refused, fmt.Sprintf("%s (%s): %s", t.trigger.LayoutName, t.trigger.Event, t.trigger.Problems[0].Message))
				continue
			}
			key := t.layout.id + "\x00" + eventKey(t.trigger.ObjectID, t.trigger.Event)
			if other, taken := claimed[key]; taken {
				refused = append(refused, fmt.Sprintf("%s (%s): the trigger %s of the same event is enabled too; only one script runs per event", t.trigger.LayoutName, t.trigger.Event, other))
				continue
			}
			claimed[key] = t.trigger.ID
		}
		if len(refused) > 0 {
			return nil, fmt.Errorf("%w: %s", ErrScriptTriggerCannotEnable, strings.Join(refused, "; "))
		}
	}

	update := &ScriptTriggerUpdate{Layouts: []string{}}
	for _, t := range wanted {
		if t.trigger.Enabled == enabled {
			continue
		}
		if enabled {
			enableTrigger(t, scripts)
		} else {
			disableTrigger(t)
		}
		t.layout.changed = true
		update.Updated++
	}

	now := time.Now().UTC()
	query := `UPDATE sys_layouts SET definition = $1, updated_at = $2 WHERE id = $3`
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		query = `UPDATE sys_layouts SET definition = ?, updated_at = ? WHERE id = ?`
	}
	for _, l := range layouts {
		if !l.changed {
			continue
		}
		encoded, err := json.Marshal(l.def)
		if err != nil {
			return nil, fmt.Errorf("layout %q: %w", l.name, err)
		}
		if _, err := tx.ExecContext(ctx, query, string(encoded), now, l.id); err != nil {
			return nil, fmt.Errorf("failed updating layout %q: %w", l.name, err)
		}
		update.Layouts = append(update.Layouts, l.id)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return update, nil
}

// enableTrigger switches a trigger on: one of an event File4Base runs moves
// from the list into its slot. It is then reviewed, so no longer marked as
// held back by the migration.
func enableTrigger(t *located, scripts scriptNames) {
	entry := t.entry
	entry["enabled"] = true
	delete(entry, "quarantined_by_migration")
	if id, name := scripts.resolve(asString(entry["script_id"]), asString(entry["script_name"])); id != "" && asString(entry["file_reference"]) == "" {
		entry["script_id"], entry["script_name"] = id, name
	}
	if t.slot != nil {
		return
	}
	slot := slotForEvent(t.trigger.Event)
	if slot == nil || t.trigger.Scope != "layout" {
		return // refused before getting here
	}
	removeFromList(t.layout.def, entry)
	entry["event"] = slot.Event
	t.layout.def[slot.Key] = entry
}

// disableTrigger switches a trigger off: one in a slot moves into the list,
// keeping the id it was listed by.
func disableTrigger(t *located) {
	entry := t.entry
	entry["enabled"] = false
	if t.slot == nil {
		return
	}
	entry["id"] = t.trigger.ID
	entry["event"] = t.slot.Event
	delete(t.layout.def, t.slot.Key)
	list, _ := t.layout.def[LayoutScriptTriggersKey].([]interface{})
	t.layout.def[LayoutScriptTriggersKey] = append(list, entry)
}

func removeFromList(def map[string]interface{}, entry map[string]interface{}) {
	list, _ := def[LayoutScriptTriggersKey].([]interface{})
	kept := make([]interface{}, 0, len(list))
	for _, raw := range list {
		if m, ok := raw.(map[string]interface{}); ok && sameMap(m, entry) {
			continue
		}
		kept = append(kept, raw)
	}
	if len(kept) == 0 {
		delete(def, LayoutScriptTriggersKey)
		return
	}
	def[LayoutScriptTriggersKey] = kept
}

// sameMap reports whether two maps are the same map, not merely equal ones.
func sameMap(a, b map[string]interface{}) bool {
	return reflect.ValueOf(a).UnsafePointer() == reflect.ValueOf(b).UnsafePointer()
}

// locateTriggers lists the triggers of one layout, slots first.
func locateTriggers(l *layoutDefinition, scripts scriptNames) []*located {
	var out []*located
	objects := layoutObjects(l.def)
	slotScripts := map[string]string{}
	for i := range LayoutTriggerSlots {
		slot := &LayoutTriggerSlots[i]
		entry := asMap(l.def[slot.Key])
		if entry == nil {
			continue
		}
		id := asString(entry["id"])
		if id == "" {
			id = l.id + ":" + slot.Key
		}
		_, name := scripts.resolve(asString(entry["script_id"]), asString(entry["script_name"]))
		if name == "" {
			name = asString(entry["script_name"])
		}
		slotScripts[strings.ToLower(slot.Event)] = name
		t := describeTrigger(l, id, slot.Event, entry, entry["enabled"] != false, objects, scripts)
		out = append(out, &located{trigger: t, entry: entry, slot: slot, layout: l})
	}
	list, _ := l.def[LayoutScriptTriggersKey].([]interface{})
	// The list's triggers that run, by object and event: one script runs
	// per event of the layout, or of one of its objects.
	running := map[string]string{}
	for _, raw := range list {
		if entry := asMap(raw); entry != nil && entry["enabled"] == true {
			_, name := scripts.resolve(asString(entry["script_id"]), asString(entry["script_name"]))
			running[eventKey(asString(entry["object_id"]), asString(entry["event"]))] = firstNonBlank(name, asString(entry["script_name"]))
		}
	}
	for i, raw := range list {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		id := asString(entry["id"])
		if id == "" {
			id = fmt.Sprintf("%s:%s:%d", l.id, LayoutScriptTriggersKey, i)
		}
		t := describeTrigger(l, id, asString(entry["event"]), entry, entry["enabled"] == true, objects, scripts)
		if !t.Enabled {
			other, taken := "", false
			if t.Scope == "layout" && slotForEvent(t.Event) != nil {
				other, taken = slotScripts[strings.ToLower(slotForEvent(t.Event).Event)]
			} else {
				other, taken = running[eventKey(t.ObjectID, t.Event)]
			}
			if taken {
				where := "This layout"
				if t.Scope == "object" {
					where = "This object"
				}
				t.Problems = append(t.Problems, ScriptTriggerProblem{
					Code:    TriggerProblemSlotTaken,
					Message: fmt.Sprintf("%s already runs the script %q on %s, and only one script runs per event.", where, other, t.Event),
					Remedy:  "Disable the other trigger first, or leave this one disabled.",
				})
				t.CanEnable = false
			}
		}
		out = append(out, &located{trigger: t, entry: entry, layout: l})
	}
	return out
}

// eventKey names an event of a layout (objectID empty) or of one of its
// objects: one script runs per key.
func eventKey(objectID, event string) string {
	return objectID + "\x00" + strings.ToLower(strings.TrimSpace(event))
}

// describeTrigger reads one stored trigger and says what keeps it from
// running.
func describeTrigger(l *layoutDefinition, id, event string, entry map[string]interface{},
	enabled bool, objects map[string]map[string]interface{}, scripts scriptNames) ScriptTrigger {
	t := ScriptTrigger{
		ID: id, LayoutID: l.id, LayoutName: l.name, Scope: "layout", Event: event,
		ScriptName: asString(entry["script_name"]), Parameter: asString(entry["parameter"]),
		Enabled: enabled, QuarantinedByMigration: asBool(entry["quarantined_by_migration"]),
		Problems: []ScriptTriggerProblem{},
	}
	fileReference := strings.TrimSpace(asString(entry["file_reference"]))
	matches := 0
	if fileReference == "" {
		var scriptID, name string
		scriptID, name, matches = scripts.lookup(asString(entry["script_id"]), t.ScriptName)
		if scriptID != "" {
			t.ScriptID, t.ScriptName = scriptID, name
		}
	}

	objectID := asString(entry["object_id"])
	source := asMap(entry["object"])
	if objectID != "" || source != nil {
		t.Scope = "object"
		t.ObjectID = objectID
		if obj := objects[objectID]; obj != nil && objectID != "" {
			t.ObjectType = asString(obj["type"])
			t.ObjectLabel = objectLabel(obj)
		} else {
			t.ObjectID = ""
			t.ObjectType = asString(source["type"])
			t.ObjectLabel = firstNonBlank(asString(source["name"]), asString(source["field"]), t.ObjectType, "object")
		}
	}

	switch {
	case fileReference != "":
		t.Problems = append(t.Problems, ScriptTriggerProblem{
			Code:    TriggerProblemExternalScript,
			Message: fmt.Sprintf("The script %q is in another file (%s), and File4Base does not run a trigger's script in another solution.", t.ScriptName, fileReference),
			Remedy:  "Leave the trigger disabled, or run the script from a button with Perform Script through the file reference.",
		})
	case t.ScriptID != "":
	case matches > 1:
		t.Problems = append(t.Problems, ScriptTriggerProblem{
			Code:    TriggerProblemAmbiguousScript,
			Message: fmt.Sprintf("%d scripts are named %q, and the trigger does not say which one it runs.", matches, t.ScriptName),
			Remedy:  "Rename the scripts so that only the one the trigger runs has this name, or set the trigger again in Layout Setup.",
		})
	case t.ScriptName == "":
		t.Problems = append(t.Problems, ScriptTriggerProblem{
			Code:    TriggerProblemMissingScript,
			Message: "The trigger runs no script: its script was deleted in FileMaker before the migration.",
			Remedy:  "Set a script for this event in Layout Setup, or leave the trigger disabled.",
		})
	default:
		t.Problems = append(t.Problems, ScriptTriggerProblem{
			Code:    TriggerProblemUnresolvedScript,
			Message: fmt.Sprintf("The script %q is not in this solution.", t.ScriptName),
			Remedy:  fmt.Sprintf("Create or import a script named %q, then come back to enable the trigger.", t.ScriptName),
		})
	}
	if why := asString(entry["parameter_problem"]); why != "" {
		t.Problems = append(t.Problems, ScriptTriggerProblem{
			Code:    TriggerProblemParameter,
			Message: fmt.Sprintf("The script parameter %s.", why),
			Remedy:  "Correct the parameter in FileMaker and migrate again, or set the trigger again in Layout Setup.",
		})
	}
	onObject := t.Scope == "object"
	if modes, isList := entry["modes"].([]interface{}); isList && !triggerRunsInBrowse(modes) {
		switch {
		case len(modes) == 0:
			t.Problems = append(t.Problems, ScriptTriggerProblem{
				Code:    TriggerProblemNoMode,
				Message: "FileMaker runs this trigger in no mode: its mode checkboxes are all cleared, which is how FileMaker switches a trigger off.",
				Remedy:  "Leave it disabled, or set it again in Layout mode if it should run.",
			})
		case TriggerEventRuns(event, onObject) && !TriggerEventRunsInFind(event):
			t.Problems = append(t.Problems, ScriptTriggerProblem{
				Code:    TriggerProblemFindOnly,
				Message: fmt.Sprintf("FileMaker runs this trigger in Find mode only; File4Base runs %s triggers in Browse mode, where it would run when FileMaker does not.", event),
				Remedy:  "Leave it disabled, or add a script step that checks the mode and set the trigger again in Layout mode.",
			})
		}
	}
	switch {
	case onObject && !ObjectTriggerEventRuns(event):
		t.Problems = append(t.Problems, ScriptTriggerProblem{
			Code: TriggerProblemUnsupportedEvent,
			Message: fmt.Sprintf("File4Base does not run %s triggers set on layout objects yet; it runs %s on fields. The trigger is kept so it is not lost.",
				firstNonBlank(event, "this event's"), strings.Join(ObjectTriggerEvents, ", ")),
			Remedy: "Leave it disabled. Use a button, or another trigger of the field, for what must run now.",
		})
	case onObject && t.ObjectID == "":
		t.Problems = append(t.Problems, ScriptTriggerProblem{
			Code:    TriggerProblemObjectMissing,
			Message: fmt.Sprintf("The object it was set on (%s) is not on the File4Base layout.", t.ObjectLabel),
			Remedy:  "Add the object to the layout in Layout mode, and set the trigger on it there.",
		})
	case onObject && !strings.EqualFold(t.ObjectType, "field"):
		t.Problems = append(t.Problems, ScriptTriggerProblem{
			Code:    TriggerProblemObjectType,
			Message: fmt.Sprintf("File4Base runs object triggers on fields; this one is set on a %s.", firstNonBlank(t.ObjectType, "layout object")),
			Remedy:  "Leave it disabled, or set the trigger on a field of the layout instead.",
		})
	case !onObject && !LayoutTriggerEventRuns(event):
		t.Problems = append(t.Problems, ScriptTriggerProblem{
			Code: TriggerProblemUnsupportedEvent,
			Message: fmt.Sprintf("File4Base does not run %s triggers yet; it runs OnLayoutEnter, OnLayoutExit, %s. The trigger is kept so it is not lost.",
				firstNonBlank(event, "this event's"), strings.Join(ListTriggerEvents, ", ")),
			Remedy: "Leave it disabled, or run its script from another trigger or a button instead.",
		})
	}
	t.CanEnable = !t.Enabled && len(t.Problems) == 0
	return t
}

func objectLabel(obj map[string]interface{}) string {
	if fb := asMap(obj["field_binding"]); fb != nil {
		if f := asString(fb["field_name"]); f != "" {
			if occ := asString(fb["table_occurrence"]); occ != "" {
				return occ + "::" + f
			}
			return f
		}
	}
	return firstNonBlank(asString(obj["name"]), asString(obj["field_name"]), asString(obj["text"]), asString(obj["label"]), asString(obj["type"]))
}

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// layoutObjects indexes a definition's objects, those of groups and portals
// included, by id.
func layoutObjects(def map[string]interface{}) map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	var walk func(list interface{})
	walk = func(list interface{}) {
		items, _ := list.([]interface{})
		for _, raw := range items {
			obj := asMap(raw)
			if obj == nil {
				continue
			}
			if id := asString(obj["id"]); id != "" {
				out[id] = obj
			}
			walk(obj["children"])
			walk(obj["objects"])
		}
	}
	walk(def["objects"])
	return out
}

// layoutDefinitions reads every layout's definition, by name. forUpdate
// locks the rows for the transaction reading them.
func (s *Service) layoutDefinitions(ctx context.Context, q queryer, forUpdate bool) ([]*layoutDefinition, error) {
	query := `SELECT id, name, definition FROM sys_layouts`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed reading layouts: %w", err)
	}
	defer rows.Close()
	var out []*layoutDefinition
	for rows.Next() {
		var l layoutDefinition
		var raw string
		if err := rows.Scan(&l.id, &l.name, &raw); err != nil {
			return nil, err
		}
		if l.def = decodeDefinition([]byte(raw)); l.def == nil {
			l.def = map[string]interface{}{}
		}
		out = append(out, &l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].name) < strings.ToLower(out[j].name) })
	return out, nil
}

// decodeDefinition reads a layout definition with its numbers kept as
// written (json.Number), so a definition written back after a trigger is
// switched has every other value exactly as it was. Nil when it does not
// read as an object.
func decodeDefinition(raw []byte) map[string]interface{} {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var def map[string]interface{}
	if err := dec.Decode(&def); err != nil {
		return nil
	}
	return def
}

// FreshScriptTriggerIDs gives every trigger of a new layout's definition an
// id of its own, so a copied layout's triggers are not the original's: the
// review switches a trigger by its id. A definition without triggers is
// returned as it was given.
func FreshScriptTriggerIDs(definition json.RawMessage) json.RawMessage {
	if !bytes.Contains(definition, []byte(`"id"`)) {
		return definition
	}
	def := decodeDefinition(definition)
	if def == nil || !renewScriptTriggerIDs(def) {
		return definition
	}
	encoded, err := json.Marshal(def)
	if err != nil {
		return definition
	}
	return encoded
}

// renewScriptTriggerIDs gives every trigger of a definition that has an id a
// new one, and reports whether there was any.
func renewScriptTriggerIDs(def map[string]interface{}) bool {
	changed := false
	renew := func(entry map[string]interface{}) {
		if entry != nil && asString(entry["id"]) != "" {
			entry["id"] = uuid.NewString()
			changed = true
		}
	}
	for _, slot := range LayoutTriggerSlots {
		renew(asMap(def[slot.Key]))
	}
	for _, raw := range asSlice(def[LayoutScriptTriggersKey]) {
		renew(asMap(raw))
	}
	return changed
}

// GuardScriptTriggerEdits checks a layout update against the definition it
// replaces. A definition that leaves the trigger list out keeps the stored
// one, as a client that does not know the list would otherwise drop it.
// Someone who is neither an owner nor an administrator may not change the
// list, nor put in a slot a trigger that was switched off — a migrated one
// above all: switching triggers on is the review's, which is theirs alone.
// Answers the definition to store.
func GuardScriptTriggerEdits(previous, next json.RawMessage, admin bool) (json.RawMessage, error) {
	old := decodeDefinition(previous)
	if old == nil {
		return next, nil
	}
	def := decodeDefinition(next)
	if def == nil {
		return next, nil
	}
	out := next
	if _, has := def[LayoutScriptTriggersKey]; !has && len(asSlice(old[LayoutScriptTriggersKey])) > 0 {
		def[LayoutScriptTriggersKey] = old[LayoutScriptTriggersKey]
		encoded, err := json.Marshal(def)
		if err != nil {
			return nil, err
		}
		out = encoded
	}
	if admin {
		return out, nil
	}

	if !reflect.DeepEqual(normalizeTriggers(asSlice(old[LayoutScriptTriggersKey])), normalizeTriggers(asSlice(def[LayoutScriptTriggersKey]))) {
		return nil, fmt.Errorf("%w: the layout's stored script triggers were changed", ErrScriptTriggersAdminOnly)
	}
	// The triggers that were switched off, wherever they were.
	var off []map[string]interface{}
	for _, raw := range asSlice(old[LayoutScriptTriggersKey]) {
		if entry := asMap(raw); entry != nil && entry["enabled"] != true {
			off = append(off, entry)
		}
	}
	for _, slot := range LayoutTriggerSlots {
		if entry := asMap(old[slot.Key]); entry != nil && entry["enabled"] == false {
			off = append(off, entry)
		}
	}
	for _, slot := range LayoutTriggerSlots {
		before, after := asMap(old[slot.Key]), asMap(def[slot.Key])
		if after == nil || after["enabled"] == false || reflect.DeepEqual(normalizeTrigger(before), normalizeTrigger(after)) {
			continue
		}
		if asBool(after["quarantined_by_migration"]) {
			return nil, fmt.Errorf("%w: %s would run a trigger imported from FileMaker that has not been reviewed", ErrScriptTriggersAdminOnly, slot.Event)
		}
		for _, entry := range off {
			if id := asString(after["id"]); (id != "" && id == asString(entry["id"])) || sameAction(entry, after) {
				return nil, fmt.Errorf("%w: %s would run a trigger that is switched off", ErrScriptTriggersAdminOnly, slot.Event)
			}
		}
	}
	return out, nil
}

// normalizeTrigger is a stored trigger without what a client may drop or
// add on a round trip without changing it: empty values and empty params.
func normalizeTrigger(entry map[string]interface{}) map[string]interface{} {
	if entry == nil {
		return nil
	}
	out := map[string]interface{}{}
	for k, v := range entry {
		if v == nil {
			continue
		}
		if m, ok := v.(map[string]interface{}); ok && len(m) == 0 && k == "params" {
			continue
		}
		out[k] = v
	}
	return out
}

func normalizeTriggers(list []interface{}) []interface{} {
	out := make([]interface{}, 0, len(list))
	for _, raw := range list {
		if entry := asMap(raw); entry != nil {
			out = append(out, normalizeTrigger(entry))
			continue
		}
		out = append(out, raw)
	}
	return out
}

// sameAction reports whether two triggers run the same script with the same
// parameter, whatever their ids, events and flags.
func sameAction(a, b map[string]interface{}) bool {
	strip := func(m map[string]interface{}) map[string]interface{} {
		out := normalizeTrigger(m)
		for _, k := range []string{"id", "event", "enabled", "quarantined_by_migration", "object_id", "object", "modes", "parameter_problem"} {
			delete(out, k)
		}
		return out
	}
	return reflect.DeepEqual(strip(a), strip(b))
}
