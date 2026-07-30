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
import {
  ArrowDown01Icon,
  ArrowUp01Icon,
  DragDropVerticalIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMemo, useState, type DragEvent } from 'react'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import { compareChannelMonitorSmartScheduleModels } from '../lib/smart-schedule-model-order'

type ChannelMonitorSmartScheduleModelOrderProps = {
  models: readonly string[]
  value: readonly string[]
  onChange: (models: string[]) => void
}

type DropPosition = 'before' | 'after'

export function ChannelMonitorSmartScheduleModelOrder(
  props: ChannelMonitorSmartScheduleModelOrderProps
) {
  const [draggedModel, setDraggedModel] = useState<string | null>(null)
  const [dragOverModel, setDragOverModel] = useState<string | null>(null)
  const [dropPosition, setDropPosition] = useState<DropPosition>('before')
  const orderedModels = useMemo(
    () =>
      [...new Set(props.models)].sort((first, second) =>
        compareChannelMonitorSmartScheduleModels(first, second, props.value)
      ),
    [props.models, props.value]
  )

  const resetDragState = () => {
    setDraggedModel(null)
    setDragOverModel(null)
    setDropPosition('before')
  }

  const moveModel = (model: string, offset: -1 | 1) => {
    const sourceIndex = orderedModels.indexOf(model)
    const targetIndex = sourceIndex + offset
    if (
      sourceIndex < 0 ||
      targetIndex < 0 ||
      targetIndex >= orderedModels.length
    ) {
      return
    }
    const nextOrder = [...orderedModels]
    const targetModel = nextOrder[targetIndex]
    nextOrder[targetIndex] = nextOrder[sourceIndex]
    nextOrder[sourceIndex] = targetModel
    props.onChange(nextOrder)
  }

  const handleDragStart = (
    event: DragEvent<HTMLButtonElement>,
    model: string
  ) => {
    setDraggedModel(model)
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', model)
  }

  const handleDragOver = (event: DragEvent<HTMLDivElement>, model: string) => {
    event.preventDefault()
    if (draggedModel == null || draggedModel === model) return
    const rect = event.currentTarget.getBoundingClientRect()
    setDragOverModel(model)
    setDropPosition(
      event.clientY - rect.top > rect.height / 2 ? 'after' : 'before'
    )
    event.dataTransfer.dropEffect = 'move'
  }

  const handleDrop = (event: DragEvent<HTMLDivElement>, model: string) => {
    event.preventDefault()
    const sourceModel = draggedModel ?? event.dataTransfer.getData('text/plain')
    if (!sourceModel || sourceModel === model) {
      resetDragState()
      return
    }
    const nextOrder = orderedModels.filter((item) => item !== sourceModel)
    let targetIndex = nextOrder.indexOf(model)
    if (targetIndex < 0) {
      resetDragState()
      return
    }
    if (dropPosition === 'after') targetIndex += 1
    nextOrder.splice(targetIndex, 0, sourceModel)
    props.onChange(nextOrder)
    resetDragState()
  }

  if (orderedModels.length === 0) {
    return (
      <div className='text-muted-foreground rounded-md border border-dashed px-3 py-6 text-center text-sm'>
        暂无可排序模型
      </div>
    )
  }

  return (
    <div
      className='max-h-60 space-y-1.5 overflow-y-auto rounded-md border p-1.5'
      data-slot='smart-schedule-model-order'
    >
      {orderedModels.map((model, index) => {
        const isDragging = model === draggedModel
        const isDropTarget =
          model === dragOverModel &&
          draggedModel != null &&
          draggedModel !== model
        return (
          <div
            key={model}
            data-model={model}
            onDragOver={(event) => handleDragOver(event, model)}
            onDrop={(event) => handleDrop(event, model)}
            className={cn(
              'bg-background flex min-w-0 items-center gap-2 rounded-md border px-2 py-1.5 transition-colors',
              isDragging && 'opacity-50',
              isDropTarget &&
                dropPosition === 'before' &&
                'border-t-primary border-t-2',
              isDropTarget &&
                dropPosition === 'after' &&
                'border-b-primary border-b-2'
            )}
          >
            <button
              type='button'
              draggable={orderedModels.length > 1}
              onDragStart={(event) => handleDragStart(event, model)}
              onDragEnd={resetDragState}
              className='text-muted-foreground hover:text-foreground flex size-7 shrink-0 cursor-grab items-center justify-center rounded-sm active:cursor-grabbing'
              aria-label={`拖动模型 ${model}`}
            >
              <HugeiconsIcon
                icon={DragDropVerticalIcon}
                className='size-4'
                aria-hidden='true'
              />
            </button>
            <span className='bg-muted text-muted-foreground flex size-6 shrink-0 items-center justify-center rounded-sm text-xs font-medium tabular-nums'>
              {index + 1}
            </span>
            <span className='min-w-0 flex-1 truncate text-sm font-medium'>
              {model}
            </span>
            <div className='flex shrink-0 gap-0.5'>
              <Button
                type='button'
                variant='ghost'
                size='icon-sm'
                onClick={() => moveModel(model, -1)}
                disabled={index === 0}
                aria-label={`上移模型 ${model}`}
              >
                <HugeiconsIcon icon={ArrowUp01Icon} aria-hidden='true' />
              </Button>
              <Button
                type='button'
                variant='ghost'
                size='icon-sm'
                onClick={() => moveModel(model, 1)}
                disabled={index === orderedModels.length - 1}
                aria-label={`下移模型 ${model}`}
              >
                <HugeiconsIcon icon={ArrowDown01Icon} aria-hidden='true' />
              </Button>
            </div>
          </div>
        )
      })}
    </div>
  )
}
