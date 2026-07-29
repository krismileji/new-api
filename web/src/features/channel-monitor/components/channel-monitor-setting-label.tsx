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
    '智能调度任务的运行频率。每次使用最新统计窗口重新评分；权重受单次变动上限约束，优先级按所选调整方式处理。',
  performanceRange:
    '评分使用最近这段时间内的消费与性能日志。窗口越短响应越快但波动更大，窗口越长越平滑但恢复更慢。',
  responseHeaderTimeout:
    '等待上游返回响应头的最长时间。0 表示不限制；超时后按现有规则重试，收到响应头后不限制后续流式输出。',
  forceReset:
    '仅在本次保存后执行一次：忽略渐进调整幅度，按当前统计窗口重新计算所有已配置路由的优先级和权重，不影响未配置分组。',
  group:
    '策略只作用于这个分组，每个分组只能配置一条策略。保存后该分组才参与智能调度，删除策略后立即退出。',
  strategy:
    '决定业务得分主要比较什么：综合指标、成本倍率、首字时间或 TPS。开启稳定性保护后，稳定性得分还会按配置占比合入最终得分。',
  applyMode:
    '“只调整权重”会保留现有优先级，并只比较同优先级渠道；“优先级分层 + 权重”会在同一分组和模型池内重新分为 100、90、80 三档。',
  models:
    '限定策略覆盖的模型。留空表示此分组内全部模型；实际评分与调整仍按“分组 + 模型”分别进行。',
  sampleMode:
    '样本不足时的处理方式：关闭会保持当前路由；探索流量使用少量真实业务请求补样本；定时探测会主动请求上游。',
  explorationTraffic:
    '每个“分组 + 模型”池同一时间只探索一个缺少样本的渠道。该值是目标流量占比，实际比例会因整数权重略有偏差。',
  probeInterval:
    '到期后对支持的文本模型发送 /v1/responses 流式请求。探测会写消费日志并计入渠道成本；非文本模型和降级中的路由会跳过。',
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
    '统计窗口内允许免罚的慢成功比例，向下取整且至少允许 1 次；超过的部分按其样本占比扣减稳定性得分。',
  jitterMultiplier:
    '相对当前基线的慢请求判定倍率。最终阈值取“基线 × 判定倍率”和“基线 + 绝对容差”中的较大值。',
  jitterAbsoluteTolerance:
    '允许高于当前基线的固定延迟，为低延迟模型提供最低缓冲。最终阈值仍取倍率阈值与固定容差阈值中的较大值。',
  jitterBaseline:
    '使用近期成功请求的首字 P50 平滑学习基线。周期越长更新越慢；每次学习最多只向当前基线的上下 10% 移动。',
  costRatioPercent:
    '成本倍率越低得分越高。该百分比是它在业务得分中的占比；成本倍率、首字时间和 TPS 三项合计必须为 100%。',
  firstTokenPercent:
    '首字时间越短得分越高。该百分比是它在业务得分中的占比；有效样本不足时会跳过此指标，并按其他可用指标重新归一。',
  tpsPercent:
    'TPS 越高得分越高。该百分比是它在业务得分中的占比；有效样本不足时会跳过此指标，并按其他可用指标重新归一。',
  curveExponent:
    '将最终 0 到 1 得分按“得分 ^ 指数”映射为权重。1 表示线性；大于 1 会压低中低分并拉开权重，过高会使流量更集中；小于 1 会收窄差距。',
  relativeWeight:
    '在同一“分组 + 模型”池内，按最低分到最高分的相对位置拉伸权重；只调整权重时，还会按渠道原优先级分别计算。',
  relativeWeightStart:
    '池内最高分与最低分的差值低于此值时，只使用绝对得分曲线；达到此值后开始渐进混合相对权重。',
  relativeWeightFull:
    '池内最高分与最低分的差值达到此值时，完全按池内相对位置计算目标权重；起始值与完整值之间会渐进混合。',
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
