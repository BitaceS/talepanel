package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/Bitaces/talepanel/api/internal/middleware"
	"github.com/Bitaces/talepanel/api/internal/models"
	"github.com/Bitaces/talepanel/api/internal/services"
	"go.uber.org/zap"
)

// AdminHandler groups all admin-panel HTTP handlers (user management, etc.).
type AdminHandler struct {
	authSvc *services.AuthService
	nodeSvc *services.NodeService
	permSvc *services.PermissionService
	log     *zap.Logger
}

// NewAdminHandler constructs an AdminHandler.
func NewAdminHandler(authSvc *services.AuthService, nodeSvc *services.NodeService, permSvc *services.PermissionService, log *zap.Logger) *AdminHandler {
	return &AdminHandler{authSvc: authSvc, nodeSvc: nodeSvc, permSvc: permSvc, log: log}
}

// ─── Create User ─────────────────────────────────────────────────────────────

type createUserRequest struct {
	Email    string `json:"email"    binding:"required"`
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Role     string `json:"role"     binding:"required"`
}

func (h *AdminHandler) CreateUser(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate role.
	if models.RoleWeight(req.Role) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role; must be user, moderator, admin, or owner"})
		return
	}

	// Only owners can create owners.
	caller, _ := middleware.GetUserFromCtx(c)
	if req.Role == models.RoleOwner && (caller == nil || caller.Role != models.RoleOwner) {
		c.JSON(http.StatusForbidden, gin.H{"error": "only owners can create owner accounts"})
		return
	}

	user, err := h.authSvc.CreateUserByAdmin(c.Request.Context(), req.Email, req.Username, req.Password, req.Role)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"user": user})
}

// ─── List Users ──────────────────────────────────────────────────────────────

func (h *AdminHandler) ListUsers(c *gin.Context) {
	users, err := h.authSvc.ListUsers(c.Request.Context())
	if err != nil {
		h.log.Error("failed to list users", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list users"})
		return
	}
	if users == nil {
		users = []*models.User{}
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}

// ─── Update User Role ────────────────────────────────────────────────────────

type updateRoleRequest struct {
	Role string `json:"role" binding:"required"`
}

func (h *AdminHandler) UpdateUserRole(c *gin.Context) {
	targetID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	var req updateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate role value.
	if models.RoleWeight(req.Role) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role; must be user, moderator, admin, or owner"})
		return
	}

	// Prevent changing own role.
	caller, _ := middleware.GetUserFromCtx(c)
	if caller != nil && caller.ID == targetID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot change your own role"})
		return
	}

	// Only owners can promote to owner.
	if req.Role == models.RoleOwner && (caller == nil || caller.Role != models.RoleOwner) {
		c.JSON(http.StatusForbidden, gin.H{"error": "only owners can promote to owner"})
		return
	}

	if err := h.authSvc.UpdateUserRole(c.Request.Context(), targetID, req.Role); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "role updated"})
}

// ─── Toggle User Active ─────────────────────────────────────────────────────

type toggleActiveRequest struct {
	IsActive bool `json:"is_active"`
}

func (h *AdminHandler) ToggleUserActive(c *gin.Context) {
	targetID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	var req toggleActiveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Prevent disabling own account.
	caller, _ := middleware.GetUserFromCtx(c)
	if caller != nil && caller.ID == targetID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot disable your own account"})
		return
	}

	if err := h.authSvc.SetUserActive(c.Request.Context(), targetID, req.IsActive); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "user status updated"})
}

// ─── Delete User ─────────────────────────────────────────────────────────────

func (h *AdminHandler) DeleteUser(c *gin.Context) {
	targetID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	// Prevent deleting own account.
	caller, _ := middleware.GetUserFromCtx(c)
	if caller != nil && caller.ID == targetID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete your own account"})
		return
	}

	if err := h.authSvc.DeleteUser(c.Request.Context(), targetID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "user deleted"})
}

// ─── Node Activate / Deactivate ──────────────────────────────────────────────

type updateNodeStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

func (h *AdminHandler) UpdateNodeStatus(c *gin.Context) {
	nodeID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	var req updateNodeStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Only allow valid statuses.
	switch req.Status {
	case "online", "offline", "draining":
		// ok
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status; must be online, offline, or draining"})
		return
	}

	if err := h.nodeSvc.SetNodeStatus(c.Request.Context(), nodeID, req.Status); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "node status updated"})
}

// ─── Activity Logs ───────────────────────────────────────────────────────────

func (h *AdminHandler) GetActivityLogs(c *gin.Context) {
	logs, err := h.authSvc.GetActivityLogs(c.Request.Context(), 100)
	if err != nil {
		h.log.Error("failed to get activity logs", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch activity logs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

// ─── User Permissions ─────────────────────────────────────────────────────

// GetUserPermissions handles GET /admin/users/:id/permissions.
func (h *AdminHandler) GetUserPermissions(c *gin.Context) {
	targetID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	perms, err := h.permSvc.GetUserPermissions(c.Request.Context(), targetID)
	if err != nil {
		h.log.Error("failed to get user permissions", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch permissions"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"permissions": perms})
}

// SetUserPermissions handles PUT /admin/users/:id/permissions.
type setPermissionsRequest struct {
	Permissions []struct {
		PermKey string `json:"perm_key" binding:"required"`
		Granted bool   `json:"granted"`
	} `json:"permissions" binding:"required"`
}

func (h *AdminHandler) SetUserPermissions(c *gin.Context) {
	targetID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	var req setPermissionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	for _, p := range req.Permissions {
		if err := h.permSvc.SetUserPermission(c.Request.Context(), targetID, p.PermKey, p.Granted); err != nil {
			h.log.Error("failed to set permission", zap.String("perm", p.PermKey), zap.Error(err))
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "permissions updated"})
}
