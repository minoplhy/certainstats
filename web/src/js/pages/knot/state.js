// Shared state of the Knot DNS page.
export const state = {
  panelPath: '',
  view: null,
  overview: null,
  zones: [],
  ui: {},
  charts: {},
  picker: null,
  filterMenu: null,
  // onZoom applies a range brushed on any chart.
  onZoom: null,
  // onFilter adds a filter from a clicked dnstap row.
  onFilter: null,
  requests: {},
  loaded: false
};
