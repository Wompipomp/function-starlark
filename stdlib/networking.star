"""Networking utilities for Crossplane compositions.

Provides CIDR math and IP address manipulation functions equivalent to
Terraform's cidrsubnet(), cidrhost(), and related functions.
"""

def _parse_ip(ip):
    """Parse and validate a dotted-quad IP address string.

    Args:
      ip: IP address string (e.g., "192.168.1.0")

    Returns:
      List of four integer octets
    """
    if not regex.match(r"^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$", ip):
        fail("invalid IP address: %s" % ip)
    parts = ip.split(".")
    octets = []
    for p in parts:
        octet = int(p)
        if octet < 0 or octet > 255:
            fail("invalid octet %d in IP: %s" % (octet, ip))
        octets.append(octet)
    return octets

def _parse_cidr(cidr):
    """Parse a CIDR string into IP integer and prefix length.

    Args:
      cidr: CIDR string (e.g., "10.0.0.0/16")

    Returns:
      Tuple of (ip_int, prefix_length)
    """
    if not regex.match(r"^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}/\d{1,2}$", cidr):
        fail("invalid CIDR: %s (expected ip/prefix)" % cidr)
    parts = cidr.split("/")
    prefix = int(parts[1])
    if prefix < 0 or prefix > 32:
        fail("invalid prefix length %d in CIDR: %s" % (prefix, cidr))
    return ip_to_int(parts[0]), prefix

def _mask(prefix):
    """Compute a 32-bit netmask from a prefix length.

    Args:
      prefix: Prefix length (0-32)

    Returns:
      Integer netmask
    """
    if prefix == 0:
        return 0
    return ~((1 << (32 - prefix)) - 1) & 0xFFFFFFFF

def ip_to_int(ip):
    """Convert a dotted-quad IP address string to an integer.

    Args:
      ip: IP address string (e.g., "192.168.1.0")

    Returns:
      Integer representation of the IP address
    """
    octets = _parse_ip(ip)
    result = 0
    for o in octets:
        result = (result << 8) + o
    return result

def int_to_ip(n):
    """Convert an integer to a dotted-quad IP address string.

    Args:
      n: Integer IP address

    Returns:
      Dotted-quad IP string (e.g., "192.168.1.0")
    """
    return "%d.%d.%d.%d" % (
        (n >> 24) & 0xFF,
        (n >> 16) & 0xFF,
        (n >> 8) & 0xFF,
        n & 0xFF,
    )

def network_address(cidr):
    """Compute the network address from a CIDR string.

    Args:
      cidr: CIDR string (e.g., "10.0.1.5/24")

    Returns:
      Network address as a dotted-quad string (e.g., "10.0.1.0")
    """
    ip_int, prefix = _parse_cidr(cidr)
    return int_to_ip(ip_int & _mask(prefix))

def broadcast_address(cidr):
    """Compute the broadcast address from a CIDR string.

    Args:
      cidr: CIDR string (e.g., "10.0.1.0/24")

    Returns:
      Broadcast address as a dotted-quad string (e.g., "10.0.1.255")
    """
    ip_int, prefix = _parse_cidr(cidr)
    host_mask = (1 << (32 - prefix)) - 1
    network = ip_int & _mask(prefix)
    return int_to_ip(network | host_mask)

def subnet_cidr(base_cidr, new_bits, subnet_num):
    """Calculate a subnet CIDR from a base CIDR.

    Equivalent to Terraform's cidrsubnet() function. Divides the base
    network into smaller subnets by adding new_bits to the prefix length.

    Args:
      base_cidr: Base CIDR string (e.g., "10.0.0.0/16")
      new_bits: Number of additional prefix bits (e.g., 8 for /24 from /16)
      subnet_num: Subnet index number (0-based)

    Returns:
      Subnet CIDR string (e.g., "10.0.0.0/24")
    """
    base_ip, base_prefix = _parse_cidr(base_cidr)
    new_prefix = base_prefix + new_bits
    if new_prefix > 32:
        fail("prefix %d + new_bits %d exceeds 32" % (base_prefix, new_bits))
    max_subnets = 1 << new_bits
    if subnet_num < 0 or subnet_num >= max_subnets:
        fail("subnet_num %d out of range [0, %d)" % (subnet_num, max_subnets))
    network = base_ip & _mask(base_prefix)
    subnet_ip = network + (subnet_num << (32 - new_prefix))
    return "%s/%d" % (int_to_ip(subnet_ip), new_prefix)

def cidr_contains(cidr, ip):
    """Check if an IP address is within a CIDR range.

    Args:
      cidr: CIDR string (e.g., "10.0.0.0/16")
      ip: IP address string to check (e.g., "10.0.1.5")

    Returns:
      True if the IP is within the CIDR range, False otherwise
    """
    net_int, prefix = _parse_cidr(cidr)
    ip_int = ip_to_int(ip)
    m = _mask(prefix)
    return (ip_int & m) == (net_int & m)
