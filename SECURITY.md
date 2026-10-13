# Security policy

## Reporting a vulnerability

Please **don't** open public issues for security problems. Report them privately through
[GitHub security advisories](https://github.com/haouarihk/ipxemultiboot/security/advisories/new).
You should get a response within a week.

## Scope and threat model

iPXE Multiboot is meant for **trusted local networks**:

- Boot endpoints (`/boot.ipxe`, `/menu.ipxe`, `/ipxe/*`, `/iso/*`, `/isos/*`) and TFTP are
  intentionally unauthenticated, because PXE firmware can't log in. Anyone on the network can
  read the ISO library.
- DHCP and PXE have no authentication by design. A malicious device on the same network can always
  impersonate a boot server.
- The web interface and `/api` need a login. Bypasses, CSRF, path traversal (for example uploads
  or ISO file access outside the configured folder), and privilege issues in the packaging
  (systemd unit, Docker image, Proxmox script) are all in scope.
