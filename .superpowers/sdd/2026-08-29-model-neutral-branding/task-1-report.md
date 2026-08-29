# Task 1 Report: Lock the public branding and compatibility contract

## Status

Complete.

## Scope implemented

- Added an add-on metadata validator rule requiring the exact public name
  `Motorola Nursery Homeassistant Bridge`.
- Updated the validator fixture and added a regression test proving a drift to
  `Motorola VM65 Bridge` is rejected.
- Updated the Home Assistant add-on manifest name.
- Updated the README heading, introduction, and installation instruction to
  use the approved public name.
- Preserved the compatibility slug `vm65_bridge`, the `vm65-bridge` directory,
  and the existing VM65 reference-model documentation.

## Verification

Command:

```text
python tools/ci/test_check_addon.py; python tools/ci/check_addon.py
```

Result: passed. The metadata suite ran 23 tests successfully, and the real
manifest validator reported `add-on validation passed:
homeassistant\\vm65-bridge`.

`git diff --check` also completed without whitespace errors.

## Files changed

- `tools/ci/test_check_addon.py`
- `tools/ci/check_addon.py`
- `homeassistant/vm65-bridge/config.yaml`
- `README.md`

## Concerns

None for Task 1. Runtime-facing display strings and release/image workflow
changes are intentionally left for later tasks.

## Reviewer follow-up

Added an explicit validator invariant requiring the historical add-on slug
`vm65_bridge`, plus a regression test that changes both the manifest slug and
matching AppArmor profile and confirms validation fails. This closes the gap
where the prior check only enforced consistency between those two fields.
