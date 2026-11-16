import { state } from './state.js';

// begin aborts the area's previous request and returns a fresh signal.
export function begin(area) {
  state.requests[area]?.abort();
  const controller = new AbortController();
  state.requests[area] = controller;
  return controller.signal;
}

export async function get(path, signal) {
  const response = await fetch(state.panelPath + '/api/collectors/knot/' + path, {signal});
  let body = null;
  try { body = await response.json(); } catch (e) { /* not JSON: use the generic message */ }
  if (!response.ok) {
    const error = new Error(body?.error || 'Request failed (' + response.status + ')');
    error.status = response.status;
    throw error;
  }
  return body;
}

export function isAbort(error) {
  return error && error.name === 'AbortError';
}
