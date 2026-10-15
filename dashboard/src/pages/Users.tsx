import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Badge, Button } from '@thyris/ui'
import { listUsers, createUser, deleteUser } from '../api/users'
import { ApiError } from '../api/client'
import type { CreateUserInput, DashboardUser } from '../types/users'
import AddUserModal from '../components/AddUserModal'
import ConfirmDialog from '../components/ConfirmDialog'
import { useAuth } from '../context/AuthContext'

type LoadState = 'loading' | 'success' | 'empty' | 'error' | 'unauthorized'

export default function Users() {
  const { user: currentUser } = useAuth()
  const [users, setUsers] = useState<DashboardUser[]>([])
  const [loadState, setLoadState] = useState<LoadState>('loading')
  const [isAddModalOpen, setIsAddModalOpen] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<DashboardUser | null>(null)
  const [isDeleting, setIsDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

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

  async function handleConfirmDelete() {
    if (!pendingDelete) return

    setIsDeleting(true)
    setDeleteError(null)
    try {
      await deleteUser(pendingDelete.ID)
    } catch (err) {
      // 404: someone else already deleted this account, so just refresh.
      if (!(err instanceof ApiError && err.status === 404)) {
        if (err instanceof ApiError && err.status === 409) {
          setDeleteError('The last active admin cannot be deleted.')
        } else if (err instanceof ApiError && err.status === 400) {
          setDeleteError('You cannot delete your own account.')
        } else {
          setDeleteError('Unable to delete this user. Please try again.')
        }
        setIsDeleting(false)
        return
      }
    }

    setPendingDelete(null)
    setIsDeleting(false)
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

      <div className="my-4 flex justify-end">
        <Button type="button" onClick={() => setIsAddModalOpen(true)}>
          + Add User
        </Button>
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
              <th scope="col">Actions</th>
            </tr>
          </thead>
          <tbody>
            {users.map((user) => (
              <tr key={user.ID} className={currentUser?.email === user.email ? 'row-self' : undefined}>
                <td data-label="Email">
                  {user.email}
                  {currentUser?.email === user.email && <Badge className="ml-2">You</Badge>}
                </td>
                <td data-label="Role">{user.role === 'admin' ? 'Admin' : 'Viewer'}</td>
                <td data-label="Status">
                  <span className={user.is_active ? 'text-sm font-medium text-green-700' : 'text-sm text-muted-foreground'}>
                    {user.is_active ? 'Active' : 'Inactive'}
                  </span>
                </td>
                <td data-label="Created">{new Date(user.CreatedAt).toLocaleDateString()}</td>
                <td data-label="Activity">
                  <Button asChild variant="link" size="sm" className="h-auto p-0">
                    <Link to={`/activity?user=${user.ID}`}>View activity</Link>
                  </Button>
                </td>
                <td data-label="Actions">
                  {currentUser?.email !== user.email && (
                    <Button
                      type="button"
                      variant="link"
                      size="sm"
                      className="h-auto p-0 text-destructive"
                      onClick={() => {
                        setDeleteError(null)
                        setPendingDelete(user)
                      }}
                    >
                      Delete
                    </Button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {isAddModalOpen && <AddUserModal onCancel={() => setIsAddModalOpen(false)} onSubmit={handleAddUser} />}

      {pendingDelete && (
        <ConfirmDialog
          title="Delete user?"
          message="Are you sure you want to delete this user? They will be signed out immediately and cannot sign in again. This cannot be undone."
          itemLabel={pendingDelete.email}
          isConfirming={isDeleting}
          confirmError={deleteError}
          onCancel={() => setPendingDelete(null)}
          onConfirm={handleConfirmDelete}
        />
      )}
    </div>
  )
}