/* Collectors dialog: list, add, rename, reset and remove collectors, and turn their
   types on or off. Any panel page can include the collectors_modal partial and this
   script; pages follow changes through CertainStatsCollectors.subscribe. */

import { api, el, copy, typeState, timeAgo } from './shared.js';
import { createList } from './list.js';
import { renderSetup } from './setup.js';

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
  ctx?.list.render();
  notify();
  return state.items;
}

function step(name) {
  ctx.root.querySelectorAll('[data-collectors-step]').forEach(node => { node.hidden = node.dataset.collectorsStep !== name; });
}

function showToken(collector, token, types) {
  ctx.ui.setupTitle.textContent = (types ? 'Collector created: ' : 'New token for ') + collector.name;
  ctx.ui.token.textContent = token;
  ctx.ui.copyToken.onclick = () => copy(token, 'Token');
  renderSetup(ctx.ui.setup, token, types || collector.types.filter(t => t.enabled).map(t => t.type));
  step('setup');
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

function mount(root) {
  const q = name => root.querySelector('[data-collectors-' + name + ']');
  ctx = {
    root, state, refresh,
    ui: {
      message: q('message'), list: q('list'), add: q('add'), name: q('name'), node: q('node'), types: q('types'),
      formMessage: q('form-message'), back: q('back'), create: q('create'), token: q('token'), copyToken: q('copy-token'),
      setup: q('setup'), setupTitle: q('setup-title'), done: q('done')
    }
  };
  ctx.showToken = (c, token) => showToken(c, token);
  ctx.list = createList(ctx);
  const form = root.querySelector('form[data-collectors-step="create"]');

  ctx.ui.add.addEventListener('click', () => open({create: true}));
  ctx.ui.back.addEventListener('click', () => step('list'));
  ctx.ui.done.addEventListener('click', () => { step('list'); ctx.ui.token.textContent = ''; });
  root.addEventListener('close', () => { step('list'); ctx.ui.token.textContent = ''; });
  form.addEventListener('submit', async e => {
    e.preventDefault();
    const types = [...ctx.ui.types.querySelectorAll('input:checked')].map(i => i.value);
    ctx.ui.formMessage.textContent = '';
    ctx.ui.create.disabled = true;
    try {
      const created = await api('collectors', {method: 'POST', body: JSON.stringify({name: ctx.ui.name.value, agent_id: ctx.ui.node.value, types})});
      showToken(created, created.token, types);
      refresh().catch(() => {});
    } catch (err) {
      ctx.ui.formMessage.textContent = err.message;
    } finally {
      ctx.ui.create.disabled = false;
    }
  });
}

// open shows the dialog; {create: true, type: 'crowdsec'} goes straight to the add form.
async function open(opts = {}) {
  if (!ctx) return;
  window.CertainStatsModal?.open(ctx.root);
  if (opts.create) {
    if (!state.loaded) await refresh().catch(() => {});
    ctx.ui.name.value = '';
    ctx.ui.node.value = '';
    ctx.ui.formMessage.textContent = '';
    renderTypeOptions(opts.type);
    step('create');
    ctx.ui.name.focus();
    return;
  }
  step('list');
  ctx.ui.message.textContent = state.loaded ? '' : 'Loading collectors…';
  try {
    await refresh();
    ctx.ui.message.textContent = '';
  } catch (e) {
    ctx.ui.message.textContent = 'Collectors could not be loaded: ' + e.message;
  }
}

function subscribe(fn) {
  listeners.add(fn);
  if (state.loaded) fn(state.items, state.types);
  return () => listeners.delete(fn);
}

window.CertainStatsCollectors = { open, refresh, subscribe, typeState, timeAgo };

function init() {
  const root = document.querySelector('[data-collectors-modal]');
  if (root) mount(root);
}
if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
else init();
