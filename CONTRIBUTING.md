# Contributing to Lanclip

Thanks for helping improve Lanclip. The project is pre-release, and changes to
pairing, transport, discovery, clipboard handling, logging, or file permissions
should be treated as security-sensitive.

## Development setup

Use Go 1.24 or newer. Linux development additionally requires `wl-clipboard`.
Build native binaries on their target operating system.

Before submitting a pull request, run:

```sh
make fmt
go test -race ./...
make vet
make build
bash -n install/linux/*.sh install/macos/*.sh
```

On macOS, also run `plutil -lint` on the LaunchAgent plist. Native clipboard
tests are opt-in because they temporarily replace the clipboard:

```sh
LANCLIP_TEST_NATIVE_CLIPBOARD=1 go test ./internal/clipboard -run Native
```

Keep changes focused, add tests for behavior changes, and update the README or
design plan when user-visible behavior changes. Use synthetic names, addresses,
identifiers, and clipboard contents in tests and documentation.

Report vulnerabilities according to [SECURITY.md](SECURITY.md), not through a
public issue or pull request.
