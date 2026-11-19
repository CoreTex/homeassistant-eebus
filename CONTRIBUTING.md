# Contributing

Thanks for helping to improve EEBUS for Home Assistant! Every report from a
real device helps, because EEBUS devices differ a lot in what they offer.

## Asking for help and reporting problems

Before opening an issue, check the
[troubleshooting section of the README](README.md#troubleshooting) and the
existing issues.

When you open a bug report, the form asks for what we need. Most important:

- **Versions** of the EEBUS Bridge add-on and of the integration. They are
  updated separately – after updating the integration in HACS, restart Home
  Assistant.
- **How Home Assistant is installed** (Home Assistant OS / Supervised with
  add-ons, or Container / Core with the bridge running in Docker).
- **Your devices** (brand and model of inverter, wallbox, heat pump, ...).
- **Diagnostics:** *Settings → Devices & services → EEBUS → ⋮ → Download
  diagnostics*. It contains the raw EEBUS data of every device, which is
  usually all we need to support a new device. The API token is removed
  automatically; you may want to remove serial numbers before uploading.
- For connection problems: the add-on log with `log_level: debug`.

## Suggesting features

Open a feature request and describe the situation you want to solve – for
example "dim my heat pump when the electricity price is high" – rather than a
technical solution. Mention the devices you use.

## Contributing code

The repository contains two parts:

| Part | Path | Language |
| --- | --- | --- |
| EEBUS Bridge add-on | `eebus_bridge/` | Go, based on [`enbility/eebus-go`](https://github.com/enbility/eebus-go) |
| Integration | `custom_components/eebus/` | Python (Home Assistant) |

### Branches and releases

- `dev` – development; pre-releases `vX.Y.Z-dev` are built from it.
- `main` – stable releases.

Please open pull requests against **`dev`**. Add-on and integration share one
version number; changes are listed in [CHANGELOG.md](CHANGELOG.md).

### Checks

Run the same checks as the CI before opening a pull request:

```bash
# add-on (Go)
cd eebus_bridge/src
gofmt -l . && go vet ./... && go test -race ./...

# integration (Python)
python -m ruff check --config ruff.toml custom_components tests
pip install --prefer-binary -r tests/requirements.txt
python -m pytest -q -o asyncio_mode=auto tests
```

### Guidelines

- Keep pull requests focused on one change and describe the user-visible
  effect.
- Add or update tests for new behaviour. Test data recorded from real devices
  (via *Download diagnostics*) is very welcome – please anonymise serial
  numbers and SKIs.
- New entities need translations in `strings.json`,
  `translations/en.json` and `translations/de.json`.
- Safety-relevant logic (grid operator limits, failsafe, limit expiry) belongs
  in the add-on, so it keeps working while Home Assistant restarts.
- Update the documentation and the changelog for user-visible changes.

By contributing you agree that your contribution is licensed under the
project's [Apache-2.0 license](LICENSE), and you follow our
[code of conduct](CODE_OF_CONDUCT.md).
