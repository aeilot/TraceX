(() => {
  'use strict';
  const m = window.TraceXExperiments;
  const $ = id => document.getElementById(id);
  const panel = $('experiment-panel');
  let catalog = {enabled:false,ready:false,profiles:[]}, selected = [], jobs = [], pending = false, refreshing = false, timer;
  const opened = new Set();
  const profile = () => catalog.profiles.find(p => p.id === $('experiment-profile').value);
  const status = message => { $('experiment-message').textContent = message; };
  async function api(url, body) {
    const r = await fetch(url, body ? {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)} : {cache:'no-store'});
    const result = await r.json();
    if (!r.ok) throw new Error(result.error || result.message || `Request failed (${r.status})`);
    return result;
  }
  function resetAck() { $('experiment-ack').checked = false; }
  function updateValidation() {
    const p = profile(), concurrency = $('experiment-concurrency').value;
    const busy = jobs.some(j => m.active(j.status));
    const error = busy ? 'An experiment still owns the runner. Wait for completion or retry graceful stop before launching another.' : m.validate(p, selected, concurrency, catalog.enabled && catalog.ready, $('experiment-ack').checked);
    $('experiment-validation').textContent = pending ? 'Submitting request. Please wait; do not retry an uncertain submission.' : error;
    $('experiment-start').disabled = pending || !!error;
    $('experiment-filter').disabled = pending;
    $('experiment-profile').disabled = pending;
    $('experiment-concurrency').disabled = pending;
    $('experiment-ack').disabled = pending;
    $('experiment-prepare').disabled = pending || busy || !!m.validate(p, selected, concurrency, catalog.enabled && catalog.ready, false, 'prepare');
    $('experiment-ack-label').textContent = `I acknowledge this will run ${selected.length} selected task${selected.length === 1 ? '' : 's'} using ${p ? [p.model || 'configured model',p.backend].filter(Boolean).join(' · ') : 'the configured model/provider'} and may incur provider charges${p && p.loop_duration ? ' across repeated loop executions' : ''}.`;
    $('experiment-task-count').textContent = `${selected.length} / ${p ? (p.tasks || []).length : 0} selected`;
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
    const p = profile();resetAck();selected = (p?.tasks || []).map(m.taskID);
    $('experiment-concurrency').value = p?.concurrency || 1;
    $('experiment-profile-details').textContent = p ? `Workload: ${p.workload || '—'} · Harness: ${p.harness || '—'} · Model: ${p.model || 'configured model'} · Backend: ${p.backend || '—'}` : 'No matching configured profiles.';
    const warnings = [];
    if(p?.loop_duration) warnings.push(`Loop duration: ${p.loop_duration}. ARIES preserves this profile’s repeated execution behavior; task counts are selections, not total calls.`);
    if(p?.arrival_schedule) warnings.push('This profile has an arrival schedule. All declared tasks are required and the configured schedule is preserved.');
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
    if(!jobs.length) { const empty = document.createElement('p');empty.textContent='No experiments launched in this server session.';list.append(empty); }
    jobs.forEach(job => {
      const card = document.createElement('article'), heading = document.createElement('div'), title = document.createElement('h3'), badge = document.createElement('span'), details = document.createElement('p'), outcome = document.createElement('p'), actions = document.createElement('div');
      card.className='experiment-job';heading.className='experiment-job-heading';title.textContent=`${job.action === 'prepare' ? 'Prepare' : 'Run'} · ${job.name || job.profile_id}`;badge.className='experiment-job-status';badge.textContent=job.status;heading.append(title,badge);
      details.textContent=`${job.workload || ''} · ${job.harness || ''} · ${job.model || 'configured model'} · ${(job.tasks || []).length} tasks · concurrency ${job.concurrency || 1}`;
      const exit = job.exit_code === undefined || job.exit_code === null ? '' : `Process exit: ${job.exit_code}. `;
      outcome.textContent=`${exit}${job.action === 'prepare' ? 'Setup status only; no benchmark evaluation.' : 'Benchmark evaluation is reported in the live profile.'} ${job.harness_status ? `Harness: ${job.harness_status}. ` : ''}${job.evaluation_status ? `Evaluation: ${job.evaluation_status}. ` : ''}${job.cleanup_status ? `Cleanup: ${job.cleanup_status}.` : m.active(job.status) ? 'Cleanup pending.' : 'Cleanup status not reported.'}${job.error ? ` ${job.error}` : ''}`;
      actions.className='experiment-job-actions';
      if(job.run_id) { const open = document.createElement('button');open.className='button small';open.textContent='Open live profile';open.onclick=()=>openRun(job.run_id);actions.append(open);if(m.active(job.status) && !opened.has(job.id)) {opened.add(job.id);openRun(job.run_id);} }
      else if(m.active(job.status) && job.action === 'run') { const wait = document.createElement('p');wait.textContent='Waiting for ARIES run artifacts to become available…';actions.append(wait); }
      if(m.active(job.status) && job.status !== 'stopping') { const stop=document.createElement('button');stop.className='button small';stop.textContent=job.status === 'stop_failed' ? 'Retry graceful stop' : 'Stop experiment';stop.onclick=async()=>{stop.disabled=true;try {await api('/api/experiments/stop',{job_id:job.id});await refreshJobs();}catch(e){status(`Stop request failed: ${e.message}`);stop.disabled=false;}};actions.append(stop); }
      if(job.status==='stopping') { const note=document.createElement('p');note.textContent='Graceful cancellation requested; waiting for ARIES and cleanup.';actions.append(note); }
      card.append(heading,details,outcome,actions);list.append(card);
    });
  }
  function schedule() { clearTimeout(timer);if(!panel.hidden || jobs.some(j=>m.active(j.status))) timer=setTimeout(refreshJobs,1000); }
  async function refreshJobs() {
    if(refreshing) return;refreshing=true;
    try { const result=await api('/api/experiments/jobs');jobs=result.jobs || [];renderJobs(); } catch(e) {status(`Could not refresh jobs: ${e.message}`);} finally {refreshing=false;schedule();}
  }
  async function loadCatalog() {
    try {
      catalog=await api('/api/experiments/catalog');catalog.profiles ||= [];
      $('experiment-target').textContent=`Execution target: ${catalog.executor || 'not configured'}${catalog.machine ? ` · machine ${catalog.machine}` : ''}. Runs execute on this target, not inside the browser.`;
      status(catalog.message || (catalog.enabled && catalog.ready ? 'Runner ready. Select a configured workload.' : 'ARIES runner is unavailable. Configure the server before launching.'));
      $('experiment-config').hidden=catalog.enabled && catalog.ready;renderProfiles();
    } catch(e) {catalog.ready=false;status(`Cannot load ARIES runner configuration: ${e.message}`);updateValidation();}
  }
  async function submit(action) {
    if(pending || jobs.some(j => m.active(j.status))) return;
    const p=profile(), error=m.validate(p,selected,$('experiment-concurrency').value,catalog.enabled && catalog.ready,$('experiment-ack').checked,action);
    if(error) {status(error);return;}
    const request=m.request(p,selected,$('experiment-concurrency').value,$('experiment-ack').checked,action);
    pending=true;updateValidation();showTasks();
    try {const result=await api('/api/experiments/start',request);resetAck();status(`${action === 'prepare' ? 'Preparation' : 'Experiment'} submitted. Follow the job below.`);if(result.job) {jobs=[result.job,...jobs.filter(j=>j.id!==result.job.id)];renderJobs();}await refreshJobs();}
    catch(e) { resetAck();status(`Submission could not be confirmed: ${e.message}. Refresh jobs before submitting again; the request is not automatically retried.`);await refreshJobs(); }
    finally {pending=false;showTasks();schedule();}
  }
  $('new-experiment').onclick=async()=>{panel.hidden=false;$('new-experiment').setAttribute('aria-expanded','true');await loadCatalog();await refreshJobs();panel.scrollIntoView({behavior:'smooth',block:'start'});};
  $('close-experiment').onclick=()=>{panel.hidden=true;$('new-experiment').setAttribute('aria-expanded','false');$('new-experiment').focus();schedule();};
  $('experiment-filter').oninput=renderProfiles;$('experiment-profile').onchange=chooseProfile;
  $('experiment-concurrency').oninput=()=>{resetAck();updateValidation();};$('experiment-ack').onchange=updateValidation;
  $('experiment-select-all').onclick=()=>{selected=(profile()?.tasks || []).map(m.taskID);resetAck();showTasks();};$('experiment-clear').onclick=()=>{selected=[];resetAck();showTasks();};
  $('experiment-prepare').onclick=()=>submit('prepare');$('experiment-form').onsubmit=e=>{e.preventDefault();submit('run');};$('experiment-reload').onclick=refreshJobs;
})();
