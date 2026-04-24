package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/BitaceS/talepanel/api/internal/services"
)

// RequirePermission aborts the request with 403 if the authenticated user
// does not have the specified global permission.
func RequirePermission(permSvc *services.PermissionService, perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := GetUserFromCtx(c)
		if !ok {
			respondError(c, http.StatusUnauthorized, "authentication required")
			return
		}

		has, err := permSvc.HasPermission(c.Request.Context(), user, perm)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "permission check failed")
			return
		}
		if !has {
			respondError(c, http.StatusForbidden, "insufficient permissions")
			return
		}

		c.Next()
	}
}

// RequireServerPermission aborts the request with 403 if the authenticated
// user does not have the specified permission for the server identified by
// the ":id" URL parameter.
func RequireServerPermission(permSvc *services.PermissionService, perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := GetUserFromCtx(c)
		if !ok {
			respondError(c, http.StatusUnauthorized, "authentication required")
			return
		}

		serverIDStr := c.Param("id")
		if serverIDStr == "" {
			respondError(c, http.StatusBadRequest, "missing server ID")
			return
		}

		serverID, err := uuid.Parse(serverIDStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid server ID")
			return
		}

		has, err := permSvc.HasServerPermission(c.Request.Context(), user, serverID, perm)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "permission check failed")
			return
		}
		if !has {
			respondError(c, http.StatusForbidden, "insufficient permissions")
			return
		}

		c.Next()
	}
}
