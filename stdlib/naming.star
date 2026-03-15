"""Smart resource naming with Kubernetes 63-character limit enforcement.

Generates resource names that fit within the K8s DNS-1123 label limit
of 63 characters, using hash-based truncation when needed.
"""

_MAX_K8S_NAME = 63
_HASH_LEN = 5

def _hash_suffix(name, length = _HASH_LEN):
    """Generate a short deterministic hash suffix from a string.

    Uses base-36 encoding of the built-in hash() value to produce a
    compact, deterministic suffix for name truncation.

    Args:
      name: String to hash
      length: Desired suffix length (default: 5)

    Returns:
      Base-36 encoded hash string of the specified length
    """
    h = hash(name)
    if h < 0:
        h = -h
    chars = "0123456789abcdefghijklmnopqrstuvwxyz"
    result = ""
    while h > 0 and len(result) < length:
        result = chars[h % 36] + result
        h = h // 36
    while len(result) < length:
        result = "0" + result
    return result

def resource_name(suffix, xr_name = None):
    """Generate a Kubernetes-safe resource name.

    Combines the XR name with a suffix, automatically truncating and
    appending a hash suffix if the result would exceed 63 characters.
    When xr_name is not provided, reads from oxr metadata.

    Args:
      suffix: Resource type suffix (e.g., "bucket", "rds")
      xr_name: XR name override. If None, reads from oxr metadata.

    Returns:
      String name that fits within 63 characters
    """
    if xr_name == None:
        xr_name = get(oxr, "metadata.name", "xr")
    candidate = "%s-%s" % (xr_name, suffix)
    if len(candidate) <= _MAX_K8S_NAME:
        return candidate
    # Truncate and add hash for uniqueness.
    full_name = candidate
    max_prefix = _MAX_K8S_NAME - _HASH_LEN - 1  # -1 for separator dash
    truncated = candidate[:max_prefix].rstrip("-")
    return "%s-%s" % (truncated, _hash_suffix(full_name))
