package api

import (
	"net/http"
	"strconv"
)

func (h *Handler) operatorDashboardUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspaceID, err := parseInt64Query(r, "workspace_id", 1)
	if err != nil {
		http.Error(w, "invalid workspace_id", http.StatusBadRequest)
		return
	}

	content := `<section class="md-grid md-grid-2">
  <article class="md-panel">
    <h2>Logs and Timeline</h2>
    <p class="md-muted">Investigate message outcomes, queue states, retries, and webhook dead letters.</p>
    <a class="md-button" href="/ui/logs?workspace_id=` + strconv.FormatInt(workspaceID, 10) + `">Open Logs</a>
  </article>
  <article class="md-panel">
    <h2>Onboarding</h2>
    <p class="md-muted">Run technical readiness checklist with evidence capture.</p>
    <a class="md-button" href="/ui/onboarding?workspace_id=` + strconv.FormatInt(workspaceID, 10) + `">Open Onboarding</a>
  </article>
  <article class="md-panel">
    <h2>Incident Response</h2>
    <p class="md-muted">Export incident bundles and inspect attempts and webhook context.</p>
    <a class="md-button" href="/ui/incidents?workspace_id=` + strconv.FormatInt(workspaceID, 10) + `">Open Incidents</a>
  </article>
  <article class="md-panel">
    <h2>Policy Controls</h2>
    <p class="md-muted">Update workspace rate limits and blocked recipient domains.</p>
    <a class="md-button" href="/ui/policy?workspace_id=` + strconv.FormatInt(workspaceID, 10) + `">Open Policy</a>
  </article>
</section>

<section class="md-panel">
  <h2>Frontend IA and API Ownership</h2>
  <table class="md-table">
    <thead>
      <tr>
        <th>Page</th>
        <th>Purpose</th>
        <th>Success Metric</th>
        <th>Primary APIs</th>
      </tr>
    </thead>
    <tbody>
      <tr>
        <td>Dashboard</td>
        <td>Entry point for operator journeys.</td>
        <td>Operators navigate to target workflow in one click.</td>
        <td>None required.</td>
      </tr>
      <tr>
        <td>Logs/Timeline</td>
        <td>Triage deliveries and perform retries/replays.</td>
        <td>Failed message is identified and retried quickly.</td>
        <td><code>GET /v1/messages/logs</code>, <code>GET /v1/messages/timeline</code>, <code>POST /v1/messages/retry</code>, <code>GET /v1/webhooks/logs</code>, <code>POST /v1/webhooks/replay</code></td>
      </tr>
      <tr>
        <td>Onboarding</td>
        <td>Run checklist and attach evidence.</td>
        <td>Checklist reaches completion with domain checks verified.</td>
        <td><code>GET /v1/ops/onboarding-checklist</code>, <code>POST /v1/domains/readiness</code></td>
      </tr>
      <tr>
        <td>Incidents</td>
        <td>Bundle export and context drill-down from message ID.</td>
        <td>Bundle downloaded with message+attempt+webhook context.</td>
        <td><code>GET /v1/incidents/bundle</code>, <code>GET /v1/messages/timeline</code>, <code>GET /v1/webhooks/logs</code></td>
      </tr>
    </tbody>
  </table>
</section>`

	html := uiShell("Operator Dashboard", workspaceID, "dashboard", content, ``)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}

func (h *Handler) onboardingUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspaceID, err := parseInt64Query(r, "workspace_id", 1)
	if err != nil {
		http.Error(w, "invalid workspace_id", http.StatusBadRequest)
		return
	}

	content := `<section class="md-panel">
  <h2>Checklist Runner</h2>
  <p class="md-muted">Use API mode or demo mode. Demo mode works without any backend/API calls.</p>
  <div class="md-form-row">
    <label class="md-label" for="apiKey">API Key</label>
    <input class="md-input" id="apiKey" type="text" placeholder="change-me-operator" />
  </div>
  <div class="md-grid md-grid-2">
    <div>
      <label class="md-label" for="domain">Domain (optional)</label>
      <input class="md-input" id="domain" type="text" placeholder="maild.click" />
    </div>
    <div>
      <label class="md-label" for="selector">DKIM selector (optional)</label>
      <input class="md-input" id="selector" type="text" placeholder="default" />
    </div>
  </div>
  <div class="md-button-row">
    <button id="loadChecklist" class="md-button">Load From API</button>
    <button id="loadDemo" class="md-button md-button-secondary">Use Demo Checklist</button>
  </div>
  <p id="status" class="md-muted">No checklist loaded.</p>
</section>

<section class="md-panel">
  <h2>Checklist Items</h2>
  <div id="emptyState" class="md-empty">Choose "Load From API" or "Use Demo Checklist" to start.</div>
  <div id="errorState" class="md-error" hidden></div>
  <table id="table" class="md-table" hidden>
    <thead>
      <tr>
        <th>Item</th>
        <th>Status</th>
        <th>Evidence</th>
        <th>Action</th>
      </tr>
    </thead>
    <tbody id="rows"></tbody>
  </table>
</section>`

	script := `<script>
const workspaceId = ` + strconv.FormatInt(workspaceID, 10) + `;
const rowsEl = document.getElementById('rows');
const tableEl = document.getElementById('table');
const emptyStateEl = document.getElementById('emptyState');
const errorStateEl = document.getElementById('errorState');
const statusEl = document.getElementById('status');

function headers() {
  const key = document.getElementById('apiKey').value.trim();
  return key ? { 'X-API-Key': key } : {};
}

function setState(kind, message) {
  statusEl.textContent = message;
  if (kind === 'empty') {
    emptyStateEl.hidden = false;
    tableEl.hidden = true;
    errorStateEl.hidden = true;
    return;
  }
  if (kind === 'loading') {
    emptyStateEl.hidden = false;
    emptyStateEl.textContent = 'Loading checklist...';
    tableEl.hidden = true;
    errorStateEl.hidden = true;
    return;
  }
  if (kind === 'error') {
    emptyStateEl.hidden = true;
    tableEl.hidden = true;
    errorStateEl.hidden = false;
    errorStateEl.textContent = message;
    return;
  }
  emptyStateEl.hidden = true;
  tableEl.hidden = false;
  errorStateEl.hidden = true;
}

function renderChecklist(payload) {
  const items = Array.isArray(payload.items) ? payload.items : [];
  rowsEl.innerHTML = '';
  for (const item of items) {
    const tr = document.createElement('tr');
    const badgeClass = item.done ? 'md-badge md-badge-ok' : 'md-badge md-badge-warn';
    tr.innerHTML =
      '<td><strong>' + (item.title || item.id || 'Untitled') + '</strong><div class="md-muted">' + (item.description || '') + '</div></td>' +
      '<td><span class="' + badgeClass + '">' + (item.done ? 'done' : 'pending') + '</span></td>' +
      '<td><code>' + (item.evidence || 'n/a') + '</code></td>' +
      '<td><code>' + (item.action || 'n/a') + '</code></td>';
    rowsEl.appendChild(tr);
  }
  setState('ready', 'Checklist loaded. Completed: ' + (payload.completed || 0) + '/' + (payload.total || 0));
}

function demoChecklist() {
  return {
    workspace_id: workspaceId,
    total: 5,
    completed: 3,
    items: [
      { id: 'smtp_connected', title: 'SMTP account connected', description: 'Active account exists for workspace.', done: true, evidence: 'active_provider=mailgun', action: 'POST /v1/smtp-accounts' },
      { id: 'domain_readiness_checked', title: 'Check domain readiness', description: 'SPF, DKIM, DMARC validated.', done: true, evidence: 'spf=true, dkim=true, dmarc=true', action: 'POST /v1/domains/readiness' },
      { id: 'retry_path_tested', title: 'Retry path tested', description: 'Failed message can be retried.', done: true, evidence: 'retry_result=retried:1', action: 'POST /v1/messages/retry' },
      { id: 'webhook_path_verified', title: 'Webhook path verified', description: 'Dead-letter replay verified.', done: false, evidence: 'no dead-letter replay run yet', action: 'POST /v1/webhooks/replay' },
      { id: 'policy_limits_checked', title: 'Workspace policy validated', description: 'Rate and domain limits reviewed.', done: false, evidence: 'blocked_domains not configured', action: 'GET/POST /v1/workspaces/policy' }
    ]
  };
}

async function loadFromAPI() {
  const domain = document.getElementById('domain').value.trim();
  const selector = document.getElementById('selector').value.trim();
  let url = '/v1/ops/onboarding-checklist?workspace_id=' + workspaceId;
  if (domain) url += '&domain=' + encodeURIComponent(domain);
  if (selector) url += '&dkim_selector=' + encodeURIComponent(selector);

  setState('loading', 'Loading checklist from API...');
  const res = await fetch(url, { headers: headers() });
  if (!res.ok) {
    setState('error', 'Onboarding checklist HTTP ' + res.status + ': ' + await res.text());
    return;
  }
  renderChecklist(await res.json());
}

document.getElementById('loadChecklist').addEventListener('click', loadFromAPI);
document.getElementById('loadDemo').addEventListener('click', () => {
  renderChecklist(demoChecklist());
});
</script>`

	html := uiShell("Onboarding", workspaceID, "onboarding", content, script)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}

func (h *Handler) incidentUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspaceID, err := parseInt64Query(r, "workspace_id", 1)
	if err != nil {
		http.Error(w, "invalid workspace_id", http.StatusBadRequest)
		return
	}

	content := `<section class="md-panel">
  <h2>Incident Bundle Export</h2>
  <p class="md-muted">Export by message ID, or use demo data to validate UI flow without API calls.</p>
  <div class="md-form-row">
    <label class="md-label" for="apiKey">API Key</label>
    <input class="md-input" id="apiKey" type="text" placeholder="change-me-operator" />
  </div>
  <div class="md-grid md-grid-2">
    <div>
      <label class="md-label" for="messageId">Message ID</label>
      <input class="md-input" id="messageId" type="number" min="1" placeholder="101" />
    </div>
    <div class="md-inline-actions">
      <button id="exportBundle" class="md-button">Export From API</button>
      <button id="loadDemo" class="md-button md-button-secondary">Use Demo Incident</button>
    </div>
  </div>
  <p id="status" class="md-muted">No incident loaded.</p>
</section>

<section class="md-grid md-grid-2">
  <article class="md-panel">
    <h2>Timeline</h2>
    <div id="timelineEmpty" class="md-empty">No incident context yet.</div>
    <ol id="timeline" class="md-timeline" hidden></ol>
  </article>
  <article class="md-panel">
    <h2>Bundle JSON</h2>
    <div id="errorState" class="md-error" hidden></div>
    <pre id="json" class="md-pre">{}</pre>
    <a id="download" class="md-button" hidden download>Download JSON</a>
  </article>
</section>`

	script := `<script>
const workspaceId = ` + strconv.FormatInt(workspaceID, 10) + `;
const timelineEl = document.getElementById('timeline');
const timelineEmptyEl = document.getElementById('timelineEmpty');
const statusEl = document.getElementById('status');
const jsonEl = document.getElementById('json');
const errorStateEl = document.getElementById('errorState');
const downloadEl = document.getElementById('download');

function headers() {
  const key = document.getElementById('apiKey').value.trim();
  return key ? { 'X-API-Key': key } : {};
}

function setLoading(message) {
  statusEl.textContent = message;
  errorStateEl.hidden = true;
  timelineEl.hidden = true;
  timelineEmptyEl.hidden = false;
  timelineEmptyEl.textContent = 'Loading incident bundle...';
}

function setError(message) {
  statusEl.textContent = message;
  errorStateEl.hidden = false;
  errorStateEl.textContent = message;
}

function renderTimeline(bundle) {
  timelineEl.innerHTML = '';
  const message = bundle.message || {};
  const attempts = Array.isArray(bundle.attempts) ? bundle.attempts : [];
  const webhooks = Array.isArray(bundle.webhook_events) ? bundle.webhook_events : [];

  const top = document.createElement('li');
  top.innerHTML = '<strong>Message</strong><span>' + (message.id || 'n/a') + ' • ' + (message.status || 'unknown') + '</span>';
  timelineEl.appendChild(top);

  for (const a of attempts) {
    const li = document.createElement('li');
    li.innerHTML = '<strong>Attempt #' + (a.attempt_number || '?') + '</strong><span>' + (a.status || 'unknown') + ' • ' + (a.provider || 'n/a') + '</span>';
    timelineEl.appendChild(li);
  }

  for (const e of webhooks) {
    const li = document.createElement('li');
    li.innerHTML = '<strong>Webhook</strong><span>' + (e.type || 'unknown') + ' • ' + (e.status || 'unknown') + '</span>';
    timelineEl.appendChild(li);
  }

  timelineEmptyEl.hidden = true;
  timelineEl.hidden = false;
}

function attachDownload(bundle, messageId) {
  const body = JSON.stringify(bundle, null, 2);
  jsonEl.textContent = body;
  const blob = new Blob([body], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  downloadEl.href = url;
  downloadEl.download = 'maild_incident_bundle_message_' + messageId + '.json';
  downloadEl.hidden = false;
}

function demoBundle() {
  return {
    message: {
      id: 101,
      workspace_id: workspaceId,
      to_email: 'ops@example.com',
      subject: 'Welcome',
      status: 'failed'
    },
    attempts: [
      { attempt_number: 1, status: 'failed', provider: 'smtp-primary' },
      { attempt_number: 2, status: 'failed', provider: 'smtp-secondary' }
    ],
    webhook_events: [
      { type: 'bounce', status: 'processed' },
      { type: 'complaint', status: 'dead_letter' }
    ]
  };
}

async function exportFromAPI() {
  const messageId = Number(document.getElementById('messageId').value || 0);
  if (!Number.isInteger(messageId) || messageId <= 0) {
    setError('Message ID is required.');
    return;
  }

  setLoading('Loading incident bundle from API...');
  const res = await fetch('/v1/incidents/bundle?workspace_id=' + workspaceId + '&message_id=' + messageId, {
    headers: headers()
  });
  if (!res.ok) {
    setError('Incident bundle HTTP ' + res.status + ': ' + await res.text());
    return;
  }

  const bundle = await res.json();
  statusEl.textContent = 'Incident bundle loaded from API.';
  renderTimeline(bundle);
  attachDownload(bundle, messageId);
}

document.getElementById('exportBundle').addEventListener('click', exportFromAPI);
document.getElementById('loadDemo').addEventListener('click', () => {
  const bundle = demoBundle();
  statusEl.textContent = 'Demo incident loaded (no API call).';
  errorStateEl.hidden = true;
  renderTimeline(bundle);
  attachDownload(bundle, bundle.message.id || 'demo');
});
</script>`

	html := uiShell("Incidents", workspaceID, "incidents", content, script)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}

func uiShell(title string, workspaceID int64, active string, content string, script string) string {
	activeDashboard := ""
	activeLogs := ""
	activeOnboarding := ""
	activeIncidents := ""
	activePolicy := ""

	switch active {
	case "dashboard":
		activeDashboard = "md-nav-active"
	case "logs":
		activeLogs = "md-nav-active"
	case "onboarding":
		activeOnboarding = "md-nav-active"
	case "incidents":
		activeIncidents = "md-nav-active"
	case "policy":
		activePolicy = "md-nav-active"
	}

	workspace := strconv.FormatInt(workspaceID, 10)
	return `<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>maild ` + title + `</title>
  <style>
    :root{
      --md-bg:#f6f8f6;
      --md-surface:#ffffff;
      --md-surface-2:#f1f4ef;
      --md-text:#121a14;
      --md-muted:#5e6f62;
      --md-border:#d4ddd4;
      --md-accent:#0a6a4f;
      --md-accent-ink:#ffffff;
      --md-warn:#8e6400;
      --md-ok:#0f7b45;
      --md-error:#9d2a1b;
      --md-shadow:0 8px 18px rgba(7, 28, 17, 0.08);
      --md-r-1:8px;
      --md-r-2:12px;
      --md-space-1:6px;
      --md-space-2:10px;
      --md-space-3:16px;
      --md-space-4:24px;
      --md-space-5:32px;
      --md-text-sm:0.88rem;
      --md-text-md:1rem;
      --md-text-lg:1.25rem;
      --md-text-xl:1.75rem;
    }
    *{box-sizing:border-box}
    body{
      margin:0;
      font-family:"IBM Plex Sans", "Avenir Next", "Segoe UI", sans-serif;
      color:var(--md-text);
      background:radial-gradient(1000px 600px at 80% -10%, #d9ece2 0%, transparent 60%), var(--md-bg);
    }
    .md-shell{max-width:1200px;margin:0 auto;padding:var(--md-space-4)}
    .md-topbar{
      display:flex;
      align-items:center;
      justify-content:space-between;
      gap:var(--md-space-3);
      margin-bottom:var(--md-space-4);
      flex-wrap:wrap;
    }
    .md-brand{font-size:var(--md-text-xl);font-weight:700;letter-spacing:.01em}
    .md-sub{color:var(--md-muted);font-size:var(--md-text-sm)}
    .md-nav{display:flex;gap:var(--md-space-2);flex-wrap:wrap}
    .md-nav a{
      text-decoration:none;
      color:var(--md-text);
      border:1px solid var(--md-border);
      border-radius:999px;
      padding:8px 14px;
      background:var(--md-surface);
      font-size:var(--md-text-sm);
    }
    .md-nav a.md-nav-active{background:var(--md-accent);color:var(--md-accent-ink);border-color:var(--md-accent)}
    .md-panel{
      background:var(--md-surface);
      border:1px solid var(--md-border);
      border-radius:var(--md-r-2);
      padding:var(--md-space-3);
      box-shadow:var(--md-shadow);
      margin-bottom:var(--md-space-3);
    }
    .md-grid{display:grid;gap:var(--md-space-3)}
    .md-grid-2{grid-template-columns:repeat(2,minmax(0,1fr))}
    .md-muted{color:var(--md-muted);font-size:var(--md-text-sm)}
    .md-label{display:block;font-size:var(--md-text-sm);margin-bottom:var(--md-space-1)}
    .md-form-row{margin-bottom:var(--md-space-3)}
    .md-input{
      width:100%;
      padding:10px 12px;
      border:1px solid var(--md-border);
      border-radius:var(--md-r-1);
      background:var(--md-surface);
      color:var(--md-text);
    }
    .md-input:focus{
      outline:2px solid color-mix(in srgb, var(--md-accent) 34%, transparent);
      outline-offset:1px;
      border-color:var(--md-accent);
    }
    .md-button{
      display:inline-block;
      text-decoration:none;
      border:1px solid var(--md-accent);
      border-radius:var(--md-r-1);
      padding:10px 14px;
      background:var(--md-accent);
      color:var(--md-accent-ink);
      cursor:pointer;
      font-weight:600;
    }
    .md-button:hover{filter:brightness(0.96)}
    .md-button:focus-visible{outline:3px solid color-mix(in srgb, var(--md-accent) 35%, transparent)}
    .md-button-secondary{background:var(--md-surface);color:var(--md-text);border-color:var(--md-border)}
    .md-button-row{display:flex;gap:var(--md-space-2);flex-wrap:wrap}
    .md-inline-actions{display:flex;gap:var(--md-space-2);align-items:end;flex-wrap:wrap}
    .md-table{width:100%;border-collapse:collapse;font-size:var(--md-text-sm)}
    .md-table th,.md-table td{border-bottom:1px solid var(--md-border);padding:10px;text-align:left;vertical-align:top}
    .md-table th{background:var(--md-surface-2)}
    .md-badge{display:inline-block;padding:4px 10px;border-radius:999px;font-size:12px;font-weight:700;border:1px solid transparent}
    .md-badge-ok{background:#e7f8ee;color:var(--md-ok);border-color:#c2ebd2}
    .md-badge-warn{background:#fff6e3;color:var(--md-warn);border-color:#f0ddb1}
    .md-empty{padding:var(--md-space-3);border:1px dashed var(--md-border);border-radius:var(--md-r-1);color:var(--md-muted)}
    .md-error{padding:var(--md-space-2);border:1px solid #efc3bf;background:#fff2f0;color:var(--md-error);border-radius:var(--md-r-1)}
    .md-pre{overflow:auto;background:#101713;color:#eaf5ee;padding:var(--md-space-3);border-radius:var(--md-r-1);font-size:12px;min-height:140px}
    .md-timeline{list-style:none;padding:0;margin:0}
    .md-timeline li{padding:10px 0;border-bottom:1px solid var(--md-border);display:flex;justify-content:space-between;gap:var(--md-space-2)}
    .md-timeline li strong{display:block;font-size:var(--md-text-sm)}
    .md-timeline li span{font-size:var(--md-text-sm);color:var(--md-muted)}
    code{background:var(--md-surface-2);padding:2px 6px;border-radius:6px}
    @media (max-width: 900px){
      .md-shell{padding:var(--md-space-3)}
      .md-grid-2{grid-template-columns:1fr}
    }
  </style>
</head>
<body>
  <main class="md-shell">
    <header class="md-topbar">
      <div>
        <div class="md-brand">maild ` + title + `</div>
        <div class="md-sub">Workspace <code>` + workspace + `</code></div>
      </div>
      <nav class="md-nav" aria-label="Primary">
        <a class="` + activeDashboard + `" href="/ui?workspace_id=` + workspace + `">Dashboard</a>
        <a class="` + activeLogs + `" href="/ui/logs?workspace_id=` + workspace + `">Logs</a>
        <a class="` + activeOnboarding + `" href="/ui/onboarding?workspace_id=` + workspace + `">Onboarding</a>
        <a class="` + activeIncidents + `" href="/ui/incidents?workspace_id=` + workspace + `">Incidents</a>
        <a class="` + activePolicy + `" href="/ui/policy?workspace_id=` + workspace + `">Policy</a>
      </nav>
    </header>
    ` + content + `
  </main>
  ` + script + `
</body>
</html>`
}
