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
import type {
  ChannelMonitorItem,
  ChannelMonitorSmartScheduleGroupPolicy,
  ChannelMonitorSmartScheduleRoute,
} from '../types'

type SmartScheduleModelRoute = Pick<
  ChannelMonitorSmartScheduleRoute,
  'group' | 'model'
>
type SmartScheduleModelChannel = Pick<ChannelMonitorItem, 'groups' | 'models'>
type SmartScheduleModelPolicy = Pick<
  ChannelMonitorSmartScheduleGroupPolicy,
  'group' | 'models' | 'model_order'
>

export function getChannelMonitorSmartScheduleModelOptionsByGroup(
  routes: readonly SmartScheduleModelRoute[],
  channels: readonly SmartScheduleModelChannel[],
  policies: readonly SmartScheduleModelPolicy[]
): ReadonlyMap<string, string[]> {
  const modelsByGroup = new Map<string, Set<string>>()
  const groupsWithRoutes = new Set<string>()
  const addModel = (group: string, model: string) => {
    const normalizedGroup = group.trim()
    const normalizedModel = model.trim()
    if (!normalizedGroup || !normalizedModel) return
    const models = modelsByGroup.get(normalizedGroup) ?? new Set<string>()
    models.add(normalizedModel)
    modelsByGroup.set(normalizedGroup, models)
  }

  for (const route of routes) {
    const group = route.group.trim()
    if (!group) continue
    groupsWithRoutes.add(group)
    addModel(group, route.model)
  }

  for (const channel of channels) {
    const models = channel.models
      .split(',')
      .map((model) => model.trim())
      .filter(Boolean)
    if (models.length === 0) continue
    for (const rawGroup of channel.groups) {
      const group = rawGroup.trim()
      if (!group || groupsWithRoutes.has(group)) continue
      for (const model of models) addModel(group, model)
    }
  }

  for (const policy of policies) {
    for (const model of policy.models) addModel(policy.group, model)
    for (const model of policy.model_order ?? []) {
      addModel(policy.group, model)
    }
  }

  return new Map(
    [...modelsByGroup]
      .sort(([firstGroup], [secondGroup]) =>
        firstGroup.localeCompare(secondGroup, 'zh-CN')
      )
      .map(([group, models]) => [
        group,
        [...models].sort((first, second) =>
          first.localeCompare(second, 'zh-CN')
        ),
      ])
  )
}

export function compareChannelMonitorSmartScheduleModels(
  first: string,
  second: string,
  configuredOrder: readonly string[] = []
): number {
  const firstIndex = configuredOrder.indexOf(first)
  const secondIndex = configuredOrder.indexOf(second)

  if (firstIndex >= 0 && secondIndex >= 0) return firstIndex - secondIndex
  if (firstIndex >= 0) return -1
  if (secondIndex >= 0) return 1
  return first.localeCompare(second)
}

export function mergeChannelMonitorSmartScheduleModelOrder(
  selectedModels: readonly string[],
  configuredOrder: readonly string[]
): string[] {
  if (selectedModels.length === 0) return [...new Set(configuredOrder)]

  const selectedSet = new Set(selectedModels)
  const seen = new Set<string>()
  const nextOrder: string[] = []
  for (const model of configuredOrder) {
    if (!selectedSet.has(model) || seen.has(model)) continue
    seen.add(model)
    nextOrder.push(model)
  }
  for (const model of selectedModels) {
    if (seen.has(model)) continue
    seen.add(model)
    nextOrder.push(model)
  }
  return nextOrder
}
