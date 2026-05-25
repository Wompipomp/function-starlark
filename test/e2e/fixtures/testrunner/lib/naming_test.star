load("assert.star", "assert")
load("./naming.star", "resource_name", "fqdn", "PREFIX")

def test_resource_name():
    assert.eq(resource_name("db", "prod"), "acme-db-prod")
    assert.eq(resource_name("api", "staging"), "acme-api-staging")

def test_fqdn_default_domain():
    assert.eq(fqdn("db", "prod"), "acme-db-prod.internal")

def test_fqdn_custom_domain():
    assert.eq(fqdn("db", "prod", domain = "example.com"), "acme-db-prod.example.com")

def test_prefix_constant():
    assert.eq(PREFIX, "acme")
