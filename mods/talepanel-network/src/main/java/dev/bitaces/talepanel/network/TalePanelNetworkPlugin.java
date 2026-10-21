package dev.bitaces.talepanel.network;

import com.hypixel.hytale.server.core.Message;
import com.hypixel.hytale.server.core.event.events.player.PlayerSetupConnectEvent;
import com.hypixel.hytale.server.core.plugin.JavaPlugin;
import com.hypixel.hytale.server.core.plugin.JavaPluginInit;

import javax.annotation.Nonnull;
import java.nio.file.Path;
import java.util.Optional;
import java.util.logging.Level;

/**
 * Hytale entry point. Hub servers get {@code /play <gamemode>}, which refers the
 * player to the gamemode with a signed handoff ticket; gamemode servers with
 * require_referral turn away connections that do not carry a valid ticket.
 * All decisions live in {@link HubRouter} and {@link ReferralGate}.
 */
public class TalePanelNetworkPlugin extends JavaPlugin {

    /**
     * Where the TalePanel daemon writes this server's network config, relative
     * to the server's working directory (see the daemon's write_mod_config).
     * Outside mods/, where Hytale would mistake the folder for an asset pack.
     */
    static final Path CONFIG_PATH = Path.of("talepanel", "network.json");

    private ConfigSource config;

    public TalePanelNetworkPlugin(@Nonnull JavaPluginInit init) {
        super(init);
    }

    @Override
    protected void setup() {
        config = new ConfigSource(CONFIG_PATH, msg -> getLogger().at(Level.WARNING).log(msg));

        NetworkConfig cfg = config.current();
        if (cfg == null) {
            getLogger().at(Level.INFO).log("TalePanel network: no %s, staying inactive until the panel writes one", CONFIG_PATH);
        } else {
            getLogger().at(Level.INFO).log("TalePanel network: role=%s require_referral=%s", cfg.role, cfg.requireReferral);
        }

        getEventRegistry().register(PlayerSetupConnectEvent.class, this::onSetupConnect);
        getCommandRegistry().registerCommand(new PlayCommand(config));
    }

    private void onSetupConnect(PlayerSetupConnectEvent event) {
        long now = System.currentTimeMillis() / 1000;
        Optional<String> deny = ReferralGate.denyReason(config.current(), event.isReferralConnection(),
                event.getReferralData(), event.getUuid(), now);
        if (deny.isPresent()) {
            event.setReason(Message.raw(deny.get()));
            event.setCancelled(true);
            getLogger().at(Level.INFO).log("TalePanel network: refused %s (%s)", event.getUsername(), deny.get());
        }
    }
}
