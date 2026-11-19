# Security policy

The EEBUS Bridge controls energy devices (wallboxes, heat pumps, inverters)
and handles grid operator limits, so security problems matter.

## Reporting a vulnerability

Please **do not open a public issue** for security problems. Report them
privately via
[GitHub private vulnerability reporting](https://github.com/CoreTex/homeassistant-eebus/security/advisories/new).

Include what is affected (add-on or integration, version), how it can be
exploited and, if possible, how to reproduce it. You will get a first answer
within a week. Fixed vulnerabilities are published as a security advisory and
mentioned in the changelog.

## Supported versions

Security fixes are made for the latest release (stable and `-dev`).

## Good practice for users

- The bridge's local API is reachable on your network (default port `7050`).
  Set an **API token** in the add-on options if other people or devices share
  your network.
- Trust only EEBUS devices you own (*Trusted devices*), and switch off
  *Accept every pairing request* after pairing.
- Do not post diagnostics or logs with serial numbers, SKIs or tokens
  publicly.
