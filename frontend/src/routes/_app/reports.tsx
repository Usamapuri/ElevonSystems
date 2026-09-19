import { createFileRoute } from '@tanstack/react-router'
import { PlaceholderPage } from '@/components/shell/PlaceholderPage'

export const Route = createFileRoute('/_app/reports')({
  component: () => <PlaceholderPage title="Reports" phase={5} />,
})
