import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { makeService } from '@/test/services'
import { SearchDialog } from './SearchDialog'

it('matches trimmed names and category labels, orders deterministically and caps results', async () => {
  const user = userEvent.setup()
  const onSelect = vi.fn()
  const services = Array.from({ length: 12 }, (_, i) => ({ ...makeService(`id-${i}`, 'up'), name: `Example ${12 - i}` }))
  render(<SearchDialog services={services} onSelect={onSelect} />)
  const input = screen.getByRole('combobox', { name: 'Search services' })
  expect(screen.getByRole('status')).toHaveTextContent('Search by service name or category.')
  await user.type(input, '  EXAMPLE  ')
  expect(screen.getAllByRole('option')).toHaveLength(8)
  expect(screen.getAllByRole('option')[0]).toHaveTextContent('Example 1')
  expect(screen.getByRole('status')).toHaveTextContent('Showing 8 of 12 matches. Refine your search.')
  await user.clear(input)
  await user.type(input, 'AI')
  expect(screen.getAllByRole('option')).toHaveLength(8)
  await user.clear(input)
  await user.type(input, 'not found')
  expect(screen.queryByRole('option')).not.toBeInTheDocument()
  expect(screen.getByRole('status')).toHaveTextContent('No services match your search.')
  await user.keyboard('{Enter}')
  expect(onSelect).not.toHaveBeenCalled()
})

it('keeps input focus during arrow navigation and selects by stable ID', async () => {
  const user = userEvent.setup()
  const onSelect = vi.fn()
  render(<SearchDialog services={[{ ...makeService('b', 'down'), name: 'Beta' }, { ...makeService('a', 'unknown'), name: 'Alpha' }]} onSelect={onSelect} />)
  const input = screen.getByRole('combobox')
  await user.type(input, 'a')
  await user.keyboard('{ArrowDown}')
  const beta = screen.getByRole('option', { name: /Beta/ })
  expect(beta).toHaveAttribute('aria-selected', 'true')
  expect(input).toHaveAttribute('aria-activedescendant', beta.id)
  expect(input).toHaveFocus()
  await user.keyboard('{Enter}')
  expect(onSelect).toHaveBeenCalledWith('b')
  await user.click(screen.getByRole('option', { name: /Alpha/ }))
  expect(onSelect).toHaveBeenLastCalledWith('a')
  expect(input).toHaveFocus()
})

it('handles a disappearing active result and avoids Enter selection during composition', async () => {
  const user = userEvent.setup()
  const onSelect = vi.fn()
  const a = { ...makeService('a', 'up'), name: 'Alpha' }
  const b = { ...makeService('b', 'up'), name: 'Beta' }
  const { rerender } = render(<SearchDialog services={[a, b]} onSelect={onSelect} />)
  const input = screen.getByRole('combobox')
  await user.type(input, 'a')
  await user.keyboard('{ArrowUp}')
  rerender(<SearchDialog services={[a]} onSelect={onSelect} />)
  expect(screen.getByRole('option')).toHaveAttribute('aria-selected', 'true')
  fireEvent.keyDown(input, { key: 'Enter', isComposing: true })
  expect(onSelect).not.toHaveBeenCalled()
  await user.keyboard('{Enter}')
  expect(onSelect).toHaveBeenCalledWith('a')
})
