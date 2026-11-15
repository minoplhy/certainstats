import { el, timeAgo, formatCount, formatTime } from '../format.js';

// Rows of the Machines and bouncers popup, laid out like the Management page:
// a summary line that opens to everything the component reported. Rows are
// built once and updated in place on every refresh, so an open row stays open.

const CHEVRON = '<svg class="mgmt-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m9 6 6 6-6 6"/></svg>';
const BADGE = {alert: ['badge-offline', 'Alert'], caution: ['badge-warn', 'Caution'], never: ['badge-warn', 'Never seen'], unknown: ['', 'Unknown'],
  ok: ['badge-online', 'OK'], muted: ['', 'Muted'], self: ['', "Collector's login"]};
export const ORDER = {alert: 0, caution: 1, never: 2, unknown: 3, ok: 4, muted: 5, self: 6};

export function setText(node, text) {
  if (node.textContent !== text) node.textContent = text;
}

function setClass(node, className) {
  if (node.className !== className) node.className = className;
}

// place keeps container's children in order, moving a node only when out of place.
export function place(container, nodes) {
  nodes.forEach((node, i) => {
    if (container.children[i] !== node) container.insertBefore(node, container.children[i] || null);
  });
  while (container.children.length > nodes.length) container.lastElementChild.remove();
}

function setBadge(node, state) {
  const [cls, text] = BADGE[state] || ['', state];
  setClass(node, 'badge ' + cls);
  setText(node, text);
}

// componentText says, in words, where one machine or bouncer stands.
export function componentText(c) {
  if (c.state === 'alert') return c.firing.map(f => f.rule).join(', ') + ' is firing' + (c.findings.length ? ': ' + c.findings.map(f => f.detail).join('. ') : '');
  if (c.state === 'caution') return c.findings.map(f => f.detail).join('. ');
  if (c.state === 'never') return c.kind === 'machine' ? 'Registered, never sent a heartbeat' : 'Registered, never pulled decisions';
  if (c.state === 'unknown') return 'Not judged: its collector is not reporting';
  if (c.state === 'muted') return 'Muted';
  if (c.state === 'self') return "The collector's own login, which never sends a heartbeat";
  if (c.revoked) return 'Revoked';
  if (c.kind === 'machine' && !c.validated) return 'Not validated';
  return 'Healthy';
}

const NOTIFIED = {pending: 'sending', queued: 'sending', success: 'delivered', failed: 'not delivered', unknown: 'outcome unknown', skipped: 'skipped'};

// facts manages a dl of label/value pairs, updated in place by key.
function facts() {
  const dl = el('dl', 'cred-list');
  const pairs = new Map();
  return {
    dl,
    // set records one fact for this round; value null hides it.
    update(list) {
      const nodes = [];
      for (const [key, label, value, mono] of list) {
        if (value === null || value === undefined || value === '') continue;
        let p = pairs.get(key);
        if (!p) {
          p = {row: el('div', 'cred-row'), dt: el('dt'), dd: el('dd', 'cred-value')};
          p.row.append(p.dt, p.dd);
          pairs.set(key, p);
        }
        setText(p.dt, label);
        setClass(p.dd, 'cred-value' + (mono ? ' mono' : ''));
        setText(p.dd, String(value));
        nodes.push(p.row);
      }
      for (const [key, p] of pairs) if (!nodes.includes(p.row)) pairs.delete(key);
      place(dl, nodes);
    }
  };
}

function seen(ms) {
  return ms ? formatTime(ms) + ' (' + timeAgo(ms) + ')' : 'Never';
}

// held lists a bouncer's decisions by origin next to the Local API's count.
function heldText(c) {
  if (!c.held) return null;
  const origins = Object.keys(c.held).sort();
  if (!origins.length) return 'None in its last metrics report';
  return origins.map(o => o + ': ' + formatCount(c.held[o]) + ' of ' + formatCount(c.active?.[o] || 0) + ' active').join('\n');
}

function componentFacts(c, many) {
  const list = [];
  if (many) list.push(['collector', 'Collector', c.collector]);
  list.push(['status', 'Status', componentText(c)]);
  if (c.kind === 'machine') {
    list.push(['ip', 'Address', c.ip, true], ['version', 'Version', c.version, true], ['os', 'System', c.os],
      ['validated', 'Validated', c.validated ? 'Yes' : 'No'], ['seen', 'Last heartbeat', seen(c.last_seen_ms), true]);
  } else {
    list.push(['type', 'Type', c.type], ['version', 'Version', c.version, true], ['ip', 'Address', c.ip, true],
      ['revoked', 'Revoked', c.revoked ? 'Yes' : null], ['seen', 'Last pull', seen(c.last_seen_ms), true],
      ['metrics', 'Usage metrics', c.metrics_ms ? seen(c.metrics_ms) : 'Not sent by this bouncer', !!c.metrics_ms],
      ['held', 'Decisions held', heldText(c), true],
      ['dropped', 'Packets dropped', c.metrics_ms ? formatCount(c.dropped) : null, true],
      ['processed', 'Packets processed', c.metrics_ms ? formatCount(c.processed) : null, true]);
  }
  for (const f of c.firing) list.push(['rule:' + f.alert_id, 'Alert rule', f.rule + ', notification ' + (NOTIFIED[f.notified] || f.notified) + (f.error ? ': ' + f.error : '')]);
  return list;
}

// muteButton is one listener for the row's life; it reads the row's current component.
function muteButton(rec, onMute) {
  const b = el('button', 'btn btn-ghost btn-sm');
  b.type = 'button';
  b.addEventListener('click', () => onMute(rec.item, b));
  return b;
}

function updateMute(b, c) {
  setText(b, c.muted ? 'Unmute' : 'Mute');
  const label = (c.muted ? 'Unmute ' : 'Mute ') + c.name;
  if (b.getAttribute('aria-label') !== label) b.setAttribute('aria-label', label);
  b.hidden = c.state === 'self';
}

function summaryParts(rec) {
  const summary = el('summary', 'mgmt-summary');
  rec.name = el('span', 'mgmt-name mono');
  rec.badge = el('span');
  rec.meta = el('span', 'cs-comp-meta muted mono');
  summary.append(rec.name, rec.badge, rec.meta);
  summary.insertAdjacentHTML('beforeend', CHEVRON);
  return summary;
}

// createRow builds a machine or a bouncer with a key of its own.
export function createRow(onMute) {
  const rec = {item: null};
  rec.details = el('details', 'mgmt-item cs-comp-row');
  const panel = el('div', 'mgmt-panel');
  rec.facts = facts();
  rec.mute = muteButton(rec, onMute);
  const actions = el('div', 'mgmt-actions');
  actions.append(rec.mute);
  panel.append(rec.facts.dl, actions);
  rec.details.append(summaryParts(rec), panel);
  return rec;
}

export function updateRow(rec, c, many) {
  rec.item = c;
  rec.details.dataset.state = c.state;
  setText(rec.name, c.name);
  setBadge(rec.badge, c.state);
  const when = (c.kind === 'machine' ? 'heartbeat ' : 'pulled ') + timeAgo(c.last_seen_ms);
  setText(rec.meta, [many ? c.collector : '', c.kind === 'bouncer' ? c.type : '', c.version, when].filter(Boolean).join(' · '));
  rec.facts.update(componentFacts(c, many));
  updateMute(rec.mute, c);
}

// A group is one shared bouncer key: its entries (name@ip) side by side.
export function createGroup(onMute) {
  const rec = {entries: new Map()};
  rec.details = el('details', 'mgmt-item cs-comp-row');
  rec.panel = el('div', 'mgmt-panel cs-comp-entries');
  rec.details.append(summaryParts(rec), rec.panel);
  rec.onMute = onMute;
  return rec;
}

export function updateGroup(rec, group, many) {
  const worst = group.entries.reduce((w, c) => (ORDER[c.state] ?? 9) < (ORDER[w] ?? 9) ? c.state : w, 'self');
  rec.details.dataset.state = worst;
  setText(rec.name, group.name);
  setBadge(rec.badge, worst);
  const first = group.entries[0];
  setText(rec.meta, [many ? first.collector : '', first.type, group.entries.length + ' addresses on one key'].filter(Boolean).join(' · '));
  const nodes = group.entries.map(c => {
    let e = rec.entries.get(c.id);
    if (!e) {
      e = {item: c, box: el('div', 'cs-comp-entry'), head: el('div', 'cs-comp-entry-head'), name: el('span', 'mono'), badge: el('span'), facts: facts()};
      e.mute = muteButton(e, rec.onMute);
      e.head.append(e.name, e.badge, e.mute);
      e.box.append(e.head, e.facts.dl);
      rec.entries.set(c.id, e);
    }
    e.item = c;
    setText(e.name, c.name);
    setBadge(e.badge, c.state);
    e.facts.update(componentFacts(c, false));
    updateMute(e.mute, c);
    return e.box;
  });
  for (const [id, e] of rec.entries) if (!group.entries.some(c => c.id === id)) rec.entries.delete(id);
  place(rec.panel, nodes);
}
