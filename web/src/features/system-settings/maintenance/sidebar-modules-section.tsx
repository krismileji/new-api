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
import { Add01Icon, Delete02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useEffect, useMemo } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormLabel,
} from '@/components/ui/form'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from '@/components/ui/input-group'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

import {
  SettingsControlChildren,
  SettingsForm,
  SettingsSwitchContent,
  SettingsControlGroup,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import {
  SIDEBAR_MODULES_DEFAULT,
  getSidebarShopEntries,
  type SidebarModulesAdminConfig,
  serializeSidebarModulesAdmin,
} from './config'

type SidebarModulesSectionProps = {
  config: SidebarModulesAdminConfig
  initialSerialized: string
}

type SidebarFormValues = SidebarModulesAdminConfig

const toTitleCase = (value: string) =>
  value
    .replaceAll(/[_-]+/g, ' ')
    .replaceAll(/\b\w/g, (char) => char.toUpperCase())

const getShopUrlInputs = (primary: unknown, legacy: unknown): string[] => {
  const normalize = (value: unknown): string[] => {
    if (Array.isArray(value)) {
      return value.filter((item): item is string => typeof item === 'string')
    }
    return typeof value === 'string' && value.trim() !== '' ? [value] : []
  }

  if (Array.isArray(primary)) return normalize(primary)

  const primaryInputs = normalize(primary)
  if (primaryInputs.length > 0) return primaryInputs

  const legacyInputs = normalize(legacy)
  return legacyInputs.length > 0 ? legacyInputs : ['']
}

const getShopNameInputs = (value: unknown, count: number): string[] => {
  let names: string[] = []
  if (Array.isArray(value)) {
    names = value.filter((item): item is string => typeof item === 'string')
  } else if (typeof value === 'string') {
    names = [value]
  }

  return Array.from({ length: count }, (_, index) => names[index] ?? '')
}

export function SidebarModulesSection({
  config,
  initialSerialized,
}: SidebarModulesSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const sectionMeta: Record<string, { title: string; description: string }> = {
    chat: {
      title: t('Chat area'),
      description: t('Playground experiments and live conversations.'),
    },
    console: {
      title: t('Console area'),
      description: t('Dashboards, tokens, and usage analytics.'),
    },
    personal: {
      title: t('Personal area'),
      description: t('Wallet management and personal preferences.'),
    },
    admin: {
      title: t('Admin area'),
      description: t('Global configuration and administrative tools.'),
    },
  }

  const moduleMeta: Record<
    string,
    Record<string, { title: string; description: string }>
  > = {
    chat: {
      playground: {
        title: t('Playground'),
        description: t('Experiment with prompts and models in real time.'),
      },
      chat: {
        title: t('Chat'),
        description: t('Access previous conversations and start new ones.'),
      },
    },
    console: {
      detail: {
        title: t('Dashboard'),
        description: t('Aggregated usage metrics and trend charts.'),
      },
      token: {
        title: t('Token management'),
        description: t('Create, revoke, and audit API tokens.'),
      },
      log: {
        title: t('Usage logs'),
        description: t('Detailed request logs for investigations.'),
      },
      midjourney: {
        title: t('Drawing logs'),
        description: t('History of MjProxy-style image tasks.'),
      },
      task: {
        title: t('Task logs'),
        description: t('Background job tracker for queued work.'),
      },
    },
    personal: {
      topup: {
        title: t('Wallet'),
        description: t('Top up balance and view billing history.'),
      },
      personal: {
        title: t('Profile'),
        description: t('Personal settings and profile management.'),
      },
      shop: {
        title: '小铺充值',
        description: '在钱包下显示小铺充值入口。',
      },
    },
    admin: {
      channel: {
        title: t('Channels'),
        description: t('Configure upstream providers and routing.'),
      },
      models: {
        title: t('Models'),
        description: t('Manage catalog visibility and pricing.'),
      },
      redemption: {
        title: t('Redeem codes'),
        description: t('Create and review invite or credit codes.'),
      },
      user: {
        title: t('Users'),
        description: t('Administer user accounts and roles.'),
      },
      setting: {
        title: t('System settings'),
        description: t('Advanced platform configuration.'),
      },
      subscription: {
        title: t('Subscription Management'),
        description: t('Manage subscription plans and pricing.'),
      },
    },
  }
  const formDefaults = useMemo(() => config, [config])

  const form = useForm<SidebarFormValues>({
    defaultValues: formDefaults,
  })

  // The shop URL controls are dynamic record fields, so RHF cannot infer their
  // nested paths from SidebarModulesAdminConfig.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const watchedShopUrls = useWatch({
    control: form.control,
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    name: 'personal.shop_urls' as any,
  })
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const watchedShopUrl = useWatch({
    control: form.control,
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    name: 'personal.shop_url' as any,
  })
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const watchedShopNames = useWatch({
    control: form.control,
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    name: 'personal.shop_names' as any,
  })
  const shopUrlInputs = getShopUrlInputs(watchedShopUrls, watchedShopUrl)
  const shopNameInputs = getShopNameInputs(
    watchedShopNames,
    shopUrlInputs.length
  )
  const occurrences = new Map<string, number>()
  const shopUrlInputItems = shopUrlInputs.map((url, index) => {
    const baseKey = url.trim() || 'empty'
    const occurrence = occurrences.get(baseKey) ?? 0
    occurrences.set(baseKey, occurrence + 1)
    return {
      key: `${baseKey}-${occurrence}`,
      url,
      name: shopNameInputs[index] ?? '',
      index,
    }
  })

  const shopModuleEnabled = Boolean(
    form.watch('personal.enabled') && form.watch('personal.shop')
  )

  const setShopInputs = (urls: string[], names: string[]) => {
    // The dynamic section keys are represented by a record, so RHF cannot infer
    // these nested paths from SidebarModulesAdminConfig.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    form.setValue('personal.shop_urls' as any, urls, { shouldDirty: true })
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    form.setValue('personal.shop_url' as any, urls, { shouldDirty: true })
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    form.setValue('personal.shop_names' as any, names, { shouldDirty: true })
  }

  useEffect(() => {
    form.reset(formDefaults)
  }, [formDefaults, form])

  const onSubmit = async (values: SidebarFormValues) => {
    const personal = values.personal ?? { enabled: true }
    const { shop_urls: _, ...personalWithoutAlias } = personal
    const shopEntries = getSidebarShopEntries(personal).filter(
      (entry) => entry.url !== ''
    )
    const valuesToSave: SidebarFormValues = {
      ...values,
      personal: {
        ...personalWithoutAlias,
        shop_url: shopEntries.map((entry) => entry.url),
        shop_names: shopEntries.map((entry) => entry.name),
      },
    }
    const serialized = serializeSidebarModulesAdmin(valuesToSave)
    if (serialized === initialSerialized) {
      return
    }

    await updateOption.mutateAsync({
      key: 'SidebarModulesAdmin',
      value: serialized,
    })
  }

  const resetToDefault = () => {
    form.reset(SIDEBAR_MODULES_DEFAULT)
  }

  const sections = Object.entries(config)

  return (
    <SettingsSection title={t('Sidebar modules')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            onReset={resetToDefault}
            isSaving={updateOption.isPending}
            resetLabel='Reset to default'
            saveLabel='Save sidebar modules'
          />
          {sections.map(([sectionKey, sectionConfig]) => {
            const sectionInfo = sectionMeta[sectionKey] ?? {
              title: toTitleCase(sectionKey),
              description: t('Custom sidebar section'),
            }
            const modules = Object.entries(sectionConfig).filter(
              ([moduleKey, moduleValue]) =>
                moduleKey !== 'enabled' && typeof moduleValue === 'boolean'
            )

            return (
              <SettingsControlGroup key={sectionKey}>
                <FormField
                  control={form.control}
                  // eslint-disable-next-line @typescript-eslint/no-explicit-any
                  name={`${sectionKey}.enabled` as any}
                  render={({ field }) => (
                    <SettingsSwitchItem>
                      <SettingsSwitchContent>
                        <FormLabel>{sectionInfo.title}</FormLabel>
                        <FormDescription>
                          {sectionInfo.description}
                        </FormDescription>
                      </SettingsSwitchContent>
                      <FormControl>
                        <Switch
                          checked={Boolean(field.value)}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </SettingsSwitchItem>
                  )}
                />

                <SettingsControlChildren className='grid gap-3 md:grid-cols-2'>
                  {modules.map(([moduleKey]) => {
                    const moduleInfo = moduleMeta[sectionKey]?.[moduleKey] ?? {
                      title: toTitleCase(moduleKey),
                      description: t('Custom module'),
                    }
                    return (
                      <FormField
                        key={`${sectionKey}.${moduleKey}`}
                        control={form.control}
                        // eslint-disable-next-line @typescript-eslint/no-explicit-any
                        name={`${sectionKey}.${moduleKey}` as any}
                        render={({ field }) => (
                          <SettingsSwitchItem className='py-2'>
                            <SettingsSwitchContent>
                              <FormLabel>{moduleInfo.title}</FormLabel>
                              <FormDescription>
                                {moduleInfo.description}
                              </FormDescription>
                            </SettingsSwitchContent>
                            <FormControl>
                              <Switch
                                checked={Boolean(field.value)}
                                onCheckedChange={field.onChange}
                                disabled={
                                  // eslint-disable-next-line @typescript-eslint/no-explicit-any
                                  !form.watch(`${sectionKey}.enabled` as any)
                                }
                              />
                            </FormControl>
                          </SettingsSwitchItem>
                        )}
                      />
                    )
                  })}
                  {sectionKey === 'personal' && (
                    <div className='grid gap-2 md:col-span-2'>
                      <Label>小铺充值链接</Label>
                      <div className='space-y-2'>
                        <div className='text-muted-foreground hidden gap-2 px-1 text-xs sm:grid sm:grid-cols-[minmax(8rem,1fr)_minmax(0,2fr)]'>
                          <span>菜单名称（可选）</span>
                          <span>充值链接</span>
                        </div>
                        {shopUrlInputItems.map((item) => (
                          <div
                            key={`shop-url-${item.key}`}
                            className='grid gap-2 sm:grid-cols-[minmax(8rem,1fr)_minmax(0,2fr)]'
                          >
                            <InputGroup>
                              <InputGroupInput
                                type='text'
                                placeholder='例如：标准充值'
                                value={item.name}
                                disabled={!shopModuleEnabled}
                                aria-label={`小铺充值菜单名称 ${item.index + 1}`}
                                onChange={(event) => {
                                  const next = [...shopNameInputs]
                                  next[item.index] = event.target.value
                                  setShopInputs(shopUrlInputs, next)
                                }}
                              />
                            </InputGroup>
                            <InputGroup>
                              <InputGroupInput
                                type='url'
                                placeholder='https://example.com/shop'
                                value={item.url}
                                disabled={!shopModuleEnabled}
                                aria-label={`小铺充值链接 ${item.index + 1}`}
                                onChange={(event) => {
                                  const next = [...shopUrlInputs]
                                  next[item.index] = event.target.value
                                  setShopInputs(next, shopNameInputs)
                                }}
                              />
                              <InputGroupAddon align='inline-end'>
                                <InputGroupButton
                                  type='button'
                                  size='icon-sm'
                                  aria-label={`删除第 ${item.index + 1} 个小铺充值链接`}
                                  disabled={!shopModuleEnabled}
                                  onClick={() =>
                                    setShopInputs(
                                      shopUrlInputs.filter(
                                        (_, inputIndex) =>
                                          inputIndex !== item.index
                                      ),
                                      shopNameInputs.filter(
                                        (_, inputIndex) =>
                                          inputIndex !== item.index
                                      )
                                    )
                                  }
                                >
                                  <HugeiconsIcon icon={Delete02Icon} />
                                </InputGroupButton>
                              </InputGroupAddon>
                            </InputGroup>
                          </div>
                        ))}
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          disabled={!shopModuleEnabled}
                          onClick={() =>
                            setShopInputs(
                              [...shopUrlInputs, ''],
                              [...shopNameInputs, '']
                            )
                          }
                        >
                          <HugeiconsIcon
                            icon={Add01Icon}
                            data-icon='inline-start'
                          />
                          添加链接
                        </Button>
                      </div>
                      <p className='text-muted-foreground text-sm'>
                        小铺充值菜单会在站内以 iframe 方式打开配置的链接。
                      </p>
                    </div>
                  )}
                </SettingsControlChildren>
              </SettingsControlGroup>
            )
          })}
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
