import { useEffect, useState } from 'react'
import { Outlet } from 'react-router-dom'
import Sidebar from './Sidebar'
import StatusBadge from '../components/StatusBadge'
import { getSystemStatus, type SystemStatus } from '../api/health'
import { useAuth } from '../context/AuthContext'
import './Layout.css'

const REFRESH_INTERVAL_MS = 30000

export default function AppLayout() {
  const { logout } = useAuth()
  const [status, setStatus] = useState<SystemStatus | 'loading'>('loading')
  const [isSidebarOpen, setIsSidebarOpen] = useState(false)

  useEffect(() => {
    let cancelled = false

    async function checkStatus() {
      const result = await getSystemStatus()
      if (!cancelled) {
        setStatus(result)
      }
    }

    checkStatus()
    const intervalId = setInterval(checkStatus, REFRESH_INTERVAL_MS)

    return () => {
      cancelled = true
      clearInterval(intervalId)
    }
  }, [])

  return (
    <div className="app-shell">
      <header className="app-header">
        <div className="app-header-left">
          {/* Hamburger button, visible on mobile only (via CSS). */}
          <button
            type="button"
            className="hamburger-button"
            aria-label={isSidebarOpen ? 'Close navigation menu' : 'Open navigation menu'}
            aria-expanded={isSidebarOpen}
            onClick={() => setIsSidebarOpen((open) => !open)}
          >
            ☰
          </button>
          <span className="app-header-title">Safe Zone</span>
        </div>
        <div className="app-header-right">
          <StatusBadge status={status} />
          <button type="button" className="button-secondary button-small" onClick={() => logout()}>
            Log out
          </button>
        </div>
      </header>
      <div className="app-body">
        <Sidebar isOpen={isSidebarOpen} onNavigate={() => setIsSidebarOpen(false)} />
        {/* Overlay to close the sidebar on outside click (mobile only). */}
        {isSidebarOpen && (
          <div
            className="sidebar-overlay"
            onClick={() => setIsSidebarOpen(false)}
            aria-hidden="true"
          />
        )}
        <main className="app-main">
          <Outlet />
        </main>
      </div>
    </div>
  )
}