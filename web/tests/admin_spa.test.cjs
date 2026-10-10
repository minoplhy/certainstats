const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const {JSDOM} = require('jsdom');

function shell(path = '/panel/') {
  const dom = new JSDOM(`<!doctype html><html><body>
    <a class="nav-link" id="agents-link" href="/panel/">Agents</a>
    <a class="nav-link" id="network-link" href="/panel/network-monitors">Network</a>
    <div id="agents-spa-view"><div id="agents-overview-view"><h1>Agents</h1></div>
      <div id="agents-detail-view" hidden><h1>Node</h1><div id="inpage-detail-time-picker-container"></div><details><section data-network-monitors></section></details></div>
    </div><div id="network-spa-view" hidden><h1>Network</h1><section data-network-monitors></section></div>
    </body></html>`, {url:'https://example.test'+path,runScripts:'outside-only',pretendToBeVisual:true});
  const w=dom.window, errors=[], sockets=[], mounts=[], requests=[], intervals=[];
  w.addEventListener('error', e=>{errors.push(e.error);e.preventDefault();});
  w.scrollTo=({top})=>{Object.defineProperty(w,'scrollY',{configurable:true,value:top});};
  w.setInterval=fn=>{intervals.push(fn);return intervals.length;};w.clearInterval=()=>{};
  w.fetch=async url=>{requests.push(url);return {ok:true,json:async()=>({series:[]})};};
  w.WebSocket=class {
    static OPEN=1;
    constructor(url) {this.url=url;this.readyState=0;sockets.push(this);}
    send() {throw Error('read-only socket must never send');}
    close() {this.readyState=3;this.onclose?.();}
  };
  w.CertainStatsDiskDOM={pathId:value=>value};
  w.CertainStatsChart={formatBps:String,convertDeltaToRate:value=>value,renderMultiChart(){return {destroy(){},updateSeries(){}};}};
  w.CertainStatsNetworkMonitors={mount(root,options){const view={root,options,activations:0,deactivations:0,nodes:[],activate(){this.activations++;},deactivate(){this.deactivations++;},setNode(id){this.nodes.push(id);}};mounts.push(view);return view;}};
  for (const file of ['telemetry','requests','admin_agents']) w.eval(fs.readFileSync('web/static/js/'+file+'.js','utf8'));
  w.CertainStatsTelemetry.onReady=fn=>fn();
  w.CertainStatsAdminAgents.init({panelPath:'/panel',agents:[{agent_id:'node',nickname:'Node',agent_type:'beszel'}]});
  function navigate(url) {w.history.pushState({},'',url);w.dispatchEvent(new w.PopStateEvent('popstate'));}
  return {dom,w,errors,sockets,mounts,requests,intervals,navigate};
}
const flush=async()=>{for(let i=0;i<3;i++) await new Promise(resolve=>setImmediate(resolve));};

test('Agents and Network share one socket and retain picker state across navigation', async()=>{
  const h=shell();await flush();
  assert.equal(h.sockets.length,1);
  assert.equal(h.w.document.querySelector('#agents-overview-view [data-request-status]'), null);
  assert.equal(h.mounts.length,2);
  assert.equal(h.mounts[0].root.closest('#agents-detail-view')!==null,true);
  h.w.document.getElementById('network-link').click();await flush();
  assert.equal(h.w.location.pathname,'/panel/network-monitors');
  assert.equal(h.w.document.getElementById('agents-spa-view').hidden,true);
  assert.equal(h.w.document.getElementById('network-spa-view').hidden,false);
  assert.equal(h.w.document.getElementById('network-link').getAttribute('aria-current'),'page');
  assert.equal(h.sockets.length,1);
  h.navigate('/panel/node');await flush();
  const picker=h.w.document.getElementById('inpage-detail-time-picker-container')._timePicker;
  picker.setState({hours:24,customRange:{start:1100,end:1900}});
  h.navigate('/panel/network-monitors');await flush();
  assert.ok(h.mounts[0].deactivations>0);
  h.navigate('/panel/node');await flush();
  assert.equal(h.w.document.getElementById('inpage-detail-time-picker-container')._timePicker,picker);
  assert.deepEqual(JSON.parse(JSON.stringify(picker.getState())),{hours:24,customRange:{start:1100,end:1900}});
  h.navigate('/panel/');await flush();
  h.navigate('/panel/node');await flush();
  assert.equal(h.mounts[0].nodes.at(-1),'node');
  assert.equal(h.w.document.getElementById('inpage-detail-time-picker-container')._timePicker,picker);
  assert.equal(h.w.document.title,'Agent Details — CertainStats');
  assert.equal(h.sockets.length,1);
  assert.deepEqual(h.errors,[]);
  h.w.dispatchEvent(new h.w.Event('pagehide'));h.dom.window.close();
});
test('direct Network route and browser Back restore views without reconnecting',async()=>{
  const h=shell('/panel/network-monitors');await flush();
  assert.equal(h.w.document.getElementById('network-spa-view').hidden,false);
  assert.equal(h.requests.length,0);
  h.w.document.getElementById('agents-link').click();await flush();
  assert.equal(h.w.document.getElementById('agents-spa-view').hidden,false);
  h.w.history.back();await new Promise(resolve=>setTimeout(resolve,25));
  assert.equal(h.w.location.pathname,'/panel/network-monitors');
  assert.equal(h.w.document.getElementById('network-spa-view').hidden,false);
  assert.equal(h.sockets.length,1);assert.deepEqual(h.errors,[]);
  h.w.dispatchEvent(new h.w.Event('pagehide'));h.dom.window.close();
});
