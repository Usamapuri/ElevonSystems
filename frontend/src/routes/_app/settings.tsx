import { createFileRoute } from '@tanstack/react-router'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { PageLoading } from '@/components/ui/loading-spinner'
import { BusinessForm } from '@/components/settings/BusinessForm'
import { CreditForm } from '@/components/settings/CreditForm'
import { DayCloseForm } from '@/components/settings/DayCloseForm'
import { ReceiptForm } from '@/components/settings/ReceiptForm'
import { TaxForm } from '@/components/settings/TaxForm'
import { UsersPanel } from '@/components/settings/UsersPanel'
import { useSettings } from '@/components/settings/useSettings'

export const Route = createFileRoute('/_app/settings')({ component: SettingsPage })

const TABS = [
  { value: 'business', label: 'Business' },
  { value: 'tax', label: 'Tax' },
  { value: 'receipt', label: 'Receipt' },
  { value: 'day-close', label: 'Day close' },
  { value: 'credit', label: 'Credit' },
  { value: 'users', label: 'Users' },
] as const

function SettingsPage() {
  const { data: settings, isLoading, error } = useSettings()

  return (
    <div className="space-y-4 p-4 md:p-6">
      <h1 className="text-2xl font-semibold">Settings</h1>
      <Tabs defaultValue="business" className="space-y-4">
        <TabsList className="flex h-auto flex-wrap justify-start">
          {TABS.map((t) => (
            <TabsTrigger key={t.value} value={t.value}>
              {t.label}
            </TabsTrigger>
          ))}
        </TabsList>
        {isLoading && <PageLoading />}
        {error && <p className="text-sm text-red-600">{error instanceof Error ? error.message : 'Could not load settings'}</p>}
        {settings && (
          <>
            <TabsContent value="business"><BusinessForm settings={settings} /></TabsContent>
            <TabsContent value="tax"><TaxForm settings={settings} /></TabsContent>
            <TabsContent value="receipt"><ReceiptForm settings={settings} /></TabsContent>
            <TabsContent value="day-close"><DayCloseForm settings={settings} /></TabsContent>
            <TabsContent value="credit"><CreditForm settings={settings} /></TabsContent>
          </>
        )}
        <TabsContent value="users">
          <UsersPanel />
        </TabsContent>
      </Tabs>
    </div>
  )
}
