package dev.bitaces.talepanel.network;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.attribute.FileTime;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;

import static org.junit.jupiter.api.Assertions.*;

class ConfigSourceTest {

    private static String config(String role, UUID id) {
        return """
            {"version":1,"role":"%s","server_id":"%s","secrets":["aabb"]}
            """.formatted(role, id);
    }

    private static void write(Path file, String body, long mtimeMillis) throws Exception {
        Files.writeString(file, body);
        Files.setLastModifiedTime(file, FileTime.fromMillis(mtimeMillis));
    }

    @Test
    void missingFileMeansNotNetworked() {
        ConfigSource src = new ConfigSource(Path.of("does-not-exist", "config.json"), msg -> {});
        assertNull(src.current());
    }

    // The panel rewrites config.json whenever roles change; the mod must pick
    // that up without a server restart.
    @Test
    void reloadsWhenThePanelRewritesTheFile(@TempDir Path dir) throws Exception {
        Path file = dir.resolve("config.json");
        UUID id = UUID.randomUUID();
        write(file, config("gamemode", id), 1_000_000L);

        ConfigSource src = new ConfigSource(file, msg -> {});
        assertEquals("gamemode", src.current().role);

        write(file, config("hub", id), 2_000_000L);
        assertEquals("hub", src.current().role);
    }

    @Test
    void keepsTheLastGoodConfigWhenARewriteIsBroken(@TempDir Path dir) throws Exception {
        Path file = dir.resolve("config.json");
        write(file, config("hub", UUID.randomUUID()), 1_000_000L);
        List<String> warnings = new ArrayList<>();
        ConfigSource src = new ConfigSource(file, warnings::add);
        assertEquals("hub", src.current().role);

        write(file, "{not json", 2_000_000L);
        assertEquals("hub", src.current().role, "a broken file must not drop the network");
        assertEquals(1, warnings.size(), "the parse failure must be reported once");
        src.current();
        assertEquals(1, warnings.size(), "an unchanged broken file must not be re-reported");
    }
}
