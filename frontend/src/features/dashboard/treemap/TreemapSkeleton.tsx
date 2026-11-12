export function TreemapSkeleton() {
  return (
    <div role="status" aria-label="Loading service statuses" className="absolute inset-0 p-2">
      <span className="sr-only">Loading service statuses…</span>
      {/* Abstract loading composition; these rectangles represent no services. */}
      <div aria-hidden="true" className="grid size-full grid-cols-[1.5fr_1fr_1fr] grid-rows-3 gap-1.5 motion-safe:animate-pulse">
        {['row-span-2', 'col-span-2', '', '', 'col-span-2', ''].map((span, index) => (
          <div key={index} className={`min-h-0 min-w-0 rounded-sm border bg-muted ${span}`} />
        ))}
      </div>
    </div>
  )
}
