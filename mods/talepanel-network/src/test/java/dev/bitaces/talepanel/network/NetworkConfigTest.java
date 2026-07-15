package dev.bitaces.talepanel.network;

import org.junit.jupiter.api.Test;

import java.util.UUID;

import static org.junit.jupiter.api.Assertions.*;

class NetworkConfigTest {

    @Test
    void parsesHubConfigWithGamemodesAndHexSecret() {
        String json = """
            {
              "version": 1,
              "role": "hub",
              "server_id": "11111111-1111-1111-1111-111111111111",
              "require_referral": false,
              "secrets": ["00ff10"],
              "gamemodes": [
                {"server_id": "22222222-2222-2222-2222-222222222222", "name": "BedWars", "address": "play.example.com:5521"}
              ]
            }
            """;

        NetworkConfig cfg = NetworkConfig.parse(json);

        assertEquals(1, cfg.version);
        assertEquals("hub", cfg.role);
        assertEquals(UUID.fromString("11111111-1111-1111-1111-111111111111"), cfg.serverId);
        assertFalse(cfg.requireReferral);
        assertEquals(1, cfg.secrets.size());
        assertArrayEquals(new byte[]{0x00, (byte) 0xff, 0x10}, cfg.secrets.get(0));
        assertEquals(1, cfg.gamemodes.size());
        assertEquals("BedWars", cfg.gamemodes.get(0).name);
        assertEquals("play.example.com:5521", cfg.gamemodes.get(0).address);
        assertEquals(UUID.fromString("22222222-2222-2222-2222-222222222222"), cfg.gamemodes.get(0).serverId);
    }

    @Test
    void parsesGamemodeConfigWithNoGamemodesKey() {
        String json = """
            {
              "version": 1,
              "role": "gamemode",
              "server_id": "33333333-3333-3333-3333-333333333333",
              "require_referral": true,
              "secrets": ["abcd"]
            }
            """;

        NetworkConfig cfg = NetworkConfig.parse(json);

        assertEquals("gamemode", cfg.role);
        assertTrue(cfg.requireReferral);
        assertNotNull(cfg.gamemodes, "a missing gamemodes key must yield an empty list, not null");
        assertTrue(cfg.gamemodes.isEmpty());
    }

    @Test
    void missingServerIdThrowsClearError() {
        String json = """
            {
              "version": 1,
              "role": "hub",
              "require_referral": false,
              "secrets": ["00ff10"]
            }
            """;

        IllegalArgumentException ex = assertThrows(IllegalArgumentException.class,
                () -> NetworkConfig.parse(json));
        assertTrue(ex.getMessage().contains("server_id"),
                "error message must mention the missing field 'server_id': " + ex.getMessage());
    }
}
