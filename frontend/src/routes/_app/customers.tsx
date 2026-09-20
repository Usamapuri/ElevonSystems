import { createFileRoute } from '@tanstack/react-router'
import { CustomersTable } from '@/components/customers/CustomersTable'

export const Route = createFileRoute('/_app/customers')({ component: CustomersPage })

function CustomersPage() {
  const { user } = Route.useRouteContext()
  return (
    <div className="space-y-4 p-4 md:p-6">
      <h1 className="text-2xl font-semibold">Customers</h1>
      <CustomersTable role={user.role} />
    </div>
  )
}
