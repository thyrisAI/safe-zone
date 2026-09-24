import { NavLink } from 'react-router-dom'
import './Layout.css'

const navItems = [
  { label: 'Overview', path: '/' },
  { label: 'Patterns', path: '/patterns' },
  { label: 'Guardrails', path: '/guardrails' },
  { label: 'Events', path: '/events' },
  { label: 'Configuration', path: '/configuration' },
]

interface SidebarProps {
  isOpen?: boolean
  onNavigate?: () => void
}

export default function Sidebar({ isOpen = false, onNavigate }: SidebarProps) {
  return (
    <nav
      className={`sidebar${isOpen ? ' sidebar-open' : ''}`}
      aria-label="Main navigation"
    >
      <ul className="sidebar-list">
        {navItems.map((item) => (
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