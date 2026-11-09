# Building and testing

## Layout

| Path | |
|---|---|
| `cmd/kona` | the binary's entry point |
| `internal/cli` | the command tree |
| `internal/cluster` | create, delete, start, stop, verify, images, support bundle |
| `internal/distro/{k3s,kubeadm}` | what runs inside a node, per distribution |
| `internal/cni` | the flannel and Cilium installs |
| `internal/runtime/container` | the Apple `container` CLI client |
| `internal/netplan`, `internal/state`, `internal/kubeconfig` | addresses, state on disk, kubeconfigs |
| `internal/doctor`, `internal/kernel` | host checks, guest kernel install |
| `images/node` | the node images |
| `kernel` | the guest kernel build |
| `versions.env` | every pinned version and checksum |
| `docs` | this site |

## Build and test

```sh
make build        # bin/kona, with versions.env linked in
make check        # gofmt, go vet, go test
```

Unit tests use fake runners and need neither `container` nor a VM. Changes
that touch real clusters are checked by hand against the gates in
[Gates](gates.md); `kona verify cluster` is the quick version.

## Versions

`versions.env` pins every component: Kubernetes, k3s, containerd, Cilium,
the node image base, the kernel, and the sha256 of each download. The
Makefile links it into the binary, so a kona build always knows which
images and kernel it belongs to. Bump a version there and in nowhere else.

## Node images

```sh
make -C images/node build            # k3s
make -C images/node build-kubeadm    # kubeadm
```

Both are built with `container build`, with no Docker. CI builds the same
Containerfiles with docker buildx and publishes them to
`ghcr.io/lestex/kona-node` from main (see [Releasing](release.md)).

If a build fails with `strconv.ParseInt: parsing ""`, a base image cached by
an older `container` is the cause; see [Failure modes](failure-modes.md).

## Guest kernel

```sh
make -C kernel          # build in a container VM
make -C kernel check    # compare with the pin in versions.env
kona kernel install --from kernel/out/vmlinux-6.18.35-kona
```

The build is reproducible, so a local build matches the pinned one exactly.
See [Guest kernel](kernel.md).

## Docs

```sh
make docs-serve         # http://127.0.0.1:8000 with live reload
make docs-build         # the strict build CI runs
```

The site is MkDocs Material, pinned in `docs-requirements.txt`. CI builds
it on every docs change and publishes it to GitHub Pages from `v*` tags,
so the site always describes the latest release.
