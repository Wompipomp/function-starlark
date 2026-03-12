//go:build generate

// Remove existing generated input.
//go:generate rm -rf ../package/input/

// Generate deepcopy methods and CRD schemas.
//go:generate go run -tags generate sigs.k8s.io/controller-tools/cmd/controller-gen paths=./v1alpha1 object crd:crdVersions=v1 output:artifacts:config=../package/input

package input

import _ "sigs.k8s.io/controller-tools/cmd/controller-gen"
