load("./naming.star", "resource_name")

def standard_tags(component, env, team = "platform"):
    return {
        "Name": resource_name(component, env),
        "Environment": env,
        "Team": team,
        "ManagedBy": "starlark",
    }

def merge_tags(base, overrides):
    result = dict(base)
    result.update(overrides)
    return result
