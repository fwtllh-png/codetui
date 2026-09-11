import {useLayoutEffect, useState, useSyncExternalStore, type RefObject} from "react";

const listeners = new Set<() => void>();
let query: MediaQueryList | undefined;
let matchMedia: typeof window.matchMedia | undefined;

function motionQuery() {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") return;
  if (matchMedia !== window.matchMedia) {
    matchMedia = window.matchMedia;
    query = window.matchMedia("(prefers-reduced-motion: reduce)");
  }
  return query;
}

function notify() {
  listeners.forEach((listener) => listener());
}

function subscribe(listener: () => void) {
  const media = motionQuery();
  listeners.add(listener);
  if (listeners.size === 1) {
    media?.addEventListener("change", notify);
    document.addEventListener("visibilitychange", notify);
  }
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0) {
      media?.removeEventListener("change", notify);
      document.removeEventListener("visibilitychange", notify);
    }
  };
}

export function useMotionEnabled() {
  return useSyncExternalStore(subscribe, () =>
    motionQuery()?.matches === false && document.visibilityState !== "hidden",
  () => false);
}

export function motionDuration(node: HTMLElement, token: string): number {
  const value = getComputedStyle(node).getPropertyValue(token).trim();
  const match = /^((?:\d+\.?\d*|\.\d+)(?:e[+-]?\d+)?)(ms|s)$/i.exec(value);
  if (!match) return 0;
  const duration = Number(match[1]) * (match[2].toLowerCase() === "s" ? 1000 : 1);
  return Number.isFinite(duration) ? duration : 0;
}

// CSS owns timing. Only user-visible transitions retain DOM, never a closed history.
export function usePresenceState(
  open: boolean,
  ref: RefObject<HTMLElement>,
  exitToken = "--ch-motion-exit"
) {
  const motion = useMotionEnabled();
  const [mounted, setMounted] = useState(open);
  const [expanded, setExpanded] = useState(false);
  useLayoutEffect(() => {
    if (open) {
      setMounted(true);
      if (!motion) {
        setExpanded(true);
        return;
      }
      let frame: number | undefined;
      const enter = () => {
        ref.current?.firstElementChild?.getBoundingClientRect();
        frame = requestAnimationFrame(() => setExpanded(true));
      };
      const node = ref.current;
      let observer: MutationObserver | undefined;
      if (node && !node.firstElementChild) {
        // Lazy dialogs start their entrance when the surface arrives, not while suspended.
        observer = new MutationObserver(() => {
          if (!node.firstElementChild) return;
          observer?.disconnect();
          enter();
        });
        observer.observe(node, {childList: true});
      } else enter();
      return () => {
        observer?.disconnect();
        if (frame !== undefined) cancelAnimationFrame(frame);
      };
    }
    setExpanded(false);
    const duration = motion && ref.current ? motionDuration(ref.current, exitToken) : 0;
    if (!duration) {
      setMounted(false);
      return;
    }
    const timer = window.setTimeout(() => setMounted(false), duration);
    return () => clearTimeout(timer);
  }, [open, motion, ref, exitToken]);
  return {present: open || mounted, expanded: open && expanded};
}
