import {useRef, type ReactNode} from "react";
import {usePresenceState} from "./motion";

// Keep large transcripts mounted only for the exit transition, not while closed.
export function Collapse({open, children, id}: {
  open: boolean;
  children: ReactNode;
  id?: string;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const {present, expanded} = usePresenceState(open, ref, "--ch-motion-disclosure");
  if (!present) return null;
  return (
    <div
      ref={ref}
      id={id}
      className="uiCollapse"
      data-open={open && expanded || undefined}
      aria-hidden={!open || undefined}
      {...(!open ? {inert: ""} : {})}
    ><div>{children}</div></div>
  );
}
