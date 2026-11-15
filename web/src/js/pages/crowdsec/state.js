import { defaultView } from './url/state.js';

// Shared mutable page state. Other modules read and write its fields.
export const state = {
  view: defaultView(),
  collectors: [],
  oldest: 0,            // oldest data known, for the "All" range
  range: null,          // resolved {start, end, custom} of the last load
  summary: null,
  components: {collectors: [], items: []},  // machine and bouncer health
  ui: {},
  picker: null,
  charts: {timeline: null, ip: null},
  requests: {},         // AbortControllers by area
  panelPath: ''
};
