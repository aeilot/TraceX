(() => {
  'use strict';
  const m = window.TraceXExperiments;
  const t9n = window.TraceXI18n.t;
  const $ = id => document.getElementById(id);
  const panel = $('experiment-panel');
  let catalog = {enabled:false,ready:false,profiles:[]}, selected = [], jobs = [], pending = false, refreshing = false, timer;
  const opened = new Set();
  let runnerPending = false;
  const modelSettings = () => ({id:$('experiment-model-id').value.trim(),base_url:$('experiment-model-url').value.trim(),api_key_env:$('experiment-model-key-env').value.trim(),reasoning_effort:$('experiment-model-reasoning').value.trim()});
  function modelError() {
    if (!$('experiment-model-override').checked) return '';
    const s=modelSettings();let u;try {u=new URL(s.base_url);}catch{return t9n('Enter a model ID, HTTP(S) endpoint and credential environment variable.');}
    return s.id && ['http:','https:'].includes(u.protocol) && !u.username && !u.password && !u.search && !u.hash && /^[A-Za-z_][A-Za-z0-9_]*$/.test(s.api_key_env) ? '' : t9n('Enter a model ID, HTTP(S) endpoint and credential environment variable.');
  }
  function runnerFields() {
    const orb=$('runner-executor').value==='orb';$('runner-machine-field').hidden=!orb;$('runner-machine').required=orb;
    const busy=jobs.some(j=>m.active(j.status));
    for(const el of $('runner-form').elements) el.disabled=runnerPending || pending || busy;
  }
  async function loadRunner() {
    try {
      const s=await api('/api/experiments/runner');
      $('runner-root').value=s.root || '';$('runner-binary').value=s.binary || '';$('runner-executor').value=s.executor || 'local';$('runner-machine').value=s.machine || '';runnerFields();
    }catch(e){$('runner-message').textContent=e.message;}
  }
  async function runnerAction(action) {
    if(runnerPending || pending || jobs.some(j=>m.active(j.status))) return;
    runnerPending=true;updateValidation();$('runner-message').textContent=t9n('Inspecting runner settings…');
    try {
      if(action==='discover') {
        const d=await api('/api/experiments/discover');
        for(const [id,values] of [['runner-roots',d.roots],['runner-machines',d.machines]]) {
          $(id).replaceChildren();for(const value of values || []) {const o=document.createElement('option');o.value=value;$(id).append(o);}
        }
        if(!$('runner-root').value && d.roots?.length) $('runner-root').value=d.roots[0];
        if(!catalog.enabled && d.platform==='darwin' && d.orb_available) $('runner-executor').value='orb';
        if(!$('runner-machine').value && d.machines?.length===1) $('runner-machine').value=d.machines[0];
        $('runner-message').textContent=d.message ? t9n(d.message) : t9n('Found {roots} ARIES directories and {machines} machines. Review the target and save.',{roots:d.roots.length,machines:d.machines.length});
      } else if(action==='save') {
        const settings={root:$('runner-root').value.trim(),binary:$('runner-binary').value.trim(),executor:$('runner-executor').value,machine:$('runner-executor').value==='orb' ? $('runner-machine').value.trim() : ''};
        await api('/api/experiments/configure',settings);$('runner-checks').replaceChildren();resetAck();await loadCatalog();await loadRunner();$('experiment-config').open=true;
        $('runner-message').textContent=t9n('Runner settings saved. Check the execution host before preparing a workload.');
      } else {
        const result=await api('/api/experiments/check',{});$('runner-checks').replaceChildren();
        for(const check of result.checks || []) {const item=document.createElement('li');item.textContent=`${check.ok ? '✓' : '✗'} ${check.name}: ${t9n(check.message || (check.ok ? 'Available on execution host' : 'Unavailable or timed out on execution host'))}`;$('runner-checks').append(item);}
        $('runner-message').textContent=t9n('Host checks complete. Model endpoints and datasets were not contacted.');
      }
    }catch(e){$('runner-message').textContent=e.message;}
    finally {runnerPending=false;updateValidation();}
  }

  const profile = () => catalog.profiles.find(p => p.id === $('experiment-profile').value);
  const status = message => { $('experiment-message').textContent = message; };
  async function api(url, body) {
    const r = await fetch(url, body ? {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)} : {cache:'no-store'});
    const result = await r.json();
    if (!r.ok) throw new Error(result.error || result.message || t9n('Request failed ({status})', {status: r.status}));
    return result;
  }
  function resetAck() { $('experiment-ack').checked = false; }
  function updateValidation() {
    const p = profile(), concurrency = $('experiment-concurrency').value;
    const busy = jobs.some(j => m.active(j.status));
    const error = busy ? t9n('An experiment still owns the runner. Wait for completion or retry graceful stop before launching another.') : modelError() || m.validate(p, selected, concurrency, catalog.enabled && catalog.ready, $('experiment-ack').checked);
    $('experiment-validation').textContent = pending ? t9n('Submitting request. Please wait; do not retry an uncertain submission.') : t9n(error);
    runnerFields();
    $('experiment-start').disabled = pending || runnerPending || !!error;
    $('experiment-filter').disabled = pending;
    $('experiment-profile').disabled = pending;
    $('experiment-concurrency').disabled = pending;
    $('experiment-ack').disabled = pending || runnerPending;
    for(const id of ['override','id','url','key-env','reasoning']) $('experiment-model-'+id).disabled=pending || runnerPending;
    $('experiment-prepare').disabled = pending || runnerPending || busy || !!modelError() || !!m.validate(p, selected, concurrency, catalog.enabled && catalog.ready, false, 'prepare');
    $('experiment-ack-label').textContent = t9n(p && p.loop_duration ? 'I acknowledge this will run {count} selected task(s) using {target} and may incur provider charges across repeated loop executions.' : 'I acknowledge this will run {count} selected task(s) using {target} and may incur provider charges.', {count: selected.length, target: p ? [($('experiment-model-override').checked ? modelSettings().id : p.model) || t9n('configured model'),p.backend].filter(Boolean).join(' · ') : t9n('the configured model/provider')});
    $('experiment-task-count').textContent = t9n('{selected} / {total} selected', {selected: selected.length, total: p ? (p.tasks || []).length : 0});
  }
  function showTasks() {
    const p = profile(), target = $('experiment-task-list');
    target.replaceChildren();
    (p?.tasks || []).forEach(task => {
      const id = m.taskID(task), label = document.createElement('label'), box = document.createElement('input'), name = document.createElement('span');
      label.className = 'experiment-task-option'; box.type = 'checkbox'; box.checked = selected.includes(id); box.disabled = !!p.arrival_schedule || pending; name.textContent = typeof task === 'string' ? task : `${task.name || id}${task.name && task.name !== id ? ` (${id})` : ''}`;
      box.addEventListener('change', () => { selected = box.checked ? [...selected,id] : selected.filter(x => x !== id); resetAck();updateValidation(); });
      label.append(box,name);target.append(label);
    });
    $('experiment-select-all').disabled = !p || pending || !!p.arrival_schedule;
    $('experiment-clear').disabled = !p || pending || !!p.arrival_schedule;
    updateValidation();
  }
  function chooseProfile() {
    const p = profile();resetAck();
    $('experiment-model-override').checked=false;
    $('experiment-model-settings').hidden=!p?.model_editable;
    const settings=p?.model_settings || {};
    for(const [id,key] of [['id','id'],['url','base_url'],['key-env','api_key_env'],['reasoning','reasoning_effort']]) $('experiment-model-'+id).value=settings[key] || '';
    selected = (p?.tasks || []).map(m.taskID);
    $('experiment-concurrency').value = p?.concurrency || 1;
    $('experiment-profile-details').textContent = p ? t9n('Workload: {workload} · Harness: {harness} · Model: {model} · Backend: {backend}', {workload: p.workload || '—', harness: p.harness || '—', model: p.model || t9n('configured model'), backend: p.backend || '—'}) : t9n('No matching configured profiles.');
    const warnings = [];
    if(p?.loop_duration) warnings.push(t9n('Loop duration: {duration}. ARIES preserves this profile’s repeated execution behavior; task counts are selections, not total calls.', {duration: p.loop_duration}));
    if(p?.arrival_schedule) warnings.push(t9n('This profile has an arrival schedule. All declared tasks are required and the configured schedule is preserved.'));
    $('experiment-schedule').textContent = warnings.join(' ');$('experiment-schedule').hidden = !warnings.length;
    showTasks();
  }
  function renderProfiles() {
    const select = $('experiment-profile'), before = select.value;
    select.replaceChildren();
    m.filterProfiles(catalog.profiles || [], $('experiment-filter').value).forEach(p => { const o = document.createElement('option');o.value=p.id;o.textContent=`${p.name || p.id} · ${p.workload || 'workload'}`;select.append(o); });
    if([...select.options].some(o=>o.value === before)) select.value=before;
    if(select.value !== before || !profile()) chooseProfile();
    updateValidation();
  }
  function openRun(runID) { window.dispatchEvent(new CustomEvent('tracex:open-run',{detail:{run_id:runID}})); }
  function renderJobs() {
    updateValidation();
    const list = $('experiment-jobs');list.replaceChildren();
    if(!jobs.length) { const empty = document.createElement('p');empty.textContent=t9n('No experiments launched in this server session.');list.append(empty); }
    jobs.forEach(job => {
      const card = document.createElement('article'), heading = document.createElement('div'), title = document.createElement('h3'), badge = document.createElement('span'), details = document.createElement('p'), outcome = document.createElement('p'), actions = document.createElement('div');
      card.className='experiment-job';heading.className='experiment-job-heading';title.textContent=`${job.action === 'prepare' ? t9n('Prepare') : t9n('Run')} · ${job.name || job.profile_id}`;badge.className='experiment-job-status';badge.textContent=t9n(job.status);heading.append(title,badge);
      details.textContent=`${job.workload || ''} · ${job.harness || ''} · ${job.model || t9n('configured model')} · ${t9n('{count} tasks · concurrency {concurrency}', {count: (job.tasks || []).length, concurrency: job.concurrency || 1})}`;
      const exit = job.exit_code === undefined || job.exit_code === null ? '' : t9n('Process exit: {code}. ', {code: job.exit_code});
      outcome.textContent=`${exit}${job.action === 'prepare' ? t9n('Setup status only; no benchmark evaluation.') : t9n('Benchmark evaluation is reported in the live profile.')} ${job.harness_status ? t9n('Harness: {status}. ', {status: t9n(job.harness_status)}) : ''}${job.evaluation_status ? t9n('Evaluation: {status}. ', {status: t9n(job.evaluation_status)}) : ''}${job.cleanup_status ? t9n('Cleanup: {status}.', {status: t9n(job.cleanup_status)}) : m.active(job.status) ? t9n('Cleanup pending.') : t9n('Cleanup status not reported.')}${job.error ? ` ${job.error}` : ''}`;
      actions.className='experiment-job-actions';
      if(job.run_id) { const open = document.createElement('button');open.className='button small';open.textContent=t9n('Open live profile');open.onclick=()=>openRun(job.run_id);actions.append(open);if(m.active(job.status) && !opened.has(job.id)) {opened.add(job.id);openRun(job.run_id);} }
      else if(m.active(job.status) && job.action === 'run') { const wait = document.createElement('p');wait.textContent=t9n('Waiting for ARIES run artifacts to become available…');actions.append(wait); }
      if(m.active(job.status) && job.status !== 'stopping') { const stop=document.createElement('button');stop.className='button small';stop.textContent=job.status === 'stop_failed' ? t9n('Retry graceful stop') : t9n('Stop experiment');stop.onclick=async()=>{stop.disabled=true;try {await api('/api/experiments/stop',{job_id:job.id});await refreshJobs();}catch(e){status(t9n('Stop request failed: {error}', {error: e.message}));stop.disabled=false;}};actions.append(stop); }
      if(job.status==='stopping') { const note=document.createElement('p');note.textContent=t9n('Graceful cancellation requested; waiting for ARIES and cleanup.');actions.append(note); }
      card.append(heading,details,outcome,actions);list.append(card);
    });
  }
  function schedule() { clearTimeout(timer);if(!panel.hidden || jobs.some(j=>m.active(j.status))) timer=setTimeout(refreshJobs,1000); }
  async function refreshJobs() {
    if(refreshing) return;refreshing=true;
    try { const result=await api('/api/experiments/jobs');jobs=result.jobs || [];renderJobs(); } catch(e) {status(t9n('Could not refresh jobs: {error}', {error: e.message}));} finally {refreshing=false;schedule();}
  }
  async function loadCatalog() {
    try {
      catalog=await api('/api/experiments/catalog');catalog.profiles ||= [];
      $('experiment-form').hidden = !catalog.enabled || !catalog.ready;
      $('experiment-target').textContent=t9n('Execution target: {target}. Runs execute on this target, not inside the browser.', {target: `${catalog.executor || t9n('not configured')}${catalog.machine ? ` · ${t9n('machine {name}', {name: catalog.machine})}` : ''}`});
      status(t9n(catalog.message || (catalog.enabled && catalog.ready ? 'Runner configured. Check the execution host, then select a workload.' : 'ARIES runner is unavailable. Configure the server before launching.')));
      $('experiment-config').open=!(catalog.enabled && catalog.ready);renderProfiles();chooseProfile();
    } catch(e) {catalog.ready=false;$('experiment-form').hidden=true;status(t9n('Cannot load ARIES runner configuration: {error}', {error: e.message}));updateValidation();}
  }
  async function submit(action) {
    if(pending || runnerPending || jobs.some(j => m.active(j.status))) return;
    const p=profile(), error=modelError() || m.validate(p,selected,$('experiment-concurrency').value,catalog.enabled && catalog.ready,$('experiment-ack').checked,action);
    if(error) {status(t9n(error));return;}
    const request=m.request(p,selected,$('experiment-concurrency').value,$('experiment-ack').checked,action);
    if($('experiment-model-override').checked) request.model=modelSettings();
    pending=true;updateValidation();showTasks();
    try {const result=await api('/api/experiments/start',request);resetAck();status(t9n(action === 'prepare' ? 'Preparation submitted. Follow the job below.' : 'Experiment submitted. Follow the job below.'));if(result.job) {jobs=[result.job,...jobs.filter(j=>j.id!==result.job.id)];renderJobs();}await refreshJobs();}
    catch(e) { resetAck();status(t9n('Submission could not be confirmed: {error}. Refresh jobs before submitting again; the request is not automatically retried.', {error: e.message}));await refreshJobs(); }
    finally {pending=false;showTasks();schedule();}
  }
  $('runner-form').onsubmit=e=>{e.preventDefault();runnerAction('save');};
  $('runner-discover').onclick=()=>runnerAction('discover');$('runner-check').onclick=()=>runnerAction('check');$('runner-executor').onchange=runnerFields;
  for(const id of ['override','id','url','key-env','reasoning']) $('experiment-model-'+id).addEventListener('input',()=>{resetAck();updateValidation();});
  $('new-experiment').onclick=async()=>{TraceXMotion.visible(panel,true);$('new-experiment').setAttribute('aria-expanded','true');await loadCatalog();await loadRunner();await refreshJobs();panel.scrollIntoView({behavior:TraceXMotion.scrollBehavior(),block:'start'});};
  $('close-experiment').onclick=()=>{TraceXMotion.visible(panel,false);$('new-experiment').setAttribute('aria-expanded','false');$('new-experiment').focus();schedule();};
  $('experiment-filter').oninput=renderProfiles;$('experiment-profile').onchange=chooseProfile;
  $('experiment-concurrency').oninput=()=>{resetAck();updateValidation();};$('experiment-ack').onchange=updateValidation;
  $('experiment-select-all').onclick=()=>{selected=(profile()?.tasks || []).map(m.taskID);resetAck();showTasks();};$('experiment-clear').onclick=()=>{selected=[];resetAck();showTasks();};
  $('experiment-prepare').onclick=()=>submit('prepare');$('experiment-form').onsubmit=e=>{e.preventDefault();submit('run');};$('experiment-reload').onclick=refreshJobs;
})();
