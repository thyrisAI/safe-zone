import { useEffect, useState } from 'react'
import { getDashboardConfig } from '../api/dashboard'
import { ApiError } from '../api/client'
import type { DashboardConfig } from '../types/dashboard'
import StatusPill from '../components/StatusPill'

type LoadState = 'loading' | 'success' | 'error' | 'unauthorized'

export default function Configuration() {
  const [config, setConfig] = useState<DashboardConfig | null>(null)
  const [loadState, setLoadState] = useState<LoadState>('loading')

  useEffect(() => {
    let cancelled = false

    async function loadConfig() {
      setLoadState('loading')
      try {
        const data = await getDashboardConfig()
        if (cancelled) return

        setConfig(data)
        setLoadState('success')
      } catch (err) {
        if (cancelled) return

        if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
          setLoadState('unauthorized')
        } else {
          setLoadState('error')
        }
      }
    }

    loadConfig()
    return () => {
      cancelled = true
    }
  }, [])

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Configuration</h1>
        <p className="page-subtitle">Read-only view of current system settings</p>
      </div>

      {loadState === 'loading' && <p className="info-message">Loading configuration...</p>}

      {loadState === 'unauthorized' && (
        <p className="info-message">
          You don't have permission to view this data. Contact your administrator if you believe this is an error.
        </p>
      )}

      {loadState === 'error' && (
        <p className="info-message">
          Unable to load configuration. Please check your connection and try again.
        </p>
      )}

      {loadState === 'success' && config && (
        <>
          <p className="info-message">
            Sensitive values such as API keys, tokens, and database credentials are never displayed here.
          </p>

          <div className="config-section">
            <h2 className="section-title">General</h2>
            <dl className="config-list">
              <div className="config-row">
                <dt>PII Mode</dt>
                <dd className="config-mono">{config.pii_mode}</dd>
              </div>
              <div className="config-row">
                <dt>Gateway Block Mode</dt>
                <dd className="config-mono">{config.gateway_block_mode}</dd>
              </div>
              <div className="config-row">
                <dt>App Mode</dt>
                <dd className="config-mono">{config.app_mode}</dd>
              </div>
            </dl>
          </div>

          <div className="config-section">
            <h2 className="section-title">AI Provider</h2>
            <dl className="config-list">
              <div className="config-row">
                <dt>Provider Type</dt>
                <dd className="config-mono">{config.ai_provider}</dd>
              </div>
              <div className="config-row">
                <dt>Model Name</dt>
                <dd className="config-mono">{config.ai_model_name}</dd>
              </div>
            </dl>
          </div>

          <div className="config-section">
            <h2 className="section-title">Security</h2>
            <dl className="config-list">
              <div className="config-row">
                <dt>Security Headers Enabled</dt>
                <dd><StatusPill active={config.security_headers_enabled} activeLabel="On" inactiveLabel="Off" /></dd>
              </div>
              <div className="config-row">
                <dt>CORS Enabled</dt>
                <dd><StatusPill active={config.cors_enabled} activeLabel="On" inactiveLabel="Off" /></dd>
              </div>
              <div className="config-row">
                <dt>Auth Enabled</dt>
                <dd><StatusPill active={config.auth_enabled} activeLabel="On" inactiveLabel="Off" /></dd>
              </div>
              <div className="config-row">
                <dt>Rate Limiting Enabled</dt>
                <dd><StatusPill active={config.rate_limit_enabled} activeLabel="On" inactiveLabel="Off" /></dd>
              </div>
            </dl>
          </div>

          <div className="config-section">
            <h2 className="section-title">Limits</h2>
            <dl className="config-list">
              <div className="config-row">
                <dt>Max Request Size</dt>
                <dd className="config-mono">{Math.round(config.max_request_size_bytes / 1024 / 1024)} MB</dd>
              </div>
              <div className="config-row">
                <dt>Detect Timeout</dt>
                <dd className="config-mono">{config.handler_timeout_detect_seconds * 1000} ms</dd>
              </div>
              <div className="config-row">
                <dt>Chat Timeout</dt>
                <dd className="config-mono">{config.handler_timeout_chat_seconds * 1000} ms</dd>
              </div>
            </dl>
          </div>

          <p>
            <a href="https://github.com/thyrisAI/safe-zone/blob/main/docs/API_REFERENCE.md" target="_blank" rel="noopener noreferrer">View full API documentation</a>
          </p>
        </>
      )}
    </div>
  )
}