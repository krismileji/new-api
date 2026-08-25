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
import { Alert02Icon, TestTubeIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { MultiSelect } from '@/components/multi-select'
import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Combobox,
  ComboboxCollection,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from '@/components/ui/combobox'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Progress,
  ProgressLabel,
  ProgressValue,
} from '@/components/ui/progress'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { getPricing } from '@/features/pricing/api'

import { getChannels } from '../../api'
import {
  CHANNEL_TEST_DEFAULTS,
  channelsQueryKeys,
  handleTestChannel,
} from '../../lib'
import type { Channel } from '../../types'
import { runChannelBatchTestQueue } from './channel-batch-test-queue'
import {
  type BatchTestChannelOption,
  createBatchTestChannelOption,
  getChannelsSupportingModels,
  getSelectableChannelIds,
  getSelectableModelNames,
  orderBatchTestChannels,
  retainCompatibleChannelIds,
} from './channel-batch-test-selection'
import {
  formatChannelTestDuration,
  parseChannelTestMetrics,
  type ChannelTestMetricValues,
} from './channel-test-metric-values'
import { ChannelTestMetrics } from './channel-test-metrics'
import { ChannelTestSampleToggle } from './channel-test-sample-toggle'

type BatchTestChannel = Pick<Channel, 'id' | 'name' | 'status'> &
  Partial<Pick<Channel, 'models' | 'remark'>> & {
    channel_remark?: string | null
  }

type ChannelBatchTestDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onTestComplete?: () => void
  channels?: ReadonlyArray<BatchTestChannel>
  modelSelectionMode?: 'multiple' | 'single'
  selectAllMode?: 'all' | 'enabled'
  enableRepeatMode?: boolean
}

type BatchTestMode = 'batch' | 'repeat'

type BatchTestTask = {
  key: string
  channelId: number
  channelName: string
  model: string
  workerIndex?: number
  iteration?: number
}

type BatchTestRequestOptions = {
  endpointType?: string
  stream?: boolean
  recordSample?: boolean
}

type BatchTestStatus = 'testing' | 'success' | 'error'

type BatchTestResult = BatchTestTask &
  ChannelTestMetricValues & {
    status: BatchTestStatus
    error?: string
    errorCode?: string
  }

type BatchTestProgress = {
  total: number
  completed: number
  success: number
  failed: number
}

const CHANNEL_PAGE_SIZE = 100
const BATCH_TEST_CONCURRENCY = 5
const DEFAULT_REPEAT_CONCURRENCY = '3'
const DEFAULT_REPEAT_ITERATIONS = '5'
const MIN_REPEAT_CONCURRENCY = 1
const MAX_REPEAT_CONCURRENCY = 20
const MIN_REPEAT_ITERATIONS = 1
const MAX_REPEAT_ITERATIONS = 50
const MAX_REPEAT_REQUESTS = 200
const POSITIVE_INTEGER_PATTERN = /^\d+$/
const EMPTY_CHANNELS: BatchTestChannel[] = []
const EMPTY_PRICED_MODELS: string[] = []
const ENDPOINT_TYPE_OPTIONS = [
  { value: 'auto', label: '自动检测' },
  { value: 'openai', label: 'OpenAI Chat (/v1/chat/completions)' },
  { value: 'openai-response', label: 'OpenAI Responses (/v1/responses)' },
  {
    value: 'openai-response-compact',
    label: 'OpenAI 响应压缩 (/v1/responses/compact)',
  },
  { value: 'anthropic', label: 'Anthropic Messages (/v1/messages)' },
  {
    value: 'gemini',
    label: 'Gemini Generate Content (/v1beta/models/{model}:generateContent)',
  },
  { value: 'jina-rerank', label: 'Jina Rerank (/v1/rerank)' },
  {
    value: 'image-generation',
    label: '图像生成 (/v1/images/generations)',
  },
  { value: 'embeddings', label: 'Embeddings (/v1/embeddings)' },
]
const STREAM_INCOMPATIBLE_ENDPOINTS = new Set([
  'embeddings',
  'image-generation',
  'jina-rerank',
  'openai-response-compact',
])
const endpointSelectContentClass = 'w-[460px] max-w-[calc(100vw-2rem)]'
const endpointSelectItemClass =
  'items-start py-2 [&_[data-slot=select-item-text]]:min-w-0 [&_[data-slot=select-item-text]]:shrink [&_[data-slot=select-item-text]]:whitespace-normal'

async function getBatchTestChannels(): Promise<Channel[]> {
  const firstPage = await getChannels({ p: 1, page_size: CHANNEL_PAGE_SIZE })
  if (!firstPage.success) {
    throw new Error(firstPage.message || '获取渠道列表失败')
  }

  const firstPageData = firstPage.data
  if (!firstPageData) return []

  const channelMap = new Map(
    firstPageData.items.map((channel) => [channel.id, channel])
  )
  const pageCount = Math.ceil(firstPageData.total / CHANNEL_PAGE_SIZE)
  const remainingPages: number[] = []
  for (let page = 2; page <= pageCount; page += 1) {
    remainingPages.push(page)
  }

  const responses = await Promise.all(
    remainingPages.map((page) =>
      getChannels({ p: page, page_size: CHANNEL_PAGE_SIZE })
    )
  )
  for (const response of responses) {
    if (!response.success) {
      throw new Error(response.message || '获取渠道列表失败')
    }
    for (const channel of response.data?.items ?? []) {
      channelMap.set(channel.id, channel)
    }
  }

  return [...channelMap.values()].sort((a, b) => a.id - b.id)
}

async function getPricedModelNames(): Promise<string[]> {
  const response = await getPricing()
  if (!response.success) {
    throw new Error(response.message || '获取定价模型失败')
  }

  return [
    ...new Set(
      response.data.map((model) => model.model_name.trim()).filter(Boolean)
    ),
  ].sort((a, b) => a.localeCompare(b))
}

function buildBatchTestTasks(
  channels: readonly BatchTestChannel[],
  models: string[]
): BatchTestTask[] {
  const tasks: BatchTestTask[] = []
  for (const channel of channels) {
    for (const model of models) {
      tasks.push({
        key: `${channel.id}::${model}`,
        channelId: channel.id,
        channelName: channel.name,
        model,
      })
    }
  }
  return tasks
}

function buildRepeatTestWorkers(
  channel: BatchTestChannel,
  model: string,
  concurrency: number,
  iterations: number
): BatchTestTask[][] {
  return Array.from({ length: concurrency }, (_, workerOffset) => {
    const workerIndex = workerOffset + 1
    return Array.from({ length: iterations }, (_, iterationOffset) => {
      const iteration = iterationOffset + 1
      return {
        key: `${channel.id}::${model}::${workerIndex}::${iteration}`,
        channelId: channel.id,
        channelName: channel.name,
        model,
        workerIndex,
        iteration,
      }
    })
  })
}

function parseBoundedInteger(
  value: string,
  minimum: number,
  maximum: number
): number | null {
  const normalized = value.trim()
  if (!POSITIVE_INTEGER_PATTERN.test(normalized)) return null

  const parsed = Number(normalized)
  if (!Number.isSafeInteger(parsed) || parsed < minimum || parsed > maximum) {
    return null
  }
  return parsed
}

async function runBatchTestTask(
  task: BatchTestTask,
  options: BatchTestRequestOptions = {}
): Promise<BatchTestResult> {
  let result: BatchTestResult | undefined
  try {
    await handleTestChannel(
      task.channelId,
      {
        channelName: task.channelName,
        testModel: task.model,
        endpointType: options.endpointType,
        stream: options.stream,
        recordSample: options.recordSample,
        silent: true,
      },
      (success, responseTime, error, errorCode, response) => {
        result = {
          ...task,
          ...parseChannelTestMetrics(response, responseTime),
          status: success ? 'success' : 'error',
          error,
          errorCode,
        }
      }
    )
  } catch (error: unknown) {
    return {
      ...task,
      status: 'error',
      error: error instanceof Error ? error.message : '测试失败',
    }
  }

  return (
    result ?? {
      ...task,
      status: 'error',
      error: '测试未返回结果',
    }
  )
}

function BatchTestStatusBadge(props: { status: BatchTestStatus }) {
  if (props.status === 'testing') {
    return (
      <Badge variant='outline' className='border-info/30 bg-info/10 text-info'>
        测试中
      </Badge>
    )
  }
  if (props.status === 'success') {
    return (
      <Badge
        variant='outline'
        className='border-success/30 bg-success/10 text-success'
      >
        成功
      </Badge>
    )
  }
  return <Badge variant='destructive'>失败</Badge>
}

function BatchTestResultContent(props: { result: BatchTestResult }) {
  if (props.result.status === 'testing') {
    return <span className='text-muted-foreground'>正在请求上游</span>
  }

  if (!props.result.error) {
    return <ChannelTestMetrics metrics={props.result} />
  }

  const errorCode = props.result.errorCode ? ` (${props.result.errorCode})` : ''
  return (
    <div className='space-y-2'>
      <p className='text-destructive wrap-break-word whitespace-normal'>
        {props.result.error}
        {errorCode}
      </p>
      <ChannelTestMetrics metrics={props.result} />
    </div>
  )
}

function formatBatchTestResponseTime(responseTime?: number): string {
  return formatChannelTestDuration(responseTime)
}

type BatchTestNumberStats = {
  average: number
  fastest: number
  slowest: number
  p95: number
}

function calculateBatchTestNumberStats(
  values: Array<number | undefined>
): BatchTestNumberStats | null {
  const sorted = values
    .filter(
      (value): value is number =>
        typeof value === 'number' && Number.isFinite(value)
    )
    .sort((a, b) => a - b)
  if (sorted.length === 0) return null

  const total = sorted.reduce((sum, value) => sum + value, 0)
  const p95Index = Math.ceil(sorted.length * 0.95) - 1
  return {
    average: total / sorted.length,
    fastest: sorted.at(0) ?? 0,
    slowest: sorted.at(-1) ?? 0,
    p95: sorted.at(p95Index) ?? 0,
  }
}

type BatchTestTokenMetric =
  | 'inputTokens'
  | 'outputTokens'
  | 'cachedTokens'
  | 'cacheWriteTokens'
  | 'reasoningTokens'
  | 'totalTokens'

function sumBatchTestTokenMetric(
  results: BatchTestResult[],
  metric: BatchTestTokenMetric
): number {
  return results.reduce((sum, result) => {
    const value = result[metric]
    return (
      sum + (typeof value === 'number' && Number.isFinite(value) ? value : 0)
    )
  }, 0)
}

function BatchTestSummaryMetric(props: { label: string; value: string }) {
  return (
    <div className='min-w-0'>
      <dt className='text-muted-foreground text-xs'>{props.label}</dt>
      <dd className='truncate font-mono text-sm font-medium tabular-nums'>
        {props.value}
      </dd>
    </div>
  )
}

function getErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : '加载失败，请稍后重试'
}

export function ChannelBatchTestDialog(props: ChannelBatchTestDialogProps) {
  const queryClient = useQueryClient()
  const stopRequestedRef = useRef(false)
  const [testMode, setTestMode] = useState<BatchTestMode>('batch')
  const [selectedChannelIds, setSelectedChannelIds] = useState<string[]>([])
  const [selectedModels, setSelectedModels] = useState<string[]>([])
  const [endpointType, setEndpointType] = useState<string>(
    CHANNEL_TEST_DEFAULTS.endpointType
  )
  const [isStreamTest, setIsStreamTest] = useState<boolean>(
    CHANNEL_TEST_DEFAULTS.stream
  )
  const [recordSample, setRecordSample] = useState<boolean>(
    CHANNEL_TEST_DEFAULTS.recordSample
  )
  const [repeatConcurrencyInput, setRepeatConcurrencyInput] = useState(
    DEFAULT_REPEAT_CONCURRENCY
  )
  const [repeatIterationsInput, setRepeatIterationsInput] = useState(
    DEFAULT_REPEAT_ITERATIONS
  )
  const [results, setResults] = useState<Record<string, BatchTestResult>>({})
  const [progress, setProgress] = useState<BatchTestProgress | null>(null)
  const [isTesting, setIsTesting] = useState(false)
  const [isStopRequested, setIsStopRequested] = useState(false)
  const isSingleModel = props.modelSelectionMode === 'single'
  const repeatModeEnabled = Boolean(props.enableRepeatMode && isSingleModel)
  const isRepeatMode = repeatModeEnabled && testMode === 'repeat'
  const usesProvidedChannels = props.channels !== undefined

  const channelsQuery = useQuery({
    queryKey: ['channel-batch-test', 'channels'],
    queryFn: getBatchTestChannels,
    enabled: props.open && !usesProvidedChannels,
    staleTime: 60_000,
  })
  const pricedModelsQuery = useQuery({
    queryKey: ['channel-batch-test', 'priced-models'],
    queryFn: getPricedModelNames,
    enabled: props.open,
    staleTime: 5 * 60_000,
  })

  const channels = useMemo(
    () =>
      orderBatchTestChannels(
        props.channels ?? channelsQuery.data ?? EMPTY_CHANNELS
      ),
    [channelsQuery.data, props.channels]
  )
  const pricedModels = pricedModelsQuery.data ?? EMPTY_PRICED_MODELS
  const selectableModels = useMemo(
    () => getSelectableModelNames(pricedModels, channels),
    [channels, pricedModels]
  )
  const compatibleChannels = useMemo(
    () => getChannelsSupportingModels(channels, selectedModels),
    [channels, selectedModels]
  )
  const channelOptions = useMemo(
    () =>
      compatibleChannels.map((channel) =>
        createBatchTestChannelOption(channel)
      ),
    [compatibleChannels]
  )
  const selectedChannels = useMemo(() => {
    const selectedIds = new Set(
      selectedChannelIds.map((channelId) => Number(channelId))
    )
    return compatibleChannels.filter((channel) => selectedIds.has(channel.id))
  }, [compatibleChannels, selectedChannelIds])
  const activeSelectedChannelIds = useMemo(
    () => selectedChannels.map((channel) => String(channel.id)),
    [selectedChannels]
  )
  const repeatChannel = isRepeatMode ? selectedChannels[0] : undefined
  const modelOptions = useMemo(
    () => selectableModels.map((model) => ({ value: model, label: model })),
    [selectableModels]
  )
  const repeatConcurrency = parseBoundedInteger(
    repeatConcurrencyInput,
    MIN_REPEAT_CONCURRENCY,
    MAX_REPEAT_CONCURRENCY
  )
  const repeatIterations = parseBoundedInteger(
    repeatIterationsInput,
    MIN_REPEAT_ITERATIONS,
    MAX_REPEAT_ITERATIONS
  )
  const repeatRequestCount =
    repeatConcurrency !== null && repeatIterations !== null
      ? repeatConcurrency * repeatIterations
      : 0
  const repeatRequestLimitExceeded = repeatRequestCount > MAX_REPEAT_REQUESTS
  const repeatConfigurationValid =
    repeatConcurrency !== null &&
    repeatIterations !== null &&
    !repeatRequestLimitExceeded
  const repeatWorkers = useMemo(() => {
    if (
      !isRepeatMode ||
      !repeatChannel ||
      selectedModels.length !== 1 ||
      repeatConcurrency === null ||
      repeatIterations === null ||
      repeatRequestLimitExceeded
    ) {
      return []
    }

    return buildRepeatTestWorkers(
      repeatChannel,
      selectedModels[0],
      repeatConcurrency,
      repeatIterations
    )
  }, [
    isRepeatMode,
    repeatChannel,
    repeatConcurrency,
    repeatIterations,
    repeatRequestLimitExceeded,
    selectedModels,
  ])
  const tasks = useMemo(
    () =>
      isRepeatMode
        ? repeatWorkers.flat()
        : buildBatchTestTasks(selectedChannels, selectedModels),
    [isRepeatMode, repeatWorkers, selectedChannels, selectedModels]
  )
  const visibleResults = useMemo(
    () =>
      tasks
        .map((task) => results[task.key])
        .filter((result): result is BatchTestResult => result !== undefined),
    [results, tasks]
  )
  const repeatStats = useMemo(() => {
    const completedResults = visibleResults.filter(
      (result) => result.status !== 'testing'
    )
    if (completedResults.length === 0) return null

    const response = calculateBatchTestNumberStats(
      completedResults.map((result) => result.responseTime)
    )
    const firstToken = calculateBatchTestNumberStats(
      completedResults.map((result) => result.firstTokenMs)
    )
    const tokensPerSecond = calculateBatchTestNumberStats(
      completedResults.map((result) => result.tokensPerSecond)
    )
    const usageResults = completedResults.filter(
      (result) => result.usageAvailable === true
    )

    return {
      response,
      firstToken,
      tokensPerSecond,
      usageSampleCount: usageResults.length,
      inputTokens: sumBatchTestTokenMetric(usageResults, 'inputTokens'),
      outputTokens: sumBatchTestTokenMetric(usageResults, 'outputTokens'),
      cachedTokens: sumBatchTestTokenMetric(usageResults, 'cachedTokens'),
      cacheWriteTokens: sumBatchTestTokenMetric(
        usageResults,
        'cacheWriteTokens'
      ),
      reasoningTokens: sumBatchTestTokenMetric(usageResults, 'reasoningTokens'),
      totalTokens: sumBatchTestTokenMetric(usageResults, 'totalTokens'),
    }
  }, [visibleResults])
  const progressPercent = progress
    ? Math.round((progress.completed / progress.total) * 100)
    : 0
  const channelLoadError = usesProvidedChannels ? null : channelsQuery.error
  const loadError = channelLoadError ?? pricedModelsQuery.error
  const channelsLoading = !usesProvidedChannels && channelsQuery.isLoading
  const optionsLoading = channelsLoading || pricedModelsQuery.isLoading
  const streamDisabled = STREAM_INCOMPATIBLE_ENDPOINTS.has(endpointType)
  const effectiveStreamTest = !streamDisabled && isStreamTest
  const selectedEndpointLabel =
    ENDPOINT_TYPE_OPTIONS.find((option) => option.value === endpointType)
      ?.label ?? 'OpenAI Responses (/v1/responses)'

  const clearResults = () => {
    setResults({})
    setProgress(null)
  }

  const handleEndpointTypeChange = (value: string | null) => {
    if (value === null) return

    setEndpointType(value)
    if (STREAM_INCOMPATIBLE_ENDPOINTS.has(value)) {
      setIsStreamTest(false)
    }
    clearResults()
  }

  const handleStreamTestChange = (checked: boolean) => {
    setIsStreamTest(checked)
    clearResults()
  }

  const handleRecordSampleChange = (checked: boolean) => {
    setRecordSample(checked)
    clearResults()
  }

  const handleSelectedModelsChange = (models: string[]) => {
    setSelectedModels(models)
    setSelectedChannelIds((current) =>
      retainCompatibleChannelIds(
        current,
        getChannelsSupportingModels(channels, models)
      )
    )
    clearResults()
  }

  const resetDialog = () => {
    stopRequestedRef.current = true
    setTestMode('batch')
    setSelectedChannelIds([])
    setSelectedModels([])
    setEndpointType(CHANNEL_TEST_DEFAULTS.endpointType)
    setIsStreamTest(CHANNEL_TEST_DEFAULTS.stream)
    setRecordSample(CHANNEL_TEST_DEFAULTS.recordSample)
    setRepeatConcurrencyInput(DEFAULT_REPEAT_CONCURRENCY)
    setRepeatIterationsInput(DEFAULT_REPEAT_ITERATIONS)
    setResults({})
    setProgress(null)
    setIsTesting(false)
    setIsStopRequested(false)
  }

  const handleOpenChange = (open: boolean) => {
    if (!open && isTesting) {
      toast.error(
        isRepeatMode
          ? '并发循环测试进行中，请先停止测试'
          : '批量测试进行中，请先停止测试'
      )
      return
    }
    if (!open) resetDialog()
    props.onOpenChange(open)
  }

  const handleStartTest = async () => {
    if (isRepeatMode && !repeatConfigurationValid) {
      toast.error(
        `请填写有效的并发与循环次数，总请求数不能超过 ${MAX_REPEAT_REQUESTS}`
      )
      return
    }
    if (tasks.length === 0) {
      toast.error(
        isRepeatMode
          ? '请先选择一个已定价模型，再选择一个支持该模型的渠道'
          : '请先选择已定价模型，再选择至少一个支持所选模型的渠道'
      )
      return
    }

    stopRequestedRef.current = false
    setIsTesting(true)
    setIsStopRequested(false)
    setResults({})
    setProgress({
      total: tasks.length,
      completed: 0,
      success: 0,
      failed: 0,
    })

    let completed = 0
    let succeeded = 0
    let failed = 0
    const requestOptions: BatchTestRequestOptions = {
      endpointType,
      stream: effectiveStreamTest,
      recordSample,
    }

    try {
      if (isRepeatMode) {
        await Promise.all(
          repeatWorkers.map(async (workerTasks) => {
            for (const task of workerTasks) {
              if (stopRequestedRef.current) break

              setResults((current) => ({
                ...current,
                [task.key]: { ...task, status: 'testing' },
              }))
              const result = await runBatchTestTask(task, requestOptions)
              completed += 1
              if (result.status === 'success') succeeded += 1
              failed = completed - succeeded
              setResults((current) => ({
                ...current,
                [result.key]: result,
              }))
              setProgress({
                total: tasks.length,
                completed,
                success: succeeded,
                failed,
              })
            }
          })
        )
      } else {
        await runChannelBatchTestQueue(tasks, {
          concurrency: BATCH_TEST_CONCURRENCY,
          shouldStop: () => stopRequestedRef.current,
          onTaskStart: (task) => {
            setResults((current) => ({
              ...current,
              [task.key]: { ...task, status: 'testing' },
            }))
          },
          runTask: (task) => runBatchTestTask(task, requestOptions),
          onTaskComplete: (result) => {
            completed += 1
            if (result.status === 'success') succeeded += 1
            failed = completed - succeeded
            setResults((current) => ({
              ...current,
              [result.key]: result,
            }))
            setProgress({
              total: tasks.length,
              completed,
              success: succeeded,
              failed,
            })
          },
        })
      }
    } finally {
      const stopped = stopRequestedRef.current && completed < tasks.length
      setIsTesting(false)
      setIsStopRequested(false)
      stopRequestedRef.current = false
      void queryClient.invalidateQueries({
        queryKey: channelsQueryKeys.lists(),
      })
      props.onTestComplete?.()

      if (stopped) {
        toast.warning(
          `${isRepeatMode ? '并发循环测试' : '批量测试'}已停止：完成 ${completed}/${tasks.length}，成功 ${succeeded}，失败 ${failed}`
        )
      } else {
        toast.success(
          `${isRepeatMode ? '并发循环测试' : '批量测试'}完成：成功 ${succeeded}，失败 ${failed}`
        )
      }
    }
  }

  const handleStopTest = () => {
    if (!isTesting || isStopRequested) return
    stopRequestedRef.current = true
    setIsStopRequested(true)
  }

  const footer = (
    <>
      <Button
        variant='outline'
        onClick={() => handleOpenChange(false)}
        disabled={isTesting}
      >
        关闭
      </Button>
      {isTesting ? (
        <Button
          variant='destructive'
          onClick={handleStopTest}
          disabled={isStopRequested}
        >
          {isStopRequested && <Spinner data-icon='inline-start' />}
          {isStopRequested ? '正在停止' : '停止测试'}
        </Button>
      ) : (
        <Button
          onClick={() => void handleStartTest()}
          disabled={
            optionsLoading ||
            Boolean(loadError) ||
            tasks.length === 0 ||
            (isRepeatMode && !repeatConfigurationValid)
          }
        >
          <HugeiconsIcon icon={TestTubeIcon} data-icon='inline-start' />
          {visibleResults.length > 0 ? '重新测试' : '开始测试'}
        </Button>
      )}
    </>
  )

  let dialogDescription =
    '先选择已定价模型，再从支持全部所选模型的渠道中选择测试目标。'
  if (repeatModeEnabled) {
    dialogDescription =
      '先选择模型和支持该模型的渠道，再进行批量测试或并发循环测试。测试会真实请求上游。'
  } else if (isSingleModel) {
    dialogDescription = '先选择一个已定价模型，再批量验证支持该模型的渠道。'
  }

  let modelDescription = `仅显示已定价且至少有一个渠道配置的模型，共 ${selectableModels.length} 个。`
  if (isSingleModel) {
    modelDescription = `仅显示已定价且至少有一个渠道配置的模型，共 ${selectableModels.length} 个，每次只能选择一个。`
  }

  let channelDescription = '请先选择模型，再选择支持所选模型的渠道。'
  if (selectedModels.length > 0 && isRepeatMode) {
    channelDescription = `共 ${compatibleChannels.length} 个渠道支持所选模型，只能选择一个测试目标。`
  } else if (selectedModels.length > 0) {
    channelDescription = `共 ${compatibleChannels.length} 个渠道支持所选模型，当前选择 ${selectedChannels.length} 个。`
  }

  let testPlanTitle = `将执行 ${tasks.length} 个测试组合`
  const testModeDescription = `端点：${selectedEndpointLabel} · ${effectiveStreamTest ? '流式' : '非流式'} · ${recordSample ? '计入渠道样本' : '不计入渠道样本'}`
  let testPlanDescription = `${selectedChannels.length} 个渠道 × ${selectedModels.length} 个模型，最多同时发起 ${BATCH_TEST_CONCURRENCY} 个请求。${testModeDescription}。`
  if (isRepeatMode) {
    testPlanTitle = `将执行 ${repeatRequestCount} 次测试请求`
    testPlanDescription = `${repeatChannel?.name} · ${selectedModels[0]}，${repeatConcurrencyInput} 个并发 × 每并发 ${repeatIterationsInput} 次循环。${testModeDescription}。`
  } else if (isSingleModel) {
    testPlanTitle = `将测试 ${selectedChannels.length} 个渠道`
    testPlanDescription = `统一使用 ${selectedModels[0]} 模型，最多同时发起 ${BATCH_TEST_CONCURRENCY} 个请求。${testModeDescription}。`
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={handleOpenChange}
      title={isSingleModel ? '渠道连通性测试' : '批量测试渠道'}
      description={dialogDescription}
      contentClassName='sm:max-w-5xl'
      bodyClassName='flex flex-col gap-5'
      footer={footer}
    >
      {repeatModeEnabled && (
        <ToggleGroup
          value={[testMode]}
          onValueChange={(values) => {
            const nextMode = values[0]
            if (nextMode !== 'batch' && nextMode !== 'repeat') return
            setTestMode(nextMode)
            setSelectedChannelIds([])
            setSelectedModels([])
            clearResults()
          }}
          variant='outline'
          spacing={0}
          aria-label='选择连通性测试模式'
          className='grid w-full grid-cols-2'
          disabled={isTesting}
        >
          <ToggleGroupItem value='batch' className='w-full'>
            批量渠道
          </ToggleGroupItem>
          <ToggleGroupItem value='repeat' className='w-full'>
            并发循环
          </ToggleGroupItem>
        </ToggleGroup>
      )}

      {loadError && (
        <Alert variant='destructive'>
          <HugeiconsIcon icon={Alert02Icon} />
          <AlertTitle>批量测试选项加载失败</AlertTitle>
          <AlertDescription>{getErrorMessage(loadError)}</AlertDescription>
          <AlertAction>
            <Button
              variant='outline'
              size='xs'
              onClick={() => {
                if (!usesProvidedChannels) void channelsQuery.refetch()
                void pricedModelsQuery.refetch()
              }}
            >
              重试
            </Button>
          </AlertAction>
        </Alert>
      )}

      <FieldGroup className='grid gap-4 lg:grid-cols-2'>
        <Field>
          <div className='flex items-center justify-between gap-3'>
            <FieldLabel htmlFor='batch-test-models'>
              {isSingleModel ? '选择模型' : '选择已定价模型'}
            </FieldLabel>
            {!isSingleModel && (
              <div className='flex items-center gap-1'>
                <Button
                  type='button'
                  variant='ghost'
                  size='xs'
                  onClick={() => {
                    handleSelectedModelsChange(selectableModels)
                  }}
                  disabled={isTesting || selectableModels.length === 0}
                >
                  全选
                </Button>
                <Button
                  type='button'
                  variant='ghost'
                  size='xs'
                  onClick={() => {
                    handleSelectedModelsChange([])
                  }}
                  disabled={isTesting || selectedModels.length === 0}
                >
                  清空
                </Button>
              </div>
            )}
          </div>
          {optionsLoading && <Skeleton className='h-9 w-full' />}
          {!optionsLoading && isSingleModel && (
            <Combobox
              items={selectableModels}
              value={selectedModels[0] ?? null}
              onValueChange={(value) => {
                handleSelectedModelsChange(value ? [value] : [])
              }}
              disabled={isTesting || Boolean(loadError)}
            >
              <ComboboxInput
                id='batch-test-models'
                className='w-full'
                placeholder='搜索并选择模型'
                showClear={selectedModels.length > 0}
                disabled={isTesting || Boolean(loadError)}
              />
              <ComboboxContent>
                <ComboboxList>
                  <ComboboxCollection>
                    {(model: string) => (
                      <ComboboxItem key={model} value={model}>
                        <span className='truncate font-mono'>{model}</span>
                      </ComboboxItem>
                    )}
                  </ComboboxCollection>
                </ComboboxList>
                <ComboboxEmpty>没有渠道配置已定价模型</ComboboxEmpty>
              </ComboboxContent>
            </Combobox>
          )}
          {!optionsLoading && !isSingleModel && (
            <MultiSelect
              id='batch-test-models'
              options={modelOptions}
              selected={selectedModels}
              onChange={handleSelectedModelsChange}
              placeholder='搜索并选择模型'
              emptyText='没有渠道配置已定价模型'
              disabled={isTesting || Boolean(loadError)}
              renderSelectedSummary={(values) => `已选 ${values.length} 个模型`}
              copyChipOnClick
            />
          )}
          <FieldDescription>{modelDescription}</FieldDescription>
        </Field>

        <Field>
          <div className='flex items-center justify-between gap-3'>
            <FieldLabel htmlFor='batch-test-channels'>选择渠道</FieldLabel>
            {!isRepeatMode && (
              <div className='flex items-center gap-1'>
                <Button
                  type='button'
                  variant='ghost'
                  size='xs'
                  onClick={() => {
                    setSelectedChannelIds(
                      getSelectableChannelIds(
                        compatibleChannels,
                        props.selectAllMode
                      )
                    )
                    clearResults()
                  }}
                  disabled={isTesting || compatibleChannels.length === 0}
                >
                  {props.selectAllMode === 'all' ? '全选' : '全选启用渠道'}
                </Button>
                <Button
                  type='button'
                  variant='ghost'
                  size='xs'
                  onClick={() => {
                    setSelectedChannelIds([])
                    clearResults()
                  }}
                  disabled={isTesting || activeSelectedChannelIds.length === 0}
                >
                  清空
                </Button>
              </div>
            )}
          </div>
          {channelsLoading && <Skeleton className='h-9 w-full' />}
          {!channelsLoading && isRepeatMode && (
            <Combobox
              items={channelOptions}
              itemToStringLabel={(option) => option.label}
              itemToStringValue={(option) => option.value}
              value={
                channelOptions.find(
                  (option) => option.value === activeSelectedChannelIds[0]
                ) ?? null
              }
              onValueChange={(option) => {
                setSelectedChannelIds(option ? [option.value] : [])
                clearResults()
              }}
              disabled={
                isTesting ||
                Boolean(channelLoadError) ||
                selectedModels.length === 0
              }
            >
              <ComboboxInput
                id='batch-test-channels'
                className='w-full'
                placeholder={
                  selectedModels.length > 0
                    ? '搜索并选择支持所选模型的渠道'
                    : '请先选择模型'
                }
                showClear={activeSelectedChannelIds.length > 0}
                disabled={
                  isTesting ||
                  Boolean(channelLoadError) ||
                  selectedModels.length === 0
                }
              />
              <ComboboxContent>
                <ComboboxList>
                  <ComboboxCollection>
                    {(option: BatchTestChannelOption) => (
                      <ComboboxItem key={option.value} value={option}>
                        <div className='min-w-0 flex-1'>
                          <div className='truncate'>{option.channelLabel}</div>
                          {option.remark && (
                            <div
                              className='text-muted-foreground truncate text-xs'
                              title={option.remark}
                            >
                              备注：{option.remark}
                            </div>
                          )}
                        </div>
                      </ComboboxItem>
                    )}
                  </ComboboxCollection>
                </ComboboxList>
                <ComboboxEmpty>没有渠道配置所选模型</ComboboxEmpty>
              </ComboboxContent>
            </Combobox>
          )}
          {!channelsLoading && !isRepeatMode && (
            <MultiSelect
              id='batch-test-channels'
              options={channelOptions}
              selected={activeSelectedChannelIds}
              onChange={(values) => {
                setSelectedChannelIds(values)
                clearResults()
              }}
              placeholder={
                selectedModels.length > 0
                  ? '搜索并选择支持所选模型的渠道'
                  : '请先选择模型'
              }
              emptyText={
                selectedModels.length > 0
                  ? '没有渠道配置所选模型'
                  : '请先选择模型'
              }
              disabled={
                isTesting ||
                Boolean(channelLoadError) ||
                selectedModels.length === 0
              }
              renderSelectedSummary={(values) => `已选 ${values.length} 个渠道`}
            />
          )}
          <FieldDescription>{channelDescription}</FieldDescription>
        </Field>
      </FieldGroup>

      <FieldGroup className='grid gap-4 md:grid-cols-3'>
        <Field>
          <FieldLabel htmlFor='batch-test-endpoint-type'>端点类型</FieldLabel>
          <Select
            items={ENDPOINT_TYPE_OPTIONS}
            value={endpointType}
            onValueChange={handleEndpointTypeChange}
            disabled={isTesting}
          >
            <SelectTrigger
              id='batch-test-endpoint-type'
              className='w-full min-w-0'
            >
              <SelectValue
                className='min-w-0 truncate'
                placeholder='OpenAI Responses (/v1/responses)'
              />
            </SelectTrigger>
            <SelectContent
              alignItemWithTrigger={false}
              className={endpointSelectContentClass}
            >
              <SelectGroup>
                {ENDPOINT_TYPE_OPTIONS.map((option) => (
                  <SelectItem
                    key={option.value}
                    value={option.value}
                    className={endpointSelectItemClass}
                  >
                    <span className='min-w-0 leading-snug break-words whitespace-normal'>
                      {option.label}
                    </span>
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          <FieldDescription>
            选择本次连通性测试请求使用的端点；自动检测会沿用后端默认规则。
          </FieldDescription>
        </Field>

        <Field>
          <FieldLabel htmlFor='batch-test-stream-mode'>流式模式</FieldLabel>
          <div className='flex h-8 items-center gap-2'>
            <Switch
              id='batch-test-stream-mode'
              checked={effectiveStreamTest}
              onCheckedChange={handleStreamTestChange}
              disabled={isTesting || streamDisabled}
            />
            <span className='text-sm'>
              {effectiveStreamTest ? '已启用' : '已关闭'}
            </span>
          </div>
          <FieldDescription>
            为本次测试请求启用流式模式；当前端点不支持时会自动关闭。
          </FieldDescription>
        </Field>

        <ChannelTestSampleToggle
          id='batch-test-record-sample'
          checked={recordSample}
          onCheckedChange={handleRecordSampleChange}
          disabled={isTesting}
        />
      </FieldGroup>

      {isRepeatMode && (
        <FieldGroup className='grid gap-4 sm:grid-cols-2'>
          <Field
            data-invalid={
              repeatConcurrency === null || repeatRequestLimitExceeded
            }
          >
            <FieldLabel htmlFor='repeat-test-concurrency'>并发数</FieldLabel>
            <Input
              id='repeat-test-concurrency'
              type='number'
              min={MIN_REPEAT_CONCURRENCY}
              max={MAX_REPEAT_CONCURRENCY}
              step={1}
              value={repeatConcurrencyInput}
              onChange={(event) => {
                setRepeatConcurrencyInput(event.target.value)
                clearResults()
              }}
              disabled={isTesting}
              aria-invalid={
                repeatConcurrency === null || repeatRequestLimitExceeded
              }
            />
            <FieldDescription>
              同时运行的测试任务，范围 {MIN_REPEAT_CONCURRENCY}-
              {MAX_REPEAT_CONCURRENCY}。
            </FieldDescription>
          </Field>
          <Field
            data-invalid={
              repeatIterations === null || repeatRequestLimitExceeded
            }
          >
            <FieldLabel htmlFor='repeat-test-iterations'>
              每并发循环次数
            </FieldLabel>
            <Input
              id='repeat-test-iterations'
              type='number'
              min={MIN_REPEAT_ITERATIONS}
              max={MAX_REPEAT_ITERATIONS}
              step={1}
              value={repeatIterationsInput}
              onChange={(event) => {
                setRepeatIterationsInput(event.target.value)
                clearResults()
              }}
              disabled={isTesting}
              aria-invalid={
                repeatIterations === null || repeatRequestLimitExceeded
              }
            />
            <FieldDescription>
              每个并发任务顺序执行，范围 {MIN_REPEAT_ITERATIONS}-
              {MAX_REPEAT_ITERATIONS} 次。
            </FieldDescription>
          </Field>
        </FieldGroup>
      )}

      {isRepeatMode && repeatRequestLimitExceeded && (
        <Alert variant='destructive'>
          <HugeiconsIcon icon={Alert02Icon} />
          <AlertTitle>总请求数超过限制</AlertTitle>
          <AlertDescription>
            当前为 {repeatRequestCount} 次，单次测试最多允许{' '}
            {MAX_REPEAT_REQUESTS} 次请求。
          </AlertDescription>
        </Alert>
      )}

      {selectedChannels.length > 0 && selectedModels.length > 0 && (
        <Alert>
          <HugeiconsIcon icon={TestTubeIcon} />
          <AlertTitle>{testPlanTitle}</AlertTitle>
          <AlertDescription>{testPlanDescription}</AlertDescription>
        </Alert>
      )}

      {progress && (
        <div className='flex flex-col gap-3 rounded-lg border p-3'>
          <Progress value={progressPercent}>
            <ProgressLabel>测试进度</ProgressLabel>
            <ProgressValue>
              {() => `${progress.completed}/${progress.total}`}
            </ProgressValue>
          </Progress>
          <div className='flex flex-wrap items-center gap-2'>
            <Badge variant='outline'>完成 {progress.completed}</Badge>
            <Badge
              variant='outline'
              className='border-success/30 bg-success/10 text-success'
            >
              成功 {progress.success}
            </Badge>
            <Badge variant='destructive'>失败 {progress.failed}</Badge>
          </div>
          {isRepeatMode && repeatStats && (
            <div className='space-y-2'>
              <dl className='grid grid-cols-2 gap-x-4 gap-y-2 sm:grid-cols-3 lg:grid-cols-6'>
                <BatchTestSummaryMetric
                  label='平均响应'
                  value={formatBatchTestResponseTime(
                    repeatStats.response?.average
                  )}
                />
                <BatchTestSummaryMetric
                  label='最快 / 最慢'
                  value={`${formatBatchTestResponseTime(repeatStats.response?.fastest)} / ${formatBatchTestResponseTime(repeatStats.response?.slowest)}`}
                />
                <BatchTestSummaryMetric
                  label='响应 P95'
                  value={formatBatchTestResponseTime(repeatStats.response?.p95)}
                />
                <BatchTestSummaryMetric
                  label='平均首字'
                  value={formatBatchTestResponseTime(
                    repeatStats.firstToken?.average
                  )}
                />
                <BatchTestSummaryMetric
                  label='首字 P95'
                  value={formatBatchTestResponseTime(
                    repeatStats.firstToken?.p95
                  )}
                />
                <BatchTestSummaryMetric
                  label='平均 TPS'
                  value={
                    repeatStats.tokensPerSecond
                      ? repeatStats.tokensPerSecond.average.toFixed(2)
                      : '-'
                  }
                />
              </dl>
              <div className='border-border/70 space-y-1 border-t pt-2'>
                <p className='text-muted-foreground text-xs'>
                  Token 合计（{repeatStats.usageSampleCount} 个 Usage 样本）
                </p>
                <dl className='grid grid-cols-2 gap-x-4 gap-y-2 sm:grid-cols-3 lg:grid-cols-6'>
                  <BatchTestSummaryMetric
                    label='输入 Token'
                    value={
                      repeatStats.usageSampleCount > 0
                        ? repeatStats.inputTokens.toLocaleString('zh-CN')
                        : '-'
                    }
                  />
                  <BatchTestSummaryMetric
                    label='输出 Token'
                    value={
                      repeatStats.usageSampleCount > 0
                        ? repeatStats.outputTokens.toLocaleString('zh-CN')
                        : '-'
                    }
                  />
                  <BatchTestSummaryMetric
                    label='缓存读取'
                    value={
                      repeatStats.usageSampleCount > 0
                        ? repeatStats.cachedTokens.toLocaleString('zh-CN')
                        : '-'
                    }
                  />
                  <BatchTestSummaryMetric
                    label='缓存写入'
                    value={
                      repeatStats.usageSampleCount > 0
                        ? repeatStats.cacheWriteTokens.toLocaleString('zh-CN')
                        : '-'
                    }
                  />
                  <BatchTestSummaryMetric
                    label='推理 Token'
                    value={
                      repeatStats.usageSampleCount > 0
                        ? repeatStats.reasoningTokens.toLocaleString('zh-CN')
                        : '-'
                    }
                  />
                  <BatchTestSummaryMetric
                    label='总 Token'
                    value={
                      repeatStats.usageSampleCount > 0
                        ? repeatStats.totalTokens.toLocaleString('zh-CN')
                        : '-'
                    }
                  />
                </dl>
              </div>
            </div>
          )}
        </div>
      )}

      {visibleResults.length > 0 && (
        <div className='rounded-lg border'>
          <Table>
            <TableHeader>
              <TableRow>
                {isRepeatMode && <TableHead>轮次</TableHead>}
                <TableHead>渠道</TableHead>
                <TableHead>模型</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>性能与 Token</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {visibleResults.map((result) => (
                <TableRow key={result.key}>
                  {isRepeatMode && (
                    <TableCell>
                      <div className='font-medium'>
                        并发 {result.workerIndex}
                      </div>
                      <div className='text-muted-foreground text-xs'>
                        第 {result.iteration} 次
                      </div>
                    </TableCell>
                  )}
                  <TableCell>
                    <div className='max-w-48'>
                      <div className='truncate font-medium'>
                        {result.channelName}
                      </div>
                      <div className='text-muted-foreground text-xs'>
                        #{result.channelId}
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className='max-w-64 truncate font-mono'>
                    {result.model}
                  </TableCell>
                  <TableCell>
                    <BatchTestStatusBadge status={result.status} />
                  </TableCell>
                  <TableCell className='max-w-[28rem] min-w-80 whitespace-normal'>
                    <BatchTestResultContent result={result} />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </Dialog>
  )
}
