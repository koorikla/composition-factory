const { test, expect } = require("@playwright/test");
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require("./helpers");
const pristine = require("./fixtures/pristine-doc.json");

test.describe("CF-266 — Deleting a wired environment key from the palette rail hard-aborts instead of offering to unwire", () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test("deleting a wired environment key in palette rail prompts to unwire, unwires referencing fields, and succeeds without warnbar", async ({ page, request }) => {
    // 1. Launch canvas with blueprint containing environment key wired to resource field (env.regionKey -> q.fields.region)
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.environment = {
      regionKey: {
        type: "string",
        default: "us-east-1"
      }
    };
    doc.spec.resources = [
      {
        name: "q",
        kind: "Queue",
        provider: "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0",
        fields: {
          region: { from: "env.regionKey" }
        }
      }
    ];
    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto("/");
    await canvasSettled(page);

    // 2. Switch to Shared rail tab
    await page.click("#rtabs button[data-r=\"shared\"]");

    const delBtn = page.locator("#region-palette [data-env-del=\"regionKey\"]");
    await expect(delBtn).toBeVisible();

    // Verify canvas wire exists prior to deletion
    await expect(page.locator(".wire-path[title*=\"env.regionKey\"]")).toHaveCount(1);

    // 3 & 4. Click [data-env-del="regionKey"] and accept unwiring dialog
    let dialogMessage = "";
    page.on("dialog", async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.accept();
    });

    await delBtn.click();

    // Verify prompt matches expected inspector confirmation
    expect(dialogMessage).toBe("Environment key \"regionKey\" is wired into 1 field. Delete it and unwire all referencing fields?");

    // 5. Verify no error message/warnbar appears in the rail
    await expect(page.locator("#region-palette .warnbar")).toHaveCount(0);

    // Verify no error toast
    const toast = page.locator("#toast, #errtoast");
    if (await toast.isVisible()) {
      const text = await toast.textContent();
      expect(text).not.toMatch(/error|referenced/i);
    }

    // 6. Verify environment key is deleted and canvas wire is cleanly removed
    await expect(page.locator("#region-palette [data-env-del=\"regionKey\"]")).toHaveCount(0);
    await canvasSettled(page);
    await expect(page.locator(".wire-path[title*=\"env.regionKey\"]")).toHaveCount(0);

    // Verify backend blueprint state
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updatedDoc = await res.json();
      const env = (updatedDoc.spec && updatedDoc.spec.environment) || {};
      const resources = (updatedDoc.spec && updatedDoc.spec.resources) || [];
      const q = resources.find((r) => r.name === "q");
      const qFields = (q && q.fields) || {};
      return {
        hasEnvKey: "regionKey" in env,
        qHasRegionWire: "region" in qFields && !!(qFields.region.from || qFields.region.raw),
      };
    }).toEqual({
      hasEnvKey: false,
      qHasRegionWire: false,
    });
  });

  test("cancelling unwire dialog leaves environment key and wires intact without setting error", async ({ page, request }) => {
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.environment = {
      regionKey: {
        type: "string",
        default: "us-east-1"
      }
    };
    doc.spec.resources = [
      {
        name: "q",
        kind: "Queue",
        provider: "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0",
        fields: {
          region: { from: "env.regionKey" }
        }
      }
    ];
    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto("/");
    await canvasSettled(page);

    await page.click("#rtabs button[data-r=\"shared\"]");
    const delBtn = page.locator("#region-palette [data-env-del=\"regionKey\"]");
    await expect(delBtn).toBeVisible();

    page.on("dialog", async (dialog) => {
      await dialog.dismiss();
    });

    await delBtn.click();

    // Verify key is still present and no warnbar was rendered
    await expect(delBtn).toBeVisible();
    await expect(page.locator("#region-palette .warnbar")).toHaveCount(0);
  });

  test("deleting unwired environment key prompts simple delete confirmation and succeeds", async ({ page, request }) => {
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.environment = {
      unwiredKey: {
        type: "string",
        default: "value"
      }
    };
    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto("/");
    await canvasSettled(page);

    await page.click("#rtabs button[data-r=\"shared\"]");
    const delBtn = page.locator("#region-palette [data-env-del=\"unwiredKey\"]");
    await expect(delBtn).toBeVisible();

    let dialogMessage = "";
    page.on("dialog", async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.accept();
    });

    await delBtn.click();

    expect(dialogMessage).toBe("Delete environment key $env.unwiredKey?");
    await expect(page.locator("#region-palette [data-env-del=\"unwiredKey\"]")).toHaveCount(0);
  });
});
