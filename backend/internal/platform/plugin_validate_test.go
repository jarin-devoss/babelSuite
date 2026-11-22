package platform

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func validateRequest(t *testing.T, schema, config string) map[string]any {
	t.Helper()
	store := &fakeStore{}
	store.settings.Plugins = []CustomPlugin{{Name: "checker", Schema: schema}}
	h := NewHandler(store, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/platform-settings/plugins/checker/validate", strings.NewReader(`{"config": `+config+`}`))
	req.SetPathValue("name", "checker")
	rec := httptest.NewRecorder()
	h.validatePluginConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var reply map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return reply
}

const validateSchema = `
target:  string
max_lag: int & >=0 | *1000
`

func TestValidatePluginConfigAcceptsAConfigThatFitsTheSchema(t *testing.T) {
	t.Parallel()
	reply := validateRequest(t, validateSchema, `{"target": "http://x", "max_lag": 5}`)
	if reply["valid"] != true {
		t.Fatalf("expected valid, got %v", reply)
	}
}

func TestValidatePluginConfigNamesTheFieldThatBreaksTheSchema(t *testing.T) {
	t.Parallel()
	reply := validateRequest(t, validateSchema, `{"target": "http://x", "max_lag": -1}`)
	if reply["valid"] != false || !strings.Contains(reply["error"].(string), "max_lag") {
		t.Fatalf("expected the field to be named, got %v", reply)
	}
}

// The runner refuses a config with a required setting missing, so this
// endpoint, which answers "would the runner accept it", must too.
func TestValidatePluginConfigRefusesAMissingRequiredSetting(t *testing.T) {
	t.Parallel()
	reply := validateRequest(t, validateSchema, `{}`)
	if reply["valid"] != false || !strings.Contains(reply["error"].(string), "target is required") {
		t.Fatalf("expected the missing setting to be named, got %v", reply)
	}
}
