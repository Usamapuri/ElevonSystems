import { createFileRoute } from '@tanstack/react-router'
import { PlaceholderPage } from '@/components/shell/PlaceholderPage'

export const Route = createFileRoute('/_app/customers')({
  component: () => <PlaceholderPage title="Customers" phase={5} />,
})
