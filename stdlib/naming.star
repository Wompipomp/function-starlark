"""Smart resource naming with Kubernetes 63-character limit enforcement.

Generates resource names that fit within the K8s DNS-1123 label limit
of 63 characters, using hash-based truncation when needed.
"""

_MAX_K8S_NAME = 63
_HASH_LEN = 5
_MIN_XR_LEN = 8

def _sanitize(name):
    """Normalize a string to a valid DNS-1123 label component.

    Uses regex-based normalization: lowercases, replaces runs of
    non-alphanumeric characters with a single hyphen, and strips
    leading/trailing hyphens.

    Args:
      name: String to sanitize

    Returns:
      DNS-1123 label-safe string
    """
    result = regex.replace_all(r"[^a-z0-9]+", name.lower(), "-")
    return result.strip("-")

def hash_suffix(name, length = _HASH_LEN):
    """Generate a short deterministic hex hash suffix from a string.

    Delegates to crypto.stable_id which returns the first `length`
    hex characters of a SHA-256 digest, providing a compact and
    deterministic suffix for name truncation.

    Args:
      name: String to hash
      length: Desired suffix length (default: 5)

    Returns:
      Hex hash string of the specified length
    """
    return crypto.stable_id(name, length = length)

def resource_name(suffix, xr_name = None):
    """Generate a Kubernetes-safe resource name.

    Combines the XR name with a suffix, automatically truncating the XR
    name and appending a hash if the result would exceed 63 characters.
    The suffix is always preserved so the resource type remains visible.
    When xr_name is not provided, reads from oxr metadata.

    Args:
      suffix: Resource type suffix (e.g., "bucket", "rds")
      xr_name: XR name override. If None, reads from oxr metadata.

    Returns:
      String name that fits within 63 characters
    """
    if xr_name == None:
        xr_name = get(oxr, "metadata.name", "xr")
    xr_name = _sanitize(xr_name) or "xr"
    suffix = _sanitize(suffix) or "resource"
    candidate = "%s-%s" % (xr_name, suffix)
    if len(candidate) <= _MAX_K8S_NAME:
        return candidate
    # Truncate XR name, preserve suffix, add hash for uniqueness.
    # Format: {xr_truncated}-{suffix}-{hash}
    full_name = candidate
    max_xr = _MAX_K8S_NAME - len(suffix) - _HASH_LEN - 2  # 2 dashes
    if max_xr < _MIN_XR_LEN:
        max_xr = _MIN_XR_LEN
    truncated_xr = xr_name[:max_xr].rstrip("-")
    return "%s-%s-%s" % (truncated_xr, suffix, hash_suffix(full_name))
