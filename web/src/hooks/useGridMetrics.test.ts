import { describe, expect, it } from "vitest";

import { readGridMetrics } from "@/hooks/useGridMetrics";

const style = (gridTemplateColumns: string, columnGap = "18px", rowGap = "28px") => ({
  gridTemplateColumns,
  columnGap,
  rowGap,
});

describe("readGridMetrics", () => {
  it("counts the resolved tracks as lanes and keeps the track list verbatim", () => {
    const metrics = readGridMetrics(style("180px 180px 180px 180px"));
    expect(metrics.lanes).toBe(4);
    expect(metrics.template).toBe("180px 180px 180px 180px");
  });

  it("reads the gaps as numbers", () => {
    expect(readGridMetrics(style("180px", "12px", "20px"))).toMatchObject({ columnGap: 12, rowGap: 20 });
  });

  it("treats a non-grid container as a single lane", () => {
    // The watched list view is a flex column, so its computed template is "none".
    expect(readGridMetrics(style("none"))).toMatchObject({ lanes: 1, template: "minmax(0, 1fr)" });
    expect(readGridMetrics(style(""))).toMatchObject({ lanes: 1, template: "minmax(0, 1fr)" });
  });

  it("falls back to zero for gaps the browser reports as 'normal'", () => {
    expect(readGridMetrics(style("180px", "normal", "normal"))).toMatchObject({ columnGap: 0, rowGap: 0 });
  });

  it("ignores the padding around a multi-space track list", () => {
    expect(readGridMetrics(style("  100px 100px  ")).lanes).toBe(2);
  });
});
