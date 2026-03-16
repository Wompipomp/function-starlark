"""Smart resource naming with Kubernetes 63-character limit enforcement.

Generates resource names that fit within the K8s DNS-1123 label limit
of 63 characters, using hash-based truncation when needed.
"""

_MAX_K8S_NAME = 63
_HASH_LEN = 5
_MIN_XR_LEN = 8

def _sanitize(name):
    """Normalize a string to a valid DNS-1123 label component.

    Lowercases, replaces non-alphanumeric characters with hyphens,
    collapses consecutive hyphens, and strips leading/trailing hyphens.

    Args:
      name: String to sanitize

    Returns:
      DNS-1123 label-safe string
    """
    result = ""
    for c in name.elems():
        lc = c.lower()
        if ("a" <= lc and lc <= "z") or ("0" <= lc and lc <= "9"):
            result += lc
        else:
            result += "-"
    while "--" in result:
        result = result.replace("--", "-")
    return result.strip("-")

def hash_suffix(name, length = _HASH_LEN):
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
