import { Navigate, Outlet } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'

export default function ProtectedRoute() {
  const { user, isLoading } = useAuth()

  if (isLoading) {
    return <p className="info-message">Loading...</p>
  }

  if (!user) {
    return <Navigate to="/login" replace />
  }

  return <Outlet />
}