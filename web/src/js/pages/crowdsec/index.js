/* CrowdSec page: alerts gathered by collectors, with shared filters, range,
   breakdowns, an alert table and a per-IP view. Data comes from
   /api/collectors/crowdsec; collectors are managed by CertainStatsCollectors. */

import { readPageConfig } from '../../lib/page_config.js';
import { init, load } from './init.js';
import { parseView, toQuery, apiParams, resolveRange, addFilter, removeFilter } from './url/state.js';
import { headline, rangeLabel, groupLabel, untilText } from './format.js';

window.CertainStatsCrowdSec = {
  load,
  // Pure helpers, exposed for the browser tests.
  parseView, toQuery, apiParams, resolveRange, addFilter, removeFilter, headline, rangeLabel, groupLabel, untilText
};

const config = readPageConfig('CertainStatsCrowdSec');
const root = typeof document !== 'undefined' ? document.querySelector('[data-cs-page]') : null;
if (config && root) init(root, config);
