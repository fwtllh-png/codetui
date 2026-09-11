import {useMemo, useSyncExternalStore} from "react";

export function useMediaQuery(query: string): boolean {
  const media = useMemo(() => typeof window.matchMedia === "function"
    ? window.matchMedia(query) : undefined, [query]);
  return useSyncExternalStore(
    useMemo(() => (notify: () => void) => {
      media?.addEventListener?.("change", notify);
      return () => media?.removeEventListener?.("change", notify);
    }, [media]),
    () => media?.matches ?? false,
    () => false
  );
}
