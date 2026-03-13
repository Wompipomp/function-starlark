package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/crossplane/function-sdk-go/errors"
	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/resource"
	"github.com/crossplane/function-sdk-go/response"
	"go.starlark.net/starlark"

	"github.com/wompipomp/function-starlark/runtime"
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
					_, err := rt.Execute("x = (", starlark.StringDict{})
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
					_, err := rt.Execute("x = {}['missing_key']", starlark.StringDict{})
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
					_, err := rt.Execute("while True: pass", starlark.StringDict{})
					response.Fatal(rsp, errors.Wrapf(err, "starlark execution failed"))
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
			contains: []string{"starlark execution failed", "Starlark compilation error", "composition.star"},
		},
		"RuntimeErrorMessage": {
			source:   "x = {}['missing_key']",
			contains: []string{"starlark execution failed", "Starlark execution error"},
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
