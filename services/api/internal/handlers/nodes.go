package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tyraxo/talepanel/api/internal/daemon"
	"github.com/tyraxo/talepanel/api/internal/middleware"
	"github.com/tyraxo/talepanel/api/internal/models"
	"github.com/tyraxo/talepanel/api/internal/services"
	"go.uber.org/zap"
)

// NodeHandler groups all daemon-node HTTP handlers.
type NodeHandler struct {
	svc     *services.NodeService
	daemons *daemon.ClientPool
	log     *zap.Logger
}

// NewNodeHandler constructs a NodeHandler.
func NewNodeHandler(svc *services.NodeService, daemons *daemon.ClientPool, log *zap.Logger) *NodeHandler {
	return &NodeHandler{svc: svc, daemons: daemons, log: log}
}

// ─── List ─────────────────────────────────────────────────────────────────────

// ListNodes handles GET /nodes.  Requires admin role.
func (h *NodeHandler) ListNodes(c *gin.Context) {
	nodes, err := h.svc.ListNodes(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list nodes"})
		return
	}

	if nodes == nil {
		nodes = []*models.Node{}
	}

	c.JSON(http.StatusOK, gin.H{"nodes": nodes})
}

// ─── Register ─────────────────────────────────────────────────────────────────

// RegisterNode handles POST /nodes.  Requires admin role.
func (h *NodeHandler) RegisterNode(c *gin.Context) {
	var req services.RegisterNodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	node, plainToken, err := h.svc.RegisterNode(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// The plaintext token is returned exactly once.
	c.JSON(http.StatusCreated, gin.H{
		"node":               node,
		"registration_token": plainToken,
		"warning":            "store the registration_token securely — it will not be shown again",
	})
}

// ─── Get ──────────────────────────────────────────────────────────────────────

// GetNode handles GET /nodes/:id.  Requires admin role.
func (h *NodeHandler) GetNode(c *gin.Context) {
	nodeID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	node, err := h.svc.GetNode(c.Request.Context(), nodeID)
	if err != nil {
		nodeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"node": node})
}

// ─── Delete ───────────────────────────────────────────────────────────────────

// DeleteNode handles DELETE /nodes/:id.  Requires owner role.
func (h *NodeHandler) DeleteNode(c *gin.Context) {
	nodeID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	if err := h.svc.DeleteNode(c.Request.Context(), nodeID); err != nil {
		nodeError(c, err)
		return
	}

	// Evict from the in-memory daemon client pool.
	h.daemons.Remove(nodeID.String())

	c.JSON(http.StatusOK, gin.H{"message": "node deleted"})
}

// ─── Daemon Self-Register ─────────────────────────────────────────────────────

// DaemonSelfRegister handles POST /nodes/:id/register.
// Called by the daemon on startup to push real hardware specs and mark the
// node online.  Protected by DaemonNodeAuth.
func (h *NodeHandler) DaemonSelfRegister(c *gin.Context) {
	nodeID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	authedID, _ := middleware.GetDaemonNodeID(c)
	if authedID != nodeID.String() {
		c.JSON(http.StatusForbidden, gin.H{"error": "token does not match node"})
		return
	}

	var req services.DaemonSelfRegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.DaemonSelfRegister(c.Request.Context(), nodeID, req); err != nil {
		h.log.Error("daemon self-register failed",
			zap.String("node_id", nodeID.String()),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register daemon"})
		return
	}

	h.log.Info("daemon self-registered",
		zap.String("node_id", nodeID.String()),
		zap.Int("cpu_cores", req.CPUCores),
		zap.Int("total_ram_mb", req.TotalRAMMB),
	)
	c.JSON(http.StatusOK, gin.H{"message": "daemon registered"})
}

// ─── Heartbeat ────────────────────────────────────────────────────────────────

// NodeHeartbeat handles POST /nodes/:id/heartbeat.
// Called by the daemon on every heartbeat tick.  In addition to refreshing the
// DB timestamp, it (re-)registers the daemon client in the in-memory pool so
// that power actions can reach the node immediately after a restart.
// Protected by DaemonNodeAuth — the token must belong to the node in the path.
func (h *NodeHandler) NodeHeartbeat(c *gin.Context) {
	nodeID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	// Ensure the authenticated node matches the path parameter — prevents a
	// daemon from spoofing heartbeats for other nodes.
	authedID, _ := middleware.GetDaemonNodeID(c)
	if authedID != nodeID.String() {
		c.JSON(http.StatusForbidden, gin.H{"error": "token does not match node"})
		return
	}

	var req services.NodeHeartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.UpdateNodeHeartbeat(c.Request.Context(), nodeID, req); err != nil {
		nodeError(c, err)
		return
	}

	// Refresh the daemon pool entry using the node's registered FQDN and port.
	// Extract the plaintext token from the Authorization header (already
	// validated by DaemonNodeAuth) to build the daemon HTTP client.
	rawToken := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	node, err := h.svc.GetNode(c.Request.Context(), nodeID)
	if err == nil {
		h.daemons.Register(nodeID.String(), node.FQDN, node.Port, rawToken)
		h.log.Debug("daemon client registered from heartbeat",
			zap.String("node_id", nodeID.String()),
			zap.String("fqdn", node.FQDN),
			zap.Int("port", node.Port),
		)
	} else {
		h.log.Warn("heartbeat: could not refresh daemon pool",
			zap.String("node_id", nodeID.String()),
			zap.Error(err),
		)
	}

	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("heartbeat received from node %s", nodeID)})
}

// ─── GetPendingCommands ───────────────────────────────────────────────────────

// GetPendingCommands handles GET /nodes/:id/commands/pending.
// Protected by DaemonNodeAuth.
func (h *NodeHandler) GetPendingCommands(c *gin.Context) {
	nodeID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	authedID, _ := middleware.GetDaemonNodeID(c)
	if authedID != nodeID.String() {
		c.JSON(http.StatusForbidden, gin.H{"error": "token does not match node"})
		return
	}

	cmds, err := h.svc.GetPendingCommands(c.Request.Context(), nodeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch commands"})
		return
	}

	c.JSON(http.StatusOK, cmds)
}

// ─── AckCommand ───────────────────────────────────────────────────────────────

// AckCommand handles POST /nodes/:id/commands/:cmd_id/ack.
// Protected by DaemonNodeAuth.
func (h *NodeHandler) AckCommand(c *gin.Context) {
	nodeID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	cmdID, ok := parseUUID(c, "cmd_id")
	if !ok {
		return
	}

	authedID, _ := middleware.GetDaemonNodeID(c)
	if authedID != nodeID.String() {
		c.JSON(http.StatusForbidden, gin.H{"error": "token does not match node"})
		return
	}

	var req services.CommandAckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.AckCommand(c.Request.Context(), nodeID, cmdID, req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "command acknowledged"})
}

// ─── Network Stats (proxy to daemon) ──────────────────────────────────────

// GetNetworkStats handles GET /nodes/:id/network-stats.
// Proxies to the daemon's /network-stats endpoint.
func (h *NodeHandler) GetNetworkStats(c *gin.Context) {
	nodeID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	client, err := h.daemons.Get(nodeID.String())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "daemon not available for this node"})
		return
	}

	stats, err := client.GetNetworkStats(c.Request.Context())
	if err != nil {
		h.log.Error("failed to get network stats from daemon",
			zap.String("node_id", nodeID.String()),
			zap.Error(err),
		)
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch network stats from daemon"})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func nodeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrNodeNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
