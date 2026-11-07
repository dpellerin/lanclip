# Transport verification and fingerprint bounds

## TLS trust model

Lanclip uses self-signed Ed25519 certificates as device identities, not Web PKI
hostnames. `transport.ClientConfig` replaces Go's CA/hostname verification with
a mandatory `VerifyConnection` callback; it does not omit peer verification.
`ServerConfig` requires a client certificate and runs the same self-signature,
algorithm, validity, and certificate-count checks. Its `InsecureSkipVerify`
field does not control Go's server-side client-certificate policy.

For `lanclip-sync/1`, both sides require an exact full certificate SHA-256
fingerprint in the explicitly approved pairing store. A discovery prefix is
only an additional consistency check, never a substitute for a trusted pin.
The TLS stack also checks possession of the matching private key.

For `lanclip-pair/1`, a valid but untrusted self-signed identity is intentionally
accepted for the comparison-code exchange. This connection cannot sync the
clipboard or add trust. The daemon binds the comparison code to both identities
and fresh nonces; approval must supply the exact displayed fingerprint, code,
and local pending-session token on both devices. Making pairing require prior
trust, or switching these certificates to hostname/CA verification, would break
the supported enrollment model rather than fix a missing certificate check.

`internal/transport/handshake_test.go` exercises actual loopback TLS handshakes
and an application-byte roundtrip for trusted sync, unknown client/server,
changed discovery identity, too-short discovery prefix, and untrusted pairing.
It separately verifies rejection of expired, not-yet-valid, incorrectly signed,
missing, and multiple certificates. It does not touch a native clipboard.

The two `go/disabled-certificate-check` findings are false positives for the
current verified pinning model, not permission to remove or weaken its callback.
No broad CodeQL exclusion or rule suppression is necessary.

## Fingerprint display bound

`groupedFingerprint` now rejects values longer than a full SHA-256 fingerprint
before arithmetic or allocation. It does not truncate an identity shown for
human comparison. Supported short discovery prefixes and full fingerprints
retain their original grouping.

The original `(len(value)+3)/4` could overflow `int` in isolation. A realistic
remote overflow was not demonstrated: network pairing derives the fingerprint
from a parsed certificate; discovery validates fixed-size prefixes; same-user
control-socket access is outside the documented security boundary. Nevertheless,
the display helper had no local bound and an oversized response caused
proportional allocation/output. Tests reproduce that behavior with 65-byte and
1-MiB inputs without unsafe fabricated strings or enormous allocations. The
new guard removes the arithmetic precondition and bounds the display work.

An opt-in real-binary test uses an isolated private Unix control socket and
synthetic pending-peer metadata:

```sh
make build
LANCLIP_TEST_BINARY="$PWD/bin/lanclip" go test ./cmd/lanclip \
  -run TestBuiltCLIRejectsOversizedFingerprintFromControlSocket -v
```

This starts neither a production daemon nor a clipboard adapter. The normal
race suite remains clipboard-safe; native clipboard tests remain opt-in.

## Security baseline

The existing source/binary `govulncheck` gates also detected new standard-library
advisories on Go 1.26.8 and module advisories on `x/net` v0.58.0. Go 1.26.10 and
`x/net` v0.62.0 (with its required `x/sys` v0.49.0) pass both unchanged scan gates.
CI, mise, toolchain selection, and development documentation use that Go patch.

These are native locally installed binaries, not an auto-deployed web service.
A merge does not update an already installed executable. Build on each target
OS using the patched toolchain and follow the existing installation process to
roll it out; do not replace a user's binary or restart their clipboard service
as a side effect of repository verification.

## v0.1.1 rollout verification

GitHub Actions passed the macOS and Linux CI jobs for the security update at
`f5efdac`. Both machines were then updated to that commit.
[CI run](https://github.com/dpellerin/lanclip/actions/runs/42389656644)

- The Apple silicon Mac build used Go 1.27.3. The race suite, vet, and source
  and binary vulnerability scans passed. The LaunchAgent was running and
  `lanclip doctor` passed all nine checks.
- The Omarchy build used Go 1.26.10. `make test` and `make vet` passed. The
  systemd user service was enabled and running, and `lanclip doctor` passed all
  eleven checks.
- Text copied on Omarchy was successfully pasted on the Mac using the existing
  pairing.
