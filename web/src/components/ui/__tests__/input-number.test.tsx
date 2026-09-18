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
import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { expect, test } from 'vitest'

import { Input } from '../input'
import { InputGroup, InputGroupInput } from '../input-group'

test.each([
  [2e-7, '0.0000002'],
  [-2e-7, '-0.0000002'],
  [1.2345678901234566e-7, '0.00000012345678901234566'],
  [1.2e21, '1200000000000000000000'],
  [5e-324, `0.${'0'.repeat(323)}5`],
  [0, '0'],
  [12.5, '12.5'],
])('数值 %s 在数字输入框中按完整十进制显示且不改变数值', (value, expected) => {
  render(<Input type='number' aria-label='数值' value={value} readOnly />)

  const input = screen.getByRole('spinbutton', { name: '数值' })
  expect(input).toHaveProperty('value', expected)
  expect(input).toHaveValue(value)
})

test('组合输入框的默认小数显示完整，用户可以清空后逐位输入', async () => {
  const user = userEvent.setup()
  render(
    <InputGroup>
      <InputGroupInput
        type='number'
        aria-label='结果乘数'
        defaultValue={2e-7}
        step='any'
      />
    </InputGroup>
  )

  const input = screen.getByRole('spinbutton', { name: '结果乘数' })
  expect(input).toHaveProperty('value', '0.0000002')
  await user.clear(input)
  expect(input).toHaveValue(null)
  await user.type(input, '0.0000003')
  expect(input).toHaveValue(3e-7)
})

function EditableDecimal() {
  const [value, setValue] = useState<number | string>(2e-7)
  return (
    <Input
      aria-label='价格'
      inputMode='decimal'
      value={value}
      onChange={(event) => setValue(event.target.value)}
    />
  )
}

test('受控小数输入可保留小数点、末尾零和空值', async () => {
  const user = userEvent.setup()
  render(<EditableDecimal />)
  const input = screen.getByRole('textbox', { name: '价格' })
  expect(input).toHaveValue('0.0000002')

  await user.clear(input)
  expect(input).toHaveValue('')
  await user.type(input, '0.')
  expect(input).toHaveValue('0.')
  await user.type(input, '00000030')
  expect(input).toHaveValue('0.00000030')
})

test('重新加载数值后显示新的完整小数', () => {
  const view = render(<Input type='number' value={1} readOnly />)
  view.rerender(<Input type='number' value={3e-8} readOnly />)
  expect(screen.getByRole('spinbutton')).toHaveProperty('value', '0.00000003')
})

test('文本内容中的科学计数法字符串保持原样', () => {
  render(<Input aria-label='标识符' value='2e-7' readOnly />)
  expect(screen.getByRole('textbox', { name: '标识符' })).toHaveValue('2e-7')
})

function NumericValueInput() {
  const [value, setValue] = useState(1)
  return (
    <Input
      type='number'
      value={value}
      onChange={(event) => setValue(event.target.valueAsNumber)}
    />
  )
}

test('以数值管理状态的普通小数输入保留用户输入的尾零', () => {
  render(<NumericValueInput />)
  const input = screen.getByRole('spinbutton')
  fireEvent.change(input, { target: { value: '1.20' } })
  expect(input).toHaveProperty('value', '1.20')
})
