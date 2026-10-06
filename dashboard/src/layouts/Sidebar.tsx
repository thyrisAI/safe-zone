import { NavLink } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'
import './Layout.css'

interface NavItem {
  label: string
  path: string
  adminOnly?: boolean
}

const navItems: NavItem[] = [
  { label: 'Overview', path: '/' },
  { label: 'Patterns', path: '/patterns' },
  { label: 'Guardrails', path: '/guardrails' },
  { label: 'Events', path: '/events' },
  { label: 'Lists', path: '/lists' },
  { label: 'Configuration', path: '/configuration' },
  { label: 'Users', path: '/users', adminOnly: true },
  { label: 'Activity', path: '/activity', adminOnly: true },
]

interface SidebarProps {
  isOpen?: boolean
  onNavigate?: () => void
}

export default function Sidebar({ isOpen = false, onNavigate }: SidebarProps) {
  const { user } = useAuth()
  const visibleItems = navItems.filter((item) => !item.adminOnly || user?.role === 'admin')

  return (
    <nav
      className={`sidebar${isOpen ? ' sidebar-open' : ''}`}
      aria-label="Main navigation"
    >
      <ul className="sidebar-list">
        {visibleItems.map((item) => (
          <li key={item.path}>
            <NavLink
              to={item.path}
              end={item.path === '/'}
              className={({ isActive }) => `sidebar-link${isActive ? ' sidebar-link-active' : ''}`}
              onClick={onNavigate}
            >
              {item.label}
            </NavLink>
          </li>
        ))}
      </ul>
    </nav>
  )
}