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
import { useId, useRef, useState, type ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

export const upstreamEditorDialogClassName =
  'top-0 left-0 flex h-dvh max-h-dvh w-full max-w-none translate-x-0 translate-y-0 flex-col gap-0 overflow-hidden rounded-none p-0 sm:max-w-none'

export const upstreamEditorHeaderClassName =
  'shrink-0 border-b px-4 py-4 pr-12 sm:px-6 sm:pr-12'

export const upstreamEditorFooterClassName =
  'bg-background flex shrink-0 flex-wrap items-center justify-end gap-2 border-t px-4 py-3 sm:px-6'

type EditorSection = {
  id: string
  label: string
  detail?: string
}

export function UpstreamEditorLayout(props: {
  sections: EditorSection[]
  reference?: ReactNode
  children: ReactNode
  enabled?: boolean
}) {
  const id = useId()
  const content = useRef<HTMLDivElement>(null)
  const [selected, setSelected] = useState(props.sections[0]?.id)

  if (props.enabled === false) return props.children

  return (
    <div className='flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden md:flex-row'>
      <aside className='bg-muted/30 flex shrink-0 flex-col border-b md:min-h-0 md:w-56 md:overflow-hidden md:border-r md:border-b-0 xl:w-64'>
        <p className='text-muted-foreground hidden px-4 pt-5 pb-2 text-xs font-medium md:block'>
          配置目录
        </p>
        <nav
          aria-label='配置目录'
          className='flex shrink-0 gap-1 overflow-x-auto p-2 md:flex-col md:overflow-x-visible md:px-3'
        >
          {props.sections.map((section, index) => (
            <Button
              key={section.id}
              type='button'
              variant={selected === section.id ? 'secondary' : 'ghost'}
              aria-current={selected === section.id ? 'location' : undefined}
              aria-controls={`${id}-content`}
              className='h-auto shrink-0 justify-start gap-3 px-3 py-2.5 text-left md:w-full'
              onClick={() => {
                const target = [
                  ...(content.current?.querySelectorAll<HTMLElement>(
                    '[data-editor-section]'
                  ) ?? []),
                ].find(
                  (element) => element.dataset.editorSection === section.id
                )
                if (!target) return
                setSelected(section.id)
                target.scrollIntoView({ block: 'start' })
                target.focus({ preventScroll: true })
              }}
            >
              <span
                className='text-muted-foreground font-mono text-xs'
                aria-hidden='true'
              >
                {String(index + 1).padStart(2, '0')}
              </span>
              <span className='flex min-w-0 flex-col gap-1'>
                <span>{section.label}</span>
                {section.detail ? (
                  <span className='text-muted-foreground hidden text-xs font-normal whitespace-normal md:block'>
                    {section.detail}
                  </span>
                ) : null}
              </span>
            </Button>
          ))}
        </nav>
        {props.reference ? (
          <div className='hidden min-h-0 overflow-y-auto overscroll-contain border-t px-4 py-4 md:block md:flex-1'>
            {props.reference}
          </div>
        ) : null}
      </aside>
      <div
        ref={content}
        id={`${id}-content`}
        role='region'
        aria-label='配置内容'
        tabIndex={0}
        onFocusCapture={(event) => {
          const section = (event.target as HTMLElement).closest<HTMLElement>(
            '[data-editor-section]'
          )
          if (section?.dataset.editorSection) {
            setSelected(section.dataset.editorSection)
          }
        }}
        className='min-h-0 min-w-0 flex-1 [scrollbar-gutter:stable] overflow-y-auto overscroll-contain p-4 outline-none sm:p-6'
      >
        <div className='mx-auto flex max-w-[100rem] flex-col gap-6'>
          {props.children}
        </div>
      </div>
    </div>
  )
}

export function UpstreamEditorSection(props: {
  id: string
  title: string
  description?: string
  children: ReactNode
  className?: string
  enabled?: boolean
}) {
  const titleId = useId()
  if (props.enabled === false) return props.children
  return (
    <section
      data-editor-section={props.id}
      aria-labelledby={titleId}
      tabIndex={-1}
      className={cn(
        'flex min-w-0 scroll-mt-4 flex-col gap-4 outline-none',
        props.className
      )}
    >
      <header className='flex flex-col gap-1 border-b pb-3'>
        <h3 id={titleId} className='text-base font-semibold'>
          {props.title}
        </h3>
        {props.description ? (
          <p className='text-muted-foreground text-sm'>{props.description}</p>
        ) : null}
      </header>
      {props.children}
    </section>
  )
}
