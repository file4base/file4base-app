package schema

import (
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
)

// Script triggers of a solution's layouts, and their review (#26).
//
// A layout definition holds its triggers in two places:
//
//   - on_layout_enter and on_layout_exit, the slots a client runs: an action
//     there runs in Browse mode when the layout is shown or left, unless it
//     carries "enabled": false;
//   - "script_triggers", a list of every other trigger: those switched off,
//     those of events File4Base does not run, and those set on a layout
//     object (an entry with "object_id", or the FileMaker "object" it was set
//     on when that object was not migrated).
//
// A migrated trigger always arrives in the list, switched off and marked
// quarantined_by_migration, so a client that does not know the enabled flag
// still never runs it. Enabling a trigger of an event File4Base runs moves it
// into its slot; disabling one moves it back into the list. A trigger File4Base
// could not run as it stands — no script, a parameter that does not read, an
// event it does not run — cannot be enabled, so "enabled" never says a trigger
// runs when it does not.

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

// LayoutTriggerEventRuns reports whether File4Base runs a layout trigger of
// the given FileMaker event.
func LayoutTriggerEventRuns(event string) bool {
	return slotForEvent(event) != nil
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
)

var (
	// ErrScriptTriggerNotFound is returned when an id names no trigger.
	ErrScriptTriggerNotFound = errors.New("script trigger not found")
	// ErrScriptTriggerCannotEnable is returned when a trigger to enable has
	// a problem that keeps it from running.
	ErrScriptTriggerCannotEnable = errors.New("script trigger cannot be enabled")
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
	if id != "" {
		if found, ok := n.byID[id]; ok {
			return id, found
		}
	}
	if ids := n.byName[strings.ToLower(strings.TrimSpace(name))]; len(ids) == 1 && name != "" {
		return ids[0], n.byID[ids[0]]
	}
	return "", ""
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
		key := strings.ToLower(name)
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
	for _, l := range layouts {
		for _, t := range locateTriggers(l, scripts) {
			out = append(out, t.trigger)
		}
	}
	return out, nil
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
	for _, l := range layouts {
		for _, t := range locateTriggers(l, scripts) {
			byID[t.trigger.ID] = t
		}
	}

	var missing, refused []string
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
		wanted = append(wanted, t)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrScriptTriggerNotFound, strings.Join(missing, ", "))
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
			key := t.layout.id + "\x00" + strings.ToLower(t.trigger.Event)
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
	if id, name := scripts.resolve(asString(entry["script_id"]), asString(entry["script_name"])); id != "" {
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
		if t.Scope == "layout" && !t.Enabled && slotForEvent(t.Event) != nil {
			if other, taken := slotScripts[strings.ToLower(slotForEvent(t.Event).Event)]; taken {
				t.Problems = append(t.Problems, ScriptTriggerProblem{
					Code:    TriggerProblemSlotTaken,
					Message: fmt.Sprintf("This layout already runs the script %q on %s, and only one script runs per event.", other, t.Event),
					Remedy:  "Disable the other trigger first, or leave this one disabled.",
				})
				t.CanEnable = false
			}
		}
		out = append(out, &located{trigger: t, entry: entry, layout: l})
	}
	return out
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
	if scriptID, name := scripts.resolve(asString(entry["script_id"]), t.ScriptName); scriptID != "" {
		t.ScriptID, t.ScriptName = scriptID, name
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
	case t.ScriptID != "":
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
	if modes, _ := entry["modes"].([]interface{}); len(modes) == 1 && asString(modes[0]) == "find" {
		t.Problems = append(t.Problems, ScriptTriggerProblem{
			Code:    TriggerProblemFindOnly,
			Message: "FileMaker runs this trigger in Find mode only; File4Base runs triggers in Browse mode, where it would run when FileMaker does not.",
			Remedy:  "Leave it disabled, or add a script step that checks the mode and set the trigger again in Layout Setup.",
		})
	}
	switch {
	case t.Scope == "object":
		t.Problems = append(t.Problems, ScriptTriggerProblem{
			Code:    TriggerProblemUnsupportedEvent,
			Message: fmt.Sprintf("File4Base does not run triggers set on layout objects (%s) yet. The trigger is kept so it is not lost.", event),
			Remedy:  "Leave it disabled. Use a button, or an OnLayoutEnter / OnLayoutExit trigger, for what must run now.",
		})
		if t.ObjectID == "" {
			t.Problems = append(t.Problems, ScriptTriggerProblem{
				Code:    TriggerProblemObjectMissing,
				Message: fmt.Sprintf("The object it was set on (%s) is not on the File4Base layout.", t.ObjectLabel),
				Remedy:  "Add the object to the layout in Layout mode if you need it.",
			})
		}
	case slotForEvent(event) == nil:
		t.Problems = append(t.Problems, ScriptTriggerProblem{
			Code:    TriggerProblemUnsupportedEvent,
			Message: fmt.Sprintf("File4Base does not run %s triggers yet; it runs OnLayoutEnter and OnLayoutExit. The trigger is kept so it is not lost.", firstNonBlank(event, "this event's")),
			Remedy:  "Leave it disabled, or run its script from OnLayoutEnter or a button instead.",
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
		if err := json.Unmarshal([]byte(raw), &l.def); err != nil || l.def == nil {
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
