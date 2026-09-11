import {describe, expect, it} from "vitest";
import type {RuntimeEvent} from "../protocol";
import {ConversationProjection, projectConversation} from "./conversation";
import {projectTrajectory} from "./trajectory";
import {projectConversationNavigation, searchConversationNavigation} from "../ui/conversationNavigation";

function event(sequence: number, kind: string, data: Record<string, unknown>): RuntimeEvent {
  return {
    version: 1, id: `event-${sequence}`, sequence, kind, data,
    operation_id: "operation", thread_id: "thread", turn_id: "turn", item_id: "item",
    created_at: new Date(sequence * 1000).toISOString()
  };
}

function commentary(sequence: number, sample: string, call: string) {
  return event(sequence, "commentary.completed", {
    message_id: `turn/commentary/${sample}`, sample_id: sample,
    text: `Established ${sample}. Checking the next caller.`, call_ids: [call]
  });
}

const timeline = [
  event(1, "turn.started", {prompt: "Find the handler"}),
  commentary(2, "first", "read"),
  event(3, "tool.start", {call_id: "read", tool: "file_read", arguments: {path: "main.go"}}),
  event(4, "tool.result", {call_id: "read", output: "handler", is_error: false}),
  commentary(5, "second", "search"),
  event(6, "tool.start", {call_id: "search", tool: "search_text", arguments: {query: "handler"}}),
  event(7, "tool.result", {call_id: "search", output: "found", is_error: false}),
  event(8, "output.delta", {text: "Final answer."}),
  event(9, "turn.completed", {text: "Final answer."})
];

describe("commentary", () => {
  it("retains distinct messages around tools and never overwrites them with the final answer", () => {
    const projection = new ConversationProjection();
    projection.applyAll(timeline.slice(0, 4));
    const first = projection.snapshot().nodes.get("commentary-turn:turn/commentary/first");
    projection.applyAll(timeline.slice(4));
    const snapshot = projection.snapshot();
    expect(snapshot.order.map((id) => snapshot.nodes.get(id)?.kind)).toEqual([
      "user", "commentary", "tool", "commentary", "tool", "assistant"
    ]);
    expect(snapshot.nodes.get(first!.id)).toBe(first);
    expect([...snapshot.nodes.values()].filter((node) => node.kind === "assistant"))
      .toMatchObject([{text: "Final answer."}]);
    expect([...snapshot.nodes]).toEqual([...projectConversation(timeline).nodes]);
    expect(snapshot.order).toEqual(projectConversation(timeline).order);
    projection.apply(commentary(10, "first", "read"));
    expect(projection.snapshot()).toBe(snapshot);
  });

  it("anchors a recovered message before its tools, including after a failed turn", () => {
    const snapshot = projectConversation([
      timeline[0]!, timeline[2]!, timeline[3]!,
      event(5, "turn.failed", {message: "Network disconnected"}),
      commentary(6, "first", "read")
    ]);
    expect(snapshot.order.map((id) => snapshot.nodes.get(id)?.kind)).toEqual([
      "user", "commentary", "tool", "status"
    ]);
    expect(snapshot.activeTurnID).toBe("");
  });

  it("indexes the full update for search and exposes an independent trajectory record", () => {
    const snapshot = projectConversation(timeline);
    const entries = snapshot.order.map((id) => snapshot.nodes.get(id)!);
    const results = searchConversationNavigation(projectConversationNavigation(entries), "Established second");
    expect(results).toHaveLength(1);
    expect(results[0]).toMatchObject({kind: "message", entryID: "commentary-turn:turn/commentary/second"});
    const trajectory = projectTrajectory([...timeline, commentary(10, "first", "read")]);
    const updates = trajectory.records.filter((record) => record.label === "UPDATE");
    expect(updates).toHaveLength(2);
    expect(updates.map((record) => record.id)).toEqual([
      "commentary-turn:turn/commentary/first", "commentary-turn:turn/commentary/second"
    ]);
    expect(trajectory.spans.find((span) => span.recordID === updates[0]!.id)).toMatchObject({
      startedAt: 2000, endedAt: 2000
    });
  });

  it("routes child commentary to its agent without reviving a completed child on replay", () => {
    const child = (value: RuntimeEvent): RuntimeEvent => ({
      ...value, thread_id: "child-thread", turn_id: "child-turn"
    });
    const projection = new ConversationProjection();
    projection.apply(event(1, "agent.spawned", {
      agent_id: "child", role: "review", detail: {thread_id: "child-thread", task_name: "Review"}
    }));
    projection.apply(child(event(2, "tool.start", {tool: "file_read", call_id: "read"})));
    projection.apply(child(event(3, "turn.completed", {text: "Review complete."})));
    projection.apply(child(commentary(4, "first", "read")));
    const snapshot = projection.snapshot();
    expect(snapshot.order).toEqual(["agent-child"]);
    const node = snapshot.nodes.get("agent-child");
    expect(node?.kind).toBe("agent");
    if (node?.kind !== "agent") throw new Error("missing child");
    expect(node.state).toBe("completed");
    expect(node.summary).toBe("Review complete.");
    expect(node.activities.map((activity) => activity.title)).toEqual([
      "Queued", "Update", "Read", "Result"
    ]);
    projection.apply(child(commentary(5, "first", "read")));
    expect(projection.snapshot()).toBe(snapshot);
  });
});
