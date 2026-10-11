package platform

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The route is a contract between two languages: the sidecar config gets this
// URI from Go, while plugin.lua decides whether to answer by comparing
// ngx.var.uri against "/_babelsuite/plugins/" .. _M.name .. "/start". If the
// two ever disagree the plugin 404s on its own trigger, so pin the exact shape
// rather than trusting both sides to be edited together.
func TestPluginTriggerRouteMatchesWhatTheLuaChecks(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, want string }{
		{"llm-judge", "/_babelsuite/plugins/llm-judge/start"},
		{"shadow-diff", "/_babelsuite/plugins/shadow-diff/start"},
		{"spice_sim2", "/_babelsuite/plugins/spice_sim2/start"},
		// Trimmed, matching how createPlugin trims before validating.
		{"  llm-judge  ", "/_babelsuite/plugins/llm-judge/start"},
		// No name means no route, rather than a path that would match
		// "/_babelsuite/plugins//start" for anything.
		{"", ""},
		{"   ", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := PluginTriggerRoute(tc.name); got != tc.want {
				t.Fatalf("PluginTriggerRoute(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// Every name the handler accepts has to produce a single-segment path. A name
// carrying a slash or a dot would otherwise build a route that silently
// addresses something else.
func TestAcceptedPluginNamesCannotEscapeTheRoutePath(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"a", "A1", "llm-judge", "spice_sim", "x-_1"} {
		if !pluginNameRe.MatchString(name) {
			t.Fatalf("expected %q to be an accepted plugin name", name)
		}
		route := PluginTriggerRoute(name)
		if strings.Count(route, "/") != 4 {
			t.Fatalf("route %q for name %q is not a single-segment path", route, name)
		}
	}
	for _, name := range []string{"../x", "a/b", "a.b", "-leading", "_leading", "a b", "a?b"} {
		if pluginNameRe.MatchString(name) {
			t.Fatalf("name %q should be rejected but was accepted", name)
		}
	}
}

type fakeStore struct{ settings PlatformSettings }

func (f *fakeStore) Load() (*PlatformSettings, error) {
	copied := f.settings
	copied.Plugins = append([]CustomPlugin{}, f.settings.Plugins...)
	return &copied, nil
}

func (f *fakeStore) Save(settings *PlatformSettings) error {
	f.settings = *settings
	return nil
}

func (f *fakeStore) SyncRegistry(string) (*PlatformSettings, error) {
	return f.Load()
}

// A plugin package no longer declares its trigger, so whatever a client sends
// must be ignored in favour of the derived route. Honouring the body would let
// a stored plugin point at a URI its Lua never answers.
func TestCreatePluginDerivesTheTriggerAndIgnoresTheBody(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	h := NewHandler(store, nil)

	body, err := json.Marshal(CustomPlugin{
		Name:    "llm-judge",
		Trigger: "/whatever/the/client/said",
		Lua:     "local _M = {} return _M",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	rec := httptest.NewRecorder()
	h.createPlugin(rec, httptest.NewRequest(http.MethodPost, "/api/v1/platform/plugins", strings.NewReader(string(body))))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(store.settings.Plugins) != 1 {
		t.Fatalf("expected one stored plugin, got %d", len(store.settings.Plugins))
	}
	if got, want := store.settings.Plugins[0].Trigger, "/_babelsuite/plugins/llm-judge/start"; got != want {
		t.Fatalf("stored trigger = %q, want %q", got, want)
	}
}

// updatePlugin overwrites Name from the URL and ignores the body's, so the
// trigger has to come from the URL too. Deriving it from the body would store
// an empty route whenever a client omitted the name.
func TestUpdatePluginDerivesTheTriggerFromTheURLNotTheBody(t *testing.T) {
	t.Parallel()

	store := &fakeStore{settings: PlatformSettings{
		Plugins: []CustomPlugin{{
			Name:    "llm-judge",
			Trigger: "/_babelsuite/plugins/llm-judge/start",
			Lua:     "local _M = {} return _M",
		}},
	}}
	h := NewHandler(store, nil)

	// Name omitted entirely, and a stale trigger supplied — the two ways a
	// client can disagree with the URL.
	body := `{"trigger":"/stale/route","lua":"local _M = {} return _M"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/platform/plugins/llm-judge", strings.NewReader(body))
	req.SetPathValue("name", "llm-judge")

	rec := httptest.NewRecorder()
	h.updatePlugin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	stored := store.settings.Plugins[0]
	if stored.Name != "llm-judge" {
		t.Fatalf("stored name = %q", stored.Name)
	}
	if got, want := stored.Trigger, "/_babelsuite/plugins/llm-judge/start"; got != want {
		t.Fatalf("stored trigger = %q, want %q", got, want)
	}
}
