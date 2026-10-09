const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
class Node {
  constructor(tag) {this.tagName=tag;this.children=[];this.attributes={};this.textContent='';this.className='';}
  append(...nodes) {this.children.push(...nodes);}
  replaceChildren(...nodes) {this.children=nodes;}
  setAttribute(k,v) {this.attributes[k]=v;}
}
function diskDOM() {
 const document={createElement:tag=>new Node(tag),createDocumentFragment:()=>new Node('#fragment'),createTextNode:text=>({textContent:text})};
 const context={window:{},document};vm.runInNewContext(fs.readFileSync('web/static/js/disk_dom.js','utf8'),context);return context.window.CertainStatsDiskDOM;
}
test('disk path IDs are distinct for punctuation and Unicode',()=>{
 const dom=diskDOM();const paths=['/','root','/a/b','/a_b','/a-b','/a.b','/é','/💾'];assert.equal(new Set(paths.map(dom.pathId)).size,paths.length);
});
test('hostile disk paths remain text and preserve field visibility',()=>{
 const dom=diskDOM();const container=new Node('div');const hostile='<img src=x onerror="alert(1)">';dom.render(container,[hostile],'pub-', ['agent_disk_read_bytes']);
 const fragment=container.children[0];assert.equal(fragment.children.length,2);assert.equal(fragment.children[0].hidden,true);assert.equal(fragment.children[1].hidden,false);
 const title=fragment.children[1].children[0].children[0];assert.equal(title.children[0].textContent,hostile);assert.equal(title.children[0].tagName,'span');
 const legend=fragment.children[1].children[0].children[1];assert.equal(legend.children.length,1);
});
test('active templates contain external scripts and inert configuration',()=>{
 function visit(dir) {for(const file of fs.readdirSync(dir,{withFileTypes:true})) {const name=dir+'/'+file.name;if(file.isDirectory()) visit(name);else if(name.endsWith('.html')) {const html=fs.readFileSync(name,'utf8');assert.doesNotMatch(html,/\son(?:click|submit|change|input)\s*=/,name);assert.doesNotMatch(html,/<script\s*>/,name);}}}
 visit('web/templates');
});
test('active JavaScript parses',()=>{for(const file of fs.readdirSync('web/static/js')) if(file.endsWith('.js')) new vm.Script(fs.readFileSync('web/static/js/'+file,'utf8'),{filename:file});});


test('history requests cancel obsolete work and preserve HTTP failures', async () => {
 const callbacks = {};
 const context={window:{addEventListener:(event,fn)=>{callbacks[event]=fn;}},document:{getElementById:()=>null},AbortController,Intl,Date,setTimeout,clearTimeout,fetch:async(url,options)=>{
   if(options.signal.aborted) throw new DOMException('Aborted','AbortError');
   return {ok:false,status:429,headers:{get:()=> '2'}};
 }};
 vm.runInNewContext(fs.readFileSync('web/static/js/requests.js','utf8'),context);
 const requests=context.window.CertainStatsRequests;
 const previous=requests.begin('detail',()=>{});
 const latest=requests.begin('detail',()=>{});
 assert.equal(previous.signal.aborted,true);
 assert.equal(latest.signal.aborted,false);
 await assert.rejects(latest.json('/metrics'),/HTTP 429/);
 callbacks.pagehide();
 assert.equal(latest.signal.aborted,true);
});
