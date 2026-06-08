package platform

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"cuelang.org/go/cue/cuecontext"

	"github.com/babelsuite/babelsuite/internal/auth"
	"github.com/babelsuite/babelsuite/internal/httpserver"
)

var (
	pluginNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	pluginCueCtx = cuecontext.New()
)

type Handler struct {
	store Store
	jwt   *auth.JWTService
}

func NewHandler(store Store, jwt *auth.JWTService) *Handler {
	return &Handler{store: store, jwt: jwt}
}

func (h *Handler) Register(mux *http.ServeMux) {
	protected := auth.RequireSession(h.jwt, auth.VerifyOptions{})
	admin := auth.RequireAdmin(h.jwt)
	httpserver.HandleFunc(mux, "GET /api/v1/platform-settings", h.getSettings, protected)
	httpserver.HandleFunc(mux, "PUT /api/v1/platform-settings", h.updateSettings, admin)
	httpserver.HandleFunc(mux, "POST /api/v1/platform-settings/registries/{registryId}/sync", h.syncRegistry, admin)
	httpserver.HandleFunc(mux, "GET /api/v1/platform-settings/plugins", h.listPlugins, protected)
	httpserver.HandleFunc(mux, "POST /api/v1/platform-settings/plugins", h.createPlugin, admin)
	httpserver.HandleFunc(mux, "DELETE /api/v1/platform-settings/plugins/{name}", h.deletePlugin, admin)
	httpserver.HandleFunc(mux, "GET /api/v1/platform-settings/plugins/{name}/check", h.checkPlugin, protected)
	httpserver.HandleFunc(mux, "POST /api/v1/platform-settings/plugins/{name}/validate", h.validatePluginConfig, protected)
}

func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.store.Load()
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not load platform settings.")
		return
	}
	out := *settings
	out.Notifications.SMTP.Password = ""
	httpserver.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) updateSettings(w http.ResponseWriter, r *http.Request) {
	var settings PlatformSettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "Invalid platform settings payload.")
		return
	}

	// Preserve existing SMTP password when client sends an empty string (never echoed back).
	if settings.Notifications.SMTP.Password == "" {
		existing, err := h.store.Load()
		if err == nil && existing != nil {
			settings.Notifications.SMTP.Password = existing.Notifications.SMTP.Password
		}
	}

	normalize(&settings)
	if err := validate(&settings); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.store.Save(&settings); err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not save platform settings.")
		return
	}
	out := settings
	out.Notifications.SMTP.Password = ""
	httpserver.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) syncRegistry(w http.ResponseWriter, r *http.Request) {
	registryID := strings.TrimSpace(r.PathValue("registryId"))
	if registryID == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "Registry ID is required.")
		return
	}

	settings, err := h.store.SyncRegistry(registryID)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			httpserver.WriteError(w, http.StatusNotFound, "Registry not found.")
		default:
			httpserver.WriteError(w, http.StatusInternalServerError, "Could not sync registry.")
		}
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, redactSettings(settings))
}

func (h *Handler) listPlugins(w http.ResponseWriter, r *http.Request) {
	settings, err := h.store.Load()
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not load platform settings.")
		return
	}
	plugins := settings.Plugins
	if plugins == nil {
		plugins = []CustomPlugin{}
	}
	httpserver.WriteJSON(w, http.StatusOK, plugins)
}

func (h *Handler) createPlugin(w http.ResponseWriter, r *http.Request) {
	var plugin CustomPlugin
	if err := json.NewDecoder(r.Body).Decode(&plugin); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "Invalid plugin payload.")
		return
	}
	plugin.Name = strings.TrimSpace(plugin.Name)
	if plugin.Name == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "Plugin name is required.")
		return
	}
	if !pluginNameRe.MatchString(plugin.Name) {
		httpserver.WriteError(w, http.StatusBadRequest, "Plugin name must start with a letter or digit and contain only letters, digits, hyphens, and underscores.")
		return
	}
	if strings.TrimSpace(plugin.Trigger) == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "Plugin trigger route is required.")
		return
	}
	if strings.TrimSpace(plugin.Lua) == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "Plugin Lua source is required.")
		return
	}

	settings, err := h.store.Load()
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not load platform settings.")
		return
	}
	for _, p := range settings.Plugins {
		if p.Name == plugin.Name {
			httpserver.WriteError(w, http.StatusConflict, "A plugin with that name already exists.")
			return
		}
	}
	settings.Plugins = append(settings.Plugins, plugin)
	if err := h.store.Save(settings); err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not save plugin.")
		return
	}
	httpserver.WriteJSON(w, http.StatusCreated, plugin)
}

func (h *Handler) deletePlugin(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "Plugin name is required.")
		return
	}
	settings, err := h.store.Load()
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not load platform settings.")
		return
	}
	filtered := settings.Plugins[:0]
	found := false
	for _, p := range settings.Plugins {
		if p.Name == name {
			found = true
			continue
		}
		filtered = append(filtered, p)
	}
	if !found {
		httpserver.WriteError(w, http.StatusNotFound, "Plugin not found.")
		return
	}
	settings.Plugins = filtered
	if err := h.store.Save(settings); err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not delete plugin.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) checkPlugin(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	settings, err := h.store.Load()
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not load platform settings.")
		return
	}
	var plugin *CustomPlugin
	for i := range settings.Plugins {
		if settings.Plugins[i].Name == name {
			plugin = &settings.Plugins[i]
			break
		}
	}
	if plugin == nil {
		httpserver.WriteError(w, http.StatusNotFound, "Plugin not found.")
		return
	}

	type checkResult struct {
		Name       string   `json:"name"`
		OK         bool     `json:"ok"`
		Issues     []string `json:"issues,omitempty"`
		Deprecated bool     `json:"deprecated,omitempty"`
	}
	result := checkResult{Name: plugin.Name, OK: true, Deprecated: plugin.Deprecated}

	if !strings.HasPrefix(plugin.Trigger, "/") {
		result.Issues = append(result.Issues, "trigger must start with /")
	}
	if plugin.Schema != "" {
		v := pluginCueCtx.CompileString(plugin.Schema)
		if v.Err() != nil {
			result.Issues = append(result.Issues, "schema CUE parse error: "+v.Err().Error())
		}
	}
	if len(result.Issues) > 0 {
		result.OK = false
	}
	httpserver.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) validatePluginConfig(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	settings, err := h.store.Load()
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not load platform settings.")
		return
	}
	var plugin *CustomPlugin
	for i := range settings.Plugins {
		if settings.Plugins[i].Name == name {
			plugin = &settings.Plugins[i]
			break
		}
	}
	if plugin == nil {
		httpserver.WriteError(w, http.StatusNotFound, "Plugin not found.")
		return
	}
	if plugin.Schema == "" {
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"valid": true, "note": "plugin has no schema defined"})
		return
	}

	var payload struct {
		Config map[string]any `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}

	schema := pluginCueCtx.CompileString(plugin.Schema)
	if schema.Err() != nil {
		httpserver.WriteError(w, http.StatusUnprocessableEntity, "Plugin schema is invalid CUE: "+schema.Err().Error())
		return
	}

	configJSON, err := json.Marshal(payload.Config)
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "Could not marshal config.")
		return
	}
	configVal := pluginCueCtx.CompileBytes(configJSON)
	unified := schema.Unify(configVal)
	if err := unified.Validate(); err != nil {
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"valid": false, "error": err.Error()})
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"valid": true})
}

// redactSettings returns a shallow copy of settings with all secret fields zeroed out.
func redactSettings(s *PlatformSettings) *PlatformSettings {
	if s == nil {
		return nil
	}
	out := *s
	out.Secrets = SecretsConfig{}
	out.Registries = make([]OCIRegistry, len(s.Registries))
	for i, reg := range s.Registries {
		reg.Secret = ""
		out.Registries[i] = reg
	}
	out.Notifications.SMTP.Password = ""
	return &out
}
