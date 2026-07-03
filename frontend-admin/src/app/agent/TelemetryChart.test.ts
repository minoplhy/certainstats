import { describe, it, expect } from "vitest";
import { calculateDowntimes, buildChartData, ChartPoint, SeriesCfg } from "./TelemetryChart";

describe("TelemetryChart helpers", () => {
  const series: SeriesCfg[] = [
    { metric: "cpu", label: "CPU", color: "#ff0000" }
  ];

  describe("calculateDowntimes", () => {
    it("returns empty array for empty data", () => {
      expect(calculateDowntimes([])).toEqual([]);
    });

    it("detects gaps when data has offline segments", () => {
      const data: ChartPoint[] = [
        { time: 60000, CPU: 10 },
        { time: 120000, CPU: 15 },
        // huge gap
        { time: 600000, CPU: 20 },
      ];
      // gaps = [60000]. normalInterval = 60000. threshold = 150000.
      // Gap is from 120000 to 600000 (diff 480000 > 150000).
      // start: 120000 + 60000 = 180000
      // end: 600000 - 60000 = 540000
      const downtimes = calculateDowntimes(data);
      expect(downtimes).toEqual([{ start: 180000, end: 540000 }]);
    });

    it("stretches downtime to queryEndTime when offline at the end", () => {
      const data: ChartPoint[] = [
        { time: 60000, CPU: 10 },
        { time: 120000, CPU: 15 },
      ];
      const queryEndTime = 600000;
      // normalInterval = 60000. threshold = 150000.
      // queryEndTime (600000) > last (120000) + threshold (150000)
      // start: 120000 + 60000 = 180000
      // end: 600000
      const downtimes = calculateDowntimes(data, queryEndTime);
      expect(downtimes).toEqual([{ start: 180000, end: 600000 }]);
    });
  });

  describe("buildChartData", () => {
    it("returns original data if single point and no queryEndTime", () => {
      const data = [{ time: 1000, CPU: 10 }];
      expect(buildChartData(data, series)).toEqual(data);
    });

    it("inserts trailing nulls for single point with queryEndTime", () => {
      const data = [{ time: 60000, CPU: 10 }];
      const queryEndTime = 200000;
      const result = buildChartData(data, series, queryEndTime);
      expect(result.length).toBe(3);
      expect(result[0]).toEqual({ time: 60000, CPU: 10 });
      expect(result[1]).toEqual({ time: 75000, CPU: null }); // 60000 + 15000
      expect(result[2]).toEqual({ time: 200000, CPU: null });
    });

    it("inserts trailing nulls for multiple points when last point is old", () => {
      const data: ChartPoint[] = [
        { time: 60000, CPU: 10 },
        { time: 120000, CPU: 15 },
      ];
      const queryEndTime = 600000;
      const result = buildChartData(data, series, queryEndTime);
      // normalInterval = 60000
      // queryEndTime > 120000 + 150000
      // null point 1 at 120000 + 60000 = 180000
      // null point 2 at 600000
      expect(result.length).toBe(4);
      expect(result[2]).toEqual({ time: 180000, CPU: null });
      expect(result[3]).toEqual({ time: 600000, CPU: null });
    });
  });
});
