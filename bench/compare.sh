#!/usr/bin/env bash
# Comparative benchmark: function-starlark vs function-go-templating vs function-kcl
#
# Usage:
#   ./bench/compare.sh              # Run all 3 (10-resource workload)
#   ./bench/compare.sh starlark     # Run only starlark
#   ./bench/compare.sh 50           # Run all 3 with 50-resource workload
#
# Prerequisites:
#   docker build . --tag=runtime
#   docker pull xpkg.upbound.io/upbound/function-go-templating:v0.11.4
#   docker pull xpkg.upbound.io/crossplane-contrib/function-kcl:v0.12.0

set -euo pipefail
cd "$(dirname "$0")/.."

RESOURCE_COUNT="${1:-10}"
ITERATIONS=500
WARMUP=10

IMAGE_STARLARK="runtime"
IMAGE_GOTEMPLATE="xpkg.upbound.io/upbound/function-go-templating:v0.11.4"
IMAGE_KCL="xpkg.upbound.io/crossplane-contrib/function-kcl:v0.12.0"

# Check Docker
if ! docker info >/dev/null 2>&1; then
    echo "ERROR: Docker is not running"
    exit 1
fi

cleanup() {
    echo ""
    echo "Cleaning up containers..."
    docker rm -f bench-starlark bench-gotemplate bench-kcl 2>/dev/null || true
}
trap cleanup EXIT

start_function() {
    local name=$1 image=$2 port=$3
    echo "Starting $name on port $port..."
    docker run -d --rm --name "$name" -p "127.0.0.1:${port}:9443" "$image" --insecure >/dev/null 2>&1
}

wait_ready() {
    local name=$1 port=$2 timeout=${3:-30}
    local deadline=$((SECONDS + timeout))
    echo -n "  Waiting for $name..."
    while [ $SECONDS -lt $deadline ]; do
        if grpcurl -plaintext "127.0.0.1:${port}" list >/dev/null 2>&1; then
            echo " ready"
            return 0
        fi
        sleep 0.5
    done
    echo " TIMEOUT"
    return 1
}

bench_grpcurl() {
    local name=$1 port=$2 request_file=$3 count=$4
    local total_ns=0
    local min_ns=999999999999
    local max_ns=0

    # Warmup
    for _ in $(seq 1 $WARMUP); do
        grpcurl -plaintext -d "@${request_file}" \
            "127.0.0.1:${port}" \
            apiextensions.fn.proto.v1.FunctionRunnerService/RunFunction >/dev/null 2>&1
    done

    # Benchmark
    local times=()
    for _ in $(seq 1 "$count"); do
        local start end elapsed
        start=$(date +%s%N 2>/dev/null || python3 -c 'import time; print(int(time.time()*1e9))')
        grpcurl -plaintext -d "@${request_file}" \
            "127.0.0.1:${port}" \
            apiextensions.fn.proto.v1.FunctionRunnerService/RunFunction >/dev/null 2>&1
        end=$(date +%s%N 2>/dev/null || python3 -c 'import time; print(int(time.time()*1e9))')
        elapsed=$((end - start))
        times+=("$elapsed")
        total_ns=$((total_ns + elapsed))
        if [ "$elapsed" -lt "$min_ns" ]; then min_ns=$elapsed; fi
        if [ "$elapsed" -gt "$max_ns" ]; then max_ns=$elapsed; fi
    done

    local avg_ns=$((total_ns / count))
    local avg_ms
    avg_ms=$(echo "scale=2; $avg_ns / 1000000" | bc)
    local min_ms
    min_ms=$(echo "scale=2; $min_ns / 1000000" | bc)
    local max_ms
    max_ms=$(echo "scale=2; $max_ns / 1000000" | bc)

    # Compute p50 and p99
    IFS=$'\n' sorted=($(sort -n <<<"${times[*]}")); unset IFS
    local p50_idx=$(( count * 50 / 100 ))
    local p99_idx=$(( count * 99 / 100 ))
    local p50_ms p99_ms
    p50_ms=$(echo "scale=2; ${sorted[$p50_idx]} / 1000000" | bc)
    p99_ms=$(echo "scale=2; ${sorted[$p99_idx]} / 1000000" | bc)

    printf "  %-20s avg=%6s ms  p50=%6s ms  p99=%6s ms  min=%6s ms  max=%6s ms\n" \
        "$name" "$avg_ms" "$p50_ms" "$p99_ms" "$min_ms" "$max_ms"
}

# --- Generate request payloads ---
generate_starlark_request() {
    local n=$1
    local script
    script=$(cat <<STAR
region = get(oxr, "spec.region", "us-east-1")
name = get(oxr, "metadata.name", "bench")
for i in range(${n}):
    Resource("bucket-%d" % i, {
        "apiVersion": "s3.aws.upbound.io/v1beta1",
        "kind": "Bucket",
        "metadata": {"name": "%s-bucket-%d" % (name, i), "labels": {"env": "prod", "index": str(i)}},
        "spec": {"forProvider": {"region": region, "tags": {"ManagedBy": "crossplane"}}},
    }, labels=None)
set_condition(type="Ready", status="True", reason="Available", message="done")
dxr["status"] = {"count": ${n}}
STAR
)
    python3 -c "
import json, sys
req = {
    'meta': {'tag': 'bench'},
    'input': {
        'apiVersion': 'starlark.fn.crossplane.io/v1alpha1',
        'kind': 'StarlarkInput',
        'spec': {'source': '''${script}'''}
    },
    'observed': {
        'composite': {
            'resource': {
                'apiVersion': 'example.crossplane.io/v1',
                'kind': 'XBucket',
                'metadata': {'name': 'bench-xr'},
                'spec': {'region': 'us-east-1'}
            }
        }
    }
}
json.dump(req, sys.stdout)
" > /tmp/bench-starlark.json
}

generate_gotemplate_request() {
    local n=$1
    local tmpl
    tmpl=$(cat <<'TMPL'
{{- $region := .observed.composite.resource.spec.region | default "us-east-1" -}}
{{- $name := .observed.composite.resource.metadata.name | default "bench" -}}
TMPL
)
    # Append the range dynamically
    tmpl+=$(printf '\n{{- range $i := until %d }}\n---\napiVersion: s3.aws.upbound.io/v1beta1\nkind: Bucket\nmetadata:\n  annotations:\n    crossplane.io/composition-resource-name: bucket-{{ $i }}\n  name: {{ $name }}-bucket-{{ $i }}\n  labels:\n    env: prod\n    index: "{{ $i }}"\nspec:\n  forProvider:\n    region: {{ $region }}\n    tags:\n      ManagedBy: crossplane\n{{- end }}' "$n")

    python3 -c "
import json, sys
req = {
    'meta': {'tag': 'bench'},
    'input': {
        'apiVersion': 'gotemplating.fn.crossplane.io/v1beta1',
        'kind': 'GoTemplate',
        'source': 'Inline',
        'inline': {'template': '''${tmpl}'''}
    },
    'observed': {
        'composite': {
            'resource': {
                'apiVersion': 'example.crossplane.io/v1',
                'kind': 'XBucket',
                'metadata': {'name': 'bench-xr'},
                'spec': {'region': 'us-east-1'}
            }
        }
    }
}
json.dump(req, sys.stdout)
" > /tmp/bench-gotemplate.json
}

generate_kcl_request() {
    local n=$1
    local kcl_src
    kcl_src=$(cat <<KCL
oxr = option("params").oxr
_region = oxr.spec.get("region", "us-east-1")
_name = oxr.metadata.name or "bench"
_items = [{
    apiVersion: "s3.aws.upbound.io/v1beta1"
    kind: "Bucket"
    metadata: {
        name: "{}-bucket-{}".format(_name, i)
        annotations: {"crossplane.io/composition-resource-name": "bucket-{}".format(i)}
        labels: {env: "prod", index: str(i)}
    }
    spec.forProvider: {region: _region, tags: {ManagedBy: "crossplane"}}
} for i in range(${n})]
items = _items
KCL
)
    python3 -c "
import json, sys
req = {
    'meta': {'tag': 'bench'},
    'input': {
        'apiVersion': 'krm.kcl.dev/v1alpha1',
        'kind': 'KCLInput',
        'spec': {'source': '''${kcl_src}'''}
    },
    'observed': {
        'composite': {
            'resource': {
                'apiVersion': 'example.crossplane.io/v1',
                'kind': 'XBucket',
                'metadata': {'name': 'bench-xr'},
                'spec': {'region': 'us-east-1'}
            }
        }
    }
}
json.dump(req, sys.stdout)
" > /tmp/bench-kcl.json
}

# --- Memory measurement ---
measure_memory() {
    local name=$1
    local mem
    mem=$(docker stats --no-stream --format '{{.MemUsage}}' "$name" 2>/dev/null | awk '{print $1}')
    printf "  %-20s %s\n" "$name" "$mem"
}

# --- Main ---
echo "============================================"
echo "  Crossplane Function Benchmark"
echo "  Resources: ${RESOURCE_COUNT}  Iterations: ${ITERATIONS}"
echo "============================================"
echo ""

# Check for grpcurl
if ! command -v grpcurl >/dev/null 2>&1; then
    echo "ERROR: grpcurl is required. Install: brew install grpcurl"
    exit 1
fi

# Build starlark image
echo "Building function-starlark..."
docker build . --tag=runtime -q >/dev/null 2>&1

# Generate request payloads
echo "Generating request payloads for ${RESOURCE_COUNT} resources..."
generate_starlark_request "$RESOURCE_COUNT"
generate_gotemplate_request "$RESOURCE_COUNT"
generate_kcl_request "$RESOURCE_COUNT"

# Start containers
echo ""
start_function bench-starlark "$IMAGE_STARLARK" 19443
start_function bench-gotemplate "$IMAGE_GOTEMPLATE" 19444
start_function bench-kcl "$IMAGE_KCL" 19445

wait_ready bench-starlark 19443
wait_ready bench-gotemplate 19444
wait_ready bench-kcl 19445 60

# Memory at idle (after startup)
echo ""
echo "--- Memory (after startup) ---"
measure_memory bench-starlark
measure_memory bench-gotemplate
measure_memory bench-kcl

# Run benchmarks
echo ""
echo "--- Latency (${ITERATIONS} iterations, ${WARMUP} warmup) ---"
bench_grpcurl "function-starlark" 19443 /tmp/bench-starlark.json "$ITERATIONS"
bench_grpcurl "function-go-template" 19444 /tmp/bench-gotemplate.json "$ITERATIONS"
bench_grpcurl "function-kcl" 19445 /tmp/bench-kcl.json "$ITERATIONS"

# Memory after benchmark
echo ""
echo "--- Memory (after benchmark) ---"
measure_memory bench-starlark
measure_memory bench-gotemplate
measure_memory bench-kcl

echo ""
echo "Done."
