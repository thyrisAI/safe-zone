export default function Events() {
  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Events</h1>
        <p className="page-subtitle">Recent request activity (since last restart)</p>
      </div>

      <p className="info-message">
        No events recorded. This dashboard does not currently have a
        backend endpoint for recent request events. Once available, this
        table will show the timestamp, request ID, result, and reason for
        each recent request.
      </p>
    </div>
  )
}
