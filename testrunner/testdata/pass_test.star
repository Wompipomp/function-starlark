load("assert.star", "assert")

def test_equality():
    assert.eq(1, 1)
    assert.eq("hello", "hello")

def test_inequality():
    assert.ne(1, 2)

def test_true():
    assert.true(True)
    assert.true(1 > 0)

# Non-test function -- should be ignored by runner
def helper():
    return 42
