import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { listUsers, createUser } from '../api/users'
import { ApiError } from '../api/client'
import type { CreateUserInput, DashboardUser } from '../types/users'
import AddUserModal from '../components/AddUserModal'
import { useAuth } from '../context/AuthContext'

type LoadState = 'loading' | 'success' | 'empty' | 'error' | 'unauthorized'

export default function Users() {
  const { user: currentUser } = useAuth()
  const [users, setUsers] = useState<DashboardUser[]>([])
  const [loadState, setLoadState] = useState<LoadState>('loading')
  const [isAddModalOpen, setIsAddModalOpen] = useState(false)

  useEffect(() => {
    let cancelled = false

    async function loadUsers() {
      try {
        const data = await listUsers()
        if (cancelled) return

        setUsers(data)
        setLoadState(data.length === 0 ? 'empty' : 'success')
      } catch (err) {
        if (cancelled) return

        if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
          setLoadState('unauthorized')
        } else {
          setLoadState('error')
        }
      }
    }

    loadUsers()
    return () => {
      cancelled = true
    }
  }, [])

  async function handleAddUser(input: CreateUserInput) {
    await createUser(input)
    setIsAddModalOpen(false)

    // The user was created; only the list refresh can fail from here.
    try {
      const data = await listUsers()
      setUsers(data)
      setLoadState(data.length === 0 ? 'empty' : 'success')
    } catch {
      setLoadState('error')
    }
  }

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Users</h1>
        <p className="page-subtitle">Manage dashboard accounts and roles</p>
      </div>

      <div className="list-toolbar">
        <button type="button" className="button-primary" onClick={() => setIsAddModalOpen(true)}>
          + Add User
        </button>
      </div>

      {loadState === 'loading' && <p className="info-message">Loading users...</p>}

      {loadState === 'unauthorized' && (
        <p className="info-message">
          You don't have permission to view this data. Contact your administrator if you believe this is an error.
        </p>
      )}

      {loadState === 'error' && (
        <p className="info-message">Unable to load users. Please check your connection and try again.</p>
      )}

      {loadState === 'empty' && <p className="info-message">No users yet.</p>}

      {loadState === 'success' && (
        <table className="data-table">
          <thead>
            <tr>
              <th scope="col">Email</th>
              <th scope="col">Role</th>
              <th scope="col">Status</th>
              <th scope="col">Created</th>
              <th scope="col">Activity</th>
            </tr>
          </thead>
          <tbody>
            {users.map((user) => (
              <tr key={user.ID} className={currentUser?.email === user.email ? 'row-self' : undefined}>
                <td data-label="Email">
                  {user.email}
                  {currentUser?.email === user.email && <span className="tag-you">You</span>}
                </td>
                <td data-label="Role">{user.role === 'admin' ? 'Admin' : 'Viewer'}</td>
                <td data-label="Status">
                  <span className={user.is_active ? 'toggle-label-on' : 'toggle-label-off'}>
                    {user.is_active ? 'Active' : 'Inactive'}
                  </span>
                </td>
                <td data-label="Created">{new Date(user.CreatedAt).toLocaleDateString()}</td>
                <td data-label="Activity">
                  <Link to={`/activity?user=${user.ID}`} className="link-button">
                    View activity
                  </Link>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {isAddModalOpen && <AddUserModal onCancel={() => setIsAddModalOpen(false)} onSubmit={handleAddUser} />}
    </div>
  )
}