# Loading images: kona-registry

```
kona load image IMAGE... [--name NAME]
```

pushes images from the Mac's `container` image store into the cluster's
registry. Pods then pull them **by their usual names**: after
`kona load image myapp:dev`, a pod with `image: myapp:dev` runs it, on any
node, with any `imagePullPolicy`.

## How it works

- **The registry VM.** The first `load image` starts `<cluster>-registry`
  (`kona-registry` for the default cluster): distribution 3.1.2, pinned by
  digest in `versions.env`, at the cluster's reserved static address `.200`,
  port 5000, plain HTTP. Images are stored on a 20G block volume
  `<cluster>-registry-data`, so they survive restarts. `stop cluster` stops
  it last, `start cluster` starts it first, `delete cluster` removes it and
  its volume. `kona verify cluster` has a `registry` check.
- **Push.** Each image is tagged `<registry ip>:5000/<host>/<repo>:<tag>`
  (for example `192.168.70.200:5000/docker.io/library/myapp:dev`), pushed
  with `container image push --scheme http --platform linux/arm64`, and the
  temporary tag is removed. Only the arm64 variant is pushed: a pulled
  multi-platform image holds just that one, and pushing the whole index
  fails on missing content.
- **Mirrors.** For every registry host that loaded images came from
  (`docker.io`, `ghcr.io`, …), each node gets a containerd
  `<host>/hosts.toml` that tries kona-registry first and then the real
  registry:

  ```toml
  server = "https://registry-1.docker.io"

  [host."http://192.168.70.200:5000/v2/docker.io"]
    capabilities = ["pull", "resolve"]
    override_path = true
  ```

  `override_path` makes containerd ask for
  `/v2/docker.io/library/myapp/manifests/dev`, which is where the push put
  it. Anything not in kona-registry falls through to the upstream registry
  unchanged. containerd reads these files on every pull, so no restart is
  needed. The directory is containerd's `config_path`:
  `/var/lib/rancher/k3s/agent/etc/containerd/certs.d` on k3s (k3s only
  rewrites it from `registries.yaml`, which kona doesn't use) and
  `/etc/containerd/certs.d` on kubeadm. Nodes added later get the same
  files.

## Limits

- Digest references (`name@sha256:…`) aren't supported; use a tag.
- A loaded image shadows the upstream image of the same name and tag on
  every node, by design. Load a new build under the same tag to replace it.
- There is no garbage collection: images stay on the registry volume until
  the cluster is deleted.
