import {expect, test} from "@playwright/test";
import {createServer, type ViteDevServer} from "vite";
import path from "node:path";
import {fileURLToPath} from "node:url";

let server: ViteDevServer;
let url: string;
test.beforeAll(async () => {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
  server = await createServer({root, configFile: path.join(root, "vite.config.ts"),
    server: {host: "127.0.0.1", port: 0, open: false}});
  await server.listen();
  const address = server.httpServer!.address();
  if (!address || typeof address === "string") throw new Error("missing fixture port");
  url = `http://127.0.0.1:${address.port}/tests/e2e/fixtures/streaming.html?withdrawal`;
});
test.afterAll(async () => { await server?.close(); });

for (const [width, scheme] of [[1440, "light"], [390, "light"], [1440, "dark"], [390, "dark"]] as const) {
  test(`withdraws the latest turn at ${width}px in ${scheme}`, async ({page}, info) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.setViewportSize({width, height: 900});
    await page.emulateMedia({colorScheme: scheme, reducedMotion: "reduce"});
    await page.goto(url);
    await page.waitForLoadState("networkidle");
    const withdraw = page.getByRole("button", {name: "Withdraw turn", exact: true});
    await expect(withdraw).toHaveCount(1);
    await expect(withdraw).toBeInViewport();
    const request = page.locator('.turnTranscript[data-turn-id="live"] .userMessageGroup');
    await expect(request.getByRole("button", {name: "Withdraw turn"})).toHaveCount(1);
    const bubble = await request.locator(".userMessage").boundingBox();
    const button = await withdraw.boundingBox();
    expect(bubble).not.toBeNull();
    expect(button).not.toBeNull();
    expect(button!.y).toBeGreaterThanOrEqual(bubble!.y + bubble!.height);
    expect(button!.y - bubble!.y - bubble!.height).toBeLessThanOrEqual(8);
    expect(Math.abs(button!.x + button!.width - bubble!.x - bubble!.width)).toBeLessThanOrEqual(1);
    await page.screenshot({
      path: info.outputPath(`withdrawal-button-${width}.png`),
      style: "body > button { visibility: hidden; }"
    });
    await withdraw.click();
    const confirmation = page.getByRole("alertdialog", {name: "Withdraw this turn?"});
    await expect(confirmation).toBeVisible();
    await expect(confirmation.getByRole("button", {name: "Cancel"})).toBeFocused();
    expect(await request.locator(".userMessage").boundingBox()).toEqual(bubble);
    const modal = await confirmation.boundingBox();
    expect(modal).not.toBeNull();
    expect(Math.abs(modal!.x + modal!.width / 2 - width / 2)).toBeLessThanOrEqual(1);
    expect(modal!.height).toBeLessThan(300);
    expect(modal!.y).toBeGreaterThanOrEqual(0);
    await page.keyboard.press("Shift+Tab");
    await expect(confirmation.getByRole("button", {name: "Withdraw", exact: true})).toBeFocused();
    await page.keyboard.press("Tab");
    await expect(confirmation.getByRole("button", {name: "Cancel"})).toBeFocused();
    await page.screenshot({
      path: info.outputPath(`withdrawal-confirm-${width}.png`),
      style: "body > button { visibility: hidden; }"
    });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await confirmation.getByRole("button", {name: "Cancel"}).click();
    await expect(withdraw).toBeVisible();
    await expect(withdraw).toBeFocused();
    await withdraw.click();
    await page.keyboard.press("Escape");
    await expect(confirmation).toHaveCount(0);
    await expect(withdraw).toBeFocused();
    await withdraw.click();
    await confirmation.getByRole("button", {name: "Withdraw", exact: true}).click();
    await expect(withdraw).toHaveCount(0);
    const turn = page.locator('.turnTranscript[data-turn-id="live"]');
    const audit = turn.getByRole("button", {name: "Turn withdrawn Excluded from context"});
    await expect(audit).toBeVisible();
    await expect(request).toHaveCount(0);
    await expect(turn.getByRole("button", {name: "Retry", exact: true})).toHaveCount(0);
    await expect(turn.getByRole("button", {name: "Continue", exact: true})).toHaveCount(0);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    expect(errors).toEqual([]);
    await page.screenshot({
      path: info.outputPath(`withdrawal-${width}.png`),
      style: "body > button { visibility: hidden; }"
    });
    await audit.click();
    await expect(request).toContainText("Stream a detailed answer");
  });
}

test("withdraws inside an embedded browser that disallows native dialogs", async ({page}) => {
  const nativeDialogs: string[] = [];
  page.on("dialog", (dialog) => { nativeDialogs.push(dialog.type()); void dialog.dismiss(); });
  await page.goto(new URL("/", url).toString());
  await page.setContent(`<iframe sandbox="allow-scripts allow-same-origin"
    style="width:100%;height:850px;border:0" src="${url}"></iframe>`);
  const frame = page.frameLocator("iframe");
  await frame.getByRole("button", {name: "Withdraw turn", exact: true}).click();
  const confirmation = frame.getByRole("alertdialog", {name: "Withdraw this turn?"});
  await expect(confirmation).toBeVisible();
  await confirmation.getByRole("button", {name: "Withdraw", exact: true}).click();
  await expect(frame.getByRole("button", {name: "Turn withdrawn Excluded from context"})).toBeVisible();
  await expect(frame.getByText("Stream a detailed answer", {exact: true})).toHaveCount(0);
  expect(nativeDialogs).toEqual([]);
});
