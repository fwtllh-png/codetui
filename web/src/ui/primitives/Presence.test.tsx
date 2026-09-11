import {act, cleanup, fireEvent, render, screen} from "@testing-library/react";
import {afterEach, beforeEach, expect, it, vi} from "vitest";
import {lazy, Suspense, useRef, useState} from "react";
import {Presence, usePresenceActive} from "./Presence";
import {motionDuration} from "./motion";
import {useModalFocus} from "./useModalFocus";

let reduced = false;
let media: EventTarget;

beforeEach(() => {
  vi.useFakeTimers();
  reduced = false;
  media = new EventTarget();
  Object.defineProperty(media, "matches", {get: () => reduced, configurable: true});
  vi.stubGlobal("matchMedia", () => media);
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) =>
    window.setTimeout(() => callback(0), 0));
  vi.stubGlobal("cancelAnimationFrame", (id: number) => clearTimeout(id));
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function Surface() {
  return <button data-motion-surface>{usePresenceActive() ? "Active surface" : "Closing surface"}</button>;
}

function Example({open}: {open: boolean}) {
  return <>
    <style>{".uiPresence { --ch-motion-exit: 0.18s; }"}</style>
    <Presence open={open}>{open && <Surface />}</Presence>
  </>;
}

it("retains the last content only during exit and disables it immediately", () => {
  const view = render(<Example open />);
  act(() => vi.advanceTimersByTime(0));
  expect(screen.getByRole("button").closest(".uiPresence")?.getAttribute("data-presence")).toBe("open");
  view.rerender(<Example open={false} />);
  expect(screen.queryByRole("button")).toBeNull();
  expect(screen.getByText("Closing surface").closest("[inert]")).not.toBeNull();
  act(() => vi.advanceTimersByTime(179));
  expect(screen.getByText("Closing surface")).toBeTruthy();
  act(() => vi.advanceTimersByTime(1));
  expect(view.container.querySelector(".uiPresence")).toBeNull();
});

it("cancels a pending exit when reopened without remounting the surface", () => {
  const view = render(<Example open />);
  act(() => vi.advanceTimersByTime(0));
  const button = screen.getByRole("button");
  view.rerender(<Example open={false} />);
  act(() => vi.advanceTimersByTime(90));
  view.rerender(<Example open />);
  act(() => vi.advanceTimersByTime(250));
  expect(screen.getByRole("button")).toBe(button);
  expect(button.closest("[inert]")).toBeNull();
  expect(vi.getTimerCount()).toBe(0);
});

it("finishes an exit immediately when reduced motion is enabled mid-transition", () => {
  const view = render(<Example open />);
  view.rerender(<Example open={false} />);
  act(() => {
    reduced = true;
    media.dispatchEvent(new Event("change"));
  });
  expect(view.container.querySelector(".uiPresence")).toBeNull();
  expect(vi.getTimerCount()).toBe(0);
});

it("settles hidden-page transitions and releases timers on unmount", () => {
  const view = render(<Example open />);
  view.rerender(<Example open={false} />);
  act(() => {
    vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
    document.dispatchEvent(new Event("visibilitychange"));
  });
  expect(view.container.querySelector(".uiPresence")).toBeNull();
  view.unmount();
  expect(vi.getTimerCount()).toBe(0);
});

it("starts a lazy surface only after its content arrives", async () => {
  let resolve!: (module: {default: typeof Surface}) => void;
  const LazySurface = lazy(() => new Promise<{default: typeof Surface}>((ready) => { resolve = ready; }));
  const view = render(<Presence open><Suspense fallback={null}><LazySurface /></Suspense></Presence>);
  expect(view.container.querySelector(".uiPresence")?.getAttribute("data-presence")).toBe("closed");
  await act(async () => { resolve({default: Surface}); });
  act(() => vi.advanceTimersByTime(0));
  expect(screen.getByRole("button").closest(".uiPresence")?.getAttribute("data-presence")).toBe("open");
});

it("shares environment subscriptions and cancels outstanding frames on unmount", () => {
  const add = vi.spyOn(media, "addEventListener");
  const remove = vi.spyOn(media, "removeEventListener");
  const view = render(<><Example open /><Example open /></>);
  expect(add).toHaveBeenCalledTimes(1);
  view.unmount();
  expect(remove).toHaveBeenCalledTimes(1);
  expect(vi.getTimerCount()).toBe(0);
});

it("uses CSS time units and fails to immediate removal for invalid timing", () => {
  const node = document.createElement("div");
  document.body.append(node);
  node.style.setProperty("--ch-motion-exit", "180ms");
  expect(motionDuration(node, "--ch-motion-exit")).toBe(180);
  node.style.setProperty("--ch-motion-exit", "0.18s");
  expect(motionDuration(node, "--ch-motion-exit")).toBe(180);
  node.style.setProperty("--ch-motion-exit", ".18s");
  expect(motionDuration(node, "--ch-motion-exit")).toBe(180);
  node.style.setProperty("--ch-motion-exit", "1.8e2MS");
  expect(motionDuration(node, "--ch-motion-exit")).toBe(180);
  node.style.setProperty("--ch-motion-exit", "invalid");
  expect(motionDuration(node, "--ch-motion-exit")).toBe(0);
  node.remove();
});

it("restores modal focus when closing begins, before the exit DOM disappears", () => {
  function Dialog({close}: {close: () => void}) {
    const ref = useRef<HTMLDivElement>(null);
    useModalFocus(ref, true, close);
    return <div ref={ref} role="dialog" aria-modal="true">
      <input aria-label="Draft" autoFocus />
    </div>;
  }
  function App() {
    const [open, setOpen] = useState(false);
    return <div className="app">
      <style>{".uiPresence { --ch-motion-exit: 180ms; }"}</style>
      <main><button onClick={() => setOpen(true)}>Open</button></main>
      <Presence open={open}><Dialog close={() => setOpen(false)} /></Presence>
    </div>;
  }
  const view = render(<App />);
  const trigger = screen.getByRole("button", {name: "Open"});
  trigger.focus();
  fireEvent.click(trigger);
  expect(document.activeElement).toBe(screen.getByLabelText("Draft"));
  fireEvent.keyDown(screen.getByLabelText("Draft"), {key: "Escape"});
  expect(document.activeElement).toBe(trigger);
  expect(view.container.querySelector("[data-exiting]")).not.toBeNull();
  expect(screen.queryByRole("dialog")).toBeNull();
  act(() => vi.advanceTimersByTime(180));
  expect(view.container.querySelector("[data-exiting]")).toBeNull();
});
