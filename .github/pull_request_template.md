## What does this change?

<!-- The user-visible effect, e.g. "Heat pumps of brand X now show their energy". -->

## Why?

<!-- Link the issue: Fixes #123 -->

## How was it tested?

<!-- Real device (brand/model), tests added, screenshots for UI changes. -->

## Checklist

- [ ] Opened against the `dev` branch
- [ ] Go: `gofmt -l .`, `go vet ./...`, `go test -race ./...` pass (if `eebus_bridge/` changed)
- [ ] Python: `ruff` and `pytest` pass (if `custom_components/` changed)
- [ ] New entities have translations (`strings.json`, `en.json`, `de.json`)
- [ ] Documentation and `CHANGELOG.md` updated for user-visible changes
- [ ] Test data from real devices is anonymised (serial numbers, SKIs)
