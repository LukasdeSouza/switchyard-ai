package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/LukasdeSouza/nexus-ai-gateway/internal/domain"
	"github.com/LukasdeSouza/nexus-ai-gateway/internal/observability"
)

// ProviderKeysStore defines the database operations required by provider keys management.
type ProviderKeysStore interface {
	Create(ctx context.Context, p *domain.ProviderConnection) error
	ListByProject(ctx context.Context, projectID string) ([]*domain.ProviderConnection, error)
	DeleteByProjectAndProvider(ctx context.Context, projectID string, p domain.ProviderID) error
}

// ProviderKeysHandlerConfig holds dependencies for the provider keys router.
type ProviderKeysHandlerConfig struct {
	ProviderStore ProviderKeysStore
	SupabaseURL   string
	SupabaseKey   string
	Logger        *zap.Logger
}

type providerKeysHandler struct {
	store       ProviderKeysStore
	supabaseURL string
	supabaseKey string
	httpClient  *http.Client
	logger      *zap.Logger
}

// NewProviderKeysRouter creates an HTTP router with GET, POST, and DELETE /v1/provider-keys.
func NewProviderKeysRouter(cfg ProviderKeysHandlerConfig) http.Handler {
	h := &providerKeysHandler{
		store:       cfg.ProviderStore,
		supabaseURL: strings.TrimRight(cfg.SupabaseURL, "/"),
		supabaseKey: cfg.SupabaseKey,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		logger:      cfg.Logger,
	}

	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Post("/", h.save)
	r.Delete("/{provider}", h.remove)
	return r
}

func (h *providerKeysHandler) authenticate(r *http.Request) (*supabaseUserResponse, error) {
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, domain.New(domain.CodeUnauthorized, "missing authentication token")
	}
	return validateSupabaseJWT(r.Context(), h.httpClient, h.supabaseURL, h.supabaseKey, token)
}

func maskKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) <= 8 {
		return "***"
	}
	prefix := raw[:min(6, len(raw))]
	suffix := raw[max(0, len(raw)-4):]
	return prefix + "..." + suffix
}

type providerKeyItem struct {
	Provider  string `json:"provider"`
	MaskedKey string `json:"masked_key"`
}

func (h *providerKeysHandler) list(w http.ResponseWriter, r *http.Request) {
	reqID := observability.RequestIDFromContext(r.Context())

	user, err := h.authenticate(r)
	if err != nil {
		writeError(w, reqID, domain.New(domain.CodeUnauthorized, "invalid or expired authentication token"))
		return
	}

	if h.store == nil {
		writeJSON(w, http.StatusOK, []providerKeyItem{})
		return
	}

	projectID := "prj_" + user.ID
	conns, err := h.store.ListByProject(r.Context(), projectID)
	if err != nil {
		h.logger.Error("failed to list provider connections", zap.Error(err), zap.String("project_id", projectID))
		writeError(w, reqID, domain.New(domain.CodeInternal, "failed to retrieve provider keys"))
		return
	}

	result := make([]providerKeyItem, 0, len(conns))
	for _, c := range conns {
		if c.Enabled {
			result = append(result, providerKeyItem{
				Provider:  string(c.Provider),
				MaskedKey: maskKey(c.SecretRef),
			})
		}
	}

	writeJSON(w, http.StatusOK, result)
}

type saveKeyRequest struct {
	Provider string `json:"provider"`
	Key      string `json:"key"`
}

func (h *providerKeysHandler) save(w http.ResponseWriter, r *http.Request) {
	reqID := observability.RequestIDFromContext(r.Context())

	user, err := h.authenticate(r)
	if err != nil {
		writeError(w, reqID, domain.New(domain.CodeUnauthorized, "invalid or expired authentication token"))
		return
	}

	var req saveKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, reqID, domain.New(domain.CodeBadRequest, "invalid request body"))
		return
	}

	req.Provider = strings.ToLower(strings.TrimSpace(req.Provider))
	req.Key = strings.TrimSpace(req.Key)

	if req.Provider == "" || req.Key == "" {
		writeError(w, reqID, domain.New(domain.CodeBadRequest, "provider and key are required"))
		return
	}

	provID := domain.ProviderID(req.Provider)
	switch provID {
	case domain.ProviderGemini, domain.ProviderOpenAI, domain.ProviderAnthropic, domain.ProviderDeepSeek:
	default:
		writeError(w, reqID, domain.New(domain.CodeBadRequest, "unsupported provider: "+req.Provider))
		return
	}

	if h.store == nil {
		writeError(w, reqID, domain.New(domain.CodeInternal, "database connection unavailable"))
		return
	}

	projectID := "prj_" + user.ID
	pconn := domain.NewProviderConnection(projectID, provID, req.Key)
	if err := h.store.Create(r.Context(), pconn); err != nil {
		h.logger.Error("failed to save provider key", zap.Error(err), zap.String("project_id", projectID), zap.String("provider", req.Provider))
		writeError(w, reqID, domain.New(domain.CodeInternal, "failed to save provider key"))
		return
	}

	h.logger.Info("saved provider key", zap.String("project_id", projectID), zap.String("provider", req.Provider))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":       true,
		"provider": req.Provider,
	})
}

func (h *providerKeysHandler) remove(w http.ResponseWriter, r *http.Request) {
	reqID := observability.RequestIDFromContext(r.Context())

	user, err := h.authenticate(r)
	if err != nil {
		writeError(w, reqID, domain.New(domain.CodeUnauthorized, "invalid or expired authentication token"))
		return
	}

	providerParam := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "provider")))
	if providerParam == "" {
		writeError(w, reqID, domain.New(domain.CodeBadRequest, "provider parameter is required"))
		return
	}

	if h.store == nil {
		writeError(w, reqID, domain.New(domain.CodeInternal, "database connection unavailable"))
		return
	}

	projectID := "prj_" + user.ID
	if err := h.store.DeleteByProjectAndProvider(r.Context(), projectID, domain.ProviderID(providerParam)); err != nil {
		if errors.Is(err, domain.ErrNotFound) || domain.CodeOf(err) == domain.CodeNotFound {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.logger.Error("failed to delete provider key", zap.Error(err), zap.String("project_id", projectID), zap.String("provider", providerParam))
		writeError(w, reqID, domain.New(domain.CodeInternal, "failed to delete provider key"))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
