---
title: "Releasing kona"
linkTitle: "Releasing"
weight: 2
aliases: ["/release/"]
---
The tag is the version: nothing is committed anywhere to forget to bump.

```sh
make check
make changelog TAG=v0.1.0       # files the pending commits under that version
git checkout -b release-v0.1.0
git commit -am "changelog for v0.1.0"
git push -u origin release-v0.1.0
gh pr create --base main --fill   # and merge it

# Tag the commit that landed on main, not the local one: otherwise the
# archive and `kona version` name a commit that is not on the branch.
git checkout main && git pull
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

The tag triggers `.github/workflows/release.yml`:

1. **check**: `make check` (gofmt, vet, tests) on macos-26 against the
   tagged commit, since a tag can point at a commit CI never saw.
2. **release**: GoReleaser builds `kona` for darwin/arm64, links the
   pinned component matrix from `versions.env` into it (`make -s
   release-env`), and creates the GitHub release with a tarball and
   checksums. The notes are the newest section from `cliff.toml`, the same
   config that writes `CHANGELOG.md`, so the release page and the file
   always match.
3. **tap-style**: runs `brew style --fix` on `Casks/kona.rb` in the tap
   and commits the result. GoReleaser can't write a cask that passes
   `brew style` as generated. The job fails if anything is left that it
   can't fix automatically.

## The Homebrew cask

GoReleaser writes `Casks/kona.rb` to
[lestex/homebrew-tap](https://github.com/lestex/homebrew-tap):

```sh
brew install lestex/tap/kona
```

Always use the fully qualified name: homebrew-core has an unrelated `kona`
formula (the K language), and a bare `brew install kona` installs that.

The cask:

- Depends on the `container` formula.
- Requires macOS 26 (Tahoe) and arm64.
- Clears the quarantine flag on install, because the binary is not
  notarized.
- Has caveats that say to delete clusters before uninstalling.
- Removes `~/Library/Application Support/kona` and `~/.kube/kona-*` on
  `--zap`.

The tap is a separate repository, which the workflow's `GITHUB_TOKEN`
can't write to. The workflow needs a PAT with `contents:write` on the tap,
stored in this repository as the `HOMEBREW_TAP_TOKEN` secret: a
fine-grained token with **Contents: Read and write** on
`lestex/homebrew-tap`. Without it, or with a token that can only read the
tap, the GitHub release is still published and only the cask push fails
with `403 Resource not accessible by personal access token`. In that case,
fix the secret and re-run the workflow from the same tag rather than moving
the tag. `replace_existing_artifacts` lets the re-run replace the archive
and checksums it uploaded the first time.

Tags made before `replace_existing_artifacts` was added (v0.1.0) carry
the old config, so a re-run of them fails with `422 already_exists` on the
first upload. For those, delete the release's assets
(`gh release delete-asset`) before re-running.

A `-rc` or `-beta` tag becomes a GitHub pre-release, and its cask is not
uploaded (`skip_upload: auto`).

## Node images

`kona create` pulls `ghcr.io/lestex/kona-node:<version>` by default.
`.github/workflows/node-images.yml` builds both images (k3s and kubeadm)
on GitHub's arm64 Linux runners with docker buildx. It uses the build args
from `images/node/Makefile`, the same ones a local `container build` uses.

- On a PR that touches `images/node/` or `versions.env`, it only builds.
- On main, and on a manual run, it also pushes.

The tags are the Kubernetes versions, so a change to the image files
republishes the same tag. Nodes that already have the image keep the old
one until it is removed from `container`.

The `kona-node` package must be **public** on GHCR, because kona pulls it
without credentials. GHCR makes a new package private, so after the first
push, change its visibility once in the package settings.

## Rehearsing locally

```sh
make snapshot                                   # the release build, into dist/
goreleaser check                                # validate .goreleaser.yaml
git cliff --latest --strip header               # exactly what the release page gets

# The cask this tag would publish, written to dist/homebrew/Casks/ and
# pushed nowhere:
set -a; eval "$(make -s release-env)"; set +a
HOMEBREW_TAP_TOKEN=x goreleaser release --snapshot --clean --skip=publish
```

GoReleaser reads the matrix from `KONA_MATRIX` and `KONA_SUMS`, which the
Makefile exports. Run it through `make`, or export those variables first.
A build without them fails rather than producing a `kona` with no pinned
versions.

## Not covered yet

- **Signing and notarization.** Both need an Apple Developer ID.
