# Quick start

This walks through one cluster from creation to deletion. It assumes
[Installation](installation.md) is done and `kona doctor` passes.

## Create a cluster

```sh
kona create cluster
```

This creates the default cluster, named `kona`: k3s, one control plane and
two workers, each a VM with 2 vCPUs and 2G of memory. kona:

1. checks the host (the same checks as `kona doctor`),
2. creates a `container` network with a free `/24` (from `192.168.70.0`
   upwards),
3. starts one VM per node at fixed addresses (`.10` for the first control
   plane, `.20` upwards for workers),
4. waits until every node is Ready and `kona verify cluster` passes,
5. writes the kubeconfig to `~/.kube/kona-kona`.

```text
✓ Cluster "kona" is ready

  kubectl --kubeconfig /Users/you/.kube/kona-kona get nodes
```

If anything fails, kona rolls the whole cluster back. Use `--retain` to
keep it for debugging.

## Use it

```sh
export KUBECONFIG=~/.kube/kona-kona
kubectl get nodes -o wide
kubectl create deployment hello --image=nginx --replicas=2
kubectl expose deployment hello --type=NodePort --port 80
curl http://192.168.70.20:$(kubectl get svc hello -o jsonpath='{.spec.ports[0].nodePort}')
```

The API server and every node are reachable from the Mac at their own
addresses, with no port forwarding, so NodePort services work on any node's
IP. `kubectl get nodes -o wide` shows the addresses. Pod IPs are internal to
the cluster; use a Service or `kubectl port-forward` to reach a pod.

To use the cluster with your usual kubeconfig instead, merge it in as the
context `kona-kona`:

```sh
kona get kubeconfig --merge
kubectl config use-context kona-kona
```

## Run your own image

Build or pull an image with `container`, then load it into the cluster:

```sh
container build -t myapp:dev .
kona load image myapp:dev
kubectl create deployment myapp --image=myapp:dev
```

Pods pull `myapp:dev` from the cluster's own registry. See
[Loading images](../registry.md).

## Look around

```sh
kona get clusters          # every cluster, with phase and health
kona get nodes             # VM state, readiness and clock skew per node
kona ssh kona-worker-1     # a shell on a node
kona verify cluster        # read-only health checks
```

## Stop it, start it again

Nodes are VMs with fixed memory, so stop the cluster when you're not using
it:

```sh
kona stop cluster
kona start cluster
```

`stop` keeps VMs, disks and state. After a Mac reboot the VMs are stopped,
and `kona start cluster` brings them back. It returns once the cluster is
healthy again. See [Stop, start, verify](../lifecycle.md).

## Delete it

```sh
kona delete cluster
```

This removes the VMs, their volumes, the network, the kubeconfig and kona's
state for the cluster. It is safe to run again if it was interrupted.

## Next

- [Clusters](../guide/clusters.md): sizes, HA control planes, Cilium,
  kubeadm, several clusters side by side.
- [Troubleshooting](../guide/troubleshooting.md): what to do when
  something doesn't come up.
