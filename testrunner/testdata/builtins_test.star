load("assert.star", "assert")

def test_json():
    assert.eq(json.encode({"a": 1}), '{"a":1}')

def test_yaml():
    result = yaml.encode({"key": "value"})
    assert.contains(result, "key:")

def test_crypto():
    h = crypto.sha256("hello")
    assert.eq(len(h), 64)

def test_encoding():
    encoded = encoding.b64enc("hello")
    assert.eq(encoding.b64dec(encoded), "hello")

def test_regex():
    assert.true(regex.match(r"^hello", "hello world"))

def test_dict_merge():
    result = dict.merge({"a": 1}, {"b": 2})
    assert.eq(result["a"], 1)
    assert.eq(result["b"], 2)

def test_struct():
    s = struct(name = "test", value = 42)
    assert.eq(s.name, "test")
    assert.eq(s.value, 42)

def test_get():
    obj = {"a": {"b": {"c": 1}}}
    assert.eq(get(obj, "a.b.c"), 1)
    assert.eq(get(obj, "a.x", "default"), "default")

def test_schema():
    Point = schema("Point",
        x = field(type = "int"),
        y = field(type = "int"),
    )
    p = Point(x = 1, y = 2)
    assert.eq(p.x, 1)
    assert.eq(p.y, 2)
