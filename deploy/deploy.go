// Package deploy embeds the Kubernetes manifests kona applies to clusters.
package deploy

import _ "embed"

// VenusDRA installs the kona-venus-dra DRA driver and the kona-gpu
// DeviceClass.
//
//go:embed kona-venus-dra.yaml
var VenusDRA []byte

// VenusDRAImage is the driver image the manifest references.
const VenusDRAImage = "ghcr.io/lestex/kona-venus-dra:v0.1.0"
