# Lanclip Design and Acceptance Plan

Status: implemented; core two-machine sync passed, extended recovery UAT remains
Target: macOS + Wayland Linux on the same trusted LAN, validated on Omarchy/Hyprland
Primary goal: fast, dependable, automatic two-way synchronization of text and URL clipboard content

This document records the normative design and release criteria. Statements
using “should” describe requirements; the source and README describe current
behavior where implementation details differ.

## 1. Product definition

Lanclip is a tiny, headless clipboard synchronizer for two user-controlled
computers on the same local network whose addresses may change.

It should:

- Synchronize UTF-8 text and URLs in both directions.
- Discover peers automatically using mDNS/DNS-SD (Bonjour on macOS, Avahi-compatible on Linux).
- Reconnect automatically after sleep, Wi-Fi interruption, process restart, or DHCP address changes.
- Require explicit one-time pairing before exchanging clipboard contents.
- Encrypt and authenticate all clipboard traffic.
- Run only in the logged-in user's desktop session.
- Start automatically on login.
- Avoid cloud services, accounts, browser extensions, clipboard history, analytics, and content logging.

It should not initially:

- Transfer images, files, HTML, rich text, or formatting.
- Synchronize the Linux primary selection.
- Work across routers, VPNs, or the public internet.
- Provide keyboard/mouse sharing.
- Provide a tray icon or graphical settings window.
- Store old clipboard entries or retry a backlog of clipboard contents.

The product should remain useful without any GUI. All setup and diagnostics should be available through the `lanclip` command.

## 2. Success criteria

The first release is complete when all of the following are true:

- A Safari or Chrome URL copied on the Mac can be pasted on Omarchy within 750 ms under normal LAN conditions.
- Plain text copied on Omarchy can be pasted on the Mac within 750 ms.
- The final value in a burst of ten copies made 200 ms apart is never lost.
- Copying the same text twice produces two local clipboard ownership changes without creating a network loop.
- A remote clipboard update is not echoed back indefinitely.
- Wi-Fi off/on, Mac sleep/wake, and either daemon restarting recover without re-pairing.
- A DHCP address change is recovered through discovery without editing configuration.
- An unpaired LAN device can discover Lanclip but cannot read or replace either clipboard.
- Neither clipboard contents nor authentication secrets appear in normal logs.
- Both services restart automatically after a crash.
- Idle CPU use is negligible and idle memory remains modest (target: under 30 MB per process).

## 3. Recommended implementation

Use one Go repository and one `lanclip` executable per platform. Keep shared discovery, security, protocol, and synchronization logic in Go. Isolate clipboard integration behind platform-specific adapters selected with Go build tags.

Why Go:

- Straightforward persistent networking and concurrency.
- TLS support in the standard library.
- Simple unit and integration testing.
- Small deployment footprint with no runtime environment.
- The macOS build can use a very small AppKit bridge through cgo while retaining one executable.

Build each binary on its target machine. Do not make cross-compilation a first-release requirement.

### 3.1 Linux clipboard adapter

Dependencies already present on Omarchy:

- `wl-paste`
- `wl-copy`

Behavior:

- Start `wl-paste --type text --watch <internal-handler>` as a long-lived child process or implement equivalent watch plumbing around `wl-paste`.
- Treat each watch callback as a local clipboard ownership event, even if its text matches an older value.
- Write received text with `wl-copy --type text/plain;charset=utf-8`.
- Limit payloads before reading them fully into memory.
- Restart the watcher if it exits unexpectedly.
- Never print clipboard bytes to stdout, stderr, or logs.

The systemd user service must run inside the graphical user session with access to `WAYLAND_DISPLAY` and `XDG_RUNTIME_DIR`. Verify this on the real Omarchy session; do not assume a shell environment is inherited.

Reference: <https://github.com/bugaevc/wl-clipboard>

### 3.2 macOS clipboard adapter

Use `NSPasteboard.general` through a minimal Objective-C/AppKit bridge compiled by cgo.

Behavior:

- Poll `NSPasteboard.changeCount` every 100 ms.
- Read only `NSPasteboard.PasteboardType.string` for version 1.
- Write received values as plain UTF-8 strings.
- Run the polling loop only inside the logged-in Aqua session.
- Do not use `pbpaste` polling in the final implementation; repeated process spawning is acceptable for a spike, but the native API is cleaner and more dependable.
- A LaunchAgent, not a system LaunchDaemon, should own the process.

Apple documents `changeCount` as incrementing when pasteboard ownership changes, which makes it the correct signal for detecting copies, including URL values that also expose a plain-text representation.

Reference: <https://developer.apple.com/documentation/appkit/nspasteboard/changecount>

## 4. Discovery and changing IP addresses

Every running instance should both advertise and browse for this DNS-SD service:

```text
_lanclip._tcp.local.
```

Advertised fields:

```text
instance: <platform machine name; macOS Computer Name or Linux hostname>
port:     24872
TXT v:    1
TXT id:   <stable random device UUID>
TXT pk:   <short SHA-256 fingerprint of the device identity public key>
TXT os:   macos | linux
```

Rules:

- The stable device UUID is identity; the advertised IP address is only a temporary location.
- Ignore advertisements carrying this instance's own UUID.
- Refresh the advertised machine name at daemon startup so host renames and
  stale generic names do not require re-pairing. Update a stored peer label only
  after that peer authenticates with its pinned identity.
- Resolve all current IPv4 and IPv6 link-local addresses for a peer.
- Prefer the address on the same active interface/subnet.
- When an advertisement changes, update the peer address and reconnect if needed.
- When a peer disappears, keep its trusted identity but mark it offline.
- Browse continuously and also run a periodic recovery query with jitter.
- Service discovery never grants trust; it only supplies a possible network endpoint.

Use the pinned `github.com/libp2p/zeroconf/v2` implementation. Validate
continuous browse behavior, TTL expiry, address changes, Avahi interoperability,
and Bonjour interoperability during physical acceptance testing.

Omarchy already has Avahi and `nss-mdns` installed and active. macOS includes Bonjour. The application should still implement DNS-SD itself so deployment does not depend on parsing output from `avahi-browse` or `dns-sd`.

Fallback behavior:

- Support an optional `manual_peers` hostname list in configuration.
- Manual peers have no default value and must be configured explicitly.
- Manual peers use exactly the same authentication and certificate pinning as discovered peers.
- Guest Wi-Fi or client-isolated networks that block multicast are out of scope, but `lanclip status` must report that discovery is not seeing peers.

Reference: <https://github.com/libp2p/zeroconf>

## 5. Identity, pairing, and transport security

Clipboard traffic often includes passwords, tokens, and private text. “LAN only” is not sufficient authentication.

### 5.1 Device identity

On first start, each instance should generate:

- A random UUID.
- An Ed25519 private/public key pair.
- A self-signed TLS certificate bound to that identity.

Store the private material with user-only permissions. Never include a private key in discovery records, configuration output, logs, or the shared project folder.

### 5.2 Pairing flow

Implement explicit short-authentication-string pairing:

1. Both daemons discover each other but remain untrusted.
2. On either machine, run `lanclip pair <peer-name-or-id>`.
3. The daemons make a temporary TLS connection and exchange public identity keys.
4. Both derive the same six-word comparison code from the complete transcript and both public keys.
5. `lanclip pair` prints the peer name, full key fingerprint, and six-word code on both machines.
6. The user verifies that the codes match, then runs `lanclip approve` on both
   machines and chooses the named device from a numbered menu.
7. Approval carries an opaque local session token and the exact fingerprint and
   code that were displayed; it fails if the pending request changed or expired.
8. Pending requests remain in memory, expire after five minutes, and are capped
   and rate-limited. They are never restored after restart.
9. Each side pins the other side's public key fingerprint using an atomic
   trust-store update.
10. All future connections require the pinned certificate. A changed identity is rejected and reported; it is never silently trusted.

If implementing a correct transcript-bound comparison protocol becomes disproportionately complex, use a one-time high-entropy pairing secret entered on the second machine instead. Do not substitute unauthenticated trust-on-first-use without a user comparison step.

### 5.3 Transport

- Use a persistent full-duplex TCP connection protected by TLS 1.3.
- Listen on TCP port 24872 by default and advertise the actual port through DNS-SD.
- Require mutual authentication using the pinned peer identities after pairing.
- Reject unknown peers before processing application messages.
- Apply read/write deadlines and periodic keepalives.
- Bound concurrent unauthenticated handshakes and pairing attempts per LAN
  source. Bound discovery, clipboard, and loop-suppression queues.
- Reconnect with bounded exponential backoff plus jitter (for example: 250 ms through 10 seconds).
- Never disable certificate verification as a production shortcut.
- Refuse application payloads larger than 1 MiB.

The TCP listener uses a wildcard bind so it survives interface and address
changes, but it rejects connections whose source is not on a directly connected
eligible multicast LAN subnet before the TLS handshake. Manual peer resolution and
discovered endpoints apply the same direct-subnet rule. Pairing remains the
cryptographic security boundary. Installers do not modify firewall rules; users
should additionally restrict the port to the trusted LAN subnet.

Reference: <https://pkg.go.dev/crypto/tls>

## 6. Wire protocol

Use a small versioned protocol over the TLS stream. Length-prefixed JSON is sufficient and easy to inspect in tests; the TLS layer hides it on the network.

Frame format:

```text
4-byte unsigned big-endian payload length
UTF-8 JSON payload
```

Initial message types:

```json
{"type":"hello","protocol":1,"device_id":"...","name":"Studio Mac"}
{"type":"clipboard","protocol":1,"event_id":"...","mime":"text/plain;charset=utf-8","text":"..."}
{"type":"ping","protocol":1,"nonce":"..."}
{"type":"pong","protocol":1,"nonce":"..."}
{"type":"error","protocol":1,"code":"..."}
```

Protocol rules:

- Reject unknown major protocol versions.
- Reject malformed frames and oversized lengths before allocating buffers.
- Treat clipboard text as UTF-8 and preserve newlines exactly.
- Ignore empty clipboard values in version 1.
- Do not queue clipboard history while disconnected.
- Do not automatically push a stale clipboard merely because a peer reconnects.
- Only clipboard changes observed while connected are sent. A later explicit copy sends the current value.
- Use a random `event_id` for every local clipboard ownership change.

## 7. Loop prevention and connection ownership

### 7.1 Clipboard loop prevention

When a remote clipboard event is applied locally:

1. Record its event ID and a SHA-256 hash of the normalized bytes in a short-lived suppression set.
2. Write the value to the platform clipboard.
3. When the local watcher fires, compare its bytes with the pending suppression entry.
4. Consume exactly one matching suppression entry and do not send it back.
5. Expire unused suppression entries after a few seconds.

Do not permanently deduplicate by content. The user must be able to intentionally copy identical text again later.

### 7.2 Avoiding duplicate peer connections

Both peers listen and discover, so both may attempt to connect simultaneously. Use the stable UUIDs to choose an initiator:

- The lexicographically smaller UUID initiates the long-lived sync connection.
- The other side listens.
- Pairing connections are exempt from this rule.
- If duplicate authenticated connections exist, deterministically retain one and close the other.

## 8. Command-line interface

The same binary should act as daemon and local control client.

Required commands:

```text
lanclip daemon
lanclip status
lanclip peers
lanclip pair [peer-name]
lanclip approve [peer-name]
lanclip reject [peer-name]
lanclip unpair [peer-name]
lanclip doctor
lanclip version
```

The daemon should expose a user-only local control socket:

- Linux: `$XDG_RUNTIME_DIR/lanclip.sock`
- macOS: `~/Library/Application Support/Lanclip/lanclip.sock`

`lanclip status` should report, without clipboard content:

- Daemon version and uptime.
- Local device name and ID.
- Clipboard watcher health.
- Discovery health and last browse result.
- Trusted peers and current connection state.
- Resolved peer addresses.
- Last sent/received event timestamps and byte counts only.
- Reconnect count and last error.

User-facing output must be human-readable rather than raw control-protocol
JSON. Peer actions should show a numbered menu when no name is supplied and
must resolve the chosen name to the stable device ID internally. Users should
never need to find or type UUIDs. Approval, rejection, and unpairing should ask
for confirmation before changing trust state.

`lanclip doctor` should test:

- Clipboard read/write adapter availability.
- mDNS multicast availability.
- Listener port availability.
- Config and key permissions.
- Local control socket access.
- Peer discovery and authentication state.
- Presence of required session environment variables.

## 9. Configuration and data locations

### Linux

```text
~/.config/lanclip/config.json
~/.local/share/lanclip/identity.pem
~/.local/share/lanclip/peers.json
~/.local/bin/lanclip
~/.config/systemd/user/lanclip.service
$XDG_RUNTIME_DIR/lanclip.sock
```

### macOS

```text
~/Library/Application Support/Lanclip/config.json
~/Library/Application Support/Lanclip/identity.pem
~/Library/Application Support/Lanclip/peers.json
~/Library/Application Support/Lanclip/bin/lanclip
~/Library/Application Support/Lanclip/lanclip.sock
~/Library/LaunchAgents/com.dpellerin.lanclip.plist
```

Configuration should contain only non-secret operational settings. Identity keys and trusted-peer state should be separate files with mode `0600` where applicable.
The `name` field is maintained automatically from the platform machine name;
it is not a manually assigned pairing alias.

Suggested configuration:

```json
{
  "version": 1,
  "name": "Studio Mac",
  "listen_port": 24872,
  "service_type": "_lanclip._tcp",
  "max_clipboard_bytes": 1048576,
  "manual_peers": []
}
```

## 10. Repository layout

```text
lanclip/
├── .github/              CI, dependency updates, and contribution templates
├── README.md
├── SECURITY.md
├── CONTRIBUTING.md
├── LICENSE
├── go.mod
├── go.sum
├── Makefile
├── cmd/lanclip/          CLI and daemon entry point
├── internal/             clipboard, config, control, discovery,
│                         identity, pairing, protocol, sync, and transport
├── install/              Linux systemd and macOS LaunchAgent installers
├── test/integration/     cross-package privacy checks
└── docs/                 design and public-release checklists
```

Run binaries from the per-user installation paths, not from a cloud-synchronized
folder or temporary checkout.

## 11. Service integration

### 11.1 Omarchy systemd user service

Requirements:

- `After=graphical-session.target`
- `PartOf=graphical-session.target`
- `Restart=on-failure`
- A short restart delay.
- Absolute `ExecStart` path.
- No root privileges.
- No access to clipboard data outside the logged-in Wayland session.

Install and validate with:

```text
systemctl --user daemon-reload
systemctl --user enable --now lanclip.service
systemctl --user status lanclip.service
journalctl --user -u lanclip.service
lanclip doctor
```

If the service cannot access Wayland, fix user-session environment propagation rather than launching it as root.

### 11.2 macOS LaunchAgent

Requirements:

- Use a per-user LaunchAgent, never a LaunchDaemon.
- Run only in the Aqua login session.
- `KeepAlive` after crashes.
- `RunAtLoad` at login.
- Absolute program and config paths.
- Metadata-only stdout/stderr logs.

Build the binary locally on the Mac so it is not quarantined as a downloaded unsigned executable. The first incoming connection may cause a macOS firewall prompt; approve local-network access for Lanclip.

## 12. Logging and privacy

Default logs may contain:

- Timestamp.
- Severity.
- Peer name and shortened ID.
- Connection state.
- Event ID.
- Clipboard byte count.
- Timing and error codes.

Default logs must never contain:

- Clipboard text.
- Full clipboard hashes.
- Private keys.
- Pairing secrets.
- TLS session secrets.

Debug mode must follow the same content rule. A packet-size trace is acceptable; payload dumping is not.

## 13. Implementation phases

### Phase 0: feasibility spikes

Complete these before building the full daemon:

1. macOS native watcher detects plain text, Safari URLs, Chrome URLs, multiline Unicode, and repeated copies.
2. macOS native writer updates the clipboard from a background LaunchAgent.
3. Omarchy `wl-paste --watch` reports changes reliably and `wl-copy` can apply remote text.
4. A remote write on each platform can be suppressed without suppressing a later intentional identical copy.
5. Each machine can advertise and browse `_lanclip._tcp.local` and observe address changes.
6. A tiny TLS connection works in both directions using locally generated Ed25519 certificates.

Stop and redesign any failed adapter before adding CLI or installer work.

### Phase 1: repository and core protocol

- Create the repository and Go module.
- Add configuration loading and secure data directories.
- Add device identity generation.
- Implement framed messages with size limits.
- Add protocol and malformed-input tests.

### Phase 2: clipboard adapters

- Implement Linux watcher/writer.
- Implement macOS AppKit watcher/writer.
- Add adapter health reporting.
- Implement and unit-test loop suppression.

### Phase 3: discovery

- Advertise and browse the service.
- Ignore self advertisements.
- Maintain peer endpoint state across TTL updates.
- Add periodic recovery queries.
- Test Wi-Fi disable/enable and DHCP changes.

### Phase 4: pairing and authenticated TLS

- Implement identity exchange and comparison code.
- Implement approval, rejection, unpairing, and changed-key errors.
- Pin peer certificates.
- Add negative tests for unknown and impersonating peers.

### Phase 5: synchronization engine

- Establish one deterministic full-duplex peer connection.
- Send local events immediately.
- Apply remote events and suppress echoes.
- Add keepalives, timeouts, and reconnection backoff.
- Ensure reconnect does not push stale clipboard contents.

### Phase 6: CLI and diagnostics

- Add the local control socket.
- Implement all required commands.
- Make `status` and `doctor` sufficient to diagnose discovery, clipboard, authentication, and reconnect failures.

### Phase 7: installers and autostart

- Add idempotent Linux and macOS installers.
- Add service definitions.
- Add `--purge` to uninstall scripts for removing identity/config only when explicitly requested.
- Verify normal uninstall preserves pairing state and purge removes it.

### Phase 8: real two-machine UAT

- Run every acceptance test below on the actual Mac and Omarchy machines.
- Leave both services running through at least one sleep/wake cycle.
- Record versions and final service status in the README.

## 14. Test plan

### Automated tests

- Frame encode/decode, partial reads, multiple frames, malformed JSON, and oversized lengths.
- UTF-8, newlines, tabs, emoji, and URL strings.
- Loop-suppression match, mismatch, consumption, and expiry.
- Self-discovery filtering and peer address updates.
- Duplicate connection resolution.
- Certificate pinning and changed-identity rejection.
- Reconnect backoff bounds and cancellation.
- Configuration permissions and migrations.
- Logs contain no test clipboard secrets.

### Real-machine acceptance tests

1. Copy plain text Mac to Linux.
2. Copy plain text Linux to Mac.
3. Copy URLs from Safari, Chrome, and terminal output in both directions.
4. Copy multiline text and Unicode in both directions.
5. Copy the same value twice intentionally.
6. Copy ten changing values 200 ms apart; verify the final value arrives.
7. Copy a value larger than 1 MiB; verify rejection without crash or partial overwrite.
8. Copy images and files; verify they are ignored without corrupting the current text clipboard.
9. Restart the Linux service; verify discovery and reconnect.
10. Restart the macOS LaunchAgent; verify discovery and reconnect.
11. Put the Mac to sleep and wake it; verify reconnect without pairing.
12. Disable and re-enable Wi-Fi on either machine.
13. Renew DHCP or move between home access points; verify changed IP recovery.
14. Run both copy actions nearly simultaneously and document last-arrival behavior.
15. Start an unpaired test instance; verify it cannot exchange clipboard data.
16. Change a paired peer's identity; verify a hard authentication failure and clear diagnostic.
17. Inspect listening sockets and confirm no outbound internet service is involved.
18. Inspect logs and confirm no clipboard contents or secrets were recorded.

## 15. Operational commands to support

Examples of the intended final workflow:

```text
# Both machines
lanclip status
lanclip peers

# Pair once
lanclip pair "Studio Mac"
lanclip approve

# Diagnose later
lanclip doctor

# Remove trust without uninstalling
lanclip unpair
```

Installation should print the exact service-management commands for that platform. It should never silently modify firewall rules or delete old identities.

## 16. Risks and deliberate tradeoffs

- mDNS can be blocked by guest Wi-Fi/client isolation. Mitigation: clear diagnostics and optional `.local` manual peers.
- macOS AppKit access from a headless process must occur in the user GUI session. Mitigation: LaunchAgent plus an early feasibility spike.
- Wayland clipboard access depends on the compositor session. Mitigation: use the already working `wl-clipboard` path and validate the systemd environment.
- Clipboard contents are sensitive. Mitigation: mutual pinned identities, TLS, strict no-content logging, and no history.
- A short pairing code is only safe if transcript-bound and compared on both machines. Mitigation: use a high-entropy one-time secret if the comparison protocol cannot be implemented confidently.
- Supporting rich clipboard formats multiplies MIME negotiation and ownership edge cases. Mitigation: version 1 is text-only.
- Simultaneous copies may race. Mitigation: clearly document last-arrival behavior; do not add distributed ordering unless real use demonstrates a need.

## 17. Definition of done

Do not call version 1 complete merely because two terminal processes exchanged one string. It is done only when:

- Both native clipboard adapters pass their spikes and real tests.
- Discovery survives a real address change.
- Pairing rejects an unknown peer and pins an approved peer.
- Clipboard traffic is encrypted and authenticated.
- Echo loops are prevented.
- Login services work after reboot/login.
- Sleep/wake and Wi-Fi recovery work without manual intervention.
- `lanclip doctor` identifies intentionally broken discovery, clipboard, and trust states.
- The actual Mac/Omarchy acceptance suite passes.
- The repository contains install, uninstall, test, and troubleshooting documentation.
