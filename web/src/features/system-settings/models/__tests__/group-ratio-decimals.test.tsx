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
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import { GroupRatioVisualEditor } from '../group-ratio-visual-editor'

test('分组倍率和充值倍率加载为小数，编辑后仍按原精度输出设置', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  render(
    <GroupRatioVisualEditor
      groupRatio='{"default":2e-7}'
      groupOrder='["default"]'
      topupGroupRatio='{"default":3e-7}'
      userUsableGroups='{}'
      groupGroupRatio='{}'
      autoGroups='[]'
      maxTokenAutoGroupsField={null}
      groupSpecialUsableGroup='{}'
      onChange={onChange}
    />
  )
  const row = screen
    .getAllByRole('row')
    .find((item) => within(item).queryByDisplayValue('default'))
  if (!row) throw new Error('未找到默认分组')
  const [ratio, topupRatio] = within(row).getAllByRole('spinbutton')
  expect(ratio).toHaveProperty('value', '0.0000002')
  expect(topupRatio).toHaveProperty('value', '0.0000003')

  await user.clear(ratio)
  await user.type(ratio, '0.0000004')
  expect(onChange).toHaveBeenCalledWith(
    'GroupRatio',
    JSON.stringify({ default: 4e-7 }, null, 2)
  )
  expect(onChange).toHaveBeenCalledWith(
    'TopupGroupRatio',
    JSON.stringify({ default: 3e-7 }, null, 2)
  )
})
