package main

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/crossplane/function-sdk-go/errors"
	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/resource"
	"github.com/crossplane/function-sdk-go/response"
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

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"ValidInput": {
			reason: "The function should accept valid StarlarkInput and return success with Normal result.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "pass"
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
								"source": "pass"
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: input parsed successfully (passthrough mode)")
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
							"source": "pass"
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
								"source": "pass"
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
					response.Normal(rsp, "function-starlark: input parsed successfully (passthrough mode)")
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
							"source": "pass"
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
								"source": "pass"
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: input parsed successfully (passthrough mode)")
					return rsp
				}(),
			},
		},
		"ScriptConfigRefOnly": {
			reason: "The function should accept scriptConfigRef as an alternative to inline source.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"scriptConfigRef": {
								"name": "my-script",
								"namespace": "default",
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
									"namespace": "default",
									"key": "main.star"
								}
							}
						}`),
					}, response.DefaultTTL)
					response.Normal(rsp, "function-starlark: input parsed successfully (passthrough mode)")
					return rsp
				}(),
			},
		},
		"InvalidInputJSON": {
			reason: "The function should return Fatal when input cannot be parsed into StarlarkInput.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": "not-an-object"
					}`),
				},
			},
			want: want{
				rsp: func() *fnv1.RunFunctionResponse {
					rsp := response.To(&fnv1.RunFunctionRequest{
						Input: resource.MustStructJSON(`{
							"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
							"kind": "StarlarkInput",
							"spec": "not-an-object"
						}`),
					}, response.DefaultTTL)
					response.Fatal(rsp, errors.Wrapf(errors.New("cannot get function input *v1alpha1.StarlarkInput from *v1.RunFunctionRequest: cannot unmarshal JSON from *structpb.Struct into *v1alpha1.StarlarkInput: json: cannot unmarshal JSON string into Go value of type v1alpha1.StarlarkInputSpec"), "cannot get Function input"))
					return rsp
				}(),
			},
		},
		"MultipleDesiredResources": {
			reason: "The function should preserve all desired resources from prior pipeline steps, not just one.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{
					Input: resource.MustStructJSON(`{
						"apiVersion": "starlark.fn.crossplane.io/v1alpha1",
						"kind": "StarlarkInput",
						"spec": {
							"source": "pass"
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
								"source": "pass"
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
					response.Normal(rsp, "function-starlark: input parsed successfully (passthrough mode)")
					return rsp
				}(),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := &Function{log: logging.NewNopLogger()}
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
