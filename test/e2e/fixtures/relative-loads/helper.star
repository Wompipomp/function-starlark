# Sibling module loaded via load("./helper.star", "greet").
# Also tests nested relative chaining: loads ./utils.star internally.

load("./utils.star", "compute")

def greet(name):
    return "hello, " + name

nested_val = compute(9, 11)
