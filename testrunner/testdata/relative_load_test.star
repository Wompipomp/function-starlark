load("assert.star", "assert")
load("./helpers.star", "greet", "add")

def test_greet():
    assert.eq(greet("world"), "hello world")

def test_add():
    assert.eq(add(2, 3), 5)
