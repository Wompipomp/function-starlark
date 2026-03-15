"""Status condition helpers for Crossplane compositions.

Provides ergonomic wrappers around set_condition and emit_event for
common condition patterns: ready, not ready, degraded, and progress tracking.
"""

def ready(message = ""):
    """Set the composite resource as Ready/Available.

    Calls set_condition with type=Ready, status=True, reason=Available.

    Args:
      message: Optional human-readable status message

    Returns:
      None
    """
    set_condition(
        type = "Ready",
        status = "True",
        reason = "Available",
        message = message,
    )

def not_ready(reason, message = ""):
    """Set the composite resource as not ready.

    Calls set_condition with type=Ready, status=False.

    Args:
      reason: Machine-readable reason (e.g., "Creating", "Waiting")
      message: Optional human-readable message

    Returns:
      None
    """
    set_condition(
        type = "Ready",
        status = "False",
        reason = reason,
        message = message,
    )

def degraded(reason, message = ""):
    """Set the composite resource as degraded and emit a warning event.

    Calls both set_condition (with status=False) and emit_event with
    severity=Warning. Use for recoverable failures.

    Args:
      reason: Machine-readable reason (e.g., "DBFailing")
      message: Optional human-readable message

    Returns:
      None
    """
    set_condition(
        type = "Ready",
        status = "False",
        reason = reason,
        message = message,
    )
    emit_event(
        severity = "Warning",
        message = "Degraded: %s" % (message if message else reason),
    )

def progress(current, total, message = ""):
    """Set a progress condition for multi-resource compositions.

    Automatically generates a progress message and calls ready() when
    current >= total, or not_ready() with reason "InProgress" otherwise.

    Args:
      current: Number of ready resources
      total: Total number of expected resources
      message: Optional override message (default: "{current}/{total} resources ready")

    Returns:
      None
    """
    msg = message if message else "%d/%d resources ready" % (current, total)
    if current >= total:
        ready(msg)
    else:
        not_ready("InProgress", msg)
