import { createFileRoute } from '@tanstack/react-router'
import { PlaceholderPage } from '@/components/shell/PlaceholderPage'

export const Route = createFileRoute('/_app/dashboard')({
  component: () => <PlaceholderPage title="Dashboard" phase={5} />,
})
