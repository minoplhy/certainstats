(function () {
  'use strict';
  const groups = new Map();
  function begin(key, retry, options = {}) {
    groups.get(key)?.abort();
    const controller = new AbortController();groups.set(key, controller);
    let pending = 0, failed = false, samples = 0, retryAt = 0;
    const container = options.silent ? null : document.getElementById(key);
    let status = container?.querySelector('[data-request-status]');
    if (container && !status) { status = document.createElement('div');status.dataset.requestStatus = '';status.className = 'request-status';status.setAttribute('role', 'status');container.prepend(status); }
    function render() {
      if (!status || controller.signal.aborted) return;
      status.hidden = !pending && !failed && samples > 0;
      status.replaceChildren(document.createTextNode(pending ? 'Loading history…' : failed ? 'History could not be loaded. ' : samples ? '' : 'No samples in this range. '));
      if (!pending && failed) {
        const button = document.createElement('button');button.type = 'button';button.className = 'btn btn-secondary btn-sm';button.textContent = 'Retry';button.disabled = Date.now() < retryAt;
        button.addEventListener('click', retry);status.append(button);
        if (button.disabled) {const timer=setTimeout(() => {if (!controller.signal.aborted) button.disabled=false;}, retryAt-Date.now());controller.signal.addEventListener('abort',()=>clearTimeout(timer),{once:true});}
      }
    }
    render();
    return {
      signal: controller.signal,
      json: async function (url) {
        pending++;render();
        try {
          const response = await fetch(url, {signal: controller.signal});
          if (response.status === 404) return {series: []};
          if (!response.ok) {const raw=response.headers.get('Retry-After');let delay=Number(raw)*1000;if (!Number.isFinite(delay)) delay=Date.parse(raw)-Date.now();retryAt=Math.max(retryAt, Date.now()+Math.max(0,delay||0));throw new Error('HTTP ' + response.status);}
          const data=await response.json();samples+=(data.series || []).reduce((n,s)=>n+(s.data || []).filter(point=>point[1]!==null).length,0);return data;
        } catch (error) {if (error.name !== 'AbortError') failed=true;throw error;} finally {pending--;render();}
      }
    };
  }
  function cancel(key) {
    groups.get(key)?.abort();
    groups.delete(key);
  }
  function cancelAll() {for (const controller of groups.values()) controller.abort();groups.clear();}
  window.addEventListener('pagehide', cancelAll);
  window.CertainStatsRequests = {begin, cancel, cancelAll};
})();
