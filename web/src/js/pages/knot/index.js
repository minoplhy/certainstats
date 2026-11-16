/* Knot DNS page: traffic from Knot's counters, zone status across servers and,
   when switched on, dnstap drill-downs. Data comes from /api/collectors/knot;
   collectors are managed on the Collectors page. */

import { readPageConfig } from '../../lib/page_config.js';
import { init, load } from './init.js';
import { parseView, toQuery, apiParams, counterParams, addFilter, removeFilter, normalize, dnstapOnly } from './view.js';
import { headline, formatCount, formatRate, formatShare, formatBytes, until, span, zoneName } from './format.js';
import { seriesFor } from './charts.js';
import { zoneStatus, roles } from './zones.js';
import { sizeLabel } from './dnstap.js';

window.CertainStatsKnot = {
  load,
  // Pure helpers, exposed for the browser tests.
  parseView, toQuery, apiParams, counterParams, addFilter, removeFilter, normalize, dnstapOnly,
  headline, formatCount, formatRate, formatShare, formatBytes, until, span, zoneName, seriesFor, zoneStatus, roles, sizeLabel
};

const config = readPageConfig('CertainStatsKnot');
const root = typeof document !== 'undefined' ? document.querySelector('[data-kn-page]') : null;
if (config && root) init(root, config);
