export function apiURL(path: string) {
  const explicitBase = import.meta.env.VITE_API_BASE as string | undefined;
  return `${explicitBase || devBackendBase()}${path}`;
}

export function apiFetch(path: string, init: RequestInit = {}) {
  return fetch(apiURL(path), { ...init, credentials: "include" });
}

export async function fetchJSON<T>(path: string, init: RequestInit = {}) {
  const response = await apiFetch(path, init);
  if (!response.ok) {
    throw new Error(await readError(response));
  }
  return (await response.json()) as T;
}

export async function readError(response: Response) {
  try {
    const payload = (await response.json()) as { error?: string };
    return payload.error || response.statusText;
  } catch {
    return response.statusText;
  }
}

function devBackendBase() {
  const isLocalHost = window.location.hostname === "127.0.0.1" || window.location.hostname === "localhost";
  if (isLocalHost && window.location.port.startsWith("517")) {
    return "http://127.0.0.1:8080";
  }
  return "";
}
