package dev.bitaces.talepanel.network;

import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.*;

class ServerAddressTest {

    @Test
    void parsesHostnameAndPort() {
        ServerAddress a = ServerAddress.parse("play.example.com:5521");
        assertEquals("play.example.com", a.host());
        assertEquals(5521, a.port());
    }

    @Test
    void parsesBracketedIpv6() {
        ServerAddress a = ServerAddress.parse("[2001:db8::7]:5520");
        assertEquals("2001:db8::7", a.host());
        assertEquals(5520, a.port());
    }

    // The panel joins public_address and port with a plain ':' — on the
    // IPv6-only nodes that yields an unbracketed literal; the port is the last
    // segment.
    @Test
    void parsesUnbracketedIpv6FromThePanel() {
        ServerAddress a = ServerAddress.parse("2001:db8::7:5522");
        assertEquals("2001:db8::7", a.host());
        assertEquals(5522, a.port());
    }

    @Test
    void rejectsMissingOrInvalidPort() {
        assertThrows(IllegalArgumentException.class, () -> ServerAddress.parse("example.com"));
        assertThrows(IllegalArgumentException.class, () -> ServerAddress.parse("example.com:"));
        assertThrows(IllegalArgumentException.class, () -> ServerAddress.parse("example.com:70000"));
        assertThrows(IllegalArgumentException.class, () -> ServerAddress.parse(":5520"));
        assertThrows(IllegalArgumentException.class, () -> ServerAddress.parse(null));
    }
}
