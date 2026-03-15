// Package metrics defines Prometheus metrics for function-starlark.
// All metrics register automatically with prometheus.DefaultRegisterer
// via promauto and are served by the function-sdk-go metrics endpoint.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ExecutionDurationSeconds measures Starlark script execution time
// (compilation/cache lookup + Init). Sub-second buckets align with
// typical script execution durations.
var ExecutionDurationSeconds = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "function_starlark_execution_duration_seconds",
		Help:    "Time spent executing Starlark scripts (compilation + Init).",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
	},
	[]string{"script"},
)

// ReconciliationDurationSeconds measures the full RunFunction handler
// duration from request to response.
var ReconciliationDurationSeconds = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "function_starlark_reconciliation_duration_seconds",
		Help:    "Total RunFunction handler duration.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	},
	[]string{"script"},
)

// OCIResolveDurationSeconds measures the time spent resolving OCI modules.
var OCIResolveDurationSeconds = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "function_starlark_oci_resolve_duration_seconds",
		Help:    "Time spent resolving OCI modules.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	},
	[]string{"script"},
)

// CacheHitsTotal counts bytecode cache hits, labeled by script filename.
var CacheHitsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "function_starlark_cache_hits_total",
		Help: "Total bytecode cache hits.",
	},
	[]string{"script"},
)

// CacheMissesTotal counts bytecode cache misses (compilations).
var CacheMissesTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "function_starlark_cache_misses_total",
		Help: "Total bytecode cache misses (compilations).",
	},
	[]string{"script"},
)

// ResourcesEmittedTotal counts composed resources emitted per reconciliation.
var ResourcesEmittedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "function_starlark_resources_emitted_total",
		Help: "Total composed resources emitted.",
	},
	[]string{"script"},
)

// ResourcesSkippedTotal counts resources skipped via skip_resource().
// Incremented once per unique resource name skipped (dedup-aware).
var ResourcesSkippedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "function_starlark_resources_skipped_total",
		Help: "Total resources skipped via skip_resource().",
	},
	[]string{"script"},
)

// ReconciliationsTotal counts RunFunction invocations.
var ReconciliationsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "function_starlark_reconciliations_total",
		Help: "Total RunFunction calls.",
	},
	[]string{"script"},
)
