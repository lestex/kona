# Releasing kona

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
stored in this repository as the `HOMEBREW_TAP_TOKEN` secret. Without it,
the rest of the release still runs and only the cask step fails. In that
case, add the secret and re-run the workflow from the same tag rather than
moving the tag.

A `-rc` or `-beta` tag becomes a GitHub pre-release, and its cask is not
uploaded (`skip_upload: auto`).

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

- **Node images.** `kona create` defaults to `ghcr.io/lestex/kona-node:<version>`,
  and nothing publishes those images yet. Until something does, a
  brew-installed kona needs the images built locally (`images/node`), or
  an image passed with `--image`.
- **Signing and notarization.** Both need an Apple Developer ID.
