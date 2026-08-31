# Public release checklist

Keep the repository private until every required item is complete.

## Privacy and security

- [ ] Scan the full reachable Git history with Gitleaks.
- [ ] Search tracked files and commit metadata for personal names, direct email
      addresses, real hostnames, device identifiers, private network details,
      credentials, certificates, keys, tokens, and clipboard contents.
- [ ] Confirm examples use synthetic names and documentation-only addresses.
- [ ] Enable GitHub private vulnerability reporting immediately when repository
      visibility becomes public.
- [ ] Confirm Actions has read-only default token permissions and cannot approve
      pull requests.

## Product verification

- [ ] Pass CI on Linux and macOS.
- [ ] Complete and record all 18 physical acceptance cases in the design plan.
- [ ] Re-run restart, sleep/wake, Wi-Fi, and DHCP/address-change recovery using
      binaries built from the exact candidate revision.
- [ ] Inspect sockets and redacted logs after physical UAT.
- [ ] Tag a release version only after every release gate passes.

## Repository settings

- [ ] Require the Linux and macOS CI checks on `main` with branch protection.
- [ ] Require pull requests and prevent force pushes after the sanitized history
      is established.
- [ ] Confirm Dependabot is active for Go modules and GitHub Actions.
- [ ] Confirm the description, topics, license, security policy, contribution
      guide, and issue templates render correctly.
- [ ] Confirm repository visibility is still private before the final approval.

Changing repository visibility is a separate, explicit action and is not part
of this checklist's implementation work.
