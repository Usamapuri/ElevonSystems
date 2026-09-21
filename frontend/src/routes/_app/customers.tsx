import { createFileRoute } from '@tanstack/react-router'
import { PageHeader } from '@/components/shell/PageHeader'
import { CustomersTable } from '@/components/customers/CustomersTable'

export const Route = createFileRoute('/_app/customers')({ component: CustomersPage })

function CustomersPage() {
  const { user } = Route.useRouteContext()
  return (
    <div className="space-y-4 p-4 md:p-6">
      <PageHeader title="Customers" description="Who buys on account, what they owe and what they have paid." />
      <CustomersTable role={user.role} />
    </div>
  )
}
