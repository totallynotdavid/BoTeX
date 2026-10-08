# Agent rules

The package map is in [docs/architecture.md](docs/architecture.md). The tasks
are in [.github/contributing.md](.github/contributing.md).

- Run `mise run ci` before you report a change done.
- Do not edit `.env.example`. Change the setting in code, explain it in
  `internal/envfile/envfile.go`, and run `mise run env:example`.
- Import whatsmeow only in `internal/whatsapp`. Other packages use the types in
  `internal/bot`. The linter enforces this.
- Tests do not connect to WhatsApp. Use `internal/whatsapp/fake`.
- Keep `docs/` in step with the behavior it describes, in the same change.
- Format Markdown the way `.github/contributing.md` says.
