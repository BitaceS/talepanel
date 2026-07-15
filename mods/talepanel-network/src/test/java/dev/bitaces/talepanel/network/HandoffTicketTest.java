package dev.bitaces.talepanel.network;

import org.junit.jupiter.api.Test;

import java.util.List;
import java.util.Optional;
import java.util.UUID;

import static org.junit.jupiter.api.Assertions.*;

class HandoffTicketTest {

    private static final byte[] SECRET = "0123456789abcdef0123456789abcdef".getBytes();

    @Test
    void signThenVerifyReturnsThePlayerUuid() {
        UUID target = UUID.randomUUID();
        UUID player = UUID.randomUUID();
        long now = 1_752_600_000L;

        byte[] ticket = HandoffTicket.sign(SECRET, target, player, now, now + 30);
        Optional<UUID> got = HandoffTicket.verify(ticket, List.of(SECRET), target, now + 5);

        assertTrue(got.isPresent(), "a freshly signed ticket must verify");
        assertEquals(player, got.get(), "verify must return the player the ticket was minted for");
    }

    @Test
    void ticketIsSixtyFiveBytes() {
        byte[] ticket = HandoffTicket.sign(SECRET, UUID.randomUUID(), UUID.randomUUID(), 1L, 2L);
        assertEquals(65, ticket.length, "ticket must stay compact and well under the referral-data cap");
    }
}
