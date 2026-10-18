const {test} = require("node:test");
const assert = require("node:assert/strict");
const {parse, serialize} = require("./workspace-model.js");
const {find, matches} = require("./workspace-model.js");

test("finding a span locates its contextual page without deleting surrounding spans", () => {
  const spans = Array.from({length:630}, (_,i) => ({id:`span-${i}`,name:i===629?"Plan the next step":"Work",start_ms:i,duration_ms:1,task_id:"a",kind:"model"}));
  const result = find(spans,{query:"PLAN",task:"all",kind:"all",searchIndex:0});
  assert.equal(result.total,1); assert.equal(result.offset,629); assert.equal(result.span_id,"span-629");
  assert.equal(Math.floor(result.offset/250)*250,500);
  assert.equal(spans.length,630);
  assert.equal(matches(spans[629],"plan"),true); assert.equal(matches(spans[0],"plan"),false);
  assert.equal(matches(spans[0],"  "),false);
});

test("match navigation respects task/type/time context and stable order", () => {
  const spans = [
    {id:"late",name:"Plan",start_ms:20,duration_ms:5,task_id:"a",kind:"model"},
    {id:"other",name:"Plan",start_ms:0,duration_ms:5,task_id:"b",kind:"tool"},
    {id:"early",name:"Plan",start_ms:10,duration_ms:5,task_id:"a",kind:"model"},
  ];
  const opts={query:"plan",task:"a",kind:"model",rangeStart:12,rangeEnd:25,searchIndex:1};
  assert.deepEqual(find(spans,opts),{total:2,index:1,span_id:"late",offset:1});
  assert.equal(find(spans,{...opts,query:"absent"}).total,0);
  assert.equal(find(spans,{...opts,searchIndex:99}).span_id,"early");
});

test("an investigation link restores source identity, Unicode filters, span and viewport", () => {
  const investigation = {page:"profiler", run:"20261017/run & baseline", task:"研究/42", kind:"model",
    query:"tool & agent", runtime:"runtime/1", selected:"task/span?x=1", view:"split",
    rangeStart:1200.25, rangeEnd:9200, zoom:2.5, pageOffset:500, pageLimit:50, baseline:"", candidate:""};
  assert.deepEqual(parse(serialize(investigation)), investigation);
});

test("invalid shared ranges and unsupported paging cannot create an unusable view", () => {
  const s = parse("#page=unknown&from=900&to=20&zoom=Infinity&offset=-1&limit=999999&view=unknown");
  assert.equal(s.page,"experiments"); assert.equal(s.rangeStart,0); assert.equal(s.rangeEnd,null);
  assert.equal(s.zoom,1); assert.equal(s.pageOffset,0); assert.equal(s.pageLimit,250); assert.equal(s.view,"timeline");
  assert.equal(parse("#from=NaN&to=-20").rangeEnd,null);
});

test("comparison links retain stable source IDs independently of mount order", () => {
  const s = parse(serialize({page:"compare", baseline:"candidate/2", candidate:"baseline/1"}));
  assert.equal(s.baseline,"candidate/2"); assert.equal(s.candidate,"baseline/1");
  assert.equal(parse("").page,"experiments");
});
