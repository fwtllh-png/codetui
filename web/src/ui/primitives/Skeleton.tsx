export function Skeleton({label}: {label: string}) {
  return (
    <div className="uiSkeleton" role="status" aria-label={label}>
      <span aria-hidden="true" /><span aria-hidden="true" />
      <span aria-hidden="true" /><span aria-hidden="true" />
    </div>
  );
}
