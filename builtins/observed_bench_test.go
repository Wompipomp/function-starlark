package builtins

import (
	"fmt"
	"testing"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"go.starlark.net/starlark"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/wompipomp/function-starlark/convert"
)

// benchObservedRequest builds a request with n observed composed resources,
// each a realistic managed-resource body (metadata, spec.forProvider, status).
func benchObservedRequest(tb testing.TB, n int) *fnv1.RunFunctionRequest {
	resources := make(map[string]*fnv1.Resource, n)
	for i := range n {
		name := fmt.Sprintf("bucket-%d", i)
		body, err := structpb.NewStruct(map[string]any{
			"apiVersion": "s3.aws.upbound.io/v1beta1",
			"kind":       "Bucket",
			"metadata": map[string]any{
				"name":        name,
				"labels":      map[string]any{"env": "prod", "index": fmt.Sprintf("%d", i)},
				"annotations": map[string]any{"crossplane.io/external-name": name},
			},
			"spec": map[string]any{
				"forProvider": map[string]any{
					"region": "eu-west-1",
					"tags":   map[string]any{"managed-by": "starlark", "env": "prod"},
				},
			},
			"status": map[string]any{
				"atProvider": map[string]any{
					"arn": "arn:aws:s3:::" + name,
					"id":  name,
				},
			},
		})
		if err != nil {
			tb.Fatalf("structpb.NewStruct: %v", err)
		}
		resources[name] = &fnv1.Resource{Resource: body}
	}
	return &fnv1.RunFunctionRequest{Observed: &fnv1.State{Resources: resources}}
}

// BenchmarkBuildObservedDict measures building the observed dict for a
// composition with many observed resources where the script reads only a few.
// With lazy materialization, the bodies of unread resources are never
// converted, so cost tracks the number read rather than the number present.
func BenchmarkBuildObservedDict(b *testing.B) {
	for _, n := range []int{10, 50} {
		b.Run(fmt.Sprintf("resources=%d_read=2", n), func(b *testing.B) {
			req := benchObservedRequest(b, n)
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				observed, err := BuildObservedDict(req)
				if err != nil {
					b.Fatal(err)
				}
				// Simulate a script reading status.atProvider.id on 2 resources.
				for _, name := range []string{"bucket-0", "bucket-1"} {
					readStatusID(b, observed, name)
				}
			}
		})
	}
}

func readStatusID(b *testing.B, observed *convert.StarlarkDict, name string) {
	b.Helper()
	cur, found, err := observed.Get(starlark.String(name))
	if err != nil || !found {
		b.Fatalf("observed[%q]: found=%v err=%v", name, found, err)
	}
	for _, key := range []string{"status", "atProvider", "id"} {
		m, ok := cur.(starlark.Mapping)
		if !ok {
			b.Fatalf("%q: %s is not a mapping", name, cur.Type())
		}
		v, ok2, err := m.Get(starlark.String(key))
		if err != nil || !ok2 {
			b.Fatalf("%q.%s: found=%v err=%v", name, key, ok2, err)
		}
		cur = v
	}
}
