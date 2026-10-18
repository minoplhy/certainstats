/* CertainStats — client-side telemetry shared by the admin panel and public dashboards: live feed,
   in-place routing, cluster figures, partitions, tooltips and toasts. Admin-only extras are added by
   admin_core. */

import { onReady } from './page/ready.js';
import { normalizeSnapshot } from './feed/snapshot.js';
import { showToast } from './ui/toast.js';
import { formatUptime } from './format/uptime.js';
import { renderPartitions, renderClusterStats } from './feed/cluster_stats.js';
import { initWebSocket, onFeedEvent } from './feed/live_feed.js';
import { initRouter } from './page/router.js';
import { initFloatingTooltips, renderTooltip } from './ui/tooltips.js';
import { initScrollRestoration, initPage } from './page/page_init.js';

window.CertainStatsTelemetry = {
  onReady,
  normalizeSnapshot,
  showToast,
  formatUptime,
  renderPartitions,
  renderClusterStats,
  initWebSocket,
  onFeedEvent,
  initScrollRestoration,
  initRouter,
  initFloatingTooltips,
  renderTooltip
};

onReady(initPage);
