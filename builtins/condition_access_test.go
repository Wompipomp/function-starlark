package builtins

import (
	"strings"
	"testing"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"go.starlark.net/starlark"
	"google.golang.org/protobuf/types/known/structpb"
)

// makeObservedWithConditions creates an observed resource with status.conditions.
func makeObservedWithConditions(conditions ...*structpb.Value) map[string]*fnv1.Resource {
	return map[string]*fnv1.Resource{
		"db": {Resource: &structpb.Struct{Fields: map[string]*structpb.Value{
			"apiVersion": structpb.NewStringValue("v1"),
			"status": structpb.NewStructValue(&structpb.Struct{
				Fields: map[string]*structpb.Value{
					"conditions": structpb.NewListValue(&structpb.ListValue{Values: conditions}),
				},
			}),
		}}},
	}
}

func makeCondition(typ, status, reason, message, lastTransition string) *structpb.Value {
	fields := map[string]*structpb.Value{
		"type":   structpb.NewStringValue(typ),
		"status": structpb.NewStringValue(status),
	}
	if reason != "" {
		fields["reason"] = structpb.NewStringValue(reason)
	}
	if message != "" {
		fields["message"] = structpb.NewStringValue(message)
	}
	if lastTransition != "" {
		fields["lastTransitionTime"] = structpb.NewStringValue(lastTransition)
	}
	return structpb.NewStructValue(&structpb.Struct{Fields: fields})
}

func TestGetCondition_Found(t *testing.T) {
	observed := makeObservedWithConditions(
		makeCondition("Ready", "True", "Available", "Resource is ready", "2024-01-01T00:00:00Z"),
		makeCondition("Synced", "True", "ReconcileSuccess", "", ""),
	)
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		observed,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "get_condition",
		starlark.Tuple{starlark.String("db"), starlark.String("Ready")}, nil)
	if err != nil {
		t.Fatal(err)
	}

	d, ok := got.(*starlark.Dict)
	if !ok {
		t.Fatalf("got %T, want *starlark.Dict", got)
	}

	// Verify all 4 keys present.
	for _, key := range []string{"status", "reason", "message", "lastTransitionTime"} {
		v, found, _ := d.Get(starlark.String(key))
		if !found {
			t.Errorf("missing key %q", key)
			continue
		}
		_ = v
	}

	// Verify specific values.
	v, _, _ := d.Get(starlark.String("status"))
	vs, ok := v.(starlark.String)
	if !ok {
		t.Fatalf("status is %T, want starlark.String", v)
	}
	if vs != "True" {
		t.Errorf("status = %v, want True", vs)
	}
	v, _, _ = d.Get(starlark.String("reason"))
	vs, ok = v.(starlark.String)
	if !ok {
		t.Fatalf("reason is %T, want starlark.String", v)
	}
	if vs != "Available" {
		t.Errorf("reason = %v, want Available", vs)
	}
	v, _, _ = d.Get(starlark.String("message"))
	vs, ok = v.(starlark.String)
	if !ok {
		t.Fatalf("message is %T, want starlark.String", v)
	}
	if vs != "Resource is ready" {
		t.Errorf("message = %v, want 'Resource is ready'", vs)
	}
	v, _, _ = d.Get(starlark.String("lastTransitionTime"))
	vs, ok = v.(starlark.String)
	if !ok {
		t.Fatalf("lastTransitionTime is %T, want starlark.String", v)
	}
	if vs != "2024-01-01T00:00:00Z" {
		t.Errorf("lastTransitionTime = %v, want 2024-01-01T00:00:00Z", vs)
	}
}

func TestGetCondition_TypeNotFound(t *testing.T) {
	observed := makeObservedWithConditions(
		makeCondition("Ready", "True", "Available", "", ""),
	)
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		observed,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "get_condition",
		starlark.Tuple{starlark.String("db"), starlark.String("NonExistent")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != starlark.None {
		t.Errorf("got %v, want None", got)
	}
}

func TestGetCondition_ResourceNotFound(t *testing.T) {
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "get_condition",
		starlark.Tuple{starlark.String("missing"), starlark.String("Ready")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != starlark.None {
		t.Errorf("got %v, want None", got)
	}
}

func TestGetCondition_PartialFields_DefaultToEmptyString(t *testing.T) {
	// Condition with only type and status -- reason, message, lastTransitionTime missing.
	observed := makeObservedWithConditions(
		makeCondition("Ready", "False", "", "", ""),
	)
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		observed,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "get_condition",
		starlark.Tuple{starlark.String("db"), starlark.String("Ready")}, nil)
	if err != nil {
		t.Fatal(err)
	}

	d, ok := got.(*starlark.Dict)
	if !ok {
		t.Fatalf("got %T, want *starlark.Dict", got)
	}

	// status should be "False".
	v, _, _ := d.Get(starlark.String("status"))
	vs, ok := v.(starlark.String)
	if !ok {
		t.Fatalf("status is %T, want starlark.String", v)
	}
	if vs != "False" {
		t.Errorf("status = %v, want False", vs)
	}

	// reason, message, lastTransitionTime should be "".
	for _, key := range []string{"reason", "message", "lastTransitionTime"} {
		v, found, _ := d.Get(starlark.String(key))
		if !found {
			t.Errorf("missing key %q", key)
			continue
		}
		vs, ok := v.(starlark.String)
		if !ok {
			t.Fatalf("%s is %T, want starlark.String", key, v)
		}
		if vs != "" {
			t.Errorf("%s = %v, want empty string", key, vs)
		}
	}
}

func TestGetCondition_EmptyName_Errors(t *testing.T) {
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	_, err = callBuiltin(t, globals, "get_condition",
		starlark.Tuple{starlark.String(""), starlark.String("Ready")}, nil)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "name must not be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGetCondition_EmptyType_Errors(t *testing.T) {
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		nil,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	_, err = callBuiltin(t, globals, "get_condition",
		starlark.Tuple{starlark.String("db"), starlark.String("")}, nil)
	if err == nil {
		t.Fatal("expected error for empty type")
	}
	if !strings.Contains(err.Error(), "type must not be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGetCondition_ReturnedDictIsMutable(t *testing.T) {
	observed := makeObservedWithConditions(
		makeCondition("Ready", "True", "Available", "", ""),
	)
	req := makeReq(
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		map[string]*structpb.Value{"apiVersion": structpb.NewStringValue("v1")},
		observed,
	)
	c := NewCollector(NewConditionCollector(), "test.star", nil)
	globals, err := testBuildGlobals(req, c)
	if err != nil {
		t.Fatal(err)
	}

	got, err := callBuiltin(t, globals, "get_condition",
		starlark.Tuple{starlark.String("db"), starlark.String("Ready")}, nil)
	if err != nil {
		t.Fatal(err)
	}

	d, ok := got.(*starlark.Dict)
	if !ok {
		t.Fatalf("got %T, want *starlark.Dict", got)
	}

	// Should be mutable (not frozen).
	if err := d.SetKey(starlark.String("custom"), starlark.String("value")); err != nil {
		t.Errorf("returned dict should be mutable: %v", err)
	}
}
