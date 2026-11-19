package handlers

import (
	"errors"
	"net/http"

	"github.com/BitaceS/talepanel/api/internal/models"
	"github.com/BitaceS/talepanel/api/internal/services"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// APIKeyHandler serves /api-keys: a signed-in user's own API keys. API keys
// themselves cannot reach these routes (see models.APIKeyRoutes).
type APIKeyHandler struct {
	svc *services.APIKeyService
	log *zap.Logger
}

func NewAPIKeyHandler(svc *services.APIKeyService, log *zap.Logger) *APIKeyHandler {
	return &APIKeyHandler{svc: svc, log: log}
}

// List handles GET /api-keys.
func (h *APIKeyHandler) List(c *gin.Context) {
	user := mustUser(c)
	keys, err := h.svc.List(c.Request.Context(), user.ID)
	if err != nil {
		h.log.Error("listing API keys failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list API keys"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"api_keys": keys, "scopes": models.APIKeyScopes})
}

// Create handles POST /api-keys. The response carries the key once.
func (h *APIKeyHandler) Create(c *gin.Context) {
	user := mustUser(c)
	var req services.CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	k, key, err := h.svc.Create(c.Request.Context(), user, req)
	if errors.Is(err, services.ErrInvalidAPIKey) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		h.log.Error("creating API key failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create the API key"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"api_key": k, "key": key})
}

// Delete handles DELETE /api-keys/:keyId (revokes the key at once).
func (h *APIKeyHandler) Delete(c *gin.Context) {
	user := mustUser(c)
	keyID, ok := parseUUID(c, "keyId")
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), user.ID, keyID); err != nil {
		if errors.Is(err, services.ErrAPIKeyNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
			return
		}
		h.log.Error("deleting API key failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not delete the API key"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "API key revoked"})
}
