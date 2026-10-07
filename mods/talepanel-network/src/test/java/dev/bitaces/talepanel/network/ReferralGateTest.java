package dev.bitaces.talepanel.network;

import org.junit.jupiter.api.Test;

import java.util.UUID;

import static org.junit.jupiter.api.Assertions.*;

class ReferralGateTest {

    private static final String SECRET_HEX = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff";
    private static final UUID SELF = UUID.fromString("22222222-2222-2222-2222-222222222222");
    private static final UUID PLAYER = UUID.fromString("33333333-3333-3333-3333-333333333333");
    private static final long NOW = 1_760_000_000L;

    private static NetworkConfig gamemode(boolean requireReferral) {
        return NetworkConfig.parse("""
            {"version":1,"role":"gamemode","server_id":"%s","require_referral":%s,"secrets":["%s"]}
            """.formatted(SELF, requireReferral, SECRET_HEX));
    }

    private static byte[] ticketFor(UUID target, UUID player, long exp) {
        return HandoffTicket.sign(NetworkConfig.hexToBytes(SECRET_HEX), target, player, NOW, exp);
    }

    @Test
    void validTicketForThisPlayerIsAdmitted() {
        byte[] t = ticketFor(SELF, PLAYER, NOW + 30);
        assertTrue(ReferralGate.denyReason(gamemode(true), true, t, PLAYER, NOW).isEmpty());
    }

    @Test
    void directJoinIsRefusedWhenReferralIsRequired() {
        assertTrue(ReferralGate.denyReason(gamemode(true), false, null, PLAYER, NOW).isPresent());
    }

    // A ticket is bound to one player; someone else replaying it must not get in.
    @Test
    void ticketMintedForAnotherPlayerIsRefused() {
        byte[] t = ticketFor(SELF, UUID.randomUUID(), NOW + 30);
        assertTrue(ReferralGate.denyReason(gamemode(true), true, t, PLAYER, NOW).isPresent());
    }

    @Test
    void expiredOrForeignTicketIsRefused() {
        assertTrue(ReferralGate.denyReason(gamemode(true), true, ticketFor(SELF, PLAYER, NOW), PLAYER, NOW).isPresent());
        assertTrue(ReferralGate.denyReason(gamemode(true), true, ticketFor(UUID.randomUUID(), PLAYER, NOW + 30), PLAYER, NOW).isPresent());
        assertTrue(ReferralGate.denyReason(gamemode(true), true, new byte[]{1, 2, 3}, PLAYER, NOW).isPresent());
    }

    @Test
    void openGamemodeAdmitsDirectJoins() {
        assertTrue(ReferralGate.denyReason(gamemode(false), false, null, PLAYER, NOW).isEmpty());
    }

    @Test
    void hubAndUnconfiguredServersAdmitEveryone() {
        NetworkConfig hub = NetworkConfig.parse("""
            {"version":1,"role":"hub","server_id":"%s","require_referral":true,"secrets":["%s"]}
            """.formatted(SELF, SECRET_HEX));
        assertTrue(ReferralGate.denyReason(hub, false, null, PLAYER, NOW).isEmpty());
        assertTrue(ReferralGate.denyReason(null, false, null, PLAYER, NOW).isEmpty());
    }
}
