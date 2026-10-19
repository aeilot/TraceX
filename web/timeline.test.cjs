const {test}=require('node:test');
const assert=require('node:assert/strict');
const {createLoader,bindGestures}=require('./timeline.js');
const tick=()=>new Promise(resolve=>setImmediate(resolve));
const spec=(id,key='run',start=0)=>({id,key,buffer:[start,start+30],px:1,view:{start:start+10,end:start+20}});
test('obsolete timeline response cannot replace the latest filter',async()=>{
  const calls=[],results=[],errors=[];
  const loader=createLoader({delay:0,fetch:(s,signal)=>new Promise((resolve,reject)=>calls.push({s,signal,resolve,reject})),result:v=>results.push(v),error:e=>errors.push(e),busy:()=>{}});
  loader.request(spec('a'));await tick();loader.request(spec('b','other'));await new Promise(r=>setTimeout(r,5));
  assert.equal(calls[0].signal.aborted,true);
  calls[0].resolve('stale');calls[1].resolve('latest');await tick();
  assert.deepEqual(results,['latest']);assert.deepEqual(errors,[]);loader.cancel();
});
test('a pending buffer can serve continued panning without request storms',async()=>{
  let count=0,resolve;
  const loader=createLoader({fetch:()=>{count++;return new Promise(r=>resolve=r);},result:()=>{},error:()=>{},busy:()=>{}});
  loader.request(spec('a'));await tick();loader.request({...spec('b'),view:{start:15,end:25}});
  assert.equal(count,1);resolve({});await tick();loader.cancel();
});
test('failure stops; only an explicit new request retries',async()=>{
  let calls=0,errors=0;
  const loader=createLoader({fetch:async()=>{calls++;throw Error('offline');},result:()=>{},error:()=>errors++,busy:()=>{}});
  loader.request(spec('a'));await tick();await tick();assert.equal(calls,1);assert.equal(errors,1);
  loader.request(spec('a'));await tick();assert.equal(calls,2);loader.cancel();
});
function surface(){
  const listeners={},captures=new Set(),classes=new Set();
  const svg={ownerDocument:{addEventListener(){}},addEventListener:(name,fn,capture)=>{(listeners[name]||=[])[capture?'unshift':'push'](fn);},classList:{add:(...names)=>names.forEach(n=>classes.add(n)),remove:(...names)=>names.forEach(n=>classes.delete(n))},getBoundingClientRect:()=>({left:0}),focus(){},setPointerCapture:id=>captures.add(id),hasPointerCapture:id=>captures.has(id),releasePointerCapture:id=>captures.delete(id)};
  return {svg,fire(name,properties={}){const e={button:0,isPrimary:true,pointerId:1,clientX:200,preventDefault(){this.prevented=true;},stopImmediatePropagation(){this.stopped=true;},stopPropagation(){this.stopped=true;},...properties};for(const fn of listeners[name]||[]){fn(e);if(e.stopped)break;}return e;}};
}
function gestures(mode='pan'){
  const s=surface(),ranges=[],views=[],busy=[];
  bindGestures(s.svg,{geometry:()=>({labelWidth:100,plotWidth:1000}),view:()=>({start:100,end:200,duration:100}),setView:(...v)=>views.push(v),mode:()=>mode,select:(...v)=>ranges.push(v),overlay:()=>{},active:v=>busy.push(v),settle:()=>{}});
  return {...s,ranges,views,busy};
}
test('shift region gesture prevents selection and never opens a span after dragging',()=>{
  const g=gestures();assert.equal(g.fire('pointerdown',{shiftKey:true}).prevented,true);
  g.fire('pointermove',{clientX:400});g.fire('pointerup',{clientX:400});
  assert.deepEqual(g.ranges,[[110,130]]);assert.equal(g.fire('click').stopped,true);assert.equal(g.busy.at(-1),false);
});
test('pan starts only beyond click tolerance and survives redraws of SVG children',()=>{
  const g=gestures();g.fire('pointerdown');g.fire('pointermove',{clientX:202});assert.equal(g.views.length,0);
  g.fire('pointermove',{clientX:300});assert.deepEqual(g.views,[[90,190]]);g.fire('pointerup');assert.equal(g.fire('click').stopped,true);
  g.fire('pointerdown');g.fire('pointerup');assert.equal(g.fire('click').stopped,undefined);
});
test('region mode works without a modifier; cancellation does not apply it',()=>{
  const g=gestures('select');g.fire('pointerdown');g.fire('pointermove',{clientX:400});g.fire('pointercancel');assert.deepEqual(g.ranges,[]);
  g.fire('pointerdown');g.fire('pointermove',{clientX:400});g.fire('pointerup');assert.deepEqual(g.ranges,[[110,130]]);
});
test('Escape rolls back an interrupted pan; secondary buttons are ignored',()=>{
  const g=gestures();g.fire('pointerdown',{button:2});g.fire('pointermove',{clientX:400});assert.deepEqual(g.views,[]);
  g.fire('pointerdown');g.fire('pointermove',{clientX:400});g.fire('keydown',{key:'Escape'});assert.deepEqual(g.views.at(-1),[100,200]);assert.equal(g.busy.at(-1),false);
});
