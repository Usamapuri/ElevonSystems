import { createFileRoute } from '@tanstack/react-router'
import { PlaceholderPage } from '@/components/shell/PlaceholderPage'

export const Route = createFileRoute('/_app/settings')({
  component: () => <PlaceholderPage title="Settings" phase={1} />,
})
