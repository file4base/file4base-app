package migration

import "strings"

// decodeNativeParameters reads the typed Parameter wrappers used by native
// Save a Copy as XML. The full original tree remains in filemaker_parameters;
// options that are not understood are reported explicitly.
func decodeNativeParameters(container *xmlNode, canonical string) (map[string]interface{}, []string) {
	out := map[string]interface{}{}
	var unknown []string
	var buttons []map[string]interface{}
	var inputFields []map[string]interface{}

	for _, parameter := range container.Children {
		if parameter.Name != "Parameter" {
			unknown = append(unknown, parameter.Name)
			continue
		}
		kind := firstAttr(parameter, "type")
		child := findFirstChild(parameter, kind)
		switch kind {
		case "Animation":
			// Presentation-only layout transition animation.
			continue
		case "Calculation":
			if child == nil {
				unknown = append(unknown, "Parameter.Calculation")
				continue
			}
			expression := calculationText(child)
			out["expression"] = expression
			switch canonical {
			case "Set Field", "Insert Calculated Result", "Replace Field Contents":
				out["value"] = expression
				out["value_is_calculation"] = true
			case "Set Field By Name":
				if _, ok := out["target_field"]; !ok {
					out["target_field"] = expression
					out["target_field_is_calculation"] = true
				} else {
					out["value"] = expression
					out["value_is_calculation"] = true
				}
			case "If", "Else If", "Exit Loop If":
				out["condition"] = expression
			case "Exit Script":
				out["result"] = expression
			case "Open URL", "Insert from URL":
				out["url"] = expression
			case "Go to Layout":
				out["layout_calculation"] = expression
			case "Go to Record/Request/Page", "Go to Portal Row":
				out["record_calculation"] = expression
			case "Go to Object", "Refresh Portal":
				out["object_name"] = expression
				out["object_name_is_calculation"] = true
			case "Set Error Logging":
				out["custom_debug_info"] = expression
			case "Pause/Resume Script":
				out["duration_seconds"] = expression
			case "New Window", "Select Window", "Close Window":
				out["window_name"] = expression
				out["window_name_is_calculation"] = true
			default:
				out["value"] = expression
				out["value_is_calculation"] = true
			}
		case "Variable":
			name := findFirstChild(parameter, "Name")
			value := findFirstChild(parameter, "value", "Value")
			if canonical != "Set Variable" || name == nil || value == nil {
				unknown = append(unknown, "Parameter.Variable")
				continue
			}
			variable := firstAttr(name, "value")
			if variable == "" {
				variable = strings.TrimSpace(name.Text)
			}
			out["name"] = variable
			out["value"] = calculationText(value)
			if repetition := findFirstChild(parameter, "repetition"); repetition != nil {
				if expression := calculationText(repetition); expression != "" && expression != "1" {
					out["repetition"] = expression
				}
			}
		case "FieldReference":
			if child == nil {
				unknown = append(unknown, "Parameter.FieldReference")
				continue
			}
			name := firstAttr(child, "name", "Name")
			out["field"] = sanitizeIdentifier(name)
			out["field_label"] = name
			out["filemaker_field_id"] = firstAttr(child, "id", "ID")
			if occurrence := findFirstChild(child, "TableOccurrenceReference"); occurrence != nil {
				out["field_table"] = firstAttr(occurrence, "name", "Name")
			}
			if rep := firstAttr(child, "repetition"); rep != "" && rep != "1" {
				out["repetition"] = rep
			}
		case "Target":
			if calc := findFirstChild(parameter, "Calculation"); calc != nil {
				out["target_field"] = calculationText(calc)
				out["target_field_is_calculation"] = true
			} else if fr := findFirstChild(parameter, "FieldReference"); fr != nil {
				name := firstAttr(fr, "name", "Name")
				out["field"] = sanitizeIdentifier(name)
				out["field_label"] = name
				if occ := findFirstChild(fr, "TableOccurrenceReference"); occ != nil {
					out["field_table"] = firstAttr(occ, "name", "Name")
				}
			} else if v := findFirstChild(parameter, "Variable"); v != nil {
				if vn := firstAttr(v, "value", "name"); vn != "" {
					out["target"] = vn
				} else if vnNode := findFirstChild(v, "Name"); vnNode != nil {
					out["target"] = firstAttr(vnNode, "value")
				}
			} else if child != nil {
				out["target"] = decodeNodeValue(child)
			}
		case "LayoutReferenceContainer":
			if child == nil {
				unknown = append(unknown, "Parameter.LayoutReferenceContainer")
				continue
			}
			if canonical == "Go to Layout" || canonical == "New Window" || canonical == "Go to Related Record" {
				decodeLayoutDestination(child, out)
				if _, hasDest := out["layout_destination"]; !hasDest {
					if layout := findFirstChild(child, "LayoutReference"); layout != nil {
						out["layout_name"] = firstAttr(layout, "name", "Name")
						out["filemaker_layout_id"] = firstAttr(layout, "id", "ID")
						out["layout_destination"] = "selected"
					}
				}
			} else if script := findFirstChild(child, "ScriptReference"); script != nil && canonical == "Perform Script" {
				out["script_name"] = firstAttr(script, "name", "Name")
				out["filemaker_script_id"] = firstAttr(script, "id", "ID")
				if external := findFirstChild(script, "DataSourceReference"); external != nil {
					out["file_reference"] = firstAttr(external, "name", "Name")
				}
			} else {
				unknown = append(unknown, "Parameter.LayoutReferenceContainer")
			}
		case "Records":
			// Go to Record/Request/Page in Save a Copy as XML: <List name="Next">
			// with optional <Boolean type="Exit after last" value="True"/> and
			// optional <Calculation> for By Calculation (#133/#137).
			list := findFirstChild(parameter, "List")
			if list == nil {
				unknown = append(unknown, "Parameter.Records")
				continue
			}
			decodeRowPageLocation(list, out)
			for _, flag := range list.Children {
				if flag.Name == "Boolean" && strings.EqualFold(firstAttr(flag, "type"), "Exit after last") {
					if v, ok := xmlAttrBool(flag, "value", "state"); ok {
						out["exit_after_last"] = v
					}
				}
			}
		case "List":
			if child == nil {
				unknown = append(unknown, "Parameter.List")
				continue
			}
			if canonical == "Perform Script" {
				ref := findFirstChild(child, "ScriptReference")
				if ref == nil {
					// FileMaker writes "<unknown>" and no script reference
					// for a script it cannot name: one that was deleted, or
					// one of another file that was not open as the design
					// was saved, which it then names by its file only.
					if external := findFirstChild(child, "DataSourceReference"); external != nil {
						out["file_reference"] = firstAttr(external, "name", "Name")
						out["script_unresolved"] = true
					} else {
						out["script_missing"] = true
					}
					continue
				}
				out["script_name"] = firstAttr(ref, "name", "Name")
				out["filemaker_script_id"] = firstAttr(ref, "id", "ID")
				if external := findFirstChild(ref, "DataSourceReference"); external != nil {
					out["file_reference"] = firstAttr(external, "name", "Name")
				}
				continue
			}
			listName := strings.ToLower(strings.TrimSpace(firstAttr(child, "name", "value")))
			switch canonical {
			case "Go to Record/Request/Page", "Go to Portal Row":
				decodeRowPageLocation(child, out)
			case "Adjust Window":
				if listName != "" {
					out["window_action"] = firstAttr(child, "name", "value")
				}
			case "Show/Hide Toolbars", "Show/Hide Menubar", "Show/Hide Text Ruler":
				if listName != "" {
					out["state"] = listName
				}
			case "Replace Field Contents":
				unknown = append(unknown, decodeReplaceOptions(child, out)...)
			default:
				if listName != "" {
					out["option"] = firstAttr(child, "name", "value")
				}
			}
		case "Boolean":
			if child == nil {
				unknown = append(unknown, "Parameter.Boolean")
				continue
			}
			bType := strings.ToLower(strings.TrimSpace(firstAttr(child, "type", "name")))
			bVal, _ := xmlAttrBool(child, "value", "state")
			switch {
			case bType == "capture" || canonical == "Set Error Capture" || canonical == "Allow User Abort" ||
				canonical == "Set Error Logging":
				out["state"] = bVal
			case strings.Contains(bType, "hidden"):
				// Open File [Open hidden]; tested before "open" below.
				out["hidden"] = bVal
			case strings.Contains(bType, "dialog"):
				out["dialog"] = bVal
			case strings.Contains(bType, "exit after last"):
				out["exit_after_last"] = bVal
			case strings.Contains(bType, "restore"):
				out["restore"] = bVal
			case strings.Contains(bType, "pause"):
				out["pause"] = bVal
			case strings.Contains(bType, "skip") && strings.Contains(bType, "validation"):
				out["skip_validation"] = bVal
			case strings.Contains(bType, "force"):
				out["force_commit"] = bVal
			case strings.Contains(bType, "select"):
				out["select"] = bVal
			case strings.Contains(bType, "flush") && strings.Contains(bType, "join"):
				out["flush_join_cache"] = bVal
			case strings.Contains(bType, "flush"):
				out["flush_external_cache"] = bVal
			case strings.Contains(bType, "ssl"):
				out["verify_ssl"] = bVal
			case strings.Contains(bType, "append"):
				out["append"] = bVal
			case strings.Contains(bType, "open"):
				out["auto_open"] = bVal
			case strings.Contains(bType, "folder"):
				out["create_folders"] = bVal
			case strings.Contains(bType, "related"):
				out["show_only_related"] = bVal
			case strings.Contains(bType, "lock"):
				out["lock"] = bVal
			case bType != "":
				out[sanitizeIdentifier(bType)] = bVal
			default:
				out["state"] = bVal
			}
		case "DataSourceReference":
			if child == nil {
				unknown = append(unknown, "Parameter.DataSourceReference")
				continue
			}
			if canonical == "Import Records" {
				decodeImportSource(child, out)
				continue
			}
			out["file_reference"] = firstAttr(child, "name", "Name")
		case "ImportField":
			if child == nil || canonical != "Import Records" {
				unknown = append(unknown, "Parameter.ImportField")
				continue
			}
			unknown = append(unknown, decodeImportField(child, out)...)
		case "Email":
			send := findFirstChild(parameter, "Send")
			if send == nil || canonical != "Send Mail" {
				unknown = append(unknown, "Parameter.Email")
				continue
			}
			unknown = append(unknown, decodeSendMail(send, out)...)
			// The With dialog flag and the attachments sit beside <Send>.
			for _, ch := range parameter.Children {
				switch ch.Name {
				case "Boolean":
					if strings.EqualFold(firstAttr(ch, "type"), "With dialog") {
						if v, ok := xmlAttrBool(ch, "value"); ok {
							out["dialog"] = v
						}
					} else {
						unknown = append(unknown, "Parameter.Email.Boolean")
					}
				case "UniversalPathList":
					if paths := universalPaths(ch); len(paths) > 0 {
						out["attachments"] = paths
					}
				case "Send":
				default:
					unknown = append(unknown, "Parameter.Email."+ch.Name)
				}
			}
		case "SaveResult":
			// Save Records as PDF / Excel: where the document goes.
			list := findFirstChild(parameter, "List")
			if list == nil {
				unknown = append(unknown, "Parameter.SaveResult")
				continue
			}
			switch strings.ToLower(firstAttr(list, "name")) {
			case "file":
				out["save_to"] = "file"
			case "variable":
				out["save_to"] = "variable"
			default:
				unknown = append(unknown, "Parameter.SaveResult")
			}
		case "Portal":
			// Go to Portal Row: <List name="Last"> or <List name="By
			// Calculation…"> with its <Calculation> and With dialog.
			list := findFirstChild(parameter, "List")
			if list == nil {
				unknown = append(unknown, "Parameter.Portal")
				continue
			}
			decodeRowPageLocation(list, out)
			for _, flag := range list.Children {
				if flag.Name != "Boolean" {
					continue
				}
				switch strings.ToLower(firstAttr(flag, "type")) {
				case "with dialog":
					if v, ok := xmlAttrBool(flag, "value"); ok {
						out["dialog"] = v
					}
				case "exit after last":
					if v, ok := xmlAttrBool(flag, "value"); ok {
						out["exit_after_last"] = v
					}
				}
			}
		case "replace":
			list := findFirstChild(parameter, "List")
			if list == nil || canonical != "Replace Field Contents" {
				unknown = append(unknown, "Parameter.replace")
				continue
			}
			unknown = append(unknown, decodeReplaceOptions(list, out)...)
		case "Field1", "Field2", "Field3":
			if canonical != "Show Custom Dialog" {
				unknown = append(unknown, "Parameter."+kind)
				continue
			}
			if entry := decodeDialogInputParameter(parameter); len(entry) > 0 {
				inputFields = append(inputFields, entry)
			}
		case "Parameter":
			if child == nil || canonical != "Perform Script" {
				unknown = append(unknown, "Parameter.Parameter")
				continue
			}
			out["script_parameter"] = calculationText(child)
		case "Comment":
			if child == nil || canonical != "Comment" {
				unknown = append(unknown, "Parameter.Comment")
				continue
			}
			text := firstAttr(child, "value")
			if text == "" {
				text = child.Text
			}
			out["text"] = text
		case "Title", "Message":
			calculation := findFirstChild(parameter, "Calculation")
			if calculation != nil {
				out[strings.ToLower(kind)] = calculationText(calculation)
				out[strings.ToLower(kind)+"_is_calculation"] = true
			} else if child != nil {
				out[strings.ToLower(kind)] = strings.TrimSpace(firstAttr(child, "value"))
			} else {
				unknown = append(unknown, "Parameter."+kind)
			}
		case "Button1", "Button2", "Button3", "Button":
			label := ""
			isCalc := false
			if calc := findFirstChild(parameter, "Calculation"); calc != nil {
				label = calculationText(calc)
				isCalc = true
			} else if child != nil {
				label = firstAttr(child, "value", "name")
			}
			commit := true
			if c, ok := xmlAttrBool(child, "CommitState", "commit"); ok {
				commit = c
			}
			buttons = append(buttons, map[string]interface{}{
				"label":          label,
				"is_calculation": isCalc,
				"commit":         commit,
			})
		case "Buttons":
			if child != nil {
				buttons = append(buttons, decodeDialogButtons(child)...)
			}
		case "InputField1", "InputField2", "InputField3", "InputField":
			entry := map[string]interface{}{}
			if fr := findFirstChild(parameter, "FieldReference"); fr != nil {
				name := firstAttr(fr, "name", "Name")
				if strings.HasPrefix(name, "$") {
					entry["field"] = name
				} else {
					entry["field"] = sanitizeIdentifier(name)
					entry["field_label"] = name
				}
			}
			if lbl := findFirstChild(parameter, "Label"); lbl != nil {
				if calc := findFirstChild(lbl, "Calculation"); calc != nil {
					entry["label"] = calculationText(calc)
					entry["label_is_calculation"] = true
				}
			}
			if len(entry) > 0 {
				inputFields = append(inputFields, entry)
			}
		case "InputFields":
			if child != nil {
				inputFields = append(inputFields, decodeDialogInputFields(child)...)
			}
		case "WindowReference", "Window":
			decodeWindowNode(parameter, out)
			if child != nil {
				decodeWindowNode(child, out)
			}
		case "Name":
			if calc := findFirstChild(parameter, "Calculation"); calc != nil {
				out["window_name"] = calculationText(calc)
				out["window_name_is_calculation"] = true
			} else if child != nil {
				if v := firstAttr(child, "value", "name"); v != "" {
					out["window_name"] = v
				}
			}
		case "Style", "NewWndStyles", "WindowStyle":
			if child != nil {
				out["window_style"] = firstAttr(child, "value", "name", "Style")
			}
		case "Top", "Left", "Width", "Height":
			key := strings.ToLower(kind)
			if calc := findFirstChild(parameter, "Calculation"); calc != nil {
				out[key] = calculationText(calc)
			} else if child != nil {
				out[key] = firstAttr(child, "value")
			}
		case "Close", "Minimize", "Maximize", "Resize", "MenuBar", "Toolbars", "DimParentWindow":
			if child != nil {
				if v, ok := xmlAttrBool(child, "value", "state"); ok {
					out["window_"+strings.ToLower(kind)] = v
				}
			}
		case "SortSpecification", "SortList":
			if child != nil {
				if sortOrder := decodeSortOrder(child); len(sortOrder) > 0 {
					out["sort_order"] = sortOrder
				}
			}
		case "FindRequest", "Query", "Restore":
			if kind == "Restore" && child != nil {
				if v, ok := xmlAttrBool(child, "value", "state"); ok {
					out["restore"] = v
				}
			} else if child != nil {
				if reqs := decodeFindRequests(child); len(reqs) > 0 {
					out["find_requests"] = reqs
				}
			}
		case "PageSetup", "Print", "PrintSetup", "PDF":
			if child != nil {
				out["print_settings"] = decodeNodeValue(child)
			}
		case "UniversalPathList", "Import", "Export", "FieldMap":
			if child != nil {
				if kind == "UniversalPathList" {
					paths := universalPaths(child)
					switch {
					case canonical == "Send Mail":
						out["attachments"] = paths
					case len(paths) > 0:
						out["path"] = paths[0]
						if len(paths) > 1 {
							out["paths"] = paths
						}
					default:
						out["path"] = decodeNodeValue(child)
					}
					if v, ok := xmlAttrBool(child, "CreateMail"); ok {
						out["create_email"] = v
					}
					if v, ok := xmlAttrBool(child, "AutoOpen", "AutomaticallyOpen"); ok {
						out["auto_open"] = v
					}
				} else {
					out[strings.ToLower(kind)+"_options"] = decodeNodeValue(child)
				}
			}
		case "URL":
			if calc := findFirstChild(parameter, "Calculation"); calc != nil {
				out["url"] = calculationText(calc)
			} else if child != nil {
				out["url"] = firstAttr(child, "value")
			}
		case "cURLOptions", "CURLOptions":
			if calc := findFirstChild(parameter, "Calculation"); calc != nil {
				out["curl_options"] = calculationText(calc)
			} else if child != nil {
				out["curl_options"] = firstAttr(child, "value")
			}
		case "Object":
			if calc := findFirstChild(parameter, "Calculation"); calc != nil {
				out["object_name"] = calculationText(calc)
				out["object_name_is_calculation"] = true
			} else if child != nil {
				out["object_name"] = firstAttr(child, "value", "name")
			}
		case "Related", "TableOccurrenceReference":
			if child != nil {
				if name := firstAttr(child, "name", "Name"); name != "" {
					out["related_table"] = name
				} else if occ := findFirstChild(child, "TableOccurrenceReference"); occ != nil {
					out["related_table"] = firstAttr(occ, "name", "Name")
				}
			}
		case "CustomMenuSetReference":
			if child != nil {
				out["menu_set"] = firstAttr(child, "name", "Name")
			}
		case "To", "CC", "BCC", "Subject", "Attachment":
			key := strings.ToLower(kind)
			if calc := findFirstChild(parameter, "Calculation"); calc != nil {
				out[key] = calculationText(calc)
			} else if child != nil {
				out[key] = decodeNodeValue(child)
			}
		case "Options":
			if child != nil {
				out["options"] = decodeNodeValue(child)
				if canonical == "Save Records as PDF" || canonical == "Save Records as Excel" {
					switch strings.ToLower(firstAttr(child, "type")) {
					case "records being browsed":
						out["records"] = "browsed"
					case "current record":
						out["records"] = "current"
					case "":
					default:
						out["records"] = "blank"
					}
				}
			}
		default:
			unknown = append(unknown, "Parameter."+kind)
		}
	}
	if len(buttons) > 0 {
		out["buttons"] = buttons
	}
	if len(inputFields) > 0 {
		out["input_fields"] = inputFields
	}
	return out, unknown
}
