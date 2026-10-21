package dev.bitaces.talepanel.network;

/**
 * A gamemode's "host:port" address as written by the panel. The panel joins
 * the node's public address and the server port with a plain ':', so an IPv6
 * literal may arrive unbracketed; the port is always the last segment.
 */
public record ServerAddress(String host, int port) {

    public static ServerAddress parse(String address) {
        if (address == null) {
            throw new IllegalArgumentException("address is null");
        }
        int colon = address.lastIndexOf(':');
        if (colon <= 0 || colon == address.length() - 1) {
            throw new IllegalArgumentException("address must be host:port, got '" + address + "'");
        }
        String host = address.substring(0, colon);
        if (host.startsWith("[") && host.endsWith("]")) {
            host = host.substring(1, host.length() - 1);
        }
        int port;
        try {
            port = Integer.parseInt(address.substring(colon + 1));
        } catch (NumberFormatException e) {
            throw new IllegalArgumentException("invalid port in '" + address + "'");
        }
        if (host.isEmpty() || port < 1 || port > 65535) {
            throw new IllegalArgumentException("invalid address '" + address + "'");
        }
        return new ServerAddress(host, port);
    }
}
