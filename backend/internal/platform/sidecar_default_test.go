package platform

import (
	"strings"
	"testing"

	"github.com/babelsuite/babelsuite/internal/apisix"
)

func TestTheDefaultSidecarImageIsPinnedToTheTestedVersion(t *testing.T) {
	image := defaultAPISIXSidecar().Image
	if image != apisix.DefaultSidecarImage {
		t.Fatalf("default image = %q, want %q", image, apisix.DefaultSidecarImage)
	}
	if strings.HasSuffix(image, ":latest") || !strings.Contains(image, ":") {
		t.Fatalf("the default image must name a version, got %q", image)
	}
}
