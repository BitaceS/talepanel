package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/Bitaces/talepanel/api/internal/daemon"
	"github.com/Bitaces/talepanel/api/internal/middleware"
	"github.com/Bitaces/talepanel/api/internal/models"
	"github.com/Bitaces/talepanel/api/internal/services"
	"go.uber.org/zap"
)

// ServerHandler groups all game-server HTTP handlers.
type ServerHandler struct {
	svc            *services.ServerService
	nodeSvc        *services.NodeService
	daemons        *daemon.ClientPool
	serversBaseDir string // base dir on daemon nodes, e.g. /srv/taledaemon/servers
	log            *zap.Logger
}

// NewServerHandler constructs a ServerHandler.
func NewServerHandler(svc *services.ServerService, nodeSvc *services.NodeService, daemons *daemon.ClientPool, serversBaseDir string, log *zap.Logger) *ServerHandler {
	return &ServerHandler{
		svc:            svc,
		nodeSvc:        nodeSvc,
		daemons:        daemons,
		serversBaseDir: serversBaseDir,
		log:            log,
	}
}

// ─── List ─────────────────────────────────────────────────────────────────────

// ListServers handles GET /servers.
func (h *ServerHandler) ListServers(c *gin.Context) {
	user := mustUser(c)

	servers, err := h.svc.ListServers(c.Request.Context(), user.ID, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list servers"})
		return
	}

	if servers == nil {
		servers = []*models.Server{}
	}

	c.JSON(http.StatusOK, gin.H{"servers": servers})
}

// ─── Create ───────────────────────────────────────────────────────────────────

// CreateServer handles POST /servers.
func (h *ServerHandler) CreateServer(c *gin.Context) {
	user := mustUser(c)

	var req services.CreateServerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// DataPath is computed here so it is persisted in the DB before the
	// async provision task runs.  The path is absolute on the daemon node.
	// server UUID is not yet known, but will be appended by the service layer
	// via a post-insert rewrite; we pre-seed with a placeholder that the
	// service will replace using the generated ID.
	// NOTE: We pass an empty DataPath; the service generates the UUID and we
	// reconstruct the path after the insert returns the server record.
	server, err := h.svc.CreateServer(c.Request.Context(), req, user.ID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrNoAvailableNode):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no available node to host the server"})
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		}
		return
	}

	// Compute and persist the data path now that we have the server UUID.
	dataPath := h.serversBaseDir + "/" + server.ID.String()
	if err := h.svc.UpdateServerDataPath(c.Request.Context(), server.ID, dataPath); err != nil {
		h.log.Warn("failed to set server data_path", zap.String("server_id", server.ID.String()), zap.Error(err))
	} else {
		server.DataPath = dataPath
	}

	// Kick off file provisioning on the assigned daemon node asynchronously.
	go h.dispatchProvision(server)

	c.JSON(http.StatusCreated, gin.H{"server": server})
}

// ─── Get ──────────────────────────────────────────────────────────────────────

// GetServer handles GET /servers/:id.
func (h *ServerHandler) GetServer(c *gin.Context) {
	user := mustUser(c)

	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	server, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role)
	if err != nil {
		serverError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"server": server})
}

// ─── Update ───────────────────────────────────────────────────────────────────

// UpdateServer handles PATCH /servers/:id.
func (h *ServerHandler) UpdateServer(c *gin.Context) {
	user := mustUser(c)

	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	var req services.UpdateServerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	server, err := h.svc.UpdateServer(c.Request.Context(), serverID, user.ID, user.Role, req)
	if err != nil {
		serverError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"server": server})
}

// ─── Delete ───────────────────────────────────────────────────────────────────

// DeleteServer handles DELETE /servers/:id.
func (h *ServerHandler) DeleteServer(c *gin.Context) {
	user := mustUser(c)

	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	// Fetch the server so we know which node it lives on before deleting.
	server, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role)
	if err != nil {
		serverError(c, err)
		return
	}

	// Best-effort: ask the daemon to stop the process AND remove the on-disk
	// data directory before we delete the DB row.  If the daemon is
	// unreachable we still delete the row so the operator can recreate the
	// server, and log a warning so they can rm -rf the leftover directory.
	cleanupWarning := ""
	if client, clientErr := h.daemonClient(server.NodeID.String()); clientErr == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		if derr := client.DeleteServerData(ctx, serverID.String()); derr != nil {
			cleanupWarning = "daemon cleanup failed; " + server.DataPath + " may still exist on disk"
			h.log.Warn("delete: daemon data-cleanup failed (non-fatal)",
				zap.String("server_id", serverID.String()),
				zap.Error(derr),
			)
		}
		cancel()
	} else {
		cleanupWarning = "daemon offline; " + server.DataPath + " will be left on disk"
		h.log.Warn("delete: daemon unreachable, skipping data cleanup",
			zap.String("server_id", serverID.String()),
			zap.String("node_id", server.NodeID.String()),
		)
	}

	if err := h.svc.DeleteServer(c.Request.Context(), serverID, user.ID, user.Role); err != nil {
		serverError(c, err)
		return
	}

	resp := gin.H{"message": "server deleted"}
	if cleanupWarning != "" {
		resp["warning"] = cleanupWarning
	}
	c.JSON(http.StatusOK, resp)
}

// ─── Power Actions ────────────────────────────────────────────────────────────

// StartServer handles POST /servers/:id/start.
func (h *ServerHandler) StartServer(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	server, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role)
	if err != nil {
		serverError(c, err)
		return
	}

	if server.Status == models.StatusRunning || server.Status == models.StatusStarting {
		c.JSON(http.StatusConflict, gin.H{"error": "server is already running or starting"})
		return
	}

	// Update DB status immediately so the UI reflects the transition
	if err := h.svc.UpdateServerStatus(c.Request.Context(), serverID, models.StatusStarting); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update status"})
		return
	}

	// Dispatch to daemon asynchronously — do not block the HTTP response
	go h.dispatchStart(server)

	c.JSON(http.StatusAccepted, gin.H{"message": "start dispatched", "status": models.StatusStarting})
}

// StopServer handles POST /servers/:id/stop.
func (h *ServerHandler) StopServer(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	server, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role)
	if err != nil {
		serverError(c, err)
		return
	}

	_ = h.svc.UpdateServerStatus(c.Request.Context(), serverID, models.StatusStopping)

	go h.dispatchStop(server.NodeID.String(), server.ID.String())

	c.JSON(http.StatusAccepted, gin.H{"message": "stop dispatched", "status": models.StatusStopping})
}

// RestartServer handles POST /servers/:id/restart.
func (h *ServerHandler) RestartServer(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	server, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role)
	if err != nil {
		serverError(c, err)
		return
	}

	_ = h.svc.UpdateServerStatus(c.Request.Context(), serverID, models.StatusStopping)
	go h.dispatchRestart(server)

	c.JSON(http.StatusAccepted, gin.H{"message": "restart dispatched", "status": models.StatusStopping})
}

// KillServer handles POST /servers/:id/kill.
func (h *ServerHandler) KillServer(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	server, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role)
	if err != nil {
		serverError(c, err)
		return
	}

	_ = h.svc.UpdateServerStatus(c.Request.Context(), serverID, models.StatusStopped)
	go h.dispatchKill(server.NodeID.String(), server.ID.String())

	c.JSON(http.StatusAccepted, gin.H{"message": "kill dispatched", "status": models.StatusStopped})
}

// ─── Daemon dispatch helpers ──────────────────────────────────────────────────

func (h *ServerHandler) daemonClient(nodeID string) (*daemon.Client, error) {
	return h.daemons.Get(nodeID)
}

func (h *ServerHandler) dispatchStart(server *models.Server) {
	client, err := h.daemonClient(server.NodeID.String())
	if err != nil {
		h.log.Error("start: no daemon client for node",
			zap.String("node_id", server.NodeID.String()),
			zap.Error(err),
		)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.svc.UpdateServerStatus(ctx, server.ID, models.StatusCrashed)
		return
	}

	// ── Step 1: Ensure server files are provisioned ──────────────────────
	// Always call provision first — the daemon's downloader is idempotent
	// and will skip if files already exist.  This also handles the case
	// where the initial CreateServer provisioning failed.
	h.log.Info("start: provisioning server files before start",
		zap.String("server_id", server.ID.String()),
	)

	_ = h.svc.UpdateServerStatus(context.Background(), server.ID, models.StatusInstalling)

	provCtx, provCancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer provCancel()

	if err := client.ProvisionServer(provCtx, server.ID.String(), server.HytaleVersion, server.DataPath); err != nil {
		h.log.Error("start: provisioning failed",
			zap.String("server_id", server.ID.String()),
			zap.Error(err),
		)
		_ = h.svc.UpdateServerStatus(provCtx, server.ID, models.StatusCrashed)
		return
	}

	// Wait for the daemon to report back that provisioning is done.
	// The daemon sets status to "stopped" (ready) or "crashed" (failed)
	// via POST /servers/:id/daemon/status.  Poll the DB until we see it.
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	timeout := time.After(10 * time.Minute)

	for {
		select {
		case <-timeout:
			h.log.Error("start: provisioning timed out",
				zap.String("server_id", server.ID.String()),
			)
			_ = h.svc.UpdateServerStatus(context.Background(), server.ID, models.StatusCrashed)
			return
		case <-ticker.C:
			status, err := h.svc.GetServerStatus(context.Background(), server.ID)
			if err != nil {
				continue
			}
			if status == models.StatusCrashed {
				h.log.Error("start: provisioning reported crashed",
					zap.String("server_id", server.ID.String()),
				)
				return // already crashed
			}
			if status == models.StatusStopped {
				goto provisioned
			}
			// still installing — keep waiting
		}
	}

provisioned:
	// ── Step 2: Start the server process ─────────────────────────────────
	h.log.Info("start: provisioning complete, starting server",
		zap.String("server_id", server.ID.String()),
	)

	_ = h.svc.UpdateServerStatus(context.Background(), server.ID, models.StatusStarting)

	startCtx, startCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer startCancel()

	ramLimit := uint32(server.RAMLimitMB)

	cfg := daemon.ServerConfig{
		ServerID:   server.ID.String(),
		Name:       server.Name,
		DataPath:   server.DataPath,
		Port:       uint16(server.Port),
		RAMLimitMB: ramLimit,
		CPULimit:   0,
		CrashLimit: uint32(server.CrashLimit),
	}

	if err := client.StartServer(startCtx, cfg); err != nil {
		h.log.Error("start: daemon start call failed",
			zap.String("server_id", server.ID.String()),
			zap.Error(err),
		)
		_ = h.svc.UpdateServerStatus(startCtx, server.ID, models.StatusCrashed)
	}
	// Daemon reports status back via POST /servers/:id/daemon/status — DB updated there
}

func (h *ServerHandler) dispatchStop(nodeID, serverID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()

	client, err := h.daemonClient(nodeID)
	if err != nil {
		h.log.Error("stop: no daemon client", zap.String("node_id", nodeID), zap.Error(err))
		return
	}

	if err := client.StopServer(ctx, serverID); err != nil {
		h.log.Error("stop: daemon call failed", zap.String("server_id", serverID), zap.Error(err))
	}
}

func (h *ServerHandler) dispatchRestart(server *models.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := h.daemonClient(server.NodeID.String())
	if err != nil {
		h.log.Error("restart: no daemon client", zap.String("node_id", server.NodeID.String()), zap.Error(err))
		return
	}

	if err := client.RestartServer(ctx, server.ID.String()); err != nil {
		h.log.Error("restart: daemon call failed", zap.String("server_id", server.ID.String()), zap.Error(err))
		_ = h.svc.UpdateServerStatus(ctx, server.ID, models.StatusCrashed)
	}
}

func (h *ServerHandler) dispatchKill(nodeID, serverID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := h.daemonClient(nodeID)
	if err != nil {
		h.log.Error("kill: no daemon client", zap.String("node_id", nodeID), zap.Error(err))
		return
	}

	if err := client.KillServer(ctx, serverID); err != nil {
		h.log.Error("kill: daemon call failed", zap.String("server_id", serverID), zap.Error(err))
	}
}

func (h *ServerHandler) dispatchProvision(server *models.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute) // download can be slow
	defer cancel()

	client, err := h.daemonClient(server.NodeID.String())
	if err != nil {
		h.log.Error("provision: no daemon client for node",
			zap.String("node_id", server.NodeID.String()),
			zap.Error(err),
		)
		_ = h.svc.UpdateServerStatus(ctx, server.ID, models.StatusCrashed)
		return
	}

	if err := client.ProvisionServer(ctx, server.ID.String(), server.HytaleVersion, server.DataPath); err != nil {
		h.log.Error("provision: daemon call failed",
			zap.String("server_id", server.ID.String()),
			zap.Error(err),
		)
		_ = h.svc.UpdateServerStatus(ctx, server.ID, models.StatusCrashed)
	}
	// Daemon reports final status (stopped = ready) via its api_client after
	// files are downloaded and the process is ready to start.
}


// ─── Console ──────────────────────────────────────────────────────────────────

// SendConsoleCommand handles POST /servers/:id/console.
func (h *ServerHandler) SendConsoleCommand(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	if _, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role); err != nil {
		serverError(c, err)
		return
	}

	var req struct {
		Cmd string `json:"cmd" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.SendConsoleCommand(c.Request.Context(), serverID, req.Cmd); err != nil {
		serverError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "command queued"})
}

// ─── Metrics ──────────────────────────────────────────────────────────────────

// GetMetrics handles GET /servers/:id/metrics.
// Fetches real-time resource metrics directly from the node daemon.
func (h *ServerHandler) GetMetrics(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	server, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role)
	if err != nil {
		serverError(c, err)
		return
	}

	client, err := h.daemonClient(server.NodeID.String())
	if err != nil {
		// Daemon not registered — return zeroed metrics rather than 500
		c.JSON(http.StatusOK, gin.H{
			"server_id": serverID,
			"timestamp": time.Now().UTC(),
			"cpu":       gin.H{"usage_percent": 0},
			"memory":    gin.H{"used_mb": 0, "limit_mb": server.RAMLimitMB},
			"uptime_s":  0,
			"note":      "daemon not connected",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	m, err := client.GetServerMetrics(ctx, serverID.String())
	if err != nil {
		h.log.Warn("failed to fetch metrics from daemon",
			zap.String("server_id", serverID.String()),
			zap.Error(err),
		)
		c.JSON(http.StatusOK, gin.H{
			"server_id": serverID,
			"timestamp": time.Now().UTC(),
			"cpu":       gin.H{"usage_percent": 0},
			"memory":    gin.H{"used_mb": 0, "limit_mb": server.RAMLimitMB},
			"uptime_s":  0,
			"note":      "daemon unreachable",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"server_id": serverID,
		"timestamp": time.Now().UTC(),
		"cpu": gin.H{
			"usage_percent": m.CPUPercent,
		},
		"memory": gin.H{
			"used_mb":  m.RAMMB,
			"limit_mb": server.RAMLimitMB,
		},
		"uptime_s": m.UptimeS,
	})
}

// ─── Daemon Callbacks ─────────────────────────────────────────────────────────

// DaemonStatusUpdate handles POST /servers/:id/daemon/status.
// Called by TaleDaemon nodes to push lifecycle status changes (starting →
// running, stopping → stopped, etc.) back to the control-plane database.
// Protected by DaemonNodeAuth middleware — NOT by user JWT.
func (h *ServerHandler) DaemonStatusUpdate(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	var req struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate the status value against known constants.
	switch req.Status {
	case models.StatusInstalling, models.StatusStopped, models.StatusStarting,
		models.StatusRunning, models.StatusStopping, models.StatusCrashed:
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown status value"})
		return
	}

	if err := h.svc.UpdateServerStatus(c.Request.Context(), serverID, req.Status); err != nil {
		h.log.Error("daemon status update failed",
			zap.String("server_id", serverID.String()),
			zap.String("status", req.Status),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update status"})
		return
	}

	h.log.Info("daemon status update",
		zap.String("server_id", serverID.String()),
		zap.String("status", req.Status),
	)
	c.JSON(http.StatusOK, gin.H{"message": "status updated"})
}

// DaemonLogsIngest handles POST /servers/:id/daemon/logs.
// Called by TaleDaemon nodes to push batches of server process log lines.
// Protected by DaemonNodeAuth middleware — NOT by user JWT.
func (h *ServerHandler) DaemonLogsIngest(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	var lines []services.LogLineInput
	if err := c.ShouldBindJSON(&lines); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(lines) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "no lines"})
		return
	}

	if err := h.svc.IngestLogs(c.Request.Context(), serverID, lines); err != nil {
		h.log.Warn("log ingest failed (non-fatal)",
			zap.String("server_id", serverID.String()),
			zap.Int("count", len(lines)),
			zap.Error(err),
		)
		// Return 200 so the daemon doesn't retry indefinitely.
	}

	c.JSON(http.StatusOK, gin.H{"ingested": len(lines)})
}

// ─── Logs ─────────────────────────────────────────────────────────────────────

// GetLogs handles GET /servers/:id/logs.
// Returns the most recent log lines stored from daemon pushes.
func (h *ServerHandler) GetLogs(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	// Access check — reuse GetServer which enforces RBAC.
	if _, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role); err != nil {
		serverError(c, err)
		return
	}

	logs, err := h.svc.GetLogs(c.Request.Context(), serverID, 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch logs"})
		return
	}
	if logs == nil {
		logs = []*models.ServerLog{}
	}

	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

// ─── File Browser ─────────────────────────────────────────────────────────────

// ListFiles handles GET /servers/:id/files.
func (h *ServerHandler) ListFiles(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	server, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role)
	if err != nil {
		serverError(c, err)
		return
	}

	client, err := h.daemonClient(server.NodeID.String())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "daemon not connected"})
		return
	}

	path := c.DefaultQuery("path", "/")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	result, err := client.ListFiles(ctx, serverID.String(), path)
	if err != nil {
		h.log.Warn("file list failed", zap.String("server_id", serverID.String()), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to list files from daemon"})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetFileContent handles GET /servers/:id/files/content.
func (h *ServerHandler) GetFileContent(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	server, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role)
	if err != nil {
		serverError(c, err)
		return
	}

	client, err := h.daemonClient(server.NodeID.String())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "daemon not connected"})
		return
	}

	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path query parameter is required"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	result, err := client.GetFileContent(ctx, serverID.String(), path)
	if err != nil {
		h.log.Warn("file content read failed", zap.String("server_id", serverID.String()), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read file from daemon"})
		return
	}

	c.JSON(http.StatusOK, result)
}

// WriteFileContent handles PUT /servers/:id/files/content.
func (h *ServerHandler) WriteFileContent(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	server, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role)
	if err != nil {
		serverError(c, err)
		return
	}

	client, err := h.daemonClient(server.NodeID.String())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "daemon not connected"})
		return
	}

	var req struct {
		Path    string `json:"path" binding:"required"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	if err := client.WriteFileContent(ctx, serverID.String(), req.Path, req.Content); err != nil {
		h.log.Warn("file write failed", zap.String("server_id", serverID.String()), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to write file to daemon"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "file written"})
}

// DeleteFile handles DELETE /servers/:id/files.
func (h *ServerHandler) DeleteFile(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	server, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role)
	if err != nil {
		serverError(c, err)
		return
	}

	client, err := h.daemonClient(server.NodeID.String())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "daemon not connected"})
		return
	}

	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path query parameter is required"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	if err := client.DeleteFile(ctx, serverID.String(), path); err != nil {
		h.log.Warn("file delete failed", zap.String("server_id", serverID.String()), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to delete file on daemon"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "file deleted"})
}

// CreateDirectory handles POST /servers/:id/files/directory.
func (h *ServerHandler) CreateDirectory(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	server, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role)
	if err != nil {
		serverError(c, err)
		return
	}

	client, err := h.daemonClient(server.NodeID.String())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "daemon not connected"})
		return
	}

	var req struct {
		Path string `json:"path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	if err := client.CreateDirectory(ctx, serverID.String(), req.Path); err != nil {
		h.log.Warn("directory create failed", zap.String("server_id", serverID.String()), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to create directory on daemon"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "directory created"})
}

// RenameFile handles POST /servers/:id/files/rename.
func (h *ServerHandler) RenameFile(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	server, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role)
	if err != nil {
		serverError(c, err)
		return
	}

	client, err := h.daemonClient(server.NodeID.String())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "daemon not connected"})
		return
	}

	var req struct {
		Path    string `json:"path" binding:"required"`
		NewName string `json:"new_name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	if err := client.RenameFile(ctx, serverID.String(), req.Path, req.NewName); err != nil {
		h.log.Warn("file rename failed", zap.String("server_id", serverID.String()), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to rename file on daemon"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "file renamed"})
}

// ─── File Upload / Download / Extract / Archive ──────────────────────────────

// UploadFile handles POST /servers/:id/files/upload.
func (h *ServerHandler) UploadFile(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	server, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role)
	if err != nil {
		serverError(c, err)
		return
	}

	client, err := h.daemonClient(server.NodeID.String())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "daemon not connected"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file field is required"})
		return
	}
	defer file.Close()

	dir := c.DefaultQuery("path", "/")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	if err := client.UploadFile(ctx, serverID.String(), dir, header.Filename, file); err != nil {
		h.log.Warn("file upload failed", zap.String("server_id", serverID.String()), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to upload file to daemon"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "file uploaded"})
}

// DownloadFile handles GET /servers/:id/files/download.
func (h *ServerHandler) DownloadFile(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	server, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role)
	if err != nil {
		serverError(c, err)
		return
	}

	client, err := h.daemonClient(server.NodeID.String())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "daemon not connected"})
		return
	}

	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path query parameter is required"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	body, fileName, err := client.DownloadFile(ctx, serverID.String(), path)
	if err != nil {
		h.log.Warn("file download failed", zap.String("server_id", serverID.String()), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to download file from daemon"})
		return
	}
	defer body.Close()

	c.Header("Content-Disposition", "attachment; filename=\""+fileName+"\"")
	c.Header("Content-Type", "application/octet-stream")
	c.Status(http.StatusOK)
	io.Copy(c.Writer, body)
}

// ExtractArchive handles POST /servers/:id/files/extract.
func (h *ServerHandler) ExtractArchive(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	server, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role)
	if err != nil {
		serverError(c, err)
		return
	}

	client, err := h.daemonClient(server.NodeID.String())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "daemon not connected"})
		return
	}

	var req struct {
		Path string `json:"path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	if err := client.ExtractArchive(ctx, serverID.String(), req.Path); err != nil {
		h.log.Warn("extract failed", zap.String("server_id", serverID.String()), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to extract archive"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "archive extracted"})
}

// CreateArchive handles POST /servers/:id/files/archive.
func (h *ServerHandler) CreateArchive(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	server, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role)
	if err != nil {
		serverError(c, err)
		return
	}

	client, err := h.daemonClient(server.NodeID.String())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "daemon not connected"})
		return
	}

	var req struct {
		Path string `json:"path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	if err := client.CreateArchive(ctx, serverID.String(), req.Path); err != nil {
		h.log.Warn("archive failed", zap.String("server_id", serverID.String()), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to create archive"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "archive created"})
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func mustUser(c *gin.Context) *models.User {
	user, _ := middleware.GetUserFromCtx(c)
	return user
}

func parseUUID(c *gin.Context, param string) (uuid.UUID, bool) {
	raw := c.Param(param)
	id, err := uuid.Parse(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ID format"})
		return uuid.Nil, false
	}
	return id, true
}

func serverError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrServerNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
	case errors.Is(err, services.ErrServerForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}
