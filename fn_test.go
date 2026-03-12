package main

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/resource"
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
				rsp: &fnv1.RunFunctionResponse{
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  "function-starlark: input parsed successfully (passthrough mode)",
						},
					},
				},
			},
		},
		"MissingInput": {
			reason: "The function should return Fatal when Input field is nil.",
			args: args{
				ctx: context.Background(),
				req: &fnv1.RunFunctionRequest{},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_FATAL,
							Message:  "cannot get Function input: cannot get Function input from *v1.RunFunctionRequest: no input was specified",
						},
					},
				},
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
				rsp: &fnv1.RunFunctionResponse{
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_FATAL,
							Message:  "spec.source or spec.scriptConfigRef is required",
						},
					},
				},
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
				rsp: &fnv1.RunFunctionResponse{
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
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  "function-starlark: input parsed successfully (passthrough mode)",
						},
					},
				},
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
				rsp: &fnv1.RunFunctionResponse{
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  "function-starlark: input parsed successfully (passthrough mode)",
						},
					},
				},
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
