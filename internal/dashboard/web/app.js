const $ = (selector) => document.querySelector(selector);
const state = { overview: null, probe: null, protocol: null, runs: [], toastTimer: null };
const EXPECTED = ['open5gs-amf', 'open5gs-smf', 'open5gs-udr', 'open5gs-upf', 'ueransim-gnb', 'ueransim-ue'];

function toast(message) {
  const el = $('#toast');
  el.textContent = message;
  el.classList.add('show');
  clearTimeout(state.toastTimer);
  state.toastTimer = setTimeout(() => el.classList.remove('show'), 6000);
}

function displayTime(value) {
  if (!value) return '';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '' : date.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
}

function clockTime(value) {
  const date = value ? new Date(value) : new Date();
  return Number.isNaN(date.getTime()) ? '' : new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(date);
}

// kubectl errors repeat client-go log lines; show one readable sentence and
// keep the raw text available on request.
function summarizeError(detail) {
  const text = String(detail || '').trim();
  if (!text) return '';
  if (/connection refused|was refused|no such host|i\/o timeout|Unable to connect/i.test(text)) return 'kubectl could not reach the cluster API.';
  if (/context .* does not exist|current-context is not set/i.test(text)) return 'The kubectl context open5gs-lab is not configured.';
  const last = text.split(/\r?\n/).pop().replace(/^kubectl failed:\s*/, '');
  return last.length > 160 ? `${last.slice(0, 157)}...` : last;
}

function setRaw(selector, detail) {
  const box = $(selector);
  const raw = String(detail || '').trim();
  box.hidden = !raw || summarizeError(raw) === raw;
  box.querySelector('pre').textContent = raw;
}

function selectedTarget() {
  return document.querySelector('input[name="target"]:checked')?.value || 'amf';
}

function deploymentReady(report, name) {
  return Boolean(report?.inventory?.deployments?.some((item) => item.name === name && Number(item.ready_replicas) === 1 && Number(item.replicas) === 1));
}

function updateTopology(report) {
  const deployments = report?.inventory?.deployments || [];
  for (const node of document.querySelectorAll('[data-workload]')) {
    node.classList.remove('ready', 'missing');
    const deployment = deployments.find((item) => item.name === node.dataset.workload);
    const label = node.querySelector('.nf-state');
    if (!report) { label.textContent = 'Not observed'; continue; }
    if (!deployment) { node.classList.add('missing'); label.textContent = 'Deployment not found'; continue; }
    const ready = Number(deployment.ready_replicas || 0);
    const desired = Number(deployment.replicas || 0);
    node.classList.add(ready > 0 && ready === desired ? 'ready' : 'missing');
    label.textContent = `${ready} of ${desired} pods ready`;
  }
  $('#topology-updated').textContent = report
    ? `Pod readiness from deployment status in ${report.namespace || 'open5gs'}, read at ${displayTime(report.generated_at)}.`
    : 'Showing the expected lab layout. Nothing on it has been observed.';
}

function setGate(name, pass) {
  const item = document.querySelector(`[data-gate="${name}"]`);
  item.classList.toggle('pass', pass === true);
  item.classList.toggle('fail', pass === false);
}

function updateGates() {
  const report = state.overview?.report;
  const target = selectedTarget();
  const known = Boolean(report && state.overview?.connected);
  const correctContext = report?.context === 'open5gs-lab';
  const targetReady = deploymentReady(report, `open5gs-${target}`);
  const expectedReady = EXPECTED.every((name) => deploymentReady(report, name));
  const pathHealthy = state.probe?.status === 'healthy';
  setGate('context', known ? correctContext : null);
  setGate('target', known ? targetReady : null);
  setGate('workloads', known ? expectedReady : null);
  const probeRan = state.probe?.status === 'healthy' || state.probe?.status === 'degraded';
  setGate('path', known && probeRan ? pathHealthy : null);
  const n2Up = state.protocol?.links?.n2 === 'up';
  const n4Up = state.protocol?.links?.n4 === 'up';
  setGate('n2', known && state.protocol ? n2Up : null);
  setGate('n4', known && state.protocol ? n4Up : null);
  const name = target.toUpperCase();
  $('#run-button').textContent = `Delete ${name} pod`;
  $('#run-button').disabled = !(correctContext && targetReady && expectedReady && pathHealthy && n2Up && n4Up);
}

function updateOverview(data) {
  state.overview = data;
  const status = $('#lab-status');
  status.dataset.state = data.connected ? 'live' : 'offline';
  $('#status-title').textContent = data.connected ? `Connected to ${data.report.context}` : 'Lab offline';
  $('#status-detail').textContent = data.connected
    ? 'Read-only discovery of the open5gs namespace.'
    : 'kubectl could not reach the lab. The page shows the expected layout only; no live data is shown.';
  $('#checked-at').textContent = data.checked_at ? `Checked ${clockTime(data.checked_at)}` : '';
  updateTopology(data.report);
  updateGates();
}

async function refreshOverview() {
  const button = $('#refresh-button');
  if (button.disabled) return;
  button.disabled = true;
  button.textContent = 'Checking';
  try {
    const response = await fetch('/api/overview', { cache: 'no-store' });
    if (!response.ok) throw new Error(`Discovery failed with HTTP ${response.status}.`);
    updateOverview(await response.json());
  } catch (error) {
    updateOverview({ connected: false });
    $('#status-detail').textContent = 'The page could not reach the CorePulse server. Check that corepulse serve is still running.';
    toast(error.message);
  } finally {
    button.disabled = false;
    button.textContent = 'Refresh';
  }
}

function packetOutcome(run) {
  // Baselines keep every probe; newer fault runs keep a recovery series;
  // older fault runs only kept the final probe.
  if (Array.isArray(run.service_probes) && run.service_probes.length) {
    return run.service_probes.every((probe) => probe.status === 'healthy')
      ? { text: 'Delivered', tone: 'good' } : { text: 'Failing', tone: 'bad' };
  }
  const service = run.service_recovery?.status;
  if (service === 'service_recovered') return { text: 'Delivered', tone: 'good' };
  if (service === 'service_degraded') return { text: 'Failing', tone: 'bad' };
  if (service) return { text: 'Not verified', tone: 'partial' };
  const probe = run.service_probe?.status;
  if (probe === 'healthy') return { text: 'Delivered', tone: 'good' };
  if (probe === 'degraded') return { text: 'Failing', tone: 'bad' };
  return { text: 'Not recorded', tone: '' };
}

function n2Outcome(run) {
  const status = run.control_plane_recovery?.status;
  if (status === 'n2_lost') return { text: 'Lost', tone: 'bad' };
  if (status === 'n2_restored') return { text: 'Restored', tone: 'good' };
  // No termination during the run; N2 may already have been down before it.
  if (status === 'n2_no_change_observed') return { text: 'No change', tone: '' };
  if (status === 'n2_unobserved') return { text: 'Not observed', tone: '' };
  return { text: 'Not recorded', tone: '' };
}

function runOutcome(run) {
  const baseline = run.fault === 'normal';
  const deployment = baseline
    ? { text: 'No fault', tone: '' }
    : run.status === 'recovered'
      ? { text: 'Yes', tone: 'good' }
      : run.status === 'recovery_pending' ? { text: 'Pending', tone: 'partial' } : { text: 'Not verified', tone: 'bad' };
  const quality = run.quality === 'clean' ? { text: 'Included', tone: 'good' } : { text: 'Excluded', tone: '' };
  return { target: baseline ? 'Baseline' : String(run.target || '').toUpperCase(), deployment, packets: packetOutcome(run), n2: n2Outcome(run), quality };
}

function cell(row, text, className, tone) {
  const td = document.createElement('td');
  if (className) td.className = className;
  if (tone !== undefined) {
    const span = document.createElement('span');
    span.className = `outcome ${tone}`.trim();
    span.textContent = text;
    td.append(span);
  } else {
    td.textContent = text;
  }
  row.append(td);
}

async function refreshRuns() {
  try {
    const response = await fetch('/api/runs', { cache: 'no-store' });
    if (!response.ok) throw new Error('Run history could not be read from the runs folder.');
    state.runs = (await response.json()).runs || [];
  } catch (error) { state.runs = []; toast(error.message); }
  const body = $('#runs-body');
  body.replaceChildren();
  $('#runs-count').textContent = state.runs.length ? `(${state.runs.length})` : '';
  $('#runs-empty').hidden = state.runs.length > 0;
  $('#runs .table-wrap').hidden = state.runs.length === 0;
  for (const run of state.runs) {
    const row = document.createElement('tr');
    const outcome = runOutcome(run);
    cell(row, displayTime(run.started_at));
    cell(row, outcome.target, 'target-cell');
    cell(row, outcome.deployment.text, '', outcome.deployment.tone);
    cell(row, outcome.packets.text, '', outcome.packets.tone);
    cell(row, outcome.n2.text, '', outcome.n2.tone);
    cell(row, outcome.quality.text, '', outcome.quality.tone);
    cell(row, String(run.run_id || '').slice(0, 8), 'id-cell');
    body.append(row);
  }
}

async function refreshEvaluation() {
  const results = $('#evaluation-results');
  const empty = $('#evaluation-empty');
  try {
    const response = await fetch('/api/evaluation', { cache: 'no-store' });
    if (!response.ok) throw new Error('The saved evaluation report could not be loaded.');
    const data = await response.json();
    results.hidden = !data.available;
    empty.hidden = Boolean(data.available);
    if (!data.available) {
      $('#evaluation-message').textContent = data.message || 'No offline evaluation report is available.';
      return;
    }
    const report = data.report;
    $('#evaluation-support').textContent = `${report.train_windows} training windows from ${report.split.train_runs.length} runs; ${report.test_windows} held-out windows from ${report.split.test_runs.length} runs. Seed ${report.split.seed}.`;
    $('#evaluation-support').textContent += ` ${report.purged_windows || 0} windows excluded by a ${report.split.embargo_seconds || 0}s capture-time embargo.`;
    const body = $('#evaluation-body');
    const predictions = $('#evaluation-predictions');
    body.replaceChildren(); predictions.replaceChildren();
    const methods = { nearest_centroid: 'Nearest centroid', majority_scenario: 'Majority scenario', normal_zscore: 'Normal-only deviation', probe_rule: 'Packet rule' };
    for (const [name, model] of Object.entries(report.models)) {
      const row = document.createElement('tr');
      const wrong = model.predictions.filter(p => p.predicted && p.predicted !== p.truth).length;
      cell(row, methods[name] || name);
      cell(row, model.task === 'scenario_classification' ? 'Scenario' : 'Normal / fault interval');
      cell(row, Number(model.window_metrics.macro_f1).toFixed(3));
      cell(row, Number(model.run_metrics.macro_f1).toFixed(3));
      cell(row, `${(100 * model.window_metrics.coverage).toFixed(0)}%`);
      cell(row, String(wrong)); cell(row, String(model.window_metrics.abstained));
      body.append(row);
      for (const prediction of model.predictions) {
        const detail = document.createElement('tr');
        cell(detail, methods[name] || name);
        cell(detail, prediction.run_id.slice(0, 8), 'id-cell');
        cell(detail, clockTime(prediction.window_start));
        cell(detail, prediction.truth);
        cell(detail, prediction.predicted || 'Abstained', '', !prediction.predicted ? 'partial' : prediction.predicted === prediction.truth ? 'good' : 'bad');
        predictions.append(detail);
      }
    }
    $('#evaluation-source').textContent = `CSV SHA-256 ${report.windows_sha256}. This snapshot changes only after evaluation is rerun.`;
    const limitations = $('#evaluation-limitations'); limitations.replaceChildren();
    for (const text of report.limitations) { const li = document.createElement('li'); li.textContent = text; limitations.append(li); }
  } catch (error) {
    results.hidden = true; empty.hidden = false;
    $('#evaluation-message').textContent = error.message;
  }
}

function drawSeries(selector, values) {
  const line = $(selector);
  if (!values.length) { line.setAttribute('points', ''); return { min: 0, max: 0 }; }
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min || Math.max(1, max * 0.1);
  const floor = max === min ? min - span / 2 : min;
  const points = values.length === 1 ? [values[0], values[0]] : values;
  line.setAttribute('points', points.map((value, index) => `${(index / (points.length - 1) * 300).toFixed(1)},${(76 - (value - floor) / span * 72).toFixed(1)}`).join(' '));
  return { min, max };
}

async function refreshTelemetry() {
  try {
    const response = await fetch('/api/telemetry', { cache: 'no-store' });
    if (!response.ok) throw new Error(`Telemetry failed with HTTP ${response.status}.`);
    const data = await response.json();
    const live = Boolean(data.available && data.latest);
    $('#telemetry-live').hidden = !live;
    $('#telemetry-empty').hidden = live;
    if (!live) {
      $('#telemetry-message').textContent = data.message
        ? `${summarizeError(data.message)} Samples appear once the lab and its metrics API respond.`
        : 'Samples appear once the lab and its metrics API respond.';
      setRaw('#telemetry-raw', data.message);
      return;
    }
    const samples = data.samples || [];
    const latest = data.latest;
    const mib = (bytes) => Number(bytes) / 1048576;
    $('#cpu-value').textContent = `${latest.cpu_millicores.toFixed(0)} m`;
    $('#memory-value').textContent = `${mib(latest.memory_bytes).toFixed(0)} MiB`;
    const cpu = drawSeries('#cpu-line', samples.map((sample) => Number(sample.cpu_millicores)));
    const memory = drawSeries('#memory-line', samples.map((sample) => mib(sample.memory_bytes)));
    $('#cpu-min').textContent = `low ${cpu.min.toFixed(0)} m`;
    $('#cpu-max').textContent = `high ${cpu.max.toFixed(0)} m`;
    $('#memory-min').textContent = `low ${memory.min.toFixed(0)} MiB`;
    $('#memory-max').textContent = `high ${memory.max.toFixed(0)} MiB`;
    $('#telemetry-time').textContent = `${samples.length} samples from ${latest.pod_count} pods. Latest at ${clockTime(latest.timestamp)}.`;
  } catch (error) {
    $('#telemetry-live').hidden = true;
    $('#telemetry-empty').hidden = false;
    $('#telemetry-message').textContent = error.message;
  }
}

function showProbe(probe) {
  state.probe = probe;
  const status = probe?.status === 'healthy' || probe?.status === 'degraded' ? probe.status : 'unknown';
  $('#path').dataset.probe = status;
  const gateway = probe?.gateway || '10.45.0.1';
  if (status === 'healthy') {
    $('#probe-title').textContent = `${probe.received} of ${probe.sent} pings reached the UPF at ${gateway}`;
    $('#probe-detail').textContent = `Sent from ${probe.ue_pod} over uesimtun0 at ${clockTime(probe.checked_at)}. This shows the UE to UPF path, not Internet access.`;
  } else if (status === 'degraded') {
    $('#probe-title').textContent = `${probe.received ?? 0} of ${probe.sent ?? 0} pings reached the UPF at ${gateway}`;
    $('#probe-detail').textContent = 'Pods can be ready while the user plane is not forwarding. scripts/recover-service.sh resets the sessions.';
  } else {
    $('#probe-title').textContent = 'Packet path not checked';
    $('#probe-detail').textContent = probe?.detail
      ? `${summarizeError(probe.detail)} The check needs a ready UE pod.`
      : 'The check needs a ready UE pod. It pings 10.45.0.1 over uesimtun0.';
  }
  setRaw('#probe-raw', status === 'healthy' ? '' : probe?.detail);
  updateGates();
}

async function refreshService() {
  try {
    const response = await fetch('/api/service-check', { cache: 'no-store' });
    if (!response.ok) throw new Error(`Packet check failed with HTTP ${response.status}.`);
    showProbe(await response.json());
  } catch (error) {
    showProbe({ status: 'unavailable', detail: error.message });
  }
}

const LINK_LABELS = {
  n2: { up: 'N2', down: 'N2 lost', unknown: 'N2' },
  n4: { up: 'N4 (PFCP)', down: 'N4 (PFCP) down', stale_peer: 'N4: old UPF address', unknown: 'N4 (PFCP)' },
};

function showLinks(data) {
  state.protocol = data;
  updateGates();
  for (const name of ['n2', 'n4']) {
    const link = document.querySelector(`[data-area="${name}"]`);
    const state = data?.links?.[name] || 'unknown';
    link.dataset.link = state;
    link.querySelector('span').textContent = LINK_LABELS[name][state] || LINK_LABELS[name].unknown;
  }
  const mismatch = data?.pfcp_peer_mismatch;
  document.querySelector('[data-area="n4"]').title = mismatch
    ? `SMF is retrying ${mismatch.retrying_peer}, but the UPF pod is ${mismatch.upf_pod_ip}. Restarting SMF (scripts/recover-service.sh) cleared this in the lab.`
    : '';
}

async function refreshProtocol() {
  try {
    const response = await fetch('/api/protocol', { cache: 'no-store' });
    if (!response.ok) throw new Error(`Protocol check failed with HTTP ${response.status}.`);
    showLinks(await response.json());
  } catch {
    showLinks(null);
  }
}

async function showPlan() {
  try {
    const response = await fetch(`/api/plan?target=${encodeURIComponent(selectedTarget())}`);
    const plan = await response.json();
    if (!response.ok) throw new Error(plan.error || 'The plan could not be loaded.');
    const list = $('#plan-content');
    list.replaceChildren();
    for (const [term, value] of [['Deployment', plan.deployment], ['Namespace', plan.namespace], ['What happens', plan.effect], ['Recovery', plan.recovery], ['Afterwards', 'Packet checks every 5 seconds, up to 6 times. Two passes in a row count as recovered.']]) {
      const dt = document.createElement('dt'); dt.textContent = term;
      const dd = document.createElement('dd'); dd.textContent = value;
      list.append(dt, dd);
    }
    $('#plan-dialog').showModal();
  } catch (error) { toast(error.message); }
}

async function runExperiment() {
  const target = selectedTarget();
  const button = $('#confirm-run');
  button.disabled = true;
  button.textContent = 'Deleting';
  try {
    const response = await fetch('/api/experiments/pod-crash', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ target, confirm: 'DELETE_ONE_LAB_POD' }),
    });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || 'The run did not start.');
    const service = result.service_recovery?.status === 'service_recovered'
      ? 'Packets reach the UPF again.'
      : 'Packets to the UPF have not recovered. See the run record.';
    const n2 = result.control_plane_recovery?.status === 'n2_lost'
      ? ' The gNB lost N2 to the AMF, so new registrations would fail.'
      : '';
    toast(result.status === 'recovered'
      ? `${target.toUpperCase()} pod replaced. ${service}${n2}`
      : 'Kubernetes did not confirm a replacement pod. Check the lab before another run.');
    await Promise.all([refreshOverview(), refreshRuns(), refreshService(), refreshProtocol()]);
  } catch (error) { toast(error.message); }
  finally {
    $('#confirm-dialog').close();
    button.textContent = 'Delete pod';
    $('#confirm-input').value = '';
  }
}

document.querySelectorAll('[data-close]').forEach((button) => button.addEventListener('click', () => $(`#${button.dataset.close}`).close()));
document.querySelectorAll('input[name="target"]').forEach((input) => input.addEventListener('change', updateGates));
$('#refresh-button').addEventListener('click', () => { refreshOverview(); refreshService(); refreshProtocol(); refreshEvaluation(); });
$('#plan-button').addEventListener('click', showPlan);
$('#run-button').addEventListener('click', () => {
  if ($('#run-button').disabled) return;
  const target = selectedTarget();
  const name = target.toUpperCase();
  $('#confirm-title').textContent = `Delete one ${name} pod?`;
  $('#confirm-copy').textContent = `This deletes one ready ${name} pod in the open5gs namespace. Kubernetes should recreate it. UE traffic may stop until the session recovers.`;
  $('#confirm-label').textContent = `Type ${target} to confirm`;
  $('#confirm-input').value = '';
  $('#confirm-run').disabled = true;
  $('#confirm-dialog').showModal();
  $('#confirm-input').focus();
});
$('#confirm-input').addEventListener('input', () => { $('#confirm-run').disabled = $('#confirm-input').value.trim().toLowerCase() !== selectedTarget(); });
$('#confirm-run').addEventListener('click', runExperiment);
setInterval(refreshOverview, 30000);
setInterval(refreshTelemetry, 10000);
setInterval(refreshService, 30000);
setInterval(refreshProtocol, 30000);
setInterval(refreshEvaluation, 30000);
Promise.all([refreshOverview(), refreshRuns(), refreshTelemetry(), refreshService(), refreshProtocol(), refreshEvaluation()]);
