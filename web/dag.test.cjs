const {test}=require('node:test');
const assert=require('node:assert/strict');
const {project,layout}=require('./dag.js');
const spans=['root','branch','a','b','join','loose'].map((id,i)=>({id,name:id,start_ms:i}));
const edge=(source,target,kind='parent')=>({source,target,kind});
const edges=[edge('root','branch'),edge('branch','a'),edge('branch','b'),edge('a','join','join'),edge('b','join','join')];
test('overview folds successors and separately counts nodes without page links',()=>{
  const g=project(spans,edges);assert.deepEqual(g.nodes.map(s=>s.id),['root','branch']);assert.equal(g.folded.get('branch'),2);assert.equal(g.isolated,1);assert.equal(g.hidden,4);
});
test('expanding a branch reveals its direct children, preserves shared joins and only recorded edges',()=>{
  const g=project(spans,edges,{expanded:new Set(['branch','a'])});assert.deepEqual(g.nodes.map(s=>s.id),['root','branch','a','b','join']);
  assert.deepEqual(g.edges,edges);assert.equal(g.parents.get('join').length,2);
  const closed=project(spans,edges,{collapsed:new Set(['root'])});assert.deepEqual(closed.nodes.map(s=>s.id),['root']);
});
test('search or selection reveals its ancestors while preserving unrelated overview context',()=>{
  const g=project(spans,edges,{reveal:['join']});assert.ok(g.nodes.some(s=>s.id==='join'));assert.equal(g.nodes.length,5);
  const loose=project(spans,edges,{reveal:['loose']});assert.deepEqual(loose.nodes.map(s=>s.id),['root','branch','loose']);
});
test('focused neighborhood retains exact incoming and outgoing edges, excludes distant descendants',()=>{
  const g=project(spans,edges,{focus:'branch'});assert.deepEqual(g.nodes.map(s=>s.id),['root','branch','a','b']);assert.equal(g.edges.length,3);assert.equal(g.nodes.some(s=>s.id==='join'),false);
});
test('empty, completely isolated and malformed cyclic pages remain inspectable',()=>{
  assert.equal(project([],[]).nodes.length,0);assert.equal(project(spans,[]).nodes.length,6);
  assert.equal(project(spans,[edge('root','branch'),edge('branch','root')]).nodes.length,2);
  assert.equal(project(spans,edges,{all:true,unlinked:true}).nodes.length,6);
});
test('layout is deterministic, preserves directed layering and never overlaps cards in a column',()=>{
  const g=project(spans,edges,{all:true,unlinked:true}),a=layout(g),b=layout(g);assert.deepEqual([...a.pos],[...b.pos]);
  for(const e of g.edges)assert.ok(a.pos.get(e.source).x<a.pos.get(e.target).x);
  for(const column of a.columns.values()){const ys=column.map(n=>a.pos.get(n.id).y);assert.equal(new Set(ys).size,ys.length);}
  assert.ok(Number.isFinite(a.width)&&Number.isFinite(a.height));
});
test('cross-page edges are not turned into guessed links between visible descendants',()=>{
  const g=project(spans,[...edges,edge('outside','branch')],{all:true,unlinked:true});assert.equal(g.edges.length,edges.length);assert.equal(g.parents.get('branch').includes('outside'),false);
});

test('wide branches expand in bounded batches; expand-page and search can reveal the rest',()=>{
  const nodes=[{id:'r',start_ms:0},{id:'b',start_ms:1},...Array.from({length:40},(_,i)=>({id:'c'+i,start_ms:i+2}))];
  const links=[edge('r','b'),...nodes.slice(2).map(s=>edge('b',s.id))];
  const first=project(nodes,links,{expanded:new Set(['b'])});assert.equal(first.nodes.length,10);assert.equal(first.folded.get('b'),32);
  assert.equal(project(nodes,links,{expanded:new Set(['b']),limits:new Map([['b',16]])}).nodes.length,18);
  assert.equal(project(nodes,links,{all:true}).nodes.length,42);assert.ok(project(nodes,links,{reveal:['c39']}).nodes.some(s=>s.id==='c39'));
});
