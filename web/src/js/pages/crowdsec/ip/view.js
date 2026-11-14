import { state } from '../state.js';
import { el, formatCount, formatTime, untilText, copyText, shortScenario } from '../format.js';
import { begin, get, isAbort, showError } from '../api.js';
import { apiParams, resolveRange } from '../url/state.js';
import { rankedList } from '../panels/breakdowns.js';
import { drawTimeline } from '../panels/timeline.js';

// The IP view shows everything retained about one address, in place of the alert list.
export function bindIPView(actions) {
  const ui = state.ui;
  ui.ipBack.addEventListener('click', () => actions.closeIP());
  ui.ipFilter.addEventListener('click', () => actions.filterIP(state.view.ip));
  ui.ipCopy.addEventListener('click', () => copyText(state.view.ip, 'IP'));
}

function decisionRows(list) {
  if (!list.length) return [el('p', 'muted', 'No decisions were taken against this IP.')];
  return list.map(d => {
    const line = el('div', 'cs-decision');
    line.append(el('span', 'badge ' + (d.type === 'ban' ? 'badge-offline' : 'badge-warn'), d.type || 'decision'),
      el('span', 'mono', formatTime(d.started_at)), el('span', '', shortScenario(d.scenario)),
      el('span', 'muted', d.origin), el('span', 'mono', (d.duration ? d.duration + ' · ' : '') + untilText(d.until)));
    return line;
  });
}

export async function loadIPView() {
  const ui = state.ui;
  const ip = state.view.ip;
  ui.ipTitle.textContent = ip;
  ui.ipSubtitle.textContent = '';
  ui.ipMessage.textContent = 'Loading…';
  const controller = begin('ip');
  try {
    const v = await get('ips/' + encodeURIComponent(ip), controller.signal);
    ui.ipFirst.textContent = formatTime(v.first_seen);
    ui.ipLast.textContent = formatTime(v.last_seen);
    ui.ipAlerts.textContent = formatCount(v.alerts);
    ui.ipEvents.textContent = formatCount(v.events);
    const where = [v.country, v.asn ? 'AS' + v.asn + (v.as_name ? ' ' + v.as_name : '') : '', v.range].filter(Boolean).join(' · ');
    ui.ipSubtitle.textContent = v.alerts ? where : 'No retained alerts from this IP' + (v.raw_since ? ' since ' + formatTime(v.raw_since) : '') + '.';
    ui.ipScenarios.replaceChildren(rankedList('scenario', v.scenarios, v.alerts, null));
    ui.ipMachines.replaceChildren(rankedList('machine', v.machines, v.alerts, null));
    ui.ipDecisionCount.textContent = v.decisions.length ? (v.decisions.length >= 50 ? 'latest 50' : v.decisions.length + ' shown') : '';
    ui.ipDecisions.replaceChildren(...decisionRows(v.decisions));
    ui.ipMessage.textContent = '';
    // Its timeline spans first to last sighting, at least a day.
    const now = Date.now();
    const range = v.alerts ? resolveRange({start: Math.min(v.first_seen, now - 86400000), end: now}, now, 0) : resolveRange({hours: 24}, now, 0);
    const params = apiParams({include: {ip: [ip]}, exclude: {}, q: ''}, range, -new Date().getTimezoneOffset());
    const timeline = await get('timeline?' + params, controller.signal);
    ui.ipRange.textContent = formatTime(range.start) + ' to now';
    state.charts.ip = drawTimeline(state.charts.ip, 'cs-ip-timeline', timeline, range, null);
  } catch (e) {
    if (!isAbort(e)) showError(ui.ipMessage, 'This IP could not be loaded: ' + e.message, loadIPView);
  }
}
