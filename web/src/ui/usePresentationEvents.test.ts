import {cleanup, renderHook} from "@testing-library/react";
import {afterEach, describe, expect, it} from "vitest";
import type {RuntimeEvent} from "../protocol";
import {usePresentationEvents} from "./usePresentationEvents";

afterEach(cleanup);

describe("presentation event cache", () => {
  it("keeps metadata identity for streaming frames and includes terminal events immediately", () => {
    const started = event(1, "turn.started");
    const events = [started];
    const view = renderHook(({events}) => usePresentationEvents(events), {initialProps: {events}});
    const before = view.result.current;
    view.rerender({events: [...events, event(2, "output.delta"), event(3, "reasoning.delta"), event(4, "tool.output")]});
    expect(view.result.current).toBe(before);
    const terminal = event(5, "turn.completed");
    view.rerender({events: [...events, event(2, "output.delta"), terminal]});
    expect(view.result.current).toEqual([started, terminal]);
  });

  it("rebuilds when earlier history is prepended", () => {
    const current = [event(10, "turn.started"), event(11, "output.delta")];
    const view = renderHook(({events}) => usePresentationEvents(events), {initialProps: {events: current}});
    const earlier = event(2, "turn.completed");
    view.rerender({events: [event(1, "output.delta"), earlier, ...current]});
    expect(view.result.current).toEqual([earlier, current[0]]);
  });

  it("drops previous session metadata on reset and snapshot replacement", () => {
    const view = renderHook(({events}) => usePresentationEvents(events), {
      initialProps: {events: [event(1, "turn.started"), event(2, "turn.completed")]}
    });
    const replacement = event(1, "usage");
    view.rerender({events: [replacement]});
    expect(view.result.current).toEqual([replacement]);
    view.rerender({events: []});
    expect(view.result.current).toEqual([]);
    const next = event(3, "turn.started");
    view.rerender({events: [next]});
    expect(view.result.current).toEqual([next]);
  });

  it("retains all non-delta metadata including usage, approvals and tool lifecycle", () => {
    const events = ["usage", "approval.required", "tool.start", "provider.attempt", "turn.receipt"]
      .map((kind, index) => event(index + 1, kind));
    const view = renderHook(() => usePresentationEvents(events));
    expect(view.result.current).toEqual(events);
  });
});

function event(sequence: number, kind: string): RuntimeEvent {
  return {
    version: 1, id: `event-${sequence}`, kind, operation_id: "operation",
    thread_id: "thread", turn_id: "turn", item_id: `item-${sequence}`, sequence,
    created_at: "2026-01-01T00:00:00Z", data: {}
  };
}
