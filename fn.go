package main

import (
	"context"

	"github.com/crossplane/function-sdk-go/errors"
	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/request"
	"github.com/crossplane/function-sdk-go/response"

	"github.com/wompipomp/function-starlark/builtins"
	"github.com/wompipomp/function-starlark/input/v1alpha1"
	"github.com/wompipomp/function-starlark/runtime"
)

// Function implements the Crossplane composition function.
type Function struct {
	fnv1.UnimplementedFunctionRunnerServiceServer
	log     logging.Logger
	runtime *runtime.Runtime
}

// RunFunction processes a RunFunctionRequest.
func (f *Function) RunFunction(_ context.Context, req *fnv1.RunFunctionRequest) (*fnv1.RunFunctionResponse, error) {
	log := f.log.WithValues("tag", req.GetMeta().GetTag())
	log.Info("Running function")

	// CRITICAL: response.To copies desired state from the request,
	// preserving resources set by previous functions in the pipeline.
	rsp := response.To(req, response.DefaultTTL)

	// Parse the StarlarkInput from the Composition.
	in := &v1alpha1.StarlarkInput{}
	if err := request.GetInput(req, in); err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot get Function input"))
		return rsp, nil
	}

	// Validate that a source script is provided.
	if in.Spec.Source == "" && in.Spec.ScriptConfigRef == nil {
		response.Fatal(rsp, errors.New("spec.source or spec.scriptConfigRef is required"))
		return rsp, nil
	}

	log.Info("Parsed StarlarkInput", "source-length", len(in.Spec.Source))

	// Execute the Starlark script if inline source is provided.
	// When only scriptConfigRef is set, skip execution (Phase 5 handles ConfigMap loading).
	if in.Spec.Source != "" {
		collector := builtins.NewCollector()
		globals, err := builtins.BuildGlobals(req, collector)
		if err != nil {
			response.Fatal(rsp, errors.Wrapf(err, "building Starlark globals"))
			return rsp, nil
		}

		result, err := f.runtime.Execute(in.Spec.Source, globals)
		if err != nil {
			response.Fatal(rsp, errors.Wrapf(err, "starlark execution failed"))
			return rsp, nil
		}

		// Apply collected resources to response (merges with prior desired state).
		if err := builtins.ApplyResources(rsp, collector); err != nil {
			response.Fatal(rsp, errors.Wrapf(err, "applying composed resources"))
			return rsp, nil
		}

		// Apply dxr status changes to response desired composite.
		if err := builtins.ApplyDXR(rsp, result["dxr"]); err != nil {
			response.Fatal(rsp, errors.Wrapf(err, "applying dxr status"))
			return rsp, nil
		}

		response.Normal(rsp, "function-starlark: executed successfully")
	} else {
		response.Normal(rsp, "function-starlark: input parsed successfully (passthrough mode)")
	}

	return rsp, nil
}
