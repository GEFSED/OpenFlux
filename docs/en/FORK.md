# Fork information

This repository is a modified fork of
[p1neappleXpress/OpenFlux](https://github.com/p1neappleXpress/OpenFlux).
It preserves the upstream Git history and remains licensed under GPL-3.0-or-later.

## Major changes in this fork

- native Android `VpnService` client with an Android 11-style UI and a
  phone-Settings-style vertical navigation;
- a second Android connection mode, Proxy (SOCKS5), alongside the tunnel mode,
  with optional local-network access and SOCKS5 authentication;
- encrypted Android Keystore storage for the document URL and shared secret;
- optional end-to-end AES-256-GCM transport encryption with scrypt key
  derivation, wire-compatible with upstream's exit-node and client binaries;
  leave the key unset to talk to a plain, unencrypted upstream exit node;
- a live upload/download speed indicator in the connection notification;
- `--encryption-key-file` reads the shared secret from a root-only file
  instead of a process argument, keeping it out of `ps`/process listings;
- removal of the non-functional iOS prototype.

## Branches

`main` stays wire-compatible with the current upstream exit-node and client
binaries. A separate `experimental` branch carries features that need a
matching exit node running this fork's own code (a framed transport protocol
multiplexing ping/DNS-relay traffic into the encrypted channel): the ping
graph, exit-node country display, and server-side DNS relay. See
[UPSTREAM_DIFF.md](UPSTREAM_DIFF.md) for the full comparison.

The fork is experimental and is not endorsed by or affiliated with Yandex.
