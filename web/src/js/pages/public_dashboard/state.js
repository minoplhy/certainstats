// Page state shared by every part of the public dashboard.
export const state = {
  dashId: '',
  dashSlug: '',
  maxDays: 30,
  pubAllowedMetrics: [],
  pubAgentsData: [],
  pubLiveMetricsStore: {},
  currentActivePubId: null,
  savedPubScrollY: 0,
  pubInpageCpuChart: null,
  pubInpageRamChart: null,
  pubInpageNetChart: null,
  pubInpageDiskCharts: {}, // { [path]: { usageChart, ioChart } }
  lastPubSyncTime: Date.now()
};

export const PUB_METADATA_SYNC_INTERVAL_MS = 300000;
