#!/usr/bin/env bash
# Gate check: cross-node pod-to-pod and ClusterIP service traffic.
# Pins a client and a server pod to two different nodes, then checks ICMP
# pod->pod and HTTP via the Service (served by kube-proxy or Cilium KPR).
# usage: KUBECONFIG=... hack/gate-pod2pod.sh <client-node> <server-node>
set -euo pipefail
a=${1:?client node}
b=${2:?server node}
ns=kona-gate
k() { kubectl -n "$ns" "$@"; }

kubectl create namespace "$ns" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
trap 'kubectl delete namespace "$ns" --wait=false >/dev/null 2>&1 || true' EXIT
k run client --image=busybox:1.37 --restart=Never \
  --overrides="{\"spec\":{\"nodeName\":\"$a\",\"tolerations\":[{\"operator\":\"Exists\"}]}}" -- sleep 3600 >/dev/null
k run server --image=busybox:1.37 --restart=Never --labels=app=server \
  --overrides="{\"spec\":{\"nodeName\":\"$b\",\"tolerations\":[{\"operator\":\"Exists\"}]}}" \
  -- sh -c 'mkdir -p /w && echo kona-ok > /w/index.html && httpd -f -p 8080 -h /w' >/dev/null
k expose pod server --port 80 --target-port 8080 >/dev/null
k wait --for=condition=Ready pod/client pod/server --timeout=180s >/dev/null

k get pods -o wide
sip=$(k get pod server -o jsonpath='{.status.podIP}')
echo "## $a -> $b pod IP $sip (ICMP)"
k exec client -- ping -c3 -W2 "$sip" | tail -2
echo "## $a -> service server.$ns.svc (HTTP via ClusterIP)"
k exec client -- wget -qO- -T5 "http://server.$ns.svc.cluster.local"
