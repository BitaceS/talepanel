package router

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/tyraxo/talepanel/api/internal/config"
	"github.com/tyraxo/talepanel/api/internal/daemon"
	"github.com/tyraxo/talepanel/api/internal/handlers"
	"github.com/tyraxo/talepanel/api/internal/middleware"
	"github.com/tyraxo/talepanel/api/internal/models"
	"github.com/tyraxo/talepanel/api/internal/services"
	"go.uber.org/zap"
)

// SetupRouter wires the full middleware stack and all route groups onto a new
// Gin engine and returns it.
func SetupRouter(
	cfg *config.Config,
	db *pgxpool.Pool,
	rdb *redis.Client,
	log *zap.Logger,
) *gin.Engine {
	if cfg.IsDevelopment() {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()

	// ── Global middleware ──────────────────────────────────────────────────────
	r.Use(gin.Recovery())
	r.Use(middleware.RequestID())
	r.Use(middleware.GinLogger(log))
	r.Use(middleware.SecurityHeaders(!cfg.IsDevelopment()))
	r.Use(corsMiddleware(cfg))

	// ── Daemon client pool ─────────────────────────────────────────────────────
	// Starts empty; nodes populate it via POST /nodes/:id/heartbeat.
	daemonPool := daemon.NewClientPool(log)

	// ── Services ──────────────────────────────────────────────────────────────
	authSvc := services.NewAuthService(db, rdb, cfg)
	serverSvc := services.NewServerService(db)
	nodeSvc := services.NewNodeService(db)
	modSvc := services.NewModService(db)
	cfSvc := services.NewCurseForgeService(cfg.CurseForgeAPIKey, cfg.CurseForgeGameID)

	worldSvc := services.NewWorldService(db)
	playerSvc := services.NewPlayerService(db)
	backupSvc := services.NewBackupService(db)
	alertSvc := services.NewAlertService(db)

	// New services
	permSvc := services.NewPermissionService(db)
	profileSvc := services.NewProfileService(db)
	invSvc := services.NewInvitationService(db)
	dbSvc := services.NewDatabaseService(db, cfg.MariaDBDSN, cfg.MariaDBHost, cfg.MariaDBPort)

	// ── Handlers ──────────────────────────────────────────────────────────────
	secureCookie := !cfg.IsDevelopment()
	authH := handlers.NewAuthHandler(authSvc, cfg.JWTSecret, secureCookie)
	serverH := handlers.NewServerHandler(serverSvc, nodeSvc, daemonPool, cfg.DaemonServersDir, log)
	nodeH := handlers.NewNodeHandler(nodeSvc, daemonPool, log, cfg.IsDevelopment())
	enrollmentSvc := services.NewEnrollmentService(db)
	enrollmentH := handlers.NewEnrollmentHandler(enrollmentSvc, log)
	healthH := handlers.NewHealthHandler(db, rdb)
	modH := handlers.NewModHandler(modSvc, cfSvc)
	gameCmdSvc := services.NewGameCommandService(db)
	gameCmdH := handlers.NewGameCommandHandler(gameCmdSvc, serverSvc, log)
	worldH := handlers.NewWorldHandler(worldSvc, serverSvc, log)
	playerH := handlers.NewPlayerHandler(playerSvc, log)
	backupH := handlers.NewBackupHandler(backupSvc, log)
	alertH := handlers.NewAlertHandler(alertSvc, log)

	// New handlers
	profileH := handlers.NewProfileHandler(profileSvc, log)
	invH := handlers.NewInvitationHandler(invSvc, log)
	dbH := handlers.NewDatabaseHandler(dbSvc, log)
	pluginH := handlers.NewPluginHandler(modSvc, log)

	// ── Rate limiters ─────────────────────────────────────────────────────────
	devMode := cfg.IsDevelopment()
	generalLimiter := middleware.RateLimit(rdb, 120, devMode)
	authLimiter := middleware.RateLimit(rdb, 30, devMode)

	// ── Auth middleware ────────────────────────────────────────────────────────
	authRequired := middleware.AuthRequired(db, rdb, cfg.JWTSecret)

	// ── API v1 base group with audit logging ───────────────────────────────────
	v1 := r.Group("/api/v1")
	v1.Use(middleware.AuditLog(db, log))

	// Health — unauthenticated, no rate limit
	healthGroup := v1.Group("/health")
	{
		healthGroup.GET("", healthH.Liveness)
		healthGroup.GET("/ready", healthH.Readiness)
	}

	// Auth — light rate limiting, no auth required (register/login)
	authGroup := v1.Group("/auth")
	authGroup.Use(authLimiter)
	{
		authGroup.POST("/register", authH.Register)
		authGroup.POST("/login", authH.Login)
		authGroup.POST("/refresh", authH.Refresh)
		authGroup.POST("/totp/verify", authH.VerifyTOTP)

		// These require a valid access token.
		authProtected := authGroup.Group("")
		authProtected.Use(authRequired)
		{
			authProtected.POST("/logout", authH.Logout)
			authProtected.GET("/me", authH.Me)
			authProtected.PATCH("/password", authH.ChangePassword)

			// 2FA (TOTP) self-service
			authProtected.POST("/totp/setup",   authH.SetupTOTP)
			authProtected.POST("/totp/confirm", authH.ConfirmTOTP)
			authProtected.POST("/totp/disable", authH.DisableTOTP)

			// Profile
			authProtected.GET("/profile", profileH.GetProfile)
			authProtected.PATCH("/profile", profileH.UpdateProfile)
			authProtected.GET("/profile/notifications", profileH.GetNotificationPrefs)
			authProtected.PUT("/profile/notifications", profileH.SetNotificationPrefs)

			// Activity & Sessions (Phase 4)
			authProtected.GET("/activity", authH.GetActivity)
			authProtected.GET("/sessions", authH.GetSessions)
			authProtected.DELETE("/sessions/:id", authH.RevokeSession)
		}
	}

	// Servers — requires user authentication, general rate limit
	serverGroup := v1.Group("/servers")
	serverGroup.Use(authRequired, generalLimiter)
	{
		serverGroup.GET("", serverH.ListServers)
		serverGroup.POST("", serverH.CreateServer)
		serverGroup.GET("/:id", serverH.GetServer)
		serverGroup.PATCH("/:id", serverH.UpdateServer)
		serverGroup.DELETE("/:id", serverH.DeleteServer)
		serverGroup.POST("/:id/start", serverH.StartServer)
		serverGroup.POST("/:id/stop", serverH.StopServer)
		serverGroup.POST("/:id/restart", serverH.RestartServer)
		serverGroup.POST("/:id/kill", serverH.KillServer)
		serverGroup.GET("/:id/metrics", serverH.GetMetrics)
		serverGroup.GET("/:id/logs", serverH.GetLogs)
		serverGroup.POST("/:id/console", serverH.SendConsoleCommand)

		// File browser
		serverGroup.GET("/:id/files", serverH.ListFiles)
		serverGroup.GET("/:id/files/content", serverH.GetFileContent)
		serverGroup.PUT("/:id/files/content", serverH.WriteFileContent)
		serverGroup.DELETE("/:id/files", serverH.DeleteFile)
		serverGroup.POST("/:id/files/directory", serverH.CreateDirectory)
		serverGroup.POST("/:id/files/rename", serverH.RenameFile)
		serverGroup.POST("/:id/files/upload", serverH.UploadFile)
		serverGroup.GET("/:id/files/download", serverH.DownloadFile)
		serverGroup.POST("/:id/files/extract", serverH.ExtractArchive)
		serverGroup.POST("/:id/files/archive", serverH.CreateArchive)

		serverGroup.GET("/:id/mods", modH.ListMods)
		serverGroup.POST("/:id/mods", modH.InstallMod)
		serverGroup.DELETE("/:id/mods/:filename", modH.RemoveMod)

		// Game Control — predefined command templates
		serverGroup.GET("/:id/game-commands", gameCmdH.ListGameCommands)
		serverGroup.POST("/:id/game-commands", gameCmdH.CreateGameCommand)
		serverGroup.POST("/:id/game-commands/execute", gameCmdH.ExecuteGameCommand)
		serverGroup.DELETE("/:id/game-commands/:cmdId", gameCmdH.DeleteGameCommand)

		// Worlds
		serverGroup.GET("/:id/worlds", worldH.ListWorlds)
		serverGroup.POST("/:id/worlds", worldH.CreateWorld)
		serverGroup.POST("/:id/worlds/:worldId/activate", worldH.SetActiveWorld)
		serverGroup.DELETE("/:id/worlds/:worldId", worldH.DeleteWorld)

		// Players
		serverGroup.GET("/:id/players", playerH.ListPlayers)
		serverGroup.POST("/:id/players/:playerId/ban", playerH.BanPlayer)
		serverGroup.POST("/:id/players/:playerId/unban", playerH.UnbanPlayer)
		serverGroup.PATCH("/:id/players/:playerId/whitelist", playerH.SetWhitelist)

		// Backup schedules (per-server)
		serverGroup.GET("/:id/backup-schedules", backupH.ListSchedules)

		// Invitations (Phase 3)
		serverGroup.POST("/:id/invitations", invH.CreateInvitation)
		serverGroup.GET("/:id/invitations", invH.ListInvitations)
		serverGroup.DELETE("/:id/invitations/:invId", invH.RevokeInvitation)

		// Database (Phase 7)
		serverGroup.GET("/:id/database", dbH.GetDatabase)
		serverGroup.POST("/:id/database", dbH.CreateDatabase)
		serverGroup.DELETE("/:id/database", dbH.DeleteDatabase)
		serverGroup.POST("/:id/database/reset-password", dbH.ResetPassword)
	}

	// Invitations — accept/decline by token, list my invitations
	invGroup := v1.Group("/invitations")
	invGroup.Use(authRequired, generalLimiter)
	{
		invGroup.GET("/mine", invH.ListMyInvitations)
		invGroup.POST("/:token/accept", invH.AcceptInvitation)
		invGroup.POST("/:token/decline", invH.DeclineInvitation)
	}

	// CurseForge proxy — auth required, general rate limit
	cfGroup := v1.Group("/curseforge")
	cfGroup.Use(authRequired, generalLimiter)
	{
		cfGroup.GET("/search", modH.SearchMods)
		cfGroup.GET("/mods/:mod_id/files", modH.GetModFiles)
	}

	// Backups — requires user authentication, general rate limit
	backupGroup := v1.Group("/backups")
	backupGroup.Use(authRequired, generalLimiter)
	{
		backupGroup.GET("", backupH.ListBackups)
		backupGroup.POST("", backupH.CreateBackup)
		backupGroup.DELETE("/:backupId", backupH.DeleteBackup)
		backupGroup.POST("/:backupId/restore", backupH.RestoreBackup)
	}

	// Backup schedules — requires user authentication, general rate limit
	scheduleGroup := v1.Group("/backup-schedules")
	scheduleGroup.Use(authRequired, generalLimiter)
	{
		scheduleGroup.POST("", backupH.CreateSchedule)
		scheduleGroup.PATCH("/:scheduleId", backupH.ToggleSchedule)
		scheduleGroup.DELETE("/:scheduleId", backupH.DeleteSchedule)
	}

	// Alerts — requires user authentication, general rate limit
	alertGroup := v1.Group("/alerts")
	alertGroup.Use(authRequired, generalLimiter)
	{
		alertGroup.GET("/rules", alertH.ListRules)
		alertGroup.POST("/rules", alertH.CreateRule)
		alertGroup.PATCH("/rules/:ruleId", alertH.ToggleRule)
		alertGroup.DELETE("/rules/:ruleId", alertH.DeleteRule)
		alertGroup.GET("/events", alertH.ListEvents)
		alertGroup.POST("/events/:eventId/resolve", alertH.ResolveEvent)
	}

	// Admin — requires admin role, general rate limit
	adminH := handlers.NewAdminHandler(authSvc, nodeSvc, permSvc, log)
	adminGroup := v1.Group("/admin")
	adminGroup.Use(authRequired, middleware.RequireRole(models.RoleAdmin), generalLimiter)
	{
		adminGroup.POST("/users", adminH.CreateUser)
		adminGroup.GET("/users", adminH.ListUsers)
		adminGroup.PATCH("/users/:id/role", adminH.UpdateUserRole)
		adminGroup.PATCH("/users/:id/active", adminH.ToggleUserActive)
		adminGroup.DELETE("/users/:id", adminH.DeleteUser)
		adminGroup.PATCH("/nodes/:id/status", adminH.UpdateNodeStatus)
		adminGroup.GET("/activity-logs", adminH.GetActivityLogs)

		// Admin permission management
		adminGroup.GET("/users/:id/permissions", adminH.GetUserPermissions)
		adminGroup.PUT("/users/:id/permissions", adminH.SetUserPermissions)

		// Enrollment: admin creates a short-lived one-shot token that the
		// daemon redeems via POST /nodes/enroll.
		adminGroup.POST("/nodes/enroll", enrollmentH.CreateEnrollment)
	}

	// Public enrollment redemption — the token itself is the authentication.
	// Kept under a named subgroup with its own rate limiter so it cannot
	// be targeted independently of the rest of /nodes.
	enrollGroup := v1.Group("/nodes")
	enrollGroup.Use(authLimiter)
	{
		enrollGroup.POST("/enroll", enrollmentH.Redeem)
	}

	// Daemon callbacks — authenticated by node bearer token, NOT user JWT.
	// These routes are called by TaleDaemon nodes to report status and push logs.
	daemonGroup := v1.Group("/servers")
	daemonGroup.Use(middleware.DaemonNodeAuth(db))
	{
		daemonGroup.POST("/:id/daemon/status", serverH.DaemonStatusUpdate)
		daemonGroup.POST("/:id/daemon/logs", serverH.DaemonLogsIngest)
		daemonGroup.POST("/:id/daemon/plugins", pluginH.DaemonPluginReport)
	}

	// Nodes — requires admin role, general rate limit
	nodeGroup := v1.Group("/nodes")
	nodeGroup.Use(authRequired, middleware.RequireRole(models.RoleAdmin), generalLimiter)
	{
		nodeGroup.GET("", nodeH.ListNodes)
		nodeGroup.POST("", nodeH.RegisterNode)
		nodeGroup.GET("/:id", nodeH.GetNode)
		nodeGroup.GET("/:id/network-stats", nodeH.GetNetworkStats)

		// Delete requires owner role.
		nodeGroup.DELETE("/:id",
			middleware.RequireRole(models.RoleOwner),
			nodeH.DeleteNode,
		)
	}

	// Node daemon routes — authenticated by node bearer token.
	// Separated from the admin node group so daemons don't need user JWTs.
	daemonNodeGroup := v1.Group("/nodes")
	daemonNodeGroup.Use(middleware.DaemonNodeAuth(db))
	{
		// Called by daemon on startup to push real hardware specs.
		daemonNodeGroup.POST("/:id/register", nodeH.DaemonSelfRegister)
		// Called by daemon on every heartbeat tick.
		daemonNodeGroup.POST("/:id/heartbeat", nodeH.NodeHeartbeat)
		// Command queue — daemon polls and acks.
		daemonNodeGroup.GET("/:id/commands/pending", nodeH.GetPendingCommands)
		daemonNodeGroup.POST("/:id/commands/:cmd_id/ack", nodeH.AckCommand)
	}

	return r
}

// corsMiddleware builds a CORS handler from the application configuration.
// If no allowed origins are configured the server defaults to no CORS headers
// (i.e. same-origin only).
func corsMiddleware(cfg *config.Config) gin.HandlerFunc {
	if len(cfg.CORSOrigins) == 0 {
		// No origins configured — install a no-op that just passes through.
		return func(c *gin.Context) { c.Next() }
	}

	corsCfg := cors.DefaultConfig()
	corsCfg.AllowOrigins = cfg.CORSOrigins
	corsCfg.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	corsCfg.AllowHeaders = []string{
		"Origin", "Content-Type", "Authorization",
		"X-Request-ID", "X-Real-IP",
	}
	corsCfg.ExposeHeaders = []string{"X-Request-ID"}
	corsCfg.AllowCredentials = true // required for httpOnly cookie flow

	return cors.New(corsCfg)
}
