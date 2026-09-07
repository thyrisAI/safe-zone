// Package observability contains the small, transport-safe metrics emitted by
// the external processor itself. No request or response content is recorded.
package observability

import "github.com/prometheus/client_golang/prometheus"

// ResponseStateMetrics records response callbacks which cannot be paired with
// a request-side stream state. outcome is deliberately a bounded enum.
type ResponseStateMetrics struct {
	counter *prometheus.CounterVec
}

func NewResponseStateMetrics(registerer prometheus.Registerer) (*ResponseStateMetrics, error) {
	metrics := &ResponseStateMetrics{counter: prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "tsz",
		Subsystem: "extproc",
		Name:      "response_without_request_state_total",
		Help:      "Response callbacks received without pinned request stream state.",
	}, []string{"outcome"})}
	if err := registerer.Register(metrics.counter); err != nil {
		return nil, err
	}
	return metrics, nil
}

func (metrics *ResponseStateMetrics) ObserveResponseWithoutRequestState(outcome string) {
	metrics.counter.WithLabelValues(outcome).Inc()
}
