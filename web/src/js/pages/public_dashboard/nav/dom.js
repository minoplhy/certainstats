import { state } from '../state.js';

export function safeId(p) {
  return window.CertainStatsDiskDOM.pathId(p);
}

// The page is served at {prefix}/{slug} or {prefix}/{slug}/{pubId}; read the
// base from the URL so every mount form (including a bare root) round-trips.
export function getPublicBasePath() {
  const publicPath = (document.body?.getAttribute('data-public-path') || '').replace(/\/+$/, '');
  if (!state.dashSlug) return publicPath;
  const segs = window.location.pathname.replace(/\/+$/, '').split('/');
  if (segs[segs.length - 1] === state.dashSlug) return segs.join('/');
  if (segs.length > 2 && segs[segs.length - 2] === state.dashSlug) return segs.slice(0, -1).join('/');
  return publicPath + '/' + state.dashSlug;
}
