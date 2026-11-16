import { state } from '../state.js';
import { FIELD_LABELS, FIELDS, addFilter, removeFilter } from '../url/state.js';

// renderChips shows each active filter as a removable chip.
export function renderChips(onChange) {
  const v = state.view;
  const chips = [];
  for (const f of FIELDS) {
    for (const exclude of [false, true]) {
      for (const value of (exclude ? v.exclude : v.include)[f] || []) {
        chips.push({label: FIELD_LABELS[f], value, exclude, mono: true, onRemove: () => { removeFilter(v, f, value, exclude); onChange(); }});
      }
    }
  }
  window.CertainStatsTelemetry.renderFilterChips(state.ui.chips, chips, {onClear: () => { v.include = {}; v.exclude = {}; v.page = 1; onChange(); }});
}

const PLACEHOLDERS = {
  ip: 'e.g. 203.0.113.7', range: 'e.g. 203.0.113.0/24', scenario: 'e.g. crowdsecurity/ssh-bf', country: 'e.g. NL',
  machine: 'Machine name', asn: 'e.g. 64500', action: 'ban, captcha or none', collector: 'Collector name'
};

// bindFilters wires search, the shared filter menu and the time picker (with "All").
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
  state.filterMenu = window.CertainStatsTelemetry.createFilterMenu(ui.filterMenu, {
    fields: FIELDS.map(f => ({value: f, label: FIELD_LABELS[f], placeholder: PLACEHOLDERS[f]})),
    exclude: true,
    maxLength: 200,
    onAdd: (field, value, exclude) => {
      addFilter(v(), field, value, exclude);
      onChange();
    }
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
