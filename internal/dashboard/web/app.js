const $ = (selector) => document.querySelector(selector);
const state = { overview: null, runs: [], toastTimer: null };

function toast(message) {
  const el = $('#toast');
  el.textContent = message;
  el.classList.add('show');
  clearTimeout(state.toastTimer);
  state.toastTimer = setTimeout(() => el.classList.remove('show'), 5000);
}

function displayTime(value) {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
}

function updateClock() {
  $('#clock').textContent = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date());
}

function updateTopology(report) {
  const deployments = report?.inventory?.deployments || [];
  for (const node of document.querySelectorAll('[data-workload]')) {
    node.classList.remove('ready', 'missing');
    const name = node.dataset.workload;
    const deployment = deployments.find((item) => item.name === name);
    const label = node.querySelector('.topo-state');
    if (!report) { label.textContent = '—'; continue; }
    if (!deployment) { node.classList.add('missing'); label.textContent = 'NOT FOUND'; continue; }
    const ready = Number(deployment.ready_replicas || 0);
    const desired = Number(deployment.replicas || 0);
    if (ready > 0 && ready === desired) { node.classList.add('ready'); label.textContent = 'READY'; }
    else { node.classList.add('missing'); label.textContent = `${ready}/${desired}`; }
  }
  $('#topology-badge').textContent = report ? 'OBSERVED' : 'REFERENCE';
  $('#topology-intro').textContent = report
    ? 'Deployment names and readiness from the current Kubernetes context.'
    : 'Expected lab components. Their live state has not been verified.';
  $('#topology-updated').textContent = report ? `Updated ${displayTime(report.generated_at)}` : 'Awaiting connection';
}

function updateOverview(data) {
  state.overview = data;
  const banner = $('#status-banner');
  banner.classList.toggle('live', Boolean(data.connected));
  $('#status-title').textContent = data.connected ? 'Lab connection established' : 'Lab offline · preview mode';
  $('#status-detail').textContent = data.connected
    ? `Read-only discovery completed in context ${data.report.context}. Resource metrics appear below when available.`
    : data.message || 'The local lab is unavailable.';
  $('#status-tag').textContent = data.connected ? 'CONNECTED' : 'PREVIEW';
  $('#status-banner .status-symbol').textContent = data.connected ? '✓' : '!';
  const report = data.report;
  $('#context-value').textContent = report?.context || 'Unavailable';
  $('#context-foot').textContent = report ? 'Current kubectl context' : 'No live Kubernetes context';
  const count = report ? Object.values(report.expected_deployments || {}).filter(Boolean).length : null;
  $('#workloads-value').innerHTML = count === null ? '—<small> / 4</small>' : `${count}<small> / 4</small>`;
  $('#workloads-foot').textContent = report ? 'Expected deployments found' : 'Reference topology only';
  $('#candidates-value').textContent = report ? String(report.prometheus_annotation_candidates?.length || 0) : '—';
  updateTopology(report);
  const correctContext = report?.context === 'open5gs-lab';
  const target = $('#target-select').value;
  const targetName = `open5gs-${target}`;
  const targetReady = report?.inventory?.deployments?.some((item) => item.name === targetName && Number(item.ready_replicas) === 1 && Number(item.replicas) === 1);
  const expectedReady = ['open5gs-amf', 'open5gs-upf', 'ueransim-gnb', 'ueransim-ue'].every((name) => report?.inventory?.deployments?.some((item) => item.name === name && Number(item.ready_replicas) === 1 && Number(item.replicas) === 1));
  $('#run-button').disabled = !(correctContext && targetReady && expectedReady);
  $('#experiment-note').textContent = !data.connected
    ? 'The run action unlocks only when the exact lab context and workload are reachable.'
    : !correctContext
      ? 'Connected to another context. Experiments require open5gs-lab.'
      : !targetReady
        ? 'The selected deployment must have exactly one ready replica.'
        : !expectedReady
          ? 'All four core and simulator workloads must be ready before a demo run.'
        : 'Lab checks passed. The server verifies context, ownership, and readiness again before deletion.';
}

async function refreshOverview() {
  const button = $('#refresh-button');
  if (button.disabled) return;
  button.disabled = true;
  button.textContent = 'Checking lab…';
  try {
    const response = await fetch('/api/overview', { cache: 'no-store' });
    if (!response.ok) throw new Error(`Discovery returned ${response.status}`);
    updateOverview(await response.json());
  } catch (error) {
    updateOverview({ connected: false, message: 'The dashboard could not reach its Go API.' });
    toast(error.message);
  } finally {
    button.disabled = false;
    button.innerHTML = '<span aria-hidden="true">↻</span> Refresh discovery';
  }
}

async function refreshRuns() {
  try {
    const response = await fetch('/api/runs', { cache: 'no-store' });
    if (!response.ok) throw new Error('Could not load run history');
    state.runs = (await response.json()).runs || [];
  } catch (error) { state.runs = []; toast(error.message); }
  $('#runs-value').textContent = String(state.runs.length);
  $('#runs-foot').textContent = state.runs.length ? 'Latest 20 run records' : 'No run records yet';
  const list = $('#runs-list');
  if (!state.runs.length) {
    list.innerHTML = '<div class="empty-runs"><span>▤</span><strong>No experiment runs yet</strong><p>Completed and unverified recoveries will appear here.</p></div>';
    return;
  }
  list.replaceChildren();
  for (const run of state.runs.slice(0, 5)) {
    const row = document.createElement('div'); row.className = 'run-item';
    const info = document.createElement('div');
    const name = document.createElement('strong'); name.textContent = `${String(run.target || '').toUpperCase()} · ${run.fault || 'experiment'}`;
    const detail = document.createElement('small'); detail.textContent = `${displayTime(run.started_at)} · ${String(run.run_id || '').slice(0, 8)}`;
    info.append(name, detail);
    const status = document.createElement('span'); status.className = `run-status${run.status === 'recovered' ? '' : ' warning'}`; status.textContent = String(run.status || 'unknown').replaceAll('_', ' ').toUpperCase();
    row.append(info, status); list.append(row);
  }
}

function drawSeries(selector, values) {
  const line = $(selector);
  if (!values.length) { line.setAttribute('points', ''); return; }
  const maximum = Math.max(1, ...values);
  const points = values.length === 1 ? [values[0], values[0]] : values;
  line.setAttribute('points', points.map((value, index) => `${(index / (points.length - 1) * 300).toFixed(1)},${(80 - value / maximum * 70).toFixed(1)}`).join(' '));
}

async function refreshTelemetry() {
  try {
    const response = await fetch('/api/telemetry', { cache: 'no-store' });
    if (!response.ok) throw new Error(`Telemetry returned ${response.status}`);
    const data = await response.json();
    const live = Boolean(data.available && data.latest);
    $('#telemetry-live').hidden = !live;
    $('#telemetry-empty').hidden = live;
    $('#telemetry-badge').textContent = live ? 'LIVE RESOURCE METRICS' : 'WAITING';
    if (!live) {
      $('#telemetry-message').textContent = data.message || 'The collector will show real Kubernetes resource metrics when the lab and metrics API are available.';
      return;
    }
    const samples = data.samples || [];
    const latest = data.latest;
    $('#telemetry-time').textContent = displayTime(latest.timestamp);
    $('#cpu-value').textContent = `${latest.cpu_millicores.toFixed(1)} m`;
    $('#memory-value').textContent = `${(latest.memory_bytes / 1048576).toFixed(1)} MiB`;
    $('#metric-pod-count').textContent = String(latest.pod_count);
    drawSeries('#cpu-line', samples.map((sample) => Number(sample.cpu_millicores)));
    drawSeries('#memory-line', samples.map((sample) => Number(sample.memory_bytes)));
  } catch (error) {
    $('#telemetry-live').hidden = true;
    $('#telemetry-empty').hidden = false;
    $('#telemetry-badge').textContent = 'WAITING';
    $('#telemetry-message').textContent = error.message;
  }
}

async function refreshService() {
  try {
    const response = await fetch('/api/service-check', { cache: 'no-store' });
    if (!response.ok) throw new Error(`Service check returned ${response.status}`);
    const probe = await response.json();
    const card = $('#service-probe');
    card.classList.toggle('healthy', probe.status === 'healthy');
    card.classList.toggle('degraded', probe.status === 'degraded');
    $('#probe-badge').textContent = probe.status === 'healthy' ? 'PATH HEALTHY' : probe.status === 'degraded' ? 'DEGRADED' : 'UNAVAILABLE';
    $('#probe-title').textContent = probe.status === 'healthy'
      ? `${probe.received}/${probe.sent} packets reached the UPF gateway`
      : probe.status === 'degraded' ? 'UE path did not pass the ping check' : 'UE path could not be checked';
    $('#probe-detail').textContent = probe.status === 'healthy'
      ? `Observed from ${probe.ue_pod} · ${displayTime(probe.checked_at)}`
      : probe.detail || 'The isolated lab or its current UE pod is unavailable.';
  } catch (error) {
    $('#service-probe').classList.remove('healthy', 'degraded');
    $('#probe-badge').textContent = 'UNAVAILABLE';
    $('#probe-title').textContent = 'UE path could not be checked';
    $('#probe-detail').textContent = error.message;
  }
}

function addPlanRow(parent, label, value) {
  const row = document.createElement('div'); row.className = 'plan-row';
  const key = document.createElement('span'); key.textContent = label;
  const content = document.createElement('strong'); content.textContent = value;
  row.append(key, content); parent.append(row);
}

async function showPlan() {
  try {
    const target = $('#target-select').value;
    const response = await fetch(`/api/plan?target=${encodeURIComponent(target)}`);
    const plan = await response.json();
    if (!response.ok) throw new Error(plan.error || 'Could not load plan');
    const container = $('#plan-content'); container.replaceChildren();
    addPlanRow(container, 'Target', `${plan.target.toUpperCase()} · ${plan.deployment}`);
    addPlanRow(container, 'Namespace', plan.namespace);
    addPlanRow(container, 'Effect', plan.effect);
    addPlanRow(container, 'Recovery', plan.recovery);
    $('#plan-dialog').showModal();
  } catch (error) { toast(error.message); }
}

async function runExperiment() {
  const target = $('#target-select').value;
  $('#confirm-run').disabled = true;
  $('#confirm-run').textContent = 'Running…';
  try {
    const response = await fetch('/api/experiments/pod-crash', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ target, confirm: 'DELETE_ONE_LAB_POD' }),
    });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || 'Experiment failed');
    const datasetNote = result.dataset ? ` Dataset updated: ${result.dataset.samples} samples, ${result.dataset.fault} in the experiment window.` : '';
    const serviceNote = result.service_probe?.status === 'healthy' ? ' UE path verified.' : result.service_probe ? ' UE path needs checking.' : '';
    toast(result.status === 'recovered' ? `Deployment recovery verified.${serviceNote} Run record saved.${datasetNote}` : 'Recovery could not be verified. Check the lab and run record.');
    await Promise.all([refreshOverview(), refreshRuns(), refreshService()]);
  } catch (error) { toast(error.message); }
  finally { $('#confirm-dialog').close(); $('#confirm-run').textContent = 'Delete one lab pod'; $('#confirm-input').value = ''; }
}

document.querySelectorAll('[data-close]').forEach((button) => button.addEventListener('click', () => $(`#${button.dataset.close}`).close()));
document.querySelectorAll('.nav-link').forEach((link) => link.addEventListener('click', () => {
  document.querySelectorAll('.nav-link').forEach((item) => item.classList.remove('active'));
  link.classList.add('active');
  $('.breadcrumb strong').textContent = link.dataset.view.toUpperCase();
}));
$('#refresh-button').addEventListener('click', refreshOverview);
$('#plan-button').addEventListener('click', showPlan);
$('#target-select').addEventListener('change', () => { if (state.overview) updateOverview(state.overview); });
$('#run-button').addEventListener('click', () => {
  if ($('#run-button').disabled) return;
  const target = $('#target-select').value;
  $('#confirm-copy').textContent = `This deletes one ready ${target.toUpperCase()} pod in the open5gs namespace. Kubernetes should recreate it. Type ${target.toUpperCase()} below to continue.`;
  $('#confirm-input').value = '';
  $('#confirm-run').disabled = true;
  $('#confirm-dialog').showModal();
  $('#confirm-input').focus();
});
$('#confirm-input').addEventListener('input', () => { $('#confirm-run').disabled = $('#confirm-input').value.trim().toLowerCase() !== $('#target-select').value; });
$('#confirm-run').addEventListener('click', runExperiment);
updateClock(); setInterval(updateClock, 30000);
setInterval(refreshOverview, 30000);
setInterval(refreshTelemetry, 10000);
setInterval(refreshService, 30000);
Promise.all([refreshOverview(), refreshRuns(), refreshTelemetry(), refreshService()]);
