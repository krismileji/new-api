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
  Add01Icon,
  Activity01Icon,
  Alert02Icon,
  ArrowDown01Icon,
  ArrowUp01Icon,
  Delete02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { nanoid } from 'nanoid'
import { useFieldArray, useFormContext } from 'react-hook-form'

import {
  SideDrawerSection,
  SideDrawerSectionHeader,
} from '@/components/drawer-layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import type { ChannelGroupMonitorConfigFormValues } from '@/features/group-monitor/lib/config-schema'
import { orderGroupNames } from '@/lib/group-order'

export function ChannelGroupMonitorCategoryEditor(props: {
  disabled: boolean
  candidateModelsByGroup: Record<string, string[]>
  groupOrder: readonly string[]
}) {
  const form = useFormContext<ChannelGroupMonitorConfigFormValues>()
  const categories = useFieldArray({
    control: form.control,
    name: 'categories',
  })
  const groups = useFieldArray({ control: form.control, name: 'groups' })
  const categoryValues = form.watch('categories')
  const groupValues = form.watch('groups')
  const selectedGroups = new Set(groupValues.map((group) => group.groupName))
  const availableGroupNames = orderGroupNames(
    Object.keys(props.candidateModelsByGroup),
    props.groupOrder
  ).filter((name) => !selectedGroups.has(name))
  const categoryOptions = categoryValues
    .filter((category) => category.name.trim())
    .map((category) => ({
      value: category.categoryId,
      label: category.name.trim(),
    }))

  function moveGroup(index: number, direction: -1 | 1) {
    const categoryId = groupValues[index].categoryId
    const indices = groupValues.flatMap((group, groupIndex) =>
      group.categoryId === categoryId ? [groupIndex] : []
    )
    const target = indices[indices.indexOf(index) + direction]
    if (target !== undefined) groups.swap(index, target)
  }

  return (
    <SideDrawerSection>
      <SideDrawerSectionHeader
        title='分类与分组'
        description='先创建分类，再在分类下添加监控分组。分类和分组均按列表顺序展示。'
        icon={<HugeiconsIcon icon={Activity01Icon} />}
        iconTone='info'
      />
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <Badge variant='outline'>
          {categories.fields.length} 个分类 · {groups.fields.length} / 100
          个分组
        </Badge>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={props.disabled || categories.fields.length >= 100}
          onClick={() =>
            categories.append({ categoryId: `new:${nanoid()}`, name: '' })
          }
        >
          <HugeiconsIcon icon={Add01Icon} data-icon='inline-start' />
          添加分类
        </Button>
      </div>
      {categories.fields.length === 0 ? (
        <Alert>
          <HugeiconsIcon icon={Alert02Icon} />
          <AlertTitle>尚未创建分类</AlertTitle>
          <AlertDescription>
            先添加分类并填写名称，再添加需要监控的分组。
          </AlertDescription>
        </Alert>
      ) : null}
      <div className='flex min-w-0 flex-col gap-4'>
        {categories.fields.map((category, categoryIndex) => {
          const categoryName = categoryValues[categoryIndex]?.name.trim() || ''
          const categoryGroups = groups.fields.flatMap((group, index) =>
            groupValues[index]?.categoryId === category.categoryId
              ? [{ group, index }]
              : []
          )
          return (
            <section
              key={category.id}
              aria-label={`分类 ${categoryName || '新分类'}`}
              className='min-w-0 rounded-xl border border-border/70 p-3 sm:p-4'
            >
              <div className='flex min-w-0 flex-wrap items-start gap-3'>
                <FormField
                  control={form.control}
                  name={`categories.${categoryIndex}.name`}
                  render={({ field }) => (
                    <FormItem className='min-w-0 flex-1 basis-40'>
                      <FormLabel>分类名称</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          placeholder='例如：通用模型、编程模型'
                          disabled={props.disabled}
                          aria-label={`第 ${categoryIndex + 1} 个分类名称`}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <div className='flex shrink-0 items-center gap-1 pt-6'>
                  <Button
                    type='button'
                    variant='outline'
                    size='icon-sm'
                    disabled={props.disabled || categoryIndex === 0}
                    onClick={() =>
                      categories.move(categoryIndex, categoryIndex - 1)
                    }
                    aria-label={`上移分类 ${categoryName || '新分类'}`}
                  >
                    <HugeiconsIcon icon={ArrowUp01Icon} />
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='icon-sm'
                    disabled={
                      props.disabled ||
                      categoryIndex === categories.fields.length - 1
                    }
                    onClick={() =>
                      categories.move(categoryIndex, categoryIndex + 1)
                    }
                    aria-label={`下移分类 ${categoryName || '新分类'}`}
                  >
                    <HugeiconsIcon icon={ArrowDown01Icon} />
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='icon-sm'
                    disabled={props.disabled || categoryGroups.length > 0}
                    title={
                      categoryGroups.length > 0
                        ? '请先移动或移除分类下的分组'
                        : '删除分类'
                    }
                    onClick={() => categories.remove(categoryIndex)}
                    aria-label={`删除分类 ${categoryName || '新分类'}`}
                  >
                    <HugeiconsIcon icon={Delete02Icon} />
                  </Button>
                </div>
              </div>
              <div className='my-3 flex flex-wrap items-center justify-between gap-2'>
                <span className='text-muted-foreground text-xs'>
                  {categoryGroups.length} 个监控分组
                </span>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={
                    props.disabled ||
                    !categoryName ||
                    availableGroupNames.length === 0 ||
                    groups.fields.length >= 100
                  }
                  aria-label={`在 ${categoryName || '新分类'} 中添加分组`}
                  onClick={() => {
                    const groupName = availableGroupNames[0]
                    if (!groupName) return
                    groups.append({
                      groupName,
                      enabled: true,
                      probeModel:
                        props.candidateModelsByGroup[groupName]?.[0] ?? '',
                      displayInitial: '',
                      categoryId: category.categoryId,
                    })
                  }}
                >
                  <HugeiconsIcon icon={Add01Icon} data-icon='inline-start' />
                  添加分组
                </Button>
              </div>
              {categoryGroups.length === 0 ? (
                <p className='text-muted-foreground rounded-lg border border-dashed p-4 text-sm'>
                  此分类暂无分组。
                </p>
              ) : null}
              <div className='flex min-w-0 flex-col gap-3'>
                {categoryGroups.map(({ group, index }) => {
                  const currentGroupName = groupValues[index]?.groupName ?? ''
                  const groupItems = orderGroupNames(
                    [currentGroupName, ...availableGroupNames].filter(Boolean),
                    props.groupOrder
                  ).map((name) => ({ value: name, label: name }))
                  const availableModelNames =
                    props.candidateModelsByGroup[currentGroupName] ?? []
                  const configuredProbeModel =
                    groupValues[index]?.probeModel?.trim() ?? ''
                  const modelNames =
                    configuredProbeModel &&
                    !availableModelNames.includes(configuredProbeModel)
                      ? [...availableModelNames, configuredProbeModel]
                      : availableModelNames
                  const modelItems = modelNames.map((modelName) => ({
                    value: modelName,
                    label: modelName,
                  }))
                  return (
                    <article
                      key={group.id}
                      className='border-border/60 bg-muted/10 grid min-w-0 gap-3 rounded-lg border p-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_5rem_auto]'
                    >
                      <FormField
                        control={form.control}
                        name={`groups.${index}.enabled`}
                        render={({ field }) => (
                          <FormItem className='flex items-center justify-between gap-3 sm:col-span-full'>
                            <div className='min-w-0 space-y-1'>
                              <FormLabel>
                                <span aria-hidden='true'>启用监控</span>
                                <span className='sr-only'>
                                  {`启用 ${currentGroupName || '当前分组'} 的监控`}
                                </span>
                              </FormLabel>
                              <FormDescription>
                                {field.value
                                  ? '关闭后保留配置和历史，暂停此分组的定时和手动探测'
                                  : '已暂停，重新启用后恢复探测'}
                              </FormDescription>
                            </div>
                            <FormControl>
                              <Switch
                                checked={field.value}
                                onCheckedChange={field.onChange}
                                disabled={props.disabled}
                                aria-label={`启用 ${currentGroupName || '当前分组'} 的监控`}
                              />
                            </FormControl>
                          </FormItem>
                        )}
                      />
                      <FormField
                        control={form.control}
                        name={`groups.${index}.groupName`}
                        render={({ field }) => (
                          <FormItem className='min-w-0'>
                            <FormLabel>分组</FormLabel>
                            <Select
                              items={groupItems}
                              value={field.value || null}
                              disabled={props.disabled}
                              onValueChange={(value) => {
                                if (value == null) return
                                field.onChange(value)
                                form.setValue(
                                  `groups.${index}.probeModel`,
                                  props.candidateModelsByGroup[value]?.[0] ??
                                    '',
                                  { shouldDirty: true, shouldValidate: true }
                                )
                              }}
                            >
                              <FormControl>
                                <SelectTrigger
                                  className='w-full min-w-0'
                                  aria-label={`第 ${index + 1} 个监控分组`}
                                >
                                  <SelectValue placeholder='选择分组' />
                                </SelectTrigger>
                              </FormControl>
                              <SelectContent alignItemWithTrigger={false}>
                                <SelectGroup>
                                  {groupItems.map((item) => (
                                    <SelectItem
                                      key={item.value}
                                      value={item.value}
                                    >
                                      {item.label}
                                    </SelectItem>
                                  ))}
                                </SelectGroup>
                              </SelectContent>
                            </Select>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <FormField
                        control={form.control}
                        name={`groups.${index}.probeModel`}
                        render={({ field }) => (
                          <FormItem className='min-w-0'>
                            <FormLabel>探测模型</FormLabel>
                            <Select
                              items={modelItems}
                              value={field.value || null}
                              disabled={
                                props.disabled || modelItems.length === 0
                              }
                              onValueChange={(value) => {
                                if (value !== null) field.onChange(value)
                              }}
                            >
                              <FormControl>
                                <SelectTrigger
                                  className='w-full min-w-0'
                                  aria-label={`${currentGroupName || '当前分组'}的探测模型`}
                                >
                                  <SelectValue placeholder='选择具体模型' />
                                </SelectTrigger>
                              </FormControl>
                              <SelectContent alignItemWithTrigger={false}>
                                <SelectGroup>
                                  {modelItems.map((item) => (
                                    <SelectItem
                                      key={item.value}
                                      value={item.value}
                                    >
                                      {item.label}
                                    </SelectItem>
                                  ))}
                                </SelectGroup>
                              </SelectContent>
                            </Select>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <FormField
                        control={form.control}
                        name={`groups.${index}.displayInitial`}
                        render={({ field }) => (
                          <FormItem className='min-w-0'>
                            <FormLabel>展示字</FormLabel>
                            <FormControl>
                              <Input
                                value={field.value}
                                maxLength={2}
                                placeholder='默认'
                                disabled={props.disabled}
                                aria-label={`${currentGroupName || '当前分组'}的展示字`}
                                onChange={(event) => {
                                  const value = event.target.value.trim()
                                  field.onChange(
                                    [...value].slice(0, 1).join('')
                                  )
                                }}
                              />
                            </FormControl>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <div className='flex items-end justify-end gap-1'>
                        <Button
                          type='button'
                          variant='outline'
                          size='icon-sm'
                          onClick={() => moveGroup(index, -1)}
                          disabled={
                            props.disabled || index === categoryGroups[0].index
                          }
                          aria-label={`上移 ${currentGroupName}`}
                        >
                          <HugeiconsIcon icon={ArrowUp01Icon} />
                        </Button>
                        <Button
                          type='button'
                          variant='outline'
                          size='icon-sm'
                          onClick={() => moveGroup(index, 1)}
                          disabled={
                            props.disabled ||
                            index === categoryGroups.at(-1)?.index
                          }
                          aria-label={`下移 ${currentGroupName}`}
                        >
                          <HugeiconsIcon icon={ArrowDown01Icon} />
                        </Button>
                        <Button
                          type='button'
                          variant='outline'
                          size='icon-sm'
                          onClick={() => groups.remove(index)}
                          disabled={props.disabled}
                          aria-label={`移除 ${currentGroupName}`}
                        >
                          <HugeiconsIcon icon={Delete02Icon} />
                        </Button>
                      </div>
                      <FormField
                        control={form.control}
                        name={`groups.${index}.categoryId`}
                        render={({ field }) => (
                          <FormItem className='min-w-0 sm:col-span-full'>
                            <FormLabel>移动到分类</FormLabel>
                            <Select
                              items={categoryOptions}
                              value={field.value}
                              disabled={props.disabled}
                              onValueChange={(value) => {
                                if (value !== null) field.onChange(value)
                              }}
                            >
                              <FormControl>
                                <SelectTrigger
                                  className='w-full min-w-0'
                                  aria-label={`移动 ${currentGroupName} 到分类`}
                                >
                                  <SelectValue placeholder='选择分类' />
                                </SelectTrigger>
                              </FormControl>
                              <SelectContent alignItemWithTrigger={false}>
                                <SelectGroup>
                                  {categoryOptions.map((option) => (
                                    <SelectItem
                                      key={option.value}
                                      value={option.value}
                                    >
                                      {option.label}
                                    </SelectItem>
                                  ))}
                                </SelectGroup>
                              </SelectContent>
                            </Select>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                    </article>
                  )
                })}
              </div>
            </section>
          )
        })}
      </div>
      <FormField
        control={form.control}
        name='categories'
        render={() => (
          <FormItem>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name='groups'
        render={() => (
          <FormItem>
            <FormMessage />
          </FormItem>
        )}
      />
    </SideDrawerSection>
  )
}
