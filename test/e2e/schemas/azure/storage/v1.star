"""Mock Azure Storage schemas for e2e testing.

Simulates the generated schemas-azure package structure.
"""

NetworkRules = schema("NetworkRules",
    default_action=field(type="string", default="Deny", enum=["Allow", "Deny"]),
    bypass=field(type="list"),
)

Account = schema("Account",
    location=field(type="string", required=True),
    account_tier=field(type="string", default="Standard", enum=["Standard", "Premium"]),
    account_replication_type=field(type="string", default="LRS", enum=["LRS", "GRS", "ZRS", "GZRS"]),
    account_kind=field(type="string", default="StorageV2"),
    min_tls_version=field(type="string", default="TLS1_2"),
    network_rules=field(type=NetworkRules),
    tags=field(type="dict"),
)
