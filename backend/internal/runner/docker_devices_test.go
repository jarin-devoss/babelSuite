package runner

import "testing"

// The Docker half of the device table in docs/profiles.md.
func TestBuildDeviceConfig(t *testing.T) {
	t.Parallel()

	requests, mappings := buildDeviceConfig([]string{"gpu", "gpu:2", "/dev/ttyUSB0", "bogus"})

	if len(requests) != 2 {
		t.Fatalf("device requests = %d, want 2", len(requests))
	}
	if r := requests[0]; r.Driver != "nvidia" || r.Count != -1 || r.Capabilities[0][0] != "gpu" {
		t.Errorf(`"gpu" = %+v, want every nvidia GPU`, r)
	}
	if r := requests[1]; r.Count != 2 {
		t.Errorf(`"gpu:2" count = %d, want 2`, r.Count)
	}
	if len(mappings) != 1 || mappings[0].PathOnHost != "/dev/ttyUSB0" || mappings[0].PathInContainer != "/dev/ttyUSB0" || mappings[0].CgroupPermissions != "rwm" {
		t.Errorf("device mappings = %+v, want /dev/ttyUSB0 passed through rwm", mappings)
	}

	if r, m := buildDeviceConfig(nil); r != nil || m != nil {
		t.Errorf("no devices must add nothing, got %v %v", r, m)
	}
}
