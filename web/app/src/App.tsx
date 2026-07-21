import {
  Activity,
  AlertTriangle,
  BarChart3,
  CheckCircle2,
  ChevronRight,
  Clock3,
  Crosshair,
  Database,
  FileText,
  FileJson2,
  Gauge,
  History,
  Play,
  Radar,
  RefreshCw,
  Search,
  Shield,
  Swords,
  Timer,
  Video
} from "lucide-react";
import type { ReactNode } from "react";
import { useEffect, useMemo, useState } from "react";

type VODItem = {
  label: string;
  rank: string;
  title: string;
  channel: string;
  video_id: string;
  source_url: string;
  duration_text: string;
  duration_seconds: number;
  rank_source: string;
  notes: string;
  enabled: boolean;
  local_status: string;
  local_size_bytes: number;
  report_count: number;
  latest_report_id?: string;
  latest_generated?: string;
  latest_report_path?: string;
};

type VODListResponse = {
  generated_at: string;
  counts: {
    total: number;
    enabled: number;
    downloaded: number;
    reported: number;
  };
  vods: VODItem[];
};

type Finding = {
  id: string;
  severity: string;
  category: string;
  title: string;
  detail: string;
  evidence?: Array<{
    artifact_type: string;
    path: string;
    timestamp_seconds?: number;
    frame_index?: number;
  }>;
  tags?: string[];
};

type Frame = {
  index: number;
  timestamp_seconds: number;
  path: string;
};

type Report = {
  run_id: string;
  status: string;
  generated_at: string;
  vod: {
    label: string;
    rank: string;
    title: string;
    channel: string;
    source_url: string;
  };
  media: {
    duration_seconds?: number;
    has_duration: boolean;
    size_bytes?: number;
    has_size: boolean;
    video_codec?: string;
    width?: number;
    height?: number;
    frame_rate?: string;
    audio_codec?: string;
    has_audio: boolean;
  };
  sample: {
    name: string;
    manifest_path: string;
    fps: string;
    start_seconds: number;
    duration_seconds?: number;
    frame_count: number;
    frames?: Frame[];
  };
  findings: Finding[];
  timeline: Array<{
    timestamp_seconds: number;
    type: string;
    title: string;
    detail?: string;
  }>;
  artifacts: Array<{
    type: string;
    format: string;
    path: string;
  }>;
  metadata: {
    analyzer: string;
    mode: string;
  };
};

type AnalyzeResponse = {
  report: Report;
  report_json: string;
  report_md: string;
};

type ReportSummary = {
  run_id: string;
  status: string;
  generated_at: string;
  finding_count: number;
  frame_count: number;
  sample_name: string;
  sample_fps: string;
  sample_duration_seconds?: number;
  json_path: string;
  markdown_path: string;
};

type ReportListResponse = {
  vod_label: string;
  reports: ReportSummary[];
};

const ranks = ["all", "iron", "bronze", "silver", "gold", "platinum", "diamond", "ascendant", "immortal", "radiant"];

export function App() {
  const [vods, setVods] = useState<VODItem[]>([]);
  const [counts, setCounts] = useState<VODListResponse["counts"] | null>(null);
  const [selectedLabel, setSelectedLabel] = useState("");
  const [rank, setRank] = useState("all");
  const [query, setQuery] = useState("");
  const [report, setReport] = useState<Report | null>(null);
  const [reportHistory, setReportHistory] = useState<ReportSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadingReport, setLoadingReport] = useState(false);
  const [analyzing, setAnalyzing] = useState(false);
  const [error, setError] = useState("");
  const [runDuration, setRunDuration] = useState(10);
  const [runFps, setRunFps] = useState("1");

  const selectedVod = useMemo(() => vods.find((vod) => vod.label === selectedLabel) ?? null, [selectedLabel, vods]);
  const filteredVods = useMemo(() => {
    return vods.filter((vod) => {
      const rankOk = rank === "all" || vod.rank === rank;
      const text = `${vod.label} ${vod.rank} ${vod.title} ${vod.channel}`.toLowerCase();
      const queryOk = !query.trim() || text.includes(query.trim().toLowerCase());
      return rankOk && queryOk;
    });
  }, [query, rank, vods]);

  useEffect(() => {
    void loadVods();
  }, []);

  useEffect(() => {
    if (!selectedLabel) {
      setReport(null);
      setReportHistory([]);
      return;
    }
    void loadReports(selectedLabel);
  }, [selectedLabel]);

  async function loadVods() {
    setLoading(true);
    setError("");
    try {
      const response = await fetch(apiURL("/api/vods"));
      if (!response.ok) {
        throw new Error(await readError(response));
      }
      const payload = (await response.json()) as VODListResponse;
      setVods(payload.vods);
      setCounts(payload.counts);
      setSelectedLabel((current) => current || payload.vods.find((vod) => vod.report_count > 0)?.label || payload.vods[0]?.label || "");
    } catch (err) {
      setError(messageFromError(err));
    } finally {
      setLoading(false);
    }
  }

  async function loadReports(label: string) {
    setLoadingReport(true);
    setError("");
    try {
      const response = await fetch(apiURL(`/api/reports?vod_label=${encodeURIComponent(label)}`));
      if (!response.ok) {
        throw new Error(await readError(response));
      }
      const payload = (await response.json()) as ReportListResponse;
      setReportHistory(payload.reports);
      if (payload.reports.length === 0) {
        setReport(null);
        return;
      }
      await loadReport(label, payload.reports[0].run_id);
    } catch (err) {
      setError(messageFromError(err));
      setReport(null);
      setReportHistory([]);
    } finally {
      setLoadingReport(false);
    }
  }

  async function loadReport(label: string, runID: string) {
    setLoadingReport(true);
    setError("");
    try {
      const response = await fetch(apiURL(`/api/reports/${encodeURIComponent(label)}/${encodeURIComponent(runID)}`));
      if (!response.ok) {
        throw new Error(await readError(response));
      }
      setReport((await response.json()) as Report);
    } catch (err) {
      setError(messageFromError(err));
      setReport(null);
    } finally {
      setLoadingReport(false);
    }
  }

  async function runAnalysis() {
    if (!selectedVod || analyzing) {
      return;
    }
    setAnalyzing(true);
    setError("");
    try {
      const response = await fetch(apiURL("/api/analysis-runs"), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          vod_label: selectedVod.label,
          run_id: `ui_${compactTimestamp(new Date())}`,
          fps: runFps,
          duration_seconds: runDuration,
          image_quality: 3,
          force: true
        })
      });
      if (!response.ok) {
        throw new Error(await readError(response));
      }
      const payload = (await response.json()) as AnalyzeResponse;
      setReport(payload.report);
      await loadVods();
      await loadReports(selectedVod.label);
      setSelectedLabel(selectedVod.label);
    } catch (err) {
      setError(messageFromError(err));
    } finally {
      setAnalyzing(false);
    }
  }

  const sampleFrames = report?.sample.frames?.slice(0, 12) ?? [];

  return (
    <main className="app-shell">
      <div className="ambient-grid" />
      <aside className="sidebar">
        <div className="brand-lockup">
          <div className="brand-mark">
            <Crosshair size={22} />
          </div>
          <div>
            <div className="brand-title">VOD COACH</div>
            <div className="brand-subtitle">TACTICAL REVIEW OS</div>
          </div>
        </div>

        <div className="search-box">
          <Search size={16} />
          <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search VOD, rank, channel" />
        </div>

        <div className="rank-strip" aria-label="Rank filter">
          {ranks.map((rankOption) => (
            <button
              className={rank === rankOption ? "rank-pill active" : "rank-pill"}
              key={rankOption}
              onClick={() => setRank(rankOption)}
              type="button"
            >
              {rankOption}
            </button>
          ))}
        </div>

        <div className="vod-list" aria-label="VOD library">
          {loading ? (
            <SkeletonList />
          ) : (
            filteredVods.map((vod) => (
              <button
                className={vod.label === selectedLabel ? "vod-row active" : "vod-row"}
                key={vod.label}
                onClick={() => setSelectedLabel(vod.label)}
                type="button"
              >
                <span className={`rank-sigil rank-${vod.rank}`}>{vod.rank.slice(0, 3)}</span>
                <span className="vod-row-main">
                  <span className="vod-row-title">{vod.title}</span>
                  <span className="vod-row-meta">
                    {vod.rank} / {vod.duration_text} / {vod.channel}
                  </span>
                </span>
                <span className={vod.local_status === "downloaded" ? "status-dot online" : "status-dot"} />
              </button>
            ))
          )}
        </div>
      </aside>

      <section className="workspace">
        <header className="topbar">
          <div>
            <p className="eyebrow">LOCAL MVP</p>
            <h1>{selectedVod?.title ?? "Valorant VOD Coach"}</h1>
          </div>
          <div className="topbar-actions">
            <button className="icon-button" onClick={() => void loadVods()} title="Refresh dataset" type="button">
              <RefreshCw size={18} />
            </button>
            <a className="source-link" href={selectedVod?.source_url ?? "#"} target="_blank" rel="noreferrer">
              <Video size={17} />
              Source
            </a>
          </div>
        </header>

        {error && (
          <div className="error-banner">
            <AlertTriangle size={18} />
            {error}
          </div>
        )}

        <div className="hud-grid">
          <Metric icon={<Database size={18} />} label="Dataset" value={counts ? `${counts.downloaded}/${counts.enabled}` : "..."} detail="downloaded" />
          <Metric icon={<FileJson2 size={18} />} label="Reports" value={counts ? String(counts.reported) : "..."} detail="VODs ready" />
          <Metric icon={<Gauge size={18} />} label="Analyzer" value={report?.metadata.analyzer ?? "baseline"} detail={report?.metadata.mode ?? "local"} />
          <Metric icon={<Timer size={18} />} label="Sample" value={report ? `${report.sample.frame_count}` : "0"} detail="frames" />
        </div>

        <div className="primary-grid">
          <section className="control-deck">
            <div className="panel-heading">
              <div>
                <p className="eyebrow">RUN CONTROL</p>
                <h2>Analysis pipeline</h2>
              </div>
              <span className={analyzing ? "live-chip active" : "live-chip"}>
                <Activity size={14} />
                {analyzing ? "running" : "ready"}
              </span>
            </div>

            <div className="vod-intel">
              <div className="intel-card">
                <span>Rank</span>
                <strong>{selectedVod?.rank ?? "select VOD"}</strong>
              </div>
              <div className="intel-card">
                <span>Duration</span>
                <strong>{selectedVod?.duration_text ?? "--"}</strong>
              </div>
              <div className="intel-card">
                <span>Status</span>
                <strong>{selectedVod?.local_status ?? "idle"}</strong>
              </div>
            </div>

            <div className="run-controls">
              <label>
                <span>Sample seconds</span>
                <input min={5} max={300} step={5} type="number" value={runDuration} onChange={(event) => setRunDuration(Number(event.target.value))} />
              </label>
              <label>
                <span>FPS</span>
                <select value={runFps} onChange={(event) => setRunFps(event.target.value)}>
                  <option value="0.5">0.5</option>
                  <option value="1">1</option>
                  <option value="2">2</option>
                </select>
              </label>
              <button className="run-button" disabled={!selectedVod || selectedVod.local_status !== "downloaded" || analyzing} onClick={() => void runAnalysis()} type="button">
                <Play size={18} fill="currentColor" />
                {analyzing ? "Analyzing" : "Run baseline"}
              </button>
            </div>

            <div className="pipeline-track">
              {["Manifest", "Probe", "Frames", "Report"].map((step, index) => (
                <div className={report || analyzing ? "pipeline-step lit" : "pipeline-step"} key={step}>
                  <span>{index + 1}</span>
                  {step}
                </div>
              ))}
            </div>
          </section>

          <section className="report-panel">
            <div className="panel-heading">
              <div>
                <p className="eyebrow">REPORT</p>
                <h2>{report ? `Run ${report.run_id}` : loadingReport ? "Loading report" : "No report selected"}</h2>
              </div>
              {report && (
                <span className="success-chip">
                  <CheckCircle2 size={14} />
                  {report.status}
                </span>
              )}
            </div>

            {reportHistory.length > 0 && (
              <div className="report-history">
                <div className="history-title">
                  <History size={15} />
                  Report history
                </div>
                <div className="history-list">
                  {reportHistory.map((item) => (
                    <button
                      className={report?.run_id === item.run_id ? "history-run active" : "history-run"}
                      key={item.run_id}
                      onClick={() => selectedVod && void loadReport(selectedVod.label, item.run_id)}
                      type="button"
                    >
                      <span>{item.run_id}</span>
                      <small>
                        {item.frame_count} frames / {item.finding_count} findings
                      </small>
                    </button>
                  ))}
                </div>
              </div>
            )}

            {report ? (
              <>
                <div className="report-stats">
                  <Metric icon={<Shield size={18} />} label="Media" value={formatResolution(report)} detail={report.media.frame_rate ?? "unknown"} compact />
                  <Metric icon={<Swords size={18} />} label="Findings" value={String(report.findings.length)} detail="baseline" compact />
                  <Metric icon={<Clock3 size={18} />} label="Coverage" value={`${Math.round(report.sample.duration_seconds ?? 0)}s`} detail={`${report.sample.fps} fps`} compact />
                </div>

                <div className="artifact-actions">
                  <a href={artifactURL(reportPath(reportHistory, report.run_id, "json"))} target="_blank" rel="noreferrer">
                    <FileJson2 size={15} />
                    JSON
                  </a>
                  <a href={artifactURL(reportPath(reportHistory, report.run_id, "markdown"))} target="_blank" rel="noreferrer">
                    <FileText size={15} />
                    Markdown
                  </a>
                </div>

                <div className="finding-list">
                  {report.findings.map((finding) => (
                    <article className={`finding severity-${finding.severity}`} key={finding.id}>
                      <div>
                        <span>{finding.severity}</span>
                        <h3>{finding.title}</h3>
                      </div>
                      <p>{finding.detail}</p>
                    </article>
                  ))}
                </div>
              </>
            ) : (
              <div className="empty-state">
                <Radar size={34} />
                <h3>No generated report</h3>
                <p>{selectedVod?.local_status === "downloaded" ? "Run baseline analysis for this VOD." : "This VOD is not downloaded locally."}</p>
              </div>
            )}
          </section>
        </div>

        <div className="secondary-grid">
          <section className="timeline-panel">
            <div className="panel-heading">
              <div>
                <p className="eyebrow">TIMELINE</p>
                <h2>Events</h2>
              </div>
              <BarChart3 size={19} />
            </div>
            <div className="timeline">
              {(report?.timeline ?? []).map((event) => (
                <div className="timeline-event" key={`${event.type}-${event.timestamp_seconds}`}>
                  <span>{formatSeconds(event.timestamp_seconds)}</span>
                  <div>
                    <strong>{event.title}</strong>
                    <small>{event.type}</small>
                  </div>
                </div>
              ))}
              {!report?.timeline?.length && <div className="muted-line">No timeline events.</div>}
            </div>
          </section>

          <section className="frames-panel">
            <div className="panel-heading">
              <div>
                <p className="eyebrow">EVIDENCE</p>
                <h2>Sample frames</h2>
              </div>
              <ChevronRight size={19} />
            </div>
            <div className="frame-grid">
              {sampleFrames.map((frame) => (
                <figure className="frame-tile" key={frame.path}>
                  <img src={artifactURL(frame.path)} alt={`Frame ${frame.index}`} loading="lazy" />
                  <figcaption>{formatSeconds(frame.timestamp_seconds)}</figcaption>
                </figure>
              ))}
              {!sampleFrames.length && <div className="muted-line">No frames loaded.</div>}
            </div>
          </section>
        </div>
      </section>
    </main>
  );
}

function Metric(props: { icon: ReactNode; label: string; value: string; detail: string; compact?: boolean }) {
  return (
    <div className={props.compact ? "metric compact" : "metric"}>
      <span className="metric-icon">{props.icon}</span>
      <div>
        <span>{props.label}</span>
        <strong>{props.value}</strong>
        <small>{props.detail}</small>
      </div>
    </div>
  );
}

function SkeletonList() {
  return (
    <>
      {Array.from({ length: 7 }).map((_, index) => (
        <div className="vod-row skeleton" key={index} />
      ))}
    </>
  );
}

async function readError(response: Response) {
  try {
    const payload = (await response.json()) as { error?: string };
    return payload.error || response.statusText;
  } catch {
    return response.statusText;
  }
}

function messageFromError(error: unknown) {
  if (error instanceof Error) {
    return error.message;
  }
  return "Unknown error";
}

function compactTimestamp(date: Date) {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getUTCFullYear()}${pad(date.getUTCMonth() + 1)}${pad(date.getUTCDate())}T${pad(date.getUTCHours())}${pad(date.getUTCMinutes())}${pad(date.getUTCSeconds())}Z`;
}

function formatResolution(report: Report) {
  if (!report.media.width || !report.media.height) {
    return "unknown";
  }
  return `${report.media.width}x${report.media.height}`;
}

function formatSeconds(seconds: number) {
  return `${seconds.toFixed(seconds % 1 === 0 ? 0 : 1)}s`;
}

function artifactURL(path: string) {
  if (!path) {
    return "#";
  }
  const normalized = path.replaceAll("\\", "/");
  const marker = "data/processed/";
  const index = normalized.indexOf(marker);
  if (index >= 0) {
    return apiURL(`/artifacts/${normalized.slice(index + marker.length)}`);
  }
  return apiURL(`/artifacts/${normalized.replace(/^\/+/, "")}`);
}

function reportPath(history: ReportSummary[], runID: string, kind: "json" | "markdown") {
  const item = history.find((entry) => entry.run_id === runID);
  if (!item) {
    return "";
  }
  return kind === "json" ? item.json_path : item.markdown_path;
}

function apiURL(path: string) {
  const explicitBase = import.meta.env.VITE_API_BASE as string | undefined;
  const base = explicitBase || devBackendBase();
  return `${base}${path}`;
}

function devBackendBase() {
  if (window.location.port === "5173") {
    return "http://127.0.0.1:8080";
  }
  return "";
}
