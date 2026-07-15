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

    @Test
    void tamperedTicketIsRejected() {
        byte[] ticket = HandoffTicket.sign(SECRET, UUID.randomUUID(), UUID.randomUUID(), 1L, 9_999_999_999L);
        ticket[20] ^= 0x01; // flip a byte inside the signed payload
        assertTrue(HandoffTicket.verify(ticket, List.of(SECRET), extractTarget(ticket), 5L).isEmpty(),
                "a modified ticket must fail the MAC check");
    }

    @Test
    void expiredTicketIsRejected() {
        UUID target = UUID.randomUUID();
        byte[] ticket = HandoffTicket.sign(SECRET, target, UUID.randomUUID(), 100L, 130L);
        assertTrue(HandoffTicket.verify(ticket, List.of(SECRET), target, 131L).isEmpty(),
                "a ticket must not verify once now >= exp");
        assertTrue(HandoffTicket.verify(ticket, List.of(SECRET), target, 130L).isEmpty(),
                "exp is exclusive: now == exp is already expired");
        assertTrue(HandoffTicket.verify(ticket, List.of(SECRET), target, 129L).isPresent(),
                "one second before exp must still verify");
    }

    @Test
    void ticketForAnotherServerIsRejected() {
        UUID target = UUID.randomUUID();
        UUID otherServer = UUID.randomUUID();
        byte[] ticket = HandoffTicket.sign(SECRET, target, UUID.randomUUID(), 1L, 9_999_999_999L);
        assertTrue(HandoffTicket.verify(ticket, List.of(SECRET), otherServer, 5L).isEmpty(),
                "a ticket minted for server A must not verify on server B");
    }

    @Test
    void wrongSecretIsRejectedButPreviousSecretIsAccepted() {
        UUID target = UUID.randomUUID();
        byte[] current = "current-secret-current-secret!!!".getBytes();
        byte[] previous = "previous-secret-previous-secret!".getBytes();
        byte[] unrelated = "unrelated-secret-unrelated-secre".getBytes();

        byte[] ticket = HandoffTicket.sign(previous, target, UUID.randomUUID(), 1L, 9_999_999_999L);
        assertTrue(HandoffTicket.verify(ticket, List.of(unrelated), target, 5L).isEmpty(),
                "an unrelated secret must not verify");
        assertTrue(HandoffTicket.verify(ticket, List.of(current, previous), target, 5L).isPresent(),
                "the previous secret in the grace window must still verify (rotation)");
    }

    @Test
    void wrongVersionByteIsRejected() {
        UUID target = UUID.randomUUID();
        byte[] ticket = HandoffTicket.sign(SECRET, target, UUID.randomUUID(), 1L, 9_999_999_999L);
        ticket[0] = 2;  // flip version byte from 1 to non-1 value
        assertTrue(HandoffTicket.verify(ticket, List.of(SECRET), target, 5L).isEmpty(),
                "a ticket with a wrong version byte must be rejected (breaks MAC check)");
    }

    @Test
    void emptySecretAlongsideValidSecretStillVerifies() {
        UUID target = UUID.randomUUID();
        UUID player = UUID.randomUUID();
        byte[] ticket = HandoffTicket.sign(SECRET, target, player, 1L, 9_999_999_999L);

        Optional<UUID> got = HandoffTicket.verify(ticket, List.of(new byte[0], SECRET), target, 5L);

        assertTrue(got.isPresent(), "a valid secret alongside a misconfigured empty one must still verify");
        assertEquals(player, got.get());
    }

    @Test
    void onlyEmptySecretReturnsEmptyWithoutThrowing() {
        UUID target = UUID.randomUUID();
        byte[] ticket = HandoffTicket.sign(SECRET, target, UUID.randomUUID(), 1L, 9_999_999_999L);

        Optional<UUID> got = HandoffTicket.verify(ticket, List.of(new byte[0]), target, 5L);

        assertTrue(got.isEmpty(), "an empty secret can never match; verify must return empty, not throw");
    }

    // Helper: pull the target UUID straight out of a ticket's payload for tests.
    private static UUID extractTarget(byte[] ticket) {
        java.nio.ByteBuffer buf = java.nio.ByteBuffer.wrap(ticket);
        buf.get();        // version
        buf.getLong();    // iat
        buf.getLong();    // exp
        long msb = buf.getLong();
        long lsb = buf.getLong();
        return new UUID(msb, lsb);
    }
}
