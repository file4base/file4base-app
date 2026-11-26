package fmdapi

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// recordedMessage is an error message FileMaker Server sent in a fixture.
type recordedMessage struct {
	where        string
	status       int
	code         string
	message      string
	messageFirst bool
}

// recordedMessages reads every error message of the conformance fixtures.
func recordedMessages(t *testing.T) []recordedMessage {
	t.Helper()
	files, err := filepath.Glob("../../testdata/dataapi/fixtures/*/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	var out []recordedMessage
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		type response struct {
			Status int `json:"status"`
			Body   *struct {
				Messages []json.RawMessage `json:"messages"`
			} `json:"body"`
		}
		type exchange struct {
			ID       string    `json:"id"`
			Response *response `json:"response"`
		}
		var f struct {
			Steps []struct {
				exchange
				Concurrent *struct {
					Background exchange `json:"background"`
					Probe      exchange `json:"probe"`
				} `json:"concurrent"`
			} `json:"steps"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		var exchanges []exchange
		for _, s := range f.Steps {
			exchanges = append(exchanges, s.exchange)
			if s.Concurrent != nil {
				exchanges = append(exchanges, s.Concurrent.Background, s.Concurrent.Probe)
			}
		}
		for _, x := range exchanges {
			if x.Response == nil || x.Response.Body == nil {
				continue
			}
			for _, m := range x.Response.Body.Messages {
				var msg struct{ Code, Message string }
				if err := json.Unmarshal(m, &msg); err != nil || msg.Code == "0" {
					continue
				}
				out = append(out, recordedMessage{
					where:        filepath.Base(file) + " " + x.ID,
					status:       x.Response.Status,
					code:         msg.Code,
					message:      msg.Message,
					messageFirst: firstKey(m) == "message",
				})
			}
		}
	}
	return out
}

// firstKey is the first key of a JSON object.
func firstKey(obj json.RawMessage) string {
	dec := json.NewDecoder(bytes.NewReader(obj))
	if _, err := dec.Token(); err != nil {
		return ""
	}
	key, _ := dec.Token()
	s, _ := key.(string)
	return s
}

// TestEngineCatalogueMatchesFixtures checks every engine reply recorded
// against the catalogue: same status, same message.
func TestEngineCatalogueMatchesFixtures(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range recordedMessages(t) {
		if m.messageFirst {
			continue
		}
		e, ok := engineErrors[m.code]
		switch {
		case !ok:
			t.Errorf("%s: code %s is not in the catalogue", m.where, m.code)
		case e.status != m.status:
			t.Errorf("%s: code %s has status %d, catalogue %d", m.where, m.code, m.status, e.status)
		case e.message != "" && e.message != m.message:
			t.Errorf("%s: code %s says %q, catalogue %q", m.where, m.code, m.message, e.message)
		}
		seen[m.code] = true
	}
	for code := range engineErrors {
		if !seen[code] && code != "-1" && code != "3" {
			t.Errorf("code %s is in the catalogue but in no fixture", code)
		}
	}
}

// TestFrontErrorsAreRecorded checks that each front-end reply File4Base
// sends as is was recorded so, message first.
func TestFrontErrorsAreRecorded(t *testing.T) {
	recorded := recordedMessages(t)
	for _, e := range []*Error{
		errMethodNotAllowed, errAuthorizationRequired, errBasicInvalid,
		errLoginCredentialsMissing, errAuthorizationInvalid,
		contentTypeError("text/plain", consumesJSON),
		contentTypeError("application/json", consumesMultipart),
		bodyError("Expected property name or '}' in JSON at position 1 (line 1 column 2)"),
		parameterError("Unknown parameter(s): foo"),
		frontError(400, "10", "Missing required property: query"),
	} {
		found := false
		for _, m := range recorded {
			if m.messageFirst && m.status == e.Status && m.code == e.Code && m.message == e.Message {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%d %s %q is not recorded as a front-end reply", e.Status, e.Code, e.Message)
		}
	}
}

func TestEngineErrorWithoutMessagePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("engineError(104) did not panic")
		}
	}()
	engineError("104")
}
