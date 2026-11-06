import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it } from 'vitest'
import App from '@/App'
import { ThemeProvider } from '@/ThemeProvider'
import { mockThemeEnvironment, resetThemeTestState } from '@/test/theme'

beforeEach(() => {
  mockThemeEnvironment()
})

afterEach(resetThemeTestState)

it('renders the verification screen and switches themes through its control', async () => {
  const user = userEvent.setup()
  render(
    <ThemeProvider>
      <App />
    </ThemeProvider>,
  )

  expect(screen.getByRole('heading', { name: 'PulseGrid', level: 1 })).toBeInTheDocument()
  const examples = within(screen.getByRole('region', { name: 'Status colours' }))
  expect(examples.getByText(/^Up/)).toBeInTheDocument()
  expect(examples.getByText('Down')).toBeInTheDocument()
  expect(examples.getByText('Unknown')).toBeInTheDocument()
  expect(document.documentElement).toHaveClass('light')

  await user.click(screen.getByRole('button', { name: 'Switch to dark' }))

  expect(document.documentElement).toHaveClass('dark')
  expect(screen.getByText('Current: dark')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Switch to light' })).toBeInTheDocument()
})
