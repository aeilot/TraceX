// Optional real launcher smoke: make build; NODE_PATH=<playwright packages> node scripts/launcher-smoke.cjs
// Creates its own harmless fixture root/server on :4319. Set TRACEX_URL to test
// an already configured fixture server. Refuses real model/backend profiles.
const assert = require('node:assert/strict');
async function smoke(page, baseURL='http://127.0.0.1:4319') {
  const errors=[]; page.on('pageerror',e=>errors.push(e.message));
  await page.goto(baseURL);
  await page.waitForSelector('.dag-node');
  const catalog=await page.evaluate(()=>fetch('/api/experiments/catalog').then(r=>r.json()));
  assert.equal(catalog.enabled,true);assert.equal(catalog.ready,true);
  assert.ok(catalog.profiles.length>=2);
  assert.ok(catalog.profiles.every(p=>p.workload.startsWith('fixture-') && p.backend==='local-fixture' && p.model==='no-model-calls'),'Only harmless fixture profiles are permitted');
  await page.locator('#new-experiment').click();
  await page.waitForFunction(()=>document.querySelector('#experiment-profile').options.length>=2);
  await page.locator('#experiment-filter').fill('terminal');
  assert.equal(await page.locator('#experiment-profile option').count(),1);
  assert.match(await page.locator('#experiment-profile-details').textContent(),/fixture-terminal/);
  await page.locator('#experiment-filter').fill('browser');
  await page.waitForFunction(()=>document.querySelector('#experiment-profile-details').textContent.includes('fixture-browser'));
  assert.equal(await page.locator('#experiment-task-list input').count(),2);
  await page.locator('#experiment-task-list input').nth(1).uncheck();
  assert.match(await page.locator('#experiment-task-count').textContent(),/1 \/ 2/);
  await page.locator('#experiment-concurrency').fill('2');
  assert.equal(await page.locator('#experiment-start').isDisabled(),true);
  await page.locator('#experiment-ack').check();
  assert.equal(await page.locator('#experiment-start').isEnabled(),true);
  await page.locator('#experiment-concurrency').fill('3');
  assert.equal(await page.locator('#experiment-ack').isChecked(),false);
  assert.equal(await page.locator('#experiment-start').isDisabled(),true);
  await page.locator('#experiment-prepare').click();
  await page.waitForFunction(()=>document.querySelector('#experiment-jobs').textContent.includes('completed'));
  assert.match(await page.locator('#experiment-jobs').innerText(),/Setup status only/);
  assert.equal(await page.locator('#experiment-start').isDisabled(),true);
  await page.locator('#experiment-ack').check();
  await page.locator('#experiment-start').click();
  await page.waitForFunction(()=>document.querySelector('#experiment-jobs').textContent.includes('Open live profile'));
  const running=await page.evaluate(async()=>{const {jobs}=await fetch('/api/experiments/jobs').then(r=>r.json());return jobs.find(j=>j.action==='run');});
  assert.equal(running.status,'running');assert.deepEqual(running.tasks,['fixture-a']);assert.equal(running.concurrency,3);assert.ok(running.run_id);
  await page.waitForFunction(id=>document.querySelector('#run-picker').value===id,running.run_id);
  const initial=await page.evaluate(id=>fetch('/api/profile?run='+id).then(r=>r.json()),running.run_id);
  assert.equal(initial.name,'Synthetic launcher fixture');assert.ok(initial.spans.length>0);
  await page.waitForSelector('.dag-node[aria-label*="Synthetic fixture agent"]');
  await page.waitForFunction(async({id,duration})=>{const p=await fetch('/api/profile?run='+id).then(r=>r.json());return p.duration_ms>duration;},{id:running.run_id,duration:initial.duration_ms});
  await page.locator('[data-view="timeline"]').click();
  await page.waitForSelector('.timeline-bar');
  assert.equal(await page.locator('#experiment-start').isDisabled(),true);
  await page.getByRole('button',{name:'Stop experiment',exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('#experiment-jobs').textContent.includes('cancelled'));
  const stopped=await page.evaluate(async()=>{const {jobs}=await fetch('/api/experiments/jobs').then(r=>r.json());return jobs.find(j=>j.action==='run');});
  assert.equal(stopped.status,'cancelled');assert.equal(stopped.cleanup_status,'confirmed');assert.equal(stopped.exit_code,0);
  assert.deepEqual(errors,[]);
  if(process.env.TRACEX_SCREENSHOT) await page.screenshot({path:process.env.TRACEX_SCREENSHOT,fullPage:true});
  return {result:'passed',prepare:'completed',run:stopped.status,cleanup:stopped.cleanup_status,run_id:running.run_id,concurrency:stopped.concurrency,tasks:stopped.tasks,live_duration_ms:initial.duration_ms};
}
module.exports={smoke};
async function fixtureServer() {
  const fs=require('node:fs/promises'),path=require('node:path'),{spawn}=require('node:child_process');
  await fs.mkdir('.cache',{recursive:true});
  const root=await fs.mkdtemp(path.resolve('.cache/launcher-smoke-'));
  await fs.mkdir(path.join(root,'profiles'));await fs.mkdir(path.join(root,'bin'));
  const helper=String.raw`const fs=require('node:fs'),path=require('node:path');
if(process.argv[2]==='setup') process.exit(0);
const cfg=JSON.parse(fs.readFileSync(process.argv[2])),run=path.join(cfg.output_dir,'synthetic-launcher-fixture');
fs.mkdirSync(run,{recursive:true,mode:0o700});
let seq=0;const task=cfg.benchmark.tasks[0],started=Date.now();
const emit=(type,value)=>fs.appendFileSync(path.join(run,'events.jsonl'),JSON.stringify({schema_version:1,sequence:++seq,type,[type]:value})+'\n',{mode:0o600});
function update(){const duration=Math.max(100,Date.now()-started);emit('metadata',{run_id:'synthetic-launcher-fixture',name:'Synthetic launcher fixture',duration_ms:duration});emit('span',{id:'fixture-agent',task_id:task,agent_id:'fixture-agent',name:'Synthetic fixture agent',kind:'agent',status:'running',duration_ms:duration,source:'Harmless launcher fixture'});}
emit('task',{id:task,name:task,status:'running',duration_ms:100});update();
const timer=setInterval(update,300);
process.on('SIGINT',()=>{clearInterval(timer);fs.writeFileSync(path.join(run,'run-result.json'),JSON.stringify({run_id:'synthetic-launcher-fixture',name:'Synthetic launcher fixture',tasks:[{task_id:task,started_at:new Date(started).toISOString(),duration:(Date.now()-started)*1e6,harness:{status:'cancelled'},evaluation:{status:'not_run'},cleanup:{status:'succeeded'}}],infrastructure:{cleanup:{status:'succeeded'}}}),{mode:0o600});process.exit(0);});`;
  await fs.writeFile(path.join(root,'bin','aries'),'#!'+process.execPath+'\n'+helper,{mode:0o700});
  for(const [name,tasks] of [['browser',['fixture-a','fixture-b']],['terminal',['fixture-c']]]) await fs.writeFile(path.join(root,'profiles',name+'.json'),JSON.stringify({name:'Synthetic '+name+' smoke',benchmark:{type:'fixture-'+name,tasks},harness:{type:'fixture-harness'},model:{id:'no-model-calls'},runtime:{backend:'local-fixture'},execution:{concurrency:1}}),{mode:0o600});
  const child=spawn(process.env.TRACEX_BINARY || path.resolve('bin/tracex'),['--addr','127.0.0.1:4319','--aries-dir',root,'--stop-timeout','5s'],{stdio:['ignore','pipe','pipe']});
  let output='';child.stdout.on('data',b=>output+=b);child.stderr.on('data',b=>output+=b);
  let closed=false;const exit=new Promise((resolve,reject)=>{child.once('error',reject);child.once('exit',code=>{closed=true;resolve(code);});});
  const close=async()=>{if(!closed) child.kill('SIGINT');await Promise.race([exit,new Promise((_,reject)=>{const timer=setTimeout(()=>reject(new Error('Fixture server did not finish graceful shutdown; inspect '+root)),8000);timer.unref();})]);};
  try {await Promise.race([new Promise(resolve=>child.stdout.on('data',()=>{if(output.includes('TraceX:'))resolve();})),exit.then(code=>{throw new Error('Fixture server exited '+code+': '+output);})]);}
  catch(e){await close();throw e;}
  return {url:'http://127.0.0.1:4319',close};
}
if(require.main===module) (async()=>{
  const fixture=process.env.TRACEX_URL?null:await fixtureServer();
  let browser;
  try {const {chromium}=require('playwright');browser=await chromium.launch({headless:true,channel:'chrome'});console.log(await smoke(await browser.newPage({viewport:{width:1440,height:1050}}),process.env.TRACEX_URL || fixture.url));}
  finally {if(browser)await browser.close();if(fixture)await fixture.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
