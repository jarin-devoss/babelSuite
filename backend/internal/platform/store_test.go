package platform

import (
	"os"
	"path/filepath"
	"testing"

)

func TestDefaultSettingsIncludeAPISIXSidecarOnAgents(t *testing.T) {
	settings := DefaultSettings()
	if len(settings.Agents) == 0 {
		t.Fatal("expected default settings to include execution agents")
	}

	for _, agent := range settings.Agents {
		sidecar := agent.APISIXSidecar
		if sidecar.Image == "" {
			t.Fatalf("expected %s to include an APISIX sidecar image", agent.AgentID)
		}
		if sidecar.ConfigMountPath == "" {
			t.Fatalf("expected %s to include an APISIX config mount path", agent.AgentID)
		}
		if sidecar.ListenPort != 9080 {
			t.Fatalf("expected %s to use listen port 9080, got %d", agent.AgentID, sidecar.ListenPort)
		}
	}
}

func TestNormalizeBackfillsAPISIXSidecarDefaults(t *testing.T) {
	settings := &PlatformSettings{
		Agents: []ExecutionAgent{
			{
				AgentID: "agent-1",
				Name:    "Agent 1",
				Type:    "local",
			},
		},
		Registries: []OCIRegistry{
			{
				RegistryID:  "registry-1",
				Name:        "Registry",
				Provider:    "Generic OCI",
				RegistryURL: "http://registry.internal",
			},
		},
	}

	normalize(settings)

	sidecar := settings.Agents[0].APISIXSidecar
	if sidecar.Image == "" {
		t.Fatal("expected normalize to backfill APISIX image")
	}
	if sidecar.ConfigMountPath == "" {
		t.Fatal("expected normalize to backfill APISIX config path")
	}
	if sidecar.ListenPort == 0 {
		t.Fatalf("expected normalize to backfill the APISIX listen port, got %d", sidecar.ListenPort)
	}
}

// Settings files written before adminPort and capabilities were removed still
// load: the keys name nothing the sidecar has, so they are ignored and dropped on
// the next save.
func TestSettingsWithTheRemovedSidecarKeysStillLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "platform.yaml")
	source := `agents:
  - agentId: local-docker
    name: Local Docker
    type: local
    enabled: true
    default: true
    apisixSidecar:
      image: apache/apisix:3.9.0-debian
      configMountPath: /usr/local/apisix/conf/apisix.yaml
      listenPort: 9080
      adminPort: 9180
      capabilities: [rest, grpc]
registries:
  - registryId: zot
    name: Zot
    provider: zot
    registryUrl: localhost:5000
    allowLocalNetwork: true
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := NewFileStore(path).Load()
	if err != nil {
		t.Fatalf("settings with the removed keys must load: %v", err)
	}
	if len(settings.Agents) == 0 || settings.Agents[0].APISIXSidecar.ListenPort != 9080 {
		t.Fatalf("the sidecar settings were not read: %+v", settings.Agents)
	}
}


func TestFileStoreLoadRequiresConfigWhenDemoDisabledAndFileMissing(t *testing.T) {

	store := NewFileStore(filepath.Join(t.TempDir(), "missing-platform.yaml"))
	if _, err := store.Load(); err == nil {
		t.Fatal("expected missing platform settings file to fail when demo is disabled")
	}
}
