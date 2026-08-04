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

const CHANNEL_MONITOR_SMART_SCHEDULE_SETTING_HELP = {
  enabled:
    '智能调度总开关。开启后，系统按调度间隔处理已配置策略的分组；没有策略的分组以及手动取消参与的渠道不会被调整。',
  interval:
    '智能调度任务的运行频率。每次使用最新统计窗口重新评分，并按主渠道切换分差决定是否更换承接最多请求的渠道。',
  performanceRange:
    '评分使用最近这段时间内的消费与性能日志。窗口越短响应越快但波动更大，窗口越长越平滑但恢复更慢。',
  responseHeaderTimeout:
    '流式请求等待首个有效模型事件的最长时间，响应头、空行和心跳不算首字；非流式请求仍限制响应头等待时间。0 表示不限制，超时后按现有规则切换渠道重试。',
  rateLimitCooldown:
    '上游 429 通常表示并发暂时达到限制。冷却只临时阻止同一渠道和模型承接新请求，到期自动恢复；不会改变优先级、权重或稳定性状态，0 秒表示关闭。',
  forceReset:
    '仅在本次保存后执行一次：忽略渐进调整幅度，按当前统计窗口重新计算所有已配置路由的优先级和权重，不影响未配置分组。',
  group:
    '策略只作用于这个分组，每个分组只能配置一条策略。保存后该分组才参与智能调度，删除策略后立即退出。',
  strategy:
    '决定业务得分主要比较什么：综合指标、成本倍率、首字时间或 TPS。开启稳定性保护后，稳定性得分还会按配置占比合入最终得分。',
  applyMode:
    '“只调整权重”保留现有优先级，只在同一优先级层按得分分配权重；“优先级分层 + 权重”为每条正常路由生成唯一顺位，N 条路由对应 PN 至 P1。',
  models:
    '限定策略覆盖的模型。留空表示此分组内全部模型；实际评分与调整仍按“分组 + 模型”分别进行。',
  modelOrder:
    '控制当前分组在智能调度看板中的模型卡片顺序，只影响展示，不会改变参与模型、调度优先级、权重或流量分配。未配置的新增模型会按名称追加在末尾。',
  sampleMode:
    '样本不足时的自动补充方式：关闭会保持当前路由；探索流量使用少量真实业务请求补样本；定时探测会主动请求上游。参与调度路由的手动渠道测试始终会计入样本。',
  explorationTraffic:
    '每个“分组 + 模型”池同一时间只探索一个缺少样本的渠道。该值是目标流量占比，实际比例会因整数权重略有偏差。',
  probeInterval:
    '到期后对支持的文本模型发送 /v1/responses 流式请求。实际执行的成功和失败结果都会计入稳定性样本；探测会写消费日志并计入渠道成本。',
  stability:
    '把最终失败、重试失败耗时和成功延迟抖动合成稳定性得分。低于降级阈值时，仅将当前“分组 + 模型”路由降为优先级 0、权重 0。',
  stabilityPercent:
    '稳定性得分在最终得分中的占比，剩余比例来自当前调度方式的业务指标。值越高，路由越偏向稳定渠道。',
  degradeStabilityScore:
    '样本达到最少样本且稳定性得分低于此值时，路由进入稳定性释放，并在降级时长内保持优先级 0、权重 0。',
  recoveryStabilityScore:
    '降级期结束后会先小流量试放，新样本得分达到此值才恢复原优先级和权重。该值必须高于降级得分，以避免状态反复切换。',
  minSamples:
    '首字时间、TPS 和稳定性等指标至少需要这些有效样本才参与评分或触发保护。样本不足时按所选样本补充方式处理。',
  fastFailurePenalty:
    '上游快速报错但后续重试成功时，每次失败按该比例计入惩罚；最终仍失败的请求始终按一次完整失败计。',
  fastFailureThreshold:
    '重试失败耗时不超过此值时按“快速失败惩罚”计算；超过后，惩罚会随耗时线性增加。',
  slowFailureThreshold:
    '重试失败耗时达到此值后按一次完整失败计算；快速失败界限与慢失败界限之间按耗时线性增加惩罚。',
  cooldown:
    '进入稳定性释放后保持优先级 0、权重 0 的最短时间。到期后进入小流量试放，不代表立即完全恢复。',
  jitter:
    '允许将偶发慢成功视为正常波动。超过允许数量的慢成功才会扣减稳定性得分，同时首字评分会使用去极值后的均值。',
  jitterTolerance:
    '统计窗口内允许免罚的慢成功比例。设为 0% 时不免罚；大于 0% 时向下取整且至少允许 1 次，超过的部分按其样本占比扣减稳定性得分。',
  jitterAbsoluteTolerance:
    '允许高于当前基线的固定延迟，慢请求阈值为当前基线加上该绝对容差。',
  jitterBaseline:
    '使用近期健康状态下成功请求的首字 P50 平滑学习基线。周期越长更新越慢；异常窗口不参与学习，每次学习最多只向当前基线的上下 10% 移动。',
  costRatioPercent:
    '成本倍率越低得分越高。该百分比是它在业务得分中的占比；成本倍率、首字时间和 TPS 三项合计必须为 100%。',
  firstTokenPercent:
    '首字时间越短得分越高。该百分比是它在业务得分中的占比；有效样本不足时会跳过此指标，并按其他可用指标重新归一。',
  tpsPercent:
    'TPS 越高得分越高。该百分比是它在业务得分中的占比；有效样本不足时会跳过此指标，并按其他可用指标重新归一。',
  primaryTraffic:
    '仅在“只调整权重”下生效。系统保留现有优先级，让每个优先级中最终得分最高的渠道承接该目标比例，其余流量按最终得分分配给同层备用渠道。',
  primarySwitchThreshold:
    '新渠道的最终得分必须至少高于当前主渠道这些百分点才会替换主渠道。主渠道被禁用、不可用或触发稳定性降级时会立即切换，不等待达到分差。',
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
