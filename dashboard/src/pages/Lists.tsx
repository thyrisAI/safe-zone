import { useEffect, useState } from 'react'
import {
  Button,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from '@thyris/ui'
import {
  getAllowlist,
  getBlacklist,
  createAllowlistItem,
  createBlacklistItem,
  deleteAllowlistItem,
  deleteBlacklistItem,
} from '../api/lists'
import { ApiError } from '../api/client'
import type { ListItem, ListKind } from '../types/list'
import AddEntryModal from '../components/AddEntryModal'
import ConfirmDialog from '../components/ConfirmDialog'

type LoadState = 'loading' | 'success' | 'empty' | 'error' | 'unauthorized'

export default function Lists() {
  const [activeTab, setActiveTab] = useState<ListKind>('allowlist')
  const [items, setItems] = useState<ListItem[]>([])
  const [loadState, setLoadState] = useState<LoadState>('loading')
  const [isAddModalOpen, setIsAddModalOpen] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<ListItem | null>(null)
  const [isDeleting, setIsDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    async function loadItems() {
      setLoadState('loading')
      try {
        const data = activeTab === 'allowlist' ? await getAllowlist() : await getBlacklist()
        if (cancelled) return

        setItems(data)
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

    loadItems()
    return () => {
      cancelled = true
    }
  }, [activeTab])

  async function refreshItems() {
    const data = activeTab === 'allowlist' ? await getAllowlist() : await getBlacklist()
    setItems(data)
    setLoadState(data.length === 0 ? 'empty' : 'success')
  }

  async function handleAddEntry(value: string, description: string) {
    if (activeTab === 'allowlist') {
      await createAllowlistItem({ value, description })
    } else {
      await createBlacklistItem({ value, description })
    }
    setIsAddModalOpen(false)
    await refreshItems()
  }

  async function handleConfirmDelete() {
    if (!pendingDelete) return

    setIsDeleting(true)
    setDeleteError(null)
    try {
      if (activeTab === 'allowlist') {
        await deleteAllowlistItem(pendingDelete.ID)
      } else {
        await deleteBlacklistItem(pendingDelete.ID)
      }
      setPendingDelete(null)
      await refreshItems()
    } catch {
      setDeleteError('Unable to delete this entry. Please try again.')
    } finally {
      setIsDeleting(false)
    }
  }

  const listLabel = activeTab === 'allowlist' ? 'Allowlist' : 'Blacklist'

  function renderListContent() {
    return (
      <>
        <div className="my-4 flex justify-end">
          <Button type="button" onClick={() => setIsAddModalOpen(true)}>
            + Add Entry
          </Button>
        </div>

        {loadState === 'loading' && <p className="info-message">Loading {listLabel.toLowerCase()} entries...</p>}

        {loadState === 'unauthorized' && (
          <p className="info-message">
            You don't have permission to view this data. Contact your administrator if you believe this is an error.
          </p>
        )}

        {loadState === 'error' && (
          <p className="info-message">
            Unable to load {listLabel.toLowerCase()}. Please check your connection and try again.
          </p>
        )}

        {loadState === 'empty' && <p className="info-message">No {listLabel.toLowerCase()} entries yet.</p>}

        {loadState === 'success' && (
          <Table className="data-table">
            <TableHeader>
              <TableRow>
                <TableHead scope="col">Value</TableHead>
                <TableHead scope="col">Description</TableHead>
                <TableHead scope="col">Created</TableHead>
                <TableHead scope="col">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <TableRow key={item.ID}>
                  <TableCell data-label="Value">{item.value}</TableCell>
                  <TableCell data-label="Description">{item.description || '—'}</TableCell>
                  <TableCell data-label="Created">{new Date(item.CreatedAt).toLocaleDateString()}</TableCell>
                  <TableCell data-label="Actions" className="actions-cell">
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="text-destructive hover:text-destructive"
                      onClick={() => {
                        setPendingDelete(item)
                        setDeleteError(null)
                      }}
                    >
                      Delete
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </>
    )
  }

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Lists</h1>
        <p className="page-subtitle">Manage allowlist and blacklist entries</p>
      </div>

      <Tabs value={activeTab} onValueChange={(value) => setActiveTab(value as ListKind)}>
        <TabsList aria-label="List type">
          <TabsTrigger value="allowlist">Allowlist</TabsTrigger>
          <TabsTrigger value="blacklist">Blacklist</TabsTrigger>
        </TabsList>
        <TabsContent value="allowlist">
          {activeTab === 'allowlist' && renderListContent()}
        </TabsContent>
        <TabsContent value="blacklist">
          {activeTab === 'blacklist' && renderListContent()}
        </TabsContent>
      </Tabs>

      {isAddModalOpen && (
        <AddEntryModal
          listKind={activeTab}
          onCancel={() => setIsAddModalOpen(false)}
          onSubmit={handleAddEntry}
        />
      )}

      {pendingDelete && (
        <ConfirmDialog
          title="Delete entry?"
          message="Are you sure you want to delete this entry? This cannot be undone."
          itemLabel={pendingDelete.value}
          isConfirming={isDeleting}
          confirmError={deleteError}
          onCancel={() => setPendingDelete(null)}
          onConfirm={handleConfirmDelete}
        />
      )}
    </div>
  )
}
