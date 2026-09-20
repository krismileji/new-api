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
import { safeJsonParse } from '../utils/json-parser'

// UserUsableGroups remains the public selectable-group map. Keep descriptions
// separately so hiding a group does not discard its text.
export function mergeGroupDescriptions(...sources: string[]): string {
  let descriptions: Record<string, string> = {}
  for (const source of sources) {
    descriptions = {
      ...descriptions,
      ...safeJsonParse<Record<string, string>>(source, {
        fallback: {},
        silent: true,
      }),
    }
  }
  return JSON.stringify(descriptions, null, 2)
}
