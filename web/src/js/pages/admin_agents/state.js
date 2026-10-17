// Page state shared by every part of the Agent Hub.
export const state = {
  panelPath: '',
  agentsData: [],
  currentActiveAgentId: null,
  // Network and incident views mounted in node details, and the Network page view.
  nodeNetwork: null,
  nodeHistory: null,
  networkPage: null,
  // SPA route: 'overview', 'network' or an agent ID; scroll is remembered per route.
  activeRoute: null,
  detailAgentId: null,
  sparklineListenersReady: false,
  inpageTimePicker: null,
  inpageCustomRange: null,
  inpageCpuChart: null,
  inpageRamChart: null,
  inpageNetChart: null,
  inpageDiskCharts: {},
  liveMetricsStore: {},
  savedScrollY: 0,
  lastAdminSyncTime: Date.now()
};

export const routeScroll = new Map();

export const ADMIN_METADATA_SYNC_INTERVAL_MS = 300000;
