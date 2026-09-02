# Security policy

Lanclip transports clipboard text, which may include passwords, tokens, and
other sensitive material. Please report security problems privately.

## Supported versions

Lanclip is currently pre-release. Until the first tagged release, only the
latest commit on `main` receives security fixes.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting for this repository. Do not open a
public issue for a suspected vulnerability.

Include a concise description, affected revision, reproduction steps, impact,
and any suggested mitigation. Replace device names, addresses, identifiers,
certificates, pairing codes, and clipboard text with synthetic values. Never
send private keys or real clipboard contents.

You should receive an acknowledgment within seven days. No disclosure deadline
is promised, but confirmed reports will be investigated and coordinated before
public disclosure.

## Security boundaries

- Lanclip accepts connections and dials peers only on directly connected LAN
  subnets discovered on eligible multicast interfaces. Known VPN, tunnel, and
  virtual interfaces are excluded.
- Discovery does not grant trust. Clipboard exchange requires explicit pairing,
  mutually authenticated TLS 1.3, and a pinned peer certificate.
- Pending pairing requests are temporary and are not persisted. Approval is
  bound to the exact comparison code and fingerprint displayed by the CLI.
- Network-facing caches, handshake concurrency, pairing attempts, clipboard
  queues, and loop-suppression state are bounded to limit LAN denial of service.
- Host firewall rules remain a recommended additional layer.
- A user who can access the local desktop session or the account's private data
  directory is outside Lanclip's threat model. Same-user processes can access
  Lanclip's user-only local control and clipboard callback sockets by design.

Lanclip device certificates are long-lived pinned identities. If a device key
may have been exposed, unpair that device on every other device, purge both the
identity and peer store on the affected device, restart Lanclip to generate a
new identity, and pair it again. There is no automatic certificate rotation or
revocation service.
