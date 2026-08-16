# Signing

Soos is code signed through [SignPath Foundation](https://signpath.org), which
signs open source projects for free. The signature is what lets Windows name a
publisher instead of warning about an unknown one, and it is the only thing
that settles the antivirus verdicts a folder-watching, uploading agent will
otherwise always attract.

The publisher shown on a signed build is **SignPath Foundation**, on behalf of
Northwest Falls. That is how the foundation's certificates read: they vouch for
the build coming from this repository, not for a company identity.

## How a release is signed

Signing happens in CI, never on anyone's machine, because the whole value of it
is that the signed file provably came from this repository and not from a
laptop that could have been carrying anything.

1. A tag `vX.Y.Z` is pushed.
2. `.github/workflows/release.yml` builds the binaries and the installer.
3. The three Windows executables are submitted to SignPath.
4. SignPath signs them and CI attaches the signed files, the Linux builds and a
   fresh `SHA256SUMS` to the GitHub release.

The checksums are generated after signing, so they match what people download.

## One-time setup

On SignPath, after the project is approved:

- Note the **organisation id**, create a **project** (slug `soos`) and a
  **signing policy** (for example `release-signing`).
- Create a **CI user** and an **API token** for it.

On GitHub, under Settings, Secrets and variables, Actions:

- Secret `SIGNPATH_API_TOKEN` — the CI user's token.
- Variable `SIGNPATH_ORG_ID` — the organisation id.
- Variable `SIGNPATH_PROJECT` — `soos`.
- Variable `SIGNPATH_POLICY` — the policy slug.

After that, pushing a tag signs and publishes on its own.

## Verifying a download without any of this

Every release carries `SHA256SUMS`. Check a file against it, or build from the
tag and compare. The signature is a convenience for the people whose security
software would otherwise stop them; it is not the thing that makes the release
trustworthy. The source being here is.
