/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"

	"sigs.k8s.io/karpenter/pkg/apis"
)

// Well known labels and resources
const (
	ArchitectureAmd64    = "amd64"
	ArchitectureArm64    = "arm64"
	CapacityTypeSpot     = "spot"
	CapacityTypeOnDemand = "on-demand"
)

// Karpenter specific domains and labels
const (
	NodePoolLabelKey        = apis.Group + "/nodepool"
	NodeInitializedLabelKey = apis.Group + "/initialized"
	NodeRegisteredLabelKey  = apis.Group + "/registered"
	CapacityTypeLabelKey    = apis.Group + "/capacity-type"
)

// Karpenter specific annotations
const (
	DoNotDisruptAnnotationKey                  = apis.Group + "/do-not-disrupt"
	ProviderCompatibilityAnnotationKey         = apis.CompatibilityGroup + "/provider"
	NodePoolHashAnnotationKey                  = apis.Group + "/nodepool-hash"
	NodePoolHashVersionAnnotationKey           = apis.Group + "/nodepool-hash-version"
	NodeClaimTerminationTimestampAnnotationKey = apis.Group + "/nodeclaim-termination-timestamp"
)

// Karpenter specific finalizers
const (
	TerminationFinalizer = apis.Group + "/termination"
)

var (
	// RestrictedLabelDomains are either rejected by the kubelet node restriction admission or reserved by karpenter
	RestrictedLabelDomains = sets.New(
		"kubernetes.io",
		"k8s.io",
		apis.Group,
	)

	// KubeletSelfSetAllowed are labels that the node restriction admission allows kubelet to set on itself.
	KubeletSelfSetSuffixesAllowed = sets.New(
		v1.LabelNamespaceSuffixNode,
	)

	// LabelDomainExceptions are sub-domains of the RestrictedLabelDomains but allowed because
	// they are rejected by node restriction kubelet self setting but an admin might choose to use these labels for workload isolation purposes.
	// Karpenter will sync these labels into Nodes centrally.
	// Bootstrap userdata implementers should filter this out from what's passed to kubelet. If they don't and the node restriction admission is enabled, the node will be rejected.
	// https://github.com/kubernetes/enhancements/blob/1226bed199ae346f935dbb8600393c9f116e6b80/keps/sig-auth/279-limit-node-access/README.md#proposal
	LabelDomainExceptions = sets.New(
		// "node-role.kubernetes.io is a widely adopted convention purely informational that can be used by consumers for taints, tolerations, or other configurations.
		// https://github.com/kubernetes/enhancements/blob/1226bed199ae346f935dbb8600393c9f116e6b80/keps/sig-architecture/1143-node-role-labels/README.md#goals
		// If your bootstrap userdata implementation try to set the labels below via kubelet, the node restriction admission will reject them.
		"node-role.kubernetes.io",
		"kops.k8s.io",
		v1.LabelNamespaceNodeRestriction,
	)

	// WellKnownLabels are labels that belong to the RestrictedLabelDomains but allowed.
	// Karpenter is aware of these labels, and they can be used to further narrow down
	// the range of the corresponding values by either nodepool or pods.
	WellKnownLabels = sets.New(
		NodePoolLabelKey,
		v1.LabelTopologyZone,
		v1.LabelTopologyRegion,
		v1.LabelInstanceTypeStable,
		v1.LabelArchStable,
		v1.LabelOSStable,
		CapacityTypeLabelKey,
		v1.LabelWindowsBuild,
	)

	// RestrictedLabels are labels that should not be used
	// because they may interfere with the internal provisioning logic.
	RestrictedLabels = sets.New(
		v1.LabelHostname,
	)

	// NormalizedLabels translate aliased concepts into the controller's
	// WellKnownLabels. Pod requirements are translated for compatibility.
	NormalizedLabels = map[string]string{
		v1.LabelFailureDomainBetaZone:   v1.LabelTopologyZone,
		"beta.kubernetes.io/arch":       v1.LabelArchStable,
		"beta.kubernetes.io/os":         v1.LabelOSStable,
		v1.LabelInstanceType:            v1.LabelInstanceTypeStable,
		v1.LabelFailureDomainBetaRegion: v1.LabelTopologyRegion,
	}
)

// IsRestrictedLabel returns an error if the label is restricted.
func IsRestrictedLabel(key string) error {
	if WellKnownLabels.Has(key) {
		return nil
	}
	if IsRestrictedNodeLabel(key) {
		return fmt.Errorf("label %s is restricted; specify a well known label: %v, or a custom label that does not use a restricted domain: %v", key, sets.List(WellKnownLabels), sets.List(RestrictedLabelDomains))
	}
	return nil
}

// IsRestrictedNodeLabel returns true if a node label should not be injected by Karpenter.
// They are either known labels that will be injected by cloud providers,
// or label domain managed by other software (e.g., kops.k8s.io managed by kOps).
func IsRestrictedNodeLabel(key string) bool {
	if WellKnownLabels.Has(key) {
		return true
	}
	labelDomain := GetLabelDomain(key)
	for exceptionLabelDomain := range LabelDomainExceptions {
		if strings.HasSuffix(labelDomain, exceptionLabelDomain) {
			return false
		}
	}

	for kubeletSelfSetSuffixAllowed := range KubeletSelfSetSuffixesAllowed {
		if strings.HasSuffix(labelDomain, kubeletSelfSetSuffixAllowed) {
			return false
		}
	}

	// TODO(enxebre): consider dropping this check and just allow any label.
	// nodeClaim Labels are the source for core karpenter to centrally sync over Node Labels.
	// Some bootstrap userdata provider implementations also consume this labels and pass them through kubelet self setting.
	// That results in a coupling between a centralized and a kubelet self setting approach.
	// We should decouple this an only filter out what's passed to kubelet.
	for restrictedLabelDomain := range RestrictedLabelDomains {
		if strings.HasSuffix(labelDomain, restrictedLabelDomain) {
			return true
		}
	}
	return RestrictedLabels.Has(key)
}

func GetLabelDomain(key string) string {
	if parts := strings.SplitN(key, "/", 2); len(parts) == 2 {
		return parts[0]
	}
	return ""
}

func NodeClassLabelKey(gk schema.GroupKind) string {
	return fmt.Sprintf("%s/%s", gk.Group, strings.ToLower(gk.Kind))
}
