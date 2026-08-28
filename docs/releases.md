# Releases

Releases use annotated semantic-version tags and publish binaries only. The
Home Assistant Supervisor continues to build the add-on locally; the project
does not publish add-on runtime images.

## Release checklist

1. Choose `X.Y.Z`, update `version` in
   `homeassistant/vm65-bridge/config.yaml` and add the section to
   `CHANGELOG.md`.
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
