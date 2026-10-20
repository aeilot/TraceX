/* Evidence-backed diagnostics, explicit model interpretation and a local notebook. */
(() => {
  "use strict";
  const model=window.TraceXInsights, cache=new Map(), storageKey="tracex.investigation-notes.v1";
  let scope={type:"run"},comparison=null,current=null,packet=null,packetRefs=new Map(),generation=0,lastKey="",aiController=null,aiResult=null,editing=null,draftContext=null;
  let aiConfig={enabled:false,endpoint:"",model:""},notes=[];
  try {notes=model.readNotes(localStorage.getItem(storageKey));} catch {}
  const source=id=>allRuns().find(r=>r.id===id);
  const ref=(runID,type,id)=>({run_id:runID,[type==="span"?"span_id":"task_id"]:id});
  const navigate=page=>window.dispatchEvent(new CustomEvent("tracex:navigate",{detail:{page}}));
  const jump=reference=>window.dispatchEvent(new CustomEvent("tracex:evidence",{detail:reference}));
  const el=(tag,cls,text)=>html(tag,cls,text);
  function button(label,action) {const b=el("button","button small",t9n(label));b.type="button";b.onclick=action;return b;}
  // Name the target on the button so several references in one card stay distinguishable.
  function referenceTarget(reference) {
    // IDs are looked up in the open profile; a reference into another run falls back to its ID.
    const p=state.profile,clip=s=>s.length>28?s.slice(0,27)+"…":s;
    if(reference.task_id){const t=p?.tasks.find(x=>x.id===reference.task_id);return clip((t?.name)||reference.task_id);}
    const s=p?.spans.find(x=>x.id===reference.span_id);
    return s?`${clip(s.name)} · ${format(s.duration_ms)}`:clip(reference.span_id);
  }
  function referenceButton(reference,label) {
    const b=label?button(label,()=>jump(reference)):button(reference.span_id?"Locate span {name}":"Locate task {name}",()=>jump(reference));
    if(!label)b.textContent=t9n(reference.span_id?"Locate span {name}":"Locate task {name}",{name:referenceTarget(reference)});
    b.title=reference.span_id || reference.task_id;return b;
  }
  function grid(target,headings,rows) {
    target.replaceChildren();if(!rows.length){target.append(el("p","workspace-empty",t9n("No matching records.")));return;}
    const t=el("table"),head=el("thead"),tr=el("tr"),body=el("tbody");for(const h of headings){const th=el("th","",t9n(h));th.scope="col";tr.append(th);}head.append(tr);
    for(const cells of rows){const row=el("tr");for(const value of cells){const td=el("td");if(value instanceof Node)td.append(value);else td.textContent=String(value??"—");row.append(td);}body.append(row);}t.append(head,body);target.append(t);
  }
  function findingText(f) {
    const strings={
      failed_spans:["Failed execution spans","{count} recorded spans failed or were canceled.","Inspect the failure and its recorded neighbors before attributing a cause."],
      critical_span:["Long interval on the critical path","A recorded critical-path span lasts {value}.","Inspect this span and compare its model or tool configuration in a new experiment."],
      slow_tasks:["Tasks above the run median","{count} tasks exceed twice the run median; the largest ratio is {value}.","Compare task inputs and measured phases. Different tasks can have different expected durations."],
      coverage_gap:["Unobserved task time","{count} tasks have less than 70% observed-work coverage; {value} is unobserved in total.","Check instrumentation coverage. Unobserved time is not a measured wait."],
      usage_gap:["Incomplete model usage","{count} leaf model records lack input or output usage.","Complete usage instrumentation before comparing token totals or estimating cost."],
      throttling:["CPU throttling observed","{value} of observed enforcement periods were throttled.","Compare CPU quotas while holding other recorded conditions fixed. Correlation does not establish the cause of slow spans."],
      sandbox_suspension:["Sandbox suspension","{count} sandbox pauses cover {value} of agent time. Unpause latency p50 {p50}, p99 {p99}. Sample intervals entirely inside a pause averaged {cpu} sandbox CPU; memory readings taken while paused averaged {memory}.","Pausing frees CPU but not memory. Compare against an unpaused run under the same recorded conditions before attributing savings."],
    };
    const [title,description,action]=strings[f.rule] || ["Evidence finding","Inspect the recorded evidence.","Inspect the recorded evidence."];
    const d=f.details||{},missing=t9n("not measured");
    const value=f.rule==="slow_tasks"?f.value.toFixed(2)+"×":f.rule==="throttling"?(f.value*100).toFixed(1)+"%":f.rule==="sandbox_suspension"?(d.agent_ms?(f.value*100).toFixed(1)+"%":missing):format(f.value);
    const extra=f.rule==="sandbox_suspension"?{p50:d.unpause_p50_ms!=null?format(d.unpause_p50_ms):missing,p99:d.unpause_p99_ms!=null?format(d.unpause_p99_ms):missing,cpu:d.paused_cpu_percent!=null?d.paused_cpu_percent.toFixed(1)+"%":missing,memory:d.paused_memory_bytes!=null?bytes(d.paused_memory_bytes):missing}:{};
    return {title:t9n(title),description:t9n(description,{count:f.count,value,...extra}),action:t9n(action)};
  }
  function findingRefs(f,runID) {return [...f.span_ids.map(id=>ref(runID,"span",id)),...f.task_ids.map(id=>ref(runID,"task",id))];}
  function findings(target,data,limit=Infinity) {
    target.replaceChildren();const rows=data.report.findings.slice(0,limit);
    if(!rows.length){target.append(el("p","workspace-empty",t9n("No diagnostic rule triggered. This does not establish complete coverage or absence of bottlenecks.")));return;}
    for(const f of rows){const text=findingText(f),card=el("article","finding-card "+f.severity),actions=el("div","insight-actions"),refs=findingRefs(f,data.report.run_id);
      card.append(el("span","tag",t9n("Measured evidence")),el("h3","",text.title),el("p","",text.description),el("p","finding-action",text.action));
      refs.forEach(r=>actions.append(referenceButton(r)));
      if(f.rule==="throttling")actions.append(button("View resource signals",()=>{navigate("profiler");$(".resources-card").scrollIntoView({behavior:TraceXMotion.scrollBehavior(),block:"start"});}));
      actions.append(button("Save finding",()=>saveRecord({title:text.title,text:text.description+"\n"+text.action,run_id:data.report.run_id,version:version(data),refs,scope:scope.type})));
      card.append(actions);target.append(card);
    }
  }
  async function getReport(id,requested={type:"run"}) {
    const meta=source(id);if(!meta)throw Error(t9n("Linked run is unavailable. Mount the source or reimport the profile."));
    const revision=meta.revision || 0,key=JSON.stringify([id,revision,requested]);
    if(cache.has(key))return cache.get(key);
    const pending=(async()=>{
      if(state.imports.has(id)){
        let p=state.imports.get(id),a=analyze(p),scoped=requested.type!=="run";
        if(requested.type==="span"){const s=p.spans.find(s=>s.id===requested.span_id);if(!s)throw Error(t9n("Span not found"));p=model.scope(p,s.task_id,s.start_ms,s.start_ms+s.duration_ms);a={};}
        if(requested.type==="window"){p=model.scope(p,requested.task,requested.start,requested.end);a={};}
        const report=model.derive(p,a);if(scoped)report.findings=report.findings.filter(f=>!["slow_tasks","coverage_gap"].includes(f.rule));
        return {report,revision:0,scoped};
      }
      const params=new URLSearchParams({run:id});
      if(requested.type==="span")params.set("span",requested.span_id);
      if(requested.type==="window"){params.set("start_ms",requested.start);params.set("end_ms",requested.end);if(requested.task&&requested.task!=="all")params.set("task",requested.task);}
      return json("/api/insights?"+params);
    })();
    cache.set(key,pending);if(cache.size>16)cache.delete(cache.keys().next().value);
    try{return await pending;}catch(e){cache.delete(key);throw e;}
  }
  const version=data=>`${data.report.run_id}@${data.revision}:${JSON.stringify(scope)}`;
  function aggregates(report) {
    grid($("#insight-groups"),["Kind","Model / tool","Calls / timed","Failed","Summed work","Union time","P50 / P95","Input / output tokens","Usage coverage","Evidence"],report.groups.map(g=>{
      const links=el("div","insight-actions");g.span_ids.slice(0,2).forEach(id=>links.append(referenceButton(ref(report.run_id,"span",id))));
      return [g.kind,t9n(g.name),`${g.count} / ${g.timed}`,g.failed,format(g.sum_ms),g.timed?format(g.union_ms):"—",`${format(g.p50_ms)} / ${format(g.p95_ms)}`,g.kind==="model"?`${g.input_known?g.input_tokens.toLocaleString():"—"} / ${g.output_known?g.output_tokens.toLocaleString():"—"}`:"—",g.kind==="model"?`${g.input_known}/${g.count} · ${g.output_known}/${g.count}`:"—",links];
    }));
    if(report.group_count>report.groups.length)$("#insight-groups").prepend(el("p","",t9n("Showing the top 200 groups by summed work.")));
  }
  function buildPacket(data,pairs=[]) {
    packetRefs=new Map();const runID=data.report.run_id;
    const cite=(id,type,run=runID)=>{const key=run+"/"+type+"/"+id;packetRefs.set(key,ref(run,type,id));return key;};
    const result={scope:scope.type,version:version(data).slice(0,256),run_id:runID,candidate_id:"",findings:data.report.findings.slice(0,8).map(f=>({...f,span_ids:f.span_ids.map(id=>cite(id,"span")),task_ids:f.task_ids.map(id=>cite(id,"task"))})),groups:data.report.groups.slice(0,12).map(g=>({...g,name:g.name.slice(0,256),span_ids:g.span_ids.map(id=>cite(id,"span"))})),tasks:[],comparison:[],conditions:[]};
    if(scope.type==="comparison"&&comparison){result.candidate_id=comparison.right.report.run_id;const included=pairs.slice(0,6);result.comparison=included.map(row=>({key:row.key,baseline_ms:row.baseline.duration_ms,candidate_ms:row.candidate.duration_ms,baseline_score:row.baseline.score,candidate_score:row.candidate.score,baseline_count:row.baseline.count,candidate_count:row.candidate.count}));for(const row of included)for(const [side,run]of [[row.baseline,runID],[row.candidate,result.candidate_id]]){const t=side.tasks[0];if(t)result.tasks.push({...t,id:cite(t.id,"task",run),name:t.name.slice(0,256)});}}
    else result.tasks=data.report.tasks.slice(0,12).map(t=>({...t,id:cite(t.id,"task"),name:t.name.slice(0,256)}));
    const leftConfig=source(scope.type==="comparison"?comparison.ids.left:state.runKey)?.configuration || {};
    const rightConfig=scope.type==="comparison"?source(comparison.ids.right)?.configuration || {}:{};
    result.conditions=[...new Set([...Object.keys(leftConfig),...Object.keys(rightConfig)])].slice(0,12).map(key=>({key:key.slice(0,128),baseline:String(leftConfig[key]??"Not recorded").slice(0,256),candidate:String(rightConfig[key]??"Not recorded").slice(0,256)}));
    return result;
  }
  function setPacket(data,pairs) {
    if(aiController)aiController.abort();aiController=null;aiResult=null;
    packet=buildPacket(data,pairs);$("#insight-packet").textContent=JSON.stringify(packet,null,2);
    $("#insight-ai-consent").checked=false;$("#insight-ai-result").replaceChildren();$("#insight-ai-status").textContent="";$("#insight-ai-save").hidden=true;$("#insight-ai-cancel").hidden=true;updateAI();
  }
  function updateAI() {$("#insight-ai-start").disabled=!aiConfig.enabled || !packet || !$("#insight-ai-consent").checked || !!aiController;}
  async function refresh(force=false) {
    const page=document.body.dataset.workspace;if(!["summary","insights"].includes(page)||!state.profile)return;
    const requested=page==="summary"?{type:"run"}:scope.type==="comparison"?{type:"run"}:scope;
    if(scope.runKey&&scope.runKey!==state.runKey){scope={type:"run"};return refresh(true);}
    const key=JSON.stringify([page,state.runKey,source(state.runKey)?.revision,requested,scope.type==="comparison"?comparison?.ids:null]);
    if(!force&&key===lastKey)return;lastKey=key;const ticket=++generation;
    if(page==="insights")$("#insight-status").textContent=t9n("Loading full-context evidence…");
    try{
      const data=scope.type==="comparison"&&page==="insights"&&comparison?comparison.left:await getReport(state.runKey,requested);
      if(ticket!==generation)return;
      if(page==="summary"){findings($("#overview-findings"),data,3);return;}
      current=data;$("#insight-scope").value=scope.type;
      $("#insight-scope-label").textContent=scope.type==="comparison"?`${comparison.left.report.run_id} → ${comparison.right.report.run_id} · ${t9n("Cards and call groups describe the baseline; the model preview also includes paired task differences and recorded conditions.")}`:data.scoped?t9n("Overlapping evidence; durations are complete recorded intervals, not window-attributed cost."):t9n("Full-run evidence, independent of the current span page or search.");
      $("#insight-status").textContent=t9n("{spans} spans · revision {revision}",{spans:data.report.span_count.toLocaleString(),revision:data.revision});
      findings($("#insight-findings"),data);aggregates(data.report);setPacket(data,comparison?.pairs||[]);renderNotes();
    }catch(e){if(ticket===generation){lastKey="";$(page==="summary"?"#overview-findings":"#insight-status").textContent=t9n("Insights unavailable: {error}",{error:e.message});}}
  }
  function openScope(requested) {
    scope={...requested,runKey:state.runKey};
    if(scope.type==="window")Object.assign(scope,{start:state.rangeStart,end:state.rangeEnd??state.profile.duration_ms,task:state.task});
    $("#insight-scope option[value=span]").disabled=scope.type!=="span";
    $("#insight-scope option[value=comparison]").disabled=!comparison;
    navigate("insights");refresh();
  }
  async function comparisonReport(ids) {
    const key=JSON.stringify(ids);comparison={ids,key};
    let target=$("#insight-comparison");if(!target){target=el("section","insight-comparison");target.id="insight-comparison";$("#comparison-result").append(target);}
    target.textContent=t9n("Loading task regressions…");
    try{
      const [left,right]=await Promise.all([getReport(ids.left),getReport(ids.right)]);if(comparison?.key!==key)return;
      const pairs=model.compare(left.report.tasks,right.report.tasks);comparison={ids,key,left,right,pairs};target.replaceChildren();
      target.append(el("h3","",t9n("Task-level regressions")),el("p","",t9n("Matched by logical task ID, falling back to exact occurrence ID. Repeated occurrences use arithmetic means; timing and score coverage are shown. Single runs do not establish statistical significance.")));
      const holder=el("div","table-scroll");target.append(holder);
      grid(holder,["Task key","Baseline mean","Candidate mean","Latency change","Score change","Occurrences / timed / scored","Outcomes","Evidence"],pairs.map(row=>{
        const l=row.baseline,r=row.candidate,links=el("div","insight-actions");if(l.tasks[0])links.append(referenceButton(ref(left.report.run_id,"task",l.tasks[0].id),"Baseline"));if(r.tasks[0])links.append(referenceButton(ref(right.report.run_id,"task",r.tasks[0].id),"Candidate"));
        return [row.key,format(l.duration_ms),format(r.duration_ms),row.delta_ms==null?t9n("Unpaired or timing unavailable"):`${row.delta_ms>=0?"+":"−"}${format(Math.abs(row.delta_ms))}${row.ratio==null?"":` (${row.ratio>=0?"+":""}${(row.ratio*100).toFixed(1)}%)`}`,l.score==null||r.score==null?"—":`${(r.score-l.score)>=0?"+":""}${(r.score-l.score).toFixed(3)}`,`${l.count}/${l.timed}/${l.scored} → ${r.count}/${r.timed}/${r.scored}`,`${l.failed} → ${r.failed} ${t9n("failed occurrences")}`,links];
      }));
      target.append(button("Explain these differences",()=>{scope={type:"comparison",runKey:state.runKey};$("#insight-scope option[value=comparison]").disabled=false;navigate("insights");refresh(true);}));
    }catch(e){target.textContent=t9n("Insights unavailable: {error}",{error:e.message});}
  }
  async function configureAI() {
    try{aiConfig=await json("/api/insights/ai");$("#insight-model-status").textContent=aiConfig.enabled?`${aiConfig.model} · ${aiConfig.endpoint}`:t9n("LLM is not configured. Set TRACEX_INSIGHT_ENDPOINT, TRACEX_INSIGHT_MODEL and optionally TRACEX_INSIGHT_API_KEY on the server. Local diagnostics remain available.");}
    catch{$("#insight-model-status").textContent=t9n("Model configuration is unavailable.");}updateAI();
  }
  async function interpret() {
    if(!packet||!aiConfig.enabled||!$("#insight-ai-consent").checked||aiController)return;
    const controller=new AbortController(),versionSent=packet.version,refs=new Map(packetRefs);aiController=controller;updateAI();$("#insight-ai-cancel").hidden=false;$("#insight-ai-status").textContent=t9n("Interpreting evidence…");
    try{
      const response=await fetch("/api/insights/ai",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({confirm:true,language:TraceXI18n.language,evidence:packet}),signal:controller.signal});
      const result=await response.json();if(!response.ok)throw Error(result.error || t9n("Model request failed"));if(controller!==aiController||versionSent!==packet?.version)return;
      aiResult={...result,refs};const target=$("#insight-ai-result");target.replaceChildren();
      for(const observation of result.analysis.observations){const card=el("article","finding-card");card.append(el("span","tag",t9n("AI interpretation · verify against evidence")),el("h3","",observation.fact),el("p","",t9n("Hypothesis: {text}",{text:observation.hypothesis})),el("p","",t9n("Next experiment: {text}",{text:observation.next_experiment})));const actions=el("div","insight-actions");observation.evidence_ids.forEach(id=>{if(refs.has(id))actions.append(referenceButton(refs.get(id)));});card.append(actions);target.append(card);}
      target.append(el("p","",result.analysis.limitations));$("#insight-ai-status").textContent=t9n("Interpretation complete. It is a hypothesis, not a measured causal conclusion.");$("#insight-ai-save").hidden=false;
    }catch(e){if(controller===aiController)$("#insight-ai-status").textContent=e.name==="AbortError"?t9n("Interpretation canceled. A provider request may already have started."):t9n("Interpretation failed: {error}",{error:e.message});}
    finally{if(controller===aiController){aiController=null;$("#insight-ai-consent").checked=false;$("#insight-ai-cancel").hidden=true;updateAI();}}
  }
  function persist(next) {try{localStorage.setItem(storageKey,JSON.stringify(next));notes=next;renderNotes();return true;}catch{notify(t9n("Could not save notebook. Browser storage is unavailable or full."));return false;}}
  function saveRecord(record,editID=null) {
    if(!editID&&notes.length>=100){notify(t9n("Notebook holds at most 100 notes. Export or remove notes before saving more."));return;}
    const item={...record,id:editID||crypto.randomUUID(),title:record.title.slice(0,160),text:record.text.slice(0,4000),created_at:new Date().toISOString()};
    const next=editID?notes.map(n=>n.id===editID?item:n):[item,...notes];
    if(persist(next)){notify(t9n("Investigation saved in this browser."));return true;}return false;
  }
  function renderNotes() {
    const target=$("#insight-notes");target.replaceChildren();
    const runID=current?.report.run_id || source(state.runKey)?.run_id,rows=notes.filter(n=>n.run_id===runID);
    if(!rows.length){target.append(el("p","",t9n("No saved investigations for this run.")));return;}
    for(const n of rows){const card=el("article","saved-investigation"),actions=el("div","insight-actions");card.append(el("h3","",n.title),el("small","",`${n.created_at} · ${n.version}`),el("p","note-text",n.text));
      n.refs.slice(0,5).forEach(r=>actions.append(referenceButton(r)));
      actions.append(button("Edit note",()=>{editing=n.id;draftContext=n;$("#insight-note-title").value=n.title;$("#insight-note-body").value=n.text;$("#insight-note-cancel").hidden=false;$("#insight-note-title").focus();}),button("Remove note",()=>{const removed=n;if(persist(notes.filter(row=>row.id!==n.id))){const undo=button("Undo removal",()=>{if(persist([removed,...notes]))undo.remove();});target.prepend(undo);}}));card.append(actions);target.append(card);
    }
  }
  function noteContext() {return {run_id:current?.report.run_id || source(state.runKey)?.run_id,version:current?version(current):"",scope:scope.type,refs:[...packetRefs.values()].slice(0,5)};}
  $("#insight-note-title").oninput=$("#insight-note-body").oninput=()=>{draftContext ||= noteContext();};
  $("#insight-note-save").onclick=()=>{const title=$("#insight-note-title").value.trim(),text=$("#insight-note-body").value.trim();if(!title||!text){notify(t9n("Enter a title and investigation note."));return;}if(saveRecord({...draftContext||noteContext(),title,text},editing))$("#insight-note-cancel").click();};
  $("#insight-note-cancel").onclick=()=>{editing=null;draftContext=null;$("#insight-note-title").value="";$("#insight-note-body").value="";$("#insight-note-cancel").hidden=true;};
  $("#insight-note-export").onclick=()=>{const url=URL.createObjectURL(new Blob([JSON.stringify(notes,null,2)],{type:"application/json"})),link=document.createElement("a");link.href=url;link.download="tracex-investigations.json";link.click();setTimeout(()=>URL.revokeObjectURL(url),1000);};
  $("#insight-ai-save").onclick=()=>{if(!aiResult)return;const refs=[...new Set(aiResult.analysis.observations.flatMap(o=>o.evidence_ids))].map(id=>aiResult.refs.get(id)).filter(Boolean);saveRecord({...noteContext(),title:t9n("LLM interpretation"),version:aiResult.version,model:aiResult.model,text:aiResult.analysis.observations.map(o=>`${o.fact}\n${o.hypothesis}\n${o.next_experiment}`).join("\n\n")+"\n"+aiResult.analysis.limitations,refs});};
  $("#insight-ai-start").onclick=interpret;$("#insight-ai-cancel").onclick=()=>aiController?.abort();$("#insight-ai-consent").onchange=updateAI;
  $("#open-insights").onclick=()=>openScope({type:"run"});$("#insight-window").onclick=()=>openScope({type:"window"});
  $("#refresh-insights").onclick=()=>{cache.clear();configureAI();refresh(true);};
  $("#insight-scope").onchange=event=>{const type=event.target.value;if(type==="window")openScope({type});else {scope=type==="comparison"?{type}:type==="span"?scope:{type:"run"};refresh(true);}};
  window.addEventListener("tracex:analyze",event=>openScope({type:"span",span_id:event.detail.span_id}));
  window.addEventListener("tracex:comparison",event=>comparisonReport(event.detail));
  window.addEventListener("tracex:render",()=>refresh());window.addEventListener("tracex:navigated",()=>refresh());
  configureAI();refresh();
})();
