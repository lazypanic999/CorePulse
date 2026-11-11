(() => {
  const el = id => document.getElementById(id);
  let current = null, rendered = null, starting = false;
  const task = kind => kind === 'mlp' ? 'Scenario classification' : 'Normal / fault interval';
  function gates() {
    const collectionFree = state.collection?.available === true && state.collection?.locked === false;
    el('neural-train').disabled = starting || !current?.available || current.state === 'running' || !collectionFree || !el('neural-confirm').checked;
    el('neural-confirm').disabled = starting || current?.state === 'running';
  }
  function samples() {
    const model = current?.models?.find(item => item.name === el('neural-model').value);
    el('neural-sample').replaceChildren(); el('neural-prediction').hidden = true;
    for (const sample of model?.samples || []) {
      const option = document.createElement('option'); option.value = sample.index;
      option.textContent = `${sample.run_id} · ${clockTime(sample.window)}`;
      el('neural-sample').append(option);
    }
  }
  function results() {
    el('neural-models').replaceChildren(); el('neural-model').replaceChildren();
    for (const model of current.models) {
      const row = document.createElement('tr');
      cell(row, model.name); cell(row, task(model.kind)); cell(row, model.macro_f1.toFixed(3)); cell(row, `${(model.coverage * 100).toFixed(0)}%`);
      el('neural-models').append(row);
      const option = document.createElement('option'); option.value = model.name; option.textContent = `${model.kind === 'mlp' ? 'MLP' : 'Autoencoder'} · seed ${model.seed}`;
      el('neural-model').append(option);
    }
    samples(); el('neural-references').replaceChildren(); el('neural-predictions').replaceChildren();
    for (const [name, method] of Object.entries(current.report.models)) {
      const row = document.createElement('tr'); cell(row, name); cell(row, method.task); cell(row, method.window_metrics.macro_f1.toFixed(3)); cell(row, `${(method.window_metrics.coverage * 100).toFixed(0)}%`); el('neural-references').append(row);
      for (const p of method.predictions) {
        const detail = document.createElement('tr'); cell(detail, name); cell(detail, p.run_id); cell(detail, p.truth); cell(detail, p.predicted || 'Abstained', '', !p.predicted ? 'partial' : p.truth === p.predicted ? 'good' : 'bad'); el('neural-predictions').append(detail);
      }
    }
  }
  async function refresh() {
    try {
      const response = await fetch('/api/neural', {cache: 'no-store'}); if (!response.ok) throw Error('Neural worker unavailable');
      current = await response.json();
      el('neural-phase').textContent = current.error || current.phase;
      el('neural-progress').hidden = current.state !== 'running';
      el('neural-results').hidden = current.state !== 'complete';
      if (current.state === 'complete' && current.id !== rendered) { results(); rendered = current.id; }
      gates();
    } catch (error) { current = null; el('neural-phase').textContent = error.message; el('neural-results').hidden = true; el('neural-progress').hidden = true; gates(); }
  }
  el('neural-confirm').addEventListener('change', gates);
  el('neural-model').addEventListener('change', samples);
  el('neural-train').addEventListener('click', async () => {
    if (el('neural-train').disabled) return;
    starting = true; gates(); el('neural-results').hidden = true;
    try {
      const response = await fetch('/api/neural/start', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({confirm: 'TRAIN_SYNTHETIC'})});
      const data = await response.json(); if (!response.ok) throw Error(data.error);
      rendered = null; await refresh();
    } catch (error) { el('neural-phase').textContent = error.message; }
    finally { starting = false; gates(); }
  });
  el('neural-predict').addEventListener('click', async () => {
    if (current?.state !== 'complete') return;
    const button = el('neural-predict'); button.disabled = true;
    try {
      const response = await fetch('/api/neural/predict', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({job_id: current.id, model: el('neural-model').value, sample: Number(el('neural-sample').value)})});
      const data = await response.json(); if (!response.ok) throw Error(data.error);
      const box = el('neural-prediction'); box.replaceChildren();
      const title = document.createElement('strong'); title.textContent = `Go prediction: ${data.result.predicted}`;
      const truth = document.createElement('p'); truth.textContent = `Known interval: ${data.sample.truth}. ${data.sample.truth === data.result.predicted ? 'Matches' : 'Does not match'} the held-out label.`;
      const score = document.createElement('p'); score.textContent = `${el('neural-model').value.startsWith('mlp') ? 'Maximum softmax (not calibrated probability)' : 'Reconstruction mean squared error'}: ${data.result.score.toFixed(6)}`;
      const note = document.createElement('p'); note.textContent = 'Exported neural model evaluated in Go. Synthetic held-out input; not a live-network diagnosis.';
      box.append(title, truth, score, note); box.hidden = false;
    } catch (error) { el('neural-prediction').textContent = error.message; el('neural-prediction').hidden = false; }
    finally { button.disabled = false; }
  });
  refresh(); setInterval(refresh, 1500);
})();
