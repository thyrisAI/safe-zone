export default function Overview() {
  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Overview</h1>
        <p className="page-subtitle">System activity and summary statistics</p>
      </div>

      <p className="info-message">
        Metrics are not available yet. This dashboard does not currently
        have a backend endpoint for request counts, allowed/blocked totals,
        or PII detection counts.
      </p>

      <div className="page-header" style={{ marginTop: '24px' }}>
        <h2 className="section-title">Recent Activity</h2>
      </div>
      <p className="info-message">
        No events recorded. Event history will appear here once the
        backend exposes a recent-events endpoint.
      </p>
    </div>
  )
}