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
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Switch } from '@/components/ui/switch'

type ChannelTestSampleToggleProps = {
  id: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  disabled?: boolean
}

export function ChannelTestSampleToggle(props: ChannelTestSampleToggleProps) {
  return (
    <Field>
      <FieldLabel htmlFor={props.id}>计入渠道样本</FieldLabel>
      <div className='flex h-8 items-center gap-2'>
        <Switch
          id={props.id}
          checked={props.checked}
          onCheckedChange={props.onCheckedChange}
          disabled={props.disabled}
        />
        <span className='text-sm'>{props.checked ? '已开启' : '已关闭'}</span>
      </div>
      <FieldDescription>
        开启后，渠道模型参与智能调度时，本次测试结果会计入共享样本。
      </FieldDescription>
    </Field>
  )
}
