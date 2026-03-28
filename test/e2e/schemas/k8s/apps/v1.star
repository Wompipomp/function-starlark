"""Mock Kubernetes apps/v1 schemas for e2e testing.

Simulates the generated schemas-k8s package structure.
"""

ContainerPort = schema("ContainerPort",
    container_port=field(type="int", required=True),
    protocol=field(type="string", default="TCP", enum=["TCP", "UDP"]),
)

Container = schema("Container",
    name=field(type="string", required=True),
    image=field(type="string", required=True),
    ports=field(type="list"),
)

PodSpec = schema("PodSpec",
    containers=field(type="list", required=True),
    restart_policy=field(type="string", default="Always", enum=["Always", "OnFailure", "Never"]),
)

DeploymentSpec = schema("DeploymentSpec",
    replicas=field(type="int", default=1),
    selector=field(type="dict", required=True),
    template=field(type="dict", required=True),
)

Deployment = schema("Deployment",
    metadata=field(type="dict", required=True),
    spec=field(type=DeploymentSpec, required=True),
)

StatefulSetSpec = schema("StatefulSetSpec",
    replicas=field(type="int", default=1),
    service_name=field(type="string", required=True),
    selector=field(type="dict", required=True),
    template=field(type="dict", required=True),
)

StatefulSet = schema("StatefulSet",
    metadata=field(type="dict", required=True),
    spec=field(type=StatefulSetSpec, required=True),
)
