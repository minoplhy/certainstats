import { state } from '../state.js';
import { begin, get, send, isAbort, showError } from '../api.js';
import { el } from '../format.js';
import { renderHero } from './hero.js';
import { ORDER, setText, place, createRow, updateRow, createGroup, updateGroup, componentText } from './component_rows.js';

export { componentText };

// The Machines and bouncers popup: what each collector's Local API knows about
// its machines and bouncers, read from CrowdSec's database on hosts that
// switched that on (CROWDSEC_HEALTH=true). Machines and bouncers are separate
// lists; bouncers sharing one key (name@ip) are one row with an entry per address.

const machineRows = new Map();  // component id -> row
const bouncerRows = new Map();  // "b:<id>" or "g:<collector>/<base>" -> row or group
const notes = new Map();        // collector id -> <li>

async function onMute(c, button) {
  button.disabled = true;
  try {
    await send(c.muted ? 'DELETE' : 'PUT', 'components/' + c.id + '/mute');
    await loadComponents();
  } catch (e) {
    window.CertainStatsTelemetry?.showToast(e.message, false);
  } finally {
    button.disabled = false;
  }
}

const byState = (a, b) => (ORDER[a.state] ?? 9) - (ORDER[b.state] ?? 9) || a.name.localeCompare(b.name);

function countText(list, noun) {
  const look = list.filter(c => c.state === 'alert' || c.state === 'caution').length;
  return list.length + ' ' + noun + (list.length === 1 ? '' : 's') + (look ? ' · ' + look + (look === 1 ? ' needs' : ' need') + ' a look' : '');
}

function renderMachines(items, many) {
  const ui = state.ui;
  const list = items.filter(c => c.kind === 'machine').sort(byState);
  const nodes = list.map(c => {
    let rec = machineRows.get(c.id);
    if (!rec) {
      rec = createRow(onMute);
      machineRows.set(c.id, rec);
    }
    updateRow(rec, c, many);
    return rec.details;
  });
  for (const [id] of machineRows) if (!list.some(c => c.id === id)) machineRows.delete(id);
  place(ui.machines, nodes);
  setText(ui.machinesCount, countText(list, 'machine'));
  ui.machinesEmpty.hidden = list.length > 0;
}

// bouncerUnits groups bouncers by collector and base name; a key used from one
// address only is a plain row.
function bouncerUnits(items) {
  const groups = new Map();
  for (const c of items.filter(i => i.kind === 'bouncer')) {
    const key = c.collector_id + '/' + c.group;
    if (!groups.has(key)) groups.set(key, {key, name: c.group, entries: []});
    groups.get(key).entries.push(c);
  }
  const units = [];
  for (const g of groups.values()) {
    g.entries.sort(byState);
    if (g.entries.length === 1 && g.entries[0].name === g.name) units.push({single: g.entries[0], state: g.entries[0].state, name: g.name});
    else units.push({group: g, state: g.entries[0].state, name: g.name});
  }
  return units.sort(byState);
}

function renderBouncers(items, many) {
  const ui = state.ui;
  const units = bouncerUnits(items);
  const used = new Set();
  const nodes = units.map(u => {
    const key = u.single ? 'b:' + u.single.id : 'g:' + u.group.key;
    used.add(key);
    let rec = bouncerRows.get(key);
    if (!rec) {
      rec = u.single ? createRow(onMute) : createGroup(onMute);
      bouncerRows.set(key, rec);
    }
    if (u.single) updateRow(rec, u.single, many);
    else updateGroup(rec, u.group, many);
    return rec.details;
  });
  for (const key of [...bouncerRows.keys()]) if (!used.has(key)) bouncerRows.delete(key);
  place(ui.bouncers, nodes);
  const all = items.filter(c => c.kind === 'bouncer');
  setText(ui.bouncersCount, countText(all, 'bouncer'));
  ui.bouncersEmpty.hidden = all.length > 0;
}

function noteText(c) {
  if (c.health === 'error') return c.name + ": CrowdSec's database could not be read: " + c.error;
  if (c.health === 'off') return c.name + ': machine and bouncer health is not switched on (CROWDSEC_HEALTH=true on its host).';
  const missing = c.unavailable.map(u => u.label + ' (needs ' + u.needs + ')');
  return missing.length ? c.name + ': not available, the host does not let these be read: ' + missing.join('; ') + '.' : '';
}

function renderNotes(collectors) {
  const ui = state.ui;
  const shown = [];
  for (const c of collectors) {
    const text = noteText(c);
    if (!text) continue;
    let li = notes.get(c.collector_id);
    if (!li) {
      li = el('li');
      notes.set(c.collector_id, li);
    }
    setText(li, text);
    shown.push(li);
  }
  for (const [id, li] of notes) if (!shown.includes(li)) notes.delete(id);
  place(ui.componentsNotes, shown);
  ui.componentsNotes.hidden = !shown.length;
}

export function renderComponents() {
  const ui = state.ui;
  const {collectors, items} = state.components;
  // Nothing is offered until some collector has health switched on.
  const active = collectors.some(c => c.health === 'on' || c.health === 'error') || items.length > 0;
  ui.componentsStat.hidden = !active;
  ui.componentsOpen.hidden = !active;
  const firing = items.filter(c => c.state === 'alert').length;
  const look = items.filter(c => c.state === 'alert' || c.state === 'caution').length;
  setText(ui.componentsCount, items.length + (look ? ' · ' + look + (look === 1 ? ' needs' : ' need') + ' a look' : ''));
  ui.componentsCount.classList.toggle('is-bad', firing > 0);
  if (!active) return;
  renderNotes(collectors);
  const many = collectors.length > 1;
  renderMachines(items, many);
  renderBouncers(items, many);
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
    showError(state.ui.componentsMessage, 'Machines and bouncers could not be loaded: ' + e.message, loadComponents);
  }
}

export function bindComponents() {
  state.ui.componentsOpen.addEventListener('click', () => window.CertainStatsModal?.open(state.ui.components));
}
