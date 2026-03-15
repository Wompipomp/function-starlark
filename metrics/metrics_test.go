package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsRegistered(t *testing.T) {
	// Verify all 8 metrics are accessible and registered by calling
	// WithLabelValues on each. If a metric were nil or unregistered,
	// this would panic.
	metrics := []struct {
		name string
		call func()
	}{
		{"ExecutionDurationSeconds", func() { ExecutionDurationSeconds.WithLabelValues("reg-test.star") }},
		{"ReconciliationDurationSeconds", func() { ReconciliationDurationSeconds.WithLabelValues("reg-test.star") }},
		{"OCIResolveDurationSeconds", func() { OCIResolveDurationSeconds.WithLabelValues("reg-test.star") }},
		{"CacheHitsTotal", func() { CacheHitsTotal.WithLabelValues("reg-test.star") }},
		{"CacheMissesTotal", func() { CacheMissesTotal.WithLabelValues("reg-test.star") }},
		{"ResourcesEmittedTotal", func() { ResourcesEmittedTotal.WithLabelValues("reg-test.star") }},
		{"ResourcesSkippedTotal", func() { ResourcesSkippedTotal.WithLabelValues("reg-test.star") }},
		{"ReconciliationsTotal", func() { ReconciliationsTotal.WithLabelValues("reg-test.star") }},
	}
	for _, m := range metrics {
		t.Run(m.name, func(t *testing.T) {
			m.call() // panics if not registered
		})
	}
}

func TestCacheHitsTotalNaming(t *testing.T) {
	expected := `
		# HELP function_starlark_cache_hits_total Total bytecode cache hits.
		# TYPE function_starlark_cache_hits_total counter
		function_starlark_cache_hits_total{script="naming-test.star"} 0
	`
	// Initialize the label to create the series.
	CacheHitsTotal.WithLabelValues("naming-test.star")

	if err := testutil.CollectAndCompare(CacheHitsTotal, strings.NewReader(expected)); err != nil {
		t.Errorf("cache hits naming: %v", err)
	}
}

func TestCacheMissesTotalNaming(t *testing.T) {
	expected := `
		# HELP function_starlark_cache_misses_total Total bytecode cache misses (compilations).
		# TYPE function_starlark_cache_misses_total counter
		function_starlark_cache_misses_total{script="naming-test.star"} 0
	`
	CacheMissesTotal.WithLabelValues("naming-test.star")

	if err := testutil.CollectAndCompare(CacheMissesTotal, strings.NewReader(expected)); err != nil {
		t.Errorf("cache misses naming: %v", err)
	}
}

func TestResourcesEmittedTotalNaming(t *testing.T) {
	expected := `
		# HELP function_starlark_resources_emitted_total Total composed resources emitted.
		# TYPE function_starlark_resources_emitted_total counter
		function_starlark_resources_emitted_total{script="naming-test.star"} 0
	`
	ResourcesEmittedTotal.WithLabelValues("naming-test.star")

	if err := testutil.CollectAndCompare(ResourcesEmittedTotal, strings.NewReader(expected)); err != nil {
		t.Errorf("resources emitted naming: %v", err)
	}
}

func TestResourcesSkippedTotalNaming(t *testing.T) {
	expected := `
		# HELP function_starlark_resources_skipped_total Total resources skipped via skip_resource().
		# TYPE function_starlark_resources_skipped_total counter
		function_starlark_resources_skipped_total{script="naming-test.star"} 0
	`
	ResourcesSkippedTotal.WithLabelValues("naming-test.star")

	if err := testutil.CollectAndCompare(ResourcesSkippedTotal, strings.NewReader(expected)); err != nil {
		t.Errorf("resources skipped naming: %v", err)
	}
}

func TestReconciliationsTotalNaming(t *testing.T) {
	expected := `
		# HELP function_starlark_reconciliations_total Total RunFunction calls.
		# TYPE function_starlark_reconciliations_total counter
		function_starlark_reconciliations_total{script="naming-test.star"} 0
	`
	ReconciliationsTotal.WithLabelValues("naming-test.star")

	if err := testutil.CollectAndCompare(ReconciliationsTotal, strings.NewReader(expected)); err != nil {
		t.Errorf("reconciliations total naming: %v", err)
	}
}

func TestExecutionDurationSecondsNaming(t *testing.T) {
	ch := make(chan *prometheus.Desc, 1)
	ExecutionDurationSeconds.Describe(ch)
	d := <-ch
	s := d.String()
	if !strings.Contains(s, "function_starlark_execution_duration_seconds") {
		t.Errorf("ExecutionDurationSeconds desc = %q, want it to contain metric name", s)
	}
}

func TestReconciliationDurationSecondsNaming(t *testing.T) {
	ch := make(chan *prometheus.Desc, 1)
	ReconciliationDurationSeconds.Describe(ch)
	d := <-ch
	s := d.String()
	if !strings.Contains(s, "function_starlark_reconciliation_duration_seconds") {
		t.Errorf("ReconciliationDurationSeconds desc = %q, want it to contain metric name", s)
	}
}

func TestOCIResolveDurationSecondsNaming(t *testing.T) {
	ch := make(chan *prometheus.Desc, 1)
	OCIResolveDurationSeconds.Describe(ch)
	d := <-ch
	s := d.String()
	if !strings.Contains(s, "function_starlark_oci_resolve_duration_seconds") {
		t.Errorf("OCIResolveDurationSeconds desc = %q, want it to contain metric name", s)
	}
}
