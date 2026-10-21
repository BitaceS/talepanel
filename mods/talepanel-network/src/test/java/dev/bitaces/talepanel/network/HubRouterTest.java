package dev.bitaces.talepanel.network;

import org.junit.jupiter.api.Test;

import java.util.List;
import java.util.Optional;
import java.util.UUID;

import static org.junit.jupiter.api.Assertions.*;

class HubRouterTest {

    private static final String SECRET_HEX = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff";
    private static final UUID HUB = UUID.fromString("11111111-1111-1111-1111-111111111111");
    private static final UUID SKYWARS = UUID.fromString("22222222-2222-2222-2222-222222222222");
    private static final UUID PLAYER = UUID.fromString("33333333-3333-3333-3333-333333333333");
    private static final long NOW = 1_760_000_000L;

    private static NetworkConfig hub() {
        return NetworkConfig.parse("""
            {"version":1,"role":"hub","server_id":"%s","require_referral":false,
             "secrets":["%s"],
             "gamemodes":[{"server_id":"%s","name":"SkyWars","address":"sky.example.com:5521"}]}
            """.formatted(HUB, SECRET_HEX, SKYWARS));
    }

    @Test
    void routesToTheNamedGamemodeWithATicketItWillAccept() {
        NetworkConfig cfg = hub();
        HubRouter.Route route = HubRouter.route(cfg, "SkyWars", PLAYER, NOW);

        HubRouter.Referral ref = assertInstanceOf(HubRouter.Referral.class, route);
        assertEquals("sky.example.com", ref.address().host());
        assertEquals(5521, ref.address().port());

        Optional<UUID> who = HandoffTicket.verify(ref.ticket(), cfg.secrets, SKYWARS, NOW + 1);
        assertEquals(Optional.of(PLAYER), who, "the gamemode must accept the ticket for this player");
    }

    @Test
    void gamemodeNamesMatchCaseInsensitively() {
        assertInstanceOf(HubRouter.Referral.class, HubRouter.route(hub(), "skywars", PLAYER, NOW));
    }

    @Test
    void ticketExpiresAfterTheTtl() {
        NetworkConfig cfg = hub();
        HubRouter.Referral ref = (HubRouter.Referral) HubRouter.route(cfg, "SkyWars", PLAYER, NOW);
        assertTrue(HandoffTicket.verify(ref.ticket(), cfg.secrets, SKYWARS, NOW + HubRouter.TICKET_TTL_SEC).isEmpty());
    }

    @Test
    void unknownGamemodeListsTheAvailableOnes() {
        HubRouter.Unknown u = assertInstanceOf(HubRouter.Unknown.class, HubRouter.route(hub(), "BedWars", PLAYER, NOW));
        assertEquals(List.of("SkyWars"), u.available());
    }

    @Test
    void badConfigBecomesUnavailableInsteadOfThrowing() {
        NetworkConfig badAddress = NetworkConfig.parse("""
            {"version":1,"role":"hub","server_id":"%s","secrets":["%s"],
             "gamemodes":[{"server_id":"%s","name":"SkyWars","address":"no-port"}]}
            """.formatted(HUB, SECRET_HEX, SKYWARS));
        assertInstanceOf(HubRouter.Unavailable.class, HubRouter.route(badAddress, "SkyWars", PLAYER, NOW));

        NetworkConfig emptySecret = NetworkConfig.parse("""
            {"version":1,"role":"hub","server_id":"%s","secrets":[""],
             "gamemodes":[{"server_id":"%s","name":"SkyWars","address":"sky.example.com:5521"}]}
            """.formatted(HUB, SKYWARS));
        assertInstanceOf(HubRouter.Unavailable.class, HubRouter.route(emptySecret, "SkyWars", PLAYER, NOW));
    }

    @Test
    void onlyAHubCanRoute() {
        NetworkConfig gm = NetworkConfig.parse("""
            {"version":1,"role":"gamemode","server_id":"%s","require_referral":true,"secrets":["%s"]}
            """.formatted(SKYWARS, SECRET_HEX));
        assertInstanceOf(HubRouter.Unavailable.class, HubRouter.route(gm, "SkyWars", PLAYER, NOW));
        assertInstanceOf(HubRouter.Unavailable.class, HubRouter.route(null, "SkyWars", PLAYER, NOW));
    }

    @Test
    void hubWithoutASecretCannotRoute() {
        NetworkConfig cfg = NetworkConfig.parse("""
            {"version":1,"role":"hub","server_id":"%s","secrets":[],
             "gamemodes":[{"server_id":"%s","name":"SkyWars","address":"sky.example.com:5521"}]}
            """.formatted(HUB, SKYWARS));
        assertInstanceOf(HubRouter.Unavailable.class, HubRouter.route(cfg, "SkyWars", PLAYER, NOW));
    }

    private static final String HUB_KEY_HEX = "aa".repeat(32);
    private static final String SKYWARS_KEY_HEX = "bb".repeat(32);

    @Test
    void version2SignsWithTheTargetGamemodesOwnKey() {
        NetworkConfig cfg = NetworkConfig.parse("""
            {"version":2,"role":"hub","server_id":"%s","secrets":["%s"],
             "gamemodes":[{"server_id":"%s","name":"SkyWars","address":"sky.example.com:5521","secret":"%s"}]}
            """.formatted(HUB, HUB_KEY_HEX, SKYWARS, SKYWARS_KEY_HEX));
        HubRouter.Referral ref = assertInstanceOf(HubRouter.Referral.class, HubRouter.route(cfg, "SkyWars", PLAYER, NOW));

        List<byte[]> skywarsKeys = List.of(NetworkConfig.hexToBytes(SKYWARS_KEY_HEX));
        assertEquals(Optional.of(PLAYER), HandoffTicket.verify(ref.ticket(), skywarsKeys, SKYWARS, NOW + 1));
        assertTrue(HandoffTicket.verify(ref.ticket(), cfg.secrets, SKYWARS, NOW + 1).isEmpty(),
                "the hub's own key must not verify a ticket for another server");
    }

    @Test
    void version2GamemodeWithoutAKeyIsUnavailable() {
        NetworkConfig cfg = NetworkConfig.parse("""
            {"version":2,"role":"hub","server_id":"%s","secrets":["%s"],
             "gamemodes":[{"server_id":"%s","name":"SkyWars","address":"sky.example.com:5521"}]}
            """.formatted(HUB, HUB_KEY_HEX, SKYWARS));
        assertInstanceOf(HubRouter.Unavailable.class, HubRouter.route(cfg, "SkyWars", PLAYER, NOW));
    }
}
