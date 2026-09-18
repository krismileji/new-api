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
/** Expand scientific notation without rounding or adding locale separators. */
export function formatDecimalNumber(value: number | null | undefined): string {
  if (value == null) return ''

  const text = String(value)
  if (!Number.isFinite(value)) return text

  const [coefficient, exponent] = text.split('e')
  if (exponent === undefined) return text

  const sign = value < 0 ? '-' : ''
  const [integer, fraction = ''] = coefficient.replace('-', '').split('.')
  const digits = integer + fraction
  const decimalPosition = integer.length + Number(exponent)

  if (decimalPosition <= 0) {
    return `${sign}0.${'0'.repeat(-decimalPosition)}${digits}`
  }
  if (decimalPosition >= digits.length) {
    return `${sign}${digits}${'0'.repeat(decimalPosition - digits.length)}`
  }
  return `${sign}${digits.slice(0, decimalPosition)}.${digits.slice(decimalPosition)}`
}
