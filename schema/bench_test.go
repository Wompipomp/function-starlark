package schema

import (
	"testing"

	"go.starlark.net/starlark"
)

// BenchmarkSchemaConstruct20Fields measures the overhead of schema constructor
// invocation (validation + dict creation) for a realistic 20-field schema.
// Schema definition happens outside the loop (one-time cost per module load);
// only the constructor call is benchmarked (hot path per reconciliation).
//
// Expected ns/op should be well under 1,000,000 (1ms), verifying PERF-01.
// The actual threshold is not asserted programmatically -- CI benchstat handles
// regression detection.
func BenchmarkSchemaConstruct20Fields(b *testing.B) {
	// Build 20 FieldDescriptors with a realistic type distribution:
	//   8 string, 4 int, 3 bool, 3 list, 2 dict
	fields := map[string]*FieldDescriptor{
		"name":        {typeName: "string", defVal: starlark.None},
		"namespace":   {typeName: "string", defVal: starlark.None},
		"apiVersion":  {typeName: "string", defVal: starlark.None},
		"kind":        {typeName: "string", defVal: starlark.None},
		"region":      {typeName: "string", defVal: starlark.None},
		"tier":        {typeName: "string", defVal: starlark.None},
		"owner":       {typeName: "string", defVal: starlark.None},
		"description": {typeName: "string", defVal: starlark.None},
		"replicas":    {typeName: "int", defVal: starlark.None},
		"port":        {typeName: "int", defVal: starlark.None},
		"minCount":    {typeName: "int", defVal: starlark.None},
		"maxCount":    {typeName: "int", defVal: starlark.None},
		"enabled":     {typeName: "bool", defVal: starlark.None},
		"public":      {typeName: "bool", defVal: starlark.None},
		"encrypted":   {typeName: "bool", defVal: starlark.None},
		"tags":        {typeName: "list", defVal: starlark.None},
		"volumes":     {typeName: "list", defVal: starlark.None},
		"ports":       {typeName: "list", defVal: starlark.None},
		"metadata":    {typeName: "dict", defVal: starlark.None},
		"annotations": {typeName: "dict", defVal: starlark.None},
	}
	order := []string{
		"name", "namespace", "apiVersion", "kind",
		"region", "tier", "owner", "description",
		"replicas", "port", "minCount", "maxCount",
		"enabled", "public", "encrypted",
		"tags", "volumes", "ports",
		"metadata", "annotations",
	}

	sc := &SchemaCallable{
		name:   "Workload",
		fields: fields,
		order:  order,
	}

	// Build kwargs matching the 20 fields -- values match expected types.
	kwargs := []starlark.Tuple{
		{starlark.String("name"), starlark.String("my-workload")},
		{starlark.String("namespace"), starlark.String("default")},
		{starlark.String("apiVersion"), starlark.String("apps/v1")},
		{starlark.String("kind"), starlark.String("Deployment")},
		{starlark.String("region"), starlark.String("us-east-1")},
		{starlark.String("tier"), starlark.String("standard")},
		{starlark.String("owner"), starlark.String("platform-team")},
		{starlark.String("description"), starlark.String("Primary workload")},
		{starlark.String("replicas"), starlark.MakeInt(3)},
		{starlark.String("port"), starlark.MakeInt(8080)},
		{starlark.String("minCount"), starlark.MakeInt(1)},
		{starlark.String("maxCount"), starlark.MakeInt(10)},
		{starlark.String("enabled"), starlark.True},
		{starlark.String("public"), starlark.False},
		{starlark.String("encrypted"), starlark.True},
		{starlark.String("tags"), starlark.NewList([]starlark.Value{starlark.String("prod"), starlark.String("critical")})},
		{starlark.String("volumes"), starlark.NewList([]starlark.Value{starlark.String("/data"), starlark.String("/logs")})},
		{starlark.String("ports"), starlark.NewList([]starlark.Value{starlark.MakeInt(80), starlark.MakeInt(443)})},
		{starlark.String("metadata"), makeDict(kv("app", starlark.String("web")), kv("version", starlark.String("v2")))},
		{starlark.String("annotations"), makeDict(kv("managed-by", starlark.String("starlark")))},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_, err := sc.CallInternal(nil, nil, kwargs)
		if err != nil {
			b.Fatal(err)
		}
	}
}
