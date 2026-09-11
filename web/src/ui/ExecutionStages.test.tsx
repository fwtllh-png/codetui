import {cleanup, fireEvent, render, screen} from "@testing-library/react";
import {afterEach, expect, it} from "vitest";
import type {ConversationNode} from "../projection/conversation";
import {ExecutionStages} from "./ExecutionStages";

afterEach(cleanup);

const entries: ConversationNode[] = [
  {id: "update", kind: "commentary", turnID: "turn", sequence: 1,
    text: "Located the handler.", sampleID: "sample", callIDs: ["read"]},
  {id: "reasoning", kind: "reasoning", turnID: "turn", sequence: 2,
    text: "Check the caller", summary: "Check", running: false},
  {id: "final", kind: "assistant", turnID: "turn", sequence: 3, text: "Complete."}
];

const renderEntry = (entry: ConversationNode) => (
  <div key={entry.id}>{"text" in entry ? entry.text : entry.kind}</div>
);

it("collapses only stage detail and reveals a search target inside it", () => {
  const view = render(<ExecutionStages entries={entries} renderEntry={renderEntry} />);
  expect(screen.getByText("Located the handler.")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", {name: "Stage details 1 step"}));
  expect(screen.queryByText("Check the caller")).toBeNull();
  expect(screen.getByText("Located the handler.")).toBeTruthy();
  expect(screen.getByText("Complete.")).toBeTruthy();
  view.rerender(<ExecutionStages entries={entries} renderEntry={renderEntry} revealEntryID="reasoning" />);
  expect(screen.getByText("Check the caller")).toBeTruthy();
});

it("leaves old transcripts without commentary unchanged", () => {
  render(<ExecutionStages entries={entries.slice(1)} renderEntry={renderEntry} />);
  expect(screen.queryByRole("button")).toBeNull();
  expect(screen.getByText("Check the caller")).toBeTruthy();
});
