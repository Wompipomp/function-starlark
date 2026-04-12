"""Status condition helpers for Crossplane compositions.

Provides ergonomic wrappers around set_condition and emit_event for
signaling operational status. These set informational status conditions
on the composite resource — they do NOT control XR readiness.

XR readiness is determined by the Ready field on desired composed resources
(managed by function-auto-ready or the ready parameter on Resource()).
"""

def degraded(reason, message = ""):
    """Signal that the composite resource is degraded and emit a warning event.

    Calls both set_condition (with type="Degraded", status="True") and emit_event with
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

def all_ready(resources = None):
    """Return True iff every resource has a Ready=True condition.

    Args:
      resources: List of resource name strings to check, or None to check all observed.
                 An explicit empty list returns True (vacuous truth).
                 None with zero observed returns False (first reconciliation).

    Returns:
      bool
    """
    if resources != None:
        names = resources
    else:
        names = list(observed.keys())
    if len(names) == 0:
        return resources != None  # [] -> True (vacuous), None with 0 observed -> False
    for name in names:
        cond = get_condition(name, "Ready")
        if cond == None or cond["status"] != "True":
            return False
    return True

def any_degraded(resources = None):
    """Return True iff any resource has Ready=False or Synced=False.

    Args:
      resources: List of resource name strings to check, or None to check all observed.
                 An explicit empty list returns False.
                 Resources with no conditions are not considered degraded.

    Returns:
      bool
    """
    if resources != None:
        names = resources
    else:
        names = list(observed.keys())
    for name in names:
        ready = get_condition(name, "Ready")
        if ready != None and ready["status"] == "False":
            return True
        synced = get_condition(name, "Synced")
        if synced != None and synced["status"] == "False":
            return True
    return False
