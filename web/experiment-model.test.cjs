const test = require('node:test');
const assert = require('node:assert/strict');
const m = require('./experiment-model.js');
const p = {id:'browser', name:'Browser run', workload:'browserbench', harness:'Hermes', model:'provider/model', tasks:['a','b']};
test('filters workload and backend descriptions', () => { assert.deepEqual(m.filterProfiles([p], 'browserbench'), [p]); assert.deepEqual(m.filterProfiles([p], 'missing'), []); });
test('requires explicit model acknowledgment only for runs', () => { assert.match(m.validate(p,['a'],1,true,false), /Acknowledge/); assert.equal(m.validate(p,['a'],1,true,false,'prepare'),''); });
test('preserves declared task ordering and ignores undeclared tasks', () => { assert.deepEqual(m.request(p,['unknown','b','a'],2,true,'run'), {profile_id:'browser',action:'run',tasks:['a','b'],concurrency:2,confirm_model_calls:true}); });
test('arrival schedules cannot be subset and concurrency must be integral', () => { assert.match(m.validate({...p,arrival_schedule:[1]},['a'],1,true,true), /all declared/); assert.match(m.validate(p,['a'],1.5,true,true), /integer/); });
test('disabled runners and empty task selections cannot launch', () => { assert.match(m.validate(p,['a'],1,false,true), /not ready/); assert.match(m.validate(p,[],1,true,true), /at least one/); assert.equal(m.request(p,['a'],1,true,'prepare').confirm_model_calls,false); });

test('stop_failed still owns the runner until process exit', () => { for (const status of ['starting','running','stopping','stop_failed']) assert.equal(m.active(status), true); for (const status of ['succeeded','failed','canceled']) assert.equal(m.active(status), false); });
