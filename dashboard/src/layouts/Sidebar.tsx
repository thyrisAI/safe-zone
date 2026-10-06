import { NavLink } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'
import './Layout.css'

interface NavItem {
  label: string
  path: string
}

interface NavSection {
  // Small, non-clickable label above the group. Omitted for the main group.
  heading?: string
  adminOnly?: boolean
  items: NavItem[]
}

const navSections: NavSection[] = [
  {
    items: [
      { label: 'Overview', path: '/' },
      { label: 'Patterns', path: '/patterns' },
      { label: 'Guardrails', path: '/guardrails' },
      { label: 'Events', path: '/events' },
      { label: 'Lists', path: '/lists' },
      { label: 'Configuration', path: '/configuration' },
    ],
  },
  {
    heading: 'Administration',
    adminOnly: true,
    items: [
      { label: 'Users', path: '/users' },
      { label: 'Activity', path: '/activity' },
    ],
  },
]

interface SidebarProps {
  isOpen?: boolean
  onNavigate?: () => void
}

export default function Sidebar({ isOpen = false, onNavigate }: SidebarProps) {
  const { user } = useAuth()

  // A group a viewer cannot use is left out entirely, heading included:
  // a heading with nothing under it would look like missing content.
  const visibleSections = navSections.filter(
    (section) => !section.adminOnly || user?.role === 'admin',
  )

  return (
    <nav
      className={`sidebar${isOpen ? ' sidebar-open' : ''}`}
      aria-label="Main navigation"
    >
      {visibleSections.map((section) => (
        <div className="sidebar-group" key={section.heading ?? 'main'}>
          {section.heading && <p className="sidebar-heading">{section.heading}</p>}
          <ul className="sidebar-list">
            {section.items.map((item) => (
              <li key={item.path}>
                <NavLink
                  to={item.path}
                  end={item.path === '/'}
                  className={({ isActive }) =>
                    `sidebar-link${isActive ? ' sidebar-link-active' : ''}`
                  }
                  onClick={onNavigate}
                >
                  {item.label}
                </NavLink>
              </li>
            ))}
          </ul>
        </div>
      ))}
    </nav>
  )
}
