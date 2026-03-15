package main

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crossplane/function-sdk-go/errors"
	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/resource"
	"github.com/crossplane/function-sdk-go/response"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"go.starlark.net/starlark"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/wompipomp/function-starlark/runtime"
	"github.com/wompipomp/function-starlark/runtime/oci"
)

func TestRunFunction(t *testing.T) {
	type args struct {
		ctx context.Context
		req *fnv1.RunFunctionRequest
	}
	type want struct {
		rsp *fnv1.RunFunctionResponse
		err error
	}

	// Shared runtime for all test cases.
	rt := runtime.NewRuntime(logging.NewNopLogger())

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"ValidInput": {
			reason: "The function should execute valid Starlark and return success with Normal result.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "x = 1 + 2"
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "x = 1 + 2"
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					// ApplyDXR always sets desired composite (empty when no observed composite).
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
					}
					return rsp
				}(),
			},
		},
		"MissingInput": {
			reason: "The function should return Fatal when Input field is nil.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{}, response.DefaultTTL)
					response.Fatal(rsp, errors.New("spec.source or spec.scriptConfigRef is required"))
					return rsp
				}(),
			},
		},
		"MissingSource": {
			reason: "The function should return Fatal when source is empty and no scriptConfigRef.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {}
						}`),
					}, response.DefaultTTL)
					response.Fatal(rsp, errors.New("spec.source or spec.scriptConfigRef is required"))
					return rsp
				}(),
			},
		},
		"PreservesDesiredState": {
			reason: "The function should preserve desired state from prior pipeline steps.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "x = 1"
						}
					}`),
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{"apiVersion":"example.crossplane.io/v1","kind":"XBucket"}`),
						},
						Resources: map[string]*fnv1.Resource{
							"bucket": {
								Resource: resource.MustStructJSON(`{"apiVersion":"s3.aws.upbound.io/v1beta1","kind":"Bucket"}`),
							},
						},
					},
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "x = 1"
							}
						}`),
						Desired: &fnv1.State{
							Composite: &fnv1.Resource{
								Resource: resource.MustStructJSON(`{"apiVersion":"example.crossplane.io/v1","kind":"XBucket"}`),
							},
							Resources: map[string]*fnv1.Resource{
								"bucket": {
									Resource: resource.MustStructJSON(`{"apiVersion":"s3.aws.upbound.io/v1beta1","kind":"Bucket"}`),
								},
							},
						},
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					return rsp
				}(),
			},
		},
		"EmptyDesiredState": {
			reason: "The function should handle empty desired state (first function in pipeline) without panic.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "x = 1"
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "x = 1"
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
					}
					return rsp
				}(),
			},
		},
		"ScriptConfigRefOnly": {
			reason: "The function should return Fatal when scriptConfigRef points to a missing ConfigMap file.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"scriptConfigRef": {
								"name": "my-script",
								"key": "main.star"
							}
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"scriptConfigRef": {
									"name": "my-script",
									"key": "main.star"
								}
							}
						}`),
					}, response.DefaultTTL)
					response.Fatal(rsp, errors.Errorf(
						"loading script from ConfigMap: script file %q not found; ensure the ConfigMap %q is mounted via DeploymentRuntimeConfig",
						"/scripts/my-script/main.star", "my-script",
					))
					return rsp
				}(),
			},
		},
		"SourceWithPrint": {
			reason: "The function should handle print() in source without error.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "print('hello from starlark')"
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "print('hello from starlark')"
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
					}
					return rsp
				}(),
			},
		},
		"SourceWithCommentsOnly": {
			reason: "The function should succeed when source contains only comments.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "# just a comment"
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "# just a comment"
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
					}
					return rsp
				}(),
			},
		},
		// InvalidInputJSON moved to TestInvalidInputJSON (substring match)
		// because Go's encoding/json error messages differ between race and
		// non-race builds ("cannot" vs "unable to" unmarshal).
		"MultipleDesiredResources": {
			reason: "The function should preserve all desired resources from prior pipeline steps, not just one.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "x = 1"
						}
					}`),
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{"apiVersion":"example.crossplane.io/v1","kind":"XBucket"}`),
						},
						Resources: map[string]*fnv1.Resource{
							"bucket": {
								Resource: resource.MustStructJSON(`{"apiVersion":"s3.aws.upbound.io/v1beta1","kind":"Bucket"}`),
							},
							"policy": {
								Resource: resource.MustStructJSON(`{"apiVersion":"iam.aws.upbound.io/v1beta1","kind":"Policy"}`),
							},
							"role": {
								Resource: resource.MustStructJSON(`{"apiVersion":"iam.aws.upbound.io/v1beta1","kind":"Role"}`),
							},
						},
					},
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "x = 1"
							}
						}`),
						Desired: &fnv1.State{
							Composite: &fnv1.Resource{
								Resource: resource.MustStructJSON(`{"apiVersion":"example.crossplane.io/v1","kind":"XBucket"}`),
							},
							Resources: map[string]*fnv1.Resource{
								"bucket": {
									Resource: resource.MustStructJSON(`{"apiVersion":"s3.aws.upbound.io/v1beta1","kind":"Bucket"}`),
								},
								"policy": {
									Resource: resource.MustStructJSON(`{"apiVersion":"iam.aws.upbound.io/v1beta1","kind":"Policy"}`),
								},
								"role": {
									Resource: resource.MustStructJSON(`{"apiVersion":"iam.aws.upbound.io/v1beta1","kind":"Role"}`),
								},
							},
						},
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					return rsp
				}(),
			},
		},
		// ========================
		// E2E tests for Phase 4 requirements (STAT-01 through STAT-05, RSRC-01 through RSRC-08)
		// ========================

		"ResourceCreation": {
			reason: "STAT-03/RSRC-06: Script creates one resource via Resource(). Resource appears in response with correct body and READY_TRUE.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "Resource('bucket', {'apiVersion': 's3.aws.upbound.io/v1beta1', 'kind': 'Bucket', 'metadata': {'name': 'my-bucket'}, 'spec': {'forProvider': {'region': 'us-east-1'}}})"
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "Resource('bucket', {'apiVersion': 's3.aws.upbound.io/v1beta1', 'kind': 'Bucket', 'metadata': {'name': 'my-bucket'}, 'spec': {'forProvider': {'region': 'us-east-1'}}})"
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
						Resources: map[string]*fnv1.Resource{
							"bucket": {
								Resource: resource.MustStructJSON(`{
									"apiVersion": "s3.aws.upbound.io/v1beta1",
									"kind": "Bucket",
									"metadata": {"name": "my-bucket"},
									"spec": {"forProvider": {"region": "us-east-1"}}
								}`),
								Ready: fnv1.Ready_READY_UNSPECIFIED,
							},
						},
					}
					return rsp
				}(),
			},
		},
		"OXRAccess": {
			reason: "STAT-01/RSRC-07: Script reads oxr spec, metadata, labels, annotations, and status fields and creates resource to prove values are readable.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "region = oxr.spec.parameters.region\napp = oxr.metadata.labels['app']\nnote = oxr.metadata.annotations['note']\nReady = oxr.status.ready\nResource('test', {'region': region, 'app': app, 'note': note, 'ready': Ready})"
						}
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.crossplane.io/v1",
								"kind": "XBucket",
								"spec": {
									"parameters": {"region": "us-east-1"}
								},
								"status": {"ready": "True"},
								"metadata": {
									"labels": {"app": "web"},
									"annotations": {"note": "test"}
								}
							}`),
						},
					},
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "region = oxr.spec.parameters.region\napp = oxr.metadata.labels['app']\nnote = oxr.metadata.annotations['note']\nReady = oxr.status.ready\nResource('test', {'region': region, 'app': app, 'note': note, 'ready': Ready})"
							}
						}`),
						Observed: &fnv1.State{
							Composite: &fnv1.Resource{
								Resource: resource.MustStructJSON(`{
									"apiVersion": "example.crossplane.io/v1",
									"kind": "XBucket",
									"spec": {
										"parameters": {"region": "us-east-1"}
									},
									"status": {"ready": "True"},
									"metadata": {
										"labels": {"app": "web"},
										"annotations": {"note": "test"}
									}
								}`),
							},
						},
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
						Resources: map[string]*fnv1.Resource{
							"test": {
								Resource: resource.MustStructJSON(`{
									"region": "us-east-1",
									"app": "web",
									"note": "test",
									"ready": "True"
								}`),
								Ready: fnv1.Ready_READY_UNSPECIFIED,
							},
						},
					}
					return rsp
				}(),
			},
		},
		"ObservedAccess": {
			reason: "STAT-02/RSRC-07: Script reads observed composed resources by name and uses their fields.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "region = observed['bucket'].spec.forProvider.region\nResource('new-bucket', {'region': region})"
						}
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{"apiVersion": "example.crossplane.io/v1", "kind": "XBucket"}`),
						},
						Resources: map[string]*fnv1.Resource{
							"bucket": {
								Resource: resource.MustStructJSON(`{
									"apiVersion": "s3.aws.upbound.io/v1beta1",
									"kind": "Bucket",
									"spec": {"forProvider": {"region": "eu-west-1"}}
								}`),
							},
							"policy": {
								Resource: resource.MustStructJSON(`{
									"apiVersion": "iam.aws.upbound.io/v1beta1",
									"kind": "Policy"
								}`),
							},
						},
					},
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "region = observed['bucket'].spec.forProvider.region\nResource('new-bucket', {'region': region})"
							}
						}`),
						Observed: &fnv1.State{
							Composite: &fnv1.Resource{
								Resource: resource.MustStructJSON(`{"apiVersion": "example.crossplane.io/v1", "kind": "XBucket"}`),
							},
							Resources: map[string]*fnv1.Resource{
								"bucket": {
									Resource: resource.MustStructJSON(`{
										"apiVersion": "s3.aws.upbound.io/v1beta1",
										"kind": "Bucket",
										"spec": {"forProvider": {"region": "eu-west-1"}}
									}`),
								},
								"policy": {
									Resource: resource.MustStructJSON(`{
										"apiVersion": "iam.aws.upbound.io/v1beta1",
										"kind": "Policy"
									}`),
								},
							},
						},
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
						Resources: map[string]*fnv1.Resource{
							"new-bucket": {
								Resource: resource.MustStructJSON(`{"region": "eu-west-1"}`),
								Ready:    fnv1.Ready_READY_UNSPECIFIED,
							},
						},
					}
					return rsp
				}(),
			},
		},
		"DXRStatusMutation": {
			reason: "STAT-04: Script sets dxr.status fields. Changes appear in response desired composite.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "dxr['status'] = {'ready': 'True', 'synced': 'True'}"
						}
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.crossplane.io/v1",
								"kind": "XBucket"
							}`),
						},
					},
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "dxr['status'] = {'ready': 'True', 'synced': 'True'}"
							}
						}`),
						Observed: &fnv1.State{
							Composite: &fnv1.Resource{
								Resource: resource.MustStructJSON(`{
									"apiVersion": "example.crossplane.io/v1",
									"kind": "XBucket"
								}`),
							},
						},
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"status": {"ready": "True", "synced": "True"}
							}`),
						},
					}
					return rsp
				}(),
			},
		},
		"PreservesDesiredWithScript": {
			reason: "STAT-05: Prior desired resources preserved even when script creates new ones.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "Resource('new-bucket', {'apiVersion': 's3.aws.upbound.io/v1beta1', 'kind': 'Bucket'})"
						}
					}`),
					Desired: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{"apiVersion":"example.crossplane.io/v1","kind":"XBucket"}`),
						},
						Resources: map[string]*fnv1.Resource{
							"existing-bucket": {
								Resource: resource.MustStructJSON(`{"apiVersion":"s3.aws.upbound.io/v1beta1","kind":"Bucket","metadata":{"name":"existing"}}`),
								Ready:    fnv1.Ready_READY_UNSPECIFIED,
							},
						},
					},
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "Resource('new-bucket', {'apiVersion': 's3.aws.upbound.io/v1beta1', 'kind': 'Bucket'})"
							}
						}`),
						Desired: &fnv1.State{
							Composite: &fnv1.Resource{
								Resource: resource.MustStructJSON(`{"apiVersion":"example.crossplane.io/v1","kind":"XBucket"}`),
							},
							Resources: map[string]*fnv1.Resource{
								"existing-bucket": {
									Resource: resource.MustStructJSON(`{"apiVersion":"s3.aws.upbound.io/v1beta1","kind":"Bucket","metadata":{"name":"existing"}}`),
									Ready:    fnv1.Ready_READY_UNSPECIFIED,
								},
							},
						},
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					// ApplyDXR sets the desired composite from the dxr (built from desired composite in request).
					rsp.Desired.Composite.Resource = resource.MustStructJSON(`{"apiVersion":"example.crossplane.io/v1","kind":"XBucket"}`)
					// ApplyResources adds new resources, preserving existing ones.
					rsp.Desired.Resources["new-bucket"] = &fnv1.Resource{
						Resource: resource.MustStructJSON(`{"apiVersion":"s3.aws.upbound.io/v1beta1","kind":"Bucket"}`),
						Ready:    fnv1.Ready_READY_UNSPECIFIED,
					}
					return rsp
				}(),
			},
		},
		"ConditionalResource": {
			reason: "RSRC-01: Script uses if/else to conditionally create resources based on oxr fields.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "if oxr.spec.parameters.createBucket == True:\n    Resource('bucket', {'apiVersion': 's3.aws.upbound.io/v1beta1', 'kind': 'Bucket'})"
						}
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.crossplane.io/v1",
								"kind": "XBucket",
								"spec": {
									"parameters": {"createBucket": true}
								}
							}`),
						},
					},
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "if oxr.spec.parameters.createBucket == True:\n    Resource('bucket', {'apiVersion': 's3.aws.upbound.io/v1beta1', 'kind': 'Bucket'})"
							}
						}`),
						Observed: &fnv1.State{
							Composite: &fnv1.Resource{
								Resource: resource.MustStructJSON(`{
									"apiVersion": "example.crossplane.io/v1",
									"kind": "XBucket",
									"spec": {
										"parameters": {"createBucket": true}
									}
								}`),
							},
						},
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
						Resources: map[string]*fnv1.Resource{
							"bucket": {
								Resource: resource.MustStructJSON(`{"apiVersion":"s3.aws.upbound.io/v1beta1","kind":"Bucket"}`),
								Ready:    fnv1.Ready_READY_UNSPECIFIED,
							},
						},
					}
					return rsp
				}(),
			},
		},
		"IteratedResources": {
			reason: "RSRC-02: Script uses for-loop to create multiple resources.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "for i in range(3):\n    Resource('instance-%d' % i, {'apiVersion': 'ec2.aws.upbound.io/v1beta1', 'kind': 'Instance', 'metadata': {'name': 'instance-%d' % i}})"
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "for i in range(3):\n    Resource('instance-%d' % i, {'apiVersion': 'ec2.aws.upbound.io/v1beta1', 'kind': 'Instance', 'metadata': {'name': 'instance-%d' % i}})"
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
						Resources: map[string]*fnv1.Resource{
							"instance-0": {
								Resource: resource.MustStructJSON(`{"apiVersion":"ec2.aws.upbound.io/v1beta1","kind":"Instance","metadata":{"name":"instance-0"}}`),
								Ready:    fnv1.Ready_READY_UNSPECIFIED,
							},
							"instance-1": {
								Resource: resource.MustStructJSON(`{"apiVersion":"ec2.aws.upbound.io/v1beta1","kind":"Instance","metadata":{"name":"instance-1"}}`),
								Ready:    fnv1.Ready_READY_UNSPECIFIED,
							},
							"instance-2": {
								Resource: resource.MustStructJSON(`{"apiVersion":"ec2.aws.upbound.io/v1beta1","kind":"Instance","metadata":{"name":"instance-2"}}`),
								Ready:    fnv1.Ready_READY_UNSPECIFIED,
							},
						},
					}
					return rsp
				}(),
			},
		},
		"HelperFunctionResource": {
			reason: "RSRC-03: Script defines helper function with def and calls it to create resources.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "def make_bucket(name, region):\n    Resource(name, {'apiVersion': 's3.aws.upbound.io/v1beta1', 'kind': 'Bucket', 'spec': {'forProvider': {'region': region}}})\nmake_bucket('my-bucket', 'us-west-2')"
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "def make_bucket(name, region):\n    Resource(name, {'apiVersion': 's3.aws.upbound.io/v1beta1', 'kind': 'Bucket', 'spec': {'forProvider': {'region': region}}})\nmake_bucket('my-bucket', 'us-west-2')"
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
						Resources: map[string]*fnv1.Resource{
							"my-bucket": {
								Resource: resource.MustStructJSON(`{"apiVersion":"s3.aws.upbound.io/v1beta1","kind":"Bucket","spec":{"forProvider":{"region":"us-west-2"}}}`),
								Ready:    fnv1.Ready_READY_UNSPECIFIED,
							},
						},
					}
					return rsp
				}(),
			},
		},
		"ReadyFalse": {
			reason: "RSRC-05: Script calls Resource with ready=False. Resource has READY_FALSE in response.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "Resource('pending', {'apiVersion': 's3.aws.upbound.io/v1beta1', 'kind': 'Bucket'}, ready=False)"
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "Resource('pending', {'apiVersion': 's3.aws.upbound.io/v1beta1', 'kind': 'Bucket'}, ready=False)"
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
						Resources: map[string]*fnv1.Resource{
							"pending": {
								Resource: resource.MustStructJSON(`{"apiVersion":"s3.aws.upbound.io/v1beta1","kind":"Bucket"}`),
								Ready:    fnv1.Ready_READY_FALSE,
							},
						},
					}
					return rsp
				}(),
			},
		},
		"GetBuiltin": {
			reason: "RSRC-08: Script uses get() for safe nested access with default fallback.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "region = get(oxr, 'spec.parameters.region')\nfallback = get(oxr, 'spec.missing.field', 'fallback-value')\nResource('test', {'region': region, 'fallback': fallback})"
						}
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.crossplane.io/v1",
								"kind": "XBucket",
								"spec": {
									"parameters": {"region": "ap-southeast-1"}
								}
							}`),
						},
					},
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "region = get(oxr, 'spec.parameters.region')\nfallback = get(oxr, 'spec.missing.field', 'fallback-value')\nResource('test', {'region': region, 'fallback': fallback})"
							}
						}`),
						Observed: &fnv1.State{
							Composite: &fnv1.Resource{
								Resource: resource.MustStructJSON(`{
									"apiVersion": "example.crossplane.io/v1",
									"kind": "XBucket",
									"spec": {
										"parameters": {"region": "ap-southeast-1"}
									}
								}`),
							},
						},
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
						Resources: map[string]*fnv1.Resource{
							"test": {
								Resource: resource.MustStructJSON(`{"region": "ap-southeast-1", "fallback": "fallback-value"}`),
								Ready:    fnv1.Ready_READY_UNSPECIFIED,
							},
						},
					}
					return rsp
				}(),
			},
		},
		"ConditionalFalse": {
			reason: "RSRC-01 negative: When condition is false, no resource is created.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "if oxr.spec.parameters.createBucket == True:\n    Resource('bucket', {'apiVersion': 's3.aws.upbound.io/v1beta1', 'kind': 'Bucket'})"
						}
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.crossplane.io/v1",
								"kind": "XBucket",
								"spec": {
									"parameters": {"createBucket": false}
								}
							}`),
						},
					},
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "if oxr.spec.parameters.createBucket == True:\n    Resource('bucket', {'apiVersion': 's3.aws.upbound.io/v1beta1', 'kind': 'Bucket'})"
							}
						}`),
						Observed: &fnv1.State{
							Composite: &fnv1.Resource{
								Resource: resource.MustStructJSON(`{
									"apiVersion": "example.crossplane.io/v1",
									"kind": "XBucket",
									"spec": {
										"parameters": {"createBucket": false}
									}
								}`),
							},
						},
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					// No resources created, but ApplyDXR still sets empty desired composite.
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
					}
					return rsp
				}(),
			},
		},
		"ObservedMissingResource": {
			reason: "STAT-02 edge: Script handles missing observed resource gracefully via get() fallback.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "val = get(observed, 'nonexistent', 'missing')\nResource('test', {'status': val})"
						}
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{"apiVersion": "example.crossplane.io/v1", "kind": "XBucket"}`),
						},
					},
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "val = get(observed, 'nonexistent', 'missing')\nResource('test', {'status': val})"
							}
						}`),
						Observed: &fnv1.State{
							Composite: &fnv1.Resource{
								Resource: resource.MustStructJSON(`{"apiVersion": "example.crossplane.io/v1", "kind": "XBucket"}`),
							},
						},
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
						Resources: map[string]*fnv1.Resource{
							"test": {
								Resource: resource.MustStructJSON(`{"status": "missing"}`),
								Ready:    fnv1.Ready_READY_UNSPECIFIED,
							},
						},
					}
					return rsp
				}(),
			},
		},
		"MultipleResourceTypes": {
			reason: "STAT-03+RSRC-06: Script creates 3 resources of different API groups in a single execution.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "Resource('bucket', {'apiVersion': 's3.aws.upbound.io/v1beta1', 'kind': 'Bucket'})\nResource('queue', {'apiVersion': 'sqs.aws.upbound.io/v1beta1', 'kind': 'Queue'})\nResource('topic', {'apiVersion': 'sns.aws.upbound.io/v1beta1', 'kind': 'Topic'})"
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "Resource('bucket', {'apiVersion': 's3.aws.upbound.io/v1beta1', 'kind': 'Bucket'})\nResource('queue', {'apiVersion': 'sqs.aws.upbound.io/v1beta1', 'kind': 'Queue'})\nResource('topic', {'apiVersion': 'sns.aws.upbound.io/v1beta1', 'kind': 'Topic'})"
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
						Resources: map[string]*fnv1.Resource{
							"bucket": {
								Resource: resource.MustStructJSON(`{"apiVersion":"s3.aws.upbound.io/v1beta1","kind":"Bucket"}`),
								Ready:    fnv1.Ready_READY_UNSPECIFIED,
							},
							"queue": {
								Resource: resource.MustStructJSON(`{"apiVersion":"sqs.aws.upbound.io/v1beta1","kind":"Queue"}`),
								Ready:    fnv1.Ready_READY_UNSPECIFIED,
							},
							"topic": {
								Resource: resource.MustStructJSON(`{"apiVersion":"sns.aws.upbound.io/v1beta1","kind":"Topic"}`),
								Ready:    fnv1.Ready_READY_UNSPECIFIED,
							},
						},
					}
					return rsp
				}(),
			},
		},
		"LastWinsResource": {
			reason: "RSRC-06 detail: Duplicate Resource() calls with same name use last-wins semantics.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "Resource('bucket', {'apiVersion': 'v1beta1', 'kind': 'Bucket', 'spec': {'region': 'us-east-1'}})\nResource('bucket', {'apiVersion': 'v1beta1', 'kind': 'Bucket', 'spec': {'region': 'eu-west-1'}})"
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "Resource('bucket', {'apiVersion': 'v1beta1', 'kind': 'Bucket', 'spec': {'region': 'us-east-1'}})\nResource('bucket', {'apiVersion': 'v1beta1', 'kind': 'Bucket', 'spec': {'region': 'eu-west-1'}})"
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
						Resources: map[string]*fnv1.Resource{
							"bucket": {
								Resource: resource.MustStructJSON(`{"apiVersion":"v1beta1","kind":"Bucket","spec":{"region":"eu-west-1"}}`),
								Ready:    fnv1.Ready_READY_UNSPECIFIED,
							},
						},
					}
					return rsp
				}(),
			},
		},
		"CompilationError": {
			reason: "The function should return Fatal when Starlark source has a syntax error.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "x = ("
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "x = ("
							}
						}`),
					}, response.DefaultTTL)
					// Execute via runtime to get the exact error.
					_, err := rt.Execute("x = (", starlark.StringDict{}, "composition.star", nil)
					response.Fatal(rsp, errors.Wrapf(err, "starlark execution failed"))
					return rsp
				}(),
			},
		},
		"RuntimeError": {
			reason: "The function should return Fatal when Starlark script encounters a runtime error.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "x = {}['missing_key']"
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "x = {}['missing_key']"
							}
						}`),
					}, response.DefaultTTL)
					// Execute via runtime to get the exact error.
					_, err := rt.Execute("x = {}['missing_key']", starlark.StringDict{}, "composition.star", nil)
					response.Fatal(rsp, errors.Wrapf(err, "starlark execution failed"))
					return rsp
				}(),
			},
		},
		"StepLimitExceeded": {
			reason: "The function should return Fatal when a Starlark script exceeds the execution step limit.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "while True: pass"
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "while True: pass"
							}
						}`),
					}, response.DefaultTTL)
					// Execute via runtime to get the exact error.
					_, err := rt.Execute("while True: pass", starlark.StringDict{}, "composition.star", nil)
					response.Fatal(rsp, errors.Wrapf(err, "starlark execution failed"))
					return rsp
				}(),
			},
		},

		// -----------------------------------------------------------------
		// E2E tests for Phase 5 requirements
		// -----------------------------------------------------------------

		// STAT-06: Pipeline Context Read/Write
		"ContextReadWrite": {
			reason: "STAT-06: Script reads and writes pipeline context; response includes both existing and new keys.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "existing = context['my-key']\ncontext['new-key'] = 'new-value'"
						}
					}`),
					Context: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"my-key": structpb.NewStringValue("my-value"),
						},
					},
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "existing = context['my-key']\ncontext['new-key'] = 'new-value'"
							}
						}`),
						Context: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"my-key": structpb.NewStringValue("my-value"),
							},
						},
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"my-key":  structpb.NewStringValue("my-value"),
							"new-key": structpb.NewStringValue("new-value"),
						},
					}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
					}
					return rsp
				}(),
			},
		},

		// STAT-07: EnvironmentConfig Access
		"EnvironmentConfigAccess": {
			reason: "STAT-07: Script reads environment.data.region via dot-access on frozen StarlarkDict.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "region = environment.data.region\nResource('bucket', {'apiVersion': 'v1', 'kind': 'Bucket', 'spec': {'region': region}})"
						}
					}`),
					Context: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"apiextensions.crossplane.io/environment": structpb.NewStructValue(&structpb.Struct{
								Fields: map[string]*structpb.Value{
									"data": structpb.NewStructValue(&structpb.Struct{
										Fields: map[string]*structpb.Value{
											"region": structpb.NewStringValue("eu-west-1"),
										},
									}),
								},
							}),
						},
					},
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "region = environment.data.region\nResource('bucket', {'apiVersion': 'v1', 'kind': 'Bucket', 'spec': {'region': region}})"
							}
						}`),
						Context: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"apiextensions.crossplane.io/environment": structpb.NewStructValue(&structpb.Struct{
									Fields: map[string]*structpb.Value{
										"data": structpb.NewStructValue(&structpb.Struct{
											Fields: map[string]*structpb.Value{
												"region": structpb.NewStringValue("eu-west-1"),
											},
										}),
									},
								}),
							},
						},
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"apiextensions.crossplane.io/environment": structpb.NewStructValue(&structpb.Struct{
								Fields: map[string]*structpb.Value{
									"data": structpb.NewStructValue(&structpb.Struct{
										Fields: map[string]*structpb.Value{
											"region": structpb.NewStringValue("eu-west-1"),
										},
									}),
								},
							}),
						},
					}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
						Resources: map[string]*fnv1.Resource{
							"bucket": {
								Resource: resource.MustStructJSON(`{"apiVersion":"v1","kind":"Bucket","spec":{"region":"eu-west-1"}}`),
								Ready:    fnv1.Ready_READY_UNSPECIFIED,
							},
						},
					}
					return rsp
				}(),
			},
		},

		// STAT-08: Extra Resources Request (first reconciliation -- no resources yet)
		"ExtraResourcesRequest": {
			reason: "STAT-08: Script calls require_resource; response has Requirements with ResourceSelector.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "require_resource('my-db', 'rds.aws.upbound.io/v1beta1', 'Instance', match_name='my-database')"
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "require_resource('my-db', 'rds.aws.upbound.io/v1beta1', 'Instance', match_name='my-database')"
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
					}
					rsp.Requirements = &fnv1.Requirements{
						Resources: map[string]*fnv1.ResourceSelector{
							"my-db": {
								ApiVersion: "rds.aws.upbound.io/v1beta1",
								Kind:       "Instance",
								Match:      &fnv1.ResourceSelector_MatchName{MatchName: "my-database"},
							},
						},
					}
					return rsp
				}(),
			},
		},

		// STAT-08: Extra Resources Read (second reconciliation -- resources available)
		"ExtraResourcesRead": {
			reason: "STAT-08: Script reads extra_resources from a previous require_resource response.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "db = extra_resources['my-db'][0]\nname = get(db, 'metadata.name')\nResource('ref', {'apiVersion': 'v1', 'kind': 'Reference', 'spec': {'dbName': name}})"
						}
					}`),
					RequiredResources: map[string]*fnv1.Resources{
						"my-db": {
							Items: []*fnv1.Resource{
								{
									Resource: resource.MustStructJSON(`{"apiVersion":"rds.aws.upbound.io/v1beta1","kind":"Instance","metadata":{"name":"my-database"}}`),
								},
							},
						},
					},
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "db = extra_resources['my-db'][0]\nname = get(db, 'metadata.name')\nResource('ref', {'apiVersion': 'v1', 'kind': 'Reference', 'spec': {'dbName': name}})"
							}
						}`),
						RequiredResources: map[string]*fnv1.Resources{
							"my-db": {
								Items: []*fnv1.Resource{
									{
										Resource: resource.MustStructJSON(`{"apiVersion":"rds.aws.upbound.io/v1beta1","kind":"Instance","metadata":{"name":"my-database"}}`),
									},
								},
							},
						},
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
						Resources: map[string]*fnv1.Resource{
							"ref": {
								Resource: resource.MustStructJSON(`{"apiVersion":"v1","kind":"Reference","spec":{"dbName":"my-database"}}`),
								Ready:    fnv1.Ready_READY_UNSPECIFIED,
							},
						},
					}
					return rsp
				}(),
			},
		},

		// RSRC-04 + COMP-02: Connection Details Per-Resource
		"ConnectionDetailsPerResource": {
			reason: "RSRC-04+COMP-02: Script creates a Resource with connection_details kwarg.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "Resource('my-rds', {'apiVersion': 'v1', 'kind': 'RDS'}, connection_details={'username': 'admin', 'password': 'secret'})"
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "Resource('my-rds', {'apiVersion': 'v1', 'kind': 'RDS'}, connection_details={'username': 'admin', 'password': 'secret'})"
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
						Resources: map[string]*fnv1.Resource{
							"my-rds": {
								Resource:          resource.MustStructJSON(`{"apiVersion":"v1","kind":"RDS"}`),
								Ready:             fnv1.Ready_READY_UNSPECIFIED,
								ConnectionDetails: map[string][]byte{"username": []byte("admin"), "password": []byte("secret")},
							},
						},
					}
					return rsp
				}(),
			},
		},

		// COMP-02: XR-Level Connection Details
		"ConnectionDetailsXRLevel": {
			reason: "COMP-02: Script calls set_connection_details; response has XR-level connection details.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "set_connection_details({'endpoint': 'db.example.com', 'port': '5432'})"
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "set_connection_details({'endpoint': 'db.example.com', 'port': '5432'})"
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource:          resource.MustStructJSON(`{}`),
							ConnectionDetails: map[string][]byte{"endpoint": []byte("db.example.com"), "port": []byte("5432")},
						},
					}
					return rsp
				}(),
			},
		},

		// OBSV-02: Conditions
		"SetCondition": {
			reason: "OBSV-02: Script calls set_condition; response has matching condition.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "set_condition('DatabaseReady', 'True', 'Available', 'All databases healthy')"
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "set_condition('DatabaseReady', 'True', 'Available', 'All databases healthy')"
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
					}
					// Condition added by ApplyConditions via SDK helper.
					response.ConditionTrue(rsp, "DatabaseReady", "Available").WithMessage("All databases healthy")
					return rsp
				}(),
			},
		},

		// OBSV-03: Events
		"EmitEvent": {
			reason: "OBSV-03: Script calls emit_event; response has Normal result with message.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "emit_event('Normal', 'Resource reconciled successfully')"
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "emit_event('Normal', 'Resource reconciled successfully')"
							}
						}`),
					}, response.DefaultTTL)
					// ApplyEvents runs before the final response.Normal, so event result comes first.
					response.Normal(rsp, "Resource reconciled successfully")
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
					}
					return rsp
				}(),
			},
		},

		// OBSV-03: Fatal builtin
		"FatalBuiltin": {
			reason: "OBSV-03: Script calls fatal(); response has Fatal result and conditions/events still applied.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "set_condition('DatabaseReady', 'False', 'Failed', 'DB check failed')\nfatal('cannot proceed: database unavailable')"
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "set_condition('DatabaseReady', 'False', 'Failed', 'DB check failed')\nfatal('cannot proceed: database unavailable')"
							}
						}`),
					}, response.DefaultTTL)
					response.Fatal(rsp, errors.New("cannot proceed: database unavailable"))
					// Conditions set before fatal() are still applied.
					response.ConditionFalse(rsp, "DatabaseReady", "Failed").WithMessage("DB check failed")
					return rsp
				}(),
			},
		},

		// -----------------------------------------------------------------
		// E2E tests for Phase 8 requirements (MOD-01 through MOD-08)
		// Module loading through RunFunction pipeline
		// -----------------------------------------------------------------

		"InlineModuleLoad": {
			reason: "MOD-01: Script loads an inline module and calls its exported function.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "load(\"helpers.star\", \"greet\")\nresult = greet(\"world\")\nResource(\"test\", {\"greeting\": result})",
							"modules": {
								"helpers.star": "def greet(name):\n    return \"hello \" + name"
							}
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "load(\"helpers.star\", \"greet\")\nresult = greet(\"world\")\nResource(\"test\", {\"greeting\": result})",
								"modules": {
									"helpers.star": "def greet(name):\n    return \"hello \" + name"
								}
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
						Resources: map[string]*fnv1.Resource{
							"test": {
								Resource: resource.MustStructJSON(`{"greeting": "hello world"}`),
								Ready:    fnv1.Ready_READY_UNSPECIFIED,
							},
						},
					}
					return rsp
				}(),
			},
		},
		"InlineModuleUsesResource": {
			reason: "MOD-02: Module calls Resource() and the resource appears in desired state -- proves shared collector.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "load(\"infra.star\", \"make_bucket\")\nmake_bucket(\"my-bucket\", \"us-east-1\")",
							"modules": {
								"infra.star": "def make_bucket(name, region):\n    Resource(name, {\"apiVersion\": \"s3.aws.upbound.io/v1beta1\", \"kind\": \"Bucket\", \"spec\": {\"forProvider\": {\"region\": region}}})"
							}
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "load(\"infra.star\", \"make_bucket\")\nmake_bucket(\"my-bucket\", \"us-east-1\")",
								"modules": {
									"infra.star": "def make_bucket(name, region):\n    Resource(name, {\"apiVersion\": \"s3.aws.upbound.io/v1beta1\", \"kind\": \"Bucket\", \"spec\": {\"forProvider\": {\"region\": region}}})"
								}
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
						Resources: map[string]*fnv1.Resource{
							"my-bucket": {
								Resource: resource.MustStructJSON(`{"apiVersion":"s3.aws.upbound.io/v1beta1","kind":"Bucket","spec":{"forProvider":{"region":"us-east-1"}}}`),
								Ready:    fnv1.Ready_READY_UNSPECIFIED,
							},
						},
					}
					return rsp
				}(),
			},
		},
		"InlineModuleAccessesXR": {
			reason: "MOD-03: Module accesses oxr global to read composite resource data -- proves shared globals.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "load(\"reader.star\", \"read_region\")\nregion = read_region()\nResource(\"test\", {\"region\": region})",
							"modules": {
								"reader.star": "def read_region():\n    return get(oxr, \"spec.parameters.region\", \"default\")"
							}
						}
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "example.crossplane.io/v1",
								"kind": "XBucket",
								"spec": {
									"parameters": {"region": "ap-southeast-1"}
								}
							}`),
						},
					},
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "load(\"reader.star\", \"read_region\")\nregion = read_region()\nResource(\"test\", {\"region\": region})",
								"modules": {
									"reader.star": "def read_region():\n    return get(oxr, \"spec.parameters.region\", \"default\")"
								}
							}
						}`),
						Observed: &fnv1.State{
							Composite: &fnv1.Resource{
								Resource: resource.MustStructJSON(`{
									"apiVersion": "example.crossplane.io/v1",
									"kind": "XBucket",
									"spec": {
										"parameters": {"region": "ap-southeast-1"}
									}
								}`),
							},
						},
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
						Resources: map[string]*fnv1.Resource{
							"test": {
								Resource: resource.MustStructJSON(`{"region": "ap-southeast-1"}`),
								Ready:    fnv1.Ready_READY_UNSPECIFIED,
							},
						},
					}
					return rsp
				}(),
			},
		},
		"InlineModuleTransitiveLoad": {
			reason: "MOD-04: Module A loads Module B (both inline), transitive loading works.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "load(\"a.star\", \"from_a\")\nResource(\"test\", {\"value\": from_a()})",
							"modules": {
								"a.star": "load(\"b.star\", \"from_b\")\ndef from_a():\n    return \"a+\" + from_b()",
								"b.star": "def from_b():\n    return \"b\""
							}
						}
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": {
								"source": "load(\"a.star\", \"from_a\")\nResource(\"test\", {\"value\": from_a()})",
								"modules": {
									"a.star": "load(\"b.star\", \"from_b\")\ndef from_a():\n    return \"a+\" + from_b()",
									"b.star": "def from_b():\n    return \"b\""
								}
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: executed successfully")
					rsp.Context = &structpb.Struct{}
					rsp.Desired = &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{}`),
						},
						Resources: map[string]*fnv1.Resource{
							"test": {
								Resource: resource.MustStructJSON(`{"value": "a+b"}`),
								Ready:    fnv1.Ready_READY_UNSPECIFIED,
							},
						},
					}
					return rsp
				}(),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := &Function{log: logging.NewNopLogger(), runtime: rt}
			rsp, err := f.RunFunction(tc.args.ctx, tc.args.req)
			if diff := cmp.Diff(tc.want.rsp, rsp, protocmp.Transform()); diff != "" {
				t.Errorf("%s\nRunFunction(...): -want, +got:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.err, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("%s\nRunFunction(...) err: -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

// TestErrorSubstrings verifies that error messages contain expected substrings,
// providing human-readable failure diagnostics independent of exact error formatting.
func TestErrorSubstrings(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}

	cases := map[string]struct {
		source   string
		contains []string
	}{
		"CompilationErrorMessage": {
			source:   "x = (",
			contains: []string{"starlark execution failed", "starlark compilation error", "composition.star"},
		},
		"RuntimeErrorMessage": {
			source:   "x = {}['missing_key']",
			contains: []string{"starlark execution failed", "starlark execution error"},
		},
		"StepLimitMessage": {
			source:   "while True: pass",
			contains: []string{"starlark execution failed", "exceeded execution limit"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := &fnv1.RunFunctionRequest{
				Input: resource.MustStructJSON(fmt.Sprintf(`{
					"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
					"kind": "StarlarkInput",
					"spec": {
						"source": %q
					}
				}`, tc.source)),
			}

			rsp, err := f.RunFunction(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}

			// Should be Fatal severity.
			if rsp.GetResults()[0].GetSeverity() != fnv1.Severity_SEVERITY_FATAL {
				t.Errorf("expected SEVERITY_FATAL, got %v", rsp.GetResults()[0].GetSeverity())
			}

			msg := rsp.GetResults()[0].GetMessage()
			for _, sub := range tc.contains {
				if !strings.Contains(msg, sub) {
					t.Errorf("expected message to contain %q, got: %s", sub, msg)
				}
			}
		})
	}
}

// TestInvalidInputJSON verifies Fatal response for unparseable input.
// Uses substring matching because Go's encoding/json error messages differ
// between race and non-race builds ("cannot" vs "unable to" unmarshal).
func TestInvalidInputJSON(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": "not-an-object"
		}`),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	if rsp.GetResults()[0].GetSeverity() != fnv1.Severity_SEVERITY_FATAL {
		t.Errorf("expected SEVERITY_FATAL, got %v", rsp.GetResults()[0].GetSeverity())
	}

	msg := rsp.GetResults()[0].GetMessage()
	for _, sub := range []string{
		"cannot get Function input",
		"unmarshal JSON",
		"v1alpha1.StarlarkInputSpec",
	} {
		if !strings.Contains(msg, sub) {
			t.Errorf("expected message to contain %q, got: %s", sub, msg)
		}
	}
}

// TestConfigMapScriptLoading verifies RUNT-03: ConfigMap-mounted scripts execute
// identically to inline source -- same runtime, same globals, same pipeline.
func TestConfigMapScriptLoading(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())

	// Create a temp directory to simulate the ConfigMap volume mount.
	tmpDir := t.TempDir()
	scriptDir := filepath.Join(tmpDir, "my-scripts")
	if err := os.MkdirAll(scriptDir, 0o750); err != nil {
		t.Fatalf("creating script dir: %v", err)
	}

	// Write a Starlark script that exercises Resource() and context.
	script := `Resource('bucket', {'apiVersion': 'v1', 'kind': 'Bucket', 'spec': {'from': 'configmap'}})
context['loaded-from'] = 'configmap'`
	if err := os.WriteFile(filepath.Join(scriptDir, "main.star"), []byte(script), 0o600); err != nil {
		t.Fatalf("writing script: %v", err)
	}

	f := &Function{log: logging.NewNopLogger(), runtime: rt, scriptDir: tmpDir}

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {
				"scriptConfigRef": {
					"name": "my-scripts",
					"key": "main.star"
				}
			}
		}`),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	// Should have Normal result (successful execution).
	if len(rsp.GetResults()) == 0 {
		t.Fatal("expected at least one result")
	}
	if rsp.GetResults()[0].GetSeverity() != fnv1.Severity_SEVERITY_NORMAL {
		t.Errorf("expected SEVERITY_NORMAL, got %v (message: %s)",
			rsp.GetResults()[0].GetSeverity(), rsp.GetResults()[0].GetMessage())
	}

	// Should have created the bucket resource.
	bucket, ok := rsp.GetDesired().GetResources()["bucket"]
	if !ok {
		t.Fatal("expected 'bucket' resource in desired state")
	}
	kind := bucket.GetResource().GetFields()["kind"].GetStringValue()
	if kind != "Bucket" {
		t.Errorf("bucket kind = %q, want 'Bucket'", kind)
	}
	from := bucket.GetResource().GetFields()["spec"].GetStructValue().GetFields()["from"].GetStringValue()
	if from != "configmap" {
		t.Errorf("bucket spec.from = %q, want 'configmap'", from)
	}

	// Should have context with loaded-from key.
	loadedFrom := rsp.GetContext().GetFields()["loaded-from"].GetStringValue()
	if loadedFrom != "configmap" {
		t.Errorf("context['loaded-from'] = %q, want 'configmap'", loadedFrom)
	}
}

// TestConfigMapDefaultKey verifies that loadScript defaults key to "main.star".
func TestConfigMapDefaultKey(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())

	tmpDir := t.TempDir()
	scriptDir := filepath.Join(tmpDir, "my-scripts")
	if err := os.MkdirAll(scriptDir, 0o750); err != nil {
		t.Fatalf("creating script dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(scriptDir, "main.star"), []byte("x = 42"), 0o600); err != nil {
		t.Fatalf("writing script: %v", err)
	}

	f := &Function{log: logging.NewNopLogger(), runtime: rt, scriptDir: tmpDir}

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {
				"scriptConfigRef": {
					"name": "my-scripts"
				}
			}
		}`),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	// Should succeed (no key specified, defaults to main.star).
	if rsp.GetResults()[0].GetSeverity() != fnv1.Severity_SEVERITY_NORMAL {
		t.Errorf("expected SEVERITY_NORMAL, got %v (message: %s)",
			rsp.GetResults()[0].GetSeverity(), rsp.GetResults()[0].GetMessage())
	}
}

// TestRunFunctionDependsOnBasic verifies that a script using depends_on with ResourceRef
// produces a Usage resource in the response alongside normal resources.
func TestRunFunctionDependsOnBasic(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}

	script := `db = Resource("db", {"apiVersion": "v1", "kind": "Database"})
Resource("app", {"apiVersion": "v1", "kind": "App"}, depends_on=[db])`

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(fmt.Sprintf(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {"source": %q}
		}`, script)),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	// Should succeed.
	assertNormalResult(t, rsp)

	resources := rsp.GetDesired().GetResources()

	// Should have 3 resources: db, app, and one usage resource.
	if len(resources) != 3 {
		t.Fatalf("expected 3 resources, got %d: %v", len(resources), resourceNames(resources))
	}

	// Check db and app exist.
	if _, ok := resources["db"]; !ok {
		t.Error("expected 'db' resource in desired state")
	}
	if _, ok := resources["app"]; !ok {
		t.Error("expected 'app' resource in desired state")
	}

	// Check usage resource exists with correct structure.
	usageName := "usage-c2727553" // sha256("app\x00db")[:4] hex
	usage, ok := resources[usageName]
	if !ok {
		t.Fatalf("expected Usage resource %q in desired state; got keys: %v", usageName, resourceNames(resources))
	}

	// Verify Usage resource structure.
	body := usage.GetResource()
	if got := body.GetFields()["apiVersion"].GetStringValue(); got != "apiextensions.crossplane.io/v1alpha1" {
		t.Errorf("Usage apiVersion = %q, want apiextensions.crossplane.io/v1alpha1", got)
	}
	if got := body.GetFields()["kind"].GetStringValue(); got != "Usage" {
		t.Errorf("Usage kind = %q, want Usage", got)
	}

	spec := body.GetFields()["spec"].GetStructValue()
	if got := spec.GetFields()["replayDeletion"].GetBoolValue(); !got {
		t.Error("Usage spec.replayDeletion should be true")
	}

	// "of" should reference the dependency (db).
	ofRef := spec.GetFields()["of"].GetStructValue().GetFields()["resourceRef"].GetStructValue()
	if got := ofRef.GetFields()["name"].GetStringValue(); got != "db" {
		t.Errorf("Usage of.resourceRef.name = %q, want 'db'", got)
	}

	// "by" should reference the dependent (app).
	byRef := spec.GetFields()["by"].GetStructValue().GetFields()["resourceRef"].GetStructValue()
	if got := byRef.GetFields()["name"].GetStringValue(); got != "app" {
		t.Errorf("Usage by.resourceRef.name = %q, want 'app'", got)
	}

	// Usage should be READY_TRUE.
	if usage.GetReady() != fnv1.Ready_READY_TRUE {
		t.Errorf("Usage ready = %v, want READY_TRUE", usage.GetReady())
	}
}

// TestRunFunctionDependsOnStringRef verifies that string refs in depends_on
// produce Usage resources and emit a warning when the ref does not match any
// resource created by the script.
func TestRunFunctionDependsOnStringRef(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}

	script := `Resource("app", {"apiVersion": "v1", "kind": "App"}, depends_on=["external-vpc"])`

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(fmt.Sprintf(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {"source": %q}
		}`, script)),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	assertNormalResult(t, rsp)

	// Verify a SEVERITY_WARNING result exists for the unmatched string ref.
	assertWarningResult(t, rsp, "external-vpc", "string ref", "does not match")

	resources := rsp.GetDesired().GetResources()

	// Should have 2 resources: app and usage resource.
	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d: %v", len(resources), resourceNames(resources))
	}

	// Usage resource for string ref.
	usageName := "usage-76593d19" // sha256("app\x00external-vpc")[:4] hex
	if _, ok := resources[usageName]; !ok {
		t.Errorf("expected Usage resource %q; got keys: %v", usageName, resourceNames(resources))
	}
}

// TestRunFunctionDependsOnStringRefMatched verifies that string refs in
// depends_on do NOT produce warnings when the ref matches a created resource.
func TestRunFunctionDependsOnStringRefMatched(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}

	script := `Resource("db", {"apiVersion": "v1", "kind": "DB"})
Resource("app", {"apiVersion": "v1", "kind": "App"}, depends_on=["db"])`

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(fmt.Sprintf(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {"source": %q}
		}`, script)),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	assertNormalResult(t, rsp)

	// No warnings should be emitted -- "db" matches a created resource.
	for _, r := range rsp.GetResults() {
		if r.GetSeverity() == fnv1.Severity_SEVERITY_WARNING {
			t.Errorf("unexpected warning when string ref matches: %s", r.GetMessage())
		}
	}

	resources := rsp.GetDesired().GetResources()

	// Should have 3 resources: app, db, and usage.
	if len(resources) != 3 {
		t.Fatalf("expected 3 resources, got %d: %v", len(resources), resourceNames(resources))
	}
}

// TestRunFunctionDependsOnForwardRef verifies that forward references work:
// a resource can depend on another resource via ResourceRef even if the
// dependency is defined later in the script.
func TestRunFunctionDependsOnForwardRef(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}

	// Forward reference: app depends on db, but db is created after app.
	// This requires depends_on to use a string ref since db ResourceRef doesn't exist yet.
	// Actually, with ResourceRef, db must exist first. For forward ref via string:
	script := `Resource("app", {"apiVersion": "v1", "kind": "App"}, depends_on=["db"])
Resource("db", {"apiVersion": "v1", "kind": "Database"})`

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(fmt.Sprintf(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {"source": %q}
		}`, script)),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	// Forward references via string are trusted -- should succeed.
	assertNormalResult(t, rsp)

	resources := rsp.GetDesired().GetResources()

	// Should have 3 resources: app, db, and usage.
	if len(resources) != 3 {
		t.Fatalf("expected 3 resources, got %d: %v", len(resources), resourceNames(resources))
	}
}

// TestRunFunctionDependsOnCircularDependency verifies that circular dependencies
// return a Fatal response with cycle path.
func TestRunFunctionDependsOnCircularDependency(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}

	// A depends on B, B depends on A -> circular (using string refs).
	script := `Resource("a", {"apiVersion": "v1", "kind": "A"}, depends_on=["b"])
Resource("b", {"apiVersion": "v1", "kind": "B"}, depends_on=["a"])`

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(fmt.Sprintf(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {"source": %q}
		}`, script)),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	// Should be Fatal.
	if len(rsp.GetResults()) == 0 {
		t.Fatal("expected at least one result")
	}
	if rsp.GetResults()[0].GetSeverity() != fnv1.Severity_SEVERITY_FATAL {
		t.Fatalf("expected SEVERITY_FATAL, got %v", rsp.GetResults()[0].GetSeverity())
	}

	msg := rsp.GetResults()[0].GetMessage()
	if !strings.Contains(msg, "dependency validation failed") {
		t.Errorf("expected error to contain 'dependency validation failed', got: %s", msg)
	}
	if !strings.Contains(msg, "circular dependency detected") {
		t.Errorf("expected error to contain 'circular dependency detected', got: %s", msg)
	}
	// Should show cycle path with arrow notation.
	if !strings.Contains(msg, "->") {
		t.Errorf("expected error to contain '->' cycle path, got: %s", msg)
	}
}

// TestRunFunctionDependsOnChain verifies that a dependency chain A->B->C
// produces 2 Usage resources with correct dependency pairs.
func TestRunFunctionDependsOnChain(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}

	script := `cache = Resource("cache", {"apiVersion": "v1", "kind": "Cache"})
db = Resource("db", {"apiVersion": "v1", "kind": "Database"}, depends_on=[cache])
Resource("app", {"apiVersion": "v1", "kind": "App"}, depends_on=[db])`

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(fmt.Sprintf(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {"source": %q}
		}`, script)),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	assertNormalResult(t, rsp)

	resources := rsp.GetDesired().GetResources()

	// Should have 5 resources: cache, db, app, usage(db->cache), usage(app->db).
	if len(resources) != 5 {
		t.Fatalf("expected 5 resources, got %d: %v", len(resources), resourceNames(resources))
	}

	// Verify both usage resources exist.
	usageDBCache := "usage-543cc3c8" // sha256("db\x00cache")[:4] hex
	usageAppDB := "usage-c2727553"   // sha256("app\x00db")[:4] hex

	if _, ok := resources[usageDBCache]; !ok {
		t.Errorf("expected Usage resource %q (db->cache); got keys: %v", usageDBCache, resourceNames(resources))
	}
	if _, ok := resources[usageAppDB]; !ok {
		t.Errorf("expected Usage resource %q (app->db); got keys: %v", usageAppDB, resourceNames(resources))
	}
}

// TestRunFunctionDependsOnUsageAPIVersionV2 verifies that usageAPIVersion="v2"
// produces Usage resources with protection.crossplane.io/v1beta1 apiVersion.
func TestRunFunctionDependsOnUsageAPIVersionV2(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}

	script := `db = Resource("db", {"apiVersion": "v1", "kind": "Database"})
Resource("app", {"apiVersion": "v1", "kind": "App"}, depends_on=[db])`

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(fmt.Sprintf(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {"source": %q, "usageAPIVersion": "v2"}
		}`, script)),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	assertNormalResult(t, rsp)

	// Check Usage resource has v2 apiVersion.
	usageName := "usage-c2727553"
	usage, ok := rsp.GetDesired().GetResources()[usageName]
	if !ok {
		t.Fatalf("expected Usage resource %q", usageName)
	}

	got := usage.GetResource().GetFields()["apiVersion"].GetStringValue()
	want := "protection.crossplane.io/v1beta1"
	if got != want {
		t.Errorf("Usage apiVersion = %q, want %q", got, want)
	}
}

// TestRunFunctionResourceRefNameAttr verifies that Resource() return value
// (ResourceRef) has a .name attribute accessible in the script.
func TestRunFunctionResourceRefNameAttr(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}

	// Use ref.name to set a field in another resource.
	script := `ref = Resource("db", {"apiVersion": "v1", "kind": "Database"})
Resource("app", {"apiVersion": "v1", "kind": "App", "spec": {"dbRef": ref.name}})`

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(fmt.Sprintf(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {"source": %q}
		}`, script)),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	assertNormalResult(t, rsp)

	// Check app resource has dbRef set to "db".
	app, ok := rsp.GetDesired().GetResources()["app"]
	if !ok {
		t.Fatal("expected 'app' resource in desired state")
	}

	dbRef := app.GetResource().GetFields()["spec"].GetStructValue().GetFields()["dbRef"].GetStringValue()
	if dbRef != "db" {
		t.Errorf("app spec.dbRef = %q, want 'db'", dbRef)
	}
}

func TestRunFunctionDependsOnEmptyList(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}

	// depends_on=[] should be a no-op: no Usage resources, no warnings.
	script := `Resource("app", {"apiVersion": "v1", "kind": "App"}, depends_on=[])`

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(fmt.Sprintf(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {"source": %q}
		}`, script)),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	assertNormalResult(t, rsp)

	// Should have "app" resource but no Usage resources.
	resources := rsp.GetDesired().GetResources()
	if _, ok := resources["app"]; !ok {
		t.Fatal("expected 'app' resource in desired state")
	}
	for name := range resources {
		if strings.HasPrefix(name, "usage-") {
			t.Errorf("unexpected Usage resource %q for empty depends_on", name)
		}
	}

	// No warnings should be emitted.
	for _, r := range rsp.GetResults() {
		if r.GetSeverity() == fnv1.Severity_SEVERITY_WARNING {
			t.Errorf("unexpected warning: %s", r.GetMessage())
		}
	}
}

// TestRunFunctionModuleCircularLoad verifies that circular load() produces Fatal.
func TestRunFunctionModuleCircularLoad(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {
				"source": "load(\"a.star\", \"fn_a\")",
				"modules": {
					"a.star": "load(\"b.star\", \"fn_b\")\ndef fn_a():\n    return fn_b()",
					"b.star": "load(\"a.star\", \"fn_a\")\ndef fn_b():\n    return fn_a()"
				}
			}
		}`),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	assertFatalResult(t, rsp, "cycle in load graph")
}

// TestRunFunctionModuleNotFound verifies that loading a missing module produces Fatal.
func TestRunFunctionModuleNotFound(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {
				"source": "load(\"missing.star\", \"fn\")"
			}
		}`),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	assertFatalResult(t, rsp, "not found")
}

// TestRunFunctionModuleErrorShowsFilename verifies that errors in modules
// include the module filename in the error message.
func TestRunFunctionModuleErrorShowsFilename(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	f := &Function{log: logging.NewNopLogger(), runtime: rt}

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {
				"source": "load(\"bad.star\", \"boom\")",
				"modules": {
					"bad.star": "def boom():\n    return 1/0\nboom()"
				}
			}
		}`),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	assertFatalResult(t, rsp, "bad.star")
}

// TestRunFunctionModuleFilesystem verifies filesystem module loading:
// - Loading modules from the ConfigMap script directory
// - Loading modules from configured modulePaths
// - Inline modules taking priority over filesystem modules
func TestRunFunctionModuleFilesystem(t *testing.T) {
	t.Run("FilesystemModuleLoad", func(t *testing.T) {
		rt := runtime.NewRuntime(logging.NewNopLogger())

		// Create temp directory simulating ConfigMap mount.
		tmpDir := t.TempDir()
		scriptDir := filepath.Join(tmpDir, "my-scripts")
		if err := os.MkdirAll(scriptDir, 0o750); err != nil {
			t.Fatalf("creating script dir: %v", err)
		}

		// Write main script and a helper module.
		mainScript := `load("helpers.star", "tag")
Resource("test", {"tag": tag("prod")})`
		if err := os.WriteFile(filepath.Join(scriptDir, "main.star"), []byte(mainScript), 0o600); err != nil {
			t.Fatalf("writing main.star: %v", err)
		}

		helperModule := `def tag(env):
    return "env-" + env`
		if err := os.WriteFile(filepath.Join(scriptDir, "helpers.star"), []byte(helperModule), 0o600); err != nil {
			t.Fatalf("writing helpers.star: %v", err)
		}

		f := &Function{log: logging.NewNopLogger(), runtime: rt, scriptDir: tmpDir}

		req := &fnv1.RunFunctionRequest{
			Input: resource.MustStructJSON(`{
				"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
				"kind": "StarlarkInput",
				"spec": {
					"scriptConfigRef": {
						"name": "my-scripts",
						"key": "main.star"
					}
				}
			}`),
		}

		rsp, err := f.RunFunction(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}

		assertNormalResult(t, rsp)

		// Verify the resource was created with the module's tag function.
		testRes, ok := rsp.GetDesired().GetResources()["test"]
		if !ok {
			t.Fatal("expected 'test' resource in desired state")
		}
		tag := testRes.GetResource().GetFields()["tag"].GetStringValue()
		if tag != "env-prod" {
			t.Errorf("tag = %q, want 'env-prod'", tag)
		}
	})

	t.Run("ModulePathsConfig", func(t *testing.T) {
		rt := runtime.NewRuntime(logging.NewNopLogger())

		// Create a separate directory for shared modules.
		sharedDir := t.TempDir()
		sharedModule := `def shared_fn():
    return "from-shared"`
		if err := os.WriteFile(filepath.Join(sharedDir, "shared.star"), []byte(sharedModule), 0o600); err != nil {
			t.Fatalf("writing shared.star: %v", err)
		}

		f := &Function{log: logging.NewNopLogger(), runtime: rt}

		req := &fnv1.RunFunctionRequest{
			Input: resource.MustStructJSON(fmt.Sprintf(`{
				"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
				"kind": "StarlarkInput",
				"spec": {
					"source": "load(\"shared.star\", \"shared_fn\")\nResource(\"test\", {\"value\": shared_fn()})",
					"modulePaths": [%q]
				}
			}`, sharedDir)),
		}

		rsp, err := f.RunFunction(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}

		assertNormalResult(t, rsp)

		testRes, ok := rsp.GetDesired().GetResources()["test"]
		if !ok {
			t.Fatal("expected 'test' resource in desired state")
		}
		value := testRes.GetResource().GetFields()["value"].GetStringValue()
		if value != "from-shared" {
			t.Errorf("value = %q, want 'from-shared'", value)
		}
	})

	t.Run("InlineBeforeFilesystem", func(t *testing.T) {
		rt := runtime.NewRuntime(logging.NewNopLogger())

		// Create filesystem module with different content.
		fsDir := t.TempDir()
		fsModule := `def origin():
    return "filesystem"`
		if err := os.WriteFile(filepath.Join(fsDir, "helpers.star"), []byte(fsModule), 0o600); err != nil {
			t.Fatalf("writing helpers.star: %v", err)
		}

		f := &Function{log: logging.NewNopLogger(), runtime: rt}

		// Inline module has same name but different implementation.
		req := &fnv1.RunFunctionRequest{
			Input: resource.MustStructJSON(fmt.Sprintf(`{
				"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
				"kind": "StarlarkInput",
				"spec": {
					"source": "load(\"helpers.star\", \"origin\")\nResource(\"test\", {\"from\": origin()})",
					"modules": {
						"helpers.star": "def origin():\n    return \"inline\""
					},
					"modulePaths": [%q]
				}
			}`, fsDir)),
		}

		rsp, err := f.RunFunction(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}

		assertNormalResult(t, rsp)

		testRes, ok := rsp.GetDesired().GetResources()["test"]
		if !ok {
			t.Fatal("expected 'test' resource in desired state")
		}
		from := testRes.GetResource().GetFields()["from"].GetStringValue()
		if from != "inline" {
			t.Errorf("from = %q, want 'inline' (inline should take priority over filesystem)", from)
		}
	})
}

// ========================
// E2E tests for OCI loading and star import through RunFunction
// ========================

// TestRunFunctionOCILoadFromCache verifies that OCI load targets resolved from
// cache are available to the Starlark script via the inline module map.
func TestRunFunctionOCILoadFromCache(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	cache := oci.NewCache(5 * time.Minute)

	// Pre-populate cache with a tag -> digest -> content chain.
	cache.PutContent("sha256:abc123", map[string]string{
		"helpers.star": `def greet(name): return "oci-hello " + name`,
	})
	cache.PutTag("ghcr.io/org/lib:v1", "sha256:abc123")

	f := &Function{log: logging.NewNopLogger(), runtime: rt, ociCache: cache}

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {
				"source": "load(\"oci://ghcr.io/org/lib:v1/helpers.star\", \"greet\")\nResource(\"test\", {\"greeting\": greet(\"world\")})"
			}
		}`),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	assertNormalResult(t, rsp)

	testRes, ok := rsp.GetDesired().GetResources()["test"]
	if !ok {
		t.Fatal("expected 'test' resource in desired state")
	}
	greeting := testRes.GetResource().GetFields()["greeting"].GetStringValue()
	if greeting != "oci-hello world" {
		t.Errorf("greeting = %q, want 'oci-hello world'", greeting)
	}
}

// TestRunFunctionOCIDigestPin verifies that digest-pinned OCI references
// resolve from the content cache layer.
func TestRunFunctionOCIDigestPin(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	cache := oci.NewCache(5 * time.Minute)

	digest := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	cache.PutContent(digest, map[string]string{
		"pinned.star": `val = "deterministic"`,
	})

	f := &Function{log: logging.NewNopLogger(), runtime: rt, ociCache: cache}

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {
				"source": "load(\"oci://ghcr.io/org/lib@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890/pinned.star\", \"val\")\nResource(\"test\", {\"value\": val})"
			}
		}`),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	assertNormalResult(t, rsp)

	testRes, ok := rsp.GetDesired().GetResources()["test"]
	if !ok {
		t.Fatal("expected 'test' resource in desired state")
	}
	value := testRes.GetResource().GetFields()["value"].GetStringValue()
	if value != "deterministic" {
		t.Errorf("value = %q, want 'deterministic'", value)
	}
}

// TestRunFunctionOCIMissingModule verifies that a script loading from an
// unreachable OCI registry (cold cache miss) produces a Fatal response.
func TestRunFunctionOCIMissingModule(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	cache := oci.NewCache(5 * time.Minute)

	// Mock fetcher that returns error.
	fetcher := &testOCIFetcher{err: fmt.Errorf("connection refused")}

	f := &Function{log: logging.NewNopLogger(), runtime: rt, ociCache: cache, ociFetcher: fetcher}

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {
				"source": "load(\"oci://ghcr.io/org/lib:v1/helpers.star\", \"fn\")"
			}
		}`),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	assertFatalResult(t, rsp, "resolving OCI modules", "connection refused")
}

// TestRunFunctionOCIMediaTypeRejection verifies that an OCI artifact with the
// wrong media type produces a Fatal response.
func TestRunFunctionOCIMediaTypeRejection(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	cache := oci.NewCache(5 * time.Minute)

	// Build an image with wrong artifact type.
	img := buildTestOCIImage(t, map[string]string{
		"helpers.star": `x = 1`,
	}, "application/vnd.wrong.type", oci.LayerMediaType)

	fetcher := &testOCIFetcher{
		images: map[string]v1.Image{
			"ghcr.io/org/lib:v1": img,
		},
	}

	f := &Function{log: logging.NewNopLogger(), runtime: rt, ociCache: cache, ociFetcher: fetcher}

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {
				"source": "load(\"oci://ghcr.io/org/lib:v1/helpers.star\", \"x\")"
			}
		}`),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	assertFatalResult(t, rsp, "resolving OCI modules", "artifact type")
}

// TestRunFunctionStarImport verifies that star import works for inline modules.
func TestRunFunctionStarImport(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())

	f := &Function{log: logging.NewNopLogger(), runtime: rt, ociCache: oci.NewCache(5 * time.Minute)}

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {
				"source": "load(\"helpers.star\", \"*\")\nResource(\"test\", {\"a_val\": str(a), \"b_val\": str(b)})",
				"modules": {
					"helpers.star": "a = 1\nb = 2\n_private = 3"
				}
			}
		}`),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	assertNormalResult(t, rsp)

	testRes, ok := rsp.GetDesired().GetResources()["test"]
	if !ok {
		t.Fatal("expected 'test' resource in desired state")
	}
	aVal := testRes.GetResource().GetFields()["a_val"].GetStringValue()
	bVal := testRes.GetResource().GetFields()["b_val"].GetStringValue()
	if aVal != "1" {
		t.Errorf("a_val = %q, want '1'", aVal)
	}
	if bVal != "2" {
		t.Errorf("b_val = %q, want '2'", bVal)
	}
}

// TestRunFunctionStarImportOCI verifies that star import works for OCI modules.
func TestRunFunctionStarImportOCI(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())
	cache := oci.NewCache(5 * time.Minute)

	// Pre-populate cache with module that has multiple exports.
	cache.PutContent("sha256:star123", map[string]string{
		"lib.star": `x = 10
y = 20
_hidden = 30`,
	})
	cache.PutTag("ghcr.io/org/lib:v1", "sha256:star123")

	f := &Function{log: logging.NewNopLogger(), runtime: rt, ociCache: cache}

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {
				"source": "load(\"oci://ghcr.io/org/lib:v1/lib.star\", \"*\")\nResource(\"test\", {\"sum\": str(x + y)})"
			}
		}`),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	assertNormalResult(t, rsp)

	testRes, ok := rsp.GetDesired().GetResources()["test"]
	if !ok {
		t.Fatal("expected 'test' resource in desired state")
	}
	sum := testRes.GetResource().GetFields()["sum"].GetStringValue()
	if sum != "30" {
		t.Errorf("sum = %q, want '30' (x+y = 10+20)", sum)
	}
}

// TestRunFunctionOCINoTargets verifies that scripts without oci:// loads
// work normally (no regression).
func TestRunFunctionOCINoTargets(t *testing.T) {
	rt := runtime.NewRuntime(logging.NewNopLogger())

	// Providing ociCache ensures the OCI code path is active but not triggered.
	f := &Function{log: logging.NewNopLogger(), runtime: rt, ociCache: oci.NewCache(5 * time.Minute)}

	req := &fnv1.RunFunctionRequest{
		Input: resource.MustStructJSON(`{
			"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
			"kind": "StarlarkInput",
			"spec": {
				"source": "Resource(\"test\", {\"value\": \"no-oci\"})"
			}
		}`),
	}

	rsp, err := f.RunFunction(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	assertNormalResult(t, rsp)

	testRes, ok := rsp.GetDesired().GetResources()["test"]
	if !ok {
		t.Fatal("expected 'test' resource in desired state")
	}
	value := testRes.GetResource().GetFields()["value"].GetStringValue()
	if value != "no-oci" {
		t.Errorf("value = %q, want 'no-oci'", value)
	}
}

// testOCIFetcher is a mock OCI fetcher for E2E tests.
type testOCIFetcher struct {
	images map[string]v1.Image
	err    error
}

func (f *testOCIFetcher) Fetch(ref name.Reference, _ authn.Keychain) (v1.Image, error) {
	if f.err != nil {
		return nil, f.err
	}
	img, ok := f.images[ref.String()]
	if !ok {
		return nil, fmt.Errorf("image not found: %s", ref.String())
	}
	return img, nil
}

// buildTestOCIImage creates an in-memory OCI image with a tar layer for E2E tests.
func buildTestOCIImage(t *testing.T, files map[string]string, artifactType, layerType string) v1.Image {
	t.Helper()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, content := range files {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("writing tar header for %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("writing tar content for %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("closing tar writer: %v", err)
	}

	layer, err := tarball.LayerFromReader(bytes.NewReader(buf.Bytes()), tarball.WithMediaType(types.MediaType(layerType)))
	if err != nil {
		t.Fatalf("creating layer: %v", err)
	}

	img := mutate.MediaType(empty.Image, types.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, types.MediaType(artifactType))
	img, err = mutate.AppendLayers(img, layer)
	if err != nil {
		t.Fatalf("appending layer: %v", err)
	}

	return img
}

// assertFatalResult verifies the response has a Fatal severity result
// whose message contains all the specified substrings.
func assertFatalResult(t *testing.T, rsp *fnv1.RunFunctionResponse, substrings ...string) {
	t.Helper()
	if len(rsp.GetResults()) == 0 {
		t.Fatal("expected at least one result")
	}
	if rsp.GetResults()[0].GetSeverity() != fnv1.Severity_SEVERITY_FATAL {
		t.Fatalf("expected SEVERITY_FATAL, got %v (message: %s)",
			rsp.GetResults()[0].GetSeverity(), rsp.GetResults()[0].GetMessage())
	}
	msg := rsp.GetResults()[0].GetMessage()
	for _, sub := range substrings {
		if !strings.Contains(msg, sub) {
			t.Errorf("expected Fatal message to contain %q, got: %s", sub, msg)
		}
	}
}

// assertNormalResult verifies the response has a Normal severity result.
func assertNormalResult(t *testing.T, rsp *fnv1.RunFunctionResponse) {
	t.Helper()
	if len(rsp.GetResults()) == 0 {
		t.Fatal("expected at least one result")
	}
	lastResult := rsp.GetResults()[len(rsp.GetResults())-1]
	if lastResult.GetSeverity() != fnv1.Severity_SEVERITY_NORMAL {
		t.Fatalf("expected last result SEVERITY_NORMAL, got %v (message: %s)",
			lastResult.GetSeverity(), lastResult.GetMessage())
	}
}

// assertWarningResult verifies the response has at least one SEVERITY_WARNING
// result whose message contains all the specified substrings.
func assertWarningResult(t *testing.T, rsp *fnv1.RunFunctionResponse, substrings ...string) {
	t.Helper()
	for _, r := range rsp.GetResults() {
		if r.GetSeverity() != fnv1.Severity_SEVERITY_WARNING {
			continue
		}
		msg := r.GetMessage()
		allMatch := true
		for _, sub := range substrings {
			if !strings.Contains(msg, sub) {
				allMatch = false
				break
			}
		}
		if allMatch {
			return
		}
	}
	t.Errorf("expected a WARNING result containing %v; results: %v",
		substrings, rsp.GetResults())
}

// resourceNames extracts the keys from a resource map for diagnostic output.
func resourceNames(resources map[string]*fnv1.Resource) []string {
	names := make([]string, 0, len(resources))
	for name := range resources {
		names = append(names, name)
	}
	return names
}
