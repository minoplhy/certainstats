/* Public status page and its in-page agent detail, served at {public_path}/{slug}. */

import { init } from './init.js';
import { handlePubItemClick, navigateToPubAgent, closePublicDetail, setPublicViewMode, filterPublicMonitors } from './nav/routing.js';
import { syncPublicDashboardMetadata } from './sync.js';
import { readPageConfig, snake } from '../../lib/page_config.js';

// Export module namespace
window.CertainStatsPublicDashboard = {
  init: init,
  handlePubItemClick: handlePubItemClick,
  navigateToPubAgent: navigateToPubAgent,
  closePublicDetail: closePublicDetail,
  setPublicViewMode: setPublicViewMode,
  filterPublicMonitors: filterPublicMonitors,
  syncPublicDashboardMetadata: syncPublicDashboardMetadata
};

// Backwards compatibility globals for inline onclick handlers
window.handlePubItemClick = handlePubItemClick;
window.navigateToPubAgent = navigateToPubAgent;
window.closePublicDetail = closePublicDetail;
window.setPublicViewMode = setPublicViewMode;
window.filterPublicMonitors = filterPublicMonitors;

// The template renders this page's configuration into #page-config ahead of this script.
const pageConfig = readPageConfig('CertainStatsPublicDashboard');
if (pageConfig) {
  const agents = snake(pageConfig.Agents || []).map(agent => Object.assign(agent, agent.net || {}));
  init({dashId: pageConfig.Dashboard.DashboardID, dashSlug: pageConfig.Dashboard.Slug, maxDays: pageConfig.AccessRules.max_days, allowedMetrics: pageConfig.AccessRules.allowed_metrics || [], agents});
}
