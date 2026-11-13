package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func newMiniRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return mr, rdb
}

func TestStreamTicketSingleUse(t *testing.T) {
	mr, rdb := newMiniRedis(t)
	svc := NewStreamTicketService(rdb)
	ctx := context.Background()
	user, server := uuid.New(), uuid.New()

	ticket, err := svc.Issue(ctx, user, server)
	if err != nil {
		t.Fatal(err)
	}
	if len(ticket) < 40 {
		t.Fatalf("ticket %q too short for 32 random bytes", ticket)
	}
	// Stored hashed: the raw ticket is not a Redis key.
	for _, k := range mr.Keys() {
		if strings.Contains(k, ticket) {
			t.Fatalf("raw ticket stored as key %q", k)
		}
	}
	if ttl := mr.TTL(streamTicketKey(ticket)); ttl <= 0 || ttl > StreamTicketTTL {
		t.Fatalf("ticket TTL %v, want (0, %v]", ttl, StreamTicketTTL)
	}

	got, err := svc.Redeem(ctx, ticket, server)
	if err != nil {
		t.Fatalf("first redeem: %v", err)
	}
	if got != user {
		t.Fatalf("redeemed user %v, want %v", got, user)
	}
	if _, err := svc.Redeem(ctx, ticket, server); !errors.Is(err, ErrStreamTicketInvalid) {
		t.Fatalf("second redeem err=%v, want ErrStreamTicketInvalid", err)
	}
}

func TestStreamTicketExpires(t *testing.T) {
	mr, rdb := newMiniRedis(t)
	svc := NewStreamTicketService(rdb)
	ctx := context.Background()
	server := uuid.New()

	ticket, err := svc.Issue(ctx, uuid.New(), server)
	if err != nil {
		t.Fatal(err)
	}
	mr.FastForward(StreamTicketTTL + time.Second)
	if _, err := svc.Redeem(ctx, ticket, server); !errors.Is(err, ErrStreamTicketInvalid) {
		t.Fatalf("expired redeem err=%v, want ErrStreamTicketInvalid", err)
	}
}

func TestStreamTicketWrongServerBurnsTicket(t *testing.T) {
	_, rdb := newMiniRedis(t)
	svc := NewStreamTicketService(rdb)
	ctx := context.Background()
	server, other := uuid.New(), uuid.New()

	ticket, err := svc.Issue(ctx, uuid.New(), server)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Redeem(ctx, ticket, other); !errors.Is(err, ErrStreamTicketInvalid) {
		t.Fatalf("wrong-server redeem err=%v, want ErrStreamTicketInvalid", err)
	}
	// The failed attempt consumed it: no second try against the right server.
	if _, err := svc.Redeem(ctx, ticket, server); !errors.Is(err, ErrStreamTicketInvalid) {
		t.Fatalf("redeem after mismatch err=%v, want ErrStreamTicketInvalid", err)
	}
}

func TestStreamTicketBoundToIssuingUser(t *testing.T) {
	_, rdb := newMiniRedis(t)
	svc := NewStreamTicketService(rdb)
	ctx := context.Background()
	server := uuid.New()
	alice, bob := uuid.New(), uuid.New()

	ta, err := svc.Issue(ctx, alice, server)
	if err != nil {
		t.Fatal(err)
	}
	tb, err := svc.Issue(ctx, bob, server)
	if err != nil {
		t.Fatal(err)
	}
	if ta == tb {
		t.Fatal("two tickets are identical")
	}
	if got, _ := svc.Redeem(ctx, tb, server); got != bob {
		t.Fatalf("bob's ticket redeemed as %v", got)
	}
	if got, _ := svc.Redeem(ctx, ta, server); got != alice {
		t.Fatalf("alice's ticket redeemed as %v", got)
	}
	for _, bad := range []string{"", "not-a-ticket", strings.Repeat("x", 500)} {
		if _, err := svc.Redeem(ctx, bad, server); !errors.Is(err, ErrStreamTicketInvalid) {
			t.Fatalf("Redeem(%q) err=%v, want ErrStreamTicketInvalid", bad, err)
		}
	}
}

func TestServerEventsFanOut(t *testing.T) {
	_, rdb := newMiniRedis(t)
	ev := NewServerEvents(rdb, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ev.Run(ctx)

	server, other := uuid.New(), uuid.New()
	ch, unsub := ev.Subscribe(server)
	defer unsub()
	otherCh, unsubOther := ev.Subscribe(other)
	defer unsubOther()

	// The PSUBSCRIBE may not be active yet; keep notifying until it lands.
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for got := false; !got; {
		select {
		case <-ch:
			got = true
		case <-tick.C:
			ev.Notify(ctx, server)
		case <-deadline:
			t.Fatal("no signal for notified server")
		}
	}
	select {
	case <-otherCh:
		t.Fatal("signal leaked to another server's subscriber")
	default:
	}

	// A nil hub is a no-op.
	var nilHub *ServerEvents
	nilHub.Notify(ctx, server)
}
