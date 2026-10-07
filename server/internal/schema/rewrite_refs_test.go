package schema

import (
	"reflect"
	"testing"
)

// A layout's portals and buttons name things by id. Import gives those things
// new ids, so the definition has to be rewritten with them (#36).
func TestRewriteRefs(t *testing.T) {
	definition := map[string]interface{}{
		"name": "Companies Form",
		"objects": []interface{}{
			map[string]interface{}{
				"id":   "portal_1",
				"type": "portal",
				"portal_config": map[string]interface{}{
					"relationship_id": "file-rel-1",
					"occurrence":      "Customers",
				},
			},
			map[string]interface{}{
				"id":   "fld_1",
				"type": "field",
				"field_binding": map[string]interface{}{
					"relationship_id": "file-rel-1",
					"field_name":      "company_address",
				},
			},
			map[string]interface{}{
				"id":     "btn_1",
				"type":   "button",
				"action": map[string]interface{}{"script_id": "file-script-1"},
			},
			map[string]interface{}{
				"id":   "portal_2",
				"type": "portal",
				// A relationship the file does not carry: left alone rather
				// than quietly pointed somewhere else.
				"portal_config": map[string]interface{}{"relationship_id": "stranger"},
			},
		},
	}

	rewritten := rewriteRefs(definition, map[string]map[string]string{
		"relationship_id": {"file-rel-1": "db-rel-9"},
		"script_id":       {"file-script-1": "db-script-9"},
	}).(map[string]interface{})

	objects := rewritten["objects"].([]interface{})
	portal := objects[0].(map[string]interface{})["portal_config"].(map[string]interface{})
	if portal["relationship_id"] != "db-rel-9" {
		t.Errorf("portal relationship: got %v, want db-rel-9", portal["relationship_id"])
	}
	if portal["occurrence"] != "Customers" {
		t.Errorf("the occurrence name is not an id and must not be rewritten: got %v", portal["occurrence"])
	}

	binding := objects[1].(map[string]interface{})["field_binding"].(map[string]interface{})
	if binding["relationship_id"] != "db-rel-9" {
		t.Errorf("related field relationship: got %v, want db-rel-9", binding["relationship_id"])
	}

	action := objects[2].(map[string]interface{})["action"].(map[string]interface{})
	if action["script_id"] != "db-script-9" {
		t.Errorf("button script: got %v, want db-script-9", action["script_id"])
	}

	stranger := objects[3].(map[string]interface{})["portal_config"].(map[string]interface{})
	if stranger["relationship_id"] != "stranger" {
		t.Errorf("an id the file does not carry must be left as it is: got %v", stranger["relationship_id"])
	}
}

func TestRewriteRefsLeavesEverythingElseAlone(t *testing.T) {
	before := map[string]interface{}{
		"name":    "Plain",
		"objects": []interface{}{map[string]interface{}{"id": "lbl_1", "text": "Hello"}},
	}
	after := rewriteRefs(before, map[string]map[string]string{"relationship_id": {"a": "b"}})
	if !reflect.DeepEqual(before, after) {
		t.Errorf("a definition with nothing to rewrite changed: %v", after)
	}
}
