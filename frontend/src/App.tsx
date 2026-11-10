import { DashboardShell } from '@/features/dashboard/DashboardShell'
import { incidentsResponseFixture, statusResponseFixture } from '@/data/fixtures'

function App() {
  return (
    <DashboardShell
      services={statusResponseFixture.services}
      incidents={incidentsResponseFixture}
    />
  )
}

export default App
