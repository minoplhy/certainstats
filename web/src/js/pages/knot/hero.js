import { state } from './state.js';
import { el, formatCount, formatRate, formatShare, headline } from './format.js';

export function renderHero(totals) {
  const ui = state.ui;
  const o = state.overview;
  const servers = o?.servers || [];
  const connected = servers.filter(s => s.connected).length;
  ui.serverCount.textContent = servers.length + (servers.length === 1 ? ' server' : ' servers') + (servers.length ? ', ' + connected + ' connected' : '');
  ui.headline.textContent = headline(o, totals, state.view);
  const t = totals || {};
  const ready = totals && !totals.collecting;
  ui.statRequests.textContent = ready ? formatCount(t.requests) : '–';
  ui.statQps.textContent = ready ? formatRate(t.qps) : '–';
  ui.statNxdomain.textContent = ready ? formatShare(t.nxdomain, t.responses) : '–';
  ui.statServfail.textContent = ready ? formatShare(t.servfail, t.responses) : '–';
  ui.statTransfers.textContent = ready ? formatCount(t.transfers) : '–';
  let note = '';
  if (totals?.collecting) note = 'Counting starts from the collector\'s first reading; Knot\'s totals from before that are not shown.';
  else if (totals && totals.from > totals.start) note = 'Counted since ' + new Date(totals.from).toLocaleString() + ', when the first reading in this range arrived.';
  ui.statNote.textContent = note;
}

export function renderChecks() {
  const ui = state.ui;
  const checks = state.overview?.checks || [];
  ui.checks.hidden = !checks.length;
  ui.checkList.replaceChildren(...checks.map(c => {
    const li = el('li', 'kn-check is-' + c.level);
    const d = el('span', 'status-dot' + (c.level === 'bad' ? ' offline' : ' warn'));
    d.setAttribute('aria-hidden', 'true');
    li.append(d, el('span', 'kn-check-text', c.message));
    if (c.server && !c.message.includes(c.server)) li.append(el('span', 'muted kn-check-server', c.server));
    return li;
  }));
}
