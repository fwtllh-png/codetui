import {expect, test} from "@playwright/test";
import {createServer, type ViteDevServer} from "vite";
import path from "node:path";
import {fileURLToPath} from "node:url";

let server: ViteDevServer;
let url: string;

test.beforeAll(async () => {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
  server = await createServer({
    root,
    configFile: path.join(root, "vite.config.ts"),
    server: {host: "127.0.0.1", port: 0, open: false}
  });
  await server.listen();
  const address = server.httpServer!.address();
  if (!address || typeof address === "string") throw new Error("missing fixture port");
  url = `http://127.0.0.1:${address.port}/tests/e2e/fixtures/streaming.html`;
});

test.afterAll(async () => { await server?.close(); });

for (const width of [1440, 390]) {
  test(`explicit navigation releases a transcript interaction anchor at ${width}px`, async ({page}) => {
    await page.setViewportSize({width, height: 900});
    await page.goto(`${url}?reasoning`);
    const think = page.locator(".reasoningDisclosure > button");
    await think.click();
    await expect(think).toHaveAttribute("aria-expanded", "true");
    await page.getByRole("button", {name: "Search conversation", exact: true}).click();
    const search = page.getByRole("dialog", {name: "Search conversation", exact: true});
    await search.getByRole("tab", {name: "Turns", exact: true}).click();
    await search.getByRole("combobox", {name: "Search conversation", exact: true})
      .fill("Historical question 0");
    await search.getByRole("option").filter({hasText: "Historical question 0"}).click();
    const target = page.locator('[data-navigation-current] .userMessage');
    await expect(target).toContainText("Historical question 0");
    await expect(target).toBeInViewport();
    await page.getByRole("button", {name: "Back to bottom", exact: true}).click();
    const scrollport = page.locator("[data-conversation-scroll]");
    await expect.poll(() => scrollport.evaluate((node) =>
      node.scrollHeight - node.scrollTop - node.clientHeight
    )).toBeLessThanOrEqual(24);
  });

  test(`copy is activated by the first click when output arrives during the press at ${width}px`, async ({page}) => {
    await page.context().grantPermissions(["clipboard-read", "clipboard-write"]);
    await page.setViewportSize({width, height: 900});
    await page.goto(`${url}?interactions`);
    const answer = page.locator('[data-entry-id="output-live"]');
    const copy = answer.getByRole("button", {name: "Copy code"}).first();
    await expect(copy).toBeVisible();
    const scrollport = page.locator("[data-conversation-scroll]");
    await expect.poll(() => scrollport.evaluate((node) =>
      node.scrollHeight - node.scrollTop - node.clientHeight
    )).toBeLessThanOrEqual(24);
    await page.getByRole("button", {name: "Append while pressed"}).click();
    const before = await copy.boundingBox();
    if (!before) throw new Error("copy button is not visible");
    await page.mouse.move(before.x + before.width / 2, before.y + before.height / 2);
    await page.mouse.down();
    await expect(answer.getByRole("region", {name: "typescript code"})).toHaveCount(5);
    const during = await copy.boundingBox();
    expect(Math.abs(during!.y - before.y)).toBeLessThan(2);
    await page.mouse.up();
    await expect(answer.getByRole("button", {name: "Copied code"})).toHaveCount(1);
    await page.getByRole("button", {name: "Back to bottom", exact: true}).click();
    await expect.poll(() => scrollport.evaluate((node) =>
      node.scrollHeight - node.scrollTop - node.clientHeight
    )).toBeLessThanOrEqual(24);
  });

  test(`tool and stage details retain their position on keyboard and pointer activation at ${width}px`, async ({page}) => {
    await page.setViewportSize({width, height: 900});
    await page.emulateMedia({reducedMotion: "no-preference"});
    await page.goto(`${url}?interactions`);
    const tool = page.locator(".toolDisclosure .disclosureRow");
    await expect(tool).toBeVisible();
    await expect.poll(() => page.locator(".turnExecution").evaluate((node) =>
      node.getAnimations({subtree: true}).filter((animation) => animation.playState === "running").length
    )).toBe(0);
    await tool.focus();
    const top = (await tool.boundingBox())!.y;
    await page.keyboard.press("Enter");
    await expect(tool).toHaveAttribute("aria-expanded", "true");
    await expect(page.locator(".toolExpanded")).toBeVisible();
    await expect.poll(async () => Math.abs((await tool.boundingBox())!.y - top)).toBeLessThan(2);
    await page.keyboard.press("Space");
    await expect(tool).toHaveAttribute("aria-expanded", "false");
    await expect(page.locator(".toolExpanded")).toHaveCount(0);
    const stage = page.getByRole("button", {name: /Stage details/});
    await stage.scrollIntoViewIfNeeded();
    // Leave scroll range for the collapsing block; at the absolute bottom,
    // the browser must clamp scrollTop when the document becomes shorter.
    await page.locator("[data-conversation-scroll]").hover();
    await page.mouse.wheel(0, -160);
    await expect(page.getByRole("button", {name: "Back to bottom", exact: true})).toBeVisible();
    const stageTop = (await stage.boundingBox())!.y;
    await stage.click();
    await expect(stage).toHaveAttribute("aria-expanded", "false");
    await expect(tool).toHaveCount(0);
    expect(Math.abs((await stage.boundingBox())!.y - stageTop)).toBeLessThan(2);
    await stage.click();
    await expect(stage).toHaveAttribute("aria-expanded", "true");
    await expect(tool).toBeVisible();
    expect(Math.abs((await stage.boundingBox())!.y - stageTop)).toBeLessThan(2);
  });

  test(`selecting transcript text is not displaced by incoming output at ${width}px`, async ({page}) => {
    await page.setViewportSize({width, height: 900});
    await page.goto(`${url}?interactions`);
    const answer = page.locator('[data-entry-id="output-live"]');
    const code = answer.locator("pre code").first();
    const scrollport = page.locator("[data-conversation-scroll]");
    await expect(code).toBeVisible();
    await expect.poll(() => scrollport.evaluate((node) =>
      node.scrollHeight - node.scrollTop - node.clientHeight
    )).toBeLessThanOrEqual(24);
    await page.getByRole("button", {name: "Append while pressed"}).click();
    const before = (await code.boundingBox())!;
    await page.mouse.move(before.x + 2, before.y + before.height / 2);
    await page.mouse.down();
    await expect(answer.getByRole("region", {name: "typescript code"})).toHaveCount(5);
    expect(Math.abs((await code.boundingBox())!.y - before.y)).toBeLessThan(2);
    await page.mouse.move(before.x + 60, before.y + before.height / 2);
    await page.mouse.up();
    expect(await page.evaluate(() => getSelection()?.toString())).toContain("const");
  });

  for (const reducedMotion of ["reduce", "no-preference"] as const) {
    test(`Think opens on the first click without moving its header at ${width}px (${reducedMotion})`, async ({page}, info) => {
      await page.setViewportSize({width, height: 900});
      await page.emulateMedia({reducedMotion});
      await page.goto(`${url}?reasoning`);
      const think = page.locator(".reasoningDisclosure > button");
      await expect(think).toBeVisible();
      const scrollport = page.locator("[data-conversation-scroll]");
      await expect.poll(() => scrollport.evaluate((node) =>
        node.scrollHeight - node.scrollTop - node.clientHeight
      )).toBeLessThanOrEqual(24);
      const before = await think.boundingBox();
      if (!before) throw new Error("Think is not visible");
      await page.mouse.move(before.x + 40, before.y + before.height / 2);
      await page.mouse.down();
      await expect(think).toBeFocused();
      const pressed = await think.boundingBox();
      expect(Math.abs(pressed!.y - before.y)).toBeLessThan(2);
      await page.mouse.up();
      await expect(think).toHaveAttribute("aria-expanded", "true");
      await expect(page.locator(".thinkBody")).toContainText("Reasoning line 80");
      await expect.poll(async () => Math.abs((await think.boundingBox())!.y - before.y))
        .toBeLessThan(2);
      // Observe the completed CSS transition, not just its initial frame.
      await expect.poll(() => page.locator(".thinkBody").evaluate((node) =>
        node.getAnimations({subtree: true}).filter((animation) => animation.playState === "running").length
      )).toBe(0);
      expect(Math.abs((await think.boundingBox())!.y - before.y)).toBeLessThan(2);
      await page.screenshot({path: info.outputPath(`think-${width}.png`)});
      await think.click();
      await expect(think).toHaveAttribute("aria-expanded", "false");
      await expect(page.locator(".thinkBody")).toHaveCount(0);
      await expect.poll(async () => Math.abs((await think.boundingBox())!.y - before.y))
        .toBeLessThan(2);
    });
  }

  test(`streaming preserves input, final content and follow at ${width}px`, async ({page}, info) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.setViewportSize({width, height: 900});
    await page.goto(url);
    await expect(page.locator(".assistantMarkdown").first()).toBeVisible();
    await page.getByRole("button", {name: "Run streaming fixture"}).click();
    await page.getByPlaceholder("Ask QCode").fill("Draft while the response is streaming");
    await expect(page.getByRole("button", {name: "Streaming fixture complete"})).toBeVisible();
    await expect(page.getByPlaceholder("Ask QCode")).toHaveValue("Draft while the response is streaming");
    const answer = page.locator('[data-entry-id="output-live"]');
    await expect(answer.getByRole("region", {name: "typescript code"})).toHaveCount(40);
    await expect(answer.getByRole("button", {name: "Copy response"})).toBeVisible();
    const scrollport = page.locator("[data-conversation-scroll]");
    await expect.poll(() => scrollport.evaluate((node) =>
      node.scrollHeight - node.scrollTop - node.clientHeight
    )).toBeLessThanOrEqual(24);
    expect(errors).toEqual([]);
    await info.attach("frame-measurement", {
      body: await page.locator("#streaming-results").textContent() ?? "",
      contentType: "application/json"
    });
    await page.screenshot({
      path: info.outputPath(`streaming-${width}.png`),
      style: "body > button { visibility: hidden; }"
    });
  });

  test(`reading history is not displaced by streaming at ${width}px`, async ({page}) => {
    await page.setViewportSize({width, height: 900});
    await page.goto(url);
    await expect(page.locator(".assistantMarkdown").first()).toBeVisible();
    const scrollport = page.locator("[data-conversation-scroll]");
    await scrollport.hover();
    await page.mouse.wheel(0, -1200);
    await expect(page.getByRole("button", {name: "Back to bottom", exact: true})).toBeVisible();
    const before = await scrollport.evaluate((node) => node.scrollTop);
    await page.getByRole("button", {name: "Run streaming fixture"}).click();
    await expect(page.getByRole("button", {name: "Streaming fixture complete"})).toBeVisible();
    expect(Math.abs(await scrollport.evaluate((node) => node.scrollTop) - before)).toBeLessThan(2);
    await page.getByRole("button", {name: "Back to bottom", exact: true}).click();
    await expect.poll(() => scrollport.evaluate((node) =>
      node.scrollHeight - node.scrollTop - node.clientHeight
    )).toBeLessThanOrEqual(24);
  });
}
