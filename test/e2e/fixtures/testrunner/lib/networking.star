CIDRS = {
    "eu-west-1": "10.0.0.0/16",
    "us-east-1": "10.1.0.0/16",
    "ap-southeast-1": "10.2.0.0/16",
}

def vpc_cidr(region):
    cidr = CIDRS.get(region, None)
    if cidr == None:
        fail("unsupported region: " + region)
    return cidr

def subnet_cidr(region, index):
    base = vpc_cidr(region)
    parts = base.split(".")
    parts[2] = str(index)
    return ".".join(parts)
