/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import { getChannels } from '../../api'
import {
  createLogicalChannelGroup,
  deleteLogicalChannelGroup,
  getLogicalChannelGroups,
  precheckLogicalChannelGroup,
  replaceLogicalChannelGroupMembers,
  updateLogicalChannelGroupStatus,
  type LogicalChannelAddressPrecheck,
  type LogicalChannelGroup,
  type LogicalChannelGroupMemberInput,
} from '../../logical-groups-api'
import type { Channel } from '../../types'

type LogicalGroupsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  canEdit?: boolean
  canDelete?: boolean
}

type GroupForm = {
  id?: number
  revision?: number
  name: string
  remark: string
  members: Record<number, number>
}

const emptyForm = (): GroupForm => ({
  name: '',
  remark: '',
  members: {},
})

function errorMessage(error: unknown) {
  if (typeof error === 'object' && error !== null) {
    const response = (error as { response?: { data?: { message?: unknown } } })
      .response
    if (typeof response?.data?.message === 'string') {
      return response.data.message
    }
  }
  if (error instanceof Error && error.message) return error.message
  return '请求失败，请稍后重试'
}

function isRevisionConflict(error: unknown) {
  if (typeof error !== 'object' || error === null) return false
  return (error as { response?: { status?: number } }).response?.status === 409
}

async function loadAllChannels() {
  const firstResponse = await getChannels({ p: 1, page_size: 100 })
  if (!firstResponse.success || !firstResponse.data) {
    throw new Error(firstResponse.message || '渠道列表加载失败')
  }

  const pageSize = firstResponse.data.page_size
  if (!Number.isFinite(pageSize) || pageSize <= 0) {
    throw new Error('渠道列表分页信息无效')
  }
  const totalPages = Math.max(1, Math.ceil(firstResponse.data.total / pageSize))
  const remainingResponses = await Promise.all(
    Array.from({ length: totalPages - 1 }, (_, index) =>
      getChannels({ p: index + 2, page_size: 100 })
    )
  )
  const channels: Channel[] = [...firstResponse.data.items]
  for (const response of remainingResponses) {
    if (!response.success || !response.data) {
      throw new Error(response.message || '渠道列表加载失败')
    }
    channels.push(...response.data.items)
  }
  return channels
}

export function LogicalGroupsDialog(props: LogicalGroupsDialogProps) {
  const canDelete = props.canDelete ?? props.canEdit
  const [groups, setGroups] = useState<LogicalChannelGroup[]>([])
  const [channels, setChannels] = useState<Channel[]>([])
  const [form, setForm] = useState<GroupForm | null>(null)
  const [precheck, setPrecheck] =
    useState<LogicalChannelAddressPrecheck | null>(null)
  const [precheckKey, setPrecheckKey] = useState('')
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [prechecking, setPrechecking] = useState(false)
  const [saving, setSaving] = useState(false)
  const [statusUpdating, setStatusUpdating] = useState<number | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<LogicalChannelGroup | null>(
    null
  )
  const loadGeneration = useRef(0)

  const loadData = useCallback(async () => {
    const generation = ++loadGeneration.current
    setLoading(true)
    setLoadError(null)
    try {
      const [groupResponse, channelItems] = await Promise.all([
        getLogicalChannelGroups(),
        loadAllChannels(),
      ])
      if (generation !== loadGeneration.current) return
      if (!groupResponse.success || !groupResponse.data) {
        throw new Error(groupResponse.message || '同渠道配置加载失败')
      }
      setGroups(groupResponse.data)
      setChannels(channelItems)
    } catch (error: unknown) {
      if (generation !== loadGeneration.current) return
      const message = errorMessage(error)
      setLoadError(message)
      toast.error(message)
    } finally {
      if (generation === loadGeneration.current) setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (!props.open) return
    void loadData()
    return () => {
      loadGeneration.current += 1
    }
  }, [loadData, props.open])

  const selectedKey = useMemo(
    () =>
      form
        ? Object.keys(form.members)
            .map(Number)
            .sort((a, b) => a - b)
            .join(',')
        : '',
    [form]
  )
  const selectedIds = useMemo(
    () => (selectedKey ? selectedKey.split(',').map(Number) : []),
    [selectedKey]
  )
  const hasForm = form !== null

  useEffect(() => {
    if (!props.open || !hasForm || selectedIds.length === 0) {
      setPrecheck(null)
      setPrecheckKey('')
      return
    }
    let active = true
    setPrechecking(true)
    void precheckLogicalChannelGroup(selectedIds)
      .then((response) => {
        if (!active) return
        if (!response.success || !response.data) {
          setPrecheck({
            compatible: false,
            members: [],
            error: response.message || '地址预检失败',
          })
        } else {
          setPrecheck(response.data)
        }
        setPrecheckKey(selectedKey)
      })
      .catch((error: unknown) => {
        if (!active) return
        setPrecheck({
          compatible: false,
          members: [],
          error: errorMessage(error),
        })
        setPrecheckKey(selectedKey)
      })
      .finally(() => {
        if (active) setPrechecking(false)
      })
    return () => {
      active = false
    }
  }, [hasForm, props.open, selectedIds, selectedKey])

  const ownerByChannelId = useMemo(() => {
    const owners = new Map<number, number>()
    for (const group of groups) {
      for (const member of group.members) {
        owners.set(member.channel_id, group.id)
      }
    }
    return owners
  }, [groups])

  const openCreate = () => {
    setForm(emptyForm())
    setPrecheck(null)
    setPrecheckKey('')
  }

  const openEdit = (group: LogicalChannelGroup) => {
    const members: Record<number, number> = {}
    for (const member of group.members) {
      members[member.channel_id] = member.weight
    }
    setForm({
      id: group.id,
      revision: group.revision,
      name: group.name,
      remark: group.remark || '',
      members,
    })
    setPrecheck(null)
    setPrecheckKey('')
  }

  const closeForm = () => {
    setForm(null)
    setPrecheck(null)
    setPrecheckKey('')
  }

  const handleDialogOpenChange = (open: boolean) => {
    if (!open) {
      setForm(null)
      setDeleteTarget(null)
      setPrecheck(null)
      setPrecheckKey('')
    }
    props.onOpenChange(open)
  }

  const toggleMember = (channelId: number, checked: boolean) => {
    if (!form) return
    setForm((current) => {
      if (!current) return current
      const members = { ...current.members }
      if (checked) members[channelId] = members[channelId] ?? 1
      else delete members[channelId]
      return { ...current, members }
    })
  }

  const updateWeight = (channelId: number, value: string) => {
    if (!form) return
    const parsed = Number(value)
    setForm((current) =>
      current
        ? {
            ...current,
            members: {
              ...current.members,
              [channelId]:
                Number.isFinite(parsed) && parsed >= 0 ? Math.floor(parsed) : 0,
            },
          }
        : current
    )
  }

  const handleSave = async () => {
    if (!form) return
    const name = form.name.trim()
    if (!name) {
      toast.error('请输入逻辑渠道组名称')
      return
    }
    if (selectedIds.length === 0) {
      toast.error('至少选择一个渠道')
      return
    }
    setSaving(true)
    try {
      let result = precheck
      if (precheckKey !== selectedKey) {
        const response = await precheckLogicalChannelGroup(selectedIds)
        if (!response.success || !response.data) {
          throw new Error(response.message || '地址预检失败')
        }
        result = response.data
        setPrecheck(result)
        setPrecheckKey(selectedKey)
      }
      if (!result?.compatible) {
        throw new Error(result?.error || '所选渠道的请求地址不一致')
      }
      const members: LogicalChannelGroupMemberInput[] = selectedIds.map(
        (channelId) => ({
          channel_id: channelId,
          weight: form.members[channelId] ?? 1,
        })
      )
      const response = form.id
        ? await replaceLogicalChannelGroupMembers({
            id: form.id,
            revision: form.revision || 0,
            members,
          })
        : await createLogicalChannelGroup({
            name,
            remark: form.remark.trim(),
            members,
          })
      if (!response.success || !response.data) {
        throw new Error(response.message || '保存同渠道配置失败')
      }
      setGroups((current) => {
        const next = current.filter((item) => item.id !== response.data?.id)
        return [...next, response.data as LogicalChannelGroup].sort(
          (a, b) => a.id - b.id
        )
      })
      toast.success('同渠道配置已保存')
      closeForm()
    } catch (error: unknown) {
      if (isRevisionConflict(error)) {
        toast.error('配置已被其他管理员修改，请刷新后重试')
      } else {
        toast.error(errorMessage(error))
      }
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      const response = await deleteLogicalChannelGroup({
        id: deleteTarget.id,
        revision: deleteTarget.revision,
      })
      if (!response.success) {
        throw new Error(response.message || '删除同渠道配置失败')
      }
      setGroups((current) =>
        current.filter((item) => item.id !== deleteTarget.id)
      )
      if (form?.id === deleteTarget.id) closeForm()
      toast.success('同渠道配置已删除')
      setDeleteTarget(null)
    } catch (error: unknown) {
      if (isRevisionConflict(error)) {
        toast.error('配置已被其他管理员修改，请刷新后重试')
      } else {
        toast.error(errorMessage(error))
      }
    } finally {
      setDeleting(false)
    }
  }

  const handleStatusChange = async (
    group: LogicalChannelGroup,
    enabled: boolean
  ) => {
    if (!props.canEdit || statusUpdating !== null) return
    setStatusUpdating(group.id)
    try {
      const response = await updateLogicalChannelGroupStatus({
        id: group.id,
        revision: group.revision,
        status: enabled ? 1 : 2,
      })
      if (!response.success || !response.data) {
        throw new Error(response.message || '更新共享功能状态失败')
      }
      const updated = response.data
      setGroups((current) =>
        current.map((item) => (item.id === updated.id ? updated : item))
      )
      setForm((current) =>
        current?.id === updated.id
          ? { ...current, revision: updated.revision }
          : current
      )
      toast.success(enabled ? '共享功能已启用' : '共享功能已停用')
    } catch (error: unknown) {
      if (isRevisionConflict(error)) {
        toast.error('配置已被其他管理员修改，请刷新后重试')
        void loadData()
      } else {
        toast.error(errorMessage(error))
      }
    } finally {
      setStatusUpdating(null)
    }
  }

  const precheckErrors =
    precheck?.members.filter((member) => member.error) ?? []
  let precheckContent
  if (prechecking) {
    precheckContent = (
      <div className='text-muted-foreground mt-1'>正在检查地址…</div>
    )
  } else if (precheck?.compatible) {
    precheckContent = (
      <div className='text-success mt-1 break-all'>
        地址一致：{precheck.normalized_address}
      </div>
    )
  } else if (precheck) {
    precheckContent = (
      <div className='text-destructive mt-1'>
        {precheck.error || '所选渠道的请求地址不一致'}
        {precheckErrors.length > 0 ? (
          <ul className='mt-1 list-inside list-disc'>
            {precheckErrors.map((member) => (
              <li key={member.channel_id}>
                渠道 #{member.channel_id}：{member.error}
              </li>
            ))}
          </ul>
        ) : null}
      </div>
    )
  } else {
    precheckContent = (
      <div className='text-muted-foreground mt-1'>
        选择渠道后自动检查规范化地址
      </div>
    )
  }

  return (
    <>
      <Dialog open={props.open} onOpenChange={handleDialogOpenChange}>
        <DialogContent
          className='flex max-h-[calc(100dvh-2rem)] flex-col overflow-hidden sm:max-w-4xl'
          data-testid='logical-groups-dialog-content'
        >
          <DialogHeader>
            <DialogTitle>同渠道配置</DialogTitle>
            <DialogDescription>
              将请求地址一致的物理渠道配置为同一逻辑渠道，组内仅共享调度、状态探测和模型检测。
            </DialogDescription>
          </DialogHeader>

          <div
            className='min-h-0 flex-1 overflow-y-auto overscroll-contain pr-1'
            data-testid='logical-groups-dialog-body'
          >
            {loading ? (
              <div className='text-muted-foreground py-12 text-center'>
                正在加载同渠道配置…
              </div>
            ) : null}
            {!loading && loadError ? (
              <div
                className='flex min-h-64 flex-col items-center justify-center gap-3 text-center'
                role='alert'
              >
                <div className='text-destructive text-sm'>{loadError}</div>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => void loadData()}
                >
                  重新加载
                </Button>
              </div>
            ) : null}
            {!loading && !loadError ? (
              <div className='grid min-h-80 gap-4 md:grid-cols-[minmax(15rem,0.8fr)_minmax(22rem,1.2fr)]'>
                <div className='flex min-h-0 flex-col gap-3'>
                  <div className='flex items-center justify-between gap-2'>
                    <span className='text-sm font-medium'>
                      已配置的逻辑渠道
                    </span>
                    <Button
                      size='sm'
                      onClick={openCreate}
                      disabled={!props.canEdit}
                    >
                      新建配置
                    </Button>
                  </div>
                  {groups.length === 0 ? (
                    <div className='text-muted-foreground rounded-lg border border-dashed p-6 text-center text-sm'>
                      暂无同渠道配置
                    </div>
                  ) : (
                    <div className='flex max-h-[28rem] flex-col gap-2 overflow-y-auto pr-1'>
                      {groups.map((group) => (
                        <div
                          key={group.id}
                          className='rounded-lg border p-3 text-sm'
                          data-testid={`logical-group-${group.id}`}
                        >
                          <div className='flex items-start justify-between gap-2'>
                            <div className='min-w-0'>
                              <div className='truncate font-medium'>
                                {group.name}
                              </div>
                              <div className='text-muted-foreground mt-1 text-xs'>
                                {group.members.length} 个渠道 · revision{' '}
                                {group.revision}
                              </div>
                            </div>
                            <div className='flex shrink-0 gap-1'>
                              <Button
                                variant='ghost'
                                size='sm'
                                onClick={() => openEdit(group)}
                                disabled={!props.canEdit}
                              >
                                编辑
                              </Button>
                              <Button
                                variant='ghost'
                                size='sm'
                                className='text-destructive'
                                onClick={() => setDeleteTarget(group)}
                                disabled={!canDelete}
                              >
                                删除
                              </Button>
                            </div>
                          </div>
                          <div className='text-muted-foreground mt-2 flex flex-col gap-1 text-xs'>
                            {group.members.map((member) => (
                              <div
                                key={member.channel_id}
                                className='flex justify-between gap-2'
                              >
                                <span className='truncate'>
                                  {member.channel_name ||
                                    `渠道 #${member.channel_id}`}
                                </span>
                                <span className='shrink-0'>
                                  weight {member.weight}
                                </span>
                              </div>
                            ))}
                          </div>
                          <Separator className='my-3' />
                          <label className='flex items-center justify-between gap-3 text-xs'>
                            <span>
                              {group.status === 1
                                ? '共享功能已启用'
                                : '共享功能已停用'}
                            </span>
                            <Switch
                              size='sm'
                              checked={group.status === 1}
                              onCheckedChange={(checked) =>
                                void handleStatusChange(group, checked)
                              }
                              disabled={
                                !props.canEdit || statusUpdating !== null
                              }
                              aria-label={`${group.name}共享功能`}
                            />
                          </label>
                        </div>
                      ))}
                    </div>
                  )}
                </div>

                <div className='min-h-0 rounded-lg border p-4'>
                  {!form ? (
                    <div className='text-muted-foreground flex h-full min-h-64 items-center justify-center text-center text-sm'>
                      选择一个逻辑渠道进行编辑，或新建配置
                    </div>
                  ) : (
                    <div className='flex h-full min-h-64 flex-col gap-4'>
                      <div className='flex items-center justify-between gap-2'>
                        <div className='font-medium'>
                          {form.id ? '编辑同渠道配置' : '新建同渠道配置'}
                        </div>
                        <Button
                          variant='ghost'
                          size='sm'
                          onClick={closeForm}
                          disabled={saving}
                        >
                          取消
                        </Button>
                      </div>
                      <div className='grid gap-3 sm:grid-cols-2'>
                        <label className='grid gap-1 text-sm'>
                          <span>名称</span>
                          <Input
                            value={form.name}
                            onChange={(event) =>
                              setForm({ ...form, name: event.target.value })
                            }
                            placeholder='例如：OpenAI 主渠道'
                            disabled={saving || form.id !== undefined}
                          />
                        </label>
                        <label className='grid gap-1 text-sm'>
                          <span>备注</span>
                          <Textarea
                            value={form.remark}
                            onChange={(event) =>
                              setForm({ ...form, remark: event.target.value })
                            }
                            placeholder='可选'
                            className='h-8 min-h-8 resize-none'
                            disabled={saving || form.id !== undefined}
                          />
                        </label>
                      </div>
                      <div className='min-h-0 flex-1'>
                        <div className='mb-2 text-sm font-medium'>
                          选择成员渠道及 weight
                        </div>
                        <div className='flex max-h-56 flex-col gap-2 overflow-y-auto rounded-lg border p-2'>
                          {channels.length === 0 ? (
                            <div className='text-muted-foreground p-4 text-center text-sm'>
                              暂无可用渠道
                            </div>
                          ) : (
                            channels.map((channel) => {
                              const owner = ownerByChannelId.get(channel.id)
                              const selected =
                                form.members[channel.id] !== undefined
                              const unavailable =
                                owner !== undefined && owner !== form.id
                              return (
                                <div
                                  key={channel.id}
                                  className='hover:bg-muted/50 flex items-center gap-2 rounded-md px-2 py-1.5'
                                >
                                  <Checkbox
                                    checked={selected}
                                    onCheckedChange={(checked) =>
                                      toggleMember(channel.id, checked === true)
                                    }
                                    disabled={saving || unavailable}
                                    aria-label={`选择渠道 ${channel.name}`}
                                  />
                                  <span className='min-w-0 flex-1 truncate text-sm'>
                                    {channel.name}{' '}
                                    <span className='text-muted-foreground'>
                                      #{channel.id}
                                    </span>
                                    {unavailable ? (
                                      <span className='text-muted-foreground'>
                                        （已归组）
                                      </span>
                                    ) : null}
                                  </span>
                                  {selected ? (
                                    <label className='flex w-28 items-center gap-1 text-xs'>
                                      <span className='text-muted-foreground'>
                                        weight
                                      </span>
                                      <Input
                                        type='number'
                                        min={0}
                                        max={1000000}
                                        value={form.members[channel.id]}
                                        onChange={(event) =>
                                          updateWeight(
                                            channel.id,
                                            event.target.value
                                          )
                                        }
                                        disabled={saving}
                                        aria-label={`${channel.name} weight`}
                                      />
                                    </label>
                                  ) : null}
                                </div>
                              )
                            })
                          )}
                        </div>
                      </div>
                      <div
                        className='rounded-lg border p-3 text-xs'
                        data-testid='logical-group-precheck'
                      >
                        <div className='font-medium'>请求地址预检</div>
                        {precheckContent}
                      </div>
                      <DialogFooter className='-mx-4 -mb-4'>
                        <Button
                          onClick={() => void handleSave()}
                          disabled={saving || prechecking || !props.canEdit}
                        >
                          {saving ? '保存中…' : '保存配置'}
                        </Button>
                      </DialogFooter>
                    </div>
                  )}
                </div>
              </div>
            ) : null}
          </div>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => {
          if (!open && !deleting) setDeleteTarget(null)
        }}
        title='删除同渠道配置？'
        desc='删除后只会解除逻辑渠道关联，不会删除物理渠道及其历史数据。'
        confirmText='删除'
        destructive
        isLoading={deleting}
        handleConfirm={() => void handleDelete()}
      />
    </>
  )
}
