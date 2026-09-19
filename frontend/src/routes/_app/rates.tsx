import { createFileRoute } from '@tanstack/react-router'
import { PlaceholderPage } from '@/components/shell/PlaceholderPage'

export const Route = createFileRoute('/_app/rates')({
  component: () => <PlaceholderPage title="Rates" phase={2} />,
})
