#!/usr/bin/env bash
# Phase 3 gate: DRA claims on a single-Venus-device GPU node.
#  1. a claim schedules only onto the GPU node
#  2. vulkaninfo in the pod shows Venus
#  3. a second claim on the single-device node stays Pending
#  4. deleting the claim unprepares cleanly: CDI spec removed, kubelet quiet
# usage: KUBECONFIG=... hack/gate-dra.sh [cluster]
set -euo pipefail
cluster=${1:-kona}
here=$(cd "$(dirname "$0")" && pwd)
kona=${KONA:-$here/../bin/kona}
ns="kona-gate-dra"
k() { kubectl -n "$ns" "$@"; }
gpu_ssh() { "$kona" ssh --name "$cluster" "$gpu" -- "$@"; }

kubectl delete namespace "$ns" --ignore-not-found --wait=true >/dev/null
kubectl create namespace "$ns" >/dev/null
trap 'kubectl delete namespace "$ns" --wait=false >/dev/null 2>&1 || true' EXIT
gpu=$(kubectl get nodes -l kona.dev/gpu=venus -o jsonpath='{.items[0].metadata.name}')
echo "## GPU node: $gpu; other nodes: $(kubectl get nodes -l 'kona.dev/gpu!=venus' -o name | tr '\n' ' ')"

pod() { # pod <name>
  cat <<EOF
apiVersion: v1
kind: Pod
metadata: {name: $1}
spec:
  restartPolicy: Never
  tolerations: [{key: kona.dev/gpu, operator: Exists, effect: NoSchedule}]
  resourceClaims: [{name: gpu, resourceClaimTemplateName: kona-gpu}]
  containers:
    - name: vk
      image: ghcr.io/lestex/kona-vulkan:25.3.6-102.fc44
      imagePullPolicy: IfNotPresent
      command: ["sh", "-c", "vulkaninfo --summary | grep -E 'deviceName|driverName|apiVersion' | head -3; sleep 3600"]
      resources: {claims: [{name: gpu}]}
EOF
}
k apply -f - >/dev/null <<EOF
apiVersion: resource.k8s.io/v1
kind: ResourceClaimTemplate
metadata: {name: kona-gpu}
spec:
  spec:
    devices:
      requests: [{name: gpu, exactly: {deviceClassName: kona-gpu}}]
EOF

echo "## 1+2. first claim: placement and vulkaninfo"
pod first | k apply -f - >/dev/null
k wait --for=condition=Ready pod/first --timeout=180s >/dev/null
k get pod first -o wide | awk '{print $1, $3, $7}'
k get resourceclaims -o wide
sleep 2
k logs first

echo "## CDI spec on $gpu while prepared"
gpu_ssh "sudo ls -la /var/run/cdi/"

echo "## 3. second claim on the single-device node"
pod second | k apply -f - >/dev/null
sleep 20
k get pod second -o wide | awk '{print $1, $3, $7}'
k get resourceclaims
k get events --field-selector involvedObject.name=second -o custom-columns=REASON:.reason,MESSAGE:.message | tail -2

echo "## 4. delete the first pod: claim is unprepared, then the second is allocated"
since=$(date -u +%Y-%m-%dT%H:%M:%SZ)
k delete pod first --wait=true >/dev/null
k wait --for=condition=Ready pod/second --timeout=180s >/dev/null
echo "second is now: $(k get pod second -o jsonpath='{.status.phase}') on $(k get pod second -o jsonpath='{.spec.nodeName}')"
k delete pod second --wait=true >/dev/null
sleep 5
echo "## CDI specs on $gpu after both claims are gone (expect none)"
gpu_ssh "sudo ls -la /var/run/cdi/"
echo "## driver log (prepare/unprepare)"
kubectl -n kona-system logs ds/kona-venus-dra --since-time="$since" | grep -E '"(prepared|unprepared)"' | sed -E 's/^.*\] //' | cut -c1-160
echo "## kubelet (k3s-agent) DRA errors since the delete (expect none)"
# One quoted argument: ssh joins arguments and the remote shell re-splits.
ts=$(date -j -u -f %Y-%m-%dT%H:%M:%SZ "$since" '+%Y-%m-%d %H:%M:%S')
log=$(gpu_ssh "sudo journalctl -u kona-agent --since '$ts UTC' --no-pager")
echo "kubelet log lines since the delete: $(echo "$log" | wc -l | tr -d ' ')"
echo "$log" | grep -iE '(E[0-9]{4}|level=error|error).*(dra|resourceclaim|cdi|venus)' || echo "(no DRA/CDI errors)"
