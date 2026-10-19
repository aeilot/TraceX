// Projection and layout of recorded edges only. Folding never invents links.
(function(root){
  "use strict";
  function project(spans,edges,{expanded=new Set(),collapsed=new Set(),limits=new Map(),all=false,unlinked=false,focus=null,reveal=[]}={}){
    const byID=new Map(spans.map(s=>[s.id,s])),parents=new Map(spans.map(s=>[s.id,[]])),children=new Map(spans.map(s=>[s.id,[]]));
    const recorded=edges.filter(e=>byID.has(e.source)&&byID.has(e.target));
    for(const e of recorded){if(!children.get(e.source).includes(e.target))children.get(e.source).push(e.target);if(!parents.get(e.target).includes(e.source))parents.get(e.target).push(e.source);}
    for(const ids of children.values())ids.sort((a,b)=>byID.get(a).start_ms-byID.get(b).start_ms||a.localeCompare(b));
    const incoming=new Map([...parents].map(([id,ps])=>[id,ps.length])),depth=new Map(),order=spans.filter(s=>!incoming.get(s.id)).map(s=>s.id);
    for(const id of order)depth.set(id,0);
    for(let i=0;i<order.length;i++)for(const child of children.get(order[i])){depth.set(child,Math.max(depth.get(child)||0,(depth.get(order[i])||0)+1));incoming.set(child,incoming.get(child)-1);if(!incoming.get(child))order.push(child);}
    const forced=new Set(reveal.filter(id=>byID.has(id))),open=new Set(expanded),walk=[...forced];
    for(let i=0;i<walk.length;i++)for(const parent of parents.get(walk[i]))if(!open.has(parent)){open.add(parent);forced.add(parent);walk.push(parent);}
    const isolated=spans.filter(s=>!parents.get(s.id).length&&!children.get(s.id).length),visible=new Set();
    if(focus&&byID.has(focus)){
      visible.add(focus);parents.get(focus).forEach(id=>visible.add(id));children.get(focus).forEach(id=>visible.add(id));
    }else{
      for(const id of order){
        const ps=parents.get(id),linked=ps.length||children.get(id).length;
        if(!linked&&!unlinked&&recorded.length&&!forced.has(id))continue;
        if(all||forced.has(id)||!ps.length||ps.some(p=>visible.has(p)&&(!collapsed.has(p)&&((depth.get(p)||0)===0||open.has(p))&&children.get(p).indexOf(id)<(limits.get(p)??8))))visible.add(id);
      }
      // A malformed cyclic input must stay inspectable, never silently disappear.
      for(const s of spans)if(!depth.has(s.id))visible.add(s.id);
    }
    const nodes=spans.filter(s=>visible.has(s.id)),links=recorded.filter(e=>visible.has(e.source)&&visible.has(e.target)),folded=new Map();
    for(const s of nodes){const hidden=children.get(s.id).filter(id=>!visible.has(id));if(hidden.length)folded.set(s.id,hidden.length);}
    return {nodes,edges:links,folded,children,parents,depth,isolated:isolated.length,total:spans.length,autoUnlinked:recorded.length===0,hidden:spans.length-nodes.length};
  }
  function layout(graph){
    const columns=new Map(),depth=new Map(),parents=new Map(graph.nodes.map(s=>[s.id,[]])),children=new Map(graph.nodes.map(s=>[s.id,[]]));
    for(const e of graph.edges){parents.get(e.target).push(e.source);children.get(e.source).push(e.target);}
    // Compact depth in a focused neighborhood without retaining empty columns.
    const levels=[...new Set(graph.nodes.map(s=>graph.depth.get(s.id)||0))].sort((a,b)=>a-b);
    for(const s of graph.nodes){const d=levels.indexOf(graph.depth.get(s.id)||0);depth.set(s.id,d);if(!columns.has(d))columns.set(d,[]);columns.get(d).push(s);}
    for(const col of columns.values())col.sort((a,b)=>a.start_ms-b.start_ms||a.id.localeCompare(b.id));
    const keys=[...columns.keys()].sort((a,b)=>a-b),rank=new Map();
    const ranks=()=>{for(const col of columns.values())col.forEach((s,i)=>rank.set(s.id,i));};ranks();
    for(let pass=0;pass<4;pass++){
      const forward=pass%2===0,neighbors=forward?parents:children;
      for(const key of forward?keys:[...keys].reverse()){
        const col=columns.get(key),score=id=>{const ns=neighbors.get(id);return ns.length?ns.reduce((sum,n)=>sum+rank.get(n),0)/ns.length:rank.get(id);};
        col.sort((a,b)=>score(a.id)-score(b.id)||rank.get(a.id)-rank.get(b.id));ranks();
      }
    }
    const pos=new Map();let rows=0;
    for(const [d,col]of columns){rows=Math.max(rows,col.length);col.forEach((s,i)=>pos.set(s.id,{x:28+d*260,y:42+i*108}));}
    return {pos,columns,width:Math.max(300,levels.length*260),height:Math.max(220,rows*108+55)};
  }
  const api={project,layout};if(typeof module!=="undefined")module.exports=api;else root.TraceXDAG=api;
})(globalThis);
