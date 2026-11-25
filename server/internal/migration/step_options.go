package migration

import "strings"

func xmlAttrBool(n *xmlNode, keys ...string) (bool, bool) {
	if n == nil {
		return false, false
	}
	v := strings.TrimSpace(firstAttr(n, keys...))
	if v == "" {
		return false, false
	}
	switch strings.ToLower(v) {
	case "true", "on", "1", "yes":
		return true, true
	case "false", "off", "0", "no":
		return false, true
	}
	return false, false
}

// displayCalculationText extracts fallback formula text from FileMaker's
// tokenized <DisplayCalculation><Chunk>...</Chunk></DisplayCalculation> tree.
func displayCalculationText(n *xmlNode) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	for _, ch := range n.Children {
		if ch.Name == "Chunk" {
			b.WriteString(ch.Text)
		} else {
			b.WriteString(displayCalculationText(ch))
		}
	}
	if b.Len() == 0 {
		return strings.TrimSpace(n.Text)
	}
	return strings.TrimSpace(b.String())
}

func decodeLayoutDestination(n *xmlNode, params map[string]interface{}) {
	if n == nil {
		return
	}
	raw := strings.TrimSpace(firstAttr(n, "value", "Value", "type"))
	switch strings.ToLower(raw) {
	case "originallayout", "original", "1":
		params["layout_destination"] = "original"
	case "selectedlayout", "selected", "layout", "5":
		params["layout_destination"] = "selected"
	case "layoutnamebycalc", "byname", "namebycalc", "3":
		params["layout_destination"] = "by_name"
	case "layoutnumberbycalc", "bynumber", "numberbycalc", "4":
		params["layout_destination"] = "by_number"
	default:
		if raw != "" {
			params["layout_destination"] = strings.ToLower(raw)
		}
	}
	if layout := findFirstChild(n, "Layout", "LayoutReference"); layout != nil {
		if name := firstAttr(layout, "name", "Name"); name != "" {
			params["layout_name"] = name
		}
		if id := firstAttr(layout, "id", "ID"); id != "" {
			params["filemaker_layout_id"] = id
		}
	}
	if calc := findFirstChild(n, "Calculation", "calculation"); calc != nil {
		if expr := calculationText(calc); expr != "" {
			params["layout_calculation"] = expr
		}
	}
}

func decodeRowPageLocation(n *xmlNode, params map[string]interface{}) {
	if n == nil {
		return
	}
	// A DDR writes the target as a word in value (<RowPageLocation
	// value="Next"/>); Save a Copy as XML writes the word in name and a code
	// in value (<List name="Next" value="4"/>). The word is read first, so a
	// code is never taken for a record number.
	target := ""
	for _, raw := range []string{firstAttr(n, "name", "Name"), firstAttr(n, "value", "Value")} {
		if target = rowPageTarget(raw); target != "" {
			break
		}
	}
	if target == "" && n.Name == "List" {
		target = map[string]string{"1": "first", "2": "last", "3": "previous", "4": "next", "5": "by_calculation"}[firstAttr(n, "value")]
	}
	if target == "" {
		if raw := strings.TrimSpace(firstAttr(n, "value", "Value", "name")); raw != "" {
			target = strings.ToLower(raw)
		}
	}
	if target != "" {
		params["target"] = target
	}
	if calc := findFirstChild(n, "Calculation", "calculation"); calc != nil {
		if expr := calculationText(calc); expr != "" {
			params["record_calculation"] = expr
		}
	}
}

// rowPageTarget reads a Go to Record/Request/Page or Go to Portal Row target
// word, with or without FileMaker's trailing ellipsis ("By Calculation…").
func rowPageTarget(raw string) string {
	word := strings.ToLower(strings.TrimSpace(raw))
	word = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(word, "…"), "..."))
	switch word {
	case "first":
		return "first"
	case "previous", "prev":
		return "previous"
	case "next":
		return "next"
	case "last":
		return "last"
	case "bycalculation", "calculation", "by calculation":
		return "by_calculation"
	}
	return ""
}

// decodeFindRequests parses DDR XML <Query> / <FindRequestList> and native
// Save-as-XML <FindRequest> trees into normalized find requests.
func decodeFindRequests(n *xmlNode) []map[string]interface{} {
	if n == nil {
		return nil
	}
	var reqNodes []*xmlNode
	collectRequests := func(root *xmlNode) {
		var walk func(*xmlNode)
		walk = func(cur *xmlNode) {
			if cur == nil {
				return
			}
			if cur.Name == "Request" || cur.Name == "FindRequest" {
				reqNodes = append(reqNodes, cur)
				return
			}
			for _, c := range cur.Children {
				walk(c)
			}
		}
		walk(root)
	}
	collectRequests(n)
	if len(reqNodes) == 0 && len(n.Children) > 0 {
		reqNodes = []*xmlNode{n}
	}
	var out []map[string]interface{}
	for _, rn := range reqNodes {
		action := strings.ToLower(strings.TrimSpace(firstAttr(rn, "action", "Action", "type", "setAction")))
		if action == "" {
			action = "find"
		}
		if action == "omit" || action == "omit records" {
			action = "omit"
		} else {
			action = "find"
		}
		var criteria []map[string]interface{}
		var walkCrit func(*xmlNode)
		walkCrit = func(cur *xmlNode) {
			if cur == nil {
				return
			}
			if cur.Name == "Criterion" || cur.Name == "FindCriterion" || cur.Name == "SetField" {
				fieldNode := findFirstChild(cur, "Field", "FieldReference")
				valNode := findFirstChild(cur, "Value", "Text", "Calculation")
				entry := map[string]interface{}{}
				if fieldNode != nil {
					if fname := firstAttr(fieldNode, "name", "Name"); fname != "" {
						entry["field"] = sanitizeIdentifier(fname)
						entry["field_label"] = fname
					}
					if tbl := firstAttr(fieldNode, "table", "Table"); tbl != "" {
						entry["table"] = sanitizeIdentifier(tbl)
					} else if occ := findFirstChild(fieldNode, "TableOccurrenceReference"); occ != nil {
						entry["table"] = sanitizeIdentifier(firstAttr(occ, "name", "Name"))
					}
				}
				if valNode != nil {
					entry["value"] = calculationText(valNode)
				} else if v := firstAttr(cur, "value", "Value"); v != "" {
					entry["value"] = v
				}
				if len(entry) > 0 {
					criteria = append(criteria, entry)
				}
				return
			}
			for _, c := range cur.Children {
				walkCrit(c)
			}
		}
		walkCrit(rn)
		out = append(out, map[string]interface{}{
			"action":   action,
			"criteria": criteria,
		})
	}
	return out
}

// decodeSortOrder parses DDR XML <SortList> and native <SortSpecification> trees.
func decodeSortOrder(n *xmlNode) []map[string]interface{} {
	if n == nil {
		return nil
	}
	var out []map[string]interface{}
	var walk func(*xmlNode)
	walk = func(cur *xmlNode) {
		if cur == nil {
			return
		}
		if cur.Name == "Sort" {
			order := strings.ToLower(strings.TrimSpace(firstAttr(cur, "type", "Type", "order")))
			if strings.HasPrefix(order, "desc") {
				order = "descending"
			} else {
				order = "ascending"
			}
			entry := map[string]interface{}{"order": order}
			if fn := findFirstChild(cur, "Field", "FieldReference"); fn != nil {
				if fname := firstAttr(fn, "name", "Name"); fname != "" {
					entry["field"] = sanitizeIdentifier(fname)
					entry["field_label"] = fname
				}
				if tbl := firstAttr(fn, "table", "Table"); tbl != "" {
					entry["table"] = sanitizeIdentifier(tbl)
				} else if occ := findFirstChild(fn, "TableOccurrenceReference"); occ != nil {
					entry["table"] = sanitizeIdentifier(firstAttr(occ, "name", "Name"))
				}
			}
			if _, ok := entry["field"]; ok {
				out = append(out, entry)
			}
			return
		}
		for _, c := range cur.Children {
			walk(c)
		}
	}
	walk(n)
	return out
}

// decodeDialogButtons parses <Buttons><Button CommitState="True"><Calculation>...
func decodeDialogButtons(n *xmlNode) []map[string]interface{} {
	if n == nil {
		return nil
	}
	var out []map[string]interface{}
	for _, ch := range n.Children {
		if ch.Name != "Button" {
			continue
		}
		label := ""
		isCalc := false
		if calc := findFirstChild(ch, "Calculation", "calculation"); calc != nil {
			label = calculationText(calc)
			isCalc = true
		} else if v := firstAttr(ch, "name", "Name", "value"); v != "" {
			label = v
		} else {
			label = strings.TrimSpace(ch.Text)
		}
		commit := true
		if c, ok := xmlAttrBool(ch, "CommitState", "commitState", "commit"); ok {
			commit = c
		}
		out = append(out, map[string]interface{}{
			"label":          label,
			"is_calculation": isCalc,
			"commit":         commit,
		})
	}
	return out
}

// decodeDialogInputFields parses <InputFields><InputField>...
func decodeDialogInputFields(n *xmlNode) []map[string]interface{} {
	if n == nil {
		return nil
	}
	var out []map[string]interface{}
	for _, ch := range n.Children {
		if ch.Name != "InputField" {
			continue
		}
		entry := map[string]interface{}{}
		if fn := findFirstChild(ch, "Field", "FieldReference"); fn != nil {
			if fname := firstAttr(fn, "name", "Name"); fname != "" {
				if strings.HasPrefix(fname, "$") {
					entry["field"] = fname
				} else {
					entry["field"] = sanitizeIdentifier(fname)
					entry["field_label"] = fname
				}
			}
		}
		if lbl := findFirstChild(ch, "Label"); lbl != nil {
			if calc := findFirstChild(lbl, "Calculation", "calculation"); calc != nil {
				entry["label"] = calculationText(calc)
				entry["label_is_calculation"] = true
			} else {
				entry["label"] = strings.TrimSpace(lbl.Text)
			}
		}
		if pwd, ok := xmlAttrBool(ch, "UsePasswordCharacter", "usePasswordCharacter"); ok {
			entry["password"] = pwd
		}
		if len(entry) > 0 {
			out = append(out, entry)
		}
	}
	return out
}

// decodeWindowNode extracts window name, layout destination, style and geometry.
func decodeWindowNode(n *xmlNode, params map[string]interface{}) {
	if n == nil {
		return
	}
	if name := firstAttr(n, "name", "Name", "value"); name != "" {
		if strings.EqualFold(name, "CurrentWindow") || strings.EqualFold(name, "Current") {
			params["window_target"] = "current"
		} else {
			params["window_name"] = name
		}
	}
	for _, ch := range n.Children {
		switch ch.Name {
		case "Name":
			if calc := findFirstChild(ch, "Calculation", "calculation"); calc != nil {
				params["window_name"] = calculationText(calc)
				params["window_name_is_calculation"] = true
			} else if t := strings.TrimSpace(firstAttr(ch, "value")); t != "" {
				params["window_name"] = t
			} else if t := strings.TrimSpace(ch.Text); t != "" {
				params["window_name"] = t
			}
		case "Calculation", "calculation":
			if expr := calculationText(ch); expr != "" {
				params["window_name"] = expr
				params["window_name_is_calculation"] = true
			}
		case "LayoutDestination":
			decodeLayoutDestination(ch, params)
		case "Layout", "LayoutReference":
			if name := firstAttr(ch, "name", "Name"); name != "" {
				params["layout_name"] = name
			}
		case "NewWndStyles", "WindowStyle", "Style":
			if v := firstAttr(ch, "Style", "style", "value", "name"); v != "" {
				params["window_style"] = v
			}
		case "Top", "Left", "Width", "Height":
			key := strings.ToLower(ch.Name)
			if calc := findFirstChild(ch, "Calculation", "calculation"); calc != nil {
				params[key] = calculationText(calc)
			} else if v := firstAttr(ch, "value"); v != "" {
				params[key] = v
			} else if t := strings.TrimSpace(ch.Text); t != "" {
				params[key] = t
			}
		}
	}
}
