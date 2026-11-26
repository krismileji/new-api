/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type {
  ColumnDef,
  RowSelectionState,
  Updater,
} from '@tanstack/react-table'
import { useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  DataTableBulkActions,
  DataTableView,
  useDataTable,
} from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { formatNumber } from '@/lib/format'

import { getChannelMonitorSmartSchedulePrimaryManagement } from '../api'
import { handleChannelMonitorMutationError } from '../lib/error'
import { CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS } from '../lib/query-options'
import {
  primaryEntryStatus,
  primaryRouteStatus,
  updatePrimaryEntry,
  type PrimaryEntry,
} from '../lib/smart-schedule-primary-management'
import { ChannelMonitorSmartSchedulePrimaryEditor } from './channel-monitor-smart-schedule-primary-editor'
import { ChannelMonitorSmartSchedulePrimaryRow } from './channel-monitor-smart-schedule-primary-row'

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onActionComplete: () => Promise<void>
}

const MANAGEMENT_KEY = [
  'channel-monitor',
  'smart-schedule',
  'primary-management',
] as const
const STATUS_OPTIONS = [
  { value: 'all', label: '全部状态' },
  { value: 'partial', label: '部分生效' },
  { value: 'inactive', label: '未生效' },
  { value: 'active', label: '全部生效' },
  { value: 'pending', label: '待新增路由' },
]

export function ChannelMonitorSmartSchedulePrimaryManagementDialog(
  props: Props
) {
  // A fresh management session must not reuse drafts from a previous opening.
  if (!props.open) return null
  return <PrimaryManagementSession {...props} />
}

function PrimaryManagementSession(props: Props) {
  const queryClient = useQueryClient()
  const [kind, setKind] = useState<'channel' | 'route'>('channel')
  const [search, setSearch] = useState('')
  const [group, setGroup] = useState('')
  const [status, setStatus] = useState('all')
  const [expanded, setExpanded] = useState<string | null>(null)
  const [editing, setEditing] = useState<PrimaryEntry | null>(null)
  const [dirty, setDirty] = useState(false)
  const [validDraft, setValidDraft] = useState(true)
  const lastEditedKey = useRef<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [discard, setDiscard] = useState<'close' | 'back' | null>(null)
  const [removing, setRemoving] = useState<PrimaryEntry[]>([])
  const [failures, setFailures] = useState<
    Array<{ entry: PrimaryEntry; message: string }>
  >([])
  const [rowSelection, setRowSelection] = useState<RowSelectionState>({})
  useEffect(() => {
    if (!editing && lastEditedKey.current) {
      document.getElementById(`primary-edit-${lastEditedKey.current}`)?.focus()
      lastEditedKey.current = null
    }
  }, [editing])
  const query = useQuery({
    queryKey: MANAGEMENT_KEY,
    queryFn: getChannelMonitorSmartSchedulePrimaryManagement,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    staleTime: 0,
  })
  const entries = useMemo<PrimaryEntry[]>(
    () => [
      ...(query.data?.data.channel_rules ?? []).map((rule) => ({
        ...rule,
        kind: 'channel' as const,
        key: `channel:${rule.channel_id}`,
      })),
      ...(query.data?.data.route_primaries ?? []).map((primary) => ({
        ...primary,
        kind: 'route' as const,
        key: JSON.stringify([
          'route',
          primary.channel_id,
          primary.group,
          primary.model,
        ]),
      })),
    ],
    [query.data]
  )
  const normalizedSearch = search.trim().toLowerCase()
  const filtered = useMemo(
    () =>
      entries.filter(
        (entry) =>
          entry.kind === kind &&
          (!group ||
            entry.group === group ||
            (entry.kind === 'channel' &&
              entry.routes.some((route) => route.group === group))) &&
          (kind === 'route' ||
            status === 'all' ||
            primaryEntryStatus(entry) === status) &&
          [
            entry.channel_name,
            String(entry.channel_id),
            entry.group,
            ...(entry.kind === 'route'
              ? [entry.model]
              : entry.routes.flatMap((route) => [
                  route.group,
                  route.model,
                  primaryRouteStatus(route),
                ])),
          ].some((value) => value.toLowerCase().includes(normalizedSearch))
      ),
    [entries, kind, group, status, normalizedSearch]
  )
  const groups = [
    ...new Set(
      entries
        .flatMap((entry) =>
          entry.kind === 'route'
            ? [entry.group]
            : [entry.group, ...entry.routes.map((route) => route.group)]
        )
        .filter(Boolean)
    ),
  ].sort()
  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: MANAGEMENT_KEY })
    try {
      await props.onActionComplete()
    } catch (error) {
      handleChannelMonitorMutationError(error)
    }
  }
  const removal = useMutation({
    mutationFn: async (targets: PrimaryEntry[]) => {
      const failed: Array<{ entry: PrimaryEntry; message: string }> = []
      // Sequential requests avoid concurrent writes to overlapping route pools.
      for (const entry of targets) {
        try {
          await updatePrimaryEntry({
            entry,
            durationMinutes: 0,
            allowStabilityDegrade: false,
          })
        } catch (error) {
          failed.push({
            entry,
            message:
              error instanceof Error ? error.message : '解除失败，请重试',
          })
        }
      }
      return { failed, succeeded: targets.length - failed.length }
    },
    onSuccess: async ({ failed, succeeded }) => {
      setRemoving([])
      setFailures(failed)
      setRowSelection(
        Object.fromEntries(failed.map((item) => [item.entry.key, true]))
      )
      if (succeeded > 0) {
        toast.success(`已解除 ${formatNumber(succeeded, 'zh-CN')} 项固定`)
      }
      if (failed.length > 0) {
        toast.error(
          `${formatNumber(failed.length, 'zh-CN')} 项解除失败，可重试失败项`
        )
      }
      await refresh()
    },
    onError: handleChannelMonitorMutationError,
  })
  const pending = saving || removal.isPending
  const columns = useMemo<ColumnDef<PrimaryEntry>[]>(
    () => [
      {
        id: 'select',
        header: ({ table: current }) => (
          <div className='flex items-center gap-2'>
            <Checkbox
              aria-label='选择当前筛选结果'
              disabled={pending || !filtered.length}
              checked={current.getIsAllRowsSelected()}
              indeterminate={current.getIsSomeRowsSelected()}
              onCheckedChange={(checked) =>
                current.toggleAllRowsSelected(checked === true)
              }
            />
            <span className='sm:hidden'>选择当前筛选结果</span>
          </div>
        ),
      },
      { id: 'name', header: kind === 'channel' ? '固定渠道' : '模型 / 分组' },
      { id: 'scope', header: kind === 'channel' ? '作用范围' : '固定渠道' },
      { id: 'status', header: kind === 'channel' ? '生效情况' : '固定期间' },
      { id: 'until', header: '固定至' },
      { id: 'actions', header: '操作' },
    ],
    [kind, pending, filtered.length]
  )
  const resolveRowSelection = (updater: Updater<RowSelectionState>) => {
    if (pending) return
    setRowSelection((current) =>
      typeof updater === 'function' ? updater(current) : updater
    )
  }
  const { table } = useDataTable({
    data: filtered,
    columns,
    getRowId: (entry) => entry.key,
    enableRowSelection: !pending,
    rowSelection,
    onRowSelectionChange: resolveRowSelection,
    withPaginationRowModel: false,
    columnVisibilityStorageKey: false,
    columnSizingStorageKey: false,
  })
  const selected = table.getSelectedRowModel().rows.map((row) => row.original)
  const clearSelection = () => {
    setRowSelection({})
    setFailures([])
    setExpanded(null)
  }
  const leave = (target: 'close' | 'back') => {
    if (pending) return
    if (dirty) {
      setDiscard(target)
      return
    }
    setEditing(null)
    if (target === 'close') props.onOpenChange(false)
  }

  return (
    <>
      <Dialog
        open
        onOpenChange={(open) => {
          if (!open) leave('close')
        }}
        title={editing ? '编辑固定项' : '固定项管理'}
        description={
          editing
            ? '调整固定时长与稳定性降级行为。'
            : '查看固定范围、生效情况，调整主渠道规则。'
        }
        contentHeight='min(36rem, 70dvh)'
        contentClassName='sm:w-[calc(100%-2rem)] sm:max-w-5xl'
        bodyClassName='py-0'
        headerClassName='pr-8'
        footer={
          editing ? (
            <>
              <Button
                type='button'
                variant='outline'
                disabled={pending}
                onClick={() => leave('back')}
              >
                取消
              </Button>
              <Button
                type='submit'
                form='primary-editor'
                disabled={pending || !dirty || !validDraft}
              >
                {saving ? <Spinner data-icon='inline-start' /> : null}保存更改
              </Button>
            </>
          ) : (
            <div className='flex w-full flex-wrap items-center justify-between gap-2'>
              {selected.length ? (
                <DataTableBulkActions
                  table={table}
                  entityName='固定项'
                  placement='inline'
                  selectedCount={selected.length}
                >
                  <Button
                    type='button'
                    size='sm'
                    variant='outline'
                    disabled={pending}
                    onClick={() => setRemoving(selected)}
                  >
                    解除所选固定
                  </Button>
                </DataTableBulkActions>
              ) : (
                <span className='text-muted-foreground text-xs'>
                  勾选后可批量解除固定
                </span>
              )}
              <Button
                type='button'
                variant='outline'
                disabled={pending}
                onClick={() => leave('close')}
              >
                关闭
              </Button>
            </div>
          )
        }
      >
        {editing ? (
          <ChannelMonitorSmartSchedulePrimaryEditor
            key={editing.key}
            entry={editing}
            onDirtyChange={setDirty}
            onValidityChange={setValidDraft}
            onPendingChange={setSaving}
            onSaved={async () => {
              await refresh()
              setEditing(null)
            }}
          />
        ) : (
          <>
            <div className='bg-popover sticky top-0 z-20 space-y-3 pb-3'>
              <Tabs
                value={kind}
                onValueChange={(value) => {
                  if (value !== 'channel' && value !== 'route') return
                  setKind(value)
                  setStatus('all')
                  clearSelection()
                }}
              >
                <TabsList variant='line' aria-label='固定项类型'>
                  <TabsTrigger value='channel'>
                    渠道固定（
                    {formatNumber(
                      entries.filter((entry) => entry.kind === 'channel')
                        .length,
                      'zh-CN'
                    )}
                    ）
                  </TabsTrigger>
                  <TabsTrigger value='route'>
                    单模型固定（
                    {formatNumber(
                      entries.filter((entry) => entry.kind === 'route').length,
                      'zh-CN'
                    )}
                    ）
                  </TabsTrigger>
                </TabsList>
              </Tabs>
              <div className='flex flex-wrap gap-2'>
                <Input
                  type='search'
                  aria-label='搜索固定项'
                  placeholder='搜索渠道、分组或模型'
                  className='min-w-0 flex-1 max-sm:basis-full'
                  value={search}
                  onChange={(event) => {
                    setSearch(event.target.value)
                    clearSelection()
                  }}
                />
                <Select
                  items={[
                    { value: '', label: '所有分组' },
                    ...groups.map((value) => ({ value, label: value })),
                  ]}
                  value={group}
                  onValueChange={(value) => {
                    setGroup(value ?? '')
                    clearSelection()
                  }}
                >
                  <SelectTrigger
                    aria-label='按分组筛选'
                    className='w-36 max-sm:flex-1'
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value=''>所有分组</SelectItem>
                    {groups.map((value) => (
                      <SelectItem key={value} value={value}>
                        {value}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {kind === 'channel' ? (
                  <Select
                    items={STATUS_OPTIONS}
                    value={status}
                    onValueChange={(value) => {
                      setStatus(value ?? 'all')
                      clearSelection()
                    }}
                  >
                    <SelectTrigger
                      aria-label='按生效情况筛选'
                      className='w-36 max-sm:flex-1'
                    >
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {STATUS_OPTIONS.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                ) : null}
              </div>
              <p className='text-muted-foreground text-xs'>
                显示 {formatNumber(filtered.length, 'zh-CN')} 项 ·
                单模型固定优先于渠道规则
              </p>
            </div>
            {query.isLoading ? (
              <div
                role='status'
                className='flex min-h-40 items-center justify-center gap-2'
              >
                <Spinner />
                正在加载固定项
              </div>
            ) : null}
            {query.isError ? (
              <Empty>
                <EmptyHeader>
                  <EmptyTitle>固定项加载失败</EmptyTitle>
                  <EmptyDescription>请重试或刷新页面</EmptyDescription>
                </EmptyHeader>
                <Button variant='outline' onClick={() => void query.refetch()}>
                  重试
                </Button>
              </Empty>
            ) : null}
            {!query.isLoading && !query.isError ? (
              <>
                {failures.length ? (
                  <div
                    role='alert'
                    className='mb-3 space-y-2 rounded-lg border p-3 text-sm'
                  >
                    <p>以下固定项解除失败：</p>
                    <ul>
                      {failures.map((item) => (
                        <li key={item.entry.key} className='break-words'>
                          {item.entry.channel_name} ·{' '}
                          {item.entry.group || '全部分组'}
                          {item.entry.kind === 'route'
                            ? ` / ${item.entry.model}`
                            : ''}
                          ：{item.message}
                        </li>
                      ))}
                    </ul>
                    <Button
                      variant='outline'
                      size='sm'
                      disabled={pending}
                      onClick={() =>
                        setRemoving(failures.map((item) => item.entry))
                      }
                    >
                      重试失败项
                    </Button>
                  </div>
                ) : null}
                <DataTableView
                  table={table}
                  emptyTitle={
                    entries.length ? '没有匹配的固定项' : '暂无固定项'
                  }
                  emptyDescription={
                    entries.length
                      ? '调整搜索或筛选条件后重试'
                      : '在渠道或模型路由中固定主渠道后，会显示在这里'
                  }
                  tableClassName='table-fixed max-sm:block [&_tbody]:max-sm:block'
                  tableBodyClassName='max-sm:[&>tr]:h-auto'
                  tableHeaderClassName='max-sm:block [&_tr]:max-sm:block [&_th:not(:first-child)]:max-sm:hidden [&_th:first-child]:max-sm:w-auto [&_th:first-child]:max-sm:block'
                  getColumnClassName={(id) => (id === 'select' ? 'w-10' : '')}
                  renderRow={(row) => (
                    <ChannelMonitorSmartSchedulePrimaryRow
                      key={row.id}
                      row={row}
                      expanded={expanded === row.id}
                      search={normalizedSearch}
                      disabled={pending}
                      onExpand={() =>
                        setExpanded(expanded === row.id ? null : row.id)
                      }
                      onEdit={() => {
                        lastEditedKey.current = row.original.key
                        setValidDraft(true)
                        setDirty(false)
                        setEditing(row.original)
                      }}
                      onRemove={() => setRemoving([row.original])}
                    />
                  )}
                />
              </>
            ) : null}
          </>
        )}
      </Dialog>
      <ConfirmDialog
        open={removing.length > 0}
        onOpenChange={(open) => {
          if (!open && !removal.isPending) setRemoving([])
        }}
        title={`解除 ${formatNumber(removing.length, 'zh-CN')} 项固定？`}
        confirmText='确认解除'
        cancelBtnText='取消'
        isLoading={removal.isPending}
        desc={
          <div className='space-y-3'>
            <p>仅解除以下选中的固定项：</p>
            <ul className='max-h-48 overflow-y-auto overscroll-contain'>
              {removing.map((entry) => (
                <li key={entry.key} className='break-words'>
                  {entry.channel_name} · {entry.group || '全部分组'}
                  {entry.kind === 'route' ? ` / ${entry.model}` : ' · 渠道固定'}
                </li>
              ))}
            </ul>
            <p>
              解除单模型固定后，仍有效的渠道规则可能接管；解除渠道规则不会删除单模型固定。实际结果以重新计算后的调度状态为准。
            </p>
          </div>
        }
        handleConfirm={() => removal.mutate(removing)}
      />
      <ConfirmDialog
        open={discard !== null}
        onOpenChange={(open) => {
          if (!open) setDiscard(null)
        }}
        title='放弃未保存的修改？'
        desc='当前固定项的修改尚未保存。'
        confirmText='放弃修改'
        cancelBtnText='继续编辑'
        handleConfirm={() => {
          const close = discard === 'close'
          setDiscard(null)
          setDirty(false)
          setEditing(null)
          if (close) props.onOpenChange(false)
        }}
      />
    </>
  )
}
