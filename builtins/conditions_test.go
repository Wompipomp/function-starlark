package builtins

import (
	"errors"
	"strings"
	"testing"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"go.starlark.net/starlark"
)

// ---------------------------------------------------------------------------
// ConditionCollector / set_condition tests
// ---------------------------------------------------------------------------

func TestConditionCollector_NewEmpty(t *testing.T) {
	cc := NewConditionCollector()
	if cc == nil {
		t.Fatal("NewConditionCollector returned nil")
	}
	if len(cc.Conditions()) != 0 {
		t.Errorf("Conditions() = %d, want 0", len(cc.Conditions()))
	}
	if len(cc.Events()) != 0 {
		t.Errorf("Events() = %d, want 0", len(cc.Events()))
	}
}

func TestSetCondition_Basic(t *testing.T) {
	cc := NewConditionCollector()
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, cc.SetConditionBuiltin(), starlark.Tuple{
		starlark.String("DatabaseReady"),
		starlark.String("True"),
		starlark.String("Available"),
		starlark.String("All databases healthy"),
	}, nil)
	if err != nil {
		t.Fatalf("set_condition error: %v", err)
	}

	conditions := cc.Conditions()
	if len(conditions) != 1 {
		t.Fatalf("Conditions() = %d, want 1", len(conditions))
	}
	c := conditions[0]
	if c.Type != "DatabaseReady" {
		t.Errorf("Type = %q, want 'DatabaseReady'", c.Type)
	}
	if c.Status != "True" {
		t.Errorf("Status = %q, want 'True'", c.Status)
	}
	if c.Reason != "Available" {
		t.Errorf("Reason = %q, want 'Available'", c.Reason)
	}
	if c.Message != "All databases healthy" {
		t.Errorf("Message = %q, want 'All databases healthy'", c.Message)
	}
	if c.Target != "Composite" {
		t.Errorf("Target = %q, want 'Composite' (default)", c.Target)
	}
}

func TestSetCondition_WithTarget(t *testing.T) {
	cc := NewConditionCollector()
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, cc.SetConditionBuiltin(), starlark.Tuple{
		starlark.String("Ready"),
		starlark.String("False"),
		starlark.String("NotReady"),
		starlark.String("Still provisioning"),
	}, []starlark.Tuple{
		{starlark.String("target"), starlark.String("CompositeAndClaim")},
	})
	if err != nil {
		t.Fatalf("set_condition error: %v", err)
	}

	c := cc.Conditions()[0]
	if c.Target != "CompositeAndClaim" {
		t.Errorf("Target = %q, want 'CompositeAndClaim'", c.Target)
	}
}

func TestSetCondition_ReservedType(t *testing.T) {
	cc := NewConditionCollector()
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, cc.SetConditionBuiltin(), starlark.Tuple{
		starlark.String(CompositeReadyConditionType),
		starlark.String("False"),
		starlark.String("WhyNot"),
		starlark.String("msg"),
	}, nil)
	if err == nil {
		t.Fatal("set_condition with reserved type should error")
	}
	if !strings.Contains(err.Error(), CompositeReadyConditionType) ||
		!strings.Contains(err.Error(), "set_composite_ready") {
		t.Errorf("error = %v, want it to name the reserved type and suggest set_composite_ready()", err)
	}
	if got := cc.Conditions(); len(got) != 0 {
		t.Errorf("expected no conditions recorded on reserved-type rejection, got %+v", got)
	}
}

func TestSetCondition_MissingArgs(t *testing.T) {
	cc := NewConditionCollector()
	thread := new(starlark.Thread)

	// Missing required positional args
	_, err := starlark.Call(thread, cc.SetConditionBuiltin(), starlark.Tuple{
		starlark.String("DatabaseReady"),
	}, nil)
	if err == nil {
		t.Fatal("set_condition with missing args should error")
	}
}

func TestSetCondition_Multiple(t *testing.T) {
	cc := NewConditionCollector()
	thread := new(starlark.Thread)

	for _, typ := range []string{"Ready", "Synced", "Available"} {
		_, err := starlark.Call(thread, cc.SetConditionBuiltin(), starlark.Tuple{
			starlark.String(typ),
			starlark.String("True"),
			starlark.String("OK"),
			starlark.String(""),
		}, nil)
		if err != nil {
			t.Fatalf("set_condition(%q) error: %v", typ, err)
		}
	}

	if len(cc.Conditions()) != 3 {
		t.Errorf("Conditions() = %d, want 3", len(cc.Conditions()))
	}
}

// ---------------------------------------------------------------------------
// emit_event tests
// ---------------------------------------------------------------------------

func TestEmitEvent_Normal(t *testing.T) {
	cc := NewConditionCollector()
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, cc.EmitEventBuiltin(), starlark.Tuple{
		starlark.String("Normal"),
		starlark.String("Resource reconciled"),
	}, nil)
	if err != nil {
		t.Fatalf("emit_event error: %v", err)
	}

	events := cc.Events()
	if len(events) != 1 {
		t.Fatalf("Events() = %d, want 1", len(events))
	}
	e := events[0]
	if e.Severity != "Normal" {
		t.Errorf("Severity = %q, want 'Normal'", e.Severity)
	}
	if e.Message != "Resource reconciled" {
		t.Errorf("Message = %q, want 'Resource reconciled'", e.Message)
	}
	if e.Target != "Composite" {
		t.Errorf("Target = %q, want 'Composite' (default)", e.Target)
	}
}

func TestEmitEvent_Warning(t *testing.T) {
	cc := NewConditionCollector()
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, cc.EmitEventBuiltin(), starlark.Tuple{
		starlark.String("Warning"),
		starlark.String("Resource degraded"),
	}, nil)
	if err != nil {
		t.Fatalf("emit_event error: %v", err)
	}

	e := cc.Events()[0]
	if e.Severity != "Warning" {
		t.Errorf("Severity = %q, want 'Warning'", e.Severity)
	}
}

func TestEmitEvent_WithTarget(t *testing.T) {
	cc := NewConditionCollector()
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, cc.EmitEventBuiltin(), starlark.Tuple{
		starlark.String("Normal"),
		starlark.String("Event for claim"),
	}, []starlark.Tuple{
		{starlark.String("target"), starlark.String("CompositeAndClaim")},
	})
	if err != nil {
		t.Fatalf("emit_event error: %v", err)
	}

	e := cc.Events()[0]
	if e.Target != "CompositeAndClaim" {
		t.Errorf("Target = %q, want 'CompositeAndClaim'", e.Target)
	}
}

func TestEmitEvent_InvalidSeverity(t *testing.T) {
	cc := NewConditionCollector()
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, cc.EmitEventBuiltin(), starlark.Tuple{
		starlark.String("Error"),
		starlark.String("some message"),
	}, nil)
	if err == nil {
		t.Fatal("emit_event with invalid severity should error")
	}
	if !strings.Contains(err.Error(), "Normal") || !strings.Contains(err.Error(), "Warning") {
		t.Errorf("error %q should mention valid severities", err.Error())
	}
}

// ---------------------------------------------------------------------------
// fatal tests
// ---------------------------------------------------------------------------

func TestFatal_ReturnsFatalError(t *testing.T) {
	cc := NewConditionCollector()
	thread := new(starlark.Thread)

	_, err := starlark.Call(thread, cc.FatalBuiltin(), starlark.Tuple{
		starlark.String("cannot proceed"),
	}, nil)
	if err == nil {
		t.Fatal("fatal() should return an error")
	}

	var fatalErr *FatalError
	if !errors.As(err, &fatalErr) {
		t.Fatalf("error is %T, want *FatalError", err)
	}
	if fatalErr.Message != "cannot proceed" {
		t.Errorf("Message = %q, want 'cannot proceed'", fatalErr.Message)
	}
}

func TestFatalError_ErrorInterface(t *testing.T) {
	fe := &FatalError{Message: "test message"}
	if fe.Error() != "test message" {
		t.Errorf("Error() = %q, want 'test message'", fe.Error())
	}
}

func TestFatalError_ErrorsAs(t *testing.T) {
	// Verify errors.As can extract *FatalError from wrapped errors.
	inner := &FatalError{Message: "inner fatal"}
	wrapped := errors.Join(errors.New("wrapper"), inner)

	var fatalErr *FatalError
	if !errors.As(wrapped, &fatalErr) {
		t.Fatal("errors.As should find *FatalError in wrapped error")
	}
	if fatalErr.Message != "inner fatal" {
		t.Errorf("Message = %q, want 'inner fatal'", fatalErr.Message)
	}
}

// ---------------------------------------------------------------------------
// ApplyConditions tests
// ---------------------------------------------------------------------------

func TestApplyConditions_Empty(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{}
	ApplyConditions(rsp, nil)
	if rsp.Conditions != nil {
		t.Error("ApplyConditions with empty conditions should not set conditions")
	}
}

func TestApplyConditions_TrueStatus(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{}
	ApplyConditions(rsp, []CollectedCondition{
		{Type: "Ready", Status: "True", Reason: "Available", Message: "All good"},
	})

	if len(rsp.Conditions) != 1 {
		t.Fatalf("Conditions = %d, want 1", len(rsp.Conditions))
	}
	c := rsp.Conditions[0]
	if c.Type != "Ready" {
		t.Errorf("Type = %q, want 'Ready'", c.Type)
	}
	if c.Status != fnv1.Status_STATUS_CONDITION_TRUE {
		t.Errorf("Status = %v, want STATUS_CONDITION_TRUE", c.Status)
	}
	if c.Reason != "Available" {
		t.Errorf("Reason = %q, want 'Available'", c.Reason)
	}
	if c.GetMessage() != "All good" {
		t.Errorf("Message = %q, want 'All good'", c.GetMessage())
	}
}

func TestApplyConditions_FalseStatus(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{}
	ApplyConditions(rsp, []CollectedCondition{
		{Type: "Ready", Status: "False", Reason: "NotReady", Message: ""},
	})

	c := rsp.Conditions[0]
	if c.Status != fnv1.Status_STATUS_CONDITION_FALSE {
		t.Errorf("Status = %v, want STATUS_CONDITION_FALSE", c.Status)
	}
}

func TestApplyConditions_UnknownStatus(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{}
	ApplyConditions(rsp, []CollectedCondition{
		{Type: "Ready", Status: "Unknown", Reason: "Checking"},
	})

	c := rsp.Conditions[0]
	if c.Status != fnv1.Status_STATUS_CONDITION_UNKNOWN {
		t.Errorf("Status = %v, want STATUS_CONDITION_UNKNOWN", c.Status)
	}
}

func TestApplyConditions_WithMessage(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{}
	ApplyConditions(rsp, []CollectedCondition{
		{Type: "Ready", Status: "True", Reason: "OK", Message: "Everything is fine"},
	})

	c := rsp.Conditions[0]
	if c.GetMessage() != "Everything is fine" {
		t.Errorf("Message = %q, want 'Everything is fine'", c.GetMessage())
	}
}

func TestApplyConditions_EmptyMessage(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{}
	ApplyConditions(rsp, []CollectedCondition{
		{Type: "Ready", Status: "True", Reason: "OK", Message: ""},
	})

	c := rsp.Conditions[0]
	// When message is empty, it should not be set (nil pointer).
	if c.Message != nil {
		t.Errorf("Message should be nil for empty message, got %q", c.GetMessage())
	}
}

func TestApplyConditions_TargetCompositeAndClaim(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{}
	ApplyConditions(rsp, []CollectedCondition{
		{Type: "Ready", Status: "True", Reason: "OK", Target: "CompositeAndClaim"},
	})

	c := rsp.Conditions[0]
	if c.GetTarget() != fnv1.Target_TARGET_COMPOSITE_AND_CLAIM {
		t.Errorf("Target = %v, want TARGET_COMPOSITE_AND_CLAIM", c.GetTarget())
	}
}

func TestApplyConditions_DefaultTarget(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{}
	ApplyConditions(rsp, []CollectedCondition{
		{Type: "Ready", Status: "True", Reason: "OK", Target: "Composite"},
	})

	c := rsp.Conditions[0]
	if c.GetTarget() != fnv1.Target_TARGET_COMPOSITE {
		t.Errorf("Target = %v, want TARGET_COMPOSITE", c.GetTarget())
	}
}

// ---------------------------------------------------------------------------
// ApplyEvents tests
// ---------------------------------------------------------------------------

func TestApplyEvents_Empty(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{}
	ApplyEvents(rsp, nil)
	if rsp.Results != nil {
		t.Error("ApplyEvents with empty events should not set results")
	}
}

func TestApplyEvents_Normal(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{}
	ApplyEvents(rsp, []CollectedEvent{
		{Severity: "Normal", Message: "Reconciled successfully"},
	})

	if len(rsp.Results) != 1 {
		t.Fatalf("Results = %d, want 1", len(rsp.Results))
	}
	r := rsp.Results[0]
	if r.Severity != fnv1.Severity_SEVERITY_NORMAL {
		t.Errorf("Severity = %v, want SEVERITY_NORMAL", r.Severity)
	}
	if r.Message != "Reconciled successfully" {
		t.Errorf("Message = %q, want 'Reconciled successfully'", r.Message)
	}
}

func TestApplyEvents_Warning(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{}
	ApplyEvents(rsp, []CollectedEvent{
		{Severity: "Warning", Message: "Resource degraded"},
	})

	r := rsp.Results[0]
	if r.Severity != fnv1.Severity_SEVERITY_WARNING {
		t.Errorf("Severity = %v, want SEVERITY_WARNING", r.Severity)
	}
	if r.Message != "Resource degraded" {
		t.Errorf("Message = %q, want 'Resource degraded'", r.Message)
	}
}

func TestApplyEvents_TargetCompositeAndClaim(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{}
	ApplyEvents(rsp, []CollectedEvent{
		{Severity: "Normal", Message: "msg", Target: "CompositeAndClaim"},
	})

	r := rsp.Results[0]
	if r.GetTarget() != fnv1.Target_TARGET_COMPOSITE_AND_CLAIM {
		t.Errorf("Target = %v, want TARGET_COMPOSITE_AND_CLAIM", r.GetTarget())
	}
}

func TestApplyEvents_DefaultTarget(t *testing.T) {
	rsp := &fnv1.RunFunctionResponse{}
	ApplyEvents(rsp, []CollectedEvent{
		{Severity: "Normal", Message: "msg", Target: "Composite"},
	})

	r := rsp.Results[0]
	if r.GetTarget() != fnv1.Target_TARGET_COMPOSITE {
		t.Errorf("Target = %v, want TARGET_COMPOSITE", r.GetTarget())
	}
}

// ---------------------------------------------------------------------------
// Conditions/Events copy-out tests
// ---------------------------------------------------------------------------

func TestConditions_ReturnsCopy(t *testing.T) {
	cc := NewConditionCollector()
	thread := new(starlark.Thread)

	_, _ = starlark.Call(thread, cc.SetConditionBuiltin(), starlark.Tuple{
		starlark.String("Ready"),
		starlark.String("True"),
		starlark.String("OK"),
		starlark.String("fine"),
	}, nil)

	c1 := cc.Conditions()
	c2 := cc.Conditions()
	c1[0].Type = "modified"
	if c2[0].Type == "modified" {
		t.Error("Conditions() should return a copy")
	}
}

func TestEvents_ReturnsCopy(t *testing.T) {
	cc := NewConditionCollector()
	thread := new(starlark.Thread)

	_, _ = starlark.Call(thread, cc.EmitEventBuiltin(), starlark.Tuple{
		starlark.String("Normal"),
		starlark.String("msg"),
	}, nil)

	e1 := cc.Events()
	e2 := cc.Events()
	e1[0].Severity = "modified"
	if e2[0].Severity == "modified" {
		t.Error("Events() should return a copy")
	}
}
