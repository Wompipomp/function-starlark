package main

import (
	"context"

	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
)

// Function implements the Crossplane composition function.
// This is a temporary stub replaced in Plan 02 with the real implementation.
type Function struct {
	fnv1.UnimplementedFunctionRunnerServiceServer
	log logging.Logger
}

// RunFunction processes a RunFunctionRequest.
// Stub - replaced in Plan 02.
func (f *Function) RunFunction(_ context.Context, _ *fnv1.RunFunctionRequest) (*fnv1.RunFunctionResponse, error) {
	return nil, nil // Stub - replaced in Plan 02
}
