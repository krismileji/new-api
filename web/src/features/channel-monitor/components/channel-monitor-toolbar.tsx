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
import { ArrowDown01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Fragment } from 'react'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

type ChannelMonitorToolbarProps = {
  actions: {
    batchTest: () => void
    taskHistory: () => void
    smartScheduleHistory: () => void
    settings: () => void
    variableGroups: () => void
    limitGroups: () => void
    upstreamAccounts: () => void
    automations: () => void
    tokenProtection: () => void
    groupMonitorSettings: () => void
    smartScheduleSettings: () => void
    refresh: () => void
  }
  autoUpdateLabel: string
  smartScheduleLabel: string
  refreshPending: boolean
  refreshDisabled: boolean
  onPrefetchTaskHistory: () => void
  onPrefetchSmartScheduleHistory: () => void
}

export function ChannelMonitorToolbar(props: ChannelMonitorToolbarProps) {
  const records = [
    {
      label: '倍率与余额更新记录',
      onSelect: props.actions.taskHistory,
      onPrefetch: props.onPrefetchTaskHistory,
    },
    {
      label: '智能调度执行记录',
      onSelect: props.actions.smartScheduleHistory,
      onPrefetch: props.onPrefetchSmartScheduleHistory,
    },
  ]
  const settingsGroups = [
    {
      label: '监控与调度',
      items: [
        {
          label: '渠道监控设置',
          description: props.autoUpdateLabel,
          onSelect: props.actions.settings,
        },
        {
          label: '分组监控设置',
          onSelect: props.actions.groupMonitorSettings,
        },
        {
          label: '智能调度设置',
          description: props.smartScheduleLabel,
          onSelect: props.actions.smartScheduleSettings,
        },
      ],
    },
    {
      label: '上游与共享资源',
      items: [
        { label: '上游账户', onSelect: props.actions.upstreamAccounts },
        { label: '上游自动任务', onSelect: props.actions.automations },
        { label: '共享请求与变量', onSelect: props.actions.variableGroups },
        { label: '共享限流组', onSelect: props.actions.limitGroups },
      ],
    },
    {
      label: '安全防护',
      items: [
        { label: 'API Key 自动禁用', onSelect: props.actions.tokenProtection },
      ],
    },
  ]

  return (
    <>
      <div className='flex items-center gap-1'>
        <DropdownMenu>
          <DropdownMenuTrigger render={<Button variant='ghost' />}>
            运行记录
            <HugeiconsIcon
              icon={ArrowDown01Icon}
              data-icon='inline-end'
              aria-hidden='true'
            />
          </DropdownMenuTrigger>
          <DropdownMenuContent align='end' className='w-56'>
            <DropdownMenuGroup>
              {records.map((record) => (
                <DropdownMenuItem
                  key={record.label}
                  render={<button type='button' />}
                  nativeButton
                  className='w-full'
                  onSelect={record.onSelect}
                  onMouseEnter={record.onPrefetch}
                  onFocus={record.onPrefetch}
                >
                  {record.label}
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
        <DropdownMenu>
          <DropdownMenuTrigger render={<Button variant='ghost' />}>
            配置管理
            <HugeiconsIcon
              icon={ArrowDown01Icon}
              data-icon='inline-end'
              aria-hidden='true'
            />
          </DropdownMenuTrigger>
          <DropdownMenuContent align='end' className='w-72'>
            {settingsGroups.map((group, index) => (
              <Fragment key={group.label}>
                {index > 0 && <DropdownMenuSeparator />}
                <DropdownMenuGroup>
                  <DropdownMenuLabel>{group.label}</DropdownMenuLabel>
                  {group.items.map((item) => (
                    <DropdownMenuItem
                      key={item.label}
                      render={<button type='button' />}
                      nativeButton
                      onSelect={item.onSelect}
                      aria-label={item.label}
                      className='w-full flex-col items-start gap-0.5 text-left'
                    >
                      <span>{item.label}</span>
                      {'description' in item && (
                        <span className='text-muted-foreground text-xs leading-relaxed break-words'>
                          {item.description}
                        </span>
                      )}
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuGroup>
              </Fragment>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <div className='flex items-center gap-2'>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                onClick={props.actions.batchTest}
                aria-label='渠道连通性测试'
              >
                连通性测试
              </Button>
            }
          />
          <TooltipContent>
            批量测试渠道，或对单个渠道和模型进行并发循环测试
          </TooltipContent>
        </Tooltip>
        <Button
          className='min-w-20'
          onClick={props.actions.refresh}
          disabled={props.refreshPending || props.refreshDisabled}
          aria-label='刷新'
          aria-busy={props.refreshPending}
        >
          {props.refreshPending ? '刷新中…' : '刷新'}
        </Button>
      </div>
    </>
  )
}
