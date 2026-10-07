package dev.bitaces.talepanel.network;

import java.util.List;
import java.util.UUID;

/**
 * Decides where the hub sends a player who asks for a gamemode, and mints the
 * handoff ticket the gamemode will check. Pure: no Hytale types, so it is
 * unit-tested without a server.
 */
public final class HubRouter {

    /** How long a ticket stays valid; it only has to survive one reconnect. */
    public static final long TICKET_TTL_SEC = 30;

    public sealed interface Route permits Referral, Unknown, Unavailable {}

    /** Send the player to {@code address} carrying {@code ticket} as referral data. */
    public record Referral(String name, ServerAddress address, byte[] ticket) implements Route {}

    /** No gamemode by that name; {@code available} lists the valid names. */
    public record Unknown(List<String> available) implements Route {}

    /** This server cannot route at all (not a hub, or not configured yet). */
    public record Unavailable(String reason) implements Route {}

    private HubRouter() {}

    public static Route route(NetworkConfig cfg, String gamemode, UUID player, long nowEpochSec) {
        if (cfg == null || !"hub".equals(cfg.role)) {
            return new Unavailable("This server is not a TalePanel network hub.");
        }
        if (cfg.secrets.isEmpty()) {
            return new Unavailable("The network is not configured yet. Ask an admin to sync it from the panel.");
        }

        for (NetworkConfig.Gamemode gm : cfg.gamemodes) {
            if (gm.name != null && gm.name.equalsIgnoreCase(gamemode)) {
                // Sign with the first (current) secret; gamemodes accept any
                // listed secret so a rotation does not break in-flight joins.
                byte[] ticket = HandoffTicket.sign(cfg.secrets.get(0), gm.serverId, player,
                        nowEpochSec, nowEpochSec + TICKET_TTL_SEC);
                return new Referral(gm.name, ServerAddress.parse(gm.address), ticket);
            }
        }
        return new Unknown(cfg.gamemodes.stream().map(g -> g.name).toList());
    }
}
