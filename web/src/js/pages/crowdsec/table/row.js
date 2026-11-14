import { el, iconButton, formatCount, formatTime, untilText, copyText } from '../format.js';
import { get, isAbort } from '../api.js';

// One cell: its text plus copy and filter buttons, shown on hover and focus.
function cell(className, text, field, value, actions) {
  const node = el('span', 'cs-cell ' + className);
  node.setAttribute('role', 'cell');
  const label = el('span', 'cs-cell-text', text);
  label.title = text;
  node.append(label);
  if (value) {
    const tools = el('span', 'cs-cell-tools');
    tools.append(iconButton('copy', 'Copy ' + value, () => copyText(value)), iconButton('filter', 'Show only ' + value, () => actions.filter(field, value)));
    node.append(tools);
  }
  return node;
}

function actionBadge(action) {
  const cls = action === 'none' ? 'badge' : action.includes('ban') ? 'badge badge-offline' : 'badge badge-warn';
  return el('span', cls, action === 'none' ? 'no decision' : action);
}

function fact(list, label, value, mono) {
  const wrap = el('div');
  wrap.append(el('dt', 'muted', label), el('dd', mono ? 'mono' : '', value || '–'));
  list.append(wrap);
}

async function loadDetails(alert, body) {
  body.replaceChildren(el('p', 'muted', 'Loading decisions…'));
  try {
    const full = await get('alerts/' + alert.id);
    const facts = el('dl', 'network-row-details cs-details');
    fact(facts, 'Started', formatTime(full.started_at, true), true);
    fact(facts, 'Stopped', formatTime(full.stopped_at, true), true);
    fact(facts, 'Range', full.range, true);
    fact(facts, 'Network', full.asn ? 'AS' + full.asn + (full.as_name ? ' ' + full.as_name : '') : '');
    fact(facts, 'Collector', full.collector || full.collector_id);
    fact(facts, 'Scope', full.scope ? full.scope + ' ' + full.value : '', true);
    if (full.simulated) fact(facts, 'Mode', 'Simulation: no decision was enforced');
    if (full.message) fact(facts, 'Message', full.message);
    const decisions = el('div', 'cs-decision-list');
    decisions.append(el('h4', 'incident-event-heading', full.decisions.length ? 'Decisions' : 'No decisions were taken'));
    for (const d of full.decisions) {
      const line = el('div', 'cs-decision');
      line.append(el('span', 'badge ' + (d.type === 'ban' ? 'badge-offline' : 'badge-warn'), d.type || 'decision'),
        el('span', 'mono', d.scope + ' ' + d.value), el('span', 'muted', d.origin),
        el('span', 'mono', (d.duration ? d.duration + ' · ' : '') + untilText(d.until)));
      decisions.append(line);
    }
    body.replaceChildren(facts, decisions);
  } catch (e) {
    if (!isAbort(e)) body.replaceChildren(el('p', 'cs-status', 'Decisions could not be loaded: ' + e.message));
  }
}

// alertRow renders one alert as an expandable row.
export function alertRow(alert, actions) {
  const row = el('details', 'cs-row network-row');
  const sum = el('summary', 'cs-row-main');
  sum.setAttribute('role', 'row');
  sum.append(
    cell('cs-col-time mono', formatTime(alert.started_at), null, null, actions),
    cell('cs-col-ip mono', alert.ip || alert.value || '–', 'ip', alert.ip, actions),
    cell('cs-col-scenario', alert.scenario, 'scenario', alert.scenario, actions),
    cell('cs-col-country mono', alert.country || '–', 'country', alert.country, actions),
    cell('cs-col-machine', alert.machine, 'machine', alert.machine, actions));
  const action = el('span', 'cs-cell cs-col-action');
  action.setAttribute('role', 'cell');
  action.append(actionBadge(alert.action));
  if (alert.simulated) action.append(el('span', 'badge', 'simulated'));
  sum.append(action, cell('cs-col-events mono', formatCount(alert.events), null, null, actions));
  const ipText = sum.querySelector('.cs-col-ip .cs-cell-text');
  if (alert.ip && ipText) {
    const link = el('button', 'cs-ip-link mono', alert.ip);
    link.type = 'button';
    link.title = 'Open everything about ' + alert.ip;
    link.addEventListener('click', e => { e.preventDefault(); e.stopPropagation(); actions.openIP(alert.ip); });
    ipText.replaceWith(link);
  }
  const body = el('div', 'cs-row-body');
  row.append(sum, body);
  let loaded = false;
  row.addEventListener('toggle', () => {
    if (row.open && !loaded) { loaded = true; loadDetails(alert, body); }
  });
  return row;
}
