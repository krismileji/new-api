/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { api, type ApiRequestConfig } from '@/lib/api'

export type LogicalChannelGroupMember = {
  id: number
  channel_id: number
  channel_name?: string
  channel_type?: number
  channel_status?: number
  weight: number
  normalized_address?: string
  address_fingerprint?: string
  created_at: number
  updated_at: number
}

export type LogicalChannelGroup = {
  id: number
  name: string
  remark?: string
  status: number
  revision: number
  created_at: number
  updated_at: number
  members: LogicalChannelGroupMember[]
}

export type LogicalChannelAddressPrecheckMember = {
  channel_id: number
  normalized_address?: string
  error?: string
}

export type LogicalChannelAddressPrecheck = {
  compatible: boolean
  normalized_address?: string
  members: LogicalChannelAddressPrecheckMember[]
  error?: string
}

type LogicalGroupResponse<T> = {
  success: boolean
  message?: string
  data?: T
}

const logicalGroupRequestConfig = (
  config: ApiRequestConfig = {}
): ApiRequestConfig => ({
  ...config,
  skipBusinessError: true,
  skipErrorHandler: true,
})

export async function getLogicalChannelGroups() {
  const response = await api.get<LogicalGroupResponse<LogicalChannelGroup[]>>(
    '/api/channel/logical-groups',
    logicalGroupRequestConfig()
  )
  return response.data
}

export async function precheckLogicalChannelGroup(channelIds: number[]) {
  const response = await api.post<
    LogicalGroupResponse<LogicalChannelAddressPrecheck>
  >(
    '/api/channel/logical-groups/precheck',
    { channel_ids: channelIds },
    logicalGroupRequestConfig()
  )
  return response.data
}

export type LogicalChannelGroupMemberInput = {
  channel_id: number
  weight?: number
}

export async function createLogicalChannelGroup(request: {
  name: string
  remark?: string
  status?: number
  members: LogicalChannelGroupMemberInput[]
}) {
  const response = await api.post<LogicalGroupResponse<LogicalChannelGroup>>(
    '/api/channel/logical-groups',
    request,
    logicalGroupRequestConfig()
  )
  return response.data
}

export async function replaceLogicalChannelGroupMembers(request: {
  id: number
  revision: number
  members: LogicalChannelGroupMemberInput[]
}) {
  const response = await api.put<LogicalGroupResponse<LogicalChannelGroup>>(
    `/api/channel/logical-groups/${request.id}/members`,
    { revision: request.revision, members: request.members },
    logicalGroupRequestConfig()
  )
  return response.data
}

export async function updateLogicalChannelGroupStatus(request: {
  id: number
  revision: number
  status: number
}) {
  const response = await api.put<LogicalGroupResponse<LogicalChannelGroup>>(
    `/api/channel/logical-groups/${request.id}/status`,
    { revision: request.revision, status: request.status },
    logicalGroupRequestConfig()
  )
  return response.data
}

export async function deleteLogicalChannelGroup(request: {
  id: number
  revision: number
}) {
  const response = await api.delete<LogicalGroupResponse<{ id: number }>>(
    `/api/channel/logical-groups/${request.id}`,
    logicalGroupRequestConfig({ data: { revision: request.revision } })
  )
  return response.data
}
