export default function Configuration() {
  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Configuration</h1>
        <p className="page-subtitle">Read-only view of current system settings</p>
      </div>

      <p className="info-message">
        Configuration details are not available yet. This dashboard does not currently have a backend endpoint exposing safe (non-sensitive) configuration values. Sensitive values such as API keys, tokens, and database credentials will never be displayed here, even once this feature is implemented.
      </p>

      <p>
        <a href="https://github.com/thyrisAI/safe-zone/blob/main/docs/API_REFERENCE.md" target="_blank" rel="noopener noreferrer">View full API documentation</a>
      </p>
    </div>
  )
}