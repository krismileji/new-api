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
export type ChannelDefaultRoutingField = 'priority' | 'weight'

const CHANNEL_DEFAULT_ROUTING_COPY = {
  priority: {
    label: '默认优先级',
    valueLabel: '优先级',
    help: '这里修改的是渠道默认优先级。已参与智能调度的“分组 + 模型”路由会保留各自的实际优先级，不会被此处覆盖。需要手动接管时，请先在渠道监控中取消对应路由参与智能调度。',
  },
  weight: {
    label: '默认权重',
    valueLabel: '权重',
    help: '这里修改的是渠道默认权重。已参与智能调度的“分组 + 模型”路由会保留各自的实际权重，不会被此处覆盖。需要手动接管时，请先在渠道监控中取消对应路由参与智能调度。',
  },
} as const

export function getChannelDefaultRoutingCopy(
  field: ChannelDefaultRoutingField
) {
  return CHANNEL_DEFAULT_ROUTING_COPY[field]
}

export function formatChannelDefaultRoutingUpdateMessage(
  field: ChannelDefaultRoutingField,
  value: number,
  tag?: string
): string {
  const copy = getChannelDefaultRoutingCopy(field)
  const target = tag ? `标签“${tag}”下的渠道` : '渠道'
  return `${target}${copy.label}已更新为 ${value}。参与智能调度的路由仍使用各自的实际${copy.valueLabel}。`
}

export function formatChannelDefaultRoutingBatchConfirmation(
  field: ChannelDefaultRoutingField,
  value: number | null,
  channelCount: number,
  tag: string
): string {
  const copy = getChannelDefaultRoutingCopy(field)
  return `将把标签“${tag}”下 ${channelCount} 个渠道的${copy.label}更新为 ${value}。参与智能调度的“分组 + 模型”路由仍保留各自的实际${copy.valueLabel}，不会被本次更新覆盖。是否继续？`
}
