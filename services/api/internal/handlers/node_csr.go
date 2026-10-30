package handlers

import (
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BitaceS/talepanel/api/internal/middleware"
	"github.com/BitaceS/talepanel/api/internal/pki"
	"github.com/BitaceS/talepanel/api/internal/services"
)

// nodeCertValidity is the lifetime of a daemon leaf certificate.
const nodeCertValidity = 2 * 365 * 24 * time.Hour

// NodeCSRHandler signs daemon certificate signing requests with the panel CA.
type NodeCSRHandler struct {
	db       *pgxpool.Pool
	settings *services.AppSettingsService
}

// NewNodeCSRHandler constructs a NodeCSRHandler.
func NewNodeCSRHandler(db *pgxpool.Pool, settings *services.AppSettingsService) *NodeCSRHandler {
	return &NodeCSRHandler{db: db, settings: settings}
}

// decodeCSRField accepts a raw PEM CSR or base64 of one.
func decodeCSRField(s string) []byte {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "-----BEGIN") {
		return []byte(s)
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b
	}
	return []byte(s)
}

// SignCSR handles POST /nodes/:id/csr.
// The daemon sends a PKCS#10 CSR; the certificate's CN is forced to the
// authenticated node id and its SAN to the node's registered fqdn, so a
// daemon cannot obtain a certificate for another identity.
// Protected by DaemonNodeAuth — the token must belong to the node in the path.
func (h *NodeCSRHandler) SignCSR(c *gin.Context) {
	nodeID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	authedID, _ := middleware.GetDaemonNodeID(c)
	if authedID != nodeID.String() {
		c.JSON(http.StatusForbidden, gin.H{"error": "token does not match node"})
		return
	}

	var req struct {
		CSRPEM string `json:"csr_pem"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	csrPEM := decodeCSRField(req.CSRPEM)
	if _, err := pki.ParseCSR(csrPEM); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid CSR"})
		return
	}

	ctx := c.Request.Context()
	var fqdn string
	if err := h.db.QueryRow(ctx, `SELECT fqdn FROM nodes WHERE id = $1`, nodeID).Scan(&fqdn); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}

	ca, err := h.settings.MTLSCA(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load CA"})
		return
	}
	certPEM, serial, notAfter, err := ca.SignCSR(csrPEM, nodeID.String(), []string{fqdn}, nodeCertValidity)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not sign certificate"})
		return
	}
	if _, err := h.db.Exec(ctx,
		`UPDATE nodes SET cert_serial = $2, cert_not_after = $3 WHERE id = $1`,
		nodeID, serial, notAfter); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not record certificate"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ca_cert_pem":   string(ca.CertPEM),
		"node_cert_pem": string(certPEM),
	})
}
