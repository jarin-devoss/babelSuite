//go:build integration

package runner

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// The subject has a required field b. The new schema removes it: that is
// BACKWARD compatible (a new reader can read old data) and not FORWARD or FULL
// compatible (an old reader needs b).
const (
	schemaWithB    = `{"type":"record","name":"T","fields":[{"name":"a","type":"string"},{"name":"b","type":"string"}]}`
	schemaWithoutB = `{"type":"record","name":"T","fields":[{"name":"a","type":"string"}]}`
)

var subjectCounter atomic.Int64

// registerSubject registers schema under a fresh subject and returns its name.
func registerSubject(t *testing.T, stack *kafkaStack, schema string) string {
	t.Helper()
	subject := fmt.Sprintf("it-orders-%d-value", subjectCounter.Add(1))
	body := fmt.Sprintf(`{"schema": %q, "schemaType": "AVRO"}`, schema)
	req, _ := http.NewRequest(http.MethodPost, stack.RegistryLocal+"/subjects/"+subject+"/versions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/vnd.schemaregistry.v1+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("register %s: %v", subject, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("register %s: HTTP %d %s", subject, resp.StatusCode, data)
	}
	return subject
}

// registrySubjects lists every subject the registry holds, soft-deleted ones
// included, so a leftover scratch subject cannot hide.
func registrySubjects(t *testing.T, stack *kafkaStack) string {
	t.Helper()
	resp, err := http.Get(stack.RegistryLocal + "/subjects?deleted=true")
	if err != nil {
		t.Fatalf("list subjects: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return string(data)
}

func compatConfig(stack *kafkaStack, subject, mode string) map[string]any {
	return map[string]any{
		"registry_url": stack.RegistryURL, "subject": subject, "new_schema": schemaWithoutB, "mode": mode,
	}
}

func TestIntegrationPluginSchemaModeIsReallyApplied(t *testing.T) {
	stack := startKafka(t)
	subject := registerSubject(t, stack, schemaWithB)

	if run := runPlugin(t, "schema-compat", "check_compat", compatConfig(stack, subject, "BACKWARD")); !run.Passed() {
		t.Fatalf("removing a field is BACKWARD compatible: %v", run.Err)
	}
	if run := runPlugin(t, "schema-compat", "check_compat", compatConfig(stack, subject, "FORWARD")); run.Passed() {
		t.Fatalf("removing a required field is not FORWARD compatible: the step must fail. Log: %v", run.Log.texts())
	} else if !run.HasFinding("schema.incompatible", "error") {
		t.Fatalf("expected a critical schema.incompatible finding, got %v", run.Findings())
	}
	if run := runPlugin(t, "schema-compat", "check_compat", compatConfig(stack, subject, "FULL")); run.Passed() {
		t.Fatal("removing a required field is not FULL compatible")
	}
}

func TestIntegrationPluginSchemaEnforceUsesFull(t *testing.T) {
	stack := startKafka(t)
	subject := registerSubject(t, stack, schemaWithB)
	run := runPlugin(t, "schema-compat", "enforce", map[string]any{
		"registry_url": stack.RegistryURL, "subject": subject, "new_schema": schemaWithoutB,
	})
	if run.Passed() {
		t.Fatalf("enforce (FULL) must fail for a removed required field. Log: %v", run.Log.texts())
	}
}

func TestIntegrationPluginSchemaScratchSubjectIsRemoved(t *testing.T) {
	stack := startKafka(t)
	subject := registerSubject(t, stack, schemaWithB)
	_ = runPlugin(t, "schema-compat", "check_compat", compatConfig(stack, subject, "FORWARD"))
	if leftover := registrySubjects(t, stack); strings.Contains(leftover, "__babelsuite_") {
		t.Fatalf("a scratch subject was left in the registry: %s", leftover)
	}
}

func TestIntegrationPluginSchemaScratchSubjectIsRemovedWhenTheCheckErrors(t *testing.T) {
	stack := startKafka(t)
	subject := registerSubject(t, stack, schemaWithB)
	cfg := compatConfig(stack, subject, "FORWARD")
	cfg["new_schema"] = "this is not an avro schema"
	run := runPlugin(t, "schema-compat", "check_compat", cfg)
	if run.Passed() {
		t.Fatal("a schema the registry cannot parse must fail the step")
	}
	if leftover := registrySubjects(t, stack); strings.Contains(leftover, "__babelsuite_") {
		t.Fatalf("a scratch subject was left behind after an error: %s", leftover)
	}
}

func TestIntegrationPluginSchemaCheckAgainstTheSubjectsOwnModeWritesNothing(t *testing.T) {
	stack := startKafka(t)
	subject := registerSubject(t, stack, schemaWithB)
	// The registry's global mode is BACKWARD, which is what this subject holds.
	run := runPlugin(t, "schema-compat", "check_compat", compatConfig(stack, subject, "BACKWARD"))
	if !run.Passed() {
		t.Fatalf("expected a pass: %v", run.Err)
	}
	if strings.Contains(registrySubjects(t, stack), "__babelsuite_") {
		t.Fatal("a check under the subject's own mode must not write to the registry")
	}
}

func TestIntegrationPluginSchemaReplyWithoutIsCompatibleIsAnError(t *testing.T) {
	// A registry whose compatibility answer says nothing: it must not read as compatible.
	url := stub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/config") {
			_, _ = w.Write([]byte(`{"compatibilityLevel":"BACKWARD"}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	run := runPlugin(t, "schema-compat", "check_compat", map[string]any{
		"registry_url": url, "subject": "s", "new_schema": schemaWithoutB, "mode": "BACKWARD",
	})
	if run.Passed() || !strings.Contains(run.Err.Error(), "no boolean is_compatible") {
		t.Fatalf("expected a named failure, got %v", run.Err)
	}
}

func TestIntegrationPluginSchemaUnsupportedModeIsRefusedByTheSchema(t *testing.T) {
	stack := startKafka(t)
	subject := registerSubject(t, stack, schemaWithB)
	run := runPlugin(t, "schema-compat", "check_compat", compatConfig(stack, subject, "SIDEWAYS"))
	if run.Passed() || !strings.Contains(run.Err.Error(), "plugin schema-compat config") || !strings.Contains(run.Err.Error(), `"SIDEWAYS"`) {
		t.Fatalf("expected the config to be refused before dispatch, naming the value, got %v", run.Err)
	}
}

func TestIntegrationPluginSchemaUnsupportedModeIsRefusedByThePluginToo(t *testing.T) {
	stack := startKafka(t)
	subject := registerSubject(t, stack, schemaWithB)
	// Posted directly, past the runner's schema check: the plugin guards itself.
	status, body := postPlugin(t, "schema-compat", fmt.Sprintf(
		`{"op":"check_compat","config":{"registry_url":%q,"subject":%q,"new_schema":%q,"mode":"SIDEWAYS"}}`,
		stack.RegistryURL, subject, schemaWithoutB))
	if status != 500 || !strings.Contains(body, "unsupported mode SIDEWAYS") {
		t.Fatalf("expected the plugin to refuse the mode, got %d %s", status, body)
	}
}

func TestIntegrationPluginSchemaCheckRegistered(t *testing.T) {
	stack := startKafka(t)
	subject := registerSubject(t, stack, schemaWithB)
	found := runPlugin(t, "schema-compat", "check_registered", map[string]any{
		"registry_url": stack.RegistryURL, "subject": subject, "new_schema": schemaWithB,
	})
	if !found.Passed() {
		t.Fatalf("the schema is registered: %v", found.Err)
	}
	missing := runPlugin(t, "schema-compat", "check_registered", map[string]any{
		"registry_url": stack.RegistryURL, "subject": subject, "new_schema": schemaWithoutB,
	})
	if missing.Passed() || !missing.HasFinding("schema.not_registered", "error") {
		t.Fatalf("an unregistered schema must be a critical finding: err=%v findings=%v", missing.Err, missing.Findings())
	}
}

func TestIntegrationPluginSchemaCheckMode(t *testing.T) {
	stack := startKafka(t)
	subject := registerSubject(t, stack, schemaWithB)
	ok := runPlugin(t, "schema-compat", "check_mode", map[string]any{
		"registry_url": stack.RegistryURL, "subject": subject, "mode": "BACKWARD",
	})
	if !ok.Passed() {
		t.Fatalf("the subject enforces BACKWARD: %v", ok.Err)
	}
	wrong := runPlugin(t, "schema-compat", "check_mode", map[string]any{
		"registry_url": stack.RegistryURL, "subject": subject, "mode": "FULL",
	})
	if wrong.Passed() || !wrong.HasFinding("schema.mode_mismatch", "error") {
		t.Fatalf("a different mode must be a finding: err=%v findings=%v", wrong.Err, wrong.Findings())
	}
}

func TestIntegrationPluginSchemaSubjectWithoutVersionsFails(t *testing.T) {
	stack := startKafka(t)
	run := runPlugin(t, "schema-compat", "check_compat", compatConfig(stack, "it-never-registered-value", "FORWARD"))
	if run.Passed() || !strings.Contains(run.Err.Error(), "no registered version") {
		t.Fatalf("expected the missing subject to be named, got %v", run.Err)
	}
}
