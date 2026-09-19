import { createFileRoute } from '@tanstack/react-router'
import { PlaceholderPage } from '@/components/shell/PlaceholderPage'

export const Route = createFileRoute('/_app/pos')({
  component: () => <PlaceholderPage title="Till" phase={4} />,
})
