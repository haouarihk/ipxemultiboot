# Contributing to iPXE Multiboot

Thanks for helping! Here are some good ways to contribute:

- **Test ISOs.** Boot your favourite distro and report what happens. A "works on X" report is
  valuable too; open a discussion or add it to the README table in a PR.
- **Write distro recipes.** See [docs/distro-recipes.md](docs/distro-recipes.md).
- **Translate.** Copy `web/src/locales/en.ts`, translate the values and register the language in
  `web/src/lib/i18n.tsx` and `internal/app/app.go` (`Languages`).
- **Fix bugs and build features.** Check the issues labelled `good first issue`.

## Development setup

You need Go 1.27+, Node 24+, pnpm, and optionally QEMU, OVMF and dnsmasq for boot tests.

```sh
make web build test
```

See [docs/development.md](docs/development.md) for the architecture and the no-root QEMU harness.

## Pull requests

- Keep changes focused; one topic per PR.
- Run `make lint test` before pushing.
- For boot-related changes, say which firmware (BIOS/UEFI) and ISOs you tested.
- New UI strings need a key in every locale file. Using the English text in the other languages is
  fine; translators will pick it up.
- Commits: short imperative subject line (`Add Fedora Silverblue recipe`).

By contributing, you agree that your contributions are licensed under the GPL-3.0.

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md). Be kind.
