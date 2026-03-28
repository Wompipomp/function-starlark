// +kubebuilder:object:generate=true
// +groupName=starlark.fn.crossplane.io
// +versionName=v1alpha1
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StarlarkInput provides input to the function-starlark composition function.
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:resource:categories=crossplane
type StarlarkInput struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec contains the function configuration.
	Spec StarlarkInputSpec `json:"spec"`
}

// StarlarkInputSpec contains the configuration for a Starlark function.
type StarlarkInputSpec struct {
	// Source is the inline Starlark script to execute.
	Source string `json:"source"`

	// ScriptConfigRef is a reference to a ConfigMap containing the Starlark script.
	// This field is reserved for Phase 5 and is not used in the current version.
	// +optional
	ScriptConfigRef *ScriptConfigRef `json:"scriptConfigRef,omitempty"`

	// UsageAPIVersion selects the Crossplane Usage API version.
	// "v1" = apiextensions.crossplane.io/v1beta1 (Crossplane 1.x).
	// "v2" = protection.crossplane.io/v1beta1 (Crossplane 2.x, default).
	// Set to "v1" if running Crossplane 1.x.
	// +optional
	UsageAPIVersion string `json:"usageAPIVersion,omitempty"`

	// Modules defines inline Starlark modules loadable by name.
	// Keys must end in ".star". Values are Starlark source code.
	// +optional
	Modules map[string]string `json:"modules,omitempty"`

	// ModulePaths specifies additional filesystem directories to search
	// for .star modules (after inline modules). Paths are typically
	// ConfigMap mount points like "/scripts/shared-lib".
	// +optional
	ModulePaths []string `json:"modulePaths,omitempty"`

	// DockerConfigSecret is the name of a Kubernetes Secret containing Docker
	// registry credentials. The secret should be mounted via DeploymentRuntimeConfig.
	// +optional
	DockerConfigSecret string `json:"dockerConfigSecret,omitempty"`

	// OCIDefaultRegistry overrides the operator-level default OCI registry
	// (STARLARK_OCI_DEFAULT_REGISTRY env var) for this composition.
	// Format: "registry/namespace" (e.g. "ghcr.io/my-org").
	// +optional
	OCIDefaultRegistry string `json:"ociDefaultRegistry,omitempty"`

	// OCIInsecureRegistries lists registries that should be accessed over plain
	// HTTP instead of HTTPS. Use for local or development registries only.
	// Credentials are never sent over insecure connections.
	// Format: ["localhost:5050", "registry.internal:5000"]
	// +optional
	OCIInsecureRegistries []string `json:"ociInsecureRegistries,omitempty"`

	// SequencingTTL configures the response TTL when resources are deferred
	// by creation sequencing. Parsed as Go duration (e.g. "10s", "30s").
	// Default: 10s. Only applied when at least one resource is deferred.
	// +optional
	SequencingTTL string `json:"sequencingTTL,omitempty"`
}

// ScriptConfigRef references a ConfigMap containing a Starlark script.
type ScriptConfigRef struct {
	// Name is the name of the ConfigMap.
	Name string `json:"name"`

	// Key is the key within the ConfigMap data that holds the script.
	// Defaults to "main.star".
	// +optional
	Key string `json:"key,omitempty"`
}
