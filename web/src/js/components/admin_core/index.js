/* Admin-only additions to the shared telemetry and chart globals. Served on the panel to signed-in
   users only; loaded after telemetry.js and chart.js. */

import { Modal } from './ui/modal.js';
import { openReinstallModal, openUninstallModal, initModalBackdropHandlers } from './ui/action_modals.js';
import { initCustomTimePicker } from './time/picker.js';
import { parseUserAgent } from './format/user_agent.js';
import { getPanelPath } from './page/panel_path.js';
import { trackNetworkFeed, subscribeNetwork, getNetworkFeedState } from './feed/network.js';
import { drawSparkline } from './chart/sparkline.js';

const telemetry = window.CertainStatsTelemetry;

window.CertainStatsModal = Modal;

Object.assign(telemetry, {
  parseUserAgent,
  initCustomTimePicker,
  getPanelPath,
  subscribeNetwork,
  getNetworkFeedState,
  openReinstallModal,
  openUninstallModal,
  initModalBackdropHandlers
});

Object.assign(window.CertainStatsChart, { drawSparkline });

trackNetworkFeed(telemetry.onFeedEvent);
telemetry.onReady(initModalBackdropHandlers);
