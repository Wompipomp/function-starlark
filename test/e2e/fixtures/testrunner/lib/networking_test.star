load("assert.star", "assert")
load("./networking.star", "vpc_cidr", "subnet_cidr", "CIDRS")

def test_vpc_cidr_known_region():
    assert.eq(vpc_cidr("eu-west-1"), "10.0.0.0/16")
    assert.eq(vpc_cidr("us-east-1"), "10.1.0.0/16")

def test_vpc_cidr_unknown_region():
    assert.fails(lambda: vpc_cidr("mars-1"), "unsupported region")

def test_subnet_cidr():
    assert.eq(subnet_cidr("eu-west-1", 5), "10.0.5.0/16")
    assert.eq(subnet_cidr("us-east-1", 0), "10.1.0.0/16")

def test_cidrs_count():
    assert.eq(len(CIDRS), 3)
