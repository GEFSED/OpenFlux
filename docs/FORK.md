# Fork information

This repository is a modified fork of
[p1neappleXpress/OpenFlux](https://github.com/p1neappleXpress/OpenFlux).
It preserves the upstream Git history and remains licensed under GPL-3.0-or-later.

## Major changes in this fork

- native Android `VpnService` client with an Android 11-style UI and a
  phone-Settings-style vertical navigation;
- a second Android connection mode, Proxy (SOCKS5), alongside the VPN mode,
  with optional local-network access and SOCKS5 authentication;
- DNS resolution relayed through the encrypted tunnel to the exit node (or
  locally, per a user setting) instead of leaving the client's network directly;
- encrypted Android Keystore storage for the document URL and shared secret;
- mandatory end-to-end AES-256-GCM transport encryption with scrypt key derivation;
- authenticated encrypted ping frames and an animated latency graph;
- a live upload/download speed indicator in the connection notification;
- safer VDS deployment using root-only URL/key files instead of process arguments;
- removal of the non-functional iOS prototype.

The fork is experimental and is not endorsed by or affiliated with Yandex.
