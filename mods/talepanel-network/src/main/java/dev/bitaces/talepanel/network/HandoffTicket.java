package dev.bitaces.talepanel.network;

import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import java.nio.ByteBuffer;
import java.security.GeneralSecurityException;
import java.security.MessageDigest;
import java.util.Arrays;
import java.util.List;
import java.util.Optional;
import java.util.UUID;

/**
 * A hub-to-gamemode handoff ticket. The hub signs it with the installation
 * secret; the gamemode verifies it before accepting a referral connection.
 * Layout (65 bytes): ver(1) | iat(8) | exp(8) | targetServerUuid(16) |
 * playerUuid(16) | HMAC-SHA256(secret, first 49 bytes) truncated to 16.
 */
public final class HandoffTicket {

    private static final byte VERSION = 1;
    private static final int PAYLOAD_LEN = 1 + 8 + 8 + 16 + 16; // 49
    private static final int MAC_LEN = 16;
    static final int TICKET_LEN = PAYLOAD_LEN + MAC_LEN;         // 65

    private HandoffTicket() {}

    public static byte[] sign(byte[] secret, UUID target, UUID player, long iatEpochSec, long expEpochSec) {
        ByteBuffer buf = ByteBuffer.allocate(TICKET_LEN);
        buf.put(VERSION);
        buf.putLong(iatEpochSec);
        buf.putLong(expEpochSec);
        buf.putLong(target.getMostSignificantBits());
        buf.putLong(target.getLeastSignificantBits());
        buf.putLong(player.getMostSignificantBits());
        buf.putLong(player.getLeastSignificantBits());

        byte[] payload = Arrays.copyOfRange(buf.array(), 0, PAYLOAD_LEN);
        byte[] mac = mac(secret, payload);
        buf.put(mac, 0, MAC_LEN);
        return buf.array();
    }

    /**
     * Verifies the MAC against any of the given secrets and returns the player
     * UUID carried in the ticket. Field guards (version, expiry, target) are
     * added in a later task; this checks length and authenticity only.
     */
    public static Optional<UUID> verify(byte[] ticket, List<byte[]> secrets, UUID selfServerId, long nowEpochSec) {
        if (ticket == null || ticket.length != TICKET_LEN) {
            return Optional.empty();
        }
        byte[] payload = Arrays.copyOfRange(ticket, 0, PAYLOAD_LEN);
        byte[] presentedMac = Arrays.copyOfRange(ticket, PAYLOAD_LEN, TICKET_LEN);

        boolean macOk = false;
        for (byte[] secret : secrets) {
            if (MessageDigest.isEqual(mac(secret, payload), presentedMac)) {
                macOk = true;
                break;
            }
        }
        if (!macOk) {
            return Optional.empty();
        }

        ByteBuffer buf = ByteBuffer.wrap(payload);
        byte version = buf.get();
        if (version != VERSION) {
            return Optional.empty();
        }
        buf.getLong();                 // iat — informational only
        long exp = buf.getLong();
        if (nowEpochSec >= exp) {
            return Optional.empty();   // expired (exp is exclusive)
        }
        long tMsb = buf.getLong();
        long tLsb = buf.getLong();
        if (!new UUID(tMsb, tLsb).equals(selfServerId)) {
            return Optional.empty();   // minted for a different server
        }
        long pMsb = buf.getLong();
        long pLsb = buf.getLong();
        return Optional.of(new UUID(pMsb, pLsb));
    }

    private static byte[] mac(byte[] secret, byte[] data) {
        try {
            Mac hmac = Mac.getInstance("HmacSHA256");
            hmac.init(new SecretKeySpec(secret, "HmacSHA256"));
            byte[] full = hmac.doFinal(data);
            return Arrays.copyOfRange(full, 0, MAC_LEN);
        } catch (GeneralSecurityException e) {
            throw new IllegalStateException("HMAC-SHA256 unavailable", e);
        }
    }
}
