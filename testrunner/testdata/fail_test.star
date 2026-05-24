load("assert.star", "assert")

def test_canary_fail():
    assert.eq(1, 2)

def test_pass_before_fail():
    assert.eq(1, 1)
