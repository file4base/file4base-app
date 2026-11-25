package migration

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Decoders for the step options Save a Copy as XML writes in wrappers of its
// own: the Import Records field map, the Send Mail message, the Save Records
// as PDF destination, the Go to Portal Row target, the Replace Field Contents
// method and the Show Custom Dialog input fields. Each one turns what the step
// does in FileMaker into the operands the File4Base runner reads; whatever a
// decoder does not recognise is returned so the conversion report names it.

// universalPaths reads the paths of a <UniversalPathList>: one <Location> per
// path, or the list's own text, one path per line.
func universalPaths(n *xmlNode) []string {
	if n == nil {
		return nil
	}
	var out []string
	for _, loc := range findNodes(n, "Location") {
		out = append(out, pathLines(loc.Text)...)
	}
	if len(out) == 0 {
		out = pathLines(n.Text)
	}
	return out
}

// fileMakerFileName is the file a FileMaker path names: "file:Invoices",
// "fmnet:/host/Invoices.fmp12" and "filemac:/Disk/dir/Invoices.fmp12" all
// name "Invoices". A path computed from a variable names no file.
func fileMakerFileName(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || strings.Contains(path, "$") {
		return ""
	}
	if i := strings.IndexByte(path, ':'); i > 0 && isFileMakerPathStart(path[:i+1]) {
		path = path[i+1:]
	}
	path = strings.TrimRight(path, "/")
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		path = path[i+1:]
	}
	lower := strings.ToLower(path)
	for _, ext := range []string{".fmp12", ".fp7", ".fp5"} {
		if strings.HasSuffix(lower, ext) {
			path = path[:len(path)-len(ext)]
			break
		}
	}
	return strings.TrimSpace(path)
}

// Import Records methods, as FileMaker 2023 and later name them.
const (
	importMethodAdd     = "add"     // add new records
	importMethodUpdate  = "update"  // update matching records (match fields)
	importMethodReplace = "replace" // replace the found set, record by record
)

// decodeImportSource reads where an Import Records step reads from: a
// FileMaker file, another kind of file, or a folder of files.
func decodeImportSource(ds *xmlNode, out map[string]interface{}) {
	fileType := strings.ToUpper(firstAttr(ds, "fileType"))
	kind := "file"
	switch fileType {
	case "FMPR":
		kind = "filemaker"
	case "BTCH":
		kind = "folder"
	}
	paths := universalPaths(ds)
	computed := false
	for _, p := range paths {
		if strings.Contains(p, "$") {
			computed = true
		}
	}
	source := map[string]interface{}{"kind": kind, "paths": paths}
	if fileType != "" {
		source["file_type"] = fileType
	}
	if computed {
		source["path_is_calculation"] = true
	}
	if kind == "filemaker" {
		for _, p := range paths {
			if name := fileMakerFileName(p); name != "" {
				source["file"] = name
				break
			}
		}
	}
	out["import_source"] = source
}

// sourceFieldLabel matches the label FileMaker gives a source field it could
// not name because the source file was not open when the design was saved:
// "Source field 12 import to ".
var sourceFieldLabel = regexp.MustCompile(`(?i)^source field (\d+)\b`)

// mapVerbs are the endings FileMaker writes after the source field in a Map
// label; what precedes one is the source field's name, when FileMaker knew it.
var mapVerbs = []string{" not import to", " import to", " match with"}

// decodeImportField reads the field map of an Import Records step: the
// table it writes, the method, the auto-enter and repetition options, and
// for each target field whether it is imported into, matched on, or left
// alone, with the source field it is fed from.
//
// FileMaker refers to the source fields of a FileMaker file by position —
// "Source field 12" — and saves neither their names nor the source table
// unless the source file was open as the design was saved. A position is
// kept as a position; it is never resolved against a table File4Base picked.
func decodeImportField(n *xmlNode, out map[string]interface{}) []string {
	var unknown []string
	method := ""
	if action := findFirstChild(n, "action"); action != nil {
		switch strings.ToLower(firstAttr(action, "name")) {
		case "add":
			method = importMethodAdd
		case "update", "update matching":
			method = importMethodUpdate
		case "replace", "update existing":
			method = importMethodReplace
		}
		if method == "" {
			switch firstAttr(action, "value") {
			case "0":
				method = importMethodAdd
			case "1":
				method = importMethodReplace
			case "2":
				method = importMethodUpdate
			}
		}
		if method == "" {
			unknown = append(unknown, "Parameter.ImportField.action")
		}
	}
	if method == "" {
		method = importMethodAdd
	}
	out["import_method"] = method
	if v, ok := xmlAttrBool(n, "addasnew"); ok && method != importMethodAdd {
		out["add_remaining"] = v
	}
	if opts := findFirstChild(n, "Options"); opts != nil {
		for attr, key := range map[string]string{
			"doAutoEntry":        "auto_enter",
			"matchFieldNames":    "match_field_names",
			"splitRepetitions":   "split_repetitions",
			"keepOriginalData":   "keep_original_data",
			"copyContainersAsIs": "copy_containers_as_is",
		} {
			if v, ok := xmlAttrBool(opts, attr); ok {
				out[key] = v
			}
		}
	}
	if cs := findFirstChild(n, "CharacterSet"); cs != nil {
		if name := firstAttr(cs, "name"); name != "" {
			out["character_set"] = name
		}
	}
	if target := findFirstChild(n, "Target"); target != nil {
		if occ := findFirstChild(target, "TableOccurrenceReference"); occ != nil {
			if name := firstAttr(occ, "name", "Name"); name != "" {
				out["target_occurrence"] = name
			}
		}
	}

	var mappings []map[string]interface{}
	skipped := 0
	if fields := findFirstChild(n, "Field"); fields != nil {
		for _, m := range fields.Children {
			if m.Name != "Map" {
				continue
			}
			ref := findFirstChild(m, "FieldReference")
			if ref == nil {
				continue
			}
			var action string
			switch firstAttr(m, "kind") {
			case "0":
				action = "import"
			case "2":
				action = "match"
			case "1":
				skipped++
				continue
			default:
				unknown = append(unknown, "Parameter.ImportField.Map")
				continue
			}
			target := firstAttr(ref, "name", "Name")
			entry := map[string]interface{}{
				"action":             action,
				"target_field":       sanitizeIdentifier(target),
				"target_field_label": target,
			}
			if pos, err := strconv.Atoi(firstAttr(m, "index")); err == nil && pos > 0 {
				entry["source_position"] = pos
			}
			label := strings.TrimSpace(firstAttr(m, "name"))
			if label != "" && !sourceFieldLabel.MatchString(label) {
				name := label
				for _, verb := range mapVerbs {
					if strings.HasSuffix(strings.ToLower(name), verb) {
						name = strings.TrimSpace(name[:len(name)-len(verb)])
						break
					}
				}
				if name != "" {
					entry["source_field"] = name
				}
			}
			mappings = append(mappings, entry)
		}
	}
	if mappings == nil {
		mappings = []map[string]interface{}{}
	}
	out["field_mappings"] = mappings
	out["skipped_source_fields"] = skipped
	return unknown
}

// importRecordsNote says, for the report and for the script editor, what an
// Import Records step converted from FileMaker did and why it does not run
// until a transfer definition is named in it.
func importRecordsNote(params map[string]interface{}, targetTable string) string {
	source, _ := params["import_source"].(map[string]interface{})
	mappings, _ := params["field_mappings"].([]map[string]interface{})
	method, _ := params["import_method"].(string)

	var what strings.Builder
	switch kind, _ := source["kind"].(string); {
	case source == nil:
		what.WriteString("imports records")
	case kind == "filemaker":
		if file, _ := source["file"].(string); file != "" {
			fmt.Fprintf(&what, "imports from the FileMaker file %q", file)
		} else {
			what.WriteString("imports from a FileMaker file whose path is computed when the script runs")
		}
	case kind == "folder":
		what.WriteString("imports a folder of files")
	default:
		what.WriteString("imports from a file on disk")
	}
	if targetTable != "" {
		fmt.Fprintf(&what, " into %s", targetTable)
	}
	var keys, imported, positions, pairs []string
	named := 0
	for _, m := range mappings {
		field, _ := m["target_field"].(string)
		if m["action"] == "match" {
			keys = append(keys, field)
		} else {
			imported = append(imported, field)
		}
		from := "?"
		if name, ok := m["source_field"].(string); ok {
			named++
			from = name
		} else if pos, ok := m["source_position"].(int); ok {
			positions = append(positions, strconv.Itoa(pos))
			from = "source field " + strconv.Itoa(pos)
		}
		pairs = append(pairs, from+" -> "+field)
	}
	switch method {
	case importMethodUpdate:
		fmt.Fprintf(&what, ", updating the records that match on %s", joinOrNone(keys))
		if v, _ := params["add_remaining"].(bool); v {
			what.WriteString(" and adding the others")
		}
	case importMethodReplace:
		what.WriteString(", replacing the found set record by record")
	default:
		what.WriteString(" as new records")
	}
	fmt.Fprintf(&what, " (%d field(s) imported", len(imported))
	if v, ok := params["auto_enter"].(bool); ok {
		if v {
			what.WriteString(", auto-enter on")
		} else {
			what.WriteString(", auto-enter off")
		}
	}
	what.WriteString(")")

	var why string
	switch kind, _ := source["kind"].(string); {
	case kind == "filemaker" && len(positions) > 0:
		why = fmt.Sprintf("FileMaker saved its source fields by position only, not by name, and did not save "+
			"the source table, so the mapping cannot be rebuilt without guessing. Create a transfer from the "+
			"solution that file became into %s mapping %s, and name it in the step",
			orTable(targetTable), strings.Join(pairs, ", "))
	case kind == "filemaker" && named < len(mappings):
		why = "the source fields it reads are not named in the document. Create a transfer and name it in the step"
	case kind == "filemaker":
		why = fmt.Sprintf("FileMaker did not save the source table. Create a transfer from the solution that file "+
			"became into %s mapping %s, and name it in the step", orTable(targetTable), strings.Join(pairs, ", "))
	default:
		why = "File4Base scripts never read files from a disk. Import the file by hand (File > Import), or " +
			"move the data into a solution and run a transfer from it"
	}
	if method == importMethodReplace {
		why += ". A transfer adds or updates matching records; replacing the found set in order has no transfer " +
			"equivalent, so give the transfer a match field"
	}
	return what.String() + "; " + why
}

func joinOrNone(fields []string) string {
	if len(fields) == 0 {
		return "no field"
	}
	return strings.Join(fields, ", ")
}

func orTable(table string) string {
	if table == "" {
		return "the target table"
	}
	return table
}

// decodeSendMail reads a <Send> element of a Send Mail step: how the mail
// leaves, its addressees, subject and body, and whether it is sent once per
// record of the found set.
func decodeSendMail(send *xmlNode, out map[string]interface{}) []string {
	var unknown []string
	if v, ok := xmlAttrBool(send, "SMTP"); ok {
		if v {
			out["send_via"] = "smtp"
		} else {
			out["send_via"] = "email_client"
		}
	}
	if v, ok := xmlAttrBool(send, "OAuthAuthentication"); ok && v {
		out["oauth"] = true
	}
	for _, ch := range send.Children {
		switch ch.Name {
		case "To", "CC", "BCC", "Subject", "Message":
			key := strings.ToLower(ch.Name)
			if calc := findFirstChild(ch, "Calculation"); calc != nil {
				out[key] = calculationText(calc)
				out[key+"_is_calculation"] = true
			} else if v := firstAttr(ch, "value"); v != "" {
				out[key] = v
			} else if t := strings.TrimSpace(ch.Text); t != "" {
				out[key] = t
			}
			if collect := findFirstChild(ch, "CollectAddresses"); collect != nil {
				if v, ok := xmlAttrBool(collect, "value"); ok && v {
					out["collect_"+key] = true
				}
			}
		case "Multiple":
			if v, ok := xmlAttrBool(ch, "value"); ok {
				out["one_email_per_record"] = v
			}
		default:
			unknown = append(unknown, "Parameter.Email."+ch.Name)
		}
	}
	return unknown
}

// sendMailNote says why a Send Mail step converted from FileMaker cannot run
// as written, or "" when it can.
func sendMailNote(params map[string]interface{}) string {
	var why []string
	if params["send_via"] == "smtp" {
		why = append(why, "it sends through an SMTP server, and File4Base scripts send mail only by opening a draft in the user's e-mail client")
	}
	if v, _ := params["one_email_per_record"].(bool); v {
		why = append(why, "it sends one e-mail per record of the found set")
	}
	for _, key := range []string{"to", "cc", "bcc"} {
		if v, _ := params["collect_"+key].(bool); v {
			why = append(why, fmt.Sprintf("it collects the %s addresses across the found set", strings.ToUpper(key)))
		}
	}
	if paths, _ := params["attachments"].([]string); len(paths) > 0 {
		why = append(why, "it attaches files, which a draft opened in the e-mail client cannot carry")
	}
	if len(why) == 0 {
		return ""
	}
	return "Send Mail cannot run as written: " + strings.Join(why, "; ")
}

// decodeReplaceOptions reads the <List> of a Replace Field Contents step:
// replace with the current contents, with serial numbers, or with a
// calculation evaluated for each record.
func decodeReplaceOptions(list *xmlNode, out map[string]interface{}) []string {
	var unknown []string
	name := strings.ToLower(firstAttr(list, "name"))
	switch {
	case firstAttr(list, "value") == "3" || strings.Contains(name, "calculation"):
		out["replace_type"] = "calculation"
	case firstAttr(list, "value") == "2" || strings.Contains(name, "serial"):
		out["replace_type"] = "serial_numbers"
	case firstAttr(list, "value") == "1" || strings.Contains(name, "current"):
		out["replace_type"] = "current_contents"
	default:
		unknown = append(unknown, "Parameter.replace")
	}
	for _, ch := range list.Children {
		switch {
		case ch.Name == "Calculation" && out["replace_type"] == "calculation":
			out["value"] = calculationText(ch)
			out["value_is_calculation"] = true
		case ch.Name == "Boolean" && strings.Contains(strings.ToLower(firstAttr(ch, "type")), "auto-enter"):
			if v, ok := xmlAttrBool(ch, "value"); ok {
				out["skip_auto_enter"] = v
			}
		case out["replace_type"] == "serial_numbers":
			// FileMaker's serial-number operands: the initial value, the
			// increment, and whether the field's own serial is updated.
			kind := strings.ToLower(ch.Name + " " + firstAttr(ch, "type", "name"))
			text := firstAttr(ch, "value")
			if calc := findFirstChild(ch, "Calculation"); calc != nil {
				text = calculationText(calc)
			}
			switch {
			case strings.Contains(kind, "initial"):
				out["serial_initial"] = text
			case strings.Contains(kind, "increment"):
				out["serial_increment"] = text
			case strings.Contains(kind, "update"):
				if v, ok := xmlAttrBool(ch, "value"); ok {
					out["serial_update_entry_options"] = v
				}
			default:
				unknown = append(unknown, "Parameter.replace."+ch.Name)
			}
		default:
			unknown = append(unknown, "Parameter.replace."+ch.Name)
		}
	}
	if out["replace_type"] == "serial_numbers" {
		if _, ok := out["serial_initial"]; !ok {
			unknown = append(unknown, "Parameter.replace (serial numbers)")
		}
	}
	return unknown
}

// decodeDialogInputParameter reads one Show Custom Dialog input field written
// as <Parameter type="Field1">: the field or variable it fills, its label and
// whether it hides what is typed.
func decodeDialogInputParameter(p *xmlNode) map[string]interface{} {
	entry := map[string]interface{}{}
	for _, ch := range p.Children {
		switch ch.Name {
		case "Boolean":
			if strings.EqualFold(firstAttr(ch, "type"), "Password") {
				if v, ok := xmlAttrBool(ch, "value"); ok {
					entry["password"] = v
				}
			}
		case "Parameter":
			switch firstAttr(ch, "type") {
			case "Target":
				if fr := findFirstChild(ch, "FieldReference"); fr != nil {
					name := firstAttr(fr, "name", "Name")
					if name == "" {
						continue
					}
					entry["field"] = sanitizeIdentifier(name)
					entry["field_label"] = name
					if occ := findFirstChild(fr, "TableOccurrenceReference"); occ != nil {
						entry["field_table"] = firstAttr(occ, "name", "Name")
					}
				} else if v := findFirstChild(ch, "Variable"); v != nil {
					if name := firstAttr(v, "value", "name"); name != "" {
						entry["field"] = name
					}
				}
			case "Label":
				if calc := findFirstChild(ch, "Calculation"); calc != nil {
					entry["label"] = calculationText(calc)
					entry["label_is_calculation"] = true
				}
			}
		}
	}
	return entry
}

// stepOptionItems reports what a converted step's FileMaker options mean for
// running it in File4Base, and leaves the same explanation in the step as
// migration_note, which the script editor shows next to the step and the
// runner gives when the step stops. A step that runs as written gets none.
func stepOptionItems(canonical string, params map[string]interface{}, occTableByName map[string]string) []ReportItem {
	note := ""
	switch canonical {
	case "Import Records":
		if _, ok := params["field_mappings"]; !ok {
			return nil
		}
		table := ""
		if occ, _ := params["target_occurrence"].(string); occ != "" {
			table = occTableByName[strings.ToLower(occ)]
		}
		if table != "" {
			params["target_table"] = table
		}
		note = "Import Records " + importRecordsNote(params, table)
	case "Perform Script":
		switch {
		case params["script_missing"] == true:
			note = "Perform Script names no script: FileMaker wrote it as <unknown>, which it does for a script " +
				"that was deleted. FileMaker skips such a step with error 104 (script missing); File4Base " +
				"does the same and stops the script unless Set Error Capture is on. Point the step at a script or remove it"
		case params["script_unresolved"] == true:
			ref, _ := params["file_reference"].(string)
			note = fmt.Sprintf("Perform Script calls a script of the file %q that the document does not name "+
				"(FileMaker writes <unknown> when that file was not open as the design was saved). Name the "+
				"script in the step, or save the design again with %q open", ref, ref)
		}
	case "Send Mail":
		note = sendMailNote(params)
	}
	if note == "" {
		return nil
	}
	params["migration_note"] = note
	return []ReportItem{{
		Category: CategoryManualActionRequired,
		Domain:   "script",
		Detail:   note,
	}}
}
