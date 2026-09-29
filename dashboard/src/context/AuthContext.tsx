import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { getMe, login as apiLogin, logout as apiLogout } from '../api/auth'
import type { User } from '../types/auth'

interface AuthContextValue {
  user: User | null
  isLoading: boolean
  login: (email: string, password: string) => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [isLoading, setIsLoading] = useState(true)

  // On first load, ask the backend if there's already a valid session
  // (e.g. the user refreshed the page after logging in earlier).
  useEffect(() => {
    let cancelled = false

    getMe()
      .then((result) => {
        if (!cancelled) setUser(result)
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [])

  async function login(email: string, password: string) {
    const loggedInUser = await apiLogin(email, password)
    setUser(loggedInUser)
  }

  async function logout() {
    await apiLogout()
    setUser(null)
  }

  return (
    <AuthContext.Provider value={{ user, isLoading, login, logout }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return ctx
}