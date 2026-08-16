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
import { Cancel01Icon, PauseIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useMemo, useState } from 'react'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Spinner } from '@/components/ui/spinner'

import { isChannelModelDetectionRunActive } from '../lib/model-detection'
import {
  cancelChannelModelDetectionRun,
  getChannelModelDetectionRuns,
  isChannelModelDetectionConfigConflict,
  isChannelModelDetectionInfrastructureConflict,
  updateChannelModelDetectionConfig,
} from '../lib/model-detection-channel-api'
import { channelModelDetectionRequestErrorMessage } from '../lib/model-detection-settings-api'
import {
  CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
  getChannelMonitorActiveRefetchInterval,
} from '../lib/query-options'
import type {
  ChannelModelDetectionChannel,
  ChannelModelDetectionHistoryQuery,
  ChannelModelDetectionRunStatus,
} from '../types-model-detection'
import { ChannelModelDetectionConfigSheet } from './channel-model-detection-config-sheet'
import { ChannelModelDetectionHistorySheet } from './channel-model-detection-history-sheet'
import { ChannelModelDetectionRunDetailSheet } from './channel-model-detection-run-detail-sheet'
import { ChannelModelDetectionRunDialog } from './channel-model-detection-run-dialog'
import { ChannelModelDetectionSettingsSheet } from './channel-model-detection-settings-sheet'
import {
  ChannelModelDetectionView,
  type ChannelModelDetectionViewProps,
} from './channel-model-detection-view'

const OVERVIEW_QUERY_KEY = [
  'channel-monitor',
  'model-detection',
  'overview',
] as const

const DEFAULT_HISTORY_QUERY: ChannelModelDetectionHistoryQuery = {
  page: 1,
  page_size: 20,
  trigger: '',
  status: '',
  model: '',
  outcome: '',
}

export type ChannelModelDetectionWorkspaceProps = ChannelModelDetectionViewProps

export function ChannelModelDetectionWorkspace(
  props: ChannelModelDetectionWorkspaceProps
) {
  const queryClient = useQueryClient()
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [configChannelId, setConfigChannelId] = useState<number | null>(null)
  const [runChannelId, setRunChannelId] = useState<number | null>(null)
  const [historyChannelId, setHistoryChannelId] = useState<number | null>(null)
  const [cancelChannelId, setCancelChannelId] = useState<number | null>(null)
  const [pauseAllOpen, setPauseAllOpen] = useState(false)
  const [detailRunId, setDetailRunId] = useState<string | null>(null)
  const [historyQueryInput, setHistoryQueryInput] = useState(
    DEFAULT_HISTORY_QUERY
  )

  const channelsById = useMemo(
    () =>
      new Map(
        (props.overview?.channels ?? []).map((channel) => [channel.id, channel])
      ),
    [props.overview?.channels]
  )
  const configChannel = configChannelId
    ? (channelsById.get(configChannelId) ?? null)
    : null
  const runChannel = runChannelId
    ? (channelsById.get(runChannelId) ?? null)
    : null
  const historyChannel = historyChannelId
    ? (channelsById.get(historyChannelId) ?? null)
    : null
  const cancelChannel = cancelChannelId
    ? (channelsById.get(cancelChannelId) ?? null)
    : null
  const scheduledChannels = useMemo(
    () =>
      (props.overview?.channels ?? []).filter(
        (channel) => channel.config?.schedule_enabled === true
      ),
    [props.overview?.channels]
  )

  const refreshOverview = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: OVERVIEW_QUERY_KEY })
  }, [queryClient])
  const refreshChannelHistory = useCallback(
    (channelId: number) => {
      void queryClient.invalidateQueries({
        queryKey: ['channel-monitor', 'model-detection', 'history', channelId],
      })
    },
    [queryClient]
  )

  const historyQuery = useQuery({
    queryKey: [
      'channel-monitor',
      'model-detection',
      'history',
      historyChannelId,
      historyQueryInput,
    ],
    queryFn: () =>
      getChannelModelDetectionRuns(historyChannelId ?? 0, historyQueryInput),
    enabled: historyChannelId != null,
    placeholderData: (previous) => previous,
    staleTime: 0,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    refetchOnMount: 'always',
    refetchInterval: (currentQuery) =>
      getChannelMonitorActiveRefetchInterval(
        currentQuery.state.data?.items.some((run) =>
          isChannelModelDetectionRunActive(run.status)
        ) ?? false
      ),
  })

  const scheduleMutation = useMutation({
    mutationFn: (channel: ChannelModelDetectionChannel) => {
      if (!channel.config) throw new Error('渠道尚未配置模型检测目标')
      return updateChannelModelDetectionConfig(channel.id, {
        schedule_enabled: !channel.config.schedule_enabled,
        targets: [...channel.targets]
          .filter((target) => target.enabled)
          .sort((left, right) => left.position - right.position)
          .map((target) => ({
            target_key: target.target_key,
            request_model: target.request_model,
            claimed_model: target.claimed_model,
          })),
        revision: channel.config.revision,
      })
    },
    onSuccess: (saved, channel) => {
      toast.success(
        saved.schedule_enabled
          ? '该渠道已参加统一定时检测'
          : '该渠道已退出统一定时检测'
      )
      refreshOverview()
      refreshChannelHistory(channel.id)
    },
    onError: (error) => {
      const prefix = isChannelModelDetectionConfigConflict(error)
        ? '渠道配置已变化，请刷新后重试：'
        : '更新渠道定时检测失败：'
      toast.error(`${prefix}${channelModelDetectionRequestErrorMessage(error)}`)
      refreshOverview()
    },
  })

  const pauseAllMutation = useMutation({
    mutationFn: (channels: ChannelModelDetectionChannel[]) =>
      Promise.allSettled(
        channels.map((channel) => {
          if (!channel.config) {
            throw new Error('渠道尚未配置模型检测目标')
          }
          return updateChannelModelDetectionConfig(channel.id, {
            schedule_enabled: false,
            targets: [...channel.targets]
              .filter((target) => target.enabled)
              .sort((left, right) => left.position - right.position)
              .map((target) => ({
                target_key: target.target_key,
                request_model: target.request_model,
                claimed_model: target.claimed_model,
              })),
            revision: channel.config.revision,
          })
        })
      ),
    onSuccess: (results) => {
      const pausedCount = results.filter(
        (result) => result.status === 'fulfilled'
      ).length
      const failedCount = results.length - pausedCount
      setPauseAllOpen(false)
      refreshOverview()
      if (failedCount === 0) {
        toast.success(`已暂停 ${pausedCount} 个渠道的模型定时检测`)
      } else if (pausedCount === 0) {
        toast.error(`暂停失败，${failedCount} 个渠道未更新，请刷新后重试`)
      } else {
        toast.error(
          `已暂停 ${pausedCount} 个渠道，${failedCount} 个渠道失败，请刷新后重试`
        )
      }
    },
  })

  const cancelMutation = useMutation({
    mutationFn: (variables: { channelId: number; runId: string }) =>
      cancelChannelModelDetectionRun(variables.runId),
    onSuccess: (result, variables) => {
      toast.success(
        result.status === 'canceled' ? '模型检测任务已取消' : '取消请求已提交'
      )
      setCancelChannelId(null)
      refreshOverview()
      refreshChannelHistory(variables.channelId)
      void queryClient.invalidateQueries({
        queryKey: ['channel-monitor', 'model-detection', 'run', result.run_id],
      })
    },
    onError: (error) => {
      const prefix = isChannelModelDetectionInfrastructureConflict(error)
        ? '取消状态发生冲突：'
        : '取消模型检测失败：'
      toast.error(`${prefix}${channelModelDetectionRequestErrorMessage(error)}`)
      if (isChannelModelDetectionInfrastructureConflict(error)) {
        setCancelChannelId(null)
      }
      refreshOverview()
    },
  })

  let actionPendingChannelId: number | null = null
  if (scheduleMutation.isPending) {
    actionPendingChannelId = scheduleMutation.variables?.id ?? null
  } else if (cancelMutation.isPending) {
    actionPendingChannelId = cancelMutation.variables?.channelId ?? null
  }

  function handleSurfaceClose(setOpen: (value: null) => void) {
    setOpen(null)
    refreshOverview()
  }

  function handleTerminalRun(
    runId: string,
    _status: ChannelModelDetectionRunStatus
  ) {
    refreshOverview()
    if (historyChannelId != null) refreshChannelHistory(historyChannelId)
    void queryClient.invalidateQueries({
      queryKey: ['channel-monitor', 'model-detection', 'run', runId],
      refetchType: 'none',
    })
  }

  const settingsSurface = (
    <ChannelModelDetectionSettingsSheet
      open={settingsOpen}
      onOpenChange={(open) => {
        setSettingsOpen(open)
        if (!open) refreshOverview()
      }}
      onSaved={refreshOverview}
    />
  )

  const channelSurface = (
    <>
      <ChannelModelDetectionConfigSheet
        channel={configChannel}
        detectorURLConfigured={Boolean(
          props.overview?.settings.detector_url_configured
        )}
        open={configChannelId != null}
        onOpenChange={(open) => {
          if (!open) handleSurfaceClose(setConfigChannelId)
        }}
        onSaved={() => {
          refreshOverview()
          if (configChannelId != null) refreshChannelHistory(configChannelId)
        }}
        onRefreshChannel={refreshOverview}
      />
      <ChannelModelDetectionRunDialog
        channel={runChannel}
        open={runChannelId != null}
        onOpenChange={(open) => {
          if (!open) handleSurfaceClose(setRunChannelId)
        }}
        onRunAccepted={(run, channelId) => {
          setDetailRunId(run.run_id)
          refreshOverview()
          refreshChannelHistory(channelId)
        }}
        onRefreshRequested={refreshOverview}
      />
      <AlertDialog
        open={pauseAllOpen}
        onOpenChange={(open) => {
          if (!pauseAllMutation.isPending) setPauseAllOpen(open)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>暂停全部模型定时检测？</AlertDialogTitle>
            <AlertDialogDescription className='space-y-2'>
              <span className='block'>
                将让全部 {scheduledChannels.length}
                个已参加统一定时检测的渠道退出定时检测，不受当前筛选条件影响。
              </span>
              <span className='block'>已经运行的检测轮次不会被取消。</span>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={pauseAllMutation.isPending}>
              返回
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={
                scheduledChannels.length === 0 || pauseAllMutation.isPending
              }
              onClick={() => pauseAllMutation.mutate(scheduledChannels)}
            >
              {pauseAllMutation.isPending ? (
                <Spinner data-icon='inline-start' />
              ) : (
                <HugeiconsIcon icon={PauseIcon} data-icon='inline-start' />
              )}
              确认暂停
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog
        open={cancelChannelId != null}
        onOpenChange={(open) => {
          if (!open && !cancelMutation.isPending) {
            setCancelChannelId(null)
            refreshOverview()
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>取消当前模型检测？</AlertDialogTitle>
            <AlertDialogDescription>
              {cancelChannel?.active_run
                ? `将请求取消 ${cancelChannel.name} 的轮次 ${cancelChannel.active_run.run_id}。已经发出的上游请求仍会按实际 Usage 记录成本。`
                : '当前活动轮次已变化，请关闭后刷新状态。'}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={cancelMutation.isPending}>
              返回
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={!cancelChannel?.active_run || cancelMutation.isPending}
              onClick={() => {
                if (!cancelChannel?.active_run) return
                cancelMutation.mutate({
                  channelId: cancelChannel.id,
                  runId: cancelChannel.active_run.run_id,
                })
              }}
            >
              {cancelMutation.isPending ? (
                <Spinner data-icon='inline-start' />
              ) : (
                <HugeiconsIcon icon={Cancel01Icon} data-icon='inline-start' />
              )}
              确认取消
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )

  const historySurface = (
    <>
      {historyChannel ? (
        <ChannelModelDetectionHistorySheet
          channel={historyChannel}
          open={historyChannelId != null}
          query={historyQueryInput}
          data={historyQuery.data}
          loading={historyQuery.isPending}
          refreshing={historyQuery.isFetching}
          error={
            historyQuery.isError
              ? channelModelDetectionRequestErrorMessage(historyQuery.error)
              : null
          }
          onOpenChange={(open) => {
            if (!open) {
              setHistoryChannelId(null)
              setDetailRunId(null)
              refreshOverview()
            }
          }}
          onQueryChange={setHistoryQueryInput}
          onRefresh={() => void historyQuery.refetch()}
          onOpenRun={(run) => setDetailRunId(run.run_id)}
        />
      ) : null}
      <ChannelModelDetectionRunDetailSheet
        runId={detailRunId}
        open={detailRunId != null}
        onOpenChange={(open) => {
          if (!open) {
            setDetailRunId(null)
            refreshOverview()
          }
        }}
        onTerminal={handleTerminalRun}
      />
    </>
  )

  return (
    <ChannelModelDetectionView
      {...props}
      actionPendingChannelId={actionPendingChannelId}
      actionPendingAll={pauseAllMutation.isPending}
      pauseAllPending={pauseAllMutation.isPending}
      pauseAllDisabled={scheduleMutation.isPending || cancelMutation.isPending}
      onPauseAll={() => setPauseAllOpen(true)}
      onOpenSettings={() => setSettingsOpen(true)}
      onOpenHistory={(channel) => {
        setHistoryQueryInput(DEFAULT_HISTORY_QUERY)
        setHistoryChannelId(channel.id)
      }}
      onOpenConfig={(channel) => setConfigChannelId(channel.id)}
      onOpenManualRun={(channel) => setRunChannelId(channel.id)}
      onCancelRun={(channel) => setCancelChannelId(channel.id)}
      onToggleSchedule={(channel) => scheduleMutation.mutate(channel)}
      settingsSurface={settingsSurface}
      channelSurface={channelSurface}
      historySurface={historySurface}
    />
  )
}
