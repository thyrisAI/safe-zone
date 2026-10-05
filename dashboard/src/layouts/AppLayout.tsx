import { useEffect, useState } from 'react'
import { Outlet } from 'react-router-dom'
import { Button, SidebarProvider, SidebarTrigger, useSidebar } from '@thyris/ui'
import Sidebar from './Sidebar'
import StatusBadge from '../components/StatusBadge'
import { getSystemStatus, type SystemStatus } from '../api/health'
import { useAuth } from '../context/AuthContext'
import './Layout.css'

const REFRESH_INTERVAL_MS = 30000

export default function AppLayout() {
  return (
    <SidebarProvider>
      <AppShell />
    </SidebarProvider>
  )
}

function AppShell() {
  const { logout } = useAuth()
  const { isCollapsed, isMobile, setCollapsed } = useSidebar()
  const [status, setStatus] = useState<SystemStatus | 'loading'>('loading')

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
          <SidebarTrigger
            className="hamburger-button"
            aria-label={isCollapsed ? 'Open navigation menu' : 'Close navigation menu'}
            aria-expanded={!isCollapsed}
          >
            ☰
          </SidebarTrigger>
          <span className="app-header-title">Safe Zone</span>
        </div>
        <div className="app-header-right">
          <StatusBadge status={status} />
          <Button type="button" variant="outline" size="sm" onClick={() => logout()}>
            Log out
          </Button>
        </div>
      </header>
      <div className="app-body">
        <Sidebar onNavigate={() => isMobile && setCollapsed(true)} />
        {/* Overlay to close the sidebar on outside click (mobile only). */}
        {isMobile && !isCollapsed && (
          <div
            className="sidebar-overlay"
            onClick={() => setCollapsed(true)}
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
