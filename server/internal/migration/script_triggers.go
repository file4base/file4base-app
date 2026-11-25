package migration

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/google/uuid"
)

// Script triggers are migrated disabled, and kept.
//
// An imported script must never run because a migrated layout was opened, so
// every trigger arrives switched off. Deleting the trigger to achieve that, as
// the migration once did, threw away thousands of definitions that each had to
// be recreated by hand. A trigger is therefore stored with its event, the
// imported script it runs, its parameter bound to the imported field names,
// enabled=false and quarantined_by_migration=true, and the developer reviews
// and switches them on in File > Manage > Script Triggers.
//
// Every migrated trigger is written to the layout definition's
// "script_triggers" list (schema.LayoutScriptTriggersKey), never to the
// on_layout_enter / on_layout_exit slots a client runs: a client that does
// not know the enabled flag still never runs a trigger that is off.
// Enabling one moves it into its slot (see schema.SetScriptTriggersEnabled).

// sourceTrigger is one trigger as the FileMaker document writes it.
type sourceTrigger struct {
	event string
	// scriptName and scriptID name the script; named is false when the
	// trigger names none (FileMaker keeps a trigger whose script was deleted).
	scriptName, scriptID string
	named                bool
	// fileReference names the other FileMaker file the script is in; its
	// scriptID is that file's, never one of this file's scripts.
	fileReference string
	parameter     string
	// findOnly is set when FileMaker runs the trigger in Find mode only.
	findOnly bool
	// object is the layout object the trigger is set on; nil for the layout.
	object *sourceTriggerObject
}

type sourceTriggerObject struct {
	node             *xmlNode
	typ, name, field string
}

// readLayoutTriggers collects the triggers of a layout and of its objects,
// each with the nearest object it is set on.
func readLayoutTriggers(ln *xmlNode) []sourceTrigger {
	var out []sourceTrigger
	var walk func(n *xmlNode, obj *xmlNode)
	walk = func(n *xmlNode, obj *xmlNode) {
		for _, c := range n.Children {
			switch c.Name {
			case "LayoutObject", "Object":
				walk(c, c)
			case "ScriptTrigger", "Trigger":
				out = append(out, readTrigger(c, obj))
			default:
				walk(c, obj)
			}
		}
	}
	walk(ln, nil)
	return out
}

func readTrigger(t *xmlNode, obj *xmlNode) sourceTrigger {
	st := sourceTrigger{event: firstAttr(t, "action", "event", "name", "type")}
	if ref := findFirstChild(t, "ScriptReference", "Script", "script"); ref != nil {
		st.named = true
		st.scriptName = firstAttr(ref, "name", "Name")
		st.scriptID = firstAttr(ref, "id", "ID")
		st.parameter = calculationText(ref)
		if external := findFirstChild(ref, "DataSourceReference", "FileReference"); external != nil {
			st.fileReference = firstAttr(external, "name", "Name")
			if st.fileReference == "" {
				st.fileReference = "another file"
			}
		}
	}
	if name := firstAttr(t, "script", "scriptName"); name != "" && st.scriptName == "" {
		st.named, st.scriptName = true, name
	}
	if st.parameter == "" {
		if p := findFirstChild(t, "Parameter", "parameter", "Calculation"); p != nil {
			st.parameter = calculationText(p)
		} else {
			st.parameter = firstAttr(t, "parameter", "param")
		}
	}
	if strings.EqualFold(firstAttr(t, "browseMode"), "false") {
		st.findOnly = true
	}
	if obj != nil {
		o := &sourceTriggerObject{node: obj, typ: firstAttr(obj, "type", "Type"), name: firstAttr(obj, "name", "Name")}
		if f := findFirstChild(obj, "Field", "field", "FieldObj"); f != nil {
			ref := f
			if inner := findFirstChild(f, "FieldReference"); inner != nil {
				ref = inner
			}
			field := firstAttr(ref, "field", "name", "Name")
			occ := firstAttr(ref, "table", "Table")
			if to := findFirstChild(ref, "TableOccurrenceReference"); to != nil && occ == "" {
				occ = firstAttr(to, "name", "Name")
			}
			if occ != "" && field != "" {
				field = occ + "::" + field
			}
			o.field = field
		}
		st.object = o
	}
	return st
}

// readFileTriggers collects the file's own triggers (OnFirstWindowOpen,
// OnLastWindowClose, …): every trigger outside the layouts, and the
// <WindowTriggers> of a Database Design Report.
func readFileTriggers(root *xmlNode) []sourceTrigger {
	var out []sourceTrigger
	var walk func(n *xmlNode)
	walk = func(n *xmlNode) {
		for _, c := range n.Children {
			switch c.Name {
			case "LayoutCatalog", "layouts", "Layout", "DDR_INFO", "ScriptCatalog", "StepsForScripts":
				continue
			case "ScriptTrigger", "Trigger":
				out = append(out, readTrigger(c, nil))
			case "WindowTriggers":
				for _, w := range c.Children {
					st := readTrigger(w, nil)
					st.event = w.Name
					out = append(out, st)
				}
			default:
				walk(c)
			}
		}
	}
	walk(root)
	return out
}

// layoutTriggerEntries turns a layout's source triggers into the stored
// form. The script and the parameter are bound once the scripts are
// converted, by bindLayoutTriggers; until then the entry carries the
// FileMaker script id under sourceScriptIDKey. objectIDs maps the field
// node of a migrated object to that object's id.
func layoutTriggerEntries(triggers []sourceTrigger, objectIDs map[*xmlNode]string) []interface{} {
	out := make([]interface{}, 0, len(triggers))
	for _, st := range triggers {
		entry := map[string]interface{}{
			"id":                       uuid.NewString(),
			"event":                    st.event,
			"type":                     "perform_script",
			"enabled":                  false,
			"quarantined_by_migration": true,
		}
		if st.named {
			entry["script_name"] = st.scriptName
			if st.fileReference != "" {
				// Its id is the other file's: it must not find one of ours.
				entry["file_reference"] = st.fileReference
			} else {
				entry[sourceScriptIDKey] = st.scriptID
			}
		}
		if st.parameter != "" {
			entry["parameter"] = st.parameter
		}
		if st.findOnly {
			entry["modes"] = []interface{}{"find"}
		}
		if o := st.object; o != nil {
			object := map[string]interface{}{}
			for k, v := range map[string]string{"type": o.typ, "name": o.name, "field": o.field} {
				if v != "" {
					object[k] = v
				}
			}
			entry["object"] = object
			if f := findFirstChild(o.node, "Field", "field", "FieldObj"); f != nil && objectIDs[f] != "" {
				entry["object_id"] = objectIDs[f]
			}
		}
		out = append(out, entry)
	}
	return out
}

// sourceScriptIDKey carries the FileMaker id of a trigger's script from the
// layout pass to bindLayoutTriggers, which removes it.
const sourceScriptIDKey = "_source_script_id"

// quarantineDefinitionTriggers switches off every trigger an explicit
// File4Base layout definition carries, and moves the slot triggers into the
// list, so that none runs before it is reviewed.
func quarantineDefinitionTriggers(def map[string]interface{}) {
	var list []interface{}
	if existing, ok := def[schema.LayoutScriptTriggersKey].([]interface{}); ok {
		list = existing
	}
	for _, slot := range schema.LayoutTriggerSlots {
		if action, ok := def[slot.Key].(map[string]interface{}); ok {
			action["event"] = slot.Event
			list = append(list, action)
		}
		delete(def, slot.Key)
	}
	for _, raw := range list {
		if entry, ok := raw.(map[string]interface{}); ok {
			if id, _ := entry["id"].(string); id == "" {
				entry["id"] = uuid.NewString()
			}
			entry["enabled"] = false
			entry["quarantined_by_migration"] = true
		}
	}
	if len(list) > 0 {
		def[schema.LayoutScriptTriggersKey] = list
	}
}

// scriptIndex finds an imported script by the FileMaker id or name a
// trigger gives.
type scriptIndex struct {
	bySourceID map[string]string
	byName     map[string][]string
	byID       map[string]bool
}

func newScriptIndex() scriptIndex {
	return scriptIndex{bySourceID: map[string]string{}, byName: map[string][]string{}, byID: map[string]bool{}}
}

func (x scriptIndex) add(sourceID string, sc *schema.BundleScript) {
	if sourceID != "" {
		x.bySourceID[sourceID] = sc.ID
	}
	key := strings.ToLower(strings.TrimSpace(sc.Name))
	x.byName[key] = append(x.byName[key], sc.ID)
	x.byID[sc.ID] = true
}

// resolve returns the imported script a trigger runs: by FileMaker id, which
// tells apart two scripts of the same name, then by name when exactly one
// script has it. matches is the number of scripts with the name when it
// finds none, so a name several scripts share is reported, not guessed. A
// script in another file is never one of these.
func (x scriptIndex) resolve(entry map[string]interface{}) (id string, matches int) {
	if ref, _ := entry["file_reference"].(string); ref != "" {
		return "", 0
	}
	if id, _ := entry["script_id"].(string); id != "" && x.byID[id] {
		return id, 1
	}
	if src, _ := entry[sourceScriptIDKey].(string); src != "" {
		if id := x.bySourceID[src]; id != "" {
			return id, 1
		}
	}
	name, _ := entry["script_name"].(string)
	ids := x.byName[strings.ToLower(strings.TrimSpace(name))]
	if len(ids) == 1 {
		return ids[0], 1
	}
	return "", len(ids)
}

// bindLayoutTriggers binds the triggers of every converted layout to the
// imported scripts and fields, and reports them: one summary for the
// solution, one item per trigger that cannot be enabled as it stands
// (script not found, parameter not bindable, Find mode only), one item per
// event File4Base does not run and one for the triggers FileMaker itself
// kept without a script, each with the number of triggers concerned.
func bindLayoutTriggers(bundle *schema.SolutionBundle, scripts scriptIndex, b formulaBinding, solution string) []ReportItem {
	tableByOccurrence := map[string]string{}
	tableByID := map[string]string{}
	for _, t := range bundle.Tables {
		tableByID[t.ID] = t.Name
	}
	for _, o := range bundle.TableOccurrences {
		tableByOccurrence[o.ID] = tableByID[o.BaseTableID]
	}

	var items []ReportItem
	total, onObjects, scriptless := 0, 0, 0
	unsupported := map[string]int{}
	for li := range bundle.Layouts {
		layout := &bundle.Layouts[li]
		list, _ := layout.Definition[schema.LayoutScriptTriggersKey].([]interface{})
		for _, raw := range list {
			entry, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			total++
			event, _ := entry["event"].(string)
			_, onObject := entry["object"]
			if !onObject {
				_, onObject = entry["object_id"]
			}
			if onObject {
				onObjects++
			}
			where := triggerLabel(layout.Name, entry)

			name, named := entry["script_name"].(string)
			_, hasSource := entry[sourceScriptIDKey]
			named = named || hasSource
			fileReference, _ := entry["file_reference"].(string)
			if id, matches := scripts.resolve(entry); id != "" {
				entry["script_id"] = id
			} else {
				delete(entry, "script_id")
				switch {
				case fileReference != "":
					items = append(items, ReportItem{Category: CategoryManualActionRequired, Domain: "trigger",
						SourceName: where, TargetName: layout.Name,
						Detail: fmt.Sprintf("The script %q this trigger runs is in another FileMaker file (%s). The trigger is kept, disabled, and cannot be enabled: File4Base does not run a trigger's script in another solution", name, fileReference)})
				case matches > 1:
					items = append(items, ReportItem{Category: CategoryManualActionRequired, Domain: "trigger",
						SourceName: where, TargetName: layout.Name,
						Detail: fmt.Sprintf("%d imported scripts are named %q, and nothing tells which one this trigger runs. The trigger is kept, disabled, and cannot be enabled until only one script has the name", matches, name)})
				case named && name != "":
					items = append(items, ReportItem{Category: CategoryManualActionRequired, Domain: "trigger",
						SourceName: where, TargetName: layout.Name,
						Detail: fmt.Sprintf("The script %q this trigger runs was not found among the imported scripts. The trigger is kept, disabled, and cannot be enabled until it runs an existing script", name)})
				default:
					// Already broken in FileMaker, which keeps a trigger
					// whose script was deleted: counted, not itemised.
					scriptless++
				}
			}
			delete(entry, sourceScriptIDKey)

			if param, _ := entry["parameter"].(string); strings.TrimSpace(param) != "" {
				bound, why := bindTriggerParameter(param, tableByOccurrence[layout.TableOccurrenceID], b)
				if why != "" {
					entry["parameter_problem"] = why
					items = append(items, ReportItem{Category: CategoryManualActionRequired, Domain: "trigger",
						SourceName: where, TargetName: layout.Name,
						Detail: fmt.Sprintf("The script parameter %q %s. The trigger is kept, disabled, with the parameter as FileMaker wrote it, and cannot be enabled until the parameter is corrected", readableFieldRefs(param), why)})
				} else {
					entry["parameter"] = bound
				}
			}

			if modes, _ := entry["modes"].([]interface{}); len(modes) == 1 && modes[0] == "find" {
				items = append(items, ReportItem{Category: CategoryManualActionRequired, Domain: "trigger",
					SourceName: where, TargetName: layout.Name,
					Detail: "FileMaker runs this trigger in Find mode only, and File4Base runs triggers in Browse mode. It is kept, disabled, and cannot be enabled"})
			}
			if onObject || !schema.LayoutTriggerEventRuns(event) {
				unsupported[event]++
			}
		}
	}

	events := make([]string, 0, len(unsupported))
	for e := range unsupported {
		events = append(events, e)
	}
	sort.Strings(events)
	for _, e := range events {
		label := e
		if label == "" {
			label = "(no event)"
		}
		items = append(items, ReportItem{Category: CategoryUnsupported, Domain: "trigger", SourceName: label,
			Detail: fmt.Sprintf("%d %s trigger(s) kept, disabled: File4Base does not run this event yet, so they cannot be enabled. They are listed in File > Manage > Script Triggers", unsupported[e], label)})
	}
	if scriptless > 0 {
		items = append(items, ReportItem{Category: CategoryManualActionRequired, Domain: "trigger", SourceName: solution,
			Detail: fmt.Sprintf("%d script trigger(s) name no script: their script was deleted in FileMaker, where they do nothing either. "+
				"They are kept, disabled, and cannot be enabled until a script is set for them; File > Manage > Script Triggers lists them under \"Problem\"", scriptless)})
	}
	if total > 0 {
		items = append([]ReportItem{{Category: CategoryManualActionRequired, Domain: "trigger", SourceName: solution,
			Detail: fmt.Sprintf("%d script trigger(s) were imported disabled (%d on layouts, %d on layout objects), so no imported script runs on its own. "+
				"Review them in File > Manage > Script Triggers and enable the ones you trust", total, total-onObjects, onObjects)}}, items...)
	}
	return items
}

// triggerLabel names a trigger in the report: its layout, its object when it
// has one, its event and its script.
func triggerLabel(layout string, entry map[string]interface{}) string {
	event, _ := entry["event"].(string)
	script, _ := entry["script_name"].(string)
	if script == "" {
		script = "no script"
	}
	where := layout
	if object, ok := entry["object"].(map[string]interface{}); ok {
		label := ""
		for _, k := range []string{"name", "field", "type"} {
			if v, _ := object[k].(string); v != "" {
				label = v
				break
			}
		}
		if label != "" {
			where += " > " + label
		}
	}
	return fmt.Sprintf("%s (%s -> %s)", where, event, script)
}

// bindTriggerParameter reads a trigger's script parameter against the
// layout's table, as a calculation field of that table is read, and returns
// it naming the imported fields — or why it cannot be.
func bindTriggerParameter(formula, table string, b formulaBinding) (string, string) {
	names := b.fields[table]
	var local []string
	if names != nil {
		for _, name := range names.sources {
			if calc.QuoteName(name) != name {
				local = append(local, name)
			}
		}
	}
	opts := calc.Options{Fields: local, Tables: b.occurrences, Locale: b.locale, Library: b.functions}
	expr, err := calc.ParseWith(formula, opts)
	if err != nil {
		return "", b.unreadable(formula, err)
	}
	var missing []string
	for _, ref := range expr.RelatedFields() {
		if ref.Missing {
			missing = append(missing, ref.Table+"::"+ref.Field)
		}
	}
	if len(missing) > 0 {
		return "", fmt.Sprintf("reads %s, a table or field that was deleted in FileMaker", quotedList(missing))
	}
	if name, ok := b.callsDropped(expr); ok {
		return "", fmt.Sprintf("calls the custom function %s, which could not be migrated", name)
	}
	rewritten, unresolved, err := calc.Rewrite(formula, opts, b.renamer(names))
	if err != nil {
		return "", fmt.Sprintf("could not be read (%v)", err)
	}
	if len(unresolved) > 0 {
		var refs []string
		for _, ref := range unresolved {
			name := ref.Field
			if ref.Table != "" {
				name = ref.Table + "::" + ref.Field
			}
			if !slices.Contains(refs, name) {
				refs = append(refs, name)
			}
		}
		return "", fmt.Sprintf("reads %s, which the imported tables do not have", quotedList(refs))
	}
	return rewritten, ""
}

// fileTriggerItem reports the file's own triggers, which File4Base does not
// run: they are named so they can be set again by hand.
func fileTriggerItem(triggers []sourceTrigger, solution string) []ReportItem {
	if len(triggers) == 0 {
		return nil
	}
	parts := make([]string, 0, len(triggers))
	for _, t := range triggers {
		script := t.scriptName
		if script == "" {
			script = "no script"
		}
		parts = append(parts, fmt.Sprintf("%s -> %q", t.event, script))
	}
	return []ReportItem{{Category: CategoryUnsupported, Domain: "trigger", SourceName: solution,
		Detail: fmt.Sprintf("%d file script trigger(s) were not imported (%s): File4Base does not run file triggers yet. "+
			"Set them again in File > File Options > Script Triggers once it does", len(triggers), strings.Join(parts, ", "))}}
}
