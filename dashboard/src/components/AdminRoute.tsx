import { Navigate, Outlet } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'

/**
 * Nested inside ProtectedRoute, so a logged-in user is already guaranteed.
 * This only narrows access further: non-admins are sent back to Overview.
 * (UX only -- the backend enforces the real permission with 403.)
 */
export default function AdminRoute() {
  const { user } = useAuth()

  if (user?.role !== 'admin') {
    return <Navigate to="/" replace />
  }

  return <Outlet />
}