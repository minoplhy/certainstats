/* Collectors: the /collectors page shows each collector type (leading to its page
   at /collectors/<type>) and lists, adds, renames, resets and removes collectors
   and turns their types on or off. Type pages load this script only for the
   data: they follow changes through CertainStatsCollectors.subscribe and link to
   /collectors to manage them. */

import { api, el, panelPath, typeState, timeAgo } from './shared.js';
import { createList } from './list.js';
import { createTypes } from './types.js';

const listeners = new Set();
const state = { items: [], types: [], loaded: false };
let ctx = null;

function notify() {
  for (const fn of listeners) {
    try { fn(state.items, state.types); } catch (e) { console.error(e); }
  }
}

async function refresh() {
  const data = await api('collectors');
  state.items = data.items || [];
  state.types = data.types || [];
  state.loaded = true;
  if (ctx) {
    ctx.list.render();
    ctx.types.render();
    const n = state.items.length;
    const online = state.items.filter(c => c.connected).length;
    ctx.ui.count.textContent = (n === 1 ? '1 collector' : n + ' collectors') + (n ? ' · ' + online + ' connected' : '');
  }
  notify();
  return state.items;
}

// showInstall opens the shared install modal (reinstall_modal) with the steps the
// server builds for this collector, as agents' Install does.
function showInstall(collector) {
  const modal = document.getElementById('reinstall-modal');
  const container = document.getElementById('reinstall-instructions-container');
  const renderer = window.CertainStatsProvisionRenderer;
  if (!modal || !container || !renderer?.loadInstructions) return;
  const title = document.getElementById('reinstall-modal-title');
  if (title) title.textContent = 'Install ' + collector.name;
  renderer.loadInstructions('/api/collectors/' + encodeURIComponent(collector.collector_id) + '/install', container, 'installation', 'collector');
  window.CertainStatsModal?.open(modal);
}

function renderTypeOptions(preselect) {
  ctx.ui.types.replaceChildren();
  for (const t of state.types) {
    const label = el('label', 'toggle-pill');
    const input = el('input');
    input.type = 'checkbox';
    input.value = t.type;
    input.checked = preselect ? preselect === t.type : state.types.length === 1;
    label.append(input, el('span', '', t.label));
    ctx.ui.types.append(label);
  }
}

// openAdd shows the add dialog, with type preselected when given.
async function openAdd(type) {
  if (!state.loaded) await refresh().catch(() => {});
  const ui = ctx.ui;
  ui.name.value = '';
  ui.node.value = '';
  ui.formMessage.textContent = '';
  renderTypeOptions(type);
  window.CertainStatsModal?.open(ui.dialog);
  ui.name.focus();
}

function mount(root) {
  const q = name => document.querySelector('[data-collectors-' + name + ']');
  ctx = {
    root, state, refresh, showInstall, openAdd,
    ui: {
      count: q('count'), message: q('message'), list: q('list'), typeList: q('type-list'), add: q('add'), search: q('search'), dialog: q('add-modal'),
      form: q('form'), name: q('name'), node: q('node'), types: q('types'), formMessage: q('form-message'), create: q('create')
    }
  };
  ctx.list = createList(ctx);
  ctx.types = createTypes(ctx);
  const ui = ctx.ui;

  ui.add.addEventListener('click', () => openAdd());
  ui.search?.addEventListener('input', () => ctx.list.search(ui.search.value));
  ui.form.addEventListener('submit', async e => {
    e.preventDefault();
    const types = [...ui.types.querySelectorAll('input:checked')].map(i => i.value);
    ui.formMessage.textContent = '';
    ui.create.disabled = true;
    try {
      const created = await api('collectors', {method: 'POST', body: JSON.stringify({name: ui.name.value, agent_id: ui.node.value, types})});
      window.CertainStatsModal?.close(ui.dialog);
      refresh().catch(() => {});
      showInstall(created);
    } catch (err) {
      ui.formMessage.textContent = err.message;
    } finally {
      ui.create.disabled = false;
    }
  });

  load();
  // Collector status changes on its own; refresh it while the page is visible.
  setInterval(() => { if (!document.hidden && !ctx.list.busy()) refresh().catch(() => {}); }, 30000);

  // ?add=crowdsec opens the add form with that type ticked, then leaves the URL clean.
  const params = new URLSearchParams(window.location.search);
  if (params.has('add')) {
    const type = params.get('add') || '';
    params.delete('add');
    const rest = params.toString();
    history.replaceState(null, '', window.location.pathname + (rest ? '?' + rest : ''));
    openAdd(type);
  }
}

async function load() {
  ctx.ui.message.textContent = state.loaded ? '' : 'Loading collectors…';
  try {
    await refresh();
    ctx.ui.message.textContent = '';
  } catch (e) {
    ctx.ui.message.textContent = 'Collectors could not be loaded: ' + e.message;
  }
}

// open goes to the Collectors page; {create: true, type} opens its add form there.
function open(opts = {}) {
  if (ctx) {
    if (opts.create) openAdd(opts.type);
    return;
  }
  const add = opts.create ? '?add=' + encodeURIComponent(opts.type || '') : '';
  window.location.assign(panelPath() + '/collectors' + add);
}

function subscribe(fn) {
  listeners.add(fn);
  if (state.loaded) fn(state.items, state.types);
  return () => listeners.delete(fn);
}

window.CertainStatsCollectors = { open, refresh, subscribe, typeState, timeAgo };

function init() {
  const root = document.querySelector('[data-collectors-page]');
  if (root) mount(root);
}
if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
else init();
