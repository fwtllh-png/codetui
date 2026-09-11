import {cleanup, render, screen, waitFor} from "@testing-library/react";
import {afterEach, describe, expect, it, vi} from "vitest";
import {MarkdownMessage} from "./MarkdownMessage";

const parsing = vi.hoisted(() => ({calls: 0}));
vi.mock("remark-gfm", async (importOriginal) => {
  const actual = await importOriginal<typeof import("remark-gfm")>();
  return {default: function (this: unknown, ...args: unknown[]) {
    parsing.calls += 1;
    return Reflect.apply(actual.default, this, args);
  }};
});
afterEach(() => {
  cleanup();
  parsing.calls = 0;
});

describe("streaming Markdown", () => {
  it("does not reparse completed messages when a sibling streams", async () => {
    const history = "**Historical answer**\n\n```go\nfunc main() {}\n```";
    const view = render(<>
      <MarkdownMessage text={history} settled />
      <MarkdownMessage text="Live" settled={false} />
    </>);
    const before = parsing.calls;
    view.rerender(<>
      <MarkdownMessage text={history} settled />
      <MarkdownMessage text="Live **update**" settled={false} />
    </>);
    await screen.findByText("update");
    expect(parsing.calls - before).toBe(1);
    expect(screen.getByText("Historical answer").tagName).toBe("STRONG");
  });

  it("applies authoritative final output without leaving a deferred draft behind", async () => {
    const view = render(<MarkdownMessage text="Provisional draft" settled={false} />);
    view.rerender(<MarkdownMessage text="Provisional draft continues" settled={false} />);
    view.rerender(<MarkdownMessage text="**Final answer**" settled />);
    expect(screen.queryByText(/Provisional draft/)).toBeNull();
    expect(screen.getByText("Final answer").tagName).toBe("STRONG");
    await waitFor(() => expect(view.container.textContent).toBe("Final answer"));
  });

  it("preserves Markdown syntax that resolves across stream chunks", async () => {
    const view = render(<MarkdownMessage text="[Source][ref]\n\n```typescript\nconst value" settled={false} />);
    view.rerender(<MarkdownMessage
      text={'[Source][ref]\n\n```typescript\nconst value = 1;\n```\n\n[ref]: https://example.test/source'}
      settled={false}
    />);
    expect(await screen.findByRole("link", {name: "Source"})).toBeTruthy();
    expect(screen.getByRole("region", {name: "typescript code"}).textContent)
      .toContain("const value = 1;");
  });
});
