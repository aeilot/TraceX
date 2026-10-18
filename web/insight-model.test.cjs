const {test}=require('node:test');
const assert=require('node:assert/strict');
const {derive,compare,scope,readNotes}=require('./insight-model.js');
const p={run_id:'fixture',tasks:[],resources:[],edges:[],spans:[
  {id:'envelope',kind:'model',name:'outer',start_ms:0,duration_ms:20},
  {id:'a',parent_id:'envelope',kind:'model',name:'api',start_ms:0,duration_ms:10,usage:{input_tokens:0}},
  {id:'b',kind:'model',name:'api',start_ms:2,duration_ms:0,attributes:{time_quality:'completion_only'},usage:{output_tokens:4}},
  {id:'c',kind:'model',name:'api',start_ms:5,duration_ms:10,status:'failed'},
]};
test('leaf aggregation distinguishes overlap, unknown timing and measured zero usage',()=>{
  const r=derive(p),g=r.groups[0];assert.equal(r.groups.length,1);assert.equal(g.count,3);assert.equal(g.timed,2);assert.equal(g.sum_ms,20);assert.equal(g.union_ms,15);assert.equal(g.p50_ms,10);assert.equal(g.input_known,1);assert.equal(g.input_tokens,0);assert.equal(r.findings.find(f=>f.rule==='usage_gap').count,3);
});
test('task pairing retains repeats and missing tasks, and excludes missing timing from means',()=>{
  const rows=compare([{id:'1',key:'a',duration_ms:10,score:0,status:'failed'},{id:'2',key:'a',duration_ms:null,score:null}],[{id:'3',key:'a',duration_ms:20,score:1},{id:'4',key:'b',duration_ms:5}]);
  const a=rows.find(r=>r.key==='a'),b=rows.find(r=>r.key==='b');assert.equal(a.baseline.count,2);assert.equal(a.baseline.timed,1);assert.equal(a.baseline.duration_ms,10);assert.equal(a.baseline.score,0);assert.equal(a.ratio,1);assert.equal(b.baseline.count,0);assert.equal(b.delta_ms,null);
});
test('scope preserves full overlapping span intervals; corrupted notebook is recoverable',()=>{
  const scoped=scope(p,'',9,11);assert.equal(scoped.spans.find(s=>s.id==='a').duration_ms,10);assert.equal(scoped.spans.some(s=>s.id==='b'),false);assert.deepEqual(readNotes('{broken'),[]);assert.deepEqual(readNotes('{}'),[]);
});

test('notebook restores valid evidence and rejects malformed references',()=>{
  const n={id:'n',title:'title',text:'text',run_id:'run',refs:[{run_id:'run',span_id:'span'}]};
  assert.equal(readNotes(JSON.stringify([n])).length,1);
  assert.deepEqual(readNotes(JSON.stringify([{...n,refs:[null]}])),[]);
});
