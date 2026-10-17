import { el, formatMs, formatPercent, plural, needsAttention, median } from '../shared/format.js';

// Hero headline and figures, from the live snapshot.
export function createHero(ctx) {
  const { options, hero, targets, live } = ctx;

  function setHero(role, text) {
    if (hero[role]) hero[role].textContent = text;
  }

  function renderHero() {
    if (!options.hero) return;
    const snapshot = live.snapshot;
    if (!snapshot) {
      setHero('headline', 'Checking monitors…');
      for (const role of ['enabled', 'response', 'loss', 'sync']) setHero(role, '–');
      setHero('paused', '');
      setHero('loss-target', '');
      return;
    }

    const monitors = Object.entries(snapshot).map(([id, monitor]) => ({ monitor_id: id, ...monitor }));
    const enabled = monitors.filter(monitor => monitor.enabled);
    const attention = enabled.filter(needsAttention).length;
    setHero('count', plural(monitors.length, 'monitor'));

    const headline = hero.headline;
    if (headline) {
      if (!monitors.length) {
        headline.replaceChildren(document.createTextNode('Add a monitor to start probing.'));
      } else if (!attention) {
        const text = enabled.length === 1 ? 'Your monitor is healthy.' : 'All ' + enabled.length + ' monitors are healthy.';
        headline.replaceChildren(document.createTextNode(enabled.length ? text : 'All monitors are paused.'));
      } else {
        const bad = el('span', 'is-bad', plural(attention, 'monitor') + (attention === 1 ? ' needs' : ' need') + ' attention.');
        const healthy = enabled.length - attention;
        headline.replaceChildren(bad);
        if (healthy) headline.append(document.createTextNode(' The other ' + healthy + (healthy === 1 ? ' is' : ' are') + ' healthy.'));
      }
    }

    const paused = monitors.length - enabled.length;
    setHero('enabled', String(enabled.length));
    setHero('paused', paused ? paused + ' paused' : '');

    const responses = enabled
      .filter(monitor => monitor.latest?.success_count > 0 && Number.isFinite(monitor.latest.response_avg_ms))
      .map(monitor => monitor.latest.response_avg_ms);
    setHero('response', responses.length ? formatMs(median(responses)) : '–');

    let worst = null;
    for (const monitor of enabled) {
      const loss = monitor.latest?.loss_1h_pct;
      if (Number.isFinite(loss) && (!worst || loss > worst.latest.loss_1h_pct)) worst = monitor;
    }
    setHero('loss', worst ? formatPercent(worst.latest.loss_1h_pct) : '–');
    setHero('loss-target', worst && worst.latest.loss_1h_pct > 0 ? (targets.get(worst.monitor_id) || '') : '');

    const syncing = monitors.filter(monitor => monitor.sync && monitor.sync.desired_generation > monitor.sync.acknowledged_generation).length;
    setHero('sync', String(syncing));
  }

  return { setHero, renderHero };
}
