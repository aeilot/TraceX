const {test} = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const fs = require("node:fs");
const path = require("node:path");

function fixture(reduced = false) {
  const timers = new Map(); let id = 0, onPreferenceChange;
  const preference = {matches:reduced, addEventListener:(_, fn) => onPreferenceChange = fn};
  const context = vm.createContext({
    matchMedia:() => preference,
    getComputedStyle:() => ({getPropertyValue:() => "350ms"}),
    setTimeout:fn => {timers.set(++id,fn); return id;},
    clearTimeout:id => timers.delete(id),
  });
  vm.runInContext(fs.readFileSync(path.join(__dirname,"motion.js"),"utf8"),context);
  const element = (toast = false) => {
    const classes = new Set(toast ? ["t-toast"] : []), attrs = new Map();
    return {hidden:true, dataset:{open:"false"}, offsetWidth:400,
      classList:{contains:c => classes.has(c), add:c => classes.add(c), remove:c => classes.delete(c)},
      setAttribute:(k,v) => attrs.set(k,v), removeAttribute:k => attrs.delete(k),
      getAttribute:k => attrs.get(k)};
  };
  return {motion:context.TraceXMotion, timers, element,
    flush:() => { for (const [id,fn] of [...timers]) {timers.delete(id);fn();} },
    reduce:() => {preference.matches = true;onPreferenceChange();}};
}
test("rapid close/reopen cannot let an old close timer hide the active panel", () => {
  const f = fixture(), el = f.element();
  f.motion.visible(el,true); f.motion.visible(el,false);
  assert.equal(el.hidden,false); assert.equal(el.inert,true);
  f.motion.visible(el,true); f.flush();
  assert.equal(el.hidden,false); assert.equal(el.dataset.open,"true");
  assert.equal(el.inert,false); assert.equal(el.getAttribute("aria-hidden"),undefined);
  assert.equal(f.timers.size,0);
});
test("closed panels leave the focus/accessibility tree immediately and layout after exit", () => {
  const f = fixture(), el = f.element(); f.motion.visible(el,true); f.motion.visible(el,false);
  assert.equal(el.getAttribute("aria-hidden"),"true"); assert.equal(el.inert,true);
  assert.equal(el.hidden,false); f.flush(); assert.equal(el.hidden,true);
  f.motion.visible(el,true); f.motion.visible(el,false,{immediate:true});
  assert.equal(el.hidden,true); assert.equal(f.timers.size,0);
});
test("reduced motion skips exit waits and responds to preference changes", () => {
  const f = fixture(true), el = f.element(); f.motion.visible(el,true); f.motion.visible(el,false);
  assert.equal(el.hidden,true); assert.equal(f.timers.size,0);
  const g = fixture(), other = g.element(); g.motion.visible(other,true); g.motion.visible(other,false);
  g.reduce(); assert.equal(other.hidden,true); assert.equal(g.timers.size,0);
  assert.equal(g.motion.scrollBehavior(),"instant");
});
test("a new toast cancels an earlier dismissal", () => {
  const f = fixture(), el = f.element(true);
  f.motion.visible(el,true); f.motion.visible(el,false); f.motion.visible(el,true); f.flush();
  assert.equal(el.hidden,false); assert.equal(el.classList.contains("is-open"),true);
});
