package services

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// serverEventsChannelPrefix is the Redis pub/sub channel prefix; the full
// channel is prefix + server UUID.
const serverEventsChannelPrefix = "talepanel:server-events:"

// ServerEvents tells open event streams that a server has something new
// (log lines ingested, status changed).
//
// Design: the message is only a wake-up signal. The data itself is always
// read from Postgres (server_logs, servers.status), which stays the single
// source of truth and the catch-up source for reconnects (?after= /
// Last-Event-ID). That keeps the stream correct even when a pub/sub message is
// lost (Redis restart, reconnect gap): the stream also re-reads on a slow
// safety tick. Redis carries the signal so it reaches streams on every API
// instance; one process-wide PSUBSCRIBE fans it out to local subscribers, so
// open streams do not each hold a Redis connection.
type ServerEvents struct {
	rdb *redis.Client
	log *zap.Logger

	mu   sync.Mutex
	subs map[uuid.UUID]map[chan struct{}]struct{}
}

// NewServerEvents constructs a ServerEvents hub. Call Run to start receiving.
func NewServerEvents(rdb *redis.Client, log *zap.Logger) *ServerEvents {
	if log == nil {
		log = zap.NewNop()
	}
	return &ServerEvents{rdb: rdb, log: log, subs: make(map[uuid.UUID]map[chan struct{}]struct{})}
}

// Run subscribes to the server event channels until ctx is cancelled.
// go-redis re-establishes the subscription by itself after a dropped
// connection.
func (e *ServerEvents) Run(ctx context.Context) {
	ps := e.rdb.PSubscribe(ctx, serverEventsChannelPrefix+"*")
	defer func() { _ = ps.Close() }()
	ch := ps.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			id, err := uuid.Parse(strings.TrimPrefix(msg.Channel, serverEventsChannelPrefix))
			if err != nil {
				continue
			}
			e.signal(id)
		}
	}
}

// Notify announces new data for serverID. A nil hub is a no-op so services
// work without streaming wired up (tests, CLI).
func (e *ServerEvents) Notify(ctx context.Context, serverID uuid.UUID) {
	if e == nil {
		return
	}
	pubCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := e.rdb.Publish(pubCtx, serverEventsChannelPrefix+serverID.String(), "1").Err(); err != nil {
		// Local streams still learn about it; others catch up on their tick.
		e.log.Debug("server event publish failed", zap.Error(err))
		e.signal(serverID)
	}
}

// Subscribe returns a channel that receives a value whenever serverID has new
// data. Signals coalesce (buffer of one), so a slow reader never blocks the
// hub. Call the returned function to unsubscribe.
func (e *ServerEvents) Subscribe(serverID uuid.UUID) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	e.mu.Lock()
	set, ok := e.subs[serverID]
	if !ok {
		set = make(map[chan struct{}]struct{})
		e.subs[serverID] = set
	}
	set[ch] = struct{}{}
	e.mu.Unlock()
	return ch, func() {
		e.mu.Lock()
		if cur, ok := e.subs[serverID]; ok {
			delete(cur, ch)
			if len(cur) == 0 {
				delete(e.subs, serverID)
			}
		}
		e.mu.Unlock()
	}
}

func (e *ServerEvents) signal(serverID uuid.UUID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for ch := range e.subs[serverID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
