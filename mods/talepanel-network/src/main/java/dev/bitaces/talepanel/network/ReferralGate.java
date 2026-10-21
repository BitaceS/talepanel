package dev.bitaces.talepanel.network;

import java.util.Optional;
import java.util.UUID;

/**
 * Decides whether a gamemode server admits a connecting player. Only a
 * gamemode with require_referral set is gated; it admits a player only with a
 * valid, unexpired ticket minted for this server and this player.
 */
public final class ReferralGate {

    static final String JOIN_VIA_HUB = "Please join this server through the network hub.";
    static final String BAD_TICKET = "Your hub ticket is invalid or has expired. Please rejoin through the hub.";

    private ReferralGate() {}

    /** Returns the disconnect reason, or empty to admit the player. */
    public static Optional<String> denyReason(NetworkConfig cfg, boolean isReferral, byte[] referralData,
                                              UUID player, long nowEpochSec) {
        if (cfg == null || !"gamemode".equals(cfg.role) || !cfg.requireReferral) {
            return Optional.empty();
        }
        if (!isReferral || referralData == null) {
            return Optional.of(JOIN_VIA_HUB);
        }
        Optional<UUID> ticketPlayer = HandoffTicket.verify(referralData, cfg.secrets, cfg.serverId, nowEpochSec);
        if (ticketPlayer.isEmpty() || !ticketPlayer.get().equals(player)) {
            return Optional.of(BAD_TICKET);
        }
        return Optional.empty();
    }
}
