load("assert.star", "assert")
load("./tags.star", "standard_tags", "merge_tags")

def test_standard_tags():
    tags = standard_tags("db", "prod")
    assert.eq(tags["Name"], "acme-db-prod")
    assert.eq(tags["Environment"], "prod")
    assert.eq(tags["Team"], "platform")
    assert.eq(tags["ManagedBy"], "starlark")

def test_standard_tags_custom_team():
    tags = standard_tags("api", "staging", team = "backend")
    assert.eq(tags["Team"], "backend")

def test_merge_tags():
    base = {"a": "1", "b": "2"}
    result = merge_tags(base, {"b": "override", "c": "3"})
    assert.eq(result["a"], "1")
    assert.eq(result["b"], "override")
    assert.eq(result["c"], "3")

def test_merge_tags_empty_overrides():
    base = {"a": "1"}
    result = merge_tags(base, {})
    assert.eq(result["a"], "1")
    assert.eq(len(result), 1)

def test_standard_tags_uses_naming_module():
    tags = standard_tags("cache", "dev")
    assert.eq(tags["Name"], "acme-cache-dev")
