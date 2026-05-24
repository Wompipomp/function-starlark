load("assert.star", "assert")

def test_normal():
    assert.eq(1, 1)

def test_with_param(x):
    assert.eq(x, 1)
