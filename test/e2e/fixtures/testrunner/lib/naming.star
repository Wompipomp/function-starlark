PREFIX = "acme"

def resource_name(component, env):
    return PREFIX + "-" + component + "-" + env

def fqdn(component, env, domain = "internal"):
    return resource_name(component, env) + "." + domain
