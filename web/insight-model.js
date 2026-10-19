(function(root) {
  "use strict";
  const known = s => !s.incomplete && !["unknown","completion_only"].includes(s.attributes?.time_quality);
  const bad = s => ["failed","error","canceled"].includes((s || "").toLowerCase());
  const finished = t => ["succeeded","success","completed","failed","canceled"].includes(t.status) && !["unknown","completion_only"].includes(t.time_quality);
  const quantile = (xs,q) => { if (!xs.length) return null; const n=q*(xs.length-1),lo=Math.floor(n),hi=Math.ceil(n);return xs[lo]+(xs[hi]-xs[lo])*(n-lo); };
  function union(xs) { let total=0,start=null,end=0; for (const [a,b] of xs.sort((a,b)=>a[0]-b[0])) {if(start===null){start=a;end=b;}else if(a>end){total+=end-start;start=a;end=b;}else end=Math.max(end,b);}return start===null?0:total+end-start; }
  const usage = (s,input) => s.usage?.[input?"input_tokens":"output_tokens"] ?? s.attributes?.[input?"gen_ai.usage.input_tokens":"gen_ai.usage.output_tokens"] ?? null;
  const finding = (rule,severity="info") => ({rule,severity,value:0,count:0,span_ids:[],task_ids:[]});
  const add = (xs,id) => {if(xs.length<5)xs.push(id);};
  function derive(p,a={}) {
    const byID=new Map(p.spans.map(s=>[s.id,s])),nested=new Set(),groups=new Map();
    for(const s of p.spans) {if(!["model","tool","browser"].includes(s.kind))continue;const seen=new Set();let id=s.parent_id;while(id && !seen.has(id)){seen.add(id);const parent=byID.get(id);if(!parent)break;if(parent.kind===s.kind)nested.add(id);id=parent.parent_id;}}
    const failed=finding("failed_spans","warning"),gap=finding("usage_gap"),out={run_id:p.run_id,findings:[],groups:[],tasks:[],span_count:p.spans.length,group_count:0};
    for(const s of p.spans) {
      if(bad(s.status)){failed.count++;add(failed.span_ids,s.id);}
      if(!["model","tool","browser"].includes(s.kind)||nested.has(s.id))continue;
      const name=(s.kind==="model" ? s.attributes?.["gen_ai.request.model"] || s.attributes?.model || "Model identity not recorded" : s.attributes?.["tool.name"] || s.name),key=s.kind+"\0"+name;
      if(!groups.has(key))groups.set(key,{kind:s.kind,name,count:0,timed:0,failed:0,sum_ms:0,union_ms:0,p50_ms:null,p95_ms:null,input_tokens:0,output_tokens:0,input_known:0,output_known:0,span_ids:[],durations:[],intervals:[]});
      const g=groups.get(key);g.count++;add(g.span_ids,s.id);if(bad(s.status))g.failed++;
      if(known(s)){g.timed++;g.sum_ms+=s.duration_ms;g.durations.push(s.duration_ms);g.intervals.push([s.start_ms,s.start_ms+s.duration_ms]);}
      if(s.kind==="model") {const input=usage(s,true),output=usage(s,false);if(input!=null){g.input_known++;g.input_tokens+=input;}if(output!=null){g.output_known++;g.output_tokens+=output;}if(input==null||output==null){gap.count++;add(gap.span_ids,s.id);}}
    }
    if(failed.count)out.findings.push(failed);
    out.groups=[...groups.values()].map(g=>{g.durations.sort((a,b)=>a-b);g.p50_ms=quantile(g.durations,.5);g.p95_ms=quantile(g.durations,.95);g.union_ms=union(g.intervals);delete g.durations;delete g.intervals;return g;}).sort((a,b)=>b.sum_ms-a.sum_ms || (a.kind+"\0"+a.name).localeCompare(b.kind+"\0"+b.name));
    out.group_count=out.groups.length;out.groups=out.groups.slice(0,200);
    if(a.critical_path?.status==="available") {const spans=a.critical_path.span_ids.map(id=>byID.get(id)).filter(s=>s&&known(s)).sort((a,b)=>b.duration_ms-a.duration_ms);if(spans.length){const f=finding("critical_span");f.value=spans[0].duration_ms;f.span_ids=[spans[0].id];out.findings.push(f);}}
    out.tasks=p.tasks.map(t=>({id:t.id,key:t.logical_id || t.id,name:t.name,status:t.status,duration_ms:finished(t)?t.duration_ms:null,score:t.score??null}));
    const xs=out.tasks.map(t=>t.duration_ms).filter(v=>v!=null).sort((a,b)=>a-b),median=quantile(xs,.5);
    if(xs.length>=5&&median>0){const f=finding("slow_tasks","warning");for(const t of out.tasks)if(t.duration_ms>median*2){f.count++;add(f.task_ids,t.id);f.value=Math.max(f.value,t.duration_ms/median);}if(f.count)out.findings.push(f);}
    const coverage=finding("coverage_gap");for(const c of a.coverage||[])if(c.status==="available"&&c.ratio<.7){coverage.count++;coverage.value+=c.unattributed_ms;add(coverage.task_ids,c.task_id);}if(coverage.count)out.findings.push(coverage);if(gap.count)out.findings.push(gap);
    const previous=new Map();let periods=0,throttled=0;
    for(const r of [...p.resources].sort((a,b)=>a.time_ms-b.time_ms)){if(!r.cpu_throttling)continue;const key=[r.task_id,r.runtime_id,r.component].join("\0"),prev=previous.get(key);if(prev&&r.time_ms>prev.time_ms){const dp=r.cpu_throttling.periods-prev.cpu_throttling.periods,dt=r.cpu_throttling.throttled_periods-prev.cpu_throttling.throttled_periods;if(dp>0&&dt>=0&&dt<=dp){periods+=dp;throttled+=dt;}}previous.set(key,r);}
    if(periods>0&&throttled/periods>=.1){const f=finding("throttling","warning");f.value=throttled/periods;out.findings.push(f);}
    const pause=suspension(p);if(pause)out.findings.push(pause);
    return out;
  }
  // Mirrors the server rule: the paused share of agent time counts only tasks
  // with both pauses and agent spans; resource details use sandbox samples
  // inside a pause, where CPU stops but memory stays held.
  function suspension(p) {
    const f={...finding("sandbox_suspension"),details:{}},paused=new Map(),agent=new Map(),unpause=[];
    const push=(m,k,v)=>{if(!m.has(k))m.set(k,[]);m.get(k).push(v);};
    for(const s of p.spans){
      if(s.kind==="suspended"&&!s.incomplete){f.count++;add(f.span_ids,s.id);push(paused,s.task_id,[s.start_ms,s.start_ms+s.duration_ms]);const v=s.attributes?.unpause_latency_ms;if(Number.isFinite(v)&&v>=0)unpause.push(v);if(bad(s.status))f.details.failed_unpauses=(f.details.failed_unpauses||0)+1;}
      else if(s.kind==="agent"&&known(s))push(agent,s.task_id,[s.start_ms,s.start_ms+s.duration_ms]);
    }
    if(!f.count)return null;
    let pausedMS=0,agentMS=0;
    for(const task of [...paused.keys()].sort()){add(f.task_ids,task);if(agent.has(task)){pausedMS+=union([...paused.get(task)]);agentMS+=union([...agent.get(task)]);}}
    if(agentMS>0){f.value=pausedMS/agentMS;f.details.paused_ms=pausedMS;f.details.agent_ms=agentMS;}
    if(unpause.length){unpause.sort((a,b)=>a-b);f.details.unpause_p50_ms=quantile(unpause,.5);f.details.unpause_p99_ms=quantile(unpause,.99);}
    // A CPU sample covers the interval since its series' previous sample, so it
    // counts only when that whole interval lies inside one pause.
    const memory=[],cpu=[],avg=xs=>xs.reduce((a,b)=>a+b,0)/xs.length,previous=new Map();
    const within=(task,t)=>(paused.get(task)||[]).findIndex(([a,b])=>t>=a&&t<=b);
    for(const r of [...p.resources].sort((a,b)=>a.time_ms-b.time_ms)){
      if(r.component!=="sandbox")continue;const key=r.task_id+"\0"+r.runtime_id,prev=previous.get(key);previous.set(key,r.time_ms);
      const i=within(r.task_id,r.time_ms);if(i<0)continue;
      if(Number.isFinite(r.memory_bytes))memory.push(r.memory_bytes);if(Number.isFinite(r.cpu_percent)&&prev!==undefined&&within(r.task_id,prev)===i)cpu.push(r.cpu_percent);
    }
    if(memory.length)f.details.paused_memory_bytes=avg(memory);if(cpu.length)f.details.paused_cpu_percent=avg(cpu);
    return f;
  }
  function scope(p,task,start=0,end=Infinity) {
    const spans=p.spans.filter(s=>(!task||s.task_id===task)&&s.start_ms<=end&&s.start_ms+s.duration_ms>=start),ids=new Set(spans.map(s=>s.id));
    return {...p,spans,tasks:p.tasks.filter(t=>(!task||t.id===task)&&t.start_ms<=end&&t.start_ms+t.duration_ms>=start),edges:p.edges.filter(e=>ids.has(e.source)&&ids.has(e.target)),resources:p.resources.filter(r=>(!task||!r.task_id||r.task_id===task)&&r.time_ms>=start&&r.time_ms<=end)};
  }
  const mean=xs=>xs.length?xs.reduce((a,b)=>a+b,0)/xs.length:null;
  function summarize(tasks) {return {count:tasks.length,scored:tasks.filter(t=>t.score!=null).length,timed:tasks.filter(t=>t.duration_ms!=null).length,duration_ms:mean(tasks.map(t=>t.duration_ms).filter(v=>v!=null)),score:mean(tasks.map(t=>t.score).filter(v=>v!=null)),failed:tasks.filter(t=>bad(t.status)).length,tasks};}
  function compare(left,right) {
    const groups=new Map();for(const [side,rows]of [["left",left],["right",right]])for(const t of rows){if(!groups.has(t.key))groups.set(t.key,{key:t.key,left:[],right:[]});groups.get(t.key)[side].push(t);}
    return [...groups.values()].map(row=>{const baseline=summarize(row.left),candidate=summarize(row.right),delta=baseline.duration_ms!=null&&candidate.duration_ms!=null?candidate.duration_ms-baseline.duration_ms:null;return {key:row.key,baseline,candidate,delta_ms:delta,ratio:delta!=null&&baseline.duration_ms>0?delta/baseline.duration_ms:null};}).sort((a,b)=>(b.delta_ms??-Infinity)-(a.delta_ms??-Infinity)||a.key.localeCompare(b.key));
  }
  function readNotes(text) {try {const rows=JSON.parse(text||"[]");return Array.isArray(rows)?rows.filter(n=>n&&typeof n.id==="string"&&typeof n.title==="string"&&typeof n.text==="string"&&typeof n.run_id==="string"&&Array.isArray(n.refs)&&n.refs.every(r=>r&&typeof r.run_id==="string"&&(typeof r.span_id==="string"||typeof r.task_id==="string"))).slice(0,100):[];}catch{return [];}}
  const api={derive,scope,compare,readNotes};if(typeof module!=="undefined")module.exports=api;else root.TraceXInsights=api;
})(globalThis);
