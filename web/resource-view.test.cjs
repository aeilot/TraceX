const {test}=require('node:test');
const assert=require('node:assert/strict');
const {createWindow,slice,rates}=require('./resource-view.js');
test('window zoom is bounded, rejects invalid selections and supports nested back/reset',()=>{
  const w=createWindow(100,1100);assert.equal(w.isFull,true);assert.equal(w.canBack,false);
  assert.equal(w.set(200,600),true);w.set(300,400);assert.equal(w.view.duration,100);
  w.back();assert.deepEqual(w.view,{start:200,end:600,duration:400});
  w.reset();assert.equal(w.isFull,true);w.back();assert.equal(w.view.start,200);
  assert.equal(w.set(NaN,400),false);assert.equal(w.set(500,400),false);
  w.set(-100,200);assert.equal(w.view.start,100);assert.equal(w.view.end,400);
  w.set(900,2000);assert.equal(w.isFull,true);
});
test('continuous zoom can coalesce history and never exceeds its history budget',()=>{
  const w=createWindow(0,1000);w.set(100,900);w.set(200,800,false);w.back();assert.equal(w.isFull,true);
  for(let i=0;i<30;i++)w.set(i,i+100);let n=0;while(w.back())n++;assert.equal(n,20);
});
test('selection keeps inclusive time bounds, shared samples and selected runtime without mutating input',()=>{
  const samples=[{time_ms:10,task_id:'a',runtime_id:'r'},{time_ms:20,task_id:'b',runtime_id:'r'},{time_ms:20,runtime_id:'shared'},{time_ms:30,task_id:'a',runtime_id:'r'}];
  assert.equal(slice(samples,{start:10,end:20},'a').length,2);
  assert.equal(slice(samples,{start:10,end:30},'a','r').length,2);assert.equal(samples.length,4);
});
test('counter rates retain measured zero, reject per-direction resets and avoid mixing task/runtime series',()=>{
  const samples=[{task_id:'a',runtime_id:'r',component:'sandbox',time_ms:0,network_rx_bytes:100,network_tx_bytes:0},{task_id:'b',runtime_id:'r',component:'sandbox',time_ms:500,network_rx_bytes:9999},{task_id:'a',runtime_id:'r',component:'sandbox',time_ms:1000,network_rx_bytes:100,network_tx_bytes:0},{task_id:'a',runtime_id:'r',component:'sandbox',time_ms:2000,network_rx_bytes:0,network_tx_bytes:200}];
  const rows=rates(samples);assert.equal(rows.length,2);assert.equal(rows[0].network_rate,0);assert.equal(rows[1].network_rate,null);
});
test('missing counters are not synthesized as zero or a rate',()=>{
  const r=rates([{runtime_id:'r',time_ms:0},{runtime_id:'r',time_ms:1000,network_rx_bytes:100},{runtime_id:'r',time_ms:2000,network_rx_bytes:300}]);
  assert.equal(r[0].network_rate,null);assert.equal(r[1].network_rate,200);
});
