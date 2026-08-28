# Releases

Releases use annotated semantic-version tags and publish binaries only. The
Home Assistant Supervisor continues to build the add-on locally; the project
does not publish add-on runtime images.

## Release checklist

1. Choose `X.Y.Z`, update `version` in
   `homeassistant/vm65-bridge/config.yaml` and add the section to
   `CHANGELOG.md`.
2. Set `ARG SOURCE_REF=vX.Y.Z` in the add-on Dockerfile.
3. Update user-facing documentation and run the complete verification suite.
4. Commit the release state.
5. Create an annotated tag: `git tag -a vX.Y.Z -m "Motorola Nursery Bridge X.Y.Z"`.
6. Push the tag and branch.

Push the tag immediately after the release commit lands. The add-on Dockerfile
pins `SOURCE_REF` to that tag, so between the commit and the tag every add-on
build and update fails to clone it. The build reports this explicitly rather
than leaving a bare git error. To build before tagging, pass
`--build-arg SOURCE_REF=main`.

The release workflow rejects lightweight tags, non-SemVer names, a mismatched
add-on version or a mismatched Dockerfile source reference. It tests the code,
builds `vm65-bridge` and `vm65-setup` for Linux amd64 and arm64, generates
SHA-256 checksums and creates the GitHub release.

Run the contract locally before tagging:

```sh
python tools/ci/check_release.py vX.Y.Z
```

Never move or reuse a published version tag. Prepare a new patch version for
any correction.
