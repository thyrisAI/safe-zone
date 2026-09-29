import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { AuthProvider } from './context/AuthContext'
import ProtectedRoute from './components/ProtectedRoute'
import AppLayout from './layouts/AppLayout'
import Login from './pages/Login'
import Overview from './pages/Overview'
import Patterns from './pages/Patterns'
import Guardrails from './pages/Guardrails'
import Events from './pages/Events'
import Configuration from './pages/Configuration'
import Lists from './pages/Lists'

export default function App() {
  return (
    <AuthProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route element={<ProtectedRoute />}>
            <Route element={<AppLayout />}>
              <Route path="/" element={<Overview />} />
              <Route path="/patterns" element={<Patterns />} />
              <Route path="/guardrails" element={<Guardrails />} />
              <Route path="/events" element={<Events />} />
              <Route path="/lists" element={<Lists />} />
              <Route path="/configuration" element={<Configuration />} />
            </Route>
          </Route>
        </Routes>
      </BrowserRouter>
    </AuthProvider>
  )
}