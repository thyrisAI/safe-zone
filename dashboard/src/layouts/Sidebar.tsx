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
import './Layout.css'

const navItems = [
  { label: 'Overview', path: '/' },
  { label: 'Patterns', path: '/patterns' },
  { label: 'Guardrails', path: '/guardrails' },
  { label: 'Events', path: '/events' },
  { label: 'Lists', path: '/lists' },
  { label: 'Configuration', path: '/configuration' },
]

interface SidebarProps {
  onNavigate?: () => void
}

export default function Sidebar({ onNavigate }: SidebarProps) {
  const location = useLocation()

  return (
    <ThyrisSidebar collapsible className="app-sidebar">
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent>
            <nav aria-label="Main navigation">
              <SidebarMenu>
                {navItems.map((item) => (
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
            </nav>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
    </ThyrisSidebar>
  )
}
