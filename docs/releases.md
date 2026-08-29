# Releases

Releases use annotated semantic-version tags and publish binaries. A tag also
builds and pushes the add-on image to GHCR, though the Supervisor still builds
the add-on locally until `image:` is set in the manifest — see
[Publishing the add-on image](#publishing-the-add-on-image) below.

## Release checklist

1. Choose `X.Y.Z`, update `version` in
   `homeassistant/vm65-bridge/config.yaml` and add the section to `CHANGELOG.md`
   and to `homeassistant/vm65-bridge/CHANGELOG.md` — the second one is what the
   add-on page shows on its Changelog tab, and the validator fails a release
   whose add-on changelog does not mention the version being released.
2. Update user-facing documentation and run the complete verification suite.
3. Commit the release state and merge it to `main`.
4. Move the release branch to that commit: `git push origin main:release`.
   The add-on builds from this branch, so this is the step that publishes the
   new code to Home Assistant.
5. Create an annotated tag: `git tag -a vX.Y.Z -m "Motorola Nursery Bridge X.Y.Z"`.
6. Push the tag.

Step 4 is what add-on users receive; step 5 only produces the GitHub release
with the prebuilt binaries. They are deliberately independent: the add-on no
longer depends on a tag existing.

`ARG SOURCE_REF` stays `release` and is never bumped per version. It used to be
pinned to the version tag, which could not exist until after the commit naming
it, so every add-on build in that window failed to clone. To build the default
branch instead, pass `--build-arg SOURCE_REF=main`.

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

## Publishing the add-on image

`.github/workflows/addon-image.yml` builds `homeassistant/vm65-bridge/Dockerfile`
per architecture on every version tag and pushes it to
`ghcr.io/paradox101/{arch}-vm65-bridge`, tagged with the add-on version.

Installing the add-on still builds it locally, because `config.yaml` carries no
`image:` key. That local build is the slow, fragile path: it pulls a Go
toolchain, clones this repository, reaches the module proxy and compiles two
binaries on the user's own machine, under emulation when the architectures do
not match.

To switch installations over to the published image, once tags exist in GHCR for
both architectures and both are public:

1. Add to `homeassistant/vm65-bridge/config.yaml`:

   ```yaml
   image: ghcr.io/paradox101/{arch}-vm65-bridge
   ```

2. Release as usual. The Supervisor then pulls the image tagged with the
   `version` from the manifest instead of building anything, so a release whose
   image push failed must not be published — check the workflow first.

Until then the workflow is a build check with a published artifact: it proves
the add-on image builds for both architectures, which the CI job
`add-on image / <arch>` also does on every change.
