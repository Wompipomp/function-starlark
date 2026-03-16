package builtins

import (
	"errors"
	"fmt"
	"sync"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/response"
	"go.starlark.net/starlark"
)

// CollectedCondition holds a single condition accumulated by set_condition.
type CollectedCondition struct {
	Type, Status, Reason, Message, Target string
}

// CollectedEvent holds a single event accumulated by emit_event.
type CollectedEvent struct {
	Severity, Message, Target string
}

// FatalError is returned by the fatal() builtin to halt Starlark execution.
type FatalError struct {
	Message string
}

// Error implements the error interface.
func (e *FatalError) Error() string { return e.Message }

// ConditionCollector accumulates conditions and events from Starlark scripts.
// Fatal errors are not collected -- fatal() returns an error immediately to
// halt execution.
type ConditionCollector struct {
	mu         sync.Mutex
	conditions []CollectedCondition
	events     []CollectedEvent
}

// NewConditionCollector creates an empty ConditionCollector.
func NewConditionCollector() *ConditionCollector {
	return &ConditionCollector{}
}

// SetConditionBuiltin returns a *starlark.Builtin for set_condition.
func (cc *ConditionCollector) SetConditionBuiltin() *starlark.Builtin {
	return starlark.NewBuiltin("set_condition", cc.setConditionFn)
}

// EmitEventBuiltin returns a *starlark.Builtin for emit_event.
func (cc *ConditionCollector) EmitEventBuiltin() *starlark.Builtin {
	return starlark.NewBuiltin("emit_event", cc.emitEventFn)
}

// FatalBuiltin returns a *starlark.Builtin for fatal.
func (cc *ConditionCollector) FatalBuiltin() *starlark.Builtin {
	return starlark.NewBuiltin("fatal", cc.fatalFn)
}

// Conditions returns a copy of all collected conditions.
func (cc *ConditionCollector) Conditions() []CollectedCondition {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	out := make([]CollectedCondition, len(cc.conditions))
	copy(out, cc.conditions)
	return out
}

// Events returns a copy of all collected events.
func (cc *ConditionCollector) Events() []CollectedEvent {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	out := make([]CollectedEvent, len(cc.events))
	copy(out, cc.events)
	return out
}

// AddEvent appends an event to the collector. Used by fn.go post-processing
// (creation sequencing) to emit events outside of Starlark execution.
func (cc *ConditionCollector) AddEvent(e CollectedEvent) {
	cc.mu.Lock()
	cc.events = append(cc.events, e)
	cc.mu.Unlock()
}

// setConditionFn implements set_condition(type, status, reason, message, target="Composite").
func (cc *ConditionCollector) setConditionFn(
	_ *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var typ, status, reason, message string
	target := "Composite"

	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"type", &typ, "status", &status, "reason", &reason,
		"message", &message, "target?", &target); err != nil {
		return nil, err
	}

	cc.mu.Lock()
	cc.conditions = append(cc.conditions, CollectedCondition{
		Type:    typ,
		Status:  status,
		Reason:  reason,
		Message: message,
		Target:  target,
	})
	cc.mu.Unlock()

	return starlark.None, nil
}

// emitEventFn implements emit_event(severity, message, target="Composite").
func (cc *ConditionCollector) emitEventFn(
	_ *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var severity, message string
	target := "Composite"

	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"severity", &severity, "message", &message, "target?", &target); err != nil {
		return nil, err
	}

	if severity != "Normal" && severity != "Warning" {
		return nil, fmt.Errorf("emit_event: severity must be \"Normal\" or \"Warning\", got %q", severity)
	}

	cc.mu.Lock()
	cc.events = append(cc.events, CollectedEvent{
		Severity: severity,
		Message:  message,
		Target:   target,
	})
	cc.mu.Unlock()

	return starlark.None, nil
}

// fatalFn implements fatal(message). It returns a *FatalError to halt
// Starlark execution immediately.
func (cc *ConditionCollector) fatalFn(
	_ *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var message string

	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"message", &message); err != nil {
		return nil, err
	}

	return nil, &FatalError{Message: message}
}

// ApplyConditions maps collected conditions to the response using SDK helpers.
// It is a no-op when conditions is empty.
func ApplyConditions(rsp *fnv1.RunFunctionResponse, conditions []CollectedCondition) {
	for _, c := range conditions {
		var opt *response.ConditionOption
		switch c.Status {
		case "True":
			opt = response.ConditionTrue(rsp, c.Type, c.Reason)
		case "False":
			opt = response.ConditionFalse(rsp, c.Type, c.Reason)
		default:
			opt = response.ConditionUnknown(rsp, c.Type, c.Reason)
		}
		if c.Message != "" {
			opt.WithMessage(c.Message)
		}
		if c.Target == "CompositeAndClaim" {
			opt.TargetCompositeAndClaim()
		}
	}
}

// ApplyEvents maps collected events to the response using SDK helpers.
// It is a no-op when events is empty.
func ApplyEvents(rsp *fnv1.RunFunctionResponse, events []CollectedEvent) {
	for _, e := range events {
		var opt *response.ResultOption
		switch e.Severity {
		case "Normal":
			opt = response.Normal(rsp, e.Message)
		case "Warning":
			opt = response.Warning(rsp, errors.New(e.Message))
		default:
			continue
		}
		if e.Target == "CompositeAndClaim" {
			opt.TargetCompositeAndClaim()
		}
	}
}
