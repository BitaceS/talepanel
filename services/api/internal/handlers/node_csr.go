package handlers

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
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

// nodeLookupStatus maps a node lookup error to an HTTP status. Only a missing
// row is a 404: the daemon reads 404 as "this panel has no CSR endpoint" and
// stops retrying, so transient DB errors must surface as 500.
func nodeLookupStatus(err error) int {
	if errors.Is(err, pgx.ErrNoRows) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

// sanFromFQDN strips the brackets of an IPv6 literal ("[2001:db8::1]") so it
// becomes an IP SAN instead of a bogus DNS SAN.
func sanFromFQDN(fqdn string) string {
	if strings.HasPrefix(fqdn, "[") && strings.HasSuffix(fqdn, "]") {
		return fqdn[1 : len(fqdn)-1]
	}
	return fqdn
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
		if status := nodeLookupStatus(err); status == http.StatusNotFound {
			c.JSON(status, gin.H{"error": "node not found"})
		} else {
			c.JSON(status, gin.H{"error": "could not load node"})
		}
		return
	}

	ca, err := h.settings.MTLSCA(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load CA"})
		return
	}
	certPEM, serial, notAfter, err := ca.SignCSR(csrPEM, nodeID.String(), []string{sanFromFQDN(fqdn)}, nodeCertValidity)
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
