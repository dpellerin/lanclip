# Lanclip

Lanclip is a small, headless, two-way text clipboard synchronizer for a trusted
LAN. It discovers peers with DNS-SD, requires explicit comparison-code pairing,
and carries clipboard events over mutually authenticated TLS 1.3 connections.
It does not use a cloud service, keep clipboard history, or log clipboard text.

Version 1 supports macOS and Wayland Linux (including Omarchy/Hyprland). It
synchronizes non-empty plain UTF-8 text and URL strings only. Images, files,
HTML, rich text, the Linux primary selection, routed networks, and VPN relay are
out of scope.

> **Pre-release:** the complete two-machine acceptance suite has passed on the
> target Mac and Omarchy systems. The first tagged release is `v0.1.0`.

## Build and test

Go 1.26.8 or a newer supported release is required. Linux also needs `wl-clipboard`; macOS builds use
the system AppKit framework and a small Objective-C bridge.

```sh
make test
make build
./bin/lanclip version
```

Builds and installers run from an exact `v*` Git tag automatically embed that
tag without the leading `v`. Untagged development builds default to
`0.1.0-dev`; override this with `make VERSION=<version> build` or
`LANCLIP_VERSION=<version>` when running an installer. On macOS, the native
AppKit adapter has an opt-in smoke test that temporarily replaces and then
restores the current plain-text clipboard:

```sh
LANCLIP_TEST_NATIVE_CLIPBOARD=1 go test ./internal/clipboard -run Native
```

Build each binary on its target OS. Cross-compiling the AppKit adapter is not
supported.

## Install

Install Go 1.26.8 or a newer supported release first. Linux additionally requires `wl-clipboard`, a
working Wayland session, and systemd user services. Clone the repository and run
the platform installer from the checkout:

```sh
git clone https://github.com/dpellerin/lanclip.git
cd lanclip
./install/linux/install.sh
# or, on the Mac:
./install/macos/install.sh
```

The installers build locally, install a per-user binary, and start either a
systemd user service or an Aqua LaunchAgent. They do not change firewall rules.
A normal uninstall preserves identity and pairing state; pass `--purge` only to
explicitly delete it:

```sh
./install/linux/uninstall.sh
./install/macos/uninstall.sh --purge
```

On Linux, make sure the graphical user service manager receives
`WAYLAND_DISPLAY` and `XDG_RUNTIME_DIR`. Never run Lanclip as root.

## Pair once

Start both services and wait a few seconds for discovery:

```sh
lanclip peers
lanclip pair
```

Lanclip presents nearby devices as a numbered menu. Choose one, then compare
the six-word code and readable fingerprint shown on both machines. On the other
machine, `lanclip peers` shows the same pending code and fingerprint. When both
match, approve from each machine:

```sh
lanclip approve
```

Approval asks for confirmation before adding trust. `lanclip reject` and
`lanclip unpair` use the same numbered device menu and confirm before making a
change. You never need to find or type a device ID. If there is only one choice,
press Return to select it. An optional device name is still accepted for quick
use, such as `lanclip pair "Studio Mac"`.

Device labels come from each machine automatically: macOS uses the Computer
Name shown in System Settings, while Linux uses its system hostname. Lanclip
refreshes the label when the service starts, so renaming a computer does not
require re-pairing. A trusted peer's label is updated only after that peer has
authenticated with its pinned identity.

A changed certificate is treated as an authentication failure and is never
silently accepted.

## Operations and privacy

```sh
lanclip status
lanclip peers
lanclip doctor
```

These commands print human-readable summaries rather than protocol JSON.
`status` shows service, clipboard, discovery, connection, endpoint, recent
activity, byte-count, and reconnect health without exposing machine IDs.
`peers` uses device names and plain-language states. `doctor` prints a compact
checklist with items that need attention clearly marked. None of these commands
show clipboard content or hashes. Logs likewise contain metadata only.

Clipboard text over 1 MiB is rejected. The wire decoder refuses frames over 7
MiB before allocation; that extra bounded headroom accommodates JSON escaping
of a valid 1 MiB text value. Empty and non-text clipboard events are ignored.
Clipboard events are only sent while a peer is connected, so reconnecting never
replays a stale clipboard.

The default listener is TCP 24872 and the advertised service is
`_lanclip._tcp.local`. Lanclip accepts and dials only addresses on a directly
connected, eligible multicast LAN interface; loopback, routed addresses, and
common VPN, tunnel, and virtual interfaces are rejected. Keep the host firewall
enabled as an additional layer and allow this port only from the local LAN
subnet. Guest Wi-Fi client isolation may block multicast and direct connections.

## Troubleshooting

- `daemon unavailable`: inspect `systemctl --user status lanclip.service` on
  Linux or `launchctl print gui/$UID/com.dpellerin.lanclip` on macOS.
- Clipboard check fails on Linux: verify `wl-paste`, `wl-copy`,
  `WAYLAND_DISPLAY`, and `XDG_RUNTIME_DIR` inside the service environment.
- No peers: confirm both devices share a non-isolated LAN and multicast DNS is
  permitted. `avahi-browse -rt _lanclip._tcp` is a useful Linux cross-check.
- Peer is discovered but `pair` times out: confirm the Linux listener with
  `ss -ltn 'sport = :24872'`, then inspect `sudo ufw status`. Omarchy blocks
  incoming traffic by default ([security documentation](https://github.com/omacom/omarchy/blob/quattro/manual/48-security.md)).
  Allow TCP 24872 only from the trusted LAN, for example `sudo ufw allow from
  192.168.1.0/24 to any port 24872 proto tcp comment 'Lanclip LAN'`;
  substitute the actual LAN subnet.
- Identity changed: inspect the other machine before unpairing and pairing it
  again. Do not work around the pinning failure.
- macOS cannot receive: approve the local-network firewall prompt and confirm
  the process is an Aqua LaunchAgent, not a LaunchDaemon.

## Verification status

Automated tests cover framing (including partial and malformed input), Unicode,
loop suppression and expiry, discovery filtering/address replacement, pairing
state and code symmetry, secure file modes, control-socket requests, and
reconnect backoff bounds. `make test`, `make vet`, and both native builds should
be run before release.

Physical two-machine acceptance is intentionally separate because it cannot be
simulated faithfully on one OS. All 18 real-machine cases in
[the implementation plan](docs/implementation-plan.md) passed on the target Mac
and Omarchy systems. Coverage included Safari/Chrome URLs, Unicode and multiline
text, repeated and simultaneous copies, the 200 ms burst, oversize and non-text
input, echo suppression, pairing and mutually authenticated TLS, unpaired and
changed-identity rejection, service restart recovery, sleep/wake, Wi-Fi cycling,
DHCP/address changes, and final socket and redacted-log inspection. No hostnames,
addresses, device identifiers, or clipboard contents are recorded here.

## Security

Lanclip handles highly sensitive data. Review [SECURITY.md](SECURITY.md) before
reporting a vulnerability, and do not place clipboard contents, pairing codes,
certificates, device identifiers, or private network details in a public issue.

## Design notes

- The lexicographically smaller stable device UUID owns the outgoing sync
  connection; this prevents duplicate full-duplex links.
- Every local ownership change gets a fresh event ID. A remote write adds one
  short-lived content-hash suppression entry, consumed exactly once, so an echo
  is stopped without permanently deduplicating intentional repeated copies.
- Pairing TLS accepts an untrusted self-signed Ed25519 certificate only long
  enough to derive and compare the transcript-bound code. Sync TLS requires the
  approved full certificate fingerprint. Pending pairings are memory-only,
  expire after five minutes, and approval is bound to the exact code and
  fingerprint shown to the user.
- Unauthenticated handshakes, pairing requests, discovered peers, clipboard
  queues, and loop-suppression state are bounded. Clipboard queues coalesce to
  the newest value so one slow peer cannot block delivery to another.
- Runtime control sockets and identity/trust files are user-only (`0600`).

See [docs/implementation-plan.md](docs/implementation-plan.md) for the full
protocol, threat model, acceptance criteria, and platform rationale.
