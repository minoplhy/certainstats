import { state } from '../state.js';
import { begin, get, send, isAbort, showError } from '../api.js';
import { renderHero } from './hero.js';

// Machines and bouncers registered with each collector's Local API, as read
// from CrowdSec's database on hosts that switched that on (CROWDSEC_HEALTH=true).

const ORDER = {alert: 0, caution: 1, never: 2, unknown: 3, ok: 4, muted: 5, self: 6};
const DOT = {alert: 'offline', caution: 'warn', never: 'warn', ok: 'online'};

function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined && text !== null) node.textContent = String(text);
  return node;
}

function ago(ms, now = Date.now()) {
  if (!ms) return 'never';
  const s = Math.max(0, Math.round((now - ms) / 1000));
  if (s < 60) return 'just now';
  if (s < 3600) return Math.floor(s / 60) + 'm ago';
  if (s < 86400) return Math.floor(s / 3600) + 'h ago';
  return Math.floor(s / 86400) + 'd ago';
}

// componentText says, in words, where one machine or bouncer stands.
export function componentText(c) {
  if (c.state === 'alert' || c.state === 'caution') return c.findings.map(f => f.detail).join('. ');
  if (c.state === 'never') return c.kind === 'machine' ? 'Registered, never sent a heartbeat' : 'Registered, never pulled decisions';
  if (c.state === 'unknown') return 'Not judged: its collector is not reporting';
  if (c.state === 'muted') return 'Muted';
  if (c.state === 'self') return "The collector's own login";
  if (c.revoked) return 'Revoked';
  if (c.kind === 'machine' && !c.validated) return 'Not validated';
  return 'Healthy';
}

function facts(c, many) {
  const list = [];
  if (many) list.push(c.collector);
  list.push((c.kind === 'machine' ? 'heartbeat ' : 'pulled ') + ago(c.last_seen_ms));
  // A bouncer that reports usage metrics says what its firewall really holds.
  if (c.held !== null && c.held !== undefined) list.push('holds ' + c.held + ' of ' + c.active + ' decisions');
  if (c.dropped) list.push(c.dropped + ' packets dropped');
  if (c.type) list.push(c.type);
  if (c.version) list.push(c.version);
  if (c.ip) list.push(c.ip);
  return list;
}

function row(c, many) {
  const item = el('li', 'cs-comp-row is-' + c.state);
  const dot = el('span', 'status-dot ' + (DOT[c.state] || ''));
  dot.setAttribute('aria-hidden', 'true');
  const name = el('span', 'cs-comp-name mono', c.name);
  name.title = c.name;
  const text = el('span', 'cs-comp-state', componentText(c));
  const failed = c.findings.find(f => f.notify === 'failed' || (f.notify === 'pending' && f.notify_error));
  if (failed) text.append(el('span', 'cs-comp-notify', ' Notification not delivered: ' + failed.notify_error));
  const meta = el('span', 'cs-comp-facts muted', facts(c, many).join(' · '));
  const button = el('button', 'btn btn-ghost btn-sm', c.muted ? 'Unmute' : 'Mute');
  button.type = 'button';
  button.hidden = c.state === 'self';
  button.setAttribute('aria-label', (c.muted ? 'Unmute ' : 'Mute ') + c.name);
  button.addEventListener('click', async () => {
    button.disabled = true;
    try {
      await send(c.muted ? 'DELETE' : 'PUT', 'components/' + c.id + '/mute');
      await loadComponents();
    } catch (e) {
      button.disabled = false;
      window.CertainStatsTelemetry?.showToast(e.message, false);
    }
  });
  item.append(dot, name, el('span', 'badge', c.kind === 'machine' ? 'Machine' : 'Bouncer'), text, meta, button);
  return item;
}

function note(c) {
  if (c.health === 'error') return c.name + ": CrowdSec's database could not be read: " + c.error;
  if (c.health === 'off') return c.name + ': machine and bouncer health is not switched on (CROWDSEC_HEALTH=true on its host).';
  const missing = c.unavailable.map(u => u.label + ' (needs ' + u.needs + ')');
  return missing.length ? c.name + ': not available, the host does not let these be read: ' + missing.join('; ') + '.' : '';
}

export function renderComponents() {
  const ui = state.ui;
  const {collectors, items} = state.components;
  // Nothing is shown until some collector has health switched on.
  const active = collectors.some(c => c.health === 'on' || c.health === 'error') || items.length > 0;
  ui.components.hidden = !active;
  if (!active) return;
  const machines = items.filter(c => c.kind === 'machine').length, bouncers = items.length - machines;
  const attention = items.filter(c => c.state === 'alert' || c.state === 'caution').length;
  ui.componentsCount.textContent = (machines === 1 ? '1 machine' : machines + ' machines') + ', ' + (bouncers === 1 ? '1 bouncer' : bouncers + ' bouncers') +
    (attention ? ' · ' + attention + (attention === 1 ? ' needs' : ' need') + ' a look' : '');
  ui.componentsNotes.replaceChildren(...collectors.map(note).filter(Boolean).map(text => el('li', '', text)));
  ui.componentsNotes.hidden = !ui.componentsNotes.children.length;
  const many = collectors.length > 1;
  const sorted = [...items].sort((a, b) => (ORDER[a.state] ?? 9) - (ORDER[b.state] ?? 9) || a.kind.localeCompare(b.kind) * -1 || a.name.localeCompare(b.name));
  ui.componentsList.replaceChildren(...sorted.map(c => row(c, many)));
}

// loadComponents reads machine and bouncer health; it is not tied to the alert filters or range.
export async function loadComponents() {
  const controller = begin('components');
  try {
    const data = await get('components', controller.signal);
    state.components = {collectors: data.collectors || [], items: data.items || []};
    state.ui.componentsMessage.textContent = '';
    renderComponents();
    renderHero();
  } catch (e) {
    if (isAbort(e)) return;
    // Keep what is shown; only say that it could not be refreshed.
    if (!state.ui.components.hidden) showError(state.ui.componentsMessage, 'Machines and bouncers could not be loaded: ' + e.message, loadComponents);
  }
}
