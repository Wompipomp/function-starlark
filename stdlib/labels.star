"""Kubernetes and Crossplane label helpers for compositions.

Provides functions to generate standard Kubernetes recommended labels,
Crossplane-specific labels, and merge multiple label dicts.
"""

# Standard Kubernetes recommended label keys.
APP_NAME = "app.kubernetes.io/name"
APP_INSTANCE = "app.kubernetes.io/instance"
APP_VERSION = "app.kubernetes.io/version"
APP_COMPONENT = "app.kubernetes.io/component"
APP_PART_OF = "app.kubernetes.io/part-of"
APP_MANAGED_BY = "app.kubernetes.io/managed-by"

# Crossplane label keys.
XP_COMPOSITE = "crossplane.io/composite"
XP_CLAIM_NAME = "crossplane.io/claim-name"
XP_CLAIM_NAMESPACE = "crossplane.io/claim-namespace"

def standard_labels(name, instance = "", version = "", component = "", part_of = "", managed_by = "crossplane"):
    """Generate Kubernetes recommended labels.

    Only includes labels with non-empty values, except name and managed_by
    which are always included.

    Args:
      name: Application name (app.kubernetes.io/name)
      instance: Unique instance identifier (optional)
      version: Application version (optional)
      component: Component within architecture (optional)
      part_of: Higher-level application (optional)
      managed_by: Tool managing the resource (default: "crossplane")

    Returns:
      Dict of label key-value pairs with only non-empty values
    """
    labels = {APP_NAME: name, APP_MANAGED_BY: managed_by}
    if instance:
        labels[APP_INSTANCE] = instance
    if version:
        labels[APP_VERSION] = version
    if component:
        labels[APP_COMPONENT] = component
    if part_of:
        labels[APP_PART_OF] = part_of
    return labels

def crossplane_labels(composite_name = "", claim_name = "", claim_namespace = ""):
    """Generate Crossplane-specific labels.

    When parameters are empty, reads values from the observed composite
    resource (oxr) metadata.

    Args:
      composite_name: XR name override (reads from oxr if empty)
      claim_name: Claim name override (reads from oxr labels if empty)
      claim_namespace: Claim namespace override (reads from oxr labels if empty)

    Returns:
      Dict of Crossplane label key-value pairs
    """
    labels = {}
    name = composite_name if composite_name else get(oxr, "metadata.name", "")
    if name:
        labels[XP_COMPOSITE] = name
    cn = claim_name if claim_name else get(oxr, ["metadata", "labels", "crossplane.io/claim-name"], "")
    if cn:
        labels[XP_CLAIM_NAME] = cn
    cns = claim_namespace if claim_namespace else get(oxr, ["metadata", "labels", "crossplane.io/claim-namespace"], "")
    if cns:
        labels[XP_CLAIM_NAMESPACE] = cns
    return labels

def merge_labels(*label_dicts):
    """Merge multiple label dicts with later dicts overriding earlier ones.

    Creates a new dict by iterating through all provided dicts in order.
    Keys from later dicts override values from earlier ones.

    Args:
      *label_dicts: Variable number of label dicts to merge

    Returns:
      New merged dict of labels
    """
    result = {}
    for d in label_dicts:
        for k in d:
            result[k] = d[k]
    return result
