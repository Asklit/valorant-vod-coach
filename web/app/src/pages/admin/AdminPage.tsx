import {
  Activity,
  AlertTriangle,
  BarChart3,
  CheckCircle2,
  Database,
  ExternalLink,
  FileText,
  Gauge,
  RefreshCw,
  Search,
  Shield,
  Users,
  XCircle
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { apiURL, fetchJSON } from "../../shared/api";

type AdminTab = "overview" | "metrics" | "logs" | "users";
type TelemetryWindow = "1h" | "6h" | "24h" | "7d";

type AuthUser = {
  id: string;
  email: string;
  display_name: string;
  role: "admin" | "user";
  created_at: string;
  last_login_at?: string;
};

type ReadinessCheck = { status: string; detail?: string; path?: string; runtime?: string; model?: string };

type AdminOverview = {
  generated_at: string;
  system: {
    schema_version: number;
    analyzer: string;
    model_review_enabled: boolean;
    manifest_path: string;
    raw_root: string;
    upload_root?: string;
    processed_root: string;
  };
  readiness: Record<string, ReadinessCheck>;
  dataset: { total: number; enabled: number; downloaded: number; reported: number };
  jobs: Record<string, number>;
  auth: { user_count: number };
  links?: Record<string, string>;
};

type AdminMetric = { method: string; route: string; status: number; count: number; duration_seconds: number };
type RequestLog = {
  time: string;
  method: string;
  path: string;
  route: string;
  status: number;
  duration_ms: number;
  user_email?: string;
};
type AdminMetrics = { started_at: string; requests: AdminMetric[]; jobs: Record<string, number>; logs: RequestLog[] };
type MetricPoint = { timestamp: string; value: number };
type MetricSeries = { id: string; label: string; unit: string; points: MetricPoint[] };
type CentralLog = {
  timestamp: string;
  service: string;
  level?: string;
  message: string;
  trace_id?: string;
  labels?: Record<string, string>;
};
type TelemetrySnapshot = {
  generated_at: string;
  window: string;
  series: MetricSeries[];
  logs: CentralLog[];
  errors?: Record<string, string>;
};

const tabs: Array<{ id: AdminTab; label: string }> = [
  { id: "overview", label: "Overview" },
  { id: "metrics", label: "Metrics" },
  { id: "logs", label: "Logs" },
  { id: "users", label: "Users" }
];
const windows: TelemetryWindow[] = ["1h", "6h", "24h", "7d"];

export function AdminPage() {
  const [tab, setTab] = useState<AdminTab>("overview");
  const [windowID, setWindowID] = useState<TelemetryWindow>("6h");
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [overview, setOverview] = useState<AdminOverview | null>(null);
  const [metrics, setMetrics] = useState<AdminMetrics | null>(null);
  const [users, setUsers] = useState<AuthUser[]>([]);
  const [telemetry, setTelemetry] = useState<TelemetrySnapshot | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [logQuery, setLogQuery] = useState("");
  const [logService, setLogService] = useState("all");
  const [logLevel, setLogLevel] = useState("all");

  const load = useCallback(async (quiet = false) => {
    if (!quiet) setLoading(true);
    setError("");
    try {
      const [nextOverview, nextMetrics, nextLogs, nextUsers] = await Promise.all([
        fetchJSON<AdminOverview>("/api/admin/overview"),
        fetchJSON<AdminMetrics>("/api/admin/metrics"),
        fetchJSON<{ logs: RequestLog[] }>("/api/admin/logs"),
        fetchJSON<{ users: AuthUser[] }>("/api/admin/users")
      ]);
      setOverview(nextOverview);
      setMetrics({ ...nextMetrics, logs: nextLogs.logs });
      setUsers(nextUsers.users);
      try {
        setTelemetry(await fetchJSON<TelemetrySnapshot>(`/api/admin/telemetry?window=${windowID}`));
      } catch (telemetryError) {
        setTelemetry(null);
        setError(messageFromError(telemetryError));
      }
    } catch (nextError) {
      setError(messageFromError(nextError));
    } finally {
      setLoading(false);
    }
  }, [windowID]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!autoRefresh) return;
    const interval = window.setInterval(() => void load(true), 15_000);
    return () => window.clearInterval(interval);
  }, [autoRefresh, load]);

  const centralLogs = telemetry?.logs ?? [];
  const services = useMemo(() => uniqueValues(centralLogs.map((entry) => entry.service)), [centralLogs]);
  const levels = useMemo(() => uniqueValues(centralLogs.map((entry) => entry.level ?? "unknown")), [centralLogs]);
  const filteredLogs = useMemo(() => {
    const query = logQuery.trim().toLowerCase();
    return centralLogs.filter((entry) => {
      const serviceMatch = logService === "all" || entry.service === logService;
      const levelMatch = logLevel === "all" || (entry.level ?? "unknown") === logLevel;
      const queryMatch = !query || `${entry.message} ${entry.trace_id ?? ""}`.toLowerCase().includes(query);
      return serviceMatch && levelMatch && queryMatch;
    });
  }, [centralLogs, logLevel, logQuery, logService]);

  return (
    <div className="admin-console">
      <header className="page-header admin-header">
        <div>
          <p className="eyebrow">Administration</p>
          <h1>Operations</h1>
        </div>
        <div className="admin-header-actions">
          <label className="auto-refresh-toggle">
            <input checked={autoRefresh} onChange={(event) => setAutoRefresh(event.target.checked)} type="checkbox" />
            <span>Live</span>
          </label>
          <button className="icon-button" disabled={loading} onClick={() => void load()} title="Refresh operations" type="button">
            <RefreshCw className={loading ? "spin" : ""} size={17} />
          </button>
        </div>
      </header>

      <div className="admin-toolbar">
        <div className="segmented-control" role="tablist">
          {tabs.map((item) => (
            <button aria-selected={tab === item.id} className={tab === item.id ? "active" : ""} key={item.id} onClick={() => setTab(item.id)} role="tab" type="button">
              {item.label}
            </button>
          ))}
        </div>
        {(tab === "metrics" || tab === "logs") && (
          <div className="segmented-control compact" aria-label="Telemetry window">
            {windows.map((item) => <button className={windowID === item ? "active" : ""} key={item} onClick={() => setWindowID(item)} type="button">{item}</button>)}
          </div>
        )}
      </div>

      {error && <div className="admin-notice"><AlertTriangle size={16} /><span>{error}</span></div>}

      {tab === "overview" && <Overview overview={overview} metrics={metrics} telemetry={telemetry} />}
      {tab === "metrics" && <Metrics metrics={metrics} telemetry={telemetry} />}
      {tab === "logs" && (
        <Logs
          entries={filteredLogs}
          fallback={metrics?.logs ?? []}
          levels={levels}
          query={logQuery}
          service={logService}
          services={services}
          setLevel={setLogLevel}
          setQuery={setLogQuery}
          setService={setLogService}
          level={logLevel}
          grafanaURL={overview?.links?.grafana}
        />
      )}
      {tab === "users" && <UserDirectory users={users} />}
    </div>
  );
}

function Overview(props: { overview: AdminOverview | null; metrics: AdminMetrics | null; telemetry: TelemetrySnapshot | null }) {
  const overview = props.overview;
  const requestRate = latestValue(props.telemetry, "api_request_rate");
  const errorRatio = latestValue(props.telemetry, "api_error_ratio");
  const activeJobs = (overview?.jobs.queued ?? 0) + (overview?.jobs.running ?? 0);
  const readiness = Object.entries(overview?.readiness ?? {});
  return (
    <>
      <div className="ops-kpi-grid">
        <OpsKPI icon={<Activity size={18} />} label="Request rate" value={requestRate == null ? "--" : `${requestRate.toFixed(2)}/s`} state={errorRatio != null && errorRatio > 0.05 ? "warning" : "ok"} />
        <OpsKPI icon={<AlertTriangle size={18} />} label="Error ratio" value={errorRatio == null ? "--" : `${(errorRatio * 100).toFixed(1)}%`} state={errorRatio != null && errorRatio > 0.05 ? "bad" : "ok"} />
        <OpsKPI icon={<Gauge size={18} />} label="Active jobs" value={String(activeJobs)} state={activeJobs > 0 ? "active" : "ok"} />
        <OpsKPI icon={<Users size={18} />} label="Accounts" value={String(overview?.auth.user_count ?? 0)} state="neutral" />
      </div>
      <div className="ops-layout">
        <section className="surface ops-readiness">
          <SectionTitle icon={<CheckCircle2 size={18} />} label="Service readiness" />
          <div className="readiness-table">
            {readiness.map(([name, check]) => (
              <div className="readiness-row" key={name}>
                {check.status === "ok" ? <CheckCircle2 className="ok" size={16} /> : check.status === "failed" ? <XCircle className="bad" size={16} /> : <AlertTriangle className="warn" size={16} />}
                <strong>{humanize(name)}</strong>
                <span>{check.detail ?? check.runtime ?? check.model ?? check.path ?? check.status}</span>
              </div>
            ))}
            {readiness.length === 0 && <InlineEmpty label="No readiness data" />}
          </div>
        </section>
        <section className="surface ops-resources">
          <SectionTitle icon={<Database size={18} />} label="Product state" />
          <dl className="resource-list">
            <div><dt>VODs</dt><dd>{overview?.dataset.total ?? 0}</dd></div>
            <div><dt>Downloaded</dt><dd>{overview?.dataset.downloaded ?? 0}</dd></div>
            <div><dt>Reviewed</dt><dd>{overview?.dataset.reported ?? 0}</dd></div>
            <div><dt>Completed jobs</dt><dd>{overview?.jobs.completed ?? 0}</dd></div>
            <div><dt>Failed jobs</dt><dd>{overview?.jobs.failed ?? 0}</dd></div>
            <div><dt>Uptime</dt><dd>{props.metrics ? elapsed(props.metrics.started_at) : "--"}</dd></div>
          </dl>
        </section>
        <section className="surface ops-links">
          <SectionTitle icon={<ExternalLink size={18} />} label="Service consoles" />
          <div className="service-link-grid">
            {Object.entries(overview?.links ?? {}).map(([name, href]) => (
              <a href={href} key={name} rel="noreferrer" target="_blank"><span>{humanize(name)}</span><ExternalLink size={15} /></a>
            ))}
            <a href={apiURL("/debug/pprof/")} rel="noreferrer" target="_blank"><span>Go profiler</span><ExternalLink size={15} /></a>
          </div>
        </section>
        <section className="surface ops-system">
          <SectionTitle icon={<Shield size={18} />} label="Runtime" />
          <dl className="runtime-list">
            <dt>Analyzer</dt><dd>{overview?.system.analyzer ?? "--"}</dd>
            <dt>Schema</dt><dd>{overview?.system.schema_version ?? "--"}</dd>
            <dt>Model review</dt><dd>{overview?.system.model_review_enabled ? "enabled" : "disabled"}</dd>
            <dt>Processed root</dt><dd>{overview?.system.processed_root ?? "--"}</dd>
          </dl>
        </section>
      </div>
    </>
  );
}

function Metrics(props: { metrics: AdminMetrics | null; telemetry: TelemetrySnapshot | null }) {
  const series = (props.telemetry?.series ?? []).filter((item) => item.id && item.points.length > 0);
  const requests = props.metrics?.requests ?? [];
  const maxRequests = Math.max(1, ...requests.map((item) => item.count));
  return (
    <div className="metrics-page-layout">
      <div className="telemetry-chart-grid">
        {series.map((item, index) => <MetricChart accent={index % 3} key={item.id} series={item} />)}
        {series.length === 0 && <section className="surface"><InlineEmpty label="Prometheus has no series for this window" /></section>}
      </div>
      <section className="surface route-metrics">
        <SectionTitle icon={<BarChart3 size={18} />} label="HTTP routes" />
        <div className="route-metric-table">
          {requests.slice().sort((a, b) => b.count - a.count).slice(0, 20).map((item) => (
            <div className="route-metric-row" key={`${item.method}-${item.route}-${item.status}`}>
              <span className={`http-status status-${Math.floor(item.status / 100)}xx`}>{item.status}</span>
              <code>{item.method} {item.route}</code>
              <div><i style={{ width: `${Math.max(2, item.count / maxRequests * 100)}%` }} /></div>
              <strong>{item.count}</strong>
              <small>{item.count ? `${(item.duration_seconds / item.count * 1000).toFixed(0)} ms` : "--"}</small>
            </div>
          ))}
          {requests.length === 0 && <InlineEmpty label="No HTTP metrics recorded" />}
        </div>
      </section>
    </div>
  );
}

function Logs(props: {
  entries: CentralLog[];
  fallback: RequestLog[];
  services: string[];
  levels: string[];
  query: string;
  service: string;
  level: string;
  setQuery: (value: string) => void;
  setService: (value: string) => void;
  setLevel: (value: string) => void;
  grafanaURL?: string;
}) {
  return (
    <section className="surface centralized-logs">
      <div className="log-toolbar">
        <label className="admin-search"><Search size={16} /><input onChange={(event) => props.setQuery(event.target.value)} placeholder="Search logs or trace ID" value={props.query} /></label>
        <select aria-label="Service" onChange={(event) => props.setService(event.target.value)} value={props.service}>
          <option value="all">All services</option>
          {props.services.map((service) => <option key={service} value={service}>{service}</option>)}
        </select>
        <select aria-label="Level" onChange={(event) => props.setLevel(event.target.value)} value={props.level}>
          <option value="all">All levels</option>
          {props.levels.map((level) => <option key={level} value={level}>{level}</option>)}
        </select>
        {props.grafanaURL && <a className="open-explore" href={`${props.grafanaURL}/explore`} rel="noreferrer" target="_blank"><ExternalLink size={15} />Explore</a>}
      </div>
      <div className="central-log-table">
        {props.entries.map((entry, index) => (
          <article className="central-log-row" key={`${entry.timestamp}-${entry.service}-${index}`}>
            <time>{new Date(entry.timestamp).toLocaleTimeString()}</time>
            <span className={`log-level level-${entry.level ?? "unknown"}`}>{entry.level ?? "unknown"}</span>
            <strong>{entry.service}</strong>
            <p>{entry.message}</p>
            {entry.trace_id ? <code title="Trace ID">{entry.trace_id.slice(0, 12)}</code> : <span />}
          </article>
        ))}
        {props.entries.length === 0 && (
          <div className="request-log-fallback">
            <InlineEmpty label="No centralized logs; showing API request buffer" />
            {props.fallback.slice(0, 30).map((entry, index) => (
              <article className="fallback-log-row" key={`${entry.time}-${entry.path}-${index}`}>
                <time>{new Date(entry.time).toLocaleTimeString()}</time><span>{entry.status}</span><code>{entry.method} {entry.route}</code><p>{entry.path}</p><small>{entry.duration_ms.toFixed(1)} ms</small>
              </article>
            ))}
          </div>
        )}
      </div>
    </section>
  );
}

function UserDirectory({ users }: { users: AuthUser[] }) {
  return (
    <section className="surface user-directory">
      <SectionTitle icon={<Users size={18} />} label="Accounts" />
      <div className="account-table">
        <div className="account-table-head"><span>User</span><span>Role</span><span>Created</span><span>Last login</span></div>
        {users.map((user) => (
          <article className="account-row" key={user.id}>
            <div><strong>{user.display_name}</strong><small>{user.email}</small></div>
            <span className={`role-chip role-${user.role}`}>{user.role}</span>
            <time>{formatDate(user.created_at)}</time>
            <time>{user.last_login_at ? formatDate(user.last_login_at) : "never"}</time>
          </article>
        ))}
        {users.length === 0 && <InlineEmpty label="No accounts" />}
      </div>
    </section>
  );
}

function OpsKPI(props: { icon: React.ReactNode; label: string; value: string; state: "ok" | "warning" | "bad" | "active" | "neutral" }) {
  return <div className={`ops-kpi state-${props.state}`}><span>{props.icon}</span><div><small>{props.label}</small><strong>{props.value}</strong></div></div>;
}

function SectionTitle(props: { icon: React.ReactNode; label: string }) {
  return <header className="ops-section-title"><h2>{props.label}</h2>{props.icon}</header>;
}

function MetricChart({ series, accent }: { series: MetricSeries; accent: number }) {
  const values = series.points.map((point) => point.value);
  const min = Math.min(...values);
  const max = Math.max(...values);
  const range = max - min || Math.max(1, Math.abs(max));
  const points = series.points.map((point, index) => {
    const x = series.points.length === 1 ? 50 : index / (series.points.length - 1) * 100;
    const y = 42 - (point.value - min) / range * 34;
    return `${x.toFixed(2)},${y.toFixed(2)}`;
  }).join(" ");
  const latest = values.at(-1) ?? 0;
  return (
    <article className={`metric-chart accent-${accent}`}>
      <header><div><small>{series.label}</small><strong>{formatMetric(latest, series.unit)}</strong></div><span>{series.unit}</span></header>
      <svg aria-label={`${series.label} chart`} preserveAspectRatio="none" role="img" viewBox="0 0 100 48">
        <line x1="0" x2="100" y1="8" y2="8" /><line x1="0" x2="100" y1="25" y2="25" /><line x1="0" x2="100" y1="42" y2="42" />
        <polyline points={points} />
      </svg>
      <footer><span>{formatMetric(min, series.unit)}</span><span>{formatMetric(max, series.unit)}</span></footer>
    </article>
  );
}

function InlineEmpty({ label }: { label: string }) {
  return <div className="inline-empty"><FileText size={18} /><span>{label}</span></div>;
}

function latestValue(snapshot: TelemetrySnapshot | null, id: string) {
  return snapshot?.series.find((series) => series.id === id)?.points.at(-1)?.value ?? null;
}

function uniqueValues(values: string[]) {
  return Array.from(new Set(values.filter(Boolean))).sort();
}

function humanize(value: string) {
  return value.replaceAll("_", " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function elapsed(value: string) {
  const seconds = Math.max(0, (Date.now() - new Date(value).getTime()) / 1000);
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ${Math.floor(seconds % 3600 / 60)}m`;
  return `${Math.floor(seconds / 86400)}d`;
}

function formatDate(value: string) {
  return new Date(value).toLocaleString([], { month: "short", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function formatMetric(value: number, unit: string) {
  if (!Number.isFinite(value)) return "--";
  if (unit === "ratio") return `${(value * 100).toFixed(1)}%`;
  if (unit === "s") return value >= 60 ? `${(value / 60).toFixed(1)}m` : `${value.toFixed(1)}s`;
  if (Math.abs(value) >= 1000) return Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 }).format(value);
  return value.toFixed(Math.abs(value) < 10 ? 2 : 0);
}

function messageFromError(error: unknown) {
  return error instanceof Error ? error.message : "Operations request failed";
}
