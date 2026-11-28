package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/control"
	"github.com/file4base/file4base-app/server/internal/fmdapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDataAPIConformanceLayoutContract checks every step of the layout_metadata
// fixtures, past the first difference the conformance replay stops at: a
// layout's fieldMetaData and portalMetaData must be exactly as recorded,
// key order and value types included, and every other reply (sign-in,
// errors, sign-out) exactly as recorded too.
//
// Two things are left out, each until its own phase serves it, and nothing
// else: the valueLists of a reply (the value lists phase), and the entries
// FileMaker Server names <No Access>, a field the account's privilege set
// may not see (field-level access). Such an entry is skipped where the
// recording has it, the other entries keeping their positions.
func TestDataAPIConformanceLayoutContract(t *testing.T) {
	var fixtures []daFixture
	for _, f := range loadDataAPIFixtures(t) {
		if f.Scenario == "metadata/layout_metadata" {
			fixtures = append(fixtures, f)
		}
	}
	require.Len(t, fixtures, 3, "one layout_metadata fixture per API version")

	store, err := control.NewStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, store.Close()) })
	h := newAPIHarness(t, api.Options{
		AllowPublicDatabaseCreation: true,
		ControlStore:                store,
		DataAPI: &fmdapi.Config{Databases: []fmdapi.DatabaseAlias{
			{Name: dataAPIDatabase, Database: dataAPIDatabase},
		}},
	})
	_, exists, err := h.mgr.LookupDatabase(t.Context(), dataAPIDatabase)
	require.NoError(t, err)
	require.False(t, exists, "a database named %s already exists on the test server; drop it first", dataAPIDatabase)
	created := false
	t.Cleanup(func() {
		if created {
			if err := h.mgr.DropDatabase(context.Background(), dataAPIDatabase); err != nil {
				t.Errorf("dropping %s: %v", dataAPIDatabase, err)
			}
		}
	})
	if err := migrateDataAPIReference(t, h, &created); err != nil {
		t.Fatalf("the reference file does not migrate here: %v", err)
	}

	for _, f := range fixtures {
		t.Run(f.Fixture, func(t *testing.T) {
			st := &daState{tokens: map[string]string{}, vars: map[string]string{}}
			described := 0
			for _, step := range f.Steps {
				require.NotNil(t, step.Request, step.ID)
				require.NotNil(t, step.Response, step.ID)
				where := f.Fixture + " step " + step.ID
				res := sendDataAPIRequest(h, st, step)
				require.NoError(t, res.err, where)
				want, ok := layoutDescription(step.Response.Body)
				if !ok {
					for _, d := range compareDataAPIResponse(where, step.Response, res) {
						t.Error(d.msg)
					}
					continue
				}
				described++
				if !assert.Equal(t, step.Response.Status, res.status, "%s: status", where) {
					continue
				}
				got, ok := layoutDescription(res.body)
				if !assert.True(t, ok, "%s: the reply is not a layout description: %s", where, res.body) {
					continue
				}
				compareLayoutDescription(t, where, want, got)
			}
			assert.Equal(t, 11, described, "the fixture describes 11 layouts")
		})
	}
}

// layoutDesc is the part of a layout description the test compares, each
// member as sent.
type layoutDesc struct {
	keys      []string
	fields    []json.RawMessage
	portals   json.RawMessage
	portalKey []string
}

// layoutDescription reads a reply's response when it describes a layout.
func layoutDescription(body []byte) (layoutDesc, bool) {
	var envelope struct {
		Response json.RawMessage `json:"response"`
	}
	if json.Unmarshal(body, &envelope) != nil || len(envelope.Response) == 0 {
		return layoutDesc{}, false
	}
	keys, members, err := daObjectMembers(envelope.Response)
	if err != nil || members["fieldMetaData"] == nil {
		return layoutDesc{}, false
	}
	d := layoutDesc{keys: keys, portals: members["portalMetaData"]}
	if json.Unmarshal(members["fieldMetaData"], &d.fields) != nil {
		return layoutDesc{}, false
	}
	if d.portals != nil {
		pk, _, err := daObjectMembers(d.portals)
		if err != nil {
			return layoutDesc{}, false
		}
		d.portalKey = pk
	}
	return d, true
}

// daObjectMembers reads a JSON object's keys in order, and its members.
func daObjectMembers(raw json.RawMessage) ([]string, map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, nil, fmt.Errorf("not an object")
	}
	var keys []string
	members := map[string]json.RawMessage{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		key, _ := tok.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, nil, err
		}
		keys = append(keys, key)
		members[key] = v
	}
	return keys, members, nil
}

func compareLayoutDescription(t *testing.T, where string, want, got layoutDesc) {
	t.Helper()
	assert.Equal(t, withoutValueLists(want.keys), withoutValueLists(got.keys), "%s: the members of the response", where)

	if assert.Len(t, got.fields, len(want.fields), "%s: fieldMetaData entries", where) {
		for i := range want.fields {
			if noAccessEntry(want.fields[i]) {
				continue
			}
			if d := compareJSONTokens(want.fields[i], got.fields[i]); d != "" {
				t.Errorf("%s: fieldMetaData[%d] %s; got %s", where, i, d, got.fields[i])
			}
		}
	}

	if !assert.Equal(t, want.portalKey, got.portalKey, "%s: the portals of portalMetaData", where) {
		return
	}
	_, wantPortals, _ := daObjectMembers(want.portals)
	_, gotPortals, _ := daObjectMembers(got.portals)
	for _, key := range want.portalKey {
		var w, g []json.RawMessage
		require.NoError(t, json.Unmarshal(wantPortals[key], &w))
		require.NoError(t, json.Unmarshal(gotPortals[key], &g))
		if !assert.Len(t, g, len(w), "%s: portalMetaData[%q] entries", where, key) {
			continue
		}
		for i := range w {
			if noAccessEntry(w[i]) {
				continue
			}
			if d := compareJSONTokens(w[i], g[i]); d != "" {
				t.Errorf("%s: portalMetaData[%q][%d] %s; got %s", where, key, i, d, g[i])
			}
		}
	}
}

// withoutValueLists leaves out the valueLists member, on both sides, until
// the value lists phase serves and checks it.
func withoutValueLists(keys []string) []string {
	out := []string{}
	for _, k := range keys {
		if k != "valueLists" {
			out = append(out, k)
		}
	}
	return out
}

// noAccessEntry is the entry FileMaker Server gives for a field the
// account may not see.
func noAccessEntry(raw json.RawMessage) bool {
	var e struct {
		Name string `json:"name"`
	}
	return json.Unmarshal(raw, &e) == nil && e.Name == "<No Access>"
}
