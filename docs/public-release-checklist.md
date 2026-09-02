# Public release checklist

This records the checks completed before and immediately after the public
launch.

## Privacy and security

- [x] Scan the full reachable Git history with Gitleaks.
- [x] Search tracked files and commit metadata for personal names, direct email
      addresses, real hostnames, device identifiers, private network details,
      credentials, certificates, keys, tokens, and clipboard contents.
- [x] Confirm examples use synthetic names and documentation-only addresses.
- [x] Enable GitHub private vulnerability reporting immediately when repository
      visibility becomes public.
- [x] Confirm Actions has read-only default token permissions and cannot approve
      pull requests.

## Product verification

- [x] Pass CI on Linux and macOS.
- [x] Complete and record all 18 physical acceptance cases in the design plan.
- [x] Re-run restart, sleep/wake, Wi-Fi, and DHCP/address-change recovery using
      binaries built from the exact candidate revision.
- [x] Inspect sockets and redacted logs after physical UAT.
- [x] Tag a release version only after every release gate passes.

## Repository settings

- [x] Require the Linux and macOS CI checks on `main` with branch protection.
- [x] Require pull requests and prevent force pushes after the sanitized history
      is established.
- [x] Confirm Dependabot is active for Go modules and GitHub Actions.
- [x] Confirm the description, topics, license, security policy, contribution
      guide, and issue templates render correctly.
- [x] Confirm repository visibility was still private before the final approval.

Repository visibility was changed only after separate, explicit approval.
