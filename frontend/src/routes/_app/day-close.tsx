import { createFileRoute } from '@tanstack/react-router'
import { PlaceholderPage } from '@/components/shell/PlaceholderPage'

export const Route = createFileRoute('/_app/day-close')({
  component: () => <PlaceholderPage title="Day close" phase={5} />,
})
