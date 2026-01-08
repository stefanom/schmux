export async function getSessions() {
  const response = await fetch('/api/sessions');
  if (!response.ok) throw new Error('Failed to fetch sessions');
  return response.json();
}

export async function getWorkspaces() {
  const response = await fetch('/api/workspaces');
  if (!response.ok) throw new Error('Failed to fetch workspaces');
  return response.json();
}

export async function getConfig() {
  const response = await fetch('/api/config');
  if (!response.ok) throw new Error('Failed to fetch config');
  return response.json();
}

export async function spawnSessions(request) {
  const response = await fetch('/api/spawn', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request)
  });
  if (!response.ok) throw new Error('Failed to spawn sessions');
  return response.json();
}

export async function disposeSession(sessionId) {
  const response = await fetch(`/api/dispose/${sessionId}`, { method: 'POST' });
  if (!response.ok) throw new Error('Failed to dispose session');
  return response.json();
}

export async function updateConfig(request) {
  const response = await fetch('/api/config', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request)
  });
  if (!response.ok) {
    const err = await response.json();
    throw new Error(err.detail || response.statusText || 'Failed to update config');
  }
  return response.json();
}
