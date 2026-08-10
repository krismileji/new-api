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
import { zodResolver } from '@hookform/resolvers/zod'
import {
  Add01Icon,
  Delete02Icon,
  Edit02Icon,
  Settings02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMemo, useState } from 'react'
import {
  useForm,
  useWatch,
  type Resolver,
  type UseFormReturn,
} from 'react-hook-form'
import type { ZodType } from 'zod'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Form, FormDescription, FormItem } from '@/components/ui/form'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import {
  createChannelMonitorSmartSchedulePolicySchema,
  type ChannelMonitorSettingsFormValues,
  type ChannelMonitorSmartScheduleGroupPolicyFormValues,
  type ChannelMonitorSmartSchedulePolicyFormValues,
} from '../lib/schema'
import {
  CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE,
  createChannelMonitorSmartScheduleGroupPolicy,
} from '../lib/smart-schedule-group-policy'
import {
  getChannelMonitorSmartScheduleApplyModeLabel,
  getChannelMonitorSmartScheduleSamplingOrderLabel,
  getChannelMonitorSmartScheduleStrategyLabel,
} from '../lib/smart-schedule-options'
import { formatChannelMonitorSmartScheduleKTokens } from '../lib/smart-schedule-prompt-tokens'
import { channelMonitorDialogContentClassName } from './channel-monitor-dialog-layout'
import { ChannelMonitorSettingLabel } from './channel-monitor-setting-label'
import { ChannelMonitorSmartScheduleGroupPolicyFields } from './channel-monitor-smart-schedule-group-policy-fields'

type ChannelMonitorSmartScheduleGroupPoliciesProps = {
  form: UseFormReturn<ChannelMonitorSettingsFormValues>
  groupOptions: string[]
  modelOptionsByGroup: ReadonlyMap<string, string[]>
}

const EMPTY_GROUP_POLICIES: ChannelMonitorSmartScheduleGroupPolicyFormValues[] =
  []
const EMPTY_MODEL_OPTIONS: string[] = []

function groupPolicyModelSummary(models: string[]): string {
  if (models.length === 0) return '全部模型'
  if (models.length === 1) return models[0] ?? '全部模型'
  return `${models[0]} 等 ${models.length} 个`
}

function GroupPolicySampleModeBadge(props: {
  policy: ChannelMonitorSmartScheduleGroupPolicyFormValues
}) {
  if (props.policy.sampleMode === 'traffic') {
    return (
      <Badge variant='warning'>
        探索流量 {props.policy.explorationTrafficPercent}% · ≤{' '}
        {props.policy.explorationMaxPromptKTokens === 0
          ? '无限制'
          : formatChannelMonitorSmartScheduleKTokens(
              props.policy.explorationMaxPromptKTokens
            )}
      </Badge>
    )
  }
  if (props.policy.sampleMode === 'probe') {
    return (
      <Badge variant='secondary'>
        每 {props.policy.probeIntervalMinutes} 分钟文本探测
      </Badge>
    )
  }
  return <Badge variant='outline'>关闭</Badge>
}

function GroupPolicySamplingOrderBadge(props: {
  policy: ChannelMonitorSmartScheduleGroupPolicyFormValues
}) {
  return (
    <Badge variant='secondary'>
      {getChannelMonitorSmartScheduleSamplingOrderLabel(
        props.policy.samplingOrder
      )}
    </Badge>
  )
}

export function ChannelMonitorSmartScheduleGroupPolicies(
  props: ChannelMonitorSmartScheduleGroupPoliciesProps
) {
  const [editorOpen, setEditorOpen] = useState(false)
  const [editingGroup, setEditingGroup] = useState<string | null>(null)
  const [draftGroup, setDraftGroup] = useState('')
  const watchedGroupPolicies = useWatch({
    control: props.form.control,
    name: 'smartScheduleGroupPolicies',
  })
  const groupPolicies = watchedGroupPolicies ?? EMPTY_GROUP_POLICIES
  const configuredGroups = useMemo(
    () => new Set(groupPolicies.map((policy) => policy.group)),
    [groupPolicies]
  )
  const availableGroups = useMemo(() => {
    const groups = props.groupOptions.filter(
      (group) => !configuredGroups.has(group) || group === editingGroup
    )
    if (editingGroup && !groups.includes(editingGroup)) {
      groups.push(editingGroup)
    }
    return groups
  }, [configuredGroups, editingGroup, props.groupOptions])
  const rows = useMemo(
    () =>
      [...groupPolicies].sort((left, right) =>
        left.group.localeCompare(right.group, 'zh-CN')
      ),
    [groupPolicies]
  )
  const policyForm = useForm<ChannelMonitorSmartSchedulePolicyFormValues>({
    resolver: zodResolver(
      createChannelMonitorSmartSchedulePolicySchema() as unknown as ZodType<
        ChannelMonitorSmartSchedulePolicyFormValues,
        ChannelMonitorSmartSchedulePolicyFormValues
      >
    ) as unknown as Resolver<ChannelMonitorSmartSchedulePolicyFormValues>,
    defaultValues: CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE,
  })

  const openEditor = (group?: string) => {
    const targetGroup = group ?? availableGroups[0] ?? ''
    const policy = groupPolicies.find((policy) => policy.group === targetGroup)
    setEditingGroup(group ?? null)
    setDraftGroup(targetGroup)
    policyForm.reset(policy ?? CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE)
    setEditorOpen(true)
  }

  const changeDraftGroup = (group: string) => {
    setDraftGroup(group)
    policyForm.reset(CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE)
  }

  const removePolicy = (group: string) => {
    props.form.setValue(
      'smartScheduleGroupPolicies',
      groupPolicies.filter((policy) => policy.group !== group),
      { shouldDirty: true, shouldValidate: true }
    )
  }

  const savePolicy = policyForm.handleSubmit((policy) => {
    if (!draftGroup) return
    const savedPolicy = createChannelMonitorSmartScheduleGroupPolicy(
      draftGroup,
      policy
    )
    const nextPolicies = groupPolicies.filter(
      (item) => item.group !== draftGroup
    )
    nextPolicies.push(savedPolicy)
    props.form.setValue(
      'smartScheduleGroupPolicies',
      nextPolicies.sort((left, right) =>
        left.group.localeCompare(right.group, 'zh-CN')
      ),
      { shouldDirty: true, shouldValidate: true }
    )
    setEditorOpen(false)
  })

  return (
    <div className='flex min-w-0 flex-col gap-4'>
      <div className='flex min-w-0 flex-wrap items-start justify-between gap-3'>
        <div className='min-w-0 flex-1 basis-72'>
          <h3 className='text-sm font-medium'>分组策略</h3>
          <p className='text-muted-foreground mt-1 text-sm'>
            只有已配置策略的分组参与智能调度；新增、编辑或删除后，需保存智能调度设置才会生效
          </p>
        </div>
        <Button
          type='button'
          variant='outline'
          className='shrink-0'
          disabled={availableGroups.length === 0}
          onClick={() => openEditor()}
        >
          <HugeiconsIcon icon={Add01Icon} data-icon='inline-start' />
          新增分组策略
        </Button>
      </div>

      {rows.length === 0 ? (
        <Empty className='min-h-48 border'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <HugeiconsIcon icon={Settings02Icon} />
            </EmptyMedia>
            <EmptyTitle>尚未配置分组调度策略</EmptyTitle>
            <EmptyDescription>
              智能调度不会处理任何分组。新增策略后，该分组才会进入调度范围
            </EmptyDescription>
          </EmptyHeader>
          {availableGroups.length > 0 && (
            <EmptyContent>
              <Button
                type='button'
                variant='outline'
                onClick={() => openEditor()}
              >
                <HugeiconsIcon icon={Add01Icon} data-icon='inline-start' />
                新增分组策略
              </Button>
            </EmptyContent>
          )}
        </Empty>
      ) : (
        <div
          data-slot='group-policy-list'
          className='min-w-0 overflow-hidden rounded-md border'
        >
          {rows.map((policy) => (
            <article
              key={policy.group}
              data-slot='group-policy-summary'
              className='grid min-w-0 grid-cols-[minmax(0,1fr)_auto] gap-x-3 gap-y-4 border-b p-3 last:border-b-0 sm:p-4'
              aria-label={`${policy.group} 分组策略`}
            >
              <div className='min-w-0'>
                <h4 className='min-w-0 font-medium break-words'>
                  {policy.group}
                </h4>
                <p
                  className='text-muted-foreground mt-1 min-w-0 truncate text-xs'
                  title={policy.models.join(', ') || '全部模型'}
                >
                  模型范围：{groupPolicyModelSummary(policy.models)}
                </p>
              </div>

              <div className='flex shrink-0 justify-end gap-1'>
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <Button
                        type='button'
                        variant='ghost'
                        size='icon-sm'
                        onClick={() => openEditor(policy.group)}
                        aria-label={`编辑分组策略 ${policy.group}`}
                      >
                        <HugeiconsIcon icon={Edit02Icon} />
                      </Button>
                    }
                  />
                  <TooltipContent>编辑</TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <Button
                        type='button'
                        variant='ghost'
                        size='icon-sm'
                        onClick={() => removePolicy(policy.group)}
                        aria-label={`删除分组调度策略 ${policy.group}`}
                      >
                        <HugeiconsIcon icon={Delete02Icon} />
                      </Button>
                    }
                  />
                  <TooltipContent>删除策略</TooltipContent>
                </Tooltip>
              </div>

              <dl className='col-span-2 grid min-w-0 gap-3 sm:grid-cols-2 xl:grid-cols-4'>
                <div className='min-w-0'>
                  <dt className='text-muted-foreground text-xs'>调度与调整</dt>
                  <dd className='mt-1 min-w-0 break-words'>
                    {getChannelMonitorSmartScheduleStrategyLabel(
                      policy.strategy
                    )}{' '}
                    ·{' '}
                    {getChannelMonitorSmartScheduleApplyModeLabel(
                      policy.applyMode
                    )}
                  </dd>
                </div>
                <div className='min-w-0'>
                  <dt className='text-muted-foreground text-xs'>样本补充</dt>
                  <dd className='mt-1 flex min-w-0'>
                    <GroupPolicySampleModeBadge policy={policy} />
                  </dd>
                </div>
                <div className='min-w-0'>
                  <dt className='text-muted-foreground text-xs'>
                    统一采样顺序
                  </dt>
                  <dd className='mt-1 flex min-w-0'>
                    <GroupPolicySamplingOrderBadge policy={policy} />
                  </dd>
                </div>
                <div className='min-w-0'>
                  <dt className='text-muted-foreground text-xs'>稳定性</dt>
                  <dd className='mt-1 min-w-0 break-words'>
                    {policy.stabilityEnabled
                      ? `保护 ${policy.burstFailureWindowMinutes} 分钟 / 最近 ${policy.burstFailureWindowRequests} 次 · 连续 ${policy.consecutiveFailureThreshold} 次 / 失败 ${policy.burstFailureThresholdPercent}% · 恢复 ${policy.recoverySuccessThreshold} 次`
                      : '关闭'}
                  </dd>
                </div>
              </dl>
            </article>
          ))}
        </div>
      )}

      <Dialog
        open={editorOpen}
        onOpenChange={(open) => {
          setEditorOpen(open)
          if (!open) setEditingGroup(null)
        }}
      >
        <DialogContent
          className={channelMonitorDialogContentClassName(
            'grid-rows-[auto_minmax(0,1fr)_auto] ring-inset sm:max-w-5xl'
          )}
        >
          <DialogHeader>
            <DialogTitle>
              {editingGroup ? `编辑 ${editingGroup} 分组策略` : '新增分组策略'}
            </DialogTitle>
            <DialogDescription>
              先将该分组的调度方式、模型范围、评分和稳定性规则应用到当前设置，再保存智能调度设置使其生效
            </DialogDescription>
          </DialogHeader>

          <Form {...policyForm}>
            <div
              data-slot='group-policy-dialog-scroll-area'
              className='flex min-h-0 min-w-0 flex-col gap-5 overflow-x-hidden overflow-y-auto pr-1'
            >
              <FormItem>
                <ChannelMonitorSettingLabel label='分组' helpKey='group' />
                <Select
                  items={availableGroups.map((group) => ({
                    value: group,
                    label: group,
                  }))}
                  value={draftGroup}
                  disabled={editingGroup !== null}
                  onValueChange={(value) =>
                    value !== null && changeDraftGroup(value)
                  }
                >
                  <SelectTrigger className='w-full' aria-label='选择调度分组'>
                    <SelectValue placeholder='选择分组' />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      {availableGroups.map((group) => (
                        <SelectItem key={group} value={group}>
                          {group}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormDescription>
                  应用到当前设置并保存智能调度设置后，该分组才会进入调度范围
                </FormDescription>
              </FormItem>

              <ChannelMonitorSmartScheduleGroupPolicyFields
                form={policyForm}
                modelOptions={
                  props.modelOptionsByGroup.get(draftGroup) ??
                  EMPTY_MODEL_OPTIONS
                }
              />
            </div>
          </Form>

          <DialogFooter>
            {editingGroup && (
              <Button
                type='button'
                variant='outline'
                onClick={() => {
                  removePolicy(editingGroup)
                  setEditorOpen(false)
                }}
              >
                删除策略
              </Button>
            )}
            <Button
              type='button'
              variant='outline'
              onClick={() => setEditorOpen(false)}
            >
              取消
            </Button>
            <Button type='button' disabled={!draftGroup} onClick={savePolicy}>
              应用到当前设置
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
