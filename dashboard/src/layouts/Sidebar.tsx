import {
  Sidebar as ThyrisSidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from '@thyris/ui'
import { Link, useLocation } from 'react-router-dom'
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
  onNavigate?: () => void
}

export default function Sidebar({ onNavigate }: SidebarProps) {
  const location = useLocation()
  const { user } = useAuth()

  // A group a viewer cannot use is left out entirely, heading included:
  // a heading with nothing under it would look like missing content.
  const visibleSections = navSections.filter(
    (section) => !section.adminOnly || user?.role === 'admin',
  )

  return (
    <ThyrisSidebar collapsible className="app-sidebar">
      <SidebarContent>
        <nav aria-label="Main navigation">
          {visibleSections.map((section) => (
            <SidebarGroup key={section.heading ?? 'main'}>
              {section.heading && (
                <p className="sidebar-heading">{section.heading}</p>
              )}
              <SidebarGroupContent>
                <SidebarMenu>
                  {section.items.map((item) => (
                    <SidebarMenuItem key={item.path}>
                      <SidebarMenuButton
                        asChild
                        isActive={location.pathname === item.path}
                        tooltip={item.label}
                      >
                        <Link to={item.path} onClick={onNavigate}>
                          {item.label}
                        </Link>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  ))}
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          ))}
        </nav>
      </SidebarContent>
    </ThyrisSidebar>
  )
}