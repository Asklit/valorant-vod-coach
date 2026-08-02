import { describe, expect, it } from "vitest";
import { buildRoute, pageFromPath, routePath } from "./router";

describe("application routes", () => {
  it("maps every product page to a stable path", () => {
    expect(routePath("dashboard")).toBe("/dashboard");
    expect(routePath("library")).toBe("/library");
    expect(routePath("review")).toBe("/review");
    expect(routePath("reports")).toBe("/reports");
    expect(routePath("admin")).toBe("/admin");
  });

  it("builds an encoded deep link without empty parameters", () => {
    expect(buildRoute("review", { vod: "diamond/demo 01", run: undefined })).toBe("/review?vod=diamond%2Fdemo+01");
  });

  it("uses a safe dashboard fallback", () => {
    expect(pageFromPath("/reports/")).toBe("reports");
    expect(pageFromPath("/unknown")).toBe("dashboard");
    expect(pageFromPath("/")).toBe("dashboard");
  });
});
