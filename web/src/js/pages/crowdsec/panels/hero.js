import { state } from '../state.js';
import { formatCount, headline } from '../format.js';

function crowdsecCollectors() {
  return state.collectors.filter(c => c.types.some(t => t.type === 'crowdsec' && t.enabled));
}

// renderHero fills the headline and figures from the last summary and collector list.
export function renderHero() {
  const ui = state.ui;
  const list = crowdsecCollectors();
  const reporting = list.filter(c => c.connected && c.types.some(t => t.type === 'crowdsec' && t.status === 'ok')).length;
  ui.collectorCount.textContent = list.length === 1 ? '1 collector' : list.length + ' collectors';
  const h = headline(state.summary, state.view, state.collectors, state.components);
  ui.headline.textContent = h.text;
  ui.headline.classList.toggle('is-bad', h.bad);
  const s = state.summary;
  ui.statAlerts.textContent = s ? formatCount(s.alerts) : '–';
  ui.statIps.textContent = s && s.unique_ips !== null && s.unique_ips !== undefined ? formatCount(s.unique_ips) : '–';
  ui.statIps.title = s && (s.unique_ips === null || s.unique_ips === undefined) ? 'Not kept in the hourly summary used for long ranges' : '';
  ui.statBans.textContent = s ? formatCount(s.active?.ban || 0) : '–';
  ui.statCollectors.textContent = list.length ? reporting + ' of ' + list.length : '–';
  ui.empty.hidden = list.length > 0 || !state.collectorsLoaded;
  ui.main.hidden = !ui.empty.hidden || !!state.view.ip;
}

export function hasCrowdSecCollectors() {
  return crowdsecCollectors().length > 0;
}
