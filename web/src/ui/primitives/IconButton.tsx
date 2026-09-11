import type {ReactNode} from "react";

export function IconButton({
  label, icon, onClick, disabled, primary, danger, expanded, controls
}: {
  label: string;
  icon: ReactNode;
  onClick: () => void;
  disabled?: boolean;
  primary?: boolean;
  danger?: boolean;
  expanded?: boolean;
  controls?: string;
}) {
  return (
    <button
      type="button"
      className="iconButton"
      data-primary={primary || undefined}
      data-danger={danger || undefined}
      aria-label={label}
      aria-expanded={expanded}
      aria-controls={controls}
      title={label}
      disabled={disabled}
      onClick={onClick}
    >{icon}</button>
  );
}
