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
type ChannelWithModels = {
  models?: string | null
}

type SelectableChannel = {
  id: number
  status: number
}

type ChannelWithRemark = {
  id: number
  name: string
  remark?: string | null
  channel_remark?: string | null
}

export type BatchTestChannelOption = {
  value: string
  label: string
  channelLabel: string
  remark: string
}

function parseConfiguredModels(models?: string | null): Set<string> {
  if (!models) return new Set()

  return new Set(
    models
      .split(',')
      .map((model) => model.trim())
      .filter(Boolean)
  )
}

export function createBatchTestChannelOption(
  channel: ChannelWithRemark
): BatchTestChannelOption {
  const channelLabel = `#${channel.id} ${channel.name}`
  const rawRemark =
    'channel_remark' in channel ? channel.channel_remark : channel.remark
  const remark = rawRemark?.trim() ?? ''

  return {
    value: String(channel.id),
    label: remark ? `${channelLabel} · 备注：${remark}` : channelLabel,
    channelLabel,
    remark,
  }
}

export function getSelectableModelNames(
  pricedModels: readonly string[],
  channels: readonly ChannelWithModels[]
): string[] {
  const configuredModels = new Set<string>()
  for (const channel of channels) {
    for (const model of parseConfiguredModels(channel.models)) {
      configuredModels.add(model)
    }
  }

  const seenModels = new Set<string>()
  const selectableModels: string[] = []
  for (const model of pricedModels) {
    const normalizedModel = model.trim()
    if (
      !normalizedModel ||
      seenModels.has(normalizedModel) ||
      !configuredModels.has(normalizedModel)
    ) {
      continue
    }
    seenModels.add(normalizedModel)
    selectableModels.push(normalizedModel)
  }
  return selectableModels
}

export function getChannelsSupportingModels<T extends ChannelWithModels>(
  channels: readonly T[],
  selectedModels: readonly string[]
): T[] {
  const requiredModels = [
    ...new Set(selectedModels.map((model) => model.trim()).filter(Boolean)),
  ]
  if (requiredModels.length === 0) return []

  return channels.filter((channel) => {
    const configuredModels = parseConfiguredModels(channel.models)
    return requiredModels.every((model) => configuredModels.has(model))
  })
}

export function retainCompatibleChannelIds(
  selectedChannelIds: readonly string[],
  compatibleChannels: readonly Pick<SelectableChannel, 'id'>[]
): string[] {
  const compatibleIds = new Set(
    compatibleChannels.map((channel) => String(channel.id))
  )
  return selectedChannelIds.filter((channelId) => compatibleIds.has(channelId))
}

export function getSelectableChannelIds(
  compatibleChannels: readonly SelectableChannel[],
  selectAllMode?: 'all' | 'enabled'
): string[] {
  return compatibleChannels
    .filter((channel) => selectAllMode === 'all' || channel.status === 1)
    .map((channel) => String(channel.id))
}
