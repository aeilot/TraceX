(function (root) {
  'use strict';
  const api = {
    filterProfiles(profiles, query) {
      const needle = String(query || '').trim().toLowerCase();
      return profiles.filter(p => [p.name, p.workload, p.harness, p.model, p.backend].join(' ').toLowerCase().includes(needle));
    },
    taskID(task) { return typeof task === 'string' ? task : String(task.id || task.name || ''); },
    selectedTasks(profile, selected) {
      return (profile.tasks || []).map(api.taskID).filter(id => selected.includes(id));
    },
    validate(profile, selected, concurrency, ready, acknowledged, action = 'run', requests = 1) {
      if (!ready) return 'Runner is not ready.';
      if (!profile) return 'Select a profile.';
      // AgentSysBench sends the first N manifest requests instead of selected tasks.
      if (profile.request_count) {
        if (action === 'run' && (!Number.isInteger(Number(requests)) || Number(requests) < 1)) return 'Request count must be a positive integer.';
      } else if (!api.selectedTasks(profile, selected).length) return 'Select at least one task.';
      const tasks = api.selectedTasks(profile, selected);
      if (!profile.concurrency_fixed && (!Number.isInteger(Number(concurrency)) || Number(concurrency) < 1)) return 'Concurrency must be a positive integer.';
      if (profile.arrival_schedule && tasks.length !== (profile.tasks || []).length) return 'Arrival schedules require all declared tasks.';
      if (action === 'run' && !acknowledged) return 'Acknowledge real model calls before starting.';
      return '';
    },
    request(profile, selected, concurrency, acknowledged, action, requests = 1) {
      const confirm = action === 'run' && acknowledged === true;
      if (profile.request_count) {
        const r = { profile_id: profile.id, action, confirm_model_calls: confirm };
        if (action === 'run') r.request_num = Number(requests);
        if (!profile.concurrency_fixed) r.concurrency = Number(concurrency);
        return r;
      }
      return { profile_id: profile.id, action, tasks: api.selectedTasks(profile, selected), concurrency: Number(concurrency), confirm_model_calls: confirm };
    },
    active(status) { return ['starting', 'running', 'stopping', 'stop_failed'].includes(status); }
  };
  root.TraceXExperiments = api;
  if (typeof module !== 'undefined') module.exports = api;
})(typeof globalThis !== 'undefined' ? globalThis : this);
