"""Mock Azure CosmosDB schemas for e2e testing.

Simulates the generated schemas-azure package structure.
"""

ConsistencyPolicy = schema("ConsistencyPolicy",
    consistency_level=field(type="string", required=True, enum=["Eventual", "Session", "Strong", "BoundedStaleness"]),
    max_staleness_prefix=field(type="int", default=100),
    max_interval_in_seconds=field(type="int", default=5),
)

GeoLocation = schema("GeoLocation",
    location=field(type="string", required=True),
    failover_priority=field(type="int", required=True),
)

Account = schema("Account",
    location=field(type="string", required=True),
    offer_type=field(type="string", default="Standard"),
    kind=field(type="string", default="GlobalDocumentDB", enum=["GlobalDocumentDB", "MongoDB", "Parse"]),
    consistency_policy=field(type=ConsistencyPolicy),
    geo_locations=field(type="list"),
    tags=field(type="dict"),
)
