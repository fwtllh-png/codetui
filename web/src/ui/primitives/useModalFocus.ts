import {useEffect, useRef, type RefObject} from "react";
import {usePresenceActive} from "./Presence";

const inertOwners = new WeakMap<HTMLElement, {count: number; previous: boolean}>();

function topModal() {
  return Array.from(document.querySelectorAll<HTMLElement>('[aria-modal="true"]'))
    .filter((node) => !node.closest('[inert], [aria-hidden="true"]')).at(-1);
}

// Shared by modal dialogs and responsive drawers; nested dialogs own keyboard focus.
export function useModalFocus(
  ref: RefObject<HTMLElement>,
  enabled: boolean,
  onClose: () => void
) {
  const active = usePresenceActive();
  enabled = enabled && active;
  const close = useRef(onClose);
  close.current = onClose;
  const invoker = useRef<HTMLElement>();
  const wasEnabled = useRef(false);
  if (enabled && !wasEnabled.current) {
    invoker.current = document.activeElement instanceof HTMLElement ? document.activeElement : undefined;
  }
  wasEnabled.current = enabled;
  useEffect(() => {
    const node = ref.current;
    if (!enabled || !node) return;
    const previous = invoker.current;
    const root = node.closest(".app");
    const background = root ? Array.from(root.children).filter(
      (child): child is HTMLElement => child instanceof HTMLElement &&
        !child.contains(node) && !child.matches("[data-modal-backdrop]")
    ) : [];
    background.forEach((element) => {
      const owner = inertOwners.get(element) ?? {count: 0, previous: Boolean(element.inert)};
      owner.count++;
      inertOwners.set(element, owner);
      element.inert = true;
    });
    const controls = () => Array.from(node.querySelectorAll<HTMLElement>(
      'button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), a[href], [tabindex="0"]'
    )).filter((element) => !element.closest('[inert], [hidden], [aria-hidden="true"]') &&
      getComputedStyle(element).display !== "none" &&
      getComputedStyle(element).visibility !== "hidden");
    if (!node.contains(document.activeElement) && topModal() === node) {
      (controls()[0] ?? node).focus();
    }
    const keydown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || topModal() !== node) return;
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        close.current();
      } else if (event.key === "Tab") {
        const items = controls();
        const index = items.indexOf(document.activeElement as HTMLElement);
        const next = event.shiftKey
          ? items[(index <= 0 ? items.length : index) - 1]
          : items[(index + 1) % items.length];
        event.preventDefault();
        (next ?? node).focus();
      }
    };
    document.addEventListener("keydown", keydown);
    return () => {
      document.removeEventListener("keydown", keydown);
      background.forEach((element) => {
        const owner = inertOwners.get(element);
        if (owner && --owner.count === 0) {
          element.inert = owner.previous;
          inertOwners.delete(element);
        }
      });
      if (previous?.isConnected && !previous.closest("[inert]")) previous.focus({preventScroll: true});
      else topModal()
        ?.querySelector<HTMLElement>('button:not(:disabled), input:not(:disabled)')?.focus();
    };
  }, [enabled, ref]);
}
