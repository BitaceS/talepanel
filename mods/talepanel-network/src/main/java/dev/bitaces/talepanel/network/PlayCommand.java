package dev.bitaces.talepanel.network;

import com.hypixel.hytale.component.Ref;
import com.hypixel.hytale.component.Store;
import com.hypixel.hytale.server.core.Message;
import com.hypixel.hytale.server.core.command.system.CommandContext;
import com.hypixel.hytale.server.core.command.system.arguments.system.RequiredArg;
import com.hypixel.hytale.server.core.command.system.arguments.types.ArgTypes;
import com.hypixel.hytale.server.core.command.system.basecommands.AbstractPlayerCommand;
import com.hypixel.hytale.server.core.universe.PlayerRef;
import com.hypixel.hytale.server.core.universe.world.World;
import com.hypixel.hytale.server.core.universe.world.storage.EntityStore;

/** {@code /play <gamemode>} — sends the player from the hub to a gamemode server. */
final class PlayCommand extends AbstractPlayerCommand {

    private final ConfigSource config;
    private final RequiredArg<String> gamemodeArg;

    PlayCommand(ConfigSource config) {
        super("play", "Join a gamemode on this network");
        this.config = config;
        this.gamemodeArg = withRequiredArg("gamemode", "Name of the gamemode to join", ArgTypes.STRING);
        // Every player on a hub must be able to leave it.
        requireNoPermission();
    }

    @Override
    protected void execute(CommandContext ctx, Store<EntityStore> store, Ref<EntityStore> ref,
                           PlayerRef player, World world) {
        long now = System.currentTimeMillis() / 1000;
        HubRouter.Route route = HubRouter.route(config.current(), gamemodeArg.get(ctx), player.getUuid(), now);
        switch (route) {
            case HubRouter.Referral r -> {
                ctx.sendMessage(Message.raw("Sending you to " + r.name() + "..."));
                player.referToServer(r.address().host(), r.address().port(), r.ticket());
            }
            case HubRouter.Unknown u -> ctx.sendMessage(Message.raw(u.available().isEmpty()
                    ? "No gamemodes are available right now."
                    : "Unknown gamemode. Available: " + String.join(", ", u.available())));
            case HubRouter.Unavailable u -> ctx.sendMessage(Message.raw(u.reason()));
        }
    }
}
