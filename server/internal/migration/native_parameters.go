package migration

import "strings"

// decodeNativeParameters reads the typed Parameter wrappers used by native
// Save a Copy as XML. The full original tree remains in filemaker_parameters;
// options that are not understood are reported explicitly.
func decodeNativeParameters(container *xmlNode, canonical string) (map[string]interface{}, []string) {
	out := map[string]interface{}{}
	var unknown []string
	for _, parameter := range container.Children {
		if parameter.Name != "Parameter" {
			unknown = append(unknown, parameter.Name)
			continue
		}
		kind := firstAttr(parameter, "type")
		child := findFirstChild(parameter, kind)
		switch kind {
		case "Calculation":
			if child == nil {
				unknown = append(unknown, "Parameter.Calculation")
				continue
			}
			expression := calculationText(child)
			out["expression"] = expression
			switch canonical {
			case "Set Field":
				out["value"] = expression
				out["value_is_calculation"] = true
			case "If", "Else If", "Exit Loop If":
				out["condition"] = expression
			case "Exit Script":
				out["result"] = expression
			case "Open URL":
				out["url"] = expression
			default:
				unknown = append(unknown, "Parameter.Calculation")
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
				expression := calculationText(repetition)
				if expression != "" && expression != "1" {
					unknown = append(unknown, "Parameter.Variable.repetition")
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
			if firstAttr(child, "repetition") != "" && firstAttr(child, "repetition") != "1" {
				unknown = append(unknown, "Parameter.FieldReference.repetition")
			}
		case "LayoutReferenceContainer":
			if child == nil {
				unknown = append(unknown, "Parameter.LayoutReferenceContainer")
				continue
			}
			if layout := findFirstChild(child, "LayoutReference"); layout != nil && canonical == "Go to Layout" {
				out["layout_name"] = firstAttr(layout, "name", "Name")
				out["filemaker_layout_id"] = firstAttr(layout, "id", "ID")
			} else if script := findFirstChild(child, "ScriptReference"); script != nil && canonical == "Perform Script" {
				out["script_name"] = firstAttr(script, "name", "Name")
				out["filemaker_script_id"] = firstAttr(script, "id", "ID")
				if external := findFirstChild(script, "DataSourceReference"); external != nil {
					out["file_reference"] = firstAttr(external, "name", "Name")
				}
			} else {
				unknown = append(unknown, "Parameter.LayoutReferenceContainer")
			}
		case "List":
			if child == nil || canonical != "Perform Script" {
				unknown = append(unknown, "Parameter.List")
				continue
			}
			ref := findFirstChild(child, "ScriptReference")
			if ref == nil {
				unknown = append(unknown, "Parameter.List")
				continue
			}
			out["script_name"] = firstAttr(ref, "name", "Name")
			out["filemaker_script_id"] = firstAttr(ref, "id", "ID")
			if external := findFirstChild(ref, "DataSourceReference"); external != nil {
				out["file_reference"] = firstAttr(external, "name", "Name")
			}
		case "DataSourceReference":
			if child == nil {
				unknown = append(unknown, "Parameter.DataSourceReference")
				continue
			}
			out["file_reference"] = firstAttr(child, "name", "Name")
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
			if calculation == nil || canonical != "Show Custom Dialog" {
				unknown = append(unknown, "Parameter."+kind)
				continue
			}
			out[strings.ToLower(kind)] = calculationText(calculation)
			out[strings.ToLower(kind)+"_is_calculation"] = true
		default:
			unknown = append(unknown, "Parameter."+kind)
		}
	}
	return out, unknown
}
