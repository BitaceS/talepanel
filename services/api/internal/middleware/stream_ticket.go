package middleware

import (
	"errors"
	"net/http"

	"github.com/BitaceS/talepanel/api/internal/services"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StreamTicketAuth authenticates GET /servers/:id/stream with the single-use
// ?ticket= from POST /servers/:id/stream-ticket, in place of AuthRequired
// (EventSource cannot send an Authorization header). The ticket is consumed
// here; it must have been issued for the server in the URL. The user is
// re-loaded so a disabled account is refused, and RequireServerPermission
// must still follow this middleware. A JWT in the query string is never
// accepted.
func StreamTicketAuth(db *pgxpool.Pool, tickets *services.StreamTicketService) gin.HandlerFunc {
	return func(c *gin.Context) {
		ticket := c.Query("ticket")
		if ticket == "" {
			respondError(c, http.StatusUnauthorized, "stream ticket required")
			return
		}
		serverID, err := uuid.Parse(c.Param("id"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid server ID")
			return
		}

		userID, err := tickets.Redeem(c.Request.Context(), ticket, serverID)
		if err != nil {
			if errors.Is(err, services.ErrStreamTicketInvalid) {
				respondError(c, http.StatusUnauthorized, "invalid or expired stream ticket")
				return
			}
			respondError(c, http.StatusInternalServerError, "could not validate stream ticket")
			return
		}

		user, err := loadUserByID(c.Request.Context(), db, userID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				respondError(c, http.StatusUnauthorized, "user not found")
				return
			}
			respondError(c, http.StatusInternalServerError, "could not load user")
			return
		}
		if !user.IsActive {
			respondError(c, http.StatusForbidden, "account is disabled")
			return
		}

		c.Set(ctxKeyUser, user)
		c.Set(ctxKeyUserID, user.ID)
		c.Next()
	}
}
