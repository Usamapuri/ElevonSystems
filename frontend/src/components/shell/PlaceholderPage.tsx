import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

export function PlaceholderPage({ title, phase }: { title: string; phase: number }) {
  return (
    <div className="p-6">
      <Card>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
        </CardHeader>
        <CardContent className="text-muted-foreground">This screen arrives in Phase {phase}.</CardContent>
      </Card>
    </div>
  )
}
