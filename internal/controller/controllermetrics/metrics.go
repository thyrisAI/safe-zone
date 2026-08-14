// Package controllermetrics owns control-plane-only Prometheus metrics.
// It intentionally uses controller-runtime's registry rather than the
// ext-proc data-plane registry.
package controllermetrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	reconcileDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "tsz_controller_reconcile_duration_seconds",
		Help: "Time spent reconciling TSZ control-plane resources.",
	}, []string{"controller", "result"})
	effectivePolicyConflicts = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tsz_controller_effective_policy_conflicts_total",
		Help: "Number of detected conflicting effective-policy attachments.",
	})
	policyActivations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "tsz_controller_policy_activations_total",
		Help: "Controller-managed Inline policy activation attempts.",
	}, []string{"result"})
	managedExtensionPolicies = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "tsz_controller_managed_extension_policies",
		Help: "Current number of EnvoyExtensionPolicies managed by TSZ.",
	})
)

func init() {
	ctrlmetrics.Registry.MustRegister(reconcileDuration, effectivePolicyConflicts, policyActivations, managedExtensionPolicies)
	// Initialize the normal success paths so all documented metric families are
	// discoverable before the first attachment is reconciled.
	reconcileDuration.WithLabelValues("tszguardrailpolicy", "success").Observe(0)
	policyActivations.WithLabelValues("success").Add(0)
}

func ObserveReconcile(controller, result string, duration time.Duration) {
	reconcileDuration.WithLabelValues(controller, result).Observe(duration.Seconds())
}

func IncEffectivePolicyConflict() { effectivePolicyConflicts.Inc() }

func IncPolicyActivation(result string) { policyActivations.WithLabelValues(result).Inc() }

func SetManagedExtensionPolicies(count int) { managedExtensionPolicies.Set(float64(count)) }
