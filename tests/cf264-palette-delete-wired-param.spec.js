const { test, expect } = require("@playwright/test");
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require("./helpers");
const pristine = require("./fixtures/pristine-doc.json");

test.describe("CF-264 — Deleting a wired parameter from the palette rail fails backend validation instead of unwiring", () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test("deleting a wired parameter in palette rail prompts to unwire, unwires referencing fields, and succeeds without warnbar or 400 error", async ({ page, request }) => {
    // 1. Launch canvas with blueprint containing single wired parameter (params.region -> q.fields.region)
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.resources = [
      {
        name: "q",
        kind: "Queue",
        provider: "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0",
        fields: {
          region: { from: "params.region" }
        }
      }
    ];
    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto("/");
    await canvasSettled(page);

    // 2. Switch to Shared rail tab
    await page.click("#rtabs button[data-r=\"shared\"]");

    const delBtn = page.locator("#region-palette [data-param-del=\"region\"]");
    await expect(delBtn).toBeVisible();

    // Verify canvas wire exists prior to deletion
    await expect(page.locator(".wire-path[title*=\"$region\"]")).toHaveCount(1);

    // 3 & 4. Click [data-param-del="region"] and accept dialog confirming unwiring
    let dialogMessage = "";
    page.on("dialog", async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.accept();
    });

    await delBtn.click();

    // Confirming unwiring prompt matches inspector pattern
    expect(dialogMessage).toBe("Parameter \"region\" is wired into 1 field. Delete it and unwire all referencing fields?");

    // 5. Verify no error warnbar is shown in palette
    await expect(page.locator("#region-palette .warnbar")).toHaveCount(0);

    // Verify no error toast
    const toast = page.locator("#toast, #errtoast");
    if (await toast.isVisible()) {
      const text = await toast.textContent();
      expect(text).not.toMatch(/error|referenced/i);
    }

    // 6. Verify parameter is removed from palette and canvas wire is removed
    await expect(page.locator("#region-palette [data-param-del=\"region\"]")).toHaveCount(0);
    await canvasSettled(page);
    await expect(page.locator(".wire-path[title*=\"$region\"]")).toHaveCount(0);

    // Verify backend blueprint state reflects parameter removal and field unwiring
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updatedDoc = await res.json();
      const params = (updatedDoc.spec.xrd && updatedDoc.spec.xrd.parameters) || {};
      const resources = updatedDoc.spec.resources || [];
      const q = resources.find((r) => r.name === "q");
      const qFields = (q && q.fields) || {};
      return {
        hasRegionParam: "region" in params,
        qHasRegionWire: "region" in qFields && !!(qFields.region.from || qFields.region.raw),
      };
    }).toEqual({
      hasRegionParam: false,
      qHasRegionWire: false,
    });
  });

  test("deleting a parameter wired into multiple resources pluralizes prompt and unwires all", async ({ page, request }) => {
    // In pristine doc, region is wired into both work-queue.region and dead-letter.region (fo = 2)
    await page.goto("/");
    await canvasSettled(page);

    await page.click("#rtabs button[data-r=\"shared\"]");
    const delBtn = page.locator("#region-palette [data-param-del=\"region\"]");
    await expect(delBtn).toBeVisible();

    let dialogMessage = "";
    page.on("dialog", async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.accept();
    });

    await delBtn.click();

    expect(dialogMessage).toBe("Parameter \"region\" is wired into 2 fields. Delete it and unwire all referencing fields?");
    await expect(page.locator("#region-palette .warnbar")).toHaveCount(0);
    await expect(page.locator("#region-palette [data-param-del=\"region\"]")).toHaveCount(0);

    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updatedDoc = await res.json();
      const params = (updatedDoc.spec.xrd && updatedDoc.spec.xrd.parameters) || {};
      const r0 = (updatedDoc.spec.resources || [])[0] || {};
      const r1 = (updatedDoc.spec.resources || [])[1] || {};
      return {
        hasParam: "region" in params,
        r0HasWire: !!(r0.fields && r0.fields.region),
        r1HasWire: !!(r1.fields && r1.fields.region)
      };
    }).toEqual({
      hasParam: false,
      r0HasWire: false,
      r1HasWire: false
    });
  });

  test("cancelling the unwire confirmation dialog keeps the parameter and wires intact", async ({ page, request }) => {
    await page.goto("/");
    await canvasSettled(page);

    await page.click("#rtabs button[data-r=\"shared\"]");
    const delBtn = page.locator("#region-palette [data-param-del=\"region\"]");
    await expect(delBtn).toBeVisible();

    let dialogMessage = "";
    page.on("dialog", async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.dismiss();
    });

    await delBtn.click();

    expect(dialogMessage).toBe("Parameter \"region\" is wired into 2 fields. Delete it and unwire all referencing fields?");
    await expect(page.locator("#region-palette .warnbar")).toHaveCount(0);
    await expect(page.locator("#region-palette [data-param-del=\"region\"]")).toBeVisible();

    const res = await request.get(`${ENGINE}/api/blueprint`);
    const doc = await res.json();
    expect(doc.spec.xrd.parameters.region).toBeDefined();
    expect(doc.spec.resources[0].fields.region.from).toBe("params.region");
  });

  test("deleting an unwired parameter in palette rail retains simple confirm and deletes parameter cleanly", async ({ page, request }) => {
    // Add an unwired parameter unwiredParam
    const docRes = await request.get(`${ENGINE}/api/blueprint`);
    const doc = await docRes.json();
    doc.spec.xrd.parameters.unwiredParam = { type: "string", required: false, default: "test" };
    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto("/");
    await canvasSettled(page);

    await page.click("#rtabs button[data-r=\"shared\"]");

    const delBtn = page.locator("#region-palette [data-param-del=\"unwiredParam\"]");
    await expect(delBtn).toBeVisible();

    let dialogMessage = "";
    page.on("dialog", async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.accept();
    });

    await delBtn.click();

    expect(dialogMessage).toBe("Delete parameter $unwiredParam?");
    await expect(page.locator("#region-palette .warnbar")).toHaveCount(0);
    await expect(page.locator("#region-palette [data-param-del=\"unwiredParam\"]")).toHaveCount(0);

    const res = await request.get(`${ENGINE}/api/blueprint`);
    const updatedDoc = await res.json();
    expect(updatedDoc.spec.xrd.parameters.unwiredParam).toBeUndefined();
  });
});
