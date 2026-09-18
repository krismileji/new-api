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
import { useWatch, type UseFormReturn } from 'react-hook-form'

import { useChannelMonitorVariableGroups } from '../api-variable-groups'
import type { UpstreamConfigFormValues } from '../lib/schema'

export function UpstreamEditorVariableReference(props: {
  form: UseFormReturn<UpstreamConfigFormValues>
}) {
  const groupId = useWatch({
    control: props.form.control,
    name: 'customConfig.variableGroupId',
  })
  const requests = useWatch({
    control: props.form.control,
    name: 'customConfig.variableRequests',
  })
  const groups = useChannelMonitorVariableGroups(Boolean(groupId))
  const group = groups.data?.find((item) => item.id === groupId)
  const sourceRequests = groupId ? (group?.variable_requests ?? []) : requests
  let sourceLabel = '当前配置的请求与变量'
  if (groupId) {
    sourceLabel = group?.name ?? `共享配置 #${groupId}（不可用）`
    if (groups.isPending) sourceLabel = '正在读取共享配置'
  }

  return (
    <section aria-label='变量速查' className='flex min-w-0 flex-col gap-3'>
      <h3 className='text-sm font-medium'>变量速查</h3>
      <p className='text-muted-foreground text-xs break-words'>{sourceLabel}</p>
      {groupId && groups.isError ? (
        <p className='text-destructive text-xs'>
          共享变量加载失败，请在请求与变量区域重试。
        </p>
      ) : null}
      {sourceRequests.length === 0 ? (
        <p className='text-muted-foreground text-xs'>暂无可用变量</p>
      ) : (
        sourceRequests.map((request, index) => (
          <div key={request.id} className='flex min-w-0 flex-col gap-2'>
            <p className='text-muted-foreground text-xs break-all'>
              {request.name || `请求 ${index + 1}`}
            </p>
            {request.variables
              .filter(
                (variable, index, variables) =>
                  variable.name &&
                  variables.findIndex((item) => item.name === variable.name) ===
                    index
              )
              .map((variable) => (
                <div
                  key={variable.name}
                  className='bg-background flex min-w-0 flex-col gap-1 rounded-md border px-2.5 py-2'
                >
                  <code className='text-xs break-all'>{`{{${variable.name}}}`}</code>
                  <span className='text-muted-foreground text-xs break-all'>
                    {'valuePath' in variable
                      ? variable.valuePath
                      : variable.value_path}
                  </span>
                </div>
              ))}
          </div>
        ))
      )}
    </section>
  )
}
