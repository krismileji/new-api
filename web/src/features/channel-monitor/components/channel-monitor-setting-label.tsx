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
import { InformationCircleIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import { FormLabel } from '@/components/ui/form'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

type SmartScheduleSettingHelpOptions = {
  meaning: string
  unit: string
  range: string
  defaultValue: string
  activation?: string
  scheduleRelation?: string
  constraints?: string
}

function smartScheduleSettingHelp({
  meaning,
  unit,
  range,
  defaultValue,
  activation = '保存成功后生效',
  scheduleRelation = '相关请求事件投影后异步使用',
  constraints = '无额外组合约束',
}: SmartScheduleSettingHelpOptions): string {
  return `${meaning} 单位：${unit}；范围：${range}；默认值：${defaultValue}；生效时机：${activation}；更新方式：${scheduleRelation}；组合约束：${constraints}。`
}

const EVENT_REFRESH = '相关请求事件投影后异步更新'
const NEXT_REQUEST = '下一次相关请求使用'
const NEXT_OBSERVATION = '下一条有效观测投影后异步更新'

const CHANNEL_MONITOR_SMART_SCHEDULE_SETTING_HELP = {
  enabled: smartScheduleSettingHelp({
    meaning: '控制已配置策略的分组是否参与智能调度。',
    unit: '开关',
    range: '开启或关闭',
    defaultValue: '关闭',
    constraints: '只作用于已配置分组，手动取消参与的渠道不调整',
  }),
  performanceRange: smartScheduleSettingHelp({
    meaning: '设定成本、首字和 TPS 业务评分的历史窗口。',
    unit: '分钟',
    range: '1–43200 的整数',
    defaultValue: '60',
    constraints: '与稳定性评分窗口独立',
  }),
  stabilityRange: smartScheduleSettingHelp({
    meaning: '设定成功率、失败耗时和首字抖动软评分窗口。',
    unit: '分钟',
    range: '1–43200 的整数',
    defaultValue: '5',
    constraints: '与性能窗口独立，不替代请求级保护失败窗口',
  }),
  responseHeaderTimeout: smartScheduleSettingHelp({
    meaning: '限制流式首个有效模型事件或非流式响应头的等待时间。',
    unit: '秒',
    range: '0–600',
    defaultValue: '0（不限制）',
    scheduleRelation: NEXT_REQUEST,
    constraints: '响应头、空行和心跳不计为流式首字，超时按现有规则重试',
  }),
  rateLimitCooldown: smartScheduleSettingHelp({
    meaning: '上游返回 429 后，暂停该渠道模型进入新请求、亲和、探测和采样。',
    unit: '秒',
    range: '0–300',
    defaultValue: '30',
    scheduleRelation: NEXT_REQUEST,
    constraints: '0 表示关闭；不改变稳定性状态，到期自动放行',
  }),
  forceReset: smartScheduleSettingHelp({
    meaning: '本次保存时忽略渐进调整幅度，重算已配置路由。',
    unit: '一次性复选框',
    range: '选中或不选中',
    defaultValue: '不选中',
    activation: '仅在本次保存后执行一次',
    scheduleRelation: '保存后立即创建重算任务',
    constraints: '不影响未配置分组',
  }),
  group: smartScheduleSettingHelp({
    meaning: '指定策略作用的渠道分组。',
    unit: '分组名称',
    range: '1–64 个字符',
    defaultValue: '无，新增时必选',
    constraints: '每个分组只能有一条策略',
  }),
  strategy: smartScheduleSettingHelp({
    meaning: '选择综合、成本倍率、首字或 TPS 作为业务得分主维度。',
    unit: '枚举',
    range: '智能调度、成本倍率、首字、TPS',
    defaultValue: '智能调度',
    constraints: '稳定性开启时还会合入稳定性得分',
  }),
  applyMode: smartScheduleSettingHelp({
    meaning: '决定仅调整同层权重，或同时生成唯一优先级和权重。',
    unit: '枚举',
    range: '只调整权重、优先级分层 + 权重',
    defaultValue: '优先级分层 + 权重',
    constraints: '探索流量和自适应备援必须使用优先级分层 + 权重',
  }),
  models: smartScheduleSettingHelp({
    meaning: '限定策略覆盖的模型，评分仍按分组 + 模型独立执行。',
    unit: '模型列表',
    range: '0–100 个唯一模型',
    defaultValue: '空（分组内全部模型）',
    constraints: '只能选择当前分组可用模型',
  }),
  modelOrder: smartScheduleSettingHelp({
    meaning: '控制智能调度看板中模型卡片的展示顺序。',
    unit: '模型列表',
    range: '当前策略模型的任意排列',
    defaultValue: '空（按名称排序）',
    activation: '保存后下次打开或刷新看板生效',
    scheduleRelation: '纯展示配置，不触发调度更新',
    constraints: '不改变参与范围、P/W 或流量',
  }),
  sampleMode: smartScheduleSettingHelp({
    meaning: '选择样本欠账时不补样、分配真实探索流量或主动探测。',
    unit: '枚举',
    range: '关闭、探索流量、定时探测',
    defaultValue: '关闭',
    scheduleRelation: EVENT_REFRESH,
    constraints: '探索流量需优先级分层 + 权重；主动探测仅支持文本模型',
  }),
  samplingOrder: smartScheduleSettingHelp({
    meaning: '设定探索流量选择样本欠账候选的排序方式，自适应备援沿用同一顺序。',
    unit: '枚举',
    range: '按基础优先级和权重、按成本倍率',
    defaultValue: '按基础优先级和权重',
    scheduleRelation: EVENT_REFRESH,
    constraints:
      '仅在探索流量配置中展示；关闭常规探索后仍保留并供自适应备援使用，不存在独立轮转顺序',
  }),
  explorationTraffic: smartScheduleSettingHelp({
    meaning: '设定当前唯一样本欠账渠道的目标真实流量。',
    unit: '%',
    range: '>0–20',
    defaultValue: '3',
    scheduleRelation: EVENT_REFRESH,
    constraints:
      '仅 sample_mode=探索流量且应用方式为优先级分层 + 权重时生效；整数权重会造成小幅偏差',
  }),
  explorationMaxPromptKTokens: smartScheduleSettingHelp({
    meaning:
      '以 K Token 设置探索请求上限，对估算超限的请求后移探索路由，不阻断请求；1K 等于 1000 Token。',
    unit: 'K Token',
    range: '0–1000 的整数',
    defaultValue: '50',
    scheduleRelation: NEXT_REQUEST,
    constraints: '仅探索流量生效；0 表示无限制，无其他候选时仍可兜底',
  }),
  stabilityReleaseMaxPromptKTokens: smartScheduleSettingHelp({
    meaning:
      '以 K Token 设置稳定性试放请求上限，对估算超限的请求后移试放路由，不阻断请求；1K 等于 1000 Token。',
    unit: 'K Token',
    range: '0–1000 的整数',
    defaultValue: '0（无限制）',
    scheduleRelation: NEXT_REQUEST,
    constraints: '仅稳定性试放生效，无其他候选时仍可兜底',
  }),
  probeInterval: smartScheduleSettingHelp({
    meaning: '设定常规探测和降级恢复探测的最短间隔。',
    unit: '分钟',
    range: '1–525600 的整数',
    defaultValue: '10',
    activation: '保存后下一次探测资格检查生效',
    scheduleRelation: '到期后由探测资格检查执行',
    constraints: 'sample_mode=定时探测或开启降级探测时生效；429 冷却期不发请求',
  }),
  degradedProbe: smartScheduleSettingHelp({
    meaning: '允许 P0/W0 硬降级路由主动探测，成功达标后立即恢复。',
    unit: '开关',
    range: '开启或关闭',
    defaultValue: '关闭',
    activation: '保存后下一次探测资格检查生效',
    scheduleRelation: '连续成功事件投影达标后恢复',
    constraints: '需开启稳定性保护，复用探测间隔，429 冷却期禁止探测',
  }),
  stability: smartScheduleSettingHelp({
    meaning: '启用软健康采样、运行时硬保护和恢复试放。',
    unit: '开关',
    range: '开启或关闭',
    defaultValue: '开启',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '关闭时稳定性评分权重及所有保护子配置不生效',
  }),
  stabilityPercent: smartScheduleSettingHelp({
    meaning: '设定稳定性得分在最终得分中的占比。',
    unit: '%',
    range: '0–100',
    defaultValue: '50',
    constraints: '仅稳定性保护开启时生效，剩余比例由业务得分承担',
  }),
  recoveryStabilityScore: smartScheduleSettingHelp({
    meaning: '设定试放新样本恢复基础 P/W 需达到的稳定性得分。',
    unit: '分',
    range: '0–100',
    defaultValue: '95',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '仅稳定性保护开启时生效；试放有效失败会立即重新降级',
  }),
  minSamples: smartScheduleSettingHelp({
    meaning: '定义动态性能指标可评分、渠道清偿样本欠账的最少样本数。',
    unit: '次',
    range: '1–100000 的整数',
    defaultValue: '5',
    scheduleRelation: '新请求和探测样本投影后使用',
    constraints: '硬保护不受此阈值限制',
  }),
  fastFailurePenalty: smartScheduleSettingHelp({
    meaning: '设定最终重试成功时，快速失败按完整失败计入的比例。',
    unit: '%',
    range: '0–100',
    defaultValue: '40',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '最终仍失败的请求始终按一次完整失败计',
  }),
  fastFailureThreshold: smartScheduleSettingHelp({
    meaning: '定义使用快速失败惩罚的耗时上界。',
    unit: '秒',
    range: '>0 且 <60',
    defaultValue: '1',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '必须小于慢失败界限',
  }),
  fastFailureSameChannelRetry: smartScheduleSettingHelp({
    meaning: '设定可重试快速失败在当前渠道的额外重试次数。',
    unit: '次',
    range: '0–10 的整数',
    defaultValue: '0（关闭）',
    scheduleRelation: NEXT_REQUEST,
    constraints: '不消耗普通重试次数，用尽后才进入跨渠道重试',
  }),
  fastFailureSameChannelRetryDelay: smartScheduleSettingHelp({
    meaning: '设定每次同渠道快速重试前的固定等待。',
    unit: '毫秒',
    range: '0–60000 的整数',
    defaultValue: '1000',
    scheduleRelation: NEXT_REQUEST,
    constraints: '仅额外快速重试生效；0 表示不等待，请求取消会立即中断',
  }),
  slowFailureThreshold: smartScheduleSettingHelp({
    meaning: '定义按一次完整失败计算的耗时下界。',
    unit: '秒',
    range: '>0–60',
    defaultValue: '10',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '必须大于快速失败界限，两者之间线性增加惩罚',
  }),
  burstFailureWindowMinutes: smartScheduleSettingHelp({
    meaning: '设定硬保护统计近期请求的最长时间范围。',
    unit: '分钟',
    range: '1–60 的整数',
    defaultValue: '1',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '与窗口请求数共同生效，不依赖稳定性评分或最少样本',
  }),
  burstFailureWindowRequests: smartScheduleSettingHelp({
    meaning: '设定硬保护在时间范围内最多统计的最近请求数。',
    unit: '次',
    range: '1–1000 的整数',
    defaultValue: '100',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '与窗口分钟数共同生效，成功请求进入失败率分母，429 不计入',
  }),
  consecutiveFailureThreshold: smartScheduleSettingHelp({
    meaning: '同一渠道模型连续失败达标时立即进入硬保护。',
    unit: '次',
    range: '1–100 的整数',
    defaultValue: '2',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '成功会清零连续失败计数，429 不计入',
  }),
  burstFailureThresholdPercent: smartScheduleSettingHelp({
    meaning: '保护失败窗口内失败请求占比达标时立即进入硬保护。',
    unit: '%',
    range: '>0–100',
    defaultValue: '3',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '成功请求进入分母，429 不计入；连续失败阈值仍可独立触发',
  }),
  recoverySuccessThreshold: smartScheduleSettingHelp({
    meaning: '设定试放或降级探测恢复正常流量所需的连续成功次数。',
    unit: '次',
    range: '1–100 的整数',
    defaultValue: '2',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '任一有效失败立即清零并重新降级',
  }),
  cooldown: smartScheduleSettingHelp({
    meaning: '设定硬降级保持 P0/W0 的最短时间。',
    unit: '分钟',
    range: '1–525600 的整数',
    defaultValue: '30',
    activation: '保存后新的降级或失败续期使用',
    scheduleRelation: '到期后由下一条相关事件投影进入试放',
    constraints: '到期只进入小流量试放，不代表立即完全恢复',
  }),
  jitter: smartScheduleSettingHelp({
    meaning: '允许一定比例的慢成功免罚，并在首字评分时去除允许的极值。',
    unit: '开关',
    range: '开启或关闭',
    defaultValue: '开启',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '需开启稳定性保护',
  }),
  jitterTolerance: smartScheduleSettingHelp({
    meaning: '设定稳定性窗口内允许免罚的慢成功比例。',
    unit: '%',
    range: '0–50',
    defaultValue: '5',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '需开启稳定性与抖动容忍；大于 0 时向下取整且至少免罚 1 次',
  }),
  jitterSlowThreshold: smartScheduleSettingHelp({
    meaning: '设定将成功请求视为慢成功的首字耗时阈值。',
    unit: '秒',
    range: '0–60',
    defaultValue: '10',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '需开启稳定性与抖动容忍；慢成功仍是协议成功，不直接触发硬保护',
  }),
  costRatioPercent: smartScheduleSettingHelp({
    meaning: '设定成本倍率在业务得分中的权重，倍率越低得分越高。',
    unit: '%',
    range: '0–100',
    defaultValue: '综合策略 40，成本策略 70',
    constraints: '同一策略的成本、首字、TPS 三项必须合计 100',
  }),
  firstTokenPercent: smartScheduleSettingHelp({
    meaning: '设定首字时间在业务得分中的权重，首字越短得分越高。',
    unit: '%',
    range: '0–100',
    defaultValue: '综合策略 40，成本策略 20',
    constraints: '三项必须合计 100；可比渠道不足时不生成单渠道满分',
  }),
  tpsPercent: smartScheduleSettingHelp({
    meaning: '设定 TPS 在业务得分中的权重，TPS 越高得分越高。',
    unit: '%',
    range: '0–100',
    defaultValue: '综合策略 20，成本策略 10',
    constraints: '三项必须合计 100；可比渠道不足时不生成单渠道满分',
  }),
  primaryTraffic: smartScheduleSettingHelp({
    meaning: '设定只调整权重时，同层最高分渠道的目标流量。',
    unit: '%',
    range: '51–99',
    defaultValue: '90',
    constraints: '仅只调整权重模式生效，余下流量按得分分配给同层渠道',
  }),
  primarySwitchThreshold: smartScheduleSettingHelp({
    meaning: '设定新胜者替换当前主渠道所需的最小得分差。',
    unit: '百分点',
    range: '0–100',
    defaultValue: '3',
    constraints: '主渠道不可用或硬保护时立即切换，不等待分差',
  }),
  adaptiveSampling: smartScheduleSettingHelp({
    meaning:
      '根据主渠道近期错误与首字压力，动态增加有样本欠账备援的小流量采样。',
    unit: '开关',
    range: '开启或关闭',
    defaultValue: '开启',
    scheduleRelation: EVENT_REFRESH,
    constraints: '需开启稳定性且使用优先级分层 + 权重；生效时暂停常规探索',
  }),
  adaptiveSamplingBasePercent: smartScheduleSettingHelp({
    meaning: '设定主渠道初入压力时备援采样的起始预算。',
    unit: '%',
    range: '0–10',
    defaultValue: '3',
    scheduleRelation: EVENT_REFRESH,
    constraints: '不得大于最大备援预算，仅自适应备援开启时生效',
  }),
  adaptiveSamplingMaxPercent: smartScheduleSettingHelp({
    meaning:
      '设定高压力时整个池可分给备援采样的最高预算，主渠道最低保留比例自动按 100% - 本值计算。',
    unit: '%',
    range: '1–49',
    defaultValue: '30',
    scheduleRelation: EVENT_REFRESH,
    constraints:
      '必须不小于基础预算，范围上限为 49%（正常单主渠道至少保留 51%）；硬不可用时不受该预算限制',
  }),
  adaptiveSamplingErrorWarningPercent: smartScheduleSettingHelp({
    meaning: '设定主渠道非 429 错误进入软压力计算的告警阈值。',
    unit: '%',
    range: '0–100',
    defaultValue: '5',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '必须小于错误高风险阈值，429 不计入',
  }),
  adaptiveSamplingErrorCriticalPercent: smartScheduleSettingHelp({
    meaning: '设定主渠道非 429 错误达到高风险的阈值。',
    unit: '%',
    range: '0–100',
    defaultValue: '15',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '必须大于错误告警阈值，硬保护仍按独立失败规则执行',
  }),
  adaptiveSamplingFirstTokenWarningSeconds: smartScheduleSettingHelp({
    meaning: '设定主渠道成功请求首字进入延迟压力计算的告警阈值。',
    unit: '秒',
    range: '0–60',
    defaultValue: '5',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '必须小于首字高风险阈值，不等同于上游响应超时',
  }),
  adaptiveSamplingFirstTokenCriticalSeconds: smartScheduleSettingHelp({
    meaning: '设定主渠道成功请求首字达到高风险的阈值。',
    unit: '秒',
    range: '0–60',
    defaultValue: '10',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '必须大于首字告警阈值，不等同于上游响应超时',
  }),
  adaptiveSamplingWindowMinutes: smartScheduleSettingHelp({
    meaning:
      '设定自适应备援计算错误率、首字告警、风险和健康请求占比的最长时间范围。',
    unit: '分钟',
    range: '1–60 的整数',
    defaultValue: '10',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '与请求数窗口共同生效，并与性能、稳定性窗口独立',
  }),
  adaptiveSamplingWindowRequests: smartScheduleSettingHelp({
    meaning: '设定自适应备援在时间范围内最多统计的最近有效请求数。',
    unit: '次',
    range: '1–1000 的整数',
    defaultValue: '100',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '业务请求、手动测试和定时探测共用该上限，429 不计入',
  }),
  adaptiveSamplingFirstTokenWarningRequestPercent: smartScheduleSettingHelp({
    meaning: '设定窗口内有效请求中，成功且首字达到告警秒数的请求占比阈值。',
    unit: '%',
    range: '>0–100',
    defaultValue: '10',
    scheduleRelation: NEXT_OBSERVATION,
    constraints:
      '与恢复健康请求占比之和必须大于 100；失败请求由错误阈值独立判断',
  }),
  adaptiveSamplingRecoverRequestPercent: smartScheduleSettingHelp({
    meaning: '设定错误和首字进入信号都解除后，窗口内健康请求占比的恢复阈值。',
    unit: '%',
    range: '>0–100',
    defaultValue: '95',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '与首字告警请求占比之和必须大于 100，且不得高于切换确认占比',
  }),
  adaptiveSamplingSwitchConfirmRequestPercent: smartScheduleSettingHelp({
    meaning: '设定备援评分胜出后替换主渠道所需的健康请求占比。',
    unit: '%',
    range: '>0–100',
    defaultValue: '95',
    scheduleRelation: NEXT_OBSERVATION,
    constraints: '不得低于恢复健康占比；硬不可用或硬保护切换不等待此确认',
  }),
  adaptiveSamplingMinComparableChannels: smartScheduleSettingHelp({
    meaning: '设定首字、TPS 等相对指标可比较所需的最少成熟渠道数。',
    unit: '条',
    range: '2–10 的整数',
    defaultValue: '2',
    constraints: '不足时不生成单渠道满分，保留当前主渠道并继续补样',
  }),
} as const

export type ChannelMonitorSettingHelpKey =
  keyof typeof CHANNEL_MONITOR_SMART_SCHEDULE_SETTING_HELP

type ChannelMonitorSettingLabelProps = {
  label: string
  helpKey: ChannelMonitorSettingHelpKey
  htmlFor?: string
}

export function ChannelMonitorSettingLabel(
  props: ChannelMonitorSettingLabelProps
) {
  const label = props.htmlFor ? (
    <FormLabel htmlFor={props.htmlFor}>{props.label}</FormLabel>
  ) : (
    <FormLabel>{props.label}</FormLabel>
  )

  return (
    <span className='inline-flex min-w-0 items-center gap-1'>
      {label}
      <TooltipProvider delay={150}>
        <Tooltip>
          <TooltipTrigger
            render={
              <button
                type='button'
                className='text-muted-foreground hover:text-foreground focus-visible:ring-ring/50 inline-flex size-5 shrink-0 cursor-help items-center justify-center rounded-sm transition-colors focus-visible:ring-2 focus-visible:outline-none'
                aria-label={`查看“${props.label}”说明`}
              >
                <HugeiconsIcon
                  icon={InformationCircleIcon}
                  className='size-3.5'
                  aria-hidden='true'
                />
              </button>
            }
          />
          <TooltipContent
            side='top'
            className='max-w-80 text-left leading-5 whitespace-normal'
          >
            {CHANNEL_MONITOR_SMART_SCHEDULE_SETTING_HELP[props.helpKey]}
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    </span>
  )
}
