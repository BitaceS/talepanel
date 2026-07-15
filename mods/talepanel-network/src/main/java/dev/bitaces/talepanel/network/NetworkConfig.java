package dev.bitaces.talepanel.network;

import com.google.gson.Gson;
import com.google.gson.annotations.SerializedName;

import java.util.ArrayList;
import java.util.List;
import java.util.UUID;

/**
 * The parsed mods/talepanel-network/config.json written by the panel (Slice 1a).
 * Secrets arrive hex-encoded and are decoded to raw bytes; ids arrive as UUID
 * strings. A missing "gamemodes" key (a gamemode server) yields an empty list.
 */
public final class NetworkConfig {

    public int version;
    public String role;
    public UUID serverId;
    public boolean requireReferral;
    public List<byte[]> secrets;
    public List<Gamemode> gamemodes;

    public static final class Gamemode {
        public UUID serverId;
        public String name;
        public String address;
    }

    private NetworkConfig() {}

    public static NetworkConfig parse(String json) {
        Dto dto = new Gson().fromJson(json, Dto.class);

        NetworkConfig cfg = new NetworkConfig();
        cfg.version = dto.version;
        cfg.role = dto.role;
        cfg.serverId = requireUuid(dto.serverId, "server_id");
        cfg.requireReferral = dto.requireReferral;

        cfg.secrets = new ArrayList<>();
        if (dto.secrets != null) {
            for (String hex : dto.secrets) {
                cfg.secrets.add(hexToBytes(hex));
            }
        }

        cfg.gamemodes = new ArrayList<>();
        if (dto.gamemodes != null) {
            for (GamemodeDto g : dto.gamemodes) {
                Gamemode gm = new Gamemode();
                gm.serverId = requireUuid(g.serverId, "gamemodes[].server_id");
                gm.name = g.name;
                gm.address = g.address;
                cfg.gamemodes.add(gm);
            }
        }
        return cfg;
    }

    private static UUID requireUuid(String value, String field) {
        if (value == null || value.isBlank()) {
            throw new IllegalArgumentException("config.json: missing or blank required field '" + field + "'");
        }
        return UUID.fromString(value);
    }

    static byte[] hexToBytes(String hex) {
        int len = hex.length();
        if ((len & 1) != 0) {
            throw new IllegalArgumentException("hex string has odd length: " + len);
        }
        byte[] out = new byte[len / 2];
        for (int i = 0; i < len; i += 2) {
            int hi = Character.digit(hex.charAt(i), 16);
            int lo = Character.digit(hex.charAt(i + 1), 16);
            if (hi < 0 || lo < 0) {
                throw new IllegalArgumentException("invalid hex in secret");
            }
            out[i / 2] = (byte) ((hi << 4) | lo);
        }
        return out;
    }

    // Gson-mapped shape of the JSON; converted into the public type above.
    private static final class Dto {
        int version;
        String role;
        @SerializedName("server_id") String serverId;
        @SerializedName("require_referral") boolean requireReferral;
        List<String> secrets;
        List<GamemodeDto> gamemodes;
    }

    private static final class GamemodeDto {
        @SerializedName("server_id") String serverId;
        String name;
        String address;
    }
}
