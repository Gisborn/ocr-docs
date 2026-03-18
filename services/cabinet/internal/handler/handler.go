package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"scan.passport.local/api/services/cabinet/internal/middleware"
	"scan.passport.local/api/services/cabinet/internal/service"
)

// Handler HTTP обработчики Cabinet Service
type Handler struct {
	authService   *service.AuthService
	apiKeyService *service.APIKeyService
}

// NewHandler создает новый handler
func NewHandler(auth *service.AuthService, apiKey *service.APIKeyService) *Handler {
	return &Handler{
		authService:   auth,
		apiKeyService: apiKey,
	}
}

// Health godoc
// @Summary Health check
// @Description Check if the cabinet service is running
// @Tags health
// @Accept json
// @Produce json
// @Success 200 {object} map[string]string
// @Router /health [get]
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// Register godoc
// @Summary Register new organization
// @Description Register a new organization with admin user
// @Tags auth
// @Accept json
// @Produce json
// @Param request body service.RegisterRequest true "Registration data"
// @Success 201 {object} service.RegisterResponse
// @Failure 400 {object} map[string]string
// @Router /api/v1/auth/register [post]
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req service.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	resp, err := h.authService.Register(r.Context(), &req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// Login godoc
// @Summary Login
// @Description Authenticate and get session token
// @Tags auth
// @Accept json
// @Produce json
// @Param request body service.LoginRequest true "Login credentials"
// @Success 200 {object} service.LoginResponse
// @Failure 401 {object} map[string]string
// @Router /api/v1/auth/login [post]
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req service.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	resp, err := h.authService.Login(r.Context(), &req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid credentials"})
		return
	}

	// Устанавливаем cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    resp.SessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Verify godoc
// @Summary Verify session
// @Description Verify session token and return user info
// @Tags auth
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Router /api/v1/auth/verify [get]
func (h *Handler) Verify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	orgID := middleware.GetOrgID(r.Context())
	userID := middleware.GetUserID(r.Context())
	billingAccountID := middleware.GetBillingAccountID(r.Context())

	if orgID == 0 || userID == 0 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// Get org details
	org, err := h.authService.GetOrgByID(r.Context(), orgID)
	if err != nil {
		http.Error(w, `{"error":"organization not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"org_id":             orgID,
		"user_id":            userID,
		"billing_account_id": billingAccountID,
		"email":              org.Email,
		"org_name":           org.Name,
	})
}

// Logout godoc
// @Summary Logout
// @Description Invalidate session
// @Tags auth
// @Accept json
// @Produce json
// @Success 200 {object} map[string]string
// @Router /api/v1/auth/logout [post]
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Получаем токен
	token := extractToken(r)
	if token != "" {
		h.authService.Logout(r.Context(), token)
	}

	// Удаляем cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "logged out"})
}

// CreateAPIKey godoc
// @Summary Create API key
// @Description Create a new API key for the organization
// @Tags api-keys
// @Accept json
// @Produce json
// @Param request body service.CreateAPIKeyRequest true "API key name"
// @Success 201 {object} service.CreateAPIKeyResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /api/v1/api-keys [post]
func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	orgID := middleware.GetOrgID(r.Context())
	userID := middleware.GetUserID(r.Context())

	if orgID == 0 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req service.CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	resp, err := h.apiKeyService.CreateAPIKey(r.Context(), orgID, userID, &req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// ListAPIKeys godoc
// @Summary List API keys
// @Description Get list of API keys for the organization
// @Tags api-keys
// @Accept json
// @Produce json
// @Success 200 {array} service.APIKeyInfo
// @Failure 401 {object} map[string]string
// @Router /api/v1/api-keys [get]
func (h *Handler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	orgID := middleware.GetOrgID(r.Context())
	if orgID == 0 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	keys, err := h.apiKeyService.ListAPIKeys(r.Context(), orgID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

// RevokeAPIKey godoc
// @Summary Revoke API key
// @Description Revoke an API key
// @Tags api-keys
// @Accept json
// @Produce json
// @Param id path int true "API Key ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /api/v1/api-keys/{id} [delete]
func (h *Handler) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	orgID := middleware.GetOrgID(r.Context())
	userID := middleware.GetUserID(r.Context())

	if orgID == 0 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// Извлекаем ID из пути
	keyID, err := extractIDFromPath(r.URL.Path, "/api-keys/")
	if err != nil {
		http.Error(w, `{"error":"invalid key id"}`, http.StatusBadRequest)
		return
	}

	if err := h.apiKeyService.RevokeAPIKey(r.Context(), orgID, keyID, userID); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "revoked"})
}

// Вспомогательные функции

func extractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if cookie, err := r.Cookie("session"); err == nil {
		return cookie.Value
	}
	return ""
}

func extractIDFromPath(path, prefix string) (int64, error) {
	idx := strings.Index(path, prefix)
	if idx == -1 {
		return 0, strconv.ErrRange
	}
	idStr := path[idx+len(prefix):]
	// Убираем trailing slash
	idStr = strings.TrimSuffix(idStr, "/")
	return strconv.ParseInt(idStr, 10, 64)
}
