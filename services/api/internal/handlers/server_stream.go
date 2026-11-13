package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/BitaceS/talepanel/api/internal/models"
	"github.com/BitaceS/talepanel/api/internal/services"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Live server event stream (Server-Sent Events).
//
// GET /servers/:id/stream replaces the panel's per-tab polling of logs,
// status and metrics with one long-lived response per open console:
//
//	event: log      one per new log line; `id:` is the line's server_logs.id
//	event: status   the server's status, on connect and whenever it changes
//	event: metrics  live CPU/RAM/uptime from the daemon, every 5 s
//	: ping          comment every 15 s so proxies keep the connection open
//
// Where events come from: log ingest and status updates call
// ServerEvents.Notify, which publishes a wake-up over Redis pub/sub; the
// stream then reads what is new from Postgres (server_logs where id > cursor,
// servers.status). Postgres stays the only source of data, so a reconnect
// with Last-Event-ID / ?after=<id> resumes exactly after the last line the
// client saw: no line lost, none repeated. A slow safety tick re-reads even
// without a signal, covering a lost pub/sub message. Compared with HTTP
// polling this is one cheap indexed query per signal per open stream instead
// of a full request (auth, permission check, 200-line query) per second per
// tab.
//
// Auth: EventSource cannot set an Authorization header and the access token
// must never be put in a URL, so the stream authenticates with a single-use
// ticket from POST /servers/:id/stream-ticket (see StreamTicketService). The
// usual per-server permission check still runs for the ticket's user.
//
// Lifetime: the http.Server's WriteTimeout would cut a long stream, so the
// stream extends its own write deadline and ends itself after
// streamMaxLifetime; the panel reconnects with a new ticket and ?after=.
const (
	streamMaxLifetime      = 30 * time.Minute
	streamPingInterval     = 15 * time.Second
	streamMetricsEvery     = 5 * time.Second
	streamSafetyPoll       = 2 * time.Second
	streamNoHubPoll        = 500 * time.Millisecond
	streamBacklogLines     = 200
	streamCatchUpBatch     = 500
	streamCatchUpMax       = 5000
	maxStreamsPerUser      = 10
	streamWriteDeadlinePad = 30 * time.Second
)

// streamState is the stream-related part of ServerHandler.
type streamState struct {
	shutdown context.Context // cancelled on process shutdown; nil = never
	tickets  *services.StreamTicketService
	events   *services.ServerEvents
	poll     time.Duration // safety re-read interval; 0 = default

	mu    sync.Mutex
	conns map[uuid.UUID]int // open streams per user
}

// SetStreaming wires the live event stream. shutdown should be cancelled when
// the process stops so open streams end and the HTTP server can drain.
func (h *ServerHandler) SetStreaming(shutdown context.Context, tickets *services.StreamTicketService, events *services.ServerEvents) {
	h.stream.shutdown = shutdown
	h.stream.tickets = tickets
	h.stream.events = events
}

func (s *streamState) acquire(userID uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conns == nil {
		s.conns = make(map[uuid.UUID]int)
	}
	if s.conns[userID] >= maxStreamsPerUser {
		return false
	}
	s.conns[userID]++
	return true
}

func (s *streamState) release(userID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conns[userID] <= 1 {
		delete(s.conns, userID)
		return
	}
	s.conns[userID]--
}

// IssueStreamTicket handles POST /servers/:id/stream-ticket.
func (h *ServerHandler) IssueStreamTicket(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	if h.stream.tickets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "live stream not available"})
		return
	}
	if _, err := h.svc.GetServer(c.Request.Context(), serverID, user.ID, user.Role); err != nil {
		serverError(c, err)
		return
	}
	ticket, err := h.stream.tickets.Issue(c.Request.Context(), user.ID, serverID)
	if err != nil {
		h.log.Error("issuing stream ticket", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not issue stream ticket"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"ticket":     ticket,
		"expires_in": int(services.StreamTicketTTL.Seconds()),
	})
}

// streamCursor reads the resume point: Last-Event-ID (sent by EventSource on
// its own reconnects) wins over ?after= (sent by the panel when it opens a
// fresh EventSource with a new ticket).
func streamCursor(c *gin.Context) (int64, bool, error) {
	raw := c.GetHeader("Last-Event-ID")
	if raw == "" {
		raw = c.Query("after")
	}
	if raw == "" {
		return 0, false, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0, false, errors.New("invalid cursor")
	}
	return n, true, nil
}

// sseWriter writes events and flushes after each one.
type sseWriter struct {
	w  gin.ResponseWriter
	rc *http.ResponseController
}

func (s *sseWriter) extendDeadline(d time.Duration) {
	// Not every ResponseWriter supports deadlines (httptest); ignore that.
	_ = s.rc.SetWriteDeadline(time.Now().Add(d))
}

func (s *sseWriter) event(name, id string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	msg := ""
	if id != "" {
		msg += "id: " + id + "\n"
	}
	msg += "event: " + name + "\ndata: " + string(data) + "\n\n"
	if _, err := io.WriteString(s.w, msg); err != nil {
		return err
	}
	s.w.Flush()
	return nil
}

func (s *sseWriter) comment(text string) error {
	if _, err := io.WriteString(s.w, ": "+text+"\n\n"); err != nil {
		return err
	}
	s.w.Flush()
	return nil
}

// StreamEvents handles GET /servers/:id/stream (see the comment at the top
// of this file).
func (h *ServerHandler) StreamEvents(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	reqCtx := c.Request.Context()

	server, err := h.svc.GetServer(reqCtx, serverID, user.ID, user.Role)
	if err != nil {
		serverError(c, err)
		return
	}
	cursor, hasCursor, err := streamCursor(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "after / Last-Event-ID must be a log id"})
		return
	}
	if !h.stream.acquire(user.ID) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many open live streams"})
		return
	}
	defer h.stream.release(user.ID)

	ctx, cancel := context.WithTimeout(reqCtx, streamMaxLifetime)
	defer cancel()
	if h.stream.shutdown != nil {
		stop := context.AfterFunc(h.stream.shutdown, cancel)
		defer stop()
	}

	hdr := c.Writer.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Connection", "keep-alive")
	hdr.Set("X-Accel-Buffering", "no") // nginx and friends: do not buffer
	c.Status(http.StatusOK)

	w := &sseWriter{w: c.Writer, rc: http.NewResponseController(c.Writer)}
	w.extendDeadline(streamPingInterval + streamWriteDeadlinePad)
	c.Writer.WriteHeaderNow()
	c.Writer.Flush()

	// Logs: backlog (fresh connect) or catch-up (resume), then whatever is new.
	sendLogs := func(lines []*models.ServerLog) error {
		for _, l := range lines {
			if err := w.event("log", strconv.FormatInt(l.ID, 10), l); err != nil {
				return err
			}
			// The backlog is ordered by logged_at, which need not follow id
			// order; the cursor is the highest id sent.
			if l.ID > cursor {
				cursor = l.ID
			}
		}
		return nil
	}
	pumpLogs := func() error {
		for sent := 0; sent < streamCatchUpMax; {
			lines, err := h.svc.GetLogsAfter(ctx, serverID, cursor, streamCatchUpBatch)
			if err != nil {
				return err
			}
			if err := sendLogs(lines); err != nil {
				return err
			}
			sent += len(lines)
			if len(lines) < streamCatchUpBatch {
				return nil
			}
		}
		return nil // the rest follows on the next tick
	}

	if hasCursor {
		err = pumpLogs()
	} else {
		var lines []*models.ServerLog
		lines, err = h.svc.GetLogs(ctx, serverID, streamBacklogLines)
		if err == nil {
			err = sendLogs(lines)
		}
	}
	if err != nil {
		h.logStreamEnd(serverID, err)
		return
	}

	lastStatus := server.Status
	if err := w.event("status", "", gin.H{"status": lastStatus}); err != nil {
		return
	}

	// Metrics come from the daemon (up to 5 s per call); fetch them off the
	// loop so a slow node never delays log lines.
	metricsCh := make(chan gin.H, 1)
	go func() {
		t := time.NewTicker(streamMetricsEvery)
		defer t.Stop()
		for {
			m := h.metricsPayload(ctx, server)
			select {
			case metricsCh <- m:
			case <-ctx.Done():
				return
			}
			select {
			case <-t.C:
			case <-ctx.Done():
				return
			}
		}
	}()

	var wake <-chan struct{}
	poll := h.stream.poll
	if h.stream.events != nil {
		ch, unsubscribe := h.stream.events.Subscribe(serverID)
		defer unsubscribe()
		wake = ch
		if poll == 0 {
			poll = streamSafetyPoll
		}
	} else if poll == 0 {
		poll = streamNoHubPoll
	}
	pollT := time.NewTicker(poll)
	defer pollT.Stop()
	pingT := time.NewTicker(streamPingInterval)
	defer pingT.Stop()

	refresh := func() error {
		if err := pumpLogs(); err != nil {
			return err
		}
		status, err := h.svc.GetServerStatus(ctx, serverID)
		if err != nil {
			return err
		}
		if status != lastStatus {
			lastStatus = status
			return w.event("status", "", gin.H{"status": status})
		}
		return nil
	}

	for {
		var err error
		select {
		case <-ctx.Done():
			// Client gone, lifetime reached or shutdown: end the response.
			// The panel reconnects with ?after=<last id>.
			return
		case <-wake:
			err = refresh()
		case <-pollT.C:
			err = refresh()
		case m := <-metricsCh:
			err = w.event("metrics", "", m)
		case <-pingT.C:
			err = w.comment("ping")
		}
		if err != nil {
			h.logStreamEnd(serverID, err)
			return
		}
		w.extendDeadline(streamPingInterval + streamWriteDeadlinePad)
	}
}

func (h *ServerHandler) logStreamEnd(serverID uuid.UUID, err error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	h.log.Debug("server stream ended", zap.String("server_id", serverID.String()), zap.Error(err))
}
