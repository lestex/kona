# Installation

## 1. Install kona

=== "Homebrew"

    ```sh
    brew install lestex/tap/kona
    ```

    The cask also installs Apple `container` from homebrew-core.

    !!! note "Use the full name"
        homebrew-core has an unrelated `kona` formula (the K programming
        language). A bare `brew install kona` installs that one, so always
        use `lestex/tap/kona`.

=== "Release archive"

    Download `kona_<version>_darwin_arm64.tar.gz` from the
    [releases page](https://github.com/lestex/kona/releases), check it
    against `checksums.txt`, and put `kona` on your `PATH`:

    ```sh
    tar xzf kona_*_darwin_arm64.tar.gz kona
    xattr -dr com.apple.quarantine kona   # the binary is not notarized
    sudo mv kona /usr/local/bin/
    brew install container
    ```

=== "From source"

    Requires Go (the version in `go.mod`):

    ```sh
    git clone https://github.com/lestex/kona && cd kona
    make build            # bin/kona, with the pinned versions linked in
    brew install container
    ```

    Build with `make`, not a plain `go build`: the Makefile links in the
    pinned component versions from `versions.env`, and kona needs them to
    pick its node image and kernel.

## 2. Start Apple `container`

```sh
container system start
```

The first start offers to install `container`'s default kernel. kona's nodes
don't use it, but `container build` and plain `container run` do.

## 3. Install the guest kernel

```sh
kona kernel install
```

kona's nodes boot a kona-built Linux kernel, not `container`'s default
one. It adds what Kubernetes networking needs, such as the tc/eBPF
datapath Cilium uses. `kona kernel install` downloads the exact build this
kona version pins, checks its sha256, and puts it in
`~/Library/Application Support/kona/kernels/`. Details are in
[Guest kernel](../kernel.md).

## 4. Check the Mac

```sh
kona doctor
```

```text
✓ macos            macOS 26.1
✓ arch             arm64
✓ container        container 1.7.1
✓ container-system services running
✓ container-build  ad-hoc signed (Homebrew build): …
✓ kernel           /Users/you/Library/Application Support/kona/kernels/vmlinux-6.18.35-kona
✓ dns              nodes will use 192.168.1.1 (the vmnet gateway forwarder is not relied on)
```

Every failed check prints the command that fixes it. `kona create cluster`
runs the same checks first.

You also need `kubectl` to use the clusters: `brew install kubectl`.

## Uninstalling

Clusters live in Apple `container` and outlive the kona binary, so delete
them first:

```sh
kona delete cluster --all
brew uninstall --zap lestex/tap/kona
```

`--zap` also removes `~/Library/Application Support/kona` (state and the
guest kernel) and the `~/.kube/kona-*` kubeconfigs.
