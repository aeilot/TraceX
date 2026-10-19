// Timeline input and latest-request scheduling, independent of SVG rendering.
(function(root) {
  "use strict";
  function createLoader({fetch:fetcher,result,error,busy,delay=60}) {
    let active=null,pending=null,timer=null,generation=0;
    function start() {
      timer=null;
      if(!pending)return;
      const spec=pending;pending=null;
      const controller=new AbortController(),token=++generation;
      active={spec,controller};busy(true);
      Promise.resolve().then(()=>fetcher(spec,controller.signal)).then(value=>{
        if(token===generation)result(value);
      }).catch(e=>{if(token===generation&&e.name!=="AbortError")error(e);}).finally(()=>{
        if(token===generation){active=null;busy(false);}
      });
    }
    return {
      request(spec) {
        // Retain an in-flight buffer that already covers the latest view.
        if(active&&spec.key===active.spec.key&&spec.px/active.spec.px<1.5&&spec.px/active.spec.px>1/1.5&&active.spec.buffer[0]<=spec.view.start&&active.spec.buffer[1]>=spec.view.end)return;
        if(pending?.id===spec.id||active?.spec.id===spec.id)return;
        const wasActive=!!active;
        if(active){generation++;active.controller.abort();active=null;}
        pending=spec;
        if(!timer){if(wasActive)timer=setTimeout(start,delay);else start();}
      },
      cancel() {generation++;active?.controller.abort();active=pending=null;clearTimeout(timer);timer=null;busy(false);},
    };
  }
  function bindGestures(svg,{geometry,view,setView,mode,select,overlay,active,settle}) {
    let gesture=null,suppressClick=false;
    function finish(cancel=false) {
      const g=gesture;if(!g)return;
      gesture=null;overlay(null);svg.classList.remove("panning","selecting");
      if(svg.hasPointerCapture(g.id))svg.releasePointerCapture(g.id);
      if(cancel&&g.moved&&!g.select)setView(g.view.start,g.view.end);
      if(!cancel&&g.select&&g.moved)select(...g.range);
      active(false);settle();
    }
    svg.addEventListener("pointerdown",e=>{
      if(e.button!==0||!e.isPrimary||gesture)return;
      const geo=geometry(),rect=svg.getBoundingClientRect(),x=e.clientX-rect.left-geo.labelWidth;
      if(x<0)return;
      e.preventDefault();svg.focus({preventScroll:true});
      gesture={id:e.pointerId,x:e.clientX,origin:Math.max(0,Math.min(geo.plotWidth,x)),rect,geo,view:view(),select:e.shiftKey||mode()==="select",moved:false};
      active(true,gesture.select?"select":"pan");
    });
    svg.addEventListener("pointermove",e=>{
      const g=gesture;if(!g||e.pointerId!==g.id)return;
      if(!g.moved&&Math.abs(e.clientX-g.x)<5)return;
      e.preventDefault();if(!g.moved)svg.setPointerCapture(e.pointerId);g.moved=true;suppressClick=true;
      svg.classList.add(g.select?"selecting":"panning");
      if(g.select){
        const x=Math.max(0,Math.min(g.geo.plotWidth,e.clientX-g.rect.left-g.geo.labelWidth)),lo=Math.min(x,g.origin),hi=Math.max(x,g.origin);
        overlay({x:lo+g.geo.labelWidth,width:hi-lo});
        g.range=[g.view.start+lo/g.geo.plotWidth*g.view.duration,g.view.start+hi/g.geo.plotWidth*g.view.duration];
      }else{
        const dt=(g.x-e.clientX)/g.geo.plotWidth*g.view.duration;
        setView(g.view.start+dt,g.view.end+dt);
      }
    });
    svg.addEventListener("pointerup",e=>{if(gesture?.id===e.pointerId)finish();});
    svg.ownerDocument?.addEventListener("pointerup",()=>finish());
    svg.addEventListener("pointercancel",()=>finish(true));
    svg.addEventListener("lostpointercapture",()=>finish(true));
    svg.addEventListener("click",e=>{if(suppressClick){suppressClick=false;e.preventDefault();e.stopImmediatePropagation();}},true);
    svg.addEventListener("pointerdown",()=>{suppressClick=false;},true);
    svg.addEventListener("keydown",e=>{if(e.key==="Escape"&&gesture){e.preventDefault();e.stopPropagation();finish(true);}});
    return {cancel:()=>finish(true)};
  }
  const api={createLoader,bindGestures};
  if(typeof module!=="undefined")module.exports=api;else root.TraceXTimeline=api;
})(globalThis);
