package dev.bitaces.talepanel.network;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.NoSuchFileException;
import java.nio.file.Path;
import java.util.function.Consumer;

/**
 * Serves the panel-written config.json, re-reading it only when its mtime
 * or size changes (mtime alone can miss a rewrite within the filesystem's
 * timestamp granularity). The daemon replaces the file atomically, so a rewrite is either
 * fully visible or not at all; a file that still fails to parse is reported
 * once and the last good config stays in effect.
 */
public final class ConfigSource {

    private final Path file;
    private final Consumer<String> warn;
    private long loadedMtime = Long.MIN_VALUE;
    private long loadedSize = -1;
    private NetworkConfig config;

    public ConfigSource(Path file, Consumer<String> warn) {
        this.file = file;
        this.warn = warn;
    }

    /** The current config, or null if this server is not part of a network. */
    public synchronized NetworkConfig current() {
        long mtime;
        long size;
        try {
            mtime = Files.getLastModifiedTime(file).toMillis();
            size = Files.size(file);
        } catch (NoSuchFileException e) {
            config = null;
            loadedMtime = Long.MIN_VALUE;
            loadedSize = -1;
            return null;
        } catch (IOException e) {
            return config;
        }
        if (mtime == loadedMtime && size == loadedSize) {
            return config;
        }
        loadedMtime = mtime;
        loadedSize = size;
        try {
            config = NetworkConfig.parse(Files.readString(file));
        } catch (IOException | RuntimeException e) {
            warn.accept("TalePanel network: ignoring unreadable " + file + " (" + e.getMessage()
                    + "); keeping the previous config");
        }
        return config;
    }
}
