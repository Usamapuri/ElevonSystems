import { createFileRoute } from '@tanstack/react-router'
import { PlaceholderPage } from '@/components/shell/PlaceholderPage'

export const Route = createFileRoute('/_app/invoices')({
  component: () => <PlaceholderPage title="Invoices" phase={5} />,
})
