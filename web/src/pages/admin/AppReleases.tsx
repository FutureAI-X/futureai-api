import { useState, useEffect, useCallback } from 'react'
import {
  Plus,
  Pencil,
  Trash2,
  Power,
  PowerOff,
  Loader2,
  X,
  ExternalLink,
} from 'lucide-react'
import { cn } from '../../lib/utils'
import {
  getApps,
  getAppReleases,
  createAppRelease,
  updateAppRelease,
  updateAppReleaseStatus,
  deleteAppRelease,
  type AdminApp,
  type AdminAppRelease,
} from '../../api/admin-app'
import { ConfirmDialog } from '../../components/admin/ConfirmDialog'

const STATUS_CONFIG: Record<number, { label: string; className: string }> = {
  1: { label: '已上架', className: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400' },
  2: { label: '已下架', className: 'bg-red-500/10 text-red-600 dark:text-red-400' },
}

const PLATFORM_LABELS: Record<string, string> = {
  windows: 'Windows',
  macos: 'macOS',
}

const PLATFORM_OPTIONS = [
  { value: 'windows', label: 'Windows' },
  { value: 'macos', label: 'macOS' },
]

// 与后端 ParseSemver 保持一致：严格三段，不接受 v 前缀或预发布后缀
const VERSION_PATTERN = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/

export function AdminAppReleases() {
  const [releases, setReleases] = useState<AdminAppRelease[]>([])
  const [apps, setApps] = useState<AdminApp[]>([])
  const [loading, setLoading] = useState(true)
  const [pageError, setPageError] = useState('')

  // 筛选
  const [filterAppID, setFilterAppID] = useState('')
  const [filterPlatform, setFilterPlatform] = useState('')

  // 弹框状态
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [editRow, setEditRow] = useState<AdminAppRelease | null>(null)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [deleteRow, setDeleteRow] = useState<AdminAppRelease | null>(null)
  const [actionLoading, setActionLoading] = useState(false)

  // 表单状态
  const [formAppID, setFormAppID] = useState('')
  const [formPlatform, setFormPlatform] = useState('windows')
  const [formVersion, setFormVersion] = useState('')
  const [formUpdateURL, setFormUpdateURL] = useState('')
  const [formNotes, setFormNotes] = useState('')
  const [formError, setFormError] = useState('')
  const [formSaving, setFormSaving] = useState(false)

  const loadApps = useCallback(async () => {
    try {
      const res = await getApps()
      if (res.success) setApps(res.data || [])
    } catch { /* ignore */ }
  }, [])

  const loadReleases = useCallback(async () => {
    setLoading(true)
    try {
      const res = await getAppReleases({
        app_id: filterAppID ? Number(filterAppID) : undefined,
        platform: filterPlatform || undefined,
      })
      if (res.success) setReleases(res.data || [])
    } catch { /* ignore */ }
    finally { setLoading(false) }
  }, [filterAppID, filterPlatform])

  useEffect(() => { loadApps() }, [loadApps])
  useEffect(() => { loadReleases() }, [loadReleases])

  const handleCreate = () => {
    setEditRow(null)
    setFormAppID(apps.length > 0 ? String(apps[0].id) : '')
    setFormPlatform('windows')
    setFormVersion('')
    setFormUpdateURL('')
    setFormNotes('')
    setFormError('')
    setDrawerOpen(true)
  }

  const handleEdit = (release: AdminAppRelease) => {
    setEditRow(release)
    setFormAppID(String(release.app_id))
    setFormPlatform(release.platform)
    setFormVersion(release.version)
    setFormUpdateURL(release.update_url)
    setFormNotes(release.release_notes || '')
    setFormError('')
    setDrawerOpen(true)
  }

  const handleSave = async () => {
    const appID = Number(formAppID)
    const version = formVersion.trim()
    const updateURL = formUpdateURL.trim()

    if (!appID) { setFormError('请选择应用'); return }
    if (!VERSION_PATTERN.test(version)) {
      setFormError('版本号必须为 x.y.z 格式（如 1.2.3）')
      return
    }
    if (!updateURL.startsWith('http://') && !updateURL.startsWith('https://')) {
      setFormError('更新地址必须以 http:// 或 https:// 开头')
      return
    }

    setFormSaving(true)
    setFormError('')

    try {
      if (editRow) {
        const res = await updateAppRelease(editRow.id, {
          app_id: appID,
          platform: formPlatform,
          version,
          update_url: updateURL,
          release_notes: formNotes,
        })
        if (!res.success) { setFormError(res.message || '更新失败'); return }
      } else {
        const res = await createAppRelease({
          app_id: appID,
          platform: formPlatform,
          version,
          update_url: updateURL,
          release_notes: formNotes,
        })
        if (!res.success) { setFormError(res.message || '发布失败'); return }
      }
      setDrawerOpen(false)
      loadReleases()
    } catch {
      setFormError('网络错误')
    } finally {
      setFormSaving(false)
    }
  }

  const handleToggle = async (release: AdminAppRelease) => {
    setActionLoading(true)
    setPageError('')
    try {
      const res = await updateAppReleaseStatus(release.id, release.status === 1 ? 2 : 1)
      if (!res.success) setPageError(res.message || '更新状态失败')
      loadReleases()
    } catch {
      setPageError('网络错误')
    } finally {
      setActionLoading(false)
    }
  }

  const handleDeleteConfirm = async () => {
    if (!deleteRow) return
    setActionLoading(true)
    setPageError('')
    try {
      const res = await deleteAppRelease(deleteRow.id)
      if (!res.success) setPageError(res.message || '删除失败')
      setDeleteDialogOpen(false)
      setDeleteRow(null)
      loadReleases()
    } catch {
      setPageError('网络错误')
    } finally {
      setActionLoading(false)
    }
  }

  useEffect(() => {
    if (drawerOpen || deleteDialogOpen) {
      document.body.style.overflow = 'hidden'
    } else {
      document.body.style.overflow = ''
    }
    return () => { document.body.style.overflow = '' }
  }, [drawerOpen, deleteDialogOpen])

  useEffect(() => {
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        if (deleteDialogOpen) setDeleteDialogOpen(false)
        else if (drawerOpen) setDrawerOpen(false)
      }
    }
    document.addEventListener('keydown', handleEsc)
    return () => document.removeEventListener('keydown', handleEsc)
  }, [drawerOpen, deleteDialogOpen])

  return (
    <div data-page-fill className='flex h-full min-h-0 flex-col gap-6'>
      <div className='flex shrink-0 items-center justify-between'>
        <div>
          <h2 className='text-2xl font-bold tracking-tight'>版本发布</h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            客户端启动时取该应用、该平台下已上架的最高版本；下架最新版会自动回退到上一版
          </p>
        </div>
        <button
          onClick={handleCreate}
          disabled={apps.length === 0}
          title={apps.length === 0 ? '请先在「应用管理」中添加应用' : undefined}
          className='bg-primary hover:bg-primary/90 inline-flex h-9 items-center justify-center gap-2 rounded-lg px-4 text-sm font-medium text-white transition-colors disabled:cursor-not-allowed disabled:opacity-50'
        >
          <Plus className='size-4' />
          发布版本
        </button>
      </div>

      {apps.length === 0 && !loading && (
        <div className='border-border/60 bg-muted/30 shrink-0 rounded-lg border px-4 py-3 text-sm'>
          还没有登记任何应用，请先到「应用管理」中添加应用，再发布版本。
        </div>
      )}

      {pageError && (
        <div className='bg-destructive/10 text-destructive shrink-0 rounded-lg px-4 py-3 text-sm'>{pageError}</div>
      )}

      {/* 筛选 */}
      <div className='flex shrink-0 flex-wrap items-center gap-3'>
        <select
          value={filterAppID}
          onChange={(e) => setFilterAppID(e.target.value)}
          className='border-border/60 bg-background focus-visible:ring-ring flex h-9 rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
        >
          <option value=''>全部应用</option>
          {apps.map((app) => (
            <option key={app.id} value={app.id}>{app.name}（{app.key}）</option>
          ))}
        </select>
        <select
          value={filterPlatform}
          onChange={(e) => setFilterPlatform(e.target.value)}
          className='border-border/60 bg-background focus-visible:ring-ring flex h-9 rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
        >
          <option value=''>全部平台</option>
          {PLATFORM_OPTIONS.map((p) => (
            <option key={p.value} value={p.value}>{p.label}</option>
          ))}
        </select>
      </div>

      <div className='border-border/60 flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border'>
        <div className='min-h-0 flex-1 overflow-auto'>
          <table className='w-full text-sm'>
            <thead className='bg-muted sticky top-0 z-10'>
              <tr className='bg-muted/30 border-border/40 border-b'>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>ID</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>应用</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>平台</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>版本</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>更新地址</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>状态</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>发布时间</th>
                <th className='text-muted-foreground px-4 py-3 text-left text-xs font-semibold tracking-wider uppercase'>操作</th>
              </tr>
            </thead>
            <tbody className='divide-border/40 divide-y'>
              {loading ? (
                <tr>
                  <td colSpan={8} className='px-4 py-12 text-center'>
                    <Loader2 className='text-muted-foreground mx-auto size-6 animate-spin' />
                  </td>
                </tr>
              ) : releases.length === 0 ? (
                <tr>
                  <td colSpan={8} className='px-4 py-12 text-center'>
                    <p className='text-muted-foreground text-sm'>暂无版本记录</p>
                  </td>
                </tr>
              ) : releases.map((release) => {
                const statusConf = STATUS_CONFIG[release.status] || STATUS_CONFIG[1]
                return (
                  <tr key={release.id} className={cn('hover:bg-muted/20 transition-colors', release.status === 2 && 'opacity-50')}>
                    <td className='px-4 py-3'>
                      <span className='text-muted-foreground font-mono text-xs'>{release.id}</span>
                    </td>
                    <td className='px-4 py-3'>
                      <div className='flex flex-col gap-0.5'>
                        <span className='font-medium'>{release.app_name || '（应用已删除）'}</span>
                        <code className='text-muted-foreground font-mono text-xs'>{release.app_key}</code>
                      </div>
                    </td>
                    <td className='px-4 py-3'>
                      <span className='text-sm'>{PLATFORM_LABELS[release.platform] || release.platform}</span>
                    </td>
                    <td className='px-4 py-3'>
                      <code className='font-mono text-xs font-medium'>{release.version}</code>
                    </td>
                    <td className='max-w-[20rem] px-4 py-3'>
                      <a
                        href={release.update_url}
                        target='_blank'
                        rel='noreferrer'
                        className='text-primary inline-flex max-w-full items-center gap-1 hover:underline'
                        title={release.update_url}
                      >
                        <span className='truncate text-xs'>{release.update_url}</span>
                        <ExternalLink className='size-3 shrink-0' />
                      </a>
                    </td>
                    <td className='px-4 py-3'>
                      <span className={cn('inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium', statusConf.className)}>
                        <span className={cn('size-1.5 rounded-full', release.status === 1 ? 'bg-emerald-500' : 'bg-red-500')} />
                        {statusConf.label}
                      </span>
                    </td>
                    <td className='px-4 py-3'>
                      <span className='text-muted-foreground text-sm'>{release.created_at}</span>
                    </td>
                    <td className='px-4 py-3'>
                      <div className='flex items-center gap-1'>
                        <button onClick={() => handleEdit(release)} className='hover:bg-muted inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors'>
                          <Pencil className='size-3.5' /> 编辑
                        </button>
                        <button
                          onClick={() => handleToggle(release)}
                          className={cn(
                            'inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors',
                            release.status === 1
                              ? 'hover:bg-amber-500/10 text-amber-600 dark:text-amber-400'
                              : 'hover:bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
                          )}
                        >
                          {release.status === 1 ? <><PowerOff className='size-3.5' /> 下架</> : <><Power className='size-3.5' /> 上架</>}
                        </button>
                        <button
                          onClick={() => { setDeleteRow(release); setDeleteDialogOpen(true) }}
                          className='text-destructive hover:bg-destructive/10 inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors'
                        >
                          <Trash2 className='size-3.5' /> 删除
                        </button>
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </div>

      {drawerOpen && (
        <div className='fixed inset-0 z-[100] flex items-center justify-center'>
          <div className='bg-background/80 fixed inset-0 backdrop-blur-sm' onClick={() => setDrawerOpen(false)} />
          <div className='bg-background border-border/60 relative z-10 w-full max-w-lg rounded-xl border shadow-lg'>
            <div className='flex items-center justify-between border-b border-border/40 px-6 py-4'>
              <h2 className='text-lg font-semibold'>{editRow ? '编辑版本' : '发布版本'}</h2>
              <button onClick={() => setDrawerOpen(false)} className='hover:bg-muted rounded-lg p-1.5 transition-colors'>
                <X className='size-4' />
              </button>
            </div>

            <div className='p-6 space-y-4'>
              {formError && (
                <div className='bg-destructive/10 text-destructive rounded-lg px-4 py-3 text-sm'>{formError}</div>
              )}

              <div className='grid grid-cols-2 gap-4'>
                <div className='space-y-2'>
                  <label className='text-sm font-medium'>应用</label>
                  <select
                    value={formAppID}
                    onChange={(e) => setFormAppID(e.target.value)}
                    className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                  >
                    <option value=''>请选择</option>
                    {apps.map((app) => (
                      <option key={app.id} value={app.id}>{app.name}（{app.key}）</option>
                    ))}
                  </select>
                </div>

                <div className='space-y-2'>
                  <label className='text-sm font-medium'>平台</label>
                  <select
                    value={formPlatform}
                    onChange={(e) => setFormPlatform(e.target.value)}
                    className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                  >
                    {PLATFORM_OPTIONS.map((p) => (
                      <option key={p.value} value={p.value}>{p.label}</option>
                    ))}
                  </select>
                </div>
              </div>

              <div className='space-y-2'>
                <label className='text-sm font-medium'>版本号</label>
                <input
                  type='text'
                  value={formVersion}
                  onChange={(e) => setFormVersion(e.target.value)}
                  placeholder='例如: 1.3.0'
                  className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 font-mono text-sm focus-visible:ring-2 focus-visible:outline-none'
                />
                <p className='text-muted-foreground text-xs'>必须为 x.y.z 格式，且同一应用、同一平台下不能重复</p>
              </div>

              <div className='space-y-2'>
                <label className='text-sm font-medium'>更新地址</label>
                <input
                  type='text'
                  value={formUpdateURL}
                  onChange={(e) => setFormUpdateURL(e.target.value)}
                  placeholder='网盘链接或文档页面，如 https://...'
                  className='border-border/60 bg-background focus-visible:ring-ring flex h-9 w-full rounded-lg border px-3 text-sm focus-visible:ring-2 focus-visible:outline-none'
                />
                <p className='text-muted-foreground text-xs'>客户端会用系统浏览器打开该地址</p>
              </div>

              <div className='space-y-2'>
                <label className='text-sm font-medium'>更新说明</label>
                <textarea
                  value={formNotes}
                  onChange={(e) => setFormNotes(e.target.value)}
                  rows={4}
                  placeholder='会展示在客户端的更新弹窗中，可留空'
                  className='border-border/60 bg-background focus-visible:ring-ring flex w-full resize-y rounded-lg border px-3 py-2 text-sm focus-visible:ring-2 focus-visible:outline-none'
                />
              </div>
            </div>

            <div className='flex justify-end gap-3 border-t border-border/40 px-6 py-4'>
              <button onClick={() => setDrawerOpen(false)} disabled={formSaving}
                className='border-border/60 hover:bg-muted inline-flex h-9 items-center justify-center rounded-lg border px-4 text-sm font-medium transition-colors'>
                取消
              </button>
              <button onClick={handleSave} disabled={formSaving}
                className='bg-primary hover:bg-primary/90 inline-flex h-9 items-center justify-center gap-2 rounded-lg px-4 text-sm font-medium text-white transition-colors disabled:opacity-50'>
                {formSaving && <Loader2 className='size-4 animate-spin' />}
                {formSaving ? '保存中...' : '保存'}
              </button>
            </div>
          </div>
        </div>
      )}

      <ConfirmDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        title='确认删除'
        description={
          <>
            确定要删除 <span className='text-foreground font-semibold'>{deleteRow?.app_name}</span>{' '}
            {deleteRow?.platform} {deleteRow?.version} 的版本记录吗？
            <br />删除后客户端将看不到该版本，如需保留历史请改用「下架」。
          </>
        }
        confirmText='删除'
        destructive
        loading={actionLoading}
        onConfirm={handleDeleteConfirm}
      />
    </div>
  )
}
