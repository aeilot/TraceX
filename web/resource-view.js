(function(root){
  'use strict';
  function createWindow(start,end){
    const full={start,end,duration:end-start},history=[];let current={...full};
    return {
      get view(){return {...current};},get canBack(){return history.length>0;},get isFull(){return current.start===full.start&&current.end===full.end;},
      set(a,b,remember=true){
        if(!Number.isFinite(a)||!Number.isFinite(b)||b<=a)return false;
        const duration=Math.min(full.duration,Math.max(1,b-a)),lo=Math.max(full.start,Math.min(a,full.end-duration));
        if(Math.abs(lo-current.start)<.001&&Math.abs(duration-current.duration)<.001)return false;
        if(remember){history.push({...current});if(history.length>20)history.shift();}
        current={start:lo,end:lo+duration,duration};return true;
      },
      back(){if(!history.length)return false;current=history.pop();return true;},
      reset(){return this.set(full.start,full.end);},
    };
  }
  const seriesKey=r=>[r.task_id||'',r.component||'',r.runtime_id||''].join('\0');
  function slice(samples,view,task='all',runtime='all'){
    return samples.filter(r=>r.time_ms>=view.start&&r.time_ms<=view.end&&(task==='all'||!r.task_id||r.task_id===task)&&(runtime==='all'||(r.runtime_id||r.component)===runtime));
  }
  function rates(samples){
    const groups=new Map(),out=[];
    for(const r of samples){const id=seriesKey(r);if(!groups.has(id))groups.set(id,[]);groups.get(id).push(r);}
    for(const rs of groups.values()){
      rs.sort((a,b)=>a.time_ms-b.time_ms);
      for(let i=1;i<rs.length;i++){
        const a=rs[i-1],b=rs[i],dt=(b.time_ms-a.time_ms)/1000;let delta=0,known=0,reset=false;
        for(const field of ['network_rx_bytes','network_tx_bytes'])if(Number.isFinite(a[field])&&Number.isFinite(b[field])){known++;const d=b[field]-a[field];if(d<0)reset=true;delta+=d;}
        out.push({task_id:b.task_id,component:b.component,runtime_id:b.runtime_id,time_ms:b.time_ms,network_rate:dt>0&&known&&!reset?delta/dt:null});
      }
    }return out;
  }
  const api={createWindow,slice,rates,seriesKey};if(typeof module!=='undefined')module.exports=api;else root.TraceXResources=api;
})(globalThis);
