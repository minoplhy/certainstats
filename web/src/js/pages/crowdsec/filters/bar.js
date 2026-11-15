import { state } from '../state.js';
import { el, iconButton } from '../format.js';
import { FIELD_LABELS, FIELDS, addFilter, removeFilter } from '../url/state.js';

// renderChips shows each active filter as a removable chip.
export function renderChips(onChange) {
  const ui = state.ui;
  const v = state.view;
  ui.chips.replaceChildren();
  const chip = (field, value, exclude) => {
    const node = el('span', 'cs-chip' + (exclude ? ' is-exclude' : ''));
    node.append(el('span', 'cs-chip-field', FIELD_LABELS[field]), el('span', 'cs-chip-op', exclude ? 'is not' : 'is'), el('span', 'cs-chip-value mono', value));
    const remove = iconButton('exclude', 'Remove filter ' + FIELD_LABELS[field] + (exclude ? ' is not ' : ' is ') + value, () => {
      removeFilter(v, field, value, exclude);
      onChange();
    });
    remove.classList.add('cs-chip-remove');
    node.append(remove);
    return node;
  };
  for (const f of FIELDS) {
    for (const value of v.include[f] || []) ui.chips.append(chip(f, value, false));
    for (const value of v.exclude[f] || []) ui.chips.append(chip(f, value, true));
  }
  if (ui.chips.children.length > 1) {
    const clear = el('button', 'btn btn-ghost btn-sm', 'Clear filters');
    clear.type = 'button';
    clear.addEventListener('click', () => { v.include = {}; v.exclude = {}; v.page = 1; onChange(); });
    ui.chips.append(clear);
  }
  ui.chips.hidden = ui.chips.children.length === 0;
}

const PLACEHOLDERS = {
  ip: 'e.g. 203.0.113.7', range: 'e.g. 203.0.113.0/24', scenario: 'e.g. crowdsecurity/ssh-bf', country: 'e.g. NL',
  machine: 'Machine name', asn: 'e.g. 64500', action: 'ban, captcha or none', collector: 'Collector name'
};

// bindPopover opens and closes the "+ Filter" form: outside clicks and Escape close it.
function bindPopover() {
  const ui = state.ui;
  const pop = ui.filterPopover;
  const setOpen = open => {
    pop.hidden = !open;
    ui.filterToggle.setAttribute('aria-expanded', String(open));
    if (open) ui.filterValue.focus();
  };
  ui.filterToggle.addEventListener('click', () => setOpen(pop.hidden));
  pop.querySelector('[data-cs-filter-cancel]')?.addEventListener('click', () => { setOpen(false); ui.filterToggle.focus(); });
  pop.addEventListener('keydown', e => {
    if (e.key !== 'Escape') return;
    e.preventDefault();
    setOpen(false);
    ui.filterToggle.focus();
  });
  document.addEventListener('click', e => {
    if (!pop.hidden && !pop.contains(e.target) && !ui.filterToggle.contains(e.target)) setOpen(false);
  });
  const placeholder = () => { ui.filterValue.placeholder = PLACEHOLDERS[ui.filterField.value] || 'Value'; };
  ui.filterField.addEventListener('change', placeholder);
  placeholder();
  return setOpen;
}

// bindFilters wires search, the add-filter popover and the time picker (with "All").
export function bindFilters(onChange) {
  const ui = state.ui;
  const v = () => state.view;
  ui.search.value = v().q;
  let timer = null;
  ui.search.addEventListener('input', () => {
    clearTimeout(timer);
    timer = setTimeout(() => {
      if (v().q === ui.search.value.trim()) return;
      v().q = ui.search.value.trim();
      v().page = 1;
      onChange();
    }, 350);
  });
  const setOpen = bindPopover();
  ui.filterPopover.addEventListener('submit', event => {
    event.preventDefault();
    const value = ui.filterValue.value.trim();
    if (!value) return;
    const exclude = ui.filterPopover.querySelector('input[name="cs-filter-op"]:checked')?.value === 'not';
    addFilter(v(), ui.filterField.value, value, exclude);
    ui.filterValue.value = '';
    setOpen(false);
    onChange();
  });

  state.picker = window.CertainStatsTelemetry.initCustomTimePicker(ui.timePicker, {
    storageKey: 'certainstats_crowdsec_hours',
    activeHours: v().hours,
    compact: true,
    allTime: {
      active: v().all,
      // "All" starts at the oldest data, which load() looks up.
      onSelect: () => {
        const view = v();
        view.all = true;
        view.start = null;
        view.end = null;
        view.page = 1;
        onChange();
      }
    },
    onApply: ({hours, customRange}) => {
      const view = v();
      view.all = false;
      view.hours = hours;
      view.start = customRange ? customRange.start : null;
      view.end = customRange ? customRange.end : null;
      view.page = 1;
      onChange();
    }
  });
  if (v().start && v().end) state.picker.setState({hours: v().hours, customRange: {start: v().start, end: v().end}, all: v().all});
}

// syncRangeControls keeps the picker's label in step with the view (Back, zoom).
export function syncRangeControls() {
  const p = state.picker;
  if (!p) return;
  const v = state.view;
  const cur = p.getState();
  const custom = v.start && v.end ? {start: v.start, end: v.end} : null;
  const same = cur.all === v.all && cur.hours === v.hours &&
    (cur.customRange ? custom && cur.customRange.start === custom.start && cur.customRange.end === custom.end : !custom);
  if (!same) p.setState({hours: v.hours, customRange: custom, all: v.all});
}
