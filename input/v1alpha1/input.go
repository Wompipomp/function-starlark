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
