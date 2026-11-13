package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// StreamTicketTTL is how long a stream ticket stays redeemable. The panel
// asks for one right before it opens the EventSource, so a few seconds are
// enough; 30 s leaves room for a slow network.
const StreamTicketTTL = 30 * time.Second

// ErrStreamTicketInvalid means the ticket is unknown, expired, already used,
// or was issued for a different server.
var ErrStreamTicketInvalid = errors.New("invalid or expired stream ticket")

// StreamTicketService issues and redeems single-use tickets for the
// server event stream (GET /servers/:id/stream).
//
// EventSource cannot send an Authorization header, and the access token lives
// in memory only, so it must never travel in a URL (proxy and access logs keep
// URLs). A ticket is the stand-in: random, bound to one user and one server,
// valid for StreamTicketTTL and deleted on first use (GETDEL), so a ticket
// that leaks through a log is already worthless.
type StreamTicketService struct {
	rdb *redis.Client
}

// NewStreamTicketService constructs a StreamTicketService.
func NewStreamTicketService(rdb *redis.Client) *StreamTicketService {
	return &StreamTicketService{rdb: rdb}
}

// streamTicketKey stores tickets under their SHA-256 so a Redis dump or
// MONITOR session does not hand out live tickets.
func streamTicketKey(ticket string) string {
	sum := sha256.Sum256([]byte(ticket))
	return "stream_ticket:" + hex.EncodeToString(sum[:])
}

// Issue creates a ticket for userID to stream serverID.
func (s *StreamTicketService) Issue(ctx context.Context, userID, serverID uuid.UUID) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating stream ticket: %w", err)
	}
	ticket := base64.RawURLEncoding.EncodeToString(buf)
	val := userID.String() + "|" + serverID.String()
	if err := s.rdb.Set(ctx, streamTicketKey(ticket), val, StreamTicketTTL).Err(); err != nil {
		return "", fmt.Errorf("storing stream ticket: %w", err)
	}
	return ticket, nil
}

// Redeem consumes ticket and returns the user it was issued to. The ticket is
// deleted even when it names another server, so a mismatched ticket cannot be
// retried against the right one.
func (s *StreamTicketService) Redeem(ctx context.Context, ticket string, serverID uuid.UUID) (uuid.UUID, error) {
	if ticket == "" || len(ticket) > 128 {
		return uuid.Nil, ErrStreamTicketInvalid
	}
	val, err := s.rdb.GetDel(ctx, streamTicketKey(ticket)).Result()
	if errors.Is(err, redis.Nil) {
		return uuid.Nil, ErrStreamTicketInvalid
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("redeeming stream ticket: %w", err)
	}
	userStr, serverStr, ok := strings.Cut(val, "|")
	if !ok || serverStr != serverID.String() {
		return uuid.Nil, ErrStreamTicketInvalid
	}
	userID, err := uuid.Parse(userStr)
	if err != nil {
		return uuid.Nil, ErrStreamTicketInvalid
	}
	return userID, nil
}
