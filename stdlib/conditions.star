"""Status condition helpers for Crossplane compositions.

Provides ergonomic wrappers around set_condition and emit_event for
signaling operational status. These set informational status conditions
on the composite resource — they do NOT control XR readiness.

XR readiness is determined by the Ready field on desired composed resources
(managed by function-auto-ready or the ready parameter on Resource()).
"""

def degraded(reason, message = ""):
    """Signal that the composite resource is degraded and emit a warning event.

    Calls both set_condition (with status=False) and emit_event with
    severity=Warning. Use for recoverable failures where the composition is
    impaired but not completely broken.

    Note: This sets an informational status condition. It does not control
    XR readiness — use function-auto-ready or Resource(..., ready=False)
    for that.

    Args:
      reason: Machine-readable reason (e.g., "DBFailing")
      message: Optional human-readable message

    Returns:
      None
    """
    set_condition(
        type = "Degraded",
        status = "True",
        reason = reason,
        message = message,
    )
    emit_event(
        severity = "Warning",
        message = "Degraded: %s" % (message if message else reason),
    )
